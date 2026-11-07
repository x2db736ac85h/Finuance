package legacyimport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/x2db736ac85h/finuance/backend/internal/assistant"
	"github.com/x2db736ac85h/finuance/backend/internal/investments"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/llm"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// Errors of the migration API.
var (
	// ErrRunning: the user already has a migration queued or running (409).
	ErrRunning = errors.New("a migration is already running")
	// ErrNotFound: no such migration for this user (404).
	ErrNotFound = errors.New("migration not found")
)

// Status is the lifecycle of a run.
type Status string

// Run statuses.
const (
	StatusQueued  Status = "queued"
	StatusRunning Status = "running"
	StatusDone    Status = "done"
	StatusFailed  Status = "failed"
)

// Run is one migration as the wizard polls it.
type Run struct {
	ID         uuid.UUID
	Status     Status
	Progress   Progress
	Report     *Report
	Error      string
	CreatedAt  time.Time
	FinishedAt *time.Time
}

// Progress is how far a run is.
type Progress struct {
	FilesDone  int    `json:"filesDone"`
	FilesTotal int    `json:"filesTotal"`
	Step       string `json:"step"` // files | labels | settings | chats | done
	Current    string `json:"current,omitempty"`
}

// Report is what a run did (PLAN §4.10 step 5).
type Report struct {
	Files        []FileReport    `json:"files"`
	Accounts     []AccountReport `json:"accounts"`
	Transactions struct {
		New        int   `json:"new"`
		Duplicates int64 `json:"duplicates"`
		Errors     int64 `json:"errors"`
	} `json:"transactions"`
	Groups struct {
		Created int `json:"created"`
		Reused  int `json:"reused"`
	} `json:"groups"`
	Labels    MatchReport `json:"labels"`
	Overrides MatchReport `json:"overrides"`
	Budgets   struct {
		Set     int      `json:"set"`
		Skipped []string `json:"skipped"`
	} `json:"budgets"`
	Opening     *OpeningReport `json:"opening,omitempty"`
	Investments string         `json:"investments"` // imported | kept | none
	Chats       struct {
		Imported int `json:"imported"`
		Skipped  int `json:"skipped"`
	} `json:"chats"`
	UnknownKeys []string `json:"unknownKeys"`
}

// FileReport is the outcome of one legacy file.
type FileReport struct {
	LegacyID    string     `json:"legacyId"`
	Name        string     `json:"name"`
	Status      string     `json:"status"` // imported | reused | failed
	ImportID    *uuid.UUID `json:"importId,omitempty"`
	AccountID   *uuid.UUID `json:"accountId,omitempty"`
	AccountName string     `json:"accountName"`
	Rows        int        `json:"rows"`
	New         int        `json:"new"`
	Duplicates  int64      `json:"duplicates"`
	Errors      int64      `json:"errors"`
	Message     string     `json:"message,omitempty"`
}

// AccountReport is an account the run used.
type AccountReport struct {
	ID      uuid.UUID `json:"id"`
	Name    string    `json:"name"`
	Created bool      `json:"created"`
}

// MatchReport counts the legacy keys that found their transactions and lists the first ones that
// did not.
type MatchReport struct {
	Matched   int         `json:"matched"`
	Unmatched int         `json:"unmatched"`
	Examples  []Unmatched `json:"examples"`
}

// Unmatched is a legacy key that found no transaction.
type Unmatched struct {
	What   string `json:"what"`   // the group name, or "keep both" / "use this version"
	Key    string `json:"key"`    // the legacy key, readable (dates as yyyy-mm-dd)
	Reason string `json:"reason"` // why it did not match
}

const maxExamples = 50

func (m *MatchReport) miss(what, key, reason string) {
	m.Unmatched++
	if len(m.Examples) < maxExamples {
		m.Examples = append(m.Examples, Unmatched{What: what, Key: readableKey(key), Reason: reason})
	}
}

// OpeningReport is where the legacy opening balance went.
type OpeningReport struct {
	AccountID   uuid.UUID `json:"accountId"`
	AccountName string    `json:"accountName"`
	AmountMinor int64     `json:"amountMinor"`
}

// Migrator creates and runs migrations.
type Migrator struct {
	st     *store.Store
	ledger *ledger.Ledger
	chats  *assistant.Chats // nil: chats are reported as skipped
	clock  platform.Clock
}

// New returns a Migrator.
func New(st *store.Store, l *ledger.Ledger, chats *assistant.Chats, clock platform.Clock) *Migrator {
	return &Migrator{st: st, ledger: l, chats: chats, clock: clock}
}

// Enqueue inserts the run's job in the same database transaction (River's InsertTx).
type Enqueue func(ctx context.Context, tx pgx.Tx, id uuid.UUID) error

