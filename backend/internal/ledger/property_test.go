package ledger_test

import (
	"bytes"
	"flag"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/ingest"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// The dedupe model property test (PLAN §4.4, §8.3): random sequences of import, delete, reprocess
// and override set/clear over generated overlapping files. After every step:
//
//  1. the incrementally maintained transactions equal what Rematerialize produces from scratch for
//     every fingerprint (ids included: a kept transaction must not be recreated);
//  2. the multiset rule holds, computed independently from the staged rows: per fingerprint, the max
//     over imports of its row count, or the sum under keep_both;
//  3. every valid staged row of a committed import is linked to a transaction with its identity.
//
// Run more with: go test -run TestDedupeModelProperty -count=1 ./internal/ledger/ -args -seeds=50 -steps=60

type poolTx struct {
	date, payee, desc, amount string
}

// A small pool, so files overlap a lot. Two pool entries share date, payee and amount but differ in
// description: different fingerprints that look alike.
var pool = []poolTx{
	{"02.01.2025", "KAWIARNIA", "Kawa", "-12,00"},
	{"02.01.2025", "KAWIARNIA", "Kawa duża", "-12,00"},
	{"03.01.2025", "BIEDRONKA", "Zakupy", "-54,20"},
	{"05.01.2025", "PRACODAWCA", "Wynagrodzenie", "8 500,00"},
	{"07.01.2025", "ORLEN", "Paliwo", "-250,00"},
	{"09.01.2025", "NETFLIX", "Subskrypcja", "-43,00"},
	{"12.01.2025", "APTEKA", "Leki", "-31,99"},
	{"15.01.2025", "WSPÓLNOTA", "Czynsz", "-2 850,00"},
}

var labels = []string{"Jedzenie", "Zakupy spożywcze", "Bez kategorii", ""}

// mappings: 0 = full, 1 = without the category column, 2 = inverted sign (changes fingerprints).
func mappingFor(variant int) (ingest.Mapping, ingest.StageOptions) {
	m := ingest.NewMapping()
	m.BookedOn, m.Payee, m.Description, m.Amount, m.Category = 0, 1, 2, 3, 4
	o := ingest.StageOptions{}
	switch variant {
	case 1:
		m.Category = ingest.NoColumn
	case 2:
		o.InvertSign = true
	}
	return m, o
}

// genFile builds a CSV from a random subset of the pool. Some rows appear twice (genuine twins in
// one export), labels vary between files (re-labelled exports), and a batch column makes every file
// unique (identical bytes would be rejected as a duplicate file).
func genFile(r *rand.Rand, batch int) []byte {
	var b strings.Builder
	b.WriteString("Data;Kontrahent;Opis;Kwota;Kategoria;Partia\n")
	for _, p := range pool {
		if r.IntN(3) == 0 {
			continue
		}
		n := 1
		if r.IntN(5) == 0 {
			n = 2
		}
		for range n {
			fmt.Fprintf(&b, "%s;%s;%s;%s;%s;%d\n", p.date, p.payee, p.desc, p.amount, labels[r.IntN(len(labels))], batch)
		}
	}
	return []byte(b.String())
}

type modelImport struct {
	id      uuid.UUID
	content []byte
	variant int
}

// stage parses content with the given mapping variant.
func stage(t *testing.T, content []byte, variant int, account *uuid.UUID, name string) ledger.NewImport {
	t.Helper()
	m, o := mappingFor(variant)
	in := stagedWith(t, name, content, account, &m)
	if o.InvertSign {
		res, err := ingest.Parse(t.Context(), content, ingest.Options{})
		if err != nil {
			t.Fatal(err)
		}
		in.Rows, _, err = ingest.Stage(t.Context(), res.Table, m, o)
		if err != nil {
			t.Fatal(err)
		}
	}
	return in
}

var (
	modelSeeds = flag.Int("seeds", 6, "dedupe property test: number of random seeds")
	modelSteps = flag.Int("steps", 30, "dedupe property test: operations per seed")
)

func TestDedupeModelProperty(t *testing.T) {
	if testDB == nil {
		t.Skip("integration test: needs Postgres (run without -short)")
	}
	steps := *modelSteps
	for seed := 1; seed <= *modelSeeds; seed++ {
		t.Run(fmt.Sprint("seed", seed), func(t *testing.T) {
			runModel(t, uint64(seed), steps)
		})
	}
}

func runModel(t *testing.T, seed uint64, steps int) {
	e := newEnv(t)
	u := e.user()
	a := e.account(u, "Model")
	r := rand.New(rand.NewPCG(seed, 42))
	var imports []*modelImport
	var log []string
	fail := func(format string, args ...any) {
		t.Helper()
		t.Fatalf("seed %d after %v: %s", seed, log, fmt.Sprintf(format, args...))
	}
	for step := range steps {
		switch op := r.IntN(100); {
		case op < 45 || len(imports) == 0: // import a new file
			content := genFile(r, step)
			in := stage(t, content, 0, &a.ID, fmt.Sprintf("f%d.csv", step))
			imp, err := e.l.CreateImport(e.ctx, u, in)
			if err != nil {
				fail("create import: %v", err)
			}
			if _, err := e.l.CommitImport(e.ctx, u, imp.ID); err != nil {
				fail("commit: %v", err)
			}
			imports = append(imports, &modelImport{id: imp.ID, content: content})
			log = append(log, fmt.Sprintf("import#%d", len(imports)-1))
		case op < 60: // delete a random import
			i := r.IntN(len(imports))
			if _, err := e.l.DeleteImport(e.ctx, u, imports[i].id); err != nil {
				fail("delete: %v", err)
			}
			imports = slices.Delete(imports, i, i+1)
			log = append(log, fmt.Sprintf("delete#%d", i))
		case op < 75: // reprocess with another mapping
			i := r.IntN(len(imports))
			imports[i].variant = r.IntN(3)
			st := stage(t, imports[i].content, imports[i].variant, nil, "re.csv")
			if _, err := e.l.Restage(e.ctx, u, imports[i].id, ledger.Staging{Encoding: st.Encoding, Delimiter: st.Delimiter,
				Headers: st.Headers, Mapping: st.Mapping, Options: st.Options, MappingSource: "user", HeaderSignature: st.HeaderSignature, Rows: st.Rows}); err != nil {
				fail("restage: %v", err)
			}
			if _, err := e.l.CommitImport(e.ctx, u, imports[i].id); err != nil {
				fail("recommit: %v", err)
			}
			log = append(log, fmt.Sprintf("reprocess#%d/v%d", i, imports[i].variant))
		case op < 92: // set an override on a random fingerprint of the account
			fp, holders := randomFingerprint(t, e, u, a.ID, r)
			if fp == nil {
				continue
			}
			if r.IntN(2) == 0 {
				p := holders[r.IntN(len(holders))]
				if err := e.l.SetOverride(e.ctx, u, a.ID, fp, ledger.OverridePrefer, &p); err != nil {
					fail("prefer: %v", err)
				}
				log = append(log, fmt.Sprintf("prefer %x", fp[:2]))
			} else {
				if err := e.l.SetOverride(e.ctx, u, a.ID, fp, ledger.OverrideKeepBoth, nil); err != nil {
					fail("keep both: %v", err)
				}
				log = append(log, fmt.Sprintf("keep %x", fp[:2]))
			}
		default: // clear a random override
			ovs := overrides(t, e, u, a.ID)
			if len(ovs) == 0 {
				continue
			}
			fp := ovs[r.IntN(len(ovs))]
			if err := e.l.ClearOverride(e.ctx, u, a.ID, fp); err != nil {
				fail("clear: %v", err)
			}
			log = append(log, fmt.Sprintf("clear %x", fp[:2]))
		}
		checkModel(t, e, u, a.ID, fail)
	}
}

// txState is everything Rematerialize decides, per transaction.
type txState struct {
	ID          uuid.UUID
	Fingerprint string
	Occurrence  int32
	Scope       uuid.UUID
	Source      uuid.UUID
	Amount      int64
	Category    string
	Raw         string
}

func snapshot(t *testing.T, e *env, u, account uuid.UUID) []txState {
	t.Helper()
	var out []txState
	err := e.st.WithUser(e.ctx, u, func(_ *db.Queries, tx pgx.Tx) error {
		rows, err := tx.Query(e.ctx, `SELECT id, encode(fingerprint, 'hex'), occurrence, split_scope, coalesce(source_import_id, '00000000-0000-0000-0000-000000000000'),
			amount_minor, bank_category, raw::text FROM transactions WHERE user_id = $1 AND account_id = $2 ORDER BY 2, 3, 4`, u, account)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, pgx.RowToStructByPos[txState])
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func checkModel(t *testing.T, e *env, u, account uuid.UUID, fail func(string, ...any)) {
	t.Helper()
	before := snapshot(t, e, u, account)

	// 1. From scratch: re-materialise every fingerprint that has a transaction, a committed row or
	// an override. Nothing may change.
	var fps [][]byte
	err := e.st.WithUser(e.ctx, u, func(_ *db.Queries, tx pgx.Tx) error {
		rows, err := tx.Query(e.ctx, `
			SELECT fingerprint FROM transactions WHERE user_id = $1 AND account_id = $2
			UNION SELECT ir.fingerprint FROM import_rows ir JOIN imports i ON i.id = ir.import_id
			      WHERE ir.user_id = $1 AND i.account_id = $2 AND i.status = 'committed' AND ir.fingerprint IS NOT NULL
			UNION SELECT fingerprint FROM dedupe_overrides WHERE user_id = $1 AND account_id = $2`, u, account)
		if err == nil {
			fps, err = pgx.CollectRows(rows, pgx.RowTo[[]byte])
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, fp := range fps {
		if err := e.l.Rematerialize(e.ctx, u, account, fp); err != nil {
			fail("rematerialize: %v", err)
		}
	}
	if diff := cmp.Diff(before, snapshot(t, e, u, account)); diff != "" {
		fail("incremental state differs from Rematerialize (-incremental +scratch):\n%s", diff)
	}

	// 2. The multiset rule, from the staged rows alone.
	type rule struct {
		Fingerprint string
		Want, Got   int
	}
	var bad []rule
	err = e.st.WithUser(e.ctx, u, func(_ *db.Queries, tx pgx.Tx) error {
		rows, err := tx.Query(e.ctx, `
			WITH per_import AS (
			  SELECT ir.fingerprint, ir.import_id, count(*) AS n
			  FROM import_rows ir JOIN imports i ON i.id = ir.import_id
			  WHERE ir.user_id = $1 AND i.account_id = $2 AND i.status = 'committed' AND ir.parse_error IS NULL
			  GROUP BY 1, 2
			), want AS (
			  SELECT p.fingerprint, CASE WHEN d.mode = 'keep_both' THEN sum(p.n) ELSE max(p.n) END AS n
			  FROM per_import p LEFT JOIN dedupe_overrides d ON d.account_id = $2 AND d.fingerprint = p.fingerprint
			  GROUP BY p.fingerprint, d.mode
			), got AS (
			  SELECT fingerprint, count(*) AS n FROM transactions WHERE user_id = $1 AND account_id = $2 GROUP BY 1
			)
			SELECT encode(coalesce(w.fingerprint, g.fingerprint), 'hex'), coalesce(w.n, 0)::int, coalesce(g.n, 0)::int
			FROM want w FULL JOIN got g ON g.fingerprint = w.fingerprint
			WHERE coalesce(w.n, 0) <> coalesce(g.n, 0)`, u, account)
		if err == nil {
			bad, err = pgx.CollectRows(rows, pgx.RowToStructByPos[rule])
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(bad) > 0 {
		fail("multiset rule broken: %+v", bad)
	}

	// 3. Links.
	var unlinked int
	err = e.st.WithUser(e.ctx, u, func(_ *db.Queries, tx pgx.Tx) error {
		return tx.QueryRow(e.ctx, `
			SELECT count(*) FROM import_rows ir JOIN imports i ON i.id = ir.import_id
			LEFT JOIN transactions t ON t.id = ir.transaction_id
			WHERE ir.user_id = $1 AND i.account_id = $2 AND i.status = 'committed' AND ir.parse_error IS NULL
			  AND (t.id IS NULL OR t.fingerprint <> ir.fingerprint OR t.occurrence <> ir.occurrence OR t.split_scope <> ir.split_scope)`,
			u, account).Scan(&unlinked)
	})
	if err != nil {
		t.Fatal(err)
	}
	if unlinked != 0 {
		fail("%d staged rows not linked to their transaction", unlinked)
	}
}

// randomFingerprint picks a fingerprint present in the account and the imports that contain it.
func randomFingerprint(t *testing.T, e *env, u, account uuid.UUID, r *rand.Rand) ([]byte, []uuid.UUID) {
	t.Helper()
	type holder struct {
		Fingerprint []byte
		ImportID    uuid.UUID
	}
	var hs []holder
	err := e.st.WithUser(e.ctx, u, func(_ *db.Queries, tx pgx.Tx) error {
		rows, err := tx.Query(e.ctx, `SELECT DISTINCT ir.fingerprint, ir.import_id FROM import_rows ir JOIN imports i ON i.id = ir.import_id
			WHERE ir.user_id = $1 AND i.account_id = $2 AND i.status = 'committed' AND ir.fingerprint IS NOT NULL ORDER BY 1, 2`, u, account)
		if err == nil {
			hs, err = pgx.CollectRows(rows, pgx.RowToStructByPos[holder])
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hs) == 0 {
		return nil, nil
	}
	fp := hs[r.IntN(len(hs))].Fingerprint
	var imps []uuid.UUID
	for _, h := range hs {
		if bytes.Equal(h.Fingerprint, fp) {
			imps = append(imps, h.ImportID)
		}
	}
	return fp, imps
}

func overrides(t *testing.T, e *env, u, account uuid.UUID) [][]byte {
	t.Helper()
	var fps [][]byte
	err := e.st.WithUser(e.ctx, u, func(_ *db.Queries, tx pgx.Tx) error {
		rows, err := tx.Query(e.ctx, `SELECT fingerprint FROM dedupe_overrides WHERE user_id = $1 AND account_id = $2 ORDER BY 1`, u, account)
		if err == nil {
			fps, err = pgx.CollectRows(rows, pgx.RowTo[[]byte])
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return fps
}
