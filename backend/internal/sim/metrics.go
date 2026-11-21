package sim

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/x2db736ac85h/finuance/backend/internal/sim/identity"
)

// Metrics are the simulator's own metrics (PLAN 25.5), next to what the lab counts of the same
// traffic: the latency the simulator saw can be put next to the server's.
type Metrics struct {
	actions    *prometheus.CounterVec
	duration   *prometheus.HistogramVec
	active     prometheus.Gauge
	paused     prometheus.Gauge
	generation prometheus.Gauge
}

// NewMetrics registers the metrics with reg.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		actions: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "finuance_sim_actions_total",
			Help: "Actions the simulator ran, by persona, action and result (ok, error, skipped)."}, []string{"persona", "action", "result"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "finuance_sim_action_duration_seconds",
			Help:    "How long an action took as the simulator saw it, all its requests together.",
			Buckets: []float64{.025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60}}, []string{"action"}),
		active:     prometheus.NewGauge(prometheus.GaugeOpts{Name: "finuance_sim_active_personas", Help: "Personas the configuration switches on."}),
		paused:     prometheus.NewGauge(prometheus.GaugeOpts{Name: "finuance_sim_paused", Help: "1 while the simulator is paused."}),
		generation: prometheus.NewGauge(prometheus.GaugeOpts{Name: "finuance_sim_config_generation", Help: "The configuration's generation."}),
	}
	reg.MustRegister(m.actions, m.duration, m.active, m.paused, m.generation)
	return m
}

func (m *Metrics) observe(ev Event) {
	m.actions.WithLabelValues(ev.Persona, ev.Action, ev.Result).Inc()
	if ev.Result != ResultSkipped {
		m.duration.WithLabelValues(ev.Action).Observe(ev.Duration.Seconds())
	}
}

func (m *Metrics) config(c *Config, gen int64) {
	on := 0
	for i := range c.Personas {
		if c.personaOn(identity.Nth(i)) {
			on++
		}
	}
	m.active.Set(float64(on))
	m.generation.Set(float64(gen))
	if c.Paused {
		m.paused.Set(1)
	} else {
		m.paused.Set(0)
	}
}
