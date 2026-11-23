package sim

import (
	"maps"
	"slices"

	"github.com/x2db736ac85h/finuance/backend/internal/sim/identity"
)

// Areas are the feature areas of the action catalogue (PLAN Phase 25, "The action catalogue").
var Areas = []string{"table", "insights", "imports", "labels", "groups", "rules", "budgets", "duplicates", "cash",
	"assistant", "sharing", "account"}

// The modes (ADR 0044): realistic plays the characters at their own pace; load paces every persona
// by one lab-wide target.
const (
	ModeRealistic = "realistic"
	ModeLoad      = "load"
)

// Config is what the tuning page changes. It lives in memory; its defaults come from flags or the
// chart's values. Clamp holds every value to the ceilings.
type Config struct {
	// Mode is ModeRealistic or ModeLoad.
	Mode string `json:"mode"`
	// Rate is load mode's lab-wide target, in actions a second.
	Rate int `json:"rate"`
	// RampSeconds is how long load mode takes to move from one target to the next (from nothing
	// when it starts).
	RampSeconds int `json:"rampSeconds"`
	// LoadMinutes is how long load mode runs before it returns to realistic mode by itself.
	LoadMinutes int `json:"loadMinutes"`
	// PushThrough keeps load mode going through 5xx answers instead of backing off, to find a
	// breaking point. Realistic mode always backs off.
	PushThrough bool `json:"pushThrough"`
	// RunSeed enters every persona's random stream, so a run can be repeated; 0 draws a new one.
	RunSeed int64 `json:"runSeed"`
	// Jitter is how much think times vary around the character's mean, in percent (100 is the
	// character's own range, 0 always the mean); in load mode, around the pacer's interval.
	Jitter int `json:"jitter"`
	// Bursts is the chance, in per mille, that an action starts a burst of a few quick ones; in load
	// mode it deepens the pacer's bucket (1000: one second's worth).
	Bursts int `json:"bursts"`
	// Personas is how many personas play: the first N (identity.First).
	Personas int `json:"personas"`
	// Off lists personas (by slug) that sit this out.
	Off []string `json:"off,omitempty"`
	// Paused stops every persona after its current action.
	Paused bool `json:"paused"`
	// Intensity scales the characters' pace, in per mille: 1000 is their own pace, 0 is idle.
	Intensity int `json:"intensity"`
	// AreasOff lists feature areas that are switched off.
	AreasOff []string `json:"areasOff,omitempty"`
	// Weights overrides an action's weight for every persona (0 switches it off).
	Weights map[string]int `json:"weights,omitempty"`
	// PersonaWeights overrides an action's weight for one persona: slug → action → weight.
	PersonaWeights map[string]map[string]int `json:"personaWeights,omitempty"`
	// ActiveHours makes personas follow their characters' daily curves; off, they act all day.
	ActiveHours bool `json:"activeHours"`
	// ClockRate is how many data days pass per real day (1 is real time).
	ClockRate int `json:"clockRate"`
	// Mistakes is the share of deliberate mistakes in per mille (the 4xx traffic besides Sloth's).
	Mistakes int `json:"mistakes"`
	// AssistantPerDay is the most questions to the assistant across all personas in one day.
	AssistantPerDay int `json:"assistantPerDay"`
}

// Defaults is the configuration a simulator starts with when nothing else is given.
func Defaults() Config {
	return Config{Mode: ModeRealistic, Personas: len(identity.Animals), Intensity: 1000, ActiveHours: true, ClockRate: 1,
		Mistakes: 20, AssistantPerDay: 100, Rate: 50, RampSeconds: 60, LoadMinutes: 30, Jitter: 100}
}

// Clamp returns c held to the ceilings: every number within its range, unknown areas, personas
// and weights below zero dropped. The page can send anything; the engine runs only this.
func (c Config) Clamp() Config {
	if c.Mode != ModeLoad {
		c.Mode, c.PushThrough = ModeRealistic, false
		c.Personas = clamp(c.Personas, 1, MaxPersonas)
	} else {
		c.Personas = clamp(c.Personas, 1, MaxLoadPersonas)
	}
	c.Rate = clamp(c.Rate, 1, MaxRate)
	c.RampSeconds = clamp(c.RampSeconds, 0, MaxRampSeconds)
	c.LoadMinutes = clamp(c.LoadMinutes, 1, MaxLoadMinutes)
	c.Jitter = clamp(c.Jitter, 0, 100)
	c.Bursts = clamp(c.Bursts, 0, 1000)
	if c.RunSeed < 0 || c.RunSeed > MaxRunSeed {
		c.RunSeed = 0 // the engine draws one
	}
	c.Intensity = clamp(c.Intensity, 0, MaxIntensity)
	c.ClockRate = clamp(c.ClockRate, 1, MaxClockRate)
	c.Mistakes = clamp(c.Mistakes, 0, 1000)
	c.AssistantPerDay = clamp(c.AssistantPerDay, 0, MaxAssistantPerDay)
	c.AreasOff = slices.DeleteFunc(slices.Clone(c.AreasOff), func(a string) bool { return !slices.Contains(Areas, a) })
	c.Off = slices.DeleteFunc(slices.Clone(c.Off), func(s string) bool { _, ok := identity.BySlug(s); return !ok })
	c.Weights = nonNegative(c.Weights)
	if c.PersonaWeights != nil {
		pw := map[string]map[string]int{}
		for slug, w := range c.PersonaWeights {
			if _, ok := identity.BySlug(slug); ok {
				pw[slug] = nonNegative(w)
			}
		}
		c.PersonaWeights = pw
	}
	return c
}

func clamp(v, lo, hi int) int { return max(lo, min(hi, v)) }

func nonNegative(w map[string]int) map[string]int {
	if w == nil {
		return nil
	}
	out := maps.Clone(w)
	for k, v := range out {
		out[k] = clamp(v, 0, 1000)
	}
	return out
}

// areaOn reports whether a feature area is switched on.
func (c *Config) areaOn(area string) bool { return !slices.Contains(c.AreasOff, area) }

// personaOn reports whether a persona plays.
func (c *Config) personaOn(p identity.Persona) bool {
	return p.Index < c.Personas && !slices.Contains(c.Off, p.Slug)
}

// weight is an action's weight for a persona: its override for that persona, else the override
// for everyone, else the character's own.
func (c *Config) weight(slug, action string, own int) int {
	if w, ok := c.PersonaWeights[slug][action]; ok {
		return w
	}
	if w, ok := c.Weights[action]; ok {
		return w
	}
	return own
}
