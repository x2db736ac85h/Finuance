package sim

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	mrand "math/rand/v2"
	"net/http"
	"net/http/cookiejar"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/client/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/sim/identity"
	"github.com/x2db736ac85h/finuance/backend/internal/sim/statements"
)

// Actor is one persona at work: its clients, its random stream and what it knows of its state on
// the instance. The simulator stores nothing; an actor discovers its state when it starts.
type Actor struct {
	Persona   identity.Persona
	Character Character
	// API calls with the persona's access token; Web with its session cookie (for the routes no
	// token reaches); Anon with neither (a failed sign-in, the public routes).
	API, Web, Anon *gen.ClientWithResponses
	// Rand is the persona's own stream, seeded from its name and the run seed: a run with the same
	// seed can be repeated.
	Rand *mrand.Rand

	engine   *Engine
	password string

	mu            sync.Mutex
	token         string
	accounts      map[int]uuid.UUID // statements.Current / Savings → the account's id
	hasData       bool
	serverErrors  int                // 5xx answers in a row
	backoffs      int                // back-offs in a row
	burst         int                // quick actions left in a burst (Config.Bursts)
	limits        map[string]*pacer  // the instance's limits per user (Allow, Pace)
	history       statements.History // the profile its accounts were made with
	rebuilt       int64              // the last history rebuild it did (Engine.RequestRebuild)
	lastAction    string
	lastStart     time.Time
	every         map[string]time.Time
	cal           calendar
	imported      map[int]platform.Date // the last imported day per bank account
	cash          uuid.UUID
	raw           *http.Client // API's client, for the paths the contract doesn't have
	lastRequestID string       // of the current action's last request (the feed shows it)
}

