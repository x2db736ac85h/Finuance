package insights_test

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/insights"
	"github.com/x2db736ac85h/finuance/backend/internal/investments"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
)

// The overview cases of the legacy oracle (testdata/oracle/overview/demo.json), with the view of
// each case repeated from tools/legacy-oracle/cases/overview.mjs.
type ovCase struct {
	month   string
	months  int
	filters map[string]string
	search  string
	budgets map[string]int64 // major units
	opening int64            // minor units
}

var ovCases = map[string]ovCase{
	"latest-partial": {month: "2025-03", months: 12},
	"full-month": {month: "2025-02", months: 12, budgets: map[string]int64{"Transport": 500, "Artykuły spożywcze": 900,
		"Zdrowie i uroda": 0}},
	"first-month":   {month: "2024-01", months: 12},
	"second-month":  {month: "2024-02", months: 6},
	"mid-year-6":    {month: "2024-06", months: 6, opening: 1250050},
	"year-end-24":   {month: "2024-12", months: 24},
	"default-month": {months: 36},
	"filtered":      {month: "2025-02", months: 12, filters: map[string]string{"Kategoria": "!Mieszkanie"}},
	"search":        {month: "2024-12", months: 12, search: "warszawa"},
	"expenses-only": {month: "2024-11", months: 12, filters: map[string]string{"Kwota operacji": "<0"}},
	"payee-filter":  {month: "2024-09", months: 12, filters: map[string]string{"Nadawca / Odbiorca": "market"}},
}

type ovOracle struct {
	Cases []struct {
		Name string `json:"name"`
		Ctx  struct {
			M, P       string
			Partial    bool    `json:"partial"`
			Elapsed    int     `json:"elapsed"`
			Dim        int     `json:"dim"`
			PrevToDate float64 `json:"prevToDate"`
			NAvg       int     `json:"nAvg"`
			Cur        struct{ Inc, Exp float64 }
			Last12     []struct {
				Month    string
				Inc, Exp float64
				N        int
			} `json:"last12"`
		} `json:"ctx"`
		Cards map[string]map[string]struct {
			Sub   string `json:"sub"`
			Table *twin  `json:"table"`
			Lines []struct {
				Kind string `json:"kind"`
				Text string `json:"text"`
			} `json:"lines"`
		} `json:"cards"`
	} `json:"cases"`
}

// fixed is JavaScript's x.toFixed(digits): halves round away from zero, "-0" stays signed.
func fixed(x float64, digits int) string {
	p := math.Pow(10, float64(digits))
	neg := x < 0 || (x == 0 && math.Signbit(x))
	v := math.Floor(math.Abs(x)*p+0.5) / p
	s := strconv.FormatFloat(v, 'f', digits, 64)
	if neg {
		return "-" + s
	}
	return s
}

func dayShort(d platform.Date) string { return d.Time().Format("Jan 2, 2006") }

