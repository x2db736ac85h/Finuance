package rules_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/rules"
)

func (e *env) categoryNamed(name string) uuid.UUID {
	e.t.Helper()
	cats, err := e.l.ListCategories(e.ctx, e.u)
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

func (e *env) label(tx, cat uuid.UUID) {
	e.t.Helper()
	if _, err := e.l.Annotate(e.ctx, e.u, []uuid.UUID{tx}, ledger.AnnotationPatch{SetCategory: true, CategoryID: &cat}); err != nil {
		e.t.Fatal(err)
	}
}

// wantRows checks category and source of each payee/amount key.
func (e *env) wantRows(when string, want map[string][2]string) {
	e.t.Helper()
	got := e.rows()
	for k, w := range want {
		if g := got[k]; g.category != w[0] || g.source != w[1] {
			e.t.Errorf("%s: %s is %q (%s), want %q (%s)", when, k, g.category, g.source, w[0], w[1])
		}
	}
}

// TestCategoryArchive (Phase 26, U7): an archived category behaves as deleted, in every way a
// category reaches a transaction (the bank's name, a rule, a label set by hand) and for its
// budget; unarchiving puts back what nobody changed in between, and nothing else.
func TestCategoryArchive(t *testing.T) {
	e := newEnv(t)
	acc := e.account("Konto")
	e.importInto(acc, "jan",
		"02.01.2025;BAR MLECZNY;obiad;-12,00;Restauracje\n"+
			"03.01.2025;ZABKA;zakupy;-30,00;Zakupy\n"+
			"04.01.2025;PIZZERIA;pizza;-40,00;\n"+
			"05.01.2025;KEBAB;kebab;-20,00;\n"+
			"06.01.2025;SUSHI;sushi;-50,00;\n")
	food, shop := e.categoryNamed("Restauracje"), e.categoryNamed("Zakupy")
	rule := e.rule("Pizza", rules.Match{Search: "pizzeria"}, &food, false)
	rows := e.rows()
	e.label(rows["KEBAB/-2000"].id, food)
	e.label(rows["SUSHI/-5000"].id, food)
	if err := e.l.SetBudget(e.ctx, e.u, food, 50000); err != nil {
		t.Fatal(err)
	}
	e.reclassify()
	before := map[string][2]string{
		"BAR MLECZNY/-1200": {"Restauracje", "bank"},
		"ZABKA/-3000":       {"Zakupy", "bank"},
		"PIZZERIA/-4000":    {"Restauracje", "rule"},
		"KEBAB/-2000":       {"Restauracje", "manual"},
		"SUSHI/-5000":       {"Restauracje", "manual"},
	}
	e.wantRows("before", before)

	if err := e.l.ArchiveCategory(e.ctx, e.u, food); err != nil {
		t.Fatal(err)
	}
	gone := map[string][2]string{
		"BAR MLECZNY/-1200": {"", "none"},
		"ZABKA/-3000":       {"Zakupy", "bank"},
		"PIZZERIA/-4000":    {"", "none"},
		"KEBAB/-2000":       {"", "none"},
		"SUSHI/-5000":       {"", "none"},
	}
	e.wantRows("archived, before the reclassification", gone)
	e.reclassify()
	e.wantRows("archived", gone)
	for _, c := range must(e.l.ListCategories(e.ctx, e.u)) {
		if c.ID == food {
			t.Error("the archived category is still listed")
		}
	}
	for _, s := range must(e.l.CategoryStats(e.ctx, e.u)) {
		if s.ID == food {
			t.Error("the archived category still has statistics")
		}
	}
	if b := must(e.l.ListBudgets(e.ctx, e.u)); len(b) != 0 {
		t.Errorf("budgets after archiving = %+v", b)
	}
	if r := must(e.r.List(e.ctx, e.u)); len(r) != 1 || r[0].SetCategoryID != nil {
		t.Errorf("the rule after archiving = %+v", r)
	}
	arch := must(e.l.ArchivedCategories(e.ctx, e.u))
	if len(arch) != 1 || arch[0].ID != food || arch[0].Labels != 2 || arch[0].Rules != 1 || arch[0].Budgets != 1 {
		t.Fatalf("archived = %+v, want Restauracje with 2 labels, 1 rule, 1 budget", arch)
	}
	// nothing can point at it: it answers like a category that does not exist
	if err := e.l.SetBudget(e.ctx, e.u, food, 100); !errors.Is(err, ledger.ErrInvalid) && !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("a budget for an archived category: %v", err)
	}
	if _, err := e.l.Annotate(e.ctx, e.u, []uuid.UUID{rows["KEBAB/-2000"].id}, ledger.AnnotationPatch{SetCategory: true, CategoryID: &food}); err == nil {
		t.Error("a label with an archived category was accepted")
	}
	if _, err := e.r.Update(e.ctx, e.u, rule.ID, rules.Input{Name: "Pizza", Enabled: true, Match: rule.Match, SetCategoryID: &food}, noop); err == nil {
		t.Error("a rule setting an archived category was accepted")
	}
	if _, err := e.l.RenameCategory(e.ctx, e.u, food, "Inna"); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("renaming an archived category: %v", err)
	}
	if err := e.l.ArchiveCategory(e.ctx, e.u, food); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("archiving twice: %v", err)
	}
	// a later statement's rows of that bank name stay uncategorised
	e.importInto(acc, "feb", "02.02.2025;BISTRO;obiad;-15,00;Restauracje\n")
	e.wantRows("an import while archived", map[string][2]string{"BISTRO/-1500": {"", "none"}})

	// in between, one label is set to something else by hand
	e.label(rows["SUSHI/-5000"].id, shop)

	if c, err := e.l.UnarchiveCategory(e.ctx, e.u, food); err != nil || c.Name != "Restauracje" {
		t.Fatalf("unarchive = %+v, %v", c, err)
	}
	e.reclassify()
	before["SUSHI/-5000"] = [2]string{"Zakupy", "manual"} // changed since: left alone
	before["BISTRO/-1500"] = [2]string{"Restauracje", "bank"}
	e.wantRows("unarchived", before)
	if b := must(e.l.ListBudgets(e.ctx, e.u)); len(b) != 1 || b[0].CategoryID != food || b[0].AmountMinor != 50000 {
		t.Errorf("budgets after unarchiving = %+v", b)
	}
	if r := must(e.r.List(e.ctx, e.u)); len(r) != 1 || r[0].SetCategoryID == nil || *r[0].SetCategoryID != food {
		t.Errorf("the rule after unarchiving = %+v", r)
	}
	if a := must(e.l.ArchivedCategories(e.ctx, e.u)); len(a) != 0 {
		t.Errorf("archived after unarchiving = %+v", a)
	}
	if _, err := e.l.UnarchiveCategory(e.ctx, e.u, food); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("unarchiving a category in use: %v", err)
	}
}

