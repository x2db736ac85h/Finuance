package assistant

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/goals"
	"github.com/x2db736ac85h/finuance/backend/internal/ingest"
	"github.com/x2db736ac85h/finuance/backend/internal/insights"
	"github.com/x2db736ac85h/finuance/backend/internal/investments"
	"github.com/x2db736ac85h/finuance/backend/internal/money"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
)

// The tool names (legacy AI_TOOLS, two overview tools, and the budget and group tools of 15.8).
const (
	ToolSearch    = "search_transactions"
	ToolAggregate = "aggregate_transactions"
	ToolValues    = "find_values"
	ToolMonth     = "month_overview"
	ToolRecurring = "recurring_payments"
	ToolBudgets   = "budget_status"
	ToolGroup     = "group_summary"
	ToolPropose   = "propose_action"
)

// Scope is what a chat's lookups may read: every account, or the table view the user asked from
// (its accounts, filters, search and period). The tools add their own filter on top.
type Scope struct {
	Context query.Context
	View    bool // the current table view (prompt wording)
	// MaxRows caps search_transactions (legacy S.ai.maxRows, at most MaxRowsLimit).
	MaxRows int
	// MultiAccount adds each row's account name to search results.
	MultiAccount bool
	// Mask shortens digit runs of six or more in tool results (cloud connections).
	Mask bool
}

// Outcome is one executed tool call: the JSON the model receives, the step summary, and for
// search and aggregate the filter that "Show in table" applies (PLAN §4.8).
type Outcome struct {
	JSON     string
	Summary  string
	Filter   *query.ToolFilter
	Matched  *int
	Proposal *Proposal // propose_action: what the confirmation card shows and applies
}

// Tools runs the read-only lookups. Every call is one read-only transaction under the user's RLS
// context (query.Engine.Insight → store.WithUserReadOnly).
type Tools struct {
	eng   *query.Engine
	ins   *insights.Service
	goals *goals.Service // goal_status and the goal proposals (WithGoals)
}

// NewTools returns the tools on eng.
func NewTools(eng *query.Engine) *Tools { return &Tools{eng: eng, ins: insights.New(eng)} }

// Run executes one call. Bad arguments are answered in the result, as the legacy did; the error
// is for failures the model can't fix (the database, a cancelled run).
func (t *Tools) Run(ctx context.Context, userID uuid.UUID, sc Scope, name, rawArgs string) (Outcome, error) {
	a := parseArgs(rawArgs)
	var (
		out Outcome
		err error
	)
	switch name {
	case ToolSearch:
		out, err = t.search(ctx, userID, sc, a)
	case ToolAggregate:
		out, err = t.aggregate(ctx, userID, sc, a)
	case ToolValues:
		out, err = t.values(ctx, userID, sc, a)
	case ToolMonth:
		out, err = t.month(ctx, userID, sc, a)
	case ToolRecurring:
		out, err = t.recurring(ctx, userID, sc, a)
	case ToolBudgets:
		out, err = t.budgets(ctx, userID, sc, a)
	case ToolGroup:
		out, err = t.group(ctx, userID, sc, a)
	case ToolGoals:
		out, err = t.goalStatus(ctx, userID, a)
	case ToolPropose:
		out, err = t.propose(ctx, userID, sc, a)
	default:
		return outcome(map[string]string{"error": "Unknown tool " + name}, "unknown tool "+name), nil
	}
	var ve *platform.ValidationError
	if errors.As(err, &ve) {
		return outcome(map[string]string{"error": ve.Error()}, name+" · invalid arguments"), nil
	}
	return out, err
}

func outcome(v any, summary string) Outcome {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false) // "H&M" stays readable for the model (JSON.stringify doesn't escape either)
	if err := enc.Encode(v); err != nil {
		return Outcome{JSON: `{"error":"result could not be encoded"}`, Summary: summary}
	}
	return Outcome{JSON: strings.TrimSuffix(b.String(), "\n"), Summary: summary}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// sums are the legacy totals of a result (total_spent, total_income, net).
