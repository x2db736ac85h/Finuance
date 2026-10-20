package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
	"github.com/x2db736ac85h/finuance/backend/testutil"
)

// singleConnStore returns a Store over a one-connection app pool, so consecutive transactions are
// guaranteed to reuse the same server session (the leak case).
func singleConnStore(t *testing.T) *store.Store {
	t.Helper()
	tdb := testutil.Require(t, testDB)
	cfg, err := pgxpool.ParseConfig(tdb.AppDSN)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return store.New(pool)
}

func currentUser(ctx context.Context, tx pgx.Tx) (string, error) {
	var s string
	err := tx.QueryRow(ctx, `SELECT coalesce(current_setting('app.user_id', true), '')`).Scan(&s)
	return s, err
}

func TestWithUserScopesTheSettingToOneTransaction(t *testing.T) {
	s := singleConnStore(t)
	ctx := context.Background()
	id := platform.NewID()
	if err := s.WithUser(ctx, id, func(_ *db.Queries, tx pgx.Tx) error {
		got, err := currentUser(ctx, tx)
		if err == nil && got != id.String() {
			t.Errorf("inside WithUser app.user_id = %q, want %s", got, id)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// Same (only) connection, next transaction: the setting must be gone.
	if err := s.WithoutUser(ctx, func(_ *db.Queries, tx pgx.Tx) error {
		got, err := currentUser(ctx, tx)
		if err == nil && got != "" {
			t.Errorf("app.user_id leaked to the next transaction: %q", got)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestWithUserRollsBackOnError(t *testing.T) {
	s := singleConnStore(t)
	ctx := context.Background()
	u := newUser(t, s)
	boom := errors.New("boom")
	err := s.WithUser(ctx, u, func(q *db.Queries, _ pgx.Tx) error {
		if _, err := q.CreateCategory(ctx, db.CreateCategoryParams{ID: platform.NewID(), UserID: u, Name: "Food", NameN: "food"}); err != nil {
			t.Fatal(err)
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the callback's error", err)
	}
	if err := s.WithUser(ctx, u, func(q *db.Queries, _ pgx.Tx) error {
		cats, err := q.ListCategories(ctx, u)
		if err == nil && len(cats) != 0 {
			t.Errorf("rolled-back insert is visible: %v", cats)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestWithUserReadOnlyRejectsWrites(t *testing.T) {
	s := singleConnStore(t)
	ctx := context.Background()
	u := newUser(t, s)
	err := s.WithUserReadOnly(ctx, u, func(q *db.Queries, _ pgx.Tx) error {
		_, err := q.CreateCategory(ctx, db.CreateCategoryParams{ID: platform.NewID(), UserID: u, Name: "Food", NameN: "food"})
		return err
	})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "25006" { // read_only_sql_transaction
		t.Fatalf("write in a read-only transaction: %v, want SQLSTATE 25006", err)
	}
	// Reads work, and the user is set.
	if err := s.WithUserReadOnly(ctx, u, func(_ *db.Queries, tx pgx.Tx) error {
		got, err := currentUser(ctx, tx)
		if err == nil && got != u.String() {
			t.Errorf("app.user_id = %q", got)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestWithUserRequiresAUser(t *testing.T) {
	s := singleConnStore(t)
	called := false
	fn := func(*db.Queries, pgx.Tx) error { called = true; return nil }
	if err := s.WithUser(context.Background(), uuid.Nil, fn); !errors.Is(err, store.ErrNoUser) {
		t.Errorf("WithUser(nil) = %v", err)
	}
	if err := s.WithUserReadOnly(context.Background(), uuid.Nil, fn); !errors.Is(err, store.ErrNoUser) {
		t.Errorf("WithUserReadOnly(nil) = %v", err)
	}
	if called {
		t.Error("callback ran without a user")
	}
}

// newUser inserts a user through the auth path (users is outside RLS) and returns its id.
func newUser(t *testing.T, s *store.Store) uuid.UUID {
	t.Helper()
	id := platform.NewID()
	err := s.WithoutUser(context.Background(), func(q *db.Queries, _ pgx.Tx) error {
		_, err := q.CreateUser(context.Background(), db.CreateUserParams{ID: id, Email: id.String() + "@example.test", PasswordHash: "x"})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}
