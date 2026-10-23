// Package insights computes the insight charts of the table view (PLAN §4.6, legacy CHARTS.sum,
// cat, flow, pace, payee and heat): summary, spending by category, cash flow by month, month pace,
// top payees and daily spending.
//
// The numbers come from query.Insight, which runs the aggregations on the same compiled context
// as the table, so every chart agrees with the rows shown. This file holds the rules on top of
// those numbers, as pure functions: how many days a period has, the previous-period delta, the
// cumulative pace arrays and the heatmap's date range. Amounts are int64 minor units; float64 is
// only used for ratios (shares, savings rate, deltas), as PLAN §8 allows.
package insights

import (
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
)

// Days is the number of days the summary averages over (legacy CHARTS.sum). With a period it is
// the period clipped to the data range (data: the context's accounts, ignoring filters), so "Last
// 30 days" over 12 days of data is 12; at least 1. Without a period it is the span of the rows
// shown (first to last booking date, inclusive), or 1 with no rows.
func Days(period *query.Resolved, data, shown query.DateRange) int {
	if period != nil {
		if data.Min == nil || data.Max == nil {
			return 1
		}
		to := minDate(period.To, data.Max.AddDays(1))
		from := maxDate(period.From, *data.Min)
		return max(1, from.DaysUntil(to))
	}
	if shown.Min == nil || shown.Max == nil {
		return 1
	}
	return shown.Min.DaysUntil(*shown.Max) + 1
}

// Ratio is a/b, or nil when b is 0.
func Ratio(a, b int64) *float64 {
	if b == 0 {
		return nil
	}
	r := float64(a) / float64(b)
	return &r
}

// SavingsRate is the share of income kept, max(0, net / income); nil without income (legacy:
// "saved N% of income", or "no income").
func SavingsRate(income, net int64) *float64 {
	r := Ratio(net, income)
	if r != nil && *r < 0 {
		z := 0.0
		r = &z
	}
	return r
}

// Delta is the change of spent against the previous period's spending, (spent − prev) / prev;
// nil when the previous period had no spending (legacy: "no spending in previous period").
func Delta(spent, prev int64) *float64 {
	if prev <= 0 {
		return nil
	}
	return Ratio(spent-prev, prev)
}

// DivRound divides and rounds half away from zero (as Intl.NumberFormat rounds the legacy's float
// averages to cents). n must be positive.
func DivRound(a, n int64) int64 {
	q, r := a/n, a%n
	if 2*abs(r) >= n {
		if a < 0 {
			q--
		} else {
			q++
		}
	}
	return q
}

func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

// Month pace comparisons (legacy paceCmp).
const (
	ComparePrev = "prev"
	CompareYoY  = "yoy"
	CompareAvg3 = "avg3"
	CompareAvg6 = "avg6"
	CompareNone = "none"
)

// PaceTarget is the month the pace chart shows: the period's month, else the month of the last
// booking date of the rows shown (or of all rows, when the period shows none).
func PaceTarget(period *query.Resolved, last platform.Date) platform.Date {
	if period != nil && period.Kind == "month" {
		return period.From
	}
	return last.FirstOfMonth()
}

// CompareMonths is how many months before the target a comparison reads.
func CompareMonths(mode string) int {
	switch mode {
	case ComparePrev:
		return 1
	case CompareYoY:
		return 12
	case CompareAvg3:
		return 3
	case CompareAvg6:
		return 6
	}
	return 0
}

// DaysIn is the number of days of month m (any day of it).
func DaysIn(m platform.Date) int { return m.FirstOfMonth().DaysUntil(m.FirstOfMonth().AddMonths(1)) }

// Cumulative is a month's spending summed up to each day of the month: index d-1 holds days 1…d.
// It always has 31 entries; days past the month's end repeat its total (legacy cumOf).
func Cumulative(days []query.DaySpend, month platform.Date) []int64 {
	var a [32]int64
	end := month.AddMonths(1)
	for _, d := range days {
		if !d.Date.Before(month) && d.Date.Before(end) {
			a[d.Date.D] += d.SpentMinor
		}
	}
	for d := 1; d <= 31; d++ {
		a[d] += a[d-1]
	}
	return append([]int64(nil), a[1:]...)
}

