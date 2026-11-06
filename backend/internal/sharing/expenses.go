package sharing

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/notify"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/splits"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// Split says how an expense is shared. Equal (the default) splits between Members (all active
// members when empty); shares splits by Weights (one per member in Members); exact takes Exact
// as given (it must add up). "For someone else" is one member taking all of it.
type Split struct {
	Mode    string // equal | shares | exact
	Members []uuid.UUID
	Weights []int
	Exact   []splits.Share
}

// ExpenseInput is an expense typed in a group (or, for a copy, only its split).
type ExpenseInput struct {
	BookedOn    platform.Date
	AmountMinor int64
	Payee       string
	Category    string
	Note        string
	PaidBy      *uuid.UUID // a member id; default the caller
	Split       Split
}

func (in ExpenseInput) clean() (ExpenseInput, error) {
	in.Payee, in.Category, in.Note = cut(in.Payee, 200), cut(in.Category, 80), cut(in.Note, 2000)
	switch {
	case in.Payee == "":
		in.Payee = "(no payee)"
	case strings.ContainsRune(in.Payee+in.Category+in.Note, 0):
		return in, invalidf("text contains a NUL character")
	}
	if in.AmountMinor == 0 {
		return in, invalidf("the amount can't be zero")
	}
	if in.BookedOn.Y < 1990 || in.BookedOn.Y > 2100 {
		return in, invalidf("the date must be between 1990 and 2100")
	}
	return in, nil
}

// shares turns a split into exact parts of amount, checking every member is active in the group.
func shares(split Split, amount int64, members []db.SharedGroupMember) ([]splits.Share, string, error) {
	activeIDs := make([]uuid.UUID, 0, len(members))
	isActive := map[uuid.UUID]bool{}
	for i := range members {
		m := &members[i]
		if m.State == "active" {
			activeIDs = append(activeIDs, m.ID)
			isActive[m.ID] = true
		}
	}
	check := func(ids []uuid.UUID) error {
		for _, id := range ids {
			if !isActive[id] {
				return invalidf("a split can only include active members")
			}
		}
		return nil
	}
	mode := split.Mode
	if mode == "" {
		mode = "equal"
	}
	switch mode {
	case "equal":
		ids := split.Members
		if len(ids) == 0 {
			ids = activeIDs
		}
		if err := check(ids); err != nil {
			return nil, "", err
		}
		ids = slices.Clone(ids)
		slices.SortFunc(ids, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
		s, err := splits.Equal(amount, slices.Compact(ids))
		if err != nil {
			return nil, "", invalidf("split it between at least one member")
		}
		return s, mode, nil
	case "shares":
		if err := check(split.Members); err != nil {
			return nil, "", err
		}
		s, err := splits.Weighted(amount, split.Members, split.Weights)
		if err != nil {
			return nil, "", invalidf("shares need one whole number from 1 to 1000 per member")
		}
		return s, mode, nil
	case "exact":
		ids := make([]uuid.UUID, len(split.Exact))
		for i, x := range split.Exact {
			ids[i] = x.Member
		}
		if err := check(ids); err != nil {
			return nil, "", err
		}
		if err := splits.Exact(amount, split.Exact); err != nil {
			return nil, "", invalidf("the exact amounts must add up to the expense")
		}
		return split.Exact, mode, nil
	}
	return nil, "", invalidf("split mode must be equal, shares or exact")
}

func writeShares(ctx context.Context, q *db.Queries, groupID, expenseID uuid.UUID, parts []splits.Share) error {
	if err := q.DeleteSharedSplits(ctx, expenseID); err != nil {
		return err
	}
	for _, p := range parts {
		var w *int32
		if p.Weight > 0 {
			v := int32(p.Weight) //nolint:gosec // weights are at most 1000
			w = &v
		}
		if err := q.InsertSharedSplit(ctx, db.InsertSharedSplitParams{ExpenseID: expenseID, GroupID: groupID, MemberID: p.Member,
			ShareMinor: p.Minor, Weight: w}); err != nil {
			return err
		}
	}
	return nil
}

func activeGroup(ctx context.Context, q *db.Queries, userID, groupID uuid.UUID) (db.SharedGroup, db.SharedGroupMember, []db.SharedGroupMember, error) {
	g, me, err := myGroup(ctx, q, userID, groupID)
	if err != nil {
		return g, me, nil, err
	}
	if err := active(me); err != nil {
		return g, me, nil, err
	}
	if g.ArchivedAt != nil {
		return g, me, nil, statef("%s is archived", g.Name)
	}
	members, err := q.ListSharedMembers(ctx, groupID)
	return g, me, members, err
}

// AddExpense adds an expense typed in the group (cash, or paid by someone else). The other active
// members are told.
func (s *Service) AddExpense(ctx context.Context, userID, groupID uuid.UUID, in ExpenseInput) (uuid.UUID, error) {
	in, err := in.clean()
	if err != nil {
		return uuid.UUID{}, err
	}
	id := platform.NewID()
	var others []uuid.UUID
	var group, who string
	err = s.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		g, me, members, err := activeGroup(ctx, q, userID, groupID)
		if err != nil {
			return err
		}
		paidBy := me.ID
		if in.PaidBy != nil {
			paidBy = *in.PaidBy
		}
		if !slices.ContainsFunc(members, func(m db.SharedGroupMember) bool { return m.ID == paidBy && m.State == "active" }) {
			return invalidf("the payer must be an active member")
		}
		parts, mode, err := shares(in.Split, in.AmountMinor, members)
		if err != nil {
			return err
		}
		if err := q.InsertSharedExpense(ctx, db.InsertSharedExpenseParams{ID: id, GroupID: groupID, AddedBy: &userID, AddedMember: me.ID,
			PaidBy: paidBy, BookedOn: in.BookedOn, AmountMinor: in.AmountMinor, Payee: in.Payee, CategoryName: in.Category,
			Note: in.Note, Origin: "typed", SplitMode: mode, Now: s.clock.Now()}); err != nil {
			return err
		}
		for i := range members {
			m := &members[i]
			if m.State == "active" && m.UserID != nil && *m.UserID != userID {
				others = append(others, *m.UserID)
			}
		}
		group, who = g.Name, me.DisplayName
		return writeShares(ctx, q, groupID, id, parts)
	})
	if err != nil {
		return uuid.UUID{}, ledger.DBError("add shared expense", err)
	}
	for _, o := range others {
		if err := s.notify.Send(ctx, o, notify.SharedExpenseAdded, map[string]string{"groupId": groupID.String(), "group": group,
			"name": who, "payee": in.Payee}); err != nil {
			return id, err
		}
	}
	return id, nil
}

