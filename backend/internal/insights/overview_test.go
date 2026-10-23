package insights

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
)

func TestRoundNiceMinorMatchesLegacy(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "oracle", "recurring-periods", "demo.json"))
	if err != nil {
		t.Fatal(err)
	}
	var o struct {
		RoundNice []struct{ In, Out float64 } `json:"roundNice"`
	}
	if err := json.Unmarshal(b, &o); err != nil {
		t.Fatal(err)
	}
	for _, c := range o.RoundNice {
		if got := RoundNiceMinor(int64(math.Round(c.In*100)), 1, 100); got != int64(c.Out*100) {
			t.Errorf("roundNice(%v) = %d, legacy %v", c.In, got, c.Out)
		}
	}
	// averages: (99 + 102) / 2 = 100.5 → 100; (40 + 41 + 41) / 3 = 40.67 → 45; halves round up
	for _, c := range []struct{ sum, n, want int64 }{{20100, 2, 10000}, {12200, 3, 4500}, {21000, 2, 11000}, {0, 3, 0}, {500, 0, 0}} {
		if got := RoundNiceMinor(c.sum, c.n, 100); got != c.want {
			t.Errorf("RoundNiceMinor(%d, %d) = %d, want %d", c.sum, c.n, got, c.want)
		}
	}
}

func TestBudgetStatus(t *testing.T) {
	cases := []struct {
		spent, budget int64
		elapsed, dim  int
		want          string
	}{
		{100, 0, 10, 30, StatusNone},
		{0, 0, 10, 30, StatusNone},
		{1001, 1000, 30, 30, StatusOver},
		{1000, 1000, 30, 30, StatusOK}, // exactly at the budget
		// day 15 of 30: pace 0.5, ahead beyond 0.55 and past half the budget
		{550, 1000, 15, 30, StatusOK},
		{551, 1000, 15, 30, StatusAhead},
		// day 5 of 30: 40 % spent is ahead of pace but not past half
		{400, 1000, 5, 30, StatusOK},
		{501, 1000, 5, 30, StatusAhead},
	}
	for _, c := range cases {
		if got := BudgetStatus(c.spent, c.budget, c.elapsed, c.dim); got != c.want {
			t.Errorf("BudgetStatus(%d, %d, %d/%d) = %s, want %s", c.spent, c.budget, c.elapsed, c.dim, got, c.want)
		}
	}
}

func charge(payee, date string, v int64, cat string) query.Charge {
	c := query.Charge{PayeeGroup: payee, BookedOn: d(date), SpentMinor: v}
	if cat != "" {
		c.Category = &cat
	}
	return c
}

