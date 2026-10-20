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
	srv := NewAdminServer(":0", m)
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
}
