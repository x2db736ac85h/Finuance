package assistant

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/config"
	"github.com/x2db736ac85h/finuance/backend/internal/llm"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

// How long a connection's model list is reused (PLAN §6.2: cached 60 s). A failed check is
// repeated sooner, so starting the model server shows up quickly.
const (
	statusTTL      = 60 * time.Second
	statusErrorTTL = 5 * time.Second
	statusTimeout  = 8 * time.Second
)

// MaxRowsLimit is the most rows one lookup may return (legacy maxRows cap).
const MaxRowsLimit = 50

// Connection is one configured model server with its client.
type Connection struct {
	config.AIConnection
	Client llm.Client
}

// ModelInfo is a chat model a connection offers, with what it can do and an administrator's
// tuning for it (nil: none).
type ModelInfo struct {
	ID     string
	Caps   llm.Caps
	Tuning *Tuning
	Loaded *bool // whether the server has it in memory now; nil when the server doesn't say
}

// Controls are an administrator's settings for one connection, the connections file's included
// (Phase 20 follow-ups, ADR 0035 addendum): on or off, how long the server keeps a model after the
// last question, the model questions use and the one for questions with photos.
type Controls struct {
	Enabled      bool
	UnloadAfter  time.Duration // 0 = the server's own setting
	DefaultModel string        // "" = the connection's own default
	PhotoModel   string        // "" = none
	Version      int32         // 0 = never saved
	UpdatedAt    *time.Time
}

// DefaultUnloadAfter is the idle time of a connection whose controls were never saved.
const DefaultUnloadAfter = 30 * time.Minute

// DefaultControls apply to a connection until an administrator saves its controls.
var DefaultControls = Controls{Enabled: true, UnloadAfter: DefaultUnloadAfter}

// UnloadChoices are the idle times an administrator can pick, in minutes.
var UnloadChoices = []int{10, 15, 30, 45, 60, 120, 240}

// keepWarmTimeout bounds moving a model's unload time after a question.
const keepWarmTimeout = 30 * time.Second

// Tuning is an administrator's settings for one model of one connection (ADR 0035): defaults for
// the temperature, the thinking level and the tool set when the question doesn't set them, the
// context size to plan with, and a short note added to the prompt.
type Tuning struct {
	Connection, Model string
	Temperature       *float64
	ContextTokens     *int
	Think             llm.Thinking // "" = the question's
	Toolset           string       // "" = the question's
	ExtraPrompt       string
	UpdatedAt         time.Time
}

// ConnectionStatus is what GET /assistant/status reports for one connection.
type ConnectionStatus struct {
	Name, Label  string
	API          llm.API
	Cloud        bool
	Reachable    bool
	Models       []ModelInfo
	DefaultModel string
	PhotoModel   string        // the connection's model for photos, when it offers it
	Latency      time.Duration // how long reading the model list took
	Server       llm.Server
	Loaded       []llm.LoadedModel // what the server holds in memory (Ollama, LM Studio)
	LoadedKnown  bool              // the server said which models are loaded
	Err          *llm.Error        // why the model list couldn't be read
}

// ErrUnknownConnection and ErrUnknownModel reject a choice the instance doesn't offer.
var (
	ErrUnknownConnection = errors.New("unknown assistant connection")
	ErrUnknownModel      = errors.New("model not offered by this connection")
)

// Connections holds the configured model servers (the connections file's, then those stored in
// the panel, ADR 0035), caches their model lists and remembers what was learned about a model
// while running (a model that rejected tools is plan-only from then on).
type Connections struct {
	clock     platform.Clock
	newClient func(llm.Config) llm.Client
	timeout   time.Duration
	idle      time.Duration

	lmu      sync.RWMutex
	file     []*Connection
	stored   []*Connection
	tuning   map[string]*Tuning // "connection\x00model"
	controls map[string]Controls

	mu      sync.Mutex
	cache   map[string]statusEntry
	flights map[string]*flight
	learned map[string]llm.Caps // "connection\x00model" → corrected caps
}

type statusEntry struct {
	st      ConnectionStatus
	expires time.Time
}

type flight struct {
	done chan struct{}
	st   ConnectionStatus
}