// UpdateExpense changes an expense the caller added. A copy of a transaction keeps its date,
// amount, payee, category and note (they follow the transaction); only its split changes.
func (s *Service) UpdateExpense(ctx context.Context, userID, groupID, expenseID uuid.UUID, in ExpenseInput) error {
	err := s.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		_, me, members, err := activeGroup(ctx, q, userID, groupID)
		if err != nil {
			return err
		}
		e, err := q.GetSharedExpense(ctx, expenseID)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && e.GroupID != groupID) {
			return notFound("expense")
		}
		if err != nil {
			return err
		}
		if e.AddedBy == nil || *e.AddedBy != userID {
			return fmt.Errorf("only the member who added an expense can change it: %w", platform.ErrForbidden)
		}
		if e.Origin == "transaction" {
			in.BookedOn, in.AmountMinor, in.Payee, in.Category, in.Note = e.BookedOn, e.AmountMinor, e.Payee, e.CategoryName, e.Note
			in.PaidBy = &e.PaidBy
		}
		if in, err = in.clean(); err != nil {
			return err
		}
		paidBy := me.ID
		if in.PaidBy != nil {
			paidBy = *in.PaidBy
		}
		if !slices.ContainsFunc(members, func(m db.SharedGroupMember) bool { return m.ID == paidBy }) {
			return invalidf("the payer must be a member")
		}
		parts, mode, err := shares(in.Split, in.AmountMinor, members)
		if err != nil {
			return err
		}
		// an assistant proposal (split_expense) records the split it replaces (ADR 0030)
		var before *SplitState
		if _, ok := ledger.AIOriginFrom(ctx); ok {
			if before, err = splitState(ctx, q, e.SplitMode, expenseID); err != nil {
				return err
			}
		}
		if _, err := q.UpdateSharedExpense(ctx, db.UpdateSharedExpenseParams{ID: expenseID, GroupID: groupID, BookedOn: in.BookedOn,
			AmountMinor: in.AmountMinor, Payee: in.Payee, CategoryName: in.Category, Note: in.Note, PaidBy: paidBy, SplitMode: mode,
			Now: s.clock.Now()}); err != nil {
			return err
		}
		if err := writeShares(ctx, q, groupID, expenseID, parts); err != nil {
			return err
		}
		if before == nil {
			return nil
		}
		after, err := splitState(ctx, q, mode, expenseID)
		if err != nil {
			return err
		}
		return ledger.RecordAIChange(ctx, q, s.clock, userID, []ledger.AIItem{{Entity: ledger.AIEntitySharedExpense, ID: expenseID,
			Before: before, After: after}})
	})
	return ledger.DBError("update shared expense", err)
}

