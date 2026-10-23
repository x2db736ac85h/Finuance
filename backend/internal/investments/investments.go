// Package investments holds the manual investment data (PLAN §4.9, legacy §32.4: PPK and
// positions, the safe withdrawal rate and the expected real return) and the three overview cards
// built on it: Investment, Yield on cost and FI/RE (legacy o_inv, o_yoc and o_fire).
//
// Amounts are PLN minor units and percentages basis points, as stored. The cards' ratios and the
// years-to-FI projection use float64 (PLAN §8); every amount they show is computed from exact
// integer sums and rounded once, half away from zero.
package investments

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

// Doc is a user's investment document. Version is the optimistic-concurrency counter: 0 until the
// first save, which must send 0 (as preferences).
type Doc struct {
	Version      int32
	PPK          PPK
	SwrBp        int // safe withdrawal rate, basis points (400 = 4 %)
	RealReturnBp int // expected real return, basis points
	Positions    []Position
}

// PPK is the Polish employee capital plan (Pracownicze Plany Kapitałowe): contributions and value.
// It pays no income, so yield on cost leaves it out.
type PPK struct {
	EmployeeMinor, EmployerMinor, StateMinor, ValueMinor int64
}

// Position is one manually tracked investment.
type Position struct {
	ID                uuid.UUID
	Name              string
	Type              string // free text with suggestions (ETF, stock, bond, deposit, crypto, other)
	InvestedMinor     int64
	ValueMinor        int64
	AnnualIncomeMinor int64 // dividends or interest per year
}

// Defaults of a user without a saved document (legacy INV0).
const (
	DefaultSwrBp        = 400
	DefaultRealReturnBp = 500
	MaxPositions        = 100
)

// Empty is the document of a user who hasn't saved one yet.
func Empty() Doc { return Doc{SwrBp: DefaultSwrBp, RealReturnBp: DefaultRealReturnBp} }

// Validate checks the §4.9 limits: amounts ≥ 0, names 1–80 characters, types ≤ 30, SWR 0.5–10 %,
// real return −10 % to 20 %, at most MaxPositions positions with distinct ids. Names and types are
// trimmed.
func (d *Doc) Validate() error {
	ve := &platform.ValidationError{}
	amount := func(field string, v int64) {
		if v < 0 {
			ve.Add(field, "must not be negative")
		}
	}
	amount("ppk.employeeMinor", d.PPK.EmployeeMinor)
	amount("ppk.employerMinor", d.PPK.EmployerMinor)
	amount("ppk.stateMinor", d.PPK.StateMinor)
	amount("ppk.valueMinor", d.PPK.ValueMinor)
	if d.SwrBp < 50 || d.SwrBp > 1000 {
		ve.Add("swrBp", "must be 50–1000 (0.5–10 %)")
	}
	if d.RealReturnBp < -1000 || d.RealReturnBp > 2000 {
		ve.Add("realReturnBp", "must be −1000–2000 (−10 % to 20 %)")
	}
	if len(d.Positions) > MaxPositions {
		ve.Add("positions", fmt.Sprintf("at most %d", MaxPositions))
	}
	seen := map[uuid.UUID]bool{}
	for i := range d.Positions {
		p := &d.Positions[i]
		f := func(s string) string { return fmt.Sprintf("positions[%d].%s", i, s) }
		p.Name, p.Type = strings.TrimSpace(p.Name), strings.TrimSpace(p.Type)
		if n := utf8.RuneCountInString(p.Name); n < 1 || n > 80 {
			ve.Add(f("name"), "must be 1–80 characters")
		}
		if utf8.RuneCountInString(p.Type) > 30 {
			ve.Add(f("type"), "must be at most 30 characters")
		}
		if strings.ContainsRune(p.Name+p.Type, 0) {
			ve.Add(f("name"), "must not contain NUL characters")
		}
		amount(f("investedMinor"), p.InvestedMinor)
		amount(f("valueMinor"), p.ValueMinor)
		amount(f("annualIncomeMinor"), p.AnnualIncomeMinor)
		if p.ID == uuid.Nil || seen[p.ID] {
			ve.Add(f("id"), "must be a unique id")
		}
		seen[p.ID] = true
	}
	return ve.Err()
}

// Row is one line of the Investment card: PPK first, then the positions.
type Row struct {
	ID          *uuid.UUID // nil for PPK
	Name        string
	Type        string
	PPK         bool
	Invested    int64
	Value       int64
	Income      int64
	GainPercent *float64 // (value − invested) / invested; nil without money invested
}

// Summary is the Investment card (legacy o_inv): every row, the totals and the PPK figures.
type Summary struct {
	Rows                       []Row
	Invested, Value, Income    int64
	PPKPaidIn, PPKGain         int64
	PPKReturnOnOwn             *float64 // (value − own contributions) / own contributions
	GainPercent                *float64
	AllocationShares           []float64 // value share of each row with a value, in row order (0 for the others)
	PPKEmployee, PPKEmployer   int64
	PPKState                   int64
	SwrBp, RealReturnBp, Count int
}

func ratio(num, den int64) *float64 {
	if den == 0 {
		return nil
	}
	r := float64(num) / float64(den)
	return &r
}

