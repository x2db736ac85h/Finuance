// Package auth holds the authentication domain: password hashing, sessions, and the user-account
// operations behind /auth and /me (PLAN Phase 5, §8.1). It talks to the users and sessions tables
// through store.WithoutUser (they are outside row-level security) and to user data through
// store.WithUser.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Params are the argon2id cost parameters (ADR 0010). The defaults are OWASP's baseline:
// 19 MiB of memory, 2 passes, 1 lane.
type Params struct {
	MemoryKiB uint32
	Time      uint32
	Threads   uint8
}

// DefaultParams is the OWASP baseline.
var DefaultParams = Params{MemoryKiB: 19 * 1024, Time: 2, Threads: 1}

const (
	saltLen = 16
	keyLen  = 32
	// MaxPasswordBytes caps the input so a megabyte-long password can't be used to burn CPU.
	// (argon2 hashes the whole password; the limit is generous for any passphrase.)
	MaxPasswordBytes = 1024
)

// ErrMalformedHash means a stored hash is not a PHC argon2id string this package can read.
var ErrMalformedHash = errors.New("auth: malformed password hash")

var b64 = base64.RawStdEncoding // the PHC string format uses unpadded standard base64

// Hash returns the PHC string for password:
//
//	$argon2id$v=19$m=19456,t=2,p=1$<salt>$<hash>
//
// The string carries its own parameters and salt, so the parameters can change without
// invalidating existing hashes (see NeedsRehash).
func Hash(password string, p Params) (string, error) {
	if len(password) > MaxPasswordBytes {
		return "", fmt.Errorf("password longer than %d bytes", MaxPasswordBytes)
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, p.Time, p.MemoryKiB, p.Threads, keyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.MemoryKiB, p.Time, p.Threads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// Verify reports whether password matches the PHC string. The comparison is constant-time. A
// password over MaxPasswordBytes never matches (and isn't hashed).
func Verify(password, phc string) (bool, error) {
	p, salt, key, err := decode(phc)
	if err != nil {
		return false, err
	}
	if len(password) > MaxPasswordBytes {
		return false, nil
	}
	got := argon2.IDKey([]byte(password), salt, p.Time, p.MemoryKiB, p.Threads, uint32(len(key))) //nolint:gosec // key length comes from our own 32-byte hashes
	return subtle.ConstantTimeCompare(got, key) == 1, nil
}

// NeedsRehash reports whether phc was made with parameters other than want, so a successful
// login can upgrade it. A malformed hash also needs rehashing.
func NeedsRehash(phc string, want Params) bool {
	p, _, _, err := decode(phc)
	return err != nil || p != want
}

// decode parses $argon2id$v=19$m=…,t=…,p=…$salt$key.
func decode(phc string) (Params, []byte, []byte, error) {
	parts := strings.Split(phc, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v="+strconv.Itoa(argon2.Version) {
		return Params{}, nil, nil, ErrMalformedHash
	}
	var p Params
	for _, kv := range strings.Split(parts[3], ",") {
		k, v, ok := strings.Cut(kv, "=")
		n, err := strconv.ParseUint(v, 10, 32)
		if !ok || err != nil || n == 0 {
			return Params{}, nil, nil, ErrMalformedHash
		}
		switch k {
		case "m":
			p.MemoryKiB = uint32(n)
		case "t":
			p.Time = uint32(n)
		case "p":
			if n > 255 {
				return Params{}, nil, nil, ErrMalformedHash
			}
			p.Threads = uint8(n)
		default:
			return Params{}, nil, nil, ErrMalformedHash
		}
	}
	if p.MemoryKiB == 0 || p.Time == 0 || p.Threads == 0 {
		return Params{}, nil, nil, ErrMalformedHash
	}
	salt, err1 := b64.DecodeString(parts[4])
	key, err2 := b64.DecodeString(parts[5])
	if err1 != nil || err2 != nil || len(salt) < 8 || len(key) < 16 {
		return Params{}, nil, nil, ErrMalformedHash
	}
	return p, salt, key, nil
}

// dummyHash is verified against when the email is unknown, so a failed login costs the same time
// whether or not the account exists (§8.1). It is computed once per parameter set.
func dummyHash(p Params) string {
	h, err := Hash("finuance-dummy-password", p)
	if err != nil {
		panic(err) // only fails if the system random source fails
	}
	return h
}
