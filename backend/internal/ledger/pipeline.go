package ledger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/ingest"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// The import pipeline (PLAN §3.2, tasks 6.2, 6.4, 6.10): upload → parse → profile → roles → account
// suggestion → stage → store → preview; PATCH re-reads the stored bytes with new settings.

const (
	previewHead = 50   // first staged rows in the preview
	previewTail = 5    // last staged rows, when the file is longer than head + tail
	sniffBytes  = 8192 // NUL sniff window
	// LinesLimit is the default number of records GET /imports/{id}/lines returns.
	LinesLimit = 18
)

// Row is a staged row as the preview and GET /imports/{id}/rows show it.
type Row struct {
	RowIndex            int
	BookedOn            *platform.Date
	ValueOn             *platform.Date
	AmountMinor         *int64
	Currency            string
	Payee               string
	Description         string
	Reference           string
	TxType              string
	BankCategory        string
	CounterpartyAccount string
	BalanceMinor        *int64
	Raw                 []ingest.RawField
	Error               string
	Duplicate           bool       // see ListImportRows
	TransactionID       *uuid.UUID // set once committed
}

func rowFromStaged(r *ingest.StagedRow) Row {
	out := Row{RowIndex: r.RowIndex, Raw: r.Raw, Error: r.Error}
	if r.Error != "" {
		return out
	}
	d, a := r.BookedOn, r.AmountMinor
	out.BookedOn, out.ValueOn, out.AmountMinor, out.BalanceMinor = &d, r.ValueOn, &a, r.BalanceMinor
	out.Currency, out.Payee, out.Description, out.Reference = r.Currency, r.Payee, r.Description, r.Reference
	out.TxType, out.BankCategory, out.CounterpartyAccount = r.TxType, r.BankCategory, r.CounterpartyAccount
	return out
}

// PreviewStats summarise a staged import.
type PreviewStats struct {
	Rows               int
	Errors             int
	DuplicatesEstimate int // valid rows the account already has (0 without an account)
	NewEstimate        int
	DateFrom, DateTo   *platform.Date
	HeaderAt           int // 1-based record of the header; 0 = none
	Dropped            int // short summary rows removed
}

// SuggestedAccount is the account suggestion of PLAN §4.4: an existing account whose number hint
// matches the file's own-account column, or a name for a new one.
type SuggestedAccount struct {
	ID         *uuid.UUID
	CreateName string
}

// Warning is a non-blocking remark on a preview.
type Warning struct {
	Code    string
	Message string
}

// Preview is what POST and PATCH /imports return.
type Preview struct {
	Import     Import
	Mapping    MappingDoc
	Options    OptionsDoc
	Stats      PreviewStats
	Suggested  SuggestedAccount
	SampleRows []Row
	TailRows   []Row
	Missing    []string // required roles not mapped yet; commit needs this empty
	Warnings   []Warning
}

// reading is one read of a file: the parse, the settings used and the staged rows.
type reading struct {
	res     *ingest.Result
	mapping MappingDoc
	options OptionsDoc
	source  string // detected | profile | user
	rows    []ingest.StagedRow
	stats   ingest.Stats
}

