package secret

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestSealOpen(t *testing.T) {
	key := bytes.Repeat([]byte{7}, KeySize)
	b, err := New(key)
	if err != nil {
		t.Fatal(err)
	}
	s1, err := b.Seal("sk-live-123", "openai")
	if err != nil {
		t.Fatal(err)
	}
	s2, _ := b.Seal("sk-live-123", "openai")
	if bytes.Equal(s1, s2) || bytes.Contains(s1, []byte("sk-live")) || s1[0] != version1 {
		t.Errorf("sealed = %x / %x (a fresh nonce each time, never the plain text)", s1, s2)
	}
	if got, err := b.Open(s1, "openai"); err != nil || got != "sk-live-123" {
		t.Errorf("open = %q %v", got, err)
	}
	other, _ := New(bytes.Repeat([]byte{8}, KeySize))
	tampered := append([]byte{}, s1...)
	tampered[len(tampered)-1] ^= 1
	for name, open := range map[string]func() (string, error){
		"another key":   func() (string, error) { return other.Open(s1, "openai") },
		"another place": func() (string, error) { return b.Open(s1, "anthropic") },
		"changed bytes": func() (string, error) { return b.Open(tampered, "openai") },
		"too short":     func() (string, error) { return b.Open(s1[:10], "openai") },
		"other version": func() (string, error) { return b.Open(append([]byte{2}, s1[1:]...), "openai") },
	} {
		if got, err := open(); !errors.Is(err, ErrOpen) || got != "" {
			t.Errorf("%s: %q %v", name, got, err)
		}
	}
	var none *Box
	if _, err := none.Seal("x", "y"); !errors.Is(err, ErrNoKey) {
		t.Errorf("no key: %v", err)
	}
	if _, err := none.Open(s1, "openai"); !errors.Is(err, ErrNoKey) {
		t.Errorf("no key: %v", err)
	}
}

func TestParseKey(t *testing.T) {
	raw := bytes.Repeat([]byte{0xfb}, KeySize)
	for _, s := range []string{base64.StdEncoding.EncodeToString(raw), base64.RawURLEncoding.EncodeToString(raw), " " + base64.StdEncoding.EncodeToString(raw) + "\n"} {
		if k, err := ParseKey(s); err != nil || !bytes.Equal(k, raw) {
			t.Errorf("%q: %x %v", s, k, err)
		}
	}
	if _, err := ParseKey(base64.StdEncoding.EncodeToString(raw[:16])); err == nil || !strings.Contains(err.Error(), "32 bytes") {
		t.Errorf("short key: %v", err)
	}
	if _, err := ParseKey("not base64!"); err == nil {
		t.Error("garbage accepted")
	}
	if _, err := New(raw[:31]); err == nil {
		t.Error("31-byte key accepted")
	}
}
