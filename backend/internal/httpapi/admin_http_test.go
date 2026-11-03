package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/auth"
	"github.com/x2db736ac85h/finuance/backend/internal/config"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	tu "github.com/x2db736ac85h/finuance/backend/testutil"
)

// adminBrowser signs in a new administrator (created like `finuance admin create-user -admin`).
func (e *authEnv) adminBrowser(t *testing.T, mail string) (*browser, auth.User) {
	t.Helper()
	u, err := e.auth.CreateUser(context.Background(), auth.SignupInput{Email: mail, Password: password, Admin: true})
	if err != nil {
		t.Fatal(err)
	}
	b := e.browser()
	expect(t, b.do("POST", "/auth/login", map[string]any{"email": mail, "password": password}), 200, "")
	return b, u
}

func signupBrowser(t *testing.T, e *authEnv, mail string) *browser {
	t.Helper()
	b := e.browser()
	expect(t, b.do("POST", "/auth/signup", map[string]any{"email": mail, "password": password, "displayName": "Alex"}), 201, "")
	return b
}

// financialWords are field names and values that would mean an admin response carries financial
// data; the admin API returns accounts and counts only (ADR 0025).
var financialWords = []string{"amount", "payee", "description", "category", "balance", "DEMO ", "PLN", "iban", "content", "title"}

func TestAdminCannotReadFinancialData(t *testing.T) {
	e := newTestEnv(t, nil, true)
	boss, _ := e.adminBrowser(t, strings.Replace(testEmail(t), "@", "-boss@", 1))
	alexMail := testEmail(t)
	alex := signupBrowser(t, e, alexMail)
	expect(t, alex.do("POST", "/demo-data", nil), 201, "")

	page := boss.do("GET", "/admin/users?search="+alexMail, nil)
	expect(t, page, 200, "")
	users := page.body["users"].([]any)
	if len(users) != 1 {
		t.Fatalf("search found %v", users)
	}
	row := users[0].(map[string]any)
	usage := row["usage"].(map[string]any)
	if usage["transactions"].(float64) < 100 || usage["accounts"].(float64) != 1 {
		t.Errorf("usage = %v", usage)
	}
	id := row["id"].(string)
	for _, r := range []resp{page, boss.do("GET", "/admin/users/"+id, nil), boss.do("GET", "/admin/usage?days=7", nil),
		boss.do("GET", "/admin/settings", nil)} {
		for _, w := range financialWords {
			if strings.Contains(strings.ToLower(r.raw), strings.ToLower(w)) {
				t.Errorf("an admin response mentions %q: %s", w, r.raw)
			}
		}
	}
	// the administrator's own data routes still only show their own (empty) data
	if r := boss.do("POST", "/transactions/query", map[string]any{"context": map[string]any{}}); r.code == 200 && strings.Contains(r.raw, "DEMO") {
		t.Errorf("the administrator saw someone else's transactions: %s", r.raw)
	}
}

func TestAdminRoutesAreHidden(t *testing.T) {
	e := newTestEnv(t, nil, false)
	alex := signupBrowser(t, e, testEmail(t))
	for _, path := range []string{"/admin/users", "/admin/settings", "/admin/usage", "/admin/themes"} {
		expect(t, alex.do("GET", path, nil), 404, "not_found")
		expect(t, e.browser().do("GET", path, nil), 401, "unauthenticated")
	}
	me := alex.do("GET", "/me", nil)
	if me.body["isAdmin"] != false || me.body["limits"] == nil {
		t.Errorf("me = %v", me.body)
	}
}