func newActor(e *Engine, p identity.Persona) (*Actor, error) {
	password, err := identity.Password(e.o.Seed, p.Slug)
	if err != nil {
		return nil, err
	}
	a := &Actor{Persona: p, Character: Characters[p.Animal], engine: e, password: password, accounts: map[int]uuid.UUID{},
		every: map[string]time.Time{}, imported: map[int]platform.Date{}, Rand: streamOf(p.Slug, e.cfg.RunSeed)} // the caller holds e.mu
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	base := strings.TrimRight(e.target.String(), "/") + "/api/v1"
	httpClient := func(auth bool, jar http.CookieJar) *http.Client {
		return &http.Client{Transport: &transport{base: e.o.Transport, a: a, auth: auth}, Jar: jar, Timeout: 2 * time.Minute,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	mk := func(auth bool, jar http.CookieJar) (*gen.ClientWithResponses, error) {
		return gen.NewClientWithResponses(base, gen.WithHTTPClient(httpClient(auth, jar)))
	}
	a.raw = httpClient(true, nil)
	if a.API, err = mk(true, nil); err != nil {
		return nil, err
	}
	if a.Web, err = mk(false, jar); err != nil {
		return nil, err
	}
	if a.Anon, err = mk(false, nil); err != nil {
		return nil, err
	}
	return a, nil
}

// streamOf is a persona's random stream for a run seed.
func streamOf(slug string, runSeed int64) *mrand.Rand {
	sum := sha256.Sum256(fmt.Appendf(nil, "finuance-sim/v1 rand %s %d", slug, runSeed))
	return mrand.New(mrand.NewPCG(binary.LittleEndian.Uint64(sum[:8]), binary.LittleEndian.Uint64(sum[8:16]))) //nolint:gosec // a simulation's stream, not a secret
}

// withRunSeed draws a run seed when c has none (1 … MaxRunSeed).
func withRunSeed(c Config) Config {
	for c.RunSeed == 0 {
		var b [8]byte
		_, _ = rand.Read(b[:])
		c.RunSeed = int64(binary.LittleEndian.Uint64(b[:]) & MaxRunSeed) //nolint:gosec // masked to 53 bits
	}
	return c
}

// IntN draws from the persona's stream: a number in [0, n).
func (a *Actor) IntN(n int) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.Rand.IntN(n)
}

// Pick draws one of xs from the persona's stream.
func Pick[T any](a *Actor, xs ...T) T { return xs[a.IntN(len(xs))] }

// Every reports whether at least d has passed since it last said yes for this key, and if so
// counts this time: for actions with a pace of their own (a failed sign-in must not lock the
// persona out: at most 20 failures an hour per address).
func (a *Actor) Every(key string, d time.Duration) bool {
	now := a.engine.o.Clock.Now()
	a.mu.Lock()
	defer a.mu.Unlock()
	if last, ok := a.every[key]; ok && now.Sub(last) < d {
		return false
	}
	a.every[key] = now
	return true
}

// Limit is one of the instance's limits per user that a persona could pass: a token bucket like
// the instance's own (internal/httpapi/middleware), one turn every Every with up to Burst at once.
type Limit struct {
	Key   string
	Every time.Duration
	Burst int
}

// The instance's limits per user at their defaults (the middleware's policies and
// FINUANCE_AI_RATE_PER_HOUR, each 30 with a burst of 30): every persona is a user of its own, so it
// keeps below them itself, at the same rate with a smaller burst.
var (
	Questions = Limit{"assistant.question", 2 * time.Minute, 20} // 30 questions an hour
	Uploads   = Limit{"upload", 2500 * time.Millisecond, 20}     // 30 uploads a minute
	Invites   = Limit{"shared.invite", 150 * time.Second, 10}    // 30 invitations an hour
)

// Allow takes a turn of the limit if one is free now (a question: skipped otherwise).
func (a *Actor) Allow(l Limit) bool {
	now := a.engine.o.Clock.Now()
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.limitLocked(l.Key).allow(now, l.Every, float64(l.Burst))
}

// Pace waits for a turn of the limit (an upload: waiting is what a person would do).
func (a *Actor) Pace(ctx context.Context, l Limit) error {
	now := a.engine.o.Clock.Now()
	a.mu.Lock()
	t := a.limitLocked(l.Key).reserve(now, l.Every, l.Every, float64(l.Burst))
	a.mu.Unlock()
	if w := t.Sub(now); w > 0 {
		return a.engine.o.Clock.Sleep(ctx, w)
	}
	return nil
}

func (a *Actor) limitLocked(key string) *pacer {
	p := a.limits[key]
	if p == nil {
		p = &pacer{}
		if a.limits == nil {
			a.limits = map[string]*pacer{}
		}
		a.limits[key] = p
	}
	return p
}

func (a *Actor) rebuiltGen() int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.rebuilt
}

// HasData reports whether the persona's accounts hold transactions.
func (a *Actor) HasData() bool { return a.has(NeedData) }

// History is the profile the persona's accounts were made with.
func (a *Actor) History() statements.History {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.history
}

// SetHistory records the profile of the persona's new accounts.
func (a *Actor) SetHistory(h statements.History) {
	a.mu.Lock()
	a.history = h.Normal()
	a.mu.Unlock()
}

// Engine is the engine the persona plays in (its configuration, the notice board).
func (a *Actor) Engine() *Engine { return a.engine }

// Now is the engine's clock.
func (a *Actor) Now() time.Time { return a.engine.o.Clock.Now() }

// How WhileBusy tries again: BusyTries times in all, BusyWait apart unless the answer says how
// long (Retry-After), at most MaxBusyWait.
const (
	BusyTries   = 5
	BusyWait    = 2 * time.Second
	MaxBusyWait = 30 * time.Second
)

// Answer is what WhileBusy reads of an answer: its status, headers and body.
type Answer struct {
	Status int
	Header http.Header
	Body   []byte
}

// AnswerOf is a generated client's answer as WhileBusy reads it.
func AnswerOf(r *http.Response, body []byte) Answer {
	return Answer{Status: r.StatusCode, Header: r.Header, Body: body}
}

