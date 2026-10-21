package query

import (
	"testing"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

func TestResolvePeriod(t *testing.T) {
	d := platform.MustDate
	anchor := d(2025, 3, 17) // the demo year's last day
	leap := d(2024, 2, 29)
	for _, tc := range []struct {
		p        Period
		anchor   platform.Date
		want     Resolved
		prevFrom platform.Date
	}{
		{Period{Preset: "l30"}, anchor, Resolved{"range", d(2025, 2, 16), d(2025, 3, 18), "Last 30 days"}, d(2025, 1, 17)},
		{Period{Preset: "l3m"}, anchor, Resolved{"range", d(2025, 1, 1), d(2025, 4, 1), "Last 3 months"}, d(2024, 10, 3)},
		{Period{Preset: "l12m"}, anchor, Resolved{"range", d(2024, 4, 1), d(2025, 4, 1), "Last 12 months"}, d(2023, 4, 2)},
		{Period{Preset: "ytd"}, anchor, Resolved{"range", d(2025, 1, 1), d(2025, 3, 18), "Year to date 2025"}, d(2024, 10, 17)},
		{Period{Preset: "l30"}, leap, Resolved{"range", d(2024, 1, 31), d(2024, 3, 1), "Last 30 days"}, d(2024, 1, 1)},
		{Period{Preset: "l3m"}, d(2025, 1, 31), Resolved{"range", d(2024, 11, 1), d(2025, 2, 1), "Last 3 months"}, d(2024, 8, 1)},
		{Period{Month: "2025-03"}, anchor, Resolved{"month", d(2025, 3, 1), d(2025, 4, 1), "Mar 2025"}, d(2025, 2, 1)},
		{Period{Month: "2024-01"}, anchor, Resolved{"month", d(2024, 1, 1), d(2024, 2, 1), "Jan 2024"}, d(2023, 12, 1)},
		{Period{Year: 2024}, anchor, Resolved{"year", d(2024, 1, 1), d(2025, 1, 1), "2024"}, d(2023, 1, 1)},
		{Period{From: ptr(d(2025, 3, 10)), To: ptr(d(2025, 3, 11))}, anchor, Resolved{"range", d(2025, 3, 10), d(2025, 3, 11), "10 Mar 2025"}, d(2025, 3, 9)},
		{Period{From: ptr(d(2025, 2, 1)), To: ptr(d(2025, 3, 1))}, anchor, Resolved{"range", d(2025, 2, 1), d(2025, 3, 1), "1 Feb 2025 – 28 Feb 2025"}, d(2025, 1, 4)},
	} {
		a := tc.anchor
		got, err := Resolve(&tc.p, &a)
		if err != nil || got == nil || *got != tc.want {
			t.Errorf("Resolve(%+v) = %+v, %v; want %+v", tc.p, got, err, tc.want)
			continue
		}
		if from, to := got.Prev(); from != tc.prevFrom || to != got.From {
			t.Errorf("Prev(%+v) = %s..%s, want %s..%s", tc.p, from, to, tc.prevFrom, got.From)
		}
	}
}

func TestResolvePeriodEdges(t *testing.T) {
	if r, err := Resolve(nil, nil); r != nil || err != nil {
		t.Errorf("nil period = %v, %v", r, err)
	}
	if r, err := Resolve(&Period{Preset: "l30"}, nil); r != nil || err != nil {
		t.Errorf("preset without data = %v, %v (want all time)", r, err)
	}
	d := platform.MustDate(2025, 3, 1)
	for _, p := range []Period{{}, {Preset: "all"}, {Preset: "l30", Year: 2024}, {Month: "2025-3"}, {Month: "2025-13"}, {Year: 99},
		{From: &d}, {From: &d, To: &d}} {
		if _, err := Resolve(&p, &d); err == nil {
			t.Errorf("Resolve(%+v) accepted", p)
		}
	}
}

func ptr[T any](v T) *T { return &v }
