// Package secret seals small secrets stored in the database (the assistant connections' API keys,
// ADR 0035) with AES-256-GCM under the instance key (FINUANCE_INSTANCE_KEY). A sealed value is a
// version byte, a random 12-byte nonce and the ciphertext with its tag; the associated data binds
// it to where it is stored (a connection's name), so a value copied to another row doesn't open.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// KeySize is the instance key's length in bytes.
const KeySize = 32

const version1 = 1

// ErrNoKey means there is no instance key to seal or open with.
var ErrNoKey = errors.New("no instance key (FINUANCE_INSTANCE_KEY)")

// ErrOpen means a value didn't open: another key, another place, or changed bytes.
var ErrOpen = errors.New("the secret can't be opened with this instance key")

// Box seals and opens with one key. A nil Box has no key.
type Box struct {
	aead cipher.AEAD
}

// ParseKey reads a base64 instance key (standard or URL alphabet, padding optional) of 32 bytes.
func ParseKey(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			if len(b) != KeySize {
				return nil, fmt.Errorf("the instance key must be %d bytes, got %d", KeySize, len(b))
			}
			return b, nil
		}
	}
	return nil, errors.New("the instance key must be base64 (e.g. openssl rand -base64 32)")
}

// New returns a Box for a 32-byte key.
func New(key []byte) (*Box, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("the instance key must be %d bytes", KeySize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Seal encrypts plain for the place named by where.
func (b *Box) Seal(plain, where string) ([]byte, error) {
	if b == nil {
		return nil, ErrNoKey
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := make([]byte, 0, 1+len(nonce)+len(plain)+b.aead.Overhead())
	out = append(append(out, version1), nonce...)
	return b.aead.Seal(out, nonce, []byte(plain), []byte(where)), nil
}

// Open decrypts a value sealed for where.
func (b *Box) Open(sealed []byte, where string) (string, error) {
	if b == nil {
		return "", ErrNoKey
	}
	n := b.aead.NonceSize()
	if len(sealed) < 1+n+b.aead.Overhead() || sealed[0] != version1 {
		return "", ErrOpen
	}
	plain, err := b.aead.Open(nil, sealed[1:1+n], sealed[1+n:], []byte(where))
	if err != nil {
		return "", ErrOpen
	}
	return string(plain), nil
}
