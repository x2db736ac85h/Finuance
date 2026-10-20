package observability

import (
	"context"
	"fmt"

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

// NewTracing exports spans over OTLP/HTTP to endpoint (e.g. http://alloy:4318), or returns a
// no-op provider when endpoint is empty.
func NewTracing(ctx context.Context, endpoint, service, version string) (*Tracing, error) {
	prop := propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})
	if endpoint == "" {
		return &Tracing{Provider: noop.NewTracerProvider(), Propagator: prop, shutdown: func(context.Context) error { return nil }}, nil
	}
	exp, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint))
	if err != nil {
		return nil, fmt.Errorf("otlp exporter: %w", err)
	}
	res := resource.NewWithAttributes(semconv.SchemaURL, semconv.ServiceName(service), semconv.ServiceVersion(version))
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exp), sdktrace.WithResource(res))
	return &Tracing{Provider: tp, Propagator: prop, shutdown: tp.Shutdown}, nil
}
