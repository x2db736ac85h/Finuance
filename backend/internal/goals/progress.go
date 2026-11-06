// Package goals keeps the user's goals in plain language (Phase 20, ADR 0031) and works out their
// progress. Two kinds: save (put an amount aside by a deadline) and reduce (spend a percentage less
// on a category than before). Category limits stay budgets. This file holds the pure rules; the
// service in goals.go reads and writes them.
package goals

import (
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

// Kinds.
const (
	KindSave   = "save"
	KindReduce = "reduce"
)

// States of a goal's progress.
const (
	StateNotStarted     = "not_started"     // before its start
	StateOnTrack        = "on_track"        // save: at least 95 % of what the pace asks; reduce: within this month's allowance so far
	StateBehind         = "behind"          // the opposite
	StateDone           = "done"            // save: the amount is put aside; reduce: every month kept, the deadline passed
	StateMissed         = "missed"          // the deadline passed without getting there
	StateNeedsAttention = "needs_attention" // its category or account is gone, or a reduce goal has no spending to reduce
)

// SaveFacts are what a save goal's progress needs: the amount put aside since the start.
type SaveFacts struct {
	SavedMinor int64
}

// ReduceFacts are what a reduce goal's progress needs: money out per month in the category, keyed
// by the first day of the month, for the baseline months and every month since the start.
type ReduceFacts struct {
	Months map[platform.Date]int64
}

// Progress is how a goal is doing on a day. Amounts are minor units of the goal's currency;
// Percent is of the target (save) or of this month's allowance (reduce).
type Progress struct {
	State    string
	Percent  int
	DaysLeft int
	// save
	SavedMinor     int64
	ExpectedMinor  int64 // what the steady pace would have put aside by today
	ProjectedMinor int64 // at today's pace, by the deadline
	// reduce
	BaselineMinor     int64 // the average month before the start
	MonthlyLimitMinor int64 // what the goal allows a month
	ThisMonthMinor    int64
	AllowedSoFarMinor int64 // this month's allowance up to today
	MonthsKept        int   // full months since the start within the limit
	MonthsPast        int   // full months since the start
}

// SaveProgress works out a save goal on today.
func SaveProgress(start, deadline, today platform.Date, targetMinor int64, f SaveFacts) Progress {
	p := Progress{SavedMinor: f.SavedMinor, DaysLeft: max(0, today.DaysUntil(deadline))}
	total := start.DaysUntil(deadline) + 1
	elapsed := min(max(0, start.DaysUntil(today)+1), total)
	if targetMinor > 0 {
		p.Percent = int(max(0, f.SavedMinor) * 100 / targetMinor)
		p.ExpectedMinor = targetMinor * int64(elapsed) / int64(total)
	}
	if elapsed > 0 {
		p.ProjectedMinor = f.SavedMinor * int64(total) / int64(elapsed)
	}
	switch {
	case today.Before(start):
		p.State = StateNotStarted
	case f.SavedMinor >= targetMinor:
		p.State = StateDone
	case today.After(deadline):
		p.State = StateMissed
	case f.SavedMinor*100 >= p.ExpectedMinor*95:
		p.State = StateOnTrack
	default:
		p.State = StateBehind
	}
	return p
}

// BaselineRange is the span of a reduce goal's baseline: the baselineMonths full months before
// the month the goal starts in.
func BaselineRange(start platform.Date, baselineMonths int) (from, to platform.Date) {
	first := start.FirstOfMonth()
	return first.AddMonths(-baselineMonths), first.AddDays(-1)
}

// ReduceProgress works out a reduce goal on today.
func ReduceProgress(start, deadline, today platform.Date, reducePct, baselineMonths int, f ReduceFacts) Progress {
	p := Progress{DaysLeft: max(0, today.DaysUntil(deadline))}
	from, _ := BaselineRange(start, baselineMonths)
	var sum int64
	for m := from; m.Before(start.FirstOfMonth()); m = m.AddMonths(1) {
		sum += f.Months[m]
	}
	p.BaselineMinor = sum / int64(baselineMonths)
	p.MonthlyLimitMinor = p.BaselineMinor * int64(100-reducePct) / 100
	if p.BaselineMinor == 0 {
		p.State = StateNeedsAttention // nothing was spent there before: nothing to reduce
		return p
	}
	// the full months since the start's month (each counted once it is over), up to the deadline's
	for m := start.FirstOfMonth(); !m.After(deadline); m = m.AddMonths(1) {
		if !m.AddMonths(1).AddDays(-1).Before(today) {
			break // this month isn't over yet
		}
		p.MonthsPast++
		if f.Months[m] <= p.MonthlyLimitMinor {
			p.MonthsKept++
		}
	}
	month := today.FirstOfMonth()
	p.ThisMonthMinor = f.Months[month]
	days := month.DaysUntil(month.AddMonths(1))
	p.AllowedSoFarMinor = p.MonthlyLimitMinor * int64(today.D) / int64(days)
	if p.MonthlyLimitMinor > 0 {
		p.Percent = int(p.ThisMonthMinor * 100 / p.MonthlyLimitMinor)
	}
	switch {
	case today.Before(start):
		p.State = StateNotStarted
	case today.After(deadline) && p.MonthsKept == p.MonthsPast:
		p.State = StateDone
	case today.After(deadline):
		p.State = StateMissed
	case p.ThisMonthMinor <= p.AllowedSoFarMinor:
		p.State = StateOnTrack
	default:
		p.State = StateBehind
	}
	return p
}