func TestOverviewParity(t *testing.T) {
	e := newEnv(t)
	e.u = e.user()
	acc := e.importFile(e.u, "Demo", testdata(t, "fixtures/demo/demo-legacy.csv"))
	cats, err := query.New(e.st).Categories(e.ctx, e.u, nil)
	if err != nil {
		t.Fatal(err)
	}
	catID := map[string]uuid.UUID{}
	for _, c := range cats {
		catID[c.Name] = c.ID
	}
	var oracle ovOracle
	if err := json.Unmarshal(testdata(t, "oracle/overview/demo.json"), &oracle); err != nil {
		t.Fatal(err)
	}
	if len(oracle.Cases) != len(ovCases) {
		t.Fatalf("%d oracle cases, %d here", len(oracle.Cases), len(ovCases))
	}
	allowed := 0
	for _, oc := range oracle.Cases {
		t.Run(oc.Name, func(t *testing.T) {
			v, ok := ovCases[oc.Name]
			if !ok {
				t.Fatalf("no view for %q", oc.Name)
			}
			// budgets and the opening balance belong to the user: set them for this case only
			for name, major := range v.budgets {
				if err := e.l.SetBudget(e.ctx, e.u, catID[name], major*100); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := e.l.UpdateAccount(e.ctx, e.u, acc.ID, ledger.AccountInput{Name: "Demo", OpeningBalanceMinor: v.opening}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				for name := range v.budgets {
					_ = e.l.DeleteBudget(e.ctx, e.u, catID[name])
				}
			})
			c := view{search: v.search, filters: v.filters, period: "all"}.context(t)
			var month *platform.Date
			if v.month != "" {
				m, err := platform.ParseDate(v.month + "-01")
				if err != nil {
					t.Fatal(err)
				}
				month = &m
			}
			o, err := e.svc.Overview(e.ctx, e.u, c, month, v.months, investments.Empty())
			if err != nil {
				t.Fatal(err)
			}
			x := oc.Ctx
			if got := o.Month.String()[:7]; got != x.M || o.Prev.String()[:7] != x.P || o.Partial != x.Partial || o.Elapsed != x.Elapsed ||
				o.DaysInMonth != x.Dim || o.AvgMonths != x.NAvg || o.PrevToDateMinor != minorOf(x.PrevToDate) ||
				o.Cur.IncomeMinor != minorOf(x.Cur.Inc) || o.Cur.ExpenseMinor != minorOf(x.Cur.Exp) {
				t.Errorf("context: month %s prev %s partial %v elapsed %d/%d avg %d prevToDate %d cur %d/%d; legacy %+v",
					o.Month, o.Prev, o.Partial, o.Elapsed, o.DaysInMonth, o.AvgMonths, o.PrevToDateMinor, o.Cur.IncomeMinor, o.Cur.ExpenseMinor, x)
			}
			if len(o.Months) != len(x.Last12) {
				t.Errorf("months: %d, legacy %d", len(o.Months), len(x.Last12))
			}
			check := func(card, size string, got *twin, sub string, ties map[[2]int]string) {
				t.Helper()
				want := oc.Cards[card][size]
				for at, alt := range ties { // ALLOWED_DIFFS row 13
					r, c := at[0], at[1]
					if want.Table != nil && got != nil && r < len(want.Table.Rows) && r < len(got.Rows) &&
						want.Table.Rows[r][c] == alt && got.Rows[r][c] != alt {
						got.Rows[r][c] = alt
						allowed++
					}
				}
				if diff := cmp.Diff(want.Table, got); diff != "" {
					t.Errorf("%s (%s) table (-legacy +new):\n%s", card, size, diff)
				}
				if sub != want.Sub {
					t.Errorf("%s (%s) subtitle = %q, legacy %q", card, size, sub, want.Sub)
				}
			}
			for _, size := range []string{"card", "big"} {
				big := size == "big"
				tw, sub := flowTwin(o)
				check("o_flow", size, tw, sub, nil)
				tw, sub = budgetTwin(o)
				check("o_budget", size, tw, sub, nil)
				tw, sub = recurTwin(o)
				check("o_recur", size, tw, sub, nil)
				tw, sub = sankeyTwin(o, big)
				check("o_sankey", size, tw, sub, nil)
				tw, sub = topTwin(o, big)
				check("o_top", size, tw, sub, nil)
				tw, sub = savingsTwin(o)
				check("o_savings", size, tw, sub, nil)
				tw, sub, ties := weekTwin(o)
				check("o_week", size, tw, sub, ties)
				tw, sub = balanceTwin(o)
				check("o_balance", size, tw, sub, nil)
				var got []string
				for _, h := range o.Highlights {
					got = append(got, h.Tone+": "+sentence(h))
				}
				var want []string
				for _, l := range oc.Cards["o_high"][size].Lines {
					want = append(want, l.Kind+": "+l.Text)
				}
				if diff := cmp.Diff(want, got); diff != "" {
					t.Errorf("highlights (-legacy +new):\n%s", diff)
				}
			}
		})
	}
	t.Logf("half-cent ties accepted (ALLOWED_DIFFS row 13): %d", allowed)
}

func minorOf(major float64) int64 { return int64(math.Round(major * 100)) }

func monthLong(d platform.Date) string { return d.Time().Format("January 2006") }

// negMoney is the legacy money(-x) of a positive x: always signed, also for zero.
func negMoney(x int64) string { return "-" + money(x) }

func flowTwin(o insights.Overview) (*twin, string) {
	tw := &twin{Cols: []string{"Month", "In", "Out", "Net", "Savings rate"}}
	for i := len(o.Months) - 1; i >= 0; i-- {
		m := o.Months[i]
		rate := "–"
		if m.IncomeMinor > 0 {
			rate = fixed(float64(m.IncomeMinor-m.ExpenseMinor)/float64(m.IncomeMinor)*100, 0) + "%"
		}
		tw.Rows = append(tw.Rows, []string{monthLong(m.Month), money(m.IncomeMinor), negMoney(m.ExpenseMinor), money(m.IncomeMinor - m.ExpenseMinor), rate})
	}
	return tw, fmt.Sprintf("last %d months", len(o.Months))
}

