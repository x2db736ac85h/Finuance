package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/x2db736ac85h/finuance/backend/internal/store"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
	"github.com/x2db736ac85h/finuance/backend/testutil"
)

// TestAuditLogIsAppendOnly: the app role can read and add its own events but can't change them,
// erase recent ones, backdate one, or empty the table (migration 00010); only events older than
// the retention can be deleted, and only the user's own.
func TestAuditLogIsAppendOnly(t *testing.T) {
	tdb := testutil.Require(t, testDB)
	ctx := context.Background()
	s := store.New(tdb.App)
	a, b := newUser(t, s), newUser(t, s)
	insert := func(u uuid.UUID) {
		t.Helper()
		if err := s.WithUser(ctx, u, func(q *db.Queries, _ pgx.Tx) error {
			return q.InsertAuditEvent(ctx, db.InsertAuditEventParams{UserID: u, Action: "auth.login", Meta: []byte(`{}`)})
		}); err != nil {
			t.Fatal(err)
		}
	}
	insert(a)
	insert(b)

	refused := map[string]string{
		"update":   `UPDATE audit_log SET action = 'auth.logout'`,
		"truncate": `TRUNCATE audit_log`,
		"backdate": `INSERT INTO audit_log (user_id, action, at) VALUES (current_setting('app.user_id')::uuid, 'auth.login', now() - interval '400 days')`,
	}
	for name, sql := range refused {
		err := s.WithUser(ctx, a, func(_ *db.Queries, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, sql)
			return err
		})
		if !isPgCode(err, "42501") {
			t.Errorf("%s: %v, want 42501 (insufficient privilege or policy violation)", name, err)
		}
	}
	if err := s.WithUser(ctx, a, func(_ *db.Queries, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM audit_log`)
		if err == nil && tag.RowsAffected() != 0 {
			t.Errorf("deleted %d recent events", tag.RowsAffected())
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// Events past the retention (made by the superuser, which bypasses RLS) can be deleted, each
	// user's by that user only.
	super, err := pgxpool.New(ctx, tdb.SuperDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer super.Close()
	for _, u := range []uuid.UUID{a, b} {
		if _, err := super.Exec(ctx, `INSERT INTO audit_log (user_id, action, at) VALUES ($1, 'auth.login', now() - interval '366 days'),
			($1, 'auth.logout', now() - interval '364 days')`, u); err != nil {
			t.Fatal(err)
		}
	}
	var deleted int64
	if err := s.WithUser(ctx, a, func(q *db.Queries, _ pgx.Tx) error {
		var err error
		deleted, err = q.DeleteExpiredAuditEvents(ctx, a)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Errorf("deleted %d expired events of A, want 1", deleted)
	}
	var left int
	if err := super.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE user_id = $1`, b).Scan(&left); err != nil || left != 3 {
		t.Errorf("B has %d events, want 3 (%v)", left, err)
	}
}