// read parses content with o (DefaultOptions when nil) and stages it with m (the detected roles when
// nil). With useProfile, a remembered profile for the file's layout replaces both (mappingSource
// "profile"). accountCurrency is the last currency fallback.
func (l *Ledger) read(ctx context.Context, userID uuid.UUID, content []byte, o *OptionsDoc, m *MappingDoc, useProfile bool, accountCurrency string) (reading, error) {
	opts := DefaultOptions()
	if o != nil {
		opts = *o
	}
	po, err := opts.ParseOptions()
	if err != nil {
		return reading{}, err
	}
	res, err := ingest.Parse(ctx, content, po)
	if err != nil {
		return reading{}, parseErr(err)
	}
	rd := reading{res: res, options: opts, source: "detected"}
	switch {
	case m != nil:
		rd.mapping, rd.source = *m, "user"
	case useProfile:
		p, err := l.GetProfile(ctx, userID, ingest.HeaderSignature(res.Table.Headers, res.Delimiter))
		if err != nil && !errors.Is(err, ErrNotFound) {
			return reading{}, err
		}
		if err == nil {
			var pm MappingDoc
			var pOpts OptionsDoc
			if json.Unmarshal(p.Mapping, &pm) == nil && json.Unmarshal(p.Options, &pOpts) == nil {
				if again, err := l.read(ctx, userID, content, &pOpts, &pm, false, accountCurrency); err == nil {
					again.source = "profile"
					return again, nil
				}
				// A profile that no longer fits the file falls back to detection below.
			}
		}
		rd.mapping = MappingFromRoles(res.Roles, res.Table.Headers)
	default:
		rd.mapping = MappingFromRoles(res.Roles, res.Table.Headers)
	}
	im, so, err := Resolve(rd.mapping, rd.options, res.Table.Headers)
	if err != nil {
		return reading{}, err
	}
	if so.ImportCurrency == "" {
		so.ImportCurrency = res.Roles.Currency
	}
	so.AccountCurrency = accountCurrency
	rd.rows, rd.stats, err = ingest.Stage(ctx, res.Table, im, so)
	return rd, err
}

// parseErr turns ingest's input errors into validation errors (422); everything else passes.
func parseErr(err error) error {
	for _, e := range []error{ingest.ErrEmptyFile, ingest.ErrHeaderRowOutOfRange, ingest.ErrTooManyRows, ingest.ErrTooManyColumns, ingest.ErrUnknownEncoding} {
		if errors.Is(err, e) {
			ve := &platform.ValidationError{}
			ve.Add("file", err.Error())
			return ve
		}
	}
	return err
}

// Upload is POST /imports.
type Upload struct {
	Filename  string
	Content   []byte
	AccountID *uuid.UUID // optional; otherwise a unique number-hint match is pre-filled
}

// CreatePreview stores an uploaded file as a previewed import (PLAN task 6.2). A file whose first
// 8 KiB contain a NUL byte is not text (platform.ErrUnsupportedFile); identical bytes already
// imported are ErrDuplicateFile.
func (l *Ledger) CreatePreview(ctx context.Context, userID uuid.UUID, up Upload) (Preview, error) {
	if bytes.IndexByte(up.Content[:min(len(up.Content), sniffBytes)], 0) >= 0 {
		return Preview{}, fmt.Errorf("%s is not a text (CSV/TSV) file: %w", up.Filename, platform.ErrUnsupportedFile)
	}
	rd, err := l.read(ctx, userID, up.Content, nil, nil, true, "")
	if err != nil {
		return Preview{}, err
	}
	suggested, err := l.suggest(ctx, userID, rd)
	if err != nil {
		return Preview{}, err
	}
	account := up.AccountID
	if account == nil {
		account = suggested.ID // pre-fill a unique match (decision of 2026-10-21)
	}
	if account != nil {
		a, err := l.GetAccount(ctx, userID, *account)
		if err != nil {
			return Preview{}, err
		}
		if a.Currency != "PLN" { // re-stage so rows without a currency get the account's
			if rd, err = l.read(ctx, userID, up.Content, &rd.options, &rd.mapping, false, a.Currency); err != nil {
				return Preview{}, err
			}
			if rd.source == "user" {
				rd.source = "detected"
			}
		}
	}
	stats := previewStats(rd)
	statsJSON, _ := json.Marshal(storedStats(stats))
	mapping, _ := json.Marshal(rd.mapping)
	options, _ := json.Marshal(rd.options)
	imp, err := l.CreateImport(ctx, userID, NewImport{AccountID: account, Filename: up.Filename, Content: up.Content,
		Encoding: string(rd.res.Encoding), Delimiter: DelimiterName(rd.res.Delimiter), Headers: rd.res.Table.Headers,
		ColumnTypes: columnTypes(rd.res), Mapping: mapping, Options: options, MappingSource: rd.source,
		HeaderSignature: ingest.HeaderSignature(rd.res.Table.Headers, rd.res.Delimiter), Stats: statsJSON, Rows: rd.rows})
	if err != nil {
		return Preview{}, err
	}
	return l.preview(ctx, userID, imp, rd, stats, suggested)
}

