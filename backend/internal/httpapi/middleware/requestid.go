package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"regexp"
)

// HeaderRequestID is accepted from clients and echoed in every response.
const HeaderRequestID = "X-Request-ID"

var validRequestID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// AssignRequestID accepts a well-formed X-Request-ID (≤ 64 chars of [A-Za-z0-9_-]) from any client and
// generates one otherwise. The id is echoed in the response and stored in the request Info.
func AssignRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r, info := withInfo(r)
		id := r.Header.Get(HeaderRequestID)
		if !validRequestID.MatchString(id) {
			id = newID()
		}
		info.RequestID = id
		w.Header().Set(HeaderRequestID, id)
		next.ServeHTTP(w, r)
	})
}

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // crypto/rand.Read never fails on supported platforms
	return hex.EncodeToString(b)
}
