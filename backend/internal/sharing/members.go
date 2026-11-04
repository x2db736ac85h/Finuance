package sharing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/notify"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/splits"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// Invite invites a friend (owner only). Someone who isn't the owner's friend can't be invited, and
// someone who left can't be invited back (the history stays theirs).
func (s *Service) Invite(ctx context.Context, userID, groupID, friendID uuid.UUID) (Member, error) {
	var out Member
	var groupName, byName string
	err := s.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		g, me, err := myGroup(ctx, q, userID, groupID)
		if err != nil {
			return err
		}
		if me.Role != "owner" || me.State != "active" {
			return fmt.Errorf("only the group's owner can invite: %w", platform.ErrForbidden)
		}
		if g.ArchivedAt != nil {
			return statef("%s is archived", g.Name)
		}
		friends, err := q.ListFriends(ctx, userID)
		if err != nil {
			return err
		}
		var name string
		for _, f := range friends {
			if f.ID == friendID {
				name = f.DisplayName
			}
		}
		if name == "" {
			return invalidf("only friends can be invited: add them in Settings → Friends first")
		}
		if _, err := q.GetMyMembership(ctx, db.GetMyMembershipParams{GroupID: groupID, UserID: &friendID}); err == nil {
			return fmt.Errorf("%s is or was in this group already: %w", name, ledger.ErrConflict)
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		m, err := q.InsertSharedMember(ctx, db.InsertSharedMemberParams{ID: platform.NewID(), GroupID: groupID, UserID: &friendID,
			Role: "member", State: "invited", DisplayName: cut(name, 80), InvitedBy: &userID, Now: s.clock.Now()})
		if err != nil {
			return err
		}
		out, groupName, byName = memberOf(m, userID), g.Name, me.DisplayName
		return nil
	})
	if err != nil {
		return Member{}, ledger.DBError("invite", err)
	}
	return out, s.notify.Send(ctx, friendID, notify.SharedInvited, map[string]string{"groupId": groupID.String(), "group": groupName, "name": byName})
}

// Accept joins a group the user was invited to. A personal group of the same name is created and
// linked, so the user can put their own transactions into it.
func (s *Service) Accept(ctx context.Context, userID, groupID uuid.UUID) (Group, error) {
	var out Group
	var owner *uuid.UUID
	var myName string
	err := s.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		g, me, err := myGroup(ctx, q, userID, groupID)
		if err != nil {
			return err
		}
		if me.State != "invited" {
			return statef("you are in this group already")
		}
		name, err := displayName(ctx, q, userID)
		if err != nil {
			return err
		}
		if _, err := q.SetSharedMemberState(ctx, db.SetSharedMemberStateParams{GroupID: groupID, ID: me.ID, State: "active",
			DisplayName: &name, Now: s.nowp()}); err != nil {
			return err
		}
		me.State, me.DisplayName = "active", name
		out, owner, myName = groupOf(g, me, userID), g.OwnerID, name
		return nil
	})
	if err != nil {
		return Group{}, ledger.DBError("accept", err)
	}
	if err := s.linkPersonalGroup(ctx, userID, out); err != nil {
		return out, err
	}
	if owner != nil && *owner != userID {
		return out, s.notify.Send(ctx, *owner, notify.SharedJoined, map[string]string{"groupId": groupID.String(), "group": out.Name, "name": myName})
	}
	return out, nil
}

// linkPersonalGroup creates the user's personal group for a shared group they joined ("Trip", or
// "Trip (shared)" when the name is taken) and links it.
func (s *Service) linkPersonalGroup(ctx context.Context, userID uuid.UUID, g Group) error {
	var created ledger.Group
	var err error
	for _, name := range []string{g.Name, cut(g.Name, 31) + " (shared)"} {
		created, err = s.ledger.CreateGroup(ctx, userID, cut(name, 40), g.Color)
		if !errors.Is(err, ledger.ErrConflict) {
			break
		}
	}
	if err != nil {
		return err
	}
	err = s.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		_, err := q.LinkGroupToShared(ctx, db.LinkGroupToSharedParams{UserID: userID, ID: created.ID, SharedGroupID: &g.ID})
		return err
	})
	return ledger.DBError("link group", err)
}

// Decline turns an invitation down.
func (s *Service) Decline(ctx context.Context, userID, groupID uuid.UUID) error {
	err := s.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		_, me, err := myGroup(ctx, q, userID, groupID)
		if err != nil {
			return err
		}
		if me.State != "invited" {
			return statef("there is no invitation to decline")
		}
		_, err = q.SetSharedMemberState(ctx, db.SetSharedMemberStateParams{GroupID: groupID, ID: me.ID, State: "left", Now: s.nowp()})
		return err
	})
	return ledger.DBError("decline", err)
}

