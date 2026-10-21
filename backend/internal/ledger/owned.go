package ledger

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// Foreign keys are checked without row-level security: PostgreSQL would accept a transaction of
// user A that points at user B's group, because the group row exists. Every write that stores a
// reference to another user-owned row therefore checks, in the same transaction, that the row is
// the caller's. Under WithUser another user's row is invisible, so the lookup fails as not found.

func ownAccount(ctx context.Context, q *db.Queries, userID, id uuid.UUID) error {
	if _, err := q.GetAccount(ctx, db.GetAccountParams{UserID: userID, ID: id}); err != nil {
		return fmt.Errorf("account %s: %w", id, err)
	}
	return nil
}

func ownCategory(ctx context.Context, q *db.Queries, userID, id uuid.UUID) error {
	if _, err := q.GetCategory(ctx, db.GetCategoryParams{UserID: userID, ID: id}); err != nil {
		return fmt.Errorf("category %s: %w", id, err)
	}
	return nil
}

func ownGroup(ctx context.Context, q *db.Queries, userID, id uuid.UUID) error {
	if _, err := q.GetGroup(ctx, db.GetGroupParams{UserID: userID, ID: id}); err != nil {
		return fmt.Errorf("group %s: %w", id, err)
	}
	return nil
}

// lockAccount takes the per-account writer lock (see LockAccount) for the rest of the transaction.
func lockAccount(ctx context.Context, q *db.Queries, userID, id uuid.UUID) error {
	if _, err := q.LockAccount(ctx, db.LockAccountParams{UserID: userID, ID: id}); err != nil {
		return fmt.Errorf("account %s: %w", id, err)
	}
	return nil
}
