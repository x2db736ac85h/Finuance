package insights

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
)

// Service answers the insight endpoints (PLAN §6.2, POST /insights/*).
type Service struct{ eng *query.Engine }

// New returns a service reading through eng.
func New(eng *query.Engine) *Service { return &Service{eng: eng} }

// Summary is the summary card (legacy CHARTS.sum).
type Summary struct {
	Period         *query.Resolved
	Currency       string
	Count          int64
	SpentMinor     int64
	IncomeMinor    int64
	NetMinor       int64
	SavingsRate    *float64 // nil without income
	Days           int
	AvgPerDayMinor int64
	Largest        *query.Expense
	PrevSpentMinor *int64   // the previous period's spending; nil without a period
	SpentDelta     *float64 // vs the previous period; nil without a period or previous spending
}

// Summary sums the rows shown: spent, income, net and savings rate, the average per day, the
// largest expense, and the spending change against the previous period (read from the base rows,
// so the period itself doesn't hide it).
func (s *Service) Summary(ctx context.Context, userID uuid.UUID, c query.Context) (Summary, error) {
	var out Summary
	err := s.eng.Insight(ctx, userID, c, func(in *query.Insight) error {
		out.Period = in.Period
		var err error
		if out.Currency, err = in.Currency(); err != nil {
			return err
		}
		t, err := in.Totals(true)
		if err != nil {
			return err
		}
		data, err := in.DataRange()
		if err != nil {
			return err
		}
		out.Count, out.SpentMinor, out.IncomeMinor = t.Count, t.SpentMinor, t.IncomeMinor
		out.NetMinor = t.IncomeMinor - t.SpentMinor
		out.SavingsRate = SavingsRate(t.IncomeMinor, out.NetMinor)
		out.Days = Days(in.Period, data, t.Range)
		out.AvgPerDayMinor = DivRound(t.SpentMinor, int64(out.Days))
		if out.Largest, err = in.Largest(); err != nil {
			return err
		}
		if p := in.Period; p != nil {
			from, to := p.Prev()
			prev, err := in.SpentBetween(from, to)
			if err != nil {
				return err
			}
			out.PrevSpentMinor = &prev
			out.SpentDelta = Delta(t.SpentMinor, prev)
		}
		return nil
	})
	return out, err
}

// CategoryShare is one category's spending and share of the total (legacy CHARTS.cat).
type CategoryShare struct {
	query.CategorySpend
	Share float64
}

// Categories is the spending-by-category card.
type Categories struct {
	Currency   string
	TotalMinor int64
	Categories []CategoryShare // largest first
}

// Categories lists the expenses of the rows shown per category with shares and colour slots. The
// client draws the top 7 + Other as a donut, or all of them as bars.
func (s *Service) Categories(ctx context.Context, userID uuid.UUID, c query.Context) (Categories, error) {
	var out Categories
	err := s.eng.Insight(ctx, userID, c, func(in *query.Insight) error {
		var err error
		if out.Currency, err = in.Currency(); err != nil {
			return err
		}
		cats, err := in.CategorySpend()
		if err != nil {
			return err
		}
		for _, x := range cats {
			out.TotalMinor += x.SpentMinor
		}
		out.Categories = make([]CategoryShare, len(cats))
		for i, x := range cats {
			out.Categories[i] = CategoryShare{CategorySpend: x, Share: Share(x.SpentMinor, out.TotalMinor)}
		}
		return nil
	})
	return out, err
}

// FlowMonth is one month of the cash-flow card.
type FlowMonth struct {
	query.MonthFlow
	InPeriod bool
}

// Cashflow is the cash-flow-by-month card (legacy CHARTS.flow).
type Cashflow struct {
	Currency string
	Period   *query.Resolved
	Months   []FlowMonth // oldest first, every month from the first to the last with data
}

// Cashflow aggregates the base rows (filters, no period: the card shows the whole history and
// highlights the period's months) per month. The client cuts the list to the range setting.
func (s *Service) Cashflow(ctx context.Context, userID uuid.UUID, c query.Context) (Cashflow, error) {
	var out Cashflow
	err := s.eng.Insight(ctx, userID, c, func(in *query.Insight) error {
		out.Period = in.Period
		var err error
		if out.Currency, err = in.Currency(); err != nil {
			return err
		}
		months, err := in.Months()
		if err != nil {
			return err
		}
		for _, m := range FillMonths(months) {
			out.Months = append(out.Months, FlowMonth{MonthFlow: m, InPeriod: InPeriod(m.Month, in.Period)})
		}
		return nil
	})
	return out, err
}

// Pace is the month-pace card (legacy CHARTS.pace). Target is nil when there is nothing to show.
type Pace struct {
	Currency    string
	Target      *platform.Date // the month shown (first day)
	DaysInMonth int
	LastDay     int     // the current line ends on this day
	Current     []int64 // cumulative spending, 31 entries
	Compare     *PaceCompare
}

