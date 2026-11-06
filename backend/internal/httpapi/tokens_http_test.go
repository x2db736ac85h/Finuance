package httpapi

import (
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"
)

// script is a client with a bearer token: no cookie jar, no CSRF header (the fin CLI).
func (e *authEnv) script(secret string) *browser {
	return &browser{e: e, c: &http.Client{}, header: map[string]string{"Authorization": "Bearer " + secret}}
}

func (b *browser) token(t *testing.T, name, scope string, days int) (string, string) {
	t.Helper()
	r := b.do("POST", "/me/tokens", map[string]any{"name": name, "scope": scope, "expiresInDays": days})
	expect(t, r, 201, "")
	return r.body["secret"].(string), obj(t, r.body, "token")["id"].(string)
}

// TestAccessTokensHTTP: a read token reads (GET and the query routes) and is refused anything that
// changes data; a write token writes without a CSRF header; no token reaches the admin, password,
// session or token routes; cookies are ignored next to a bearer token; a revoked or expired token
// is refused; a new password ends every token (ADR 0036).
func TestAccessTokensHTTP(t *testing.T) {
	e := newTestEnv(t, nil, true)
	boss, _ := e.adminBrowser(t, "boss-"+testEmail(t))
	b, accID := demoBrowser(t, e)

	readSecret, readID := b.token(t, "reports", "read", 30)
	if !strings.HasPrefix(readSecret, "fin_") || len(readSecret) < 40 {
		t.Fatalf("secret %q", readSecret)
	}
	list := b.do("GET", "/me/tokens", nil)
	if rows := list.body2(t); len(rows) != 1 || rows[0]["prefix"] != readSecret[:12] || rows[0]["scope"] != "read" ||
		strings.Contains(list.raw, readSecret) {
		t.Fatalf("list = %s", list.raw)
	}
	expect(t, b.do("POST", "/me/tokens", map[string]any{"name": "", "scope": "admin", "expiresInDays": 400}), 422, "validation_failed")

	rd := e.script(readSecret)
	if r := rd.do("GET", "/accounts", nil); r.code != 200 || len(r.body2(t)) != 1 {
		t.Errorf("read GET = %d %s", r.code, r.raw)
	}
	q := rd.do("POST", "/transactions/query", map[string]any{"context": map[string]any{"accountIds": []string{accID}}, "limit": 5})
	expect(t, q, 200, "")
	expect(t, rd.do("POST", "/accounts", map[string]any{"name": "Nope"}), 403, "forbidden")
	expect(t, rd.do("PATCH", "/me", map[string]any{"displayName": "X"}), 403, "forbidden")
	if r := rd.do("GET", "/meta", nil); r.code != 200 {
		t.Errorf("meta with a token = %d", r.code)
	}

	writeSecret, _ := b.token(t, "cli", "write", 365)
	wr := e.script(writeSecret)
	if r := wr.do("POST", "/accounts", map[string]any{"name": "From the CLI"}); r.code != 201 {
		t.Errorf("write POST without a CSRF header = %d %s", r.code, r.raw)
	}
	for _, c := range []struct{ method, path string }{
		{"GET", "/me/tokens"}, {"POST", "/me/tokens"}, {"GET", "/me/sessions"}, {"POST", "/me/password"},
		{"DELETE", "/me"}, {"POST", "/auth/logout"},
	} {
		if r := wr.do(c.method, c.path, map[string]any{}); r.code != 403 {
			t.Errorf("%s %s with a token = %d %s", c.method, c.path, r.code, r.raw)
		}
	}
	// an administrator's token doesn't reach the admin panel either
	bossSecret, _ := boss.token(t, "boss cli", "write", 7)
	expect(t, e.script(bossSecret).do("GET", "/admin/users", nil), 403, "forbidden")

	// a bad bearer next to a good cookie: the cookie is not used
	jar, _ := cookiejar.New(nil)
	both := &browser{e: e, c: &http.Client{Jar: jar}, header: map[string]string{"Authorization": "Bearer fin_wrong", "X-Finuance-Client": "web"}}
	both.c.Jar = b.c.Jar
	expect(t, both.do("GET", "/accounts", nil), 401, "unauthenticated")
	expect(t, e.script("not-a-token").do("GET", "/accounts", nil), 401, "unauthenticated")

	// revoked, then expired
	expect(t, b.do("DELETE", "/me/tokens/"+readID, nil), 204, "")
	expect(t, rd.do("GET", "/accounts", nil), 401, "unauthenticated")
	expect(t, b.do("DELETE", "/me/tokens/"+readID, nil), 404, "not_found")
	shortSecret, _ := b.token(t, "one day", "read", 1)
	e.clock.Add(25 * time.Hour)
	expect(t, e.script(shortSecret).do("GET", "/accounts", nil), 401, "unauthenticated")
	e.clock.Add(-25 * time.Hour)

	acts := b.do("GET", "/me/activity", nil)
	created, revoked := 0, 0
	for _, a := range acts.body["events"].([]any) {
		switch a.(map[string]any)["action"] {
		case "auth.token_created":
			created++
		case "auth.token_revoked":
			revoked++
		}
	}
	if created != 3 || revoked != 1 || strings.Contains(acts.raw, writeSecret) {
		t.Errorf("activity: %d created, %d revoked: %s", created, revoked, acts.raw)
	}

	// a new password ends every token, and so does an administrator signing the account out
	expect(t, b.do("POST", "/me/password", map[string]any{"currentPassword": password, "newPassword": "another long password 2"}), 204, "")
	expect(t, wr.do("GET", "/accounts", nil), 401, "unauthenticated")
	again, _ := b.token(t, "cli again", "write", 30)
	me := b.do("GET", "/me", nil)
	expect(t, boss.do("POST", "/admin/users/"+me.body["id"].(string)+"/revoke-sessions", nil), 200, "")
	expect(t, e.script(again).do("GET", "/accounts", nil), 401, "unauthenticated")
}
