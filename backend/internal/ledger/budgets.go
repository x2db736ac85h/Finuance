package ledger

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// Budget is a monthly limit for a category (global per category, as in the legacy app).
type Budget struct {
	CategoryID   uuid.UUID
	CategoryName string
	AmountMinor  int64
}

// SetBudget creates or replaces the budget of a category.
func (l *Ledger) SetBudget(ctx context.Context, userID, categoryID uuid.UUID, amountMinor int64) error {
	if amountMinor < 0 {
		return invalidf("budget must not be negative")
	}
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		if err := ownCategory(ctx, q, userID, categoryID); err != nil {
			return err
		}
		return q.SetBudget(ctx, db.SetBudgetParams{UserID: userID, CategoryID: categoryID, AmountMinor: amountMinor, Now: l.clock.Now()})
	})
	return dbErr("set budget", err)
}

// ListBudgets returns every budget with its category name.
func (l *Ledger) ListBudgets(ctx context.Context, userID uuid.UUID) ([]Budget, error) {
	var out []Budget
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		rows, err := q.ListBudgets(ctx, userID)
		for _, b := range rows {
			out = append(out, Budget{CategoryID: b.CategoryID, CategoryName: b.Name, AmountMinor: b.AmountMinor})
		}
		return err
	})
	return out, dbErr("list budgets", err)
}

// DeleteBudget removes the budget of a category.
func (l *Ledger) DeleteBudget(ctx context.Context, userID, categoryID uuid.UUID) error {
	var n int64
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) (err error) {
		n, err = q.DeleteBudget(ctx, db.DeleteBudgetParams{UserID: userID, CategoryID: categoryID})
		return err
	})
	return notFound("delete budget", n, err)
}

// ClearBudget removes the budget of a category the user owns, if it has one (PUT with null:
// back to the default budget; idempotent).
func (l *Ledger) ClearBudget(ctx context.Context, userID, categoryID uuid.UUID) error {
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		if err := ownCategory(ctx, q, userID, categoryID); err != nil {
			return err
		}
		_, err := q.DeleteBudget(ctx, db.DeleteBudgetParams{UserID: userID, CategoryID: categoryID})
		return err
	})
	return dbErr("clear budget", err)
}
