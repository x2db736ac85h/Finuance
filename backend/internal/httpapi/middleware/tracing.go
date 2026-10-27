package middleware

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/trace"

	"github.com/x2db736ac85h/finuance/backend/internal/observability"
)

// Tracing starts a server span per request and propagates traceparent. The span starts as the
// method and ends as the route pattern ("POST /api/v1/transactions/query"): otelhttp renames it
// with this formatter once the mux has set the request's pattern (and would undo any other rename).
func Tracing(t *observability.Tracing) Middleware {
	return func(next http.Handler) http.Handler {
		return otelhttp.NewHandler(next, "http.request",
			otelhttp.WithTracerProvider(t.Provider),
			otelhttp.WithPropagators(t.Propagator),
			otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
				if r.Pattern == "" {
					return r.Method
				}
				return routeName(r.Pattern)
			}))
	}
}

// routeName is the route label of a matched pattern; the catch-alls count as "unmatched".
func routeName(pattern string) string {
	if pattern == "" || pattern == "/" || pattern == "/api/v1/" {
		return "unmatched"
	}
	return pattern
}

// Route wraps the mux. After routing it records the matched pattern (ServeMux sets r.Pattern on the
// request it receives), names the span after it, and copies the trace id into Info for the access log.
func Route(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r, info := withInfo(r)
		span := trace.SpanFromContext(r.Context())
		if sc := span.SpanContext(); sc.HasTraceID() {
			info.TraceID = sc.TraceID().String()
		}
		next.ServeHTTP(w, r)
		info.Route = routeName(r.Pattern)
		span.SetName(info.Route) // otelhttp names it the same when it ends (Tracing)
	})
}
