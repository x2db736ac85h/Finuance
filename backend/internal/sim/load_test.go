package sim

import (
	"context"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/x2db736ac85h/finuance/backend/internal/sim/identity"
)

// TestLoadModeCeilings: whatever the page sends, each mode holds the configuration to its own
// ceilings (ADR 0044), and realistic mode keeps ADR 0043's.
func TestLoadModeCeilings(t *testing.T) {
	load := Config{Mode: ModeLoad, Personas: 9999, Rate: 99999, RampSeconds: -1, LoadMinutes: 0, Jitter: 300, Bursts: 5000,
		RunSeed: -5, PushThrough: true}.Clamp()
	if load.Personas != MaxLoadPersonas || load.Rate != MaxRate || load.RampSeconds != 0 || load.LoadMinutes != 1 ||
		load.Jitter != 100 || load.Bursts != 1000 || load.RunSeed != 0 || !load.PushThrough {
		t.Errorf("load mode: %+v", load)
	}
	over := Config{Mode: ModeLoad, RampSeconds: 99999, LoadMinutes: 99999, RunSeed: MaxRunSeed + 1}.Clamp()
	if over.RampSeconds != MaxRampSeconds || over.LoadMinutes != MaxLoadMinutes || over.RunSeed != 0 || over.Rate != 1 {
		t.Errorf("load mode, too long: %+v", over)
	}
	for _, mode := range []string{ModeRealistic, "", "turbo"} {
		c := Config{Mode: mode, Personas: 9999, PushThrough: true}.Clamp()
		if c.Mode != ModeRealistic || c.Personas != MaxPersonas || c.PushThrough {
			t.Errorf("mode %q: %+v", mode, c)
		}
	}
	if MaxLoadPersonas != identity.Max || time.Second/MinLoadGap != 2 {
		t.Errorf("load mode's ceilings: %d personas, %v between two actions", MaxLoadPersonas, MinLoadGap)
	}
}

func loadEngine(t *testing.T, c Config) (*Engine, *fakeClock, *Actor) {
	t.Helper()
	e, clock := newTestEngine(t, &fakeLab{simulation: true}, nil)
	c.Mode = ModeLoad
	e.SetConfig(c)
	return e, clock, testActor(e, Character{})
}

// TestLoadPacer: load mode gives the lab one schedule at the target rate, ramps to a new target
// from the rate reached, lets a quiet pacer through at once only as far as Bursts allows, keeps the
// average rate under Jitter, and keeps one persona to MinLoadGap.
func TestLoadPacer(t *testing.T) {
	t.Run("one schedule at the target", func(t *testing.T) {
		e, clock, a := loadEngine(t, Config{Rate: 10})
		now := clock.Now()
		for i := range 5 {
			if got, want := e.loadTurn(a, &e.cfg, now), now.Add(time.Duration(i)*100*time.Millisecond); !got.Equal(want) {
				t.Fatalf("turn %d at %v, want %v", i, got.Sub(now), want.Sub(now))
			}
		}
	})
	t.Run("bursts let a quiet pacer through at once", func(t *testing.T) {
		e, clock, a := loadEngine(t, Config{Rate: 10, Bursts: 1000})
		now := clock.Now()
		at := 0
		for e.loadTurn(a, &e.cfg, now).Equal(now) {
			at++
		}
		if at != 11 { // 1 + one second's worth at 10 a second
			t.Errorf("%d turns at once, want 11", at)
		}
	})
	t.Run("jitter keeps the average", func(t *testing.T) {
		e, clock, a := loadEngine(t, Config{Rate: 100, Jitter: 100})
		now := clock.Now()
		var last time.Time
		for range 2000 {
			last = e.loadTurn(a, &e.cfg, now)
		}
		if got := last.Sub(now).Seconds(); got < 19 || got > 21 {
			t.Errorf("2,000 turns at 100 a second with jitter took %.1f s, want about 20", got)
		}
	})
	t.Run("a ramp from the rate reached", func(t *testing.T) {
		e, clock, _ := loadEngine(t, Config{Rate: 100, RampSeconds: 100})
		start := clock.Now()
		for _, c := range []struct {
			after time.Duration
			want  float64
		}{{0, 1}, {50 * time.Second, 50}, {100 * time.Second, 100}, {time.Hour, 100}} {
			clock.now = start.Add(c.after)
			if got := e.Rate(); got != c.want {
				t.Errorf("%v into the ramp: %v a second, want %v", c.after, got, c.want)
			}
		}
		clock.now = start.Add(time.Hour)
		cfg, _ := e.Config()
		cfg.Rate = 200
		e.SetConfig(cfg)
		clock.now = clock.now.Add(50 * time.Second)
		if got := e.Rate(); got != 150 {
			t.Errorf("half-way from 100 to 200: %v", got)
		}
	})
	t.Run("one persona waits MinLoadGap", func(t *testing.T) {
		e, clock, a := loadEngine(t, Config{Rate: MaxRate, Bursts: 1000})
		a.lastStart = clock.Now()
		before := clock.Now()
		if !e.wait(context.Background(), a) {
			t.Fatal("the wait was interrupted")
		}
		if got := clock.Now().Sub(before); got != MinLoadGap {
			t.Errorf("waited %v, want %v", got, MinLoadGap)
		}
	})
}

