package llmmock

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Vertex makes the mock answer like Vertex AI's OpenAI-compatible endpoint with Google's metadata
// server next to it (PLAN 26.6), so a connection with `auth: google` can be tested without Google:
//
//   - chat lives at the long path only,
//     /v1/projects/{project}/locations/{location}/endpoints/openapi/chat/completions;
//   - there is no model list (GET …/models is 404), and nothing of Ollama or LM Studio;
//   - every request needs a bearer token this mock issued and that hasn't run out: anything else
//     is 401, as a token that expired or was revoked is;
//   - tokens come from the metadata path Google's libraries ask
//     (/computeMetadata/v1/instance/service-accounts/default/token), each valid for TokenTTL.
//
// Point GCE_METADATA_HOST at the mock's address and the standard credentials find it.
type Vertex struct {
	Project, Location string
	// TokenTTL is how long an issued token is accepted (and what its expires_in says).
	TokenTTL time.Duration
	// Now is the clock (nil: time.Now); a test moves it to let tokens run out.
	Now func() time.Time

	mu     sync.Mutex
	n      int
	tokens map[string]time.Time // token → the moment it stops being accepted
	issued int
	denied int
}

func (v *Vertex) now() time.Time {
	if v.Now != nil {
		return v.Now()
	}
	return time.Now()
}

// Base is the path of the endpoint, to append to the mock's address.
func (v *Vertex) Base() string {
	return fmt.Sprintf("/v1/projects/%s/locations/%s/endpoints/openapi", v.Project, v.Location)
}

// Issued is how many tokens were handed out; Denied how many requests were refused with 401.
func (v *Vertex) Issued() int { v.mu.Lock(); defer v.mu.Unlock(); return v.issued }

// Denied is how many requests were refused for their token.
func (v *Vertex) Denied() int { v.mu.Lock(); defer v.mu.Unlock(); return v.denied }

// Revoke makes every issued token unacceptable now (a rotated or revoked credential).
func (v *Vertex) Revoke() {
	v.mu.Lock()
	v.tokens = nil
	v.mu.Unlock()
}

// token answers the metadata server's token request.
func (v *Vertex) token(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Metadata-Flavor", "Google")
	if r.Header.Get("Metadata-Flavor") != "Google" {
		http.Error(w, "Missing Metadata-Flavor:Google header.", http.StatusForbidden)
		return
	}
	ttl := v.TokenTTL
	if ttl <= 0 {
		ttl = time.Hour
	}
	v.mu.Lock()
	v.n++
	v.issued++
	tok := fmt.Sprintf("ya29.mock-%d", v.n)
	if v.tokens == nil {
		v.tokens = map[string]time.Time{}
	}
	v.tokens[tok] = v.now().Add(ttl)
	v.mu.Unlock()
	writeJSON(w, map[string]any{"access_token": tok, "expires_in": int(ttl / time.Second), "token_type": "Bearer"})
}

// authorized reports whether the request carries a token that is still good; it answers 401 itself
// when not.
func (v *Vertex) authorized(w http.ResponseWriter, r *http.Request) bool {
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	v.mu.Lock()
	until, known := v.tokens[tok]
	good := ok && known && v.now().Before(until)
	if !good {
		v.denied++
	}
	v.mu.Unlock()
	if !good {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":401,"message":"Request had invalid authentication credentials.","status":"UNAUTHENTICATED"}}`))
	}
	return good
}

// serve handles a request in Vertex mode; false when the path is not Vertex's or the metadata
// server's (the caller answers 404).
func (s *Server) serveVertex(w http.ResponseWriter, r *http.Request) bool {
	v := s.Vertex
	switch {
	case strings.HasPrefix(r.URL.Path, "/computeMetadata/v1/"):
		if strings.HasSuffix(r.URL.Path, "/token") {
			v.token(w, r)
			return true
		}
		// the library's other questions (is this Google Cloud, the project id)
		w.Header().Set("Metadata-Flavor", "Google")
		_, _ = w.Write([]byte(v.Project))
		return true
	case r.Method == http.MethodPost && r.URL.Path == v.Base()+"/chat/completions":
		if v.authorized(w, r) {
			s.chat(w, r, false)
		}
		return true
	}
	return false
}