// NewConnections builds a client per connection. timeout bounds one model request; idle ends a
// stream that stays silent that long (also before the first token: a slow GPU reading a long prompt
// needs more than the default), at most timeout; 0 = 60 s. newClient is llm.New in production.
func NewConnections(cfgs []config.AIConnection, timeout, idle time.Duration, clock platform.Clock, newClient func(llm.Config) llm.Client) *Connections {
	if newClient == nil {
		newClient = llm.New
	}
	if idle <= 0 {
		idle = 60 * time.Second
	}
	cs := &Connections{clock: clock, newClient: newClient, timeout: timeout, idle: idle, tuning: map[string]*Tuning{},
		controls: map[string]Controls{}, cache: map[string]statusEntry{}, flights: map[string]*flight{}, learned: map[string]llm.Caps{}}
	for i := range cfgs {
		cs.file = append(cs.file, cs.connect(cfgs[i]))
	}
	return cs
}

func (cs *Connections) connect(c config.AIConnection) *Connection {
	cl := cs.newClient(llm.Config{API: c.API, BaseURL: c.BaseURL.String(), APIKey: c.APIKey, AllowRemote: c.Cloud,
		Timeout: cs.timeout, IdleTimeout: min(cs.timeout, cs.idle)})
	return &Connection{AIConnection: c, Client: cl}
}

// all is every connection: the file's, then the stored ones.
func (cs *Connections) all() []*Connection {
	if cs == nil {
		return nil
	}
	cs.lmu.RLock()
	defer cs.lmu.RUnlock()
	return append(slices.Clip(cs.file), cs.stored...)
}

// enabled is every connection an administrator hasn't switched off: the ones people use.
func (cs *Connections) enabled() []*Connection {
	var out []*Connection
	for _, c := range cs.all() {
		if cs.ControlsOf(c.Name).Enabled {
			out = append(out, c)
		}
	}
	return out
}

// ControlsOf returns a connection's controls (DefaultControls until saved).
func (cs *Connections) ControlsOf(name string) Controls {
	if cs == nil {
		return DefaultControls
	}
	cs.lmu.RLock()
	defer cs.lmu.RUnlock()
	if c, ok := cs.controls[name]; ok {
		return c
	}
	return DefaultControls
}

// Disabled reports whether an administrator switched the connection off.
func (cs *Connections) Disabled(name string) bool { return name != "" && !cs.ControlsOf(name).Enabled }

// FileNames are the names of the connections file's connections (the panel can't take them).
func (cs *Connections) FileNames() []string {
	if cs == nil {
		return nil
	}
	cs.lmu.RLock()
	defer cs.lmu.RUnlock()
	names := make([]string, len(cs.file))
	for i, c := range cs.file {
		names[i] = c.Name
	}
	return names
}

// SetStored replaces the connections, tuning and controls stored in the panel (ADR 0035). A
// connection whose settings changed gets a new client and forgets its cached model list and what
// was learned; one whose controls changed forgets its cached model list.
func (cs *Connections) SetStored(conns []config.AIConnection, tuning []Tuning, controls map[string]Controls) {
	cs.lmu.Lock()
	old := map[string]*Connection{}
	for _, c := range cs.stored {
		old[c.Name] = c
	}
	var stored []*Connection
	var changed []string
	for i := range conns {
		c := conns[i]
		if o, ok := old[c.Name]; ok && sameConnection(o.AIConnection, c) {
			stored = append(stored, o)
			delete(old, c.Name)
			continue
		}
		stored = append(stored, cs.connect(c))
		changed = append(changed, c.Name)
	}
	for name := range old {
		changed = append(changed, name)
	}
	cs.stored = stored
	for name, c := range controls {
		if o, ok := cs.controls[name]; !ok || o.Version != c.Version {
			changed = append(changed, name)
		}
	}
	for name := range cs.controls {
		if _, ok := controls[name]; !ok {
			changed = append(changed, name)
		}
	}
	cs.controls = map[string]Controls{}
	for name, c := range controls {
		cs.controls[name] = c
	}
	cs.tuning = map[string]*Tuning{}
	for i := range tuning {
		t := tuning[i]
		cs.tuning[t.Connection+"\x00"+t.Model] = &t
	}
	cs.lmu.Unlock()
	cs.mu.Lock()
	for _, name := range changed {
		delete(cs.cache, name)
		for k := range cs.learned {
			if strings.HasPrefix(k, name+"\x00") {
				delete(cs.learned, k)
			}
		}
	}
	cs.mu.Unlock()
}

