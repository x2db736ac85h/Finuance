package httpapi

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/x2db736ac85h/finuance/backend/internal/admin"
	"github.com/x2db736ac85h/finuance/backend/internal/httpapi/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/insights"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/money"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
	"github.com/x2db736ac85h/finuance/backend/internal/sharing"
	"github.com/x2db736ac85h/finuance/backend/internal/splits"
)

// Shared groups (Phase 19, ADR 0028). The routes answer 404 when the instance switched shared
// groups off (session gate, featureRoutes), in demo mode and on a bare server. Audit events name
// the group, never amounts, payees or notes.

func (a *api) sharingOn() error {
	if a.sharing == nil || a.cfg.DemoMode {
		return fmt.Errorf("shared groups: %w", platform.ErrNotFound)
	}
	return nil
}

func sharedMemberDTO(m sharing.Member) gen.SharedMember {
	return gen.SharedMember{Id: m.ID, DisplayName: m.DisplayName, Role: gen.SharedMemberRole(m.Role),
		State: gen.SharedMemberState(m.State), IsMe: m.IsMe, Former: m.Former}
}

func sharedGroupDTO(g sharing.Group) gen.SharedGroup {
	return gen.SharedGroup{Id: g.ID, Name: g.Name, Color: g.Color, Currency: g.Currency, Kind: gen.SharedGroupKind(g.Kind),
		StartsOn: dateDTO(g.StartsOn), EndsOn: dateDTO(g.EndsOn), BudgetMinor: g.BudgetMinor, Archived: g.Archived,
		CreatedAt: g.CreatedAt, Me: sharedMemberDTO(g.Me), LinkedGroupId: g.LinkedGroupID}
}

func sharesDTO(s []splits.Share) []gen.SharedShare {
	out := make([]gen.SharedShare, len(s))
	for i, x := range s {
		out[i] = gen.SharedShare{MemberId: x.Member, ShareMinor: x.Minor}
		if x.Weight > 0 {
			w := x.Weight
			out[i].Weight = &w
		}
	}
	return out
}

func sharedExpenseDTO(e *sharing.Expense) gen.SharedExpense {
	return gen.SharedExpense{Id: e.ID, AddedBy: e.AddedBy, PaidBy: e.PaidBy, BookedOn: openapi_types.Date{Time: e.BookedOn.Time()},
		AmountMinor: e.AmountMinor, Payee: e.Payee, Category: e.Category, Note: e.Note, Origin: gen.SharedExpenseOrigin(e.Origin),
		SplitMode: gen.SharedExpenseSplitMode(e.SplitMode), Shares: sharesDTO(e.Shares), Mine: e.Mine, SourceMissing: e.SourceMissing}
}

func sharedDetailDTO(d *sharing.Detail) gen.SharedGroupDetail {
	out := gen.SharedGroupDetail{Group: sharedGroupDTO(d.Group), Members: []gen.SharedMember{}, Expenses: []gen.SharedExpense{},
		Settlements: []gen.SharedSettlement{}, Balances: []gen.SharedBalance{}, Transfers: []gen.SharedTransfer{}}
	for _, m := range d.Members {
		out.Members = append(out.Members, sharedMemberDTO(m))
		if b, ok := d.Balances[m.ID]; ok {
			out.Balances = append(out.Balances, gen.SharedBalance{MemberId: m.ID, BalanceMinor: b})
		}
	}
	for i := range d.Expenses {
		out.Expenses = append(out.Expenses, sharedExpenseDTO(&d.Expenses[i]))
	}
	for _, s := range d.Settlements {
		out.Settlements = append(out.Settlements, gen.SharedSettlement{Id: s.ID, From: s.From, To: s.To, AmountMinor: s.AmountMinor,
			SettledOn: openapi_types.Date{Time: s.SettledOn.Time()}, Note: s.Note, Mine: s.Mine})
	}
	for _, t := range d.Transfers {
		out.Transfers = append(out.Transfers, gen.SharedTransfer{From: t.From, To: t.To, AmountMinor: t.Amount})
	}
	return out
}

func sharedGroupIn(b gen.SharedGroupInput) sharing.GroupInput {
	in := sharing.GroupInput{Name: b.Name, Color: deref(b.Color), Currency: deref(b.Currency), StartsOn: dateIn(b.StartsOn),
		EndsOn: dateIn(b.EndsOn), BudgetMinor: b.BudgetMinor, Archived: deref(b.Archived), FromGroupID: b.FromGroupId}
	if b.Kind != nil {
		in.Kind = string(*b.Kind)
	}
	return in
}

