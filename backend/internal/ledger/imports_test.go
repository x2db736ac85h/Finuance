package ledger_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/ingest"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// identities is the set of (fingerprint, occurrence) of the valid staged rows of the given files:
// what the account must contain after importing all of them (the max rule, PLAN §4.4).
func identities(t *testing.T, files ...string) map[[36]byte]bool {
	t.Helper()
	out := map[[36]byte]bool{}
	for _, f := range files {
		rows := staged(t, f, fixture(t, f), nil).Rows
		for i := range rows {
			r := &rows[i]
			if r.Error != "" {
				continue
			}
			var k [36]byte
			copy(k[:], r.Fingerprint[:])
			k[32], k[33] = byte(r.Occurrence), byte(r.Occurrence>>8)
			out[k] = true
		}
	}
	return out
}

const (
	fileA = "synthetic/overlap/A_jan-mar.csv"
	fileB = "synthetic/overlap/B_mar-may.csv"
	fileC = "synthetic/overlap/C_q1_again.csv"
)

func TestCommitOverlapAndDelete(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	a := e.account(u, "Pekao")
	impA, resA := e.importFile(u, a.ID, fileA)
	wantA := len(identities(t, fileA))
	if resA.New != wantA || resA.Duplicates != 0 || e.countTransactions(u, a.ID) != wantA {
		t.Fatalf("A: %+v, %d transactions, want %d new", resA, e.countTransactions(u, a.ID), wantA)
	}
	impB, resB := e.importFile(u, a.ID, fileB)
	_, resC := e.importFile(u, a.ID, fileC)
	all := len(identities(t, fileA, fileB, fileC))
	if got := e.countTransactions(u, a.ID); got != all {
		t.Fatalf("after A, B, C: %d transactions, want %d (one per distinct identity)", got, all)
	}
	if resB.Duplicates == 0 || resC.Duplicates == 0 {
		t.Errorf("overlapping files reported no duplicates: B %+v, C %+v", resB, resC)
	}
	if staged := int64(len(staged(t, fileB, fixture(t, fileB), nil).Rows)); int64(resB.New)+resB.Duplicates+resB.Errors != staged {
		t.Errorf("B stats %+v don't add up to %d rows", resB, staged)
	}
	got, err := e.l.GetImport(e.ctx, u, impB.ID)
	if err != nil || got.Status != ledger.StatusCommitted || got.CommittedAt == nil || !got.CommittedAt.Equal(clock.T) {
		t.Errorf("B after commit = %+v, %v", got, err)
	}
	// Committing again is a state error.
	_, err = e.l.CommitImport(e.ctx, u, impB.ID)
	isErr(t, err, ledger.ErrState)

	// Deleting A removes only what B and C don't also contain.
	if _, err := e.l.DeleteImport(e.ctx, u, impA.ID); err != nil {
		t.Fatal(err)
	}
	if got, want := e.countTransactions(u, a.ID), len(identities(t, fileB, fileC)); got != want {
		t.Errorf("after deleting A: %d transactions, want %d", got, want)
	}
	_, err = e.l.GetImport(e.ctx, u, impA.ID)
	isErr(t, err, ledger.ErrNotFound)
	// Deleting everything leaves nothing.
	list, _ := e.l.ListImports(e.ctx, u, 10)
	for _, i := range list {
		if _, err := e.l.DeleteImport(e.ctx, u, i.ID); err != nil {
			t.Fatal(err)
		}
	}
	if n := e.countTransactions(u, a.ID); n != 0 {
		t.Errorf("after deleting every import: %d transactions", n)
	}
}

