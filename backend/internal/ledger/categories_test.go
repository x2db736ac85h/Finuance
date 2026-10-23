package ledger_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

const catCSV = "Data operacji;Kontrahent;Kwota;Kategoria\n" +
	"02.01.2025;BAR MLECZNY;-12,00;Restauracje\n" +
	"03.01.2025;ZABKA;-30,00;Zakupy spożywcze\n"

// commitCSV stages and commits content into the account; it returns the import.
func (e *env) commitCSV(u, acc uuid.UUID, name, content string) ledger.Import {
	e.t.Helper()
	imp, err := e.l.CreateImport(e.ctx, u, staged(e.t, name, []byte(content), &acc))
	if err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.l.CommitImport(e.ctx, u, imp.ID); err != nil {
		e.t.Fatal(err)
	}
	return imp
}

// categoryOf returns each payee's category name and source.
func (e *env) categoryOf(u uuid.UUID) map[string][2]string {
	e.t.Helper()
	out := map[string][2]string{}
	if err := e.st.WithUser(e.ctx, u, func(_ *db.Queries, tx pgx.Tx) error {
		rows, err := tx.Query(e.ctx, `SELECT t.payee, coalesce(c.name, ''), t.category_source FROM transactions t
			LEFT JOIN categories c ON c.id = t.category_id WHERE t.user_id = $1`, u)
		if err != nil {
			return err
		}
		for rows.Next() {
			var p, c, s string
			if err := rows.Scan(&p, &c, &s); err != nil {
				return err
			}
			out[p] = [2]string{c, s}
		}
		return rows.Err()
	}); err != nil {
		e.t.Fatal(err)
	}
	return out
}

func (e *env) categoryID(u uuid.UUID, name string) uuid.UUID {
	e.t.Helper()
	cats, err := e.l.ListCategories(e.ctx, u)
	if err != nil {
		e.t.Fatal(err)
	}
	for _, c := range cats {
		if c.Name == name {
			return c.ID
		}
	}
	e.t.Fatalf("no category %q in %+v", name, cats)
	return uuid.Nil
}

func wantCategory(t *testing.T, got map[string][2]string, payee, name, source string) {
	t.Helper()
	if g := got[payee]; g != [2]string{name, source} {
		t.Errorf("%s: category %q (%s), want %q (%s)", payee, g[0], g[1], name, source)
	}
}

// TestCategoryRenameSurvivesReimport: a renamed bank category keeps its transactions, and the
// bank's rows of the old name come to it again after a delete + re-import.
func TestCategoryRenameSurvivesReimport(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	acc := e.account(u, "Konto")
	imp := e.commitCSV(u, acc.ID, "jan.csv", catCSV)
	food := e.categoryID(u, "Restauracje")
	if _, err := e.l.RenameCategory(e.ctx, u, food, "Jedzenie na mieście"); err != nil {
		t.Fatal(err)
	}
	wantCategory(t, e.categoryOf(u), "BAR MLECZNY", "Jedzenie na mieście", "bank")
	if _, err := e.l.DeleteImport(e.ctx, u, imp.ID); err != nil {
		t.Fatal(err)
	}
	e.commitCSV(u, acc.ID, "jan.csv", catCSV)
	wantCategory(t, e.categoryOf(u), "BAR MLECZNY", "Jedzenie na mieście", "bank")
	if n := len(must(e.l.ListCategories(e.ctx, u))); n != 2 {
		t.Errorf("%d categories after re-import, want 2 (no new Restauracje)", n)
	}
	// another category can't take the name; renaming to the old name takes the alias back
	_, err := e.l.RenameCategory(e.ctx, u, e.categoryID(u, "Zakupy spożywcze"), "jedzenie na miescie")
	isErr(t, err, ledger.ErrConflict)
	if _, err := e.l.RenameCategory(e.ctx, u, food, "Restauracje"); err != nil {
		t.Fatal(err)
	}
	stats := must(e.l.CategoryStats(e.ctx, u))
	for _, s := range stats {
		if s.Name == "Restauracje" && (s.TxCount != 1 || len(s.Aliases) != 1 || s.Aliases[0] != "jedzenie na miescie") {
			t.Errorf("stats after renaming back: %+v (the alias of its own name went, the old one stays)", s)
		}
	}
}

