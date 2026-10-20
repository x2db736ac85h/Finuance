package ledger_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/ingest"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
	"github.com/x2db736ac85h/finuance/backend/testutil"
)

var testDB *testutil.DB

func TestMain(m *testing.M) { testutil.Main(m, &testDB) }

var clock = platform.FixedClock{T: time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)}

type env struct {
	t   *testing.T
	ctx context.Context
	st  *store.Store
	l   *ledger.Ledger
}

func newEnv(t *testing.T) *env {
	t.Helper()
	tdb := testutil.Require(t, testDB)
	st := store.New(tdb.App)
	return &env{t: t, ctx: context.Background(), st: st, l: ledger.New(st, clock)}
}

// user creates a user through the auth path and returns its id.
func (e *env) user() uuid.UUID {
	e.t.Helper()
	id := platform.NewID()
	if err := e.st.WithoutUser(e.ctx, func(q *db.Queries, _ pgx.Tx) error {
		_, err := q.CreateUser(e.ctx, db.CreateUserParams{ID: id, Email: id.String() + "@example.test", PasswordHash: "x"})
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	return id
}

func (e *env) account(u uuid.UUID, name string) ledger.Account {
	e.t.Helper()
	a, err := e.l.CreateAccount(e.ctx, u, ledger.AccountInput{Name: name})
	if err != nil {
		e.t.Fatal(err)
	}
	return a
}

// fixture reads a committed test fixture (paths relative to testdata/fixtures).
func fixture(t *testing.T, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "fixtures", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// staged parses and stages a file with the detected mapping, as the upload endpoint will.
func staged(t *testing.T, name string, content []byte, account *uuid.UUID) ledger.NewImport {
	t.Helper()
	return stagedWith(t, name, content, account, nil)
}

// stagedWith stages with an explicit mapping (nil: the detected one), as a user sets it in the preview.
func stagedWith(t *testing.T, name string, content []byte, account *uuid.UUID, m *ingest.Mapping) ledger.NewImport {
	t.Helper()
	res, err := ingest.Parse(context.Background(), content, ingest.Options{})
	if err != nil {
		t.Fatal(err)
	}
	mapping := ingest.MappingFromRoles(res.Roles)
	if m != nil {
		mapping = *m
	}
	rows, stats, err := ingest.Stage(context.Background(), res.Table, mapping,
		ingest.StageOptions{ImportCurrency: res.Roles.Currency})
	if err != nil {
		t.Fatal(err)
	}
	types := make([]string, len(res.Columns))
	for i, c := range res.Columns {
		types[i] = string(c.Type)
	}
	st, _ := json.Marshal(map[string]any{"rows": stats.Rows, "errors": stats.Errors, "headerAt": res.Table.HeaderAt})
	return ledger.NewImport{AccountID: account, Filename: name, Content: content, Encoding: string(res.Encoding),
		Delimiter: string(res.Delimiter), Headers: res.Table.Headers, ColumnTypes: types, Mapping: json.RawMessage(`{}`),
		Options: json.RawMessage(`{}`), MappingSource: "detected", HeaderSignature: ingest.HeaderSignature(res.Table.Headers, res.Delimiter),
		Stats: st, Rows: rows}
}

// importFile stores and commits a fixture into account and returns the import.
func (e *env) importFile(u, account uuid.UUID, rel string) (ledger.Import, ledger.CommitResult) {
	e.t.Helper()
	imp, err := e.l.CreateImport(e.ctx, u, staged(e.t, filepath.Base(rel), fixture(e.t, rel), &account))
	if err != nil {
		e.t.Fatal(err)
	}
	res, err := e.l.CommitImport(e.ctx, u, imp.ID)
	if err != nil {
		e.t.Fatal(err)
	}
	return imp, res
}

// countTransactions counts the account's transactions with test-only SQL (the ledger's list query
// arrives in Phase 7).
func (e *env) countTransactions(u, account uuid.UUID) int {
	e.t.Helper()
	var n int
	if err := e.st.WithUser(e.ctx, u, func(_ *db.Queries, tx pgx.Tx) error {
		return tx.QueryRow(e.ctx, `SELECT count(*) FROM transactions WHERE user_id = $1 AND account_id = $2`, u, account).Scan(&n)
	}); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *env) transactionIDs(u, account uuid.UUID) []uuid.UUID {
	e.t.Helper()
	var ids []uuid.UUID
	if err := e.st.WithUser(e.ctx, u, func(_ *db.Queries, tx pgx.Tx) error {
		rows, err := tx.Query(e.ctx, `SELECT id FROM transactions WHERE user_id = $1 AND account_id = $2 ORDER BY seq`, u, account)
		if err != nil {
			return err
		}
		ids, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	return ids
}

func isErr(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

// count counts the user's rows of a table (test-only SQL; table names are constants).
func (e *env) count(u uuid.UUID, table string) int {
	e.t.Helper()
	var n int
	if err := e.st.WithUser(e.ctx, u, func(_ *db.Queries, tx pgx.Tx) error {
		return tx.QueryRow(e.ctx, `SELECT count(*) FROM `+table+` WHERE user_id = $1`, u).Scan(&n)
	}); err != nil {
		e.t.Fatal(err)
	}
	return n
}
