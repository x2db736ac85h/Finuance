package middleware

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/trace"

	"github.com/x2db736ac85h/finuance/backend/internal/observability"
)

// Tracing starts a server span per request and propagates traceparent. The span is renamed to the
// route pattern by Route once the mux has matched.
func Tracing(t *observability.Tracing) Middleware {
	return func(next http.Handler) http.Handler {
		return otelhttp.NewHandler(next, "http.request",
			otelhttp.WithTracerProvider(t.Provider),
			otelhttp.WithPropagators(t.Propagator),
			otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string { return r.Method }))
	}
}

// Route wraps the mux. After routing it records the matched pattern (ServeMux sets r.Pattern on the
// request it receives), renames the span, and copies the trace id into Info for the access log.
func Route(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r, info := withInfo(r)
		span := trace.SpanFromContext(r.Context())
		if sc := span.SpanContext(); sc.HasTraceID() {
			info.TraceID = sc.TraceID().String()
		}
		next.ServeHTTP(w, r)
		info.Route = r.Pattern
		if info.Route == "" || info.Route == "/" || info.Route == "/api/v1/" {
			info.Route = "unmatched"
		}
		span.SetName(info.Route)
	})
}
