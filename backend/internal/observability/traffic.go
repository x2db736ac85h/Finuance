package observability

import "context"

// Traffic says whether work is done for a person or for a synthetic user (ADR 0041, PLAN 24.7).
// The session layer sets it from the signed-in user's kind, a job inherits it from the request
// that queued it, and from there it labels log lines, metrics and spans. Nothing a client sends
// can set it: there is no header, cookie or field that is read for it.
const (
	TrafficReal      = "real"
	TrafficSynthetic = "synthetic"
)

type trafficKey struct{}

// WithTraffic marks ctx as work for a synthetic user (or not).
func WithTraffic(ctx context.Context, synthetic bool) context.Context {
	return context.WithValue(ctx, trafficKey{}, synthetic)
}

// Synthetic reports whether ctx is marked as work for a synthetic user.
func Synthetic(ctx context.Context) bool {
	s, _ := ctx.Value(trafficKey{}).(bool)
	return s
}

// Traffic is the label value for ctx: "synthetic" when marked, else "real" (requests without a
// user, such as signing in and the probes, are real).
func Traffic(ctx context.Context) string { return TrafficLabel(Synthetic(ctx)) }

// TrafficLabel is the label value for a known answer.
func TrafficLabel(synthetic bool) string {
	if synthetic {
		return TrafficSynthetic
	}
	return TrafficReal
}
