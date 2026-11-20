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

// TestLimiterKeepsHotBucketsUnderAKeyFlood: when the limiter is full and nothing is idle, the
// buckets used longest ago go; a client that is being limited stays limited (API-1; the map used
// to be reset, handing everyone a full bucket).
func TestLimiterKeepsHotBucketsUnderAKeyFlood(t *testing.T) {
	clk := &clock{t: time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)}
	l := NewLimiter(Policy{Events: 3, Per: time.Hour}, clk, 1000)
	for range 3 {
		if err := l.Allow("target@example.com"); err != nil {
			t.Fatal(err)
		}
	}
	if err := l.Allow("target@example.com"); err == nil {
		t.Fatal("the fourth attempt was allowed")
	}
	// a flood of distinct keys, with the target still being tried now and then
	for i := range 20_000 {
		clk.t = clk.t.Add(time.Millisecond)
		_ = l.Allow(fmt.Sprintf("flood-%d", i))
		if i%500 == 0 {
			if err := l.Allow("target@example.com"); err == nil {
				t.Fatalf("after %d other keys the target's bucket was full again", i)
			}
		}
	}
	if n := l.Len(); n > 1000 {
		t.Errorf("%d keys kept, want at most 1000", n)
	}
	var ra *platform.RetryAfterError
	if err := l.Check("target@example.com"); !errors.As(err, &ra) || ra.After <= 0 {
		t.Errorf("Check on the limited key: %v", err)
	}
}

// TestLimiterCheckTakesNothing: Check reports a spent bucket without spending from it.
func TestLimiterCheckTakesNothing(t *testing.T) {
	clk := &clock{t: time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)}
	l := NewLimiter(Policy{Events: 2, Per: time.Minute}, clk, 0)
	for range 10 {
		if err := l.Check("k"); err != nil {
			t.Fatalf("Check on an unused key: %v", err)
		}
	}
	_ = l.Allow("k")
	if err := l.Check("k"); err != nil {
		t.Errorf("one token left: %v", err)
	}
	_ = l.Allow("k")
	if err := l.Check("k"); err == nil {
		t.Error("Check passed on a spent bucket")
	}
	clk.t = clk.t.Add(31 * time.Second)
	if err := l.Check("k"); err != nil {
		t.Errorf("after half the period one token is back: %v", err)
	}
}

func TestClientKey(t *testing.T) {
	for in, want := range map[string]string{
		"192.0.2.7": "192.0.2.7", "::ffff:192.0.2.7": "192.0.2.7",
		"2001:db8:1:2:aaaa:bbbb:cccc:dddd": "2001:db8:1:2::/64", "2001:db8:1:2::1": "2001:db8:1:2::/64",
		"2001:db8:1:3::1": "2001:db8:1:3::/64", "::1": "::/64", "": "", "not an address": "not an address",
	} {
		if got := ClientKey(in); got != want {
			t.Errorf("ClientKey(%q) = %q, want %q", in, got, want)
		}
	}
}