func TestFindRecurring(t *testing.T) {
	end := d("2025-03-20")
	monthly := func(payee string, amounts []int64, cats ...string) []query.Charge {
		var out []query.Charge
		for i, a := range amounts {
			c := ""
			if i < len(cats) {
				c = cats[i]
			}
			out = append(out, charge(payee, platform.Date{Y: 2024, M: 10, D: 31}.AddMonths(i).String(), a, c))
		}
		return out
	}
	cases := []struct {
		name    string
		charges []query.Charge
		want    int
	}{
		{"three months", monthly("A", []int64{1000, 1000, 1000}), 1},
		{"two months are not enough", monthly("A", []int64{1000, 1000}), 0},
		{"1.5 charges a month is still monthly", append(monthly("A", []int64{1000, 1000}), charge("A", "2024-11-15", 1000, ""), charge("A", "2024-12-15", 1000, "")), 1},
		{"more than 1.5 a month is not", append(monthly("A", []int64{1000, 1000}), charge("A", "2024-10-10", 1000, ""), charge("A", "2024-11-15", 1000, ""), charge("A", "2024-12-15", 1000, "")), 0},
		// median 1000 (upper of 4): 300 off is in, 301 off is out; 3 of 4 near is exactly 75 %
		{"amounts within ±30 %", monthly("A", []int64{1000, 1000, 1300, 700}), 1},
		{"one too far at 75 % is fine", monthly("A", []int64{1000, 1000, 1000, 1301}), 1},
		// the median is the upper one: 1000, 1000, 1301, 1500 → 1301, and all four are near it
		{"the upper median", monthly("A", []int64{1000, 1000, 1301, 1500}), 1},
		{"three of five near is 60 %", monthly("A", []int64{1000, 1000, 1000, 1301, 1400}), 0},
		{"coverage 3 of 5 months is exactly 60 %", []query.Charge{charge("A", "2024-10-05", 1000, ""), charge("A", "2024-11-05", 1000, ""), charge("A", "2025-02-05", 1000, "")}, 1},
		{"coverage 3 of 6 months", []query.Charge{charge("A", "2024-10-05", 1000, ""), charge("A", "2024-11-05", 1000, ""), charge("A", "2025-03-05", 1000, "")}, 0},
	}
	for _, c := range cases {
		if got := FindRecurring(c.charges, end); len(got) != c.want {
			t.Errorf("%s: %d recurring, want %d (%+v)", c.name, len(got), c.want, got)
		}
	}

	// next = same day next month, clamped (31 Jan → 28 Feb); active within 45 days; category ties
	// go to the first seen
	r := FindRecurring(monthly("A", []int64{900, 1000, 1100}, "Media", "Rachunki", "Rachunki"), d("2025-02-14"))
	want := Recurring{Payee: "A", Category: "Rachunki", MedianMinor: 1000, Last: d("2024-12-31"), Next: d("2025-01-31"), Active: true,
		YearlyMinor: 12000, Count: 3}
	if diff := cmp.Diff([]Recurring{want}, r); diff != "" {
		t.Errorf("recurring (-want +got):\n%s", diff)
	}
	if r := FindRecurring(monthly("A", []int64{1000, 1000, 1000}, "X", "Y"), d("2025-02-15")); r[0].Active || r[0].Category != "X" {
		t.Errorf("46 days after the last charge is not active; ties go to the first category: %+v", r)
	}
	jan := FindRecurring([]query.Charge{charge("B", "2024-11-30", 500, ""), charge("B", "2024-12-31", 500, ""), charge("B", "2025-01-31", 500, "")}, end)
	if jan[0].Next != d("2025-02-28") {
		t.Errorf("next after 31 Jan = %s", jan[0].Next)
	}
	// active first, then by yearly cost
	mixed := append(monthly("Big", []int64{5000, 5000, 5000}), monthly("Small", []int64{100, 100, 100})...)
	mixed = append(mixed, charge("Old", "2023-01-05", 9000, ""), charge("Old", "2023-02-05", 9000, ""), charge("Old", "2023-03-05", 9000, ""))
	var order []string
	for _, x := range FindRecurring(mixed, d("2025-02-01")) {
		order = append(order, x.Payee)
	}
	if diff := cmp.Diff([]string{"Big", "Small", "Old"}, order); diff != "" {
		t.Errorf("order (-want +got):\n%s", diff)
	}
}

