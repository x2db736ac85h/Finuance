package insights_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/insights"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
	"github.com/x2db736ac85h/finuance/backend/testutil"
)

var testDB *testutil.DB

func TestMain(m *testing.M) { testutil.Main(m, &testDB) }

type env struct {
	t   *testing.T
	ctx context.Context
	st  *store.Store
	l   *ledger.Ledger
	svc *insights.Service
	u   uuid.UUID
}

func newEnv(t *testing.T) *env {
	t.Helper()
	tdb := testutil.Require(t, testDB)
	st := store.New(tdb.App)
	return &env{t: t, ctx: context.Background(), st: st,
		l: ledger.New(st, platform.FixedClock{T: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)}), svc: insights.New(query.New(st))}
}

func (e *env) user() uuid.UUID {
	e.t.Helper()
	id := platform.NewID()
	if err := e.st.WithoutUser(e.ctx, func(q *db.Queries, _ pgx.Tx) error {
		_, err := q.CreateUser(e.ctx, db.CreateUserParams{ID: id, Email: id.String() + "@example.test", PasswordHash: "x"})
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	return id
}

func testdata(t *testing.T, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", rel))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// importFile commits a file into a new account through the real import path (preview, commit).
func (e *env) importFile(u uuid.UUID, name string, content []byte) ledger.Account {
	e.t.Helper()
	acc, err := e.l.CreateAccount(e.ctx, u, ledger.AccountInput{Name: name})
	if err != nil {
		e.t.Fatal(err)
	}
	p, err := e.l.CreatePreview(e.ctx, u, ledger.Upload{Filename: name + ".csv", Content: content, AccountID: &acc.ID})
	if err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.l.StartCommit(e.ctx, u, p.Import.ID, func(context.Context, pgx.Tx) error { return nil }); err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.l.CommitImport(e.ctx, u, p.Import.ID); err != nil {
		e.t.Fatal(err)
	}
	return acc
}

// legacyColumns maps the demo CSV headers of the oracle to API columns (tools/legacy-oracle/column-map.json).
var legacyColumns = map[string]string{
	"Data księgowania": "bookedOn", "Data waluty": "valueOn", "Nadawca / Odbiorca": "payee", "Tytułem": "description",
	"Kwota operacji": "amount", "Waluta": "currency", "Numer referencyjny": "reference", "Typ operacji": "txType", "Kategoria": "category",
}

type twin struct {
	Cols []string   `json:"cols"`
	Rows [][]string `json:"rows"`
}

type chartOut struct {
	Sub      string   `json:"sub"`
	Table    *twin    `json:"table"`
	Captions []string `json:"captions"`
}

type chartCase struct {
	Name   string              `json:"name"`
	Period json.RawMessage     `json:"period"`
	Rows   int                 `json:"rows"`
	Charts map[string]chartOut `json:"charts"`
}

// The case definitions (tools/legacy-oracle/cases/charts.mjs) aren't in the output, so the view
// of each case is repeated here.
type view struct {
	period  string
	custom  [2]string
	filters map[string]string
	search  string
	cfg     map[string]string
}

var views = map[string]view{
	"all-time":         {period: "all"},
	"month-latest":     {period: "m:2025-03", cfg: map[string]string{"paceCmp": "prev", "heatRange": "period"}},
	"month-yoy":        {period: "m:2025-02", cfg: map[string]string{"paceCmp": "yoy", "heatRange": "3", "payeeTop": "10"}},
	"month-avg3":       {period: "m:2024-06", cfg: map[string]string{"paceCmp": "avg3", "heatRange": "12", "payeeTop": "10", "payeeScope": "all"}},
	"first-month":      {period: "m:2024-01", cfg: map[string]string{"paceCmp": "prev", "heatRange": "6"}},
	"l30-avg6":         {period: "l30", cfg: map[string]string{"paceCmp": "avg6", "heatRange": "all", "payeeTop": "0"}},
	"l3m-search":       {period: "l3m", search: "warszawa", cfg: map[string]string{"paceCmp": "avg3", "payeeTop": "50"}},
	"l12m-none":        {period: "l12m", cfg: map[string]string{"paceCmp": "none", "payeeTop": "100"}},
	"ytd":              {period: "ytd", cfg: map[string]string{"paceCmp": "yoy"}},
	"year":             {period: "y:2024", cfg: map[string]string{"paceCmp": "avg6", "payeeScope": "all", "payeeTop": "0"}},
	"custom-range":     {period: "custom", custom: [2]string{"2024-07-01", "2024-07-15"}, cfg: map[string]string{"paceCmp": "prev"}},
	"one-day":          {period: "custom", custom: [2]string{"2025-03-10", "2025-03-11"}, cfg: map[string]string{"heatRange": "period"}},
	"before-the-data":  {period: "m:2023-06", cfg: map[string]string{"paceCmp": "prev", "heatRange": "period"}},
	"category-filter":  {period: "l12m", filters: map[string]string{"Kategoria": "!Rachunki"}, cfg: map[string]string{"paceCmp": "avg3", "heatRange": "6"}},
	"payee-and-amount": {period: "all", filters: map[string]string{"Nadawca / Odbiorca": "warszawa", "Kwota operacji": "<-50"}, cfg: map[string]string{"paceCmp": "yoy", "payeeTop": "25", "heatRange": "12"}},
	"income-only":      {period: "y:2024", filters: map[string]string{"Kwota operacji": ">0"}},
}

// legacy defaults (S.cfg)
var defaults = map[string]string{"paceCmp": "prev", "payeeTop": "25", "payeeScope": "list", "heatRange": "period"}

func (v view) cfgOf(k string) string {
	if s, ok := v.cfg[k]; ok {
		return s
	}
	return defaults[k]
}

func (v view) context(t *testing.T) query.Context {
	c := query.Context{Search: v.search}
	for col, cond := range v.filters {
		c.Filters = append(c.Filters, query.Filter{Column: legacyColumns[col], Cond: cond})
	}
	switch p := v.period; {
	case p == "all":
	case p == "custom":
		from, err1 := platform.ParseDate(v.custom[0])
		to, err2 := platform.ParseDate(v.custom[1])
		if err1 != nil || err2 != nil {
			t.Fatal(err1, err2)
		}
		c.Period = &query.Period{From: &from, To: &to}
	case strings.HasPrefix(p, "m:"):
		c.Period = &query.Period{Month: p[2:]}
	case strings.HasPrefix(p, "y:"):
		y, _ := strconv.Atoi(p[2:])
		c.Period = &query.Period{Year: y}
	default:
		c.Period = &query.Period{Preset: p}
	}
	return c
}

// money formats minor units as the legacy oracle does (en-US, 2 decimals, currency suffix).
func money(minor int64) string {
	neg := minor < 0
	if neg {
		minor = -minor
	}
	whole, cents := minor/100, minor%100
	s := strconv.FormatInt(whole, 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if neg {
		s = "-" + s
	}
	return fmt.Sprintf("%s.%02d PLN", s, cents)
}

// toFixed0 is JavaScript's x.toFixed(0) for x ≥ 0 (ties round up).
func toFixed0(x float64) string { return strconv.FormatFloat(math.Floor(x+0.5), 'f', 0, 64) }

func ddmmyyyy(d platform.Date) string { return d.Time().Format("02.01.2006") }

// TestChartParity replays every legacy chart case (testdata/oracle/charts/demo.json) on the demo
// year imported through the real import path: each chart's table twin must match the legacy one.
func TestChartParity(t *testing.T) {
	e := newEnv(t)
	e.u = e.user()
	e.importFile(e.u, "Demo", testdata(t, "fixtures/demo/demo-legacy.csv"))

	var oracle struct {
		Cases []chartCase `json:"cases"`
	}
	if err := json.Unmarshal(testdata(t, "oracle/charts/demo.json"), &oracle); err != nil {
		t.Fatal(err)
	}
	if len(oracle.Cases) != len(views) {
		t.Fatalf("%d oracle cases, %d views", len(oracle.Cases), len(views))
	}
	allowed := map[string]int{}
	for _, oc := range oracle.Cases {
		t.Run(oc.Name, func(t *testing.T) {
			v, ok := views[oc.Name]
			if !ok {
				t.Fatalf("no view for case %q", oc.Name)
			}
			c := v.context(t)
			check := func(chart string, got *twin, sub string, ties map[[2]int]string) {
				t.Helper()
				want := oc.Charts[chart]
				// ALLOWED_DIFFS row 13: where the exact average is a half-cent tie, the legacy value
				// rounded toward zero (ties holds that text, cell by cell) is accepted too.
				for at, alt := range ties {
					r, c := at[0], at[1]
					if want.Table != nil && got != nil && r < len(want.Table.Rows) && r < len(got.Rows) &&
						want.Table.Rows[r][c] == alt && got.Rows[r][c] != alt {
						got.Rows[r][c] = alt
						allowed[chart]++
					}
				}
				if diff := cmp.Diff(want.Table, got); diff != "" {
					t.Errorf("%s table (-legacy +new):\n%s", chart, diff)
				}
				if sub != want.Sub {
					t.Errorf("%s subtitle = %q, legacy %q", chart, sub, want.Sub)
				}
			}
			e.summary(t, c, check, oc.Charts["sum"].Captions)
			e.categories(t, c, check)
			e.cashflow(t, c, check)
			e.pace(t, c, v.cfgOf("paceCmp"), check)
			e.payees(t, c, v.cfgOf("payeeTop"), v.cfgOf("payeeScope"), check)
			e.daily(t, c, v.cfgOf("heatRange"), check)
		})
	}
	t.Logf("half-cent ties accepted (ALLOWED_DIFFS row 13): %v", allowed)
}

type checker func(chart string, got *twin, sub string, ties map[[2]int]string)

func (e *env) summary(t *testing.T, c query.Context, check checker, captions []string) {
	s, err := e.svc.Summary(e.ctx, e.u, c)
	if err != nil {
		t.Fatal(err)
	}
	largest := "–"
	if s.Largest != nil {
		largest = money(s.Largest.AmountMinor)
	}
	sub := "All time"
	if s.Period != nil {
		sub = legacyLabel(s.Period)
	}
	check("sum", &twin{Cols: []string{"Metric", "Value"}, Rows: [][]string{{"Spent", money(s.SpentMinor)}, {"Income", money(s.IncomeMinor)},
		{"Net", money(s.NetMinor)}, {"Avg / day", money(s.AvgPerDayMinor)}, {"Days", strconv.Itoa(s.Days)},
		{"Operations", strconv.FormatInt(s.Count, 10)}, {"Largest expense", largest}}}, sub, nil)

	delta := "all time"
	switch {
	case s.Period == nil:
	case s.SpentDelta == nil:
		delta = "no spending in previous period"
	case math.Abs(*s.SpentDelta) < .005:
		delta = "same as previous"
	case *s.SpentDelta > 0:
		delta = "▲ " + toFixed0(*s.SpentDelta*100) + "% vs previous"
	default:
		delta = "▼ " + toFixed0(-*s.SpentDelta*100) + "% vs previous"
	}
	saved := "no income"
	if s.SavingsRate != nil {
		saved = "saved " + toFixed0(*s.SavingsRate*100) + "% of income"
	}
	days := "over " + strconv.Itoa(s.Days) + " day"
	if s.Days > 1 {
		days += "s"
	}
	big := ""
	if s.Largest != nil {
		big = s.Largest.Payee + " · " + ddmmyyyy(s.Largest.BookedOn)
	}
	got := []string{delta, strconv.FormatInt(s.Count, 10) + " operations", saved, days, big}
	if diff := cmp.Diff(captions, got); diff != "" {
		t.Errorf("summary captions (-legacy +new):\n%s", diff)
	}
}

// legacyLabel is the legacy period label (en-US formats in the oracle): months are "March 2025"
// and custom days "Jul 1, 2024", where the API's labels are "Mar 2025" and "1 Jul 2024".
func legacyLabel(p *query.Resolved) string {
	switch p.Kind {
	case "month":
		return p.From.Time().Format("January 2006")
	case "year":
		return p.Label
	}
	if strings.HasPrefix(p.Label, "Last") || strings.HasPrefix(p.Label, "Year to date") {
		return p.Label
	}
	last := p.To.AddDays(-1)
	if last == p.From {
		return p.From.Time().Format("Jan 2, 2006")
	}
	return p.From.Time().Format("Jan 2, 2006") + " – " + last.Time().Format("Jan 2, 2006")
}

func (e *env) categories(t *testing.T, c query.Context, check checker) {
	r, err := e.svc.Categories(e.ctx, e.u, c)
	if err != nil {
		t.Fatal(err)
	}
	if r.TotalMinor == 0 {
		check("cat", nil, "", nil)
		return
	}
	tw := &twin{Cols: []string{"Category", "Spent", "Share"}}
	for _, x := range r.Categories {
		name := "(uncategorized)"
		if x.Name != nil {
			name = *x.Name
		}
		tw.Rows = append(tw.Rows, []string{name, money(x.SpentMinor), strconv.FormatFloat(x.Share*100, 'f', 1, 64) + "%"})
	}
	n := len(r.Categories)
	sub := strconv.Itoa(n) + " categories"
	if n == 1 {
		sub = "1 category"
	}
	check("cat", tw, sub, nil)
}

func (e *env) cashflow(t *testing.T, c query.Context, check checker) {
	r, err := e.svc.Cashflow(e.ctx, e.u, c)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Months) == 0 {
		check("flow", nil, "", nil)
		return
	}
	tw := &twin{Cols: []string{"Month", "In", "Out", "Net", "Operations"}}
	for i := len(r.Months) - 1; i >= 0; i-- {
		m := r.Months[i]
		tw.Rows = append(tw.Rows, []string{m.Month.Time().Format("January 2006"), money(m.IncomeMinor), paceMoney(m.ExpenseMinor),
			money(m.IncomeMinor - m.ExpenseMinor), strconv.FormatInt(m.Count, 10)})
	}
	check("flow", tw, strconv.Itoa(len(r.Months))+" months", nil)
}

// paceMoney is the legacy money(-x) of a positive sum: always signed, also for zero ("-0.00 PLN").
func paceMoney(v int64) string { return "-" + money(v) }

func (e *env) pace(t *testing.T, c query.Context, mode string, check checker) {
	p, err := e.svc.Pace(e.ctx, e.u, c, mode)
	if err != nil {
		t.Fatal(err)
	}
	if p.Target == nil {
		check("pace", nil, "", nil)
		return
	}
	tw := &twin{Cols: []string{"Day", p.Target.Time().Format("Jan 2006")}}
	pd := 0
	if cp := p.Compare; cp != nil {
		pd = cp.Days
		label := strconv.Itoa(cp.Months) + "-mo avg"
		switch cp.Mode {
		case insights.ComparePrev:
			label = cp.Month.Time().Format("Jan")
		case insights.CompareYoY:
			label = cp.Month.Time().Format("Jan 2006")
		}
		tw.Cols = append(tw.Cols, label)
	}
	for d := 1; d <= max(p.DaysInMonth, pd); d++ {
		row := []string{strconv.Itoa(d), ""}
		if d <= p.LastDay {
			row[1] = paceMoney(p.Current[d-1])
		}
		if cp := p.Compare; cp != nil {
			v := ""
			if d <= pd {
				v = paceMoney(cp.Values[d-1])
			}
			row = append(row, v)
		}
		tw.Rows = append(tw.Rows, row)
	}
	check("pace", tw, "cumulative spend", e.paceTies(t, c, p))
}

func (e *env) payees(t *testing.T, c query.Context, top, scope string, check checker) {
	n, _ := strconv.Atoi(top)
	sc := insights.ScopePeriod
	if scope == "all" {
		sc = insights.ScopeAll
	}
	r, err := e.svc.Payees(e.ctx, e.u, c, n, sc)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Payees) == 0 {
		check("payee", nil, "", nil)
		return
	}
	tw := &twin{Cols: []string{"Payee", "Spent", "Operations", "Average"}}
	ties := map[[2]int]string{}
	for i, x := range r.Payees {
		tw.Rows = append(tw.Rows, []string{x.Payee, money(x.SpentMinor), strconv.FormatInt(x.Count, 10), money(insights.DivRound(x.SpentMinor, x.Count))})
		if 2*(x.SpentMinor%x.Count) == x.Count {
			ties[[2]int{i, 3}] = money(x.SpentMinor / x.Count)
		}
	}
	sub := fmt.Sprintf("top %d of %d", len(r.Payees), r.Total)
	if sc == insights.ScopeAll {
		sub += " · all time"
	}
	check("payee", tw, sub, ties)
}

func (e *env) daily(t *testing.T, c query.Context, rng string, check checker) {
	mode := map[string]string{"period": insights.RangePeriod, "3": insights.Range3m, "6": insights.Range6m, "12": insights.Range12m,
		"all": insights.RangeAll}[rng]
	r, err := e.svc.Daily(e.ctx, e.u, c, mode)
	if err != nil {
		t.Fatal(err)
	}
	if r.From == nil {
		check("heat", nil, "", nil)
		return
	}
	tw := &twin{Cols: []string{"Day", "Spent", "Operations"}}
	for i := len(r.Days) - 1; i >= 0; i-- {
		x := r.Days[i]
		tw.Rows = append(tw.Rows, []string{x.Date.Time().Format("Mon, Jan 2, 2006"), money(x.SpentMinor), strconv.FormatInt(x.Count, 10)})
	}
	if tw.Rows == nil {
		tw.Rows = [][]string{}
	}
	// the subtitle counts the calendar's weeks (Mon-first), a layout the client computes
	start := r.From.AddDays(-((int(r.From.Weekday()) + 6) % 7))
	weeks := (start.DaysUntil(*r.To) + 6) / 7
	sub := strconv.Itoa(weeks) + " weeks"
	if mode != insights.RangePeriod {
		if mode == insights.RangeAll {
			sub += " · all data"
		} else {
			sub += " · last " + rng + " mo"
		}
	}
	check("heat", tw, sub, nil)
}

// paceTies finds the days whose exact average comparison is a half-cent tie: it reads each
// averaged month's cumulative spending (that month as the target, no comparison) and sums them.
func (e *env) paceTies(t *testing.T, c query.Context, p insights.Pace) map[[2]int]string {
	cp := p.Compare
	if cp == nil || cp.Months == 0 {
		return nil
	}
	sums := make([]int64, 31)
	for j := 1; j <= cp.Months; j++ {
		mc := c
		mc.Period = &query.Period{Month: p.Target.AddMonths(-j).String()[:7]}
		m, err := e.svc.Pace(e.ctx, e.u, mc, insights.CompareNone)
		if err != nil {
			t.Fatal(err)
		}
		for d := range sums {
			sums[d] += m.Current[d]
		}
	}
	n := int64(cp.Months)
	ties := map[[2]int]string{}
	for d, s := range sums {
		if 2*(s%n) == n {
			ties[[2]int{d, 2}] = paceMoney(s / n)
		}
	}
	return ties
}