func sharedExpenseIn(b gen.SharedExpenseInput) sharing.ExpenseInput {
	in := sharing.ExpenseInput{AmountMinor: deref(b.AmountMinor), Payee: deref(b.Payee), Category: deref(b.Category),
		Note: deref(b.Note), PaidBy: b.PaidBy}
	if d := dateIn(b.BookedOn); d != nil {
		in.BookedOn = *d
	}
	if sp := b.Split; sp != nil {
		if sp.Mode != nil {
			in.Split.Mode = string(*sp.Mode)
		}
		in.Split.Members, in.Split.Weights = deref(sp.Members), deref(sp.Weights)
		for _, e := range deref(sp.Exact) {
			in.Split.Exact = append(in.Split.Exact, splits.Share{Member: e.MemberId, Minor: e.ShareMinor})
		}
	}
	return in
}

// ListSharedGroups returns the user's shared groups and invitations.
func (a *api) ListSharedGroups(ctx context.Context, _ gen.ListSharedGroupsRequestObject) (gen.ListSharedGroupsResponseObject, error) {
	if err := a.sharingOn(); err != nil {
		return nil, err
	}
	list, err := a.sharing.List(ctx, identity(ctx).User.ID)
	if err != nil {
		return nil, err
	}
	out := make(gen.ListSharedGroups200JSONResponse, len(list))
	for i := range list {
		out[i] = sharedGroupDTO(list[i])
	}
	return out, nil
}

// CreateSharedGroup creates a group, optionally from a personal group.
func (a *api) CreateSharedGroup(ctx context.Context, req gen.CreateSharedGroupRequestObject) (gen.CreateSharedGroupResponseObject, error) {
	if err := a.sharingOn(); err != nil {
		return nil, err
	}
	if err := a.checkCount(ctx, admin.LimitSharedGroups); err != nil {
		return nil, err
	}
	u := identity(ctx).User.ID
	g, err := a.sharing.Create(ctx, u, sharedGroupIn(*req.Body))
	if err != nil {
		return nil, err
	}
	a.audit(ctx, u, ledger.AuditSharedCreated, g.ID.String(), map[string]any{"group": g.Name})
	return gen.CreateSharedGroup201JSONResponse(sharedGroupDTO(g)), nil
}

// GetSharedGroup returns a group with its members, expenses, settlements and balances.
func (a *api) GetSharedGroup(ctx context.Context, req gen.GetSharedGroupRequestObject) (gen.GetSharedGroupResponseObject, error) {
	if err := a.sharingOn(); err != nil {
		return nil, err
	}
	d, err := a.sharing.Get(ctx, identity(ctx).User.ID, req.Id)
	if err != nil {
		return nil, err
	}
	return gen.GetSharedGroup200JSONResponse(sharedDetailDTO(&d)), nil
}

// UpdateSharedGroup changes or archives a group (owner).
func (a *api) UpdateSharedGroup(ctx context.Context, req gen.UpdateSharedGroupRequestObject) (gen.UpdateSharedGroupResponseObject, error) {
	if err := a.sharingOn(); err != nil {
		return nil, err
	}
	u := identity(ctx).User.ID
	g, err := a.sharing.Update(ctx, u, req.Id, sharedGroupIn(*req.Body))
	if err != nil {
		return nil, err
	}
	if g.Archived {
		a.audit(ctx, u, ledger.AuditSharedArchived, g.ID.String(), map[string]any{"group": g.Name})
	}
	return gen.UpdateSharedGroup200JSONResponse(sharedGroupDTO(g)), nil
}

// DeleteSharedGroup deletes a group nobody else joined (owner).
func (a *api) DeleteSharedGroup(ctx context.Context, req gen.DeleteSharedGroupRequestObject) (gen.DeleteSharedGroupResponseObject, error) {
	if err := a.sharingOn(); err != nil {
		return nil, err
	}
	u := identity(ctx).User.ID
	if err := a.sharing.Delete(ctx, u, req.Id); err != nil {
		return nil, err
	}
	a.audit(ctx, u, ledger.AuditSharedDeleted, req.Id.String(), nil)
	return gen.DeleteSharedGroup204Response{}, nil
}

