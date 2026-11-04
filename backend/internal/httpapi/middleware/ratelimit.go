package middleware

import (
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
	LoginPerIP         = Policy{Events: 30, Per: time.Minute} // login and signup, per client IP
	LoginPerIPEmail    = Policy{Events: 10, Per: time.Minute} // login and signup, per (IP, email)
	PasswordPerUser    = Policy{Events: 10, Per: time.Minute} // password checks of a signed-in user
	UploadPerUser      = Policy{Events: 30, Per: time.Minute} // uploads (Phase 6)
	DemoPerIP          = Policy{Events: 5, Per: time.Hour}    // new demo users, per client IP (PLAN 19.1)
	FriendCodesPerHour = Policy{Events: 10, Per: time.Hour}   // friend codes entered, per user and per IP (ADR 0027)
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
		return &platform.RetryAfterError{After: d}
	}
	return nil
}

// evict drops buckets that have been idle long enough to be full again (forgetting them changes
// nothing). If that frees nothing, the map is reset: under a flood of distinct keys, starting the
// legitimate clients over with full buckets is better than refusing them.
func (l *Limiter) evict(now time.Time) {
	for k, b := range l.buckets {
		if now.Sub(b.last) >= l.policy.Per {
			delete(l.buckets, k)
		}
	}
	if len(l.buckets) >= l.maxKeys {
		clear(l.buckets)
	}
}

// Len is the number of tracked keys (tests and metrics).
func (l *Limiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}
