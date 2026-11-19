package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

// TestClientTraceContextIsNotTrusted: a traceparent and baggage sent by the client decide nothing.
// The request gets its own trace id (and is sampled although the client said not to), the client's
// context is only a link, and what the handler would hand to a job carries neither the client's
// trace id nor its baggage (PLAN 23.3).
func TestClientTraceContextIsNotTrusted(t *testing.T) {
	const forgedTrace, forgedSpan = "0af7651916cd43dd8448eb211c80319c", "b7ad6b7169203331"
	rec := tracetest.NewSpanRecorder()
	real, err := observability.NewTracing(t.Context(), "", "test", "0")
	if err != nil {
		t.Fatal(err)
	}
	// the propagator of the real setup, with a provider that records
	tr := &observability.Tracing{Provider: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)), Propagator: real.Propagator}
	carrier := propagation.MapCarrier{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/x", func(_ http.ResponseWriter, r *http.Request) {
		tr.Propagator.Inject(r.Context(), carrier) // what jobs.Telemetry stores with an enqueued job
	})
	h := Chain(mux, AssignRequestID, Tracing(tr), Route)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/x", nil)
	req.Header.Set("traceparent", "00-"+forgedTrace+"-"+forgedSpan+"-00") // "not sampled"
	req.Header.Set("tracestate", "vendor=secret-state")
	req.Header.Set("baggage", "user.email=someone@example.com,synthetic=true")
	h.ServeHTTP(httptest.NewRecorder(), req)

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("%d spans", len(spans))
	}
	s := spans[0]
	if got := s.SpanContext().TraceID().String(); got == forgedTrace {
		t.Error("the request continued the client's trace")
	}
	if s.Parent().IsValid() {
		t.Errorf("the span has a parent: %s", s.Parent().SpanID())
	}
	if !s.SpanContext().IsSampled() {
		t.Error("the client's flags switched sampling off")
	}
	if len(s.Links()) != 1 || s.Links()[0].SpanContext.TraceID().String() != forgedTrace {
		t.Errorf("links = %+v, want one link to the client's context", s.Links())
	}
	for k, v := range carrier {
		if k != "traceparent" {
			t.Errorf("a job would carry %s=%q", k, v)
		}
	}
	if tp := carrier["traceparent"]; tp == "" || strings.Contains(tp, forgedTrace) || !strings.Contains(tp, s.SpanContext().TraceID().String()) {
		t.Errorf("a job would carry traceparent %q, want the request's own trace", tp)
	}
	for _, kv := range s.Attributes() {
		if v := kv.Value.String(); strings.Contains(v, "someone@example.com") || strings.Contains(v, "secret-state") {
			t.Errorf("span attribute %s carries what the client sent: %q", kv.Key, v)
		}
	}
}

// TestRequestSpanCarriesNoUserText: what otelhttp records for a request with a search in its query
// string, a browser's user agent and a forwarded client address. After the exporter's redaction
// (observability.Redact, as the real setup exports) the span names the route, the method and the
// status, and nothing of the query, the user agent or the caller's address (PLAN 23.4).
func TestRequestSpanCarriesNoUserText(t *testing.T) {
	mem := tracetest.NewInMemoryExporter()
	tr := &observability.Tracing{Provider: sdktrace.NewTracerProvider(sdktrace.WithSyncer(observability.Redact(mem))), Propagator: propagation.TraceContext{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/duplicates", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) })
	h := Chain(mux, AssignRequestID, Tracing(tr), Route)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/duplicates?q=MARKERQUERY&limit=5", nil)
	req.Header.Set("User-Agent", "MARKERAGENT/1.0")
	req.Header.Set("X-Forwarded-For", "203.0.113.77")
	req.Header.Set("Referer", "https://f.example.com/?q=MARKERREFERER")
	req.RemoteAddr = "198.51.100.23:4711"
	h.ServeHTTP(httptest.NewRecorder(), req)

	spans := mem.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("%d spans", len(spans))
	}
	s := spans[0]
	if s.Name != "GET /api/v1/duplicates" {
		t.Errorf("span name %q", s.Name)
	}
	allowed := map[string]bool{
		"http.request.method": true, "http.response.status_code": true, "http.route": true, "url.path": true, "url.scheme": true,
		"server.address": true, "server.port": true, "network.protocol.version": true, "network.protocol.name": true,
		"http.request.body.size": true, "http.response.body.size": true, "error.type": true,
	}
	for _, kv := range s.Attributes {
		if !allowed[string(kv.Key)] {
			t.Errorf("unexpected span attribute %s = %s (decide: allow it here or drop it in observability.Redact)", kv.Key, kv.Value.String())
		}
	}
	all := fmt.Sprintf("%+v", s)
	for _, marker := range []string{"MARKERQUERY", "MARKERAGENT", "MARKERREFERER", "203.0.113.77", "198.51.100.23"} {
		if strings.Contains(all, marker) {
			t.Errorf("the exported span carries %s", marker)
		}
	}
}
