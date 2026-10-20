package ledger_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
)

// TestAnnotationSurvivesDeleteAndReimport is PLAN task 4.8: label a transaction, delete its only
// import (orphan cleanup removes the transaction), import the same file again → the label is back.
func TestAnnotationSurvivesDeleteAndReimport(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	a := e.account(u, "A")
	imp, _ := e.importFile(u, a.ID, fileA)
	ids := e.transactionIDs(u, a.ID)
	g, err := e.l.CreateGroup(e.ctx, u, "Italy trip 2025", -1)
	if err != nil {
		t.Fatal(err)
	}
	food, err := e.l.CreateCategory(e.ctx, u, "Jedzenie")
	if err != nil {
		t.Fatal(err)
	}
	n, err := e.l.Annotate(e.ctx, u, ids[:2], ledger.AnnotationPatch{SetGroup: true, GroupID: &g.ID, SetNote: true, Note: "Pizza w Rzymie"})
	if err != nil || n != 2 {
		t.Fatalf("annotate = %d, %v", n, err)
	}
	if _, err := e.l.Annotate(e.ctx, u, ids[1:2], ledger.AnnotationPatch{SetCategory: true, CategoryID: &food.ID}); err != nil {
		t.Fatal(err)
	}
	first, _ := e.l.GetTransaction(e.ctx, u, ids[0])
	second, _ := e.l.GetTransaction(e.ctx, u, ids[1])
	if first.GroupName != "Italy trip 2025" || first.Note != "Pizza w Rzymie" || second.CategoryName != "Jedzenie" || second.CategorySource != "manual" {
		t.Fatalf("after annotate: %+v / %+v", first, second)
	}

	if _, err := e.l.DeleteImport(e.ctx, u, imp.ID); err != nil {
		t.Fatal(err)
	}
	if n := e.countTransactions(u, a.ID); n != 0 {
		t.Fatalf("%d transactions after deleting the only import", n)
	}
	if n := e.count(u, "tx_annotations"); n != 2 {
		t.Fatalf("annotations after delete = %d, want 2 (deleting an import never deletes them)", n)
	}

	_, res := e.importFile(u, a.ID, fileA)
	if res.New != len(ids) {
		t.Fatalf("re-import = %+v", res)
	}
	again := e.transactionIDs(u, a.ID)
	r0, _ := e.l.GetTransaction(e.ctx, u, again[0])
	r1, _ := e.l.GetTransaction(e.ctx, u, again[1])
	r2, _ := e.l.GetTransaction(e.ctx, u, again[2])
	if r0.ID == first.ID {
		t.Fatal("re-import reused the old transaction id; the test proves nothing")
	}
	if r0.GroupName != "Italy trip 2025" || r0.Note != "Pizza w Rzymie" || r1.CategoryName != "Jedzenie" || r1.CategorySource != "manual" ||
		r1.GroupName != "Italy trip 2025" || r2.GroupID != nil || r2.Note != "" {
		t.Errorf("after re-import: %+v / %+v / %+v", r0, r1, r2)
	}
}

// TestClearedAnnotationIsDeleted: removing the label, resetting the category and emptying the note
// leaves nothing, so the annotation row goes.
func TestClearedAnnotationIsDeleted(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	a := e.account(u, "A")
	e.importFile(u, a.ID, fileA)
	id := e.transactionIDs(u, a.ID)[:1]
	g, _ := e.l.CreateGroup(e.ctx, u, "G", -1)
	c, _ := e.l.CreateCategory(e.ctx, u, "C")
	if _, err := e.l.Annotate(e.ctx, u, id, ledger.AnnotationPatch{SetGroup: true, GroupID: &g.ID, SetCategory: true, CategoryID: &c.ID, SetNote: true, Note: "n"}); err != nil {
		t.Fatal(err)
	}
	steps := []ledger.AnnotationPatch{{SetGroup: true}, {SetCategory: true}, {SetNote: true}}
	for i, p := range steps {
		if _, err := e.l.Annotate(e.ctx, u, id, p); err != nil {
			t.Fatal(err)
		}
		want := 1
		if i == len(steps)-1 {
			want = 0
		}
		if n := e.count(u, "tx_annotations"); n != want {
			t.Errorf("after clear step %d: %d annotations, want %d", i, n, want)
		}
	}
	tx, _ := e.l.GetTransaction(e.ctx, u, id[0])
	if tx.GroupID != nil || tx.Note != "" || tx.CategorySource == "manual" {
		t.Errorf("transaction after clearing = %+v", tx)
	}
}

// TestDeletingGroupKeepsTransactions: the label goes, the transactions stay, and annotations that
// held only the label are deleted.
func TestDeletingGroupKeepsTransactions(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	a := e.account(u, "A")
	e.importFile(u, a.ID, fileA)
	ids := e.transactionIDs(u, a.ID)
	g, _ := e.l.CreateGroup(e.ctx, u, "G", -1)
	if _, err := e.l.Annotate(e.ctx, u, ids, ledger.AnnotationPatch{SetGroup: true, GroupID: &g.ID}); err != nil {
		t.Fatal(err)
	}
	if err := e.l.DeleteGroup(e.ctx, u, g.ID); err != nil {
		t.Fatal(err)
	}
	if n := e.countTransactions(u, a.ID); n != len(ids) {
		t.Errorf("transactions after deleting the group: %d, want %d", n, len(ids))
	}
	if n := e.count(u, "tx_annotations"); n != 0 {
		t.Errorf("annotations left: %d", n)
	}
	tx, _ := e.l.GetTransaction(e.ctx, u, ids[0])
	if tx.GroupID != nil {
		t.Errorf("label survived: %+v", tx)
	}
}

func TestAnnotateRules(t *testing.T) {
	e := newEnv(t)
	u, other := e.user(), e.user()
	a := e.account(u, "A")
	e.importFile(u, a.ID, fileA)
	ids := e.transactionIDs(u, a.ID)
	_, err := e.l.Annotate(e.ctx, u, ids, ledger.AnnotationPatch{})
	isErr(t, err, ledger.ErrInvalid)
	_, err = e.l.Annotate(e.ctx, u, make([]uuid.UUID, ledger.MaxBulkIDs+1), ledger.AnnotationPatch{SetNote: true})
	isErr(t, err, ledger.ErrInvalid)
	_, err = e.l.Annotate(e.ctx, u, ids, ledger.AnnotationPatch{SetNote: true, Note: strings.Repeat("x", 2001)})
	isErr(t, err, ledger.ErrInvalid)
	// Another user's group or category is rejected, even though the foreign key would accept it.
	og, _ := e.l.CreateGroup(e.ctx, other, "Theirs", -1)
	oc, _ := e.l.CreateCategory(e.ctx, other, "Theirs")
	_, err = e.l.Annotate(e.ctx, u, ids, ledger.AnnotationPatch{SetGroup: true, GroupID: &og.ID})
	isErr(t, err, ledger.ErrNotFound)
	_, err = e.l.Annotate(e.ctx, u, ids, ledger.AnnotationPatch{SetCategory: true, CategoryID: &oc.ID})
	isErr(t, err, ledger.ErrNotFound)
	// Another user can't annotate my transactions: nothing changes.
	n, err := e.l.Annotate(e.ctx, other, ids, ledger.AnnotationPatch{SetNote: true, Note: "pwned"})
	if err != nil || n != 0 {
		t.Errorf("other user's annotate = %d, %v", n, err)
	}
	if tx, _ := e.l.GetTransaction(e.ctx, u, ids[0]); tx.Note != "" {
		t.Errorf("note = %q", tx.Note)
	}
}
