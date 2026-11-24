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
	"slices"
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
	// Origin is the Origin the personas send (the lab's FINUANCE_PUBLIC_ORIGIN); empty: the
	// target's own. It differs where the simulator reaches the front door by another name, as on
	// Compose (http://web:8080 for http://localhost:8020).
	Origin string
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
	// OnEvent is told about every action that ran.
	OnEvent func(Event)
	// Metrics, when given, count the actions and show the configuration.
	Metrics *Metrics
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
	board   map[string]note // what one persona leaves for another (a friend code), until it runs out
	every   map[string]time.Time
	logs    map[string]*personaLog
	recent  []Event
	subs    map[chan Event]*pacer // each viewer's feed, sampled at FeedRate

	// load mode (ADR 0044): the lab-wide schedule, when load mode began, and the ramp towards the
	// current target from the rate at the last change
	load         pacer
	loadSince    time.Time
	rampFrom     time.Time
	rampFromRate float64
	signIns      pacer // every persona's sign-in, paced below the instance's limit per address

	// history rebuilds: the generation asked for, the action that does it and the turns at once
	rebuildGen int64
	rebuildAct *Action
	rebuilds   chan struct{}
}

// Every reports whether at least d has passed since it last said yes for this key, across every
// persona, and if so counts this time: for what the instance limits per address, which all
// personas share behind the front door (ten friend codes entered an hour).
func (e *Engine) Every(key string, d time.Duration) bool {
	now := e.o.Clock.Now()
	e.mu.Lock()
	defer e.mu.Unlock()
	if last, ok := e.every[key]; ok && now.Sub(last) < d {
		return false
	}
	e.every[key] = now
	return true
}

// note is what Post leaves on the board: a value, and the moment it is no use any more (zero:
// never).
type note struct {
	value string
	until time.Time
}

// NoteMargin is how long before its end a note is dropped: the time between taking a friend code
// and redeeming it, and any difference between the simulator's clock and the lab's.
const NoteMargin = time.Minute

// Post leaves a note for other personas under key (a friend code under "friend-code/<slug>") until
// the moment it runs out (a code's expiry; zero: never). A note under the same key replaces it.
func (e *Engine) Post(key, value string, until time.Time) {
	e.mu.Lock()
	e.board[key] = note{value: value, until: until}
	e.mu.Unlock()
}

// Take removes and returns the first note whose key starts with prefix and for which keep says
// yes, in key order. Notes within NoteMargin of their end are dropped on the way: the lab refuses
// an expired friend code (422), and that would be the simulator's mistake, not the lab's.
func (e *Engine) Take(prefix string, keep func(key string) bool) (string, string, bool) {
	now := e.o.Clock.Now()
	e.mu.Lock()
	defer e.mu.Unlock()
	keys := make([]string, 0, len(e.board))
	for k, n := range e.board {
		if !n.until.IsZero() && !now.Add(NoteMargin).Before(n.until) {
			delete(e.board, k)
			continue
		}
		if strings.HasPrefix(k, prefix) && keep(k) {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return "", "", false
	}
	slices.Sort(keys)
	n := e.board[keys[0]]
	delete(e.board, keys[0])
	return keys[0], n.value, true
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
	origin := u.Scheme + "://" + u.Host
	if o.Origin != "" {
		origin = strings.TrimRight(o.Origin, "/")
	}
	e := &Engine{o: o, target: u, origin: origin, cfg: withRunSeed(o.Config.Clamp()), actors: map[string]*Actor{},
		running: map[string]bool{}, model: make(chan struct{}, 1), board: map[string]note{}, every: map[string]time.Time{},
		logs: map[string]*personaLog{}, subs: map[chan Event]*pacer{}, rebuilds: make(chan struct{}, MaxRebuilds)}
	for i := range o.Actions {
		if o.Actions[i].Name == RebuildAction {
			e.rebuildAct = &o.Actions[i]
		}
	}
	if e.cfg.Mode == ModeLoad {
		e.loadSince, e.rampFrom = o.Clock.Now(), o.Clock.Now()
	}
	if o.Metrics != nil {
		o.Metrics.config(&e.cfg, 0)
	}
	e.meta, err = gen.NewClientWithResponses(u.Scheme+"://"+u.Host+u.Path+"/api/v1", gen.WithHTTPClient(&http.Client{Transport: o.Transport, Timeout: 30 * time.Second}))
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
// A waiting persona sees the change within ConfigCheck. Entering load mode starts its clock and a
// ramp from nothing; a new target or ramp in load mode ramps from the rate reached so far. A new
// run seed re-seeds every persona's stream.
func (e *Engine) SetConfig(c Config) (Config, int64) {
	now := e.o.Clock.Now()
	e.mu.Lock()
	old := e.cfg
	c = withRunSeed(c.Clamp())
	switch {
	case c.Mode == ModeLoad && old.Mode != ModeLoad:
		e.loadSince, e.rampFrom, e.rampFromRate = now, now, 0
	case c.Mode == ModeLoad && (c.Rate != old.Rate || c.RampSeconds != old.RampSeconds):
		e.rampFromRate, e.rampFrom = e.rateLocked(&old, now), now
	}
	e.load = pacer{}
	if c.RunSeed != old.RunSeed {
		for _, a := range e.actors {
			a.mu.Lock()
			a.Rand = streamOf(a.Persona.Slug, c.RunSeed)
			a.mu.Unlock()
		}
	}
	e.cfg = c
	e.gen++
	cfg, gen := e.cfg, e.gen
	e.mu.Unlock()
	if e.o.Metrics != nil {
		e.o.Metrics.config(&cfg, gen)
	}
	return cfg, gen
}

// Run checks the target, then plays every persona the configuration switches on until ctx ends.
func (e *Engine) Run(ctx context.Context) error {
	if err := e.CheckTarget(ctx); err != nil {
		return err
	}
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		cfg := e.endLoadIfDue(ctx)
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
		if err := e.SignInTurn(ctx); err != nil {
			return a, err
		}
		if err := a.start(ctx); err != nil {
			return a, err
		}
	}
	return a, nil
}

// RebuildAction is the on-demand action that rebuilds a persona's history (internal/sim/actions).
const RebuildAction = "history.rebuild"

// RequestRebuild asks every persona that plays to rebuild its history with the configured profile
// at its next turn, MaxRebuilds at a time; it returns the request's generation.
func (e *Engine) RequestRebuild() int64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rebuildGen++
	return e.rebuildGen
}

