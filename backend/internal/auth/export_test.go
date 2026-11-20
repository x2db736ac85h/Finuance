package auth

import "time"

// CodeAt is the one-time code of a secret (as an authenticator app shows it) at a time; for tests.
func CodeAt(secretText string, t time.Time) string {
	raw, err := b32.DecodeString(secretText)
	if err != nil {
		panic(err)
	}
	return totpCode(raw, totpStep(t))
}
