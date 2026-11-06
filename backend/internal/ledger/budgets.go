package ledger

import (
	"context"
	"errors"

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
		before, err := l.budgetBefore(ctx, q, userID, categoryID)
		if err != nil {
			return err
		}
		if err := q.SetBudget(ctx, db.SetBudgetParams{UserID: userID, CategoryID: categoryID, AmountMinor: amountMinor, Now: l.clock.Now()}); err != nil {
			return err
		}
		return l.recordBudget(ctx, q, userID, categoryID, before, &amountMinor)
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
		before, err := l.budgetBefore(ctx, q, userID, categoryID)
		if err != nil {
			return err
		}
		if _, err := q.DeleteBudget(ctx, db.DeleteBudgetParams{UserID: userID, CategoryID: categoryID}); err != nil {
			return err
		}
		return l.recordBudget(ctx, q, userID, categoryID, before, nil)
	})
	return dbErr("clear budget", err)
}

// BudgetState is a category's budget in an AI change: AmountMinor nil = no budget of its own (the
// default from earlier months applies).
type BudgetState struct {
	AmountMinor *int64 `json:"amountMinor"`
}

// budgetBefore reads the budget an assistant change replaces; nothing is read for ordinary edits.
func (l *Ledger) budgetBefore(ctx context.Context, q *db.Queries, userID, categoryID uuid.UUID) (*int64, error) {
	if _, ok := AIOriginFrom(ctx); !ok {
		return nil, nil
	}
	v, err := q.BudgetAmountForUpdate(ctx, db.BudgetAmountForUpdateParams{UserID: userID, CategoryID: categoryID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &v, err
}

func (l *Ledger) recordBudget(ctx context.Context, q *db.Queries, userID, categoryID uuid.UUID, before, after *int64) error {
	return RecordAIChange(ctx, q, l.clock, userID, []AIItem{{Entity: AIEntityBudget, ID: categoryID,
		Before: BudgetState{AmountMinor: before}, After: BudgetState{AmountMinor: after}}})
}

// BudgetTx sets (amount) or clears (nil) a budget inside the caller's transaction as an ordinary
// edit (an AI change's revert).
func (l *Ledger) BudgetTx(ctx context.Context, q *db.Queries, userID, categoryID uuid.UUID, amountMinor *int64) error {
	if amountMinor == nil {
		_, err := q.DeleteBudget(ctx, db.DeleteBudgetParams{UserID: userID, CategoryID: categoryID})
		return err
	}
	return q.SetBudget(ctx, db.SetBudgetParams{UserID: userID, CategoryID: categoryID, AmountMinor: *amountMinor, Now: l.clock.Now()})
}
