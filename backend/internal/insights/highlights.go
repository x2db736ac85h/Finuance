package insights

import (
	"cmp"
	"math"
	"slices"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
)

// Highlights of the overview (legacy o_high): one rule per sentence, in the legacy order. The
// server decides which sentences apply and with which numbers; the browser writes the words
// (PLAN §4.6), so the wording lives in one place.

// Highlight kinds.
const (
	HLProjection  = "projection"  // Amount (projected spending), Days of Of
	HLVsPrev      = "vsPrev"      // Pct, Amount (this month), Other (compared with), Month, SameDays
	HLVsAverage   = "vsAverage"   // Amount (difference), Months; Tone up: above, down: below
	HLTopCategory = "topCategory" // Category, Pct
	HLCategoryUp  = "categoryUp"  // Category, Amount above its usual level
	HLCategoryDn  = "categoryDown"
	HLSavings     = "savings"     // Pct (kept), AvgPct, Months
	HLOverspend   = "overspend"   // Amount spent beyond income
	HLNoIncome    = "noIncome"    //
	HLNoSpendDays = "noSpendDays" // Days
	HLRecurring   = "recurring"   // Count, Amount (per month), Other (per year)
	HLLargest     = "largest"     // Amount (negative), Payee
)

// Highlight tones (the legacy icon kinds; the icon always comes with the text).
const (
	ToneInfo = "info"
	ToneUp   = "up"
	ToneDown = "down"
	ToneGood = "good"
	ToneWarn = "warn"
)

// Highlight is one sentence's kind, tone and numbers.
type Highlight struct {
	Kind     string
	Tone     string
	Amount   int64
	Other    int64
	Pct      float64
	AvgPct   float64
	Days     int
	Of       int
	Months   int
	Count    int
	Category string
	Payee    string
	Month    platform.Date
	SameDays bool
}

func highlights(o *Overview, v *monthView, cats []query.MonthCategory, days []query.DaySpend, unit int64) []Highlight {
	var out []Highlight
	add := func(h *Highlight) {
		if h != nil {
			out = append(out, *h)
		}
	}
	add(hlProjection(o))
	add(hlVsPrev(o))
	add(hlVsAverage(o, v))
	add(hlTopCategory(o))
	up, down := hlCategoryChanges(v, cats, unit)
	add(up)
	add(down)
	add(hlSavings(o))
	add(hlNoSpendDays(o, days))
	add(hlRecurring(o))
	add(hlLargest(o))
	return out
}

func hlProjection(o *Overview) *Highlight {
	if o.ProjectedMinor == nil {
		return nil
	}
	return &Highlight{Kind: HLProjection, Tone: ToneInfo, Amount: *o.ProjectedMinor, Days: o.Elapsed, Of: o.DaysInMonth}
}

// hlVsPrev compares with the previous month (up to the same day while the month is in progress);
// changes under 1 % are left out.
func hlVsPrev(o *Overview) *Highlight {
	cmpTo := o.PrevFlow.ExpenseMinor
	if o.Partial {
		cmpTo = o.PrevToDateMinor
	}
	if cmpTo <= 0 {
		return nil
	}
	d := float64(o.Cur.ExpenseMinor-cmpTo) / float64(cmpTo) * 100
	if math.Abs(d) < 1 {
		return nil
	}
	return &Highlight{Kind: HLVsPrev, Tone: upDown(d), Pct: math.Abs(d), Amount: o.Cur.ExpenseMinor, Other: cmpTo, Month: o.Prev, SameDays: o.Partial}
}

// hlVsAverage compares with the average of up to three earlier months, scaled to the days elapsed;
// differences under 5 % are left out.
func hlVsAverage(o *Overview, v *monthView) *Highlight {
	if v.nAvg == 0 || o.Cur.ExpenseMinor == 0 {
		return nil
	}
	// a = average × elapsed / days, kept as the fraction aNum / den
	aNum, den := v.avgExp*int64(v.elapsed), int64(v.nAvg)*int64(v.dim)
	if aNum == 0 {
		return nil
	}
	diff := o.Cur.ExpenseMinor*den - aNum
	d := float64(diff) / float64(aNum) * 100
	if math.Abs(d) < 5 {
		return nil
	}
	return &Highlight{Kind: HLVsAverage, Tone: upDown(d), Amount: DivRound(abs(diff), den), Months: v.nAvg}
}

func hlTopCategory(o *Overview) *Highlight {
	if o.Cur.ExpenseMinor == 0 {
		return nil
	}
	var top *BudgetRow
	for i := range o.Categories {
		c := &o.Categories[i]
		if c.CurMinor > 0 && (top == nil || c.CurMinor > top.CurMinor) {
			top = c
		}
	}
	if top == nil {
		return nil
	}
	return &Highlight{Kind: HLTopCategory, Tone: ToneInfo, Category: top.Name, Pct: float64(top.CurMinor) / float64(o.Cur.ExpenseMinor) * 100}
}