func TestBalanceAndWeekday(t *testing.T) {
	days := []query.DayTotal{{Date: d("2024-12-30"), SumMinor: 1000}, {Date: d("2025-01-03"), SumMinor: -200}, {Date: d("2025-02-10"), SumMinor: 50}, {Date: d("2025-03-02"), SumMinor: 7}}
	b := balanceSeries(500, days, d("2025-01-01"), d("2025-03-01"))
	want := BalanceSeries{OpeningMinor: 500, NowMinor: 1357, Points: []BalancePoint{{d("2025-01-01"), 1500}, {d("2025-01-03"), 1300}, {d("2025-02-10"), 1350}}}
	if diff := cmp.Diff(want, b); diff != "" {
		t.Errorf("balance (-want +got):\n%s", diff)
	}
	// a range that starts on a day with a transaction needs no extra point
	if b := balanceSeries(0, days, d("2025-01-03"), d("2025-03-01")); b.Points[0] != (BalancePoint{d("2025-01-03"), 800}) || len(b.Points) != 2 {
		t.Errorf("balance from a transaction day: %+v", b)
	}

	spend := []query.DaySpend{{Date: d("2025-03-03"), SpentMinor: 300}, {Date: d("2025-03-10"), SpentMinor: 100}, {Date: d("2025-03-16"), SpentMinor: 70},
		{Date: d("2025-02-28"), SpentMinor: 999}}
	w := weekdaySpend(spend, d("2025-03-01"), d("2025-03-18"))
	// 1–17 March 2025: Sat 1 … Mon 17 → three Mondays, two of everything else except Sat/Sun (3 and 3)
	if w[0] != (WeekdaySpend{400, 3}) || w[6] != (WeekdaySpend{70, 3}) || w[1] != (WeekdaySpend{0, 2}) {
		t.Errorf("weekday: %+v", w)
	}
	if got := prevToDate(spend, d("2025-02-01"), 27); got != 0 {
		t.Errorf("prevToDate up to day 27 = %d", got)
	}
	if got := prevToDate(spend, d("2025-02-01"), 28); got != 999 {
		t.Errorf("prevToDate up to day 28 = %d", got)
	}
}

func ovFixture() (*Overview, *monthView) {
	flows := []query.MonthFlow{
		{Month: d("2024-12-01"), IncomeMinor: 500000, ExpenseMinor: 300000, Count: 10},
		{Month: d("2025-01-01"), IncomeMinor: 500000, ExpenseMinor: 300000, Count: 10},
		{Month: d("2025-02-01"), IncomeMinor: 500000, ExpenseMinor: 300000, Count: 10},
		{Month: d("2025-03-01"), IncomeMinor: 500000, ExpenseMinor: 150000, Count: 5},
	}
	v := newMonthView(flows, d("2025-03-15"), nil)
	o := &Overview{Month: v.m, Prev: v.p, Partial: v.partial, Elapsed: v.elapsed, DaysInMonth: v.dim, Cur: v.get(v.m), PrevFlow: v.get(v.p),
		Months: v.series(12)}
	return o, v
}

func TestMonthView(t *testing.T) {
	o, v := ovFixture()
	if o.Month != d("2025-03-01") || !o.Partial || o.Elapsed != 15 || o.DaysInMonth != 31 || v.nAvg != 3 || v.avgExp != 900000 {
		t.Errorf("month view: %+v nAvg %d avgExp %d", o, v.nAvg, v.avgExp)
	}
	// a month outside the data falls back to the latest
	if v := newMonthView([]query.MonthFlow{{Month: d("2025-01-01")}}, d("2025-01-31"), &platform.Date{Y: 2030, M: 1, D: 1}); v.m != d("2025-01-01") || v.partial {
		t.Errorf("fallback month: %+v", v)
	}
	if len(v.series(6)) != 4 || len(v.series(3)) != 3 || v.series(3)[0].Month != d("2025-01-01") {
		t.Error("series is cut at the first month with data")
	}
}

