package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/x2db736ac85h/finuance/backend/internal/observability"
)

// TestRequestIDReachesTheContext: the id the response carries is the one in the context, from
// where a queued job takes it along; a well-formed id from the client is kept, anything else is
// replaced by one of the server's.
func TestRequestIDReachesTheContext(t *testing.T) {
	for _, tc := range []struct{ sent, want string }{
		{"", ""},
		{"abc-123_X", "abc-123_X"},
		{"has space", ""},
		{strings.Repeat("a", 65), ""},
		{`x"); DROP`, ""},
	} {
		var inCtx, inInfo string
		h := AssignRequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			inCtx, inInfo = observability.RequestID(r.Context()), RequestID(r.Context())
		}))
		req := httptest.NewRequest(http.MethodGet, "/api/v1/meta", nil)
		if tc.sent != "" {
			req.Header.Set(HeaderRequestID, tc.sent)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		got := rec.Header().Get(HeaderRequestID)
		if inCtx != got || inInfo != got || !observability.ValidRequestID(got) {
			t.Errorf("sent %q: response %q, context %q, info %q", tc.sent, got, inCtx, inInfo)
		}
		if tc.want != "" && got != tc.want {
			t.Errorf("sent %q: got %q, want the client's id kept", tc.sent, got)
		}
		if tc.want == "" && got == tc.sent {
			t.Errorf("sent %q: a malformed id was kept", tc.sent)
		}
	}
}