// Changes is PATCH /imports/{id}: any of the account, the mapping and the options.
type Changes struct {
	AccountID *uuid.UUID
	Mapping   *MappingDoc
	Options   *OptionsDoc
}

// UpdatePreview applies changes to a previewed import: it re-reads the stored bytes with the
// (new) options, keeps the stored mapping while the headers stay the same (re-detecting it when
// they change, e.g. another header row), re-stages and returns the new preview (task 6.4).
func (l *Ledger) UpdatePreview(ctx context.Context, userID, id uuid.UUID, ch Changes) (Preview, error) {
	imp, content, err := l.importWithFile(ctx, userID, id)
	if err != nil {
		return Preview{}, err
	}
	if imp.Status != StatusPreviewed {
		return Preview{}, fmt.Errorf("only a previewed import can be changed (this one is %s): %w", imp.Status, ErrState)
	}
	opts, mapping, err := storedDocs(imp)
	if err != nil {
		return Preview{}, err
	}
	if ch.Options != nil {
		opts = *ch.Options
	}
	account := imp.AccountID
	if ch.AccountID != nil {
		account = ch.AccountID
	}
	currency := ""
	if account != nil {
		a, err := l.GetAccount(ctx, userID, *account)
		if err != nil {
			return Preview{}, err
		}
		currency = a.Currency
	}
	var rd reading
	switch {
	case ch.Mapping != nil:
		rd, err = l.read(ctx, userID, content, &opts, ch.Mapping, false, currency)
	default:
		// Try the stored mapping; if the new options changed the columns, detect again.
		rd, err = l.read(ctx, userID, content, &opts, &mapping, false, currency)
		var ve *platform.ValidationError
		if errors.As(err, &ve) && slices.ContainsFunc(ve.Fields, func(f platform.FieldError) bool { return len(f.Field) > 8 && f.Field[:8] == "mapping." }) {
			rd, err = l.read(ctx, userID, content, &opts, nil, false, currency)
		} else if err == nil {
			rd.source = imp.MappingSource
		}
		if ch.Options != nil && rd.source == "profile" {
			rd.source = "user"
		}
	}
	if err != nil {
		return Preview{}, err
	}
	stats := previewStats(rd)
	statsJSON, _ := json.Marshal(storedStats(stats))
	mj, _ := json.Marshal(rd.mapping)
	oj, _ := json.Marshal(rd.options)
	updated, err := l.Restage(ctx, userID, id, Staging{AccountID: account, Encoding: string(rd.res.Encoding),
		Delimiter: DelimiterName(rd.res.Delimiter), Headers: rd.res.Table.Headers, ColumnTypes: columnTypes(rd.res),
		Mapping: mj, Options: oj, MappingSource: rd.source, HeaderSignature: ingest.HeaderSignature(rd.res.Table.Headers, rd.res.Delimiter),
		Stats: statsJSON, Rows: rd.rows}, nil)
	if err != nil {
		return Preview{}, err
	}
	suggested, err := l.suggest(ctx, userID, rd)
	if err != nil {
		return Preview{}, err
	}
	return l.preview(ctx, userID, updated, rd, stats, suggested)
}

// ImportLines returns the first limit decoded records of an import with their status under its
// current options (the raw-lines panel; task 6.10).
func (l *Ledger) ImportLines(ctx context.Context, userID, id uuid.UUID, limit int) (*ingest.LinesResult, error) {
	imp, content, err := l.importWithFile(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	opts, _, err := storedDocs(imp)
	if err != nil {
		return nil, err
	}
	po, err := opts.ParseOptions()
	if err != nil {
		return nil, err
	}
	out, err := ingest.Lines(ctx, content, po, min(max(limit, 1), 200))
	return out, parseErr(err)
}

// ImportFile returns the stored bytes of an import.
func (l *Ledger) ImportFile(ctx context.Context, userID, id uuid.UUID) (Import, []byte, error) {
	return l.importWithFile(ctx, userID, id)
}

func (l *Ledger) importWithFile(ctx context.Context, userID, id uuid.UUID) (Import, []byte, error) {
	var imp Import
	var content []byte
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		i, err := q.GetImport(ctx, db.GetImportParams{UserID: userID, ID: id})
		if err != nil {
			return err
		}
		if imp, err = importOf(i); err != nil {
			return err
		}
		content, err = q.GetImportFile(ctx, db.GetImportFileParams{UserID: userID, ImportID: id})
		return err
	})
	return imp, content, dbErr("import", err)
}