// TestIdenticalRowsInOneFileStayTwo: occurrence numbering keeps genuine twins (two coffees).
func TestIdenticalRowsInOneFileStayTwo(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	a := e.account(u, "A")
	twins := []byte("Data;Kontrahent;Kwota\n02.01.2025;KAWIARNIA;-12,00\n02.01.2025;KAWIARNIA;-12,00\n03.01.2025;SKLEP;-5,00\n")
	imp, err := e.l.CreateImport(e.ctx, u, staged(t, "twins.csv", twins, &a.ID))
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.l.CommitImport(e.ctx, u, imp.ID)
	if err != nil || res.New != 3 || e.countTransactions(u, a.ID) != 3 {
		t.Fatalf("commit = %+v, %v; %d transactions", res, err, e.countTransactions(u, a.ID))
	}
	// The same purchases in an overlapping export (a different file) are stored once.
	again := append([]byte("Data;Kontrahent;Kwota\n"), twins[len("Data;Kontrahent;Kwota\n"):]...)
	again = append(again, "04.01.2025;SKLEP;-1,00\n"...)
	imp2, err := e.l.CreateImport(e.ctx, u, staged(t, "twins-later.csv", again, &a.ID))
	if err != nil {
		t.Fatal(err)
	}
	res, err = e.l.CommitImport(e.ctx, u, imp2.ID)
	if err != nil || res.New != 1 || res.Duplicates != 3 || e.countTransactions(u, a.ID) != 4 {
		t.Errorf("overlapping commit = %+v, %v; %d transactions", res, err, e.countTransactions(u, a.ID))
	}
}

// TestSameNameFilesAreDistinctImports: the legacy bucketed by file name; here buckets are imports.
func TestSameNameFilesAreDistinctImports(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	a := e.account(u, "A")
	_, ra := e.importFile(u, a.ID, "synthetic/same-name/a/export.csv")
	_, rb := e.importFile(u, a.ID, "synthetic/same-name/b/export.csv")
	want := len(identities(t, "synthetic/same-name/a/export.csv", "synthetic/same-name/b/export.csv"))
	if got := e.countTransactions(u, a.ID); got != want || ra.New == 0 || rb.New == 0 {
		t.Errorf("same-name files: %d transactions, want %d (a %+v, b %+v)", got, want, ra, rb)
	}
}

func TestCreateImportRules(t *testing.T) {
	e := newEnv(t)
	u, other := e.user(), e.user()
	a := e.account(u, "A")
	content := fixture(t, fileA)
	imp, err := e.l.CreateImport(e.ctx, u, staged(t, "a.csv", content, nil))
	if err != nil {
		t.Fatal(err)
	}
	if imp.Status != ledger.StatusPreviewed || imp.AccountID != nil || len(imp.Headers) == 0 || imp.SizeBytes != len(content) {
		t.Errorf("import = %+v", imp)
	}
	// Identical bytes again: duplicate file, with the existing id.
	_, err = e.l.CreateImport(e.ctx, u, staged(t, "copy.csv", content, &a.ID))
	var dup ledger.ErrDuplicateFile
	if !errors.As(err, &dup) || dup.ExistingID != imp.ID {
		t.Fatalf("duplicate upload: %v", err)
	}
	// Another user may upload the same file.
	if _, err := e.l.CreateImport(e.ctx, other, staged(t, "a.csv", content, nil)); err != nil {
		t.Errorf("other user's upload: %v", err)
	}
	// An account of another user is not accepted (FK checks ignore RLS; the ledger checks).
	otherAcc := e.account(other, "B")
	_, err = e.l.CreateImport(e.ctx, u, staged(t, "b.csv", fixture(t, fileB), &otherAcc.ID))
	isErr(t, err, ledger.ErrNotFound)
	// Without an account the import can't be committed.
	_, err = e.l.CommitImport(e.ctx, u, imp.ID)
	isErr(t, err, ledger.ErrState)
}

