package money

import (
	"errors"
	"math"
	"math/rand/v2"
	"strings"
	"testing"
)

func TestParseMinor(t *testing.T) {
	for _, tc := range []struct {
		in   string
		exp  int
		want int64
	}{
		{"-127,13", 2, -12713},
		{"-1 234,56", 2, -123456},
		{"1.234,56", 2, 123456},
		{"1,234.56", 2, 123456},
		{"12,00 PLN", 2, 1200},
		{"-800,00 PLN", 2, -80000},
		{"€12", 2, 1200},
		{"+15,00", 2, 1500},
		{"-2 500,00", 2, -250000},
		{"-10", 2, -1000},
		{"-43.52", 2, -4352},
		{"1.234.567", 2, 123456700},
		{",5", 2, 50},
		{"5,", 2, 500},
		{"0,125", 2, 13},   // half away from zero
		{"-0,125", 2, -13}, // half away from zero
		{"0,124", 2, 12},
		{"0,005", 2, 1},
		{"12", 0, 12},
		{"12,6", 0, 13},
		{"0", 2, 0},
		{"-0", 2, 0},
		{"999999999999999", 2, 99999999999999900},
	} {
		got, err := ParseMinor(tc.in, tc.exp)
		if err != nil || got != tc.want {
			t.Errorf("ParseMinor(%q, %d) = %d, %v; want %d", tc.in, tc.exp, got, err, tc.want)
		}
	}
	for _, bad := range []string{"", "-", "abc", "12a", "0123", "1234567890123456", "1,2.3,4", "1.2,3.4", "PLN 12 PLN", "1e3", "--1", "12-", "-€12,50"} {
		if v, err := ParseMinor(bad, 2); !errors.Is(err, ErrInvalidAmount) {
			t.Errorf("ParseMinor(%q) = %d, %v; want ErrInvalidAmount", bad, v, err)
		}
	}
}

func TestFormatMinor(t *testing.T) {
	for _, tc := range []struct {
		v    int64
		exp  int
		want string
	}{
		{-12713, 2, "-127.13"}, {5, 2, "0.05"}, {-5, 2, "-0.05"}, {0, 2, "0.00"}, {100, 2, "1.00"},
		{12, 0, "12"}, {-1, 3, "-0.001"}, {math.MaxInt64, 2, "92233720368547758.07"}, {math.MinInt64, 2, "-92233720368547758.08"},
	} {
		if got := FormatMinor(tc.v, tc.exp); got != tc.want {
			t.Errorf("FormatMinor(%d, %d) = %q, want %q", tc.v, tc.exp, got, tc.want)
		}
	}
}

// Property (PLAN task 3.6): parsing the canonical format returns the original value.
func TestFormatParseRoundTrip(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	values := []int64{0, 1, -1, 99, -100, math.MaxInt64, math.MinInt64, math.MaxInt64 - 1, math.MinInt64 + 1}
	for i := 0; i < 20000; i++ {
		values = append(values, r.Int64()>>uint(r.IntN(63)), -r.Int64()>>uint(r.IntN(63)))
	}
	for _, v := range values {
		for _, exp := range []int{0, 2, 3} {
			s := FormatMinor(v, exp)
			back, err := ParseCanonical(s, exp)
			if err != nil || back != v {
				t.Fatalf("round trip %d (exp %d): %q → %d, %v", v, exp, s, back, err)
			}
			// The canonical text is also a valid bank-style amount, unless its digits exceed the
			// 15-digit identifier rule of Split.
			if digits := len(strings.Map(func(r rune) rune {
				if r >= '0' && r <= '9' {
					return r
				}
				return -1
			}, s)); digits <= 15 {
				if got, err := ParseMinor(s, exp); err != nil || got != v {
					t.Fatalf("ParseMinor(%q) = %d, %v; want %d", s, got, err, v)
				}
			}
		}
	}
}

func TestOverflowAndCanonical(t *testing.T) {
	if _, err := FromParts(Parts{Int: "92233720368547758", Frac: "08"}, 2); !errors.Is(err, ErrOverflow) {
		t.Errorf("positive overflow: %v", err)
	}
	if v, err := FromParts(Parts{Negative: true, Int: "92233720368547758", Frac: "08"}, 2); err != nil || v != math.MinInt64 {
		t.Errorf("MinInt64: %d %v", v, err)
	}
	if _, err := FromParts(Parts{Int: "99999999999999999999"}, 0); !errors.Is(err, ErrOverflow) {
		t.Errorf("huge: %v", err)
	}
	for _, bad := range []string{"1.5", "1", "1.500", "", "-", ".50", "1,50"} {
		if _, err := ParseCanonical(bad, 2); err == nil {
			t.Errorf("ParseCanonical(%q) accepted", bad)
		}
	}
	if e, ok := Exponent("pln"); e != 2 || !ok {
		t.Error("PLN exponent")
	}
	if e, ok := Exponent("XYZ"); e != 2 || ok {
		t.Error("unknown currency")
	}
}
