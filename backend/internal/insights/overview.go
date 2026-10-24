package insights

import (
	"cmp"
	"context"
	"slices"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/investments"
	"github.com/x2db736ac85h/finuance/backend/internal/money"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
)

// The month-centric overview (PLAN §4.6 "Overview", legacy ovCtx, renderOverview and CHARTS.o_*).
// It reads the "base" rows: the list's filters and search apply, its period does not. Amounts are
// minor units; where the legacy divided floats (averages, projections) the result is rounded once,
// half away from zero.

// Uncategorized is the legacy name of spending without a category.
const Uncategorized = "(uncategorized)"

// OverviewMonths are the accepted values of the months setting (legacy cfg.ovMonths).
var OverviewMonths = []int{6, 12, 24, 36}

// MaxLargest is how many largest expenses the overview returns (the modal shows 40, the card 12).
const MaxLargest = 40

// Overview is everything the overview screen shows for one month.
type Overview struct {
	Currency string
	Empty    bool // no dated amounts in the base rows

	Month, Prev           platform.Date // first days of the selected and the previous month
	FirstMonth, LastMonth platform.Date // the first and last months with data (base rows)
	DataMax               platform.Date // the latest booking date of the context's accounts
	Partial               bool          // the selected month is the data's last and not complete
	Elapsed, DaysInMonth  int

	Cur, PrevFlow   query.MonthFlow
	PrevToDateMinor int64  // the previous month's spending up to the same day of month
	ProjectedMinor  *int64 // partial months: spent / elapsed × days in month
	AvgMonths       int    // how many earlier months (up to 3) the averages and default budgets use

	Months        []query.MonthFlow // the months setting's range ending at Month, from FirstMonth on, oldest first
	Categories    []BudgetRow       // budget order (legacy o_budget); also the month's spending per category
	IncomeSources []query.PayeeSpend
	ColorSlots    []query.CategorySlot
	Highlights    []Highlight
	Recurring     []Recurring
	Largest       []query.ExpenseRow // the month's largest expenses, at most MaxLargest
	Weekday       [7]WeekdaySpend    // Monday first
	Balance       BalanceSeries

	Investment  investments.Summary
	YieldOnCost investments.Yield
	Fire        investments.FIRE
}

// BudgetRow is one category in the budgets card: the month's spending, the previous month's, the
// average of up to three earlier months, and the budget with its status.
type BudgetRow struct {
	ID              *uuid.UUID // nil: uncategorised (default budget only)
	Name            string
	CurMinor        int64
	PrevMinor       int64
	Avg3Minor       int64
	BudgetMinor     int64
	BudgetIsDefault bool
	Status          string // ok, ahead, over, none
}

// Budget statuses (legacy STATUS good / warn / crit / none).
const (
	StatusOK    = "ok"
	StatusAhead = "ahead"
	StatusOver  = "over"
	StatusNone  = "none"
)

// WeekdaySpend is the spending of one weekday over the weekday window and how many of its days
// the window has (the average is Total / Days).
type WeekdaySpend struct {
	TotalMinor int64
	Days       int
}

// BalanceSeries is the running balance: opening balances plus every transaction, end of day.
type BalanceSeries struct {
	OpeningMinor int64
	NowMinor     int64 // after the last transaction
	Points       []BalancePoint
}

// BalancePoint is the balance at the end of a day.
type BalancePoint struct {
	Date         platform.Date
	BalanceMinor int64
}