// SplitState is an expense's split in an AI change: the mode and each member's share.
type SplitState struct {
	Mode   string       `json:"mode"`
	Shares []SplitShare `json:"shares"`
}

// SplitShare is one member's part of a SplitState.
type SplitShare struct {
	Member uuid.UUID `json:"member"`
	Minor  int64     `json:"minor"`
	Weight *int32    `json:"weight,omitempty"`
}

// Same reports whether two states are the same split.
func (s SplitState) Same(o SplitState) bool {
	if s.Mode != o.Mode || len(s.Shares) != len(o.Shares) {
		return false
	}
	for i := range s.Shares {
		a, b := s.Shares[i], o.Shares[i]
		if a.Member != b.Member || a.Minor != b.Minor || (a.Weight == nil) != (b.Weight == nil) || (a.Weight != nil && *a.Weight != *b.Weight) {
			return false
		}
	}
	return true
}

func splitState(ctx context.Context, q *db.Queries, mode string, expenseID uuid.UUID) (*SplitState, error) {
	rows, err := q.SharedSplitsOf(ctx, expenseID)
	if err != nil {
		return nil, err
	}
	st := &SplitState{Mode: mode, Shares: make([]SplitShare, len(rows))}
	for i, r := range rows {
		st.Shares[i] = SplitShare{Member: r.MemberID, Minor: r.ShareMinor, Weight: r.Weight}
	}
	return st, nil
}

// CurrentSplit reads an expense's split inside the caller's transaction (an AI change's revert);
// nil when the expense is gone.
func CurrentSplit(ctx context.Context, q *db.Queries, expenseID uuid.UUID) (*SplitState, error) {
	e, err := q.GetSharedExpense(ctx, expenseID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return splitState(ctx, q, e.SplitMode, expenseID)
}

// RestoreSplitTx puts a recorded split back inside the caller's transaction (an AI change's
// revert). Only the member who added the expense may (as for any split change).
func RestoreSplitTx(ctx context.Context, q *db.Queries, now time.Time, userID, expenseID uuid.UUID, st SplitState) error {
	e, err := q.GetSharedExpense(ctx, expenseID)
	if err != nil {
		return err
	}
	if e.AddedBy == nil || *e.AddedBy != userID {
		return fmt.Errorf("only the member who added an expense can change it: %w", platform.ErrForbidden)
	}
	if _, err := q.UpdateSharedExpense(ctx, db.UpdateSharedExpenseParams{ID: expenseID, GroupID: e.GroupID, BookedOn: e.BookedOn,
		AmountMinor: e.AmountMinor, Payee: e.Payee, CategoryName: e.CategoryName, Note: e.Note, PaidBy: e.PaidBy, SplitMode: st.Mode,
		Now: now}); err != nil {
		return err
	}
	parts := make([]splits.Share, len(st.Shares))
	for i, sh := range st.Shares {
		w := 0
		if sh.Weight != nil {
			w = int(*sh.Weight)
		}
		parts[i] = splits.Share{Member: sh.Member, Minor: sh.Minor, Weight: w}
	}
	return writeShares(ctx, q, e.GroupID, expenseID, parts)
}

// DeleteExpense deletes an expense the caller typed in the group. A copy goes when its
// transaction leaves the group (or the typed cash row is deleted).
func (s *Service) DeleteExpense(ctx context.Context, userID, groupID, expenseID uuid.UUID) error {
	err := s.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		if _, _, _, err := activeGroup(ctx, q, userID, groupID); err != nil {
			return err
		}
		e, err := q.GetSharedExpense(ctx, expenseID)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && e.GroupID != groupID) {
			return notFound("expense")
		}
		if err != nil {
			return err
		}
		if e.AddedBy == nil || *e.AddedBy != userID {
			return fmt.Errorf("only the member who added an expense can delete it: %w", platform.ErrForbidden)
		}
		if e.Origin == "transaction" {
			return statef("this expense is one of your transactions: take it out of the group in the table instead")
		}
		_, err = q.DeleteSharedExpense(ctx, expenseID)
		return err
	})
	return ledger.DBError("delete shared expense", err)
}