func TestImportRowsPageAndErrors(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	a := e.account(u, "A")
	content := []byte("Data;Opis;Kwota\n02.01.2025;ok;-1,00\n31.02.2025;bad date;-2,00\n03.01.2025;bad amount;x\n")
	m := ingest.NewMapping()
	m.BookedOn, m.Description, m.Amount = 0, 1, 2
	in := stagedWith(t, "e.csv", content, &a.ID, &m)
	imp, err := e.l.CreateImport(e.ctx, u, in)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := e.l.ImportRowsPage(e.ctx, u, imp.ID, false, 0, 10)
	if err != nil || len(rows) != 3 {
		t.Fatalf("rows = %v, %v", rows, err)
	}
	if rows[0].Error != "" || rows[0].AmountMinor == nil || *rows[0].AmountMinor != -100 || rows[0].Raw[1] != (ingest.RawField{Header: "Opis", Value: "ok"}) {
		t.Errorf("valid row = %+v", rows[0])
	}
	if rows[1].Error == "" || rows[1].BookedOn != nil || rows[1].AmountMinor != nil {
		t.Errorf("errored row keeps canonical values: %+v", rows[1])
	}
	errs, _ := e.l.ImportRowsPage(e.ctx, u, imp.ID, true, 0, 10)
	if len(errs) != 2 {
		t.Errorf("only errors = %d rows", len(errs))
	}
	res, err := e.l.CommitImport(e.ctx, u, imp.ID)
	if err != nil || res.New != 1 || res.Errors != 2 {
		t.Errorf("commit = %+v, %v", res, err)
	}
	rows, _ = e.l.ImportRowsPage(e.ctx, u, imp.ID, false, 0, 10)
	if rows[0].TransactionID == nil || rows[1].TransactionID != nil {
		t.Errorf("linking: %v, %v", rows[0].TransactionID, rows[1].TransactionID)
	}
	tx, err := e.l.GetTransaction(e.ctx, u, *rows[0].TransactionID)
	if err != nil || tx.AmountMinor != -100 || tx.Description != "ok" || tx.SourceImportID == nil || *tx.SourceImportID != imp.ID ||
		tx.Occurrence != 1 || tx.CategorySource != "none" || len(tx.Raw) != 3 {
		t.Errorf("transaction = %+v, %v", tx, err)
	}
	prov, err := e.l.TransactionProvenance(e.ctx, u, tx.ID)
	if err != nil || len(prov) != 1 || prov[0].ImportID != imp.ID || prov[0].RowIndex != 0 || prov[0].Filename != "e.csv" {
		t.Errorf("provenance = %+v, %v", prov, err)
	}
}

