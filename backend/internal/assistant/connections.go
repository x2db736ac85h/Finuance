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
}

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
	Err          *llm.Error // why the model list couldn't be read
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

	lmu    sync.RWMutex
	file   []*Connection
	stored []*Connection
	tuning map[string]*Tuning // "connection\x00model"

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
		cache: map[string]statusEntry{}, flights: map[string]*flight{}, learned: map[string]llm.Caps{}}
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

// SetStored replaces the connections and tuning stored in the panel (ADR 0035). A connection
// whose settings changed gets a new client and forgets its cached model list and what was learned.
func (cs *Connections) SetStored(conns []config.AIConnection, tuning []Tuning) {
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

// Enabled reports whether any connection is configured.
func (cs *Connections) Enabled() bool { return len(cs.all()) > 0 }

// Get returns a connection by name; "" is the first one.
func (cs *Connections) Get(name string) (*Connection, error) {
	list := cs.all()
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

// Status returns every connection's state, checking those whose cached state expired (in
// parallel, one check per connection at a time). refresh ignores the cache.
func (cs *Connections) Status(ctx context.Context, refresh bool) []ConnectionStatus {
	list := cs.all()
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
			return ConnectionStatus{Name: c.Name, Label: c.Label, API: c.API, Cloud: c.Cloud, DefaultModel: c.DefaultModel,
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

// check reads a connection's model list: chat models only (no embeddings), limited to the
// connection's allowlist, each with its detected capabilities.
func (cs *Connections) check(ctx context.Context, c *Connection) ConnectionStatus {
	st := ConnectionStatus{Name: c.Name, Label: c.Label, API: c.API, Cloud: c.Cloud}
	models, err := c.Client.Models(ctx)
	if err != nil {
		var le *llm.Error
		if !errors.As(err, &le) {
			le = &llm.Error{Kind: llm.KindUnreachable, Message: err.Error()}
		}
		st.Err = le
		st.DefaultModel = c.DefaultModel
		return st
	}
	st.Reachable = true
	var chat []llm.Model
	for _, m := range models {
		if !llm.IsEmbedding(m) && c.offers(m.ID) {
			chat = append(chat, m)
			st.Models = append(st.Models, ModelInfo{ID: m.ID, Caps: llm.DetectCaps(c.API, m, c.ContextTokens, c.Caps)})
		}
	}
	st.DefaultModel = llm.DefaultModel(chat, c.DefaultModel)
	return st
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