// WhileBusy sends a request again while the instance answers 409 account_busy: the account's lock
// is held by a commit or a reclassification of the same user (ADR 0038), and a person tries again
// a moment later. Any other answer, a 409 of another kind included (a file already imported),
// ends it at once; so does the last try. send keeps its typed answer itself (the generated
// client's, with the body read) and tells WhileBusy what it needs of it.
func (a *Actor) WhileBusy(ctx context.Context, send func() (Answer, error)) error {
	for try := 1; ; try++ {
		ans, err := send()
		if err != nil || ans.Status != http.StatusConflict || try == BusyTries {
			return err
		}
		var problem struct {
			Code string `json:"code"`
		}
		if json.Unmarshal(ans.Body, &problem) != nil || problem.Code != "account_busy" {
			return nil
		}
		wait := BusyWait
		if secs, err := strconv.Atoi(ans.Header.Get("Retry-After")); err == nil && secs >= 0 {
			wait = time.Duration(secs) * time.Second
		}
		if err := a.engine.o.Clock.Sleep(ctx, min(wait, MaxBusyWait)); err != nil {
			return err
		}
	}
}

// Raw sends a request the contract has no operation for (an unknown path), with the persona's
// token and headers; path is below /api/v1.
func (a *Actor) Raw(ctx context.Context, method, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(a.engine.target.String(), "/")+"/api/v1"+path, http.NoBody)
	if err != nil {
		return nil, err
	}
	return a.raw.Do(req)
}

// Password is the persona's password (for the actions that confirm it).
func (a *Actor) Password() string { return a.password }

// Account returns the id of the persona's account (statements.Current or Savings).
func (a *Actor) Account(n int) uuid.UUID {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.accounts[n]
}

// SetHasData records that the persona has transactions (after an import or a hand-typed one).
func (a *Actor) SetHasData(v bool) {
	a.mu.Lock()
	a.hasData = v
	a.mu.Unlock()
}

func (a *Actor) has(n Need) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if n&NeedAccounts != 0 && len(a.accounts) < 2 {
		return false
	}
	if n&NeedData != 0 && !a.hasData {
		return false
	}
	return true
}

func (a *Actor) signedIn() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.token != ""
}

// start signs the persona in with a session, replaces its earlier sim-* access tokens with a new
// one (one sign-in per start, not per action; at most 25 live tokens per user) and discovers its
// accounts (PLAN Phase 25, decision 5).
func (a *Actor) start(ctx context.Context) error {
	login, err := a.Web.LoginWithResponse(ctx, gen.LoginJSONRequestBody{Email: a.Persona.Email, Password: a.password})
	if err != nil {
		return err
	}
	if err := Expect("sign in", login.HTTPResponse, http.StatusOK); err != nil {
		return err
	}
	list, err := a.Web.ListAccessTokensWithResponse(ctx)
	if err != nil {
		return err
	}
	if err := Expect("list tokens", list.HTTPResponse, http.StatusOK); err != nil {
		return err
	}
	for i := range *list.JSON200 {
		t := &(*list.JSON200)[i]
		if strings.HasPrefix(t.Name, "sim-") && t.RevokedAt == nil {
			r, err := a.Web.RevokeAccessTokenWithResponse(ctx, t.Id)
			if err != nil {
				return err
			}
			if err := Expect("revoke a token", r.HTTPResponse, http.StatusNoContent, http.StatusNotFound); err != nil {
				return err
			}
		}
	}
	pw := a.password
	made, err := a.Web.CreateAccessTokenWithResponse(ctx, gen.CreateAccessTokenJSONRequestBody{Name: fmt.Sprintf("sim-%s-%d",
		a.Persona.Slug, a.engine.o.Clock.Now().Unix()), Scope: gen.AccessTokenRequestScopeWrite, ExpiresInDays: 30, Password: &pw})
	if err != nil {
		return err
	}
	if err := Expect("make a token", made.HTTPResponse, http.StatusCreated); err != nil {
		return err
	}
	a.mu.Lock()
	a.token = made.JSON201.Secret
	a.mu.Unlock()
	return a.discover(ctx)
}