// TestLoadModeEnds: load mode returns to realistic mode by itself after LoadMinutes, keeping the
// other settings and holding the personas to realistic mode's ceiling.
func TestLoadModeEnds(t *testing.T) {
	e, clock, _ := loadEngine(t, Config{Rate: 20, LoadMinutes: 5, Personas: 200})
	ends := e.LoadEndsAt()
	if got := ends.Sub(clock.Now()); got != 5*time.Minute {
		t.Fatalf("load mode ends in %v", got)
	}
	clock.now = ends.Add(-time.Second)
	if c := e.endLoadIfDue(context.Background()); c.Mode != ModeLoad {
		t.Fatal("load mode ended early")
	}
	clock.now = ends
	c := e.endLoadIfDue(context.Background())
	if c.Mode != ModeRealistic || c.Personas != MaxPersonas || c.Rate != 20 || !e.LoadEndsAt().IsZero() || e.Rate() != 0 {
		t.Errorf("after load mode: %+v, ends %v, rate %v", c, e.LoadEndsAt(), e.Rate())
	}
}

// TestPushThrough: in load mode with PushThrough, five 5xx in a row don't send a persona into a
// back-off; realistic mode ignores the setting.
func TestPushThrough(t *testing.T) {
	for _, c := range []struct {
		cfg   Config
		sleep bool
	}{
		{Config{Mode: ModeLoad, PushThrough: true}, false},
		{Config{Mode: ModeLoad}, true},
		{Config{Mode: ModeRealistic, PushThrough: true}, true},
	} {
		e, clock := newTestEngine(t, &fakeLab{simulation: true}, nil)
		e.SetConfig(c.cfg)
		a := testActor(e, Character{})
		a.serverErrors = ServerErrorsInARow
		e.afterwards(context.Background(), a, Event{Result: ResultError, Status: http.StatusServiceUnavailable})
		if slept := len(clock.sleeps()) > 0; slept != c.sleep || a.serverErrors != 0 {
			t.Errorf("%+v: slept %v, %d errors left", c.cfg, slept, a.serverErrors)
		}
	}
}

