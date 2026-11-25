package llm

import "context"

// TokenSource supplies a short-lived bearer token for a connection that has no stored key
// (PLAN 26.1): a Google Cloud connection asks its credentials for one (llm/googleauth). It is asked
// before every request and answers from its cache until the token is about to expire.
//
// A token is a credential like a key: it is never logged and never a span attribute.
type TokenSource interface {
	// Token returns a token that is valid now, fetching or renewing one when needed. Concurrent
	// callers share one fetch.
	Token(ctx context.Context) (string, error)
	// Invalidate drops the cached token: the server answered 401 with it, so the next Token
	// fetches a new one whatever the old one's expiry says.
	Invalidate()
}

// StaticToken is a TokenSource that always answers the same token (tests, and a key used as one).
type StaticToken string

// Token returns the token.
func (s StaticToken) Token(context.Context) (string, error) { return string(s), nil }

// Invalidate does nothing: there is no other token to get.
func (StaticToken) Invalidate() {}
