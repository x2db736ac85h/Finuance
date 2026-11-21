package httpapi

import (
	"net/http"
	"strings"
	"testing"

	promtest "github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/x2db736ac85h/finuance/backend/internal/config"
)

// TestClientEvents: the web app reports an error without a session; it is counted by kind and
// written as one line with the fixed fields. Anything that could be free text is refused by the
// contract: a message in the name, a path with a query in the route, an unknown field, a body over
// 1 KiB. Reports are limited per client address (PLAN 24.8).
func TestClientEvents(t *testing.T) {
	e := newAuthEnv(t, config.EnvTest, false)
	b := e.browser() // not signed in
	ok := map[string]any{"kind": "error", "name": "TypeError", "route": "/accounts/:id", "version": "0.5.0"}
	if r := b.do("POST", "/client-events", ok); r.code != 204 {
		t.Fatalf("a report: %d %s", r.code, r.raw)
	}
	if r := b.do("POST", "/client-events", map[string]any{"kind": "render", "route": "/"}); r.code != 204 {
		t.Fatalf("a report without a name: %d %s", r.code, r.raw)
	}
	if got := promtest.ToFloat64(e.metrics.ClientErrors.WithLabelValues("error")); got != 1 {
		t.Errorf("finuance_client_errors_total{kind=error} = %v, want 1", got)
	}
	logs := e.logs.String()
	if !strings.Contains(logs, `"msg":"client error"`) || !strings.Contains(logs, `"error_name":"TypeError"`) || !strings.Contains(logs, `"screen":"/accounts/:id"`) {
		t.Errorf("no log line for the report:\n%s", logs)
	}

	const marker = "CANARY the user typed this"
	for name, body := range map[string]map[string]any{
		"a message as the name":      {"kind": "error", "name": marker, "route": "/"},
		"a query in the route":       {"kind": "error", "name": "TypeError", "route": "/search?q=" + marker},
		"an unknown kind":            {"kind": marker, "route": "/"},
		"an extra field":             {"kind": "error", "route": "/", "message": marker},
		"a version with other signs": {"kind": "error", "route": "/", "version": marker},
		"more than 1 KiB":            {"kind": "error", "route": "/" + strings.Repeat("a", 2000)},
	} {
		if r := b.do("POST", "/client-events", body); r.code < 400 || r.code >= 500 {
			t.Errorf("%s: %d %s, want a refusal", name, r.code, r.raw)
		}
	}
	if strings.Contains(e.logs.String(), "CANARY") {
		t.Errorf("a refused report reached the log:\n%s", e.logs.String())
	}

	// the limit per address: thirty a minute, refusals above counted too
	var limited int
	for range 40 {
		if r := b.do("POST", "/client-events", ok); r.code == 429 {
			limited++
		}
	}
	if limited == 0 {
		t.Error("forty more reports in a row were all accepted")
	}
	if got := promtest.ToFloat64(e.metrics.RateLimited.WithLabelValues("client_events")); int(got) != limited {
		t.Errorf("finuance_rate_limited_total{limiter=client_events} = %v, want %d", got, limited)
	}
}

// TestCSPReports: a browser's content security policy report arrives without the client header
// and in its own format. It is counted and its content is never logged; nothing else on that
// route works without the header.
func TestCSPReports(t *testing.T) {
	e := newAuthEnv(t, config.EnvTest, false)
	const marker = "CANARY-blocked-uri"
	post := func(path, contentType, body string) int {
		req, err := http.NewRequest(http.MethodPost, e.srv.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", contentType) // no X-Finuance-Client: a browser's own request
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		return res.StatusCode
	}
	if code := post("/api/v1/csp-reports", "application/csp-report", `{"csp-report":{"blocked-uri":"https://evil.example/`+marker+`","violated-directive":"script-src"}}`); code != 204 {
		t.Fatalf("a report: %d", code)
	}
	if code := post("/api/v1/csp-reports", "application/reports+json", `[{"type":"csp-violation","body":{"blockedURL":"`+marker+`"}}]`); code != 204 {
		t.Fatalf("a report in the newer format: %d", code)
	}
	if got := promtest.ToFloat64(e.metrics.ClientErrors.WithLabelValues("csp")); got != 2 {
		t.Errorf("finuance_client_errors_total{kind=csp} = %v, want 2", got)
	}
	if strings.Contains(e.logs.String(), marker) {
		t.Errorf("the report's content reached the log:\n%s", e.logs.String())
	}
	if code := post("/api/v1/csp-reports", "application/csp-report", strings.Repeat("x", 9<<10)); code != 413 {
		t.Errorf("a report over the size limit: %d, want 413", code)
	}
	// the exemption is this route's alone
	if code := post("/api/v1/client-events", "application/json", `{"kind":"error","route":"/"}`); code != 403 {
		t.Errorf("the web app's route without the client header: %d, want 403", code)
	}
}
