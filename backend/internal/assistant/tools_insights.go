package assistant

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/ingest"
	"github.com/x2db736ac85h/finuance/backend/internal/insights"
	"github.com/x2db736ac85h/finuance/backend/internal/investments"
	"github.com/x2db736ac85h/finuance/backend/internal/money"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
)

// The insight tools of PLAN task 15.8: budget questions and group (trip) summaries without row
// lookups. Like the others they only read, under the user's read-only RLS context.

type budgetResult struct {
	Currency    string      `json:"currency"`
	Month       string      `json:"month"`
	Partial     bool        `json:"partial"`
	DaysElapsed int         `json:"days_elapsed,omitempty"`
	DaysInMonth int         `json:"days_in_month"`
	Spent       json.Number `json:"spent"`
	Budget      json.Number `json:"budget"`
	Left        json.Number `json:"left"`
	Over        int         `json:"over_budget"`
	Ahead       int         `json:"ahead_of_pace"`
	OnTrack     int         `json:"on_track"`
	Categories  []budgetCat `json:"categories"`
	Omitted     int         `json:"omitted_categories"`
	Note        string      `json:"note"`
}

type budgetCat struct {
	Category string      `json:"category"`
	Spent    json.Number `json:"spent"`
	Budget   json.Number `json:"budget"`
	Left     json.Number `json:"left"`
	Default  bool        `json:"budget_is_default,omitempty"`
	Status   string      `json:"status"`
}

// budgetCategories caps budget_status.
const budgetCategories = 20

// statusRank orders budget_status: over budget first, then ahead of pace, then on track.
var statusRank = map[string]int{insights.StatusOver: 0, insights.StatusAhead: 1, insights.StatusOK: 2, insights.StatusNone: 3}

func monthArg(a args) *platform.Date {
	if m := monthRe.FindStringSubmatch(a.str("month")); m != nil {
		return query.ParseToolDate(m[0] + "-01")
	}
	return nil
}

func (t *Tools) budgets(ctx context.Context, userID uuid.UUID, sc Scope, a args) (Outcome, error) {
	ov, err := t.ins.Overview(ctx, userID, sc.Context, monthArg(a), 6, investments.Doc{})
	if err != nil {
		return Outcome{}, err
	}
	if ov.Empty {
		return outcome(map[string]string{"error": "No dated transactions in scope"}, "budget status · no data"), nil
	}
	cur := ov.Currency
	r := budgetResult{Currency: cur, Month: ov.Month.String()[:7], Partial: ov.Partial, DaysInMonth: ov.DaysInMonth,
		Categories: []budgetCat{},
		Note:       "A budget without budget_is_default was set by the user; a default budget is the rounded average of up to 3 earlier months. ahead = spending faster than the month's pace."}
	if ov.Partial {
		r.DaysElapsed = ov.Elapsed
	}
	rows := slices.Clone(ov.Categories)
	slices.SortStableFunc(rows, func(x, y insights.BudgetRow) int {
		return cmp.Or(cmp.Compare(statusRank[x.Status], statusRank[y.Status]), cmp.Compare(y.CurMinor, x.CurMinor))
	})
	var spent, budget int64
	for i := range rows {
		c := &rows[i]
		if c.BudgetMinor == 0 && c.CurMinor == 0 {
			continue
		}
		spent += c.CurMinor
		budget += c.BudgetMinor
		switch c.Status {
		case insights.StatusOver:
			r.Over++
		case insights.StatusAhead:
			r.Ahead++
		case insights.StatusOK:
			r.OnTrack++
		}
		if len(r.Categories) == budgetCategories {
			r.Omitted++
			continue
		}
		name := c.Name
		if c.ID == nil {
			name = "(uncategorized)"
		}
		r.Categories = append(r.Categories, budgetCat{Category: name, Spent: amount(c.CurMinor, cur), Budget: amount(c.BudgetMinor, cur),
			Left: amount(c.BudgetMinor-c.CurMinor, cur), Default: c.BudgetIsDefault, Status: c.Status})
	}
	r.Spent, r.Budget, r.Left = amount(spent, cur), amount(budget, cur), amount(budget-spent, cur)
	return outcome(r, fmt.Sprintf("budget status · %s · %d over", r.Month, r.Over)), nil
}

