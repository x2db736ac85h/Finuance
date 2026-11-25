// Package googleauth is the token source of a Google Cloud connection (PLAN 26.1, ADR 0045): it
// asks Google's standard credentials for a short-lived access token, so no key is stored.
//
// Credentials are found the standard way (Application Default Credentials):
//   - on GKE with workload identity, and on any Google Cloud machine, from the metadata server;
//   - on a developer's machine, from `gcloud auth application-default login`;
//   - on a cluster outside Google, from an external-account file named by
//     GOOGLE_APPLICATION_CREDENTIALS (workload identity federation).
//
// They are looked for at the first request, not at startup: an instance whose Google connection
// has no credentials yet still starts, and the connection says why it can't answer.
//
// A token is a credential. This package never logs one and returns none in an error.
package googleauth

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// Scope is the one scope asked for: Vertex AI accepts no narrower one. What the credentials may
// actually do is decided by their IAM roles (roles/aiplatform.user is enough).
const Scope = "https://www.googleapis.com/auth/cloud-platform"

// RenewBefore is how long before its expiry a token is replaced: a request that starts with it
// (a model may think for minutes) should end with it.
const RenewBefore = 5 * time.Minute

// Source is an llm.TokenSource over Google's credentials. Safe for concurrent use; concurrent
// callers share one fetch.
type Source struct {
	// find looks the credentials up; replaced in tests.
	find func(ctx context.Context) (oauth2.TokenSource, error)
	now  func() time.Time

	mu  sync.Mutex
	tok string
	// until is when the token runs out, on this source's clock (zero: it doesn't)
	until time.Time
}

// New returns a source over Application Default Credentials.
func New() *Source {
	return &Source{find: findDefault, now: time.Now}
}

// NewWith returns a source over a given way to find credentials and a clock (tests); nil is the
// standard way and the system's clock.
func NewWith(find func(ctx context.Context) (oauth2.TokenSource, error), now func() time.Time) *Source {
	if find == nil {
		find = findDefault
	}
	if now == nil {
		now = time.Now
	}
	return &Source{find: find, now: now}
}

func findDefault(ctx context.Context) (oauth2.TokenSource, error) {
	creds, err := google.FindDefaultCredentials(ctx, Scope)
	if err != nil {
		return nil, err
	}
	return creds.TokenSource, nil
}

var errNoToken = errors.New("the credentials answered without an access token")

// Token returns a token that is valid for at least RenewBefore more, fetching one when the cached
// one is missing, about to expire or was invalidated.
func (s *Source) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tok != "" && s.fresh() {
		return s.tok, nil
	}
	// The credentials' own source caches too (until ten seconds before the expiry), so for a new
	// token the credentials are looked up again: cheap for a file, one request to the metadata server.
	creds, err := s.find(ctx)
	if err != nil {
		return "", fmt.Errorf("find Google credentials: %w", err)
	}
	tok, err := creds.Token()
	if err != nil {
		return "", fmt.Errorf("get a Google access token: %w", err)
	}
	if tok == nil || tok.AccessToken == "" {
		return "", errNoToken
	}
	// The lifetime is kept as a length of time from now on this source's clock: the library stamps
	// the expiry with the system's clock, and the two need not agree (a test's clock doesn't).
	s.tok, s.until = tok.AccessToken, time.Time{}
	if !tok.Expiry.IsZero() {
		s.until = s.now().Add(time.Until(tok.Expiry))
	}
	return s.tok, nil
}

// fresh reports whether the cached token can still be used for a new request. A token without an
// expiry never runs out by the clock.
func (s *Source) fresh() bool {
	return s.until.IsZero() || s.now().Add(RenewBefore).Before(s.until)
}

// Invalidate drops the cached token: the next Token fetches a new one.
func (s *Source) Invalidate() {
	s.mu.Lock()
	s.tok, s.until = "", time.Time{}
	s.mu.Unlock()
}
