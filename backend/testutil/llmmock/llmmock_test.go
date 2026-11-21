package llmmock

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// TestHistoryIsBounded: the mock keeps the newest History requests, so days in a lab don't grow
// its memory (PLAN 25.7).
func TestHistoryIsBounded(t *testing.T) {
	s := New()
	srv := httptest.NewServer(s)
	defer srv.Close()
	for i := range History + 44 {
		body := `{"model":"mock-tools","n":` + strconv.Itoa(i) + `}`
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("X-Test", strconv.Itoa(i))
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
	}
	reqs, hdrs := s.Requests(), s.Headers()
	if len(reqs) != History || len(hdrs) != History {
		t.Fatalf("%d requests and %d headers kept, want %d", len(reqs), len(hdrs), History)
	}
	if !strings.Contains(string(reqs[History-1]), `"n":299`) || hdrs[0].Get("X-Test") != "44" {
		t.Errorf("not the newest: last %s, first header %s", reqs[History-1], hdrs[0].Get("X-Test"))
	}
}
