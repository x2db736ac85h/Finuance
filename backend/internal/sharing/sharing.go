// Package sharing runs shared groups (Phase 19, ADR 0028): friends share a group of expenses (a
// trip, a household), split them and settle up. A group holds copies of what members share (the
// ledger keeps copies of members' transactions in step, ledger/shared.go) and expenses typed in
// the group. Everything runs under the caller's own context (store.WithUser): the membership
// policies of migration 00014 decide what they see and may change, and the checks here give the
// clear error first. Members never see each other's accounts, bank descriptions or raw rows.
package sharing

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/money"
	"github.com/x2db736ac85h/finuance/backend/internal/notify"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/splits"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// Kinds are the group kinds.
var Kinds = []string{"trip", "household", "event", "other"}

// Service runs shared groups.
type Service struct {
	st     *store.Store
	ledger *ledger.Ledger
	notify *notify.Service
	clock  platform.Clock
}

// New returns the service.
func New(st *store.Store, l *ledger.Ledger, n *notify.Service, clock platform.Clock) *Service {
	return &Service{st: st, ledger: l, notify: n, clock: clock}
}

// Group is a shared group with the caller's own membership.
type Group struct {
	ID          uuid.UUID
	Name        string
	Color       int
	Currency    string
	Kind        string
	StartsOn    *platform.Date
	EndsOn      *platform.Date
	BudgetMinor *int64
	Archived    bool
	CreatedAt   time.Time
	Me          Member
	// LinkedGroupID is the caller's personal group linked to this one, if any.
	LinkedGroupID *uuid.UUID
}

// Member is someone in a group as the others see them: a display name, never an e-mail.
type Member struct {
	ID          uuid.UUID
	DisplayName string
	Role        string // owner | member
	State       string // invited | active | left
	IsMe        bool
	Former      bool // the user deleted their account
}

// Expense is one expense of a group.
type Expense struct {
	ID            uuid.UUID
	AddedBy       uuid.UUID // a member id
	PaidBy        uuid.UUID // a member id
	BookedOn      platform.Date
	AmountMinor   int64
	Payee         string
	Category      string
	Note          string
	Origin        string // transaction | typed
	SplitMode     string // equal | shares | exact
	Shares        []splits.Share
	Mine          bool // the caller added it (and may change it)
	SourceMissing bool // the caller's own copy whose transaction is gone (only they see it)
}

// Settlement is a payment between two members.
type Settlement struct {
	ID          uuid.UUID
	From, To    uuid.UUID
	AmountMinor int64
	SettledOn   platform.Date
	Note        string
	Mine        bool
}

// Detail is everything the group page shows.
type Detail struct {
	Group       Group
	Members     []Member
	Expenses    []Expense
	Settlements []Settlement
	Balances    map[uuid.UUID]int64
	Transfers   []splits.Transfer
}

func invalidf(format string, args ...any) error {
	return fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), ledger.ErrInvalid)
}

func statef(format string, args ...any) error {
	return fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), ledger.ErrState)
}

func notFound(what string) error { return fmt.Errorf("%s: %w", what, ledger.ErrNotFound) }

func groupOf(g db.SharedGroup, me db.SharedGroupMember, userID uuid.UUID) Group {
	return Group{ID: g.ID, Name: g.Name, Color: int(g.Color), Currency: g.Currency, Kind: g.Kind, StartsOn: g.StartsOn,
		EndsOn: g.EndsOn, BudgetMinor: g.BudgetMinor, Archived: g.ArchivedAt != nil, CreatedAt: g.CreatedAt,
		Me: memberOf(me, userID)}
}

func memberOf(m db.SharedGroupMember, userID uuid.UUID) Member {
	return Member{ID: m.ID, DisplayName: m.DisplayName, Role: m.Role, State: m.State,
		IsMe: m.UserID != nil && *m.UserID == userID, Former: m.UserID == nil}
}

