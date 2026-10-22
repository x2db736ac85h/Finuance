package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/auth"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
	"github.com/x2db736ac85h/finuance/backend/testutil"
)

var testDB *testutil.DB

func TestMain(m *testing.M) { testutil.Main(m, &testDB) }

// fakeClock is a settable clock for expiry tests.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) Add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

var cheap = auth.Params{MemoryKiB: 8 * 1024, Time: 1, Threads: 1}

func newService(t *testing.T) (*auth.Service, *fakeClock, *store.Store) {
	t.Helper()
	tdb := testutil.Require(t, testDB)
	st := store.New(tdb.App)
	clk := &fakeClock{t: time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)}
	return auth.New(st, clk, auth.Config{Params: cheap, SessionTTL: 30 * 24 * time.Hour, SessionIdleTTL: 7 * 24 * time.Hour, AllowSignup: true}), clk, st
}

var client = auth.Client{UserAgent: "test-agent", IP: netip.MustParseAddr("192.0.2.7")}

func email(t *testing.T) string { return strings.ReplaceAll(t.Name(), "/", "-") + "@example.test" }

const pw = "correct horse battery"

func signup(t *testing.T, s *auth.Service) (auth.User, string) {
	t.Helper()
	u, tok, err := s.Signup(context.Background(), auth.SignupInput{Email: email(t), Password: pw, DisplayName: " Alex "}, client)
	if err != nil {
		t.Fatal(err)
	}
	return u, tok
}

func TestSignupValidation(t *testing.T) {
	s, _, _ := newService(t)
	ctx := context.Background()
	for _, in := range []auth.SignupInput{
		{Email: "", Password: pw},
		{Email: "not-an-email", Password: pw},
		{Email: "Alex <alex@example.test>", Password: pw},
		{Email: "a@example.test", Password: "short"},
		{Email: "a@example.test", Password: strings.Repeat("x", auth.MaxPasswordBytes+1)},
		{Email: "a@example.test", Password: pw, DisplayName: strings.Repeat("ż", 81)},
	} {
		_, _, err := s.Signup(ctx, in, client)
		var ve *platform.ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("Signup(%+v) = %v, want a validation error", in, err)
		}
	}
	// 12 characters, multi-byte, no composition rules: fine.
	if _, _, err := s.Signup(ctx, auth.SignupInput{Email: "ok-" + email(t), Password: "żółwżółwżółw"}, client); err != nil {
		t.Errorf("12-character password: %v", err)
	}
}

func TestSignupAndResolve(t *testing.T) {
	s, _, _ := newService(t)
	ctx := context.Background()
	u, tok := signup(t, s)
	if u.DisplayName != "Alex" || u.Email != email(t) {
		t.Errorf("user = %+v", u)
	}
	id, err := s.Resolve(ctx, tok)
	if err != nil || id.User.ID != u.ID {
		t.Fatalf("resolve = %+v, %v", id, err)
	}
	// The same address in another case is taken.
	_, _, err = s.Signup(ctx, auth.SignupInput{Email: strings.ToUpper(email(t)), Password: pw}, client)
	if !errors.Is(err, platform.ErrConflict) {
		t.Errorf("duplicate signup: %v", err)
	}
	for _, bad := range []string{"", "x", tok + "A", strings.Repeat("A", 43)} {
		if _, err := s.Resolve(ctx, bad); !errors.Is(err, platform.ErrUnauthenticated) {
			t.Errorf("Resolve(%q) = %v", bad, err)
		}
	}
}

func TestSignupDisabled(t *testing.T) {
	tdb := testutil.Require(t, testDB)
	s := auth.New(store.New(tdb.App), platform.SystemClock{}, auth.Config{Params: cheap, SessionTTL: time.Hour, SessionIdleTTL: time.Hour})
	_, _, err := s.Signup(context.Background(), auth.SignupInput{Email: email(t), Password: pw}, client)
	if !errors.Is(err, platform.ErrForbidden) {
		t.Errorf("signup while disabled: %v", err)
	}
	// The admin path still works.
	if _, err := s.CreateUser(context.Background(), auth.SignupInput{Email: email(t), Password: pw}); err != nil {
		t.Errorf("admin create-user: %v", err)
	}
}

