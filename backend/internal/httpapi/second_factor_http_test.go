package httpapi

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // RFC 6238, as an authenticator app computes it
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/config"
)

// appCode is the code an authenticator app shows for a secret at a time, computed here from the
// RFC and not with the server's code, so the test is a second implementation.
func appCode(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		t.Fatal(err)
	}
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(at.Unix()/30)) //nolint:gosec // a time after 1970
	mac := hmac.New(sha1.New, key)
	mac.Write(counter[:])
	sum := mac.Sum(nil)
	off := sum[19] & 0x0f
	return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(sum[off:off+4])&0x7fffffff)%1_000_000)
}

func secondFactorEnv(t *testing.T) *authEnv {
	return newTestEnv(t, func(c *config.Config) { c.InstanceKey = bytes.Repeat([]byte{7}, 32) }, false)
}

// enrolHTTP sets two-step sign-in up through the API and returns the secret and recovery codes.
func enrolHTTP(t *testing.T, e *authEnv, b *browser) (string, []string) {
	t.Helper()
	r := b.do("POST", "/me/second-factor", nil)
	expect(t, r, 200, "")
	secret := r.body["secret"].(string)
	if !strings.HasPrefix(r.body["uri"].(string), "otpauth://totp/Finuance%20test:") {
		t.Fatalf("uri %v", r.body["uri"])
	}
	r = b.do("POST", "/me/second-factor/confirm", map[string]any{"code": appCode(t, secret, e.clock.Now())})
	expect(t, r, 200, "")
	var codes []string
	for _, c := range r.body["recoveryCodes"].([]any) {
		codes = append(codes, c.(string))
	}
	e.clock.Add(31 * time.Second) // the confirming code's step is spent
	return secret, codes
}

