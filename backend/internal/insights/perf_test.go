package insights_test

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	demogen "github.com/x2db736ac85h/finuance/backend/internal/demo"
	"github.com/x2db736ac85h/finuance/backend/internal/insights"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
)

// TestInsightsPerf (PLAN Phase 11, docs/perf.md "Insights") seeds the same ~100k-transaction user
// as TestQueryPerf and times the six insight calls the pane makes for each view shape. Opt-in:
// FINUANCE_PERF=1 go test -run TestInsightsPerf -v ./internal/insights/ (FINUANCE_PERF_OUT=<file>
// also writes the table there).
func TestInsightsPerf(t *testing.T) {
	if os.Getenv("FINUANCE_PERF") == "" {
		t.Skip("set FINUANCE_PERF=1")
	}
	e := newEnv(t)
	e.u = e.user()
	total := 0
	for k := range 14 {
		start := time.Date(2015, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, 7*k)
		csv := demogen.CSV(start, start.AddDate(10, 0, 0))
		total += strings.Count(csv, "\n")
		e.importFile(e.u, fmt.Sprintf("Account %02d", k+1), []byte(csv))
	}
	if _, err := testDB.Owner.Exec(e.ctx, "VACUUM ANALYZE"); err != nil {
		t.Fatal(err)
	}

	shapes := []struct {
		name string
		c    query.Context
	}{
		{"all time, no filter", query.Context{}},
		{"last 12 months", query.Context{Period: &query.Period{Preset: "l12m"}}},
		{"one month", query.Context{Period: &query.Period{Month: "2024-06"}}},
		{"category = Transport, last 3 months", query.Context{Filters: []query.Filter{{Column: "category", Cond: "=Transport"}},
			Period: &query.Period{Preset: "l3m"}}},
		{"search \"blik warszawa\"", query.Context{Search: "blik warszawa"}},
	}
	calls := []struct {
		name string
		run  func(c query.Context) error
	}{
		{"summary", func(c query.Context) error { _, err := e.svc.Summary(e.ctx, e.u, c); return err }},
		{"categories", func(c query.Context) error { _, err := e.svc.Categories(e.ctx, e.u, c); return err }},
		{"cashflow", func(c query.Context) error { _, err := e.svc.Cashflow(e.ctx, e.u, c); return err }},
		{"pace", func(c query.Context) error { _, err := e.svc.Pace(e.ctx, e.u, c, insights.ComparePrev); return err }},
		{"payees", func(c query.Context) error {
			_, err := e.svc.Payees(e.ctx, e.u, c, 25, insights.ScopePeriod)
			return err
		}},
		{"daily", func(c query.Context) error { _, err := e.svc.Daily(e.ctx, e.u, c, insights.RangePeriod); return err }},
	}
	var out strings.Builder
	fmt.Fprintf(&out, "%d transactions in 14 accounts; p50 / p95 of 20 runs (first discarded)\n\n", total)
	fmt.Fprintf(&out, "| Shape |")
	for _, c := range calls {
		fmt.Fprintf(&out, " %s |", c.name)
	}
	fmt.Fprintf(&out, " all six, sequential |\n|---|%s---|\n", strings.Repeat("---|", len(calls)))
	for _, s := range shapes {
		fmt.Fprintf(&out, "| %s |", s.name)
		var sums []time.Duration
		for _, c := range calls {
			var times []time.Duration
			for i := range 21 {
				t0 := time.Now()
				if err := c.run(s.c); err != nil {
					t.Fatal(err)
				}
				if i > 0 {
					times = append(times, time.Since(t0))
				}
			}
			slices.Sort(times)
			if sums == nil {
				sums = make([]time.Duration, len(times))
			}
			for i, d := range times {
				sums[i] += d
			}
			fmt.Fprintf(&out, " %s / %s |", ms(times[len(times)/2]), ms(times[len(times)*95/100]))
		}
		slices.Sort(sums)
		fmt.Fprintf(&out, " %s |\n", ms(sums[len(sums)/2]))
	}
	t.Log("\n" + out.String())
	if p := os.Getenv("FINUANCE_PERF_OUT"); p != "" {
		if err := os.WriteFile(p, []byte(out.String()), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func ms(d time.Duration) string { return fmt.Sprintf("%.0f ms", float64(d.Microseconds())/1000) }
