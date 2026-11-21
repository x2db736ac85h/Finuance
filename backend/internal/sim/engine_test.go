package sim

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/sim/identity"
	"github.com/x2db736ac85h/finuance/backend/internal/sim/statements"
)

var testSeed = []byte(strings.Repeat("k", identity.MinSeedBytes))

// fakeClock moves only when something sleeps; onSleep runs before each sleep (to change things
// while a persona waits).
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	slept   []time.Duration
	onSleep func()
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	hook := c.onSleep
	c.now = c.now.Add(d)
	c.slept = append(c.slept, d)
	c.mu.Unlock()
	if hook != nil {
		hook()
	}
	return nil
}

func (c *fakeClock) sleeps() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.slept...)
}

// fakeLab answers the few routes the engine itself uses, and records what it was sent.
type fakeLab struct {
	mu         sync.Mutex
	simulation bool
	requests   []*http.Request
	revoked    []string
	created    map[string]any
	accounts   []map[string]any
	status     int // the answer to GET /api/v1/notifications (a test action's route)
	logins     []string
	retryAfter string
}

func (f *fakeLab) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.Clone(r.Context()))
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Request-ID", r.Header.Get("X-Request-ID"))
	switch r.Method + " " + r.URL.Path {
	case "GET /api/v1/meta":
		_ = json.NewEncoder(w).Encode(map[string]any{"simulation": f.simulation})
	case "POST /api/v1/auth/login":
		var in struct{ Email string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.logins = append(f.logins, in.Email)
		_, _ = w.Write([]byte(`{}`))
	case "GET /api/v1/me/tokens":
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": "00000000-0000-4000-8000-000000000001", "name": "sim-otter-1", "prefix": "fin_1", "scope": "write"},
			{"id": "00000000-0000-4000-8000-000000000002", "name": "sim-otter-0", "prefix": "fin_0", "scope": "write", "revokedAt": "2026-01-01T00:00:00Z"},
			{"id": "00000000-0000-4000-8000-000000000003", "name": "fin on my laptop", "prefix": "fin_3", "scope": "read"},
		})
	case "POST /api/v1/me/tokens":
		_ = json.NewDecoder(r.Body).Decode(&f.created)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"secret":"fin_secret","token":{"id":"00000000-0000-4000-8000-000000000009","name":"x","prefix":"fin_","scope":"write"}}`))
	case "GET /api/v1/accounts":
		_ = json.NewEncoder(w).Encode(f.accounts)
	case "POST /api/v1/accounts":
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		acc := map[string]any{"id": uuid.NewString(), "name": in["name"], "txCount": 0}
		f.accounts = append(f.accounts, acc)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(acc)
	case "GET /api/v1/notifications":
		if f.retryAfter != "" {
			w.Header().Set("Retry-After", f.retryAfter)
		}
		w.WriteHeader(f.status)
		_, _ = w.Write([]byte(`{"items":[]}`))
	default:
		if strings.HasPrefix(r.URL.Path, "/api/v1/me/tokens/") && r.Method == http.MethodDelete {
			f.revoked = append(f.revoked, strings.TrimPrefix(r.URL.Path, "/api/v1/me/tokens/"))
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}
}

func newTestEngine(t *testing.T, lab *fakeLab, actions []Action) (*Engine, *fakeClock) {
	t.Helper()
	srv := httptest.NewServer(lab)
	t.Cleanup(srv.Close)
	clock := &fakeClock{now: time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)}
	e, err := New(Options{Target: srv.URL, Seed: testSeed, Version: "test", Actions: actions, Config: Defaults(), Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	return e, clock
}

// TestRefusesAnInstanceThatIsNotALab: the simulator stops before signing anyone in when the
// target's /meta does not report simulation: true (PLAN Phase 25, decision 4; THREAT-MODEL T19).
func TestRefusesAnInstanceThatIsNotALab(t *testing.T) {
	lab := &fakeLab{simulation: false}
	e, _ := newTestEngine(t, lab, nil)
	if err := e.Run(context.Background()); !errors.Is(err, ErrNotALab) {
		t.Fatalf("Run against an instance that is not a lab: %v", err)
	}
	for _, r := range lab.requests {
		if r.URL.Path != "/api/v1/meta" {
			t.Errorf("it sent %s %s before refusing", r.Method, r.URL.Path)
		}
	}
	lab.simulation = true
	if err := e.CheckTarget(context.Background()); err != nil {
		t.Errorf("a lab: %v", err)
	}
	if _, err := New(Options{Target: "ftp://lab", Seed: testSeed}); err == nil {
		t.Error("an ftp target was accepted")
	}
	if _, err := New(Options{Target: "https://lab.example", Seed: []byte("short")}); !errors.Is(err, identity.ErrShortSeed) {
		t.Errorf("a short seed: %v", err)
	}
}

// TestStart: a persona signs in with its derived password, replaces only its own live sim-*
// tokens with one new write token, makes its two accounts, and from then on sends the token with
// the simulator's headers (PLAN Phase 25, decision 5).
func TestStart(t *testing.T) {
	lab := &fakeLab{simulation: true, status: http.StatusOK}
	e, _ := newTestEngine(t, lab, nil)
	a, err := e.Actor(context.Background(), identity.Nth(0))
	if err != nil {
		t.Fatal(err)
	}
	if got := lab.revoked; len(got) != 1 || got[0] != "00000000-0000-4000-8000-000000000001" {
		t.Errorf("revoked %v, want only the live sim-* token", got)
	}
	want, _ := identity.Password(testSeed, "otter")
	if lab.created["password"] != want || lab.created["scope"] != "write" || !strings.HasPrefix(lab.created["name"].(string), "sim-otter-") {
		t.Errorf("the new token: %v", lab.created)
	}
	if len(lab.accounts) != 2 || a.Account(statements.Current) == uuid.Nil || a.Account(statements.Savings) == uuid.Nil {
		t.Errorf("accounts: %v", lab.accounts)
	}
	r, err := a.API.ListNotificationsWithResponse(context.Background(), nil)
	if err != nil || r.StatusCode() != http.StatusOK {
		t.Fatal(r, err)
	}
	id := regexp.MustCompile(`^sim-otter-[0-9a-f]{16}$`)
	for _, req := range lab.requests {
		if req.URL.Path == "/api/v1/meta" {
			continue
		}
		if ua := req.Header.Get("User-Agent"); ua != "finuance-sim/test (otter)" {
			t.Errorf("%s: User-Agent %q", req.URL.Path, ua)
		}
		if !id.MatchString(req.Header.Get("X-Request-ID")) || req.Header.Get("X-Finuance-Client") != "web" || req.Header.Get("Origin") != e.origin {
			t.Errorf("%s: headers %v", req.URL.Path, req.Header)
		}
	}
	last := lab.requests[len(lab.requests)-1]
	if last.Header.Get("Authorization") != "Bearer fin_secret" {
		t.Errorf("the API client sends %q", last.Header.Get("Authorization"))
	}
	for _, req := range lab.requests {
		if strings.HasPrefix(req.URL.Path, "/api/v1/me/tokens") && req.Header.Get("Authorization") != "" {
			t.Errorf("a token was sent to %s (the token routes are session-only)", req.URL.Path)
		}
	}
}

func testActor(e *Engine, c Character) *Actor {
	return &Actor{Persona: identity.Nth(0), Character: c, engine: e, Rand: rand.New(rand.NewPCG(1, 2)), //nolint:gosec // a test's stream
		accounts: map[int]uuid.UUID{statements.Current: uuid.New(), statements.Savings: uuid.New()}, every: map[string]time.Time{},
		imported: map[int]platform.Date{}}
}

func TestPause(t *testing.T) {
	e, _ := newTestEngine(t, &fakeLab{simulation: true}, nil)
	flat := [24]int{}
	for i := range flat {
		flat[i] = 10
	}
	a := testActor(e, Character{Think: [2]time.Duration{12 * time.Second, 12 * time.Second}, Hours: flat})
	noon := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		cfg  Config
		want time.Duration
	}{
		{Config{Intensity: 1000, ActiveHours: true}, 12 * time.Second},
		{Config{Intensity: 2000, ActiveHours: true}, 6 * time.Second},
		{Config{Intensity: 3000}, 4 * time.Second},
		{Config{Intensity: 9000}.Clamp(), 4 * time.Second}, // held to 3×
		{Config{Intensity: 500}, 24 * time.Second},
		{Config{Intensity: 0}, ConfigCheck}, // idle: look again later
	} {
		if got := e.pause(a, &c.cfg, noon); got != c.want {
			t.Errorf("intensity %d: %v, want %v", c.cfg.Intensity, got, c.want)
		}
	}
	a.Character.Hours[12], a.Character.Hours[13] = 5, 0
	if got := e.pause(a, &Config{Intensity: 1000, ActiveHours: true}, noon); got != 24*time.Second {
		t.Errorf("half as active: %v", got)
	}
	if got := e.pause(a, &Config{Intensity: 1000}, noon); got != 12*time.Second {
		t.Errorf("active hours off: %v", got)
	}
	if got := e.pause(a, &Config{Intensity: 1000, ActiveHours: true}, noon.Add(time.Hour+20*time.Minute)); got != 40*time.Minute {
		t.Errorf("asleep: %v, want until the next hour", got)
	}
	a.Character.Think = [2]time.Duration{0, 0}
	if got := e.pause(a, &Config{Intensity: 3000}, noon); got != MinGap {
		t.Errorf("no think time: %v, want MinGap", got)
	}
}

func noop(context.Context, *Actor) error { return nil }

func TestDraw(t *testing.T) {
	acts := []Action{
		{Name: "table.window", Area: "table", Needs: NeedData, Run: noop},
		{Name: "insights.overview", Area: "insights", Run: noop},
		{Name: "rules.apply", Area: "rules", Recomputes: true, Run: noop},
		{Name: "account.unknown-path", Area: "account", Mistake: true, Run: noop},
	}
	e, _ := newTestEngine(t, &fakeLab{simulation: true}, acts)
	a := testActor(e, Character{Weights: map[string]int{"table.window": 1, "insights.overview": 1, "rules.apply": 1}})
	seen := func(cfg Config, n int) map[string]int {
		out := map[string]int{}
		for range n {
			if act, ok := e.draw(a, &cfg); ok {
				out[act.Name]++
				a.lastAction = act.Name
			} else {
				out[""]++
			}
		}
		return out
	}
	got := seen(Config{}, 300)
	if got["table.window"] != 0 || got["insights.overview"] == 0 || got["rules.apply"] == 0 || got["account.unknown-path"] != 0 {
		t.Errorf("without data: %v", got)
	}
	a.hasData = true
	if got := seen(Config{AreasOff: []string{"insights", "rules"}}, 100); got["table.window"] != 100 {
		t.Errorf("areas off: %v", got)
	}
	if got := seen(Config{Weights: map[string]int{"insights.overview": 0, "rules.apply": 0}}, 100); got["table.window"] != 100 {
		t.Errorf("weights 0: %v", got)
	}
	if got := seen(Config{PersonaWeights: map[string]map[string]int{"otter": {"table.window": 0, "rules.apply": 0}}}, 100); got["insights.overview"] != 100 {
		t.Errorf("the persona's own weights: %v", got)
	}
	if got := seen(Config{Mistakes: 1000}, 50); got["account.unknown-path"] != 50 {
		t.Errorf("every draw a mistake: %v", got)
	}
	a.lastAction = ""
	prev := ""
	for range 200 {
		act, _ := e.draw(a, &Config{AreasOff: []string{"table"}})
		if act.Name == "rules.apply" && prev == "rules.apply" {
			t.Fatal("an action that recomputes was drawn twice in a row")
		}
		prev, a.lastAction = act.Name, act.Name
	}
}

// TestCeilings: no configuration passes the code's ceilings (PLAN Phase 25, decision 7).
func TestCeilings(t *testing.T) {
	c := Config{Personas: 500, Intensity: 99_000, ClockRate: 1e6, Mistakes: 5000, AssistantPerDay: 1e9,
		AreasOff: []string{"table", "bitcoin"}, Off: []string{"fox", "unicorn"},
		Weights: map[string]int{"table.window": 1e6, "x": -5}}.Clamp()
	if c.Personas != MaxPersonas || c.Intensity != MaxIntensity || c.ClockRate != MaxClockRate || c.Mistakes != 1000 ||
		c.AssistantPerDay != MaxAssistantPerDay || len(c.AreasOff) != 1 || len(c.Off) != 1 || c.Weights["table.window"] != 1000 || c.Weights["x"] != 0 {
		t.Errorf("clamped: %+v", c)
	}
	if c := (Config{Personas: -3, ClockRate: 0}).Clamp(); c.Personas != 1 || c.ClockRate != 1 {
		t.Errorf("below the floor: %+v", c)
	}
	e, _ := newTestEngine(t, &fakeLab{simulation: true}, nil)
	if got, _ := e.SetConfig(Config{Personas: 1000}); got.Personas != MaxPersonas {
		t.Errorf("SetConfig kept %d personas", got.Personas)
	}
}

// TestOneQuestionAtATime: at most one assistant run across every persona, and a daily cap.
func TestOneQuestionAtATime(t *testing.T) {
	e, clock := newTestEngine(t, &fakeLab{simulation: true}, nil)
	e.SetConfig(Config{AssistantPerDay: 2, Intensity: 1000})
	release, started := make(chan struct{}), make(chan struct{})
	ask := Action{Name: "assistant.ask", Area: "assistant", Cost: Model, Run: func(context.Context, *Actor) error {
		close(started)
		<-release
		return nil
	}}
	quick := Action{Name: "assistant.ask", Area: "assistant", Cost: Model, Run: noop}
	a, b := testActor(e, Character{}), testActor(e, Character{})
	done := make(chan Event)
	go func() { done <- e.Do(context.Background(), a, ask) }()
	<-started
	if ev := e.Do(context.Background(), b, quick); ev.Result != ResultSkipped {
		t.Errorf("a second question while one runs: %+v", ev)
	}
	close(release)
	if ev := <-done; ev.Result != ResultOK {
		t.Errorf("the first question: %+v", ev)
	}
	if ev := e.Do(context.Background(), b, quick); ev.Result != ResultOK {
		t.Errorf("the second question of the day (the skipped one did not count): %+v", ev)
	}
	if ev := e.Do(context.Background(), b, quick); ev.Result != ResultSkipped || !strings.Contains(ev.Error, "daily cap") {
		t.Errorf("over the daily cap: %+v", ev)
	}
	clock.now = clock.now.Add(24 * time.Hour)
	if ev := e.Do(context.Background(), b, quick); ev.Result != ResultOK {
		t.Errorf("the next day: %+v", ev)
	}
}

// TestBackoff: five 5xx answers in a row send the persona into a back-off that doubles; a 429
// waits its Retry-After; a 401 signs the persona in again.
func TestBackoff(t *testing.T) {
	lab := &fakeLab{simulation: true, status: http.StatusServiceUnavailable}
	e, clock := newTestEngine(t, lab, nil)
	ctx := context.Background()
	a, err := e.Actor(ctx, identity.Nth(0))
	if err != nil {
		t.Fatal(err)
	}
	read := Action{Name: "account.notifications", Area: "account", Run: func(ctx context.Context, a *Actor) error {
		r, err := a.API.ListNotificationsWithResponse(ctx, nil)
		if err != nil {
			return err
		}
		return Expect("notifications", r.HTTPResponse, http.StatusOK)
	}}
	for round, want := range []time.Duration{FirstBackoff, 2 * FirstBackoff} {
		for i := range ServerErrorsInARow {
			ev := e.Do(ctx, a, read)
			if ev.Status != http.StatusServiceUnavailable || ev.Result != ResultError || !strings.HasPrefix(ev.RequestID, "sim-otter-") {
				t.Fatalf("round %d, answer %d: %+v", round, i, ev)
			}
			before := len(clock.sleeps())
			e.afterwards(ctx, a, ev)
			slept := clock.sleeps()[before:]
			if i < ServerErrorsInARow-1 && len(slept) != 0 {
				t.Fatalf("round %d: a back-off after %d errors: %v", round, i+1, slept)
			}
			if i == ServerErrorsInARow-1 && (len(slept) != 1 || slept[0] != want) {
				t.Fatalf("round %d: after %d errors it slept %v, want %v", round, ServerErrorsInARow, slept, want)
			}
		}
	}
	if backoff(10) != MaxBackoff {
		t.Errorf("the back-off is not capped: %v", backoff(10))
	}
	lab.status, lab.retryAfter = http.StatusTooManyRequests, "7"
	ev := e.Do(ctx, a, read)
	before := len(clock.sleeps())
	e.afterwards(ctx, a, ev)
	if slept := clock.sleeps()[before:]; len(slept) != 1 || slept[0] != 7*time.Second {
		t.Errorf("after a 429 with Retry-After 7: %v", slept)
	}
	lab.status, lab.retryAfter = http.StatusUnauthorized, ""
	e.afterwards(ctx, a, e.Do(ctx, a, read))
	if a.signedIn() {
		t.Error("a 401 kept the token")
	}
}

// TestConfigChangeInterruptsTheWait: a persona waiting through a long pause sees a new
// configuration within ConfigCheck (the tuning page's changes take effect within five seconds).
func TestConfigChangeInterruptsTheWait(t *testing.T) {
	e, clock := newTestEngine(t, &fakeLab{simulation: true}, nil)
	e.SetConfig(Config{Intensity: 1000})
	a := testActor(e, Character{Think: [2]time.Duration{time.Hour, time.Hour}})
	changed := false
	clock.onSleep = func() {
		if !changed {
			changed = true
			e.SetConfig(Config{Intensity: 3000})
		}
	}
	start := clock.Now()
	if e.wait(context.Background(), a) {
		t.Fatal("the wait ran its full hour through a change")
	}
	if waited := clock.Now().Sub(start); waited > ConfigCheck {
		t.Errorf("the change was seen after %v", waited)
	}
}

// TestRunPlaysThePersonasSwitchedOn: Run signs in exactly the personas the configuration switches
// on, they act, and a change of the configuration reaches the ones already playing.
func TestRunPlaysThePersonasSwitchedOn(t *testing.T) {
	lab := &fakeLab{simulation: true}
	var mu sync.Mutex
	ran := map[string]int{}
	act := Action{Name: "account.notifications", Area: "account", Run: func(_ context.Context, a *Actor) error {
		mu.Lock()
		ran[a.Persona.Slug]++
		mu.Unlock()
		return nil
	}}
	e, _ := newTestEngine(t, lab, []Action{act})
	e.SetConfig(Config{Personas: 3, Intensity: 1000, Off: []string{"owl"}, Weights: map[string]int{"account.notifications": 1}})
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if err := e.Run(ctx); err != nil {
		t.Fatal(err)
	}
	lab.mu.Lock()
	logins := slices.Clone(lab.logins)
	lab.mu.Unlock()
	slices.Sort(logins)
	if !slices.Equal(slices.Compact(logins), []string{"badger@sim.invalid", "otter@sim.invalid"}) {
		t.Errorf("signed in: %v", logins)
	}
	mu.Lock()
	defer mu.Unlock()
	if ran["otter"] == 0 || ran["badger"] == 0 || ran["owl"] != 0 || ran["magpie"] != 0 {
		t.Errorf("actions by persona: %v", ran)
	}
}