// storedDocs decodes an import's mapping and options.
func storedDocs(imp Import) (OptionsDoc, MappingDoc, error) {
	opts := DefaultOptions()
	var m MappingDoc
	if err := json.Unmarshal(imp.Options, &opts); err != nil {
		return opts, m, fmt.Errorf("import %s options: %w", imp.ID, err)
	}
	if err := json.Unmarshal(imp.Mapping, &m); err != nil {
		return opts, m, fmt.Errorf("import %s mapping: %w", imp.ID, err)
	}
	return opts, m, nil
}

// suggest implements the account suggestion: the most common digit string of the own-account
// column; its last four digits against the accounts' number hints.
func (l *Ledger) suggest(ctx context.Context, userID uuid.UUID, rd reading) (SuggestedAccount, error) {
	counts := map[string]int{}
	best := ""
	for i := range rd.rows {
		d := onlyDigits(rd.rows[i].OwnAccount)
		if len(d) < 4 {
			continue
		}
		counts[d]++
		if counts[d] > counts[best] || (counts[d] == counts[best] && d < best) {
			best = d
		}
	}
	if best == "" {
		return SuggestedAccount{}, nil
	}
	matches, err := l.SuggestAccounts(ctx, userID, best)
	if err != nil {
		return SuggestedAccount{}, err
	}
	switch len(matches) {
	case 0:
		return SuggestedAccount{CreateName: "Account ••" + best[len(best)-4:]}, nil
	case 1:
		return SuggestedAccount{ID: &matches[0].ID}, nil
	}
	return SuggestedAccount{}, nil // several accounts share the hint: let the user choose
}

func previewStats(rd reading) PreviewStats {
	s := PreviewStats{Rows: len(rd.rows), HeaderAt: rd.res.Table.HeaderAt, Dropped: rd.res.Table.Dropped}
	for i := range rd.rows {
		r := &rd.rows[i]
		if r.Error != "" {
			s.Errors++
			continue
		}
		d := r.BookedOn
		if s.DateFrom == nil || d.Before(*s.DateFrom) {
			s.DateFrom = &d
		}
		if s.DateTo == nil || d.After(*s.DateTo) {
			d2 := d
			s.DateTo = &d2
		}
	}
	s.NewEstimate = s.Rows - s.Errors
	return s
}

// storedStats is the stats object kept on the import row (the commit adds new/duplicates).
func storedStats(s PreviewStats) map[string]any {
	m := map[string]any{"rows": s.Rows, "errors": s.Errors, "headerAt": s.HeaderAt, "dropped": s.Dropped}
	if s.DateFrom != nil {
		m["dateFrom"], m["dateTo"] = s.DateFrom.String(), s.DateTo.String()
	}
	return m
}

func columnTypes(res *ingest.Result) []string {
	out := make([]string, len(res.Columns))
	for i, c := range res.Columns {
		out[i] = string(c.Type)
	}
	return out
}

