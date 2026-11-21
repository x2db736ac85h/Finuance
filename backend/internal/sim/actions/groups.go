package actions

import (
	"context"
	"fmt"
	"net/http"

	"github.com/x2db736ac85h/finuance/backend/internal/client/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/sim"
)

// Groups: make one for a trip, rename one, the timeline, a group's report, delete an old one.
func groups() []sim.Action {
	return []sim.Action{
		{Name: "groups.create", Area: "groups", Run: groupCreate},
		{Name: "groups.rename", Area: "groups", Run: groupRename},
		{Name: "groups.timeline", Area: "groups", Run: groupTimeline},
		{Name: "groups.report", Area: "groups", Run: groupReport},
		{Name: "groups.delete", Area: "groups", Run: groupDelete},
	}
}

// maxGroups keeps a persona's groups few: above it the persona deletes rather than makes one.
const maxGroups = 12

var places = []string{"Mazury", "Zakopane", "Gdańsk", "Kraków", "Hel", "Bieszczady", "Wrocław", "Toruń"}

func listGroups(ctx context.Context, a *sim.Actor) ([]gen.Group, error) {
	r, err := a.API.ListGroupsWithResponse(ctx)
	if err != nil {
		return nil, err
	}
	if err := sim.Expect("groups", r.HTTPResponse, http.StatusOK); err != nil {
		return nil, err
	}
	return *r.JSON200, nil
}

// someGroup is one of the persona's groups, or nothing.
func someGroup(ctx context.Context, a *sim.Actor) (gen.Group, error) {
	list, err := listGroups(ctx, a)
	if err != nil {
		return gen.Group{}, err
	}
	if len(list) == 0 {
		return gen.Group{}, sim.ErrNothingToDo
	}
	return list[a.IntN(len(list))], nil
}

func groupCreate(ctx context.Context, a *sim.Actor) error {
	list, err := listGroups(ctx, a)
	if err != nil {
		return err
	}
	if len(list) >= maxGroups {
		return groupDelete(ctx, a)
	}
	name := fmt.Sprintf("%s %d", sim.Pick(a, places...), a.DataDay().Y)
	r, err := a.API.CreateGroupWithResponse(ctx, nil, gen.CreateGroupJSONRequestBody{Name: name, Color: ptr(a.IntN(8))})
	if err != nil {
		return err
	}
	return sim.Expect("make a group", r.HTTPResponse, http.StatusCreated, http.StatusConflict)
}

func groupRename(ctx context.Context, a *sim.Actor) error {
	g, err := someGroup(ctx, a)
	if err != nil {
		return err
	}
	name := fmt.Sprintf("%s %d", sim.Pick(a, places...), a.DataDay().Y-a.IntN(3))
	r, err := a.API.UpdateGroupWithResponse(ctx, g.Id, gen.UpdateGroupJSONRequestBody{Name: name})
	if err != nil {
		return err
	}
	return sim.Expect("rename a group", r.HTTPResponse, http.StatusOK, http.StatusConflict)
}

func groupTimeline(ctx context.Context, a *sim.Actor) error {
	r, err := a.API.GroupsTimelineWithResponse(ctx)
	if err != nil {
		return err
	}
	return sim.Expect("group timeline", r.HTTPResponse, http.StatusOK)
}

func groupReport(ctx context.Context, a *sim.Actor) error {
	g, err := someGroup(ctx, a)
	if err != nil {
		return err
	}
	r, err := a.API.GroupReportWithResponse(ctx, g.Id)
	if err != nil {
		return err
	}
	return sim.Expect("group report", r.HTTPResponse, http.StatusOK)
}

func groupDelete(ctx context.Context, a *sim.Actor) error {
	g, err := someGroup(ctx, a)
	if err != nil {
		return err
	}
	r, err := a.API.DeleteGroupWithResponse(ctx, g.Id)
	if err != nil {
		return err
	}
	return sim.Expect("delete a group", r.HTTPResponse, http.StatusNoContent)
}
