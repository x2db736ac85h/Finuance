package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/x2db736ac85h/finuance/backend/internal/money"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
	"github.com/x2db736ac85h/finuance/backend/testutil"
)

// TestAmountBounds: the tables refuse an amount beyond money.MaxAbsMinor and take one at it
// (migration 00026, API-4), and the RUNBOOK's statement "Validating the amount checks" validates
// every one of the checks.
func TestAmountBounds(t *testing.T) {
	tdb := testutil.Require(t, testDB)
	s := singleConnStore(t)
	ctx := context.Background()
	u := newUser(t, s)
	cat := platform.NewID()
	if err := s.WithUser(ctx, u, func(q *db.Queries, _ pgx.Tx) error {
		_, err := q.CreateCategory(ctx, db.CreateCategoryParams{ID: cat, UserID: u, Name: "Food", NameN: "food"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	budget := func(amount int64) error {
		return s.WithUser(ctx, u, func(_ *db.Queries, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO budgets (user_id, category_id, amount_minor) VALUES ($1, $2, $3)
				ON CONFLICT (user_id, category_id) DO UPDATE SET amount_minor = EXCLUDED.amount_minor`, u, cat, amount)
			return err
		})
	}
	if err := budget(money.MaxAbsMinor); err != nil {
		t.Fatalf("at the bound: %v", err)
	}
	var pgErr *pgconn.PgError
	if err := budget(money.MaxAbsMinor + 1); !errors.As(err, &pgErr) || pgErr.Code != "23514" || pgErr.ConstraintName != "budgets_amount_minor_range" {
		t.Errorf("beyond the bound: %v", err)
	}

	if _, err := tdb.Owner.Exec(ctx, `DO $$ DECLARE r record; BEGIN
  FOR r IN SELECT conrelid::regclass AS t, conname FROM pg_constraint
           WHERE conname LIKE '%\_minor\_range' AND NOT convalidated LOOP
    EXECUTE format('ALTER TABLE %s VALIDATE CONSTRAINT %I', r.t, r.conname);
  END LOOP; END $$`); err != nil {
		t.Fatalf("the RUNBOOK's validation: %v", err)
	}
	var total, open int
	if err := tdb.Owner.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE NOT convalidated) FROM pg_constraint
		WHERE conname LIKE '%\_minor\_range'`).Scan(&total, &open); err != nil || total != 16 || open != 0 {
		t.Errorf("checks: %d, not validated: %d (%v)", total, open, err)
	}
}
