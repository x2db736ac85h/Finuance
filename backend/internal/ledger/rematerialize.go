package ledger

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// Duplicate review (PLAN §4.4, legacy §32.3). A transaction's identity in an account is
// (fingerprint, occurrence, split_scope); its field values come from one staged row, its *source*.
//
//   - No override: one transaction per occurrence that any committed import contains (the max rule),
//     with the earliest-committed import containing that occurrence as the source.
//   - "prefer" import P: the same transactions, but P is the source for every occurrence P contains.
//   - "keep_both": nothing is merged. Each import's rows get the split_scope of that import (from its
//     file's SHA-256), so they become separate transactions (the sum rule).
//
// Rematerialize computes that desired set for one fingerprint from scratch and makes the table match
// it. Commit, delete, reprocess and override changes keep the table right incrementally where they
// can and call Rematerialize for the fingerprints they can't handle set-wise; the property test
// checks that the two always agree.

// OverrideMode is a duplicate-review decision.
type OverrideMode string

// Override modes (dedupe_overrides.mode).
const (
	OverridePrefer   OverrideMode = "prefer"
	OverrideKeepBoth OverrideMode = "keep_both"
)

// SplitScope is the keep-both scope of an import: a UUID built from the first 16 bytes of the
// import file's SHA-256, so re-importing the identical file gets the same scope (and annotations).
func SplitScope(fileSHA256 []byte) uuid.UUID {
	var u uuid.UUID
	copy(u[:], fileSHA256)
	return u
}

// slot is one desired transaction: its identity within the fingerprint and its source row.
type slot struct {
	occurrence int32
	scope      uuid.UUID
}

type sourceRow struct {
	importID uuid.UUID
	rowIndex int32
}

// Rematerialize rebuilds the transactions of one (account, fingerprint) in its own transaction.
func (l *Ledger) Rematerialize(ctx context.Context, userID, accountID uuid.UUID, fingerprint []byte) error {
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		if err := lockAccount(ctx, q, userID, accountID); err != nil {
			return err
		}
		_, err := rematerialize(ctx, q, userID, accountID, fingerprint, l.clock.Now())
		return err
	})
	return dbErr("rematerialize", err)
}

// rematerialize is the reference implementation, run inside the caller's transaction:
//  1. load the override and every non-errored row of the fingerprint in committed imports;
//  2. compute the desired set {(occurrence, scope) → source row};
//  3. delete transactions no longer desired (annotations stay — they are keyed by identity);
//  4. insert the missing ones and re-point the kept ones at their source;
//  5. relink the staged rows and re-apply annotations to the inserted transactions.
//
// It returns the number of transactions it inserted.
func rematerialize(ctx context.Context, q *db.Queries, userID, accountID uuid.UUID, fp []byte, now time.Time) (int, error) {
	var mode OverrideMode
	var preferred uuid.UUID
	ov, err := q.GetDedupeOverride(ctx, db.GetDedupeOverrideParams{UserID: userID, AccountID: accountID, Fingerprint: fp})
	switch {
	case err == nil:
		mode = OverrideMode(ov.Mode)
		if ov.PreferredImportID != nil {
			preferred = *ov.PreferredImportID
		}
	case !errors.Is(err, pgx.ErrNoRows):
		return 0, err
	}
	rows, err := q.FingerprintRows(ctx, db.FingerprintRowsParams{UserID: userID, AccountID: &accountID, Fingerprint: fp})
	if err != nil {
		return 0, err
	}

	// 2. Desired set. rows are ordered earliest-committed import first, so the first row seen for a
	// slot is its default source.
	desired := map[slot]sourceRow{}
	var order []slot // deterministic insert order (file order of the source)
	rowSlot := make([]slot, len(rows))
	for i, r := range rows {
		if r.Occurrence == nil {
			continue
		}
		s := slot{occurrence: *r.Occurrence}
		if mode == OverrideKeepBoth {
			s.scope = SplitScope(r.Sha256)
		}
		rowSlot[i] = s
		src := sourceRow{importID: r.ImportID, rowIndex: r.RowIndex}
		cur, seen := desired[s]
		switch {
		case !seen:
			desired[s] = src
			order = append(order, s)
		case mode == OverridePrefer && r.ImportID == preferred && cur.importID != preferred:
			desired[s] = src
		}
	}

	// 3./4. Compare with the current transactions.
	current, err := q.FingerprintTransactions(ctx, db.FingerprintTransactionsParams{UserID: userID, AccountID: accountID, Fingerprint: fp})
	if err != nil {
		return 0, err
	}
	have := map[slot]db.FingerprintTransactionsRow{}
	var drop []uuid.UUID
	for _, t := range current {
		s := slot{occurrence: t.Occurrence, scope: t.SplitScope}
		if _, want := desired[s]; want {
			have[s] = t
		} else {
			drop = append(drop, t.ID)
		}
	}
	if len(drop) > 0 {
		if _, err := q.DeleteTransactionsByID(ctx, db.DeleteTransactionsByIDParams{UserID: userID, Ids: drop}); err != nil {
			return 0, err
		}
	}
	txOf := map[slot]uuid.UUID{}
	var inserted []uuid.UUID
	for _, s := range order {
		src := desired[s]
		if t, ok := have[s]; ok {
			txOf[s] = t.ID
			if err := q.UpdateTransactionFromRow(ctx, db.UpdateTransactionFromRowParams{UserID: userID, ID: t.ID,
				ImportID: src.importID, RowIndex: src.rowIndex, Now: now}); err != nil {
				return 0, err
			}
			continue
		}
		id := platform.NewID()
		if err := q.InsertTransactionFromRow(ctx, db.InsertTransactionFromRowParams{UserID: userID, AccountID: accountID, ID: id,
			SplitScope: s.scope, ImportID: src.importID, RowIndex: src.rowIndex}); err != nil {
			return 0, fmt.Errorf("insert %x/%d: %w", fp[:4], s.occurrence, err)
		}
		txOf[s] = id
		inserted = append(inserted, id)
	}

	// 5. Relink every staged row of the fingerprint (setting its scope) and re-apply annotations.
	for i, r := range rows {
		if r.Occurrence == nil {
			continue
		}
		s := rowSlot[i]
		id := txOf[s]
		if err := q.LinkImportRow(ctx, db.LinkImportRowParams{UserID: userID, ImportID: r.ImportID, RowIndex: r.RowIndex,
			SplitScope: s.scope, TransactionID: &id}); err != nil {
			return 0, err
		}
	}
	if len(inserted) > 0 {
		if _, err := q.ApplyAnnotations(ctx, db.ApplyAnnotationsParams{UserID: userID, Ids: inserted}); err != nil {
			return 0, err
		}
	}
	return len(inserted), nil
}

