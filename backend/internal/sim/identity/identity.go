// Package identity names the traffic simulator's synthetic users and derives their passwords from
// one seed (PLAN Phase 25, ADR 0043). The instance's `finuance admin synthetic-users` and the
// simulator both import it, so no password travels between them: each side derives the same one.
// It imports nothing of the server or of the simulator, so the production binary gains no more
// than these few functions.
package identity

import (
	"bufio"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
)

// Max is the most personas the simulator plays: load mode's ceiling (ADR 0044); realistic mode
// plays at most 50 (sim.MaxPersonas).
const Max = 500

// Domain is the e-mail domain of every persona; .invalid is reserved and never delivers.
const Domain = "sim.invalid"

// MinSeedBytes is the shortest seed accepted (FINUANCE_SIM_SEED; `openssl rand -hex 32` gives 64).
const MinSeedBytes = 32

// Animals are the ten characters, in the order the personas are numbered (PLAN Phase 25, "The
// animals"); a count above ten starts the list again with a number (otter-2, owl-2, …).
var Animals = [...]string{"otter", "owl", "badger", "magpie", "heron", "hedgehog", "fox", "squirrel", "mole", "sloth"}

// Persona is one synthetic user.
type Persona struct {
	Index  int    // 0 … Max-1
	Animal string // the character: one of Animals
	Slug   string // otter, …, otter-2: the persona's name in flags, metrics and request ids
	Name   string // Otter, …, Otter 2: the display name
	Email  string // otter@sim.invalid
}

// Nth returns the i-th persona (0 ≤ i < Max).
func Nth(i int) Persona {
	if i < 0 || i >= Max {
		panic(fmt.Sprintf("identity: persona %d out of range [0, %d)", i, Max))
	}
	animal := Animals[i%len(Animals)]
	slug, name := animal, strings.ToUpper(animal[:1])+animal[1:]
	if round := i/len(Animals) + 1; round > 1 {
		slug += "-" + strconv.Itoa(round)
		name += " " + strconv.Itoa(round)
	}
	return Persona{Index: i, Animal: animal, Slug: slug, Name: name, Email: slug + "@" + Domain}
}

// First returns the first n personas (1 ≤ n ≤ Max).
func First(n int) ([]Persona, error) {
	if n < 1 || n > Max {
		return nil, fmt.Errorf("the number of personas must be between 1 and %d, not %d", Max, n)
	}
	out := make([]Persona, n)
	for i := range out {
		out[i] = Nth(i)
	}
	return out, nil
}

// BySlug finds a persona by its slug (fox, otter-2).
func BySlug(slug string) (Persona, bool) {
	animal, round := slug, 1
	if i := strings.LastIndexByte(slug, '-'); i > 0 {
		n, err := strconv.Atoi(slug[i+1:])
		if err != nil || n < 2 {
			return Persona{}, false
		}
		animal, round = slug[:i], n
	}
	k := slices.Index(Animals[:], animal)
	if k < 0 || round > Max/len(Animals) {
		return Persona{}, false
	}
	p := Nth((round-1)*len(Animals) + k)
	return p, p.Slug == slug // otter-02 is not otter-2
}

// ErrShortSeed is the error for a seed shorter than MinSeedBytes.
var ErrShortSeed = fmt.Errorf("the simulator's seed must be at least %d bytes", MinSeedBytes)

// Password derives a persona's password from the seed: HKDF-SHA256 with the persona's slug in the
// info, 18 bytes written as 24 URL-safe characters. The same seed and slug always give the same
// password; knowing one password tells nothing about another or about the seed.
func Password(seed []byte, slug string) (string, error) {
	if len(seed) < MinSeedBytes {
		return "", ErrShortSeed
	}
	if slug == "" {
		return "", errors.New("identity: no persona named")
	}
	key, err := hkdf.Key(sha256.New, seed, nil, "finuance-sim/v1 password "+slug, 18)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(key), nil
}

// ReadSeed takes the seed from the first line of r (stdin: never on a command line), without the
// line ending.
func ReadSeed(r io.Reader) ([]byte, error) {
	if r == nil {
		return nil, errors.New("the seed is read from stdin (-seed-stdin)")
	}
	line, err := bufio.NewReader(io.LimitReader(r, 4096)).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	seed := []byte(strings.TrimRight(line, "\r\n"))
	if len(seed) < MinSeedBytes {
		return nil, ErrShortSeed
	}
	return seed, nil
}