func TestSecondFactorHTTP(t *testing.T) {
	e := secondFactorEnv(t)
	mail := testEmail(t)
	b := signupBrowser(t, e, mail)
	other := e.browser() // a second session of the same account
	expect(t, other.do("POST", "/auth/login", map[string]any{"email": mail, "password": password}), 200, "")

	r := b.do("GET", "/me/second-factor", nil)
	expect(t, r, 200, "")
	if r.body["enabled"] != false || r.body["available"] != true || r.body["recoveryCodes"] != float64(0) {
		t.Fatalf("before: %s", r.raw)
	}
	if me := b.do("GET", "/me", nil); me.body["secondFactor"] != false || me.body["secondFactorRequired"] != false {
		t.Errorf("/me before: %s", me.raw)
	}
	// a wrong code confirms nothing
	r = b.do("POST", "/me/second-factor", nil)
	expect(t, r, 200, "")
	expect(t, b.do("POST", "/me/second-factor/confirm", map[string]any{"code": "12"}), 422, "validation_failed")
	secret, codes := enrolHTTP(t, e, b)
	if len(codes) != 10 {
		t.Fatalf("%d recovery codes", len(codes))
	}
	if me := b.do("GET", "/me", nil); me.body["secondFactor"] != true {
		t.Errorf("/me after: %s", me.raw)
	}
	expect(t, other.do("GET", "/me", nil), 401, "unauthenticated") // sessions from before end
	expect(t, b.do("POST", "/me/second-factor", nil), 409, "conflict")

	// signing in: the password gives a challenge and no cookie
	c := e.browser()
	r = c.do("POST", "/auth/login", map[string]any{"email": mail, "password": password})
	expect(t, r, 202, "")
	challenge := r.body["challenge"].(string)
	if c.cookie() != "" || challenge == "" || r.body["expiresAt"] == nil || r.body["email"] != nil {
		t.Fatalf("202 answer: cookie %q body %s", c.cookie(), r.raw)
	}
	expect(t, c.do("GET", "/me", nil), 401, "unauthenticated")
	// a wrong code, an unknown challenge, then the right code
	expect(t, c.do("POST", "/auth/login/second-step", map[string]any{"challenge": challenge, "code": "000000"}), 401, "unauthenticated")
	expect(t, c.do("POST", "/auth/login/second-step", map[string]any{"challenge": "nope", "code": "000000"}), 401, "unauthenticated")
	code := appCode(t, secret, e.clock.Now())
	r = c.do("POST", "/auth/login/second-step", map[string]any{"challenge": challenge, "code": code[:3] + " " + code[3:]})
	expect(t, r, 200, "")
	if r.body["email"] != mail || r.body["secondFactor"] != true || c.cookie() == "" {
		t.Fatalf("second step: %s", r.raw)
	}
	expect(t, c.do("GET", "/me", nil), 200, "")
	// the same code again (another browser, a new challenge) is refused: replay
	d := e.browser()
	r = d.do("POST", "/auth/login", map[string]any{"email": mail, "password": password})
	expect(t, r, 202, "")
	expect(t, d.do("POST", "/auth/login/second-step", map[string]any{"challenge": r.body["challenge"], "code": code}), 401, "unauthenticated")
	// a recovery code works once
	expect(t, d.do("POST", "/auth/login/second-step", map[string]any{"challenge": r.body["challenge"], "code": codes[0]}), 200, "")
	r = d.do("POST", "/auth/login", map[string]any{"email": mail, "password": password})
	expect(t, d.do("POST", "/auth/login/second-step", map[string]any{"challenge": r.body["challenge"], "code": codes[0]}), 401, "unauthenticated")
	if sf := b.do("GET", "/me/second-factor", nil); sf.body["recoveryCodes"] != float64(9) {
		t.Errorf("recovery codes left: %s", sf.raw)
	}

	// the activity log: on, the failed steps, the recovery code; never a code or the secret
	acts := b.do("GET", "/me/activity?limit=50", nil)
	expect(t, acts, 200, "")
	for _, want := range []string{"auth.second_factor_enabled", "auth.second_step_failed", "auth.recovery_code_used"} {
		if !strings.Contains(acts.raw, want) {
			t.Errorf("activity lacks %s", want)
		}
	}
	for _, secretText := range []string{secret, codes[0], codes[1], code} {
		if strings.Contains(acts.raw, secretText) {
			t.Errorf("the activity log carries %q", secretText)
		}
	}

	// new recovery codes need a code from the app
	expect(t, b.do("POST", "/me/second-factor/recovery-codes", map[string]any{"code": codes[1]}), 422, "validation_failed")
	e.clock.Add(31 * time.Second)
	r = b.do("POST", "/me/second-factor/recovery-codes", map[string]any{"code": appCode(t, secret, e.clock.Now())})
	expect(t, r, 200, "")
	fresh := r.body["recoveryCodes"].([]any)[0].(string)

	// switching off: the password and a code
	expect(t, b.do("POST", "/me/second-factor/disable", map[string]any{"password": "not the password", "code": fresh}), 422, "validation_failed")
	expect(t, b.do("POST", "/me/second-factor/disable", map[string]any{"password": password, "code": codes[2]}), 422, "validation_failed") // replaced
	expect(t, b.do("POST", "/me/second-factor/disable", map[string]any{"password": password, "code": fresh}), 204, "")
	expect(t, e.browser().do("POST", "/auth/login", map[string]any{"email": mail, "password": password}), 200, "")
}

// TestSecondFactorNeedsASession: no route of it is reachable signed out, and an instance without
// the instance key says so instead of storing an unsealed secret.
func TestSecondFactorNeedsASessionAndTheKey(t *testing.T) {
	e := secondFactorEnv(t)
	anon := e.browser()
	for _, route := range [][2]string{{"GET", "/me/second-factor"}, {"POST", "/me/second-factor"}, {"POST", "/me/second-factor/confirm"},
		{"POST", "/me/second-factor/recovery-codes"}, {"POST", "/me/second-factor/disable"}} {
		var body any
		if route[0] == "POST" && route[1] != "/me/second-factor" {
			body = map[string]any{"code": "123456", "password": password}
		}
		expect(t, anon.do(route[0], route[1], body), 401, "unauthenticated")
	}
	plain := newAuthEnv(t, config.EnvTest, false) // no instance key
	b := signupBrowser(t, plain, "nokey-"+testEmail(t))
	if r := b.do("GET", "/me/second-factor", nil); r.body["available"] != false {
		t.Errorf("available without a key: %s", r.raw)
	}
	expect(t, b.do("POST", "/me/second-factor", nil), 409, "conflict")
}