// TestSignInsArePaced: every persona comes through one front door, so sign-ins are paced below the
// instance's limit per address: SignInBurst at once, then one every SignInEvery.
func TestSignInsArePaced(t *testing.T) {
	e, clock := newTestEngine(t, &fakeLab{simulation: true}, nil)
	for range SignInBurst + 3 {
		if err := e.SignInTurn(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := clock.sleeps(), []time.Duration{SignInEvery, SignInEvery, SignInEvery}; !slices.Equal(got, want) {
		t.Errorf("slept %v, want %v", got, want)
	}
}

// TestMissingPersonaWaits: a persona whose sign-in is refused (the lab made fewer animals) is shown
// missing and waits for the next change instead of trying again.
func TestMissingPersonaWaits(t *testing.T) {
	lab := &fakeLab{simulation: true, refuse: map[string]bool{identity.Nth(0).Email: true}}
	e, clock := newTestEngine(t, lab, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	changes := 0
	clock.onSleep = func() {
		if st, _ := e.State(); len(st) > 0 && st[0].Missing && changes == 0 {
			changes++
			lab.mu.Lock()
			lab.refuse = nil
			lab.mu.Unlock()
			cfg, _ := e.Config()
			e.SetConfig(cfg) // any change: the persona tries again
		}
		if e.actors["otter"] != nil && e.actors["otter"].signedIn() {
			cancel()
		}
	}
	e.play(ctx, identity.Nth(0))
	lab.mu.Lock()
	defer lab.mu.Unlock()
	if changes != 1 || len(lab.logins) != 2 {
		t.Errorf("%d changes, sign-ins %v", changes, lab.logins)
	}
	if st, _ := e.State(); st[0].Missing {
		t.Error("still missing after it signed in")
	}
}

// TestRunSeedRepeats: the same run seed gives every persona the same stream, another seed another
// one, and a new seed re-seeds the personas already playing.
func TestRunSeedRepeats(t *testing.T) {
	draws := func(r interface{ IntN(int) int }) []int {
		out := make([]int, 20)
		for i := range out {
			out[i] = r.IntN(1000)
		}
		return out
	}
	if !slices.Equal(draws(streamOf("otter", 7)), draws(streamOf("otter", 7))) {
		t.Error("the same seed gave two streams")
	}
	if slices.Equal(draws(streamOf("otter", 7)), draws(streamOf("otter", 8))) ||
		slices.Equal(draws(streamOf("otter", 7)), draws(streamOf("owl", 7))) {
		t.Error("another seed or persona gave the same stream")
	}
	e, _ := newTestEngine(t, &fakeLab{simulation: true}, nil)
	if c, _ := e.Config(); c.RunSeed < 1 || c.RunSeed > MaxRunSeed {
		t.Errorf("no run seed drawn: %d", c.RunSeed)
	}
	a, err := e.Actor(context.Background(), identity.Nth(0))
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := e.Config()
	cfg.RunSeed = 42
	e.SetConfig(cfg)
	if !slices.Equal(draws(a.Rand), draws(streamOf("otter", 42))) {
		t.Error("a new run seed did not re-seed the persona")
	}
}

// TestJitter: jitter spreads a think time around its mean: 0 always the mean, 100 the character's
// whole range (today's draw), 50 half of it.
func TestJitter(t *testing.T) {
	e, _ := newTestEngine(t, &fakeLab{simulation: true}, nil)
	a := testActor(e, Character{Think: [2]time.Duration{10 * time.Second, 30 * time.Second}})
	noon := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		jitter int
		lo, hi time.Duration
	}{{0, 20 * time.Second, 20 * time.Second}, {50, 15 * time.Second, 25 * time.Second}, {100, 10 * time.Second, 30 * time.Second}} {
		lo, hi := time.Hour, time.Duration(0)
		for range 2000 {
			d := e.pause(a, &Config{Intensity: 1000, Jitter: c.jitter}, noon)
			lo, hi = min(lo, d), max(hi, d)
		}
		if lo < c.lo || hi > c.hi || hi-lo < (c.hi-c.lo)*9/10 {
			t.Errorf("jitter %d: pauses %v … %v, want %v … %v", c.jitter, lo, hi, c.lo, c.hi)
		}
	}
}

// TestBursts: a burst makes the next few actions come at MinGap; without bursts none does.
func TestBursts(t *testing.T) {
	e, _ := newTestEngine(t, &fakeLab{simulation: true}, nil)
	a := testActor(e, Character{Think: [2]time.Duration{time.Minute, time.Minute}})
	noon := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	quick := 0
	for range 1000 {
		if e.pause(a, &Config{Intensity: 1000, Jitter: 100}, noon) == MinGap {
			quick++
		}
	}
	if quick != 0 {
		t.Errorf("%d quick actions without bursts", quick)
	}
	if d := e.pause(a, &Config{Intensity: 1000, Bursts: 1000}, noon); d != MinGap || a.burst < 2 || a.burst > 7 {
		t.Fatalf("a burst: %v, %d to come", d, a.burst)
	}
	for n := a.burst; n > 0; n-- {
		if d := e.pause(a, &Config{Intensity: 1000}, noon); d != MinGap {
			t.Fatalf("%d left in the burst: %v", n, d)
		}
	}
	if d := e.pause(a, &Config{Intensity: 1000}, noon); d != time.Minute {
		t.Errorf("after the burst: %v", d)
	}
}

// TestMetricsByAnimal: the simulator's action counter is labelled by animal, so 500 personas make
// at most ten label values.
func TestMetricsByAnimal(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	for _, slug := range []string{"otter", "otter-2", "otter-50"} {
		m.observe(Event{Persona: slug, Action: "table.window", Result: ResultOK})
	}
	if got := testutil.ToFloat64(m.actions.WithLabelValues("otter", "table.window", ResultOK)); got != 3 {
		t.Errorf("otter counted %v", got)
	}
	if n := testutil.CollectAndCount(m.actions); n != 1 {
		t.Errorf("%d series, want 1", n)
	}
}

// TestFeedIsSampled: a viewer of the feed gets at most FeedRate actions a second; the state counts
// every one.
func TestFeedIsSampled(t *testing.T) {
	e, clock := newTestEngine(t, &fakeLab{simulation: true}, nil)
	ch, stop := e.Subscribe()
	defer stop()
	got := 0
	for second := range 2 {
		at := clock.Now().Add(time.Duration(second) * time.Second)
		for range 200 {
			e.record(Event{Time: at, Persona: "otter", Action: "table.window", Result: ResultOK})
		}
		for len(ch) > 0 {
			<-ch
			got++
		}
	}
	if got != 2*FeedRate {
		t.Errorf("the viewer got %d actions in two seconds, want %d", got, 2*FeedRate)
	}
	if _, totals := e.State(); totals.OK != 400 {
		t.Errorf("the state counted %d", totals.OK)
	}
}

// TestLimits: a persona keeps below the instance's limits per user as a token bucket does: a burst
// at once, then one turn every interval; Allow skips when none is free, Pace waits for one. Each
// default stays below the instance's (30 at once, 30 an hour or a minute).
func TestLimits(t *testing.T) {
	e, clock := newTestEngine(t, &fakeLab{simulation: true}, nil)
	a := testActor(e, Character{})
	l := Limit{"test", time.Minute, 3}
	for i := range 3 {
		if !a.Allow(l) {
			t.Fatalf("turn %d of the burst refused", i)
		}
	}
	if a.Allow(l) {
		t.Fatal("a fourth turn at once")
	}
	clock.now = clock.now.Add(time.Minute)
	if !a.Allow(l) || a.Allow(l) {
		t.Error("not one turn a minute later")
	}
	u := Limit{"test-upload", 2 * time.Second, 2}
	for range 4 {
		if err := a.Pace(context.Background(), u); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := clock.sleeps(), []time.Duration{2 * time.Second, 2 * time.Second}; !slices.Equal(got, want) {
		t.Errorf("slept %v, want %v", got, want)
	}
	for _, d := range []struct {
		l      Limit
		per    time.Duration
		events int
	}{{Questions, time.Hour, 30}, {Uploads, time.Minute, 30}, {Invites, time.Hour, 30}} {
		if d.l.Burst >= d.events || d.l.Every < d.per/time.Duration(d.events) {
			t.Errorf("%s: %v with a burst of %d passes the instance's %d in %v", d.l.Key, d.l.Every, d.l.Burst, d.events, d.per)
		}
	}
}

// TestRebuildRequests: a rebuild request reaches every persona that plays once, at most
// MaxRebuilds at a time, and the state counts those still to do it.
func TestRebuildRequests(t *testing.T) {
	ran := 0
	acts := []Action{{Name: RebuildAction, Area: "imports", OnDemand: true, Run: func(context.Context, *Actor) error { ran++; return nil }}}
	e, _ := newTestEngine(t, &fakeLab{simulation: true}, acts)
	cfg, _ := e.Config()
	cfg.Mode, cfg.Personas = ModeLoad, MaxRebuilds+2
	e.SetConfig(cfg)
	var actors []*Actor
	for i := range cfg.Personas {
		a := testActor(e, Character{})
		a.Persona = identity.Nth(i)
		e.actors[a.Persona.Slug] = a
		actors = append(actors, a)
	}
	if _, ok := e.rebuildTurn(actors[0]); ok {
		t.Fatal("a rebuild nobody asked for")
	}
	if g, pending := e.Rebuilds(); g != 0 || pending != 0 {
		t.Errorf("before a request: %d, %d pending", g, pending)
	}
	e.RequestRebuild()
	if _, pending := e.Rebuilds(); pending != len(actors) {
		t.Errorf("%d pending, want %d", pending, len(actors))
	}
	got := 0
	for _, a := range actors {
		if _, ok := e.rebuildTurn(a); ok {
			got++
		}
	}
	if got != MaxRebuilds {
		t.Errorf("%d rebuilds at once, want %d", got, MaxRebuilds)
	}
	for range got {
		<-e.rebuilds
	}
	for _, a := range actors {
		if _, ok := e.rebuildTurn(a); ok {
			<-e.rebuilds
		}
	}
	if _, pending := e.Rebuilds(); pending != 0 {
		t.Errorf("%d still pending", pending)
	}
	if _, ok := e.rebuildTurn(actors[0]); ok {
		t.Error("a persona rebuilt twice for one request")
	}
	if act, ok := e.draw(actors[0], &cfg); ok {
		t.Errorf("an on-demand action was drawn: %s", act.Name)
	}
}