// Overview builds the overview of month (nil: the latest month with data) with the months
// setting's range (6, 12, 24 or 36); inv is the user's investment document.
func (s *Service) Overview(ctx context.Context, userID uuid.UUID, c query.Context, month *platform.Date, months int,
	inv investments.Doc) (Overview, error) {
	if !slices.Contains(OverviewMonths, months) {
		ve := &platform.ValidationError{}
		ve.Add("months", "must be 6, 12, 24 or 36")
		return Overview{}, ve
	}
	c.Period = nil // the overview never applies the list's period
	var out Overview
	err := s.eng.Insight(ctx, userID, c, func(in *query.Insight) error {
		var err error
		if out.Currency, err = in.Currency(); err != nil {
			return err
		}
		flows, err := in.Months()
		if err != nil {
			return err
		}
		data, err := in.DataRange()
		if err != nil {
			return err
		}
		if len(flows) == 0 || data.Max == nil {
			out.Empty = true
			return nil
		}
		unit := unitOf(out.Currency)
		ov := newMonthView(flows, *data.Max, month)
		out.Month, out.Prev, out.FirstMonth, out.LastMonth, out.DataMax = ov.m, ov.p, ov.fm, ov.lm, *data.Max
		out.Partial, out.Elapsed, out.DaysInMonth = ov.partial, ov.elapsed, ov.dim
		out.Cur, out.PrevFlow = ov.get(ov.m), ov.get(ov.p)
		out.Months = ov.series(months)
		out.AvgMonths = ov.nAvg
		if ov.partial {
			p := DivRound(out.Cur.ExpenseMinor*int64(ov.dim), int64(ov.elapsed))
			out.ProjectedMinor = &p
		}

		// Days of the previous month, this month and the weekday window, in one statement.
		from := ov.p
		if w := ov.m.AddMonths(-2); w.Before(from) {
			from = w
		}
		days, err := in.ExpenseDays(from, ov.m.AddMonths(1))
		if err != nil {
			return err
		}
		out.PrevToDateMinor = prevToDate(days, ov.p, ov.elapsed)
		out.Weekday = weekdaySpend(days, ov.m.AddMonths(-2), minDate(ov.m.AddMonths(1), data.Max.AddDays(1)))

		cats, err := in.MonthCategorySpend(ov.m.AddMonths(-3), ov.m.AddMonths(1))
		if err != nil {
			return err
		}
		budgets, err := in.Budgets()
		if err != nil {
			return err
		}
		out.Categories = budgetRows(cats, budgets, ov, unit)
		if out.IncomeSources, err = in.IncomeSources(ov.m, ov.m.AddMonths(1)); err != nil {
			return err
		}
		if out.ColorSlots, err = in.CategorySlots(); err != nil {
			return err
		}
		if out.Largest, err = in.LargestExpenses(ov.m, ov.m.AddMonths(1), MaxLargest); err != nil {
			return err
		}
		charges, others, err := in.RecurringCandidates()
		if err != nil {
			return err
		}
		out.Recurring = WithCadences(FindRecurring(charges, *data.Max), others, *data.Max)
		opening, totals, err := in.Balance()
		if err != nil {
			return err
		}
		out.Balance = balanceSeries(opening, totals, out.Months[0].Month, ov.m.AddMonths(1))
		out.Highlights = highlights(&out, ov, cats, days, unit)

		detected, err := in.DividendIncome(ov.m.AddMonths(-11), ov.m.AddMonths(1))
		if err != nil {
			return err
		}
		out.Investment = investments.Summarize(inv)
		out.YieldOnCost = investments.YieldOnCost(inv, detected)
		var fire []investments.MonthTotals
		for k := 11; k >= 0; k-- {
			if t := ov.m.AddMonths(-k); !t.Before(ov.fm) {
				f := ov.get(t)
				fire = append(fire, investments.MonthTotals{Income: f.IncomeMinor, Expense: f.ExpenseMinor})
			}
		}
		out.Fire = investments.FireOf(inv, fire, out.Investment.Value)
		return nil
	})
	return out, err
}

// unitOf is one major unit in minor units (100 for PLN), for the legacy thresholds and rounding
// written in major units.
func unitOf(currency string) int64 {
	exp, ok := money.Exponent(currency)
	if !ok {
		exp = 2
	}
	u := int64(1)
	for range exp {
		u *= 10
	}
	return u
}

// monthView is the legacy ovCtx: the month aggregates of the base rows and the selected month.
type monthView struct {
	byMonth          map[platform.Date]query.MonthFlow
	fm, lm, m, p     platform.Date
	partial          bool
	elapsed, dim     int
	nAvg             int
	avgExp, avgIncom int64 // sums over the nAvg months before m
}

func newMonthView(flows []query.MonthFlow, dataMax platform.Date, month *platform.Date) *monthView {
	v := &monthView{byMonth: map[platform.Date]query.MonthFlow{}}
	for _, f := range flows {
		v.byMonth[f.Month] = f
	}
	v.fm, v.lm = flows[0].Month, flows[len(flows)-1].Month
	v.m = v.lm
	if month != nil {
		if m := month.FirstOfMonth(); !m.Before(v.fm) && !m.After(v.lm) {
			v.m = m
		}
	}
	v.p = v.m.AddMonths(-1)
	v.dim = DaysIn(v.m)
	v.partial = !dataMax.Before(v.m) && dataMax.Before(v.m.AddMonths(1)) && dataMax.D < v.dim
	v.elapsed = v.dim
	if v.partial {
		v.elapsed = dataMax.D
	}
	for k := 1; k <= 3; k++ {
		t := v.m.AddMonths(-k)
		if t.Before(v.fm) {
			break
		}
		v.nAvg++
		v.avgExp += v.get(t).ExpenseMinor
		v.avgIncom += v.get(t).IncomeMinor
	}
	return v
}

func (v *monthView) get(t platform.Date) query.MonthFlow {
	if f, ok := v.byMonth[t]; ok {
		return f
	}
	return query.MonthFlow{Month: t}
}

