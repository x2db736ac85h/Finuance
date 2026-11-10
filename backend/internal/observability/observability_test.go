package observability

import (
	"bytes"
	"context"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/x2db736ac85h/finuance/backend/internal/config"
)

func TestLoggerFormat(t *testing.T) {
	var b bytes.Buffer
	NewLogger(&b, config.EnvProd, slog.LevelInfo).Info("hello", "k", 1)
	if !strings.HasPrefix(b.String(), "{") {
		t.Errorf("prod log is not JSON: %s", b.String())
	}
	b.Reset()
	l := NewLogger(&b, config.EnvDev, slog.LevelWarn)
	l.Info("hidden")
	l.Warn("shown")
	if strings.Contains(b.String(), "hidden") || !strings.Contains(b.String(), "level=WARN msg=shown") {
		t.Errorf("dev log: %q", b.String())
	}
	ctx := WithLogger(context.Background(), l)
	if Logger(ctx) != l || Logger(context.Background()) != slog.Default() {
		t.Error("context logger round trip")
	}
}

func TestStatusClass(t *testing.T) {
	for in, want := range map[int]string{101: "1xx", 200: "2xx", 204: "2xx", 304: "3xx", 404: "4xx", 499: "4xx", 500: "5xx", 503: "5xx"} {
		if got := StatusClass(in); got != want {
			t.Errorf("StatusClass(%d) = %s, want %s", in, got, want)
		}
	}
}

func TestNoopTracingAndAdmin(t *testing.T) {
	tr, err := NewTracing(context.Background(), "", "finuance", "test")
	if err != nil || tr.Provider == nil || tr.Shutdown(context.Background()) != nil {
		t.Fatalf("noop tracing: %v", err)
	}
	m := NewMetrics()
	m.HTTPRequests.WithLabelValues("GET /x", "GET", "2xx").Inc()
	srv := NewAdminServer(":0", m, true, nil)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	for _, want := range []string{"http_server_requests_total", "go_goroutines", "process_cpu_seconds_total"} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("/metrics lacks %s", want)
		}
	}
	w = httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, httptest.NewRequest("GET", "/debug/pprof/", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "goroutine") {
		t.Errorf("pprof index: %d", w.Code)
	}
	// Profiling is off unless asked for: the admin port then has metrics and nothing else.
	off := NewAdminServer(":0", m, false, nil)
	for path, want := range map[string]int{"/metrics": 200, "/debug/pprof/": 404, "/debug/pprof/profile": 404, "/debug/pprof/trace": 404} {
		w = httptest.NewRecorder()
		off.Handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != want {
			t.Errorf("profiling off: GET %s = %d, want %d", path, w.Code, want)
		}
	}
}

// The latency SLO counts requests faster than 300 ms (docs/SLO.md); the histogram needs that
// bucket edge, or the SLI would have to round to 250 or 500 ms.
func TestLatencyHistogramHasTheSLOBucket(t *testing.T) {
	m := NewMetrics()
	m.HTTPDuration.WithLabelValues("POST /api/v1/transactions/query").Observe(0.27)
	srv := NewAdminServer(":0", m, false, nil)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	if !strings.Contains(w.Body.String(), `http_server_request_duration_seconds_bucket{route="POST /api/v1/transactions/query",le="0.3"} 1`) {
		t.Error("no le=0.3 bucket counting a 270 ms request")
	}
}
