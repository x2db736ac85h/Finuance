package httpapi

import (
	"testing"

	"github.com/x2db736ac85h/finuance/backend/internal/money"
)

// TestAmountBoundsHTTP: every amount a request carries is at most money.MaxAbsMinor either way
// (422 beyond it, API-4), and totals of amounts at the bound still answer: the timeline and the
// shared group's balances sum them without overflowing.
func TestAmountBoundsHTTP(t *testing.T) {
	e := newTestEnv(t, nil, false)
	alex := named(t, e, "alex", "Alex")
	const top = int64(money.MaxAbsMinor)

	expect(t, alex.do("POST", "/accounts", map[string]any{"name": "Za dużo", "kind": "cash", "openingBalanceMinor": top + 1}), 422, "validation_failed")
	wallet := alex.do("POST", "/accounts", map[string]any{"name": "Portfel", "kind": "cash"})
	expect(t, wallet, 201, "")
	cash := func(amount int64) map[string]any {
		return map[string]any{"accountId": wallet.body["id"], "bookedOn": "2026-06-02", "amountMinor": amount, "payee": "Dom"}
	}
	expect(t, alex.do("POST", "/transactions", cash(-top-1)), 422, "validation_failed")
	expect(t, alex.do("POST", "/transactions", cash(-top)), 201, "")
	expect(t, alex.do("POST", "/transactions", cash(-top)), 201, "")

	g := alex.do("POST", "/shared-groups", map[string]any{"name": "Dom", "kind": "household"})
	expect(t, g, 201, "")
	gid := g.body["id"].(string)
	expense := func(amount int64) map[string]any {
		return map[string]any{"bookedOn": "2026-06-03", "amountMinor": amount, "payee": "Dom"}
	}
	expect(t, alex.do("POST", "/shared-groups/"+gid+"/expenses", expense(top+1)), 422, "validation_failed")
	expect(t, alex.do("POST", "/shared-groups/"+gid+"/expenses", expense(top)), 201, "")
	expect(t, alex.do("POST", "/shared-groups/"+gid+"/expenses", expense(top)), 201, "")

	tl := alex.do("GET", "/groups/timeline", nil)
	expect(t, tl, 200, "")
	if gs := tl.body["groups"].([]any); len(gs) != 1 || int64(num(gs[0].(map[string]any)["spentMinor"])) != 2*top {
		t.Errorf("timeline = %s", tl.raw)
	}
	expect(t, alex.do("GET", "/shared-groups/"+gid, nil), 200, "")
}
