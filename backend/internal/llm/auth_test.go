package llm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// rotating is a token source whose token changes with every Invalidate.
type rotating struct {
	mu          sync.Mutex
	n           int
	asked       int
	invalidated int
	err         error
}

func (r *rotating) Token(context.Context) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.asked++
	if r.err != nil {
		return "", r.err
	}
	return "tok-" + string(rune('a'+r.n)), nil
}

func (r *rotating) Invalidate() {
	r.mu.Lock()
	r.n++
	r.invalidated++
	r.mu.Unlock()
}

const okStream = "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n"

// TestTokenSource (PLAN 26.1): a connection with a token source sends its token as the bearer; a
// 401 drops the token and the request goes once more with a new one; a second 401 is the answer.
func TestTokenSource(t *testing.T) {
	var seen []string
	var mu sync.Mutex
	accept := "tok-a"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get("Authorization"))
		ok := r.Header.Get("Authorization") == "Bearer "+accept
		mu.Unlock()
		if !ok {
			http.Error(w, `{"error":{"message":"the token has expired"}}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(okStream))
	}))
	t.Cleanup(srv.Close)
	src := &rotating{}
	c := New(Config{API: OpenAI, BaseURL: srv.URL, Auth: src, APIKey: "a key that is not used", Timeout: 5 * time.Second})
	req := Request{Model: "m", Messages: []Message{{Role: RoleUser, Content: "x"}}}

	if r, _, err := run(t, c, req); err != nil || r.Content != "hi" {
		t.Fatalf("with a good token: %+v, %v", r, err)
	}
	if len(seen) != 1 || seen[0] != "Bearer tok-a" {
		t.Fatalf("sent %q, want the source's token once (and never the key)", seen)
	}

	// the server stops accepting the token (it ran out, or was revoked): one renewal, one retry
	mu.Lock()
	accept, seen = "tok-b", nil
	mu.Unlock()
	if r, _, err := run(t, c, req); err != nil || r.Content != "hi" {
		t.Fatalf("after the token ran out: %+v, %v", r, err)
	}
	if len(seen) != 2 || seen[0] != "Bearer tok-a" || seen[1] != "Bearer tok-b" || src.invalidated != 1 {
		t.Fatalf("sent %q with %d invalidations, want the old token, then the new one", seen, src.invalidated)
	}

	// a server that refuses the new token too is answered with its 401, not with a loop
	mu.Lock()
	accept, seen = "nothing", nil
	mu.Unlock()
	_, _, err := run(t, c, req)
	if KindOf(err) != KindAuth || len(seen) != 2 {
		t.Fatalf("refused twice: %v after %d requests, want an auth error after 2", err, len(seen))
	}
	if strings.Contains(err.Error(), "tok-") {
		t.Errorf("the error quotes a token: %v", err)
	}
}

// TestTokenSourceFailure: without a token nothing is sent, and the error says credentials are
// missing without the source's secrets.
func TestTokenSourceFailure(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	t.Cleanup(srv.Close)
	src := &rotating{err: errors.New("metadata server: connection refused")}
	c := New(Config{API: OpenAI, BaseURL: srv.URL, Auth: src, Timeout: 5 * time.Second})
	_, _, err := run(t, c, Request{Model: "m", Messages: []Message{{Role: RoleUser, Content: "x"}}})
	var e *Error
	if !errors.As(err, &e) || e.Kind != KindAuth || !strings.Contains(e.Message, "no credentials") || hits.Load() != 0 {
		t.Fatalf("err = %v after %d requests, want an auth error and no request", err, hits.Load())
	}
	if _, err := c.Models(context.Background()); KindOf(err) != KindAuth {
		t.Errorf("Models without a token: %v", err)
	}
}

// TestHostedEndpoint (PLAN 26.3): a provider's endpoint is not probed for LM Studio or Ollama, and
// an endpoint without a model list answers with the connection's own.
func TestHostedEndpoint(t *testing.T) {
	var paths []string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	c := New(Config{API: OpenAI, BaseURL: srv.URL + "/v1", Auth: StaticToken("t"), Hosted: true,
		StaticModels: []string{"google/gemini-3.5-flash-lite", "google/gemini-*", ""}, Timeout: 5 * time.Second})
	models, err := c.Models(context.Background())
	if err != nil || len(models) != 1 || models[0].ID != "google/gemini-3.5-flash-lite" {
		t.Fatalf("models = %+v, %v; want the one id written out (a pattern is not a model)", models, err)
	}
	if len(paths) != 1 || paths[0] != "/v1/models" {
		t.Errorf("requests %q, want the model list alone: nothing beside the base address", paths)
	}
	m, ok := c.(Manager)
	if !ok || m.Server() != ServerOther {
		t.Fatalf("server = %v", m.Server())
	}
	if err := m.Load(context.Background(), "x", 0); !errors.Is(err, ErrCannotManage) {
		t.Errorf("Load on a hosted endpoint: %v", err)
	}

	// an endpoint that does list its models is not probed either
	paths = nil
	listing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-x"}]}`))
	}))
	t.Cleanup(listing.Close)
	c = New(Config{API: OpenAI, BaseURL: listing.URL + "/v1", Hosted: true, Timeout: 5 * time.Second})
	if models, err := c.Models(context.Background()); err != nil || len(models) != 1 {
		t.Fatalf("models = %+v, %v", models, err)
	}
	if len(paths) != 1 {
		t.Errorf("requests %q, want one", paths)
	}
}

// TestTokenGoesOnlyToItsHosts (PLAN 26.2): with an allowlist the token is not asked for, and
// nothing is sent, when the address is another host or not https; and a redirect to another host
// is refused, token or not (the guard of 23.5).
func TestTokenGoesOnlyToItsHosts(t *testing.T) {
	var hits atomic.Int32
	other := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	t.Cleanup(other.Close)
	src := &rotating{}
	req := Request{Model: "m", Messages: []Message{{Role: RoleUser, Content: "x"}}}
	for _, base := range []string{other.URL + "/v1", "http://europe-west4-aiplatform.googleapis.com/v1"} {
		c := New(Config{API: OpenAI, BaseURL: base, Auth: src, TokenHost: GoogleAPIHost, HTTPClient: other.Client(), Timeout: 5 * time.Second})
		if _, _, err := run(t, c, req); err == nil || !strings.Contains(err.Error(), "not sent to that address") {
			t.Errorf("%s: err = %v, want a refusal", base, err)
		}
	}
	if src.asked != 0 || hits.Load() != 0 {
		t.Fatalf("%d tokens asked for, %d requests sent; want none of either", src.asked, hits.Load())
	}

	// a server that redirects elsewhere: the client stops, and the other host hears nothing
	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redirecting.Close)
	c := New(Config{API: OpenAI, BaseURL: redirecting.URL, Auth: src, Timeout: 5 * time.Second})
	if _, _, err := run(t, c, req); err == nil {
		t.Error("a redirect to another host was followed")
	}
	if hits.Load() != 0 {
		t.Errorf("the other host got %d requests", hits.Load())
	}
}
