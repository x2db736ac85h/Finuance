package sharing_test

import (
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/sharing"
)

// TestSharedPerf (PLAN 19.11, docs/perf.md "Shared groups") times a group of four members with
// 5,000 expenses: the group page (detail with balances), its report and the timeline. Opt-in:
// FINUANCE_PERF=1 go test -run TestSharedPerf -v ./internal/sharing/
func TestSharedPerf(t *testing.T) {
	if os.Getenv("FINUANCE_PERF") == "" {
		t.Skip("set FINUANCE_PERF=1")
	}
	e := newEnv(t)
	alex := e.user("Alex")
	g, err := e.s.Create(e.ctx, alex, sharing.GroupInput{Name: "Dom", Currency: "PLN", Kind: "household"})
	if err != nil {
		t.Fatal(err)
	}
	others := []string{"Bo", "Cy", "Dee"}
	for _, n := range others {
		u := e.user(n)
		e.befriend(alex, u)
		if _, err := e.s.Invite(e.ctx, alex, g.ID, u); err != nil {
			t.Fatal(err)
		}
		if _, err := e.s.Accept(e.ctx, u, g.ID); err != nil {
			t.Fatal(err)
		}
	}
	start := time.Now()
	for i := range 5000 {
		day := platform.Date{Y: 2024 + i/1800, M: time.Month(1 + i%12), D: 1 + i%28}
		if _, err := e.s.AddExpense(e.ctx, alex, g.ID, sharing.ExpenseInput{BookedOn: day, AmountMinor: int64(100 + i%9000),
			Payee: fmt.Sprintf("Sklep %d", i%40), Category: fmt.Sprintf("Kategoria %d", i%12)}); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("seeded 5,000 expenses in %s", time.Since(start).Round(time.Millisecond))
	time3 := func(name string, f func() error) {
		var ds []time.Duration
		for range 15 {
			s := time.Now()
			if err := f(); err != nil {
				t.Fatal(err)
			}
			ds = append(ds, time.Since(s))
		}
		slices.Sort(ds)
		t.Logf("%-10s p50 %6s  p95 %6s", name, ds[len(ds)/2].Round(time.Millisecond), ds[len(ds)*95/100].Round(time.Millisecond))
	}
	time3("detail", func() error { _, err := e.s.Get(e.ctx, alex, g.ID); return err })
	time3("report", func() error { _, _, err := e.s.Report(e.ctx, alex, g.ID); return err })
	time3("timeline", func() error { _, err := e.s.Timeline(e.ctx, alex); return err })
}
