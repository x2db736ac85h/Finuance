package insights

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/x2db736ac85h/finuance/backend/internal/query"
)

// every builds n charges of payee, step days apart from start, with the given amounts (cycled).
func every(payee, start string, step, n int, amounts ...int64) []query.Charge {
	out := make([]query.Charge, n)
	for i := range out {
		out[i] = query.Charge{PayeeGroup: payee, BookedOn: d(start).AddDays(i * step), SpentMinor: amounts[i%len(amounts)]}
	}
	return out
}

func TestCadences(t *testing.T) {
	end := d("2025-03-20")
	cases := []struct {
		name    string
		charges []query.Charge
		want    string // "" = nothing found
	}{
		{"weekly", every("Gym", "2025-01-03", 7, 10, 2000), CadenceWeekly},
		{"five weeks are not enough", every("Gym", "2025-02-14", 7, 5, 2000), ""},
		{"six weeks are", every("Gym", "2025-02-07", 7, 6, 2000), CadenceWeekly},
		// gaps 7, 7, 7, 14, 7, 7, 7: 6 of 7 in 4–10 days (86 %)
		{"one skipped week", append(every("Gym", "2025-01-03", 7, 4, 2000), every("Gym", "2025-02-07", 7, 4, 2000)...), CadenceWeekly},
		// gaps alternate 3 and 11 days: the median is 11 and none lies in 4–10
		{"irregular", alternate("Shop", "2025-01-01", 3, 11, 10), ""},
		{"amounts vary too much", every("Shop", "2025-01-03", 7, 8, 1000, 2000), ""},
		// every 2–3 days: the median 2 is below 5
		{"too often", every("Bakery", "2025-01-03", 2, 30, 500), ""},
		{"yearly", every("Insurance", "2023-03-10", 365, 3, 60000, 62000, 65000), CadenceYearly},
		{"two years are enough", every("Domain", "2024-01-15", 366, 2, 5000), CadenceYearly},
		{"18 months apart is not yearly", every("Domain", "2023-06-15", 548, 2, 5000), ""},
		{"a one-off", every("Domain", "2024-06-15", 1, 1, 5000), ""},
	}
	for _, c := range cases {
		got := WithCadences(nil, c.charges, end)
		switch {
		case c.want == "" && len(got) != 0:
			t.Errorf("%s: found %+v", c.name, got)
		case c.want != "" && (len(got) != 1 || got[0].Cadence != c.want):
			t.Errorf("%s: %+v, want one %s payment", c.name, got, c.want)
		}
	}
}

func alternate(payee, start string, a, b, n int) []query.Charge {
	out := make([]query.Charge, n)
	day := d(start)
	for i := range out {
		out[i] = query.Charge{PayeeGroup: payee, BookedOn: day, SpentMinor: 1000}
		if i%2 == 0 {
			day = day.AddDays(a)
		} else {
			day = day.AddDays(b)
		}
	}
	return out
}

func TestCadenceFields(t *testing.T) {
	gym := every("Gym", "2025-01-03", 7, 10, 2000, 2000, 2100) // last 2025-03-07
	got := WithCadences(nil, gym, d("2025-03-21"))
	want := Recurring{Payee: "Gym", MedianMinor: 2000, Last: d("2025-03-07"), Next: d("2025-03-14"), Active: true,
		YearlyMinor: 2000 * 52, Count: 10, Cadence: CadenceWeekly}
	if diff := cmp.Diff([]Recurring{want}, got); diff != "" {
		t.Errorf("weekly (-want +got):\n%s", diff)
	}
	if r := WithCadences(nil, gym, d("2025-03-22")); r[0].Active {
		t.Error("15 days after the last weekly charge is not active")
	}

	cat := "Ubezpieczenia"
	ins := every("PZU", "2023-02-28", 366, 2, 60000) // 2023-02-28, 2024-02-29
	ins[1].Category = &cat
	got = WithCadences(nil, ins, d("2025-03-20"))
	want = Recurring{Payee: "PZU", Category: "", MedianMinor: 60000, Last: d("2024-02-29"), Next: d("2025-02-28"), Active: true,
		YearlyMinor: 60000, Count: 2, Cadence: CadenceYearly}
	if diff := cmp.Diff([]Recurring{want}, got); diff != "" {
		t.Errorf("yearly (-want +got; next clamped to 28 Feb; a category tie goes to the first seen):\n%s", diff)
	}
	if r := WithCadences(nil, ins, d("2025-04-05")); r[0].Active {
		t.Error("401 days after the last yearly charge is not active")
	}
}

func TestWithCadencesMerges(t *testing.T) {
	end := d("2025-03-20")
	monthly := []Recurring{
		{Payee: "Netflix", Active: true, YearlyMinor: 6000 * 12, Cadence: CadenceMonthly},
		{Payee: "Old", Active: false, YearlyMinor: 90000, Cadence: CadenceMonthly},
	}
	others := append(every("Gym", "2025-01-03", 7, 10, 2000), every("Insurance", "2023-03-10", 365, 3, 60000)...)
	// a group the monthly rules already took is not looked at again
	others = append(others, every("Netflix", "2025-01-03", 7, 10, 2000)...)
	var order []string
	for _, r := range WithCadences(monthly, others, end) {
		order = append(order, r.Payee+" "+r.Cadence)
	}
	want := []string{"Gym weekly", "Netflix monthly", "Insurance yearly", "Old monthly"}
	if diff := cmp.Diff(want, order); diff != "" {
		t.Errorf("order (-want +got; active first, then yearly cost):\n%s", diff)
	}
	if monthly[0].Payee != "Netflix" || monthly[1].Payee != "Old" {
		t.Error("the monthly list was reordered in place")
	}
	if got := WithCadences(monthly, nil, end); &got[0] != &monthly[0] {
		t.Error("without other cadences the monthly list comes back as it is")
	}
}
