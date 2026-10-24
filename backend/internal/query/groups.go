package query

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// GroupInfo is a group with its stats as the Groups menu counts them (every account, ignoring
// filters, transfers included; PLAN §4.7). FirstOn/LastOn are nil for an empty group.
type GroupInfo struct {
	ID          uuid.UUID
	Name        string
	Count       int64
	SpentMinor  int64
	IncomeMinor int64
	Currency    string // the most common currency of its transactions; "" for an empty group
	FirstOn     *platform.Date
	LastOn      *platform.Date
	BudgetMinor *int64
}

// Groups lists the user's groups with their stats, read-only (the assistant's group_summary).
func (e *Engine) Groups(ctx context.Context, userID uuid.UUID) ([]GroupInfo, error) {
	var out []GroupInfo
	err := e.st.WithUserReadOnly(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		rows, err := q.GroupStats(ctx, userID)
		for i := range rows {
			r := &rows[i]
			g := GroupInfo{ID: r.ID, Name: r.Name, Count: r.TxCount, SpentMinor: r.SpentMinor, IncomeMinor: r.IncomeMinor,
				Currency: r.Currency, BudgetMinor: r.BudgetMinor}
			if !r.FirstOn.IsZero() {
				first, last := r.FirstOn, r.LastOn
				g.FirstOn, g.LastOn = &first, &last
			}
			out = append(out, g)
		}
		return err
	})
	return out, err
}
