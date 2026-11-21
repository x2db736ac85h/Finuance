package actions_test

import (
	"slices"
	"testing"

	"github.com/x2db736ac85h/finuance/backend/internal/sim"
	"github.com/x2db736ac85h/finuance/backend/internal/sim/actions"
	"github.com/x2db736ac85h/finuance/backend/internal/sim/identity"
)

// TestCharactersNameRealActions: every animal has a character, every weight names an action of
// the catalogue, and every action is drawn by some character (or is a mistake, which the mistake
// share draws).
func TestCharactersNameRealActions(t *testing.T) {
	names := map[string]sim.Action{}
	for _, a := range actions.All() {
		if _, dup := names[a.Name]; dup {
			t.Errorf("two actions named %s", a.Name)
		}
		if !slices.Contains(sim.Areas, a.Area) || a.Run == nil {
			t.Errorf("%s: area %q, run %v", a.Name, a.Area, a.Run != nil)
		}
		names[a.Name] = a
	}
	drawn := map[string]bool{}
	for _, animal := range identity.Animals {
		c, ok := sim.Characters[animal]
		if !ok || c.Animal != animal || c.Think[0] > c.Think[1] || c.Says == "" {
			t.Errorf("%s: character %+v", animal, c)
		}
		for name, w := range c.Weights {
			if _, ok := names[name]; !ok || w <= 0 {
				t.Errorf("%s weighs %q (%d), which is not an action", animal, name, w)
			}
			drawn[name] = true
		}
	}
	for name, a := range names {
		if !drawn[name] && !a.Mistake {
			t.Errorf("no character draws %s", name)
		}
	}
}
