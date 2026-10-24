package assistant

import (
	"bytes"
	"context"
	"encoding/json"
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
	for _, c := range []struct{ name, args string }{{ToolSearch, `{}`}, {ToolAggregate, `{"group_by":"payee"}`}, {ToolValues, `{}`}} {
		out, err := tools.Run(e.ctx, other, Scope{MaxRows: 50}, c.name, c.args)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.JSON, "DEMO") || strings.Contains(out.JSON, "WSPÓLNOTA") {
			t.Errorf("%s leaks: %s", c.name, out.JSON)
		}
	}
}
