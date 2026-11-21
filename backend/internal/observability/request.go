package observability

import (
	"context"
	"regexp"
)

// The request id names one request on its log lines and in what the person is shown when it
// fails ("Reference: …", PLAN 24.8). The request middleware puts it into the context; a job queued
// while serving the request carries it in its metadata (package jobs), so the job's lines, the
// jobs it queues in turn and a failed job's message name the same request.

type requestIDKey struct{}

var validRequestID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// ValidRequestID reports whether s has the form of a request id: 1 to 64 of [A-Za-z0-9_-]. The
// middleware accepts such an id from a client and makes one otherwise.
func ValidRequestID(s string) bool { return validRequestID.MatchString(s) }

// WithRequestID stores the request id in ctx; an id of the wrong form is not stored.
func WithRequestID(ctx context.Context, id string) context.Context {
	if !ValidRequestID(id) {
		return ctx
	}
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestID returns the id stored by WithRequestID ("" when there is none).
func RequestID(ctx context.Context) string {
	s, _ := ctx.Value(requestIDKey{}).(string)
	return s
}

// WithReference adds the request id to a message for the person, in the form the web app gives a
// failed request: "<message> (Reference: <id>)". Without an id the message is unchanged.
func WithReference(ctx context.Context, message string) string {
	if id := RequestID(ctx); id != "" {
		return message + " (Reference: " + id + ")"
	}
	return message
}
