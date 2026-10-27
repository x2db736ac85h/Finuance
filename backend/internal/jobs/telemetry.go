package jobs

import (
	"context"
	"encoding/json"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// Telemetry is River middleware for traces and job metrics (PLAN task 17.2, §8.2). When a job is
// inserted it stores the caller's trace context (W3C traceparent, tracestate, baggage) in the
// job's metadata; when the job is worked it starts the span "job <kind>" as a child of that
// context. A request, the job it enqueued and the job's SQL (otelpgx) then share one trace. It
// also counts jobs by kind and result and times them. Spans carry ids and kinds only, never
// arguments or error text (errors can name user data).
type Telemetry struct {
	river.MiddlewareDefaults
	Tracer     trace.Tracer                  // nil: no spans
	Propagator propagation.TextMapPropagator // nil: no propagation
	Metrics    *JobMetrics                   // nil: unregistered metrics
}

var (
	_ rivertype.JobInsertMiddleware = (*Telemetry)(nil)
	_ rivertype.WorkerMiddleware    = (*Telemetry)(nil)
)

// JobMetrics are finuance_jobs_total{kind,result} and finuance_job_duration_seconds{kind}.
type JobMetrics struct {
	Jobs     *prometheus.CounterVec
	Duration *prometheus.HistogramVec
}

// NewJobMetrics creates the metrics and registers them on reg (nil: unregistered, for tests).
func NewJobMetrics(reg prometheus.Registerer) *JobMetrics {
	m := &JobMetrics{
		Jobs: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "finuance_jobs_total",
			Help: "Worked River jobs by kind and result (ok, error)."}, []string{"kind", "result"}),
		Duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "finuance_job_duration_seconds",
			Help: "Duration of worked River jobs by kind.", Buckets: []float64{.01, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60, 120}},
			[]string{"kind"}),
	}
	if reg != nil {
		reg.MustRegister(m.Jobs, m.Duration)
	}
	return m
}

// NewTelemetry builds the middleware from a tracer provider and propagator (both may be nil).
func NewTelemetry(tp trace.TracerProvider, prop propagation.TextMapPropagator, m *JobMetrics) *Telemetry {
	if tp == nil {
		tp = noop.NewTracerProvider()
	}
	if m == nil {
		m = NewJobMetrics(nil)
	}
	return &Telemetry{Tracer: tp.Tracer("github.com/x2db736ac85h/finuance/backend/internal/jobs"), Propagator: prop, Metrics: m}
}

// traceKey is the metadata key holding the trace context (a JSON object of carrier keys).
const traceKey = "otel"

// InsertMany adds the caller's trace context to every inserted job's metadata.
func (t *Telemetry) InsertMany(ctx context.Context, params []*rivertype.JobInsertParams,
	doInner func(context.Context) ([]*rivertype.JobInsertResult, error)) ([]*rivertype.JobInsertResult, error) {
	if t.Propagator != nil {
		carrier := propagation.MapCarrier{}
		t.Propagator.Inject(ctx, carrier)
		if len(carrier) > 0 {
			for _, p := range params {
				p.Metadata = withTraceContext(p.Metadata, carrier)
			}
		}
	}
	return doInner(ctx)
}

// Work runs the job inside a span continued from the inserting request and records its metrics.
func (t *Telemetry) Work(ctx context.Context, job *rivertype.JobRow, doInner func(context.Context) error) error {
	if t.Propagator != nil {
		ctx = t.Propagator.Extract(ctx, traceContext(job.Metadata))
	}
	tracer := t.Tracer
	if tracer == nil {
		tracer = noop.NewTracerProvider().Tracer("")
	}
	ctx, span := tracer.Start(ctx, "job "+job.Kind, trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(attribute.String("river.kind", job.Kind), attribute.Int64("river.job_id", job.ID),
			attribute.Int("river.attempt", job.Attempt), attribute.String("river.queue", job.Queue)))
	defer span.End()
	start := time.Now()
	err := doInner(ctx)
	result := "ok"
	if err != nil {
		result = "error"
		span.SetStatus(codes.Error, "job failed")
	}
	if t.Metrics != nil {
		t.Metrics.Jobs.WithLabelValues(job.Kind, result).Inc()
		t.Metrics.Duration.WithLabelValues(job.Kind).Observe(time.Since(start).Seconds())
	}
	return err
}

// withTraceContext returns metadata (a JSON object, possibly empty) with the carrier under
// traceKey, keeping every other key. Metadata that isn't an object is replaced.
func withTraceContext(metadata []byte, carrier propagation.MapCarrier) []byte {
	obj := map[string]json.RawMessage{}
	if len(metadata) > 0 {
		if err := json.Unmarshal(metadata, &obj); err != nil || obj == nil {
			obj = map[string]json.RawMessage{}
		}
	}
	tc, err := json.Marshal(map[string]string(carrier))
	if err != nil {
		return metadata
	}
	obj[traceKey] = tc
	out, err := json.Marshal(obj)
	if err != nil {
		return metadata
	}
	return out
}

// traceContext reads the carrier stored by withTraceContext (empty when there is none).
func traceContext(metadata []byte) propagation.MapCarrier {
	var obj struct {
		Otel map[string]string `json:"otel"`
	}
	if len(metadata) == 0 || json.Unmarshal(metadata, &obj) != nil || obj.Otel == nil {
		return propagation.MapCarrier{}
	}
	return propagation.MapCarrier(obj.Otel)
}
