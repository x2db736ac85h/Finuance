package sim

import (
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/sim/statements"
)

// The data calendar (PLAN Phase 25, "Statements on demand"): the day a persona's bank history has
// reached. At the configuration's ClockRate it runs faster than the clock (one data day a minute at
// most), so personas reach month ends and imports often; it never passes today. A persona that
// starts without data begins three years back when the calendar runs fast, and today otherwise
// (then its first import brings a year of history).

// historyDays is how far back a persona's first import reaches when its calendar starts today: its
// history profile's years (one by default).
func (a *Actor) historyDaysLocked() int { return a.history.Normal().Years * 365 }

// Today is the instance's today, in its time zone.
func (a *Actor) Today() platform.Date {
	return platform.DateOf(a.engine.o.Clock.Now(), a.engine.o.Location)
}

// DataDay is the day the persona's history has reached.
func (a *Actor) DataDay() platform.Date {
	cfg, _ := a.engine.Config()
	now := a.engine.o.Clock.Now()
	today := a.Today()
	a.mu.Lock()
	defer a.mu.Unlock()
	day := a.dataDayLocked(now, today)
	if a.cal.rate != cfg.ClockRate { // a new rate runs from where the old one got to
		a.cal = calendar{anchorReal: now, anchorDay: day, rate: cfg.ClockRate}
	}
	return day
}

func (a *Actor) dataDayLocked(now time.Time, today platform.Date) platform.Date {
	if a.cal.rate == 0 {
		return today
	}
	days := int(now.Sub(a.cal.anchorReal) * time.Duration(a.cal.rate) / (24 * time.Hour))
	day := addDays(a.cal.anchorDay, days)
	if day.Compare(today) > 0 {
		return today
	}
	return day
}

// startCalendarLocked places the persona's calendar after discovery (the caller holds a.mu): where
// its current account's data ends, or, without data, three years back with a fast clock and today
// otherwise. rate is the configuration's ClockRate.
func (a *Actor) startCalendarLocked(lastOn map[int]*platform.Date, rate int) {
	now := a.engine.o.Clock.Now()
	today := platform.DateOf(now, a.engine.o.Location)
	start := today
	switch cur := lastOn[statements.Current]; {
	case cur != nil && rate > 1:
		start = *cur
	case cur == nil && rate > 1:
		start = addDays(today, -3*365)
	}
	a.cal = calendar{anchorReal: now, anchorDay: start, rate: rate}
	for _, n := range []int{statements.Current, statements.Savings} {
		if d := lastOn[n]; d != nil {
			a.imported[n] = *d
		} else {
			a.imported[n] = addDays(start, -a.historyDaysLocked()-1)
		}
	}
}

// ImportedThrough is the last day of the account's history the persona has imported.
func (a *Actor) ImportedThrough(account int) platform.Date {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.imported[account]
}

// SetImportedThrough records the last imported day of the account.
func (a *Actor) SetImportedThrough(account int, d platform.Date) {
	a.mu.Lock()
	a.imported[account] = d
	a.mu.Unlock()
}

// Restart starts the persona's calendar again three years back (after its accounts were deleted).
func (a *Actor) Restart() {
	now := a.engine.o.Clock.Now()
	today := platform.DateOf(now, a.engine.o.Location)
	cfg, _ := a.engine.Config()
	a.mu.Lock()
	defer a.mu.Unlock()
	start := addDays(today, -3*365)
	a.cal = calendar{anchorReal: now, anchorDay: start, rate: cfg.ClockRate}
	back := addDays(start, -a.historyDaysLocked()-1)
	a.imported = map[int]platform.Date{statements.Current: back, statements.Savings: back}
	a.hasData = false
}

// StartHistory starts a rebuilt history (its accounts made again): the calendar at today, and
// nothing imported since its profile's years back, which the rebuild then imports. It returns the
// first day of that history.
func (a *Actor) StartHistory() platform.Date {
	now := a.engine.o.Clock.Now()
	today := platform.DateOf(now, a.engine.o.Location)
	cfg, _ := a.engine.Config()
	a.mu.Lock()
	defer a.mu.Unlock()
	first := addDays(today, -a.historyDaysLocked())
	a.cal = calendar{anchorReal: now, anchorDay: today, rate: cfg.ClockRate}
	a.imported = map[int]platform.Date{statements.Current: addDays(first, -1), statements.Savings: addDays(first, -1)}
	a.hasData = false
	return first
}

type calendar struct {
	anchorReal time.Time
	anchorDay  platform.Date
	rate       int
}

// AddDays moves a date by n days.
func AddDays(d platform.Date, n int) platform.Date { return addDays(d, n) }

func addDays(d platform.Date, n int) platform.Date {
	return platform.DateOf(d.Time().AddDate(0, 0, n), time.UTC)
}