// Create stores a validated manifest and the files' bytes (in manifest order) and enqueues the
// run, in one transaction. A second run while one is queued or running is ErrRunning.
func (m *Migrator) Create(ctx context.Context, userID uuid.UUID, man Manifest, files [][]byte, enqueue Enqueue) (Run, error) {
	if err := man.Validate(); err != nil {
		return Run{}, err
	}
	if len(files) != len(man.Files) {
		ve := &platform.ValidationError{}
		ve.Add("files", fmt.Sprintf("expected %d files, got %d", len(man.Files), len(files)))
		return Run{}, ve.Err()
	}
	doc, err := json.Marshal(man)
	if err != nil {
		return Run{}, err
	}
	prog, _ := json.Marshal(Progress{FilesTotal: len(files), Step: "files"})
	var out Run
	err = m.st.WithUser(ctx, userID, func(q *db.Queries, tx pgx.Tx) error {
		r, err := q.CreateLegacyImport(ctx, db.CreateLegacyImportParams{ID: platform.NewID(), UserID: userID, Manifest: doc, Progress: prog, Now: m.clock.Now()})
		if err != nil {
			if pgErr := (*pgconn.PgError)(nil); errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return ErrRunning
			}
			return err
		}
		for i, b := range files {
			if err := q.AddLegacyImportFile(ctx, db.AddLegacyImportFileParams{LegacyImportID: r.ID, Position: int32(i), UserID: userID, Content: b}); err != nil { //nolint:gosec // ≤ MaxFiles
				return err
			}
		}
		if out, err = runOf(r.ID, r.Status, r.Progress, r.Report, r.Error, r.CreatedAt, r.FinishedAt); err != nil {
			return err
		}
		return enqueue(ctx, tx, r.ID)
	})
	return out, err
}

func runOf(id uuid.UUID, status string, progress, report []byte, errText *string, created time.Time, finished *time.Time) (Run, error) {
	r := Run{ID: id, Status: Status(status), CreatedAt: created, FinishedAt: finished}
	if errText != nil {
		r.Error = *errText
	}
	if len(progress) > 0 {
		if err := json.Unmarshal(progress, &r.Progress); err != nil {
			return r, err
		}
	}
	if len(report) > 0 {
		r.Report = &Report{}
		if err := json.Unmarshal(report, r.Report); err != nil {
			return r, err
		}
	}
	return r, nil
}

// Get returns one run.
func (m *Migrator) Get(ctx context.Context, userID, id uuid.UUID) (Run, error) {
	var out Run
	err := m.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		r, err := q.GetLegacyImport(ctx, db.GetLegacyImportParams{UserID: userID, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		out, err = runOf(r.ID, r.Status, r.Progress, r.Report, r.Error, r.CreatedAt, r.FinishedAt)
		return err
	})
	return out, err
}

// List returns the user's latest runs, newest first.
func (m *Migrator) List(ctx context.Context, userID uuid.UUID) ([]Run, error) {
	var out []Run
	err := m.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		rows, err := q.ListLegacyImports(ctx, userID)
		if err != nil {
			return err
		}
		for i := range rows {
			r := &rows[i]
			run, err := runOf(r.ID, r.Status, r.Progress, r.Report, r.Error, r.CreatedAt, r.FinishedAt)
			if err != nil {
				return err
			}
			out = append(out, run)
		}
		return nil
	})
	return out, err
}

// Fail ends a run with a message that is safe to show (the job's last attempt failed).
func (m *Migrator) Fail(ctx context.Context, userID, id uuid.UUID, message string) error {
	return m.finish(ctx, userID, id, StatusFailed, nil, message)
}

func (m *Migrator) finish(ctx context.Context, userID, id uuid.UUID, status Status, rep *Report, message string) error {
	var doc []byte
	if rep != nil {
		var err error
		if doc, err = json.Marshal(rep); err != nil {
			return err
		}
	}
	var msg *string
	if message != "" {
		msg = &message
	}
	now := m.clock.Now()
	return m.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		if err := q.FinishLegacyImport(ctx, db.FinishLegacyImportParams{UserID: userID, ID: id, Status: string(status), Report: doc, Error: msg, Now: &now}); err != nil {
			return err
		}
		return q.DeleteLegacyImportFiles(ctx, db.DeleteLegacyImportFilesParams{UserID: userID, LegacyImportID: id})
	})
}

func (m *Migrator) progress(ctx context.Context, userID, id uuid.UUID, p Progress) error {
	doc, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return m.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		return q.SetLegacyImportProgress(ctx, db.SetLegacyImportProgressParams{UserID: userID, ID: id, Progress: doc})
	})
}