var statusLabel = map[string]string{insights.StatusOK: "On track", insights.StatusAhead: "Ahead of pace", insights.StatusOver: "Over budget",
	insights.StatusNone: "No budget"}

func budgetTwin(o insights.Overview) (*twin, string) {
	sub := "set your own limits"
	if o.AvgMonths > 0 {
		sub = fmt.Sprintf("default = %d-month average", o.AvgMonths)
	}
	if len(o.Categories) == 0 {
		return nil, sub
	}
	tw := &twin{Cols: []string{"Category", "Spent", "Budget", "Left", "Status"}}
	for _, r := range o.Categories {
		tw.Rows = append(tw.Rows, []string{r.Name, money(r.CurMinor), money(r.BudgetMinor), money(r.BudgetMinor - r.CurMinor), statusLabel[r.Status]})
	}
	return tw, sub
}

func recurTwin(o insights.Overview) (*twin, string) {
	if len(o.Recurring) == 0 {
		return nil, ""
	}
	tw := &twin{Cols: []string{"Payee", "Category", "Typical", "Last", "Next expected", "Per year", "Status"}}
	n, m := 0, int64(0)
	for _, r := range o.Recurring {
		next, st := "–", "stopped?"
		if r.Active {
			next, st = dayShort(r.Next), "active"
			n++
			m += r.MedianMinor
		}
		tw.Rows = append(tw.Rows, []string{r.Payee, r.Category, negMoney(r.MedianMinor), dayShort(r.Last), next, negMoney(r.YearlyMinor), st})
	}
	return tw, fmt.Sprintf("%d active · ≈ %s/mo", n, money(m))
}

type node struct {
	k string
	v int64
}

// topN is the legacy top(): the n largest, then the rest as one entry when it is positive.
func topN(list []node, n int, other string) []node {
	if len(list) <= n {
		return list
	}
	var rest int64
	for _, x := range list[n:] {
		rest += x.v
	}
	out := append([]node(nil), list[:n]...)
	if rest > 0 {
		out = append(out, node{other, rest})
	}
	return out
}

func sankeyTwin(o insights.Overview, big bool) (*twin, string) {
	inc, exp := o.Cur.IncomeMinor, o.Cur.ExpenseMinor
	if inc == 0 && exp == 0 {
		return nil, ""
	}
	var src, cats []node
	for _, s := range o.IncomeSources {
		src = append(src, node{s.Payee, s.SpentMinor})
	}
	for _, c := range o.Categories {
		if c.CurMinor > 0 {
			cats = append(cats, node{c.Name, c.CurMinor})
		}
	}
	sortNodes(cats)
	nl, nr := 4, 7
	if big {
		nl, nr = 6, 10
	}
	L := topN(src, nl, "Other income")
	R := topN(cats, nr, "Other spending")
	if net := inc - exp; net < 0 {
		L = append(L, node{"From savings", -net})
	} else if net > 0 {
		R = append(R, node{"Saved", net})
	}
	T := max(inc, exp)
	tw := &twin{Cols: []string{"Side", "Item", "Amount", "Share"}}
	for _, n := range L {
		tw.Rows = append(tw.Rows, []string{"In", n.k, money(n.v), fixed(float64(n.v)/float64(T)*100, 1) + "%"})
	}
	for _, n := range R {
		tw.Rows = append(tw.Rows, []string{"Out", n.k, money(n.v), fixed(float64(n.v)/float64(T)*100, 1) + "%"})
	}
	return tw, monthLong(o.Month)
}

func sortNodes(ns []node) {
	for i := 1; i < len(ns); i++ { // stable insertion sort, largest first
		for j := i; j > 0 && ns[j].v > ns[j-1].v; j-- {
			ns[j], ns[j-1] = ns[j-1], ns[j]
		}
	}
}

func topTwin(o insights.Overview, big bool) (*twin, string) {
	n := 12
	if big {
		n = 40
	}
	list := o.Largest[:min(n, len(o.Largest))]
	if len(list) == 0 {
		return nil, ""
	}
	tw := &twin{Cols: []string{"Date", "Payee", "Category", "Amount"}}
	for _, x := range list {
		cat := ""
		if x.Category != nil {
			cat = x.Category.Name
		}
		tw.Rows = append(tw.Rows, []string{ddmmyyyy(x.BookedOn), x.Payee, cat, money(x.AmountMinor)})
	}
	return tw, monthLong(o.Month)
}