// discover finds the persona's two accounts by name, makes the missing ones, and whether it has
// transactions.
func (a *Actor) discover(ctx context.Context) error {
	list, err := a.API.ListAccountsWithResponse(ctx)
	if err != nil {
		return err
	}
	if err := Expect("list accounts", list.HTTPResponse, http.StatusOK); err != nil {
		return err
	}
	cfg, _ := a.engine.Config()
	found, data, lastOn := map[int]uuid.UUID{}, false, map[int]*platform.Date{}
	history := cfg.History // a persona without accounts starts with the configured profile
	for i := range *list.JSON200 {
		acc := &(*list.JSON200)[i]
		if n, h, ok := statements.ParseAccountName(acc.Name); ok && acc.Kind == gen.AccountKindBank {
			if _, dup := found[n]; dup {
				continue
			}
			found[n] = acc.Id
			if n == statements.Current || len(found) == 1 {
				history = h // the current account's profile is the persona's
			}
			if acc.LastOn != nil {
				d := platform.DateOf(acc.LastOn.Time, time.UTC)
				lastOn[n] = &d
			}
		}
		if acc.Kind == gen.AccountKindCash && acc.Name == CashAccount {
			a.mu.Lock()
			a.cash = acc.Id
			a.mu.Unlock()
		}
		data = data || acc.TxCount > 0
	}
	for _, n := range []int{statements.Current, statements.Savings} {
		if _, ok := found[n]; ok {
			continue
		}
		made, err := a.API.CreateAccountWithResponse(ctx, gen.CreateAccountJSONRequestBody{Name: statements.AccountNameFor(n, history)})
		if err != nil {
			return err
		}
		if err := Expect("make an account", made.HTTPResponse, http.StatusCreated); err != nil {
			return err
		}
		found[n] = made.JSON201.Id
	}
	a.mu.Lock()
	a.accounts, a.hasData, a.history = found, data, history
	a.startCalendarLocked(lastOn, cfg.ClockRate)
	a.mu.Unlock()
	return nil
}

// CashAccount is the name of the account a persona types its cash into.
const CashAccount = "Cash"

// CashAccountID is the persona's cash account, or the zero id before it has one.
func (a *Actor) CashAccountID() uuid.UUID {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cash
}

// SetCashAccount records the persona's cash account.
func (a *Actor) SetCashAccount(id uuid.UUID) {
	a.mu.Lock()
	a.cash = id
	a.mu.Unlock()
}

// SetAccount records the id of one of the persona's two bank accounts (after it made it again).
func (a *Actor) SetAccount(n int, id uuid.UUID) {
	a.mu.Lock()
	a.accounts[n] = id
	a.mu.Unlock()
}

// transport adds the simulator's headers to every request and keeps count of the persona's 5xx
// answers in a row. The User-Agent and the request id are informative only: the server labels the
// traffic from the user's row (ADR 0041).
type transport struct {
	base http.RoundTripper
	a    *Actor
	auth bool
}

func (t *transport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	e := t.a.engine
	r.Header.Set("User-Agent", fmt.Sprintf("finuance-sim/%s (%s)", e.o.Version, t.a.Persona.Slug))
	id := newRequestID(t.a.Persona.Slug)
	r.Header.Set("X-Request-ID", id)
	t.a.mu.Lock()
	t.a.lastRequestID = id
	t.a.mu.Unlock()
	r.Header.Set("X-Finuance-Client", "web")
	r.Header.Set("Origin", e.origin)
	if t.auth {
		t.a.mu.Lock()
		token := t.a.token
		t.a.mu.Unlock()
		r.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := t.base.RoundTrip(r)
	t.a.mu.Lock()
	// the id the logs use is the one the server answers: the simulator's own where the api takes it
	// from the client, the web server's on Compose, which gives every request its own (PLAN 23.4)
	if resp != nil {
		if answered := resp.Header.Get("X-Request-ID"); answered != "" {
			t.a.lastRequestID = answered
		}
	}
	switch {
	case err != nil || resp.StatusCode >= 500:
		t.a.serverErrors++
	default:
		t.a.serverErrors = 0
	}
	t.a.mu.Unlock()
	return resp, err
}

// newRequestID is sim-<persona>-<16 hex digits>: a reader can tell the simulator's lines apart.
func newRequestID(slug string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "sim-" + slug + "-" + hex.EncodeToString(b)
}
