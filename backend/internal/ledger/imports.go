package ledger

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/ingest"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// ImportStatus is the lifecycle of an import: previewed → committing → committed, or failed.
type ImportStatus string

// Import statuses (imports.status).
const (
	StatusPreviewed  ImportStatus = "previewed"
	StatusCommitting ImportStatus = "committing"
	StatusCommitted  ImportStatus = "committed"
	StatusFailed     ImportStatus = "failed"
)

// Import is one uploaded file and how it was read.
type Import struct {
	ID              uuid.UUID
	AccountID       *uuid.UUID
	Filename        string
	SizeBytes       int
	SHA256          [32]byte
	Status          ImportStatus
	Encoding        string
	Delimiter       string
	Headers         []string
	ColumnTypes     []string
	Mapping         json.RawMessage // PLAN §4.3
	Options         json.RawMessage // PLAN §4.3
	MappingSource   string          // detected | profile | user
	HeaderSignature [32]byte
	Stats           json.RawMessage
	Error           string
	CreatedAt       time.Time
	CommittedAt     *time.Time
}

func importOf(i db.Import) (Import, error) {
	out := Import{ID: i.ID, AccountID: i.AccountID, Filename: i.Filename, SizeBytes: int(i.SizeBytes),
		Status: ImportStatus(i.Status), Encoding: i.Encoding, Delimiter: i.Delimiter, Mapping: i.Mapping,
		Options: i.Options, MappingSource: i.MappingSource, Stats: i.Stats, CreatedAt: i.CreatedAt, CommittedAt: i.CommittedAt}
	copy(out.SHA256[:], i.Sha256)
	copy(out.HeaderSignature[:], i.HeaderSignature)
	if i.Error != nil {
		out.Error = *i.Error
	}
	if err := json.Unmarshal(i.Headers, &out.Headers); err != nil {
		return out, fmt.Errorf("import %s headers: %w", i.ID, err)
	}
	if err := json.Unmarshal(i.ColumnTypes, &out.ColumnTypes); err != nil {
		return out, fmt.Errorf("import %s column types: %w", i.ID, err)
	}
	return out, nil
}

// NewImport is a parsed and staged file, ready to be stored as a preview.
type NewImport struct {
	AccountID       *uuid.UUID
	Filename        string
	Content         []byte // the raw file, kept for lossless re-processing
	Encoding        string
	Delimiter       string
	Headers         []string
	ColumnTypes     []string
	Mapping         json.RawMessage
	Options         json.RawMessage
	MappingSource   string
	HeaderSignature [32]byte
	Stats           json.RawMessage // {rows, errors, headerAt, dropped, …}; nil means {}
	Rows            []ingest.StagedRow
}

// ErrDuplicateFile means the user already has a non-failed import of the identical file. The error
// carries the existing import's id.
type ErrDuplicateFile struct{ ExistingID uuid.UUID }

func (e ErrDuplicateFile) Error() string { return fmt.Sprintf("duplicate of import %s", e.ExistingID) }

// Unwrap maps the error to 409 duplicate_file.
func (e ErrDuplicateFile) Unwrap() error { return platform.ErrDuplicateFile }

// CreateImport stores the import, its file and its staged rows in one transaction (PLAN task 6.2
// step 9). An identical file already imported is ErrDuplicateFile.
func (l *Ledger) CreateImport(ctx context.Context, userID uuid.UUID, in NewImport) (Import, error) {
	sum := sha256.Sum256(in.Content)
	headers, err := json.Marshal(nonNil(in.Headers))
	if err != nil {
		return Import{}, err
	}
	types, err := json.Marshal(nonNil(in.ColumnTypes))
	if err != nil {
		return Import{}, err
	}
	if in.Stats == nil {
		in.Stats = json.RawMessage(`{}`)
	}
	rows := ImportRows(in.Rows)
	var out Import
	err = l.st.WithUser(ctx, userID, func(q *db.Queries, tx pgx.Tx) error {
		if existing, err := q.FindImportBySHA(ctx, db.FindImportBySHAParams{UserID: userID, Sha256: sum[:]}); err == nil {
			return ErrDuplicateFile{ExistingID: existing}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if in.AccountID != nil {
			if err := ownAccount(ctx, q, userID, *in.AccountID); err != nil {
				return err
			}
		}
		imp, err := q.CreateImport(ctx, db.CreateImportParams{ID: platform.NewID(), UserID: userID, AccountID: in.AccountID,
			Filename: in.Filename, SizeBytes: int32(len(in.Content)), Sha256: sum[:], Encoding: in.Encoding, //nolint:gosec // uploads are capped far below 2 GB
			Delimiter: in.Delimiter, Headers: headers, ColumnTypes: types, Mapping: in.Mapping, Options: in.Options,
			MappingSource: in.MappingSource, HeaderSignature: in.HeaderSignature[:], Stats: in.Stats, Now: l.clock.Now()})
		if err != nil {
			return err
		}
		if err := q.InsertImportFile(ctx, db.InsertImportFileParams{ImportID: imp.ID, UserID: userID, Content: in.Content}); err != nil {
			return err
		}
		if _, err := store.InsertImportRows(ctx, tx, imp.ID, userID, rows); err != nil {
			return err
		}
		out, err = importOf(imp)
		return err
	})
	var dup ErrDuplicateFile
	if errors.As(err, &dup) {
		return Import{}, err
	}
	return out, dbErr("create import", err)
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// GetImport returns one import.
func (l *Ledger) GetImport(ctx context.Context, userID, id uuid.UUID) (Import, error) {
	var out Import
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		i, err := q.GetImport(ctx, db.GetImportParams{UserID: userID, ID: id})
		if err != nil {
			return err
		}
		out, err = importOf(i)
		return err
	})
	return out, dbErr("get import", err)
}