// TestCategoryArchiveName: an archived category's name is free; while a category in use has it,
// the archived one can't come back, and it can be deleted for good.
func TestCategoryArchiveName(t *testing.T) {
	e := newEnv(t)
	old := e.category("Kawa")
	if err := e.l.ArchiveCategory(e.ctx, e.u, old); err != nil {
		t.Fatal(err)
	}
	fresh := e.category("kawa") // the same name after normalisation
	if _, err := e.l.UnarchiveCategory(e.ctx, e.u, old); !errors.Is(err, ledger.ErrConflict) {
		t.Fatalf("unarchiving next to a category of that name: %v", err)
	}
	if err := e.l.DeleteCategory(e.ctx, e.u, old); err != nil {
		t.Fatal(err)
	}
	if a := must(e.l.ArchivedCategories(e.ctx, e.u)); len(a) != 0 {
		t.Errorf("archived after deleting for good = %+v", a)
	}
	if c := must(e.l.ListCategories(e.ctx, e.u)); len(c) != 1 || c[0].ID != fresh {
		t.Errorf("categories = %+v, want the new one only", c)
	}

	// another user can do none of it
	other := newEnv(t)
	mine := e.category("Herbata")
	if err := other.l.ArchiveCategory(other.ctx, other.u, mine); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("another user's archive: %v", err)
	}
	if err := e.l.ArchiveCategory(e.ctx, e.u, mine); err != nil {
		t.Fatal(err)
	}
	if _, err := other.l.UnarchiveCategory(other.ctx, other.u, mine); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("another user's unarchive: %v", err)
	}
	if a := must(other.l.ArchivedCategories(other.ctx, other.u)); len(a) != 0 {
		t.Errorf("another user sees %+v", a)
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
