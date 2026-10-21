package middleware

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

type clock struct{ t time.Time }

func (c *clock) Now() time.Time { return c.t }

func TestLimiterBucket(t *testing.T) {
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	l := NewLimiter(Policy{Events: 3, Per: time.Minute}, c, 0)
	for i := range 3 {
		if err := l.Allow("k"); err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
	}
	err := l.Allow("k")
	var ra *platform.RetryAfterError
	if !errors.As(err, &ra) || !errors.Is(err, platform.ErrRateLimited) || ra.Seconds() != 20 {
		t.Fatalf("4th request: %v (want retry after 20s)", err)
	}
	// Rejections don't consume tokens: after 20 s exactly one more request passes.
	for range 5 {
		_ = l.Allow("k")
	}
	c.t = c.t.Add(20 * time.Second)
	if err := l.Allow("k"); err != nil {
		t.Errorf("after the refill: %v", err)
	}
	if err := l.Allow("k"); err == nil {
		t.Error("two requests passed after one refill")
	}
	// Keys are independent.
	if err := l.Allow("other"); err != nil {
		t.Errorf("other key: %v", err)
	}
}

func TestLimiterEviction(t *testing.T) {
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	l := NewLimiter(Policy{Events: 1, Per: time.Minute}, c, 100)
	for i := range 100 {
		_ = l.Allow(fmt.Sprint(i))
	}
	if l.Len() != 100 {
		t.Fatalf("len = %d", l.Len())
	}
	// Idle buckets are full again, so the 101st key evicts them instead of growing the map.
	c.t = c.t.Add(time.Minute)
	_ = l.Allow("new")
	if l.Len() != 1 {
		t.Errorf("after eviction len = %d, want 1", l.Len())
	}
	// A flood of fresh keys never exceeds the cap.
	for i := range 1000 {
		_ = l.Allow(fmt.Sprint("flood", i))
	}
	if l.Len() > 100 {
		t.Errorf("len = %d exceeds the cap", l.Len())
	}
}

func TestRetryAfterSeconds(t *testing.T) {
	for d, want := range map[time.Duration]int{0: 1, time.Millisecond: 1, time.Second: 1, 1500 * time.Millisecond: 2, 6 * time.Second: 6} {
		if got := (&platform.RetryAfterError{After: d}).Seconds(); got != want {
			t.Errorf("Seconds(%v) = %d, want %d", d, got, want)
		}
	}
}
