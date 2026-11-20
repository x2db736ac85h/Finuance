package observability

import (
	"context"
	"fmt"
	"os"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// Tracing bundles the tracer provider and propagator. Both are passed explicitly to the
// instrumented libraries (otelhttp, otelpgx); nothing is installed globally.
type Tracing struct {
	Provider   trace.TracerProvider
	Propagator propagation.TextMapPropagator
	shutdown   func(context.Context) error
}

// Shutdown flushes pending spans.
func (t *Tracing) Shutdown(ctx context.Context) error { return t.shutdown(ctx) }

// TracingConfig is what NewTracingWith needs.
type TracingConfig struct {
	Endpoint    string  // the OTLP/HTTP traces URL; empty: no tracing
	Service     string  // service.name: finuance-api or finuance-worker
	Version     string  // service.version
	Environment string  // deployment.environment.name (dev, test, prod)
	SampleRatio float64 // the share of new traces that are recorded, 0 to 1
}

// NewTracing is NewTracingWith for a service that records every trace.
func NewTracing(ctx context.Context, endpoint, service, version string) (*Tracing, error) {
	return NewTracingWith(ctx, TracingConfig{Endpoint: endpoint, Service: service, Version: version, SampleRatio: 1})
}

// NewTracingWith exports spans over OTLP/HTTP to the endpoint (e.g. http://alloy:4318/v1/traces),
// or returns a no-op provider when it is empty.
func NewTracingWith(ctx context.Context, c TracingConfig) (*Tracing, error) {
	// Only the W3C trace context travels (to the jobs a request enqueues). Baggage is not
	// propagated: nothing reads it, and it would carry whatever a client sent (PLAN 23.3).
	var prop propagation.TextMapPropagator = propagation.TraceContext{}
	if c.Endpoint == "" {
		return &Tracing{Provider: noop.NewTracerProvider(), Propagator: prop, shutdown: func(context.Context) error { return nil }}, nil
	}
	exp, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(c.Endpoint))
	if err != nil {
		return nil, fmt.Errorf("otlp exporter: %w", err)
	}
	res, err := traceResource(ctx, c)
	if err != nil {
		return nil, fmt.Errorf("trace resource: %w", err)
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(Redact(exp)), sdktrace.WithResource(res), sdktrace.WithSampler(sampler(c.SampleRatio)))
	return &Tracing{Provider: tp, Propagator: prop, shutdown: tp.Shutdown}, nil
}

// sampler decides per trace, at its root: a request starts a new root (the API is a public
// endpoint, PLAN 23.3), so the ratio applies there; everything below a recorded root is recorded,
// and a job follows the request that enqueued it. A whole trace is kept or dropped, never a part.
func sampler(ratio float64) sdktrace.Sampler {
	return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio))
}

// traceResource describes the process that sends the spans (PLAN 24.4): the service, the
// environment, and which instance it is. On Kubernetes the chart puts the pod and namespace into
// OTEL_RESOURCE_ATTRIBUTES (from the downward API) with service.instance.id; elsewhere the instance
// is the host name. Attributes from the environment win over the defaults here, except the service's
// own name and version.
func traceResource(ctx context.Context, c TracingConfig) (*resource.Resource, error) {
	host, _ := os.Hostname()
	attrs := []attribute.KeyValue{semconv.ServiceName(c.Service), semconv.ServiceVersion(c.Version)}
	defaults := []attribute.KeyValue{semconv.ServiceInstanceID(host)}
	if c.Environment != "" {
		defaults = append(defaults, attribute.String("deployment.environment.name", c.Environment))
	}
	return resource.New(ctx, resource.WithSchemaURL(semconv.SchemaURL),
		resource.WithAttributes(defaults...), resource.WithFromEnv(), resource.WithAttributes(attrs...))
}