// target is the new transaction of one legacy row.
type target struct {
	accountID, txID, importID uuid.UUID
	fingerprint               []byte
}

// state is a run in progress.
type state struct {
	userID   uuid.UUID
	runID    uuid.UUID
	man      Manifest
	order    []int // manifest indices in legacy order
	legacy   []legacyFile
	targets  [][]*target // per manifest file, per legacy row (nil: no transaction)
	imports  map[int]uuid.UUID
	accounts map[string]ledger.Account
	report   Report
}

// Run performs a migration (the LegacyImport job). It is idempotent: a retry, or running the same
// backup again, reuses the imports, accounts and groups it finds and changes nothing twice.
func (m *Migrator) Run(ctx context.Context, userID, id uuid.UUID) error {
	var man Manifest
	run, err := m.Get(ctx, userID, id)
	if err != nil {
		return err
	}
	if run.Status == StatusDone || run.Status == StatusFailed {
		return nil
	}
	err = m.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		doc, err := q.GetLegacyImportManifest(ctx, db.GetLegacyImportManifestParams{UserID: userID, ID: id})
		if err != nil {
			return err
		}
		return json.Unmarshal(doc, &man)
	})
	if err != nil {
		return err
	}
	s := &state{userID: userID, runID: id, man: man, order: man.sortedFiles(), legacy: make([]legacyFile, len(man.Files)),
		targets: make([][]*target, len(man.Files)), imports: map[int]uuid.UUID{}, accounts: map[string]ledger.Account{}}
	s.report.Investments = "none"
	s.report.UnknownKeys = append([]string{}, man.UnknownKeys...)
	s.report.Budgets.Skipped = []string{}
	s.report.Accounts = []AccountReport{}
	s.report.Labels.Examples, s.report.Overrides.Examples = []Unmatched{}, []Unmatched{}
	if err := m.migrateFiles(ctx, s, run.CreatedAt); err != nil {
		return err
	}
	if err := m.progress(ctx, userID, id, Progress{FilesDone: len(man.Files), FilesTotal: len(man.Files), Step: "labels"}); err != nil {
		return err
	}
	if err := m.migrateLabels(ctx, s); err != nil {
		return err
	}
	if err := m.progress(ctx, userID, id, Progress{FilesDone: len(man.Files), FilesTotal: len(man.Files), Step: "settings"}); err != nil {
		return err
	}
	if err := m.migrateSettings(ctx, s); err != nil {
		return err
	}
	if man.Chats != nil {
		if err := m.progress(ctx, userID, id, Progress{FilesDone: len(man.Files), FilesTotal: len(man.Files), Step: "chats"}); err != nil {
			return err
		}
		if err := m.migrateChats(ctx, s); err != nil {
			return err
		}
	}
	return m.finish(ctx, userID, id, StatusDone, &s.report, "")
}

// account resolves an AccountRef: an existing account of the user, or one created — unless an
// account with that name exists already.
func (m *Migrator) account(ctx context.Context, s *state, ref AccountRef) (ledger.Account, error) {
	key := ref.key()
	if a, ok := s.accounts[key]; ok {
		return a, nil
	}
	var acc ledger.Account
	created := false
	if ref.ID != nil {
		a, err := m.ledger.GetAccount(ctx, s.userID, *ref.ID)
		if err != nil {
			return acc, err
		}
		acc = a
	} else {
		all, err := m.ledger.ListAccounts(ctx, s.userID)
		if err != nil {
			return acc, err
		}
		name := strings.TrimSpace(ref.New.Name)
		i := slices.IndexFunc(all, func(a ledger.Account) bool { return strings.EqualFold(a.Name, name) })
		if i >= 0 {
			acc = all[i]
		} else {
			if acc, err = m.ledger.CreateAccount(ctx, s.userID, ledger.AccountInput{Name: name, Currency: ref.New.Currency}); err != nil {
				return acc, err
			}
			created = true
		}
	}
	s.accounts[key] = acc
	if !slices.ContainsFunc(s.report.Accounts, func(a AccountReport) bool { return a.ID == acc.ID }) {
		s.report.Accounts = append(s.report.Accounts, AccountReport{ID: acc.ID, Name: acc.Name, Created: created})
	}
	return acc, nil
}