// Pace builds the cumulative spending of the target month and the comparison series from the
// base rows (compare: prev, yoy, avg3, avg6 or none).
func (s *Service) Pace(ctx context.Context, userID uuid.UUID, c query.Context, compare string) (Pace, error) {
	switch compare {
	case ComparePrev, CompareYoY, CompareAvg3, CompareAvg6, CompareNone:
	default:
		ve := &platform.ValidationError{}
		ve.Add("compare", fmt.Sprintf("unknown comparison %q", compare))
		return Pace{}, ve
	}
	var out Pace
	err := s.eng.Insight(ctx, userID, c, func(in *query.Insight) error {
		var err error
		if out.Currency, err = in.Currency(); err != nil {
			return err
		}
		shown, err := in.Totals(true)
		if err != nil {
			return err
		}
		last := shown.Range.Max
		if shown.Count == 0 {
			base, err := in.Totals(false)
			if err != nil {
				return err
			}
			last = base.Range.Max
		}
		data, err := in.DataRange()
		if err != nil {
			return err
		}
		if last == nil || data.Min == nil || data.Max == nil {
			return nil
		}
		target := PaceTarget(in.Period, *last)
		days, err := in.DailySpend(false, target.AddMonths(-CompareMonths(compare)), target.AddMonths(1))
		if err != nil {
			return err
		}
		out.Target = &target
		out.DaysInMonth = DaysIn(target)
		out.LastDay = LastDay(target, *data.Max)
		out.Current = Cumulative(days, target)
		out.Compare = Compare(compare, target, data.Min.FirstOfMonth(), days)
		return nil
	})
	return out, err
}

// Payee scopes (legacy payeeScope).
const (
	ScopePeriod = "period"
	ScopeAll    = "all"
)

// PayeeTops are the accepted top-N values; 0 means all payees.
var PayeeTops = []int{0, 10, 25, 50, 100}

// Payees is the top-payees card (legacy CHARTS.payee).
type Payees struct {
	Currency string
	Total    int // payee groups with spending, before the top-N cut
	Payees   []query.PayeeSpend
}

// Payees ranks payee groups by spending over the rows shown (scope period) or the base rows
// (scope all: all time, with filters), keeping the top entries (0: all).
func (s *Service) Payees(ctx context.Context, userID uuid.UUID, c query.Context, top int, scope string) (Payees, error) {
	ve := &platform.ValidationError{}
	if scope != ScopePeriod && scope != ScopeAll {
		ve.Add("scope", fmt.Sprintf("unknown scope %q (period, all)", scope))
	}
	ok := false
	for _, t := range PayeeTops {
		ok = ok || t == top
	}
	if !ok {
		ve.Add("top", "must be 0 (all), 10, 25, 50 or 100")
	}
	if err := ve.Err(); err != nil {
		return Payees{}, err
	}
	var out Payees
	err := s.eng.Insight(ctx, userID, c, func(in *query.Insight) error {
		var err error
		if out.Currency, err = in.Currency(); err != nil {
			return err
		}
		out.Payees, out.Total, err = in.PayeeSpend(scope == ScopePeriod, top)
		return err
	})
	return out, err
}

// Daily is the daily-spending calendar (legacy CHARTS.heat). From/To are nil when there is
// nothing to show; Days lists only days with spending.
type Daily struct {
	Currency string
	From, To *platform.Date // To exclusive
	Days     []query.DaySpend
}

// Daily lists the spending per day in the calendar's range (range: period, 3m, 6m, 12m or all).
// The client bins the values into quintiles and lays out the weeks.
func (s *Service) Daily(ctx context.Context, userID uuid.UUID, c query.Context, rng string) (Daily, error) {
	switch rng {
	case RangePeriod, Range3m, Range6m, Range12m, RangeAll:
	default:
		ve := &platform.ValidationError{}
		ve.Add("range", fmt.Sprintf("unknown range %q (period, 3m, 6m, 12m, all)", rng))
		return Daily{}, ve
	}
	var out Daily
	err := s.eng.Insight(ctx, userID, c, func(in *query.Insight) error {
		var err error
		if out.Currency, err = in.Currency(); err != nil {
			return err
		}
		scoped := rng == RangePeriod
		shown, err := in.Totals(scoped)
		if err != nil {
			return err
		}
		data, err := in.DataRange()
		if err != nil {
			return err
		}
		from, to, ok := HeatRange(rng, in.Period, data, shown.Range)
		if !ok {
			return nil
		}
		out.From, out.To = &from, &to
		out.Days, err = in.DailySpend(scoped, from, to)
		return err
	})
	return out, err
}