// InviteToSharedGroup invites a friend (owner).
func (a *api) InviteToSharedGroup(ctx context.Context, req gen.InviteToSharedGroupRequestObject) (gen.InviteToSharedGroupResponseObject, error) {
	if err := a.sharingOn(); err != nil {
		return nil, err
	}
	u := identity(ctx).User.ID
	m, err := a.sharing.Invite(ctx, u, req.Id, req.Body.FriendId)
	if err != nil {
		return nil, err
	}
	a.audit(ctx, u, ledger.AuditSharedInvited, req.Id.String(), map[string]any{"name": m.DisplayName})
	return gen.InviteToSharedGroup201JSONResponse(sharedMemberDTO(m)), nil
}

// AcceptSharedGroup joins a group the user was invited to.
func (a *api) AcceptSharedGroup(ctx context.Context, req gen.AcceptSharedGroupRequestObject) (gen.AcceptSharedGroupResponseObject, error) {
	if err := a.sharingOn(); err != nil {
		return nil, err
	}
	u := identity(ctx).User.ID
	g, err := a.sharing.Accept(ctx, u, req.Id)
	if err != nil {
		return nil, err
	}
	a.audit(ctx, u, ledger.AuditSharedJoined, g.ID.String(), map[string]any{"group": g.Name})
	d, err := a.sharing.Get(ctx, u, req.Id)
	if err != nil {
		return nil, err
	}
	return gen.AcceptSharedGroup200JSONResponse(sharedGroupDTO(d.Group)), nil
}

// DeclineSharedGroup turns an invitation down.
func (a *api) DeclineSharedGroup(ctx context.Context, req gen.DeclineSharedGroupRequestObject) (gen.DeclineSharedGroupResponseObject, error) {
	if err := a.sharingOn(); err != nil {
		return nil, err
	}
	u := identity(ctx).User.ID
	if err := a.sharing.Decline(ctx, u, req.Id); err != nil {
		return nil, err
	}
	a.audit(ctx, u, ledger.AuditSharedDeclined, req.Id.String(), nil)
	return gen.DeclineSharedGroup204Response{}, nil
}

// LeaveSharedGroup leaves a group with a zero balance.
func (a *api) LeaveSharedGroup(ctx context.Context, req gen.LeaveSharedGroupRequestObject) (gen.LeaveSharedGroupResponseObject, error) {
	if err := a.sharingOn(); err != nil {
		return nil, err
	}
	u := identity(ctx).User.ID
	if err := a.sharing.Leave(ctx, u, req.Id); err != nil {
		return nil, err
	}
	a.audit(ctx, u, ledger.AuditSharedLeft, req.Id.String(), nil)
	return gen.LeaveSharedGroup204Response{}, nil
}

// RemoveSharedMember withdraws an invitation or removes a member (owner).
func (a *api) RemoveSharedMember(ctx context.Context, req gen.RemoveSharedMemberRequestObject) (gen.RemoveSharedMemberResponseObject, error) {
	if err := a.sharingOn(); err != nil {
		return nil, err
	}
	u := identity(ctx).User.ID
	if err := a.sharing.RemoveMember(ctx, u, req.Id, req.MemberId); err != nil {
		return nil, err
	}
	a.audit(ctx, u, ledger.AuditSharedMemberRemoved, req.Id.String(), map[string]any{"member": req.MemberId.String()})
	return gen.RemoveSharedMember204Response{}, nil
}

// AddSharedExpense adds an expense typed in the group.
func (a *api) AddSharedExpense(ctx context.Context, req gen.AddSharedExpenseRequestObject) (gen.AddSharedExpenseResponseObject, error) {
	if err := a.sharingOn(); err != nil {
		return nil, err
	}
	id, err := a.sharing.AddExpense(ctx, identity(ctx).User.ID, req.Id, sharedExpenseIn(*req.Body))
	if err != nil {
		return nil, err
	}
	return gen.AddSharedExpense201JSONResponse{Id: id}, nil
}

// UpdateSharedExpense changes the caller's own expense.
func (a *api) UpdateSharedExpense(ctx context.Context, req gen.UpdateSharedExpenseRequestObject) (gen.UpdateSharedExpenseResponseObject, error) {
	if err := a.sharingOn(); err != nil {
		return nil, err
	}
	if err := a.sharing.UpdateExpense(ctx, identity(ctx).User.ID, req.Id, req.ExpenseId, sharedExpenseIn(*req.Body)); err != nil {
		return nil, err
	}
	return gen.UpdateSharedExpense204Response{}, nil
}

