package middleware

import (
	"net/http"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/observability"
)

// Metrics records request counts and durations labelled by route pattern (never the raw path,
// which would explode label cardinality).
func Metrics(m *observability.Metrics) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r, info := withInfo(r)
			start := time.Now()
			m.HTTPInFlight.Inc()
			defer m.HTTPInFlight.Dec()
			rec := record(w)
			next.ServeHTTP(rec, r)
			route := info.Route
			if route == "" {
				route = "unmatched"
			}
			m.HTTPRequests.WithLabelValues(route, r.Method, observability.StatusClass(rec.code())).Inc()
			m.HTTPDuration.WithLabelValues(route).Observe(time.Since(start).Seconds())
		})
	}
}