// PaceCompare is the comparison series of the pace chart.
type PaceCompare struct {
	Mode   string
	Month  *platform.Date // prev and yoy: the month compared with
	Months int            // avg3 and avg6: how many months the average has (fewer when the data starts later)
	Days   int            // how many days of Values to draw: the month's length, or 31 for averages
	Values []int64        // cumulative, 31 entries
}

// Compare builds the comparison for target from the daily spending of the months before it.
// Months before first (the first month with data) don't count: prev/yoy then have no comparison,
// and averages use the months there are (none: no comparison). Averages round to minor units.
func Compare(mode string, target, first platform.Date, days []query.DaySpend) *PaceCompare {
	switch mode {
	case ComparePrev, CompareYoY:
		mo := target.AddMonths(-CompareMonths(mode))
		if mo.Before(first) {
			return nil
		}
		return &PaceCompare{Mode: mode, Month: &mo, Days: DaysIn(mo), Values: Cumulative(days, mo)}
	case CompareAvg3, CompareAvg6:
		var arrs [][]int64
		for j := 1; j <= CompareMonths(mode); j++ {
			mo := target.AddMonths(-j)
			if mo.Before(first) {
				break
			}
			arrs = append(arrs, Cumulative(days, mo))
		}
		if len(arrs) == 0 {
			return nil
		}
		vals := make([]int64, 31)
		for d := range vals {
			var s int64
			for _, a := range arrs {
				s += a[d]
			}
			vals[d] = DivRound(s, int64(len(arrs)))
		}
		return &PaceCompare{Mode: mode, Months: len(arrs), Days: 31, Values: vals}
	}
	return nil
}

// LastDay is the last day of the target month the current line reaches: the day of the latest
// booking date when the data ends inside (or before) the month, else the whole month.
func LastDay(target, dataMax platform.Date) int {
	if dataMax.Before(target.AddMonths(1)) {
		return max(1, dataMax.D)
	}
	return DaysIn(target)
}

// Heatmap ranges (legacy heatRange).
const (
	RangePeriod = "period"
	Range3m     = "3m"
	Range6m     = "6m"
	Range12m    = "12m"
	RangeAll    = "all"
)

// HeatRange is the date interval [from, to) of the daily-spending calendar. "period" follows the
// period (without one: the first to the last row shown), clipped to the end of the data; "all" is
// the whole data range; "Nm" is the last N calendar months of the data. shown is the range of the
// rows the mode reads (scoped for "period", base otherwise); ok is false when there are none.
func HeatRange(mode string, period *query.Resolved, data, shown query.DateRange) (from, to platform.Date, ok bool) {
	if shown.Min == nil || shown.Max == nil || data.Min == nil || data.Max == nil {
		return platform.Date{}, platform.Date{}, false
	}
	end := data.Max.AddDays(1)
	switch mode {
	case RangePeriod:
		if period == nil {
			return *shown.Min, shown.Max.AddDays(1), true
		}
		return period.From, minDate(period.To, end), true
	case RangeAll:
		return *data.Min, end, true
	}
	n := map[string]int{Range3m: 3, Range6m: 6, Range12m: 12}[mode]
	return data.Max.FirstOfMonth().AddMonths(-(n - 1)), end, true
}

// Share is v / total, 0 when total is 0.
func Share(v, total int64) float64 {
	if total == 0 {
		return 0
	}
	return float64(v) / float64(total)
}

// InPeriod tells whether a cash-flow month is highlighted: months from the period's first month
// up to its end (legacy: t ≥ monthStart(from) && t < to); every month without a period.
func InPeriod(month platform.Date, period *query.Resolved) bool {
	if period == nil {
		return true
	}
	return !month.Before(period.From.FirstOfMonth()) && month.Before(period.To)
}

// FillMonths returns every month from the first to the last of months (which is sorted and has no
// duplicates), with empty months filled in.
func FillMonths(months []query.MonthFlow) []query.MonthFlow {
	if len(months) == 0 {
		return nil
	}
	var out []query.MonthFlow
	i := 0
	for m := months[0].Month; !m.After(months[len(months)-1].Month); m = m.AddMonths(1) {
		if i < len(months) && months[i].Month == m {
			out = append(out, months[i])
			i++
			continue
		}
		out = append(out, query.MonthFlow{Month: m})
	}
	return out
}

func minDate(a, b platform.Date) platform.Date {
	if a.Before(b) {
		return a
	}
	return b
}

func maxDate(a, b platform.Date) platform.Date {
	if a.After(b) {
		return a
	}
	return b
}