func savingsTwin(o insights.Overview) (*twin, string) {
	tw := &twin{Cols: []string{"Month", "Savings rate"}}
	var sum float64
	n := 0
	for i := len(o.Months) - 1; i >= 0; i-- {
		m := o.Months[i]
		v := "–"
		if m.IncomeMinor > 0 {
			r := math.Max(-100, math.Min(100, float64(m.IncomeMinor-m.ExpenseMinor)/float64(m.IncomeMinor)*100))
			v = fixed(r, 1) + "%"
			sum += r
			n++
		}
		tw.Rows = append(tw.Rows, []string{monthLong(m.Month), v})
	}
	if n == 0 {
		return nil, ""
	}
	return tw, "avg " + fixed(sum/float64(n), 0) + "%"
}

func weekTwin(o insights.Overview) (*twin, string, map[[2]int]string) {
	tw := &twin{Cols: []string{"Weekday", "Avg per day", "Total"}}
	ties := map[[2]int]string{}
	names := []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}
	for j, w := range o.Weekday {
		avg := int64(0)
		if w.Days > 0 {
			avg = insights.DivRound(w.TotalMinor, int64(w.Days))
			if 2*(w.TotalMinor%int64(w.Days)) == int64(w.Days) {
				ties[[2]int{j, 1}] = money(w.TotalMinor / int64(w.Days))
			}
		}
		tw.Rows = append(tw.Rows, []string{names[j], money(avg), money(w.TotalMinor)})
	}
	return tw, "avg / day · 3 months", ties
}

func balanceTwin(o insights.Overview) (*twin, string) {
	p := o.Balance.Points
	if len(p) < 2 {
		return nil, ""
	}
	tw := &twin{Cols: []string{"Day", "Balance (end of day)"}}
	for i := len(p) - 1; i >= 0; i-- {
		tw.Rows = append(tw.Rows, []string{dayShort(p[i].Date), money(p[i].BalanceMinor)})
	}
	return tw, "now " + money(o.Balance.NowMinor)
}

// sentence writes a highlight as the legacy o_high does (en-US, money with the currency).
func sentence(h insights.Highlight) string {
	switch h.Kind {
	case insights.HLProjection:
		return fmt.Sprintf("Projected month-end spending %s — %d of %d days in.", money(h.Amount), h.Days, h.Of)
	case insights.HLVsPrev:
		dir, same := "down", ""
		if h.Tone == insights.ToneUp {
			dir = "up"
		}
		if h.SameDays {
			same = " at the same point"
		}
		return fmt.Sprintf("Spending is %s %s%% vs %s%s (%s vs %s).", dir, fixed(h.Pct, 0), h.Month.Time().Format("Jan"), same, money(h.Amount), money(h.Other))
	case insights.HLVsAverage:
		dir := "Below"
		if h.Tone == insights.ToneUp {
			dir = "Above"
		}
		return fmt.Sprintf("%s your %d-month average by %s.", dir, h.Months, money(h.Amount))
	case insights.HLTopCategory:
		return fmt.Sprintf("%s is the biggest category — %s%% of spending.", h.Category, fixed(h.Pct, 0))
	case insights.HLCategoryUp:
		return fmt.Sprintf("%s is %s above its usual level.", h.Category, money(h.Amount))
	case insights.HLCategoryDn:
		return fmt.Sprintf("%s is %s below its usual level.", h.Category, money(h.Amount))
	case insights.HLSavings:
		return fmt.Sprintf("You kept %s%% of income (%d-month average %s%%).", fixed(h.Pct, 0), h.Months, fixed(h.AvgPct, 0))
	case insights.HLOverspend:
		return fmt.Sprintf("You spent %s more than you earned.", money(h.Amount))
	case insights.HLNoIncome:
		return "No income recorded this month."
	case insights.HLNoSpendDays:
		s := "s"
		if h.Days == 1 {
			s = ""
		}
		return fmt.Sprintf("%d no-spend day%s so far this month.", h.Days, s)
	case insights.HLRecurring:
		return fmt.Sprintf("%d recurring payments ≈ %s/month (%s/year).", h.Count, money(h.Amount), money(h.Other))
	case insights.HLLargest:
		return fmt.Sprintf("Largest expense: %s at %s.", money(h.Amount), h.Payee)
	}
	return "?" + h.Kind
}
