package splits

import (
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/google/uuid"
)

func ids(n int) []uuid.UUID {
	out := make([]uuid.UUID, n)
	for i := range out {
		out[i] = uuid.UUID{byte(i + 1)}
	}
	return out
}

func sum(s []Share) int64 {
	var t int64
	for _, x := range s {
		t += x.Minor
	}
	return t
}

func TestEqual(t *testing.T) {
	m := ids(3)
	for _, c := range []struct {
		amount int64
		want   []int64
	}{
		{100, []int64{34, 33, 33}},
		{101, []int64{34, 34, 33}},
		{-100, []int64{-34, -33, -33}},
		{2, []int64{1, 1, 0}},
		{-1, []int64{-1, 0, 0}},
	} {
		got, err := Equal(c.amount, m)
		if err != nil {
			t.Fatal(err)
		}
		var minors []int64
		for _, s := range got {
			minors = append(minors, s.Minor)
		}
		if !slices.Equal(minors, c.want) || sum(got) != c.amount {
			t.Errorf("Equal(%d) = %v, want %v", c.amount, minors, c.want)
		}
	}
	if _, err := Equal(100, nil); err == nil {
		t.Error("nobody to split between")
	}
}

func TestWeighted(t *testing.T) {
	m := ids(3)
	got, err := Weighted(1000, m, []int{2, 1, 1})
	if err != nil || got[0].Minor != 500 || got[1].Minor != 250 || got[2].Minor != 250 {
		t.Errorf("2:1:1 of 1000 = %v, %v", got, err)
	}
	got, _ = Weighted(100, m, []int{1, 1, 1})
	if sum(got) != 100 || got[0].Minor != 34 {
		t.Errorf("1:1:1 of 100 = %v", got)
	}
	got, _ = Weighted(-100, m, []int{1, 2, 3})
	if sum(got) != -100 {
		t.Errorf("negative = %v", got)
	}
	for _, bad := range [][]int{{0, 1, 1}, {1, 1}, {-1, 1, 1}} {
		if _, err := Weighted(100, m, bad); err == nil {
			t.Errorf("weights %v accepted", bad)
		}
	}
}

func TestExact(t *testing.T) {
	m := ids(2)
	if err := Exact(100, []Share{{Member: m[0], Minor: 70}, {Member: m[1], Minor: 30}}); err != nil {
		t.Error(err)
	}
	if err := Exact(100, []Share{{Member: m[0], Minor: 70}, {Member: m[1], Minor: 20}}); err == nil {
		t.Error("90 of 100 accepted")
	}
	if err := Exact(100, []Share{{Member: m[0], Minor: 50}, {Member: m[0], Minor: 50}}); err == nil {
		t.Error("a member twice accepted")
	}
}

// TestBalancesProperty: for random groups, the balances sum to zero, the simplification settles
// every balance exactly, and it needs at most n − 1 transfers.
func TestBalancesProperty(t *testing.T) {
	r := rand.New(rand.NewPCG(19, 9))
	for round := range 2000 {
		n := 2 + r.IntN(7)
		m := ids(n)
		var exps []Expense
		for range r.IntN(30) {
			amount := r.Int64N(50000) - 2000
			if amount == 0 {
				amount = 1
			}
			var sh []Share
			switch r.IntN(3) {
			case 0:
				sh, _ = Equal(amount, m)
			case 1:
				w := make([]int, n)
				for i := range w {
					w[i] = 1 + r.IntN(5)
				}
				sh, _ = Weighted(amount, m, w)
			default: // for someone else
				sh = []Share{{Member: m[r.IntN(n)], Minor: amount}}
			}
			exps = append(exps, Expense{PaidBy: m[r.IntN(n)], Amount: amount, Shares: sh})
		}
		var setts []Settlement
		for range r.IntN(3) {
			a, b := r.IntN(n), r.IntN(n)
			if a != b {
				setts = append(setts, Settlement{From: m[a], To: m[b], Amount: 1 + r.Int64N(1000)})
			}
		}
		bal := Balances(exps, setts)
		var total int64
		for _, v := range bal {
			total += v
		}
		if total != 0 {
			t.Fatalf("round %d: balances sum to %d", round, total)
		}
		tr := Simplify(bal)
		if len(tr) > n-1 {
			t.Fatalf("round %d: %d transfers for %d members", round, len(tr), n)
		}
		after := map[uuid.UUID]int64{}
		for k, v := range bal {
			after[k] = v
		}
		for _, x := range tr {
			if x.Amount <= 0 || x.From == x.To {
				t.Fatalf("round %d: transfer %+v", round, x)
			}
			after[x.From] += x.Amount
			after[x.To] -= x.Amount
		}
		for k, v := range after {
			if v != 0 {
				t.Fatalf("round %d: %s left at %d", round, k, v)
			}
		}
	}
}