// myGroup loads a group and the caller's membership; not a member (or left) is not found.
func myGroup(ctx context.Context, q *db.Queries, userID, groupID uuid.UUID) (db.SharedGroup, db.SharedGroupMember, error) {
	g, err := q.GetSharedGroup(ctx, groupID)
	if errors.Is(err, pgx.ErrNoRows) {
		return g, db.SharedGroupMember{}, notFound("shared group")
	}
	if err != nil {
		return g, db.SharedGroupMember{}, err
	}
	me, err := q.GetMyMembership(ctx, db.GetMyMembershipParams{GroupID: groupID, UserID: &userID})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && me.State == "left") {
		return g, me, notFound("shared group")
	}
	return g, me, err
}

func active(me db.SharedGroupMember) error {
	if me.State != "active" {
		return statef("accept the invitation first")
	}
	return nil
}

func displayName(ctx context.Context, q *db.Queries, userID uuid.UUID) (string, error) {
	name, err := q.FriendDisplayName(ctx, userID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("set a display name in Settings → Profile first: %w", platform.ErrDisplayNameRequired)
	}
	return cut(name, 80), nil
}

func cut(s string, n int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// GroupInput is what the owner sets on a group.
type GroupInput struct {
	Name        string
	Color       int
	Currency    string // create only
	Kind        string
	StartsOn    *platform.Date
	EndsOn      *platform.Date
	BudgetMinor *int64
	Archived    bool       // update only
	FromGroupID *uuid.UUID // create only: the personal group to share (linked, and its transactions copied)
}

func (in GroupInput) clean() (GroupInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	switch n := utf8.RuneCountInString(in.Name); {
	case n == 0 || n > 60:
		return in, invalidf("a group name has 1 to 60 characters")
	case in.Color < 0 || in.Color > 7:
		return in, invalidf("colour 0 to 7")
	}
	if in.Kind == "" {
		in.Kind = "trip"
	}
	if !slices.Contains(Kinds, in.Kind) {
		return in, invalidf("kind must be one of %s", strings.Join(Kinds, ", "))
	}
	if in.StartsOn != nil && in.EndsOn != nil && in.EndsOn.Before(*in.StartsOn) {
		return in, invalidf("the group ends before it starts")
	}
	if in.BudgetMinor != nil && *in.BudgetMinor <= 0 {
		in.BudgetMinor = nil
	}
	return in, nil
}

// Create makes a shared group with the caller as its owner. From a personal group, that group is
// linked and its transactions are shared.
func (s *Service) Create(ctx context.Context, userID uuid.UUID, in GroupInput) (Group, error) {
	in, err := in.clean()
	if err != nil {
		return Group{}, err
	}
	if in.Currency == "" {
		in.Currency = "PLN"
	}
	if _, ok := money.Exponent(in.Currency); !ok {
		return Group{}, invalidf("unknown currency %q", in.Currency)
	}
	now := s.clock.Now()
	var out Group
	err = s.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		name, err := displayName(ctx, q, userID)
		if err != nil {
			return err
		}
		if in.FromGroupID != nil {
			g, err := q.GetGroup(ctx, db.GetGroupParams{UserID: userID, ID: *in.FromGroupID})
			if errors.Is(err, pgx.ErrNoRows) {
				return notFound("group")
			}
			if err != nil {
				return err
			}
			if g.SharedGroupID != nil {
				return statef("%s is shared already", g.Name)
			}
		}
		g, err := q.CreateSharedGroup(ctx, db.CreateSharedGroupParams{ID: platform.NewID(), OwnerID: &userID, Name: in.Name,
			Color: int16(in.Color), Currency: in.Currency, Kind: in.Kind, StartsOn: in.StartsOn, EndsOn: in.EndsOn, //nolint:gosec // 0…7
			BudgetMinor: in.BudgetMinor, Now: now})
		if err != nil {
			return err
		}
		me, err := q.InsertSharedMember(ctx, db.InsertSharedMemberParams{ID: platform.NewID(), GroupID: g.ID, UserID: &userID,
			Role: "owner", State: "active", DisplayName: name, Now: now, JoinedAt: &now})
		if err != nil {
			return err
		}
		if in.FromGroupID != nil {
			if _, err := q.LinkGroupToShared(ctx, db.LinkGroupToSharedParams{UserID: userID, ID: *in.FromGroupID, SharedGroupID: &g.ID}); err != nil {
				return err
			}
		}
		out = groupOf(g, me, userID)
		return nil
	})
	if err != nil {
		return Group{}, ledger.DBError("create shared group", err)
	}
	if in.FromGroupID != nil {
		if err := s.ledger.SyncSharedGroup(ctx, userID, *in.FromGroupID); err != nil {
			return out, err
		}
	}
	return out, nil
}