// TestBankCategoryResolvedOnCommit: the commit upserts a category per bank category (PLAN 6.6 step
// 1). An existing category with the same normalised name is reused and keeps its spelling.
func TestBankCategoryResolvedOnCommit(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	a := e.account(u, "A")
	in := staged(t, "c.csv", []byte("Data;Opis;Kwota;Kategoria\n02.01.2025;a;-1,00;Żywność\n03.01.2025;b;-2,00;Inne\n04.01.2025;c;-3,00;\n"), &a.ID)
	if _, err := e.l.EnsureCategories(e.ctx, u, []string{"ŻYWNOŚĆ"}); err != nil {
		t.Fatal(err)
	}
	imp, err := e.l.CreateImport(e.ctx, u, in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.l.CommitImport(e.ctx, u, imp.ID); err != nil {
		t.Fatal(err)
	}
	rows, _ := e.l.ImportRowsPage(e.ctx, u, imp.ID, false, 0, 10)
	food, _ := e.l.GetTransaction(e.ctx, u, *rows[0].TransactionID)
	other, _ := e.l.GetTransaction(e.ctx, u, *rows[1].TransactionID)
	none, _ := e.l.GetTransaction(e.ctx, u, *rows[2].TransactionID)
	if food.CategoryName != "ŻYWNOŚĆ" || food.CategorySource != "bank" || other.CategoryName != "Inne" || other.CategorySource != "bank" ||
		none.CategoryID != nil || none.CategorySource != "none" {
		t.Errorf("food = %q/%s, other = %q/%s, blank = %v/%s", food.CategoryName, food.CategorySource, other.CategoryName,
			other.CategorySource, none.CategoryID, none.CategorySource)
	}
	if cats, _ := e.l.ListCategories(e.ctx, u); len(cats) != 2 {
		t.Errorf("categories = %+v, want ŻYWNOŚĆ and Inne", cats)
	}
}

// TestConcurrentOverlappingCommits commits two overlapping imports at the same time, several times:
// no duplicate transactions, and the stats add up (PLAN 6 tests, "concurrency").
func TestConcurrentOverlappingCommits(t *testing.T) {
	e := newEnv(t)
	for round := range 5 {
		u := e.user()
		a := e.account(u, "A")
		impA, err := e.l.CreateImport(e.ctx, u, staged(t, "a.csv", fixture(t, fileA), &a.ID))
		if err != nil {
			t.Fatal(err)
		}
		impC, err := e.l.CreateImport(e.ctx, u, staged(t, "c.csv", fixture(t, fileC), &a.ID))
		if err != nil {
			t.Fatal(err)
		}
		var res [2]ledger.CommitResult
		var errs [2]error
		var wg sync.WaitGroup
		for i, id := range []uuid.UUID{impA.ID, impC.ID} {
			wg.Go(func() { res[i], errs[i] = e.l.CommitImport(e.ctx, u, id) })
		}
		wg.Wait()
		if errs[0] != nil || errs[1] != nil {
			t.Fatalf("round %d: %v / %v", round, errs[0], errs[1])
		}
		want := len(identities(t, fileA, fileC))
		if got := e.countTransactions(u, a.ID); got != want {
			t.Fatalf("round %d: %d transactions, want %d", round, got, want)
		}
		if res[0].New+res[1].New != want {
			t.Errorf("round %d: new %d + %d ≠ %d transactions", round, res[0].New, res[1].New, want)
		}
		rows := len(staged(t, fileA, fixture(t, fileA), nil).Rows) + len(staged(t, fileC, fixture(t, fileC), nil).Rows)
		if total := int64(res[0].New+res[1].New) + res[0].Duplicates + res[1].Duplicates + res[0].Errors + res[1].Errors; total != int64(rows) {
			t.Errorf("round %d: stats %+v %+v don't add up to %d rows", round, res[0], res[1], rows)
		}
	}
}

// TestOverlapMatchesOracleRows: after A, B, C the account holds exactly the legacy merged rows,
// with their multiplicity (the oracle's default result keeps three 03.01 coffees: two genuine ones
// from A, plus the third that only C has).
func TestOverlapMatchesOracleRows(t *testing.T) {
	var oracle struct {
		Default struct {
			Rows [][]string `json:"rows"` // date, payee, title, amount, category, file
		} `json:"default"`
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "oracle", "dedupe", "overlap-A-B-C.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &oracle); err != nil {
		t.Fatal(err)
	}
	want := map[string]int{}
	for _, r := range oracle.Default.Rows {
		amount := strings.NewReplacer(" ", "", " ", "", ",", "").Replace(r[3]) // "-2 850,00" → "-285000"
		want[strings.Join([]string{r[0], r[1], r[2], amount}, "|")]++
	}

	e := newEnv(t)
	u := e.user()
	a := e.account(u, "Overlap")
	for _, f := range []string{fileA, fileB, fileC} {
		e.importFile(u, a.ID, f)
	}
	got := map[string]int{}
	if err := e.st.WithUser(e.ctx, u, func(_ *db.Queries, tx pgx.Tx) error {
		rows, err := tx.Query(e.ctx, `SELECT to_char(booked_on, 'DD.MM.YYYY') || '|' || payee || '|' || description || '|' || amount_minor
			FROM transactions WHERE user_id = $1 AND account_id = $2`, u, a.ID)
		if err != nil {
			return err
		}
		keys, err := pgx.CollectRows(rows, pgx.RowTo[string])
		for _, k := range keys {
			got[k]++
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("transactions differ from the oracle's merged rows (-oracle +db):\n%s", diff)
	}
	if n := got["03.01.2025|KAWIARNIA BETA|Kawa|-1200"]; n != 3 {
		t.Errorf("%d coffees on 03.01, want 3", n)
	}
}
