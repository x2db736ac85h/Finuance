package httpapi

import (
	"context"

	"github.com/x2db736ac85h/finuance/backend/internal/httpapi/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/insights"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
)

// Insight charts (PLAN tasks 11.1–11.2, §4.6).

// InsightSummary returns the summary card.
func (a *api) InsightSummary(ctx context.Context, req gen.InsightSummaryRequestObject) (gen.InsightSummaryResponseObject, error) {
	s, err := a.insights.Summary(ctx, identity(ctx).User.ID, contextIn(req.Body.Context))
	if err != nil {
		return nil, err
	}
	out := gen.InsightSummary{Period: periodDTO(s.Period), Currency: s.Currency, Count: s.Count, SpentMinor: s.SpentMinor,
		IncomeMinor: s.IncomeMinor, NetMinor: s.NetMinor, SavingsRate: s.SavingsRate, Days: s.Days, AvgPerDayMinor: s.AvgPerDayMinor,
		PrevSpentMinor: s.PrevSpentMinor, SpentDelta: s.SpentDelta}
	if x := s.Largest; x != nil {
		out.LargestExpense = &gen.LargestExpense{Id: x.ID, BookedOn: *dateDTO(&x.BookedOn), AmountMinor: x.AmountMinor, Payee: x.Payee}
	}
	return gen.InsightSummary200JSONResponse(out), nil
}

// InsightCategories returns spending by category.
func (a *api) InsightCategories(ctx context.Context, req gen.InsightCategoriesRequestObject) (gen.InsightCategoriesResponseObject, error) {
	c, err := a.insights.Categories(ctx, identity(ctx).User.ID, contextIn(req.Body.Context))
	if err != nil {
		return nil, err
	}
	out := gen.InsightCategories{Currency: c.Currency, TotalMinor: c.TotalMinor, Categories: categorySpendDTO(c.Categories)}
	return gen.InsightCategories200JSONResponse(out), nil
}

func categorySpendDTO(cats []insights.CategoryShare) []gen.CategorySpend {
	out := make([]gen.CategorySpend, len(cats))
	for i, x := range cats {
		out[i] = gen.CategorySpend{SpentMinor: x.SpentMinor, Share: x.Share, ColorSlot: x.ColorSlot}
		if x.ID != nil && x.Name != nil {
			out[i].Category = &gen.NamedRef{Id: *x.ID, Name: *x.Name}
		}
	}
	return out
}

// InsightCashflow returns money in and out per month.
func (a *api) InsightCashflow(ctx context.Context, req gen.InsightCashflowRequestObject) (gen.InsightCashflowResponseObject, error) {
	c, err := a.insights.Cashflow(ctx, identity(ctx).User.ID, contextIn(req.Body.Context))
	if err != nil {
		return nil, err
	}
	out := gen.InsightCashflow{Currency: c.Currency, Period: periodDTO(c.Period), Months: make([]gen.MonthFlow, len(c.Months))}
	for i, m := range c.Months {
		out.Months[i] = gen.MonthFlow{Month: monthDTO(m.Month), IncomeMinor: m.IncomeMinor, ExpenseMinor: m.ExpenseMinor, Count: m.Count,
			InPeriod: m.InPeriod}
	}
	return gen.InsightCashflow200JSONResponse(out), nil
}

// InsightPace returns the month pace.
func (a *api) InsightPace(ctx context.Context, req gen.InsightPaceRequestObject) (gen.InsightPaceResponseObject, error) {
	p, err := a.insights.Pace(ctx, identity(ctx).User.ID, contextIn(req.Body.Context), string(req.Body.Compare))
	if err != nil {
		return nil, err
	}
	out := gen.InsightPace{Currency: p.Currency, Current: p.Current}
	if out.Current == nil {
		out.Current = []int64{}
	}
	if p.Target != nil {
		t, dim, last := monthDTO(*p.Target), p.DaysInMonth, p.LastDay
		out.Target, out.DaysInMonth, out.LastDay = &t, &dim, &last
	}
	if c := p.Compare; c != nil {
		out.Compare = &gen.PaceCompare{Mode: gen.PaceCompareMode(c.Mode), Days: c.Days, Values: c.Values}
		if c.Month != nil {
			m := monthDTO(*c.Month)
			out.Compare.Month = &m
		}
		if c.Months > 0 {
			n := c.Months
			out.Compare.Months = &n
		}
	}
	return gen.InsightPace200JSONResponse(out), nil
}

// InsightPayees returns the top payees.
func (a *api) InsightPayees(ctx context.Context, req gen.InsightPayeesRequestObject) (gen.InsightPayeesResponseObject, error) {
	b := req.Body
	p, err := a.insights.Payees(ctx, identity(ctx).User.ID, contextIn(b.Context), int(b.Top), string(b.Scope))
	if err != nil {
		return nil, err
	}
	out := gen.InsightPayees{Currency: p.Currency, Total: p.Total, Payees: payeeSpendDTO(p.Payees)}
	return gen.InsightPayees200JSONResponse(out), nil
}

func payeeSpendDTO(ps []query.PayeeSpend) []gen.PayeeSpend {
	out := make([]gen.PayeeSpend, len(ps))
	for i, x := range ps {
		out[i] = gen.PayeeSpend{Payee: x.Payee, SpentMinor: x.SpentMinor, Count: x.Count}
	}
	return out
}

// InsightDaily returns the spending per day for the calendar.
func (a *api) InsightDaily(ctx context.Context, req gen.InsightDailyRequestObject) (gen.InsightDailyResponseObject, error) {
	d, err := a.insights.Daily(ctx, identity(ctx).User.ID, contextIn(req.Body.Context), string(req.Body.Range))
	if err != nil {
		return nil, err
	}
	out := gen.InsightDaily{Currency: d.Currency, From: dateDTO(d.From), To: dateDTO(d.To), Days: daySpendDTO(d.Days)}
	return gen.InsightDaily200JSONResponse(out), nil
}

func daySpendDTO(ds []query.DaySpend) []gen.DaySpend {
	out := make([]gen.DaySpend, len(ds))
	for i, x := range ds {
		out[i] = gen.DaySpend{Date: *dateDTO(&x.Date), SpentMinor: x.SpentMinor, Count: x.Count}
	}
	return out
}

func periodDTO(r *query.Resolved) *gen.ResolvedPeriod {
	if r == nil {
		return nil
	}
	return &gen.ResolvedPeriod{Kind: gen.ResolvedPeriodKind(r.Kind), From: *dateDTO(&r.From), To: *dateDTO(&r.To), Label: r.Label}
}

// monthDTO is a month as "YYYY-MM".
func monthDTO(d platform.Date) string { return d.String()[:7] }
