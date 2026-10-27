package httpapi

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/auth"
	"github.com/x2db736ac85h/finuance/backend/internal/config"
	tu "github.com/x2db736ac85h/finuance/backend/testutil"
)

// demoConfig is what config.Load makes of FINUANCE_DEMO_MODE=true.
func demoConfig(c *config.Config) {
	c.DemoMode, c.AllowSignup, c.MaxUploadBytes = true, false, config.DemoMaxUploadBytes
}

// TestDemoHTTP (PLAN 19.1): a visitor gets a fresh demo user with the demo year, signed in; the
// instance takes no sign-ups, no migration and no upload over 2 MiB; five demo users per address
// and hour; visitors see only their own data; the hourly cleanup removes demo users after 24 hours
// and nothing else.
func TestDemoHTTP(t *testing.T) {
	e := newTestEnv(t, demoConfig, false)
	meta := e.browser().do("GET", "/meta", nil)
	if meta.body["demoMode"] != true || meta.body["signupEnabled"] != false {
		t.Errorf("meta = %s", meta.raw)
	}

	v1 := e.browser()
	start := v1.do("POST", "/auth/demo", nil)
	expect(t, start, 200, "")
	if start.body["isDemo"] != true || !strings.HasSuffix(start.body["email"].(string), "@demo.invalid") {
		t.Errorf("demo user = %s", start.raw)
	}
	if !strings.Contains(start.header.Get("Set-Cookie"), "HttpOnly") {
		t.Errorf("no session cookie: %v", start.header)
	}
	accs := v1.body2Get(t, "/accounts")
	if len(accs) != 1 || num(accs[0]["txCount"]) < 800 { // a year of the demo generator, ending today
		t.Fatalf("accounts = %v", accs)
	}
	if got := actions(t, v1); len(got) != 2 || got[0] != "import.committed" || got[1] != "auth.login" {
		t.Errorf("activity = %v", got)
	}

	// what the demo refuses
	expect(t, e.browser().do("POST", "/auth/signup", map[string]any{"email": testEmail(t), "password": password}), 403, "forbidden")
	expect(t, v1.migrate(map[string]any{"version": 1, "files": []any{}}, [][]byte{[]byte("a;b\n1;2\n")}), 403, "forbidden")
	big := bytes.Repeat([]byte("2024-01-01;x;-1,00\n"), config.DemoMaxUploadBytes/19+1)
	expect(t, v1.upload("", "big.csv", big, accs[0]["id"].(string)), 413, "payload_too_large")

	// a second visitor sees only their own demo year
	v2 := e.browser()
	expect(t, v2.do("POST", "/auth/demo", nil), 200, "")
	if a2 := v2.body2Get(t, "/accounts"); len(a2) != 1 || a2[0]["id"] == accs[0]["id"] {
		t.Errorf("second visitor's accounts = %v", a2)
	}
	// three more from this address, then 429
	for range 3 {
		expect(t, e.browser().do("POST", "/auth/demo", nil), 200, "")
	}
	tooMany := e.browser().do("POST", "/auth/demo", nil)
	expect(t, tooMany, 429, "rate_limited")
	if tooMany.header.Get("Retry-After") == "" {
		t.Error("no Retry-After")
	}

	// the cleanup: v1 created 25 h ago goes, v2 and an old normal account stay
	tdb := tu.Require(t, testDB)
	svc := auth.New(e.st, e.clock, auth.Config{Params: auth.Params{MemoryKiB: 8 * 1024, Time: 1, Threads: 1}, SessionTTL: time.Hour, SessionIdleTTL: time.Hour})
	normal, err := svc.CreateUser(context.Background(), auth.SignupInput{Email: testEmail(t), Password: password})
	if err != nil {
		t.Fatal(err)
	}
	me := v1.do("GET", "/me", nil)
	if _, err := tdb.Owner.Exec(context.Background(), `UPDATE users SET created_at = $2 WHERE id = ANY($1)`,
		[]any{me.body["id"], normal.ID}, e.clock.Now().Add(-auth.DemoLifetime-time.Hour)); err != nil {
		t.Fatal(err)
	}
	n, err := svc.CleanupDemoUsers(context.Background())
	if err != nil || n < 1 {
		t.Fatalf("cleanup = %d, %v", n, err)
	}
	expect(t, v1.do("GET", "/me", nil), 401, "unauthenticated")
	expect(t, v2.do("GET", "/me", nil), 200, "")
	if _, err := svc.UserByEmail(context.Background(), normal.Email); err != nil {
		t.Errorf("a normal account was deleted: %v", err)
	}
	var left int
	if err := tdb.Owner.QueryRow(context.Background(), `SELECT count(*) FROM accounts WHERE user_id = $1`, me.body["id"]).Scan(&left); err != nil || left != 0 {
		t.Errorf("the demo user's accounts left: %d, %v", left, err)
	}
}

// TestDemoOffHTTP: without demo mode the route doesn't exist and sign-up works as before.
func TestDemoOffHTTP(t *testing.T) {
	e := newTestEnv(t, nil, false)
	expect(t, e.browser().do("POST", "/auth/demo", nil), 404, "not_found")
	if meta := e.browser().do("GET", "/meta", nil); meta.body["demoMode"] != false {
		t.Errorf("meta = %s", meta.raw)
	}
}

// TestDemoAssistantHTTP: with FINUANCE_DEMO_ASSISTANT a demo user gets 10 questions over its
// lifetime and lookups of at most 25 rows.
func TestDemoAssistantHTTP(t *testing.T) {
	e, _ := mockEnv(t, func(c *config.Config) { demoConfig(c); c.DemoAssistant = true })
	b := e.browser()
	expect(t, b.do("POST", "/auth/demo", nil), 200, "")
	st := b.do("GET", "/assistant/status", nil)
	if st.body["enabled"] != true || num(st.body["maxRowsLimit"]) != config.DemoMaxRows {
		t.Errorf("status = %s", st.raw)
	}
	chat := b.do("POST", "/assistant/chats", nil)
	expect(t, chat, 201, "")
	path := "/assistant/chats/" + chat.body["id"].(string) + "/messages"
	ask := b.do("POST", path, map[string]any{"text": `Show "warszawa"`, "settings": map[string]any{"maxRows": 50}})
	expect(t, ask, 200, "")
	step := parseSSE(t, ask.raw)[slicesIndexEv(parseSSE(t, ask.raw), "step")].data
	if rows, _ := step["rows"].([]any); len(rows) > config.DemoMaxRows || num(step["matched"]) <= config.DemoMaxRows {
		t.Errorf("step: %d rows of %v matched (at most %d)", len(rows), step["matched"], config.DemoMaxRows)
	}
	for i := 2; i <= config.DemoAssistantMessages; i++ {
		expect(t, b.do("POST", path, map[string]any{"text": "Hello"}), 200, "")
	}
	expect(t, b.do("POST", path, map[string]any{"text": "Hello"}), 429, "rate_limited")
}

// body2Get reads a JSON array from a GET that must succeed.
func (b *browser) body2Get(t *testing.T, path string) []map[string]any {
	t.Helper()
	r := b.do("GET", path, nil)
	expect(t, r, 200, "")
	return r.body2(t)
}
