package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/x2db736ac85h/finuance/backend/internal/config"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/observability"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

type harness struct {
	h       http.Handler
	metrics *observability.Metrics
	logs    *bytes.Buffer
}

func newHarness(t *testing.T, mut func(*Deps)) harness {
	t.Helper()
	logs := &bytes.Buffer{}
	tr, _ := observability.NewTracing(context.Background(), "", "finuance", "test")
	d := Deps{
		Config:  config.Config{Env: config.EnvTest, AllowSignup: true},
		Version: "v0.0.0-test",
		Logger:  slog.New(slog.NewJSONHandler(logs, nil)),
		Metrics: observability.NewMetrics(),
		Tracing: tr,
	}
	if mut != nil {
		mut(&d)
	}
	h, err := NewHandler(d)
	if err != nil {
		t.Fatal(err)
	}
	return harness{h: h, metrics: d.Metrics, logs: logs}
}

func (hs harness) do(method, path string, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	hs.h.ServeHTTP(w, r)
	return w
}

func TestMeta(t *testing.T) {
	hs := newHarness(t, nil)
	w := hs.do("GET", "/api/v1/meta", nil)
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("status %d, content-type %q, body %s", w.Code, w.Header().Get("Content-Type"), w.Body)
	}
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m["version"] != "v0.0.0-test" || m["signupEnabled"] != true || m["demoMode"] != false || m["assistantEnabled"] != false {
		t.Errorf("meta = %v", m)
	}
}

func TestSecurityHeadersAndRequestID(t *testing.T) {
	hs := newHarness(t, nil)
	w := hs.do("GET", "/healthz", map[string]string{"X-Request-ID": "abc-123_XYZ"})
	for k, v := range map[string]string{"X-Content-Type-Options": "nosniff", "Referrer-Policy": "no-referrer", "X-Frame-Options": "DENY", "Cross-Origin-Resource-Policy": "same-origin",
		"Cache-Control": "no-store", "X-Request-ID": "abc-123_XYZ"} {
		if got := w.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	for _, bad := range []string{"", "has space", strings.Repeat("a", 65), "<script>"} {
		got := hs.do("GET", "/healthz", map[string]string{"X-Request-ID": bad}).Header().Get("X-Request-ID")
		if got == bad || len(got) != 32 {
			t.Errorf("request id %q was accepted or not regenerated: %q", bad, got)
		}
	}
}

func TestNotFoundIsProblemAndUnmatchedLabel(t *testing.T) {
	hs := newHarness(t, nil)
	for _, p := range []string{"/api/v1/nope", "/nope/12345"} {
		w := hs.do("GET", p, nil)
		if w.Code != 404 || w.Header().Get("Content-Type") != "application/problem+json" {
			t.Errorf("%s: %d %q", p, w.Code, w.Header().Get("Content-Type"))
		}
		var pr map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &pr)
		if pr["code"] != "not_found" || pr["type"] != "/problems/not_found" || pr["requestId"] == nil {
			t.Errorf("%s: problem = %v", p, pr)
		}
	}
	if n := testutil.ToFloat64(hs.metrics.HTTPRequests.WithLabelValues("unmatched", "GET", "4xx")); n != 2 {
		t.Errorf("unmatched 4xx = %v, want 2", n)
	}
}

func TestMetricsUseRoutePattern(t *testing.T) {
	hs := newHarness(t, nil)
	hs.do("GET", "/api/v1/meta", nil)
	hs.do("GET", "/api/v1/meta?x=1", nil)
	if n := testutil.ToFloat64(hs.metrics.HTTPRequests.WithLabelValues("GET /api/v1/meta", "GET", "2xx")); n != 2 {
		t.Errorf("meta requests = %v, want 2", n)
	}
	out, err := testutil.CollectAndLint(hs.metrics.HTTPRequests)
	if err != nil || len(out) > 0 {
		t.Errorf("metric lint: %v %v", err, out)
	}
}