// TestCategoryMerge moves transactions, manual categories, rules' targets, the budget and the bank
// name, and a re-import lands in the target.
func TestCategoryMerge(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	acc := e.account(u, "Konto")
	imp := e.commitCSV(u, acc.ID, "jan.csv", catCSV)
	from, into := e.categoryID(u, "Restauracje"), e.categoryID(u, "Zakupy spożywcze")
	if err := e.l.SetBudget(e.ctx, u, from, 50000); err != nil {
		t.Fatal(err)
	}
	moved, err := e.l.MergeCategory(e.ctx, u, from, into)
	if err != nil || moved != 1 {
		t.Fatalf("merge = %d, %v", moved, err)
	}
	got := e.categoryOf(u)
	wantCategory(t, got, "BAR MLECZNY", "Zakupy spożywcze", "bank")
	if b := must(e.l.ListBudgets(e.ctx, u)); len(b) != 1 || b[0].CategoryID != into || b[0].AmountMinor != 50000 {
		t.Errorf("budgets after merge = %+v, want the merged one on the target", b)
	}
	if _, err := e.l.DeleteImport(e.ctx, u, imp.ID); err != nil {
		t.Fatal(err)
	}
	e.commitCSV(u, acc.ID, "jan.csv", catCSV)
	wantCategory(t, e.categoryOf(u), "BAR MLECZNY", "Zakupy spożywcze", "bank")
	if n := len(must(e.l.ListCategories(e.ctx, u))); n != 1 {
		t.Errorf("%d categories after re-import, want 1", n)
	}
	// EnsureCategories (the inspector's typed name, legacy budgets) follows the alias too
	ids := must(e.l.EnsureCategories(e.ctx, u, []string{"Restauracje"}))
	if ids["restauracje"] != into {
		t.Errorf("ensure Restauracje = %v, want the merge target", ids)
	}
	_, err = e.l.MergeCategory(e.ctx, u, into, into)
	isErr(t, err, ledger.ErrInvalid)
}

// TestCategoryDelete: transactions become uncategorised (bank and manual alike), and the bank name
// stays uncategorised on re-import until a category of that name is created again.
func TestCategoryDelete(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	acc := e.account(u, "Konto")
	imp := e.commitCSV(u, acc.ID, "jan.csv", catCSV)
	food := e.categoryID(u, "Restauracje")
	ids := e.transactionIDs(u, acc.ID)
	if _, err := e.l.Annotate(e.ctx, u, ids[1:], ledger.AnnotationPatch{SetCategory: true, CategoryID: &food}); err != nil {
		t.Fatal(err)
	}
	if err := e.l.DeleteCategory(e.ctx, u, food); err != nil {
		t.Fatal(err)
	}
	got := e.categoryOf(u)
	wantCategory(t, got, "BAR MLECZNY", "", "none")
	wantCategory(t, got, "ZABKA", "", "none") // its manual category is gone too
	if n := e.count(u, "tx_annotations"); n != 0 {
		t.Errorf("%d annotations left (they held only the deleted category)", n)
	}
	if _, err := e.l.DeleteImport(e.ctx, u, imp.ID); err != nil {
		t.Fatal(err)
	}
	e.commitCSV(u, acc.ID, "jan.csv", catCSV)
	got = e.categoryOf(u)
	wantCategory(t, got, "BAR MLECZNY", "", "none")
	wantCategory(t, got, "ZABKA", "Zakupy spożywcze", "bank")
	// typing the name in the inspector asks for the category again
	tx := e.transactionIDs(u, acc.ID)[0]
	name := "Restauracje"
	if _, err := e.l.UpdateTransaction(e.ctx, u, tx, ledger.TransactionPatch{CategoryName: &name}); err != nil {
		t.Fatal(err)
	}
	wantCategory(t, e.categoryOf(u), "BAR MLECZNY", "Restauracje", "manual")
	// and from now on the bank's rows of that name land in it again
	e.commitCSV(u, acc.ID, "feb.csv", "Data operacji;Kontrahent;Kwota;Kategoria\n05.02.2025;PIZZERIA;-40,00;Restauracje\n")
	wantCategory(t, e.categoryOf(u), "PIZZERIA", "Restauracje", "bank")
}

// TestCategoryTenancy: another user's category can't be renamed, merged or deleted.
func TestCategoryTenancy(t *testing.T) {
	e := newEnv(t)
	u, other := e.user(), e.user()
	c, err := e.l.CreateCategory(e.ctx, u, "Moja")
	if err != nil {
		t.Fatal(err)
	}
	mine, err := e.l.CreateCategory(e.ctx, other, "Cudza")
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.l.RenameCategory(e.ctx, other, c.ID, "x")
	isErr(t, err, ledger.ErrNotFound)
	_, err = e.l.MergeCategory(e.ctx, other, c.ID, mine.ID)
	isErr(t, err, ledger.ErrNotFound)
	_, err = e.l.MergeCategory(e.ctx, other, mine.ID, c.ID)
	isErr(t, err, ledger.ErrNotFound)
	isErr(t, e.l.DeleteCategory(e.ctx, other, c.ID), ledger.ErrNotFound)
	if s := must(e.l.CategoryStats(e.ctx, other)); len(s) != 1 || s[0].Name != "Cudza" {
		t.Errorf("other user's stats = %+v", s)
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
