package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/x2db736ac85h/finuance/backend/internal/config"
)

// TestTraceHandler: a line logged with a context inside a span carries that span's trace_id and
// span_id, also through With and WithGroup; a line outside a span carries neither (PLAN 24.2).
func TestTraceHandler(t *testing.T) {
	var b bytes.Buffer
	log := NewLogger(&b, config.EnvProd, slog.LevelInfo)
	ctx, span := sdktrace.NewTracerProvider().Tracer("test").Start(context.Background(), "op")
	defer span.End()
	sc := span.SpanContext()

	line := func() map[string]any {
		t.Helper()
		var rec map[string]any
		if err := json.Unmarshal(b.Bytes(), &rec); err != nil {
			t.Fatalf("%q: %v", b.String(), err)
		}
		b.Reset()
		return rec
	}

	log.InfoContext(ctx, "inside")
	if rec := line(); rec["trace_id"] != sc.TraceID().String() || rec["span_id"] != sc.SpanID().String() {
		t.Errorf("inside a span: %v", rec)
	}
	log.With("job_id", 7).InfoContext(ctx, "with attributes")
	if rec := line(); rec["trace_id"] != sc.TraceID().String() || rec["job_id"] != float64(7) {
		t.Errorf("through With: %v", rec)
	}
	log.WithGroup("g").InfoContext(ctx, "in a group", "k", 1)
	if b.Len() == 0 || !strings.Contains(b.String(), sc.TraceID().String()) {
		t.Errorf("through WithGroup: %s", b.String())
	}
	b.Reset()
	log.InfoContext(context.Background(), "outside")
	if rec := line(); rec["trace_id"] != nil || rec["span_id"] != nil {
		t.Errorf("outside a span: %v", rec)
	}
	log.Info("no context at all")
	if rec := line(); rec["trace_id"] != nil {
		t.Errorf("without a context: %v", rec)
	}
}

// TestLogFieldContract: docs/observability.md lists exactly the fields of LogFields in its
// "Log fields" table, so the document and the code can't drift apart.
func TestLogFieldContract(t *testing.T) {
	doc, err := os.ReadFile("../../../docs/observability.md")
	if err != nil {
		t.Fatal(err)
	}
	_, table, ok := strings.Cut(string(doc), "## Log fields")
	if !ok {
		t.Fatal(`docs/observability.md has no "## Log fields" section`)
	}
	if next := strings.Index(table, "\n## "); next >= 0 {
		table = table[:next]
	}
	var documented []string
	for _, m := range regexp.MustCompile("(?m)^\\| `([a-z_]+)` \\|").FindAllStringSubmatch(table, -1) {
		documented = append(documented, m[1])
	}
	want := slices.Clone(LogFields)
	slices.Sort(want)
	slices.Sort(documented)
	if !slices.Equal(want, documented) {
		t.Errorf("the document's table lists %v\nLogFields is             %v", documented, want)
	}
}

// TestSampler: the ratio decides at a trace's root only. With 0 nothing new is recorded, with 1
// everything; below a recorded parent a span is always recorded (a job follows its request), below
// an unrecorded one never.
func TestSampler(t *testing.T) {
	sampled := func(ratio float64, parent trace.SpanContext) bool {
		ctx := context.Background()
		if parent.IsValid() {
			ctx = trace.ContextWithRemoteSpanContext(ctx, parent)
		}
		tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sampler(ratio)))
		_, span := tp.Tracer("test").Start(ctx, "op")
		defer span.End()
		return span.SpanContext().IsSampled()
	}
	parent := func(flags trace.TraceFlags) trace.SpanContext {
		return trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}, TraceFlags: flags, Remote: true})
	}
	none := trace.SpanContext{}
	if sampled(0, none) || !sampled(1, none) {
		t.Error("a root span: ratio 0 must record nothing and ratio 1 everything")
	}
	if !sampled(0, parent(trace.FlagsSampled)) {
		t.Error("below a recorded parent a span is not recorded")
	}
	if sampled(1, parent(0)) {
		t.Error("below an unrecorded parent a span is recorded")
	}
}

// TestTraceResource: the spans name the service, the environment and the instance; what the chart
// puts into OTEL_RESOURCE_ATTRIBUTES (the pod, the namespace, the instance id) is taken over, but
// can't rename the service.
func TestTraceResource(t *testing.T) {
	attrs := func() map[string]string {
		res, err := traceResource(context.Background(), TracingConfig{Service: "finuance-api", Version: "1.2.3", Environment: "prod"})
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]string{}
		for _, kv := range res.Attributes() {
			m[string(kv.Key)] = kv.Value.AsString()
		}
		return m
	}
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "")
	a := attrs()
	if a["service.name"] != "finuance-api" || a["service.version"] != "1.2.3" || a["deployment.environment.name"] != "prod" || a["service.instance.id"] == "" {
		t.Errorf("defaults: %v", a)
	}
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "k8s.pod.name=finuance-api-abc,k8s.namespace.name=finuance,service.instance.id=finuance-api-abc,service.name=other")
	a = attrs()
	if a["k8s.pod.name"] != "finuance-api-abc" || a["k8s.namespace.name"] != "finuance" || a["service.instance.id"] != "finuance-api-abc" || a["service.name"] != "finuance-api" {
		t.Errorf("with the chart's attributes: %v", a)
	}
}
