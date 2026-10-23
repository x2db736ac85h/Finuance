package ledger_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/investments"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

func TestInvestments(t *testing.T) {
	e := newEnv(t)
	u, other := e.user(), e.user()
	d, err := e.l.Investments(e.ctx, u)
	if err != nil || d.Version != 0 || d.SwrBp != investments.DefaultSwrBp || d.RealReturnBp != investments.DefaultRealReturnBp || len(d.Positions) != 0 {
		t.Fatalf("empty document = %+v, %v", d, err)
	}

	a, b := uuid.New(), uuid.New()
	d.PPK = investments.PPK{EmployeeMinor: 1, EmployerMinor: 2, StateMinor: 3, ValueMinor: 7}
	d.SwrBp, d.RealReturnBp = 350, -100
	d.Positions = []investments.Position{{ID: a, Name: " Bonds ", Type: "bond", InvestedMinor: 100, ValueMinor: 110, AnnualIncomeMinor: 6},
		{ID: b, Name: "ETF", InvestedMinor: 50, ValueMinor: 40}}
	saved, err := e.l.SaveInvestments(e.ctx, u, d)
	if err != nil || saved.Version != 1 {
		t.Fatalf("first save = %+v, %v", saved, err)
	}
	got, _ := e.l.Investments(e.ctx, u)
	if got.Version != 1 || got.PPK != d.PPK || got.SwrBp != 350 || got.RealReturnBp != -100 || len(got.Positions) != 2 ||
		got.Positions[0].ID != a || got.Positions[0].Name != "Bonds" || got.Positions[1].ID != b {
		t.Errorf("stored = %+v", got)
	}

	// Stale: the old version again carries the current document.
	_, err = e.l.SaveInvestments(e.ctx, u, d)
	var stale *ledger.StaleInvestmentsError
	if !errors.As(err, &stale) || !errors.Is(err, platform.ErrStaleVersion) || stale.Current.Version != 1 || len(stale.Current.Positions) != 2 {
		t.Fatalf("stale save = %v", err)
	}

	// Replacing drops removed positions and keeps the new order.
	got.Positions = []investments.Position{got.Positions[1]}
	if s, err := e.l.SaveInvestments(e.ctx, u, got); err != nil || s.Version != 2 {
		t.Fatalf("second save = %+v, %v", s, err)
	}
	if g, _ := e.l.Investments(e.ctx, u); len(g.Positions) != 1 || g.Positions[0].ID != b {
		t.Errorf("after removing a position = %+v", g)
	}

	// Invalid documents are rejected before the database.
	bad := got
	bad.Version = 2
	bad.SwrBp = 10
	var ve *platform.ValidationError
	if _, err := e.l.SaveInvestments(e.ctx, u, bad); !errors.As(err, &ve) {
		t.Errorf("invalid save = %v", err)
	}

	// The other user has their own document; reusing my position id is a conflict and leaves mine.
	if g, _ := e.l.Investments(e.ctx, other); g.Version != 0 || len(g.Positions) != 0 {
		t.Errorf("other user's document = %+v", g)
	}
	od := investments.Empty()
	od.Positions = []investments.Position{{ID: b, Name: "Mine now"}}
	_, err = e.l.SaveInvestments(e.ctx, other, od)
	isErr(t, err, ledger.ErrConflict)
	if g, _ := e.l.Investments(e.ctx, u); len(g.Positions) != 1 || g.Positions[0].Name != "ETF" {
		t.Errorf("my document after the conflict = %+v", g)
	}
}
