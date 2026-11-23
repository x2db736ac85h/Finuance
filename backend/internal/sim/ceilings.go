package sim

import (
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/sim/identity"
)

// The ceilings (PLAN Phase 25, decision 7; ADR 0044 for load mode). They are in the code, not only
// on the tuning page: no configuration can pass them. Realistic mode keeps ADR 0043's; load mode,
// lab only, has its own.
const (
	// MaxPersonas is the most personas realistic mode plays.
	MaxPersonas = 50
	// MinGap is the shortest time between two actions of one persona: at most 30 a minute.
	MinGap = 2 * time.Second
	// MaxLoadPersonas is the most personas load mode plays.
	MaxLoadPersonas = identity.Max
	// MinLoadGap is the shortest time between two actions of one persona in load mode: at most two
	// a second (an access token may make 600 requests a minute, an action makes about three).
	MinLoadGap = 500 * time.Millisecond
	// MaxRate is load mode's highest lab-wide target, in actions a second; bursts add at most one
	// second's worth (Bursts).
	MaxRate = 1000
	// MaxRampSeconds is the slowest ramp from one target to the next.
	MaxRampSeconds = 3600
	// MaxLoadMinutes is the longest load mode runs before it returns to realistic mode by itself.
	MaxLoadMinutes = 24 * 60
	// SignInBurst and SignInEvery pace the personas' sign-ins below the instance's limit per
	// address (30 a minute), which every persona shares behind the front door.
	SignInBurst = 25
	SignInEvery = 2500 * time.Millisecond
	// MaxRebuilds is how many personas rebuild their history at once (each import waits for the
	// worker's commit).
	MaxRebuilds = 8
	// FeedRate is the most actions a second the feed sends one viewer; beyond it, it samples.
	FeedRate = 50
	// MaxRunSeed is the largest run seed: a page in a browser reads numbers up to 2^53 exactly.
	MaxRunSeed = 1<<53 - 1
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
