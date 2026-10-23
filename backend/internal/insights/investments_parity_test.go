package insights_test

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/investments"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
)

// investmentDatasets rebuilds the oracle's variants of the demo file (tools/legacy-oracle/oracle.mjs
// investmentDatasets): with dividend and interest income, a short history, expenses only.
func investmentDatasets(demo string) map[string]string {
	lines := strings.Split(demo, "\n")
	head, rows := lines[0], lines[1:]
	date := func(r string) string {
		p := strings.Split(strings.Split(r, ";")[0], ".")
		if len(p) != 3 {
			return ""
		}
		return p[2] + "-" + p[1] + "-" + p[0]
	}
	amount := func(r string) float64 {
		f := strings.Split(r, ";")
		if len(f) < 8 {
			return math.NaN()
		}
		v, _ := strconv.ParseFloat(strings.ReplaceAll(strings.ReplaceAll(f[7], " ", ""), ",", "."), 64)
		return v
	}
	extra := []string{
		"15.06.2024;15.06.2024;DOM MAKLERSKI PRZYKŁADOWY;;00000000000000000000000000;;DYWIDENDA SPÓŁKA ALFA;640,00;PLN;DEMODIV1;PRZELEW PRZYCHODZĄCY;Inwestycje",
		"30.09.2024;30.09.2024;BANK PRZYKŁADOWY;;00000000000000000000000000;;ODSETKI OD LOKATY;125,40;PLN;DEMODIV2;PRZELEW PRZYCHODZĄCY;Inwestycje",
		"20.12.2024;20.12.2024;DOM MAKLERSKI PRZYKŁADOWY;;00000000000000000000000000;;DIVIDEND ETF;212,15;PLN;DEMODIV3;PRZELEW PRZYCHODZĄCY;Inwestycje",
	}
	keep := func(f func(string) bool) string {
		out := []string{head}
		for _, r := range rows {
			if f(r) {
				out = append(out, r)
			}
		}
		return strings.Join(out, "\n")
	}
	return map[string]string{
		"demo":               demo,
		"demo-dividends":     strings.Join(append(append([]string{head}, extra...), rows...), "\n"),
		"demo-short":         keep(func(r string) bool { return date(r) >= "2024-11-01" }),
		"demo-expenses-only": keep(func(r string) bool { return amount(r) < 0 }),
	}
}

type legacyInv struct {
	PPK       struct{ Employee, Employer, State, Value float64 }
	Positions []struct {
		ID              string
		Name, Type      string
		Invested, Value float64
		Income          json.RawMessage // a number or "" (legacy text inputs)
	}
	Swr float64
	Ret float64
}

func (l legacyInv) doc() investments.Doc {
	d := investments.Doc{SwrBp: int(math.Round(l.Swr * 100)), RealReturnBp: int(math.Round(l.Ret * 100)),
		PPK: investments.PPK{EmployeeMinor: minorOf(l.PPK.Employee), EmployerMinor: minorOf(l.PPK.Employer), StateMinor: minorOf(l.PPK.State),
			ValueMinor: minorOf(l.PPK.Value)}}
	for _, p := range l.Positions {
		var inc float64
		_ = json.Unmarshal(p.Income, &inc) // "" stays 0, as +'' || 0 in the legacy
		d.Positions = append(d.Positions, investments.Position{ID: uuid.NewSHA1(uuid.Nil, []byte(p.ID)), Name: p.Name, Type: p.Type,
			InvestedMinor: minorOf(p.Invested), ValueMinor: minorOf(p.Value), AnnualIncomeMinor: minorOf(inc)})
	}
	return d
}

// pctS is the legacy signed percentage with one decimal ("+10.9%").
func pctS(x *float64) string {
	if x == nil {
		return "–"
	}
	s := fixed(*x*100, 1) + "%"
	if *x >= 0 {
		return "+" + s
	}
	return s
}

func invTwin(s investments.Summary) *twin {
	tw := &twin{Cols: []string{"Position", "Invested", "Value", "Annual income", "Gain", "Gain %"}}
	for _, r := range s.Rows {
		tw.Rows = append(tw.Rows, []string{r.Name, money(r.Invested), money(r.Value), money(r.Income), money(r.Value - r.Invested), pctS(r.GainPercent)})
	}
	return tw
}

func yocTwin(y investments.Yield) *twin {
	tw := &twin{Cols: []string{"Position", "Invested", "Annual income", "Yield on cost"}}
	for _, p := range y.Positions {
		tw.Rows = append(tw.Rows, []string{p.Name, money(p.Invested), money(p.Income), fixed(p.Yield*100, 2) + "%"})
	}
	total := "–"
	if y.Total != nil {
		total = fixed(*y.Total*100, 2) + "%"
	}
	tw.Rows = append(tw.Rows, []string{"Total", money(y.Invested), money(y.Income), total})
	return tw
}

func fireTwin(f investments.FIRE) *twin {
	years := strconv.Itoa(f.Years)
	if !f.Reachable {
		years = "–"
	}
	return &twin{Cols: []string{"Item", "Value"}, Rows: [][]string{
		{"Annual expenses", money(f.AnnualExpenses)},
		{"Safe withdrawal rate", fixed(float64(f.SwrBp)/100, 1) + "%"},
		{"FI number", money(f.FINumber)},
		{"Invested assets", money(f.Assets)},
		{"Progress", fixed(f.Progress*100, 1) + "%"},
		{"Yearly savings", money(f.YearlySavings)},
		{"Years to FI", years},
	}}
}

func TestInvestmentCardsParity(t *testing.T) {
	demo := string(testdata(t, "fixtures/demo/demo-legacy.csv"))
	variants := investmentDatasets(strings.TrimRight(demo, "\n"))
	files, err := filepath.Glob(filepath.Join("..", "..", "..", "testdata", "oracle", "investments", "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no investment oracle files: %v", err)
	}
	sort.Strings(files)
	e := newEnv(t)
	users := map[string]uuid.UUID{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var o struct {
			Portfolio string    `json:"portfolio"`
			Dataset   string    `json:"dataset"`
			Inv       legacyInv `json:"inv"`
			Months    []struct {
				Month string           `json:"month"`
				Cards map[string]*twin `json:"cards"`
			} `json:"months"`
		}
		if err := json.Unmarshal(raw, &o); err != nil {
			t.Fatal(err)
		}
		t.Run(o.Portfolio, func(t *testing.T) {
			u, ok := users[o.Dataset]
			if !ok {
				u = e.user()
				e.importFile(u, "Konto", []byte(variants[o.Dataset]))
				users[o.Dataset] = u
			}
			doc := o.Inv.doc()
			if err := doc.Validate(); err != nil {
				t.Fatalf("portfolio doesn't validate: %v", err)
			}
			for _, m := range o.Months {
				month, err := platform.ParseDate(m.Month)
				if err != nil {
					t.Fatal(err)
				}
				ov, err := e.svc.Overview(e.ctx, u, query.Context{}, &month, 12, doc)
				if err != nil {
					t.Fatal(err)
				}
				for card, got := range map[string]*twin{"o_inv": invTwin(ov.Investment), "o_yoc": yocTwin(ov.YieldOnCost), "o_fire": fireTwin(ov.Fire)} {
					if diff := cmp.Diff(m.Cards[card], got); diff != "" {
						t.Errorf("%s %s (-legacy +new):\n%s", m.Month, card, diff)
					}
				}
			}
		})
	}
}