// Rebuilds is the last rebuild asked for and how many personas that play have yet to do it.
func (e *Engine) Rebuilds() (generation int64, pending int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range e.cfg.Personas {
		p := identity.Nth(i)
		if !e.cfg.personaOn(p) {
			continue
		}
		if a := e.actors[p.Slug]; a == nil || a.rebuiltGen() < e.rebuildGen {
			pending++
		}
	}
	return e.rebuildGen, pending
}

// rebuildTurn reports whether the persona rebuilds its history now: a rebuild it hasn't done is
// asked for and fewer than MaxRebuilds are running. The caller releases the turn (<-e.rebuilds).
func (e *Engine) rebuildTurn(a *Actor) (Action, bool) {
	e.mu.Lock()
	gen, act := e.rebuildGen, e.rebuildAct
	e.mu.Unlock()
	if act == nil || a.rebuiltGen() >= gen {
		return Action{}, false
	}
	select {
	case e.rebuilds <- struct{}{}:
	default:
		return Action{}, false
	}
	a.mu.Lock()
	a.rebuilt = gen // once per request, also when it fails (the feed says so)
	a.mu.Unlock()
	return *act, true
}

// waitForChange marks the persona missing until the configuration changes (or ctx ends).
func (e *Engine) waitForChange(ctx context.Context, slug string) {
	_, gen := e.Config()
	e.mu.Lock()
	e.logOf(slug).missing = true
	e.mu.Unlock()
	for ctx.Err() == nil {
		if _, g := e.Config(); g != gen {
			break
		}
		_ = e.o.Clock.Sleep(ctx, ConfigCheck)
	}
	e.mu.Lock()
	e.logOf(slug).missing = false
	e.mu.Unlock()
}

// SignInTurn waits for a turn to sign in: every persona comes through the same front door, and the
// instance allows an address 30 sign-ins a minute (ADR 0044). A mistyped password takes one too.
func (e *Engine) SignInTurn(ctx context.Context) error {
	now := e.o.Clock.Now()
	e.mu.Lock()
	t := e.signIns.reserve(now, SignInEvery, SignInEvery, SignInBurst)
	e.mu.Unlock()
	if d := t.Sub(now); d > 0 {
		return e.o.Clock.Sleep(ctx, d)
	}
	return nil
}

// endLoadIfDue returns load mode to realistic mode once its time is up (the other settings stay,
// the personas above realistic mode's ceiling stop) and returns the configuration in force.
func (e *Engine) endLoadIfDue(ctx context.Context) Config {
	cfg, _ := e.Config()
	if cfg.Mode == ModeLoad && !e.o.Clock.Now().Before(e.LoadEndsAt()) {
		cfg.Mode = ModeRealistic
		cfg, _ = e.SetConfig(cfg)
		e.o.Log.InfoContext(ctx, "load mode ended", slog.Int("personas", cfg.Personas))
	}
	return cfg
}

// LoadEndsAt is when load mode returns to realistic mode by itself (zero outside load mode).
func (e *Engine) LoadEndsAt() time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cfg.Mode != ModeLoad {
		return time.Time{}
	}
	return e.loadSince.Add(time.Duration(e.cfg.LoadMinutes) * time.Minute)
}

// Rate is load mode's lab-wide target now, in actions a second, on its ramp (0 outside load mode).
func (e *Engine) Rate() float64 {
	now := e.o.Clock.Now()
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cfg.Mode != ModeLoad {
		return 0
	}
	return e.rateLocked(&e.cfg, now)
}