// DeleteSharedExpense deletes the caller's own typed expense.
func (a *api) DeleteSharedExpense(ctx context.Context, req gen.DeleteSharedExpenseRequestObject) (gen.DeleteSharedExpenseResponseObject, error) {
	if err := a.sharingOn(); err != nil {
		return nil, err
	}
	if err := a.sharing.DeleteExpense(ctx, identity(ctx).User.ID, req.Id, req.ExpenseId); err != nil {
		return nil, err
	}
	return gen.DeleteSharedExpense204Response{}, nil
}

// AddSharedSettlement records a payment between two members.
func (a *api) AddSharedSettlement(ctx context.Context, req gen.AddSharedSettlementRequestObject) (gen.AddSharedSettlementResponseObject, error) {
	if err := a.sharingOn(); err != nil {
		return nil, err
	}
	u, b := identity(ctx).User.ID, req.Body
	s, err := a.sharing.Settle(ctx, u, req.Id, sharing.SettlementInput{From: b.From, To: b.To, AmountMinor: b.AmountMinor,
		SettledOn: *dateIn(&b.SettledOn), Note: deref(b.Note)})
	if err != nil {
		return nil, err
	}
	a.audit(ctx, u, ledger.AuditSharedSettled, req.Id.String(), nil)
	return gen.AddSharedSettlement201JSONResponse{Id: s.ID, From: s.From, To: s.To, AmountMinor: s.AmountMinor,
		SettledOn: b.SettledOn, Note: s.Note, Mine: true}, nil
}

// DeleteSharedSettlement deletes a payment the caller recorded.
func (a *api) DeleteSharedSettlement(ctx context.Context, req gen.DeleteSharedSettlementRequestObject) (gen.DeleteSharedSettlementResponseObject, error) {
	if err := a.sharingOn(); err != nil {
		return nil, err
	}
	if err := a.sharing.DeleteSettlement(ctx, identity(ctx).User.ID, req.Id, req.SettlementId); err != nil {
		return nil, err
	}
	return gen.DeleteSharedSettlement204Response{}, nil
}

// SharedGroupReport returns a group's cards.
func (a *api) SharedGroupReport(ctx context.Context, req gen.SharedGroupReportRequestObject) (gen.SharedGroupReportResponseObject, error) {
	if err := a.sharingOn(); err != nil {
		return nil, err
	}
	d, r, err := a.sharing.Report(ctx, identity(ctx).User.ID, req.Id)
	if err != nil {
		return nil, err
	}
	byID := map[uuid.UUID]*sharing.Expense{}
	for i := range d.Expenses {
		byID[d.Expenses[i].ID] = &d.Expenses[i]
	}
	named := func(in []insights.NamedSpend) []gen.NamedSpend {
		out := make([]gen.NamedSpend, len(in))
		for i, n := range in {
			out[i] = gen.NamedSpend{Name: n.Name, SpentMinor: n.Spent, Count: n.Count}
		}
		return out
	}
	out := gen.SharedGroupReport{Currency: d.Group.Currency, TotalMinor: r.Total, Count: r.Count, FirstOn: dateDTO(r.FirstOn),
		LastOn: dateDTO(r.LastOn), Days: r.Days, AvgPerDayMinor: r.AvgPerDay, Daily: []gen.DaySpend{},
		Categories: named(r.Categories), Payees: named(r.Payees), Largest: []gen.SharedExpense{}, Members: []gen.MemberPaidShare{}}
	for _, day := range r.Daily {
		out.Daily = append(out.Daily, gen.DaySpend{Date: openapi_types.Date{Time: day.Date.Time()}, SpentMinor: day.Spent, Count: day.Count})
	}
	for _, id := range r.Largest {
		out.Largest = append(out.Largest, sharedExpenseDTO(byID[id]))
	}
	for _, m := range r.Members {
		out.Members = append(out.Members, gen.MemberPaidShare{MemberId: m.Member, PaidMinor: m.Paid, ShareMinor: m.Share})
	}
	return gen.SharedGroupReport200JSONResponse(out), nil
}

