package insights_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/insights"
	"github.com/x2db736ac85h/finuance/backend/internal/investments"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
)

// Nine categories plus uncategorised spending: slots follow the all-time ranking of
// query.Engine.Categories (uncategorised takes part), and the period doesn't repaint them.
const categorised = `Data;Kwota;Odbiorca;Kategoria
01.01.2025;-900,00;A;Czynsz
02.01.2025;-800,00;B;
03.01.2025;-700,00;C;Jedzenie
04.01.2025;-600,00;D;Transport
05.01.2025;-500,00;E;Zdrowie
06.01.2025;-400,00;F;Kino
07.01.2025;-300,00;G;Prezenty
08.01.2025;-200,00;H;Ubrania
09.01.2025;-100,00;I;Książki
10.01.2025;-50,00;J;Kawa
01.02.2025;-10,00;K;Kawa
02.02.2025;-5,00;L;
03.02.2025;5000,00;Pracodawca;Wynagrodzenie
`

func TestCategoriesSlotsAndUncategorised(t *testing.T) {
	e := newEnv(t)
	e.u = e.user()
	acc := e.importFile(e.u, "Konto", []byte(categorised))

	all, err := e.svc.Categories(e.ctx, e.u, query.Context{})
	if err != nil {
		t.Fatal(err)
	}
	if all.TotalMinor != 456500 || all.Currency != "PLN" {
		t.Fatalf("total %d %q", all.TotalMinor, all.Currency)
	}
	slots := map[string]*int{}
	for _, c := range all.Categories {
		name := "(uncategorized)"
		if c.Name != nil {
			name = *c.Name
		}
		slots[name] = c.ColorSlot
	}
	want := map[string]int{"Czynsz": 1, "(uncategorized)": 2, "Jedzenie": 3, "Transport": 4, "Zdrowie": 5, "Kino": 6, "Prezenty": 7}
	for name, s := range slots {
		if w, ok := want[name]; ok != (s != nil) || (ok && *s != w) {
			t.Errorf("%s: slot %v, want %v", name, s, w)
		}
	}
	if first := all.Categories[0]; first.Name == nil || *first.Name != "Czynsz" || first.Share != 900.0/4565 {
		t.Errorf("first category: %+v", first)
	}

	// The engine's category list gives every named category the same slot.
	listed, err := query.New(e.st).Categories(e.ctx, e.u, []uuid.UUID{acc.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range listed {
		if s := slots[c.Name]; (s == nil) != (c.ColorSlot == nil) || (s != nil && *s != *c.ColorSlot) {
			t.Errorf("%s: insights slot %v, categories slot %v", c.Name, s, c.ColorSlot)
		}
	}

	// February only: Kawa and the uncategorised row keep their all-time colours.
	feb, err := e.svc.Categories(e.ctx, e.u, query.Context{Period: &query.Period{Month: "2025-02"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(feb.Categories) != 2 || feb.TotalMinor != 1500 {
		t.Fatalf("february: %+v", feb)
	}
	if k := feb.Categories[0]; k.Name == nil || *k.Name != "Kawa" || k.ColorSlot != nil {
		t.Errorf("Kawa: %+v", k)
	}
	if u := feb.Categories[1]; u.ID != nil || u.ColorSlot == nil || *u.ColorSlot != 2 {
		t.Errorf("uncategorised: %+v", u)
	}
}

func TestEmptyUser(t *testing.T) {
	e := newEnv(t)
	e.u = e.user()
	c := query.Context{Period: &query.Period{Preset: "l30"}}
	s, err := e.svc.Summary(e.ctx, e.u, c)
	if err != nil {
		t.Fatal(err)
	}
	if s.Period != nil || s.Days != 1 || s.Count != 0 || s.Largest != nil || s.SavingsRate != nil || s.Currency != "" {
		t.Errorf("summary: %+v", s)
	}
	if cf, err := e.svc.Cashflow(e.ctx, e.u, c); err != nil || len(cf.Months) != 0 {
		t.Errorf("cashflow: %+v %v", cf, err)
	}
	if p, err := e.svc.Pace(e.ctx, e.u, c, insights.ComparePrev); err != nil || p.Target != nil {
		t.Errorf("pace: %+v %v", p, err)
	}
	if p, err := e.svc.Payees(e.ctx, e.u, c, 25, insights.ScopeAll); err != nil || len(p.Payees) != 0 || p.Total != 0 {
		t.Errorf("payees: %+v %v", p, err)
	}
	if d, err := e.svc.Daily(e.ctx, e.u, c, insights.RangeAll); err != nil || d.From != nil {
		t.Errorf("daily: %+v %v", d, err)
	}
	if cat, err := e.svc.Categories(e.ctx, e.u, c); err != nil || len(cat.Categories) != 0 {
		t.Errorf("categories: %+v %v", cat, err)
	}
}

func TestValidation(t *testing.T) {
	e := newEnv(t)
	e.u = e.user()
	var ve *platform.ValidationError
	if _, err := e.svc.Pace(e.ctx, e.u, query.Context{}, "weekly"); !errors.As(err, &ve) {
		t.Errorf("pace: %v", err)
	}
	if _, err := e.svc.Payees(e.ctx, e.u, query.Context{}, 7, "forever"); !errors.As(err, &ve) || len(ve.Fields) != 2 {
		t.Errorf("payees: %v", err)
	}
	if _, err := e.svc.Daily(e.ctx, e.u, query.Context{}, "2w"); !errors.As(err, &ve) {
		t.Errorf("daily: %v", err)
	}
	if _, err := e.svc.Summary(e.ctx, e.u, query.Context{Filters: []query.Filter{{Column: "nope", Cond: "x"}}}); !errors.As(err, &ve) {
		t.Errorf("summary: %v", err)
	}
}

// Weekly and yearly payments come from the database next to the monthly ones (PLAN 15.4); a
// payee with irregular charges is left out.
func TestOverviewCadences(t *testing.T) {
	e := newEnv(t)
	e.u = e.user()
	csv := "Data;Kwota;Odbiorca;Kategoria\n"
	add := func(day platform.Date, amount, payee, cat string) {
		csv += day.Time().Format("02.01.2006") + ";" + amount + ";" + payee + ";" + cat + "\n"
	}
	start := platform.Date{Y: 2025, M: 1, D: 3}
	for i := range 12 {
		add(start.AddDays(7*i), "-25,00", "SILOWNIA", "Sport") // weekly, last 2025-03-21
	}
	for y := range 3 {
		add(platform.Date{Y: 2022 + y, M: 3, D: 10}, "-600,00", "PZU", "Ubezpieczenia") // yearly, last 2024-03-10
	}
	for m := range 3 {
		add(platform.Date{Y: 2025, M: 1, D: 5}.AddMonths(m), "-49,00", "NETFLIX", "Rozrywka")
	}
	for _, d := range []int{1, 2, 3, 15, 16, 28, 40, 41, 60, 75} {
		add(start.AddDays(d), "-"+[]string{"12,00", "80,00", "7,50"}[d%3], "ZABKA", "Jedzenie")
	}
	e.importFile(e.u, "Konto", []byte(csv))

	ov, err := e.svc.Overview(e.ctx, e.u, query.Context{}, nil, 6, investments.Doc{})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range ov.Recurring {
		got = append(got, fmt.Sprintf("%s %s %d %s active=%v", r.Payee, r.Cadence, r.YearlyMinor, r.Next, r.Active))
	}
	want := []string{
		"SILOWNIA weekly 130000 2025-03-28 active=true",
		"PZU yearly 60000 2025-03-10 active=true",
		"NETFLIX monthly 58800 2025-04-05 active=true",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("recurring (-want +got):\n%s", diff)
	}
	for _, h := range ov.Highlights {
		if h.Kind == insights.HLRecurring && (h.Count != 3 || h.Other != 130000+58800+60000 || h.Amount != (130000+58800+60000)/12) {
			t.Errorf("recurring highlight: %+v", h)
		}
	}
}