// OwnedCount is how many active shared groups the user owns (the maxSharedGroups limit).
func (s *Service) OwnedCount(ctx context.Context, userID uuid.UUID) (int64, error) {
	var n int64
	err := s.st.WithUserReadOnly(ctx, userID, func(q *db.Queries, _ pgx.Tx) (err error) {
		n, err = q.CountOwnedSharedGroups(ctx, &userID)
		return err
	})
	return n, err
}

// List returns the groups the user is in or invited to.
func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]Group, error) {
	var out []Group
	err := s.st.WithUserReadOnly(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		rows, err := q.ListMySharedGroups(ctx, &userID)
		for i := range rows {
			r := &rows[i]
			g := db.SharedGroup{ID: r.ID, OwnerID: r.OwnerID, Name: r.Name, Color: r.Color, Currency: r.Currency, Kind: r.Kind,
				StartsOn: r.StartsOn, EndsOn: r.EndsOn, BudgetMinor: r.BudgetMinor, ArchivedAt: r.ArchivedAt, CreatedAt: r.CreatedAt}
			x := groupOf(g, db.SharedGroupMember{ID: r.MemberID, UserID: &userID, Role: r.Role, State: r.State}, userID)
			x.LinkedGroupID = r.LinkedGroupID
			out = append(out, x)
		}
		return err
	})
	return out, ledger.DBError("list shared groups", err)
}

// Get returns a group with its members, expenses, settlements, balances and suggested transfers.
// An invited user sees the group and its members only.
func (s *Service) Get(ctx context.Context, userID, groupID uuid.UUID) (Detail, error) {
	var out Detail
	err := s.st.WithUserReadOnly(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		g, me, err := myGroup(ctx, q, userID, groupID)
		if err != nil {
			return err
		}
		out.Group = groupOf(g, me, userID)
		if linked, err := q.GroupLinkedTo(ctx, db.GroupLinkedToParams{UserID: userID, SharedGroupID: &groupID}); err == nil {
			out.Group.LinkedGroupID = &linked
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		members, err := q.ListSharedMembers(ctx, groupID)
		if err != nil {
			return err
		}
		for i := range members {
			m := &members[i]
			out.Members = append(out.Members, memberOf(*m, userID))
		}
		if me.State != "active" {
			return nil
		}
		return s.fill(ctx, q, userID, groupID, &out)
	})
	return out, ledger.DBError("shared group", err)
}

