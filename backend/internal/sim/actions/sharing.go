package actions

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/x2db736ac85h/finuance/backend/internal/client/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/sim"
	"github.com/x2db736ac85h/finuance/backend/internal/sim/identity"
)

// Friends and sharing: a friend code made and another persona redeeming it (codes pass between
// personas on the engine's notice board; a synthetic user can only befriend synthetic users, ADR
// 0041), a shared group with an invitation, accepting it, an expense split equally, settling up,
// the group's report, and leaving a group (rare).
func sharing() []sim.Action {
	return []sim.Action{
		{Name: "sharing.code", Area: "sharing", Run: sharingCode},
		{Name: "sharing.befriend", Area: "sharing", Run: sharingBefriend},
		{Name: "sharing.group", Area: "sharing", Run: sharingGroup},
		{Name: "sharing.accept", Area: "sharing", Run: sharingAccept},
		{Name: "sharing.expense", Area: "sharing", Run: sharingExpense},
		{Name: "sharing.settle", Area: "sharing", Run: sharingSettle},
		{Name: "sharing.report", Area: "sharing", Run: sharingReport},
		{Name: "sharing.leave", Area: "sharing", Run: sharingLeave},
	}
}

// maxSharedGroups keeps the groups a persona owns few.
const maxSharedGroups = 4

const codePrefix = "friend-code/"

// sharingCode makes a friend code and leaves it for another persona until it expires (the end of
// its clock hour and a grace), every half hour at most.
func sharingCode(ctx context.Context, a *sim.Actor) error {
	if !a.Every("sharing.code", 30*time.Minute) {
		return sim.ErrNothingToDo
	}
	r, err := a.API.NewFriendCodeWithResponse(ctx)
	if err != nil {
		return err
	}
	if err := sim.Expect("make a friend code", r.HTTPResponse, http.StatusCreated); err != nil {
		return err
	}
	a.Engine().Post(codePrefix+a.Persona.Slug, r.JSON201.Code, r.JSON201.ExpiresAt)
	return nil
}

func friends(ctx context.Context, a *sim.Actor) ([]gen.Friend, error) {
	r, err := a.API.ListFriendsWithResponse(ctx)
	if err != nil {
		return nil, err
	}
	if err := sim.Expect("friends", r.HTTPResponse, http.StatusOK); err != nil {
		return nil, err
	}
	return *r.JSON200, nil
}

// sharingBefriend redeems a code another persona left, from one it isn't friends with yet. The
// instance allows ten codes entered an hour per address, which all personas share behind the
// front door: one every seven minutes across all of them.
func sharingBefriend(ctx context.Context, a *sim.Actor) error {
	list, err := friends(ctx, a)
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, f := range list {
		known[f.DisplayName] = true
	}
	_, code, ok := a.Engine().Take(codePrefix, func(key string) bool {
		p, found := identity.BySlug(strings.TrimPrefix(key, codePrefix))
		return found && p.Slug != a.Persona.Slug && !known[p.Name]
	})
	if !ok {
		return sim.ErrNothingToDo
	}
	if !a.Engine().Every("sharing.befriend", 7*time.Minute) {
		return sim.ErrNothingToDo
	}
	r, err := a.API.AddFriendWithResponse(ctx, gen.AddFriendJSONRequestBody{Code: code})
	if err != nil {
		return err
	}
	return sim.Expect("redeem a friend code", r.HTTPResponse, http.StatusCreated, http.StatusConflict)
}

func sharedGroups(ctx context.Context, a *sim.Actor) ([]gen.SharedGroup, error) {
	r, err := a.API.ListSharedGroupsWithResponse(ctx)
	if err != nil {
		return nil, err
	}
	if err := sim.Expect("shared groups", r.HTTPResponse, http.StatusOK); err != nil {
		return nil, err
	}
	return *r.JSON200, nil
}

// sharingGroup makes a shared group for a trip and invites one of the persona's friends.
func sharingGroup(ctx context.Context, a *sim.Actor) error {
	if !a.Allow(sim.Invites) { // the instance's limit of invitations per user and hour
		return sim.ErrNothingToDo
	}
	fr, err := friends(ctx, a)
	if err != nil {
		return err
	}
	groups, err := sharedGroups(ctx, a)
	if err != nil {
		return err
	}
	owned := 0
	for i := range groups {
		if groups[i].Me.Role == gen.Owner {
			owned++
		}
	}
	if len(fr) == 0 || owned >= maxSharedGroups {
		return sim.ErrNothingToDo
	}
	kind := gen.SharedGroupKindTrip
	g, err := a.API.CreateSharedGroupWithResponse(ctx, gen.CreateSharedGroupJSONRequestBody{
		Name: fmt.Sprintf("%s z przyjaciółmi", sim.Pick(a, places...)), Kind: &kind})
	if err != nil {
		return err
	}
	if err := sim.Expect("make a shared group", g.HTTPResponse, http.StatusCreated); err != nil {
		return err
	}
	friend := fr[a.IntN(len(fr))]
	r, err := a.API.InviteToSharedGroupWithResponse(ctx, g.JSON201.Id, gen.InviteToSharedGroupJSONRequestBody{FriendId: friend.Id})
	if err != nil {
		return err
	}
	return sim.Expect("invite a friend", r.HTTPResponse, http.StatusCreated)
}