func sameConnection(a, b config.AIConnection) bool {
	return a.Name == b.Name && a.Label == b.Label && a.API == b.API && a.BaseURL.String() == b.BaseURL.String() && a.APIKey == b.APIKey &&
		a.Cloud == b.Cloud && a.DefaultModel == b.DefaultModel && slices.Equal(a.Models, b.Models) && a.ContextTokens == b.ContextTokens
}

func (cs *Connections) tuningOf(conn, model string) *Tuning {
	cs.lmu.RLock()
	defer cs.lmu.RUnlock()
	return cs.tuning[conn+"\x00"+model]
}

// Enabled reports whether any connection is configured and switched on.
func (cs *Connections) Enabled() bool { return len(cs.enabled()) > 0 }

// Get returns a connection people may use by name; "" is the first one switched on.
func (cs *Connections) Get(name string) (*Connection, error) {
	return get(cs.enabled(), name)
}

// GetAny returns any connection by name, also one switched off (Administration).
func (cs *Connections) GetAny(name string) (*Connection, error) {
	return get(cs.all(), name)
}

func get(list []*Connection, name string) (*Connection, error) {
	if len(list) == 0 {
		return nil, ErrUnknownConnection
	}
	if name == "" {
		return list[0], nil
	}
	for _, c := range list {
		if c.Name == name {
			return c, nil
		}
	}
	return nil, fmt.Errorf("%w: %q", ErrUnknownConnection, name)
}

// CheckHosts resolves the host of every local connection at startup and fails when one resolves
// to a public address (PLAN §4.8, SSRF). A host that doesn't resolve yet (the model server may
// start later) is returned as a warning; the dial guard in llm checks every connection anyway.
func (cs *Connections) CheckHosts(ctx context.Context, r *net.Resolver) (warnings []string, err error) {
	if cs == nil {
		return nil, nil
	}
	var bad []string
	for _, c := range cs.all() {
		if c.Cloud {
			continue
		}
		ok, rerr := llm.CheckHost(ctx, r, c.BaseURL.Hostname())
		switch {
		case rerr != nil:
			warnings = append(warnings, fmt.Sprintf("assistant connection %q: %s doesn't resolve yet", c.Name, c.BaseURL.Hostname()))
		case !ok:
			bad = append(bad, fmt.Sprintf("assistant connection %q: %s resolves to a public address; mark it as a cloud connection if that is intended", c.Name, c.BaseURL.Hostname()))
		}
	}
	if len(bad) > 0 {
		return warnings, errors.New(strings.Join(bad, "; "))
	}
	return warnings, nil
}

// StatusOf checks one connection, also one switched off (Administration).
func (cs *Connections) StatusOf(ctx context.Context, name string, refresh bool) (ConnectionStatus, error) {
	c, err := cs.GetAny(name)
	if err != nil {
		return ConnectionStatus{}, err
	}
	return cs.status(ctx, c, refresh), nil
}

// Status returns the state of every connection switched on, checking those whose cached state
// expired (in parallel, one check per connection at a time). refresh ignores the cache.
func (cs *Connections) Status(ctx context.Context, refresh bool) []ConnectionStatus {
	list := cs.enabled()
	if len(list) == 0 {
		return nil
	}
	out := make([]ConnectionStatus, len(list))
	var wg sync.WaitGroup
	for i, c := range list {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i] = cs.status(ctx, c, refresh)
		}()
	}
	wg.Wait()
	return out
}

func (cs *Connections) status(ctx context.Context, c *Connection, refresh bool) ConnectionStatus {
	cs.mu.Lock()
	if e, ok := cs.cache[c.Name]; ok && !refresh && cs.clock.Now().Before(e.expires) {
		cs.mu.Unlock()
		return cs.withLearned(e.st)
	}
	if f, ok := cs.flights[c.Name]; ok {
		cs.mu.Unlock()
		select {
		case <-f.done:
			return cs.withLearned(f.st)
		case <-ctx.Done():
			return ConnectionStatus{Name: c.Name, Label: c.Label, API: c.API, Cloud: c.Cloud, DefaultModel: cs.preferred(c),
				Err: &llm.Error{Kind: llm.KindTimeout, Message: "the check was cancelled"}}
		}
	}
	f := &flight{done: make(chan struct{})}
	cs.flights[c.Name] = f
	cs.mu.Unlock()

	// the check outlives a cancelled request so the other waiters still get an answer
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), statusTimeout)
	st := cs.check(cctx, c)
	cancel()

	cs.mu.Lock()
	ttl := statusTTL
	if !st.Reachable {
		ttl = statusErrorTTL
	}
	cs.cache[c.Name] = statusEntry{st: st, expires: cs.clock.Now().Add(ttl)}
	f.st = st
	delete(cs.flights, c.Name)
	close(f.done)
	cs.mu.Unlock()
	return cs.withLearned(st)
}

