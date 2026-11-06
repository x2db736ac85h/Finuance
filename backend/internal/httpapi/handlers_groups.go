package httpapi

import (
	"context"

	"github.com/x2db736ac85h/finuance/backend/internal/admin"
	"github.com/x2db736ac85h/finuance/backend/internal/httpapi/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
)

// Groups and bulk labelling (PLAN task 10.10, §4.7).

func groupDTO(g ledger.GroupStat) gen.Group {
	return gen.Group{Id: g.ID, Name: g.Name, Color: g.Color, Count: g.Count,
		Spent:   gen.Money{AmountMinor: g.SpentMinor, Currency: g.Currency},
		Income:  gen.Money{AmountMinor: g.IncomeMinor, Currency: g.Currency},
		FirstOn: dateDTO(g.FirstOn), LastOn: dateDTO(g.LastOn), BudgetMinor: g.BudgetMinor, SharedGroupId: g.SharedGroupID}
}

// budgetIn reads GroupInput.budgetMinor: omitted keeps the budget, 0 removes it.
func budgetIn(b *int64) int64 {
	if b == nil {
		return ledger.KeepBudget
	}
	return *b
}

// ListGroups returns every group with its stats.
func (a *api) ListGroups(ctx context.Context, _ gen.ListGroupsRequestObject) (gen.ListGroupsResponseObject, error) {
	groups, err := a.ledger.GroupStats(ctx, identity(ctx).User.ID)
	if err != nil {
		return nil, err
	}
	out := make(gen.ListGroups200JSONResponse, len(groups))
	for i := range groups {
		out[i] = groupDTO(groups[i])
	}
	return out, nil
}

// CreateGroup adds a group; without a colour it takes the first unused slot.
func (a *api) CreateGroup(ctx context.Context, req gen.CreateGroupRequestObject) (gen.CreateGroupResponseObject, error) {
	userID := identity(ctx).User.ID
	if err := a.checkCount(ctx, admin.LimitGroups); err != nil {
		return nil, err
	}
	ctx, err := a.aiOrigin(ctx, req.Params.XAssistantChat, req.Params.XAssistantProposal, req.Params.XAssistantAuto)
	if err != nil {
		return nil, err
	}
	color := -1
	if req.Body.Color != nil {
		color = *req.Body.Color
	}
	g, err := a.ledger.CreateGroupWith(ctx, userID, req.Body.Name, color, budgetIn(req.Body.BudgetMinor))
	if err != nil {
		return nil, err
	}
	st, err := a.ledger.GroupStatOf(ctx, userID, g.ID)
	if err != nil {
		return nil, err
	}
	return gen.CreateGroup201JSONResponse(groupDTO(st)), nil
}

// UpdateGroup renames, recolours and/or budgets a group.
func (a *api) UpdateGroup(ctx context.Context, req gen.UpdateGroupRequestObject) (gen.UpdateGroupResponseObject, error) {
	userID := identity(ctx).User.ID
	color := -1
	if req.Body.Color != nil {
		color = *req.Body.Color
	}
	if _, err := a.ledger.EditGroup(ctx, userID, req.Id, req.Body.Name, color, budgetIn(req.Body.BudgetMinor)); err != nil {
		return nil, err
	}
	st, err := a.ledger.GroupStatOf(ctx, userID, req.Id)
	if err != nil {
		return nil, err
	}
	return gen.UpdateGroup200JSONResponse(groupDTO(st)), nil
}

// GroupReport breaks a group down for the group window (PLAN 15.9).
func (a *api) GroupReport(ctx context.Context, req gen.GroupReportRequestObject) (gen.GroupReportResponseObject, error) {
	userID := identity(ctx).User.ID
	st, err := a.ledger.GroupStatOf(ctx, userID, req.Id)
	if err != nil {
		return nil, err
	}
	r, err := a.insights.GroupReport(ctx, userID, req.Id)
	if err != nil {
		return nil, err
	}
	return gen.GroupReport200JSONResponse{Group: groupDTO(st), Currency: st.Currency, Days: r.Days, AvgPerDayMinor: r.AvgPerDayMinor,
		Daily: daySpendDTO(r.Daily), Categories: categorySpendDTO(r.Categories), Payees: payeeSpendDTO(r.Payees), PayeeCount: r.PayeeCount}, nil
}

// DeleteGroup removes a group and its labels, never transactions.
func (a *api) DeleteGroup(ctx context.Context, req gen.DeleteGroupRequestObject) (gen.DeleteGroupResponseObject, error) {
	if err := a.ledger.DeleteGroup(ctx, identity(ctx).User.ID, req.Id); err != nil {
		return nil, err
	}
	return gen.DeleteGroup204Response{}, nil
}

// BulkUpdateTransactions applies one action to many transactions (annotations included): the
// group, a manual category or back to the bank's, or the transfer decision.
func (a *api) BulkUpdateTransactions(ctx context.Context, req gen.BulkUpdateTransactionsRequestObject) (gen.BulkUpdateTransactionsResponseObject, error) {
	u, b := identity(ctx).User.ID, req.Body
	actions := 0
	for _, set := range []bool{b.Set != nil, b.Category != nil, b.Transfer != nil} {
		if set {
			actions++
		}
	}
	if actions != 1 {
		return nil, validation("set", "send exactly one of set, category and transfer")
	}
	ctx, err := a.aiOrigin(ctx, req.Params.XAssistantChat, req.Params.XAssistantProposal, req.Params.XAssistantAuto)
	if err != nil {
		return nil, err
	}
	var n int
	switch {
	case b.Set != nil:
		n, err = a.ledger.Annotate(ctx, u, b.Ids, ledger.AnnotationPatch{SetGroup: true, GroupID: b.Set.GroupId})
	case b.Category != nil:
		if (b.Category.Id != nil) == deref(b.Category.Reset) {
			return nil, validation("category", "send either id or reset: true")
		}
		n, err = a.ledger.Annotate(ctx, u, b.Ids, ledger.AnnotationPatch{SetCategory: true, CategoryID: b.Category.Id})
	default:
		var v *bool
		if *b.Transfer != gen.BulkUpdateTransferAuto {
			yes := *b.Transfer == gen.BulkUpdateTransferYes
			v = &yes
		}
		if n, err = a.ledger.SetTransfer(ctx, u, b.Ids, v); err == nil && v == nil {
			a.reclassifyAfter(ctx, u)
		}
	}
	if err != nil {
		return nil, err
	}
	switch {
	case b.Set != nil:
		a.auditAppliedAction(ctx, req.Params.XAssistantChat, req.Params.XAssistantProposal, "set_group", n)
	case b.Category != nil:
		a.auditAppliedAction(ctx, req.Params.XAssistantChat, req.Params.XAssistantProposal, "set_category", n)
	default:
		a.auditAppliedAction(ctx, req.Params.XAssistantChat, req.Params.XAssistantProposal, "set_transfer", n)
	}
	return gen.BulkUpdateTransactions200JSONResponse{Updated: n}, nil
}
