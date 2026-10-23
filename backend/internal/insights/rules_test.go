package insights

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
)

func d(s string) platform.Date {
	v, err := platform.ParseDate(s)
	if err != nil {
		panic(err)
	}
	return v
}

func dp(s string) *platform.Date { v := d(s); return &v }

func rng(lo, hi string) query.DateRange { return query.DateRange{Min: dp(lo), Max: dp(hi)} }

func month(s string) *query.Resolved {
	r, err := query.Resolve(&query.Period{Month: s}, nil)
	if err != nil {
		panic(err)
	}
	return r
}

func TestDays(t *testing.T) {
	l30, _ := query.Resolve(&query.Period{Preset: "l30"}, dp("2025-03-20"))
	cases := []struct {
		name   string
		period *query.Resolved
		data   query.DateRange
		shown  query.DateRange
		want   int
	}{
		{"month inside the data", month("2025-02"), rng("2025-01-01", "2025-03-20"), rng("2025-02-03", "2025-02-27"), 28},
		{"latest month clipped to the last data day", month("2025-03"), rng("2025-01-01", "2025-03-20"), query.DateRange{}, 20},
		{"first month clipped to the first data day", month("2025-01"), rng("2025-01-10", "2025-03-20"), query.DateRange{}, 22},
		{"last 30 days over 12 days of data", l30, rng("2025-03-09", "2025-03-20"), query.DateRange{}, 12},
		{"period outside the data", month("2024-06"), rng("2025-01-01", "2025-03-20"), query.DateRange{}, 1},
		{"no data", month("2025-02"), query.DateRange{}, query.DateRange{}, 1},
		{"all time: span of the rows shown", nil, rng("2025-01-01", "2025-03-20"), rng("2025-01-05", "2025-01-14"), 10},
		{"all time: one day", nil, rng("2025-01-01", "2025-03-20"), rng("2025-01-05", "2025-01-05"), 1},
		{"all time: no rows", nil, rng("2025-01-01", "2025-03-20"), query.DateRange{}, 1},
	}
	for _, c := range cases {
		if got := Days(c.period, c.data, c.shown); got != c.want {
			t.Errorf("%s: Days = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestRatesAndDeltas(t *testing.T) {
	f := func(p *float64) any {
		if p == nil {
			return nil
		}
		return *p
	}
	cases := []struct {
		name      string
		got, want any
	}{
		{"savings rate", f(SavingsRate(1000, 250)), 0.25},
		{"savings rate clamps an overspend at 0", f(SavingsRate(1000, -400)), 0.0},
		{"savings rate without income", f(SavingsRate(0, -400)), nil},
		{"delta up", f(Delta(1500, 1000)), 0.5},
		{"delta down", f(Delta(500, 1000)), -0.5},
		{"no previous spending", f(Delta(500, 0)), nil},
		{"share", Share(25, 100), 0.25},
		{"share of nothing", Share(0, 0), 0.0},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestDivRound(t *testing.T) {
	for _, c := range []struct{ a, n, want int64 }{
		{10, 4, 3}, {9, 4, 2}, {-10, 4, -3}, {-9, 4, -2}, {7, 2, 4}, {0, 3, 0}, {100, 3, 33}, {200, 3, 67},
	} {
		if got := DivRound(c.a, c.n); got != c.want {
			t.Errorf("DivRound(%d, %d) = %d, want %d", c.a, c.n, got, c.want)
		}
	}
}

func spend(date string, v int64) query.DaySpend {
	return query.DaySpend{Date: d(date), SpentMinor: v, Count: 1}
}

func TestCumulative(t *testing.T) {
	days := []query.DaySpend{spend("2025-01-31", 99), spend("2025-02-01", 100), spend("2025-02-03", 50), spend("2025-02-28", 10),
		spend("2025-03-01", 7)}
	got := Cumulative(days, d("2025-02-01"))
	if len(got) != 31 {
		t.Fatalf("len = %d, want 31", len(got))
	}
	want := []int64{100, 100, 150}
	if diff := cmp.Diff(want, got[:3]); diff != "" {
		t.Errorf("first days (-want +got):\n%s", diff)
	}
	// February ends on the 28th; the days after repeat the total.
	for day := 28; day <= 31; day++ {
		if got[day-1] != 160 {
			t.Errorf("day %d = %d, want 160", day, got[day-1])
		}
	}
}

func TestPaceTargetAndLastDay(t *testing.T) {
	if got := PaceTarget(month("2024-11"), d("2025-03-20")); got != d("2024-11-01") {
		t.Errorf("period month: %s", got)
	}
	l3m, _ := query.Resolve(&query.Period{Preset: "l3m"}, dp("2025-03-20"))
	if got := PaceTarget(l3m, d("2025-03-20")); got != d("2025-03-01") {
		t.Errorf("range period follows the last row: %s", got)
	}
	if got := PaceTarget(nil, d("2025-03-20")); got != d("2025-03-01") {
		t.Errorf("all time: %s", got)
	}
	for _, c := range []struct {
		target, max string
		want        int
	}{
		{"2025-03-01", "2025-03-20", 20}, // data ends inside the month
		{"2025-02-01", "2025-03-20", 28}, // a full month
		{"2024-02-01", "2025-03-20", 29}, // a leap month
		{"2025-03-01", "2025-03-31", 31},
	} {
		if got := LastDay(d(c.target), d(c.max)); got != c.want {
			t.Errorf("LastDay(%s, %s) = %d, want %d", c.target, c.max, got, c.want)
		}
	}
}

func TestCompare(t *testing.T) {
	days := []query.DaySpend{
		spend("2024-03-02", 10),                          // a year before
		spend("2024-12-01", 30),                          // 3 months before
		spend("2025-01-01", 20), spend("2025-01-15", 40), // 2 months before
		spend("2025-02-01", 1), spend("2025-02-10", 9), // the month before
		spend("2025-03-05", 500), // the target
	}
	target := d("2025-03-01")

	prev := Compare(ComparePrev, target, d("2024-01-01"), days)
	if prev == nil || *prev.Month != d("2025-02-01") || prev.Days != 28 || prev.Values[0] != 1 || prev.Values[9] != 10 || prev.Values[30] != 10 {
		t.Errorf("prev = %+v", prev)
	}
	yoy := Compare(CompareYoY, target, d("2024-01-01"), days)
	if yoy == nil || *yoy.Month != d("2024-03-01") || yoy.Days != 31 || yoy.Values[0] != 0 || yoy.Values[1] != 10 {
		t.Errorf("yoy = %+v", yoy)
	}
	if got := Compare(CompareYoY, target, d("2024-06-01"), days); got != nil {
		t.Errorf("yoy before the data: %+v", got)
	}
	// avg3: Feb (1, …, 10), Jan (20, …, 60), Dec (30, …, 30) → day 1: (1+20+30)/3 = 17, day 31: (10+60+30)/3 = 33.33 → 33
	avg := Compare(CompareAvg3, target, d("2024-01-01"), days)
	if avg == nil || avg.Months != 3 || avg.Days != 31 || avg.Values[0] != 17 || avg.Values[30] != 33 {
		t.Errorf("avg3 = %+v", avg)
	}
	// the data starts in January: the 6-month average has two months
	avg6 := Compare(CompareAvg6, target, d("2025-01-01"), days)
	if avg6 == nil || avg6.Months != 2 || avg6.Values[30] != 35 {
		t.Errorf("avg6 = %+v", avg6)
	}
	if got := Compare(CompareAvg3, target, d("2025-03-01"), days); got != nil {
		t.Errorf("avg without earlier months: %+v", got)
	}
	if got := Compare(CompareNone, target, d("2024-01-01"), days); got != nil {
		t.Errorf("none: %+v", got)
	}
}

func TestHeatRange(t *testing.T) {
	data := rng("2024-05-10", "2025-03-20")
	feb := month("2025-02")
	mar := month("2025-03")
	cases := []struct {
		name     string
		mode     string
		period   *query.Resolved
		shown    query.DateRange
		from, to string
		ok       bool
	}{
		{"period month", RangePeriod, feb, rng("2025-02-02", "2025-02-27"), "2025-02-01", "2025-03-01", true},
		{"period clipped to the data", RangePeriod, mar, rng("2025-03-01", "2025-03-20"), "2025-03-01", "2025-03-21", true},
		{"no period: the rows shown", RangePeriod, nil, rng("2024-06-01", "2025-01-15"), "2024-06-01", "2025-01-16", true},
		{"nothing shown", RangePeriod, feb, query.DateRange{}, "", "", false},
		{"all", RangeAll, feb, data, "2024-05-10", "2025-03-21", true},
		{"3 months", Range3m, feb, data, "2025-01-01", "2025-03-21", true},
		{"12 months", Range12m, nil, data, "2024-04-01", "2025-03-21", true},
	}
	for _, c := range cases {
		from, to, ok := HeatRange(c.mode, c.period, data, c.shown)
		if ok != c.ok || (ok && (from != d(c.from) || to != d(c.to))) {
			t.Errorf("%s: %s–%s %v, want %s–%s %v", c.name, from, to, ok, c.from, c.to, c.ok)
		}
	}
}

func TestInPeriodAndFillMonths(t *testing.T) {
	l30, _ := query.Resolve(&query.Period{Preset: "l30"}, dp("2025-03-20"))
	// l30 runs from 19 Feb: February is highlighted (legacy: from the period's first month)
	for _, c := range []struct {
		m    string
		p    *query.Resolved
		want bool
	}{
		{"2025-02-01", l30, true}, {"2025-03-01", l30, true}, {"2025-01-01", l30, false},
		{"2025-02-01", month("2025-02"), true}, {"2025-03-01", month("2025-02"), false}, {"2020-01-01", nil, true},
	} {
		if got := InPeriod(d(c.m), c.p); got != c.want {
			t.Errorf("InPeriod(%s) = %v, want %v", c.m, got, c.want)
		}
	}

	got := FillMonths([]query.MonthFlow{{Month: d("2024-11-01"), IncomeMinor: 5}, {Month: d("2025-02-01"), ExpenseMinor: 7}})
	var months []string
	for _, m := range got {
		months = append(months, m.Month.String())
	}
	if diff := cmp.Diff([]string{"2024-11-01", "2024-12-01", "2025-01-01", "2025-02-01"}, months); diff != "" {
		t.Errorf("FillMonths (-want +got):\n%s", diff)
	}
	if got[0].IncomeMinor != 5 || got[1] != (query.MonthFlow{Month: d("2024-12-01")}) || got[3].ExpenseMinor != 7 {
		t.Errorf("FillMonths values: %+v", got)
	}
	if FillMonths(nil) != nil {
		t.Error("FillMonths(nil) should be nil")
	}
	if DaysIn(platform.Date{Y: 2024, M: time.February, D: 17}) != 29 {
		t.Error("DaysIn(Feb 2024) should be 29")
	}
}
