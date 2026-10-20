package money

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Errors from ParseMinor and ParseCanonical.
var (
	ErrInvalidAmount = errors.New("not a valid amount")
	ErrOverflow      = errors.New("amount out of range")
)

// Parts is a validated number as a sign and two digit strings, e.g. "-1 234,56" →
// {Negative: true, Int: "1234", Frac: "56"}.
type Parts struct {
	Negative bool
	Int      string // integer digits; may be "" (",5")
	Frac     string // fraction digits; may be ""
}

// currencyToken matches one currency marker at the start or the end. The legacy regex has no /g
// flag, so only the leftmost match is removed: "PLN 12 PLN" keeps its trailing "PLN" and fails.
var currencyToken = regexp.MustCompile(`(?i)^(?:PLN|EUR|USD|GBP|CHF|zł|\$|€|£)|(?:PLN|EUR|USD|GBP|CHF|zł|\$|€|£)$`)

var numberShape = regexp.MustCompile(`^[-+]?[\d.,]+$`)

// Split validates s with the legacy `parseNum` rules and splits it into digits:
//
//  1. remove all whitespace (JavaScript's definition: incl. NBSP U+00A0, narrow NBSP U+202F,
//     U+FEFF) and apostrophes: "1 234,56", "1'234.56";
//  2. remove one currency token at the start or the end: PLN, EUR, USD, GBP, CHF, zł, $, €, £;
//  3. the rest must be [-+]?[digits . ,]+ with at least one digit;
//  4. more than 15 digits → not a number (account and reference numbers);
//  5. a leading-zero integer like "0229197" → not a number (identifiers);
//  6. separators: with both "," and ".", the later one is the decimal mark and the other groups
//     thousands ("1.234,56", "1,234.56"); with only one kind, a single occurrence is the decimal
//     mark ("12,50", "12.50") and repeated ones group thousands ("1.234.567").
func Split(s string) (Parts, bool) {
	s = strings.Map(func(r rune) rune {
		if isJSSpace(r) || r == '\'' {
			return -1
		}
		return r
	}, s)
	if loc := currencyToken.FindStringIndex(s); loc != nil {
		s = s[:loc[0]] + s[loc[1]:]
	}
	if !numberShape.MatchString(s) || !strings.ContainsAny(s, "0123456789") {
		return Parts{}, false
	}
	if len(strings.Map(keepDigits, s)) > 15 {
		return Parts{}, false
	}
	var p Parts
	switch s[0] {
	case '-':
		p.Negative, s = true, s[1:]
	case '+':
		s = s[1:]
	}
	if len(s) > 1 && s[0] == '0' && s[1] >= '0' && s[1] <= '9' {
		return Parts{}, false
	}
	lc, ld := strings.LastIndexByte(s, ','), strings.LastIndexByte(s, '.')
	dec := -1
	switch {
	case lc >= 0 && ld >= 0:
		dec = max(lc, ld)
	case lc >= 0 && strings.Count(s, ",") == 1:
		dec = lc
	case ld >= 0 && strings.Count(s, ".") == 1:
		dec = ld
	}
	intPart, frac := s, ""
	if dec >= 0 {
		intPart, frac = s[:dec], s[dec+1:]
	}
	// After the decimal mark only digits may follow, and with both kinds present the decimal mark
	// may not also appear earlier ("1,2.3,4" was NaN in the legacy code too).
	if strings.ContainsAny(frac, ".,") || (lc >= 0 && ld >= 0 && strings.IndexByte(intPart, s[dec]) >= 0) {
		return Parts{}, false
	}
	p.Int = strings.Map(keepDigits, intPart)
	p.Frac = frac
	return p, true
}

func keepDigits(r rune) rune {
	if r >= '0' && r <= '9' {
		return r
	}
	return -1
}

// isJSSpace reports whether r is whitespace in ECMAScript (what the legacy \s matched).
func isJSSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0x00A0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}

// ParseMinor parses an amount cell into minor units with exponent exp — exactly, without floating
// point ("-1 234,56 PLN", 2 → -123456). It accepts what Split accepts. More fraction digits than
// exp are rounded half away from zero ("0,125" → 13, "-0,125" → -13). [CHANGE] the legacy app kept
// float64 values; see ADR 0004.
func ParseMinor(s string, exp int) (int64, error) {
	p, ok := Split(s)
	if !ok {
		return 0, fmt.Errorf("%w: %q", ErrInvalidAmount, s)
	}
	return FromParts(p, exp)
}

// FromParts converts digit strings into minor units with exponent exp (see ParseMinor).
func FromParts(p Parts, exp int) (int64, error) {
	if p.Int == "" && p.Frac == "" || !isDigits(p.Int) || !isDigits(p.Frac) {
		return 0, ErrInvalidAmount
	}
	frac, roundUp := p.Frac, false
	if len(frac) > exp {
		roundUp = frac[exp] >= '5'
		frac = frac[:exp]
	}
	frac += strings.Repeat("0", exp-len(frac))
	digits := strings.TrimLeft(p.Int+frac, "0")
	if digits == "" {
		digits = "0"
	}
	u, err := strconv.ParseUint(digits, 10, 64)
	if err != nil {
		return 0, ErrOverflow
	}
	if roundUp {
		u++
	}
	// The magnitude may be at most MaxInt64, or MaxInt64+1 for negative values.
	if u > math.MaxInt64 && !(p.Negative && u == math.MaxInt64+1) {
		return 0, ErrOverflow
	}
	if p.Negative {
		return int64(-u), nil //nolint:gosec // range checked above; -u wraps to MinInt64 on purpose
	}
	return int64(u), nil //nolint:gosec // range checked above
}

func isDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// FormatMinor writes minor units as a plain decimal with exp fraction digits and "." as the
// separator: FormatMinor(-12713, 2) == "-127.13", FormatMinor(5, 2) == "0.05". It is the amount
// text inside de-duplication fingerprints (PLAN §4.4), so its output must never change.
func FormatMinor(v int64, exp int) string {
	u := uint64(v) //nolint:gosec // reinterpreted below for negatives
	if v < 0 {
		u = -u // two's complement magnitude, also correct for MinInt64
	}
	s := strconv.FormatUint(u, 10)
	if exp > 0 {
		if len(s) <= exp {
			s = strings.Repeat("0", exp-len(s)+1) + s
		}
		s = s[:len(s)-exp] + "." + s[len(s)-exp:]
	}
	if v < 0 {
		s = "-" + s
	}
	return s
}

// ParseCanonical parses FormatMinor's output strictly ("-" sign, digits, exactly exp fraction
// digits). ParseCanonical(FormatMinor(v, exp), exp) == v for every int64.
func ParseCanonical(s string, exp int) (int64, error) {
	var p Parts
	rest := s
	if strings.HasPrefix(rest, "-") {
		p.Negative, rest = true, rest[1:]
	}
	ip, fp, hasDot := strings.Cut(rest, ".")
	if ip == "" || (exp > 0 && (!hasDot || len(fp) != exp)) || (exp == 0 && hasDot) {
		return 0, fmt.Errorf("%w: %q is not canonical", ErrInvalidAmount, s)
	}
	p.Int, p.Frac = ip, fp
	return FromParts(p, exp)
}