func TestLoginRotatesAndFailsUniformly(t *testing.T) {
	s, _, _ := newService(t)
	ctx := context.Background()
	u, first := signup(t, s)
	got, second, err := s.Login(ctx, strings.ToUpper(email(t)), pw, first, client)
	if err != nil || got.ID != u.ID {
		t.Fatalf("login = %+v, %v", got, err)
	}
	if second == first {
		t.Fatal("login reused the session token")
	}
	// Rotation: the session the browser had before logging in is gone.
	if _, err := s.Resolve(ctx, first); !errors.Is(err, platform.ErrUnauthenticated) {
		t.Errorf("pre-login session still valid: %v", err)
	}
	if _, err := s.Resolve(ctx, second); err != nil {
		t.Errorf("new session: %v", err)
	}
	_, _, errWrong := s.Login(ctx, email(t), pw+"!", "", client)
	_, _, errUnknown := s.Login(ctx, "nobody-"+email(t), pw, "", client)
	if !errors.Is(errWrong, auth.ErrInvalidCredentials) || !errors.Is(errUnknown, auth.ErrInvalidCredentials) || errWrong.Error() != errUnknown.Error() {
		t.Errorf("wrong password %v / unknown email %v: want the same error", errWrong, errUnknown)
	}
}

// TestUnknownEmailRunsArgon2: the failure path for an unknown email costs a full hash, like a
// wrong password does (constant-time failure path, §8.1).
func TestUnknownEmailRunsArgon2(t *testing.T) {
	tdb := testutil.Require(t, testDB)
	slow := auth.Params{MemoryKiB: 64 * 1024, Time: 3, Threads: 1} // ~250 ms per hash
	s := auth.New(store.New(tdb.App), platform.SystemClock{}, auth.Config{Params: slow, SessionTTL: time.Hour, SessionIdleTTL: time.Hour, AllowSignup: true})
	ctx := context.Background()
	if _, _, err := s.Signup(ctx, auth.SignupInput{Email: email(t), Password: pw}, client); err != nil {
		t.Fatal(err)
	}
	measure := func(e string) time.Duration {
		start := time.Now()
		_, _, _ = s.Login(ctx, e, "wrong password!", "", client)
		return time.Since(start)
	}
	known, unknown := measure(email(t)), measure("nobody-"+email(t))
	if unknown < known/3 {
		t.Errorf("unknown email took %v, wrong password %v: the dummy hash isn't verified", unknown, known)
	}
}

func TestLoginUpgradesOldHashes(t *testing.T) {
	s, _, st := newService(t)
	ctx := context.Background()
	old := auth.Params{MemoryKiB: 8 * 1024, Time: 2, Threads: 1}
	phc, _ := auth.Hash(pw, old)
	u := platform.NewID()
	if err := st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		_, err := q.CreateUser(ctx, db.CreateUserParams{ID: u, Email: email(t), PasswordHash: phc})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Login(ctx, email(t), pw, "", client); err != nil {
		t.Fatal(err)
	}
	var now string
	_ = st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		row, err := q.GetUser(ctx, u)
		now = row.PasswordHash
		return err
	})
	if auth.NeedsRehash(now, cheap) || now == phc {
		t.Errorf("hash not upgraded: %s", now)
	}
}

func TestSessionExpiry(t *testing.T) {
	s, clk, _ := newService(t)
	ctx := context.Background()
	_, tok := signup(t, s)
	// Idle: 7 days without a request.
	clk.Add(7*24*time.Hour + time.Minute)
	if _, err := s.Resolve(ctx, tok); !errors.Is(err, platform.ErrUnauthenticated) {
		t.Errorf("idle session: %v", err)
	}
	// Active every 6 days: alive until the absolute 30 days, then gone.
	_, _, err := s.Login(ctx, email(t), pw, "", client)
	if err != nil {
		t.Fatal(err)
	}
	_, tok, _ = s.Login(ctx, email(t), pw, "", client)
	for day := 6; day <= 30; day += 6 {
		clk.Add(6 * 24 * time.Hour)
		_, err := s.Resolve(ctx, tok)
		if day < 30 && err != nil {
			t.Fatalf("day %d: active session expired: %v", day, err)
		}
		if day == 30 && !errors.Is(err, platform.ErrUnauthenticated) {
			t.Errorf("day %d: session past the absolute expiry: %v", day, err)
		}
	}
}

