package platform

import (
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// Date is a calendar date without time or zone (ADR 0005). Bank booking and value dates are dates,
// not instants. Date is comparable with ==, usable as a map key, and maps to SQL `date`. The zero
// value is "no date".
type Date struct {
	Y int
	M time.Month
	D int
}

// ErrInvalidDate is returned for impossible or out-of-range dates (e.g. 2025-02-31).
var ErrInvalidDate = errors.New("invalid date")

// NewDate validates y-m-d; 2025-02-31 is an error, not 2025-03-03.
func NewDate(y int, m time.Month, d int) (Date, error) {
	if y < 1 || y > 9999 {
		return Date{}, fmt.Errorf("%w: year %d out of range", ErrInvalidDate, y)
	}
	t := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	if t.Year() != y || t.Month() != m || t.Day() != d {
		return Date{}, fmt.Errorf("%w: %04d-%02d-%02d", ErrInvalidDate, y, int(m), d)
	}
	return Date{y, m, d}, nil
}

// MustDate is NewDate for constants in tests and fixtures; it panics on invalid input.
func MustDate(y int, m time.Month, d int) Date {
	dt, err := NewDate(y, m, d)
	if err != nil {
		panic(err)
	}
	return dt
}

// ParseDate parses YYYY-MM-DD.
func ParseDate(s string) (Date, error) {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return Date{}, fmt.Errorf("%w: %q is not YYYY-MM-DD", ErrInvalidDate, s)
	}
	var y, m, d int
	if _, err := fmt.Sscanf(s, "%4d-%2d-%2d", &y, &m, &d); err != nil {
		return Date{}, fmt.Errorf("%w: %q is not YYYY-MM-DD", ErrInvalidDate, s)
	}
	return NewDate(y, time.Month(m), d)
}

// DateOf returns the calendar date of t in loc ("today" for a user is DateOf(clock.Now(), userTZ)).
func DateOf(t time.Time, loc *time.Location) Date {
	y, m, d := t.In(loc).Date()
	return Date{y, m, d}
}

// IsZero reports whether d is the zero value.
func (d Date) IsZero() bool { return d == Date{} }

func (d Date) String() string {
	if d.IsZero() {
		return ""
	}
	return fmt.Sprintf("%04d-%02d-%02d", d.Y, int(d.M), d.D)
}

// Time returns d at UTC midnight. Keep its use to date arithmetic and formatting.
func (d Date) Time() time.Time { return time.Date(d.Y, d.M, d.D, 0, 0, 0, 0, time.UTC) }

// Compare returns -1, 0 or +1.
func (d Date) Compare(o Date) int {
	switch {
	case d.Y != o.Y:
		return cmp3(d.Y, o.Y)
	case d.M != o.M:
		return cmp3(int(d.M), int(o.M))
	}
	return cmp3(d.D, o.D)
}

func cmp3(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// Before reports d < o.
func (d Date) Before(o Date) bool { return d.Compare(o) < 0 }

// After reports d > o.
func (d Date) After(o Date) bool { return d.Compare(o) > 0 }

// AddDays returns d + n days.
func (d Date) AddDays(n int) Date { return DateOf(d.Time().AddDate(0, 0, n), time.UTC) }

// AddMonths returns d + n months, clamped to the end of the target month (Jan 31 + 1 → Feb 28/29).
func (d Date) AddMonths(n int) Date {
	first := time.Date(d.Y, d.M+time.Month(n), 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1).Day()
	return Date{first.Year(), first.Month(), min(d.D, last)}
}

// FirstOfMonth returns the 1st of d's month.
func (d Date) FirstOfMonth() Date { return Date{d.Y, d.M, 1} }

// Weekday returns the day of the week.
func (d Date) Weekday() time.Weekday { return d.Time().Weekday() }

// DaysUntil returns the number of days from d to o (negative when o is earlier).
func (d Date) DaysUntil(o Date) int { return int(o.Time().Sub(d.Time()).Hours() / 24) }

// MarshalText implements encoding.TextMarshaler (JSON: "YYYY-MM-DD").
func (d Date) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

// UnmarshalText implements encoding.TextUnmarshaler.
func (d *Date) UnmarshalText(b []byte) error {
	if len(b) == 0 {
		*d = Date{}
		return nil
	}
	v, err := ParseDate(string(b))
	if err != nil {
		return err
	}
	*d = v
	return nil
}

// ScanDate implements pgtype.DateScanner so pgx can scan SQL `date` into Date.
func (d *Date) ScanDate(v pgtype.Date) error {
	switch {
	case !v.Valid:
		*d = Date{}
	case v.InfinityModifier != pgtype.Finite:
		return fmt.Errorf("%w: infinite dates are not supported", ErrInvalidDate)
	default:
		*d = DateOf(v.Time, time.UTC)
	}
	return nil
}

// DateValue implements pgtype.DateValuer. The zero Date is stored as NULL.
func (d Date) DateValue() (pgtype.Date, error) {
	if d.IsZero() {
		return pgtype.Date{}, nil
	}
	return pgtype.Date{Time: d.Time(), Valid: true}, nil
}
