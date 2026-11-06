package httpapi

import (
	"context"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/x2db736ac85h/finuance/backend/internal/goals"
	"github.com/x2db736ac85h/finuance/backend/internal/httpapi/gen"
)

// Goals (ADR 0031): savings targets and reductions with their progress.

func goalDTO(g *goals.Goal) gen.Goal {
	p := g.Progress
	out := gen.Goal{Id: g.ID, Name: g.Name, Kind: gen.GoalKind(g.Kind), TargetMinor: g.TargetMinor, Currency: g.Currency,
		ReducePct: g.ReducePct, BaselineMonths: g.BaselineMonths, StartsOn: openapi_types.Date{Time: g.StartsOn.Time()},
		Deadline: openapi_types.Date{Time: g.Deadline.Time()}, Status: gen.GoalStatus(g.Status), Version: g.Version, Ai: g.AI,
		Progress: gen.GoalProgress{State: gen.GoalProgressState(p.State), Percent: p.Percent, DaysLeft: p.DaysLeft}}
	if g.AccountID != nil {
		out.Account = &gen.NamedRef{Id: *g.AccountID, Name: g.AccountName}
	}
	if g.CategoryID != nil {
		out.Category = &gen.NamedRef{Id: *g.CategoryID, Name: g.CategoryName}
	}
	if g.Kind == goals.KindSave {
		out.Progress.SavedMinor, out.Progress.ExpectedMinor, out.Progress.ProjectedMinor = &p.SavedMinor, &p.ExpectedMinor, &p.ProjectedMinor
	} else {
		out.Progress.BaselineMinor, out.Progress.MonthlyLimitMinor = &p.BaselineMinor, &p.MonthlyLimitMinor
		out.Progress.ThisMonthMinor, out.Progress.AllowedSoFarMinor = &p.ThisMonthMinor, &p.AllowedSoFarMinor
		out.Progress.MonthsKept, out.Progress.MonthsPast = &p.MonthsKept, &p.MonthsPast
	}
	return out
}

// ListGoals returns every goal with its progress.
func (a *api) ListGoals(ctx context.Context, _ gen.ListGoalsRequestObject) (gen.ListGoalsResponseObject, error) {
	list, err := a.goals.List(ctx, identity(ctx).User.ID)
	if err != nil {
		return nil, err
	}
	out := gen.ListGoals200JSONResponse{Goals: make([]gen.Goal, len(list))}
	for i := range list {
		out.Goals[i] = goalDTO(&list[i])
	}
	return out, nil
}

// CreateGoal adds a goal.
func (a *api) CreateGoal(ctx context.Context, req gen.CreateGoalRequestObject) (gen.CreateGoalResponseObject, error) {
	ctx, err := a.aiOrigin(ctx, req.Params.XAssistantChat, req.Params.XAssistantProposal, req.Params.XAssistantAuto)
	if err != nil {
		return nil, err
	}
	b := req.Body
	g, err := a.goals.Create(ctx, identity(ctx).User.ID, goals.Input{Name: b.Name, Kind: string(b.Kind), TargetMinor: b.TargetMinor,
		Currency: deref(b.Currency), AccountID: b.AccountId, CategoryID: b.CategoryId, ReducePct: b.ReducePct,
		BaselineMonths: deref(b.BaselineMonths), StartsOn: dateIn(b.StartsOn), Deadline: *dateIn(&b.Deadline)})
	if err != nil {
		return nil, err
	}
	return gen.CreateGoal201JSONResponse(goalDTO(&g)), nil
}

// UpdateGoal changes a goal (optimistic concurrency by version).
func (a *api) UpdateGoal(ctx context.Context, req gen.UpdateGoalRequestObject) (gen.UpdateGoalResponseObject, error) {
	ctx, err := a.aiOrigin(ctx, req.Params.XAssistantChat, req.Params.XAssistantProposal, req.Params.XAssistantAuto)
	if err != nil {
		return nil, err
	}
	b := req.Body
	g, err := a.goals.Update(ctx, identity(ctx).User.ID, req.Id, goals.Input{Name: b.Name, TargetMinor: b.TargetMinor,
		AccountID: b.AccountId, CategoryID: b.CategoryId, ReducePct: b.ReducePct, BaselineMonths: deref(b.BaselineMonths),
		Deadline: *dateIn(&b.Deadline), Status: string(deref(b.Status)), Version: b.Version})
	if err != nil {
		return nil, err
	}
	return gen.UpdateGoal200JSONResponse(goalDTO(&g)), nil
}

// DeleteGoal removes a goal.
func (a *api) DeleteGoal(ctx context.Context, req gen.DeleteGoalRequestObject) (gen.DeleteGoalResponseObject, error) {
	if err := a.goals.Delete(ctx, identity(ctx).User.ID, req.Id); err != nil {
		return nil, err
	}
	return gen.DeleteGoal204Response{}, nil
}
