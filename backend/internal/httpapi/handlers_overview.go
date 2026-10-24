package httpapi

import (
	"context"
	"errors"

	"github.com/x2db736ac85h/finuance/backend/internal/httpapi/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/insights"
	"github.com/x2db736ac85h/finuance/backend/internal/investments"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
)

// Overview page (PLAN tasks 12.1, 12.2, 12.6).

// Overview returns everything the overview page shows for one month.
func (a *api) Overview(ctx context.Context, req gen.OverviewRequestObject) (gen.OverviewResponseObject, error) {
	b := req.Body
	var month *platform.Date
	if b.Month != nil {
		m, err := platform.ParseDate(*b.Month + "-01")
		if err != nil {
			ve := &platform.ValidationError{}
			ve.Add("month", "must be YYYY-MM")
			return nil, ve
		}
		month = &m
	}
	user := identity(ctx).User.ID
	inv, err := a.ledger.Investments(ctx, user)
	if err != nil {
		return nil, err
	}
	o, err := a.insights.Overview(ctx, user, contextIn(b.Context), month, int(b.Months), inv)
	if err != nil {
		return nil, err
	}
	out := gen.Overview{Currency: o.Currency}
	if !o.Empty {
		d := overviewDTO(&o)
		out.Data = &d
	}
	return gen.Overview200JSONResponse(out), nil
}

func monthTotalsDTO(m query.MonthFlow) gen.MonthTotals {
	return gen.MonthTotals{Month: monthDTO(m.Month), IncomeMinor: m.IncomeMinor, ExpenseMinor: m.ExpenseMinor, Count: m.Count}
}

func overviewDTO(o *insights.Overview) gen.OverviewData {
	d := gen.OverviewData{Month: monthDTO(o.Month), PrevMonth: monthDTO(o.Prev), FirstMonth: monthDTO(o.FirstMonth),
		LastMonth: monthDTO(o.LastMonth), DataMax: *dateDTO(&o.DataMax), Partial: o.Partial, ElapsedDays: o.Elapsed,
		DaysInMonth: o.DaysInMonth, Current: monthTotalsDTO(o.Cur), Previous: monthTotalsDTO(o.PrevFlow),
		PrevToDateMinor: o.PrevToDateMinor, ProjectedMinor: o.ProjectedMinor, AvgMonths: o.AvgMonths,
		Months: make([]gen.MonthTotals, len(o.Months)), Categories: make([]gen.BudgetRow, len(o.Categories)),
		IncomeSources: make([]gen.PayeeSpend, len(o.IncomeSources)), ColorSlots: make([]gen.ColorSlot, len(o.ColorSlots)),
		Highlights: make([]gen.Highlight, len(o.Highlights)), Recurring: make([]gen.RecurringPayment, len(o.Recurring)),
		Largest: make([]gen.OverviewExpense, len(o.Largest)), Weekday: make([]gen.WeekdaySpend, len(o.Weekday)),
		Balance: gen.BalanceSeries{OpeningMinor: o.Balance.OpeningMinor, NowMinor: o.Balance.NowMinor,
			Points: make([]gen.BalancePoint, len(o.Balance.Points))},
		Investment: investmentSummaryDTO(o.Investment), YieldOnCost: yieldDTO(o.YieldOnCost), Fire: fireDTO(o.Fire)}
	for i, m := range o.Months {
		d.Months[i] = monthTotalsDTO(m)
	}
	for i, c := range o.Categories {
		d.Categories[i] = gen.BudgetRow{Name: c.Name, CurMinor: c.CurMinor, PrevMinor: c.PrevMinor, Avg3Minor: c.Avg3Minor,
			BudgetMinor: c.BudgetMinor, BudgetIsDefault: c.BudgetIsDefault, Status: gen.BudgetRowStatus(c.Status)}
		if c.ID != nil {
			d.Categories[i].Category = &gen.NamedRef{Id: *c.ID, Name: c.Name}
		}
	}
	for i, p := range o.IncomeSources {
		d.IncomeSources[i] = gen.PayeeSpend{Payee: p.Payee, SpentMinor: p.SpentMinor, Count: p.Count}
	}
	for i, s := range o.ColorSlots {
		d.ColorSlots[i] = gen.ColorSlot{Slot: s.Slot}
		if s.ID != nil && s.Name != nil {
			d.ColorSlots[i].Category = &gen.NamedRef{Id: *s.ID, Name: *s.Name}
		}
	}
	for i := range o.Highlights {
		h := &o.Highlights[i]
		d.Highlights[i] = gen.Highlight{Kind: gen.HighlightKind(h.Kind), Tone: gen.HighlightTone(h.Tone), AmountMinor: h.Amount,
			OtherMinor: h.Other, Pct: h.Pct, AvgPct: h.AvgPct, Days: h.Days, Of: h.Of, Months: h.Months, Count: h.Count,
			Category: h.Category, Payee: h.Payee, SameDays: h.SameDays}
		if !h.Month.IsZero() {
			m := monthDTO(h.Month)
			d.Highlights[i].Month = &m
		}
	}
	for i := range o.Recurring {
		r := &o.Recurring[i]
		d.Recurring[i] = gen.RecurringPayment{Payee: r.Payee, MedianMinor: r.MedianMinor, Last: *dateDTO(&r.Last), Next: *dateDTO(&r.Next),
			Active: r.Active, YearlyMinor: r.YearlyMinor, Count: r.Count, Cadence: gen.RecurringPaymentCadence(r.Cadence)}
		if r.Category != "" {
			d.Recurring[i].Category = ptr(r.Category)
		}
	}
	for i, x := range o.Largest {
		d.Largest[i] = gen.OverviewExpense{Id: x.ID, BookedOn: *dateDTO(&x.BookedOn), Payee: x.Payee, PayeeGroup: x.PayeeGroup,
			AmountMinor: x.AmountMinor}
		if x.Category != nil {
			d.Largest[i].Category = &gen.NamedRef{Id: x.Category.ID, Name: x.Category.Name}
		}
	}
	for i, w := range o.Weekday {
		d.Weekday[i] = gen.WeekdaySpend{TotalMinor: w.TotalMinor, Days: w.Days}
	}
	for i, p := range o.Balance.Points {
		d.Balance.Points[i] = gen.BalancePoint{Date: *dateDTO(&p.Date), BalanceMinor: p.BalanceMinor}
	}
	return d
}

