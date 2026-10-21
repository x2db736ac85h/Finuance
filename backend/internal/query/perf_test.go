package query_test

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	demogen "github.com/x2db736ac85h/finuance/backend/internal/demo"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
)

// TestQueryPerf (PLAN task 7.11) seeds one user with ~100k transactions — the demo generator over
// ten years, in 14 accounts with shifted ranges, through the real import path — and measures the
// main query shapes. Opt-in: FINUANCE_PERF=1 go test -run TestQueryPerf -v ./internal/query/
// (FINUANCE_PERF_OUT=<file> also writes the plans and timings there).
func TestQueryPerf(t *testing.T) {
	if os.Getenv("FINUANCE_PERF") == "" {
		t.Skip("set FINUANCE_PERF=1")
	}
	e := newEnv(t)
	u := e.user()
	seedStart := time.Now()
	total := 0
	for k := range 14 {
		start := time.Date(2015, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, 7*k)
		csv := demogen.CSV(start, start.AddDate(10, 0, 0))
		total += strings.Count(csv, "\n")
		e.importFile(u, fmt.Sprintf("Account %02d", k+1), []byte(csv))
	}
	// Statistics and the visibility map as autovacuum leaves them after the imports (index-only
	// scans need the latter). VACUUM needs the table owner.
	if _, err := testDB.Owner.Exec(e.ctx, "VACUUM ANALYZE"); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	fmt.Fprintf(&out, "seeded %d transactions in 14 accounts in %s\n\n", total, time.Since(seedStart).Round(time.Millisecond))

	shapes := []struct {
		name string
		req  query.PageRequest
	}{
		{"default sort, no filter", query.PageRequest{}},
		{"default sort, deep window (offset 90 000)", query.PageRequest{Offset: 90000}},
		{"payee contains \"kawiarnia\"", query.PageRequest{Context: query.Context{Filters: []query.Filter{{Column: "payee", Cond: "kawiarnia"}}}}},
		{"search \"blik warszawa\"", query.PageRequest{Context: query.Context{Search: "blik warszawa"}}},
		{"category = Transport, last 3 months", query.PageRequest{Context: query.Context{Filters: []query.Filter{{Column: "category", Cond: "=Transport"}},
			Period: &query.Period{Preset: "l3m"}}}},
		{"sort by payee (collation)", query.PageRequest{Sort: &query.Sort{Column: "payee"}}},
		{"amount < -100, sort by amount", query.PageRequest{Context: query.Context{Filters: []query.Filter{{Column: "amount", Cond: "<-100"}}},
			Sort: &query.Sort{Column: "amount"}}},
	}
	fmt.Fprintf(&out, "| Shape | rows | p50 | p95 |\n|---|---|---|---|\n")
	var plans strings.Builder
	for _, s := range shapes {
		s.req.Limit = 200
		var times []time.Duration
		var n int
		for i := range 30 {
			t0 := time.Now()
			p, err := e.q.Page(e.ctx, u, s.req)
			if err != nil {
				t.Fatal(err)
			}
			if i > 0 { // the first run warms the cache
				times = append(times, time.Since(t0))
			}
			n = p.Total
		}
		slices.Sort(times)
		p50, p95 := times[len(times)/2], times[len(times)*95/100]
		fmt.Fprintf(&out, "| %s | %d | %s | %s |\n", s.name, n, p50.Round(100*time.Microsecond), p95.Round(100*time.Microsecond))
		pl, err := e.q.Explain(e.ctx, u, s.req)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&plans, "\n### %s\n", s.name)
		for _, k := range []string{"totals", "rows", "duplicates"} {
			fmt.Fprintf(&plans, "\n%s:\n```\n%s\n```\n", k, pl[k])
		}
	}
	t.Log("\n" + out.String())
	if f := os.Getenv("FINUANCE_PERF_OUT"); f != "" {
		if err := os.WriteFile(f, []byte(out.String()+plans.String()), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
