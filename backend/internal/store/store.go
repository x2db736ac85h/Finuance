// Package store owns the database: the pgx pool, migrations (goose, then River) and the sqlc
// queries in db/. Domain packages use it through store.Querier and the WithUser helpers (Phase 4).
package store

import (
	"context"
	"fmt"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
)

// Open creates a pgx pool with tracing and pings the database once.
func Open(ctx context.Context, dsn string, maxConns int32, tp trace.TracerProvider) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse database URL: %w", errRedacted(err))
	}
	cfg.MaxConns = maxConns
	cfg.ConnConfig.Tracer = otelpgx.NewTracer(otelpgx.WithTracerProvider(tp), otelpgx.WithDisableSQLStatementInAttributes())
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

// errRedacted drops the error text of DSN parse errors, which can echo the password.
func errRedacted(error) error { return fmt.Errorf("invalid connection string") }
