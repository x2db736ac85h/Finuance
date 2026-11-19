package store_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/x2db736ac85h/finuance/backend/internal/observability"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
	"github.com/x2db736ac85h/finuance/backend/testutil"
)

// TestFailingQueryLeavesNoValuesInSpans: PostgreSQL quotes the offending value in the message of
// a failing statement, and otelpgx records that message on the span. Exported the way the real
// setup exports (observability.Redact), the span keeps the SQLSTATE and nothing of the value; the
// error's log text (platform.SafeError) does the same. The unredacted exporter is the control: it
// shows that the value would be there (PLAN 23.4, DB-5).
func TestFailingQueryLeavesNoValuesInSpans(t *testing.T) {
	tdb := testutil.Require(t, testDB)
	ctx := context.Background()
	const marker = "MARKERVALUE"
	run := func(wrap func(sdktrace.SpanExporter) sdktrace.SpanExporter) (string, error) {
		mem := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(wrap(mem)))
		pool, err := store.Open(ctx, tdb.AppDSN, 2, tp, store.Timeouts{})
		if err != nil {
			t.Fatal(err)
		}
		defer pool.Close()
		// otelpgx traces statements inside a recording span only, as a request or a job provides
		ctx, parent := tp.Tracer("test").Start(ctx, "request")
		defer parent.End()
		_, qerr := pool.Exec(ctx, "SELECT $1::text::int", marker)
		if qerr == nil {
			t.Fatal("the statement did not fail")
		}
		_, uerr := pool.Exec(ctx, "CREATE TEMP TABLE redact_probe (v text PRIMARY KEY); INSERT INTO redact_probe VALUES ('"+marker+"'), ('"+marker+"')")
		if uerr == nil {
			t.Fatal("the duplicate key was accepted")
		}
		return fmt.Sprintf("%+v", mem.GetSpans()), fmt.Errorf("probe: %w / %w", qerr, uerr)
	}

	raw, err := run(func(e sdktrace.SpanExporter) sdktrace.SpanExporter { return e })
	if !strings.Contains(raw, marker) {
		t.Fatal("the control found no value in the unredacted spans: the test no longer proves anything")
	}
	if !strings.Contains(err.Error(), marker) {
		t.Fatal("the control found no value in the raw error text")
	}
	redacted, err := run(observability.Redact)
	if strings.Contains(redacted, marker) {
		t.Errorf("a redacted span carries the value:\n%s", redacted)
	}
	if !strings.Contains(redacted, "22P02") || !strings.Contains(redacted, "23505") {
		t.Errorf("the redacted spans lost the SQLSTATEs:\n%s", redacted)
	}
	safe := platform.SafeError(err)
	if strings.Contains(safe, marker) || !strings.Contains(safe, "SQLSTATE 22P02") {
		t.Errorf("SafeError = %q", safe)
	}
}