func investmentSummaryDTO(s investments.Summary) gen.InvestmentSummary {
	out := gen.InvestmentSummary{Rows: make([]gen.InvestmentRow, len(s.Rows)), InvestedMinor: s.Invested, ValueMinor: s.Value,
		IncomeMinor: s.Income, PpkPaidInMinor: s.PPKPaidIn, PpkGainMinor: s.PPKGain, PpkReturnOnOwn: s.PPKReturnOnOwn,
		GainPercent: s.GainPercent, Count: s.Count}
	for i, r := range s.Rows {
		out.Rows[i] = gen.InvestmentRow{Id: r.ID, Name: r.Name, Type: r.Type, Ppk: r.PPK, InvestedMinor: r.Invested, ValueMinor: r.Value,
			IncomeMinor: r.Income, GainPercent: r.GainPercent, AllocationShare: s.AllocationShares[i]}
	}
	return out
}

func yieldDTO(y investments.Yield) gen.YieldOnCost {
	out := gen.YieldOnCost{Positions: make([]gen.YieldRow, len(y.Positions)), InvestedMinor: y.Invested, IncomeMinor: y.Income,
		DetectedMinor: y.DetectedMinor, UsesDetected: y.UsesDetected, Total: y.Total}
	for i, p := range y.Positions {
		out.Positions[i] = gen.YieldRow{Id: p.ID, Name: p.Name, InvestedMinor: p.Invested, IncomeMinor: p.Income, Yield: p.Yield}
	}
	return out
}

func fireDTO(f investments.FIRE) gen.Fire {
	return gen.Fire{Months: f.Months, AnnualExpensesMinor: f.AnnualExpenses, YearlySavingsMinor: f.YearlySavings,
		FiNumberMinor: f.FINumber, AssetsMinor: f.Assets, SwrBp: f.SwrBp, RealReturnBp: f.RealReturnBp, Progress: f.Progress,
		Years: f.Years, Reachable: f.Reachable}
}

