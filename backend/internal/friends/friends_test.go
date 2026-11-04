package friends_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/friends"
	"github.com/x2db736ac85h/finuance/backend/internal/notify"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
	"github.com/x2db736ac85h/finuance/backend/testutil"
)

var testDB *testutil.DB

func TestMain(m *testing.M) { testutil.Main(m, &testDB) }

type clock struct{ t time.Time }

func (c *clock) Now() time.Time { return c.t }

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"K7Q4-9MZD":  "K7Q49MZD",
		"k7q4 9mzd":  "K7Q49MZD",
		"io0l-abcd":  "1001ABCD", // I, L → 1 and O → 0, as Crockford reads them
		" K7Q49MZD ": "K7Q49MZD",
		"K7Q4-9MZ":   "", // too short
		"K7Q4-9MZDD": "", // too long
		"K7Q4-9MZU":  "", // U isn't in the alphabet
		"K7Q4_9MZD":  "",
		"ąćęł-ńóśź":  "",
	} {
		got, ok := friends.Normalize(in)
		if (want == "") == ok || (ok && got != want) {
			t.Errorf("Normalize(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
}

func TestExpiresAt(t *testing.T) {
	at := time.Date(2026, 11, 3, 10, 59, 30, 0, time.UTC)
	if got := friends.ExpiresAt(at); !got.Equal(time.Date(2026, 11, 3, 11, 10, 0, 0, time.UTC)) {
		t.Errorf("ExpiresAt = %v", got)
	}
	if got := friends.ExpiresAt(time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)); !got.Equal(time.Date(2026, 11, 3, 11, 10, 0, 0, time.UTC)) {
		t.Errorf("ExpiresAt at the full hour = %v", got)
	}
}

type env struct {
	ctx context.Context
	st  *store.Store
	clk *clock
	f   *friends.Service
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st := store.New(testutil.Require(t, testDB).App)
	clk := &clock{t: time.Date(2026, 11, 3, 10, 59, 0, 0, time.UTC)}
	return &env{ctx: context.Background(), st: st, clk: clk, f: friends.New(st, clk, notify.New(st, clk))}
}

func (e *env) user(t *testing.T, name string) uuid.UUID {
	t.Helper()
	id := platform.NewID()
	if err := e.st.WithoutUser(e.ctx, func(q *db.Queries, _ pgx.Tx) error {
		_, err := q.CreateUser(e.ctx, db.CreateUserParams{ID: id, Email: id.String() + "@example.test", PasswordHash: "x", DisplayName: name})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

// TestCodeLifetime: a code made at 10:59 is good until 11:10 (the hour plus 10 minutes' grace),
// a used code and a replaced code are gone, and the cleanup removes expired ones.
func TestCodeLifetime(t *testing.T) {
	e := newEnv(t)
	a, b, c := e.user(t, "A"), e.user(t, "B"), e.user(t, "C")

	old, err := e.f.NewCode(e.ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	code, err := e.f.NewCode(e.ctx, a) // replaces the first
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.f.Redeem(e.ctx, b, old.Code); !errors.Is(err, friends.ErrInvalid) {
		t.Errorf("a replaced code worked: %v", err)
	}
	e.clk.t = time.Date(2026, 11, 3, 11, 9, 59, 0, time.UTC)
	if f, err := e.f.Redeem(e.ctx, b, code.Code); err != nil || f.ID != a || f.DisplayName != "A" {
		t.Fatalf("redeem in the grace = %+v, %v", f, err)
	}
	if _, err := e.f.Redeem(e.ctx, c, code.Code); !errors.Is(err, friends.ErrInvalid) {
		t.Errorf("a used code worked again: %v", err)
	}

	e.clk.t = time.Date(2026, 11, 3, 10, 59, 0, 0, time.UTC)
	late, err := e.f.NewCode(e.ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	e.clk.t = time.Date(2026, 11, 3, 11, 10, 1, 0, time.UTC)
	if _, err := e.f.Redeem(e.ctx, a, late.Code); !errors.Is(err, friends.ErrInvalid) {
		t.Errorf("an expired code worked: %v", err)
	}
	if n, err := e.f.Cleanup(e.ctx); err != nil || n != 1 {
		t.Errorf("cleanup = %d, %v", n, err)
	}
	if _, err := e.f.Redeem(e.ctx, c, late.Code); !errors.Is(err, friends.ErrInvalid) { // own code (and gone)
		t.Errorf("own code: %v", err)
	}
}

// TestFriendshipRLS: only the two sides see a friendship, nobody can add a pair they aren't in,
// and a third user can't remove it.
func TestFriendshipRLS(t *testing.T) {
	e := newEnv(t)
	a, b, c := e.user(t, "A"), e.user(t, "B"), e.user(t, "C")
	code, err := e.f.NewCode(e.ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.f.Redeem(e.ctx, b, code.Code); err != nil {
		t.Fatal(err)
	}
	for _, u := range []uuid.UUID{a, b} {
		if list, err := e.f.List(e.ctx, u); err != nil || len(list) != 1 {
			t.Errorf("%s sees %v (%v)", u, list, err)
		}
	}
	if list, _ := e.f.List(e.ctx, c); len(list) != 0 {
		t.Errorf("a third user sees %v", list)
	}
	// unsafe SQL as the third user: the policy, not the WHERE clause, keeps the pair hidden
	err = e.st.WithUser(e.ctx, c, func(_ *db.Queries, tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(e.ctx, `SELECT count(*) FROM friendships`).Scan(&n); err != nil || n != 0 {
			t.Errorf("third user counts %d friendships (%v)", n, err)
		}
		tag, err := tx.Exec(e.ctx, `DELETE FROM friendships`)
		if err != nil || tag.RowsAffected() != 0 {
			t.Errorf("third user deleted %d (%v)", tag.RowsAffected(), err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	lo, hi := a, b
	if hi.String() < lo.String() {
		lo, hi = hi, lo
	}
	err = e.st.WithUser(e.ctx, c, func(_ *db.Queries, tx pgx.Tx) error {
		_, err := tx.Exec(e.ctx, `INSERT INTO friendships (user_a, user_b) VALUES ($1, $2) ON CONFLICT DO NOTHING`, lo, hi)
		return err
	})
	if err == nil {
		t.Error("a third user inserted someone else's friendship")
	}
	if err := e.f.Remove(e.ctx, c, a); err == nil {
		t.Error("a third user removed a friendship")
	}
	// friends already
	again, _ := e.f.NewCode(e.ctx, b)
	if _, err := e.f.Redeem(e.ctx, a, again.Code); !errors.Is(err, friends.ErrAlready) {
		t.Errorf("second friendship: %v", err)
	}
	// deleting a user takes the friendship along
	if err := e.st.WithoutUser(e.ctx, func(_ *db.Queries, tx pgx.Tx) error {
		_, err := tx.Exec(e.ctx, `DELETE FROM users WHERE id = $1`, b)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if list, _ := e.f.List(e.ctx, a); len(list) != 0 {
		t.Errorf("friendship outlived the user: %v", list)
	}
}