// balanceOf computes one member's balance in a group (the caller must be an active member).
func balanceOf(ctx context.Context, q *db.Queries, groupID, memberID uuid.UUID) (int64, error) {
	exps, err := q.ListSharedExpenses(ctx, groupID)
	if err != nil {
		return 0, err
	}
	rows, err := q.ListSharedSplits(ctx, groupID)
	if err != nil {
		return 0, err
	}
	shares := map[uuid.UUID][]splits.Share{}
	for _, r := range rows {
		shares[r.ExpenseID] = append(shares[r.ExpenseID], splits.Share{Member: r.MemberID, Minor: r.ShareMinor})
	}
	calc := make([]splits.Expense, 0, len(exps))
	for i := range exps {
		e := &exps[i]
		calc = append(calc, splits.Expense{PaidBy: e.PaidBy, Amount: e.AmountMinor, Shares: shares[e.ID]})
	}
	sets, err := q.ListSharedSettlements(ctx, groupID)
	if err != nil {
		return 0, err
	}
	calcS := make([]splits.Settlement, 0, len(sets))
	for i := range sets {
		st := &sets[i]
		calcS = append(calcS, splits.Settlement{From: st.FromMember, To: st.ToMember, Amount: st.AmountMinor})
	}
	return splits.Balances(calc, calcS)[memberID], nil
}

// Leave leaves a group; it needs a zero balance (settle up first). The owner archives the group
// instead. The user's personal group is unlinked; what they shared stays in the group.
func (s *Service) Leave(ctx context.Context, userID, groupID uuid.UUID) error {
	var owner *uuid.UUID
	var name, myName string
	err := s.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		g, me, err := myGroup(ctx, q, userID, groupID)
		if err != nil {
			return err
		}
		if err := active(me); err != nil {
			return err
		}
		if me.Role == "owner" {
			return statef("the owner can't leave: archive the group instead")
		}
		bal, err := balanceOf(ctx, q, groupID, me.ID)
		if err != nil {
			return err
		}
		if bal != 0 {
			return statef("settle up first: your balance in %s isn't zero", g.Name)
		}
		if _, err := q.SetSharedMemberState(ctx, db.SetSharedMemberStateParams{GroupID: groupID, ID: me.ID, State: "left", Now: s.nowp()}); err != nil {
			return err
		}
		_, err = q.UnlinkGroupsFromShared(ctx, db.UnlinkGroupsFromSharedParams{UserID: userID, SharedGroupID: &groupID})
		owner, name, myName = g.OwnerID, g.Name, me.DisplayName
		return err
	})
	if err != nil {
		return ledger.DBError("leave", err)
	}
	if owner != nil {
		return s.notify.Send(ctx, *owner, notify.SharedLeft, map[string]string{"groupId": groupID.String(), "group": name, "name": myName})
	}
	return nil
}

// RemoveMember withdraws an invitation, or removes a member whose balance is zero (owner only).
func (s *Service) RemoveMember(ctx context.Context, userID, groupID, memberID uuid.UUID) error {
	err := s.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		_, me, err := myGroup(ctx, q, userID, groupID)
		if err != nil {
			return err
		}
		if me.Role != "owner" || me.State != "active" {
			return fmt.Errorf("only the group's owner can remove members: %w", platform.ErrForbidden)
		}
		m, err := q.GetSharedMember(ctx, db.GetSharedMemberParams{GroupID: groupID, ID: memberID})
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound("member")
		}
		if err != nil {
			return err
		}
		switch {
		case m.ID == me.ID:
			return statef("the owner can't remove themselves")
		case m.State == "invited":
			_, err = q.DeleteSharedMember(ctx, db.DeleteSharedMemberParams{GroupID: groupID, ID: memberID})
			return err
		case m.State == "left":
			return nil
		}
		bal, err := balanceOf(ctx, q, groupID, memberID)
		if err != nil {
			return err
		}
		if bal != 0 {
			return statef("%s's balance isn't zero: settle up first", m.DisplayName)
		}
		_, err = q.SetSharedMemberState(ctx, db.SetSharedMemberStateParams{GroupID: groupID, ID: memberID, State: "left", Now: s.nowp()})
		return err
	})
	return ledger.DBError("remove member", err)
}

func (s *Service) nowp() *time.Time {
	t := s.clock.Now()
	return &t
}
