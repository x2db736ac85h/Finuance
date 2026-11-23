package actions

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/client/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/sim"
	"github.com/x2db736ac85h/finuance/backend/internal/sim/identity"
	"github.com/x2db736ac85h/finuance/backend/internal/sim/statements"
)

// history is the on-demand action that gives a persona a history of the configured profile
// (ADR 0044): the page's *Rebuild history*, `finuance-sim seed`, or act now.
func history() []sim.Action {
	return []sim.Action{{Name: sim.RebuildAction, Area: "imports", Cost: sim.Heavy, OnDemand: true, Run: RebuildHistory}}
}

// topUp imports the days since the persona's last import, for a persona whose history already has
// the configured profile (`finuance-sim seed` run again).
func topUp(ctx context.Context, a *sim.Actor) error {
	from := sim.AddDays(a.ImportedThrough(statements.Current), 1)
	if s := sim.AddDays(a.ImportedThrough(statements.Savings), 1); s.Compare(from) < 0 {
		from = s
	}
	last := sim.AddDays(a.Today(), -1)
	if from.Compare(last) > 0 {
		return sim.ErrNothingToDo
	}
	return ImportUpTo(ctx, a, from, last)
}

// Seed gives the first count personas a history of the engine's configured profile (ADR 0044),
// parallel at a time: a persona whose accounts already carry that profile and hold data imports
// only the days since, any other rebuilds its history. report is told about each persona's event.
// It returns the first failure, after every persona had its turn.
func Seed(ctx context.Context, e *sim.Engine, count, parallel int, report func(sim.Event)) error {
	cfg, _ := e.Config()
	turns := make(chan struct{}, max(1, parallel))
	var mu sync.Mutex
	var first error
	fail := func(err error) {
		mu.Lock()
		if first == nil {
			first = err
		}
		mu.Unlock()
	}
	var wg sync.WaitGroup
	for i := range count {
		p := identity.Nth(i)
		wg.Go(func() {
			turns <- struct{}{}
			defer func() { <-turns }()
			a, err := e.Actor(ctx, p)
			if err != nil {
				fail(fmt.Errorf("%s: %w", p.Slug, err))
				return
			}
			act := sim.Action{Name: sim.RebuildAction, Area: "imports", Cost: sim.Heavy, OnDemand: true, Run: RebuildHistory}
			if a.HasData() && a.History() == cfg.History.Normal() {
				act = sim.Action{Name: "history.top-up", Area: "imports", Cost: sim.Heavy, OnDemand: true, Run: topUp}
			}
			ev := e.Do(ctx, a, act)
			if report != nil {
				report(ev)
			}
			if ev.Result == sim.ResultError {
				fail(fmt.Errorf("%s %s: %s", p.Slug, ev.Action, ev.Error))
			}
		})
	}
	wg.Wait()
	return first
}

// rowsPerStatement is about how many rows one statement of a rebuilt history holds: a default
// history comes a year at a time, a dense one about a quarter.
const rowsPerStatement = 1500

// RebuildHistory deletes the persona's two bank accounts, makes them again under the configured
// history profile (its label in their names) and imports that history up to yesterday through the
// normal upload and commit, in statements of about rowsPerStatement rows.
func RebuildHistory(ctx context.Context, a *sim.Actor) error {
	cfg, _ := a.Engine().Config()
	h := cfg.History.Normal()
	for _, n := range []int{statements.Current, statements.Savings} {
		if id := a.Account(n); id != uuid.Nil {
			d, err := a.API.DeleteAccountWithResponse(ctx, id)
			if err != nil {
				return err
			}
			if err := sim.Expect("delete an account", d.HTTPResponse, http.StatusNoContent, http.StatusNotFound); err != nil {
				return err
			}
		}
		made, err := a.API.CreateAccountWithResponse(ctx, gen.CreateAccountJSONRequestBody{Name: statements.AccountNameFor(n, h)})
		if err != nil {
			return err
		}
		if err := sim.Expect("make an account", made.HTTPResponse, http.StatusCreated); err != nil {
			return err
		}
		a.SetAccount(n, made.JSON201.Id)
	}
	a.SetHistory(h)
	first := a.StartHistory()
	return ImportUpTo(ctx, a, first, sim.AddDays(a.Today(), -1))
}

// ImportUpTo imports both accounts' history from first to last in statements sized for the
// persona's profile, and records each as imported.
func ImportUpTo(ctx context.Context, a *sim.Actor, first, last platform.Date) error {
	span := spanFor(a.History())
	for _, n := range []int{statements.Current, statements.Savings} {
		for from := first; from.Compare(last) <= 0; {
			to := sim.AddDays(from, span-1)
			if to.Compare(last) > 0 {
				to = last
			}
			if err := Import(ctx, a, n, from, to); err != nil {
				return err
			}
			a.SetImportedThrough(n, to)
			from = sim.AddDays(to, 1)
		}
	}
	return nil
}

// spanFor is how many days one statement of a profile covers: about rowsPerStatement rows, a
// month to a year.
func spanFor(h statements.History) int {
	perDay := float64(statements.Estimate(statements.History{Density: h.Normal().Density})) / 365
	return max(31, min(366, int(rowsPerStatement/perDay)))
}
