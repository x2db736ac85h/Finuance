package middleware

import (
	"net/netip"
	"slices"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

// Policy is a token bucket: Events per Per, with bursts up to Events (ADR 0012).
type Policy struct {
	Events int
	Per    time.Duration
}

// Rate limits from docs/PLAN.md §6.1.
var (
	LoginPerIP      = Policy{Events: 30, Per: time.Minute} // login and signup, per client IP
	LoginPerIPEmail = Policy{Events: 10, Per: time.Minute} // login and signup, per (IP, email)
	// failed sign-ins per e-mail address, whatever address they come from: guessing one account's
	// password from many addresses ends here (API-1). It also keeps the account's owner out while
	// it lasts, which is why it is generous and why two-step sign-in is the stronger answer.
	LoginFailuresPerEmail = Policy{Events: 20, Per: time.Hour}
	PasswordPerUser       = Policy{Events: 10, Per: time.Minute}      // password checks of a signed-in user
	SecondStepPerUser     = Policy{Events: 10, Per: 10 * time.Minute} // codes at the second step of signing in (ADR 0040)
	UploadPerUser         = Policy{Events: 30, Per: time.Minute}      // uploads (Phase 6)
	// each of these reads and parses a whole stored file again: changing a preview, listing its lines,
	// reprocessing an import (IMP-4). A person adjusting a preview by hand stays far below it.
	ReparsePerUser = Policy{Events: 60, Per: time.Minute}
	DemoPerIP      = Policy{Events: 5, Per: time.Hour} // new demo users, per client IP (PLAN 19.1)
	// asking the model servers for their state again instead of using the cached answer (AI-5)
	StatusRefreshPerUser = Policy{Events: 6, Per: time.Minute}
	// what one member writes into shared groups: every write is read by, and told to, the others (API-5)
	SharedWritesPerUser  = Policy{Events: 120, Per: time.Minute}
	SharedInvitesPerUser = Policy{Events: 30, Per: time.Hour}
	FriendCodesPerHour   = Policy{Events: 10, Per: time.Hour} // friend codes entered, per user and per IP (ADR 0027)
	// errors the web app reports, per client address: a page that fails in a loop can't flood the log (PLAN 24.8)
	ClientEventsPerIP = Policy{Events: 30, Per: time.Minute}
)

// DefaultMaxKeys caps the buckets one Limiter keeps, so a flood of distinct IPs or emails can't
// grow memory without bound.
const DefaultMaxKeys = 100_000

// Limiter keeps one token bucket per key (an IP, an IP + email, a user id). It is in memory, so
// limits are per api replica (ADR 0012).
type Limiter struct {
	policy  Policy
	clock   platform.Clock
	maxKeys int

	mu      sync.Mutex
	buckets map[string]*bucket
	limited func() // called for every refusal (finuance_rate_limited_total); nil: nothing
}

type bucket struct {
	lim  *rate.Limiter
	last time.Time
}

// NewLimiter returns a limiter for p. maxKeys ≤ 0 means DefaultMaxKeys.
func NewLimiter(p Policy, clock platform.Clock, maxKeys int) *Limiter {
	if maxKeys <= 0 {
		maxKeys = DefaultMaxKeys
	}
	return &Limiter{policy: p, clock: clock, maxKeys: maxKeys, buckets: map[string]*bucket{}}
}

// OnLimited makes the limiter call f for every request it refuses, and returns the limiter.
func (l *Limiter) OnLimited(f func()) *Limiter {
	l.limited = f
	return l
}

func (l *Limiter) refuse(d time.Duration) error {
	if l.limited != nil {
		l.limited()
	}
	return &platform.RetryAfterError{After: d}
}

// Allow takes one token for key. When the bucket is empty it returns a *platform.RetryAfterError
// saying when the next token arrives.
func (l *Limiter) Allow(key string) error {
	now := l.clock.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.buckets[key]
	if b == nil {
		if len(l.buckets) >= l.maxKeys {
			l.evict(now)
		}
		b = &bucket{lim: rate.NewLimiter(rate.Every(l.policy.Per/time.Duration(l.policy.Events)), l.policy.Events)}
		l.buckets[key] = b
	}
	b.last = now
	r := b.lim.ReserveN(now, 1)
	if d := r.DelayFrom(now); d > 0 {
		r.CancelAt(now) // don't consume a future token for a rejected request
		return l.refuse(d)
	}
	return nil
}

// Check reports whether key has a token left, without taking one: for a limit that counts
// failures (Allow after each failure, Check before each attempt).
func (l *Limiter) Check(key string) error {
	now := l.clock.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.buckets[key]
	if b == nil || b.lim.TokensAt(now) >= 1 {
		return nil
	}
	r := b.lim.ReserveN(now, 1)
	d := r.DelayFrom(now)
	r.CancelAt(now)
	return l.refuse(d)
}

// evict drops buckets that have been idle long enough to be full again (forgetting them changes
// nothing). If that frees nothing, the tenth of the buckets used longest ago goes: under a flood
// of distinct keys the flood's own stale keys are forgotten, while a bucket in use (the account
// being guessed at, a client being limited) keeps its state. The map is never reset as a whole,
// which would hand every limited client a full bucket (API-1).
func (l *Limiter) evict(now time.Time) {
	for k, b := range l.buckets {
		if now.Sub(b.last) >= l.policy.Per {
			delete(l.buckets, k)
		}
	}
	if len(l.buckets) < l.maxKeys {
		return
	}
	times := make([]time.Time, 0, len(l.buckets))
	for _, b := range l.buckets {
		times = append(times, b.last)
	}
	slices.SortFunc(times, func(a, b time.Time) int { return a.Compare(b) })
	drop := max(len(times)/10, 1)
	cutoff := times[drop-1]
	for k, b := range l.buckets {
		if drop > 0 && !b.last.After(cutoff) {
			delete(l.buckets, k)
			drop--
		}
	}
}

// ClientKey is the limiter key of a client address: the address itself for IPv4, the /64 for
// IPv6. One customer gets a whole /64 (or more), so counting each of its 2^64 addresses apart
// would give every request its own bucket (API-1). Text that is not an address is its own key.
func ClientKey(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	addr = addr.Unmap()
	if addr.Is4() {
		return addr.String()
	}
	p, _ := addr.Prefix(64)
	return p.String()
}

// Len is the number of tracked keys (tests and metrics).
func (l *Limiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}
