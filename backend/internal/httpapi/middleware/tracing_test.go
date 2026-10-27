package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/x2db736ac85h/finuance/backend/internal/observability"
)

// TestSpanNamedAfterRoute: a request's span is named after the matched pattern, not the bare
// method (otelhttp renames it at the end, so the name must come from its formatter), and unmatched
// paths share one name.
func TestSpanNamedAfterRoute(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tr := &observability.Tracing{Provider: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)), Propagator: propagation.TraceContext{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/transactions/query", func(http.ResponseWriter, *http.Request) {})
	mux.HandleFunc("/api/v1/", http.NotFound)
	h := Chain(mux, AssignRequestID, Tracing(tr), Route) // the order of the server
	for _, path := range []string{"/api/v1/transactions/query", "/api/v1/nothing"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, path, nil))
	}
	spans := rec.Ended()
	if len(spans) != 2 || spans[0].Name() != "POST /api/v1/transactions/query" || spans[1].Name() != "unmatched" {
		names := []string{}
		for _, s := range spans {
			names = append(names, s.Name())
		}
		t.Errorf("span names = %v", names)
	}
}
