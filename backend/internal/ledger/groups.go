package ledger

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// GroupColors is the number of colour slots (legacy GCOLS: --s1…--s7 and --g8).
const GroupColors = 8

const maxGroupName = 40

// Group is a user-defined, colour-coded label such as "Italy trip 2025" (PLAN §4.7).
type Group struct {
	ID          uuid.UUID
	Name        string
	Color       int    // 0…7
	BudgetMinor *int64 // the money out the group should stay under (PLAN 15.9); nil: no budget
	CreatedAt   time.Time
	// SharedGroupID is the shared group this group is linked to (ADR 0028): putting a transaction
	// into the group shares a copy there.
	SharedGroupID *uuid.UUID
}

func groupOf(g db.Group) Group {
	return Group{ID: g.ID, Name: g.Name, Color: int(g.Color), BudgetMinor: g.BudgetMinor, CreatedAt: g.CreatedAt, SharedGroupID: g.SharedGroupID}
}

// Budget arguments of CreateGroupWith and EditGroup: KeepBudget leaves it as it is, NoBudget
// removes it, and a positive amount sets it.
const (
	KeepBudget int64 = -1
	NoBudget   int64 = 0
)

// maxGroupBudget caps a group budget (10 billion in major units of a 2-decimal currency).
const maxGroupBudget = 1_000_000_000_000

func checkBudget(budget int64) error {
	if budget < KeepBudget || budget > maxGroupBudget {
		return invalidf("group budget must be between 0 and %d minor units", int64(maxGroupBudget))
	}
	return nil
}

func setBudget(ctx context.Context, q *db.Queries, userID, id uuid.UUID, budget int64, g *Group) error {
	if budget == KeepBudget {
		return nil
	}
	var v *int64
	if budget > 0 {
		v = &budget
	}
	if _, err := q.SetGroupBudget(ctx, db.SetGroupBudgetParams{UserID: userID, ID: id, BudgetMinor: v}); err != nil {
		return err
	}
	g.BudgetMinor = v
	return nil
}

// NextGroupColor is the legacy nextColor: the first unused slot, or — when all are used — the
// number of groups modulo the number of slots.
func NextGroupColor(existing []Group) int {
	used := map[int]bool{}
	for _, g := range existing {
		used[g.Color] = true
	}
	for k := range GroupColors {
		if !used[k] {
			return k
		}
	}
	return len(existing) % GroupColors
}

// CreateGroup adds a group without a budget. color < 0 picks NextGroupColor.
func (l *Ledger) CreateGroup(ctx context.Context, userID uuid.UUID, name string, color int) (Group, error) {
	return l.CreateGroupWith(ctx, userID, name, color, NoBudget)
}

// CreateGroupWith adds a group with a budget (NoBudget for none). color < 0 picks NextGroupColor.
func (l *Ledger) CreateGroupWith(ctx context.Context, userID uuid.UUID, name string, color int, budget int64) (Group, error) {
	name, err := cleanName("group", name, maxGroupName)
	if err != nil {
		return Group{}, err
	}
	if err := checkBudget(budget); err != nil {
		return Group{}, err
	}
	if color >= GroupColors {
		return Group{}, invalidf("group colour must be 0–%d", GroupColors-1)
	}
	var out Group
	err = l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		if color < 0 {
			rows, err := q.ListGroups(ctx, userID)
			if err != nil {
				return err
			}
			existing := make([]Group, len(rows))
			for i, g := range rows {
				existing[i] = groupOf(g)
			}
			color = NextGroupColor(existing)
		}
		g, err := q.CreateGroup(ctx, db.CreateGroupParams{ID: platform.NewID(), UserID: userID, Name: name, NameN: nameKey(name), Color: int16(color)}) //nolint:gosec // 0…7, checked above
		if err != nil {
			return err
		}
		out = groupOf(g)
		return setBudget(ctx, q, userID, g.ID, budget, &out)
	})
	return out, dbErr("create group", err)
}

// ListGroups returns the user's groups in natural Polish order.
func (l *Ledger) ListGroups(ctx context.Context, userID uuid.UUID) ([]Group, error) {
	var out []Group
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		rows, err := q.ListGroups(ctx, userID)
		for _, g := range rows {
			out = append(out, groupOf(g))
		}
		return err
	})
	return out, dbErr("list groups", err)
}