func TestLastSeenIsThrottled(t *testing.T) {
	s, clk, _ := newService(t)
	ctx := context.Background()
	_, tok := signup(t, s)
	id, _ := s.Resolve(ctx, tok)
	lastSeen := func() time.Time {
		list, err := s.Sessions(ctx, id)
		if err != nil || len(list) != 1 {
			t.Fatalf("sessions = %v, %v", list, err)
		}
		return list[0].LastSeenAt
	}
	start := lastSeen()
	clk.Add(4 * time.Minute)
	_, _ = s.Resolve(ctx, tok)
	if got := lastSeen(); !got.Equal(start) {
		t.Errorf("last_seen_at written after 4 minutes: %v", got)
	}
	clk.Add(2 * time.Minute)
	_, _ = s.Resolve(ctx, tok)
	if got := lastSeen(); !got.Equal(clk.Now()) {
		t.Errorf("last_seen_at not written after 6 minutes: %v, want %v", got, clk.Now())
	}
}

func TestSessionsListAndRevoke(t *testing.T) {
	s, _, _ := newService(t)
	ctx := context.Background()
	_, a := signup(t, s)
	_, b, _ := s.Login(ctx, email(t), pw, "", auth.Client{UserAgent: "phone"})
	_, c, _ := s.Login(ctx, email(t), pw, "", auth.Client{UserAgent: "tablet"})
	idA, _ := s.Resolve(ctx, a)
	list, err := s.Sessions(ctx, idA)
	if err != nil || len(list) != 3 {
		t.Fatalf("sessions = %+v, %v", list, err)
	}
	current := 0
	for _, x := range list {
		if x.Current {
			current++
			if x.UserAgent != "test-agent" || x.IP != "192.0.2.7" {
				t.Errorf("current session = %+v", x)
			}
		}
		if len(x.ID) != 16 {
			t.Errorf("session id %q is not 8 bytes of hex", x.ID)
		}
	}
	if current != 1 {
		t.Errorf("%d sessions marked current", current)
	}
	idB, _ := s.Resolve(ctx, b)
	var bID string
	for _, x := range list {
		if x.UserAgent == "phone" {
			bID = x.ID
		}
	}
	if err := s.RevokeSession(ctx, idA, bID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Resolve(ctx, b); !errors.Is(err, platform.ErrUnauthenticated) {
		t.Errorf("revoked session still valid: %v", err)
	}
	if err := s.RevokeSession(ctx, idB, bID); !errors.Is(err, platform.ErrNotFound) {
		t.Errorf("revoking twice: %v", err)
	}
	if err := s.RevokeSession(ctx, idA, "zz"); !errors.Is(err, platform.ErrNotFound) {
		t.Errorf("malformed id: %v", err)
	}
	// Another user can't revoke my sessions.
	other, _ := func() (auth.Identity, error) {
		_, tok, err := s.Signup(ctx, auth.SignupInput{Email: "other-" + email(t), Password: pw}, client)
		if err != nil {
			t.Fatal(err)
		}
		return s.Resolve(ctx, tok)
	}()
	list, _ = s.Sessions(ctx, idA)
	for _, x := range list {
		if err := s.RevokeSession(ctx, other, x.ID); !errors.Is(err, platform.ErrNotFound) {
			t.Errorf("other user revoked %s: %v", x.ID, err)
		}
	}
	if n, err := s.RevokeOtherSessions(ctx, idA); err != nil || n != 1 {
		t.Errorf("revoke others = %d, %v", n, err)
	}
	if _, err := s.Resolve(ctx, c); !errors.Is(err, platform.ErrUnauthenticated) {
		t.Errorf("other session survived: %v", err)
	}
	if _, err := s.Resolve(ctx, a); err != nil {
		t.Errorf("current session revoked: %v", err)
	}
	if err := s.Logout(ctx, a); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Resolve(ctx, a); !errors.Is(err, platform.ErrUnauthenticated) {
		t.Errorf("session after logout: %v", err)
	}
	if err := s.Logout(ctx, "garbage"); err != nil {
		t.Errorf("logout with a garbage cookie: %v", err)
	}
}

func TestChangePasswordRotatesEverything(t *testing.T) {
	s, _, _ := newService(t)
	ctx := context.Background()
	_, a := signup(t, s)
	_, b, _ := s.Login(ctx, email(t), pw, "", client)
	id, _ := s.Resolve(ctx, a)
	var ve *platform.ValidationError
	if _, err := s.ChangePassword(ctx, id, "wrong current", "new password 123", client); !errors.As(err, &ve) || ve.Fields[0].Field != "currentPassword" {
		t.Errorf("wrong current password: %v", err)
	}
	if _, err := s.ChangePassword(ctx, id, pw, "short", client); !errors.As(err, &ve) || ve.Fields[0].Field != "newPassword" {
		t.Errorf("short new password: %v", err)
	}
	fresh, err := s.ChangePassword(ctx, id, pw, "new password 123", client)
	if err != nil {
		t.Fatal(err)
	}
	for _, old := range []string{a, b} {
		if _, err := s.Resolve(ctx, old); !errors.Is(err, platform.ErrUnauthenticated) {
			t.Errorf("old session survived the password change: %v", err)
		}
	}
	if _, err := s.Resolve(ctx, fresh); err != nil {
		t.Errorf("fresh session: %v", err)
	}
	if _, _, err := s.Login(ctx, email(t), pw, "", client); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("old password still works: %v", err)
	}
	if _, _, err := s.Login(ctx, email(t), "new password 123", "", client); err != nil {
		t.Errorf("new password: %v", err)
	}
	// Admin reset revokes everything too.
	if err := s.SetPassword(ctx, email(t), "admin set password"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Resolve(ctx, fresh); !errors.Is(err, platform.ErrUnauthenticated) {
		t.Errorf("session survived the admin reset: %v", err)
	}
	if err := s.SetPassword(ctx, "nobody-"+email(t), "admin set password"); !errors.Is(err, platform.ErrNotFound) {
		t.Errorf("reset of an unknown user: %v", err)
	}
}

func TestPreferences(t *testing.T) {
	s, _, _ := newService(t)
	ctx := context.Background()
	_, tok := signup(t, s)
	id, _ := s.Resolve(ctx, tok)
	p, err := s.GetPreferences(ctx, id)
	if err != nil || p.Version != 0 || string(p.Prefs) != `{}` {
		t.Fatalf("empty prefs = %+v, %v", p, err)
	}
	v, err := s.SavePreferences(ctx, id, 0, json.RawMessage(`{"theme":"dark"}`))
	if err != nil || v != 1 {
		t.Fatalf("first save = %d, %v", v, err)
	}
	// A second client still at version 0 loses, and gets the current document back.
	_, err = s.SavePreferences(ctx, id, 0, json.RawMessage(`{"theme":"light"}`))
	var stale *auth.StaleVersionError
	if !errors.As(err, &stale) || !errors.Is(err, platform.ErrStaleVersion) || stale.Current.Version != 1 || !strings.Contains(string(stale.Current.Prefs), "dark") {
		t.Errorf("stale save = %v", err)
	}
	if v, err := s.SavePreferences(ctx, id, 1, json.RawMessage(`{"theme":"light"}`)); err != nil || v != 2 {
		t.Errorf("second save = %d, %v", v, err)
	}
	for _, bad := range []string{`[]`, `"x"`, `null`, `{`} {
		var ve *platform.ValidationError
		if _, err := s.SavePreferences(ctx, id, 2, json.RawMessage(bad)); !errors.As(err, &ve) {
			t.Errorf("prefs %s: %v", bad, err)
		}
	}
	big := json.RawMessage(`{"x":"` + strings.Repeat("a", auth.MaxPreferencesBytes) + `"}`)
	var ve *platform.ValidationError
	if _, err := s.SavePreferences(ctx, id, 2, big); !errors.As(err, &ve) {
		t.Errorf("oversized prefs: %v", err)
	}
}

func TestDeleteAccountNeedsPassword(t *testing.T) {
	s, _, _ := newService(t)
	ctx := context.Background()
	_, tok := signup(t, s)
	id, _ := s.Resolve(ctx, tok)
	var ve *platform.ValidationError
	if err := s.DeleteAccount(ctx, id, "wrong password"); !errors.As(err, &ve) {
		t.Errorf("wrong password: %v", err)
	}
	if err := s.DeleteAccount(ctx, id, pw); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Resolve(ctx, tok); !errors.Is(err, platform.ErrUnauthenticated) {
		t.Errorf("session after deleting the account: %v", err)
	}
	if _, _, err := s.Login(ctx, email(t), pw, "", client); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("login after deleting the account: %v", err)
	}
}

func TestCleanupExpiredSessions(t *testing.T) {
	s, clk, _ := newService(t)
	ctx := context.Background()
	_, tok := signup(t, s)
	if n, _ := s.CleanupExpiredSessions(ctx); n != 0 {
		// Other tests' sessions are in the same database but younger than the idle timeout.
		t.Logf("cleanup removed %d sessions of other tests", n)
	}
	clk.Add(8 * 24 * time.Hour)
	if n, err := s.CleanupExpiredSessions(ctx); err != nil || n < 1 {
		t.Errorf("cleanup = %d, %v", n, err)
	}
	if _, err := s.Resolve(ctx, tok); !errors.Is(err, platform.ErrUnauthenticated) {
		t.Errorf("session after cleanup: %v", err)
	}
}
