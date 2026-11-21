package observability

import (
	"context"
	"testing"
)

func TestReference(t *testing.T) {
	const msg = "The import could not be committed."
	if got := WithReference(context.Background(), msg); got != msg {
		t.Errorf("without an id: %q", got)
	}
	ctx := WithRequestID(context.Background(), "cf0ff0a8")
	if got := WithReference(ctx, msg); got != msg+" (Reference: cf0ff0a8)" {
		t.Errorf("with an id: %q", got)
	}
	// an id of the wrong form is never stored, so it can't reach a message
	for _, bad := range []string{"", "a b", "x\n", "<script>", string(make([]byte, 65))} {
		if id := RequestID(WithRequestID(context.Background(), bad)); id != "" {
			t.Errorf("%q stored as %q", bad, id)
		}
	}
}
