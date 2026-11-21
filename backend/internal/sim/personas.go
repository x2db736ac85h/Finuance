package sim

import "time"

// Character is an animal's habits (PLAN Phase 25, "The animals"): what it mostly does, when it is
// awake and how long it thinks between two actions. Personas above ten reuse their animal's.
type Character struct {
	Animal string
	// Says is the character in a few words, for the tuning page.
	Says string
	// Weights draws the next action: action name → weight. Actions not listed are never drawn
	// for this character unless the configuration gives them a weight.
	Weights map[string]int
	// Hours is the activity per hour of the day in the instance's time zone, 0 (asleep) to 10.
	Hours [24]int
	// Think is the pause between two actions at intensity 1×, drawn uniformly from [min, max].
	Think [2]time.Duration
}

// Daily curves.
var (
	officeHours = [24]int{0, 0, 0, 0, 0, 0, 1, 4, 6, 3, 2, 2, 4, 3, 2, 2, 3, 5, 7, 8, 7, 5, 3, 1}
	nightOwl    = [24]int{4, 3, 1, 0, 0, 0, 0, 0, 1, 1, 1, 2, 2, 2, 2, 2, 3, 4, 5, 7, 9, 10, 9, 7}
	earlyBird   = [24]int{0, 0, 0, 0, 0, 2, 7, 9, 8, 5, 3, 3, 3, 2, 2, 2, 2, 3, 4, 4, 2, 1, 0, 0}
	allDay      = [24]int{1, 1, 1, 1, 1, 1, 2, 3, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 3, 3, 2, 1}
)

// Characters are the ten animals' habits, by animal. The weights name actions of the catalogue
// (internal/sim/actions); TestCharactersNameRealActions keeps them in step.
var Characters = map[string]Character{
	"otter": {Animal: "otter", Says: "the tidy one: imports each month, categorises everything, opens the overview daily",
		Hours: earlyBird, Think: [2]time.Duration{20 * time.Second, 3 * time.Minute},
		Weights: map[string]int{"insights.overview": 30, "table.window": 20, "table.filter": 10, "insights.charts": 10,
			"account.notifications": 5, "account.preferences": 3}},
	"owl": {Animal: "owl", Says: "the analyst: long periods, every chart, the filter language, CSV exports",
		Hours: nightOwl, Think: [2]time.Duration{10 * time.Second, 90 * time.Second},
		Weights: map[string]int{"insights.charts": 30, "table.filter": 25, "table.search": 15, "table.distinct": 10,
			"table.range": 5, "table.window": 10, "insights.overview": 5, "account.export": 1}},
	"badger": {Animal: "badger", Says: "the budgeter: budgets and goals, the pace chart, limits at the start of a month",
		Hours: officeHours, Think: [2]time.Duration{30 * time.Second, 4 * time.Minute},
		Weights: map[string]int{"insights.charts": 25, "insights.overview": 20, "table.window": 10, "account.notifications": 5}},
	"magpie": {Animal: "magpie", Says: "the collector: groups for every trip, bulk labels, group reports",
		Hours: officeHours, Think: [2]time.Duration{15 * time.Second, 2 * time.Minute},
		Weights: map[string]int{"table.window": 20, "table.range": 15, "table.search": 10, "insights.charts": 5}},
	"heron": {Animal: "heron", Says: "the sharer: friends by code, shared groups, expenses, settling up",
		Hours: officeHours, Think: [2]time.Duration{30 * time.Second, 5 * time.Minute},
		Weights: map[string]int{"account.notifications": 25, "insights.overview": 10, "table.window": 10}},
	"hedgehog": {Animal: "hedgehog", Says: "cash only: a cash account, expenses typed in by hand, receipts by photo",
		Hours: earlyBird, Think: [2]time.Duration{1 * time.Minute, 8 * time.Minute},
		Weights: map[string]int{"table.window": 20, "insights.overview": 10, "account.preferences": 5}},
	"fox": {Animal: "fox", Says: "the one who asks: questions to the assistant, applies and reverts proposals",
		Hours: allDay, Think: [2]time.Duration{20 * time.Second, 3 * time.Minute},
		Weights: map[string]int{"insights.overview": 15, "table.search": 10, "insights.charts": 10, "account.notifications": 5}},
	"squirrel": {Animal: "squirrel", Says: "the hoarder: two banks, overlapping exports, the duplicates window",
		Hours: officeHours, Think: [2]time.Duration{20 * time.Second, 3 * time.Minute},
		Weights: map[string]int{"table.window": 20, "table.distinct": 10, "insights.overview": 10}},
	"mole": {Animal: "mole", Says: "the rare visitor: once a week, the overview, sometimes a mistyped password",
		Hours: officeHours, Think: [2]time.Duration{6 * time.Hour, 72 * time.Hour},
		Weights: map[string]int{"insights.overview": 60, "account.wrong-password": 15, "table.window": 10}},
	"sloth": {Animal: "sloth", Says: "slow and clumsy: broken files, abandoned previews, validation errors",
		Hours: allDay, Think: [2]time.Duration{1 * time.Minute, 10 * time.Minute},
		Weights: map[string]int{"account.unknown-path": 20, "account.wrong-password": 10, "table.filter": 10,
			"table.window": 10, "insights.overview": 5}},
}
