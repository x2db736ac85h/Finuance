// Package money handles amounts as int64 minor units plus an ISO 4217 currency (ADR 0004). It never
// uses floating point: parsing and formatting work on digit strings.
package money

import "strings"

// exponents lists the minor-unit exponent of the currencies Finuance knows (PLAN §4.1).
var exponents = map[string]int{
	"PLN": 2, "EUR": 2, "USD": 2, "GBP": 2, "CHF": 2, "CZK": 2, "SEK": 2, "NOK": 2, "DKK": 2, "HUF": 2,
}

// DefaultExponent applies to currencies not in the table; callers should warn about them.
const DefaultExponent = 2

// Exponent returns the number of minor-unit digits of currency and whether the currency is known.
func Exponent(currency string) (int, bool) {
	if e, ok := exponents[strings.ToUpper(currency)]; ok {
		return e, true
	}
	return DefaultExponent, false
}

// NonDefaultExponents returns the known currencies whose exponent is not DefaultExponent, for SQL
// that has to scale amount_minor per row (internal/query). Empty while every known currency has
// two decimals.
func NonDefaultExponents() map[string]int {
	out := map[string]int{}
	for c, e := range exponents {
		if e != DefaultExponent {
			out[c] = e
		}
	}
	return out
}
