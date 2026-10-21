package query_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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

// demo is one user with the legacy demo year imported, shared by the read-only tests.
var demo struct {
	once sync.Once
	user uuid.UUID
	acc  ledger.Account
	refs []string // the reference of each demo CSV row, in file order (oracle row indices)
}

func demoUser(t *testing.T) (*env, uuid.UUID) {
	t.Helper()
	e := newEnv(t)
	demo.once.Do(func() {
		content := fixture(t, "fixtures/demo/demo-legacy.csv")
		demo.user = e.user()
		demo.acc = e.importFile(demo.user, "Demo", content)
		lines := strings.Split(strings.TrimRight(string(content), "\n"), "\n")[1:]
		for _, l := range lines {
			demo.refs = append(demo.refs, strings.Split(l, ";")[9])
		}
		// The assistant oracle's groups (tools/legacy-oracle/page-helpers.js assignDemoGroups): every
		// 17th row of the file in "Wakacje nad morzem", the other "Ogród" rows in "Remont".
		byRef := map[string]query.Row{}
		rows := e.all(demo.user, query.Context{}, nil)
		for i := range rows {
			byRef[rows[i].Reference] = rows[i]
		}
		var g1, g2 []uuid.UUID
		for i, ref := range demo.refs {
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

func (e *env) page(u uuid.UUID, req query.PageRequest) query.Page {
	e.t.Helper()
	if req.Limit == 0 {
		req.Limit = query.MaxPageRows
	}
	p, err := e.q.Page(e.ctx, u, req)
	if err != nil {
		e.t.Fatal(err)
	}
	return p
}

// all returns every row of the ordered result by paging through it.
func (e *env) all(u uuid.UUID, c query.Context, s *query.Sort) []query.Row {
	e.t.Helper()
	var out []query.Row
	for off := 0; ; off += query.MaxPageRows {
		p := e.page(u, query.PageRequest{Context: c, Sort: s, Offset: off})
		out = append(out, p.Rows...)
		if off+query.MaxPageRows >= p.Total {
			return out
		}
	}
}

// legacyColumns maps the demo CSV headers of the oracle to API columns (tools/legacy-oracle/column-map.json).
var legacyColumns = map[string]string{
	"Data księgowania": "bookedOn", "Data waluty": "valueOn", "Nadawca / Odbiorca": "payee", "Tytułem": "description",
	"Kwota operacji": "amount", "Waluta": "currency", "Numer referencyjny": "reference", "Typ operacji": "txType", "Kategoria": "category",
}

// TestFilterParity replays every legacy filter case (testdata/oracle/filters/demo.json) on the demo
// year imported through the real import path: the count and the first rows must match.
func TestFilterParity(t *testing.T) {
	e, u := demoUser(t)
	var oracle struct {
		Cases []struct {
			Column   string `json:"column"`
			Cond     string `json:"cond"`
			Compiled bool   `json:"compiled"`
			Count    *int   `json:"count"`
			First    []int  `json:"first"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(fixture(t, "oracle/filters/demo.json"), &oracle); err != nil {
		t.Fatal(err)
	}
	all := len(demo.refs)
	// ALLOWED_DIFFS row 12: "=" compares whitespace-collapsed values, so a single-spaced name finds
	// the bank's padded one (Irwin's decision, Phase 7). The value is what the 3-space form finds.
	allowed := map[string]int{"payee\x00=KINO POD GWIAZDAMI WARSZAWA": 10}
	for _, c := range oracle.Cases {
		col, ok := legacyColumns[c.Column]
		if !ok {
			t.Fatalf("no API column for %q", c.Column)
		}
		want := all
		if c.Compiled {
			want = *c.Count
		}
		if n, ok := allowed[col+"\x00"+c.Cond]; ok {
			delete(allowed, col+"\x00"+c.Cond)
			want, c.First = n, nil
		}
		p := e.page(u, query.PageRequest{Context: query.Context{Filters: []query.Filter{{Column: col, Cond: c.Cond}}}, Limit: 20})
		if p.Total != want {
			t.Errorf("%s %q: %d rows, legacy %d", col, c.Cond, p.Total, want)
			continue
		}
		for i, idx := range c.First {
			if i >= len(p.Rows) || p.Rows[i].Reference != demo.refs[idx] {
				t.Errorf("%s %q: row %d is %v, legacy row %d (%s)", col, c.Cond, i, refAt(p.Rows, i), idx, demo.refs[idx])
				break
			}
		}
	}
	if len(allowed) > 0 {
		t.Errorf("allowed differences no longer in the oracle: %v", allowed)
	}
}

func refAt(rows []query.Row, i int) string {
	if i < len(rows) {
		return rows[i].Reference
	}
	return "(none)"
}

// TestToolFilterParity replays the assistant oracle's search and aggregate cases
// (testdata/oracle/assistant/tools.json): the typed ToolFilter must match the same rows as legacy
// aiFilter (PLAN §4.8, task 7.12).
func TestToolFilterParity(t *testing.T) {
	e, u := demoUser(t)
	var oracle struct {
		Cases []struct {
			Tool   string          `json:"tool"`
			Args   json.RawMessage `json:"args"`
			Result struct {
				Matched *int `json:"matched"`
				Rows    []struct {
					ID int `json:"id"`
				} `json:"rows"`
			} `json:"result"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(fixture(t, "oracle/assistant/tools.json"), &oracle); err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, c := range oracle.Cases {
		if (c.Tool != "search_transactions" && c.Tool != "aggregate_transactions") || c.Result.Matched == nil {
			continue
		}
		var args map[string]json.RawMessage
		if json.Unmarshal(c.Args, &args) != nil {
			continue // null or not an object: the legacy treats it as no filter, covered by {}
		}
		tf := toolFilterOf(args)
		rows := e.all(u, query.Context{ToolFilter: &tf}, nil)
		if len(rows) != *c.Result.Matched {
			t.Errorf("%s %s: %d rows, legacy %d", c.Tool, c.Args, len(rows), *c.Result.Matched)
			continue
		}
		// Every row the legacy returned is among ours (ids are row indices of the demo file).
		ours := map[string]bool{}
		for _, r := range rows {
			ours[r.Reference] = true
		}
		for _, r := range c.Result.Rows {
			if !ours[demo.refs[r.ID]] {
				t.Errorf("%s %s: legacy row %d (%s) missing", c.Tool, c.Args, r.ID, demo.refs[r.ID])
				break
			}
		}
		checked++
	}
	if checked < 50 {
		t.Fatalf("only %d oracle cases checked", checked)
	}
}

// toolFilterOf converts assistant arguments the way the Phase 13 tool layer will: strings and
// numbers as given, dates via ParseToolDate.
func toolFilterOf(args map[string]json.RawMessage) query.ToolFilter {
	str := func(k string) string {
		var s string
		if json.Unmarshal(args[k], &s) == nil {
			return s
		}
		return ""
	}
	num := func(k string) *string {
		raw, ok := args[k]
		if !ok || string(raw) == "null" {
			return nil
		}
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return &s
		}
		v := string(raw)
		return &v
	}
	tf := query.ToolFilter{Query: str("query"), Payee: str("payee"), Category: str("category"), Group: str("group"),
		Type: str("type"), MinAmount: num("min_amount"), MaxAmount: num("max_amount")}
	tf.DateFrom, tf.DateTo = query.ParseToolDate(str("date_from")), query.ParseToolDate(str("date_to"))
	return tf
}

func TestPeriodStatsAndRange(t *testing.T) {
	e, u := demoUser(t)
	all := e.all(u, query.Context{}, nil)
	p := e.page(u, query.PageRequest{Context: query.Context{Period: &query.Period{Preset: "l3m"},
		Filters: []query.Filter{{Column: "amount", Cond: "<0"}}}, Limit: 10})
	if p.Period == nil || p.Period.Label != "Last 3 months" || p.Period.From.String() != "2025-01-01" || p.Period.To.String() != "2025-04-01" {
		t.Fatalf("period = %+v", p.Period)
	}
	if p.DataMin.String() != "2024-01-01" || p.DataMax.String() != "2025-03-17" {
		t.Errorf("data range %s..%s ignores the period, want the whole filtered range", p.DataMin, p.DataMax)
	}
	var n int
	var sum, mn, mx int64
	for _, r := range all {
		if r.AmountMinor < 0 && !r.BookedOn.Before(p.Period.From) && r.BookedOn.Before(p.Period.To) {
			if n == 0 || r.AmountMinor < mn {
				mn = r.AmountMinor
			}
			if n == 0 || r.AmountMinor > mx {
				mx = r.AmountMinor
			}
			n++
			sum += r.AmountMinor
		}
	}
	avg := (2*sum - int64(n)) / (2 * int64(n)) // round half away from zero for a negative sum
	if p.Total != n || *p.Stats.Sum != sum || *p.Stats.Min != mn || *p.Stats.Max != mx || *p.Stats.Avg != avg {
		t.Errorf("stats %d %+v (sum %d avg %d), want %d rows sum %d avg %d min %d max %d", p.Total, p.Stats, *p.Stats.Sum, *p.Stats.Avg, n, sum, avg, mn, mx)
	}
	if p.DuplicatesSkipped != 0 {
		t.Errorf("duplicatesSkipped = %d", p.DuplicatesSkipped)
	}
	// balanceAfter: the demo has no balance column, so there are no values.
	b := e.page(u, query.PageRequest{StatColumn: "balanceAfter", Limit: 1})
	if b.Stats.Sum != nil || b.Total != len(all) {
		t.Errorf("balance stats = %+v", b.Stats)
	}
}

func TestPagingAndLocate(t *testing.T) {
	e, u := demoUser(t)
	sort := &query.Sort{Column: "payee"}
	full := e.all(u, query.Context{}, sort)
	var joined []query.Row
	for off := 0; off < len(full); off += 37 {
		joined = append(joined, e.page(u, query.PageRequest{Sort: sort, Offset: off, Limit: 37}).Rows...)
	}
	if len(joined) != len(full) {
		t.Fatalf("%d rows from windows, %d in full", len(joined), len(full))
	}
	for i := range full {
		if joined[i].ID != full[i].ID {
			t.Fatalf("windows differ from the full order at %d", i)
		}
	}
	target := full[123]
	p := e.page(u, query.PageRequest{Sort: sort, Limit: 50, LocateID: &target.ID})
	if p.LocatedOffset == nil || *p.LocatedOffset != 123 || p.Offset != 100 || p.Rows[23].ID != target.ID {
		t.Errorf("locate: offset %d located %v", p.Offset, p.LocatedOffset)
	}
	missing := uuid.New()
	if p := e.page(u, query.PageRequest{Sort: sort, Offset: 50, Limit: 50, LocateID: &missing}); p.LocatedOffset != nil || p.Offset != 50 {
		t.Errorf("unknown locateId moved the window: %d %v", p.Offset, p.LocatedOffset)
	}
}

func TestDistinct(t *testing.T) {
	e, u := demoUser(t)
	all := e.all(u, query.Context{}, nil)
	want := map[string]int{}
	for _, r := range all {
		want[r.Payee]++
	}
	// The payee filter itself is ignored, so unticked and filtered-out payees stay listed.
	c := query.Context{Filters: []query.Filter{{Column: "payee", Cond: "kino", Excluded: []string{"KINO POD GWIAZDAMI   WARSZAWA"}}}}
	vals, err := e.q.Distinct(e.ctx, u, c, "payee", "", query.MaxDistinct)
	if err != nil {
		t.Fatal(err)
	}
	if len(vals) != len(want) {
		t.Fatalf("%d values, want %d", len(vals), len(want))
	}
	for i, v := range vals {
		if want[v.Value] != v.Count || (i > 0 && vals[i-1].Count < v.Count) {
			t.Errorf("value %q count %d (want %d), order at %d", v.Value, v.Count, want[v.Value], i)
		}
	}
	// Other columns' filters apply; search narrows the values.
	c.Filters = append(c.Filters, query.Filter{Column: "category", Cond: "=Rozrywka"})
	vals, _ = e.q.Distinct(e.ctx, u, c, "payee", "", 600)
	if len(vals) != 2 {
		t.Errorf("entertainment payees = %v", vals)
	}
	vals, _ = e.q.Distinct(e.ctx, u, query.Context{}, "payee", "teatr", 600)
	if len(vals) != 1 || !strings.HasPrefix(vals[0].Value, "TEATR") {
		t.Errorf("search = %v", vals)
	}
	// Amounts and dates are listed in their fixed text form.
	vals, _ = e.q.Distinct(e.ctx, u, query.Context{Filters: []query.Filter{{Column: "payee", Cond: "=OPERATOR INTERNETU"}}}, "amount", "", 600)
	if len(vals) != 1 || vals[0].Value != "-79,99" {
		t.Errorf("amount values = %v", vals)
	}
	vals, _ = e.q.Distinct(e.ctx, u, query.Context{Filters: []query.Filter{{Column: "bookedOn", Cond: "2025-03-10"}}}, "bookedOn", "", 600)
	if len(vals) < 300 { // its own filter is removed: every booking date is listed, not only 10.03.2025
		t.Errorf("only %d dates listed", len(vals))
	}
	if _, err := e.q.Distinct(e.ctx, u, query.Context{}, "nope", "", 10); err == nil {
		t.Error("unknown column accepted")
	}
}

// TestSortOrder: Polish collation, numbers inside text, empties last in both directions, and the
// booking date / file order tie-break.
func TestSortOrder(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	csv := "Data;Kontrahent;Kwota;Saldo\n" +
		"05.01.2025;zabka 10;-1,00;100,00\n" +
		"05.01.2025;Żabka;-2,00;\n" +
		"04.01.2025;zabka 2;-3,00;90,00\n" +
		"04.01.2025;Ąbc;-4,00;80,00\n" +
		"03.01.2025;abc;-5,00;\n" +
		"03.01.2025;;-6,00;70,00\n" +
		"02.01.2025;Bank;-7,00;60,00\n" +
		"02.01.2025;abc;-8,00;50,00\n"
	e.importFile(u, "Sort", []byte(csv))
	payees := func(s *query.Sort) []string {
		var out []string
		for _, r := range e.all(u, query.Context{}, s) {
			out = append(out, r.Payee+"|"+r.BookedOn.String()[8:])
		}
		return out
	}
	// abc twice: the later date first (tie-break booked_on DESC).
	want := []string{"abc|03", "abc|02", "Ąbc|04", "Bank|02", "zabka 2|04", "zabka 10|05", "Żabka|05", "|03"}
	if got := payees(&query.Sort{Column: "payee"}); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("payee asc = %v\n want %v", got, want)
	}
	if got := payees(&query.Sort{Column: "payee", Desc: true}); got[len(got)-1] != "|03" || got[0] != "Żabka|05" {
		t.Errorf("payee desc = %v (empty must stay last)", got)
	}
	var bal []string
	for _, r := range e.all(u, query.Context{}, &query.Sort{Column: "balanceAfter", Desc: true}) {
		if r.BalanceAfterMinor == nil {
			bal = append(bal, "-")
		} else {
			bal = append(bal, r.Payee)
		}
	}
	if strings.Join(bal, ",") != "zabka 10,zabka 2,Ąbc,,Bank,abc,-,-" {
		t.Errorf("balance desc = %v (NULLs last)", bal)
	}
	if got := payees(nil); got[0] != "zabka 10|05" || got[1] != "Żabka|05" {
		t.Errorf("default order = %v (newest first, file order within a day)", got)
	}
	if _, err := e.q.Page(e.ctx, u, query.PageRequest{Sort: &query.Sort{Column: "t.payee"}, Limit: 1}); err == nil {
		t.Error("unknown sort column accepted")
	}
}

// TestHostileInputAgainstDB runs hostile conditions on every column of the real database: they
// must compile, run without SQL errors and match only as text.
func TestHostileInputAgainstDB(t *testing.T) {
	e, u := demoUser(t)
	for _, h := range []string{"'; DROP TABLE transactions; --", `\' OR 1=1 --`, "%", "_", `\`, "$1", "/* x */", "ąę'\"`;", "%%%_\\"} {
		for _, col := range query.ColumnNames() {
			for _, cond := range []string{h, "=" + h, ">" + h, h + ".." + h, "!" + h} {
				c := query.Context{Search: h, Filters: []query.Filter{{Column: col, Cond: cond, Excluded: []string{h}}}}
				p, err := e.q.Page(e.ctx, u, query.PageRequest{Context: c, Limit: 5})
				if err != nil {
					t.Fatalf("%s %q: %v", col, cond, err)
				}
				if p.Total != 0 && h != "%" && h != "_" {
					t.Errorf("%s %q matched %d rows", col, cond, p.Total)
				}
			}
		}
	}
	var n int
	if err := e.st.WithUser(e.ctx, u, func(_ *db.Queries, tx pgx.Tx) error {
		return tx.QueryRow(e.ctx, "SELECT count(*) FROM transactions WHERE user_id = $1", u).Scan(&n)
	}); err != nil || n != len(demo.refs) {
		t.Fatalf("transactions after the hostile run: %d, %v", n, err)
	}
}

// TestTenancy: another user's account ids select nothing.
func TestTenancy(t *testing.T) {
	e, _ := demoUser(t)
	other := e.user()
	p := e.page(other, query.PageRequest{Context: query.Context{AccountIDs: []uuid.UUID{demo.acc.ID}}, Limit: 10})
	if p.Total != 0 || len(p.Rows) != 0 || p.DataMax != nil || p.DuplicatesSkipped != 0 {
		t.Errorf("other user sees %+v", p)
	}
	vals, err := e.q.Distinct(e.ctx, other, query.Context{AccountIDs: []uuid.UUID{demo.acc.ID}}, "payee", "", 10)
	if err != nil || len(vals) != 0 {
		t.Errorf("other user's distinct = %v, %v", vals, err)
	}
}

// FuzzQueryDB compiles fuzzed contexts and runs them on Postgres (nightly CI, 60 s): no input may
// cause an SQL error.
func FuzzQueryDB(f *testing.F) {
	f.Add("payee", "'; DROP TABLE x; --", "a b", "")
	f.Add("amount", "-1 000,50..2e3", "", "x")
	f.Add("bookedOn", ">=2025-13", "%", `\`)
	f.Fuzz(func(t *testing.T, col, cond, search, excluded string) {
		if testDB == nil {
			t.Skip("needs Postgres")
		}
		if _, ok := query.Lookup(col); !ok {
			col = "payee"
		}
		e, u := demoUser(t)
		c := query.Context{Search: search, Filters: []query.Filter{{Column: col, Cond: cond, Excluded: []string{excluded}}},
			ToolFilter: &query.ToolFilter{Payee: cond, MinAmount: &excluded}}
		if _, err := e.q.Page(e.ctx, u, query.PageRequest{Context: c, Limit: 5, Sort: &query.Sort{Column: col}}); err != nil {
			t.Fatalf("%q %q %q %q: %v", col, cond, search, excluded, err)
		}
	})
}
