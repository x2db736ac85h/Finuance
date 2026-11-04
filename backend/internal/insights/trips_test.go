package insights

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

func tday(y, m, d int) *platform.Date {
	v := platform.Date{Y: y, M: time.Month(m), D: d}
	return &v
}

func TestSpanDays(t *testing.T) {
	cases := []struct {
		name                    string
		start, end, first, last *platform.Date
		want                    int
	}{
		{"empty", nil, nil, nil, nil, 1},
		{"one day", nil, nil, tday(2026, 5, 2), tday(2026, 5, 2), 1},
		{"expenses", nil, nil, tday(2026, 5, 2), tday(2026, 5, 8), 7},
		{"own dates win", tday(2026, 5, 1), tday(2026, 5, 10), tday(2026, 5, 2), tday(2026, 5, 8), 10},
		{"start only", tday(2026, 5, 1), nil, tday(2026, 5, 2), tday(2026, 5, 3), 2},
		{"reversed own dates ignored", tday(2026, 5, 10), tday(2026, 5, 1), tday(2026, 5, 2), tday(2026, 5, 3), 2},
		{"across a month", nil, nil, tday(2026, 1, 30), tday(2026, 2, 2), 4},
	}
	for _, c := range cases {
		if got := SpanDays(c.start, c.end, c.first, c.last); got != c.want {
			t.Errorf("%s: %d, want %d", c.name, got, c.want)
		}
	}
}

func trip(name, kind string, spent int64, days int, cats ...NamedSpend) TripGroup {
	first := tday(2025, 6, 1)
	last := platform.Date{Y: 2025, M: 6, D: days}
	return TripGroup{ID: uuid.New(), Source: "personal", Name: name, Kind: kind, Currency: "PLN", FirstOn: first, LastOn: &last,
		Spent: spent, Count: 3, Categories: cats}
}

func TestTimelineHints(t *testing.T) {
	// three earlier trips at 100/day with food at 30/day; the newest spent 200/day, food 75/day
	a := trip("A", "trip", 1000, 10, NamedSpend{Name: "Jedzenie", Spent: 300})
	b := trip("B", "trip", 500, 5, NamedSpend{Name: "Jedzenie", Spent: 150})
	c := trip("C", "trip", 2000, 20, NamedSpend{Name: "Jedzenie", Spent: 600})
	n := trip("New", "trip", 800, 4, NamedSpend{Name: "Jedzenie", Spent: 300}, NamedSpend{Name: "Muzea", Spent: 20})
	n.LastOn, n.FirstOn = tday(2026, 7, 4), tday(2026, 7, 1)
	budget := int64(700)
	n.BudgetMinor = &budget
	other := trip("Home", "household", 9000, 30) // another kind: never compared
	tl := BuildTimeline([]TripGroup{a, b, c, n, other})
	if tl.Groups[0].Name != "New" || tl.Groups[0].PerDay != 200 || tl.Groups[0].Top.Name != "Jedzenie" {
		t.Fatalf("newest = %+v", tl.Groups[0])
	}
	got := map[string]Hint{}
	for _, h := range tl.Hints {
		if h.GroupID == n.ID {
			got[h.Kind] = h
		} else if h.Kind != HintOverBudget {
			t.Errorf("unexpected hint %+v", h)
		}
	}
	if h := got[HintPerDayHigh]; h.Ratio != 2 {
		t.Errorf("per day: %+v", h)
	}
	if h := got[HintCategoryHigh]; h.Category != "Jedzenie" || h.Ratio != 2.5 {
		t.Errorf("category: %+v", h)
	}
	if h := got[HintOverBudget]; h.Ratio < 1.14 || h.Ratio > 1.15 {
		t.Errorf("budget: %+v", h)
	}
	// at the threshold exactly (1.5×) a hint appears, just below it doesn't
	n2 := trip("Edge", "trip", 600, 4) // 150/day vs median 100
	n2.FirstOn, n2.LastOn = tday(2026, 8, 1), tday(2026, 8, 4)
	if h := BuildTimeline([]TripGroup{a, b, c, n2}).Hints; len(h) != 1 || h[0].Kind != HintPerDayHigh {
		t.Errorf("at 1.5×: %+v", h)
	}
	n2.Spent = 596 // 149/day
	if h := BuildTimeline([]TripGroup{a, b, c, n2}).Hints; len(h) != 0 {
		t.Errorf("below 1.5×: %+v", h)
	}
	// one comparison isn't enough
	if h := BuildTimeline([]TripGroup{a, n}).Hints; len(h) != 1 || h[0].Kind != HintOverBudget {
		t.Errorf("with one other trip: %+v", h)
	}
	// years: by the last day, in the most common currency
	if len(tl.Years) != 2 || tl.Years[0].Year != 2026 || tl.Years[0].Spent != 800 || tl.Years[1].Groups != 4 || tl.Years[1].Spent != 12500 {
		t.Errorf("years = %+v", tl.Years)
	}
}

func TestTripReport(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	exps := []TripExpense{
		{ID: uuid.New(), PaidBy: a, BookedOn: *tday(2026, 5, 2), Amount: 600, Payee: "Hotel", Category: "Nocleg", Shares: map[uuid.UUID]int64{a: 300, b: 300}},
		{ID: uuid.New(), PaidBy: b, BookedOn: *tday(2026, 5, 3), Amount: 90, Payee: "Bar", Category: "Jedzenie", Shares: map[uuid.UUID]int64{a: 45, b: 45}},
		{ID: uuid.New(), PaidBy: b, BookedOn: *tday(2026, 5, 3), Amount: 10, Payee: "Bar", Category: "Jedzenie", Shares: map[uuid.UUID]int64{b: 10}},
	}
	r := BuildTripReport(exps, []uuid.UUID{a, b}, nil, nil)
	if r.Total != 700 || r.Days != 2 || r.AvgPerDay != 350 || len(r.Daily) != 2 || r.Daily[1].Spent != 100 {
		t.Errorf("totals %+v", r)
	}
	if r.Categories[0].Name != "Nocleg" || r.Payees[1] != (NamedSpend{Name: "Bar", Spent: 100, Count: 2}) || r.Largest[0] != exps[0].ID {
		t.Errorf("lists %+v %+v %+v", r.Categories, r.Payees, r.Largest)
	}
	if r.Members[0] != (MemberPaidShare{Member: a, Paid: 600, Share: 345}) || r.Members[1] != (MemberPaidShare{Member: b, Paid: 100, Share: 355}) {
		t.Errorf("members %+v", r.Members)
	}
	empty := BuildTripReport(nil, []uuid.UUID{a}, tday(2026, 1, 1), tday(2026, 1, 3))
	if empty.Days != 3 || empty.Total != 0 || len(empty.Members) != 1 {
		t.Errorf("empty %+v", empty)
	}
}