type sums struct {
	TotalSpent  json.Number `json:"total_spent"`
	TotalIncome json.Number `json:"total_income"`
	Net         json.Number `json:"net"`
}

func sumsOf(t query.Totals, cur string) sums {
	return sums{amount(t.SpentMinor, cur), amount(t.IncomeMinor, cur), amount(t.IncomeMinor-t.SpentMinor, cur)}
}

// amount writes minor units as the shortest decimal JSON number in major units ("-98.07",
// "-27.4", "141197"), the legacy's r2() numbers without going through a float.
func amount(minor int64, currency string) json.Number {
	exp, _ := money.Exponent(currency)
	s := money.FormatMinor(minor, exp)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	if s == "-0" {
		s = "0"
	}
	return json.Number(s)
}

type searchResult struct {
	Currency  string `json:"currency"`
	Matched   int    `json:"matched"`
	Returned  int    `json:"returned"`
	Truncated bool   `json:"truncated"`
	sums
	Rows []toolRow `json:"rows"`
}

type toolRow struct {
	ID       int         `json:"id"`
	Date     string      `json:"date"`
	Amount   json.Number `json:"amount"`
	Payee    string      `json:"payee,omitempty"`
	Category string      `json:"category,omitempty"`
	Title    string      `json:"title,omitempty"`
	Kind     string      `json:"kind,omitempty"`
	Group    string      `json:"group,omitempty"`
	Account  string      `json:"account,omitempty"`
}

func (t *Tools) search(ctx context.Context, userID uuid.UUID, sc Scope, a args) (Outcome, error) {
	tf := a.filter()
	c := sc.Context
	c.ToolFilter = &tf
	limit := a.count("limit", 25, max(1, min(sc.MaxRows, MaxRowsLimit)))
	var res searchResult
	err := t.eng.Insight(ctx, userID, c, func(in *query.Insight) error {
		cur, err := in.Currency()
		if err != nil {
			return err
		}
		tot, err := in.Totals(true)
		if err != nil {
			return err
		}
		rows, err := in.ToolRows(a.or("sort", "date_desc"), limit)
		if err != nil {
			return err
		}
		res = searchResult{Currency: cur, Matched: int(tot.Count), Returned: len(rows), Truncated: int(tot.Count) > len(rows),
			sums: sumsOf(tot, cur), Rows: make([]toolRow, 0, len(rows))}
		for i := range rows {
			r := &rows[i]
			row := toolRow{ID: i, Date: r.BookedOn.String(), Amount: amount(r.AmountMinor, r.Currency),
				Payee: sc.mask(ingest.Collapse(r.Payee)), Category: r.Category, Title: sc.mask(clip(ingest.Collapse(r.Description), 80)),
				Kind: sc.mask(r.TxType), Group: r.Group}
			if sc.MultiAccount {
				row.Account = r.Account
			}
			res.Rows = append(res.Rows, row)
		}
		return nil
	})
	if err != nil {
		return Outcome{}, err
	}
	out := outcome(res, fmt.Sprintf("%s → %d %s", tf.Label, res.Matched, plural(res.Matched, "match", "matches")))
	out.Filter, out.Matched = &tf, &res.Matched
	return out, nil
}

type aggregateResult struct {
	Currency string `json:"currency"`
	GroupBy  string `json:"group_by"`
	Matched  int    `json:"matched"`
	sums
	Groups  []groupOut `json:"groups"`
	Omitted int        `json:"omitted_groups"`
}

type groupOut struct {
	Key    string      `json:"key"`
	Count  int64       `json:"count"`
	Spent  json.Number `json:"spent"`
	Income json.Number `json:"income"`
	Net    json.Number `json:"net"`
}

// Weekdays are the weekday keys, Monday first (legacy WD).
var Weekdays = [7]string{"Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"}

func timeKey(by string) bool { return by == "month" || by == "week" || by == "day" }