// Summarize computes the Investment card.
func Summarize(d Doc) Summary {
	p := d.PPK
	paidIn := p.EmployeeMinor + p.EmployerMinor + p.StateMinor
	s := Summary{PPKPaidIn: paidIn, PPKGain: p.ValueMinor - paidIn, PPKReturnOnOwn: ratio(p.ValueMinor-p.EmployeeMinor, p.EmployeeMinor),
		PPKEmployee: p.EmployeeMinor, PPKEmployer: p.EmployerMinor, PPKState: p.StateMinor, SwrBp: d.SwrBp, RealReturnBp: d.RealReturnBp,
		Count: len(d.Positions)}
	s.Rows = append(s.Rows, Row{Name: "PPK", PPK: true, Invested: paidIn, Value: p.ValueMinor, GainPercent: ratio(p.ValueMinor-paidIn, paidIn)})
	for _, x := range d.Positions {
		id := x.ID
		s.Rows = append(s.Rows, Row{ID: &id, Name: x.Name, Type: x.Type, Invested: x.InvestedMinor, Value: x.ValueMinor,
			Income: x.AnnualIncomeMinor, GainPercent: ratio(x.ValueMinor-x.InvestedMinor, x.InvestedMinor)})
	}
	for _, r := range s.Rows {
		s.Invested += r.Invested
		s.Value += r.Value
		s.Income += r.Income
	}
	s.GainPercent = ratio(s.Value-s.Invested, s.Invested)
	for _, r := range s.Rows {
		share := 0.0
		if s.Value > 0 && r.Value > 0 {
			share = float64(r.Value) / float64(s.Value)
		}
		s.AllocationShares = append(s.AllocationShares, share)
	}
	return s
}

// YieldRow is one position's yield on cost.
type YieldRow struct {
	ID       uuid.UUID
	Name     string
	Invested int64
	Income   int64
	Yield    float64 // income / invested
}

// Yield is the Yield-on-cost card (legacy o_yoc): positions with money invested (PPK is left out:
// it pays nothing out) and the total. Without any income entered by hand, the dividends and
// interest found in the transactions of the 12 months up to the selected month are used, and
// UsesDetected says so.
type Yield struct {
	Positions     []YieldRow
	Invested      int64
	Income        int64
	DetectedMinor int64
	UsesDetected  bool
	Total         *float64 // nil without money invested
}

// YieldOnCost computes the Yield-on-cost card; detected is the dividend and interest income found
// in the transactions.
func YieldOnCost(d Doc, detected int64) Yield {
	y := Yield{DetectedMinor: detected}
	var manual int64
	for _, p := range d.Positions {
		if p.InvestedMinor <= 0 {
			continue
		}
		y.Positions = append(y.Positions, YieldRow{ID: p.ID, Name: p.Name, Invested: p.InvestedMinor, Income: p.AnnualIncomeMinor,
			Yield: float64(p.AnnualIncomeMinor) / float64(p.InvestedMinor)})
		y.Invested += p.InvestedMinor
		manual += p.AnnualIncomeMinor
	}
	y.Income = manual
	if manual == 0 {
		y.Income, y.UsesDetected = detected, detected > 0
	}
	y.Total = ratio(y.Income, y.Invested)
	return y
}

// MonthTotals are a month's money in and out (minor units, both ≥ 0).
type MonthTotals struct{ Income, Expense int64 }

// FIRE is the FI/RE card (legacy o_fire).
type FIRE struct {
	Months         int   // how many months the annual figures come from (up to 12)
	AnnualExpenses int64 // the months' spending, annualised
	YearlySavings  int64 // (income − spending), annualised
	FINumber       int64 // annual expenses / SWR
	Assets         int64 // the value of every investment, PPK included
	SwrBp          int
	RealReturnBp   int
	Progress       float64 // assets / FI number (0 without an FI number)
	Years          int     // years to FI by yearly compounding; 0 when reached or without an FI number
	Reachable      bool    // false: not within 100 years at this savings rate
}

// FireOf computes the FI/RE card from the months up to the selected one (at most 12, from the
// first month with data on): the FI number is the annualised spending divided by the safe
// withdrawal rate (at least 0.5 %); the years to reach it compound the invested value yearly at the
// expected real return plus the yearly savings, up to 100 years.
func FireOf(d Doc, months []MonthTotals, assets int64) FIRE {
	f := FIRE{Months: len(months), Assets: assets, SwrBp: d.SwrBp, RealReturnBp: d.RealReturnBp, Reachable: true}
	n := int64(max(len(months), 1))
	var exp, inc int64
	for _, m := range months {
		exp += m.Expense
		inc += m.Income
	}
	swr := int64(max(d.SwrBp, 50))
	f.AnnualExpenses = divRound(exp*12, n)
	f.YearlySavings = divRound((inc-exp)*12, n)
	f.FINumber = divRound(exp*12*10000, n*swr) // exact: (exp / n × 12) / (swr / 10 000)
	if f.FINumber > 0 {
		f.Progress = float64(assets) / float64(f.FINumber)
	}
	if f.FINumber <= 0 || assets >= f.FINumber {
		return f
	}
	r := float64(d.RealReturnBp) / 10000
	save := float64(inc-exp) * 12 / float64(n)
	fi := float64(exp) * 12 * 10000 / float64(n*swr)
	a := float64(assets)
	for a < fi && f.Years < 100 {
		a = a*(1+r) + save
		f.Years++
		if save <= 0 && r <= 0 {
			f.Years = 100
			break
		}
	}
	if f.Years >= 100 {
		f.Reachable = false
	}
	return f
}

// divRound divides and rounds half away from zero; n > 0.
func divRound(a, n int64) int64 {
	q, r := a/n, a%n
	if r < 0 {
		r = -r
	}
	if 2*r >= n {
		if a < 0 {
			q--
		} else {
			q++
		}
	}
	return q
}
