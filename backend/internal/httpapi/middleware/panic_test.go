package middleware

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/x2db736ac85h/finuance/backend/internal/observability"
)

// chain is the server's order of the layers these tests are about.
func chain(t *testing.T, mux *http.ServeMux, level slog.Level) (http.Handler, *bytes.Buffer, *observability.Metrics, *tracetest.SpanRecorder) {
	t.Helper()
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: level}))
	m := observability.NewMetrics()
	spans := tracetest.NewSpanRecorder()
	tr := &observability.Tracing{Provider: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)), Propagator: propagation.TraceContext{}}
	h := Chain(mux,
		Recover(logger, m.Panics.Inc, func(w http.ResponseWriter, _ *http.Request, _ error) {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"status":500}`))
		}),
		AssignRequestID, Logging(logger), Metrics(m), Tracing(tr), Route)
	return h, &logs, m, spans
}

func logLines(t *testing.T, logs *bytes.Buffer, msg string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}
		if rec["msg"] == msg {
			out = append(out, rec)
		}
	}
	return out
}

// TestPanicIsCounted: a handler that panics still yields one access line with status 500, one 5xx
// in the request counter under its route, one panic in finuance_panics_total, a span with error
// status and the 500 problem response (PLAN 24.1). Before, the access line and the counter were
// written after the handler returned, so a panic skipped both and never burned the error budget.
func TestPanicIsCounted(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/boom", func(http.ResponseWriter, *http.Request) { panic("boom") })
	h, logs, m, spans := chain(t, mux, slog.LevelInfo)

	res := httptest.NewRecorder()
	h.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/v1/boom", nil))

	if res.Code != http.StatusInternalServerError || !strings.Contains(res.Body.String(), `"status":500`) {
		t.Errorf("response = %d %q, want the 500 problem", res.Code, res.Body.String())
	}
	access := logLines(t, logs, "request")
	if len(access) != 1 {
		t.Fatalf("%d access lines, want 1:\n%s", len(access), logs.String())
	}
	if a := access[0]; a["status"] != float64(500) || a["route"] != "GET /api/v1/boom" || a["level"] != "ERROR" || a["trace_id"] == "" {
		t.Errorf("access line = %v", a)
	}
	p := logLines(t, logs, "panic")
	if len(p) != 1 || p[0]["request_id"] != access[0]["request_id"] || p[0]["trace_id"] != access[0]["trace_id"] {
		t.Errorf("panic lines = %v, want one with the access line's request id and trace id", p)
	}
	if access[0]["span_id"] == nil || access[0]["span_id"] == "" {
		t.Errorf("the access line has no span_id: %v", access[0])
	}
	for k := range access[0] { // the access line uses the contract's fields only (docs/observability.md)
		if !slices.Contains(observability.LogFields, k) {
			t.Errorf("access-line field %q is not in the contract", k)
		}
	}
	if got := testutil.ToFloat64(m.HTTPRequests.WithLabelValues("GET /api/v1/boom", "GET", "5xx", "real")); got != 1 {
		t.Errorf("5xx count = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.Panics); got != 1 {
		t.Errorf("finuance_panics_total = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.HTTPInFlight); got != 0 {
		t.Errorf("in flight = %v after the request", got)
	}
	ended := spans.Ended()
	if len(ended) != 1 {
		t.Fatalf("%d spans, want 1", len(ended))
	}
	if s := ended[0]; s.Status().Code != codes.Error || s.Name() != "GET /api/v1/boom" {
		t.Errorf("span %q status = %v, want error", s.Name(), s.Status())
	}
}

// TestPanicAfterTheResponseStarted: what was already sent stays the recorded status; the panic is
// still counted.
func TestPanicAfterTheResponseStarted(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/half", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		panic("after the header")
	})
	h, logs, m, _ := chain(t, mux, slog.LevelInfo)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/half", nil))
	if access := logLines(t, logs, "request"); len(access) != 1 || access[0]["status"] != float64(200) {
		t.Errorf("access lines = %v, want one with the status that was sent", access)
	}
	if got := testutil.ToFloat64(m.Panics); got != 1 {
		t.Errorf("finuance_panics_total = %v, want 1", got)
	}
}

// TestAbortIsNotAPanic: http.ErrAbortHandler is the standard library's way to drop a connection on
// purpose. It is passed on and not counted as a panic.
func TestAbortIsNotAPanic(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/abort", func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) })
	h, logs, m, _ := chain(t, mux, slog.LevelInfo)
	func() {
		defer func() {
			if err, ok := recover().(error); !ok || !errors.Is(err, http.ErrAbortHandler) {
				t.Errorf("recovered %v, want http.ErrAbortHandler passed on", err)
			}
		}()
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/abort", nil))
	}()
	if got := testutil.ToFloat64(m.Panics); got != 0 {
		t.Errorf("finuance_panics_total = %v, want 0", got)
	}
	if p := logLines(t, logs, "panic"); len(p) != 0 {
		t.Errorf("panic lines = %v", p)
	}
}

// TestProbesAreQuiet: the kubelet's probes get no span, and their access lines are written at debug
// level only; a failing probe is still an error line (PLAN 24.3).
func TestProbesAreQuiet(t *testing.T) {
	newMux := func() *http.ServeMux {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /healthz", func(http.ResponseWriter, *http.Request) {})
		mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) })
		mux.HandleFunc("GET /api/v1/meta", func(http.ResponseWriter, *http.Request) {})
		return mux
	}
	get := func(h http.Handler, path string) {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}

	h, logs, m, spans := chain(t, newMux(), slog.LevelInfo)
	get(h, "/healthz")
	get(h, "/api/v1/meta")
	access := logLines(t, logs, "request")
	if len(access) != 1 || access[0]["route"] != "GET /api/v1/meta" {
		t.Errorf("at info level: access lines = %v, want only the API request", access)
	}
	if ended := spans.Ended(); len(ended) != 1 || ended[0].Name() != "GET /api/v1/meta" {
		t.Errorf("%d spans, want only the API request's", len(ended))
	}
	if got := testutil.ToFloat64(m.HTTPRequests.WithLabelValues("GET /healthz", "GET", "2xx", "real")); got != 1 {
		t.Errorf("the probe is not counted: %v", got) // the metrics still see it
	}

	get(h, "/readyz")
	if access := logLines(t, logs, "request"); len(access) != 2 || access[1]["level"] != "ERROR" || access[1]["status"] != float64(503) {
		t.Errorf("a failing probe: access lines = %v, want an error line", access)
	}

	h, logs, _, _ = chain(t, newMux(), slog.LevelDebug)
	get(h, "/healthz")
	if access := logLines(t, logs, "request"); len(access) != 1 || access[0]["level"] != "DEBUG" {
		t.Errorf("at debug level: access lines = %v, want the probe's", access)
	}
}

// TestDurationCarriesAnExemplar: a traced request leaves its trace id on the latency bucket it
// falls into, and the admin port serves it to a scraper that asks for OpenMetrics (PLAN 24.5).
func TestDurationCarriesAnExemplar(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/meta", func(http.ResponseWriter, *http.Request) {})
	h, logs, m, _ := chain(t, mux, slog.LevelInfo)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/meta", nil))
	traceID, _ := logLines(t, logs, "request")[0]["trace_id"].(string)
	if traceID == "" {
		t.Fatal("the request was not traced")
	}

	admin := observability.NewAdminServer("", m, false, nil)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Accept", "application/openmetrics-text; version=1.0.0")
	res := httptest.NewRecorder()
	admin.Handler.ServeHTTP(res, req)
	body := res.Body.String()
	if !strings.Contains(body, `# {trace_id="`+traceID+`"}`) {
		t.Errorf("no exemplar with trace id %s in the OpenMetrics output:\n%s", traceID, grepLines(body, "http_server_request_duration_seconds_bucket"))
	}
	// the plain text format, which Prometheus asks for without the feature, stays valid without them
	res = httptest.NewRecorder()
	admin.Handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if strings.Contains(res.Body.String(), "trace_id") {
		t.Error("the text format carries an exemplar")
	}
}

func grepLines(s, sub string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, sub) {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}
