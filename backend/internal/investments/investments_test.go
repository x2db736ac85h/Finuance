package investments

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

func pos(name string, invested, value, income int64) Position {
	return Position{ID: uuid.NewSHA1(uuid.Nil, []byte(name)), Name: name, InvestedMinor: invested, ValueMinor: value, AnnualIncomeMinor: income}
}

func TestValidate(t *testing.T) {
	ok := Empty()
	ok.Positions = []Position{pos("ETF", 1, 1, 0)}
	ok.Positions[0].Name = "  ETF  "
	if err := ok.Validate(); err != nil || ok.Positions[0].Name != "ETF" {
		t.Fatalf("valid document: %v (%q)", err, ok.Positions[0].Name)
	}
	cases := []struct {
		name  string
		edit  func(d *Doc)
		field string
	}{
		{"negative PPK", func(d *Doc) { d.PPK.ValueMinor = -1 }, "ppk.valueMinor"},
		{"SWR below 0.5 %", func(d *Doc) { d.SwrBp = 49 }, "swrBp"},
		{"SWR above 10 %", func(d *Doc) { d.SwrBp = 1001 }, "swrBp"},
		{"real return below −10 %", func(d *Doc) { d.RealReturnBp = -1001 }, "realReturnBp"},
		{"real return above 20 %", func(d *Doc) { d.RealReturnBp = 2001 }, "realReturnBp"},
		{"empty name", func(d *Doc) { d.Positions[0].Name = "  " }, "positions[0].name"},
		{"name of 81 characters", func(d *Doc) { d.Positions[0].Name = strings.Repeat("ą", 81) }, "positions[0].name"},
		{"type of 31 characters", func(d *Doc) { d.Positions[0].Type = strings.Repeat("x", 31) }, "positions[0].type"},
		{"negative value", func(d *Doc) { d.Positions[0].ValueMinor = -5 }, "positions[0].valueMinor"},
		{"duplicate id", func(d *Doc) { d.Positions = append(d.Positions, d.Positions[0]) }, "positions[1].id"},
		{"missing id", func(d *Doc) { d.Positions[0].ID = uuid.Nil }, "positions[0].id"},
		{"too many", func(d *Doc) {
			for i := range MaxPositions {
				d.Positions = append(d.Positions, pos(strings.Repeat("p", i%70+1)+string(rune('a'+i%26)), 0, 0, 0))
			}
		}, "positions"},
	}
	for _, c := range cases {
		d := Empty()
		d.Positions = []Position{pos("ETF", 1, 1, 0)}
		c.edit(&d)
		err := d.Validate()
		var ve *platform.ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		found := false
		for _, f := range ve.Fields {
			found = found || f.Field == c.field
		}
		if !found {
			t.Errorf("%s: fields %+v, want %s", c.name, ve.Fields, c.field)
		}
	}
	// the limits themselves are allowed
	d := Empty()
	d.SwrBp, d.RealReturnBp = 50, -1000
	if err := d.Validate(); err != nil {
		t.Errorf("lower limits: %v", err)
	}
	d.SwrBp, d.RealReturnBp = 1000, 2000
	if err := d.Validate(); err != nil {
		t.Errorf("upper limits: %v", err)
	}
}

func TestSummarize(t *testing.T) {
	d := Empty()
	d.PPK = PPK{EmployeeMinor: 1000, EmployerMinor: 750, StateMinor: 250, ValueMinor: 2400}
	d.Positions = []Position{pos("ETF", 4000, 3000, 0), pos("Free", 0, 600, 0)}
	s := Summarize(d)
	if s.PPKPaidIn != 2000 || s.PPKGain != 400 || *s.PPKReturnOnOwn != 1.4 || s.Invested != 6000 || s.Value != 6000 {
		t.Errorf("summary: %+v", s)
	}
	if g := *s.Rows[1].GainPercent; g != -0.25 || s.Rows[2].GainPercent != nil || *s.GainPercent != 0 {
		t.Errorf("gains: %v %v %v", g, s.Rows[2].GainPercent, *s.GainPercent)
	}
	if s.AllocationShares[0] != 0.4 || s.AllocationShares[1] != 0.5 || s.AllocationShares[2] != 0.1 {
		t.Errorf("allocation: %v", s.AllocationShares)
	}
	if e := Summarize(Empty()); e.PPKReturnOnOwn != nil || e.GainPercent != nil || len(e.Rows) != 1 || e.AllocationShares[0] != 0 {
		t.Errorf("empty: %+v", e)
	}
}

func TestYieldOnCost(t *testing.T) {
	d := Empty()
	d.Positions = []Position{pos("Bonds", 20000, 21000, 1360), pos("Stocks", 8000, 9000, 640), pos("Gift", 0, 500, 100)}
	y := YieldOnCost(d, 99999)
	if len(y.Positions) != 2 || y.Invested != 28000 || y.Income != 2000 || y.UsesDetected || *y.Total != 2000.0/28000 || y.Positions[0].Yield != 0.068 {
		t.Errorf("manual income: %+v", y)
	}
	for i := range d.Positions {
		d.Positions[i].AnnualIncomeMinor = 0
	}
	if y := YieldOnCost(d, 977); y.Income != 977 || !y.UsesDetected {
		t.Errorf("detected fallback: %+v", y)
	}
	if y := YieldOnCost(d, 0); y.UsesDetected || y.Income != 0 {
		t.Errorf("nothing detected: %+v", y)
	}
	if y := YieldOnCost(Empty(), 500); y.Total != nil || len(y.Positions) != 0 {
		t.Errorf("no positions: %+v", y)
	}
}

func TestFireOf(t *testing.T) {
	months := func(n int, inc, exp int64) []MonthTotals {
		out := make([]MonthTotals, n)
		for i := range out {
			out[i] = MonthTotals{Income: inc, Expense: exp}
		}
		return out
	}
	d := Empty() // 4 % SWR, 5 % real return
	f := FireOf(d, months(12, 500000, 300000), 1000000)
	// 3 000 a month → 36 000 a year → FI number 900 000; 24 000 saved a year
	if f.AnnualExpenses != 3600000 || f.FINumber != 90000000 || f.YearlySavings != 2400000 || f.Progress != 1.0/90 || !f.Reachable {
		t.Errorf("fire: %+v", f)
	}
	// compounding: 10 000 → (×1.05 + 24 000) … crosses 900 000 in year 22
	if f.Years != 22 {
		t.Errorf("years: %d", f.Years)
	}
	// short history annualises: 3 months of 3 000 → the same FI number
	if s := FireOf(d, months(3, 500000, 300000), 0); s.FINumber != 90000000 || s.Months != 3 {
		t.Errorf("short history: %+v", s)
	}
	// reached, unreachable, no spending
	if r := FireOf(d, months(12, 500000, 300000), 90000000); r.Years != 0 || !r.Reachable || r.Progress != 1 {
		t.Errorf("reached: %+v", r)
	}
	d.RealReturnBp = 0
	if u := FireOf(d, months(12, 0, 300000), 1000); u.Reachable || u.Years != 100 {
		t.Errorf("unreachable: %+v", u)
	}
	if z := FireOf(d, nil, 1000); z.FINumber != 0 || z.Progress != 0 || z.Years != 0 {
		t.Errorf("no months: %+v", z)
	}
}
