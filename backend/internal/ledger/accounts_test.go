package ledger_test

import (
	"testing"

	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
)

func TestAccounts(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	a, err := e.l.CreateAccount(e.ctx, u, ledger.AccountInput{Name: "  Konto Pekao ", NumberHint: "1234", OpeningBalanceMinor: 100})
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "Konto Pekao" || a.Currency != "PLN" || a.NumberHint != "1234" {
		t.Errorf("created = %+v", a)
	}
	// Same name after normalisation (case, accents, spacing) is a conflict.
	_, err = e.l.CreateAccount(e.ctx, u, ledger.AccountInput{Name: "KONTO  pekao"})
	isErr(t, err, ledger.ErrConflict)
	for _, bad := range []ledger.AccountInput{{Name: ""}, {Name: "x", Currency: "XYZ"}, {Name: "x", NumberHint: "12a4"}} {
		_, err := e.l.CreateAccount(e.ctx, u, bad)
		isErr(t, err, ledger.ErrInvalid)
	}
	e.account(u, "Ąbc konto")
	e.account(u, "Zeta")
	list, err := e.l.ListAccounts(e.ctx, u)
	if err != nil || len(list) != 3 || list[0].Name != "Ąbc konto" || list[2].Name != "Zeta" {
		t.Fatalf("list (natural Polish order) = %+v, %v", list, err)
	}
	up, err := e.l.UpdateAccount(e.ctx, u, a.ID, ledger.AccountInput{Name: "Pekao", Currency: "EUR", Archived: true})
	if err != nil || up.Currency != "EUR" || up.ArchivedAt == nil || !up.ArchivedAt.Equal(clock.T) || up.NumberHint != "" {
		t.Errorf("update = %+v, %v", up, err)
	}
	up, err = e.l.UpdateAccount(e.ctx, u, a.ID, ledger.AccountInput{Name: "Pekao", NumberHint: "9876"})
	if err != nil || up.ArchivedAt != nil {
		t.Errorf("unarchive = %+v, %v", up, err)
	}
	s, err := e.l.SuggestAccounts(e.ctx, u, "'61 1090 1014 0000 0712 1981 9876")
	if err != nil || len(s) != 1 || s[0].ID != a.ID {
		t.Errorf("suggest = %+v, %v", s, err)
	}
	if s, _ := e.l.SuggestAccounts(e.ctx, u, "98"); s != nil {
		t.Errorf("suggest with too few digits = %+v", s)
	}
	// Another user sees nothing of it.
	other := e.user()
	_, err = e.l.GetAccount(e.ctx, other, a.ID)
	isErr(t, err, ledger.ErrNotFound)
	isErr(t, e.l.DeleteAccount(e.ctx, other, a.ID), ledger.ErrNotFound)
	if err := e.l.DeleteAccount(e.ctx, u, a.ID); err != nil {
		t.Fatal(err)
	}
	_, err = e.l.GetAccount(e.ctx, u, a.ID)
	isErr(t, err, ledger.ErrNotFound)
}

// TestDeleteAccountCascades: the account takes its imports, transactions and annotations with it.
func TestDeleteAccountCascades(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	a := e.account(u, "A")
	imp, _ := e.importFile(u, a.ID, "synthetic/overlap/A_jan-mar.csv")
	ids := e.transactionIDs(u, a.ID)
	g, err := e.l.CreateGroup(e.ctx, u, "Trip", -1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.l.Annotate(e.ctx, u, ids[:1], ledger.AnnotationPatch{SetGroup: true, GroupID: &g.ID}); err != nil {
		t.Fatal(err)
	}
	if err := e.l.DeleteAccount(e.ctx, u, a.ID); err != nil {
		t.Fatal(err)
	}
	_, err = e.l.GetImport(e.ctx, u, imp.ID)
	isErr(t, err, ledger.ErrNotFound)
	if n := e.countTransactions(u, a.ID); n != 0 {
		t.Errorf("%d transactions survived", n)
	}
	if n := e.count(u, "tx_annotations"); n != 0 {
		t.Errorf("%d annotations survived", n)
	}
	if gs, _ := e.l.ListGroups(e.ctx, u); len(gs) != 1 {
		t.Errorf("groups belong to the user, not the account: %v", gs)
	}
}