func sharingAccept(ctx context.Context, a *sim.Actor) error {
	groups, err := sharedGroups(ctx, a)
	if err != nil {
		return err
	}
	for i := range groups {
		if groups[i].Me.State != gen.SharedMemberStateInvited {
			continue
		}
		r, err := a.API.AcceptSharedGroupWithResponse(ctx, groups[i].Id)
		if err != nil {
			return err
		}
		return sim.Expect("accept an invitation", r.HTTPResponse, http.StatusOK)
	}
	return sim.ErrNothingToDo
}

// activeGroup is a shared group the persona is in with at least one other active member: the
// group and the other active members.
func activeGroup(ctx context.Context, a *sim.Actor) (*gen.SharedGroupDetail, []gen.SharedMember, error) {
	groups, err := sharedGroups(ctx, a)
	if err != nil {
		return nil, nil, err
	}
	groups = slices.DeleteFunc(groups, func(g gen.SharedGroup) bool { return g.Me.State != gen.SharedMemberStateActive || g.Archived })
	for range len(groups) {
		g := groups[a.IntN(len(groups))]
		d, err := a.API.GetSharedGroupWithResponse(ctx, g.Id)
		if err != nil {
			return nil, nil, err
		}
		if err := sim.Expect("a shared group", d.HTTPResponse, http.StatusOK); err != nil {
			return nil, nil, err
		}
		var others []gen.SharedMember
		for _, m := range d.JSON200.Members {
			if !m.IsMe && m.State == gen.SharedMemberStateActive && !m.Former {
				others = append(others, m)
			}
		}
		if len(others) > 0 {
			return d.JSON200, others, nil
		}
	}
	return nil, nil, sim.ErrNothingToDo
}

func sharingExpense(ctx context.Context, a *sim.Actor) error {
	g, others, err := activeGroup(ctx, a)
	if err != nil {
		return err
	}
	members := []openapi_types.UUID{g.Group.Me.Id}
	for _, m := range others {
		members = append(members, m.Id)
	}
	mode := gen.SharedSplitModeEqual
	day := openapi_types.Date{Time: a.DataDay().Time()}
	r, err := a.API.AddSharedExpenseWithResponse(ctx, g.Group.Id, gen.AddSharedExpenseJSONRequestBody{
		Payee:       ptr(sim.Pick(a, "NOCLEG POD SOSNAMI", "KOLACJA W PORCIE", "BILETY KOLEJOWE", "ZAKUPY NA DROGĘ")),
		AmountMinor: ptr(int64(4_000 + a.IntN(40)*1_000)), BookedOn: &day,
		Split: &gen.SharedSplit{Mode: &mode, Members: &members}})
	if err != nil {
		return err
	}
	return sim.Expect("add a shared expense", r.HTTPResponse, http.StatusCreated)
}

func sharingSettle(ctx context.Context, a *sim.Actor) error {
	g, others, err := activeGroup(ctx, a)
	if err != nil {
		return err
	}
	to := others[a.IntN(len(others))]
	r, err := a.API.AddSharedSettlementWithResponse(ctx, g.Group.Id, gen.AddSharedSettlementJSONRequestBody{From: g.Group.Me.Id, To: to.Id,
		AmountMinor: int64(1_000 + a.IntN(20)*500), SettledOn: openapi_types.Date{Time: a.DataDay().Time()}})
	if err != nil {
		return err
	}
	return sim.Expect("settle up", r.HTTPResponse, http.StatusCreated)
}

func sharingReport(ctx context.Context, a *sim.Actor) error {
	g, _, err := activeGroup(ctx, a)
	if err != nil {
		return err
	}
	r, err := a.API.SharedGroupReportWithResponse(ctx, g.Group.Id)
	if err != nil {
		return err
	}
	return sim.Expect("a shared group's report", r.HTTPResponse, http.StatusOK)
}

// sharingLeave leaves a group the persona doesn't own and where its balance is zero (the
// instance asks to settle up first), once a week at most.
func sharingLeave(ctx context.Context, a *sim.Actor) error {
	groups, err := sharedGroups(ctx, a)
	if err != nil {
		return err
	}
	for i := range groups {
		g := &groups[i]
		if g.Me.Role == gen.Owner || g.Me.State != gen.SharedMemberStateActive {
			continue
		}
		d, err := a.API.GetSharedGroupWithResponse(ctx, g.Id)
		if err != nil {
			return err
		}
		if err := sim.Expect("a shared group", d.HTTPResponse, http.StatusOK); err != nil {
			return err
		}
		if slices.ContainsFunc(d.JSON200.Balances, func(b gen.SharedBalance) bool { return b.MemberId == g.Me.Id && b.BalanceMinor != 0 }) {
			continue
		}
		if !a.Every("sharing.leave", 7*24*time.Hour) {
			return sim.ErrNothingToDo
		}
		r, err := a.API.LeaveSharedGroupWithResponse(ctx, g.Id)
		if err != nil {
			return err
		}
		return sim.Expect("leave a shared group", r.HTTPResponse, http.StatusNoContent)
	}
	return sim.ErrNothingToDo
}
