package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

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
	q   *query.Engine
}

func newEnv(t *testing.T) *env {
	t.Helper()
	tdb := testutil.Require(t, testDB)
	st := store.New(tdb.App)
	return &env{t: t, ctx: context.Background(), st: st,
		l: ledger.New(st, platform.FixedClock{T: time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)}), q: query.New(st)}
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

func fixture(t *testing.T, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", rel))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

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

// demo is one user with the legacy demo year and the oracle's two groups (as in internal/query).
var demo struct {
	once sync.Once
	user uuid.UUID
}

func demoUser(t *testing.T) (*env, uuid.UUID) {
	t.Helper()
	e := newEnv(t)
	demo.once.Do(func() {
		content := fixture(t, "fixtures/demo/demo-legacy.csv")
		demo.user = e.user()
		e.importFile(demo.user, "Demo", content)
		var refs []string
		for _, l := range strings.Split(strings.TrimRight(string(content), "\n"), "\n")[1:] {
			refs = append(refs, strings.Split(l, ";")[9])
		}
		// tools/legacy-oracle/page-helpers.js assignDemoGroups: every 17th row of the file in
		// "Wakacje nad morzem", the other "Ogród" rows in "Remont".
		var rows []query.Row
		for off := 0; ; off += query.MaxPageRows {
			p, err := e.q.Page(e.ctx, demo.user, query.PageRequest{Offset: off, Limit: query.MaxPageRows})
			if err != nil {
				t.Fatal(err)
			}
			rows = append(rows, p.Rows...)
			if len(p.Rows) < query.MaxPageRows {
				break
			}
		}
		byRef := map[string]query.Row{}
		for i := range rows {
			byRef[rows[i].Reference] = rows[i]
		}
		var g1, g2 []uuid.UUID
		for i, ref := range refs {
			switch r := byRef[ref]; {
			case i%17 == 0:
				g1 = append(g1, r.ID)
			case r.CategoryName != nil && *r.CategoryName == "Ogród":
				g2 = append(g2, r.ID)
			}
		}
		for i, g := range []struct {
			name string
			ids  []uuid.UUID
		}{{"Wakacje nad morzem", g1}, {"Remont", g2}} {
			grp, err := e.l.CreateGroup(e.ctx, demo.user, g.name, i+1)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := e.l.Annotate(e.ctx, demo.user, g.ids, ledger.AnnotationPatch{SetGroup: true, GroupID: &grp.ID}); err != nil {
				t.Fatal(err)
			}
		}
	})
	if demo.user == uuid.Nil {
		t.Fatal("demo import failed in an earlier test")
	}
	return e, demo.user
}

type oracleCase struct {
	Tool    string          `json:"tool"`
	Args    json.RawMessage `json:"args"`
	Result  json.RawMessage `json:"result"`
	Summary string          `json:"summary"`
}

func oracleTools(t *testing.T) []oracleCase {
	t.Helper()
	var o struct {
		Cases []oracleCase `json:"cases"`
	}
	if err := json.Unmarshal(fixture(t, "oracle/assistant/tools.json"), &o); err != nil {
		t.Fatal(err)
	}
	return o.Cases
}

func decode(t *testing.T, s []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(s, &v); err != nil {
		t.Fatalf("%v in %s", err, s)
	}
	return v
}

// TestToolsParity replays every execTool case the legacy oracle recorded on the demo year
// (testdata/oracle/assistant/tools.json) and compares the whole result and the step summary.
// Allowed differences (testdata/oracle/ALLOWED_DIFFS.md row 14): a row's id is its position in the
// result (the legacy used the file's row index), and a filter label names the group filter.
func TestToolsParity(t *testing.T) {
	e, u := demoUser(t)
	tools := NewTools(e.q)
	sc := Scope{MaxRows: 50}
	cases := oracleTools(t)
	if len(cases) < 67 {
		t.Fatalf("only %d oracle cases", len(cases))
	}
	for _, c := range cases {
		name := c.Tool + " " + string(c.Args)
		out, err := tools.Run(e.ctx, u, sc, c.Tool, string(c.Args))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		want, got := decode(t, c.Result), decode(t, []byte(out.JSON))
		if w, ok := want.(map[string]any); ok {
			if rows, ok := w["rows"].([]any); ok {
				for i, r := range rows {
					r.(map[string]any)["id"] = float64(i)
				}
			}
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("%s (-legacy +new):\n%s", name, diff)
		}
		wantSummary := c.Summary
		if g := parseArgs(string(c.Args)); g.truthy("group") {
			wantSummary = strings.Replace(wantSummary, "all rows", "group: "+g.str("group"), 1)
		}
		if out.Summary != wantSummary {
			t.Errorf("%s: summary %q, legacy %q", name, out.Summary, c.Summary)
		}
		if (c.Tool == ToolSearch || c.Tool == ToolAggregate) != (out.Filter != nil) {
			t.Errorf("%s: filter %+v", name, out.Filter)
		}
	}
}

func TestToolsNewDimensions(t *testing.T) {
	e, u := demoUser(t)
	tools := NewTools(e.q)
	run := func(name, a string, sc Scope) map[string]any {
		t.Helper()
		out, err := tools.Run(e.ctx, u, sc, name, a)
		if err != nil {
			t.Fatal(err)
		}
		return decode(t, []byte(out.JSON)).(map[string]any)
	}
	sc := Scope{MaxRows: 50}
	acc := run(ToolAggregate, `{"group_by":"account"}`, sc)
	if g := acc["groups"].([]any); len(g) != 1 || g[0].(map[string]any)["key"] != "Demo" || g[0].(map[string]any)["count"] != float64(917) {
		t.Errorf("by account = %v", acc)
	}
	grp := run(ToolValues, `{"field":"group"}`, sc)
	want := []any{map[string]any{"value": "Wakacje nad morzem", "count": float64(54)}, map[string]any{"value": "Remont", "count": float64(15)}}
	if diff := cmp.Diff(want, grp["values"]); diff != "" {
		t.Errorf("group values: %s", diff)
	}
	// rows name their account when the scope has several; max rows caps the limit
	multi := run(ToolSearch, `{"limit":10}`, Scope{MaxRows: 3, MultiAccount: true})
	if rows := multi["rows"].([]any); len(rows) != 3 || rows[0].(map[string]any)["account"] != "Demo" {
		t.Errorf("multi-account rows = %v", rows)
	}
	// a cloud connection's results hide long digit runs
	masked := run(ToolSearch, `{"limit":1}`, Scope{MaxRows: 50, Mask: true})
	if row := masked["rows"].([]any)[0].(map[string]any); row["title"] != "DEMO 83…" {
		t.Errorf("masked = %v", row)
	}
	// a scope narrows every tool: a view of 2025 only
	from, _ := platform.ParseDate("2025-01-01")
	to, _ := platform.ParseDate("2026-01-01")
	view := Scope{MaxRows: 50, View: true, Context: query.Context{Period: &query.Period{From: &from, To: &to}}}
	if r := run(ToolAggregate, `{"group_by":"month"}`, view); len(r["groups"].([]any)) != 3 || r["matched"] != float64(155) {
		t.Errorf("view = %v", r)
	}
	// text with a NUL character is answered as an error, not a failure
	if r := run(ToolSearch, `{"payee":"a\u0000b"}`, sc); r["error"] == nil {
		t.Errorf("NUL = %v", r)
	}
}

func TestOverviewTools(t *testing.T) {
	e, u := demoUser(t)
	tools := NewTools(e.q)
	sc := Scope{MaxRows: 50}
	out, err := tools.Run(e.ctx, u, sc, ToolMonth, `{"month":"2025-02"}`)
	if err != nil {
		t.Fatal(err)
	}
	m := decode(t, []byte(out.JSON)).(map[string]any)
	// February 2025 as the overview and insights tests know it
	if m["month"] != "2025-02" || m["spent"] != 5569.62 || m["income"] != float64(9580) || m["partial"] != false ||
		m["previous_month"] != "2025-01" || m["savings_rate_percent"] != 41.9 || out.Summary != "month overview · 2025-02" {
		t.Errorf("month = %s", out.JSON)
	}
	if cats := m["categories"].([]any); len(cats) == 0 || cats[0].(map[string]any)["status"] == "" {
		t.Errorf("categories = %v", cats)
	}
	if l := m["largest_expenses"].([]any); len(l) != 5 || l[0].(map[string]any)["payee"] != "WSPÓLNOTA MIESZKANIOWA" {
		t.Errorf("largest = %v", l)
	}
	cur, _ := tools.Run(e.ctx, u, sc, ToolMonth, `{}`)
	if c := decode(t, []byte(cur.JSON)).(map[string]any); c["month"] != "2025-03" || c["partial"] != true || c["days_elapsed"] != float64(17) ||
		c["projected_spent"] == nil {
		t.Errorf("latest month = %s", cur.JSON)
	}
	rec, err := tools.Run(e.ctx, u, sc, ToolRecurring, `{"active_only":true}`)
	if err != nil {
		t.Fatal(err)
	}
	r := decode(t, []byte(rec.JSON)).(map[string]any)
	pays := r["payments"].([]any)
	if len(pays) == 0 || r["count"] != float64(len(pays)) {
		t.Fatalf("recurring = %s", rec.JSON)
	}
	for _, p := range pays {
		if p.(map[string]any)["active"] != true || p.(map[string]any)["next"] == nil || p.(map[string]any)["cadence"] != "monthly" {
			t.Errorf("payment = %v", p)
		}
	}
	// a user without data
	empty := e.user()
	if out, _ := tools.Run(e.ctx, empty, sc, ToolMonth, `{}`); !bytes.Contains([]byte(out.JSON), []byte("No dated transactions")) {
		t.Errorf("empty = %s", out.JSON)
	}
}

// Another user's lookups never see the demo user's rows (RLS under WithUserReadOnly).
func TestToolsTenancy(t *testing.T) {
	e, _ := demoUser(t)
	other := e.user()
	tools := NewTools(e.q)
	for _, c := range []struct{ name, args string }{{ToolSearch, `{}`}, {ToolAggregate, `{"group_by":"payee"}`}, {ToolValues, `{}`},
		{ToolBudgets, `{}`}, {ToolGroup, `{"group":"Wakacje nad morzem"}`}} {
		out, err := tools.Run(e.ctx, other, Scope{MaxRows: 50}, c.name, c.args)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.JSON, "DEMO") || strings.Contains(out.JSON, "WSPÓLNOTA") {
			t.Errorf("%s leaks: %s", c.name, out.JSON)
		}
	}
}