// series is the months setting's range ending at m, without months before the first with data.
func (v *monthView) series(n int) []query.MonthFlow {
	var out []query.MonthFlow
	for k := n - 1; k >= 0; k-- {
		if t := v.m.AddMonths(-k); !t.Before(v.fm) {
			out = append(out, v.get(t))
		}
	}
	return out
}

// prevToDate is the previous month's spending on days up to elapsed (a like-for-like comparison
// while the month is in progress).
func prevToDate(days []query.DaySpend, p platform.Date, elapsed int) int64 {
	var s int64
	end := p.AddMonths(1)
	for _, d := range days {
		if !d.Date.Before(p) && d.Date.Before(end) && d.Date.D <= elapsed {
			s += d.SpentMinor
		}
	}
	return s
}

// weekdaySpend sums spending per weekday (Monday first) over [from, to) and counts each weekday's
// days in it (legacy o_week: the 3 months ending at the selected month, cut at the data's end).
func weekdaySpend(days []query.DaySpend, from, to platform.Date) [7]WeekdaySpend {
	var out [7]WeekdaySpend
	for d := from; d.Before(to); d = d.AddDays(1) {
		out[(int(d.Weekday())+6)%7].Days++
	}
	for _, d := range days {
		if !d.Date.Before(from) && d.Date.Before(to) {
			out[(int(d.Date.Weekday())+6)%7].TotalMinor += d.SpentMinor
		}
	}
	return out
}

type catKey struct {
	id   uuid.UUID
	none bool // uncategorised
}

func keyOf(id *uuid.UUID) catKey {
	if id == nil {
		return catKey{none: true}
	}
	return catKey{id: *id}
}

// RoundNiceMinor is the legacy roundNice of an average in major units, sum / n: ≤ 0 → 0; under
// 100 up to the next multiple of 5; else to the nearest 10 (halves up). It works on the exact sum
// in minor units (unit = one major unit) and returns minor units.
func RoundNiceMinor(sum, n, unit int64) int64 {
	if sum <= 0 || n <= 0 {
		return 0
	}
	if sum < 100*unit*n {
		step := 5 * unit * n
		return (sum + step - 1) / step * 5 * unit
	}
	step := 10 * unit * n
	return (2*sum + step) / (2 * step) * 10 * unit
}

// BudgetStatus is the legacy budget status: no budget; over it; ahead of pace (more than 10 % ahead
// of the days elapsed and past half the budget); else on track.
func BudgetStatus(spent, budget int64, elapsed, dim int) string {
	switch {
	case budget == 0:
		return StatusNone
	case spent > budget:
		return StatusOver
	}
	r := float64(spent) / float64(budget)
	if r > float64(elapsed)/float64(dim)*1.1 && r > 0.5 {
		return StatusAhead
	}
	return StatusOK
}

// budgetRows builds the budgets card (legacy o_budget): every category with spending this month or
// in the averaged months; the budget is the override, else roundNice of the average; rows with
// neither spending nor budget are left out; largest budget first, then largest spending.
func budgetRows(cats []query.MonthCategory, overrides map[uuid.UUID]int64, v *monthView, unit int64) []BudgetRow {
	type acc struct {
		id         *uuid.UUID
		name       string
		cur, prev  int64
		sum3       int64
		firstOrder int
	}
	rows := map[catKey]*acc{}
	order := 0
	for _, c := range cats {
		k := keyOf(c.ID)
		a := rows[k]
		if a == nil {
			name := Uncategorized
			if c.Name != nil {
				name = *c.Name
			}
			a = &acc{id: c.ID, name: name, firstOrder: order}
			order++
			rows[k] = a
		}
		switch {
		case c.Month == v.m:
			a.cur += c.SpentMinor
		case !c.Month.Before(v.m.AddMonths(-v.nAvg)) && c.Month.Before(v.m):
			a.sum3 += c.SpentMinor
			if c.Month == v.p {
				a.prev += c.SpentMinor
			}
		}
	}
	var out []BudgetRow
	for _, a := range rows {
		r := BudgetRow{ID: a.id, Name: a.name, CurMinor: a.cur, PrevMinor: a.prev}
		if v.nAvg > 0 {
			r.Avg3Minor = DivRound(a.sum3, int64(v.nAvg))
		}
		r.BudgetMinor, r.BudgetIsDefault = RoundNiceMinor(a.sum3, int64(v.nAvg), unit), true
		if a.id != nil {
			if b, ok := overrides[*a.id]; ok {
				r.BudgetMinor, r.BudgetIsDefault = b, false
			}
		}
		if r.CurMinor == 0 && r.BudgetMinor == 0 {
			continue
		}
		r.Status = BudgetStatus(r.CurMinor, r.BudgetMinor, v.elapsed, v.dim)
		out = append(out, r)
	}
	slices.SortStableFunc(out, func(x, y BudgetRow) int {
		return cmp.Or(cmp.Compare(y.BudgetMinor, x.BudgetMinor), cmp.Compare(y.CurMinor, x.CurMinor), cmp.Compare(x.Name, y.Name))
	})
	return out
}