// hlCategoryChanges finds the categories furthest above and below their usual level (the earlier
// months' average scaled to the days elapsed), beyond max(50 units, 2 % of the average spending).
func hlCategoryChanges(v *monthView, cats []query.MonthCategory, unit int64) (up, down *Highlight) {
	if v.nAvg == 0 {
		return nil, nil
	}
	type delta struct {
		name string
		num  int64 // (cur × nAvg × dim − sum3 × elapsed); the delta is num / den
	}
	den := int64(v.nAvg) * int64(v.dim)
	idx := map[catKey]int{}
	var ds []delta
	var sums []int64
	var curs []int64
	for _, c := range cats {
		var inCur, inAvg bool
		switch {
		case c.Month == v.m:
			inCur = true
		case !c.Month.Before(v.m.AddMonths(-v.nAvg)) && c.Month.Before(v.m):
			inAvg = true
		default:
			continue
		}
		k := keyOf(c.ID)
		i, ok := idx[k]
		if !ok {
			name := Uncategorized
			if c.Name != nil {
				name = *c.Name
			}
			i = len(ds)
			idx[k] = i
			ds = append(ds, delta{name: name})
			sums = append(sums, 0)
			curs = append(curs, 0)
		}
		if inCur {
			curs[i] += c.SpentMinor
		}
		if inAvg {
			sums[i] += c.SpentMinor
		}
	}
	for i := range ds {
		ds[i].num = curs[i]*den - sums[i]*int64(v.elapsed)
	}
	if len(ds) == 0 {
		return nil, nil
	}
	slices.SortStableFunc(ds, func(x, y delta) int { return cmp.Compare(y.num, x.num) })
	// threshold = max(50 units, 2 % of the average monthly spending), compared as fractions of den
	limit := math.Max(float64(50*unit), float64(v.avgExp)/float64(v.nAvg)*0.02) * float64(den)
	if f := ds[0]; float64(f.num) > limit {
		up = &Highlight{Kind: HLCategoryUp, Tone: ToneUp, Category: f.name, Amount: DivRound(f.num, den)}
	}
	if l := ds[len(ds)-1]; float64(l.num) < -limit {
		down = &Highlight{Kind: HLCategoryDn, Tone: ToneDown, Category: l.name, Amount: DivRound(-l.num, den)}
	}
	return up, down
}

// hlSavings: the share of income kept with the average over the months shown, or the overspend,
// or that there was no income.
func hlSavings(o *Overview) *Highlight {
	inc, exp := o.Cur.IncomeMinor, o.Cur.ExpenseMinor
	if inc <= 0 {
		if exp != 0 {
			return &Highlight{Kind: HLNoIncome, Tone: ToneWarn}
		}
		return nil
	}
	if exp > inc {
		return &Highlight{Kind: HLOverspend, Tone: ToneWarn, Amount: exp - inc}
	}
	var sum float64
	n := 0
	for _, m := range o.Months {
		if m.IncomeMinor > 0 {
			sum += float64(m.IncomeMinor-m.ExpenseMinor) / float64(m.IncomeMinor) * 100
			n++
		}
	}
	return &Highlight{Kind: HLSavings, Tone: ToneGood, Pct: float64(inc-exp) / float64(inc) * 100, AvgPct: sum / float64(max(n, 1)),
		Months: len(o.Months)}
}

// hlNoSpendDays counts the days elapsed without an expense.
func hlNoSpendDays(o *Overview, days []query.DaySpend) *Highlight {
	end := o.Month.AddMonths(1)
	spent := 0
	for _, d := range days {
		if !d.Date.Before(o.Month) && d.Date.Before(end) && d.Date.D <= o.Elapsed {
			spent++
		}
	}
	n := o.Elapsed - spent
	tone := ToneInfo
	if n > 0 {
		tone = ToneGood
	}
	return &Highlight{Kind: HLNoSpendDays, Tone: tone, Days: n}
}

func hlRecurring(o *Overview) *Highlight {
	var n int
	var m int64
	for _, r := range o.Recurring {
		if r.Active {
			n++
			m += r.MedianMinor
		}
	}
	if n == 0 {
		return nil
	}
	return &Highlight{Kind: HLRecurring, Tone: ToneInfo, Count: n, Amount: m, Other: m * 12}
}

func hlLargest(o *Overview) *Highlight {
	if len(o.Largest) == 0 {
		return nil
	}
	x := o.Largest[0]
	return &Highlight{Kind: HLLargest, Tone: ToneInfo, Amount: x.AmountMinor, Payee: x.PayeeGroup}
}

func upDown(d float64) string {
	if d > 0 {
		return ToneUp
	}
	return ToneDown
}
