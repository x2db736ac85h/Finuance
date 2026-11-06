package ledger

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// MaxBulkIDs caps the ids of one bulk operation (PLAN §4.7).
const MaxBulkIDs = 5000

const maxNote = 2000

// AnnotationPatch is a user edit applied to a set of transactions. Only the fields whose Set* flag
// is true change:
//
//   - group: GroupID, or nil to remove the label;
//   - category: CategoryID sets a manual category; nil resets to the bank category;
//   - note: Note, where "" clears it.
type AnnotationPatch struct {
	SetGroup    bool
	GroupID     *uuid.UUID
	SetCategory bool
	CategoryID  *uuid.UUID
	SetNote     bool
	Note        string
}

// Annotate applies patch to the transactions ids and mirrors the result into tx_annotations, all in
// one database transaction (PLAN §4.4 "user annotations"). The annotation is keyed by the dedupe
// identity, so it outlives the transaction row: delete the import, re-import the file, and the
// commit re-applies it. An annotation left with nothing in it is deleted. Ids that are not the
// user's are ignored; the result is the number of transactions changed.
func (l *Ledger) Annotate(ctx context.Context, userID uuid.UUID, ids []uuid.UUID, patch AnnotationPatch) (int, error) {
	switch {
	case len(ids) == 0:
		return 0, nil
	case len(ids) > MaxBulkIDs:
		return 0, invalidf("at most %d transactions per request", MaxBulkIDs)
	case !patch.SetGroup && !patch.SetCategory && !patch.SetNote:
		return 0, invalidf("empty annotation patch")
	case len([]rune(patch.Note)) > maxNote:
		return 0, invalidf("note longer than %d characters", maxNote)
	}
	var changed int
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) (err error) {
		changed, err = l.annotateTx(ctx, q, userID, ids, patch)
		return err
	})
	return changed, dbErr("annotate", err)
}

// annotateTx is Annotate inside the caller's transaction (also used by AddManual, so a new
// expense and its category, group and note are written together).
func (l *Ledger) annotateTx(ctx context.Context, q *db.Queries, userID uuid.UUID, ids []uuid.UUID, patch AnnotationPatch) (int, error) {
	if patch.SetGroup && patch.GroupID != nil {
		if err := ownGroup(ctx, q, userID, *patch.GroupID); err != nil {
			return 0, err
		}
	}
	if patch.SetCategory && patch.CategoryID != nil {
		if err := ownCategory(ctx, q, userID, *patch.CategoryID); err != nil {
			return 0, err
		}
	}
	// an applied assistant proposal records what it replaces (ADR 0030)
	var before map[uuid.UUID]db.TransactionAnnotationStateRow
	if _, ok := AIOriginFrom(ctx); ok {
		prev, err := q.TransactionAnnotationState(ctx, db.TransactionAnnotationStateParams{UserID: userID, Ids: ids})
		if err != nil {
			return 0, err
		}
		before = make(map[uuid.UUID]db.TransactionAnnotationStateRow, len(prev))
		for _, r := range prev {
			before[r.ID] = r
		}
	}
	now := l.clock.Now()
	rows, err := q.UpdateTransactionAnnotations(ctx, db.UpdateTransactionAnnotationsParams{
		UserID: userID, Ids: ids, Now: now,
		SetGroup: patch.SetGroup, GroupID: patch.GroupID,
		SetCategory: patch.SetCategory, CategoryID: patch.CategoryID,
		SetNote: patch.SetNote, Note: patch.Note, NoteN: nameKey(patch.Note),
	})
	if err != nil {
		return 0, err
	}
	for i := range rows {
		r := &rows[i]
		var manual *uuid.UUID // annotations hold manual categories only
		if r.CategorySource == "manual" {
			manual = r.CategoryID
		}
		if err := q.UpsertAnnotation(ctx, db.UpsertAnnotationParams{AccountID: r.AccountID, Fingerprint: r.Fingerprint,
			Occurrence: r.Occurrence, SplitScope: r.SplitScope, UserID: userID, GroupID: r.GroupID, CategoryID: manual,
			Note: r.Note, NoteN: r.NoteN, Now: now}); err != nil {
			return 0, fmt.Errorf("annotation of %s: %w", r.ID, err)
		}
	}
	if _, err := q.DeleteEmptyAnnotations(ctx, userID); err != nil {
		return 0, err
	}
	// copies in shared groups follow the group, category and note (ADR 0028)
	changed := make([]uuid.UUID, len(rows))
	for i := range rows {
		changed[i] = rows[i].ID
	}
	if err := syncShared(ctx, q, userID, changed, now, true); err != nil {
		return 0, err
	}
	if before != nil {
		items := make([]AIItem, 0, len(rows))
		for i := range rows {
			r, b := &rows[i], before[rows[i].ID]
			items = append(items, AIItem{Entity: AIEntityTransaction, ID: r.ID,
				Before: annotationState(b.CategoryID, b.CategorySource, b.GroupID, patch),
				After:  annotationState(r.CategoryID, r.CategorySource, r.GroupID, patch)})
		}
		if err := RecordAIChange(ctx, q, l.clock, userID, items); err != nil {
			return 0, err
		}
	}
	return len(rows), nil
}
