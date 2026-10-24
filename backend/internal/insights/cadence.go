package insights

import (
	"slices"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
)

// Cadences of a recurring payment (PLAN 15.4). Monthly is the legacy findRecurring; weekly and
// yearly are new and only look at payee groups the monthly rules did not take.
const (
	CadenceMonthly = "monthly"
	CadenceWeekly  = "weekly"
	CadenceYearly  = "yearly"
)

// The weekly and yearly rules, in the spirit of the monthly ones: enough charges, regular
// intervals (75 % of them in the window, the median interval near the cadence), 75 % of the
// amounts within ±30 % of the median amount, and active while the next charge isn't overdue by
// much more than one interval.
const (
	weeklyMinCharges  = 6
	weeklyActiveDays  = 14
	yearlyMinCharges  = 2
	yearlyActiveDays  = 400
	weeksPerYear      = 52
	weeklyIntervalMin = 4
	weeklyIntervalMax = 10
	weeklyMedianMin   = 5
	weeklyMedianMax   = 9
	yearlyIntervalMin = 330
	yearlyIntervalMax = 400
)

// WithCadences adds the weekly and yearly payments found among others (charges sorted by payee
// group, then date) to the monthly list, skipping payee groups it already has, and sorts the
// whole list like FindRecurring (active first, then yearly cost). With no weekly or yearly
// payment the monthly list comes back unchanged.
func WithCadences(monthly []Recurring, others []query.Charge, dataMax platform.Date) []Recurring {
	seen := make(map[string]bool, len(monthly))
	for i := range monthly {
		seen[monthly[i].Payee] = true
	}
	out := monthly
	added := false
	for i := 0; i < len(others); {
		j := i
		for j < len(others) && others[j].PayeeGroup == others[i].PayeeGroup {
			j++
		}
		if list := others[i:j]; !seen[list[0].PayeeGroup] {
			if r, ok := cadenceOf(list, dataMax); ok {
				if !added {
					out = slices.Clone(monthly)
					added = true
				}
				out = append(out, r)
			}
		}
		i = j
	}
	if added {
		sortRecurring(out)
	}
	return out
}

// cadenceOf tries the weekly rules, then the yearly ones.
func cadenceOf(list []query.Charge, dataMax platform.Date) (Recurring, bool) {
	if len(list) < yearlyMinCharges {
		return Recurring{}, false
	}
	med, ok := typicalAmount(list)
	if !ok {
		return Recurring{}, false
	}
	gaps := make([]int, len(list)-1)
	for i := 1; i < len(list); i++ {
		gaps[i-1] = list[i-1].BookedOn.DaysUntil(list[i].BookedOn)
	}
	last := list[len(list)-1].BookedOn
	r := Recurring{Payee: list[0].PayeeGroup, Category: topCategory(list), MedianMinor: med, Last: last, Count: len(list)}
	switch {
	case len(list) >= weeklyMinCharges && regular(gaps, weeklyIntervalMin, weeklyIntervalMax) &&
		between(medianInt(gaps), weeklyMedianMin, weeklyMedianMax):
		r.Cadence, r.Next, r.YearlyMinor = CadenceWeekly, last.AddDays(7), med*weeksPerYear
		r.Active = last.DaysUntil(dataMax) <= weeklyActiveDays
	case regular(gaps, yearlyIntervalMin, yearlyIntervalMax):
		r.Cadence, r.YearlyMinor = CadenceYearly, med
		r.Next = last.AddMonths(12)
		r.Active = last.DaysUntil(dataMax) <= yearlyActiveDays
	default:
		return Recurring{}, false
	}
	return r, true
}

// typicalAmount is the median amount (the upper one, as the monthly rules) when at least 75 % of
// the amounts lie within ±30 % of it.
func typicalAmount(list []query.Charge) (int64, bool) {
	am := make([]int64, len(list))
	for i, c := range list {
		am[i] = c.SpentMinor
	}
	slices.Sort(am)
	med := am[len(am)/2]
	near := 0
	for _, a := range am {
		if abs(a-med)*10 <= med*3 {
			near++
		}
	}
	return med, near*4 >= len(am)*3 // near / n ≥ 0.75, in integers
}

// regular reports whether at least 75 % of the intervals lie in [lo, hi] days.
func regular(gaps []int, lo, hi int) bool {
	in := 0
	for _, g := range gaps {
		if between(g, lo, hi) {
			in++
		}
	}
	return len(gaps) > 0 && in*4 >= len(gaps)*3
}

func medianInt(xs []int) int {
	s := slices.Clone(xs)
	slices.Sort(s)
	return s[len(s)/2]
}

func between(x, lo, hi int) bool { return x >= lo && x <= hi }