func TestHighlightThresholds(t *testing.T) {
	o, v := ovFixture()
	// vs previous (to the same day while partial): changes under 1 % are left out
	for _, c := range []struct {
		prev int64
		tone string // "" = left out
	}{{148600, ""}, {148515, ""}, {148514, ToneUp}, {151515, ""}, {151516, ToneDown}} {
		o.PrevToDateMinor = c.prev
		h := hlVsPrev(o)
		switch {
		case c.tone == "" && h != nil, c.tone != "" && h == nil:
			t.Errorf("vsPrev against %d: %+v", c.prev, h)
		case h != nil && (h.Tone != c.tone || h.Amount != 150000 || h.Other != c.prev || !h.SameDays || h.Month != d("2025-02-01")):
			t.Errorf("vsPrev against %d: %+v", c.prev, h)
		}
	}
	o.PrevToDateMinor = 0
	if hlVsPrev(o) != nil {
		t.Error("vsPrev: nothing to compare with")
	}

	// vs average: 300 000 a month scaled to 15/31 days = 145 161.29; 150 000 is +3.3 % → nothing
	if h := hlVsAverage(o, v); h != nil {
		t.Errorf("vsAverage under 5 %%: %+v", h)
	}
	o.Cur.ExpenseMinor = 152420 // +5.0 %
	if h := hlVsAverage(o, v); h == nil || h.Tone != ToneUp || h.Amount != 7259 || h.Months != 3 {
		t.Errorf("vsAverage at 5 %%: %+v", h)
	}
	o.Cur.ExpenseMinor = 100000
	if h := hlVsAverage(o, v); h == nil || h.Tone != ToneDown || h.Amount != 45161 {
		t.Errorf("vsAverage below: %+v", h)
	}

	// category changes: usual = sum / 3 × 15/31; threshold max(50 units, 2 % of 3 000) = 60 units
	cat := func(m string, name string, v int64) query.MonthCategory {
		id := uuid.NewSHA1(uuid.Nil, []byte(name))
		return query.MonthCategory{Month: d(m), ID: &id, Name: &name, SpentMinor: v}
	}
	cats := []query.MonthCategory{
		cat("2025-01-01", "Kino", 31000), cat("2025-02-01", "Kino", 31000), cat("2024-12-01", "Kino", 31000), // usual 15 000
		cat("2025-03-01", "Kino", 21001),                                    // +6 001: over 60 units by one minor unit
		cat("2025-02-01", "Media", 93000), cat("2025-03-01", "Media", 9000), // usual 15 000 → −6 000: not below −60 units
	}
	up, down := hlCategoryChanges(v, cats, 100)
	if up == nil || up.Category != "Kino" || up.Amount != 6001 || down != nil {
		t.Errorf("category changes: up %+v down %+v", up, down)
	}
	cats[len(cats)-1].SpentMinor = 8999
	if _, down := hlCategoryChanges(v, cats, 100); down == nil || down.Category != "Media" || down.Amount != 6001 {
		t.Errorf("category below: %+v", down)
	}

	// savings: kept, overspend, no income
	o.Cur = query.MonthFlow{IncomeMinor: 1000, ExpenseMinor: 250}
	if h := hlSavings(o); h.Kind != HLSavings || h.Pct != 75 || h.Months != 4 || math.Abs(h.AvgPct-(40*3+70)/4.0) > 1e-9 {
		t.Errorf("savings: %+v", h)
	}
	o.Cur = query.MonthFlow{IncomeMinor: 1000, ExpenseMinor: 1500}
	if h := hlSavings(o); h.Kind != HLOverspend || h.Amount != 500 || h.Tone != ToneWarn {
		t.Errorf("overspend: %+v", h)
	}
	o.Cur = query.MonthFlow{ExpenseMinor: 1}
	if h := hlSavings(o); h.Kind != HLNoIncome {
		t.Errorf("no income: %+v", h)
	}
	o.Cur = query.MonthFlow{}
	if h := hlSavings(o); h != nil {
		t.Errorf("nothing at all: %+v", h)
	}

	// no-spend days count only days elapsed; projection only for partial months
	days := []query.DaySpend{{Date: d("2025-03-01")}, {Date: d("2025-03-15")}, {Date: d("2025-03-16")}, {Date: d("2025-02-20")}}
	if h := hlNoSpendDays(o, days); h.Days != 13 || h.Tone != ToneGood {
		t.Errorf("no-spend days: %+v", h)
	}
	if h := hlProjection(&Overview{}); h != nil {
		t.Errorf("projection without a partial month: %+v", h)
	}
}
