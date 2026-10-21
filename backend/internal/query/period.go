package query

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

// Resolved is a period as concrete booking dates: From inclusive, To exclusive (PLAN §4.5, port of
// legacy resolvePeriod). Kind is month, year or range; it decides the previous period.
type Resolved struct {
	Kind  string
	From  platform.Date
	To    platform.Date
	Label string
}

// Resolve turns p into dates. Presets anchor to the latest booking date of the context's accounts
// (anchor; statements are historical, so "last 30 days" means the last 30 days of data). With no
// data (anchor nil) a preset resolves to nil, i.e. all time, as in the legacy. A nil p is all time.
func Resolve(p *Period, anchor *platform.Date) (*Resolved, error) {
	if p == nil {
		return nil, nil
	}
	ve := &platform.ValidationError{}
	set := 0
	for _, b := range []bool{p.Preset != "", p.Month != "", p.Year != 0, p.From != nil || p.To != nil} {
		if b {
			set++
		}
	}
	if set != 1 {
		ve.Add("context.period", "set exactly one of preset, month, year or from/to")
		return nil, ve
	}
	switch {
	case p.Preset != "":
		switch p.Preset {
		case "l30", "l3m", "l12m", "ytd":
		default:
			ve.Add("context.period.preset", fmt.Sprintf("unknown preset %q (l30, l3m, l12m, ytd)", p.Preset))
			return nil, ve
		}
		if anchor == nil {
			return nil, nil
		}
		end := anchor.AddDays(1)
		ms := platform.Date{Y: anchor.Y, M: anchor.M, D: 1}
		switch p.Preset {
		case "l30":
			return &Resolved{"range", end.AddDays(-30), end, "Last 30 days"}, nil
		case "l3m":
			return &Resolved{"range", ms.AddMonths(-2), ms.AddMonths(1), "Last 3 months"}, nil
		case "l12m":
			return &Resolved{"range", ms.AddMonths(-11), ms.AddMonths(1), "Last 12 months"}, nil
		}
		return &Resolved{"range", platform.Date{Y: anchor.Y, M: time.January, D: 1}, end, "Year to date " + strconv.Itoa(anchor.Y)}, nil
	case p.Month != "":
		from, err := parseMonth(p.Month)
		if err != nil {
			ve.Add("context.period.month", err.Error())
			return nil, ve
		}
		return &Resolved{"month", from, from.AddMonths(1), from.Time().Format("Jan 2006")}, nil
	case p.Year != 0:
		if p.Year < 1000 || p.Year > 9998 {
			ve.Add("context.period.year", "must be a four-digit year")
			return nil, ve
		}
		from := platform.Date{Y: p.Year, M: time.January, D: 1}
		return &Resolved{"year", from, platform.Date{Y: p.Year + 1, M: time.January, D: 1}, strconv.Itoa(p.Year)}, nil
	}
	if p.From == nil || p.To == nil || !p.From.Before(*p.To) {
		ve.Add("context.period", "a custom period needs from < to (to is exclusive)")
		return nil, ve
	}
	label := fmtDay(*p.From)
	if last := p.To.AddDays(-1); last != *p.From {
		label += " – " + fmtDay(last)
	}
	return &Resolved{"range", *p.From, *p.To, label}, nil
}

// Prev is the period before r (legacy prevRange): the previous month or year, or an equally long
// range ending where r starts. Comparisons (previous period deltas) use it.
func (r Resolved) Prev() (from, to platform.Date) {
	switch r.Kind {
	case "month":
		return r.From.AddMonths(-1), r.From
	case "year":
		return platform.Date{Y: r.From.Y - 1, M: time.January, D: 1}, r.From
	}
	days := int(r.To.Time().Sub(r.From.Time()).Hours() / 24)
	return r.From.AddDays(-days), r.From
}

func parseMonth(s string) (platform.Date, error) {
	y, m, ok := strings.Cut(s, "-")
	yy, err1 := strconv.Atoi(y)
	mm, err2 := strconv.Atoi(m)
	if !ok || len(y) != 4 || len(m) != 2 || err1 != nil || err2 != nil || mm < 1 || mm > 12 || yy < 1000 {
		return platform.Date{}, fmt.Errorf("must be YYYY-MM, got %q", s)
	}
	return platform.Date{Y: yy, M: time.Month(mm), D: 1}, nil
}

func fmtDay(d platform.Date) string { return d.Time().Format("2 Jan 2006") }
