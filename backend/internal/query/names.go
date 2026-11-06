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

// Groups lists the user's groups with their stats, read-only (the assistant's group_summary and
// action proposals).
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

// CategoryRefs lists the user's categories, read-only (the assistant's action proposals).
func (e *Engine) CategoryRefs(ctx context.Context, userID uuid.UUID) ([]NamedRef, error) {
	var out []NamedRef
	err := e.st.WithUserReadOnly(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		rows, err := q.ListCategories(ctx, userID)
		for _, r := range rows {
			out = append(out, NamedRef{ID: r.ID, Name: r.Name})
		}
		return err
	})
	return out, err
}

// SharedLink is a personal group linked to a shared group the user is in: putting a transaction
// into it shares a copy (ADR 0028).
type SharedLink struct {
	GroupID       uuid.UUID
	GroupName     string
	SharedGroupID uuid.UUID
	SharedName    string
	Currency      string
}

// SharedLinks lists the user's personal groups linked to active shared groups.
func (e *Engine) SharedLinks(ctx context.Context, userID uuid.UUID) ([]SharedLink, error) {
	var out []SharedLink
	err := e.st.WithUserReadOnly(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		rows, err := q.ListSharedGroupLinks(ctx, userID)
		for _, r := range rows {
			out = append(out, SharedLink{GroupID: r.ID, GroupName: r.Name, SharedGroupID: r.SharedGroupID, SharedName: r.SharedName, Currency: r.Currency})
		}
		return err
	})
	return out, err
}

// AccountRef is an account with its kind and currency (the assistant's add_expense target).
type AccountRef struct {
	ID       uuid.UUID
	Name     string
	Kind     string
	Currency string
}

// Accounts lists the user's accounts that aren't archived.
func (e *Engine) Accounts(ctx context.Context, userID uuid.UUID) ([]AccountRef, error) {
	var out []AccountRef
	err := e.st.WithUserReadOnly(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		rows, err := q.ListAccounts(ctx, userID)
		for i := range rows {
			if r := &rows[i]; r.ArchivedAt == nil {
				out = append(out, AccountRef{ID: r.ID, Name: r.Name, Kind: r.Kind, Currency: r.Currency})
			}
		}
		return err
	})
	return out, err
}

// Budgets maps category ids to their budgets (minor units).
func (e *Engine) Budgets(ctx context.Context, userID uuid.UUID) (map[uuid.UUID]int64, error) {
	out := map[uuid.UUID]int64{}
	err := e.st.WithUserReadOnly(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		rows, err := q.ListBudgets(ctx, userID)
		for _, r := range rows {
			out[r.CategoryID] = r.AmountMinor
		}
		return err
	})
	return out, err
}

// SharedMember is a member of a shared group as the assistant names them.
type SharedMember struct {
	ID     uuid.UUID
	Name   string
	Active bool
	Me     bool
}

// SharedExpense is an expense of a shared group (the split_expense target).
type SharedExpense struct {
	ID          uuid.UUID
	BookedOn    platform.Date
	AmountMinor int64
	Payee       string
	Category    string
	Note        string
	PaidBy      uuid.UUID
	Origin      string // transaction (a copy) or typed
	SplitMode   string
	Mine        bool // added by the user: only they may change its split
}

// SharedGroup returns a shared group's members and expenses, newest first, read under the
// membership RLS (nothing for a group the user isn't in).
func (e *Engine) SharedGroup(ctx context.Context, userID, sharedGroupID uuid.UUID) ([]SharedMember, []SharedExpense, error) {
	var members []SharedMember
	var expenses []SharedExpense
	err := e.st.WithUserReadOnly(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		ms, err := q.ListSharedMembers(ctx, sharedGroupID)
		if err != nil {
			return err
		}
		for i := range ms {
			m := &ms[i]
			members = append(members, SharedMember{ID: m.ID, Name: m.DisplayName, Active: m.State == "active",
				Me: m.UserID != nil && *m.UserID == userID})
		}
		xs, err := q.ListSharedExpenses(ctx, sharedGroupID)
		if err != nil {
			return err
		}
		for i := range xs {
			x := &xs[i]
			expenses = append(expenses, SharedExpense{ID: x.ID, BookedOn: x.BookedOn, AmountMinor: x.AmountMinor, Payee: x.Payee,
				Category: x.CategoryName, Note: x.Note, PaidBy: x.PaidBy, Origin: x.Origin, SplitMode: x.SplitMode,
				Mine: x.AddedBy != nil && *x.AddedBy == userID})
		}
		return nil
	})
	return members, expenses, err
}