func (s *Service) fill(ctx context.Context, q *db.Queries, userID, groupID uuid.UUID, out *Detail) error {
	exps, err := q.ListSharedExpenses(ctx, groupID)
	if err != nil {
		return err
	}
	splitRows, err := q.ListSharedSplits(ctx, groupID)
	if err != nil {
		return err
	}
	byExpense := map[uuid.UUID][]splits.Share{}
	for _, r := range splitRows {
		w := 0
		if r.Weight != nil {
			w = int(*r.Weight)
		}
		byExpense[r.ExpenseID] = append(byExpense[r.ExpenseID], splits.Share{Member: r.MemberID, Minor: r.ShareMinor, Weight: w})
	}
	copies, err := q.SharedCopiesFrom(ctx, db.SharedCopiesFromParams{UserID: userID, GroupID: groupID})
	if err != nil {
		return err
	}
	missing := map[uuid.UUID]bool{}
	for _, c := range copies {
		if !c.SourcePresent {
			missing[c.ExpenseID] = true
		}
	}
	var calc []splits.Expense
	for i := range exps {
		e := &exps[i]
		x := Expense{ID: e.ID, AddedBy: e.AddedMember, PaidBy: e.PaidBy, BookedOn: e.BookedOn, AmountMinor: e.AmountMinor,
			Payee: e.Payee, Category: e.CategoryName, Note: e.Note, Origin: e.Origin, SplitMode: e.SplitMode,
			Shares: byExpense[e.ID], Mine: e.AddedBy != nil && *e.AddedBy == userID, SourceMissing: missing[e.ID]}
		out.Expenses = append(out.Expenses, x)
		calc = append(calc, splits.Expense{PaidBy: e.PaidBy, Amount: e.AmountMinor, Shares: x.Shares})
	}
	sets, err := q.ListSharedSettlements(ctx, groupID)
	if err != nil {
		return err
	}
	var calcS []splits.Settlement
	for i := range sets {
		st := &sets[i]
		out.Settlements = append(out.Settlements, Settlement{ID: st.ID, From: st.FromMember, To: st.ToMember, AmountMinor: st.AmountMinor,
			SettledOn: st.SettledOn, Note: st.Note, Mine: st.CreatedBy != nil && *st.CreatedBy == userID})
		calcS = append(calcS, splits.Settlement{From: st.FromMember, To: st.ToMember, Amount: st.AmountMinor})
	}
	out.Balances = splits.Balances(calc, calcS)
	for _, m := range out.Members {
		if _, ok := out.Balances[m.ID]; !ok && m.State != "invited" {
			out.Balances[m.ID] = 0
		}
	}
	out.Transfers = splits.Simplify(out.Balances)
	return nil
}

// Update changes the group (owner only).
func (s *Service) Update(ctx context.Context, userID, groupID uuid.UUID, in GroupInput) (Group, error) {
	in, err := in.clean()
	if err != nil {
		return Group{}, err
	}
	var out Group
	err = s.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		g, me, err := myGroup(ctx, q, userID, groupID)
		if err != nil {
			return err
		}
		if me.Role != "owner" || me.State != "active" {
			return fmt.Errorf("only the group's owner can change it: %w", platform.ErrForbidden)
		}
		archived := g.ArchivedAt
		switch {
		case !in.Archived:
			archived = nil
		case archived == nil:
			now := s.clock.Now()
			archived = &now
		}
		g, err = q.UpdateSharedGroup(ctx, db.UpdateSharedGroupParams{ID: groupID, Name: in.Name, Color: int16(in.Color), Kind: in.Kind, //nolint:gosec // 0…7
			StartsOn: in.StartsOn, EndsOn: in.EndsOn, BudgetMinor: in.BudgetMinor, ArchivedAt: archived})
		out = groupOf(g, me, userID)
		return err
	})
	return out, ledger.DBError("update shared group", err)
}

// Delete deletes a group that nobody else ever joined (others archive it instead).
func (s *Service) Delete(ctx context.Context, userID, groupID uuid.UUID) error {
	err := s.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		_, me, err := myGroup(ctx, q, userID, groupID)
		if err != nil {
			return err
		}
		if me.Role != "owner" {
			return fmt.Errorf("only the group's owner can delete it: %w", platform.ErrForbidden)
		}
		members, err := q.ListSharedMembers(ctx, groupID)
		if err != nil {
			return err
		}
		for i := range members {
			m := &members[i]
			if m.ID != me.ID && m.State != "invited" {
				return statef("other people have been in this group: archive it instead")
			}
		}
		if _, err := q.UnlinkGroupsFromShared(ctx, db.UnlinkGroupsFromSharedParams{UserID: userID, SharedGroupID: &groupID}); err != nil {
			return err
		}
		_, err = q.DeleteSharedGroup(ctx, groupID)
		return err
	})
	return ledger.DBError("delete shared group", err)
}
