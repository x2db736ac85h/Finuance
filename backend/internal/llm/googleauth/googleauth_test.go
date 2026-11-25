package googleauth_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/x2db736ac85h/finuance/backend/internal/llm/googleauth"
)

// metadata is a stand-in for Google's metadata server: every token request is counted and answered
// with a new token that lives for ttl.
type metadata struct {
	srv   *httptest.Server
	calls atomic.Int32
	ttl   time.Duration
	fail  atomic.Bool
}

func fakeMetadata(t *testing.T, ttl time.Duration) *metadata {
	t.Helper()
	m := &metadata{ttl: ttl}
	m.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Metadata-Flavor") != "Google" {
			http.Error(w, "no flavor", http.StatusForbidden)
			return
		}
		w.Header().Set("Metadata-Flavor", "Google")
		if !strings.HasSuffix(r.URL.Path, "/token") {
			_, _ = w.Write([]byte("ok")) // the library's other questions (is this Google Cloud, which project)
			return
		}
		n := m.calls.Add(1)
		if m.fail.Load() {
			http.Error(w, "the service account is disabled", http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "ya29.token-" + string(rune('0'+n)),
			"expires_in":   int(m.ttl / time.Second),
			"token_type":   "Bearer",
		})
	}))
	t.Cleanup(m.srv.Close)
	// the standard way to find credentials, pointed at the stand-in and at nothing of this machine
	t.Setenv("GCE_METADATA_HOST", strings.TrimPrefix(m.srv.URL, "http://"))
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("CLOUDSDK_CONFIG", t.TempDir())
	return m
}

// TestMetadataServer: credentials come from the metadata server; a token is reused while it has
// more than RenewBefore left and replaced before it runs out, without a failed request in between.
func TestMetadataServer(t *testing.T) {
	m := fakeMetadata(t, 30*time.Minute)
	var ahead time.Duration
	s := googleauth.NewWith(nil, func() time.Time { return time.Now().Add(ahead) })
	ctx := context.Background()

	first, err := s.Token(ctx)
	if err != nil || first != "ya29.token-1" {
		t.Fatalf("first token = %q, %v", first, err)
	}
	for range 3 {
		if tok, err := s.Token(ctx); err != nil || tok != first {
			t.Fatalf("a fresh token was not reused: %q, %v", tok, err)
		}
	}
	ahead = 24 * time.Minute // 6 minutes left: still good
	if tok, _ := s.Token(ctx); tok != first {
		t.Errorf("with 6 minutes left the token was replaced by %q", tok)
	}
	ahead = 26 * time.Minute // 4 minutes left: replaced before a request can fail with it
	second, err := s.Token(ctx)
	if err != nil || second == first {
		t.Fatalf("with 4 minutes left: %q, %v; want a new token", second, err)
	}
	if n := m.calls.Load(); n != 2 {
		t.Errorf("%d token requests, want 2", n)
	}
}

// TestInvalidate: after a 401 the cached token is dropped whatever its expiry says.
func TestInvalidate(t *testing.T) {
	m := fakeMetadata(t, time.Hour)
	s := googleauth.NewWith(nil, nil)
	first, _ := s.Token(context.Background())
	s.Invalidate()
	second, err := s.Token(context.Background())
	if err != nil || second == first || m.calls.Load() != 2 {
		t.Errorf("after Invalidate: %q then %q (%v), %d requests; want two different tokens from two requests", first, second, err, m.calls.Load())
	}
}

// TestConcurrentCallersShareOneFetch: many requests starting at once ask for one token.
func TestConcurrentCallersShareOneFetch(t *testing.T) {
	m := fakeMetadata(t, time.Hour)
	s := googleauth.NewWith(nil, nil)
	var wg sync.WaitGroup
	toks := make([]string, 32)
	for i := range toks {
		wg.Go(func() {
			toks[i], _ = s.Token(context.Background())
		})
	}
	wg.Wait()
	for _, tok := range toks {
		if tok != toks[0] || tok == "" {
			t.Fatalf("tokens differ: %q", toks)
		}
	}
	if n := m.calls.Load(); n != 1 {
		t.Errorf("%d token requests for 32 callers, want 1", n)
	}
}

// TestErrors: an answer without credentials is an error that names no token, and the next call
// tries again.
func TestErrors(t *testing.T) {
	m := fakeMetadata(t, time.Hour)
	m.fail.Store(true)
	s := googleauth.NewWith(nil, nil)
	if tok, err := s.Token(context.Background()); err == nil || tok != "" || strings.Contains(err.Error(), "ya29") {
		t.Fatalf("a refused token request: %q, %v", tok, err)
	}
	m.fail.Store(false)
	if tok, err := s.Token(context.Background()); err != nil || tok == "" {
		t.Errorf("after the refusal ended: %q, %v", tok, err)
	}

	boom := errors.New("no credentials here")
	none := googleauth.NewWith(func(context.Context) (oauth2.TokenSource, error) { return nil, boom }, nil)
	if _, err := none.Token(context.Background()); !errors.Is(err, boom) {
		t.Errorf("credentials that can't be found: %v", err)
	}
	empty := googleauth.NewWith(func(context.Context) (oauth2.TokenSource, error) {
		return oauth2.StaticTokenSource(&oauth2.Token{}), nil
	}, nil)
	if _, err := empty.Token(context.Background()); err == nil {
		t.Error("an answer without an access token was accepted")
	}
}
