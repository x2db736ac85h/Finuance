package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// TestAutoApplyRefusesSharedKinds: the table itself refuses the two shared kinds as kinds the panel
// applies by itself (migration 00025, AI-1), and keeps accepting the others.
func TestAutoApplyRefusesSharedKinds(t *testing.T) {
	s := singleConnStore(t)
	ctx := context.Background()
	u := newUser(t, s)
	set := func(kinds ...string) error {
		return s.WithUser(ctx, u, func(_ *db.Queries, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO assistant_settings (user_id, auto_apply) VALUES ($1, $2)
				ON CONFLICT (user_id) DO UPDATE SET auto_apply = EXCLUDED.auto_apply`, u, kinds)
			return err
		})
	}
	if err := set("set_category", "create_rule", "update_goal"); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"add_to_shared_group", "split_expense"} {
		var pgErr *pgconn.PgError
		if err := set("set_budget", kind); !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Errorf("%s: %v, want a check violation", kind, err)
		}
	}
}