// migrateFiles runs every file through the normal import pipeline, in the legacy order (so the
// first file containing a transaction is its source, as the legacy merged view kept the first
// file's version), and records which transaction each legacy row became.
func (m *Migrator) migrateFiles(ctx context.Context, s *state, runStarted time.Time) error {
	s.report.Files = make([]FileReport, len(s.man.Files))
	for done, fi := range s.order {
		f := s.man.Files[fi]
		if err := m.progress(ctx, s.userID, s.runID, Progress{FilesDone: done, FilesTotal: len(s.man.Files), Step: "files", Current: f.Name}); err != nil {
			return err
		}
		var raw []byte
		err := m.st.WithUser(ctx, s.userID, func(q *db.Queries, _ pgx.Tx) (err error) {
			raw, err = q.LegacyImportFile(ctx, db.LegacyImportFileParams{UserID: s.userID, LegacyImportID: s.runID, Position: int32(fi)}) //nolint:gosec // ≤ MaxFiles
			return err
		})
		if err != nil {
			return err
		}
		cfg := FileCfg{}
		if f.Cfg != nil {
			cfg = *f.Cfg
		}
		s.legacy[fi] = readLegacy(ctx, raw, optsOf(cfg, s.man.Opts))
		rep := &s.report.Files[fi]
		*rep = FileReport{LegacyID: f.LegacyID, Name: f.Name}
		acc, err := m.account(ctx, s, f.Account)
		if err != nil {
			return err
		}
		rep.AccountID, rep.AccountName = &acc.ID, acc.Name
		imp, reused, res, err := m.importFile(ctx, s, f, raw, acc)
		if err != nil {
			if errors.Is(err, errFileFailed) {
				rep.Status, rep.Message = "failed", strings.TrimPrefix(err.Error(), errFileFailed.Error()+": ")
				if imp.ID != uuid.Nil {
					rep.ImportID = &imp.ID
				}
				continue
			}
			return err
		}
		rep.ImportID = &imp.ID
		s.imports[fi] = imp.ID
		if imp.AccountID != nil && *imp.AccountID != acc.ID {
			if a, err := m.ledger.GetAccount(ctx, s.userID, *imp.AccountID); err == nil {
				rep.AccountID, rep.AccountName = &a.ID, a.Name
				rep.Message = "This file was already imported into " + a.Name + "; that import is used."
			}
		}
		rep.Status = "imported"
		if reused && (imp.CommittedAt == nil || imp.CommittedAt.Before(runStarted)) {
			rep.Status = "reused" // committed before this run (not by an earlier attempt of it)
		}
		rep.New, rep.Duplicates, rep.Errors = res.New, res.Duplicates, res.Errors
		s.report.Transactions.New += res.New
		s.report.Transactions.Duplicates += res.Duplicates
		s.report.Transactions.Errors += res.Errors
		if err := m.linkRows(ctx, s, fi, imp, rep); err != nil {
			return err
		}
	}
	return nil
}

// errFileFailed marks a file the run could not import; the run goes on with the others.
var errFileFailed = errors.New("file failed")

func fileFailed(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errFileFailed, fmt.Sprintf(format, args...))
}

// importFile creates (or finds) the import of a legacy file, applies its legacy settings and
// commits it inline. reused: the same bytes were committed already, and that import is used.
func (m *Migrator) importFile(ctx context.Context, s *state, f ManifestFile, raw []byte, acc ledger.Account) (_ ledger.Import, reused bool, _ ledger.CommitResult, _ error) {
	var imp ledger.Import
	p, err := m.ledger.CreatePreview(ctx, s.userID, ledger.Upload{Filename: f.Name, Content: raw, AccountID: &acc.ID})
	var dup ledger.ErrDuplicateFile
	switch {
	case errors.As(err, &dup):
		if imp, err = m.ledger.GetImport(ctx, s.userID, dup.ExistingID); err != nil {
			return imp, false, ledger.CommitResult{}, err
		}
		switch imp.Status {
		case ledger.StatusCommitted:
			counts, err := m.ledger.ImportRowCounts(ctx, s.userID, imp.ID) // its transactions exist already
			return imp, true, ledger.CommitResult{Errors: int64(counts.Errors)}, err
		case ledger.StatusCommitting:
			return imp, false, ledger.CommitResult{}, fileFailed("this file is being imported already; run the migration again once it is done")
		}
		// a preview or a failed import of these bytes (e.g. an earlier attempt): set it up again
		if p, err = m.ledger.UpdatePreview(ctx, s.userID, imp.ID, ledger.Changes{AccountID: &acc.ID}); err != nil {
			return imp, false, ledger.CommitResult{}, m.previewErr(err)
		}
	case err != nil:
		return imp, false, ledger.CommitResult{}, m.previewErr(err)
	}
	imp = p.Import
	opts := importOptions(f.Cfg, s.man.Opts)
	if !sameOptions(opts, p.Options) {
		if p, err = m.ledger.UpdatePreview(ctx, s.userID, imp.ID, ledger.Changes{Options: &opts}); err != nil {
			return imp, false, ledger.CommitResult{}, m.previewErr(err)
		}
	}
	if f.Cfg != nil && len(f.Cfg.Map) > 0 {
		o := p.Options
		mp := applyRoles(p.Detected, &o, f.Cfg, p.Import.Headers)
		if p, err = m.ledger.UpdatePreview(ctx, s.userID, imp.ID, ledger.Changes{Mapping: &mp, Options: &o}); err != nil {
			return imp, false, ledger.CommitResult{}, m.previewErr(err)
		}
	}
	if len(p.Missing) > 0 {
		return imp, false, ledger.CommitResult{}, fileFailed("no column is mapped to %s; open the import from the sidebar, set the column meanings and commit it",
			strings.Join(p.Missing, " and "))
	}
	if _, err := m.ledger.StartCommit(ctx, s.userID, imp.ID, func(context.Context, pgx.Tx) error { return nil }); err != nil {
		return imp, false, ledger.CommitResult{}, err
	}
	res, err := m.ledger.CommitImport(ctx, s.userID, imp.ID)
	if err != nil {
		return imp, false, res, err
	}
	imp, err = m.ledger.GetImport(ctx, s.userID, imp.ID)
	return imp, false, res, err
}

