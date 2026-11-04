// Package splits is the arithmetic of shared groups (ADR 0028): splitting an amount between
// members, members' balances, and the fewest transfers that settle them. It is pure: minor units in,
// minor units out, no floats, no database.
package splits

import (
	"cmp"
	"errors"
	"slices"

	"github.com/google/uuid"
)

// ErrShares means a split doesn't add up or names nobody.
var ErrShares = errors.New("splits: invalid shares")

// Share is one member's part of an expense.
type Share struct {
	Member uuid.UUID
	Minor  int64
	Weight int // only in the shares mode
}

// Equal splits amount between members (in the order given; callers pass them sorted by id) so
// that the parts differ by at most one minor unit and add up exactly. The first members take the
// remainder. A negative amount (a refund) splits the same way.
func Equal(amount int64, members []uuid.UUID) ([]Share, error) {
	if len(members) == 0 {
		return nil, ErrShares
	}
	n := int64(len(members))
	base, rem := amount/n, amount%n // Go truncates toward zero: rem has amount's sign
	step := int64(1)
	if rem < 0 {
		step, rem = -1, -rem
	}
	out := make([]Share, len(members))
	for i, m := range members {
		out[i] = Share{Member: m, Minor: base}
		if int64(i) < rem {
			out[i].Minor += step
		}
	}
	return out, nil
}

// Weighted splits amount in proportion to the weights (all > 0), exactly: each part is rounded
// down and the cents left over go, one each, to the members with the largest remainders (ties to
// the first given).
func Weighted(amount int64, members []uuid.UUID, weights []int) ([]Share, error) {
	if len(members) == 0 || len(members) != len(weights) {
		return nil, ErrShares
	}
	var total int64
	for _, w := range weights {
		if w <= 0 || w > 1000 {
			return nil, ErrShares
		}
		total += int64(w)
	}
	sign := int64(1)
	if amount < 0 {
		sign, amount = -1, -amount
	}
	type part struct {
		i   int
		rem int64
	}
	out := make([]Share, len(members))
	parts := make([]part, len(members))
	var given int64
	for i, m := range members {
		q := amount * int64(weights[i]) / total
		out[i] = Share{Member: m, Minor: q, Weight: weights[i]}
		parts[i] = part{i, amount * int64(weights[i]) % total}
		given += q
	}
	slices.SortStableFunc(parts, func(a, b part) int { return cmp.Compare(b.rem, a.rem) })
	for k := int64(0); k < amount-given; k++ {
		out[parts[k].i].Minor++
	}
	for i := range out {
		out[i].Minor *= sign
	}
	return out, nil
}

// Exact checks shares given in minor units: they must add up to amount.
func Exact(amount int64, shares []Share) error {
	if len(shares) == 0 {
		return ErrShares
	}
	var sum int64
	seen := map[uuid.UUID]bool{}
	for _, s := range shares {
		if seen[s.Member] {
			return ErrShares
		}
		seen[s.Member] = true
		sum += s.Minor
	}
	if sum != amount {
		return ErrShares
	}
	return nil
}

// Expense is what balances need of one expense.
type Expense struct {
	PaidBy uuid.UUID
	Amount int64
	Shares []Share
}

// Settlement is a payment from one member to another.
type Settlement struct {
	From, To uuid.UUID
	Amount   int64
}

// Balances returns each member's balance: what they paid minus their shares, plus what they paid
// back minus what they were paid. Positive: the others owe them. The balances sum to zero.
func Balances(expenses []Expense, settlements []Settlement) map[uuid.UUID]int64 {
	out := map[uuid.UUID]int64{}
	for _, e := range expenses {
		out[e.PaidBy] += e.Amount
		for _, s := range e.Shares {
			out[s.Member] -= s.Minor
		}
	}
	for _, s := range settlements {
		out[s.From] += s.Amount
		out[s.To] -= s.Amount
	}
	return out
}

// Transfer is one suggested payment.
type Transfer struct {
	From, To uuid.UUID
	Amount   int64
}

// Simplify returns transfers that bring every balance to zero: the largest debtor pays the largest
// creditor, repeatedly. Each transfer zeroes at least one member, so there are at most n − 1.
// Ties are broken by member id, so the answer is deterministic.
func Simplify(balances map[uuid.UUID]int64) []Transfer {
	type bal struct {
		m uuid.UUID
		v int64
	}
	var debt, cred []bal
	for m, v := range balances {
		switch {
		case v < 0:
			debt = append(debt, bal{m, -v})
		case v > 0:
			cred = append(cred, bal{m, v})
		}
	}
	order := func(a, b bal) int {
		if c := cmp.Compare(b.v, a.v); c != 0 {
			return c
		}
		return cmp.Compare(a.m.String(), b.m.String())
	}
	var out []Transfer
	for len(debt) > 0 && len(cred) > 0 {
		slices.SortFunc(debt, order)
		slices.SortFunc(cred, order)
		d, c := &debt[0], &cred[0]
		amount := min(d.v, c.v)
		out = append(out, Transfer{From: d.m, To: c.m, Amount: amount})
		d.v -= amount
		c.v -= amount
		if d.v == 0 {
			debt = debt[1:]
		}
		if c.v == 0 {
			cred = cred[1:]
		}
	}
	return out
}
