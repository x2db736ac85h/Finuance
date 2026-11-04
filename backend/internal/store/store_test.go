package store_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
	"github.com/x2db736ac85h/finuance/backend/testutil"
)

var testDB *testutil.DB

func TestMain(m *testing.M) { testutil.Main(m, &testDB) }

func TestExpectedVersion(t *testing.T) {
	if v := store.ExpectedVersion(); v != 14 {
		t.Errorf("ExpectedVersion = %d, want 14 (00014_shared_groups.sql)", v)
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
		if i == 0 {
			if err := store.New(tdb.App).MigrationsCurrent(ctx); err == nil {
				t.Error("readiness probe passed with the schema one migration behind")
			}
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
	s := store.New(tdb.App)
	ctx := context.Background()
	if err := s.Ping(ctx); err != nil {
		t.Errorf("ping: %v", err)
	}
	if err := s.MigrationsCurrent(ctx); err != nil {
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

// TestLikeIsLeakproof: bootstrap.sql marks textlike LEAKPROOF (Phase 7 decision), so a LIKE on
// search_text can be an index condition for the app role despite forced RLS. That the planner then
// uses the per-user trigram index is asserted by TestSearchReadsOnlyTheUsersEntries and TestQueryPerf
// (internal/query).
func TestLikeIsLeakproof(t *testing.T) {
	tdb := testutil.Require(t, testDB)
	var leakproof bool
	if err := tdb.App.QueryRow(context.Background(), `SELECT proleakproof FROM pg_proc WHERE oid = 'pg_catalog.textlike(text, text)'::regprocedure`).Scan(&leakproof); err != nil || !leakproof {
		t.Fatalf("textlike leakproof = %v, %v (deploy/db/bootstrap.sql)", leakproof, err)
	}
}

// TestWhoMayConnect: bootstrap.sql lets only the two Finuance roles and monitoring (pg_monitor
// members, such as CloudNativePG's cnpg_metrics_exporter) connect to the database; any other role
// can't, and pg_monitor gets no table access.
func TestWhoMayConnect(t *testing.T) {
	tdb := testutil.Require(t, testDB)
	ctx := context.Background()
	super, err := pgx.Connect(ctx, tdb.SuperDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer super.Close(ctx)
	for _, q := range []string{
		`CREATE ROLE who_may_connect_stranger LOGIN`,
		`CREATE ROLE who_may_connect_exporter LOGIN IN ROLE pg_monitor`,
	} {
		if _, err := super.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = super.Exec(context.Background(), `DROP ROLE who_may_connect_stranger, who_may_connect_exporter`)
	})
	for role, want := range map[string]bool{
		"finuance_owner": true, "finuance_app": true,
		"who_may_connect_exporter": true, "who_may_connect_stranger": false,
	} {
		var got bool
		if err := super.QueryRow(ctx, `SELECT has_database_privilege($1, 'finuance', 'CONNECT')`, role).Scan(&got); err != nil || got != want {
			t.Errorf("%s may connect = %v, %v; want %v", role, got, err, want)
		}
	}
	var tables bool
	if err := super.QueryRow(ctx, `SELECT has_table_privilege('pg_monitor', 'transactions', 'SELECT')`).Scan(&tables); err != nil || tables {
		t.Errorf("pg_monitor reads transactions = %v, %v; want false", tables, err)
	}
}

// TestReadinessTimeoutsAreDeadlines: /readyz treats a check that only ran out of time as "busy"
// (httpapi.TestReadyzBusyDatabase), which needs the store's checks to keep context.DeadlineExceeded
// in their errors.
func TestReadinessTimeoutsAreDeadlines(t *testing.T) {
	tdb := testutil.Require(t, testDB)
	st := store.New(tdb.App)
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)
	for name, probe := range map[string]func(context.Context) error{"Ping": st.Ping, "MigrationsCurrent": st.MigrationsCurrent} {
		if err := probe(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("%s with an expired deadline = %v, want context.DeadlineExceeded", name, err)
		}
	}
}