// previewErr turns input problems (an unreadable file, a bad setting) into a failed file.
func (m *Migrator) previewErr(err error) error {
	var ve *platform.ValidationError
	switch {
	case errors.As(err, &ve):
		return fileFailed("%s", ve.Error())
	case errors.Is(err, platform.ErrUnsupportedFile), errors.Is(err, ledger.ErrInvalid):
		return fileFailed("%s", err.Error())
	}
	return err
}

func sameOptions(a, b ledger.OptionsDoc) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

// linkRows records the transaction of each legacy row of a file. The legacy table and the import
// are aligned row by row when both read the same header row and the same number of rows — always
// the case, except for a file whose header the legacy app found elsewhere (PLAN Phase 9 header
// fix); its labels cannot be matched and the report says so.
func (m *Migrator) linkRows(ctx context.Context, s *state, fi int, imp ledger.Import, rep *FileReport) error {
	lf := s.legacy[fi]
	var rows []ledger.Row
	for off := 0; ; off += 1000 {
		page, err := m.ledger.ImportRowsPage(ctx, s.userID, imp.ID, false, off, 1000)
		if err != nil {
			return err
		}
		rows = append(rows, page...)
		if len(page) < 1000 {
			break
		}
	}
	rep.Rows = len(rows)
	if lf.err != nil || len(lf.rows) != len(rows) || lf.headerAt != headerAtOf(imp) {
		if lf.err == nil && hasLegacyLabels(s) {
			msg := "The legacy app read this file with another header row, so its labels can't be matched."
			if rep.Message != "" {
				msg = rep.Message + " " + msg
			}
			rep.Message = msg
		}
		return nil
	}
	ts := make([]*target, len(lf.rows))
	for i := range rows {
		r := &rows[i]
		if r.TransactionID == nil || r.RowIndex < 0 || r.RowIndex >= len(ts) || imp.AccountID == nil {
			continue
		}
		ts[r.RowIndex] = &target{accountID: *imp.AccountID, txID: *r.TransactionID, importID: imp.ID, fingerprint: r.Fingerprint}
	}
	s.targets[fi] = ts
	return nil
}

func hasLegacyLabels(s *state) bool {
	return len(s.man.Private.Tags) > 0 || len(s.man.Private.DupKeep) > 0 || len(s.man.Private.DupPick) > 0
}

func headerAtOf(imp ledger.Import) int {
	var st struct {
		HeaderAt int `json:"headerAt"`
	}
	_ = json.Unmarshal(imp.Stats, &st)
	return st.HeaderAt
}