// TestAdministratorsNeedSecondFactor: the instance rule. Only an administrator who uses two-step
// sign-in can switch it on; then an administrator without it keeps their own data and the security
// settings but gets 403 on every administration route until they set it up; an administrator can
// remove another account's second factor, never their own that way.
func TestAdministratorsNeedSecondFactor(t *testing.T) {
	e := secondFactorEnv(t)
	alice, _ := e.adminBrowser(t, "alice-"+testEmail(t))
	bob, bobUser := e.adminBrowser(t, "bob-"+testEmail(t))

	settings := func(b *browser, on bool) resp {
		cur := b.do("GET", "/admin/settings", nil)
		expect(t, cur, 200, "")
		return b.do("PUT", "/admin/settings", map[string]any{"version": cur.body["version"], "defaults": cur.body["defaults"],
			"features": cur.body["features"], "limits": cur.body["limits"], "security": map[string]any{"adminsNeedSecondFactor": on}})
	}
	// without her own second factor Alice can't require it
	expect(t, settings(alice, true), 422, "validation_failed")
	secret, _ := enrolHTTP(t, e, alice)
	expect(t, settings(alice, true), 200, "")
	t.Cleanup(func() { // the settings row is shared by every test of the package
		e.clock.Add(31 * time.Second)
		if r := settings(alice, false); r.code != 200 {
			t.Errorf("switching the rule off again: %s", r.raw)
		}
	})

	// Bob has none: his own data and the security settings work, administration does not
	me := bob.do("GET", "/me", nil)
	if me.body["secondFactorRequired"] != true || me.body["secondFactor"] != false {
		t.Errorf("Bob's /me: %s", me.raw)
	}
	expect(t, bob.do("GET", "/accounts", nil), 200, "")
	if r := bob.do("GET", "/me/second-factor", nil); r.body["requiredForAdmins"] != true {
		t.Errorf("Bob's second factor: %s", r.raw)
	}
	for _, route := range [][2]string{{"GET", "/admin/users"}, {"GET", "/admin/settings"}, {"GET", "/admin/usage"}} {
		expect(t, bob.do(route[0], route[1], nil), 403, "second_factor_required")
	}
	// a document saved without the security part keeps the rule (the settings page's other tabs)
	cur := alice.do("GET", "/admin/settings", nil)
	r := alice.do("PUT", "/admin/settings", map[string]any{"version": cur.body["version"], "defaults": cur.body["defaults"],
		"features": cur.body["features"], "limits": cur.body["limits"]})
	expect(t, r, 200, "")
	if sec, _ := r.body["security"].(map[string]any); sec["adminsNeedSecondFactor"] != true {
		t.Errorf("the rule was dropped by a save without it: %s", r.raw)
	}
	expect(t, bob.do("GET", "/admin/users", nil), 403, "second_factor_required")
	// once Bob sets it up, administration opens again
	bobSecret, _ := enrolHTTP(t, e, bob)
	expect(t, bob.do("GET", "/admin/users", nil), 200, "")

	// Alice removes Bob's second factor (he lost his phone); both logs say so
	list := alice.do("GET", "/admin/users?search=bob-", nil)
	expect(t, list, 200, "")
	if !strings.Contains(list.raw, `"secondFactor":true`) {
		t.Errorf("the user list doesn't show Bob's second factor: %s", list.raw)
	}
	expect(t, alice.do("DELETE", "/admin/users/"+bobUser.ID.String()+"/second-factor", nil), 204, "")
	expect(t, alice.do("DELETE", "/admin/users/"+bobUser.ID.String()+"/second-factor", nil), 404, "not_found")
	if acts := bob.do("GET", "/me/activity?limit=20", nil); !strings.Contains(acts.raw, "admin.second_factor_removed") {
		t.Errorf("Bob's activity doesn't say an administrator removed it: %s", acts.raw)
	}
	fresh := e.browser()
	expect(t, fresh.do("POST", "/auth/login", map[string]any{"email": bobUser.Email, "password": password}), 200, "")
	expect(t, fresh.do("GET", "/admin/users", nil), 403, "second_factor_required") // and he is asked to set it up again
	_ = bobSecret
	// her own goes through Settings → Security
	aliceID := alice.do("GET", "/me", nil).body["id"].(string)
	expect(t, alice.do("DELETE", "/admin/users/"+aliceID+"/second-factor", nil), 422, "validation_failed")
	_ = secret
}
