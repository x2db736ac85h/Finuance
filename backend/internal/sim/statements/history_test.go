package statements

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/sim/identity"
)

// TestDefaultHistoryIsTheOldOne: the default profile, and one that differs in Years only, write the
// same days as before profiles existed, so the samples and every lab's history stay as they were.
func TestDefaultHistoryIsTheOldOne(t *testing.T) {
	p := identity.Nth(3)
	for d := date(2025, time.March, 1); d.Compare(date(2025, time.March, 31)) <= 0; d = platform.DateOf(d.Time().AddDate(0, 0, 1), time.UTC) {
		for _, h := range []History{{}, {Density: 1, Spread: 100, Variety: 1}, {Years: 7}} {
			if !slices.Equal(DayWith(p, d, h), Day(p, d)) {
				t.Fatalf("%v under %+v differs from the default", d, h)
			}
		}
	}
}

// TestHistoryProfiles: density multiplies the day's spending, spread widens the amounts, variety
// numbers the shops, and a profile's day is the same in any range.
func TestHistoryProfiles(t *testing.T) {
	from, to := date(2025, time.January, 1), date(2025, time.December, 31)
	spending := func(rows []Row) []Row {
		return slices.DeleteFunc(slices.Clone(rows), func(r Row) bool { return r.Title != "ZAKUP" })
	}
	var base, dense int
	for i := range 10 {
		p := identity.Nth(i)
		base += len(spending(RowsWith(p, Current, from, to, History{})))
		dense += len(spending(RowsWith(p, Current, from, to, History{Density: 10})))
	}
	if ratio := float64(dense) / float64(base); ratio < 9 || ratio > 11 {
		t.Errorf("density 10 draws %.1f times the spending", ratio)
	}
	p := identity.Nth(0)
	most := func(h History) int64 {
		var m int64
		for _, r := range spending(RowsWith(p, Current, from, to, h)) {
			m = max(m, -r.Amount)
		}
		return m
	}
	if narrow, wide := most(History{Spread: MinSpread}), most(History{Spread: MaxSpread}); narrow >= most(History{}) || wide <= most(History{}) {
		t.Errorf("largest purchase: spread 50 %d, 100 %d, 300 %d", narrow, most(History{}), wide)
	}
	shops := map[string]bool{}
	for _, r := range spending(RowsWith(p, Current, from, to, History{Variety: 3})) {
		shops[r.Payee] = true
	}
	numbered := 0
	for s := range shops {
		if strings.Contains(s, " 2 ") || strings.Contains(s, " 3 ") || strings.HasSuffix(s, " 2") || strings.HasSuffix(s, " 3") {
			numbered++
		}
	}
	if numbered == 0 || len(shops) <= 23 {
		t.Errorf("variety 3: %d shops, %d numbered", len(shops), numbered)
	}
	h := History{Density: 5, Spread: 150, Variety: 2}
	all := RowsWith(p, Current, from, to, h)
	march := RowsWith(p, Current, date(2025, time.March, 1), date(2025, time.March, 31), h)
	var inMarch []Row
	for _, r := range all {
		if r.Booked.M == time.March {
			inMarch = append(inMarch, r)
		}
	}
	if !slices.Equal(march, inMarch) {
		t.Error("a profile's March differs between a year's statement and March's")
	}
}

// TestAccountNames: a profile is written into the accounts' names and read back; the default
// keeps the old names, and a name that isn't one of the persona's accounts is refused.
func TestAccountNames(t *testing.T) {
	for _, h := range []History{{}, {Density: 10, Years: 3}, {Density: 20, Spread: 50, Variety: 5, Years: 10}} {
		for _, n := range []int{Current, Savings} {
			name := AccountNameFor(n, h)
			got, gh, ok := ParseAccountName(name)
			if !ok || got != n || gh != h.Normal() {
				t.Errorf("%q read back as %d %+v %v", name, got, gh, ok)
			}
			if h.IsDefault() && name != AccountName(n) {
				t.Errorf("the default profile renamed the account: %q", name)
			}
		}
	}
	for _, name := range []string{"Cash", "Current account ·", "Current account · d0 s100 v1 y1", "Current account · d10 s100 v1 y1 x",
		"Current account · d1 s100 v1 y1", "Savings · d99 s100 v1 y1"} {
		if _, _, ok := ParseAccountName(name); ok {
			t.Errorf("%q was read as an account", name)
		}
	}
}

// TestEstimate: the page's estimate is within 15 % of what ten personas' histories hold, and
// DensityFor picks the smallest density that reaches a size.
func TestEstimate(t *testing.T) {
	for _, h := range []History{{}, {Density: 10}} {
		rows := 0
		for i := range 10 {
			for _, n := range []int{Current, Savings} {
				rows += len(RowsWith(identity.Nth(i), n, date(2025, time.January, 1), date(2025, time.December, 31), h))
			}
		}
		if got, want := float64(Estimate(h)), float64(rows)/10; got < want*0.85 || got > want*1.15 {
			t.Errorf("%+v: estimate %v, a persona holds %v", h, got, want)
		}
	}
	d := DensityFor(20_000, 3)
	if Estimate(History{Density: d, Years: 3}) < 20_000 || Estimate(History{Density: d - 1, Years: 3}) >= 20_000 {
		t.Errorf("DensityFor(20000, 3) = %d", d)
	}
	if DensityFor(1<<30, 1) != MaxDensity || DensityFor(0, 1) != 1 {
		t.Error("DensityFor out of range")
	}
}
