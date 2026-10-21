// Package demo generates the fictional "demo year" of transactions (port of legacy demoCSV). The
// output for the legacy date range is byte-identical to testdata/fixtures/demo/demo-legacy.csv,
// which the tests check. Longer ranges produce more rows for benchmarks.
package demo

import (
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"
)

// Header is the Pekao-style header of the demo file.
const Header = "Data księgowania;Data waluty;Nadawca / Odbiorca;Adres nadawcy / odbiorcy;Rachunek źródłowy;Rachunek docelowy;Tytułem;Kwota operacji;Waluta;Numer referencyjny;Typ operacji;Kategoria"

// Legacy date range: 1 January 2024 up to (excluding) 18 March 2025.
var (
	LegacyStart = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	LegacyEnd   = time.Date(2025, 3, 18, 0, 0, 0, 0, time.UTC)
)

// Recent is the demo file shifted to end yesterday: the legacy range's length (442 days), up to
// (excluding) today's date in UTC.
func Recent(today time.Time) string {
	end := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	return CSV(end.Add(-LegacyEnd.Sub(LegacyStart)), end)
}

type category struct {
	name   string
	payees []string
	p      float64 // daily probability
	lo, hi float64 // amount range (the spread is skewed towards lo: lo + r·r·(hi−lo))
	kind   string  // operation type
}

var categories = []category{
	{"Artykuły spożywcze", []string{"MARKET NA ROGU       WARSZAWA", "DELIKATESY POD LIPĄ  WARSZAWA", "PIEKARNIA ZIARNO     WARSZAWA"}, .9, 15, 160, "TRANSAKCJA KARTĄ PŁATNICZĄ"},
	{"Restauracje i kawiarnie", []string{"KAWIARNIA PODWÓRKO   WARSZAWA", "BISTRO ZIELONE       WARSZAWA", "PIZZERIA SŁONECZNA   WARSZAWA"}, .35, 18, 140, "TRANSAKCJA KARTĄ PŁATNICZĄ"},
	{"Transport", []string{"KOMUNIKACJA MIEJSKA", "STACJA PALIW POLNA   WARSZAWA", "TAXI MIEJSKIE"}, .3, 4, 220, "PŁATNOŚĆ BLIK"},
	{"Zakupy przez internet", []string{"SKLEP INTERNETOWY ALFA", "MARKETPLACE OMEGA"}, .15, 30, 450, "PŁATNOŚĆ BLIK"},
	{"Sport", []string{"KLUB FITNESS RUCH    WARSZAWA", "BASEN MIEJSKI        WARSZAWA"}, .08, 25, 120, "TRANSAKCJA KARTĄ PŁATNICZĄ"},
	{"Zdrowie i uroda", []string{"APTEKA ZDROWIE       WARSZAWA", "SALON FRYZJERSKI LOK WARSZAWA"}, .07, 20, 180, "TRANSAKCJA KARTĄ PŁATNICZĄ"},
	{"Rozrywka", []string{"KINO POD GWIAZDAMI   WARSZAWA", "TEATR MAŁY           WARSZAWA"}, .05, 30, 160, "TRANSAKCJA KARTĄ PŁATNICZĄ"},
	{"Ogród", []string{"CENTRUM OGRODNICZE   CZESTOCHOWA"}, .03, 25, 200, "TRANSAKCJA KARTĄ PŁATNICZĄ"},
	{"Bez kategorii", []string{"PRZELEW WŁASNY", "OPŁATA ZA KARTĘ"}, .04, 5, 300, "PRZELEW MOBILE"},
}

// rng is the legacy Park–Miller generator: seed = seed·16807 mod (2³¹−1), value = seed / (2³¹−1).
// The products stay below 2⁵³, so float64 arithmetic is exact, exactly as in JavaScript.
type rng struct{ seed float64 }

func (r *rng) next() float64 {
	r.seed = math.Mod(r.seed*16807, 2147483647)
	return r.seed / 2147483647
}

// CSV returns the legacy demo file for days in [start, end) (newest row first, no trailing
// newline). CSV(LegacyStart, LegacyEnd) reproduces the legacy demo data exactly.
//
// The order of random draws matters for byte-identity and mirrors JavaScript's left-to-right
// evaluation: for a category purchase the payee is picked first, then the amount's two draws, then
// (inside the row) the value-date draw and the two demo reference numbers.
func CSV(start, end time.Time) string {
	r := &rng{seed: 7}
	var rows []string
	day := 24 * time.Hour
	line := func(t time.Time, who string, amount float64, kind, cat string) {
		valueDay := t
		if r.next() < .3 {
			valueDay = t.Add(-day)
		}
		title := "DEMO " + strconv.FormatFloat(math.Floor(r.next()*1e8), 'f', 0, 64)
		ref := "DEMO" + strconv.FormatFloat(math.Floor(r.next()*1e10), 'f', 0, 64)
		rows = append(rows, strings.Join([]string{dstr(t), dstr(valueDay), who, "", strings.Repeat("0", 26), "", title,
			strings.Replace(toFixed2(amount), ".", ",", 1), "PLN", ref, kind, cat}, ";"))
	}
	for t := start; t.Before(end); t = t.Add(day) {
		switch t.Day() {
		case 10:
			line(t, "PRACODAWCA PRZYKŁADOWY SP. Z O.O.", 9200+jsRound(r.next()*400), "PRZELEW PRZYCHODZĄCY", "Wynagrodzenie")
		case 2:
			line(t, "WSPÓLNOTA MIESZKANIOWA", -2850, "PRZELEW MOBILE", "Mieszkanie")
		case 15:
			line(t, "DOSTAWCA ENERGII", -(180 + r.next()*120), "PRZELEW MOBILE", "Rachunki")
		case 20:
			line(t, "OPERATOR INTERNETU", -79.99, "PRZELEW MOBILE", "Rachunki")
		}
		for _, c := range categories {
			if r.next() < c.p {
				who := c.payees[int(math.Floor(r.next()*float64(len(c.payees))))]
				a, b := r.next(), r.next()
				line(t, who, -(c.lo + a*b*(c.hi-c.lo)), c.kind, c.name)
			}
		}
	}
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}
	return Header + "\n" + strings.Join(rows, "\n")
}

func dstr(t time.Time) string { return t.Format("02.01.2006") }

// jsRound is Math.round: halves round towards +∞ (Math.round(-2.5) == -2).
func jsRound(x float64) float64 { return math.Floor(x + 0.5) }

// toFixed2 is Number.prototype.toFixed(2): the exact binary value of x rounded to 2 decimals, ties
// away from zero (the magnitude is rounded, then the sign restored). strconv rounds ties to even,
// which would differ on values whose exact expansion ends in …5.
func toFixed2(x float64) string {
	neg := x < 0
	exact := new(big.Float).SetFloat64(math.Abs(x)).Text('f', 1100) // every float64 is exact here
	ip, fp, _ := strings.Cut(exact, ".")
	fp += "000"
	digits := []byte(ip + fp[:2])
	if fp[2] >= '5' {
		for i := len(digits) - 1; i >= 0; i-- {
			if digits[i] < '9' {
				digits[i]++
				break
			}
			digits[i] = '0'
			if i == 0 {
				digits = append([]byte{'1'}, digits...)
			}
		}
	}
	s := string(digits[:len(digits)-2]) + "." + string(digits[len(digits)-2:])
	if neg { // JavaScript keeps the sign of any x < 0: (-0.001).toFixed(2) == "-0.00"
		s = "-" + s
	}
	return s
}
