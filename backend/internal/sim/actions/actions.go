// Package actions is the traffic simulator's action catalogue (PLAN 25.4), one file per feature
// area. Each action calls the lab's API as the web app would and checks the answer it expects.
package actions

import (
	"github.com/x2db736ac85h/finuance/backend/internal/client/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/sim"
)

// All is the whole catalogue.
func All() []sim.Action {
	var out []sim.Action
	for _, area := range [][]sim.Action{table(), insights(), imports(), labels(), groups(), rules(), budgets(), duplicates(), cash(),
		assistant(), sharing(), account()} {
		out = append(out, area...)
	}
	return out
}

func ptr[T any](v T) *T { return &v }

// period draws one of the web app's period presets.
func period(a *sim.Actor) *gen.Period {
	return &gen.Period{Preset: ptr(sim.Pick(a, gen.L30, gen.L3m, gen.L3m, gen.L12m, gen.Ytd))}
}

// scope is the query context of most reads: every account, a drawn period.
func scope(a *sim.Actor) gen.QueryContext { return gen.QueryContext{Period: period(a)} }
