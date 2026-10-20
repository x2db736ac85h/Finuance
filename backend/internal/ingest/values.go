package ingest

import (
	"math"
	"regexp"
	"strconv"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/money"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

// ParseNumber returns the value of s as a float (legacy `parseNum`). It is used only to decide
// whether a column is numeric and for the legacy-parity tests — never for money, which goes through
// money.ParseMinor. The validation and separator rules live in money.Split, shared by both.
func ParseNumber(s string) (float64, bool) {
	p, ok := money.Split(s)
	if !ok {
		return 0, false
	}
	t := p.Int
	if p.Frac != "" {
		t += "." + p.Frac
	}
	if t == "" {
		return 0, false
	}
	x, err := strconv.ParseFloat(t, 64)
	if err != nil || math.IsInf(x, 0) {
		return 0, false
	}
	if p.Negative {
		x = -x
	}
	return x, true
}

var (
	// isoDate is anchored at the start only — like the legacy regex, "2025-01-02x" parses.
	isoDate = regexp.MustCompile(`^(\d{4})[-./](\d{1,2})[-./](\d{1,2})(?:[ T](\d{1,2}):(\d{2})(?::(\d{2}))?)?`)
	// dmyDate is day-first and anchored at both ends, with an optional time.
	dmyDate = regexp.MustCompile(`^(\d{1,2})[./-](\d{1,2})[./-](\d{4})(?:[ T,]+(\d{1,2}):(\d{2})(?::(\d{2}))?)?$`)
	// mdyCandidate is the shape that DateOrderMDY reinterprets as month/day/year.
	mdyCandidate = regexp.MustCompile(`^(\d{1,2})[./-](\d{1,2})[./-](\d{4})(.*)$`)
)

// DateOrder says how to read ambiguous dd/mm/yyyy-shaped values.
type DateOrder int

// Date orders. DMY (day first) is the default for European exports.
const (
	DateOrderDMY DateOrder = iota
	DateOrderMDY
)

// ParseDate parses a booking date (legacy `parseDate`):
//
//   - ISO-like yyyy-m-d with "-", "." or "/" separators, optionally followed by " hh:mm[:ss]" or
//     "Thh:mm[:ss]" (anything after the date is accepted, as in the legacy regex);
//   - day-first d.m.yyyy, d/m/yyyy or d-m-yyyy, optionally with a time.
//
// Times are ignored: booking dates are calendar dates (ADR 0005). [CHANGE] Impossible dates such as
// 31.02.2025 are rejected instead of rolling over to March. See ALLOWED_DIFFS.md.
func ParseDate(s string) (platform.Date, bool) {
	s = jsTrim(s)
	if m := isoDate.FindStringSubmatch(s); m != nil {
		return mkDate(m[1], m[2], m[3])
	}
	if m := dmyDate.FindStringSubmatch(s); m != nil {
		return mkDate(m[3], m[2], m[1])
	}
	return platform.Date{}, false
}

// ParseDateOrdered is ParseDate, except that with DateOrderMDY a dd/mm/yyyy-shaped value is read
// month first (10/31/2025). The legacy app rewrote such values to ISO before parsing; this is the
// same rule without touching the stored raw value.
func ParseDateOrdered(s string, order DateOrder) (platform.Date, bool) {
	if order == DateOrderMDY {
		if m := mdyCandidate.FindStringSubmatch(jsTrim(s)); m != nil {
			return ParseDate(m[3] + "-" + pad2(m[1]) + "-" + pad2(m[2]) + m[4])
		}
	}
	return ParseDate(s)
}

func pad2(s string) string {
	if len(s) == 1 {
		return "0" + s
	}
	return s
}

// mkDate validates year/month/day strings. Years below 1000 other than 0 are kept, but legacy
// Date.UTC maps years 0–99 to 1900–1999; those are rejected here (ALLOWED_DIFFS.md).
func mkDate(ys, ms, ds string) (platform.Date, bool) {
	y, _ := strconv.Atoi(ys)
	m, _ := strconv.Atoi(ms)
	d, _ := strconv.Atoi(ds)
	if y < 100 {
		return platform.Date{}, false
	}
	dt, err := platform.NewDate(y, time.Month(m), d)
	return dt, err == nil
}

var (
	looseYear      = regexp.MustCompile(`^(\d{4})$`)
	looseYearMonth = regexp.MustCompile(`^(\d{4})[-./](\d{1,2})$`)
	looseMonthYear = regexp.MustCompile(`^(\d{1,2})[-./](\d{4})$`)
)

// LooseDate parses a date as typed into a filter and returns the half-open interval [From, To)
// it covers (legacy `looseDate`). It makes "=2025-03", ">=2024" and "01.2025..03.2025" intuitive:
//
//   - a full date (anything ParseDate accepts) → that day; [CHANGE] a time narrows nothing here
//     (the legacy used a one-minute window), because booking dates have no time;
//   - "yyyy" → that year;
//   - "yyyy-mm", "yyyy.mm", "yyyy/mm" → that month;
//   - "mm.yyyy", "mm-yyyy", "mm/yyyy" → that month.
//
// Months outside 1–12 roll over like the legacy Date.UTC ("2025-13" is January 2026). Years
// below 100 are rejected, as in ParseDate.
func LooseDate(s string) (from, to platform.Date, ok bool) {
	s = jsTrim(s)
	if d, ok := ParseDate(s); ok {
		return d, d.AddDays(1), true
	}
	month := func(ys, ms string) (platform.Date, platform.Date, bool) {
		y, _ := strconv.Atoi(ys)
		m, _ := strconv.Atoi(ms)
		if y < 100 {
			return platform.Date{}, platform.Date{}, false
		}
		start := platform.Date{Y: y, M: time.January, D: 1}.AddMonths(m - 1)
		if start.Y > 9999 {
			return platform.Date{}, platform.Date{}, false
		}
		return start, start.AddMonths(1), true
	}
	if m := looseYear.FindStringSubmatch(s); m != nil {
		y, _ := strconv.Atoi(m[1])
		if y < 100 {
			return platform.Date{}, platform.Date{}, false
		}
		return platform.Date{Y: y, M: time.January, D: 1}, platform.Date{Y: y + 1, M: time.January, D: 1}, true
	}
	if m := looseYearMonth.FindStringSubmatch(s); m != nil {
		return month(m[1], m[2])
	}
	if m := looseMonthYear.FindStringSubmatch(s); m != nil {
		return month(m[2], m[1])
	}
	return platform.Date{}, platform.Date{}, false
}