func (t *Tools) aggregate(ctx context.Context, userID uuid.UUID, sc Scope, a args) (Outcome, error) {
	tf := a.filter()
	c := sc.Context
	c.ToolFilter = &tf
	by := a.or("group_by", "none")
	var res aggregateResult
	err := t.eng.Insight(ctx, userID, c, func(in *query.Insight) error {
		cur, err := in.Currency()
		if err != nil {
			return err
		}
		tot, err := in.Totals(true)
		if err != nil {
			return err
		}
		groups, err := in.ToolGroups(by)
		if err != nil {
			return err
		}
		res = aggregateResult{Currency: cur, GroupBy: by, Matched: int(tot.Count), sums: sumsOf(tot, cur)}
		groups = sortGroups(by, groups)
		def := 20
		if timeKey(by) {
			def = 60
		}
		top := a.count("top", def, 120)
		res.Omitted = max(0, len(groups)-top)
		if timeKey(by) {
			groups = groups[max(0, len(groups)-top):]
		} else {
			groups = groups[:min(top, len(groups))]
		}
		res.Groups = make([]groupOut, 0, len(groups))
		for _, g := range groups {
			res.Groups = append(res.Groups, groupOut{Key: sc.mask(groupKey(by, g.Key)), Count: g.Count, Spent: amount(g.SpentMinor, cur),
				Income: amount(g.IncomeMinor, cur), Net: amount(g.IncomeMinor-g.SpentMinor, cur)})
		}
		return nil
	})
	if err != nil {
		return Outcome{}, err
	}
	out := outcome(res, fmt.Sprintf("by %s · %s → %d rows, %d %s", by, tf.Label, res.Matched, len(res.Groups), plural(len(res.Groups), "group", "groups")))
	out.Filter, out.Matched = &tf, &res.Matched
	return out, nil
}

// groupKey names a group as the legacy did.
func groupKey(by, k string) string {
	switch by {
	case "category":
		if k == "" {
			return "(uncategorized)"
		}
	case "group":
		if k == "" {
			return "(no group)"
		}
	case "week":
		return "week of " + k
	case "weekday":
		if n, err := strconv.Atoi(k); err == nil && n >= 1 && n <= 7 {
			return Weekdays[n-1]
		}
	}
	return k
}

// sortGroups orders groups as the legacy did: time keys ascending, weekdays Monday first, anything
// else by volume (spent + income), largest first; ties keep file order (groups arrive in it).
func sortGroups(by string, gs []query.ToolGroup) []query.ToolGroup {
	switch {
	case timeKey(by):
		slices.SortStableFunc(gs, func(x, y query.ToolGroup) int { return cmp.Compare(x.Key, y.Key) })
	case by == "weekday":
		slices.SortStableFunc(gs, func(x, y query.ToolGroup) int { return cmp.Compare(x.Key, y.Key) })
	default:
		slices.SortStableFunc(gs, func(x, y query.ToolGroup) int {
			return cmp.Compare(y.SpentMinor+y.IncomeMinor, x.SpentMinor+x.IncomeMinor)
		})
	}
	return gs
}

type valuesResult struct {
	Field  string     `json:"field"`
	Query  string     `json:"query"`
	Values []valueOut `json:"values"`
}

type valueOut struct {
	Value string `json:"value"`
	Count int64  `json:"count"`
}

// MaxValues is how many values find_values returns (legacy).
const MaxValues = 40

func (t *Tools) values(ctx context.Context, userID uuid.UUID, sc Scope, a args) (Outcome, error) {
	field := a.or("field", "payee")
	kind := "payee"
	if f := a.str("field"); f == "category" || f == "group" {
		kind = f
	}
	q := a.or("query", "")
	nq := ingest.Norm(q)
	res := valuesResult{Field: field, Query: q, Values: []valueOut{}}
	err := t.eng.Insight(ctx, userID, sc.Context, func(in *query.Insight) error {
		vals, err := in.ValueCounts(kind)
		if err != nil {
			return err
		}
		for _, v := range vals {
			if nq != "" && !strings.Contains(ingest.Norm(v.Value), nq) {
				continue
			}
			res.Values = append(res.Values, valueOut{Value: sc.mask(v.Value), Count: v.Count})
			if len(res.Values) == MaxValues {
				break
			}
		}
		return nil
	})
	if err != nil {
		return Outcome{}, err
	}
	n := len(res.Values)
	return outcome(res, fmt.Sprintf("%s ~ %q → %d %s", field, q, n, plural(n, "value", "values"))), nil
}

