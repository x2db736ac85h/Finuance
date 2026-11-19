package observability

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

// What the instrumentation libraries put on spans that must not leave the process (PLAN 23.4):
// who called (the access log already has the client's address, and a browser's user agent
// identifies a person), and free text: an error's message can quote the values of a row or of a
// request. Spans keep ids, names, codes and durations.
var droppedSpanAttributes = map[attribute.Key]bool{
	"client.address":       true,
	"client.port":          true,
	"network.peer.address": true,
	"network.peer.port":    true,
	"user_agent.original":  true,
	"url.query":            true,
	"url.full":             true,
	"http.target":          true, // the old name of path?query
	"http.url":             true,
	"http.user_agent":      true,
	"http.client_ip":       true,
	"net.sock.peer.addr":   true,
	"db.query.text":        true, // off in otelpgx already; statements carry no literals, but stay out
	"db.statement":         true,
	"pgx.query.parameters": true,
	"exception.message":    true,
	"exception.stacktrace": true,
}

// Redact wraps a span exporter so that every span leaves without the attributes above, without the
// text of recorded errors and without a status description. A status keeps its code; a database
// span keeps its SQLSTATE (pgx.sql_state), an exception event its type.
func Redact(next sdktrace.SpanExporter) sdktrace.SpanExporter { return redactingExporter{next} }

type redactingExporter struct{ next sdktrace.SpanExporter }

func (e redactingExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	out := make([]sdktrace.ReadOnlySpan, len(spans))
	for i, s := range spans {
		out[i] = redactedSpan{s}
	}
	return e.next.ExportSpans(ctx, out)
}

func (e redactingExporter) Shutdown(ctx context.Context) error { return e.next.Shutdown(ctx) }

type redactedSpan struct{ sdktrace.ReadOnlySpan }

func keep(attrs []attribute.KeyValue) []attribute.KeyValue {
	out := make([]attribute.KeyValue, 0, len(attrs))
	for _, kv := range attrs {
		if !droppedSpanAttributes[kv.Key] {
			out = append(out, kv)
		}
	}
	return out
}

func (s redactedSpan) Attributes() []attribute.KeyValue { return keep(s.ReadOnlySpan.Attributes()) }

func (s redactedSpan) Events() []sdktrace.Event {
	events := s.ReadOnlySpan.Events()
	out := make([]sdktrace.Event, len(events))
	for i, ev := range events {
		ev.Attributes = keep(ev.Attributes)
		out[i] = ev
	}
	return out
}

func (s redactedSpan) Links() []sdktrace.Link {
	links := s.ReadOnlySpan.Links()
	out := make([]sdktrace.Link, len(links))
	for i, l := range links {
		l.Attributes = keep(l.Attributes)
		out[i] = l
	}
	return out
}

func (s redactedSpan) Status() sdktrace.Status {
	st := s.ReadOnlySpan.Status()
	if st.Code == codes.Error {
		st.Description = ""
	}
	return st
}

// safeErrors is the logger's ReplaceAttr: an error logged as a value is written as
// platform.SafeError's text, so a PostgreSQL error never brings its message or detail into a log
// line, whichever line logs it.
func safeErrors(_ []string, a slog.Attr) slog.Attr {
	if a.Value.Kind() == slog.KindAny {
		if err, ok := a.Value.Any().(error); ok {
			a.Value = slog.StringValue(platform.SafeError(err))
		}
	}
	return a
}
