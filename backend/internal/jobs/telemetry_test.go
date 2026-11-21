package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strings"
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

	"github.com/x2db736ac85h/finuance/backend/internal/config"
	"github.com/x2db736ac85h/finuance/backend/internal/observability"
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
	if testutil.ToFloat64(m.Jobs.WithLabelValues("commit_import", "ok", "real")) != 1 || testutil.ToFloat64(m.Jobs.WithLabelValues("commit_import", "error", "real")) != 1 {
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
	pool, err := store.Open(ctx, tdb.AppDSN, 4, tp, store.Timeouts{})
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

// TestJobLinesNameTheJob: every line a job writes carries job_id, kind and attempt, and the job
// span's trace_id and span_id, without the worker's code adding them (PLAN 24.2).
func TestJobLinesNameTheJob(t *testing.T) {
	rec, tp := recorder()
	tel := NewTelemetry(tp, propagation.TraceContext{}, nil)
	var b bytes.Buffer
	ctx := observability.WithLogger(context.Background(), observability.NewLogger(&b, config.EnvProd, slog.LevelInfo))

	job := &rivertype.JobRow{ID: 42, Kind: "commit_import", Attempt: 3, Queue: "default"}
	if err := tel.Work(ctx, job, func(ctx context.Context) error {
		observability.Logger(ctx).InfoContext(ctx, "import committed", "new", 5)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var line map[string]any
	if err := json.Unmarshal(b.Bytes(), &line); err != nil {
		t.Fatalf("%q: %v", b.String(), err)
	}
	span := spanNamed(t, rec, "job commit_import")
	if line["job_id"] != float64(42) || line["kind"] != "commit_import" || line["attempt"] != float64(3) ||
		line["trace_id"] != span.SpanContext().TraceID().String() || line["span_id"] != span.SpanContext().SpanID().String() {
		t.Errorf("job line = %v", line)
	}
	for k := range line {
		if k != "new" && !slices.Contains(observability.LogFields, k) {
			t.Errorf("field %q is not in the contract", k)
		}
	}
}

// TestSyntheticJobs: a job queued while serving a synthetic user carries the mark in its metadata
// (written from the server's context, next to the trace context), and when it is worked the mark
// labels its metric, its log lines and its span, and reaches whatever the job queues in turn. A
// job queued for a person carries nothing and counts as real (ADR 0041, PLAN 24.7).
func TestSyntheticJobs(t *testing.T) {
	rec, tp := recorder()
	m := NewJobMetrics(nil)
	tel := NewTelemetry(tp, propagation.TraceContext{}, m)
	insert := func(ctx context.Context, metadata string) []byte {
		p := &rivertype.JobInsertParams{Kind: "commit_import", Metadata: []byte(metadata)}
		if _, err := tel.InsertMany(ctx, []*rivertype.JobInsertParams{p}, func(context.Context) ([]*rivertype.JobInsertResult, error) { return nil, nil }); err != nil {
			t.Fatal(err)
		}
		return p.Metadata
	}
	person := insert(context.Background(), `{"keep":1}`)
	robot := insert(observability.WithTraffic(context.Background(), true), `{"keep":1}`)
	if strings.Contains(string(person), "synthetic") || !strings.Contains(string(robot), `"finuance_synthetic":true`) || !strings.Contains(string(robot), `"keep":1`) {
		t.Fatalf("metadata: a person's %s, a synthetic user's %s", person, robot)
	}

	var b bytes.Buffer
	base := observability.WithLogger(context.Background(), observability.NewLogger(&b, config.EnvProd, slog.LevelInfo))
	var inner bool
	work := func(id int64, metadata []byte) {
		job := &rivertype.JobRow{ID: id, Kind: "commit_import", Attempt: 1, Metadata: metadata}
		if err := tel.Work(base, job, func(ctx context.Context) error {
			inner = observability.Synthetic(ctx)
			observability.Logger(ctx).InfoContext(ctx, "import committed")
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	work(1, person)
	if inner || !strings.Contains(b.String(), `"traffic":"real"`) {
		t.Errorf("a person's job: synthetic in the context = %v, line %s", inner, b.String())
	}
	b.Reset()
	work(2, robot)
	if !inner || !strings.Contains(b.String(), `"traffic":"synthetic"`) {
		t.Errorf("a synthetic user's job: synthetic in the context = %v, line %s", inner, b.String())
	}
	if r, s := testutil.ToFloat64(m.Jobs.WithLabelValues("commit_import", "ok", "real")), testutil.ToFloat64(m.Jobs.WithLabelValues("commit_import", "ok", "synthetic")); r != 1 || s != 1 {
		t.Errorf("finuance_jobs_total: real = %v, synthetic = %v, want 1 and 1", r, s)
	}
	var marked int
	for _, s := range rec.Ended() {
		for _, kv := range s.Attributes() {
			if kv.Key == "finuance.synthetic" && kv.Value.AsBool() {
				marked++
			}
		}
	}
	if marked != 1 {
		t.Errorf("%d job spans carry finuance.synthetic, want the synthetic user's only", marked)
	}
	// a mark that is not exactly the server's own is no mark
	for _, forged := range []string{`{"finuance_synthetic":"true"}`, `{"synthetic":true}`, `not json`} {
		if isSynthetic([]byte(forged)) {
			t.Errorf("metadata %s counts as synthetic", forged)
		}
	}
}
