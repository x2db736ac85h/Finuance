package assistant

import "github.com/prometheus/client_golang/prometheus"

// Metrics are the assistant's Prometheus metrics (PLAN §8.2). They carry no user data.
type Metrics struct {
	Runs      *prometheus.CounterVec   // finuance_assistant_runs_total{mode, result}
	Duration  *prometheus.HistogramVec // finuance_assistant_run_duration_seconds{mode}
	ToolCalls *prometheus.CounterVec   // finuance_assistant_tool_calls_total{tool, result}
	Rounds    prometheus.Histogram     // finuance_assistant_rounds
	Tokens    prometheus.Histogram     // finuance_assistant_tokens_sent_approx
	Upstream  *prometheus.CounterVec   // finuance_assistant_model_requests_total{connection, result}
	// One request to a model server (PLAN 24.5), next to the span llm.request:
	LLMDuration   *prometheus.HistogramVec // finuance_llm_request_duration_seconds{connection}
	LLMFirstToken *prometheus.HistogramVec // finuance_llm_time_to_first_token_seconds{connection}
	LLMTokens     *prometheus.CounterVec   // finuance_llm_tokens_total{connection, direction}: what the server reported
}

// NewMetrics creates the metrics and registers them on reg (nil: unregistered, for tests).
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		Runs: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "finuance_assistant_runs_total",
			Help: "Assistant runs by mode and result (ok, error, stopped, fallback_plan)."}, []string{"mode", "result"}),
		Duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "finuance_assistant_run_duration_seconds",
			Help: "Duration of an assistant run.", Buckets: []float64{.5, 1, 2.5, 5, 10, 20, 30, 60, 120, 300}}, []string{"mode"}),
		ToolCalls: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "finuance_assistant_tool_calls_total",
			Help: "Assistant tool calls by tool and result (ok, error, unknown, repeated)."}, []string{"tool", "result"}),
		Rounds: prometheus.NewHistogram(prometheus.HistogramOpts{Name: "finuance_assistant_rounds",
			Help: "Model requests per assistant run.", Buckets: []float64{1, 2, 3, 4, 5, 6, 7, 8}}),
		Tokens: prometheus.NewHistogram(prometheus.HistogramOpts{Name: "finuance_assistant_tokens_sent_approx",
			Help: "Approximate tokens sent to the model per run (JSON length / 4).", Buckets: prometheus.ExponentialBuckets(500, 2, 9)}),
		Upstream: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "finuance_assistant_model_requests_total",
			Help: "Requests to model servers by connection and result (ok or an error kind)."}, []string{"connection", "result"}),
		LLMDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "finuance_llm_request_duration_seconds",
			Help: "Duration of one request to a model server, to the end of its answer.", Buckets: []float64{.25, .5, 1, 2.5, 5, 10, 20, 30, 60, 120, 300}}, []string{"connection"}),
		LLMFirstToken: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "finuance_llm_time_to_first_token_seconds",
			Help: "Time from sending a request to a model server to the first piece of its answer.", Buckets: []float64{.1, .25, .5, 1, 2.5, 5, 10, 20, 30, 60, 120}}, []string{"connection"}),
		LLMTokens: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "finuance_llm_tokens_total",
			Help: "Tokens as the model servers reported them, by connection and direction (input, output)."}, []string{"connection", "direction"}),
	}
	if reg != nil {
		reg.MustRegister(m.Runs, m.Duration, m.ToolCalls, m.Rounds, m.Tokens, m.Upstream, m.LLMDuration, m.LLMFirstToken, m.LLMTokens)
	}
	return m
}

// knownTool limits the tool label to the defined tools (a model can invent any name).
func knownTool(name string) string {
	for _, t := range toolDefs {
		if t.Name == name {
			return name
		}
	}
	return "unknown"
}
