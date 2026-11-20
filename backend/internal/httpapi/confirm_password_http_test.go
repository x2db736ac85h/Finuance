package httpapi

import (
	"testing"

	"github.com/x2db736ac85h/finuance/backend/internal/config"
)

// TestConfirmPassword: with FINUANCE_CONFIRM_PASSWORD on (the default of a real instance), making
// an access token and changing an administrator's role need the current password again; a missing
// or wrong one is a validation error on "password" and nothing is made or changed. Other changes
// to an account don't ask. Switched off, neither asks (SYS-4).
func TestConfirmPassword(t *testing.T) {
	e := newTestEnv(t, func(c *config.Config) { c.ConfirmPassword = true }, false)
	if r := e.browser().do("GET", "/meta", nil); r.body["confirmPassword"] != true {
		t.Fatalf("/meta: %s", r.raw)
	}
	admin, _ := e.adminBrowser(t, "admin-"+testEmail(t))
	signupBrowser(t, e, "user-"+testEmail(t))
	list := admin.do("GET", "/admin/users?search=user-", nil)
	expect(t, list, 200, "")
	userID := list.body["users"].([]any)[0].(map[string]any)["id"].(string)

	token := map[string]any{"name": "cli", "scope": "read", "expiresInDays": 7}
	with := func(body map[string]any, pw string) map[string]any {
		out := map[string]any{"password": pw}
		for k, v := range body {
			out[k] = v
		}
		return out
	}
	expect(t, admin.do("POST", "/me/tokens", token), 422, "validation_failed")
	expect(t, admin.do("POST", "/me/tokens", with(token, "not my password")), 422, "validation_failed")
	if r := admin.do("GET", "/me/tokens", nil); r.raw != "[]\n" && r.raw != "[]" {
		t.Errorf("a token was made without the password: %s", r.raw)
	}
	expect(t, admin.do("POST", "/me/tokens", with(token, password)), 201, "")

	promote := map[string]any{"admin": true}
	expect(t, admin.do("PATCH", "/admin/users/"+userID, promote), 422, "validation_failed")
	expect(t, admin.do("PATCH", "/admin/users/"+userID, with(promote, "not my password")), 422, "validation_failed")
	if r := admin.do("GET", "/admin/users/"+userID, nil); r.body["isAdmin"] != false {
		t.Errorf("promoted without the password: %s", r.raw)
	}
	expect(t, admin.do("PATCH", "/admin/users/"+userID, with(promote, password)), 200, "")
	// repeating the current role, and changes that are not the role, don't ask
	expect(t, admin.do("PATCH", "/admin/users/"+userID, promote), 200, "")
	expect(t, admin.do("PATCH", "/admin/users/"+userID, map[string]any{"displayName": "Sam"}), 200, "")

	off := newTestEnv(t, nil, false) // the tests' default: not asked
	if r := off.browser().do("GET", "/meta", nil); r.body["confirmPassword"] != false {
		t.Errorf("/meta with it off: %s", r.raw)
	}
	b := signupBrowser(t, off, "off-"+testEmail(t))
	expect(t, b.do("POST", "/me/tokens", token), 201, "")
}

// TestDemotionEndsSessions: an administrator who is demoted by another one is signed out, so no
// open browser keeps an administrator's session (SYS-4).
func TestDemotionEndsSessions(t *testing.T) {
	e := newAuthEnv(t, config.EnvTest, false)
	alice, _ := e.adminBrowser(t, "alice-"+testEmail(t))
	bob, bobUser := e.adminBrowser(t, "bob-"+testEmail(t))
	expect(t, bob.do("GET", "/admin/users", nil), 200, "")
	expect(t, alice.do("PATCH", "/admin/users/"+bobUser.ID.String(), map[string]any{"admin": false}), 200, "")
	expect(t, bob.do("GET", "/me", nil), 401, "unauthenticated")
	// he signs in again as an ordinary user
	expect(t, bob.do("POST", "/auth/login", map[string]any{"email": bobUser.Email, "password": password}), 200, "")
	expect(t, bob.do("GET", "/admin/users", nil), 404, "not_found")
	// demoting yourself keeps your session (Alice makes Bob an administrator again first)
	expect(t, alice.do("PATCH", "/admin/users/"+bobUser.ID.String(), map[string]any{"admin": true}), 200, "")
	aliceID := alice.do("GET", "/me", nil).body["id"].(string)
	expect(t, alice.do("PATCH", "/admin/users/"+aliceID, map[string]any{"admin": false}), 200, "")
	expect(t, alice.do("GET", "/me", nil), 200, "")
}
