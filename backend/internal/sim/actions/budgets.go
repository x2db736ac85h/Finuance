package actions

import (
	"context"
	"net/http"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/x2db736ac85h/finuance/backend/internal/client/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/sim"
)

// Budgets and goals: read them with the pace chart, set a category's budget (often at the start
// of a month), make, change and drop a goal.
func budgets() []sim.Action {
	return []sim.Action{
		{Name: "budgets.read", Area: "budgets", Run: budgetsRead},
		{Name: "budgets.set", Area: "budgets", Run: budgetSet},
		{Name: "budgets.goal", Area: "budgets", Run: goalCreate},
		{Name: "budgets.goal-update", Area: "budgets", Run: goalUpdate},
	}
}

// maxGoals keeps a persona's goals few: above it, a new goal replaces the oldest.
const maxGoals = 4

func budgetsRead(ctx context.Context, a *sim.Actor) error {
	b, err := a.API.ListBudgetsWithResponse(ctx)
	if err != nil {
		return err
	}
	if err := sim.Expect("budgets", b.HTTPResponse, http.StatusOK); err != nil {
		return err
	}
	g, err := a.API.ListGoalsWithResponse(ctx)
	if err != nil {
		return err
	}
	if err := sim.Expect("goals", g.HTTPResponse, http.StatusOK); err != nil {
		return err
	}
	p, err := a.API.InsightPaceWithResponse(ctx, gen.InsightPaceJSONRequestBody{Context: gen.QueryContext{}, Compare: gen.PaceRequestCompareAvg3})
	if err != nil {
		return err
	}
	return sim.Expect("pace", p.HTTPResponse, http.StatusOK)
}

func budgetSet(ctx context.Context, a *sim.Actor) error {
	cat, err := someCategory(ctx, a)
	if err != nil {
		return err
	}
	amount := ptr(int64(20_000 + a.IntN(20)*10_000)) // 200 … 2 100 zł
	if a.IntN(10) == 0 {
		amount = nil // remove the budget
	}
	r, err := a.API.PutBudgetWithResponse(ctx, cat.Id, nil, gen.PutBudgetJSONRequestBody{AmountMinor: amount})
	if err != nil {
		return err
	}
	return sim.Expect("set a budget", r.HTTPResponse, http.StatusNoContent)
}

func listGoals(ctx context.Context, a *sim.Actor) ([]gen.Goal, error) {
	r, err := a.API.ListGoalsWithResponse(ctx)
	if err != nil {
		return nil, err
	}
	if err := sim.Expect("goals", r.HTTPResponse, http.StatusOK); err != nil {
		return nil, err
	}
	return r.JSON200.Goals, nil
}

func goalCreate(ctx context.Context, a *sim.Actor) error {
	goals, err := listGoals(ctx, a)
	if err != nil {
		return err
	}
	if len(goals) >= maxGoals {
		d, err := a.API.DeleteGoalWithResponse(ctx, goals[len(goals)-1].Id)
		if err != nil {
			return err
		}
		return sim.Expect("drop a goal", d.HTTPResponse, http.StatusNoContent)
	}
	deadline := platform.DateOf(a.Now().AddDate(0, 3+a.IntN(9), 0), time.UTC)
	in := gen.CreateGoalJSONRequestBody{Name: "Na " + sim.Pick(a, places...), Kind: gen.GoalInputKindSave,
		TargetMinor: ptr(int64(100_000 + a.IntN(50)*10_000)), Deadline: openapi_types.Date{Time: deadline.Time()}}
	if a.IntN(2) == 0 {
		cat, err := someCategory(ctx, a)
		if err != nil {
			return err
		}
		in = gen.CreateGoalJSONRequestBody{Name: "Mniej na " + cat.Name, Kind: gen.GoalInputKindReduce, CategoryId: &cat.Id,
			ReducePct: ptr(10 + 5*a.IntN(5)), Deadline: openapi_types.Date{Time: deadline.Time()}}
	}
	r, err := a.API.CreateGoalWithResponse(ctx, nil, in)
	if err != nil {
		return err
	}
	return sim.Expect("make a goal", r.HTTPResponse, http.StatusCreated)
}

func goalUpdate(ctx context.Context, a *sim.Actor) error {
	goals, err := listGoals(ctx, a)
	if err != nil {
		return err
	}
	if len(goals) == 0 {
		return sim.ErrNothingToDo
	}
	g := goals[a.IntN(len(goals))]
	up := gen.UpdateGoalJSONRequestBody{Name: g.Name, Deadline: g.Deadline, Version: g.Version, TargetMinor: g.TargetMinor,
		ReducePct: g.ReducePct}
	if up.TargetMinor != nil {
		up.TargetMinor = ptr(*up.TargetMinor + int64(a.IntN(5)-2)*10_000)
	}
	if g.Category != nil {
		up.CategoryId = &g.Category.Id
	}
	r, err := a.API.UpdateGoalWithResponse(ctx, g.Id, nil, up)
	if err != nil {
		return err
	}
	return sim.Expect("change a goal", r.HTTPResponse, http.StatusOK, http.StatusConflict, http.StatusUnprocessableEntity)
}