// preview assembles the response: samples from the in-memory rows, the duplicate estimate from the
// database (the account's existing transactions), the missing roles and warnings.
func (l *Ledger) preview(ctx context.Context, userID uuid.UUID, imp Import, rd reading, stats PreviewStats, suggested SuggestedAccount) (Preview, error) {
	p := Preview{Import: imp, Mapping: rd.mapping, Options: rd.options, Stats: stats, Suggested: suggested,
		Missing: Missing(rd.mapping, rd.options)}
	if imp.AccountID != nil {
		err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
			n, err := q.CountDuplicateEstimate(ctx, db.CountDuplicateEstimateParams{UserID: userID, ImportID: imp.ID, AccountID: *imp.AccountID})
			p.Stats.DuplicatesEstimate = int(n)
			return err
		})
		if err != nil {
			return Preview{}, dbErr("duplicate estimate", err)
		}
		p.Stats.NewEstimate -= p.Stats.DuplicatesEstimate
	}
	dup := map[int]bool{}
	if p.Stats.DuplicatesEstimate > 0 { // flag the sample rows from the stored rows
		head, err := l.ImportRowsPage(ctx, userID, imp.ID, false, 0, previewHead)
		if err != nil {
			return Preview{}, err
		}
		for i := range head {
			dup[head[i].RowIndex] = head[i].Duplicate
		}
		if n := len(rd.rows); n > previewHead+previewTail {
			tail, err := l.ImportRowsPage(ctx, userID, imp.ID, false, n-previewTail, previewTail)
			if err != nil {
				return Preview{}, err
			}
			for i := range tail {
				dup[tail[i].RowIndex] = tail[i].Duplicate
			}
		}
	}
	for i := range rd.rows[:min(len(rd.rows), previewHead)] {
		r := rowFromStaged(&rd.rows[i])
		r.Duplicate = dup[r.RowIndex]
		p.SampleRows = append(p.SampleRows, r)
	}
	if n := len(rd.rows); n > previewHead+previewTail {
		for i := n - previewTail; i < n; i++ {
			r := rowFromStaged(&rd.rows[i])
			r.Duplicate = dup[r.RowIndex]
			p.TailRows = append(p.TailRows, r)
		}
	}
	if stats.Errors > 0 {
		p.Warnings = append(p.Warnings, Warning{Code: "row_errors", Message: fmt.Sprintf("%d of %d rows can't be imported (see the errors)", stats.Errors, stats.Rows)})
	}
	if stats.Dropped > 0 {
		p.Warnings = append(p.Warnings, Warning{Code: "rows_dropped", Message: fmt.Sprintf("%d short summary rows were left out", stats.Dropped)})
	}
	if len(p.Missing) > 0 {
		p.Warnings = append(p.Warnings, Warning{Code: "mapping_incomplete", Message: "map the required columns before importing"})
	}
	return p, nil
}

// ErrNotCommittable wraps platform.ErrImportNotCommitable (409 import_not_committable).
func notCommittable(format string, args ...any) error {
	return fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), platform.ErrImportNotCommitable)
}

// Enqueue inserts a job in the caller's database transaction (River's InsertTx); the ledger takes it
// as a function so it doesn't depend on the job package.
type Enqueue func(ctx context.Context, tx pgx.Tx) error

// StartCommit validates a previewed import and moves it to "committing", calling enqueue in the same
// transaction (task 6.5). It needs an account, the required roles and at least one valid row; an
// import that is committing, committed or failed is import_not_committable.
func (l *Ledger) StartCommit(ctx context.Context, userID, id uuid.UUID, enqueue Enqueue) (Import, error) {
	var out Import
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, tx pgx.Tx) error {
		row, err := q.GetImport(ctx, db.GetImportParams{UserID: userID, ID: id})
		if err != nil {
			return dbErr("get import", err) // not ErrNoRows any more: that means a lost race below
		}
		imp, err := importOf(row)
		if err != nil {
			return err
		}
		if imp.Status != StatusPreviewed {
			return notCommittable("the import is %s", imp.Status)
		}
		opts, mapping, err := storedDocs(imp)
		if err != nil {
			return err
		}
		ve := &platform.ValidationError{}
		if imp.AccountID == nil {
			ve.Add("accountId", "choose or create an account first")
		}
		for _, m := range Missing(mapping, opts) {
			ve.Add("mapping."+m, "is required")
		}
		if err := ve.Err(); err != nil {
			return err
		}
		counts, err := q.CountImportRows(ctx, db.CountImportRowsParams{UserID: userID, ImportID: id})
		if err != nil {
			return err
		}
		if counts.Rows-counts.Errors == 0 {
			return notCommittable("no row can be imported")
		}
		updated, err := q.SetImportStatus(ctx, db.SetImportStatusParams{UserID: userID, ID: id, FromStatus: string(StatusPreviewed),
			ToStatus: string(StatusCommitting), Now: l.clock.Now()})
		if err != nil {
			return err
		}
		if err := enqueue(ctx, tx); err != nil {
			return err
		}
		out, err = importOf(updated)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Import{}, notCommittable("the import changed state") // lost a race with another commit
	}
	return out, dbErr("start commit", err)
}