// rateLocked is the target at now on the ramp from rampFromRate to cfg.Rate (the caller holds e.mu);
// never below one action a second.
func (e *Engine) rateLocked(cfg *Config, now time.Time) float64 {
	target := float64(cfg.Rate)
	if ramp := time.Duration(cfg.RampSeconds) * time.Second; ramp > 0 {
		if done := float64(now.Sub(e.rampFrom)) / float64(ramp); done < 1 {
			target = e.rampFromRate + (target-e.rampFromRate)*max(done, 0)
		}
	}
	return max(target, 1)
}

// loadTurn reserves the persona's next turn on load mode's lab-wide schedule: one turn every
// 1/rate on average, each interval stretched or shrunk by Jitter, and as many at once after a
// quiet spell as Bursts allows (at most one second's worth above the target).
func (e *Engine) loadTurn(a *Actor, cfg *Config, now time.Time) time.Time {
	a.mu.Lock()
	u := a.Rand.Float64()
	a.mu.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	rate := e.rateLocked(cfg, now)
	interval := time.Duration(float64(time.Second) / rate)
	step := time.Duration(float64(interval) * (1 + float64(cfg.Jitter)/100*(2*u-1)))
	return e.load.reserve(now, interval, step, 1+rate*float64(cfg.Bursts)/1000)
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
		if se := (*StatusError)(nil); errors.As(err, &se) && se.Op == "sign in" && se.Status == http.StatusUnauthorized {
			// not in the lab (more personas than synthetic-users made, or another seed): it waits
			// for the next change of configuration instead of taking the others' sign-in turns
			log.InfoContext(ctx, "not in the lab", slog.String("request_id", se.RequestID))
			e.waitForChange(ctx, p.Slug)
			continue
		}
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
		act, rebuild := e.rebuildTurn(a)
		ok := rebuild
		if !rebuild {
			act, ok = e.draw(a, &cfg)
		}
		if !ok {
			continue
		}
		ev := e.Do(ctx, a, act)
		if rebuild {
			<-e.rebuilds
		}
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
	var until time.Time
	gap := MinGap
	if cfg.Mode == ModeLoad {
		until, gap = e.loadTurn(a, &cfg, now), MinLoadGap
	} else {
		until = now.Add(e.pause(a, &cfg, now))
	}
	a.mu.Lock()
	if earliest := a.lastStart.Add(gap); until.Before(earliest) {
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

// pause draws the time before the persona's next action in realistic mode: its character's think
// time, spread around its mean by Jitter, divided by the intensity and, with active hours on,
// stretched by the hour's activity. Asleep (activity 0) it waits for the next hour. A burst (the
// chance is Bursts) makes the next few actions come at MinGap. Never shorter than MinGap.
func (e *Engine) pause(a *Actor, cfg *Config, now time.Time) time.Duration {
	if cfg.Intensity == 0 {
		return ConfigCheck
	}
	lo, hi := a.Character.Think[0], a.Character.Think[1]
	spread := time.Duration(float64(hi-lo) * float64(cfg.Jitter) / 100)
	a.mu.Lock()
	if a.burst > 0 {
		a.burst--
		a.mu.Unlock()
		return MinGap
	}
	if cfg.Bursts > 0 && a.Rand.IntN(1000) < cfg.Bursts {
		a.burst = 2 + a.Rand.IntN(6)
		a.mu.Unlock()
		return MinGap
	}
	d := (lo+hi)/2 - spread/2 + time.Duration(a.Rand.Int64N(int64(spread)+1))
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
		if act.OnDemand || !cfg.areaOn(act.Area) || !a.has(act.Needs) || (act.Recomputes && act.Name == last) {
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
	a.lastAction, a.lastStart, a.lastRequestID = act.Name, ev.Time, ""
	a.mu.Unlock()
	e.started(a.Persona.Slug, act.Name)
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
	if ev.RequestID == "" {
		a.mu.Lock()
		ev.RequestID = a.lastRequestID
		a.mu.Unlock()
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
// without one), five 5xx answers in a row wait FirstBackoff, doubling up to MaxBackoff (unless load
// mode pushes through), and a 401 signs the persona in again.
func (e *Engine) afterwards(ctx context.Context, a *Actor, ev Event) {
	var wait time.Duration
	cfg, _ := e.Config()
	a.mu.Lock()
	switch {
	case ev.Status == http.StatusUnauthorized:
		a.token = ""
	case ev.Status == http.StatusTooManyRequests:
		wait = 30 * time.Second
		if ev.RetryAfter > 0 {
			wait = ev.RetryAfter
		}
	case a.serverErrors >= ServerErrorsInARow && cfg.Mode == ModeLoad && cfg.PushThrough:
		a.serverErrors = 0
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
	e.record(ev)
	if e.o.OnEvent != nil {
		e.o.OnEvent(ev)
	}
}
