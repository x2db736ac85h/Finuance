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

// ModelInfo is a chat model a connection offers, with what it can do.
type ModelInfo struct {
	ID   string
	Caps llm.Caps
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

// Connections holds the configured model servers, caches their model lists and remembers what
// was learned about a model while running (a model that rejected tools is plan-only from then on).
type Connections struct {
	list  []*Connection
	clock platform.Clock

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

// NewConnections builds a client per connection. newClient is llm.New in production.
func NewConnections(cfgs []config.AIConnection, timeout time.Duration, clock platform.Clock, newClient func(llm.Config) llm.Client) *Connections {
	if newClient == nil {
		newClient = llm.New
	}
	cs := &Connections{clock: clock, cache: map[string]statusEntry{}, flights: map[string]*flight{}, learned: map[string]llm.Caps{}}
	for i := range cfgs {
		c := &cfgs[i]
		cl := newClient(llm.Config{API: c.API, BaseURL: c.BaseURL.String(), APIKey: c.APIKey, AllowRemote: c.Cloud,
			Timeout: timeout, IdleTimeout: min(timeout, 60*time.Second)})
		cs.list = append(cs.list, &Connection{AIConnection: *c, Client: cl})
	}
	return cs
}

// Enabled reports whether any connection is configured.
func (cs *Connections) Enabled() bool { return cs != nil && len(cs.list) > 0 }

// Get returns a connection by name; "" is the first one.
func (cs *Connections) Get(name string) (*Connection, error) {
	if !cs.Enabled() {
		return nil, ErrUnknownConnection
	}
	if name == "" {
		return cs.list[0], nil
	}
	for _, c := range cs.list {
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
	for _, c := range cs.list {
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
	if !cs.Enabled() {
		return nil
	}
	out := make([]ConnectionStatus, len(cs.list))
	var wg sync.WaitGroup
	for i, c := range cs.list {
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
		return c, st.Models[i], nil
	}
	if st.Reachable {
		return nil, ModelInfo{}, fmt.Errorf("%w: %q", ErrUnknownModel, model)
	}
	return c, cs.learnedInfo(c, ModelInfo{ID: model, Caps: llm.DetectCaps(c.API, llm.Model{ID: model}, c.ContextTokens, c.Caps)}), nil
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
