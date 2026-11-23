package sim

import "maps"

// Presets change the configuration in one step (PLAN 25.5): each starts from the current one. The
// load presets (ADR 0044) switch to load mode with a hundred animals, the lab's default number;
// the others shape what the animals do in either mode, and quiet-day returns to realistic mode.
var Presets = map[string]func(Config) Config{
	"quiet-day": func(c Config) Config {
		c.Mode, c.Intensity, c.Mistakes, c.Weights, c.AreasOff, c.ActiveHours = ModeRealistic, 300, 10, nil, nil, true
		return c
	},
	// ramp climbs for 20 minutes to a target past a small machine's breaking point, through errors
	"ramp": func(c Config) Config {
		c.Mode, c.Personas, c.Rate, c.RampSeconds, c.LoadMinutes, c.PushThrough = ModeLoad, 100, 200, 1200, 30, true
		c.Jitter, c.Bursts = 20, 0
		return c
	},
	// soak holds a moderate target for an hour, backing off on errors
	"soak": func(c Config) Config {
		c.Mode, c.Personas, c.Rate, c.RampSeconds, c.LoadMinutes, c.PushThrough = ModeLoad, 100, 50, 120, 60, false
		c.Jitter, c.Bursts = 30, 0
		return c
	},
	// spike lets a second's worth of actions through at once, again and again, for ten minutes
	"spike": func(c Config) Config {
		c.Mode, c.Personas, c.Rate, c.RampSeconds, c.LoadMinutes, c.PushThrough = ModeLoad, 100, 100, 0, 10, false
		c.Jitter, c.Bursts = 100, 1000
		return c
	},
	"month-end": func(c Config) Config {
		c.Intensity = 1500
		c.Weights = with(c.Weights, map[string]int{"imports.month": 60, "budgets.read": 30, "budgets.set": 20, "insights.overview": 40,
			"insights.charts": 30, "duplicates.list": 10})
		return c
	},
	"assistant-heavy": func(c Config) Config {
		c.Weights = with(c.Weights, map[string]int{"assistant.ask": 60, "assistant.apply": 30, "assistant.revert": 10,
			"assistant.stop": 10, "assistant.digest": 5, "cash.receipt": 10})
		c.AssistantPerDay = MaxAssistantPerDay
		return c
	},
	"import-flood": func(c Config) Config {
		c.Intensity = 2000
		c.Weights = with(c.Weights, map[string]int{"imports.month": 60, "imports.overlap": 40, "imports.again": 20,
			"imports.remap": 20, "duplicates.list": 20, "duplicates.prefer": 10})
		return c
	},
	"error-storm": func(c Config) Config {
		c.Mistakes = 300
		c.Weights = with(c.Weights, map[string]int{"account.unknown-path": 30, "account.wrong-password": 10, "imports.broken": 30})
		return c
	},
}

func with(base, add map[string]int) map[string]int {
	out := maps.Clone(base)
	if out == nil {
		out = map[string]int{}
	}
	maps.Copy(out, add)
	return out
}
