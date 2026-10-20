package platform

import (
	"time"

	"github.com/google/uuid"
)

// Clock is the only source of "now" in domain code, so tests stay deterministic (PLAN §3.4).
type Clock interface {
	Now() time.Time
}

// SystemClock reads the wall clock.
type SystemClock struct{}

// Now returns time.Now().
func (SystemClock) Now() time.Time { return time.Now() }

// FixedClock always returns T.
type FixedClock struct{ T time.Time }

// Now returns the fixed time.
func (c FixedClock) Now() time.Time { return c.T }

// NewID returns a time-ordered UUIDv7 (index-friendly primary keys).
func NewID() uuid.UUID { return uuid.Must(uuid.NewV7()) }