func TestAccessLog(t *testing.T) {
	hs := newHarness(t, nil)
	hs.do("GET", "/api/v1/meta?secret=hunter2", map[string]string{"X-Request-ID": "req1", "Cookie": "finuance_session=s3cr3t"})
	var line map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(hs.logs.Bytes()), &line); err != nil {
		t.Fatalf("log line: %v\n%s", err, hs.logs)
	}
	if line["route"] != "GET /api/v1/meta" || line["status"] != float64(200) || line["request_id"] != "req1" || line["msg"] != "request" {
		t.Errorf("log line = %v", line)
	}
	for _, leak := range []string{"hunter2", "s3cr3t"} {
		if strings.Contains(hs.logs.String(), leak) {
			t.Errorf("log leaks %q", leak)
		}
	}
}

func TestPanicBecomesProblem(t *testing.T) {
	hs := newHarness(t, func(d *Deps) {
		d.Extra = func(m *http.ServeMux) {
			m.HandleFunc("GET /test/panic", func(http.ResponseWriter, *http.Request) { panic("boom") })
		}
	})
	w := hs.do("GET", "/test/panic", nil)
	if w.Code != 500 || !strings.Contains(w.Body.String(), `"code":"internal"`) {
		t.Errorf("panic: %d %s", w.Code, w.Body)
	}
	if !strings.Contains(hs.logs.String(), `"panic":"boom"`) || !strings.Contains(hs.logs.String(), "goroutine") {
		t.Error("panic stack not logged")
	}
}

func TestReadyz(t *testing.T) {
	ok := Check{Name: "db", Probe: func(context.Context) error { return nil }}
	bad := Check{Name: "migrations", Probe: func(context.Context) error { return errors.New("version 3 < 5 (postgres://u:pw@h)") }}
	if w := newHarness(t, func(d *Deps) { d.Ready = []Check{ok} }).do("GET", "/readyz", nil); w.Code != 200 {
		t.Errorf("ready: %d %s", w.Code, w.Body)
	}
	w := newHarness(t, func(d *Deps) { d.Ready = []Check{ok, bad} }).do("GET", "/readyz", nil)
	if w.Code != 503 || !strings.Contains(w.Body.String(), `"migrations":"fail"`) || strings.Contains(w.Body.String(), "pw@") {
		t.Errorf("not ready: %d %s", w.Code, w.Body)
	}
	if w := newHarness(t, nil).do("GET", "/healthz", nil); w.Code != 200 {
		t.Errorf("healthz: %d", w.Code)
	}
}

func TestBodyLimit(t *testing.T) {
	hs := newHarness(t, func(d *Deps) {
		d.Extra = func(m *http.ServeMux) {
			m.HandleFunc("POST /test/echo", func(w http.ResponseWriter, r *http.Request) {
				if _, err := io.ReadAll(r.Body); err != nil {
					WriteProblem(w, r, err, false)
				}
			})
		}
	})
	r := httptest.NewRequest("POST", "/test/echo", strings.NewReader(strings.Repeat("x", MaxJSONBody+1)))
	w := httptest.NewRecorder()
	hs.h.ServeHTTP(w, r)
	if w.Code != 413 || !strings.Contains(w.Body.String(), "payload_too_large") {
		t.Errorf("body limit: %d %s", w.Code, w.Body)
	}
}