// preferred is the model a connection's questions use: the administrator's choice, else the
// connection's own default.
func (cs *Connections) preferred(c *Connection) string {
	if m := cs.ControlsOf(c.Name).DefaultModel; m != "" {
		return m
	}
	return c.DefaultModel
}

// check reads a connection's model list: chat models only (no embeddings), limited to the
// connection's allowlist, each with its detected capabilities, and for a server that says which
// models are loaded (Ollama, LM Studio), that too.
func (cs *Connections) check(ctx context.Context, c *Connection) ConnectionStatus {
	st := ConnectionStatus{Name: c.Name, Label: c.Label, API: c.API, Cloud: c.Cloud, Server: llm.ServerOther}
	t0 := cs.clock.Now()
	models, err := c.Client.Models(ctx)
	st.Latency = cs.clock.Now().Sub(t0)
	if err != nil {
		var le *llm.Error
		if !errors.As(err, &le) {
			le = &llm.Error{Kind: llm.KindUnreachable, Message: err.Error()}
		}
		st.Err = le
		st.DefaultModel = cs.preferred(c)
		return st
	}
	st.Reachable = true
	loaded := map[string]bool{}
	if m, ok := c.Client.(llm.Manager); ok {
		st.Server = m.Server()
		if st.Server != llm.ServerOther {
			if list, lerr := m.Loaded(ctx); lerr == nil {
				st.Loaded, st.LoadedKnown = list, true
				for _, l := range list {
					loaded[l.ID] = true
				}
			}
		}
	}
	var chat []llm.Model
	for _, m := range models {
		if !llm.IsEmbedding(m) && c.offers(m.ID) {
			chat = append(chat, m)
			info := ModelInfo{ID: m.ID, Caps: llm.DetectCaps(c.API, m, c.ContextTokens, c.Caps)}
			if st.LoadedKnown {
				l := loaded[m.ID]
				info.Loaded = &l
			}
			st.Models = append(st.Models, info)
		}
	}
	st.DefaultModel = llm.DefaultModel(chat, cs.preferred(c))
	if p := cs.ControlsOf(c.Name).PhotoModel; p != "" && c.offers(p) {
		st.PhotoModel = p
	}
	return st
}

// Forget drops a connection's cached state, so the next check reads the server again (after
// loading or unloading a model).
func (cs *Connections) Forget(name string) {
	cs.mu.Lock()
	delete(cs.cache, name)
	cs.mu.Unlock()
}

// KeepWarm moves the model's unload time on Ollama to the connection's idle time, in the
// background, after a question or digest: the request itself leaves Ollama's own keep_alive. Ollama
// then unloads the model by itself once nothing used it that long, so the api replicas and the
// worker never need to agree on a timer.
func (cs *Connections) KeepWarm(ctx context.Context, c *Connection, model string) {
	d := cs.ControlsOf(c.Name).UnloadAfter
	m, ok := c.Client.(llm.Manager)
	if d <= 0 || !ok || model == "" || m.Server() != llm.ServerOllama {
		return
	}
	// the question's request may be over (or cancelled) by now; the reset still goes out
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), keepWarmTimeout)
	go func() {
		defer cancel()
		_ = m.Load(ctx, model, d)
	}()
}

// LoadModel loads one of the connection's offered models now (Ollama), kept for its idle time.
func (cs *Connections) LoadModel(ctx context.Context, name, model string) error {
	c, m, err := cs.Manager(ctx, name)
	if err != nil {
		return err
	}
	if m == nil {
		return llm.ErrCannotManage
	}
	if !c.offers(model) {
		return fmt.Errorf("%w: %q", ErrUnknownModel, model)
	}
	defer cs.Forget(name)
	return m.Load(ctx, model, cs.ControlsOf(name).UnloadAfter)
}