// The budget and group tools (PLAN 15.8) on the demo year.
func TestBudgetAndGroupTools(t *testing.T) {
	e, u := demoUser(t)
	tools := NewTools(e.q)
	run := func(name, args string, sc Scope) (map[string]any, string) {
		t.Helper()
		out, err := tools.Run(e.ctx, u, sc, name, args)
		if err != nil {
			t.Fatal(err)
		}
		return decode(t, []byte(out.JSON)).(map[string]any), out.Summary
	}
	sc := Scope{MaxRows: 50}

	// budget_status: the month's categories, worst first; the totals add up
	b, sum := run(ToolBudgets, `{"month":"2025-02"}`, sc)
	if b["month"] != "2025-02" || b["partial"] != false || b["spent"] != 5569.62 || !strings.HasPrefix(sum, "budget status · 2025-02 · ") {
		t.Fatalf("budget_status = %v (%s)", b, sum)
	}
	cats := b["categories"].([]any)
	rank := map[string]int{"over": 0, "ahead": 1, "ok": 2, "none": 3}
	counts := map[string]int{}
	var spent float64
	prev := -1
	for _, c := range cats {
		c := c.(map[string]any)
		st := c["status"].(string)
		if rank[st] < prev {
			t.Errorf("order: %v", cats)
		}
		prev = rank[st]
		counts[st]++
		spent += c["spent"].(float64)
		if math.Abs(c["left"].(float64)-(c["budget"].(float64)-c["spent"].(float64))) > 0.005 {
			t.Errorf("left = %v", c)
		}
	}
	if num := func(k string) int { return int(b[k].(float64)) }; num("over_budget") != counts["over"] || num("ahead_of_pace") != counts["ahead"] ||
		num("on_track") != counts["ok"] || b["omitted_categories"] != float64(0) {
		t.Errorf("counts %v vs %v", b, counts)
	}
	if math.Abs(spent-5569.62) > 0.005 {
		t.Errorf("categories spend %v", spent)
	}
	// the latest month is in progress
	if cur, _ := run(ToolBudgets, `{}`, sc); cur["month"] != "2025-03" || cur["partial"] != true || cur["days_elapsed"] != float64(17) {
		t.Errorf("latest = %v", cur)
	}

	// group_summary: matched ignoring case and accents, or by a unique part of the name
	stats, err := e.l.GroupStats(e.ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	var wak ledger.GroupStat
	for _, g := range stats {
		if g.Name == "Wakacje nad morzem" {
			wak = g
		}
	}
	for _, name := range []string{"Wakacje nad morzem", "WAKACJE  NAD MORZEM", "wakacje"} {
		g, sum := run(ToolGroup, `{"group":"`+name+`"}`, sc)
		if g["group"] != "Wakacje nad morzem" || sum != "group summary · Wakacje nad morzem" {
			t.Fatalf("%q: %v", name, g)
		}
		if int(g["transactions"].(float64)) != wak.Count || g["spent"] != float64(wak.SpentMinor)/100 ||
			g["first_day"] != wak.FirstOn.String() || g["last_day"] != wak.LastOn.String() || g["budget"] != nil {
			t.Errorf("%q: %v vs %+v", name, g, wak)
		}
		var cs float64
		for _, c := range g["categories"].([]any) {
			cs += c.(map[string]any)["spent"].(float64)
		}
		if len(g["categories"].([]any)) < groupCategories && math.Abs(cs-float64(wak.SpentMinor)/100) > 0.005 {
			t.Errorf("categories add up to %v", cs)
		}
		if p := g["top_payees"].([]any); len(p) == 0 || len(p) > 10 {
			t.Errorf("payees = %v", p)
		}
	}
	// a budget shows with what is left
	if _, err := e.l.EditGroup(e.ctx, u, wak.ID, wak.Name, -1, 100000); err != nil {
		t.Fatal(err)
	}
	if g, _ := run(ToolGroup, `{"group":"wakacje"}`, sc); g["budget"] != float64(1000) || g["budget_left"] != float64(100000-wak.SpentMinor)/100 {
		t.Errorf("budget = %v", g)
	}
	if _, err := e.l.EditGroup(e.ctx, u, wak.ID, wak.Name, -1, ledger.NoBudget); err != nil {
		t.Fatal(err)
	}
	// no single match: the error lists the candidates
	for args, want := range map[string]int{`{"group":"e"}`: 2, `{"group":"Italy"}`: 2} {
		g, _ := run(ToolGroup, args, sc)
		if g["error"] == nil || len(g["groups"].([]any)) != want {
			t.Errorf("%s = %v", args, g)
		}
	}
	if g, _ := run(ToolGroup, `{}`, sc); g["error"] == nil {
		t.Errorf("no group = %v", g)
	}
	// cloud connections: long digit runs in payees are masked
	g, _ := run(ToolGroup, `{"group":"wakacje"}`, Scope{MaxRows: 50, Mask: true})
	for _, p := range g["top_payees"].([]any) {
		if longDigits.MatchString(p.(map[string]any)["payee"].(string)) {
			t.Errorf("unmasked payee %v", p)
		}
	}
}

// propose_action (PLAN 15.7) reads what a change would touch and never changes anything.
func TestProposeAction(t *testing.T) {
	e, u := demoUser(t)
	tools := NewTools(e.q)
	sc := Scope{MaxRows: 50}
	run := func(args string, sc Scope) (Outcome, map[string]any) {
		t.Helper()
		out, err := tools.Run(e.ctx, u, sc, ToolPropose, args)
		if err != nil {
			t.Fatal(err)
		}
		return out, decode(t, []byte(out.JSON)).(map[string]any)
	}
	before := totalsOf(t, e, u)

	// set_category with a new category: 14 rows, their ids for Apply, a sample; the model gets no ids
	out, m := run(`{"action":"set_category","payee":"OPERATOR INTERNETU","to_category":"Internet","reason":"Monthly internet bills"}`, sc)
	p := out.Proposal
	if p == nil || p.Action != ActionSetCategory || p.Matched != 14 || len(p.IDs) != 14 || p.CategoryID != nil || p.CategoryName != "Internet" ||
		len(p.Sample) != 5 || p.Reason != "Monthly internet bills" || p.Filter == nil || p.Filter.Payee != "OPERATOR INTERNETU" {
		t.Fatalf("proposal = %+v", p)
	}
	if m["status"] != proposedStatus || m["creates_category"] != true || m["matched"] != float64(14) || len(m["sample"].([]any)) != 5 ||
		strings.Contains(out.JSON, p.IDs[0].String()) || !strings.HasPrefix(out.Summary, "proposed set category · payee: OPERATOR INTERNETU → 14") {
		t.Errorf("result = %s (%s)", out.JSON, out.Summary)
	}
	// an existing category is found ignoring case and accents
	out, m = run(`{"action":"set_category","payee":"OPERATOR INTERNETU","to_category":"rachunki"}`, sc)
	if out.Proposal.CategoryID == nil || out.Proposal.CategoryName != "Rachunki" || m["creates_category"] != nil {
		t.Errorf("existing category: %+v %v", out.Proposal, m)
	}
	// set_group to an existing group
	out, _ = run(`{"action":"set_group","payee":"OPERATOR INTERNETU","date_from":"2025-01-01","to_group":"wakacje nad morzem"}`, sc)
	if p := out.Proposal; p.GroupID == nil || p.GroupName != "Wakacje nad morzem" || p.Matched != 2 {
		t.Errorf("set_group = %+v", p)
	}
	// create_rule: payee contains …, counted over everything
	out, m = run(`{"action":"create_rule","payee":"OPERATOR INTERNETU","to_category":"Internet"}`, sc)
	if p := out.Proposal; p.Match == nil || len(p.Match.Filters) != 1 || p.Match.Filters[0] != (ProposalFilter{"payee", "OPERATOR INTERNETU"}) ||
		p.RuleName != "OPERATOR INTERNETU" || p.Matched != 14 || len(p.IDs) != 0 || m["rule_name"] != "OPERATOR INTERNETU" {
		t.Errorf("create_rule = %+v %v", p, m)
	}

	// refusals are answered to the model, with no proposal
	for args, want := range map[string]string{
		`{}`:                             "action must be",
		`{"action":"delete_everything"}`: "action must be",
		`{"action":"set_category","to_category":"Internet"}`:                              "Give a filter",
		`{"action":"set_category","payee":"OPERATOR INTERNETU"}`:                          "to_category must be",
		`{"action":"set_group","payee":"x","to_group":"` + strings.Repeat("g", 41) + `"}`: "to_group must be",
		`{"action":"set_category","payee":"NO SUCH PAYEE","to_category":"X"}`:             "No transactions match",
		`{"action":"create_rule","to_category":"Internet"}`:                               "A rule needs",
		`{"action":"create_rule","payee":"=OPERATOR","to_category":"Internet"}`:           "plain text",
	} {
		out, m := run(args, sc)
		if out.Proposal != nil || !strings.Contains(fmt.Sprint(m["error"]), want) {
			t.Errorf("%s = %s", args, out.JSON)
		}
	}
	old := proposalCap
	proposalCap = 10
	out, m = run(`{"action":"set_category","payee":"OPERATOR INTERNETU","to_category":"Internet"}`, sc)
	proposalCap = old
	if out.Proposal != nil || !strings.Contains(fmt.Sprint(m["error"]), "14 transactions match; a proposal may change at most 10") {
		t.Errorf("cap = %s", out.JSON)
	}
	// the chat's scope applies: a view filtered to 2025 sees 2 bills
	view := Scope{MaxRows: 50, Context: query.Context{Period: &query.Period{Year: 2025}}, View: true}
	if out, _ := run(`{"action":"set_category","payee":"OPERATOR INTERNETU","to_category":"Internet"}`, view); out.Proposal.Matched != 2 {
		t.Errorf("view scope = %+v", out.Proposal)
	}
	// cloud connections: the model's sample is masked, the card's isn't
	out, _ = run(`{"action":"set_category","query":"DEMO","type":"income","to_category":"Wpływy"}`, Scope{MaxRows: 50, Mask: true})
	if longDigits.MatchString(out.JSON) {
		t.Errorf("unmasked: %s", out.JSON)
	}
	// nothing changed
	if after := totalsOf(t, e, u); after != before {
		t.Errorf("data changed: %v → %v", before, after)
	}
	// another user's proposal sees none of these rows
	if out, err := tools.Run(e.ctx, e.user(), sc, ToolPropose, `{"action":"set_category","payee":"OPERATOR INTERNETU","to_category":"X"}`); err != nil ||
		out.Proposal != nil {
		t.Errorf("tenancy: %v %s", err, out.JSON)
	}
}

// totalsOf is a fingerprint of the user's categorisation: categories, manual ones and groups.
func totalsOf(t *testing.T, e *env, u uuid.UUID) [3]int {
	t.Helper()
	var out [3]int
	cats, err := e.q.Categories(e.ctx, u, nil)
	if err != nil {
		t.Fatal(err)
	}
	out[0] = len(cats)
	for _, f := range []query.Filter{{Column: "category", Cond: "="}, {Column: "group", Cond: "!="}} {
		p, err := e.q.Page(e.ctx, u, query.PageRequest{Context: query.Context{Filters: []query.Filter{f}}, Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		out[1+map[string]int{"category": 0, "group": 1}[f.Column]] = p.Total
	}
	return out
}
