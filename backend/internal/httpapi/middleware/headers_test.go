package middleware

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestMaxBytes: the default limit, an exact route and a route with a "*" segment.
func TestMaxBytes(t *testing.T) {
	h := MaxBytes(10, map[string]int64{"POST /up": 20, "POST /chats/*/photos": 30})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
		}
	}))
	for _, c := range []struct {
		path string
		n    int
		want int
	}{
		{"/json", 10, 200}, {"/json", 11, 413}, {"/up", 20, 200}, {"/up", 21, 413},
		{"/chats/c1/photos", 30, 200}, {"/chats/c1/photos", 31, 413}, {"/chats/c1/x/photos", 11, 413}, {"/chats/c1/photos/x", 11, 413},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, c.path, bytes.NewReader(make([]byte, c.n))))
		if w.Code != c.want {
			t.Errorf("POST %s with %d bytes: %d, want %d", c.path, c.n, w.Code, c.want)
		}
	}
}
