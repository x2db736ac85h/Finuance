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
			Help: "HTTP requests by route pattern, method and status class.",
		}, []string{"route", "method", "status_class"}),
		HTTPDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "http_server_request_duration_seconds",
			Help: "HTTP request duration by route pattern.",
			// 0.3 s is the latency SLO's threshold (docs/SLO.md): the SLI counts requests in that bucket.
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .3, .5, 1, 2.5, 5, 10, 30},
		}, []string{"route"}),
		HTTPInFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "http_server_requests_in_flight",
			Help: "HTTP requests currently being served.",
		}),
		BuildInfo: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "finuance_build_info",
			Help: "Always 1; labels carry the build version and commit.",
		}, []string{"version", "commit"}),
	}
	m.collectors = []prometheus.Collector{collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.HTTPRequests, m.HTTPDuration, m.HTTPInFlight, m.BuildInfo}
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
