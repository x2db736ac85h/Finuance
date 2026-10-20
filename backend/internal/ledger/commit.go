package ledger

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// CommitResult counts what a commit did (PLAN §4.4 step 3).
type CommitResult struct {
	New        int   // transactions inserted
	Duplicates int64 // staged rows linked to a transaction that already existed
	Errors     int64 // staged rows with a parse error (never committed)
}

// CommitImport turns the staged rows of a previewed (or committing) import into transactions, in
// one database transaction — the set-based core of the commit algorithm, PLAN §4.4:
//
//  1. insert new transactions; rows whose (fingerprint, occurrence) the account already has are
//     duplicates and are skipped by the unique constraint. Then (step 1b) re-apply annotations to
//     the inserted rows, so labels survive delete + re-import;
//  2. link every staged row to its transaction (provenance);
//  3. record the stats and mark the import committed.
//
// The import must have an account. Phase 6 wraps this in the CommitImport job and adds the
// category upsert before step 1, the dedupe-override path (step 2b) and the profile upsert.
func (l *Ledger) CommitImport(ctx context.Context, userID, importID uuid.UUID) (CommitResult, error) {
	var res CommitResult
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		imp, err := q.GetImport(ctx, db.GetImportParams{UserID: userID, ID: importID})
		if err != nil {
			return err
		}
		if s := ImportStatus(imp.Status); s != StatusPreviewed && s != StatusCommitting {
			return fmt.Errorf("import is %s: %w", s, ErrState)
		}
		if imp.AccountID == nil {
			return fmt.Errorf("import has no account: %w", ErrState)
		}
		account := *imp.AccountID

		// 1. One UUIDv7 per eligible row; ids of rows that turn out to be duplicates stay unused.
		n, err := q.CountEligibleImportRows(ctx, db.CountEligibleImportRowsParams{UserID: userID, ImportID: importID, AccountID: account})
		if err != nil {
			return err
		}
		ids := make([]uuid.UUID, n)
		for i := range ids {
			ids[i] = platform.NewID()
		}
		inserted, err := q.InsertTransactionsFromImport(ctx, db.InsertTransactionsFromImportParams{
			UserID: userID, ImportID: importID, AccountID: account, Ids: ids})
		if err != nil {
			return err
		}
		// 1b.
		if len(inserted) > 0 {
			if _, err := q.ApplyAnnotations(ctx, db.ApplyAnnotationsParams{UserID: userID, Ids: inserted}); err != nil {
				return err
			}
		}
		// 2.
		linked, err := q.LinkImportRows(ctx, db.LinkImportRowsParams{UserID: userID, ImportID: importID, AccountID: account})
		if err != nil {
			return err
		}
		// 3.
		counts, err := q.CountImportRows(ctx, db.CountImportRowsParams{UserID: userID, ImportID: importID})
		if err != nil {
			return err
		}
		res = CommitResult{New: len(inserted), Duplicates: linked - int64(len(inserted)), Errors: counts.Errors}
		stats, err := mergeStats(imp.Stats, map[string]any{"rows": counts.Rows, "errors": res.Errors, "new": res.New, "duplicates": res.Duplicates})
		if err != nil {
			return err
		}
		_, err = q.SetImportStatus(ctx, db.SetImportStatusParams{UserID: userID, ID: importID, FromStatus: imp.Status,
			ToStatus: string(StatusCommitted), Stats: stats, Now: l.clock.Now()})
		return err
	})
	return res, dbErr("commit import", err)
}

// mergeStats sets keys in the import's stats object, keeping the preview's (headerAt, dropped, …).
func mergeStats(old []byte, set map[string]any) ([]byte, error) {
	m := map[string]any{}
	if len(old) > 0 {
		if err := json.Unmarshal(old, &m); err != nil {
			return nil, fmt.Errorf("import stats: %w", err)
		}
	}
	for k, v := range set {
		m[k] = v
	}
	return json.Marshal(m)
}
