package auth_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/auth"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/secret"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
	"github.com/x2db736ac85h/finuance/backend/testutil"
)

func newServiceWithKey(t *testing.T) (*auth.Service, *fakeClock, *store.Store) {
	t.Helper()
	tdb := testutil.Require(t, testDB)
	st := store.New(tdb.App)
	clk := &fakeClock{t: time.Date(2026, 11, 19, 9, 0, 10, 0, time.UTC)}
	box, err := secret.New([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	return auth.New(st, clk, auth.Config{Params: cheap, SessionTTL: 30 * 24 * time.Hour, SessionIdleTTL: 7 * 24 * time.Hour,
		AllowSignup: true, Box: box, Issuer: "Finuance test"}), clk, st
}

func identity(t *testing.T, s *auth.Service, token string) auth.Identity {
	t.Helper()
	id, err := s.Resolve(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// enrol sets two-step sign-in up for a new user and returns the secret and the recovery codes.
func enrol(t *testing.T, s *auth.Service, clk *fakeClock) (auth.Identity, string, []string) {
	t.Helper()
	ctx := context.Background()
	_, tok := signup(t, s)
	id := identity(t, s, tok)
	e, err := s.StartSecondFactor(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	codes, err := s.ConfirmSecondFactor(ctx, id, auth.CodeAt(e.Secret, clk.Now()))
	if err != nil {
		t.Fatal(err)
	}
	clk.Add(31 * time.Second) // the confirming code's step is spent
	return id, e.Secret, codes
}

// challengeOf signs in with the password and returns the challenge.
func challengeOf(t *testing.T, s *auth.Service) string {
	t.Helper()
	_, tok, err := s.Login(context.Background(), email(t), pw, "", client)
	var second *auth.SecondStepRequiredError
	if !errors.As(err, &second) || tok != "" || second.Challenge == "" {
		t.Fatalf("login with a second factor: token %q, err %v", tok, err)
	}
	return second.Challenge
}

func isValidation(err error, field string) bool {
	var ve *platform.ValidationError
	return errors.As(err, &ve) && strings.Contains(ve.Error(), field)
}

func TestSecondFactorEnrolment(t *testing.T) {
	s, clk, st := newServiceWithKey(t)
	ctx := context.Background()
	u, tok := signup(t, s)
	id := identity(t, s, tok)
	other, _, err := s.Login(ctx, email(t), pw, "", client) // a second session of the same user
	_ = other
	if err != nil {
		t.Fatal(err)
	}

	if f, err := s.SecondFactorOf(ctx, u.ID); err != nil || f.Enabled {
		t.Fatalf("before: %+v %v", f, err)
	}
	if _, err := s.ConfirmSecondFactor(ctx, id, "123456"); !errors.Is(err, platform.ErrConflict) {
		t.Errorf("confirm before start: %v", err)
	}
	e1, err := s.StartSecondFactor(ctx, id)
	if err != nil || len(e1.Secret) != 32 || !strings.HasPrefix(e1.URI, "otpauth://totp/Finuance%20test:") || !strings.Contains(e1.URI, "secret="+e1.Secret) {
		t.Fatalf("start: %+v %v", e1, err)
	}
	// started but not confirmed: not in force, sign-in is as before, and starting again replaces it
	if f, _ := s.SecondFactorOf(ctx, u.ID); f.Enabled {
		t.Error("an unconfirmed enrolment is in force")
	}
	if _, tok2, err := s.Login(ctx, email(t), pw, "", client); err != nil || tok2 == "" {
		t.Errorf("login during enrolment: %v", err)
	}
	e2, err := s.StartSecondFactor(ctx, id)
	if err != nil || e2.Secret == e1.Secret {
		t.Fatalf("restart: %v", err)
	}
	if _, err := s.ConfirmSecondFactor(ctx, id, auth.CodeAt(e1.Secret, clk.Now())); !isValidation(err, "code") {
		t.Errorf("the replaced secret's code: %v", err)
	}
	if _, err := s.ConfirmSecondFactor(ctx, id, "000000"); !isValidation(err, "code") && auth.CodeAt(e2.Secret, clk.Now()) != "000000" {
		t.Errorf("a wrong code: %v", err)
	}
	// the stored secret is sealed: neither the text nor its bytes are in the row
	var sealed []byte
	_ = st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		f, err := q.GetSecondFactor(ctx, u.ID)
		sealed = f.SecretSealed
		return err
	})
	if len(sealed) < 40 || strings.Contains(string(sealed), e2.Secret) {
		t.Errorf("the secret is stored unsealed (%d bytes)", len(sealed))
	}

	codes, err := s.ConfirmSecondFactor(ctx, id, auth.CodeAt(e2.Secret, clk.Now()))
	if err != nil || len(codes) != auth.RecoveryCodes {
		t.Fatalf("confirm: %v %v", codes, err)
	}
	f, err := s.SecondFactorOf(ctx, u.ID)
	if err != nil || !f.Enabled || f.RecoveryCodes != auth.RecoveryCodes || f.ConfirmedAt == nil {
		t.Errorf("after: %+v %v", f, err)
	}
	// the confirming session stays, the others end
	if _, err := s.Resolve(ctx, tok); err != nil {
		t.Errorf("the confirming session ended: %v", err)
	}
	if _, err := s.Resolve(ctx, other.Email); err == nil {
		t.Error("garbage resolved")
	}
	if ss, _ := s.Sessions(ctx, id); len(ss) != 1 {
		t.Errorf("%d sessions after confirming, want 1", len(ss))
	}
	// in force: it can't be started or confirmed again
	if _, err := s.StartSecondFactor(ctx, id); !errors.Is(err, platform.ErrConflict) {
		t.Errorf("start while on: %v", err)
	}
	if _, err := s.ConfirmSecondFactor(ctx, id, auth.CodeAt(e2.Secret, clk.Now())); !errors.Is(err, platform.ErrConflict) {
		t.Errorf("confirm while on: %v", err)
	}
}

func TestSecondFactorNeedsTheInstanceKeyAndARealAccount(t *testing.T) {
	s, _, _ := newService(t) // no instance key
	_, tok := signup(t, s)
	if _, err := s.StartSecondFactor(context.Background(), identity(t, s, tok)); !errors.Is(err, platform.ErrConflict) || !strings.Contains(err.Error(), "FINUANCE_INSTANCE_KEY") {
		t.Errorf("without a key: %v", err)
	}
	sk, _, _ := newServiceWithKey(t)
	id := identity(t, s, tok)
	id.User.IsDemo = true
	if _, err := sk.StartSecondFactor(context.Background(), id); !errors.Is(err, platform.ErrForbidden) {
		t.Errorf("a demo account: %v", err)
	}
}

func TestSecondStepSignIn(t *testing.T) {
	s, clk, _ := newServiceWithKey(t)
	ctx := context.Background()
	id, sec, _ := enrol(t, s, clk)

	// the password alone starts no session
	ch := challengeOf(t, s)
	if ss, _ := s.Sessions(ctx, id); len(ss) != 1 {
		t.Errorf("%d sessions after the password alone, want the enrolling one only", len(ss))
	}
	// a wrong password still fails as before, without a challenge
	if _, _, err := s.Login(ctx, email(t), "wrong password here", "", client); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("wrong password: %v", err)
	}

	// a wrong code, then the right one
	var failed *auth.FailedSecondStepError
	if _, _, _, err := s.CompleteLogin(ctx, ch, "000000", "", client); !errors.As(err, &failed) || failed.UserID != id.User.ID || !errors.Is(err, platform.ErrUnauthenticated) {
		if auth.CodeAt(sec, clk.Now()) != "000000" {
			t.Errorf("a wrong code: %v", err)
		}
	}
	code := auth.CodeAt(sec, clk.Now())
	u, tok, recovery, err := s.CompleteLogin(ctx, ch, code, "", client)
	if err != nil || tok == "" || recovery || u.ID != id.User.ID {
		t.Fatalf("the right code: %v", err)
	}
	if got, err := s.Resolve(ctx, tok); err != nil || got.User.ID != id.User.ID {
		t.Errorf("the new session: %v", err)
	}
	// the challenge is gone, and the same code is refused for a new one (replay)
	if _, _, _, err := s.CompleteLogin(ctx, ch, code, "", client); !errors.Is(err, auth.ErrInvalidSecondStep) {
		t.Errorf("the answered challenge again: %v", err)
	}
	ch = challengeOf(t, s)
	if _, _, _, err := s.CompleteLogin(ctx, ch, code, "", client); !errors.As(err, &failed) {
		t.Errorf("the same code for a new challenge: %v", err)
	}
	// the next step's code works: drift of one step is allowed
	if _, _, _, err := s.CompleteLogin(ctx, ch, auth.CodeAt(sec, clk.Now().Add(30*time.Second)), "", client); err != nil {
		t.Errorf("the next step's code: %v", err)
	}
	// a code two steps away does not
	clk.Add(2 * time.Minute)
	ch = challengeOf(t, s)
	if _, _, _, err := s.CompleteLogin(ctx, ch, auth.CodeAt(sec, clk.Now().Add(-61*time.Second)), "", client); !errors.As(err, &failed) {
		t.Errorf("a code two steps old: %v", err)
	}
}

func TestChallengeLimits(t *testing.T) {
	s, clk, _ := newServiceWithKey(t)
	ctx := context.Background()
	_, sec, _ := enrol(t, s, clk)

	// five attempts per challenge: the sixth is refused even with the right code
	ch := challengeOf(t, s)
	for i := range 5 {
		var failed *auth.FailedSecondStepError
		if _, _, _, err := s.CompleteLogin(ctx, ch, "00000"+string(rune('1'+i)), "", client); !errors.As(err, &failed) {
			t.Fatalf("attempt %d: %v", i+1, err)
		}
	}
	if _, _, _, err := s.CompleteLogin(ctx, ch, auth.CodeAt(sec, clk.Now()), "", client); !errors.Is(err, auth.ErrInvalidSecondStep) || errors.As(err, new(*auth.FailedSecondStepError)) {
		t.Errorf("after five attempts: %v", err)
	}
	if _, err := s.ChallengeUser(ctx, ch); !errors.Is(err, auth.ErrInvalidSecondStep) {
		t.Errorf("a used-up challenge still names its user: %v", err)
	}
	// a new sign-in replaces the earlier challenge
	first := challengeOf(t, s)
	second := challengeOf(t, s)
	if _, _, _, err := s.CompleteLogin(ctx, first, auth.CodeAt(sec, clk.Now()), "", client); !errors.Is(err, auth.ErrInvalidSecondStep) {
		t.Errorf("the replaced challenge: %v", err)
	}
	if uid, err := s.ChallengeUser(ctx, second); err != nil || uid.String() == "" {
		t.Errorf("the live challenge: %v", err)
	}
	// it expires
	clk.Add(auth.ChallengeTTL + time.Second)
	if _, _, _, err := s.CompleteLogin(ctx, second, auth.CodeAt(sec, clk.Now()), "", client); !errors.Is(err, auth.ErrInvalidSecondStep) {
		t.Errorf("an expired challenge: %v", err)
	}
	if n, err := s.CleanupExpiredChallenges(ctx); err != nil || n < 1 {
		t.Errorf("cleanup: %d %v", n, err)
	}
	// garbage never reaches the database
	for _, bad := range []string{"", "x", strings.Repeat("A", 43), "../../etc"} {
		if _, _, _, err := s.CompleteLogin(ctx, bad, "123456", "", client); !errors.Is(err, auth.ErrInvalidSecondStep) {
			t.Errorf("challenge %q: %v", bad, err)
		}
	}
}

func TestRecoveryCodesAndSwitchingOff(t *testing.T) {
	s, clk, _ := newServiceWithKey(t)
	ctx := context.Background()
	id, sec, codes := enrol(t, s, clk)

	// a recovery code signs in once
	ch := challengeOf(t, s)
	if _, tok, recovery, err := s.CompleteLogin(ctx, ch, strings.ToUpper(codes[0]), "", client); err != nil || tok == "" || !recovery {
		t.Fatalf("a recovery code: %v", err)
	}
	ch = challengeOf(t, s)
	if _, _, _, err := s.CompleteLogin(ctx, ch, codes[0], "", client); !errors.As(err, new(*auth.FailedSecondStepError)) {
		t.Errorf("the same recovery code again: %v", err)
	}
	if f, _ := s.SecondFactorOf(ctx, id.User.ID); f.RecoveryCodes != auth.RecoveryCodes-1 {
		t.Errorf("%d recovery codes left", f.RecoveryCodes)
	}

	// new recovery codes need a code from the app, not a recovery code; the old ones stop working
	if _, err := s.NewRecoveryCodesFor(ctx, id, codes[1]); !isValidation(err, "code") {
		t.Errorf("new codes with a recovery code: %v", err)
	}
	fresh, err := s.NewRecoveryCodesFor(ctx, id, auth.CodeAt(sec, clk.Now()))
	if err != nil || len(fresh) != auth.RecoveryCodes || fresh[0] == codes[1] {
		t.Fatalf("new codes: %v", err)
	}
	clk.Add(31 * time.Second)
	ch = challengeOf(t, s)
	if _, _, _, err := s.CompleteLogin(ctx, ch, codes[1], "", client); !errors.As(err, new(*auth.FailedSecondStepError)) {
		t.Errorf("a replaced recovery code: %v", err)
	}

	// switching off needs the password and a code
	if err := s.DisableSecondFactor(ctx, id, "not the password!!", auth.CodeAt(sec, clk.Now())); !isValidation(err, "password") {
		t.Errorf("disable with a wrong password: %v", err)
	}
	if err := s.DisableSecondFactor(ctx, id, pw, "000000"); !isValidation(err, "code") && auth.CodeAt(sec, clk.Now()) != "000000" {
		t.Errorf("disable with a wrong code: %v", err)
	}
	if err := s.DisableSecondFactor(ctx, id, pw, fresh[0]); err != nil {
		t.Fatalf("disable with a recovery code: %v", err)
	}
	if f, _ := s.SecondFactorOf(ctx, id.User.ID); f.Enabled || f.RecoveryCodes != 0 {
		t.Errorf("after switching off: %+v", f)
	}
	if _, tok, err := s.Login(ctx, email(t), pw, "", client); err != nil || tok == "" {
		t.Errorf("sign-in after switching off: %v", err)
	}
	if _, _, _, err := s.CompleteLogin(ctx, ch, fresh[1], "", client); !errors.Is(err, auth.ErrInvalidSecondStep) {
		t.Errorf("a challenge from before switching off: %v", err)
	}
	if err := s.RemoveSecondFactor(ctx, id.User.ID); !errors.Is(err, platform.ErrNotFound) {
		t.Errorf("remove when off: %v", err)
	}
}

// TestSecondFactorIsPerUser: one user's code, recovery code or challenge does nothing for another.
func TestSecondFactorIsPerUser(t *testing.T) {
	s, clk, st := newServiceWithKey(t)
	ctx := context.Background()
	_, secA, codesA := enrol(t, s, clk)
	// a second user with their own factor
	_, tokB, err := s.Signup(ctx, auth.SignupInput{Email: "b-" + email(t), Password: pw}, client)
	if err != nil {
		t.Fatal(err)
	}
	idB := identity(t, s, tokB)
	eB, err := s.StartSecondFactor(ctx, idB)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConfirmSecondFactor(ctx, idB, auth.CodeAt(secA, clk.Now())); !isValidation(err, "code") {
		t.Errorf("confirming B's factor with A's code: %v", err)
	}
	if _, err := s.ConfirmSecondFactor(ctx, idB, auth.CodeAt(eB.Secret, clk.Now())); err != nil {
		t.Fatal(err)
	}
	clk.Add(31 * time.Second)
	_, _, err = s.Login(ctx, "b-"+email(t), pw, "", client)
	var second *auth.SecondStepRequiredError
	if !errors.As(err, &second) {
		t.Fatal(err)
	}
	for name, code := range map[string]string{"A's one-time code": auth.CodeAt(secA, clk.Now()), "A's recovery code": codesA[0]} {
		if _, _, _, err := s.CompleteLogin(ctx, second.Challenge, code, "", client); !errors.As(err, new(*auth.FailedSecondStepError)) {
			t.Errorf("B's challenge answered with %s: %v", name, err)
		}
	}
	// A's recovery code is still unused for A
	chA := challengeOf(t, s)
	if _, _, recovery, err := s.CompleteLogin(ctx, chA, codesA[0], "", client); err != nil || !recovery {
		t.Errorf("A's own recovery code after B tried it: %v", err)
	}
	// a sealed secret copied to another user's row doesn't open there
	err = st.WithoutUser(ctx, func(q *db.Queries, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE user_second_factors SET secret_sealed = (SELECT secret_sealed FROM user_second_factors WHERE user_id <> $1 AND confirmed_at IS NOT NULL LIMIT 1) WHERE user_id = $1`, idB.User.ID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.CompleteLogin(ctx, second.Challenge, auth.CodeAt(secA, clk.Now()), "", client); !errors.Is(err, secret.ErrOpen) {
		t.Errorf("a copied secret: %v", err)
	}
}
