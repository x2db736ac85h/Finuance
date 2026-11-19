// Package store owns the database: the pgx pool, migrations (goose, then River) and the sqlc
// queries in db/. Domain packages use it through store.Querier and the WithUser helpers (Phase 4).
package store

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
)

// Timeouts are PostgreSQL session settings for every connection of a pool (DB-4, IMP-2). Zero
// keeps the server's default, which is no limit.
type Timeouts struct {
	Statement         time.Duration // statement_timeout
	Lock              time.Duration // lock_timeout
	IdleInTransaction time.Duration // idle_in_transaction_session_timeout
}

// RequestTimeouts bound the api's request path. A request waits at most 5 s for a lock, such as
// the account lock an import's commit holds in the worker, and then answers 409 instead of keeping
// a pool connection; a statement runs at most 30 s (the streamed export sets a longer limit for its
// own transaction); a transaction left idle ends after a minute. The worker keeps the defaults: an
// import's commit waits for its locks and may run long statements.
var RequestTimeouts = Timeouts{Statement: 30 * time.Second, Lock: 5 * time.Second, IdleInTransaction: time.Minute}

// params are the settings as startup parameters, in milliseconds.
func (t Timeouts) params() map[string]string {
	out := map[string]string{}
	for name, d := range map[string]time.Duration{
		"statement_timeout": t.Statement, "lock_timeout": t.Lock, "idle_in_transaction_session_timeout": t.IdleInTransaction,
	} {
		if d > 0 {
			out[name] = strconv.FormatInt(d.Milliseconds(), 10)
		}
	}
	return out
}

// Open creates a pgx pool with tracing and the given timeouts, and pings the database once.
func Open(ctx context.Context, dsn string, maxConns int32, tp trace.TracerProvider, t Timeouts) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse database URL: %w", errRedacted(err))
	}
	cfg.MaxConns = maxConns
	for k, v := range t.params() {
		cfg.ConnConfig.RuntimeParams[k] = v
	}
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
