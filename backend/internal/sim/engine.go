// Package sim is the traffic simulator's engine (PLAN Phase 25, ADR 0043): personas with a
// character and habits use a lab instance through its front door, as a browser would, all day.
// The actions live in internal/sim/actions; the statements they upload in internal/sim/statements.
package sim

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/client/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/sim/identity"
)

// Options configure an engine.
type Options struct {
	// Target is the lab's public address (https://lab.example): ingress, nginx, the API.
	Target string
	// Seed derives the personas' passwords (FINUANCE_SIM_SEED; identity.Password).
	Seed    []byte
	Version string
	Actions []Action
	Config  Config
	Clock   Clock
	// Transport carries every request (nil: http.DefaultTransport).
	Transport http.RoundTripper
	// Location is the instance's time zone, for the characters' active hours (nil: UTC).
	Location *time.Location
	Log      *slog.Logger
	// OnEvent is told about every action that ran (the control API's feed and metrics).
	OnEvent func(Event)
}

// Event is one action that ran.
type Event struct {
	Time      time.Time     `json:"time"`
	Persona   string        `json:"persona"`
	Action    string        `json:"action"`
	Area      string        `json:"area"`
	Result    string        `json:"result"` // ok, error, skipped
	Status    int           `json:"status,omitempty"`
	RequestID string        `json:"requestId,omitempty"`
	Duration  time.Duration `json:"duration"`
	Error     string        `json:"error,omitempty"`
	// RetryAfter is a 429's Retry-After (not shown).
	RetryAfter time.Duration `json:"-"`
}

// The results of an action.
const (
	ResultOK      = "ok"
	ResultError   = "error"
	ResultSkipped = "skipped"
)

// ErrNotALab is the refusal of a target whose /meta does not report simulation: true (PLAN Phase 25,
// decision 4).
var ErrNotALab = errors.New("the target is not a lab: its /meta does not report simulation: true, so the simulator stops")

// Engine runs the personas.
type Engine struct {
	o      Options
	target *url.URL
	origin string
	meta   *gen.ClientWithResponses

	mu      sync.Mutex
	cfg     Config
	gen     int64
	actors  map[string]*Actor
	running map[string]bool
	model   chan struct{} // one assistant run at a time, across every persona
	askDay  string        // the day AskedToday counts, in the instance's time zone
	asked   int
}

