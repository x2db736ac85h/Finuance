package query_test

import (
	"encoding/json"
	"testing"
	"time"

	demogen "github.com/x2db736ac85h/finuance/backend/internal/demo"
	"github.com/x2db736ac85h/finuance/backend/testutil"
)

// TestSearchReadsOnlyTheUsersEntries: a search reads only the searching user's entries from the
// trigram index, so its cost follows the user's data, not the instance's (migration 00011). With
// search_text indexed alone, the index returned every user's matches and RLS dropped all but one
// user's afterwards (the Phase 17 load test: 58,661 entries read for 1,127 rows).
func TestSearchReadsOnlyTheUsersEntries(t *testing.T) {
	e := newEnv(t)
	// Eight years of the demo generator each (~6,000 rows): the user's own rows outnumber what a
	// rare word matches by far, as with a real history, so the trigram index is worth reading.
	start := time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC)
	content := []byte(demogen.CSV(start, start.AddDate(8, 0, 0)))
	for range 3 {
		e.importFile(e.user(), "Other", content)
	}
	u := e.user()
	e.importFile(u, "Mine", content)
	if _, err := testutil.Require(t, testDB).Owner.Exec(e.ctx, "ANALYZE transactions"); err != nil {
		t.Fatal(err)
	}

	tx, err := testDB.App.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(e.ctx) }()
	// As the app role under RLS. Only bitmap scans, so the trigram indexes are what the planner
	// weighs (the tables are small enough for a sequential scan otherwise).
	for _, s := range []string{
		"SELECT set_config('app.user_id', '" + u.String() + "', true)",
		"SET LOCAL enable_seqscan = off", "SET LOCAL enable_indexscan = off", "SET LOCAL enable_indexonlyscan = off",
	} {
		if _, err := tx.Exec(e.ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	for _, word := range []string{"kawiarnia", "blik", "apteka"} {
		var mine int
		if err := tx.QueryRow(e.ctx, `SELECT count(*) FROM transactions WHERE user_id = $1 AND search_text LIKE $2`, u, "%"+word+"%").Scan(&mine); err != nil {
			t.Fatal(err)
		}
		var plan []struct{ Plan node }
		var raw []byte
		if err := tx.QueryRow(e.ctx, `EXPLAIN (ANALYZE, FORMAT JSON) SELECT count(*) FROM transactions t WHERE t.user_id = $1 AND t.search_text LIKE $2`,
			u, "%"+word+"%").Scan(&raw); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &plan); err != nil || len(plan) != 1 {
			t.Fatalf("plan: %v %s", err, raw)
		}
		var trigram []node
		plan[0].Plan.walk(func(n node) {
			if n.Type == "Bitmap Index Scan" && n.Index != "tx_user_group_idx" {
				trigram = append(trigram, n)
			}
		})
		if len(trigram) != 1 {
			t.Fatalf("%q: want one trigram index scan, got %+v\n%s", word, trigram, raw)
		}
		if got := trigram[0]; got.Index != "tx_user_search_trgm_idx" || got.Rows > float64(mine) {
			t.Errorf("%q: %s returned %v entries for the user's %d matches", word, got.Index, got.Rows, mine)
		}
	}
}

// node is one node of an EXPLAIN (FORMAT JSON) plan.
type node struct {
	Type  string  `json:"Node Type"`
	Index string  `json:"Index Name"`
	Rows  float64 `json:"Actual Rows"`
	Plans []node  `json:"Plans"`
}

func (n node) walk(f func(node)) {
	f(n)
	for _, c := range n.Plans {
		c.walk(f)
	}
}
