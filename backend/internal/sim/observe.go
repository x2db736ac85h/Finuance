package sim

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/sim/identity"
)

// What the control API shows (PLAN 25.5): each persona's state, the recent actions and a stream
// of new ones.

// recentEvents is how many actions the engine remembers for a new viewer of the feed.
const recentEvents = 200

// Counts are actions by result.
type Counts struct {
	OK, Error, Skipped int64
}

func (c *Counts) add(result string) {
	switch result {
	case ResultOK:
		c.OK++
	case ResultError:
		c.Error++
	default:
		c.Skipped++
	}
}

// PersonaState is one persona as the tuning page shows it.
type PersonaState struct {
	Persona   identity.Persona
	Character Character
	On        bool
	SignedIn  bool
	Missing   bool   // its sign-in was refused: the lab has no such animal (or another seed)
	History   string // its history profile's label ("" for the default)
	Action    string // what it is doing now
	Counts    Counts
	Last      *Event
}

type personaLog struct {
	counts  Counts
	missing bool
	last    *Event
	now     string
}

// State returns the personas the configuration covers, in order, and the totals.
func (e *Engine) State() ([]PersonaState, Counts) {
	cfg, _ := e.Config()
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []PersonaState
	var total Counts
	for i := range identity.Max {
		p := identity.Nth(i)
		lg := e.logs[p.Slug]
		if i >= cfg.Personas && lg == nil {
			continue
		}
		st := PersonaState{Persona: p, Character: Characters[p.Animal], On: cfg.personaOn(p)}
		if a := e.actors[p.Slug]; a != nil {
			st.SignedIn = a.signedIn()
			st.History = a.History().Label()
		}
		if lg != nil {
			st.Counts, st.Last, st.Action, st.Missing = lg.counts, lg.last, lg.now, lg.missing
			total.OK, total.Error, total.Skipped = total.OK+lg.counts.OK, total.Error+lg.counts.Error, total.Skipped+lg.counts.Skipped
		}
		out = append(out, st)
	}
	return out, total
}

// Recent returns the last actions, oldest first.
func (e *Engine) Recent() []Event {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.recent)
}

// Subscribe returns a channel that receives the new actions, and a function that ends the
// subscription. Above FeedRate actions a second it samples (load mode, ADR 0044), and a subscriber
// that doesn't keep up misses actions rather than slowing the engine; the state's counts stay exact.
func (e *Engine) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 64)
	e.mu.Lock()
	e.subs[ch] = &pacer{}
	e.mu.Unlock()
	return ch, func() {
		e.mu.Lock()
		delete(e.subs, ch)
		e.mu.Unlock()
	}
}

func (e *Engine) started(slug, action string) {
	e.mu.Lock()
	e.logOf(slug).now = action
	e.mu.Unlock()
}

// record keeps an action for the state, the feed and the metrics.
func (e *Engine) record(ev Event) {
	e.mu.Lock()
	lg := e.logOf(ev.Persona)
	lg.counts.add(ev.Result)
	evc := ev
	lg.last, lg.now = &evc, ""
	e.recent = append(e.recent, ev)
	if len(e.recent) > recentEvents {
		e.recent = slices.Delete(e.recent, 0, len(e.recent)-recentEvents)
	}
	for ch, p := range e.subs {
		if !p.allow(ev.Time, time.Second/FeedRate, FeedRate) {
			continue
		}
		select {
		case ch <- ev:
		default:
		}
	}
	e.mu.Unlock()
	if e.o.Metrics != nil {
		e.o.Metrics.observe(ev)
	}
}

func (e *Engine) logOf(slug string) *personaLog {
	lg := e.logs[slug]
	if lg == nil {
		lg = &personaLog{}
		e.logs[slug] = lg
	}
	return lg
}

// Errors of ActNow.
var (
	ErrUnknownPersona = errors.New("no such persona")
	ErrUnknownAction  = errors.New("no such action")
)

// ActNow makes one persona do one action at once (the tuning page's "act now"), signing it in
// first if it isn't yet.
func (e *Engine) ActNow(ctx context.Context, slug, name string) (Event, error) {
	p, ok := identity.BySlug(slug)
	if !ok {
		return Event{}, ErrUnknownPersona
	}
	i := slices.IndexFunc(e.o.Actions, func(a Action) bool { return a.Name == name })
	if i < 0 {
		return Event{}, ErrUnknownAction
	}
	a, err := e.Actor(ctx, p)
	if err != nil {
		return Event{}, err
	}
	return e.Do(ctx, a, e.o.Actions[i]), nil
}

// Actions is the catalogue the engine draws from.
func (e *Engine) Actions() []Action { return e.o.Actions }

// Defaults is the configuration the engine started with (what a reset goes back to).
func (e *Engine) Defaults() Config { return e.o.Config.Clamp() }

// Target is the lab's address.
func (e *Engine) Target() string { return e.target.String() }