// New checks the options and makes an engine; Run starts it.
func New(o Options) (*Engine, error) {
	u, err := url.Parse(strings.TrimRight(o.Target, "/"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("the target must be an http or https address, not %q", o.Target)
	}
	if len(o.Seed) < identity.MinSeedBytes {
		return nil, identity.ErrShortSeed
	}
	if o.Clock == nil {
		o.Clock = SystemClock{}
	}
	if o.Transport == nil {
		o.Transport = http.DefaultTransport
	}
	if o.Location == nil {
		o.Location = time.UTC
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	e := &Engine{o: o, target: u, origin: u.Scheme + "://" + u.Host, cfg: o.Config.Clamp(), actors: map[string]*Actor{},
		running: map[string]bool{}, model: make(chan struct{}, 1)}
	e.meta, err = gen.NewClientWithResponses(e.origin+u.Path+"/api/v1", gen.WithHTTPClient(&http.Client{Transport: o.Transport, Timeout: 30 * time.Second}))
	if err != nil {
		return nil, err
	}
	return e, nil
}

// CheckTarget refuses an instance that is not a lab.
func (e *Engine) CheckTarget(ctx context.Context) error {
	r, err := e.meta.GetMetaWithResponse(ctx)
	if err != nil {
		return fmt.Errorf("reading the target's /meta: %w", err)
	}
	if r.JSON200 == nil {
		return fmt.Errorf("reading the target's /meta: HTTP %d", r.StatusCode())
	}
	if !r.JSON200.Simulation {
		return ErrNotALab
	}
	return nil
}

// Config returns the configuration in force and its generation (it grows with every change).
func (e *Engine) Config() (Config, int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cfg, e.gen
}

// SetConfig replaces the configuration, held to the ceilings, and returns it with its generation.
// A waiting persona sees the change within ConfigCheck.
func (e *Engine) SetConfig(c Config) (Config, int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cfg = c.Clamp()
	e.gen++
	return e.cfg, e.gen
}

// Run checks the target, then plays every persona the configuration switches on until ctx ends.
func (e *Engine) Run(ctx context.Context) error {
	if err := e.CheckTarget(ctx); err != nil {
		return err
	}
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		cfg, _ := e.Config()
		for i := range cfg.Personas {
			p := identity.Nth(i)
			e.mu.Lock()
			start := !e.running[p.Slug]
			e.running[p.Slug] = true
			e.mu.Unlock()
			if start {
				wg.Go(func() { e.play(ctx, p) })
			}
		}
		if err := e.o.Clock.Sleep(ctx, ConfigCheck); err != nil {
			return nil //nolint:nilerr // the end of ctx is the normal end of a run
		}
	}
}

// Actor returns the persona's actor, signed in and with its state discovered (made on first use).
func (e *Engine) Actor(ctx context.Context, p identity.Persona) (*Actor, error) {
	e.mu.Lock()
	a, ok := e.actors[p.Slug]
	if !ok {
		var err error
		if a, err = newActor(e, p); err != nil {
			e.mu.Unlock()
			return nil, err
		}
		e.actors[p.Slug] = a
	}
	e.mu.Unlock()
	if !a.signedIn() {
		if err := a.start(ctx); err != nil {
			return a, err
		}
	}
	return a, nil
}

// play is one persona's life: sign in, then think, act, think, act … while it is switched on.
func (e *Engine) play(ctx context.Context, p identity.Persona) {
	log := e.o.Log.With(slog.String("persona", p.Slug))
	failures := 0
	for ctx.Err() == nil {
		cfg, _ := e.Config()
		if !cfg.personaOn(p) || cfg.Paused {
			_ = e.o.Clock.Sleep(ctx, ConfigCheck)
			continue
		}
		a, err := e.Actor(ctx, p)
		if err != nil {
			failures++
			wait := backoff(failures)
			log.WarnContext(ctx, "sign-in failed", slog.String("error", err.Error()), slog.Duration("retry_in", wait))
			_ = e.o.Clock.Sleep(ctx, wait)
			continue
		}
		failures = 0
		if !e.wait(ctx, a) {
			continue // the configuration changed, or ctx ended: look again
		}
		cfg, _ = e.Config()
		act, ok := e.draw(a, &cfg)
		if !ok {
			continue
		}
		ev := e.Do(ctx, a, act)
		if ev.Result == ResultError {
			log.InfoContext(ctx, "action failed", slog.String("action", act.Name), slog.Int("status", ev.Status),
				slog.String("request_id", ev.RequestID), slog.String("error", ev.Error))
		}
		e.afterwards(ctx, a, ev)
	}
}

// wait is the persona's pause before its next action. It returns false when the configuration
// changed while waiting (the pause is then drawn again) or ctx ended.
func (e *Engine) wait(ctx context.Context, a *Actor) bool {
	cfg, gen := e.Config()
	now := e.o.Clock.Now()
	until := now.Add(e.pause(a, &cfg, now))
	a.mu.Lock()
	if earliest := a.lastStart.Add(MinGap); until.Before(earliest) {
		until = earliest
	}
	a.mu.Unlock()
	for {
		now = e.o.Clock.Now()
		if !now.Before(until) {
			return true
		}
		if e.o.Clock.Sleep(ctx, min(until.Sub(now), ConfigCheck)) != nil {
			return false
		}
		if _, g := e.Config(); g != gen {
			return false
		}
	}
}

// pause draws the time before the persona's next action: its character's think time, divided by
// the intensity and, with active hours on, stretched by the hour's activity. Asleep (activity 0)
// it waits for the next hour. Never shorter than MinGap.
func (e *Engine) pause(a *Actor, cfg *Config, now time.Time) time.Duration {
	if cfg.Intensity == 0 {
		return ConfigCheck
	}
	lo, hi := a.Character.Think[0], a.Character.Think[1]
	a.mu.Lock()
	d := lo + time.Duration(a.Rand.Int64N(int64(hi-lo)+1))
	a.mu.Unlock()
	d = d * 1000 / time.Duration(cfg.Intensity)
	if cfg.ActiveHours {
		local := now.In(e.o.Location)
		level := a.Character.Hours[local.Hour()]
		if level == 0 {
			return max(local.Truncate(time.Hour).Add(time.Hour).Sub(local), MinGap)
		}
		d = d * 10 / time.Duration(level)
	}
	return max(d, MinGap)
}

// draw picks the persona's next action: with probability Mistakes‰ one of the deliberate
// mistakes, otherwise a weighted draw over the actions whose area is on, whose needs are met and
// whose weight (the configuration's, else the character's) is above zero. An action that makes
// the server recompute categories is not drawn twice in a row.
func (e *Engine) draw(a *Actor, cfg *Config) (Action, bool) {
	a.mu.Lock()
	mistake := a.Rand.IntN(1000) < cfg.Mistakes
	last := a.lastAction
	a.mu.Unlock()
	var cands []Action
	var weights []int
	total := 0
	for _, act := range e.o.Actions {
		if !cfg.areaOn(act.Area) || !a.has(act.Needs) || (act.Recomputes && act.Name == last) {
			continue
		}
		w := cfg.weight(a.Persona.Slug, act.Name, a.Character.Weights[act.Name])
		if mistake {
			if !act.Mistake {
				continue
			}
			w = max(w, 1)
		}
		if w <= 0 {
			continue
		}
		cands, weights, total = append(cands, act), append(weights, w), total+w
	}
	if total == 0 {
		return Action{}, false
	}
	a.mu.Lock()
	n := a.Rand.IntN(total)
	a.mu.Unlock()
	for i, w := range weights {
		if n < w {
			return cands[i], true
		}
		n -= w
	}
	return Action{}, false
}

// Do runs one action for the persona now, with the ceilings that apply to every action (one
// assistant run at a time, the daily cap on questions), and reports it as an event.
func (e *Engine) Do(ctx context.Context, a *Actor, act Action) Event {
	ev := Event{Time: e.o.Clock.Now(), Persona: a.Persona.Slug, Action: act.Name, Area: act.Area}
	if act.Cost == Model {
		select {
		case e.model <- struct{}{}:
			defer func() { <-e.model }()
		default:
			ev.Result, ev.Error = ResultSkipped, "another persona is asking the assistant"
			e.emit(ev)
			return ev
		}
		if !e.takeQuestion() {
			ev.Result, ev.Error = ResultSkipped, "the daily cap on questions is reached"
			e.emit(ev)
			return ev
		}
	}
	a.mu.Lock()
	a.lastAction, a.lastStart = act.Name, ev.Time
	a.mu.Unlock()
	err := act.Run(ctx, a)
	ev.Duration = e.o.Clock.Now().Sub(ev.Time)
	var se *StatusError
	switch {
	case err == nil:
		ev.Result = ResultOK
	case errors.Is(err, ErrNothingToDo):
		ev.Result, ev.Error = ResultSkipped, err.Error()
	case errors.As(err, &se):
		ev.Result, ev.Status, ev.RequestID, ev.RetryAfter, ev.Error = ResultError, se.Status, se.RequestID, se.RetryAfter, err.Error()
	default:
		ev.Result, ev.Error = ResultError, err.Error()
	}
	e.emit(ev)
	return ev
}

// takeQuestion counts a question against the day's cap; false when the cap is reached.
func (e *Engine) takeQuestion() bool {
	day := e.o.Clock.Now().In(e.o.Location).Format(time.DateOnly)
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.askDay != day {
		e.askDay, e.asked = day, 0
	}
	if e.asked >= e.cfg.AssistantPerDay {
		return false
	}
	e.asked++
	return true
}

// afterwards applies the back-off after an action: a 429 waits its Retry-After (half a minute
// without one), five 5xx answers in a row wait FirstBackoff, doubling up to MaxBackoff, and a 401
// signs the persona in again.
func (e *Engine) afterwards(ctx context.Context, a *Actor, ev Event) {
	var wait time.Duration
	a.mu.Lock()
	switch {
	case ev.Status == http.StatusUnauthorized:
		a.token = ""
	case ev.Status == http.StatusTooManyRequests:
		wait = 30 * time.Second
		if ev.RetryAfter > 0 {
			wait = ev.RetryAfter
		}
	case a.serverErrors >= ServerErrorsInARow:
		a.backoffs++
		wait = backoff(a.backoffs)
		a.serverErrors = 0
	case ev.Result == ResultOK:
		a.backoffs = 0
	}
	a.mu.Unlock()
	if wait > 0 {
		e.o.Log.InfoContext(ctx, "backing off", slog.String("persona", a.Persona.Slug), slog.Duration("for", wait), slog.Int("status", ev.Status))
		_ = e.o.Clock.Sleep(ctx, wait)
	}
}

// backoff is FirstBackoff doubled for every further failure in a row, at most MaxBackoff.
func backoff(n int) time.Duration {
	d := FirstBackoff
	for i := 1; i < n && d < MaxBackoff; i++ {
		d *= 2
	}
	return min(d, MaxBackoff)
}

func (e *Engine) emit(ev Event) {
	if e.o.OnEvent != nil {
		e.o.OnEvent(ev)
	}
}