// Acceptance (task 2.4): a request in flight when shutdown starts still completes.
func TestGracefulShutdownFinishesInFlightRequest(t *testing.T) {
	started := make(chan struct{})
	hs := newHarness(t, func(d *Deps) {
		d.Extra = func(m *http.ServeMux) {
			m.HandleFunc("GET /test/slow", func(w http.ResponseWriter, _ *http.Request) {
				close(started)
				time.Sleep(300 * time.Millisecond)
				fmt.Fprint(w, "done")
			})
		}
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(context.Background())
	srv := NewServer(ln.Addr().String(), hs.h, slog.New(slog.DiscardHandler))
	served := make(chan error, 1)
	go func() { served <- Serve(ctx, srv, ln, 5*time.Second) }()

	type result struct {
		body string
		err  error
	}
	got := make(chan result, 1)
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String() + "/test/slow")
		if err != nil {
			got <- result{err: err}
			return
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		got <- result{string(b), err}
	}()
	<-started
	stop() // what SIGTERM does in app.Serve
	if r := <-got; r.err != nil || r.body != "done" {
		t.Fatalf("in-flight request: %q, %v", r.body, r.err)
	}
	if err := <-served; err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if _, err := http.Get("http://" + ln.Addr().String() + "/healthz"); err == nil {
		t.Error("server still accepts connections after shutdown")
	}
}

func TestProblemMapping(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/v1/x", nil)
	for _, tc := range []struct {
		err    error
		code   string
		status int
	}{
		{platform.ErrUnauthenticated, "unauthenticated", 401},
		{platform.ErrForbidden, "forbidden", 403},
		{fmt.Errorf("account 7: %w", platform.ErrNotFound), "not_found", 404},
		{platform.ErrConflict, "conflict", 409},
		{platform.ErrDuplicateFile, "duplicate_file", 409},
		{platform.ErrImportNotCommitable, "import_not_committable", 409},
		{platform.ErrStaleVersion, "stale_version", 409},
		{platform.ErrRateLimited, "rate_limited", 429},
		{platform.ErrPayloadTooLarge, "payload_too_large", 413},
		{&http.MaxBytesError{Limit: 10}, "payload_too_large", 413},
		{platform.ErrUnsupportedFile, "unsupported_file", 415},
		{(&platform.ValidationError{Fields: []platform.FieldError{{Field: "a", Message: "b"}, {Field: "c", Message: "d"}}}), "validation_failed", 422},
		{fmt.Errorf("get import: %w", ledger.ErrNotFound), "not_found", 404},
		{ledger.ErrConflict, "conflict", 409},
		{ledger.ErrState, "conflict", 409},
		{fmt.Errorf("name too long: %w", ledger.ErrInvalid), "validation_failed", 422},
		{errors.New("pq: relation x does not exist"), "internal", 500},
	} {
		p := ProblemFor(tc.err, r, false)
		if string(p.Code) != tc.code || p.Status != tc.status || p.Type != "/problems/"+tc.code || p.Instance == nil || *p.Instance != "/api/v1/x" {
			t.Errorf("%v → %s %d %s", tc.err, p.Code, p.Status, p.Type)
		}
		var ve *platform.ValidationError
		if errors.As(tc.err, &ve) && (p.Errors == nil || len(*p.Errors) != 2) {
			t.Errorf("validation errors not aggregated: %+v", p.Errors)
		}
	}
	if p := ProblemFor(errors.New("pq: secret SQL"), r, true); p.Detail != nil {
		t.Errorf("prod leaks internal detail: %q", *p.Detail)
	}
	if p := ProblemFor(platform.ErrNotFound, r, true); p.Detail == nil {
		t.Error("prod should keep details of non-internal problems")
	}
}

// TestClientGoneIsNotAServerError: a request whose client went away (its context canceled) is
// answered 499 without a body and without an error log, so it never counts against the
// availability SLO. A cancellation the client didn't cause stays a 500.
func TestClientGoneIsNotAServerError(t *testing.T) {
	var logs bytes.Buffer
	ctx := observability.WithLogger(context.Background(), slog.New(slog.NewTextHandler(&logs, nil)))
	gone, cancel := context.WithCancel(ctx)
	cancel()
	w := httptest.NewRecorder()
	WriteProblem(w, httptest.NewRequest("PUT", "/api/v1/preferences", nil).WithContext(gone), fmt.Errorf("save: %w", context.Canceled), true)
	if w.Code != StatusClientClosed || w.Body.Len() != 0 || logs.Len() != 0 {
		t.Errorf("client gone: %d %q, log %q", w.Code, w.Body, logs.String())
	}

	w = httptest.NewRecorder()
	WriteProblem(w, httptest.NewRequest("PUT", "/api/v1/preferences", nil).WithContext(ctx), fmt.Errorf("job: %w", context.Canceled), true)
	if w.Code != http.StatusInternalServerError || !strings.Contains(logs.String(), "request failed") {
		t.Errorf("own cancellation: %d, log %q", w.Code, logs.String())
	}
}
