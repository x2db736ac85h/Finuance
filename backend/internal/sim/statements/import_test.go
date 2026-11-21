package statements_test

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/sim/identity"
	"github.com/x2db736ac85h/finuance/backend/internal/sim/statements"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
	"github.com/x2db736ac85h/finuance/backend/testutil"
)

var testDB *testutil.DB

func TestMain(m *testing.M) { testutil.Main(m, &testDB) }

type importer struct {
	t   *testing.T
	ctx context.Context
	st  *store.Store
	l   *ledger.Ledger
}

func (im importer) user() uuid.UUID {
	im.t.Helper()
	id := platform.NewID()
	if err := im.st.WithoutUser(im.ctx, func(q *db.Queries, _ pgx.Tx) error {
		_, err := q.CreateUser(im.ctx, db.CreateUserParams{ID: id, Email: id.String() + "@example.test", PasswordHash: "x"})
		return err
	}); err != nil {
		im.t.Fatal(err)
	}
	return id
}

// upload sends a statement through the upload path (detection, the preview) and commits it as the
// worker does; it fails the test on any row the importer could not read.
func (im importer) upload(u, account uuid.UUID, name string, body []byte) {
	im.t.Helper()
	pv, err := im.l.CreatePreview(im.ctx, u, ledger.Upload{Filename: name, Content: body, AccountID: &account})
	if err != nil {
		im.t.Fatalf("%s: %v", name, err)
	}
	if pv.Stats.Errors != 0 || len(pv.Missing) != 0 {
		im.t.Fatalf("%s: %d rows with errors, missing %v", name, pv.Stats.Errors, pv.Missing)
	}
	if _, err := im.l.CommitImport(im.ctx, u, pv.Import.ID); err != nil {
		im.t.Fatalf("%s: commit: %v", name, err)
	}
}

// transactions lists an account's transactions by what identifies them to a reader, sorted.
func (im importer) transactions(u, account uuid.UUID) []string {
	im.t.Helper()
	var out []string
	if err := im.st.WithUser(im.ctx, u, func(_ *db.Queries, tx pgx.Tx) error {
		rows, err := tx.Query(im.ctx, `SELECT booked_on::text, amount_minor, payee, description, reference
			FROM transactions WHERE user_id = $1 AND account_id = $2`, u, account)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (string, error) {
			var day, payee, desc, ref string
			var amount int64
			err := r.Scan(&day, &amount, &payee, &desc, &ref)
			return fmt.Sprintf("%s %d %q %q %q", day, amount, payee, desc, ref), err
		})
		return err
	}); err != nil {
		im.t.Fatal(err)
	}
	slices.Sort(out)
	return out
}

// TestOverlappingStatementsImportOnce: two statements that overlap by three weeks, imported one
// after the other, leave the same transactions as one statement covering both, in both layouts and
// through the real upload and commit path (PLAN 25.2). Every row the generator wrote is one
// transaction: nothing is lost and nothing doubled.
func TestOverlappingStatementsImportOnce(t *testing.T) {
	tdb := testutil.Require(t, testDB)
	st := store.New(tdb.App)
	im := importer{t: t, ctx: context.Background(), st: st, l: ledger.New(st, platform.SystemClock{})}
	from, to := platform.MustDate(2026, time.January, 1), platform.MustDate(2026, time.March, 31)
	splitEnd, splitStart := platform.MustDate(2026, time.February, 20), platform.MustDate(2026, time.February, 1)
	for _, c := range []struct{ persona, account int }{{0, statements.Current}, {1, statements.Current}, {0, statements.Savings}} {
		p := identity.Nth(c.persona)
		t.Run(fmt.Sprintf("%s-%s", p.Slug, statements.LayoutOf(p, c.account)), func(t *testing.T) {
			im.t = t
			account := func(u uuid.UUID) uuid.UUID {
				a, err := im.l.CreateAccount(im.ctx, u, ledger.AccountInput{Name: statements.AccountName(c.account)})
				if err != nil {
					t.Fatal(err)
				}
				return a.ID
			}
			split, whole := im.user(), im.user()
			splitAcc, wholeAcc := account(split), account(whole)
			for i, r := range [][2]platform.Date{{from, splitEnd}, {splitStart, to}} {
				name, body := statements.File(p, c.account, r[0], r[1], fmt.Sprintf("part-%d", i))
				im.upload(split, splitAcc, name, body)
			}
			name, body := statements.File(p, c.account, from, to, "")
			im.upload(whole, wholeAcc, name, body)

			got, want := im.transactions(split, splitAcc), im.transactions(whole, wholeAcc)
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("two overlapping statements (-one statement, +two):\n%s", diff)
			}
			if n := len(statements.Rows(p, c.account, from, to)); len(want) != n {
				t.Errorf("%d transactions from %d rows", len(want), n)
			}
		})
	}
}
