package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238's HMAC-SHA1 is what authenticator apps implement; HMAC does not rest on SHA-1's collision resistance
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Time-based one-time passwords (RFC 6238 on RFC 4226) for the second step of signing in (PLAN
// 23.7, ADR 0040): HMAC-SHA1, six digits, 30-second steps, the parameters every authenticator app
// uses by default. No dependency: the algorithm is a dozen lines over the standard library.
const (
	totpPeriod     = 30 * time.Second
	totpDigits     = 6
	totpSecretSize = 20 // bytes; RFC 4226 recommends 160 bits
	totpDrift      = 1  // steps accepted before and after the current one (a slow clock, a slow typist)

	// RecoveryCodes is how many single-use codes a user gets; each is 50 random bits.
	RecoveryCodes = 10
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewTOTPSecret returns a fresh random secret.
func NewTOTPSecret() ([]byte, error) {
	s := make([]byte, totpSecretSize)
	_, err := rand.Read(s)
	return s, err
}

// TOTPSecretText is the secret as people type it into an authenticator app (base32).
func TOTPSecretText(secret []byte) string { return b32.EncodeToString(secret) }

// TOTPURI is the otpauth:// address an authenticator app reads from a QR code. issuer names the
// instance and account the user in the app's list.
func TOTPURI(issuer, account string, secret []byte) string {
	q := url.Values{"secret": {TOTPSecretText(secret)}, "issuer": {issuer}, "algorithm": {"SHA1"},
		"digits": {fmt.Sprint(totpDigits)}, "period": {fmt.Sprint(int(totpPeriod.Seconds()))}}
	// the label is "issuer:account"; a colon inside either would split it differently
	label := strings.ReplaceAll(issuer, ":", " ") + ":" + strings.ReplaceAll(account, ":", " ")
	return (&url.URL{Scheme: "otpauth", Host: "totp", Path: "/" + label, RawQuery: strings.ReplaceAll(q.Encode(), "+", "%20")}).String()
}

// totpStep is the number of the 30-second step t falls into.
func totpStep(t time.Time) int64 { return t.Unix() / int64(totpPeriod.Seconds()) }

// totpCode is the code of one step (RFC 4226 §5.3 with the step as the counter).
func totpCode(secret []byte, step int64) string {
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(step)) //nolint:gosec // steps since 1970 are positive
	mac := hmac.New(sha1.New, secret)
	mac.Write(counter[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", totpDigits, v%1_000_000)
}

// VerifyTOTP checks a code typed at now against the current step and one step either way. It
// returns the step the code belongs to, which the caller stores: a step at or before lastStep is
// never accepted again, so a code can't be used twice (RFC 6238 §5.2). Every candidate is compared
// in constant time, whether or not an earlier one matched.
func VerifyTOTP(secret []byte, code string, now time.Time, lastStep int64) (step int64, ok bool) {
	code = cleanCode(code)
	if len(code) != totpDigits {
		return 0, false
	}
	current := totpStep(now)
	for s := current - totpDrift; s <= current+totpDrift; s++ {
		match := subtle.ConstantTimeCompare([]byte(totpCode(secret, s)), []byte(code)) == 1
		if match && s > lastStep && !ok {
			step, ok = s, true
		}
	}
	return step, ok
}

// cleanCode drops the spaces and hyphens people type or paste with a code ("123 456", "abcde-fghij").
func cleanCode(code string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' || r == '\t' {
			return -1
		}
		return r
	}, strings.TrimSpace(code))
}

// NewRecoveryCodes returns fresh codes as shown to the user once ("abcde-fghij") with the hash of
// each as it is stored.
func NewRecoveryCodes() (codes []string, hashes [][]byte, err error) {
	for range RecoveryCodes {
		raw := make([]byte, 7) // 56 bits; ten base32 characters carry 50 of them
		if _, err := rand.Read(raw); err != nil {
			return nil, nil, err
		}
		text := strings.ToLower(b32.EncodeToString(raw))[:10]
		codes = append(codes, text[:5]+"-"+text[5:])
		hashes = append(hashes, RecoveryCodeHash(text))
	}
	return codes, hashes, nil
}

// RecoveryCodeHash is what is stored for a recovery code and looked up when one is typed: SHA-256
// of the code without its hyphen, in lower case. The codes are random (50 bits), so a plain hash is
// enough and lets the database find the row; nothing here is a password a person chose.
func RecoveryCodeHash(code string) []byte {
	sum := sha256.Sum256([]byte("finuance recovery code\x00" + strings.ToLower(cleanCode(code))))
	return sum[:]
}

// looksLikeRecoveryCode tells a recovery code from a one-time code by its shape, so the second
// step takes either in one field.
func looksLikeRecoveryCode(code string) bool { return len(cleanCode(code)) == 10 }