type groupResult struct {
	Group        string       `json:"group"`
	Currency     string       `json:"currency"`
	Transactions int64        `json:"transactions"`
	Spent        json.Number  `json:"spent"`
	Income       json.Number  `json:"income"`
	Net          json.Number  `json:"net"`
	FirstDay     string       `json:"first_day,omitempty"`
	LastDay      string       `json:"last_day,omitempty"`
	Days         int          `json:"days"`
	SpentPerDay  json.Number  `json:"spent_per_day"`
	Budget       *json.Number `json:"budget,omitempty"`
	BudgetLeft   *json.Number `json:"budget_left,omitempty"`
	Categories   []groupCat   `json:"categories"`
	TopPayees    []groupPayee `json:"top_payees"`
	Note         string       `json:"note"`
}

type groupCat struct {
	Category string      `json:"category"`
	Spent    json.Number `json:"spent"`
	Share    json.Number `json:"share_percent"`
}

type groupPayee struct {
	Payee   string      `json:"payee"`
	Spent   json.Number `json:"spent"`
	Charges int64       `json:"charges"`
}

// groupCategories caps group_summary's categories.
const groupCategories = 10

// findGroup resolves a group name: equal ignoring case, accents and spacing, else the only group
// whose name contains it. ok is false with the candidates when that is ambiguous or nothing.
func findGroup(groups []query.GroupInfo, name string) (g query.GroupInfo, candidates []string, ok bool) {
	key := ingest.Collapse(ingest.Norm(name))
	var hits []query.GroupInfo
	for i := range groups {
		n := ingest.Collapse(ingest.Norm(groups[i].Name))
		if n == key {
			return groups[i], nil, true
		}
		if key != "" && strings.Contains(n, key) {
			hits = append(hits, groups[i])
		}
	}
	if len(hits) == 1 {
		return hits[0], nil, true
	}
	if len(hits) == 0 {
		hits = groups
	}
	for i := range hits {
		candidates = append(candidates, hits[i].Name)
	}
	return query.GroupInfo{}, candidates, false
}

// percent is part / total in percent with one decimal ("42.5"), rounded half away from zero,
// without a float; "0" when total is 0.
func percent(part, total int64) json.Number {
	if total == 0 {
		return "0"
	}
	tenths := insights.DivRound(part*1000, total)
	return json.Number(strings.TrimSuffix(strings.TrimSuffix(money.FormatMinor(tenths, 1), "0"), "."))
}

func (t *Tools) group(ctx context.Context, userID uuid.UUID, sc Scope, a args) (Outcome, error) {
	name := a.str("group")
	if name == "" {
		return outcome(map[string]string{"error": "Give the group name (find_values with field group lists them)"}, "group summary · no group"), nil
	}
	groups, err := t.eng.Groups(ctx, userID)
	if err != nil {
		return Outcome{}, err
	}
	g, candidates, ok := findGroup(groups, name)
	if !ok {
		if len(candidates) > 20 {
			candidates = candidates[:20]
		}
		return outcome(map[string]any{"error": fmt.Sprintf("No single group named %q", name), "groups": candidates},
			"group summary · no group "+name), nil
	}
	rep, err := t.ins.GroupReport(ctx, userID, g.ID)
	if err != nil {
		return Outcome{}, err
	}
	cur := g.Currency
	if cur == "" {
		cur = emptyGroupCurrency
	}
	r := groupResult{Group: g.Name, Currency: cur, Transactions: g.Count, Spent: amount(g.SpentMinor, cur), Income: amount(g.IncomeMinor, cur),
		Net: amount(g.IncomeMinor-g.SpentMinor, cur), Days: rep.Days, SpentPerDay: amount(rep.AvgPerDayMinor, cur),
		Categories: []groupCat{}, TopPayees: []groupPayee{},
		Note: "All accounts, transfers included (like the group window)."}
	if g.FirstOn != nil && g.LastOn != nil {
		r.FirstDay, r.LastDay = g.FirstOn.String(), g.LastOn.String()
	}
	if g.BudgetMinor != nil {
		b, left := amount(*g.BudgetMinor, cur), amount(*g.BudgetMinor-g.SpentMinor, cur)
		r.Budget, r.BudgetLeft = &b, &left
	}
	for _, c := range rep.Categories[:min(groupCategories, len(rep.Categories))] {
		n := "(uncategorized)"
		if c.Name != nil {
			n = *c.Name
		}
		r.Categories = append(r.Categories, groupCat{Category: n, Spent: amount(c.SpentMinor, cur), Share: percent(c.SpentMinor, rep.SpentMinor)})
	}
	for _, p := range rep.Payees {
		r.TopPayees = append(r.TopPayees, groupPayee{Payee: sc.mask(p.Payee), Spent: amount(p.SpentMinor, cur), Charges: p.Count})
	}
	return outcome(r, "group summary · "+g.Name), nil
}

// emptyGroupCurrency is the currency of an empty group (no transactions to take it from), as in
// the group stats.
const emptyGroupCurrency = "PLN"
