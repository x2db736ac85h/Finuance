package platform

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestNewDateRejectsImpossible(t *testing.T) {
	for _, tc := range []struct {
		y, m, d int
		ok      bool
	}{{2025, 2, 28, true}, {2024, 2, 29, true}, {2025, 2, 29, false}, {2025, 2, 31, false}, {2025, 13, 1, false},
		{2025, 0, 1, false}, {2025, 1, 0, false}, {2025, 1, 32, false}, {0, 1, 1, false}, {10000, 1, 1, false}, {1, 1, 1, true}, {9999, 12, 31, true}} {
		_, err := NewDate(tc.y, time.Month(tc.m), tc.d)
		if (err == nil) != tc.ok || (err != nil && !errors.Is(err, ErrInvalidDate)) {
			t.Errorf("NewDate(%d,%d,%d) err = %v", tc.y, tc.m, tc.d, err)
		}
	}
}

func TestParseAndFormat(t *testing.T) {
	d, err := ParseDate("2025-03-01")
	if err != nil || d != MustDate(2025, 3, 1) || d.String() != "2025-03-01" {
		t.Fatalf("parse: %v %v", d, err)
	}
	for _, bad := range []string{"", "2025-3-1", "2025/03/01", "01.03.2025", "2025-02-31", "20250301", "2025-03-01T00:00"} {
		if _, err := ParseDate(bad); err == nil {
			t.Errorf("ParseDate(%q) accepted", bad)
		}
	}
}

func TestArithmetic(t *testing.T) {
	d := MustDate(2025, 1, 31)
	cases := []struct {
		got, want Date
	}{
		{d.AddDays(1), MustDate(2025, 2, 1)},
		{d.AddDays(-31), MustDate(2024, 12, 31)},
		{d.AddMonths(1), MustDate(2025, 2, 28)},
		{MustDate(2024, 1, 31).AddMonths(1), MustDate(2024, 2, 29)},
		{d.AddMonths(-2), MustDate(2024, 11, 30)},
		{d.AddMonths(12), MustDate(2026, 1, 31)},
		{d.FirstOfMonth(), MustDate(2025, 1, 1)},
		{MustDate(2024, 12, 31).AddDays(366), MustDate(2026, 1, 1)},
	}
	for i, c := range cases {
		if c.got != c.want {
			t.Errorf("case %d: %v, want %v", i, c.got, c.want)
		}
	}
	if MustDate(2025, 3, 20).Weekday() != time.Thursday {
		t.Error("weekday")
	}
	if n := MustDate(2025, 1, 1).DaysUntil(MustDate(2025, 3, 1)); n != 59 {
		t.Errorf("DaysUntil = %d", n)
	}
	a, b := MustDate(2025, 1, 2), MustDate(2025, 1, 10)
	if !a.Before(b) || !b.After(a) || a.Compare(MustDate(2025, 1, 2)) != 0 || b.Compare(a) != 1 {
		t.Error("compare")
	}
}

func TestDateOfUsesLocation(t *testing.T) {
	warsaw, err := time.LoadLocation("Europe/Warsaw")
	if err != nil {
		t.Skip("no tzdata")
	}
	instant := time.Date(2025, 3, 31, 22, 30, 0, 0, time.UTC) // 00:30 on 1 April in Warsaw (CEST)
	if got := DateOf(instant, warsaw); got != MustDate(2025, 4, 1) {
		t.Errorf("DateOf = %v", got)
	}
	if got := DateOf(instant, time.UTC); got != MustDate(2025, 3, 31) {
		t.Errorf("DateOf UTC = %v", got)
	}
}

func TestJSONAndPg(t *testing.T) {
	type wrap struct {
		D Date  `json:"d"`
		P *Date `json:"p,omitempty"`
	}
	b, _ := json.Marshal(wrap{D: MustDate(2025, 3, 1)})
	if string(b) != `{"d":"2025-03-01"}` {
		t.Errorf("json = %s", b)
	}
	var w wrap
	if err := json.Unmarshal([]byte(`{"d":"2024-02-29"}`), &w); err != nil || w.D != MustDate(2024, 2, 29) {
		t.Errorf("unmarshal: %v %v", w, err)
	}
	if err := json.Unmarshal([]byte(`{"d":"2025-02-30"}`), &w); err == nil {
		t.Error("invalid JSON date accepted")
	}
	v, _ := MustDate(2025, 3, 1).DateValue()
	var back Date
	if err := back.ScanDate(v); err != nil || back != MustDate(2025, 3, 1) {
		t.Errorf("pg round trip: %v %v", back, err)
	}
	if v, _ := (Date{}).DateValue(); v.Valid {
		t.Error("zero Date must be NULL")
	}
	if err := back.ScanDate(pgtype.Date{Valid: true, InfinityModifier: pgtype.Infinity}); err == nil {
		t.Error("infinity accepted")
	}
}
