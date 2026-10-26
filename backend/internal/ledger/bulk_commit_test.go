package ledger_test

import (
	"testing"
	"time"

	"github.com/x2db736ac85h/finuance/backend/testutil"
)

// TestBulkCommitForANewUser: a new user's first import of a long history commits in time linear in
// its rows. The table statistics predate the rows the commit itself inserts, and before Phase 17
// the planner linked the staged rows with a nested loop that scanned every one of the user's
// transactions per row (20,000 rows: 119 s; docs/perf.md "Phase 17").
func TestBulkCommitForANewUser(t *testing.T) {
	if testing.Short() {
		t.Skip("database test")
	}
	e := newEnv(t)
	// Statistics taken while the tables held only a small demo year, as autoanalyze leaves them on
	// an instance with few users (the owner role may ANALYZE).
	small := e.user()
	if _, err := e.l.SeedDemo(e.ctx, small); err != nil {
		t.Fatal(err)
	}
	if _, err := testutil.Require(t, testDB).Owner.Exec(e.ctx, "ANALYZE transactions, import_rows"); err != nil {
		t.Fatal(err)
	}
	u := e.user()
	start := time.Now()
	seed, err := e.l.SeedDemoDays(e.ctx, u, 4800) // about 10,000 rows
	if err != nil {
		t.Fatal(err)
	}
	took := time.Since(start)
	if seed.Result.New < 9000 {
		t.Fatalf("seeded %d rows", seed.Result.New)
	}
	t.Logf("%d rows committed in %s", seed.Result.New, took.Round(time.Millisecond))
	if took > 20*time.Second { // about 3 s when linear in the sandbox; the quadratic plan takes ~30 s
		t.Errorf("committing %d rows for a new user took %s", seed.Result.New, took)
	}
}