// rematerializeAll runs rematerialize for each distinct fingerprint and returns the number of
// transactions inserted.
func rematerializeAll(ctx context.Context, q *db.Queries, userID, accountID uuid.UUID, fps [][]byte, now time.Time) (int, error) {
	seen := map[string]bool{}
	total := 0
	for _, fp := range fps {
		if seen[string(fp)] {
			continue
		}
		seen[string(fp)] = true
		n, err := rematerialize(ctx, q, userID, accountID, fp, now)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

// SetOverride records a duplicate-review decision for (account, fingerprint) and re-materialises
// it in the same transaction. mode "prefer" needs importID, an import of that account; "keep_both"
// takes none.
func (l *Ledger) SetOverride(ctx context.Context, userID, accountID uuid.UUID, fingerprint []byte, mode OverrideMode, importID *uuid.UUID) error {
	switch {
	case len(fingerprint) != 32:
		return invalidf("fingerprint must be 32 bytes")
	case mode == OverridePrefer && importID == nil:
		return invalidf("prefer needs an import")
	case mode == OverrideKeepBoth && importID != nil:
		return invalidf("keep_both takes no import")
	case mode != OverridePrefer && mode != OverrideKeepBoth:
		return invalidf("unknown override mode %q", mode)
	}
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		if err := lockAccount(ctx, q, userID, accountID); err != nil {
			return err
		}
		if importID != nil {
			imp, err := q.GetImport(ctx, db.GetImportParams{UserID: userID, ID: *importID})
			if err != nil {
				return fmt.Errorf("import %s: %w", importID, err)
			}
			if imp.AccountID == nil || *imp.AccountID != accountID {
				return invalidf("import %s is not an import of this account", importID)
			}
		}
		now := l.clock.Now()
		if _, err := q.SetDedupeOverride(ctx, db.SetDedupeOverrideParams{AccountID: accountID, Fingerprint: fingerprint, UserID: userID,
			Mode: string(mode), PreferredImportID: importID, Now: now}); err != nil {
			return err
		}
		_, err := rematerialize(ctx, q, userID, accountID, fingerprint, now)
		return err
	})
	return dbErr("set override", err)
}

// ClearOverride goes back to the default merge ("Merge again") and re-materialises.
func (l *Ledger) ClearOverride(ctx context.Context, userID, accountID uuid.UUID, fingerprint []byte) error {
	var n int64
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) (err error) {
		if err := lockAccount(ctx, q, userID, accountID); err != nil {
			return err
		}
		if n, err = q.ClearDedupeOverride(ctx, db.ClearDedupeOverrideParams{UserID: userID, AccountID: accountID, Fingerprint: fingerprint}); err != nil || n == 0 {
			return err
		}
		_, err = rematerialize(ctx, q, userID, accountID, fingerprint, l.clock.Now())
		return err
	})
	return notFound("clear override", n, err)
}

// DuplicatesSkipped is "N duplicates skipped" for a set of accounts: staged rows linked to a
// transaction minus the import-origin transactions (PLAN §4.4).
func (l *Ledger) DuplicatesSkipped(ctx context.Context, userID uuid.UUID, accountIDs []uuid.UUID) (int64, error) {
	var n int64
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) (err error) {
		n, err = q.DuplicatesSkipped(ctx, db.DuplicatesSkippedParams{UserID: userID, AccountIds: accountIDs})
		return err
	})
	return n, dbErr("duplicates skipped", err)
}
