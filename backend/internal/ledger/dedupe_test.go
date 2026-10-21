package ledger_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

const (
	v1 = "synthetic/dup-versions/v1_jan-feb.csv" // older export: "Bez kategorii"
	v2 = "synthetic/dup-versions/v2_jan-mar.csv" // newer export, the same rows re-labelled
	// The SKLEP ALFA purchase, in both files: "Bez kategorii" in v1, "Artykuły spożywcze" in v2.
	alfaRef = "D000000000000005"
)

// txByRef returns the transactions of the account with this reference.
func (e *env) txByRef(u, account uuid.UUID, ref string) []ledger.Transaction {
	e.t.Helper()
	var ids []uuid.UUID
	if err := e.st.WithUser(e.ctx, u, func(_ *db.Queries, tx pgx.Tx) error {
		rows, err := tx.Query(e.ctx, `SELECT id FROM transactions WHERE user_id = $1 AND account_id = $2 AND reference = $3 ORDER BY split_scope, occurrence`, u, account, ref)
		if err == nil {
			ids, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		}
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	out := make([]ledger.Transaction, len(ids))
	for i, id := range ids {
		t, err := e.l.GetTransaction(e.ctx, u, id)
		if err != nil {
			e.t.Fatal(err)
		}
		out[i] = t
	}
	return out
}

func (e *env) fingerprintOf(u, txID uuid.UUID) []byte {
	e.t.Helper()
	var fp []byte
	if err := e.st.WithUser(e.ctx, u, func(_ *db.Queries, tx pgx.Tx) error {
		return tx.QueryRow(e.ctx, `SELECT fingerprint FROM transactions WHERE user_id = $1 AND id = $2`, u, txID).Scan(&fp)
	}); err != nil {
		e.t.Fatal(err)
	}
	return fp
}

func (e *env) skipped(u, account uuid.UUID) int64 {
	e.t.Helper()
	n, err := e.l.DuplicatesSkipped(e.ctx, u, []uuid.UUID{account})
	if err != nil {
		e.t.Fatal(err)
	}
	return n
}

func TestOverridesPreferKeepBothMerge(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	a := e.account(u, "Pekao")
	imp1, _ := e.importFile(u, a.ID, v1)
	imp2, _ := e.importFile(u, a.ID, v2)
	alfa := e.txByRef(u, a.ID, alfaRef)
	if len(alfa) != 1 || alfa[0].BankCategory != "Bez kategorii" || *alfa[0].SourceImportID != imp1.ID {
		t.Fatalf("default: the earliest import is the source: %+v", alfa)
	}
	before := e.countTransactions(u, a.ID)
	skipped := e.skipped(u, a.ID)
	if skipped == 0 {
		t.Fatal("overlapping versions skipped no duplicates")
	}
	fp := e.fingerprintOf(u, alfa[0].ID)
	// Label it, so we can see annotations survive every step.
	g, _ := e.l.CreateGroup(e.ctx, u, "Shopping", -1)
	if _, err := e.l.Annotate(e.ctx, u, []uuid.UUID{alfa[0].ID}, ledger.AnnotationPatch{SetGroup: true, GroupID: &g.ID}); err != nil {
		t.Fatal(err)
	}

	// "Use this version": v2 becomes the source; the count doesn't change (max rule).
	if err := e.l.SetOverride(e.ctx, u, a.ID, fp, ledger.OverridePrefer, &imp2.ID); err != nil {
		t.Fatal(err)
	}
	alfa = e.txByRef(u, a.ID, alfaRef)
	if len(alfa) != 1 || alfa[0].BankCategory != "Artykuły spożywcze" || *alfa[0].SourceImportID != imp2.ID || alfa[0].GroupName != "Shopping" {
		t.Errorf("prefer v2: %+v", alfa)
	}
	if n := e.countTransactions(u, a.ID); n != before {
		t.Errorf("prefer changed the count: %d → %d", before, n)
	}

	// "Keep both": one transaction per import (sum rule); skipped goes down by one.
	if err := e.l.SetOverride(e.ctx, u, a.ID, fp, ledger.OverrideKeepBoth, nil); err != nil {
		t.Fatal(err)
	}
	alfa = e.txByRef(u, a.ID, alfaRef)
	if len(alfa) != 2 || alfa[0].BankCategory == alfa[1].BankCategory {
		t.Errorf("keep both: %+v", alfa)
	}
	if n := e.countTransactions(u, a.ID); n != before+1 {
		t.Errorf("keep both: %d transactions, want %d", n, before+1)
	}
	if s := e.skipped(u, a.ID); s != skipped-1 {
		t.Errorf("keep both: skipped %d, want %d", s, skipped-1)
	}

	// "Merge again": back to one, from the earliest import, label intact.
	if err := e.l.ClearOverride(e.ctx, u, a.ID, fp); err != nil {
		t.Fatal(err)
	}
	alfa = e.txByRef(u, a.ID, alfaRef)
	if len(alfa) != 1 || alfa[0].BankCategory != "Bez kategorii" || alfa[0].GroupName != "Shopping" {
		t.Errorf("merge again: %+v", alfa)
	}
	if n, s := e.countTransactions(u, a.ID), e.skipped(u, a.ID); n != before || s != skipped {
		t.Errorf("merge again: %d transactions / %d skipped, want %d / %d", n, s, before, skipped)
	}
	isErr(t, e.l.ClearOverride(e.ctx, u, a.ID, fp), ledger.ErrNotFound)
}

// TestDeletingPreferredImportFallsBack: deleting the preferred import removes its override (FK
// cascade) and the transaction takes its values from the remaining import.
func TestDeletingPreferredImportFallsBack(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	a := e.account(u, "Pekao")
	imp1, _ := e.importFile(u, a.ID, v1)
	imp2, _ := e.importFile(u, a.ID, v2)
	alfa := e.txByRef(u, a.ID, alfaRef)
	fp := e.fingerprintOf(u, alfa[0].ID)
	if err := e.l.SetOverride(e.ctx, u, a.ID, fp, ledger.OverridePrefer, &imp1.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.l.SetOverride(e.ctx, u, a.ID, fp, ledger.OverridePrefer, &imp2.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.l.DeleteImport(e.ctx, u, imp2.ID); err != nil {
		t.Fatal(err)
	}
	alfa = e.txByRef(u, a.ID, alfaRef)
	if len(alfa) != 1 || alfa[0].BankCategory != "Bez kategorii" || *alfa[0].SourceImportID != imp1.ID {
		t.Errorf("after deleting the preferred import: %+v", alfa)
	}
	if n := e.count(u, "dedupe_overrides"); n != 0 {
		t.Errorf("override survived its import: %d", n)
	}
	// Deleting the earliest import moves the source of shared rows to v2 … gone too here, so
	// re-import v2 and delete v1: the shared transaction now comes from v2.
	imp2b, _ := e.importFile(u, a.ID, v2)
	if _, err := e.l.DeleteImport(e.ctx, u, imp1.ID); err != nil {
		t.Fatal(err)
	}
	alfa = e.txByRef(u, a.ID, alfaRef)
	if len(alfa) != 1 || *alfa[0].SourceImportID != imp2b.ID || alfa[0].BankCategory != "Artykuły spożywcze" {
		t.Errorf("after deleting the source import: %+v", alfa)
	}
}

func TestSetOverrideRules(t *testing.T) {
	e := newEnv(t)
	u, other := e.user(), e.user()
	a := e.account(u, "A")
	b := e.account(u, "B")
	imp, _ := e.importFile(u, a.ID, v1)
	impB, _ := e.importFile(u, b.ID, v2)
	fp := e.fingerprintOf(u, e.txByRef(u, a.ID, alfaRef)[0].ID)
	isErr(t, e.l.SetOverride(e.ctx, u, a.ID, fp[:5], ledger.OverrideKeepBoth, nil), ledger.ErrInvalid)
	isErr(t, e.l.SetOverride(e.ctx, u, a.ID, fp, ledger.OverridePrefer, nil), ledger.ErrInvalid)
	isErr(t, e.l.SetOverride(e.ctx, u, a.ID, fp, ledger.OverrideKeepBoth, &imp.ID), ledger.ErrInvalid)
	isErr(t, e.l.SetOverride(e.ctx, u, a.ID, fp, "nope", nil), ledger.ErrInvalid)
	// An import of another account can't be preferred.
	isErr(t, e.l.SetOverride(e.ctx, u, a.ID, fp, ledger.OverridePrefer, &impB.ID), ledger.ErrInvalid)
	// Another user can't touch my account.
	isErr(t, e.l.SetOverride(e.ctx, other, a.ID, fp, ledger.OverrideKeepBoth, nil), ledger.ErrNotFound)
}

// TestReprocessKeepsManualEdits: reprocessing a committed import with the same mapping keeps the
// manual category and the note (they live in annotations), and the counts don't change.
func TestReprocessKeepsManualEdits(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	a := e.account(u, "A")
	created, _ := e.importFile(u, a.ID, v1)
	imp, err := e.l.GetImport(e.ctx, u, created.ID)
	if err != nil || imp.CommittedAt == nil {
		t.Fatalf("committed import = %+v, %v", imp, err)
	}
	before := e.countTransactions(u, a.ID)
	alfa := e.txByRef(u, a.ID, alfaRef)[0]
	c, _ := e.l.CreateCategory(e.ctx, u, "Moja kategoria")
	if _, err := e.l.Annotate(e.ctx, u, []uuid.UUID{alfa.ID}, ledger.AnnotationPatch{SetCategory: true, CategoryID: &c.ID, SetNote: true, Note: "ważne"}); err != nil {
		t.Fatal(err)
	}
	st := staged(t, "v1.csv", fixture(t, v1), nil)
	restaged, err := e.l.Restage(e.ctx, u, imp.ID, ledger.Staging{Encoding: st.Encoding, Delimiter: st.Delimiter, Headers: st.Headers,
		Mapping: st.Mapping, Options: st.Options, MappingSource: "user", HeaderSignature: st.HeaderSignature, Rows: st.Rows})
	if err != nil {
		t.Fatal(err)
	}
	if restaged.Status != ledger.StatusCommitting {
		t.Fatalf("status after reprocess restage = %s", restaged.Status)
	}
	if _, err := e.l.CommitImport(e.ctx, u, imp.ID); err != nil {
		t.Fatal(err)
	}
	got := e.txByRef(u, a.ID, alfaRef)
	if len(got) != 1 || got[0].CategoryName != "Moja kategoria" || got[0].CategorySource != "manual" || got[0].Note != "ważne" {
		t.Errorf("after reprocess: %+v", got)
	}
	if n := e.countTransactions(u, a.ID); n != before {
		t.Errorf("reprocess changed the count: %d → %d", before, n)
	}
	again, _ := e.l.GetImport(e.ctx, u, imp.ID)
	if again.Status != ledger.StatusCommitted || again.CommittedAt == nil || !again.CommittedAt.Equal(*imp.CommittedAt) {
		t.Errorf("import after reprocess = %s, committed %v (was %v)", again.Status, again.CommittedAt, imp.CommittedAt)
	}
}
