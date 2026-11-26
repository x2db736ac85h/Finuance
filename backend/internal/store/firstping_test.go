package store

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// The first connection is tried again while the database can't be reached, and not when the
// database itself says no (PLAN 27.10).
func TestFirstPing(t *testing.T) {
	refused := &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	var slept []time.Duration
	sleep := func(d time.Duration) { slept = append(slept, d) }

	// refused twice, then there: no error, and it waited a little longer each time
	n := 0
	err := firstPing(context.Background(), func(context.Context) error {
		n++
		if n < 3 {
			return refused
		}
		return nil
	}, time.Minute, sleep)
	if err != nil || n != 3 || len(slept) != 2 || slept[1] <= slept[0] {
		t.Errorf("refused twice: err %v, %d pings, slept %v", err, n, slept)
	}

	// the database answers with an error (a wrong password): at once, without waiting
	slept, n = nil, 0
	denied := &pgconn.PgError{Code: "28P01", Message: "password authentication failed"}
	err = firstPing(context.Background(), func(context.Context) error { n++; return denied }, time.Minute, sleep)
	if !errors.Is(err, denied) || n != 1 || len(slept) != 0 {
		t.Errorf("an answer from the database: err %v, %d pings, slept %v", err, n, slept)
	}

	// never there: the last failure after the wait, not for ever
	slept, n = nil, 0
	err = firstPing(context.Background(), func(context.Context) error { n++; return refused }, 0, sleep)
	if !errors.Is(err, refused) || n != 1 || len(slept) != 0 {
		t.Errorf("no time to wait: err %v, %d pings, slept %v", err, n, slept)
	}

	// the process is asked to stop while it waits
	ctx, cancel := context.WithCancel(context.Background())
	n = 0
	err = firstPing(ctx, func(context.Context) error { n++; cancel(); return refused }, time.Minute, sleep)
	if !errors.Is(err, refused) || n != 1 {
		t.Errorf("cancelled: err %v, %d pings", err, n)
	}
}
