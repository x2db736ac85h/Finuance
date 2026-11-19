package auth

import (
	"bytes"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestTOTPVectors: the test vectors of RFC 6238, Appendix B (SHA-1 column; its eight-digit values
// end in our six).
func TestTOTPVectors(t *testing.T) {
	secret := []byte("12345678901234567890")
	for unix, want := range map[int64]string{
		59: "287082", 1111111109: "081804", 1111111111: "050471", 1234567890: "005924", 2000000000: "279037", 20000000000: "353130",
	} {
		if got := totpCode(secret, totpStep(time.Unix(unix, 0))); got != want {
			t.Errorf("at %d: %s, want %s", unix, got, want)
		}
	}
	// RFC 4226, Appendix D: HOTP counters 0..2
	for counter, want := range []string{"755224", "287082", "359152"} {
		if got := totpCode(secret, int64(counter)); got != want {
			t.Errorf("counter %d: %s, want %s", counter, got, want)
		}
	}
}

func TestVerifyTOTP(t *testing.T) {
	secret, err := NewTOTPSecret()
	if err != nil || len(secret) != 20 {
		t.Fatal(err, len(secret))
	}
	now := time.Unix(1_800_000_015, 0)
	step := totpStep(now)
	code := totpCode(secret, step)

	if got, ok := VerifyTOTP(secret, code, now, 0); !ok || got != step {
		t.Errorf("the current code: %d %v", got, ok)
	}
	if got, ok := VerifyTOTP(secret, " "+code[:3]+" "+code[3:]+" ", now, 0); !ok || got != step {
		t.Errorf("the code with spaces: %d %v", got, ok)
	}
	// drift: one step either way, not two
	for delta, want := range map[int64]bool{-2: false, -1: true, 0: true, 1: true, 2: false} {
		c := totpCode(secret, step+delta)
		if got, ok := VerifyTOTP(secret, c, now, 0); ok != want || (ok && got != step+delta) {
			t.Errorf("a code %d steps away: step %d ok %v, want %v", delta, got, ok, want)
		}
	}
	// replay: the step that was accepted, and every earlier one, is refused afterwards
	if _, ok := VerifyTOTP(secret, code, now, step); ok {
		t.Error("the same code was accepted twice")
	}
	if _, ok := VerifyTOTP(secret, totpCode(secret, step-1), now, step); ok {
		t.Error("an older code was accepted after a newer one")
	}
	if got, ok := VerifyTOTP(secret, totpCode(secret, step+1), now, step); !ok || got != step+1 {
		t.Error("the next code was refused after the current one")
	}
	// not a code
	other, _ := NewTOTPSecret()
	for _, bad := range []string{"", "12345", "1234567", "abcdef", code + "0", totpCode(other, step)} {
		if bad == code {
			continue // one in a million: the other secret gives the same code
		}
		if _, ok := VerifyTOTP(secret, bad, now, 0); ok {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestTOTPURI(t *testing.T) {
	secret := []byte("12345678901234567890")
	if got := TOTPSecretText(secret); got != "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ" {
		t.Errorf("secret text %s", got)
	}
	uri := TOTPURI("Finuance (home: lab)", "anna+test@example.com", secret)
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "otpauth" || u.Host != "totp" {
		t.Fatalf("%s: %v", uri, err)
	}
	if u.Path != "/Finuance (home  lab):anna+test@example.com" {
		t.Errorf("label %q", u.Path)
	}
	q := u.Query()
	if q.Get("secret") != "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ" || q.Get("issuer") != "Finuance (home: lab)" || q.Get("algorithm") != "SHA1" ||
		q.Get("digits") != "6" || q.Get("period") != "30" {
		t.Errorf("query %v", q)
	}
	if strings.Contains(uri, "+") && !strings.Contains(uri, "anna+test") {
		t.Errorf("a space was written as a plus sign, which some apps show literally: %s", uri)
	}
}

func TestRecoveryCodes(t *testing.T) {
	codes, hashes, err := NewRecoveryCodes()
	if err != nil || len(codes) != RecoveryCodes || len(hashes) != RecoveryCodes {
		t.Fatal(err, len(codes))
	}
	seen := map[string]bool{}
	for i, c := range codes {
		if len(c) != 11 || c[5] != '-' || strings.ToLower(c) != c || seen[c] {
			t.Errorf("code %q", c)
		}
		seen[c] = true
		for _, typed := range []string{c, strings.ToUpper(c), strings.ReplaceAll(c, "-", ""), " " + c[:5] + " " + c[6:] + " "} {
			if !bytes.Equal(RecoveryCodeHash(typed), hashes[i]) {
				t.Errorf("%q does not hash like %q", typed, c)
			}
			if !looksLikeRecoveryCode(typed) {
				t.Errorf("%q is not taken for a recovery code", typed)
			}
		}
		if bytes.Contains(hashes[i], []byte(strings.ReplaceAll(c, "-", ""))) {
			t.Error("the hash contains the code")
		}
	}
	if looksLikeRecoveryCode("123456") || looksLikeRecoveryCode("123 456") {
		t.Error("a one-time code is taken for a recovery code")
	}
}