// Reprocess re-reads a committed import's stored bytes with new options and/or mapping, restages it
// and enqueues the commit, all in one transaction (task 6.8). Manual edits survive wherever the
// fingerprint is unchanged.
func (l *Ledger) Reprocess(ctx context.Context, userID, id uuid.UUID, ch Changes, enqueue Enqueue) (Import, error) {
	imp, content, err := l.importWithFile(ctx, userID, id)
	if err != nil {
		return Import{}, err
	}
	if imp.Status != StatusCommitted {
		return Import{}, notCommittable("only a committed import can be reprocessed (this one is %s)", imp.Status)
	}
	opts, mapping, err := storedDocs(imp)
	if err != nil {
		return Import{}, err
	}
	if ch.Options != nil {
		opts = *ch.Options
	}
	if ch.Mapping != nil {
		mapping = *ch.Mapping
	}
	a, err := l.GetAccount(ctx, userID, *imp.AccountID)
	if err != nil {
		return Import{}, err
	}
	rd, err := l.read(ctx, userID, content, &opts, &mapping, false, a.Currency)
	if err != nil {
		return Import{}, err
	}
	if missing := Missing(rd.mapping, rd.options); len(missing) > 0 {
		ve := &platform.ValidationError{}
		for _, m := range missing {
			ve.Add("mapping."+m, "is required")
		}
		return Import{}, ve
	}
	stats := previewStats(rd)
	statsJSON, _ := json.Marshal(storedStats(stats))
	mj, _ := json.Marshal(rd.mapping)
	oj, _ := json.Marshal(rd.options)
	return l.Restage(ctx, userID, id, Staging{Encoding: string(rd.res.Encoding), Delimiter: DelimiterName(rd.res.Delimiter),
		Headers: rd.res.Table.Headers, ColumnTypes: columnTypes(rd.res), Mapping: mj, Options: oj, MappingSource: "user",
		HeaderSignature: ingest.HeaderSignature(rd.res.Table.Headers, rd.res.Delimiter), Stats: statsJSON, Rows: rd.rows}, enqueue)
}

// FailImport records the final failure of a commit: committing → failed with a message that is safe
// to show (the job passes a sanitised one).
func (l *Ledger) FailImport(ctx context.Context, userID, id uuid.UUID, message string) error {
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		_, err := q.SetImportStatus(ctx, db.SetImportStatusParams{UserID: userID, ID: id, FromStatus: string(StatusCommitting),
			ToStatus: string(StatusFailed), Error: &message, Now: l.clock.Now()})
		return err
	})
	return dbErr("fail import", err)
}

// RememberMapping saves the import's mapping and options as the profile of its file layout ("Remember
// this mapping for files like this", on by default).
func (l *Ledger) RememberMapping(ctx context.Context, userID, id uuid.UUID) error {
	imp, err := l.GetImport(ctx, userID, id)
	if err != nil {
		return err
	}
	return l.SaveProfile(ctx, userID, Profile{HeaderSignature: imp.HeaderSignature, Name: imp.Filename, Mapping: imp.Mapping, Options: imp.Options})
}

// LayoutLabels maps each header signature of the user's imports to its source label b1, b2, … in
// first-seen order (task 6.12). It is empty while the user has only one layout: labels only help
// to tell layouts apart.
func (l *Ledger) LayoutLabels(ctx context.Context, userID uuid.UUID) (map[[32]byte]string, error) {
	out := map[[32]byte]string{}
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		rows, err := q.ImportLayoutLabels(ctx, userID)
		if err != nil || len(rows) < 2 {
			return err
		}
		for _, r := range rows {
			var sig [32]byte
			copy(sig[:], r.HeaderSignature)
			out[sig] = fmt.Sprintf("b%d", r.Position)
		}
		return nil
	})
	return out, dbErr("layout labels", err)
}