// UpdateGroup renames and/or recolours a group; color < 0 keeps the colour. (The client renames view
// filters that mention the old name, legacy renameGroupFilters.)
func (l *Ledger) UpdateGroup(ctx context.Context, userID, id uuid.UUID, name string, color int) (Group, error) {
	return l.EditGroup(ctx, userID, id, name, color, KeepBudget)
}

// EditGroup is UpdateGroup with the budget: KeepBudget, NoBudget or a positive amount.
func (l *Ledger) EditGroup(ctx context.Context, userID, id uuid.UUID, name string, color int, budget int64) (Group, error) {
	name, err := cleanName("group", name, maxGroupName)
	if err != nil {
		return Group{}, err
	}
	if err := checkBudget(budget); err != nil {
		return Group{}, err
	}
	if color >= GroupColors {
		return Group{}, invalidf("group colour must be 0–%d", GroupColors-1)
	}
	var out Group
	err = l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		if color < 0 {
			cur, err := q.GetGroup(ctx, db.GetGroupParams{UserID: userID, ID: id})
			if err != nil {
				return err
			}
			color = int(cur.Color)
		}
		g, err := q.UpdateGroup(ctx, db.UpdateGroupParams{UserID: userID, ID: id, Name: name, NameN: nameKey(name), Color: int16(color)}) //nolint:gosec // 0…7, checked above
		if err != nil {
			return err
		}
		out = groupOf(g)
		return setBudget(ctx, q, userID, id, budget, &out)
	})
	return out, dbErr("update group", err)
}

// DeleteGroup removes the group and its labels, never transactions. Annotations left empty by the
// removal are deleted.
func (l *Ledger) DeleteGroup(ctx context.Context, userID, id uuid.UUID) error {
	var n int64
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) (err error) {
		if n, err = q.DeleteGroup(ctx, db.DeleteGroupParams{UserID: userID, ID: id}); err != nil {
			return err
		}
		_, err = q.DeleteEmptyAnnotations(ctx, userID)
		return err
	})
	return notFound("delete group", n, err)
}

// GroupStat is a group with its transactions over all the user's accounts, ignoring filters
// (PLAN §4.7). SpentMinor and IncomeMinor are positive sums of money out and in (transfers
// included, legacy groupStats); FirstOn/LastOn are nil for an empty group.
type GroupStat struct {
	Group
	Count       int
	SpentMinor  int64
	IncomeMinor int64
	Currency    string
	FirstOn     *platform.Date
	LastOn      *platform.Date
}

// GroupStats returns every group with its stats, in natural Polish order.
func (l *Ledger) GroupStats(ctx context.Context, userID uuid.UUID) ([]GroupStat, error) {
	var out []GroupStat
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		rows, err := q.GroupStats(ctx, userID)
		for i := range rows {
			r := &rows[i]
			st := GroupStat{
				Group:      Group{ID: r.ID, Name: r.Name, Color: int(r.Color), BudgetMinor: r.BudgetMinor, CreatedAt: r.CreatedAt, SharedGroupID: r.SharedGroupID},
				Count:      int(r.TxCount),
				SpentMinor: r.SpentMinor, IncomeMinor: r.IncomeMinor, Currency: r.Currency,
			}
			if st.Currency == "" {
				st.Currency = defaultCurrency
			}
			if !r.FirstOn.IsZero() {
				first, last := r.FirstOn, r.LastOn
				st.FirstOn, st.LastOn = &first, &last
			}
			out = append(out, st)
		}
		return err
	})
	return out, dbErr("group stats", err)
}

// GroupStatOf returns one group's stats (after a create or an update).
func (l *Ledger) GroupStatOf(ctx context.Context, userID, id uuid.UUID) (GroupStat, error) {
	all, err := l.GroupStats(ctx, userID)
	if err != nil {
		return GroupStat{}, err
	}
	for i := range all {
		if all[i].ID == id {
			return all[i], nil
		}
	}
	return GroupStat{}, fmt.Errorf("group %s: %w", id, ErrNotFound)
}
