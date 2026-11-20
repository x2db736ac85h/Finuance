package assistant

import (
	"testing"

	promtest "github.com/prometheus/client_golang/prometheus/testutil"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/x2db736ac85h/finuance/backend/internal/query"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
	"github.com/x2db736ac85h/finuance/backend/testutil"
	"github.com/x2db736ac85h/finuance/backend/testutil/llmmock"
)

// TestModelRequestSpan: each request to the model server is one client span "llm.request" with the
// connection, the model, the round and the time to the first chunk; a failing server gives it an
// error status and the error's kind. Nothing of our trace reaches the model server (PLAN 24.4).
func TestModelRequestSpan(t *testing.T) {
	re := newRunEnv(t, 2)
	rec := tracetest.NewSpanRecorder()
	metrics := NewMetrics(nil)
	re.runner = NewRunner(RunnerConfig{Connections: re.conns, Tools: NewTools(re.q), Chats: re.chats, Clock: re.clock, MaxConcurrent: 2,
		Tracer: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)), Metrics: metrics})

	re.mock.Push(llmmock.Reply{Content: "Nothing to report."})
	if evs := re.ask(re.newChat(), "anything new?", nil); last(evs).Name != "done" {
		t.Fatalf("events %s", names(evs))
	}
	re.mock.Push(llmmock.Reply{Status: 500, Body: `{"error":{"message":"boom"}}`}, llmmock.Reply{Status: 500, Body: `{"error":{"message":"boom"}}`})
	if evs := re.ask(re.newChat(), "and now?", nil); last(evs).Name != "error" {
		t.Fatalf("events %s", names(evs))
	}

	var ok, failed []sdktrace.ReadOnlySpan
	for _, s := range rec.Ended() {
		if s.Name() != "llm.request" {
			continue
		}
		if s.SpanKind() != trace.SpanKindClient {
			t.Errorf("span kind = %v, want client", s.SpanKind())
		}
		if s.Status().Code == codes.Error {
			failed = append(failed, s)
		} else {
			ok = append(ok, s)
		}
	}
	if len(ok) == 0 || len(failed) == 0 {
		t.Fatalf("%d llm.request spans succeeded and %d failed, want both", len(ok), len(failed))
	}
	attrs := func(s sdktrace.ReadOnlySpan) map[attribute.Key]attribute.Value {
		m := map[attribute.Key]attribute.Value{}
		for _, kv := range s.Attributes() {
			m[kv.Key] = kv.Value
		}
		return m
	}
	a := attrs(ok[0])
	if a["gen_ai.operation.name"].AsString() != "chat" || a["gen_ai.provider.name"].AsString() != "openai" ||
		a["gen_ai.request.model"].AsString() == "" || a["assistant.connection"].AsString() == "" || a["assistant.round"].AsInt64() != 1 {
		t.Errorf("attributes = %v", ok[0].Attributes())
	}
	if v, found := a["gen_ai.response.time_to_first_chunk"]; !found || v.AsFloat64() <= 0 {
		t.Errorf("no time to the first chunk: %v", ok[0].Attributes())
	}
	if kind := attrs(failed[0])["error.type"].AsString(); kind == "" {
		t.Errorf("the failed request has no error.type: %v", failed[0].Attributes())
	}

	// the same requests in the metrics (PLAN 24.5), under the connection's name only
	if d, f := promtest.CollectAndCount(metrics.LLMDuration), promtest.CollectAndCount(metrics.LLMFirstToken); d != 1 || f != 1 {
		t.Errorf("series: %d durations and %d first-token times, want one each (the connection)", d, f)
	}

	hs := re.mock.Headers()
	if len(hs) == 0 {
		t.Fatal("the mock saw no request")
	}
	for _, h := range hs {
		for _, name := range []string{"Traceparent", "Tracestate", "Baggage"} {
			if h.Get(name) != "" {
				t.Errorf("the model server received %s: %q", name, h.Get(name))
			}
		}
	}
}

// TestFailingToolSpan: a tool that fails for a reason the model can't fix (here: its database
// connections go away while the model is answering) gives its span an error status with a fixed
// text, never the error's own.
func TestFailingToolSpan(t *testing.T) {
	re := newRunEnv(t, 2)
	tdb := testutil.Require(t, testDB)
	toolsPool, err := pgxpool.NewWithConfig(re.ctx, tdb.App.Config().Copy())
	if err != nil {
		t.Fatal(err)
	}
	defer toolsPool.Close()
	rec := tracetest.NewSpanRecorder()
	re.runner = NewRunner(RunnerConfig{Connections: re.conns, Tools: NewTools(query.New(store.New(toolsPool))), Chats: re.chats, Clock: re.clock,
		MaxConcurrent: 2, Tracer: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))})

	re.mock.Push(llmmock.Reply{Reasoning: "I will look it up.", Calls: []llmmock.Call{{Name: ToolSearch, Arguments: `{"search":"coffee"}`}}})
	run, err := re.runner.Prepare(re.ctx, re.user, Request{ChatID: re.newChat(), Text: "what did I spend on coffee?", Connection: "mock", Model: "mock-tools"})
	if err != nil {
		t.Fatal(err)
	}
	var evs []Event
	run.Execute(re.ctx, func(e Event) {
		if e.Name == "reasoning" {
			toolsPool.Close() // the tool call that follows finds no database
		}
		evs = append(evs, e)
	})
	if last(evs).Name != "error" {
		t.Fatalf("events %s, want the run to end in an error", names(evs))
	}
	var seen []string
	for _, s := range rec.Ended() {
		seen = append(seen, s.Name())
		if s.Name() == "assistant.tool" {
			if s.Status().Code != codes.Error || s.Status().Description != "tool failed" {
				t.Errorf("the tool span's status = %+v, want error \"tool failed\"", s.Status())
			}
			return
		}
	}
	t.Errorf("no assistant.tool span among %v", seen)
}
