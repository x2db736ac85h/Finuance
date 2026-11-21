package sim

import (
	"context"
	"time"
)

// Clock is the engine's time: the system's in production, a fake one in tests.
type Clock interface {
	Now() time.Time
	// Sleep waits d, or less when ctx ends (then it returns ctx's error).
	Sleep(ctx context.Context, d time.Duration) error
}

// SystemClock is the real time.
type SystemClock struct{}

// Now is time.Now.
func (SystemClock) Now() time.Time { return time.Now() }

// Sleep waits d or until ctx ends.
func (SystemClock) Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