func TestAdminManagesUsers(t *testing.T) {
	e := newTestEnv(t, nil, false)
	boss, bossUser := e.adminBrowser(t, strings.Replace(testEmail(t), "@", "-boss@", 1))
	mail := testEmail(t)
	r := boss.do("POST", "/admin/users", map[string]any{"email": mail, "displayName": "New"})
	expect(t, r, 201, "")
	one, _ := r.body["oneTimePassword"].(string)
	user := r.body["user"].(map[string]any)
	id := user["id"].(string)
	if one == "" || user["mustChangePassword"] != true {
		t.Fatalf("created %v", r.body)
	}

	// the new user must change the one-time password before anything else
	nu := e.browser()
	expect(t, nu.do("POST", "/auth/login", map[string]any{"email": mail, "password": one}), 200, "")
	expect(t, nu.do("GET", "/me", nil), 200, "")
	expect(t, nu.do("GET", "/accounts", nil), 403, "password_change_required")
	expect(t, nu.do("POST", "/me/password", map[string]any{"currentPassword": one, "newPassword": "my own password now"}), 204, "")
	expect(t, nu.do("GET", "/accounts", nil), 200, "")

	// limits: one account, then 422 limit_reached
	expect(t, boss.do("PATCH", "/admin/users/"+id, map[string]any{"limits": map[string]any{"maxAccounts": 1}}), 200, "")
	expect(t, nu.do("POST", "/accounts", map[string]any{"name": "First", "currency": "PLN"}), 201, "")
	r = nu.do("POST", "/accounts", map[string]any{"name": "Second", "currency": "PLN"})
	expect(t, r, 422, "limit_reached")
	if lim := r.body["limit"].(map[string]any); lim["name"] != "maxAccounts" || lim["value"].(float64) != 1 {
		t.Errorf("limit = %v", lim)
	}
	if me := nu.do("GET", "/me", nil); me.body["limits"].(map[string]any)["maxAccounts"].(float64) != 1 {
		t.Errorf("me limits = %v", me.body["limits"])
	}

	// disable signs the user out; enabling lets them back in
	expect(t, boss.do("PATCH", "/admin/users/"+id, map[string]any{"disabled": true}), 200, "")
	expect(t, nu.do("GET", "/me", nil), 401, "unauthenticated")
	expect(t, e.browser().do("POST", "/auth/login", map[string]any{"email": mail, "password": "my own password now"}), 403, "account_disabled")
	expect(t, boss.do("PATCH", "/admin/users/"+id, map[string]any{"disabled": false}), 200, "")
	expect(t, nu.do("POST", "/auth/login", map[string]any{"email": mail, "password": "my own password now"}), 200, "")

	// reset password and revoke sessions
	r = boss.do("POST", "/admin/users/"+id+"/reset-password", nil)
	expect(t, r, 200, "")
	expect(t, nu.do("GET", "/me", nil), 401, "unauthenticated")
	expect(t, nu.do("POST", "/auth/login", map[string]any{"email": mail, "password": r.body["oneTimePassword"]}), 200, "")
	r = boss.do("POST", "/admin/users/"+id+"/revoke-sessions", nil)
	expect(t, r, 200, "")
	if r.body["revoked"].(float64) != 1 {
		t.Errorf("revoked %v", r.body)
	}

	// the user's Activity shows what an administrator did, not who
	ents, err := e.ledgerFor().AuditPage(context.Background(), mustUUID(t, id), 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	actions := map[string]bool{}
	for _, en := range ents {
		actions[en.Action] = true
		if en.Action == ledger.AuditAdminPasswordReset && (en.Meta["by"] != "administrator" || strings.Contains(fmt.Sprint(en.Meta), bossUser.Email)) {
			t.Errorf("the user's event names the administrator: %+v", en)
		}
	}
	for _, a := range []string{ledger.AuditAdminLimitsChanged, ledger.AuditAdminUserDisabled, ledger.AuditAdminUserEnabled,
		ledger.AuditAdminPasswordReset, ledger.AuditAdminSessionsRevoked} {
		if !actions[a] {
			t.Errorf("the user's Activity lacks %s (has %v)", a, actions)
		}
	}

	// delete needs the e-mail; an admin can't delete or disable themselves here
	expect(t, boss.do("DELETE", "/admin/users/"+id, map[string]any{"confirmEmail": "x@example.test"}), 422, "validation_failed")
	expect(t, boss.do("DELETE", "/admin/users/"+bossUser.ID.String(), map[string]any{"confirmEmail": bossUser.Email}), 422, "validation_failed")
	expect(t, boss.do("PATCH", "/admin/users/"+bossUser.ID.String(), map[string]any{"disabled": true}), 422, "validation_failed")
	expect(t, boss.do("DELETE", "/admin/users/"+id, map[string]any{"confirmEmail": mail}), 204, "")
	expect(t, boss.do("GET", "/admin/users/"+id, nil), 404, "not_found")
}

func TestInstanceSettingsHTTP(t *testing.T) {
	e := newTestEnv(t, func(c *config.Config) { c.MaxUploadBytes = 1 << 20 }, false)
	boss, _ := e.adminBrowser(t, strings.Replace(testEmail(t), "@", "-boss@", 1))
	t.Cleanup(func() { // the settings are instance-wide: the package's other tests need the defaults
		if _, err := tu.Require(t, testDB).Owner.Exec(context.Background(), `UPDATE instance_settings SET doc = '{}'`); err != nil {
			t.Error(err)
		}
	})
	r := boss.do("GET", "/admin/settings", nil)
	expect(t, r, 200, "")
	v := r.body["version"]
	doc := map[string]any{"version": v, "defaults": map[string]any{"themeId": "grove", "noViz": true, "cards": []string{"sum", "flow"}},
		"features": map[string]any{"overview": false}, "limits": map[string]any{"maxUploadBytes": 4096}}
	r = boss.do("PUT", "/admin/settings", doc)
	expect(t, r, 200, "")
	expect(t, boss.do("PUT", "/admin/settings", doc), 409, "stale_version")
	doc["version"] = r.body["version"]
	doc["defaults"] = map[string]any{"themeId": "missing"}
	expect(t, boss.do("PUT", "/admin/settings", doc), 422, "validation_failed")

	meta := e.browser().do("GET", "/meta", nil)
	f := meta.body["features"].(map[string]any)
	d := meta.body["defaults"].(map[string]any)
	if f["overview"] != false || f["assistant"] != true || d["themeId"] != "grove" || d["noViz"] != true {
		t.Errorf("meta = %v", meta.body)
	}
	e.clock.Add(11e9) // other replicas see the change within the cache time; this one at once

	alex := signupBrowser(t, e, testEmail(t))
	expect(t, alex.do("POST", "/overview", map[string]any{"context": map[string]any{}}), 404, "not_found")
	me := alex.do("GET", "/me", nil)
	if me.body["limits"].(map[string]any)["maxUploadBytes"].(float64) != 4096 {
		t.Errorf("limits = %v", me.body["limits"])
	}
}

func TestThemesHTTP(t *testing.T) {
	e := newTestEnv(t, nil, false)
	boss, _ := e.adminBrowser(t, strings.Replace(testEmail(t), "@", "-boss@", 1))
	mode := func(bg, fg, accent string) map[string]string {
		m := map[string]string{}
		for _, k := range []string{"bg", "panel", "elev", "input", "border", "accent-solid", "logo-a", "logo-b"} {
			m[k] = bg
		}
		for _, k := range []string{"text", "muted", "pos", "neg", "on-accent"} {
			m[k] = fg
		}
		m["accent"], m["accent-soft"], m["sel"] = accent, fg+"10", fg+"10"
		return m
	}
	slug := strings.ToLower(strings.ReplaceAll(t.Name(), "Test", "t"))
	body := map[string]any{"slug": slug, "name": "Sea", "basedOn": "fjord",
		"tokens": map[string]any{"dark": mode("#000000", "#ffffff", "#222222"), "light": mode("#ffffff", "#000000", "#000000")}}
	r := boss.do("POST", "/admin/themes", body)
	expect(t, r, 201, "")
	id, v := r.body["id"].(string), r.body["version"]
	r = boss.do("PUT", "/admin/themes/"+id+"/status", map[string]any{"version": v, "status": "approved"})
	expect(t, r, 422, "contrast_failed")
	if c := r.body["contrast"].([]any); len(c) == 0 || c[0].(map[string]any)["fg"] != "accent" {
		t.Errorf("contrast = %v", r.body["contrast"])
	}
	bad := map[string]any{"version": v, "name": "Sea", "tokens": map[string]any{"dark": map[string]string{"bg": "url(x)"}, "light": map[string]string{}}}
	expect(t, boss.do("PUT", "/admin/themes/"+id, bad), 422, "validation_failed")
	body["tokens"].(map[string]any)["dark"] = mode("#000000", "#ffffff", "#ffffff")
	r = boss.do("PUT", "/admin/themes/"+id, map[string]any{"version": v, "name": "Sea", "tokens": body["tokens"]})
	expect(t, r, 200, "")
	expect(t, boss.do("PUT", "/admin/themes/"+id+"/status", map[string]any{"version": r.body["version"], "status": "approved"}), 200, "")
	meta := e.browser().do("GET", "/meta", nil)
	raw, _ := json.Marshal(meta.body["themes"])
	if !strings.Contains(string(raw), `"id":"`+slug+`"`) {
		t.Errorf("meta themes = %s", raw)
	}
	expect(t, boss.do("DELETE", "/admin/themes/"+id, nil), 404, "not_found")
}

func (e *authEnv) ledgerFor() *ledger.Ledger { return ledger.New(e.st, e.clock) }

func mustUUID(t *testing.T, s string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