// ListImports returns the user's most recent imports, newest first.
func (l *Ledger) ListImports(ctx context.Context, userID uuid.UUID, limit int) ([]Import, error) {
	var out []Import
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		rows, err := q.ListImports(ctx, db.ListImportsParams{UserID: userID, MaxRows: int32(min(max(limit, 1), 1000))}) //nolint:gosec // clamped
		if err != nil {
			return err
		}
		for k := range rows {
			i, err := importOf(rows[k])
			if err != nil {
				return err
			}
			out = append(out, i)
		}
		return nil
	})
	return out, dbErr("list imports", err)
}

// ImportRowsPage returns staged rows in file order; onlyErrors keeps the rows with a parse error.
func (l *Ledger) ImportRowsPage(ctx context.Context, userID, importID uuid.UUID, onlyErrors bool, offset, limit int) ([]Row, error) {
	var out []Row
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		if _, err := q.GetImport(ctx, db.GetImportParams{UserID: userID, ID: importID}); err != nil {
			return err
		}
		rows, err := q.ListImportRows(ctx, db.ListImportRowsParams{UserID: userID, ImportID: importID, OnlyErrors: onlyErrors,
			SkipRows: int32(max(offset, 0)), MaxRows: int32(min(max(limit, 1), 1000))}) //nolint:gosec // clamped
		if err != nil {
			return err
		}
		for k := range rows {
			r := &rows[k]
			row := Row{RowIndex: int(r.RowIndex), BookedOn: r.BookedOn, ValueOn: r.ValueOn, AmountMinor: r.AmountMinor,
				Currency: deref(r.Currency), Payee: deref(r.Payee), Description: deref(r.Description), Reference: deref(r.Reference),
				TxType: deref(r.TxType), BankCategory: deref(r.BankCategory), CounterpartyAccount: deref(r.CounterpartyAccount),
				BalanceMinor: r.BalanceAfterMinor, Error: deref(r.ParseError), Duplicate: r.Duplicate, TransactionID: r.TransactionID,
				Fingerprint: r.Fingerprint}
			if r.Occurrence != nil {
				row.Occurrence = int(*r.Occurrence)
			}
			if row.Raw, err = decodeRaw(r.Raw); err != nil {
				return err
			}
			out = append(out, row)
		}
		return nil
	})
	return out, dbErr("import rows", err)
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// DeleteImport deletes an import, its file and its staged rows, then (PLAN §4.4, delete import):
//
//  1. the import-origin transactions no other import of the account contains (orphans);
//  2. re-materialises the fingerprints whose value source was this import, or whose override
//     preferred it (the FK cascade removes such overrides), so their values come from the
//     remaining sources.
//
// Annotations stay, so re-importing the file brings the labels back.
func (l *Ledger) DeleteImport(ctx context.Context, userID, id uuid.UUID) (orphansDeleted int64, err error) {
	err = l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		imp, err := q.GetImport(ctx, db.GetImportParams{UserID: userID, ID: id})
		if err != nil {
			return err
		}
		var fps [][]byte
		if imp.AccountID != nil {
			if err := lockAccount(ctx, q, userID, *imp.AccountID); err != nil {
				return err
			}
			if fps, err = sourcedFingerprints(ctx, q, userID, *imp.AccountID, id); err != nil {
				return err
			}
		}
		if _, err := q.DeleteImportRows(ctx, db.DeleteImportRowsParams{UserID: userID, ImportID: id}); err != nil {
			return err
		}
		if _, err := q.DeleteImport(ctx, db.DeleteImportParams{UserID: userID, ID: id}); err != nil {
			return err
		}
		if imp.AccountID == nil {
			return nil
		}
		if orphansDeleted, err = q.DeleteOrphanTransactions(ctx, db.DeleteOrphanTransactionsParams{UserID: userID, AccountID: *imp.AccountID}); err != nil {
			return err
		}
		// A fingerprint with no rows left has no transactions left either (the orphans just went),
		// so only the ones other imports still contain need rebuilding.
		if len(fps) > 0 {
			if fps, err = q.FingerprintsWithRows(ctx, db.FingerprintsWithRowsParams{UserID: userID, AccountID: imp.AccountID, Fingerprints: fps}); err != nil {
				return err
			}
		}
		_, err = rematerializeAll(ctx, q, userID, *imp.AccountID, fps, l.clock.Now())
		return err
	})
	return orphansDeleted, dbErr("delete import", err)
}

