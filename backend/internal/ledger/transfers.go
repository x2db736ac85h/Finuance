package ledger

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// SetTransfer records the user's decision on transactions (PLAN task 15.3, ADR 0018): true marks
// them as transfers, false says they are not (a detected pair is unpaired on both sides), nil hands
// them back to the rules and the detection (the caller enqueues a reclassification). The decision
// is mirrored into tx_annotations, so it survives delete + re-import. A detected pair is a transfer
// already: true and nil leave it as it is. Returns the transactions changed.
func (l *Ledger) SetTransfer(ctx context.Context, userID uuid.UUID, ids []uuid.UUID, value *bool) (int, error) {
	switch {
	case len(ids) == 0:
		return 0, nil
	case len(ids) > MaxBulkIDs:
		return 0, invalidf("at most %d transactions per request", MaxBulkIDs)
	}
	var changed int
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		target := ids
		if value != nil && !*value {
			peers, err := q.TransferPeers(ctx, db.TransferPeersParams{UserID: userID, Ids: ids})
			if err != nil {
				return err
			}
			target = append(append([]uuid.UUID{}, ids...), peers...)
		}
		now := l.clock.Now()
		rows, err := q.SetManualTransfer(ctx, db.SetManualTransferParams{UserID: userID, Ids: target, IsTransfer: value, Now: now})
		if err != nil {
			return err
		}
		for i := range rows {
			r := &rows[i]
			if err := q.SetAnnotationTransfer(ctx, db.SetAnnotationTransferParams{AccountID: r.AccountID, Fingerprint: r.Fingerprint,
				Occurrence: r.Occurrence, SplitScope: r.SplitScope, UserID: userID, TransferOverride: value, Now: now}); err != nil {
				return fmt.Errorf("annotation of %s: %w", r.ID, err)
			}
		}
		if _, err := q.DeleteEmptyAnnotations(ctx, userID); err != nil {
			return err
		}
		changed = len(rows)
		return nil
	})
	return changed, dbErr("set transfer", err)
}