// ListBudgets returns the budgets set by hand.
func (a *api) ListBudgets(ctx context.Context, _ gen.ListBudgetsRequestObject) (gen.ListBudgetsResponseObject, error) {
	bs, err := a.ledger.ListBudgets(ctx, identity(ctx).User.ID)
	if err != nil {
		return nil, err
	}
	out := gen.BudgetList{Budgets: make([]gen.Budget, len(bs))}
	for i, b := range bs {
		out.Budgets[i] = gen.Budget{Category: gen.NamedRef{Id: b.CategoryID, Name: b.CategoryName}, AmountMinor: b.AmountMinor}
	}
	return gen.ListBudgets200JSONResponse(out), nil
}

// PutBudget sets a category's budget, or removes it with null.
func (a *api) PutBudget(ctx context.Context, req gen.PutBudgetRequestObject) (gen.PutBudgetResponseObject, error) {
	user := identity(ctx).User.ID
	var err error
	if v := req.Body.AmountMinor; v != nil {
		err = a.ledger.SetBudget(ctx, user, req.CategoryId, *v)
	} else {
		err = a.ledger.ClearBudget(ctx, user, req.CategoryId)
	}
	if err != nil {
		return nil, err
	}
	return gen.PutBudget204Response{}, nil
}

func investmentsDTO(d investments.Doc) gen.Investments {
	out := gen.Investments{Version: d.Version, SwrBp: d.SwrBp, RealReturnBp: d.RealReturnBp,
		Ppk: gen.PPK{EmployeeMinor: d.PPK.EmployeeMinor, EmployerMinor: d.PPK.EmployerMinor, StateMinor: d.PPK.StateMinor,
			ValueMinor: d.PPK.ValueMinor}, Positions: make([]gen.InvestmentPosition, len(d.Positions))}
	for i, p := range d.Positions {
		out.Positions[i] = gen.InvestmentPosition{Id: p.ID, Name: p.Name, Type: p.Type, InvestedMinor: p.InvestedMinor,
			ValueMinor: p.ValueMinor, AnnualIncomeMinor: p.AnnualIncomeMinor}
	}
	return out
}

func investmentsIn(b gen.Investments) investments.Doc {
	d := investments.Doc{Version: b.Version, SwrBp: b.SwrBp, RealReturnBp: b.RealReturnBp,
		PPK: investments.PPK{EmployeeMinor: b.Ppk.EmployeeMinor, EmployerMinor: b.Ppk.EmployerMinor, StateMinor: b.Ppk.StateMinor,
			ValueMinor: b.Ppk.ValueMinor}}
	for _, p := range b.Positions {
		d.Positions = append(d.Positions, investments.Position{ID: p.Id, Name: p.Name, Type: p.Type, InvestedMinor: p.InvestedMinor,
			ValueMinor: p.ValueMinor, AnnualIncomeMinor: p.AnnualIncomeMinor})
	}
	return d
}

// GetInvestments returns the investment document.
func (a *api) GetInvestments(ctx context.Context, _ gen.GetInvestmentsRequestObject) (gen.GetInvestmentsResponseObject, error) {
	d, err := a.ledger.Investments(ctx, identity(ctx).User.ID)
	if err != nil {
		return nil, err
	}
	return gen.GetInvestments200JSONResponse(investmentsDTO(d)), nil
}

// PutInvestments replaces the investment document if the client's version is current; otherwise
// it answers 409 stale_version with the current document.
func (a *api) PutInvestments(ctx context.Context, req gen.PutInvestmentsRequestObject) (gen.PutInvestmentsResponseObject, error) {
	d, err := a.ledger.SaveInvestments(ctx, identity(ctx).User.ID, investmentsIn(*req.Body))
	var stale *ledger.StaleInvestmentsError
	if errors.As(err, &stale) {
		p := ProblemFor(err, nil, a.prod)
		return gen.PutInvestments409ApplicationProblemPlusJSONResponse{Type: p.Type, Title: p.Title, Status: p.Status,
			Code: p.Code, Detail: p.Detail, RequestId: requestID(ctx), Instance: ptr("/api/v1/investments"),
			Current: investmentsDTO(stale.Current)}, nil
	}
	if err != nil {
		return nil, err
	}
	return gen.PutInvestments200JSONResponse(investmentsDTO(d)), nil
}