// migrateLabels recreates the groups and puts every labelled legacy row's transaction in its
// group; then the duplicate decisions.
func (m *Migrator) migrateLabels(ctx context.Context, s *state) error {
	pr := &s.man.Private
	if len(pr.Groups) == 0 && len(pr.Tags) == 0 && len(pr.DupKeep) == 0 && len(pr.DupPick) == 0 {
		return nil
	}
	// the keys of each file view and of the merged view, as the legacy computed them
	byKey := map[string][]RowRef{}
	var files []legacyFile
	var names, ids []string
	var index []int // position in files → manifest index
	for _, fi := range s.order {
		lf := s.legacy[fi]
		if lf.err == nil {
			for row, k := range fileKeys(lf.headers, lf.rows) {
				byKey[k] = append(byKey[k], RowRef{File: fi, Row: row})
			}
		}
		files = append(files, lf)
		names = append(names, s.man.Files[fi].Name)
		ids = append(ids, s.man.Files[fi].LegacyID)
		index = append(index, fi)
	}
	keep := map[string]bool{}
	for k, v := range pr.DupKeep {
		keep[k] = truthy(v)
	}
	mg := mergeFiles(files, names, ids, pr.DupPick, keep)
	for i, k := range mg.txKeys {
		r := mg.kept[i]
		byKey[k] = append(byKey[k], RowRef{File: index[r.File], Row: r.Row})
	}
	dedupe := map[string][]RowRef{}
	for k, refs := range mg.keys {
		for _, r := range refs {
			dedupe[k] = append(dedupe[k], RowRef{File: index[r.File], Row: r.Row})
		}
	}

	groups, err := m.groups(ctx, s)
	if err != nil {
		return err
	}
	names2 := map[string]string{}
	for _, g := range pr.Groups {
		names2[g.ID] = g.Name
	}
	members := map[uuid.UUID][]uuid.UUID{}
	keys := make([]string, 0, len(pr.Tags))
	for k := range pr.Tags {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		gid := pr.Tags[k]
		g, ok := groups[gid]
		if !ok {
			s.report.Labels.miss(gid, k, "its group is not in the backup")
			continue
		}
		refs := byKey[k]
		var txs []uuid.UUID
		for _, r := range refs {
			if t := s.target(r); t != nil && !slices.Contains(txs, t.txID) {
				txs = append(txs, t.txID)
			}
		}
		switch {
		case len(txs) > 0:
			s.report.Labels.Matched++
			members[g] = append(members[g], txs...)
		case len(refs) > 0:
			s.report.Labels.miss(names2[gid], k, "its row was not imported")
		default:
			s.report.Labels.miss(names2[gid], k, "no row of the backup's files has this key")
		}
	}
	for g, txs := range members {
		slices.SortFunc(txs, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
		txs = slices.Compact(txs)
		for len(txs) > 0 {
			n := min(len(txs), ledger.MaxBulkIDs)
			if _, err := m.ledger.Annotate(ctx, s.userID, txs[:n], ledger.AnnotationPatch{SetGroup: true, GroupID: &g}); err != nil {
				return err
			}
			txs = txs[n:]
		}
	}
	return m.overrides(ctx, s, dedupe)
}

func (s *state) target(r RowRef) *target {
	if r.File < 0 || r.File >= len(s.targets) || r.Row < 0 || r.Row >= len(s.targets[r.File]) {
		return nil
	}
	return s.targets[r.File][r.Row]
}

func truthy(v json.RawMessage) bool {
	switch strings.TrimSpace(string(v)) {
	case "", "null", "false", "0", `""`:
		return false
	}
	return true
}

// groups creates the legacy groups (or finds them by name) and maps legacy group ids to new ids.
func (m *Migrator) groups(ctx context.Context, s *state) (map[string]uuid.UUID, error) {
	existing, err := m.ledger.ListGroups(ctx, s.userID)
	if err != nil {
		return nil, err
	}
	out := map[string]uuid.UUID{}
	for _, g := range s.man.Private.Groups {
		name := strings.Join(strings.Fields(g.Name), " ")
		i := slices.IndexFunc(existing, func(e ledger.Group) bool { return strings.EqualFold(e.Name, name) })
		if i >= 0 {
			out[g.ID] = existing[i].ID
			s.report.Groups.Reused++
			continue
		}
		color := g.Color % ledger.GroupColors
		if color < 0 {
			color = -1
		}
		ng, err := m.ledger.CreateGroup(ctx, s.userID, name, color)
		if err != nil {
			if errors.Is(err, ledger.ErrInvalid) {
				continue // an empty or over-long name: its labels are reported as unmatched
			}
			return nil, err
		}
		existing = append(existing, ng)
		out[g.ID] = ng.ID
		s.report.Groups.Created++
	}
	return out, nil
}

// overrides moves the duplicate decisions: dupKeep ("keep both") and dupPick ("use this
// version"), keyed by the legacy de-dupe key.
func (m *Migrator) overrides(ctx context.Context, s *state, dedupe map[string][]RowRef) error {
	pr := &s.man.Private
	type fpKey struct {
		account uuid.UUID
		fp      string
	}
	keys := func(mp map[string]bool) []string {
		out := make([]string, 0, len(mp))
		for k := range mp {
			out = append(out, k)
		}
		slices.Sort(out)
		return out
	}
	keepKeys := map[string]bool{}
	for k, v := range pr.DupKeep {
		if truthy(v) {
			keepKeys[k] = true
		}
	}
	for _, k := range keys(keepKeys) {
		done := map[fpKey]bool{}
		for _, r := range dedupe[k] {
			if t := s.target(r); t != nil && !done[fpKey{t.accountID, string(t.fingerprint)}] {
				done[fpKey{t.accountID, string(t.fingerprint)}] = true
				if err := m.ledger.SetOverride(ctx, s.userID, t.accountID, t.fingerprint, ledger.OverrideKeepBoth, nil); err != nil {
					return err
				}
			}
		}
		if len(done) > 0 {
			s.report.Overrides.Matched++
		} else {
			s.report.Overrides.miss("keep both", k, "no imported row has this key")
		}
	}
	pickKeys := map[string]bool{}
	for k := range pr.DupPick {
		pickKeys[k] = true
	}
	for _, k := range keys(pickKeys) {
		fi := slices.IndexFunc(s.man.Files, func(f ManifestFile) bool { return f.LegacyID == pr.DupPick[k] })
		done := map[fpKey]bool{}
		for _, r := range dedupe[k] {
			t := s.target(r)
			if r.File != fi || t == nil || done[fpKey{t.accountID, string(t.fingerprint)}] {
				continue
			}
			done[fpKey{t.accountID, string(t.fingerprint)}] = true
			imp := t.importID
			if err := m.ledger.SetOverride(ctx, s.userID, t.accountID, t.fingerprint, ledger.OverridePrefer, &imp); err != nil {
				return err
			}
		}
		if len(done) > 0 {
			s.report.Overrides.Matched++
		} else {
			s.report.Overrides.miss("use this version", k, "the chosen file has no imported row with this key")
		}
	}
	return nil
}

// migrateSettings moves the budgets, the opening balance and the investments.
func (m *Migrator) migrateSettings(ctx context.Context, s *state) error {
	pr := &s.man.Private
	if len(pr.Budgets) > 0 {
		names := make([]string, 0, len(pr.Budgets))
		for n := range pr.Budgets {
			names = append(names, n)
		}
		slices.Sort(names)
		for _, n := range names {
			v, ok := pr.Budgets[n].minor()
			if !ok || v <= 0 {
				s.report.Budgets.Skipped = append(s.report.Budgets.Skipped, n)
				continue
			}
			ids, err := m.ledger.EnsureCategories(ctx, s.userID, []string{n}) // budgets are kept by category name
			if err != nil {
				return err
			}
			var id uuid.UUID
			for _, x := range ids {
				id = x
			}
			if len(ids) != 1 {
				s.report.Budgets.Skipped = append(s.report.Budgets.Skipped, n)
				continue
			}
			if err := m.ledger.SetBudget(ctx, s.userID, id, v); err != nil {
				return err
			}
			s.report.Budgets.Set++
		}
	}
	if v, ok := pr.Opening.minor(); ok && v != 0 && s.man.Opening != nil {
		acc, err := m.account(ctx, s, *s.man.Opening)
		if err != nil {
			return err
		}
		in := ledger.AccountInput{Name: acc.Name, Currency: acc.Currency, NumberHint: acc.NumberHint, OpeningBalanceMinor: v, Archived: acc.ArchivedAt != nil}
		if _, err := m.ledger.UpdateAccount(ctx, s.userID, acc.ID, in); err != nil {
			return err
		}
		s.report.Opening = &OpeningReport{AccountID: acc.ID, AccountName: acc.Name, AmountMinor: v}
	}
	if pr.Inv != nil {
		doc, ok := investmentsOf(pr.Inv)
		if !ok {
			return nil
		}
		cur, err := m.ledger.Investments(ctx, s.userID)
		if err != nil {
			return err
		}
		if cur.Version > 0 {
			s.report.Investments = "kept"
			return nil
		}
		if _, err := m.ledger.SaveInvestments(ctx, s.userID, doc); err != nil {
			var ve *platform.ValidationError
			if errors.As(err, &ve) {
				s.report.Investments = "none"
				return nil
			}
			return err
		}
		s.report.Investments = "imported"
	}
	return nil
}

// investmentsOf translates S.inv; ok is false for an empty document (nothing entered).
func investmentsOf(inv *Inv) (investments.Doc, bool) {
	amount := func(n Num) int64 {
		v, _ := n.minor()
		return max(v, 0)
	}
	bp := func(n Num, def int) int {
		if n == "" {
			return def
		}
		v, ok := n.minor()
		if !ok {
			return def
		}
		return int(v)
	}
	d := investments.Doc{
		PPK:          investments.PPK{EmployeeMinor: amount(inv.PPK.Employee), EmployerMinor: amount(inv.PPK.Employer), StateMinor: amount(inv.PPK.State), ValueMinor: amount(inv.PPK.Value)},
		SwrBp:        bp(inv.Swr, investments.DefaultSwrBp),
		RealReturnBp: bp(inv.Ret, investments.DefaultRealReturnBp),
	}
	d.SwrBp = min(max(d.SwrBp, 50), 1000)
	d.RealReturnBp = min(max(d.RealReturnBp, -1000), 2000)
	for _, p := range inv.Positions {
		if len(d.Positions) == investments.MaxPositions {
			break
		}
		name := strings.TrimSpace(p.Name)
		if name == "" {
			name = "Position"
		}
		typ := strings.TrimSpace(p.Type)
		if r := []rune(typ); len(r) > 30 {
			typ = string(r[:30])
		}
		if r := []rune(name); len(r) > 80 {
			name = string(r[:80])
		}
		d.Positions = append(d.Positions, investments.Position{ID: platform.NewID(), Name: name, Type: typ,
			InvestedMinor: amount(p.Invested), ValueMinor: amount(p.Value), AnnualIncomeMinor: amount(p.Income)})
	}
	empty := d.PPK == (investments.PPK{}) && len(d.Positions) == 0
	return d, !empty
}

// migrateChats copies the legacy chats, newest first, as far as the chat limit leaves room.
// A chat whose title matches an existing chat's is taken as migrated already.
func (m *Migrator) migrateChats(ctx context.Context, s *state) error {
	if m.chats == nil {
		s.report.Chats.Skipped = len(s.man.Chats)
		return nil
	}
	existing, err := m.chats.List(ctx, s.userID)
	if err != nil {
		return err
	}
	titles := map[string]bool{}
	for i := range existing {
		titles[existing[i].Title] = true
	}
	room := m.chats.Limit() - len(existing)
	chats := slices.Clone(s.man.Chats)
	slices.SortStableFunc(chats, func(a, b Chat) int { return cmpInt(b.Updated, a.Updated) })
	for _, c := range chats {
		msgs, title := chatMessages(c)
		if len(msgs) == 0 || titles[title] || room <= 0 {
			s.report.Chats.Skipped++
			continue
		}
		chat, err := m.chats.Create(ctx, s.userID)
		if err != nil {
			return err
		}
		if _, err := m.chats.Append(ctx, s.userID, chat.ID, msgs, assistant.ChatUpdate{Title: title}); err != nil {
			return err
		}
		titles[title] = true
		room--
		s.report.Chats.Imported++
	}
	return nil
}

// chatMessages turns legacy turns into stored messages: each question and its answer, with the
// answer's lookups as steps. The model's intermediate tool messages are not kept (the legacy
// history cut them anyway).
func chatMessages(c Chat) ([]assistant.Message, string) {
	var out []assistant.Message
	title := strings.TrimSpace(c.Title)
	for _, t := range c.Turns {
		switch t.Role {
		case "user":
			if strings.TrimSpace(t.Text) == "" {
				continue
			}
			if title == "" {
				title = t.Text
			}
			out = append(out, assistant.Message{Role: llm.RoleUser, Content: t.Text})
		case "ai":
			if len(out) == 0 || out[len(out)-1].Role != llm.RoleUser {
				continue
			}
			msg := assistant.Message{Role: llm.RoleAssistant, Content: t.Content, Reasoning: t.Reasoning,
				Meta: &assistant.Meta{Model: t.Meta.Model, Mode: "tools", Scope: "all", Lookups: len(t.Steps)}}
			for _, st := range t.Steps {
				args := st.Args
				if len(args) == 0 || !json.Valid(args) {
					args = json.RawMessage(`{}`)
				}
				msg.Steps = append(msg.Steps, assistant.Step{Tool: st.Name, Args: args, Summary: st.Summary, Result: st.JSON})
			}
			if t.Error != "" {
				msg.Error = &assistant.TurnError{Code: "legacy", Message: t.Error}
			}
			out = append(out, msg)
		}
	}
	if len(out) > 0 && out[len(out)-1].Role == llm.RoleUser {
		out = out[:len(out)-1] // a question that never got an answer
	}
	return out, title
}

// readableKey shows a legacy key with its date as yyyy-mm-dd instead of milliseconds.
func readableKey(k string) string {
	head, rest, ok := strings.Cut(k, "|")
	if !ok || strings.HasPrefix(k, "r|") {
		return k
	}
	var ms int64
	if _, err := fmt.Sscan(head, &ms); err != nil || head == "" {
		return k
	}
	return time.UnixMilli(ms).UTC().Format("2006-01-02") + " | " + strings.ReplaceAll(rest, "|", " | ")
}