// sourcedFingerprints are the fingerprints that must be re-materialised when import id goes away
// or changes: those whose transactions take their values from it and those whose override prefers it.
func sourcedFingerprints(ctx context.Context, q *db.Queries, userID, accountID, id uuid.UUID) ([][]byte, error) {
	fps, err := q.FingerprintsSourcedBy(ctx, db.FingerprintsSourcedByParams{UserID: userID, AccountID: accountID, ImportID: &id})
	if err != nil {
		return nil, err
	}
	pref, err := q.OverridesPreferring(ctx, db.OverridesPreferringParams{UserID: userID, AccountID: accountID, ImportID: &id})
	return append(fps, pref...), err
}

// Staging is a re-parse of an import's stored bytes with new options or a new mapping.
type Staging struct {
	AccountID       *uuid.UUID // previewed imports only
	Encoding        string
	Delimiter       string
	Headers         []string
	ColumnTypes     []string
	Mapping         json.RawMessage
	Options         json.RawMessage
	MappingSource   string
	HeaderSignature [32]byte
	Stats           json.RawMessage
	Rows            []ingest.StagedRow
}

// Restage replaces the staged rows of an import in one transaction.
//
//   - A previewed import (PATCH) gets the new rows, mapping, options and account.
//   - A committed import (reprocess) keeps its account. Its old rows go, orphaned transactions are
//     deleted, fingerprints it was the source of move to other sources, and the import returns to
//     "committing": the CommitImport job then commits the new rows. Manual edits survive wherever
//     the fingerprint is unchanged (annotations).
//
// enqueue, when not nil, runs in the same transaction after a reprocess restage (the handler inserts
// the CommitImport job with River's InsertTx there, so the job exists exactly when the restage does).
func (l *Ledger) Restage(ctx context.Context, userID, id uuid.UUID, s Staging, enqueue func(context.Context, pgx.Tx) error) (Import, error) {
	headers, err := json.Marshal(nonNil(s.Headers))
	if err != nil {
		return Import{}, err
	}
	types, err := json.Marshal(nonNil(s.ColumnTypes))
	if err != nil {
		return Import{}, err
	}
	if s.Stats == nil {
		s.Stats = json.RawMessage(`{}`)
	}
	rows := ImportRows(s.Rows)
	var out Import
	err = l.st.WithUser(ctx, userID, func(q *db.Queries, tx pgx.Tx) error {
		imp, err := q.GetImport(ctx, db.GetImportParams{UserID: userID, ID: id})
		if err != nil {
			return err
		}
		status := ImportStatus(imp.Status)
		account := s.AccountID
		switch status {
		case StatusPreviewed:
			if account != nil {
				if err := ownAccount(ctx, q, userID, *account); err != nil {
					return err
				}
			}
		case StatusCommitted:
			account = imp.AccountID
		default:
			return fmt.Errorf("import is %s: %w", status, ErrState)
		}
		var fps [][]byte
		if status == StatusCommitted {
			if err := lockAccount(ctx, q, userID, *account); err != nil {
				return err
			}
			if fps, err = sourcedFingerprints(ctx, q, userID, *account, id); err != nil {
				return err
			}
		}
		if _, err := q.DeleteImportRows(ctx, db.DeleteImportRowsParams{UserID: userID, ImportID: id}); err != nil {
			return err
		}
		updated, err := q.UpdateImportPreview(ctx, db.UpdateImportPreviewParams{UserID: userID, ID: id, ExpectedStatus: string(status),
			AccountID: account, Encoding: s.Encoding, Delimiter: s.Delimiter, Headers: headers, ColumnTypes: types,
			Mapping: s.Mapping, Options: s.Options, MappingSource: s.MappingSource, HeaderSignature: s.HeaderSignature[:], Stats: s.Stats})
		if err != nil {
			return err
		}
		if _, err := store.InsertImportRows(ctx, tx, id, userID, rows); err != nil {
			return err
		}
		if status == StatusCommitted {
			if updated, err = q.SetImportStatus(ctx, db.SetImportStatusParams{UserID: userID, ID: id, FromStatus: string(StatusCommitted),
				ToStatus: string(StatusCommitting), Now: l.clock.Now()}); err != nil {
				return err
			}
			if _, err := q.DeleteOrphanTransactions(ctx, db.DeleteOrphanTransactionsParams{UserID: userID, AccountID: *account}); err != nil {
				return err
			}
			if _, err := rematerializeAll(ctx, q, userID, *account, fps, l.clock.Now()); err != nil {
				return err
			}
			if enqueue != nil {
				if err := enqueue(ctx, tx); err != nil {
					return err
				}
			}
		}
		out, err = importOf(updated)
		return err
	})
	return out, dbErr("restage import", err)
}

