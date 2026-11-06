package goals

import (
	"testing"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

func d(y int, m time.Month, day int) platform.Date { return platform.MustDate(y, m, day) }

func TestSaveProgress(t *testing.T) {
	start, deadline := d(2026, 1, 1), d(2026, 12, 31) // 365 days, 10 000.00 to put aside
	const target = 1_000_000
	cases := []struct {
		name          string
		today         platform.Date
		saved         int64
		state         string
		percent       int
		expected      int64
		daysLeft      int
		projectedOver bool
	}{
		{"before the start", d(2025, 12, 20), 0, StateNotStarted, 0, 0, 376, false},
		{"on the pace after a quarter", d(2026, 3, 31), 250_000, StateOnTrack, 25, 246_575, 275, true},
		{"5 % short of the pace still counts", d(2026, 3, 31), 235_000, StateOnTrack, 23, 246_575, 275, false},
		{"more than 5 % short", d(2026, 3, 31), 200_000, StateBehind, 20, 246_575, 275, false},
		{"done early", d(2026, 6, 1), 1_000_000, StateDone, 100, 416_438, 213, true},
		{"missed", d(2027, 1, 5), 900_000, StateMissed, 90, 1_000_000, 0, false},
		{"spent more than earned", d(2026, 2, 1), -50_000, StateBehind, 0, 87_671, 333, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := SaveProgress(start, deadline, tc.today, target, SaveFacts{SavedMinor: tc.saved})
			if p.State != tc.state || p.Percent != tc.percent || p.ExpectedMinor != tc.expected || p.DaysLeft != tc.daysLeft ||
				(p.ProjectedMinor >= target) != tc.projectedOver {
				t.Errorf("%+v", p)
			}
		})
	}
}

func TestReduceProgress(t *testing.T) {
	// 20 % less on eating out than the average of the three months before March: 600, 400, 500 → 500
	start, deadline := d(2026, 3, 1), d(2026, 8, 31)
	months := map[platform.Date]int64{d(2025, 12, 1): 60_000, d(2026, 1, 1): 40_000, d(2026, 2, 1): 50_000}
	facts := func(extra map[platform.Date]int64) ReduceFacts {
		m := map[platform.Date]int64{}
		for k, v := range months {
			m[k] = v
		}
		for k, v := range extra {
			m[k] = v
		}
		return ReduceFacts{Months: m}
	}
	if from, to := BaselineRange(start, 3); from != d(2025, 12, 1) || to != d(2026, 2, 28) {
		t.Errorf("baseline %s..%s", from, to)
	}
	cases := []struct {
		name               string
		today              platform.Date
		extra              map[platform.Date]int64
		state              string
		kept, past         int
		allowed, thisMonth int64
	}{
		{"before the start", d(2026, 2, 20), nil, StateNotStarted, 0, 0, 28_571, 50_000},
		{"mid-March within the pace", d(2026, 3, 15), map[platform.Date]int64{d(2026, 3, 1): 15_000}, StateOnTrack, 0, 0, 19_354, 15_000},
		{"mid-March ahead of the pace", d(2026, 3, 15), map[platform.Date]int64{d(2026, 3, 1): 25_000}, StateBehind, 0, 0, 19_354, 25_000},
		{"April: March kept", d(2026, 4, 10), map[platform.Date]int64{d(2026, 3, 1): 39_000, d(2026, 4, 1): 5_000}, StateOnTrack, 1, 1, 13_333, 5_000},
		{"after the deadline, every month kept", d(2026, 9, 2), map[platform.Date]int64{d(2026, 3, 1): 40_000, d(2026, 4, 1): 30_000,
			d(2026, 5, 1): 1, d(2026, 6, 1): 0, d(2026, 7, 1): 39_999, d(2026, 8, 1): 40_000}, StateDone, 6, 6, 2_666, 0},
		{"after the deadline, one month over", d(2026, 9, 2), map[platform.Date]int64{d(2026, 5, 1): 40_001}, StateMissed, 5, 6, 2_666, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := ReduceProgress(start, deadline, tc.today, 20, 3, facts(tc.extra))
			if p.BaselineMinor != 50_000 || p.MonthlyLimitMinor != 40_000 || p.State != tc.state || p.MonthsKept != tc.kept ||
				p.MonthsPast != tc.past || p.AllowedSoFarMinor != tc.allowed || p.ThisMonthMinor != tc.thisMonth {
				t.Errorf("%+v", p)
			}
		})
	}
	// nothing spent there before: nothing to reduce
	if p := ReduceProgress(start, deadline, d(2026, 4, 1), 20, 3, ReduceFacts{}); p.State != StateNeedsAttention {
		t.Errorf("no baseline: %+v", p)
	}
}