// UnloadModel frees a model the server holds now (Ollama), offered or not: it is the
// administrator's server, and any loaded model takes its memory.
func (cs *Connections) UnloadModel(ctx context.Context, name, model string) error {
	_, m, err := cs.Manager(ctx, name)
	if err != nil {
		return err
	}
	if m == nil {
		return llm.ErrCannotManage
	}
	defer cs.Forget(name)
	return m.Unload(ctx, model)
}

// UnloadAll frees every model the server holds (a connection switched off); a server that can't
// unload from here is left as it is.
func (cs *Connections) UnloadAll(ctx context.Context, name string) error {
	_, m, err := cs.Manager(ctx, name)
	if err != nil || m == nil {
		return err
	}
	defer cs.Forget(name)
	loaded, err := m.Loaded(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, l := range loaded {
		errs = append(errs, m.Unload(ctx, l.ID))
	}
	return errors.Join(errs...)
}

// Manager returns a connection's model manager, after a check so the server is known; nil when
// the server can't load or unload models from here.
func (cs *Connections) Manager(ctx context.Context, name string) (*Connection, llm.Manager, error) {
	c, err := cs.GetAny(name)
	if err != nil {
		return nil, nil, err
	}
	cs.status(ctx, c, false)
	m, ok := c.Client.(llm.Manager)
	if !ok || m.Server() != llm.ServerOllama {
		return c, nil, nil
	}
	return c, m, nil
}

// offers applies the connection's model allowlist (case-insensitive globs).
func (c *Connection) offers(id string) bool {
	if len(c.Models) == 0 {
		return true
	}
	for _, p := range c.Models {
		if ok, _ := path.Match(strings.ToLower(p), strings.ToLower(id)); ok {
			return true
		}
	}
	return false
}

// Resolve picks the model for a run: the requested one, else the user's saved one, else the
// connection's default. A model the server didn't list is still allowed when the list couldn't be
// read (the run then reports the server's own error) but never outside the allowlist.
func (cs *Connections) Resolve(ctx context.Context, conn, model string) (*Connection, ModelInfo, error) {
	c, err := cs.Get(conn)
	if err != nil {
		return nil, ModelInfo{}, err
	}
	st := cs.status(ctx, c, false)
	if model == "" {
		model = st.DefaultModel
	}
	if model == "" {
		if st.Err != nil {
			return nil, ModelInfo{}, st.Err
		}
		return nil, ModelInfo{}, &llm.Error{Kind: llm.KindNotFound, Message: "the server offers no chat model; load one first"}
	}
	if !c.offers(model) {
		return nil, ModelInfo{}, fmt.Errorf("%w: %q", ErrUnknownModel, model)
	}
	if i := slices.IndexFunc(st.Models, func(m ModelInfo) bool { return m.ID == model }); i >= 0 {
		return c, cs.tuned(c, st.Models[i]), nil
	}
	if st.Reachable {
		return nil, ModelInfo{}, fmt.Errorf("%w: %q", ErrUnknownModel, model)
	}
	return c, cs.tuned(c, cs.learnedInfo(c, ModelInfo{ID: model, Caps: llm.DetectCaps(c.API, llm.Model{ID: model}, c.ContextTokens, c.Caps)})), nil
}

// tuned adds an administrator's tuning; its context size replaces the detected one.
func (cs *Connections) tuned(c *Connection, m ModelInfo) ModelInfo {
	if t := cs.tuningOf(c.Name, m.ID); t != nil {
		m.Tuning = t
		if t.ContextTokens != nil {
			m.Caps.ContextTokens = *t.ContextTokens
		}
	}
	return m
}

// LearnNoTools remembers that a model rejected tool definitions, so later runs in this process
// start in plan mode without a failed request.
func (cs *Connections) LearnNoTools(conn, model string, caps llm.Caps) {
	caps.Tools = false
	cs.mu.Lock()
	cs.learned[conn+"\x00"+model] = caps
	cs.mu.Unlock()
}

func (cs *Connections) withLearned(st ConnectionStatus) ConnectionStatus {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if len(cs.learned) == 0 {
		return st
	}
	models := slices.Clone(st.Models)
	for i, m := range models {
		if l, ok := cs.learned[st.Name+"\x00"+m.ID]; ok {
			models[i].Caps = l
		}
	}
	st.Models = models
	return st
}

func (cs *Connections) learnedInfo(c *Connection, m ModelInfo) ModelInfo {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if l, ok := cs.learned[c.Name+"\x00"+m.ID]; ok {
		m.Caps = l
	}
	return m
}