// month_overview: the overview page's facts for one month (PLAN §4.6), compact.
type monthResult struct {
	Currency          string       `json:"currency"`
	Month             string       `json:"month"`
	DataUntil         string       `json:"data_until"`
	Partial           bool         `json:"partial"`
	DaysElapsed       int          `json:"days_elapsed,omitempty"`
	DaysInMonth       int          `json:"days_in_month"`
	Spent             json.Number  `json:"spent"`
	Income            json.Number  `json:"income"`
	Net               json.Number  `json:"net"`
	SavingsRate       *json.Number `json:"savings_rate_percent,omitempty"`
	ProjectedSpent    *json.Number `json:"projected_spent,omitempty"`
	PrevMonth         string       `json:"previous_month"`
	PrevSpent         json.Number  `json:"previous_month_spent"`
	PrevIncome        json.Number  `json:"previous_month_income"`
	PrevSpentToDate   *json.Number `json:"previous_month_spent_to_same_day,omitempty"`
	Categories        []monthCat   `json:"categories"`
	IncomeSources     []incomeOut  `json:"income_sources"`
	LargestExpenses   []largestOut `json:"largest_expenses"`
	OmittedCategories int          `json:"omitted_categories"`
}

type monthCat struct {
	Category  string      `json:"category"`
	Spent     json.Number `json:"spent"`
	PrevMonth json.Number `json:"previous_month"`
	Avg       json.Number `json:"average_before"`
	Budget    json.Number `json:"budget"`
	Default   bool        `json:"budget_is_default,omitempty"`
	Status    string      `json:"status"`
}

type incomeOut struct {
	Payee  string      `json:"payee"`
	Amount json.Number `json:"amount"`
}

type largestOut struct {
	Date     string      `json:"date"`
	Amount   json.Number `json:"amount"`
	Payee    string      `json:"payee"`
	Category string      `json:"category,omitempty"`
}

const (
	monthCategories = 15
	monthLargest    = 5
	monthIncome     = 5
)

var monthRe = regexp.MustCompile(`^(\d{4})-(\d{2})`)

func (t *Tools) month(ctx context.Context, userID uuid.UUID, sc Scope, a args) (Outcome, error) {
	ov, err := t.ins.Overview(ctx, userID, sc.Context, monthArg(a), 6, investments.Doc{})
	if err != nil {
		return Outcome{}, err
	}
	if ov.Empty {
		return outcome(map[string]string{"error": "No dated transactions in scope"}, "month overview · no data"), nil
	}
	cur := ov.Currency
	r := monthResult{Currency: cur, Month: ov.Month.String()[:7], DataUntil: ov.DataMax.String(), Partial: ov.Partial,
		DaysInMonth: ov.DaysInMonth, Spent: amount(ov.Cur.ExpenseMinor, cur), Income: amount(ov.Cur.IncomeMinor, cur),
		Net: amount(ov.Cur.IncomeMinor-ov.Cur.ExpenseMinor, cur), PrevMonth: ov.Prev.String()[:7],
		PrevSpent: amount(ov.PrevFlow.ExpenseMinor, cur), PrevIncome: amount(ov.PrevFlow.IncomeMinor, cur),
		Categories: []monthCat{}, IncomeSources: []incomeOut{}, LargestExpenses: []largestOut{}}
	if ov.Partial {
		r.DaysElapsed = ov.Elapsed
		p := amount(ov.PrevToDateMinor, cur)
		r.PrevSpentToDate = &p
	}
	if ov.ProjectedMinor != nil {
		p := amount(*ov.ProjectedMinor, cur)
		r.ProjectedSpent = &p
	}
	if inc := ov.Cur.IncomeMinor; inc > 0 {
		n := percent(inc-ov.Cur.ExpenseMinor, inc)
		r.SavingsRate = &n
	}
	for _, c := range ov.Categories {
		if c.CurMinor == 0 && c.PrevMinor == 0 {
			continue
		}
		if len(r.Categories) == monthCategories {
			r.OmittedCategories++
			continue
		}
		name := c.Name
		if c.ID == nil {
			name = "(uncategorized)"
		}
		r.Categories = append(r.Categories, monthCat{Category: name, Spent: amount(c.CurMinor, cur), PrevMonth: amount(c.PrevMinor, cur),
			Avg: amount(c.Avg3Minor, cur), Budget: amount(c.BudgetMinor, cur), Default: c.BudgetIsDefault, Status: c.Status})
	}
	for _, s := range ov.IncomeSources[:min(monthIncome, len(ov.IncomeSources))] {
		r.IncomeSources = append(r.IncomeSources, incomeOut{Payee: sc.mask(s.Payee), Amount: amount(s.SpentMinor, cur)})
	}
	for _, x := range ov.Largest[:min(monthLargest, len(ov.Largest))] {
		l := largestOut{Date: x.BookedOn.String(), Amount: amount(-x.AmountMinor, cur), Payee: sc.mask(x.PayeeGroup)}
		if x.Category != nil {
			l.Category = x.Category.Name
		}
		r.LargestExpenses = append(r.LargestExpenses, l)
	}
	return outcome(r, "month overview · "+r.Month), nil
}

