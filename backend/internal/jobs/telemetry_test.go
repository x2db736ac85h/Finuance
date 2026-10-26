package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/x2db736ac85h/finuance/backend/internal/store"
	dbtest "github.com/x2db736ac85h/finuance/backend/testutil"
)

var testDB *dbtest.DB

func TestMain(m *testing.M) { dbtest.Main(m, &testDB) }

func recorder() (*tracetest.SpanRecorder, *sdktrace.TracerProvider) {
	rec := tracetest.NewSpanRecorder()
	return rec, sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
}

func spanNamed(t *testing.T, rec *tracetest.SpanRecorder, name string) sdktrace.ReadOnlySpan {
	t.Helper()
	for _, s := range rec.Ended() {
		if s.Name() == name {
			return s
		}
	}
	t.Fatalf("no span %q among %d", name, len(rec.Ended()))
	return nil
}

func TestTelemetryCarriesTheTraceIntoTheJob(t *testing.T) {
	rec, tp := recorder()
	prop := propagation.TraceContext{}
	m := NewJobMetrics(nil)
	tel := NewTelemetry(tp, prop, m)

	// the inserting request
	ctx, req := tp.Tracer("test").Start(context.Background(), "POST /imports/{id}/commit")
	params := []*rivertype.JobInsertParams{
		{Kind: "commit_import", Metadata: []byte(`{"keep":1}`)},
		{Kind: "commit_import"},
	}
	if _, err := tel.InsertMany(ctx, params, func(context.Context) ([]*rivertype.JobInsertResult, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	req.End()
	var meta map[string]json.RawMessage
	if err := json.Unmarshal(params[0].Metadata, &meta); err != nil || string(meta["keep"]) != "1" || meta[traceKey] == nil {
		t.Fatalf("metadata = %s (other keys must survive)", params[0].Metadata)
	}

	// the worker, later, in another process
	job := &rivertype.JobRow{ID: 7, Kind: "commit_import", Attempt: 1, Queue: "default", Metadata: params[1].Metadata}
	var inner trace.SpanContext
	if err := tel.Work(context.Background(), job, func(ctx context.Context) error {
		inner = trace.SpanContextFromContext(ctx)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	span := spanNamed(t, rec, "job commit_import")
	if span.Parent().SpanID() != req.SpanContext().SpanID() || span.SpanContext().TraceID() != req.SpanContext().TraceID() {
		t.Errorf("job span parent %s / trace %s, want the request's %s / %s", span.Parent().SpanID(), span.SpanContext().TraceID(),
			req.SpanContext().SpanID(), req.SpanContext().TraceID())
	}
	if inner.SpanID() != span.SpanContext().SpanID() {
		t.Error("the worker's context doesn't carry the job span (SQL spans would not nest under it)")
	}
	if span.SpanKind() != trace.SpanKindConsumer {
		t.Errorf("kind = %v", span.SpanKind())
	}

	// a failing job: error status without the error's text, counted as an error
	boom := errors.New("import 42: secret payee name")
	if err := tel.Work(context.Background(), &rivertype.JobRow{ID: 8, Kind: "commit_import"}, func(context.Context) error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("error not passed through: %v", err)
	}
	failed := rec.Ended()[len(rec.Ended())-1]
	if failed.Status().Code != codes.Error || failed.Status().Description != "job failed" || len(failed.Events()) != 0 {
		t.Errorf("failed span status %+v, events %d (no error text in spans)", failed.Status(), len(failed.Events()))
	}
	if failed.Parent().IsValid() {
		t.Error("a job inserted without a trace got a parent")
	}
	if testutil.ToFloat64(m.Jobs.WithLabelValues("commit_import", "ok")) != 1 || testutil.ToFloat64(m.Jobs.WithLabelValues("commit_import", "error")) != 1 {
		t.Error("job counts not recorded")
	}
	if testutil.CollectAndCount(m.Duration) != 1 {
		t.Error("job duration not recorded")
	}
}

func TestTraceContextMetadata(t *testing.T) {
	c := propagation.MapCarrier{"traceparent": "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"}
	for _, in := range []string{"", "null", "[1]", "not json", `{"a":"b"}`} {
		out := withTraceContext([]byte(in), c)
		if got := traceContext(out); got["traceparent"] != c["traceparent"] {
			t.Errorf("%q → %s → %v", in, out, got)
		}
	}
	if len(traceContext(nil)) != 0 || len(traceContext([]byte(`{"otel":"x"}`))) != 0 {
		t.Error("missing or malformed trace context not ignored")
	}
}

// sqlWorker runs one statement through a traced pool, as the real workers do through the store.
type sqlWorkerArgs struct{}

func (sqlWorkerArgs) Kind() string { return "telemetry_test_sql" }

type sqlWorker struct {
	river.WorkerDefaults[sqlWorkerArgs]
	run func(ctx context.Context) error
}

func (w *sqlWorker) Work(ctx context.Context, _ *river.Job[sqlWorkerArgs]) error { return w.run(ctx) }

// TestJobTraceEndToEnd: request span → River insert → worker in another client → job span →
// SQL span, all in one trace, through a real database (PLAN 17.2's "API → job → SQL").
func TestJobTraceEndToEnd(t *testing.T) {
	tdb := dbtest.Require(t, testDB)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rec, tp := recorder()
	prop := propagation.TraceContext{}
	pool, err := store.Open(ctx, tdb.AppDSN, 4, tp)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	done := make(chan struct{})
	workers := river.NewWorkers()
	river.AddWorker(workers, &sqlWorker{run: func(ctx context.Context) error {
		defer close(done)
		_, err := pool.Exec(ctx, "SELECT 1")
		return err
	}})
	worker, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Queues:     map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 1}},
		Workers:    workers,
		Middleware: []rivertype.Middleware{NewTelemetry(tp, prop, nil)},
	})
	if err != nil {
		t.Fatal(err)
	}
	inserter, err := river.NewClient(riverpgxv5.New(tdb.App), &river.Config{Middleware: []rivertype.Middleware{NewTelemetry(tp, prop, nil)}})
	if err != nil {
		t.Fatal(err)
	}
	reqCtx, req := tp.Tracer("test").Start(ctx, "POST /test")
	if _, err := inserter.Insert(reqCtx, sqlWorkerArgs{}, nil); err != nil {
		t.Fatal(err)
	}
	req.End()
	if err := worker.Start(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("job not worked")
	}
	if err := worker.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	job := spanNamed(t, rec, "job telemetry_test_sql")
	if job.SpanContext().TraceID() != req.SpanContext().TraceID() || job.Parent().SpanID() != req.SpanContext().SpanID() {
		t.Fatalf("job span not a child of the request span")
	}
	var sql bool
	for _, s := range rec.Ended() {
		if s.Parent().SpanID() == job.SpanContext().SpanID() && s.SpanContext().TraceID() == req.SpanContext().TraceID() {
			sql = true
		}
	}
	if !sql {
		t.Error("no SQL span under the job span")
	}
}
