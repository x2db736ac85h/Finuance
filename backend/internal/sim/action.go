package sim

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"time"
)

// Cost is an action's cost class (PLAN Phase 25, "The action catalogue").
type Cost int

// The cost classes.
const (
	Light Cost = iota // a read or a small write
	Heavy             // an import, an export, a rule run
	Model             // a question to the assistant: one at a time across every persona
)

// Need is the state an action needs before it can be drawn.
type Need int

// The needs (a bit set).
const (
	NeedAccounts Need = 1 << iota // the persona's two accounts exist
	NeedData                      // the persona has transactions
)

// Action is one thing a persona can do. Actions live in internal/sim/actions, one file per area.
type Action struct {
	Name  string // "area.what", the name in weights, metrics and the feed
	Area  string // one of Areas
	Cost  Cost
	Needs Need
	// Recomputes: the action makes the server recompute categories (a rule run, a reclassification);
	// such actions are paced by the persona's think time like any other and never run twice in a row.
	Recomputes bool
	// Mistake: the action is a deliberate mistake (a 4xx); its answer is the expected one.
	Mistake bool
	Run     func(ctx context.Context, a *Actor) error
}

// StatusError is an answer the action did not expect.
type StatusError struct {
	Op         string
	Status     int
	RequestID  string
	RetryAfter time.Duration // from a 429's Retry-After
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("%s: HTTP %d (request %s)", e.Op, e.Status, e.RequestID)
}

// Expect checks an answer: nil when its status is one of want, a *StatusError otherwise. op names
// the call in the error.
func Expect(op string, r *http.Response, want ...int) error {
	if r == nil {
		return fmt.Errorf("%s: no answer", op)
	}
	if slices.Contains(want, r.StatusCode) {
		return nil
	}
	e := &StatusError{Op: op, Status: r.StatusCode, RequestID: r.Header.Get("X-Request-ID")}
	if s, err := strconv.Atoi(r.Header.Get("Retry-After")); err == nil && s > 0 {
		e.RetryAfter = min(time.Duration(s)*time.Second, MaxRetryAfter)
	}
	return e
}

// ErrNothingToDo is returned by an action that found nothing to act on this time (not a failure).
var ErrNothingToDo = errors.New("nothing to do")
