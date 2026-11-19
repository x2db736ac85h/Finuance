package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
	"github.com/x2db736ac85h/finuance/backend/testutil"
)

// TestTimeoutsApply: a pool opened with Timeouts sets them on every connection (DB-4, IMP-2).
// Waiting for an account that another transaction has locked (as an import's commit does) ends
// with lock_not_available within the lock timeout, the pool stays usable, a statement that runs too
// long ends with query_canceled, and a pool without Timeouts keeps the server's defaults.
func TestTimeoutsApply(t *testing.T) {
	tdb := testutil.Require(t, testDB)
	ctx := context.Background()
	open := func(to store.Timeouts) *store.Store {
		pool, err := store.Open(ctx, tdb.AppDSN, 2, noop.NewTracerProvider(), to)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(pool.Close)
		return store.New(pool)
	}
	limited := open(store.Timeouts{Statement: 400 * time.Millisecond, Lock: 200 * time.Millisecond, IdleInTransaction: time.Second})
	plain := open(store.Timeouts{})

	settings := func(s *store.Store) [3]string {
		var got [3]string
		err := s.WithoutUser(ctx, func(_ *db.Queries, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT current_setting('statement_timeout'), current_setting('lock_timeout'),
				current_setting('idle_in_transaction_session_timeout')`).Scan(&got[0], &got[1], &got[2])
		})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := settings(limited); got != [3]string{"400ms", "200ms", "1s"} {
		t.Errorf("limited pool: %v", got)
	}
	if got := settings(plain); got != [3]string{"0", "0", "0"} {
		t.Errorf("pool without timeouts: %v", got)
	}

	// another transaction holds the account's lock
	u := newUser(t, plain)
	account := platform.NewID()
	if err := plain.WithUser(ctx, u, func(q *db.Queries, _ pgx.Tx) error {
		_, err := q.CreateAccount(ctx, db.CreateAccountParams{ID: account, UserID: u, Name: "Konto", NameN: "konto", Currency: "PLN", Kind: "bank"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	holding, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- plain.WithUser(ctx, u, func(q *db.Queries, _ pgx.Tx) error {
			if _, err := q.LockAccount(ctx, db.LockAccountParams{UserID: u, ID: account}); err != nil {
				close(holding)
				return err
			}
			close(holding)
			<-release
			return nil
		})
	}()
	<-holding
	start := time.Now()
	err := limited.WithUser(ctx, u, func(q *db.Queries, _ pgx.Tx) error {
		_, err := q.LockAccount(ctx, db.LockAccountParams{UserID: u, ID: account})
		return err
	})
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55P03" || time.Since(start) > 2*time.Second {
		t.Errorf("waiting for the lock: %v after %v", err, time.Since(start))
	}
	if got := settings(limited); got[1] != "200ms" {
		t.Errorf("the pool after a lock timeout: %v", got)
	}

	err = limited.WithoutUser(ctx, func(_ *db.Queries, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "SELECT pg_sleep(2)")
		return err
	})
	if !errors.As(err, &pgErr) || pgErr.Code != "57014" {
		t.Errorf("a long statement: %v", err)
	}
}
