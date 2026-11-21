package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// Metrics holds the process-wide Prometheus registry and the HTTP metrics.
// Domain packages register their own collectors on Registry.
type Metrics struct {
	Registry     *prometheus.Registry
	HTTPRequests *prometheus.CounterVec
	HTTPDuration *prometheus.HistogramVec
	HTTPInFlight prometheus.Gauge
	Panics       prometheus.Counter
	RateLimited  *prometheus.CounterVec // finuance_rate_limited_total{limiter}
	AuthFailures *prometheus.CounterVec // finuance_auth_failures_total{reason}
	ClientErrors *prometheus.CounterVec // finuance_client_errors_total{kind}
	BuildInfo    *prometheus.GaugeVec
	collectors   []prometheus.Collector
}

// NewMetrics creates a registry with Go runtime and process collectors plus the HTTP metrics.
func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()
	m := &Metrics{
		Registry: reg,
		HTTPRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_server_requests_total",
			Help: "HTTP requests by route pattern, method, status class and traffic (real, or synthetic for the simulator's users).",
		}, []string{"route", "method", "status_class", "traffic"}),
		HTTPDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "http_server_request_duration_seconds",
			Help: "HTTP request duration by route pattern.",
			// 0.3 s is the latency SLO's threshold (docs/SLO.md): the SLI counts requests in that bucket.
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .3, .5, 1, 2.5, 5, 10, 30},
		}, []string{"route", "traffic"}),
		HTTPInFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "http_server_requests_in_flight",
			Help: "HTTP requests currently being served.",
		}),
		Panics: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "finuance_panics_total",
			Help: "Requests whose handler panicked (each is also a 5xx in http_server_requests_total).",
		}),
		RateLimited: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "finuance_rate_limited_total",
			Help: "Requests refused by one of the api's own limiters, by limiter (a fixed list of names).",
		}, []string{"limiter"}),
		AuthFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "finuance_auth_failures_total",
			Help: "Failed sign-ins by reason: password (a wrong e-mail address or password) or second_step (a wrong code).",
		}, []string{"reason"}),
		ClientErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "finuance_client_errors_total",
			Help: "Errors in the browser, by kind: error, unhandled_rejection, render (reported by the web app) or csp (a content security policy violation reported by the browser).",
		}, []string{"kind"}),
		BuildInfo: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "finuance_build_info",
			Help: "Always 1; labels carry the build version and commit.",
		}, []string{"version", "commit"}),
	}
	m.collectors = []prometheus.Collector{collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.HTTPRequests, m.HTTPDuration, m.HTTPInFlight, m.Panics, m.RateLimited, m.AuthFailures, m.ClientErrors, m.BuildInfo}
	reg.MustRegister(m.collectors...)
	return m
}

// Collectors are the collectors NewMetrics registered (the metric-name list in the deployment
// checks describes them).
func (m *Metrics) Collectors() []prometheus.Collector { return m.collectors }

// StatusClass maps 204 → "2xx", 404 → "4xx".
func StatusClass(status int) string {
	switch {
	case status >= 500:
		return "5xx"
	case status >= 400:
		return "4xx"
	case status >= 300:
		return "3xx"
	case status >= 200:
		return "2xx"
	}
	return "1xx"
}