// rawField is the stored form of ingest.RawField: {"h": header, "v": value} (PLAN §5.2).
type rawField struct {
	H string `json:"h"`
	V string `json:"v"`
}

func encodeRaw(fields []ingest.RawField) []byte {
	out := make([]rawField, len(fields))
	for i, f := range fields {
		out[i] = rawField{H: f.Header, V: f.Value}
	}
	b, _ := json.Marshal(out) // strings only: cannot fail
	return b
}

func decodeRaw(b []byte) ([]ingest.RawField, error) {
	var in []rawField
	if err := json.Unmarshal(b, &in); err != nil {
		return nil, fmt.Errorf("raw fields: %w", err)
	}
	out := make([]ingest.RawField, len(in))
	for i, f := range in {
		out[i] = ingest.RawField{Header: f.H, Value: f.V}
	}
	return out, nil
}

// ImportRows converts staged rows to the stored form. Errored rows keep only their raw cells and
// the error; their partial canonical values are not stored, so nothing can commit them by accident.
func ImportRows(rows []ingest.StagedRow) []store.ImportRow {
	out := make([]store.ImportRow, len(rows))
	for i := range rows {
		r := &rows[i]
		o := &out[i]
		o.RowIndex = int32(r.RowIndex) //nolint:gosec // ingest caps a file at 200k rows
		o.Raw = encodeRaw(r.Raw)
		o.SearchText = r.SearchText
		if r.Error != "" {
			msg := r.Error
			o.ParseError = &msg
			continue
		}
		occ, amount := int32(r.Occurrence), r.AmountMinor //nolint:gosec // occurrence ≤ rows per file
		o.Fingerprint, o.Occurrence, o.AmountMinor = r.Fingerprint[:], &occ, &amount
		o.BookedOn, o.Currency, o.BalanceAfterMinor = r.BookedOn, r.Currency, r.BalanceMinor
		if r.ValueOn != nil {
			o.ValueOn = *r.ValueOn
		}
		o.Payee, o.PayeeN, o.PayeeGroup = r.Payee, r.PayeeN, r.PayeeGroup
		o.Description, o.DescriptionN = r.Description, r.DescriptionN
		o.Reference, o.ReferenceN = r.Reference, r.ReferenceN
		o.TxType, o.TxTypeN = r.TxType, r.TxTypeN
		o.BankCategory, o.BankCategoryN = r.BankCategory, r.BankCategoryN
		o.CounterpartyAccount, o.CounterpartyN = r.CounterpartyAccount, r.CounterpartyN
	}
	return out
}

// RowCounts are the staged rows of an import: all, with a parse error, and linked to a transaction.
type RowCounts struct {
	Rows, Errors, Linked int
}

// ImportRowCounts counts an import's staged rows.
func (l *Ledger) ImportRowCounts(ctx context.Context, userID, importID uuid.UUID) (RowCounts, error) {
	var out RowCounts
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		if _, err := q.GetImport(ctx, db.GetImportParams{UserID: userID, ID: importID}); err != nil {
			return err
		}
		c, err := q.CountImportRows(ctx, db.CountImportRowsParams{UserID: userID, ImportID: importID})
		out = RowCounts{Rows: int(c.Rows), Errors: int(c.Errors), Linked: int(c.Linked)}
		return err
	})
	return out, dbErr("import row counts", err)
}