// ExportSharedGroup streams the group's expenses as CSV: one column per member with their share.
func (a *api) ExportSharedGroup(ctx context.Context, req gen.ExportSharedGroupRequestObject) (gen.ExportSharedGroupResponseObject, error) {
	if err := a.sharingOn(); err != nil {
		return nil, err
	}
	u := identity(ctx).User.ID
	d, err := a.sharing.Get(ctx, u, req.Id)
	if err != nil {
		return nil, err
	}
	if d.Group.Me.State != "active" {
		return nil, fmt.Errorf("accept the invitation first: %w", ledger.ErrState)
	}
	exp, _ := money.Exponent(d.Group.Currency)
	amount := func(v int64) string { return strings.Replace(money.FormatMinor(v, exp), ".", ",", 1) }
	names := map[uuid.UUID]string{}
	var members []sharing.Member
	for _, m := range d.Members {
		if m.State != "invited" {
			names[m.ID] = m.DisplayName
			members = append(members, m)
		}
	}
	var b bytes.Buffer
	b.WriteString("\ufeff")
	head := []string{"Date", "Payee", "Category", "Amount", "Currency", "Paid by", "Note"}
	for _, m := range members {
		head = append(head, m.DisplayName)
	}
	b.WriteString(query.CSVLine(head, ";") + "\r\n")
	for i := range d.Expenses {
		e := &d.Expenses[i]
		dd := e.BookedOn
		row := []string{fmt.Sprintf("%02d.%02d.%04d", dd.D, int(dd.M), dd.Y), e.Payee, e.Category, amount(e.AmountMinor),
			d.Group.Currency, names[e.PaidBy], e.Note}
		shares := map[uuid.UUID]int64{}
		for _, s := range e.Shares {
			shares[s.Member] = s.Minor
		}
		for _, m := range members {
			row = append(row, amount(shares[m.ID]))
		}
		b.WriteString(query.CSVLine(row, ";") + "\r\n")
	}
	cd := fmt.Sprintf(`attachment; filename="%s.csv"`, safeFilename(d.Group.Name))
	return gen.ExportSharedGroup200TextcsvResponse{Body: &b, ContentLength: int64(b.Len()),
		Headers: gen.ExportSharedGroup200ResponseHeaders{ContentDisposition: &cd}}, nil
}

// safeFilename keeps letters, digits, spaces, dots and dashes of a name for Content-Disposition.
func safeFilename(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r == '"' || r == '\\' || r == '/' || r < 0x20 || r == 0x7f:
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	if s := strings.TrimSpace(b.String()); s != "" {
		return s
	}
	return "shared-group"
}

// GroupsTimeline lists personal and shared groups for the Shared tab.
func (a *api) GroupsTimeline(ctx context.Context, _ gen.GroupsTimelineRequestObject) (gen.GroupsTimelineResponseObject, error) {
	if err := a.sharingOn(); err != nil {
		return nil, err
	}
	tl, err := a.sharing.Timeline(ctx, identity(ctx).User.ID)
	if err != nil {
		return nil, err
	}
	out := gen.GroupTimeline{Groups: []gen.TimelineGroup{}, Years: []gen.TimelineYear{}, Hints: []gen.TimelineHint{}}
	for i := range tl.Groups {
		g := &tl.Groups[i]
		x := gen.TimelineGroup{Id: g.ID, Source: gen.TimelineGroupSource(g.Source), Name: g.Name, Color: g.Color,
			Kind: gen.TimelineGroupKind(g.Kind), Currency: g.Currency, FirstOn: dateDTO(g.FirstOn), LastOn: dateDTO(g.LastOn),
			Days: g.Days, SpentMinor: g.Spent, Count: g.Count, PerDayMinor: g.PerDay, BudgetMinor: g.BudgetMinor}
		if g.StartsOn != nil && g.EndsOn != nil {
			x.FirstOn, x.LastOn = dateDTO(g.StartsOn), dateDTO(g.EndsOn)
		}
		if g.Top != nil {
			share := g.TopShare
			x.TopCategory, x.TopCategoryShare = &gen.NamedSpend{Name: g.Top.Name, SpentMinor: g.Top.Spent, Count: g.Top.Count}, &share
		}
		out.Groups = append(out.Groups, x)
	}
	for _, y := range tl.Years {
		out.Years = append(out.Years, gen.TimelineYear{Year: y.Year, Groups: y.Groups, SpentMinor: y.Spent, Currency: y.Currency, Days: y.Days})
	}
	for _, h := range tl.Hints {
		x := gen.TimelineHint{GroupId: h.GroupID, Kind: gen.TimelineHintKind(h.Kind), Ratio: h.Ratio}
		if h.Kind == insights.HintCategoryHigh {
			c := h.Category
			x.Category = &c
		}
		out.Hints = append(out.Hints, x)
	}
	return gen.GroupsTimeline200JSONResponse(out), nil
}
