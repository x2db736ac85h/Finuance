package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// ErrNoUser is returned by WithUser and WithUserReadOnly when the user id is the nil UUID: a caller
// that lost the user must fail loudly, not run with an identity that happens to match nothing.
var ErrNoUser = errors.New("store: no user id")

// TxFunc is the body of a transaction. q runs the sqlc queries inside the transaction; tx is there
// for the few hand-written statements in this package (InsertImportRows) and for River's InsertTx.
type TxFunc func(q *db.Queries, tx pgx.Tx) error

// Store is the only way the rest of the backend reaches the database (PLAN §5.3, ADR 0007). Every
// operation runs in a transaction:
//
//   - WithUser / WithUserReadOnly for user data: the transaction carries app.user_id, and the
//     row-level security policies hide and protect every other user's rows;
//   - WithoutUser for the auth tables (users, sessions), which sit outside RLS because a session is
//     resolved before the user is known. User-owned tables look empty there.
//
// Queries still filter by user_id explicitly; RLS is the second line of defence, not the first.
type Store struct {
	pool *pgxpool.Pool
}

// New wraps a pool opened with Open.
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// WithUser runs fn in a read-write transaction scoped to userID. The setting is transaction-local
// (set_config(…, true) is SET LOCAL), so it ends with the transaction and never leaks to the next
// user of the pooled connection. fn's error rolls the transaction back and is returned unchanged.
func (s *Store) WithUser(ctx context.Context, userID uuid.UUID, fn TxFunc) error {
	return s.withUser(ctx, userID, pgx.TxOptions{}, fn)
}

// WithUserReadOnly is WithUser in a READ ONLY transaction: any write fails with SQLSTATE 25006. The
// assistant's tools run here (PLAN §4.8), so a tool can never change data even if its SQL is wrong.
func (s *Store) WithUserReadOnly(ctx context.Context, userID uuid.UUID, fn TxFunc) error {
	return s.withUser(ctx, userID, pgx.TxOptions{AccessMode: pgx.ReadOnly}, fn)
}

func (s *Store) withUser(ctx context.Context, userID uuid.UUID, opts pgx.TxOptions, fn TxFunc) error {
	if userID == uuid.Nil {
		return ErrNoUser
	}
	return pgx.BeginTxFunc(ctx, s.pool, opts, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT set_config('app.user_id', $1, true)`, userID.String()); err != nil {
			return err
		}
		return fn(db.New(tx), tx)
	})
}

// WithoutUser runs fn in a read-write transaction with no user set. It exists for the auth package
// (look up a user by email, resolve a session token) and nothing else; on user-owned tables every
// query returns no rows and every write fails the RLS check.
func (s *Store) WithoutUser(ctx context.Context, fn TxFunc) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error { return fn(db.New(tx), tx) })
}

// Ping is the readiness probe "database": one trivial query.
func (s *Store) Ping(ctx context.Context) error {
	_, err := db.New(s.pool).Ping(ctx)
	return err
}

// MigrationsCurrent is the readiness probe "migrations": the database must be at least at
// ExpectedVersion, so an api whose migrate job failed never receives traffic.
func (s *Store) MigrationsCurrent(ctx context.Context) error {
	got, err := db.New(s.pool).SchemaVersion(ctx)
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if want := ExpectedVersion(); got < want {
		return fmt.Errorf("schema version %d is behind the expected %d", got, want)
	}
	return nil
}
