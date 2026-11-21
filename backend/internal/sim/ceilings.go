package sim

import (
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/sim/identity"
)

// The ceilings (PLAN Phase 25, decision 7). They are in the code, not only on the tuning page:
// no configuration can pass them, so the simulator can't be turned into a load generator.
const (
	// MaxPersonas is the most personas the engine plays.
	MaxPersonas = identity.Max
	// MinGap is the shortest time between two actions of one persona: at most 30 a minute.
	MinGap = 2 * time.Second
	// MaxIntensity is the highest intensity, in per mille of the characters' own pace (3×).
	MaxIntensity = 3000
	// MaxClockRate is the fastest data calendar, in data days per real day: one a minute.
	MaxClockRate = 1440
	// ServerErrorsInARow is how many 5xx answers in a row send a persona into a back-off.
	ServerErrorsInARow = 5
	// FirstBackoff is the first back-off after ServerErrorsInARow; each further one doubles.
	FirstBackoff = time.Minute
	// MaxBackoff is the longest back-off.
	MaxBackoff = 15 * time.Minute
	// MaxRetryAfter caps a 429's Retry-After.
	MaxRetryAfter = 5 * time.Minute
	// ConfigCheck is how often a waiting persona looks at the configuration again: a change takes
	// effect within this time.
	ConfigCheck = 5 * time.Second
	// MaxAssistantPerDay caps the questions to the assistant across all personas in one day.
	MaxAssistantPerDay = 500
)