// balanceSeries is the end-of-day balance over [from, to): the balance before from as the first
// point when the range starts without a transaction (legacy o_balance).
func balanceSeries(opening int64, days []query.DayTotal, from, to platform.Date) BalanceSeries {
	out := BalanceSeries{OpeningMinor: opening}
	run, before := opening, opening
	for _, d := range days {
		run += d.SumMinor
		switch {
		case d.Date.Before(from):
			before = run
		case d.Date.Before(to):
			out.Points = append(out.Points, BalancePoint{d.Date, run})
		}
	}
	out.NowMinor = run
	if len(out.Points) == 0 || out.Points[0].Date.After(from) {
		out.Points = append([]BalancePoint{{from, before}}, out.Points...)
	}
	return out
}

// Recurring is a detected recurring payment (legacy findRecurring).
type Recurring struct {
	Payee       string
	Category    string // the most frequent category; "" when uncategorised
	MedianMinor int64  // the typical amount, positive
	Last, Next  platform.Date
	Active      bool
	YearlyMinor int64
	Count       int
	Cadence     string // CadenceMonthly, CadenceWeekly or CadenceYearly
}

// FindRecurring ports the legacy rules over a payee group's expenses (charges sorted by payee
// group, then date): at least 3 distinct months; at most 1.5 charges a month; at least 75 % of
// the amounts within ±30 % of the median (the upper one); charges in at least 60 % of the months
// between the first and the last. Active when the last charge is within 45 days of the data's
// end; the next one is expected on the same day next month (clamped). Active first, then by
// yearly cost.
func FindRecurring(charges []query.Charge, dataMax platform.Date) []Recurring {
	var out []Recurring
	for i := 0; i < len(charges); {
		j := i
		for j < len(charges) && charges[j].PayeeGroup == charges[i].PayeeGroup {
			j++
		}
		if r, ok := recurringOf(charges[i:j], dataMax); ok {
			out = append(out, r)
		}
		i = j
	}
	sortRecurring(out)
	return out
}

// sortRecurring puts active payments first, then the larger yearly costs; ties keep their order.
func sortRecurring(out []Recurring) {
	slices.SortStableFunc(out, func(x, y Recurring) int {
		if x.Active != y.Active {
			if x.Active {
				return -1
			}
			return 1
		}
		return cmp.Compare(y.YearlyMinor, x.YearlyMinor)
	})
}

func recurringOf(list []query.Charge, dataMax platform.Date) (Recurring, bool) {
	months := map[platform.Date]bool{}
	for _, c := range list {
		months[c.BookedOn.FirstOfMonth()] = true
	}
	ms := len(months)
	if ms < 3 || float64(len(list))/float64(ms) > 1.5 {
		return Recurring{}, false
	}
	am := make([]int64, len(list))
	for i, c := range list {
		am[i] = c.SpentMinor
	}
	slices.Sort(am)
	med := am[len(am)/2]
	near := 0
	for _, a := range am {
		if abs(a-med)*10 <= med*3 { // |a − med| ≤ med × 0.3
			near++
		}
	}
	if float64(near)/float64(len(am)) < 0.75 {
		return Recurring{}, false
	}
	first, last := list[0].BookedOn, list[len(list)-1].BookedOn
	span := (last.Y-first.Y)*12 + int(last.M) - int(first.M) + 1
	if float64(ms)/float64(span) < 0.6 {
		return Recurring{}, false
	}
	nm := last.FirstOfMonth().AddMonths(1)
	next := minDate(nm.AddDays(last.D-1), nm.AddMonths(1).AddDays(-1))
	return Recurring{Payee: list[0].PayeeGroup, Category: topCategory(list), MedianMinor: med, Last: last, Next: next,
		Active: last.DaysUntil(dataMax) <= 45, YearlyMinor: med * 12, Count: len(list), Cadence: CadenceMonthly}, true
}

// topCategory is the most frequent category of the charges ("" for uncategorised); ties go to
// the one seen first (oldest).
func topCategory(list []query.Charge) string {
	counts := map[string]int{}
	var names []string
	for _, c := range list {
		n := ""
		if c.Category != nil {
			n = *c.Category
		}
		if counts[n] == 0 {
			names = append(names, n)
		}
		counts[n]++
	}
	cat := names[0]
	for _, n := range names {
		if counts[n] > counts[cat] {
			cat = n
		}
	}
	return cat
}
