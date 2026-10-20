package ledger

import (
	"context"
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
	ID        uuid.UUID
	Name      string
	Color     int // 0…7
	CreatedAt time.Time
}

func groupOf(g db.Group) Group {
	return Group{ID: g.ID, Name: g.Name, Color: int(g.Color), CreatedAt: g.CreatedAt}
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

// CreateGroup adds a group. color < 0 picks NextGroupColor.
func (l *Ledger) CreateGroup(ctx context.Context, userID uuid.UUID, name string, color int) (Group, error) {
	name, err := cleanName("group", name, maxGroupName)
	if err != nil {
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
		out = groupOf(g)
		return err
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

// UpdateGroup renames and/or recolours a group. (The client renames view filters that mention the
// old name, legacy renameGroupFilters.)
func (l *Ledger) UpdateGroup(ctx context.Context, userID, id uuid.UUID, name string, color int) (Group, error) {
	name, err := cleanName("group", name, maxGroupName)
	if err != nil {
		return Group{}, err
	}
	if color < 0 || color >= GroupColors {
		return Group{}, invalidf("group colour must be 0–%d", GroupColors-1)
	}
	var out Group
	err = l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		g, err := q.UpdateGroup(ctx, db.UpdateGroupParams{UserID: userID, ID: id, Name: name, NameN: nameKey(name), Color: int16(color)}) //nolint:gosec // 0…7, checked above
		out = groupOf(g)
		return err
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
