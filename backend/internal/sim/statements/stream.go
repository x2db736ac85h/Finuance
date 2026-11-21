package statements

import (
	"crypto/sha256"
	"encoding/binary"
)

// stream is the generator's randomness: SHA-256 in counter mode over a key made of its parts (the
// persona and the day). Its output is fixed by this definition, so a day's rows stay the same
// across Go releases; math/rand/v2 guarantees only its sources' output, not its methods'.
type stream struct {
	key   [32]byte
	block uint64
	buf   [32]byte
	off   int
}

func newStream(parts ...string) *stream {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	s := &stream{off: len(stream{}.buf)}
	copy(s.key[:], h.Sum(nil))
	return s
}

func (s *stream) uint64() uint64 {
	if s.off+8 > len(s.buf) {
		var in [40]byte
		copy(in[:32], s.key[:])
		binary.LittleEndian.PutUint64(in[32:], s.block)
		s.buf = sha256.Sum256(in[:])
		s.block++
		s.off = 0
	}
	v := binary.LittleEndian.Uint64(s.buf[s.off:])
	s.off += 8
	return v
}

// intn returns a number in [0, n) (n > 0; the modulo's bias is far below what matters here).
func (s *stream) intn(n int) int { return int(s.uint64() % uint64(n)) } //nolint:gosec // n > 0 (a length or a constant), and the result is below n

// permille is true with probability p/1000.
func (s *stream) permille(p int) bool { return s.intn(1000) < p }

// skewed returns an amount in [lo, hi], skewed towards lo (lo + a·b·(hi−lo) with a and b uniform,
// as the demo year draws its purchases).
func (s *stream) skewed(lo, hi int64) int64 {
	a, b := int64(s.intn(1001)), int64(s.intn(1001))
	return lo + a*b*(hi-lo)/1_000_000
}

func pick[T any](s *stream, xs []T) T { return xs[s.intn(len(xs))] }
