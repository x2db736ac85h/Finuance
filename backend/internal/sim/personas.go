package sim

import (
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/sim/identity"
)

// AnimalOrder is the ten animals in the order personas are numbered.
func AnimalOrder() []string { return identity.Animals[:] }

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
		Weights: map[string]int{"imports.month": 25, "insights.overview": 30, "table.window": 15, "labels.category": 15, "labels.note": 5, "rules.suggestion": 8, "rules.preview": 4, "rules.apply": 3, "rules.category": 1, "rules.merge": 1, "budgets.read": 5, "duplicates.list": 2, "account.notifications": 5, "account.preferences": 3}},
	"owl": {Animal: "owl", Says: "the analyst: long periods, every chart, the filter language, CSV exports",
		Hours: nightOwl, Think: [2]time.Duration{10 * time.Second, 90 * time.Second},
		Weights: map[string]int{"insights.charts": 30, "table.filter": 25, "table.search": 15, "table.distinct": 10, "table.range": 5, "table.window": 10, "insights.overview": 5, "groups.timeline": 3, "imports.month": 5, "account.export": 1}},
	"badger": {Animal: "badger", Says: "the budgeter: budgets and goals, the pace chart, limits at the start of a month",
		Hours: officeHours, Think: [2]time.Duration{30 * time.Second, 4 * time.Minute},
		Weights: map[string]int{"budgets.read": 25, "budgets.set": 15, "budgets.goal": 5, "budgets.goal-update": 8, "insights.charts": 15, "insights.overview": 15, "imports.month": 8, "account.notifications": 5}},
	"magpie": {Animal: "magpie", Says: "the collector: groups for every trip, bulk labels, group reports",
		Hours: officeHours, Think: [2]time.Duration{15 * time.Second, 2 * time.Minute},
		Weights: map[string]int{"groups.create": 8, "groups.rename": 4, "groups.timeline": 10, "groups.report": 10, "groups.delete": 2, "labels.bulk-group": 15, "labels.bulk-category": 8, "table.range": 10, "table.search": 10, "table.window": 10, "sharing.expense": 8, "sharing.accept": 5, "sharing.report": 4, "imports.month": 8}},
	"heron": {Animal: "heron", Says: "the sharer: friends by code, shared groups, expenses, settling up",
		Hours: officeHours, Think: [2]time.Duration{30 * time.Second, 5 * time.Minute},
		Weights: map[string]int{"sharing.code": 6, "sharing.befriend": 8, "sharing.group": 5, "sharing.accept": 8, "sharing.expense": 15, "sharing.settle": 8, "sharing.report": 8, "sharing.leave": 1, "account.notifications": 20, "insights.overview": 8, "imports.month": 8}},
	"hedgehog": {Animal: "hedgehog", Says: "cash only: a cash account, expenses typed in by hand, receipts by photo",
		Hours: earlyBird, Think: [2]time.Duration{1 * time.Minute, 8 * time.Minute},
		Weights: map[string]int{"cash.add": 30, "cash.edit": 10, "cash.delete": 5, "cash.receipt": 5, "table.window": 10, "insights.overview": 8, "budgets.read": 4, "sharing.accept": 3, "sharing.expense": 3, "imports.month": 3, "account.preferences": 3}},
	"fox": {Animal: "fox", Says: "the one who asks: questions to the assistant, applies and reverts proposals",
		Hours: allDay, Think: [2]time.Duration{20 * time.Second, 3 * time.Minute},
		Weights: map[string]int{"assistant.ask": 25, "assistant.apply": 12, "assistant.revert": 5, "assistant.stop": 4, "assistant.digest": 3, "insights.overview": 10, "table.search": 8, "imports.month": 8, "account.notifications": 5}},
	"squirrel": {Animal: "squirrel", Says: "the hoarder: two banks, overlapping exports, the duplicates window",
		Hours: officeHours, Think: [2]time.Duration{20 * time.Second, 3 * time.Minute},
		Weights: map[string]int{"imports.month": 15, "imports.overlap": 15, "imports.again": 8, "imports.remap": 6, "duplicates.list": 12, "duplicates.prefer": 6, "duplicates.keep-both": 4, "duplicates.clear": 3, "table.window": 10, "table.distinct": 5, "insights.overview": 5}},
	"mole": {Animal: "mole", Says: "the rare visitor: once a week, the overview, sometimes a mistyped password",
		Hours: officeHours, Think: [2]time.Duration{6 * time.Hour, 72 * time.Hour},
		Weights: map[string]int{"insights.overview": 60, "account.wrong-password": 15, "table.window": 10, "imports.month": 10}},
	"sloth": {Animal: "sloth", Says: "slow and clumsy: broken files, abandoned previews, validation errors",
		Hours: allDay, Think: [2]time.Duration{1 * time.Minute, 10 * time.Minute},
		Weights: map[string]int{"account.unknown-path": 15, "account.wrong-password": 8, "imports.broken": 15, "imports.abandon": 10, "table.filter": 10, "table.window": 8, "assistant.stop": 3, "insights.overview": 5, "imports.month": 5}},
}