type recurringResult struct {
	Currency string         `json:"currency"`
	Count    int            `json:"count"`
	Payments []recurringOut `json:"payments"`
	Omitted  int            `json:"omitted"`
}

type recurringOut struct {
	Payee    string      `json:"payee"`
	Category string      `json:"category,omitempty"`
	Typical  json.Number `json:"typical_amount"`
	Yearly   json.Number `json:"yearly_cost"`
	Charges  int         `json:"charges"`
	Cadence  string      `json:"cadence"`
	Last     string      `json:"last"`
	Next     string      `json:"next,omitempty"`
	Active   bool        `json:"active"`
}

// maxRecurring caps recurring_payments.
const maxRecurring = 30

func (t *Tools) recurring(ctx context.Context, userID uuid.UUID, sc Scope, a args) (Outcome, error) {
	ov, err := t.ins.Overview(ctx, userID, sc.Context, nil, 6, investments.Doc{})
	if err != nil {
		return Outcome{}, err
	}
	activeOnly := a.truthy("active_only")
	r := recurringResult{Currency: ov.Currency, Payments: []recurringOut{}}
	for i := range ov.Recurring {
		x := &ov.Recurring[i]
		if activeOnly && !x.Active {
			continue
		}
		r.Count++
		if len(r.Payments) == maxRecurring {
			r.Omitted++
			continue
		}
		p := recurringOut{Payee: sc.mask(x.Payee), Category: x.Category, Typical: amount(x.MedianMinor, ov.Currency),
			Yearly: amount(x.YearlyMinor, ov.Currency), Charges: x.Count, Cadence: x.Cadence, Last: x.Last.String(), Active: x.Active}
		if x.Active {
			p.Next = x.Next.String()
		}
		r.Payments = append(r.Payments, p)
	}
	return outcome(r, fmt.Sprintf("recurring payments → %d", r.Count)), nil
}

var longDigits = regexp.MustCompile(`\d{6,}`)

// mask shortens digit runs of six or more to their first two digits (account and card numbers,
// references) when the scope says so (legacy sample masking, applied to cloud tool results).
func (sc Scope) mask(s string) string {
	if !sc.Mask {
		return s
	}
	return MaskDigits(s)
}

// MaskDigits is the legacy sample masking: "12345678" → "12…".
func MaskDigits(s string) string {
	return longDigits.ReplaceAllStringFunc(s, func(m string) string { return m[:2] + "…" })
}

// clip cuts s to n characters (legacy .slice(0, n)).
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
