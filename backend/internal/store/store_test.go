package store_test

import (
	"context"
	"strings"
	"testing"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
	"github.com/x2db736ac85h/finuance/backend/testutil"
)

var testDB *testutil.DB

func TestMain(m *testing.M) { testutil.Main(m, &testDB) }

func TestExpectedVersion(t *testing.T) {
	if v := store.ExpectedVersion(); v != 3 {
		t.Errorf("ExpectedVersion = %d, want 3 (00003_rls.sql)", v)
	}
}

func TestMigrationsUpDownUp(t *testing.T) {
	tdb := testutil.Require(t, testDB)
	ctx := context.Background()
	mig, err := store.NewMigrator(tdb.Owner)
	if err != nil {
		t.Fatal(err)
	}
	defer mig.Close()
	if v, err := mig.Version(ctx); err != nil || v != store.ExpectedVersion() {
		t.Fatalf("version after up = %d, %v", v, err)
	}
	for i := int64(0); i < store.ExpectedVersion(); i++ {
		if _, err := mig.Down(ctx); err != nil {
			t.Fatalf("down: %v", err)
		}
	}
	if v, _ := mig.Version(ctx); v != 0 {
		t.Fatalf("version after down = %d", v)
	}
	if _, err := mig.Up(ctx); err != nil {
		t.Fatalf("up again: %v", err)
	}
	st, err := mig.Status(ctx)
	if err != nil || len(st) == 0 {
		t.Fatalf("status: %v", err)
	}
}

func TestRiverTablesAndRoles(t *testing.T) {
	tdb := testutil.Require(t, testDB)
	ctx := context.Background()
	var n int
	if err := tdb.App.QueryRow(ctx, `SELECT count(*) FROM river_job`).Scan(&n); err != nil {
		t.Fatalf("app role cannot read river_job (River migrations + default privileges): %v", err)
	}
	if _, err := tdb.App.Exec(ctx, `CREATE TABLE nope (id int)`); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("app role ran DDL: %v", err)
	}
	var user string
	_ = tdb.App.QueryRow(ctx, `SELECT current_user`).Scan(&user)
	if user != "finuance_app" {
		t.Errorf("app pool user = %s", user)
	}
	var exts []string
	rows, _ := tdb.Owner.Query(ctx, `SELECT extname FROM pg_extension ORDER BY 1`)
	for rows.Next() {
		var e string
		_ = rows.Scan(&e)
		exts = append(exts, e)
	}
	if got := strings.Join(exts, ","); !strings.Contains(got, "citext") || !strings.Contains(got, "pg_trgm") {
		t.Errorf("extensions = %s", got)
	}
}

func TestReadinessProbes(t *testing.T) {
	tdb := testutil.Require(t, testDB)
	q := db.New(tdb.App)
	ctx := context.Background()
	if err := store.Ping(q)(ctx); err != nil {
		t.Errorf("ping: %v", err)
	}
	if err := store.MigrationsCurrent(q)(ctx); err != nil {
		t.Errorf("migrations current: %v", err)
	}
}

func TestDateRoundTrip(t *testing.T) {
	tdb := testutil.Require(t, testDB)
	ctx := context.Background()
	want := platform.MustDate(2024, 2, 29)
	var got platform.Date
	var null *platform.Date
	if err := tdb.App.QueryRow(ctx, `SELECT $1::date, NULL::date`, want).Scan(&got, &null); err != nil {
		t.Fatal(err)
	}
	if got != want || null != nil {
		t.Errorf("round trip = %v, %v", got, null)
	}
	var s string
	if err := tdb.App.QueryRow(ctx, `SELECT ($1::date + 1)::text`, want).Scan(&s); err != nil || s != "2024-03-01" {
		t.Errorf("SQL date arithmetic = %q, %v", s, err)
	}
}
