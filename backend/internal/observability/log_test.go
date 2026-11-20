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
