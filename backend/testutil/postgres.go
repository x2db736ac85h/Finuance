// Package testutil holds test helpers shared across packages: a Postgres test database started once
// per package with testcontainers, bootstrapped exactly like deploy/compose does.
package testutil

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/x2db736ac85h/finuance/backend/internal/store"
)

// PostgresImage must match deploy/compose/compose.yaml.
const PostgresImage = "postgres:18.6-bookworm"

const (
	superPassword = "test-super"
	ownerPassword = "test-owner"
	appPassword   = "test-app"
)

// DB is a migrated test database.
type DB struct {
	App      *pgxpool.Pool // finuance_app: subject to RLS, no DDL
	Owner    *pgxpool.Pool // finuance_owner: owns the schema
	AppDSN   string
	OwnerDSN string
	// SuperDSN is the container's superuser, which bypasses row level security. Only for tests
	// that must create what the app roles can't (e.g. audit events older than the retention).
	SuperDSN string
}

// Main is called from TestMain: os.Exit(run(...)).  In -short mode no container is started and tests that
// need the database skip (see Require). Otherwise it starts Postgres, runs the bootstrap and every
// migration, runs the tests, and removes the container.
func Main(m *testing.M, out **DB) { os.Exit(run(m, out)) }

func run(m *testing.M, out **DB) int {
	flag.Parse() // testing.Short needs parsed flags inside TestMain
	if testing.Short() {
		return m.Run()
	}
	ctx := context.Background()
	db, stop, err := StartPostgres(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "testutil: cannot start Postgres (is Docker running? use -short to skip):", err)
		return 1
	}
	defer stop()
	*out = db
	return m.Run()
}

// Require skips t in -short mode and returns the database otherwise.
func Require(t *testing.T, db *DB) *DB {
	t.Helper()
	if db == nil {
		t.Skip("integration test: needs Postgres (run without -short)")
	}
	return db
}

// StartPostgres starts a container, bootstraps roles and extensions as the superuser (the same
// initdb wrapper and bootstrap.sql Compose uses), applies migrations as the owner, and opens pools.
func StartPostgres(ctx context.Context) (*DB, func(), error) {
	root, err := repoRoot()
	if err != nil {
		return nil, nil, err
	}
	ctr, err := tcpostgres.Run(ctx, PostgresImage,
		tcpostgres.WithDatabase("postgres"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword(superPassword),
		testcontainers.WithEnv(map[string]string{"FINUANCE_OWNER_PASSWORD": ownerPassword, "FINUANCE_APP_PASSWORD": appPassword}),
		testcontainers.WithFiles(
			testcontainers.ContainerFile{HostFilePath: filepath.Join(root, "deploy", "db", "bootstrap.sql"), ContainerFilePath: "/finuance/bootstrap.sql", FileMode: 0o644},
			testcontainers.ContainerFile{HostFilePath: filepath.Join(root, "deploy", "db", "initdb", "00-bootstrap.sh"), ContainerFilePath: "/docker-entrypoint-initdb.d/00-bootstrap.sh", FileMode: 0o755},
		),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("start container: %w", err)
	}
	stop := func() { _ = testcontainers.TerminateContainer(ctr) }
	super, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		stop()
		return nil, nil, err
	}
	dsn := func(user, pw string) string {
		u, _ := url.Parse(super)
		u.User = url.UserPassword(user, pw)
		u.Path = "/finuance"
		return u.String()
	}
	db := &DB{OwnerDSN: dsn("finuance_owner", ownerPassword), AppDSN: dsn("finuance_app", appPassword), SuperDSN: dsn("postgres", superPassword)}
	if db.Owner, err = pgxpool.New(ctx, db.OwnerDSN); err != nil {
		stop()
		return nil, nil, err
	}
	mig, err := store.NewMigrator(db.Owner)
	if err != nil {
		stop()
		return nil, nil, err
	}
	defer func() { _ = mig.Close() }()
	if _, err := mig.Up(ctx); err != nil {
		stop()
		return nil, nil, fmt.Errorf("migrate: %w", err)
	}
	if db.App, err = pgxpool.New(ctx, db.AppDSN); err != nil {
		stop()
		return nil, nil, err
	}
	return db, func() { db.App.Close(); db.Owner.Close(); stop() }, nil
}

// Truncate empties every table we own (not the goose and River bookkeeping) after the test.
func (db *DB) Truncate(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := db.Owner.Exec(ctx, `
			DO $$ DECLARE r record; BEGIN
			  FOR r IN SELECT tablename FROM pg_tables WHERE schemaname = 'public'
			    AND tablename NOT IN ('goose_db_version', 'river_migration') LOOP
			    EXECUTE format('TRUNCATE TABLE %I CASCADE', r.tablename);
			  END LOOP;
			END $$`)
		if err != nil {
			t.Errorf("truncate: %v", err)
		}
	})
}

func repoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("cannot locate testutil source")
	}
	return filepath.Abs(filepath.Join(filepath.Dir(file), "..", ".."))
}
