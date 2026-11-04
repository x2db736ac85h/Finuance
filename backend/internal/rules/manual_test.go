package rules_test

import (
	"testing"

	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
	"github.com/x2db736ac85h/finuance/backend/internal/rules"
)

// TestRulesAndTransfersOnManualRows (ADR 0029, decided 2026-11-03): rules categorise typed cash
// expenses like imported rows, a category picked by hand wins, and an ATM withdrawal pairs with
// the cash it put in the wallet.
func TestRulesAndTransfersOnManualRows(t *testing.T) {
	e := newEnv(t)
	bank := e.account("Konto")
	wallet, err := e.l.CreateAccount(e.ctx, e.u, ledger.AccountInput{Name: "Portfel", Kind: ledger.KindCash})
	if err != nil {
		t.Fatal(err)
	}
	e.importInto(bank, "atm", "03.01.2025;BANKOMAT;Wypłata;-200,00;\n")
	add := func(day int, amount int64, payee string, category *string) {
		t.Helper()
		if _, err := e.l.AddManual(e.ctx, e.u, ledger.ManualInput{AccountID: wallet.ID, BookedOn: platform.Date{Y: 2025, M: 1, D: day},
			AmountMinor: amount, Payee: payee, CategoryName: category}); err != nil {
			t.Fatal(err)
		}
	}
	mine := "Moje"
	add(3, 20000, "Wypłata z bankomatu", nil)
	add(4, -1250, "BIEDRONKA", nil)
	add(5, -900, "BIEDRONKA", &mine)
	groceries := e.category("Zakupy")
	e.rule("Biedronka", rules.Match{Filters: []query.Filter{{Column: "payee", Cond: "biedronka"}}}, &groceries, false)
	if res := e.reclassify(); res.Pairs != 1 {
		t.Errorf("pairs = %d, want 1", res.Pairs)
	}
	rows := e.rows()
	want(t, rows["BIEDRONKA/-1250"], "Zakupy", "rule")
	want(t, rows["BIEDRONKA/-900"], "Moje", "manual")
	out, in := rows["BANKOMAT/-20000"], rows["Wypłata z bankomatu/20000"]
	if !out.transfer || !in.transfer || in.peer == nil || *in.peer != out.id {
		t.Errorf("the withdrawal and the cash top-up aren't a pair: %+v %+v", out, in)
	}
}
