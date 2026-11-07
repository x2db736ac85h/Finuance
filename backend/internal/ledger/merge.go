package ledger

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// Merge suggestions (Phase 20, ADR 0032): a card payment typed into a bank account before its
// file was imported meets the bank's own row when the file arrives. Merging keeps the bank's row
// (its values are the bank's) and gives it what the user added to the typed one: a category set by
// hand, the note and the group, and the shared-group copy (the shared expense keeps its id and
// split and then follows the bank's row). The typed row is then deleted. "Keep both" remembers the
// pair, so it isn't suggested again.

// mergeDays is how far apart the typed and the imported row may be booked.
const mergeDays = 3

// MergeSuggestion is a typed row of a bank account and the imported row it probably is.
type MergeSuggestion struct {
	TypedID, ImportedID       uuid.UUID
	TypedOn, ImportedOn       platform.Date
	TypedPayee, ImportedPayee string
	TypedNote                 string
	AmountMinor               int64
	Currency                  string
	AccountID                 uuid.UUID
	AccountName               string
}

// MergeSuggestions lists, for each typed row of a bank account, the nearest imported row of the
// same account and amount within mergeDays days that the user hasn't kept apart.
func (l *Ledger) MergeSuggestions(ctx context.Context, userID uuid.UUID) ([]MergeSuggestion, error) {
	out := []MergeSuggestion{}
	err := l.st.WithUserReadOnly(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		rows, err := q.MergeSuggestions(ctx, userID)
		if err != nil {
			return err
		}
		seen := map[uuid.UUID]bool{}
		for i := range rows {
			r := &rows[i]
			if seen[r.TypedID] {
				continue // rows come nearest first for each typed row
			}
			seen[r.TypedID] = true
			out = append(out, MergeSuggestion{TypedID: r.TypedID, ImportedID: r.ImportedID, TypedOn: r.TypedOn, ImportedOn: r.ImportedOn,
				TypedPayee: r.TypedPayee, ImportedPayee: r.ImportedPayee, TypedNote: r.TypedNote, AmountMinor: r.AmountMinor,
				Currency: r.Currency, AccountID: r.AccountID, AccountName: r.AccountName})
		}
		return nil
	})
	return out, dbErr("merge suggestions", err)
}

// mergePair reads and checks a typed row and the imported row it would merge into.
func mergePair(ctx context.Context, q *db.Queries, userID, typedID, importedID uuid.UUID) (typed, imported db.TransactionForMergeRow, err error) {
	if typed, err = q.TransactionForMerge(ctx, db.TransactionForMergeParams{UserID: userID, ID: typedID}); err != nil {
		return
	}
	if imported, err = q.TransactionForMerge(ctx, db.TransactionForMergeParams{UserID: userID, ID: importedID}); err != nil {
		return
	}
	gap := typed.BookedOn.DaysUntil(imported.BookedOn)
	switch {
	case typed.Origin != "manual":
		err = invalidf("only a transaction added by hand merges into an imported one")
	case imported.Origin != "import":
		err = invalidf("a typed transaction merges into one from the bank's file")
	case typed.AccountID != imported.AccountID || typed.AmountMinor != imported.AmountMinor || gap < -mergeDays || gap > mergeDays:
		err = invalidf("the two transactions must be in the same account, of the same amount, booked within %d days", mergeDays)
	}
	return
}

// Merge keeps the imported row with the typed row's hand-set category, note, group and shared copy,
// and deletes the typed row; it returns the account (for the reclassification).
func (l *Ledger) Merge(ctx context.Context, userID, typedID, importedID uuid.UUID) (uuid.UUID, error) {
	var account uuid.UUID
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		t, imp, err := mergePair(ctx, q, userID, typedID, importedID)
		if err != nil {
			return err
		}
		account = t.AccountID
		if err := lockAccount(ctx, q, userID, account); err != nil {
			return err
		}
		// the shared copy goes to the imported row, unless that one has a copy of its own
		link := func(r db.TransactionForMergeRow) (*uuid.UUID, error) {
			id, err := q.SharedLinkOf(ctx, db.SharedLinkOfParams{UserID: userID, AccountID: r.AccountID, Fingerprint: r.Fingerprint,
				Occurrence: r.Occurrence, SplitScope: r.SplitScope})
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, nil
			}
			return &id, err
		}
		typedCopy, err := link(t)
		if err != nil {
			return err
		}
		impCopy, err := link(imp)
		if err != nil {
			return err
		}
		if typedCopy != nil && impCopy == nil {
			if err := q.MoveSharedLink(ctx, db.MoveSharedLinkParams{UserID: userID, AccountID: t.AccountID, Fingerprint: t.Fingerprint,
				Occurrence: t.Occurrence, SplitScope: t.SplitScope, ToFingerprint: imp.Fingerprint, ToOccurrence: imp.Occurrence,
				ToSplitScope: imp.SplitScope}); err != nil {
				return err
			}
		}
		patch := AnnotationPatch{}
		if t.CategorySource == "manual" && t.CategoryID != nil {
			patch.SetCategory, patch.CategoryID = true, t.CategoryID
		}
		if t.GroupID != nil && imp.GroupID == nil {
			patch.SetGroup, patch.GroupID = true, t.GroupID
		}
		if note := strings.TrimSpace(t.Note); note != "" && !strings.Contains(imp.Note, note) {
			patch.SetNote, patch.Note = true, strings.TrimSpace(strings.TrimSpace(imp.Note)+"\n"+note)
		}
		if patch.SetCategory || patch.SetGroup || patch.SetNote {
			if _, err := l.annotateTx(withoutAIOrigin(ctx), q, userID, []uuid.UUID{imp.ID}, patch); err != nil {
				return err
			}
		} else if typedCopy != nil && impCopy == nil {
			// nothing else changed: the moved copy still takes the bank's values
			if err := syncShared(ctx, q, userID, []uuid.UUID{imp.ID}, l.clock.Now(), true); err != nil {
				return err
			}
		}
		_, err = l.DeleteManualTx(ctx, q, userID, t.ID) // takes its copy along only when it wasn't moved
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return account, fmt.Errorf("transaction: %w", ErrNotFound)
	}
	return account, dbErr("merge transactions", err)
}

// KeepApart remembers that a typed row and an imported one are different payments.
func (l *Ledger) KeepApart(ctx context.Context, userID, typedID, importedID uuid.UUID) error {
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		if _, _, err := mergePair(ctx, q, userID, typedID, importedID); err != nil {
			return err
		}
		return q.DismissMerge(ctx, db.DismissMergeParams{UserID: userID, TypedID: typedID, ImportedID: importedID, Now: l.clock.Now()})
	})
	return dbErr("keep transactions apart", err)
}
