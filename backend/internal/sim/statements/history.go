package statements

import (
	"fmt"
	"strconv"
	"strings"
)

// History is how much a persona's bank history holds (ADR 0044): how often it spends in a day, how
// far its amounts spread, how many merchants it knows and how many years back its history
// reaches. The zero value is the default, whose statements are the ones the simulator always
// wrote; any other profile is part of every day's stream and of the accounts' names, so a
// restarted simulator reads it back and overlapping statements still de-duplicate.
type History struct {
	// Density draws the day's spending this many times (1 … MaxDensity; about 1.7 rows a time).
	Density int `json:"density"`
	// Spread is the amounts' range in percent of the default (MinSpread … MaxSpread).
	Spread int `json:"spread"`
	// Variety is how many shops of each merchant there are (1 … MaxVariety).
	Variety int `json:"variety"`
	// Years is how far back a rebuilt history starts (1 … MaxYears).
	Years int `json:"years"`
}

// The history profile's ranges.
const (
	MaxDensity = 20
	MinSpread  = 50
	MaxSpread  = 300
	MaxVariety = 5
	MaxYears   = 10
)

// Normal holds h to the ranges, the zero value of a field meaning its default.
func (h History) Normal() History {
	def := func(v, d, lo, hi int) int {
		if v == 0 {
			return d
		}
		return max(lo, min(hi, v))
	}
	return History{Density: def(h.Density, 1, 1, MaxDensity), Spread: def(h.Spread, 100, MinSpread, MaxSpread),
		Variety: def(h.Variety, 1, 1, MaxVariety), Years: def(h.Years, 1, 1, MaxYears)}
}

// IsDefault reports whether h is the default profile.
func (h History) IsDefault() bool { return h.Normal() == History{}.Normal() }

// Label names a profile other than the default ("d10 s100 v2 y3"); "" for the default.
func (h History) Label() string {
	if h.IsDefault() {
		return ""
	}
	n := h.Normal()
	return fmt.Sprintf("d%d s%d v%d y%d", n.Density, n.Spread, n.Variety, n.Years)
}

// key enters a day's stream: what changes the rows (not Years, which only says where a rebuilt
// history starts); "" for the default, so its days stay as they always were.
func (h History) key() string {
	n := h.Normal()
	if n.Density == 1 && n.Spread == 100 && n.Variety == 1 {
		return ""
	}
	return fmt.Sprintf("d%d s%d v%d", n.Density, n.Spread, n.Variety)
}

// AccountNameFor is the account's name under a profile: AccountName for the default, else with the
// profile's label after a middle dot.
func AccountNameFor(account int, h History) string {
	if l := h.Label(); l != "" {
		return AccountName(account) + " · " + l
	}
	return AccountName(account)
}

// ParseAccountName reads which of a persona's accounts a name is and its history profile.
func ParseAccountName(name string) (int, History, bool) {
	for _, n := range []int{Current, Savings} {
		base := AccountName(n)
		if name == base {
			return n, History{}.Normal(), true
		}
		label, ok := strings.CutPrefix(name, base+" · ")
		if !ok {
			continue
		}
		var h History
		if _, err := fmt.Sscanf(label, "d%d s%d v%d y%d", &h.Density, &h.Spread, &h.Variety, &h.Years); err != nil ||
			h.Normal() != h || h.Label() != label {
			return 0, History{}, false
		}
		return n, h, true
	}
	return 0, History{}, false
}

// Estimate is about how many transactions a profile's history holds for an average persona: the
// fixed monthly rows (salary, rent, subscriptions, bills, transfers, interest) and the day's
// spending times the density, over the profile's years.
func Estimate(h History) int {
	n := h.Normal()
	spend := 0
	for _, c := range everyday {
		spend += c.p
	}
	perDay := 0.35 + float64(n.Density)*float64(spend)/1000
	return int(perDay * 365 * float64(n.Years))
}

// DensityFor is the smallest density whose history of years holds about rows transactions.
func DensityFor(rows, years int) int {
	for d := 1; d < MaxDensity; d++ {
		if Estimate(History{Density: d, Years: years}) >= rows {
			return d
		}
	}
	return MaxDensity
}

// shop is one of a merchant's shops: the merchant itself for the first, else with its number after
// the name ("MARKET POD DĘBEM 2   WARSZAWA").
func shop(payee string, n int) string {
	if n <= 1 {
		return payee
	}
	num := " " + strconv.Itoa(n)
	if i := strings.Index(payee, "  "); i > 0 {
		return payee[:i] + num + payee[i:]
	}
	return payee + num
}
