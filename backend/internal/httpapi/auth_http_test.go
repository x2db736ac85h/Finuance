package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/auth"
	"github.com/x2db736ac85h/finuance/backend/internal/config"
	"github.com/x2db736ac85h/finuance/backend/internal/ingest"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/observability"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
	tu "github.com/x2db736ac85h/finuance/backend/testutil"
)

var testDB *tu.DB

func TestMain(m *testing.M) { tu.Main(m, &testDB) }

const origin = "http://localhost:8000"

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) Add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

// authEnv is a real HTTP server with the auth service on the test database.
type authEnv struct {
	t     *testing.T
	srv   *httptest.Server
	st    *store.Store
	clock *fakeClock
	logs  *syncBuffer
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}
func (s *syncBuffer) String() string { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

func newAuthEnv(t *testing.T, env config.Env, cookieSecure bool) *authEnv {
	t.Helper()
	tdb := tu.Require(t, testDB)
	o, _ := url.Parse(origin)
	cfg := config.Config{Env: env, AllowSignup: true, PublicOrigin: o, CookieSecure: cookieSecure,
		SessionTTL: 30 * 24 * time.Hour, SessionIdleTTL: 7 * 24 * time.Hour}
	st := store.New(tdb.App)
	clk := &fakeClock{t: time.Now().UTC()}
	svc := auth.New(st, clk, auth.Config{Params: auth.Params{MemoryKiB: 8 * 1024, Time: 1, Threads: 1},
		SessionTTL: cfg.SessionTTL, SessionIdleTTL: cfg.SessionIdleTTL, AllowSignup: true})
	logs := &syncBuffer{}
	tr, _ := observability.NewTracing(context.Background(), "", "finuance", "test")
	h, err := NewHandler(Deps{Config: cfg, Version: "test", Logger: slog.New(slog.NewJSONHandler(logs, nil)),
		Metrics: observability.NewMetrics(), Tracing: tr, Auth: svc, Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &authEnv{t: t, srv: srv, st: st, clock: clk, logs: logs}
}

// browser is an HTTP client with a cookie jar that sends the CSRF header like the web app does.
type browser struct {
	e      *authEnv
	c      *http.Client
	header map[string]string
}

func (e *authEnv) browser() *browser {
	jar, _ := cookiejar.New(nil)
	return &browser{e: e, c: &http.Client{Jar: jar}, header: map[string]string{"X-Finuance-Client": "web", "Origin": origin}}
}

type resp struct {
	code   int
	body   map[string]any
	raw    string
	header http.Header
}

func (b *browser) do(method, path string, body any) resp {
	b.e.t.Helper()
	var rd io.Reader
	if body != nil {
		js, _ := json.Marshal(body)
		rd = bytes.NewReader(js)
	}
	req, _ := http.NewRequest(method, b.e.srv.URL+"/api/v1"+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range b.header {
		req.Header.Set(k, v)
	}
	res, err := b.c.Do(req)
	if err != nil {
		b.e.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	out := resp{code: res.StatusCode, raw: string(raw), header: res.Header}
	_ = json.Unmarshal(raw, &out.body)
	return out
}

func (b *browser) cookie() string {
	u, _ := url.Parse(b.e.srv.URL)
	for _, c := range b.c.Jar.Cookies(u) {
		if c.Name == SessionCookie {
			return c.Value
		}
	}
	return ""
}

func (b *browser) setCookie(v string) {
	u, _ := url.Parse(b.e.srv.URL)
	b.c.Jar.SetCookies(u, []*http.Cookie{{Name: SessionCookie, Value: v, Path: "/"}})
}

func expect(t *testing.T, r resp, code int, problem string) {
	t.Helper()
	if r.code != code {
		t.Fatalf("status %d, want %d: %s", r.code, code, r.raw)
	}
	if problem != "" && r.body["code"] != problem {
		t.Fatalf("problem code %v, want %s: %s", r.body["code"], problem, r.raw)
	}
}

func testEmail(t *testing.T) string { return strings.ReplaceAll(t.Name(), "/", "-") + "@example.test" }

const password = "correct horse battery"

func TestAuthFlow(t *testing.T) {
	e := newAuthEnv(t, config.EnvTest, false)
	b := e.browser()
	email := testEmail(t)

	expect(t, b.do("GET", "/me", nil), 401, "unauthenticated")
	r := b.do("POST", "/auth/signup", map[string]any{"email": email, "password": password, "displayName": "Irwin"})
	expect(t, r, 201, "")
	if r.body["email"] != email || r.body["displayName"] != "Irwin" {
		t.Errorf("signup body = %v", r.body)
	}
	first := b.cookie()
	if first == "" {
		t.Fatal("no session cookie after signup")
	}
	expect(t, b.do("GET", "/me", nil), 200, "")
	r = b.do("PATCH", "/me", map[string]any{"displayName": "Irwin K."})
	expect(t, r, 200, "")
	if r.body["displayName"] != "Irwin K." {
		t.Errorf("patch me = %v", r.body)
	}
	expect(t, b.do("POST", "/auth/signup", map[string]any{"email": strings.ToUpper(email), "password": password}), 409, "conflict")
	expect(t, b.do("POST", "/auth/signup", map[string]any{"email": "x", "password": "short"}), 422, "validation_failed")

	// Logout clears the cookie and kills the session even if the cookie is replayed.
	r = b.do("POST", "/auth/logout", nil)
	expect(t, r, 204, "")
	if !strings.Contains(r.header.Get("Set-Cookie"), "Max-Age=0") {
		t.Errorf("logout cookie = %q", r.header.Get("Set-Cookie"))
	}
	b.setCookie(first)
	expect(t, b.do("GET", "/me", nil), 401, "unauthenticated")

	// Login with a stale cookie: rotation revokes it and issues a new one.
	expect(t, b.do("POST", "/auth/login", map[string]any{"email": email, "password": "wrong password!"}), 401, "unauthenticated")
	expect(t, b.do("POST", "/auth/login", map[string]any{"email": "nobody-" + email, "password": password}), 401, "unauthenticated")
	expect(t, b.do("POST", "/auth/login", map[string]any{"email": email, "password": password}), 200, "")
	second := b.cookie()
	if second == first || second == "" {
		t.Fatal("login did not rotate the session")
	}

	// Another browser signs in; the sessions list shows both, one of them current.
	other := e.browser()
	expect(t, other.do("POST", "/auth/login", map[string]any{"email": email, "password": password}), 200, "")
	r = b.do("GET", "/me/sessions", nil)
	expect(t, r, 200, "")
	var sessions []map[string]any
	_ = json.Unmarshal([]byte(r.raw), &sessions)
	if len(sessions) != 2 {
		t.Fatalf("sessions = %s", r.raw)
	}
	var otherID string
	for _, s := range sessions {
		if s["current"] == false {
			otherID = s["id"].(string)
		}
	}
	expect(t, b.do("DELETE", "/me/sessions/"+otherID, nil), 204, "")
	expect(t, other.do("GET", "/me", nil), 401, "unauthenticated")
	expect(t, b.do("DELETE", "/me/sessions/"+otherID, nil), 404, "not_found")
	expect(t, b.do("DELETE", "/me/sessions/not-hex", nil), 422, "validation_failed")

	// Password change: wrong current password is a field error; success rotates every session.
	expect(t, b.do("POST", "/me/password", map[string]any{"currentPassword": "nope nope nope", "newPassword": "brand new password"}), 422, "validation_failed")
	expect(t, other.do("POST", "/auth/login", map[string]any{"email": email, "password": password}), 200, "")
	r = b.do("POST", "/me/password", map[string]any{"currentPassword": password, "newPassword": "brand new password"})
	expect(t, r, 204, "")
	third := b.cookie()
	if third == second {
		t.Error("password change kept the session token")
	}
	expect(t, b.do("GET", "/me", nil), 200, "")
	expect(t, other.do("GET", "/me", nil), 401, "unauthenticated")
	b.setCookie(second)
	expect(t, b.do("GET", "/me", nil), 401, "unauthenticated")
	b.setCookie(third)

	expect(t, b.do("GET", "/me/export", nil), 501, "not_implemented")
}

func TestPreferencesHTTP(t *testing.T) {
	e := newAuthEnv(t, config.EnvTest, false)
	b := e.browser()
	expect(t, b.do("POST", "/auth/signup", map[string]any{"email": testEmail(t), "password": password}), 201, "")
	r := b.do("GET", "/preferences", nil)
	expect(t, r, 200, "")
	if r.body["version"] != float64(0) {
		t.Errorf("initial prefs = %s", r.raw)
	}
	r = b.do("PUT", "/preferences", map[string]any{"version": 0, "prefs": map[string]any{"theme": "dark", "uiScale": 1.1}})
	expect(t, r, 200, "")
	if r.body["version"] != float64(1) {
		t.Errorf("first save = %s", r.raw)
	}
	r = b.do("PUT", "/preferences", map[string]any{"version": 0, "prefs": map[string]any{"theme": "light"}})
	expect(t, r, 409, "stale_version")
	cur, _ := r.body["current"].(map[string]any)
	if cur["version"] != float64(1) || !strings.Contains(r.raw, `"dark"`) {
		t.Errorf("409 body = %s", r.raw)
	}
	expect(t, b.do("PUT", "/preferences", map[string]any{"version": 1, "prefs": []int{1}}), 422, "validation_failed")
	expect(t, b.do("PUT", "/preferences", map[string]any{"version": 1, "prefs": map[string]any{"theme": "light"}}), 200, "")
}

func TestCookieAttributes(t *testing.T) {
	for _, c := range []struct {
		env    config.Env
		secure bool
		want   bool
	}{{config.EnvDev, false, false}, {config.EnvDev, true, true}, {config.EnvProd, true, true}} {
		e := newAuthEnv(t, c.env, c.secure)
		r := e.browser().do("POST", "/auth/signup", map[string]any{"email": fmt.Sprintf("%s-%s-%v", c.env, testEmail(t), c.secure), "password": password})
		expect(t, r, 201, "")
		sc := r.header.Get("Set-Cookie")
		for _, part := range []string{SessionCookie + "=", "Path=/", "HttpOnly", "SameSite=Lax", "Max-Age=2592000"} {
			if !strings.Contains(sc, part) {
				t.Errorf("%s: cookie %q lacks %s", c.env, sc, part)
			}
		}
		if strings.Contains(sc, "Secure") != c.want {
			t.Errorf("env %s, FINUANCE_COOKIE_SECURE=%v: cookie %q, Secure want %v", c.env, c.secure, sc, c.want)
		}
	}
}

func TestCSRF(t *testing.T) {
	e := newAuthEnv(t, config.EnvTest, false)
	body := map[string]any{"email": testEmail(t), "password": password}
	b := e.browser()
	delete(b.header, "X-Finuance-Client")
	expect(t, b.do("POST", "/auth/signup", body), 403, "forbidden")
	b = e.browser()
	b.header["Origin"] = "https://evil.example"
	expect(t, b.do("POST", "/auth/signup", body), 403, "forbidden")
	b.header["Origin"] = origin + ".evil.example"
	expect(t, b.do("POST", "/auth/signup", body), 403, "forbidden")
	// No Origin (non-browser clients) but the header: allowed.
	b = e.browser()
	delete(b.header, "Origin")
	expect(t, b.do("POST", "/auth/signup", body), 201, "")
	// GET needs neither.
	b.header = map[string]string{}
	expect(t, b.do("GET", "/me", nil), 200, "")
	// A protected unsafe request without the header is refused before anything else.
	expect(t, b.do("PATCH", "/me", map[string]any{"displayName": "x"}), 403, "forbidden")
}

func TestLoginRateLimit(t *testing.T) {
	e := newAuthEnv(t, config.EnvTest, false)
	b := e.browser()
	body := map[string]any{"email": testEmail(t), "password": "wrong password!"}
	for range 10 {
		expect(t, b.do("POST", "/auth/login", body), 401, "unauthenticated")
	}
	r := b.do("POST", "/auth/login", body)
	expect(t, r, 429, "rate_limited")
	ra, err := strconv.Atoi(r.header.Get("Retry-After"))
	if err != nil || ra < 1 || ra > 60 {
		t.Errorf("Retry-After = %q", r.header.Get("Retry-After"))
	}
	// Other emails from the same IP still work until the per-IP limit (30/min): 11 attempts so far
	// (the rejected one counted against the IP too), so 19 more pass and the 20th is refused.
	other := func(i int) map[string]any {
		return map[string]any{"email": fmt.Sprintf("other%d-%s", i, testEmail(t)), "password": "wrong password!"}
	}
	for i := range 19 {
		expect(t, b.do("POST", "/auth/login", other(i)), 401, "unauthenticated")
	}
	expect(t, b.do("POST", "/auth/login", other(99)), 429, "rate_limited")
	// Tokens come back with time.
	e.clock.Add(time.Minute)
	expect(t, b.do("POST", "/auth/login", body), 401, "unauthenticated")
}

// TestDeleteMeRemovesEverything fills every user-owned table, deletes the account over HTTP and
// counts the user's rows in every table that has a user_id column (read from the catalog).
func TestDeleteMeRemovesEverything(t *testing.T) {
	e := newAuthEnv(t, config.EnvTest, false)
	b := e.browser()
	r := b.do("POST", "/auth/signup", map[string]any{"email": testEmail(t), "password": password})
	expect(t, r, 201, "")
	u := uuid.MustParse(r.body["id"].(string))
	keep := e.browser()
	r = keep.do("POST", "/auth/signup", map[string]any{"email": "keep-" + testEmail(t), "password": password})
	expect(t, r, 201, "")
	kept := uuid.MustParse(r.body["id"].(string))
	for _, who := range []uuid.UUID{u, kept} {
		fillEveryTable(t, e.st, who)
	}

	tables := userTables(t)
	before := countRows(t, e.st, u, tables)
	for _, tb := range tables {
		if before[tb] == 0 {
			t.Errorf("%s has no rows for the user before deletion; fillEveryTable must cover it", tb)
		}
	}
	expect(t, b.do("DELETE", "/me", map[string]any{"password": "not the password"}), 422, "validation_failed")
	r = b.do("DELETE", "/me", map[string]any{"password": password})
	expect(t, r, 204, "")
	if !strings.Contains(r.header.Get("Set-Cookie"), "Max-Age=0") {
		t.Errorf("cookie not cleared: %q", r.header.Get("Set-Cookie"))
	}
	for tb, n := range countRows(t, e.st, u, tables) {
		if n != 0 {
			t.Errorf("%s: %d rows of the deleted user remain", tb, n)
		}
	}
	for tb, n := range countRows(t, e.st, kept, tables) {
		if n == 0 {
			t.Errorf("%s: the other user's rows were deleted", tb)
		}
	}
	expect(t, b.do("GET", "/me", nil), 401, "unauthenticated")
}

// TestLogsNeverContainSecrets runs the auth flow and checks the captured access log for the
// passwords, the email and the session tokens.
func TestLogsNeverContainSecrets(t *testing.T) {
	e := newAuthEnv(t, config.EnvTest, false)
	b := e.browser()
	email := testEmail(t)
	expect(t, b.do("POST", "/auth/signup", map[string]any{"email": email, "password": password}), 201, "")
	t1 := b.cookie()
	expect(t, b.do("POST", "/auth/login", map[string]any{"email": email, "password": "wrong password!"}), 401, "")
	expect(t, b.do("POST", "/auth/login", map[string]any{"email": email, "password": password}), 200, "")
	t2 := b.cookie()
	expect(t, b.do("POST", "/me/password", map[string]any{"currentPassword": password, "newPassword": "brand new password"}), 204, "")
	t3 := b.cookie()
	expect(t, b.do("GET", "/me", nil), 200, "")
	logs := e.logs.String()
	if !strings.Contains(logs, `"msg":"request"`) || !strings.Contains(logs, `"user":"`) {
		t.Fatalf("no access log lines with a user hash captured: %s", logs)
	}
	for _, secret := range []string{password, "wrong password!", "brand new password", email, t1, t2, t3, SessionCookie + "="} {
		if strings.Contains(logs, secret) {
			t.Errorf("the log contains %q", secret)
		}
	}
}

func userTables(t *testing.T) []string {
	t.Helper()
	rows, err := testDB.Owner.Query(context.Background(), `
		SELECT c.relname::text FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace AND n.nspname = 'public'
		WHERE c.relkind = 'r' AND EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = c.oid AND a.attname = 'user_id' AND NOT a.attisdropped)
		ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// countRows counts u's rows per table. Sessions (outside RLS) are counted without a user; the rest
// under WithUser, so RLS can't hide a leftover from another user.
func countRows(t *testing.T, st *store.Store, u uuid.UUID, tables []string) map[string]int {
	t.Helper()
	out := map[string]int{}
	ctx := context.Background()
	for _, tb := range tables {
		run := st.WithUser
		if tb == "sessions" {
			run = func(ctx context.Context, _ uuid.UUID, fn store.TxFunc) error { return st.WithoutUser(ctx, fn) }
		}
		var n int
		err := run(ctx, u, func(_ *db.Queries, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM `+tb+` WHERE user_id = $1`, u).Scan(&n)
		})
		if err != nil {
			t.Fatal(err)
		}
		out[tb] = n
	}
	return out
}

// fillEveryTable gives u rows in every user-owned table, through the ledger and the queries.
func fillEveryTable(t *testing.T, st *store.Store, u uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	l := ledger.New(st, platform.SystemClock{})
	acc, err := l.CreateAccount(ctx, u, ledger.AccountInput{Name: "Konto"})
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("Data;Opis;Kwota;Kategoria\n02.01.2025;Kawa;-12,00;Jedzenie\n03.01.2025;Pensja;100,00;Wynagrodzenie\n")
	m := ingest4(t, content)
	m.AccountID = &acc.ID
	imp, err := l.CreateImport(ctx, u, m)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.EnsureCategories(ctx, u, []string{"Jedzenie"}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.CommitImport(ctx, u, imp.ID); err != nil {
		t.Fatal(err)
	}
	g, err := l.CreateGroup(ctx, u, "Trip", -1)
	if err != nil {
		t.Fatal(err)
	}
	cats, _ := l.ListCategories(ctx, u)
	if err := l.SetBudget(ctx, u, cats[0].ID, 1000); err != nil {
		t.Fatal(err)
	}
	if err := l.SaveProfile(ctx, u, ledger.Profile{HeaderSignature: [32]byte{1}, Mapping: []byte(`{}`), Options: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	var txIDs []uuid.UUID
	err = st.WithUser(ctx, u, func(q *db.Queries, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id FROM transactions WHERE user_id = $1`, u)
		if err != nil {
			return err
		}
		if txIDs, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID]); err != nil {
			return err
		}
		now := time.Now()
		if _, err := q.SetDedupeOverride(ctx, db.SetDedupeOverrideParams{AccountID: acc.ID, Fingerprint: []byte{9}, UserID: u, Mode: "keep_both", Now: now}); err != nil {
			return err
		}
		if _, err := q.SaveInvestmentProfile(ctx, db.SaveInvestmentProfileParams{UserID: u, SwrBp: 400, RealReturnBp: 500, Now: now}); err != nil {
			return err
		}
		if _, err := q.CreateInvestmentPosition(ctx, db.CreateInvestmentPositionParams{ID: platform.NewID(), UserID: u, Name: "ETF", Now: now}); err != nil {
			return err
		}
		_, err = q.SavePreferences(ctx, db.SavePreferencesParams{UserID: u, Prefs: []byte(`{}`), Now: now})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) { // the HTTP test may have saved them already
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(txIDs) == 0 {
		t.Fatal("no transactions committed")
	}
	if _, err := l.Annotate(ctx, u, txIDs[:1], ledger.AnnotationPatch{SetGroup: true, GroupID: &g.ID}); err != nil {
		t.Fatal(err)
	}
}

// ingest4 parses and stages a small file with the detected mapping.
func ingest4(t *testing.T, content []byte) ledger.NewImport {
	t.Helper()
	res, err := ingest.Parse(context.Background(), content, ingest.Options{})
	if err != nil {
		t.Fatal(err)
	}
	rows, _, err := ingest.Stage(context.Background(), res.Table, ingest.MappingFromRoles(res.Roles), ingest.StageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return ledger.NewImport{Filename: "a.csv", Content: content, Encoding: "utf-8", Delimiter: ";", Headers: res.Table.Headers,
		Mapping: []byte(`{}`), Options: []byte(`{}`), MappingSource: "detected", Rows: rows}
}
