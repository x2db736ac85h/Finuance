package ledger_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

func (e *env) cash(u uuid.UUID, name string) ledger.Account {
	e.t.Helper()
	a, err := e.l.CreateAccount(e.ctx, u, ledger.AccountInput{Name: name, Kind: ledger.KindCash})
	if err != nil {
		e.t.Fatal(err)
	}
	return a
}

// TestManualTransactions (ADR 0029): typed expenses live in cash accounts only, can be changed and
// deleted, and an import's delete or another account's files never touch them.
func TestManualTransactions(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	wallet := e.cash(u, "Portfel")
	if wallet.Kind != ledger.KindCash {
		t.Fatalf("kind = %q", wallet.Kind)
	}
	bank := e.account(u, "Konto")
	if bank.Kind != ledger.KindBank {
		t.Fatalf("default kind = %q", bank.Kind)
	}
	if _, err := e.l.CreateAccount(e.ctx, u, ledger.AccountInput{Name: "X", Kind: "card"}); err == nil {
		t.Error("an unknown kind was accepted")
	}

	day := platform.Date{Y: 2026, M: 10, D: 18}
	food := "Jedzenie"
	id, err := e.l.AddManual(e.ctx, u, ledger.ManualInput{AccountID: wallet.ID, BookedOn: day, AmountMinor: -2450,
		Payee: "  Piekarnia Pod Lipą ", CategoryName: &food, Note: "drożdżówki"})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := e.l.GetTransaction(e.ctx, u, id)
	if err != nil {
		t.Fatal(err)
	}
	if tx.Origin != "manual" || tx.Payee != "Piekarnia Pod Lipą" || tx.AmountMinor != -2450 || tx.Currency != "PLN" ||
		tx.CategoryName != food || tx.CategorySource != "manual" || tx.Note != "drożdżówki" || tx.BookedOn != day {
		t.Errorf("added = %+v", tx)
	}
	var search string
	if err := e.st.WithUser(e.ctx, u, func(_ *db.Queries, x pgx.Tx) error {
		return x.QueryRow(e.ctx, `SELECT search_text FROM transactions WHERE id = $1`, id).Scan(&search)
	}); err != nil {
		t.Fatal(err)
	}
	if search != "18.10.2026\u0001-24,50\u0001piekarnia pod lipa\u0001" {
		t.Errorf("search_text = %q", search)
	}

	// invalid input
	for name, in := range map[string]ledger.ManualInput{
		"no payee":     {AccountID: wallet.ID, BookedOn: day, AmountMinor: -1},
		"zero amount":  {AccountID: wallet.ID, BookedOn: day, AmountMinor: 0, Payee: "x"},
		"bad date":     {AccountID: wallet.ID, BookedOn: platform.Date{Y: 1900, M: 1, D: 1}, AmountMinor: -1, Payee: "x"},
		"bank account": {AccountID: bank.ID, BookedOn: day, AmountMinor: -1, Payee: "x"},
	} {
		if _, err := e.l.AddManual(e.ctx, u, in); err == nil {
			t.Errorf("%s: accepted", name)
		} else {
			isErr(t, err, ledger.ErrInvalid)
		}
	}

	// edit
	amount, payee := int64(-3000), "Piekarnia"
	if err := e.l.EditManual(e.ctx, u, id, ledger.ManualEdit{AmountMinor: &amount, Payee: &payee}); err != nil {
		t.Fatal(err)
	}
	if tx, _ = e.l.GetTransaction(e.ctx, u, id); tx.AmountMinor != -3000 || tx.Payee != "Piekarnia" || tx.CategoryName != food {
		t.Errorf("edited = %+v", tx)
	}

	// files never go into a cash account, and imported rows stay the bank's
	if _, err := e.l.CreateImport(e.ctx, u, staged(t, "a.csv", fixture(t, "synthetic/overlap/A_jan-mar.csv"), &wallet.ID)); err == nil {
		t.Error("an import into a cash account was accepted")
	} else {
		isErr(t, err, ledger.ErrInvalid)
	}
	imp, _ := e.importFile(u, bank.ID, "synthetic/overlap/A_jan-mar.csv")
	imported := e.transactionIDs(u, bank.ID)[0]
	if err := e.l.EditManual(e.ctx, u, imported, ledger.ManualEdit{Payee: &payee}); err == nil {
		t.Error("an imported row was edited")
	} else {
		isErr(t, err, ledger.ErrInvalid)
	}
	if _, err := e.l.DeleteManual(e.ctx, u, imported); err == nil {
		t.Error("an imported row was deleted by hand")
	} else {
		isErr(t, err, ledger.ErrState)
	}
	if _, err := e.l.DeleteImport(e.ctx, u, imp.ID); err != nil {
		t.Fatal(err)
	}
	if n := e.countTransactions(u, wallet.ID); n != 1 {
		t.Errorf("cash rows after deleting an import = %d, want 1", n)
	}

	// another user sees nothing
	other := e.user()
	if _, err := e.l.GetTransaction(e.ctx, other, id); err == nil {
		t.Error("another user read the row")
	}
	if err := e.l.EditManual(e.ctx, other, id, ledger.ManualEdit{Payee: &payee}); err == nil {
		t.Error("another user edited the row")
	}
	if _, err := e.l.DeleteManual(e.ctx, other, id); err == nil {
		t.Error("another user deleted the row")
	}
	if _, err := e.l.AddManual(e.ctx, other, ledger.ManualInput{AccountID: wallet.ID, BookedOn: day, AmountMinor: -1, Payee: "x"}); err == nil {
		t.Error("another user added to the wallet")
	}

	// delete takes the annotation along
	account, err := e.l.DeleteManual(e.ctx, u, id)
	if err != nil || account != wallet.ID {
		t.Fatalf("delete = %v, %v", account, err)
	}
	if n := e.countTransactions(u, wallet.ID); n != 0 {
		t.Errorf("rows after delete = %d", n)
	}
	if n := e.count(u, "tx_annotations"); n != 0 {
		t.Errorf("annotations after delete = %d", n)
	}
}
