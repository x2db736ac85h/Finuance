package observability

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/x2db736ac85h/finuance/backend/internal/config"
)

// TestRedactedSpans: what an instrumentation library records about the caller and the text of an
// error does not leave through the exporter; ids, codes and names do (PLAN 23.4).
func TestRedactedSpans(t *testing.T) {
	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(Redact(mem)))
	linked := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}})
	_, span := tp.Tracer("t").Start(context.Background(), "GET /api/v1/duplicates",
		trace.WithLinks(trace.Link{SpanContext: linked, Attributes: []attribute.KeyValue{attribute.String("url.query", "q=MARKER")}}))
	span.SetAttributes(
		attribute.String("url.path", "/api/v1/duplicates"), attribute.String("url.query", "q=MARKER"),
		attribute.String("user_agent.original", "MARKER browser"), attribute.String("client.address", "203.0.113.9"),
		attribute.String("network.peer.address", "203.0.113.9"), attribute.String("db.query.text", "select 'MARKER'"),
		attribute.String("pgx.sql_state", "22P02"), attribute.Int("http.response.status_code", 500))
	err := fmt.Errorf(`invalid input syntax for type integer: "MARKER"`)
	span.RecordError(err, trace.WithStackTrace(true))
	span.SetStatus(codes.Error, err.Error())
	span.End()

	spans := mem.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("%d spans", len(spans))
	}
	s := spans[0]
	got := map[string]string{}
	for _, kv := range s.Attributes {
		got[string(kv.Key)] = kv.Value.String()
	}
	want := map[string]string{"url.path": "/api/v1/duplicates", "pgx.sql_state": "22P02", "http.response.status_code": "500"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("attributes = %v, want %v", got, want)
	}
	if s.Status.Code != codes.Error || s.Status.Description != "" {
		t.Errorf("status = %+v, want Error without a description", s.Status)
	}
	if len(s.Events) != 1 || s.Events[0].Name != "exception" {
		t.Fatalf("events = %+v", s.Events)
	}
	for _, kv := range s.Events[0].Attributes {
		if kv.Key != "exception.type" {
			t.Errorf("exception event carries %s", kv.Key)
		}
	}
	if len(s.Links) != 1 || len(s.Links[0].Attributes) != 0 {
		t.Errorf("links = %+v", s.Links)
	}
	if all := fmt.Sprintf("%+v", s); strings.Contains(all, "MARKER") || strings.Contains(all, "203.0.113.9") {
		t.Errorf("the exported span still carries a marker: %s", all)
	}
}

// TestLoggerWritesSafeErrors: a PostgreSQL error logged as a value, by any line, loses its message
// and detail in both log formats; other errors and other values are untouched.
func TestLoggerWritesSafeErrors(t *testing.T) {
	pg := &pgconn.PgError{Severity: "ERROR", Code: "23505", Message: "duplicate key MARKER", Detail: "Key (email)=(MARKER) already exists.",
		ConstraintName: "users_email_key", TableName: "users"}
	for _, env := range []config.Env{config.EnvProd, config.EnvDev} {
		var buf bytes.Buffer
		log := NewLogger(&buf, env, slog.LevelInfo)
		log.Error("request failed", "error", fmt.Errorf("create user: %w", pg), "route", "POST /x")
		log.With("error", error(pg)).Warn("with")
		log.WithGroup("g").Info("grouped", slog.Any("err", pg))
		log.Info("plain", "error", errors.New("disk full"), "text", "MARKER-free value")
		out := buf.String()
		if strings.Contains(out, "duplicate key") || strings.Contains(out, "MARKER)") || strings.Contains(out, "already exists") {
			t.Errorf("%s: the PostgreSQL message or detail was logged:\n%s", env, out)
		}
		for _, want := range []string{"create user: postgres error SQLSTATE 23505 constraint users_email_key table users", "disk full", "MARKER-free value"} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: %q is missing from:\n%s", env, want, out)
			}
		}
		if n := strings.Count(out, "SQLSTATE 23505"); n != 3 {
			t.Errorf("%s: %d safe errors, want 3:\n%s", env, n, out)
		}
	}
}
