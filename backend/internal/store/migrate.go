package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrator runs goose migrations (our schema) and River's migrations. It must connect as the
// owner role (FINUANCE_MIGRATE_DATABASE_URL); the app role cannot run DDL.
type Migrator struct {
	pool  *pgxpool.Pool
	sqlDB *sql.DB
	goose *goose.Provider
}

// NewMigrator wraps an owner-role pool.
func NewMigrator(pool *pgxpool.Pool) (*Migrator, error) {
	fsys, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		return nil, err
	}
	sqlDB := stdlib.OpenDBFromPool(pool)
	p, err := goose.NewProvider(goose.DialectPostgres, sqlDB, fsys)
	if err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("goose: %w", err)
	}
	return &Migrator{pool: pool, sqlDB: sqlDB, goose: p}, nil
}

// Close releases the database/sql wrapper (not the pool).
func (m *Migrator) Close() error { return m.sqlDB.Close() }

// Up applies every pending goose migration, then River's.
func (m *Migrator) Up(ctx context.Context) ([]*goose.MigrationResult, error) {
	res, err := m.goose.Up(ctx)
	if err != nil {
		return res, fmt.Errorf("goose up: %w", err)
	}
	rm, err := rivermigrate.New(riverpgxv5.New(m.pool), nil)
	if err != nil {
		return res, fmt.Errorf("river migrator: %w", err)
	}
	if _, err := rm.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return res, fmt.Errorf("river migrate up: %w", err)
	}
	return res, nil
}

// Down rolls back the most recent goose migration. River's schema is left alone.
func (m *Migrator) Down(ctx context.Context) (*goose.MigrationResult, error) {
	res, err := m.goose.Down(ctx)
	if errors.Is(err, goose.ErrNoNextVersion) {
		return nil, nil
	}
	return res, err
}

// Status lists every embedded migration and whether it is applied.
func (m *Migrator) Status(ctx context.Context) ([]*goose.MigrationStatus, error) {
	return m.goose.Status(ctx)
}

// Version returns the current goose version of the database.
func (m *Migrator) Version(ctx context.Context) (int64, error) { return m.goose.GetDBVersion(ctx) }

// ExpectedVersion is the highest migration embedded in this binary.
func ExpectedVersion() int64 {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return 0
	}
	var top int64
	for _, e := range entries {
		if v, err := goose.NumericComponent(e.Name()); err == nil && v > top {
			top = v
		}
	}
	return top
}
