package middleware

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/x2db736ac85h/finuance/backend/internal/observability"
)

// Metrics records request counts and durations labelled by route pattern (never the raw path,
// which would explode label cardinality). It records in a defer: a request whose handler panics
// counts as a 5xx and burns the availability budget like any other failure (PLAN 24.1).
func Metrics(m *observability.Metrics) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r, info := withInfo(r)
			start := time.Now()
			m.HTTPInFlight.Inc()
			defer m.HTTPInFlight.Dec()
			rec := record(w)
			panicked := true
			defer func() {
				route := info.Route
				if route == "" { // a panic before or inside the handler: Route has not recorded the pattern yet
					route = routeName(r.Pattern)
				}
				m.HTTPRequests.WithLabelValues(route, r.Method, observability.StatusClass(rec.outcome(panicked))).Inc()
				// An exemplar: the trace id of this request on the bucket it falls into, so a slow bucket
				// leads to a trace (PLAN 24.5). Only scrapers that ask for OpenMetrics receive it.
				d := time.Since(start).Seconds()
				if eo, ok := m.HTTPDuration.WithLabelValues(route).(prometheus.ExemplarObserver); ok && info.TraceID != "" {
					eo.ObserveWithExemplar(d, prometheus.Labels{"trace_id": info.TraceID})
				} else {
					m.HTTPDuration.WithLabelValues(route).Observe(d)
				}
			}()
			next.ServeHTTP(rec, r)
			panicked = false
		})
	}
}