// SettlementInput is a payment between two members; the caller must be one of them.
type SettlementInput struct {
	From, To    uuid.UUID
	AmountMinor int64
	SettledOn   platform.Date
	Note        string
}

// Settle records a payment and tells the other member.
func (s *Service) Settle(ctx context.Context, userID, groupID uuid.UUID, in SettlementInput) (Settlement, error) {
	in.Note = cut(in.Note, 500)
	switch {
	case in.AmountMinor <= 0:
		return Settlement{}, invalidf("a payment is more than zero")
	case in.From == in.To:
		return Settlement{}, invalidf("a payment goes between two members")
	case in.SettledOn.Y < 1990 || in.SettledOn.Y > 2100:
		return Settlement{}, invalidf("the date must be between 1990 and 2100")
	}
	var out Settlement
	var other *uuid.UUID
	var group, who string
	err := s.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		g, me, members, err := activeGroup(ctx, q, userID, groupID)
		if err != nil {
			return err
		}
		if in.From != me.ID && in.To != me.ID {
			return invalidf("you can only record a payment you made or received")
		}
		for _, id := range []uuid.UUID{in.From, in.To} {
			i := slices.IndexFunc(members, func(m db.SharedGroupMember) bool { return m.ID == id })
			if i < 0 {
				return invalidf("both sides must be members")
			}
			if id != me.ID {
				other = members[i].UserID
			}
		}
		st, err := q.InsertSharedSettlement(ctx, db.InsertSharedSettlementParams{ID: platform.NewID(), GroupID: groupID, FromMember: in.From,
			ToMember: in.To, AmountMinor: in.AmountMinor, SettledOn: in.SettledOn, Note: in.Note, CreatedBy: &userID, Now: s.clock.Now()})
		if err != nil {
			return err
		}
		out = Settlement{ID: st.ID, From: st.FromMember, To: st.ToMember, AmountMinor: st.AmountMinor, SettledOn: st.SettledOn, Note: st.Note, Mine: true}
		group, who = g.Name, me.DisplayName
		return nil
	})
	if err != nil {
		return Settlement{}, ledger.DBError("settle", err)
	}
	if other != nil {
		return out, s.notify.Send(ctx, *other, notify.SharedSettled, map[string]string{"groupId": groupID.String(), "group": group, "name": who})
	}
	return out, nil
}

// DeleteSettlement removes a payment the caller recorded.
func (s *Service) DeleteSettlement(ctx context.Context, userID, groupID, settlementID uuid.UUID) error {
	err := s.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		if _, _, _, err := activeGroup(ctx, q, userID, groupID); err != nil {
			return err
		}
		n, err := q.DeleteSharedSettlement(ctx, db.DeleteSharedSettlementParams{GroupID: groupID, ID: settlementID})
		if err == nil && n == 0 {
			return notFound("payment")
		}
		return err
	})
	return ledger.DBError("delete settlement", err)
}
