package assistant

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/config"
	"github.com/x2db736ac85h/finuance/backend/internal/llm"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// fakeServer answers Models from a list or an error and counts the calls.
type fakeServer struct {
	api    llm.API
	models []llm.Model
	err    error
	calls  atomic.Int32
	block  chan struct{}
}

func (f *fakeServer) API() llm.API { return f.api }
func (f *fakeServer) Models(context.Context) ([]llm.Model, error) {
	f.calls.Add(1)
	if f.block != nil {
		<-f.block
	}
	return f.models, f.err
}
func (f *fakeServer) Chat(context.Context, llm.Request, func(llm.Delta)) (llm.Result, error) {
	return llm.Result{}, nil
}

func conn(name, base string, mod func(*config.AIConnection)) config.AIConnection {
	u, _ := url.Parse(base)
	c := config.AIConnection{Name: name, Label: name, API: llm.OpenAI, BaseURL: u, ContextTokens: 8192}
	if mod != nil {
		mod(&c)
	}
	return c
}

func newTestConnections(clock *fakeClock, servers map[string]*fakeServer, cfgs ...config.AIConnection) *Connections {
	return NewConnections(cfgs, time.Minute, 0, clock, func(c llm.Config) llm.Client {
		for i := range cfgs {
			if cfgs[i].BaseURL.String() == c.BaseURL {
				return servers[cfgs[i].Name]
			}
		}
		panic("no server for " + c.BaseURL)
	})
}

func TestStatusFiltersAndCaches(t *testing.T) {
	f := false
	clock := &fakeClock{t: time.Date(2025, 3, 20, 12, 0, 0, 0, time.UTC)}
	lm := &fakeServer{models: []llm.Model{{ID: "text-embedding-nomic"}, {ID: "gemma-3-12b", ContextTokens: 32768}, {ID: "qwen3-14b"}, {ID: "llama-3.1-8b"}}}
	down := &fakeServer{err: &llm.Error{Kind: llm.KindUnreachable, Message: "connection refused"}}
	cs := newTestConnections(clock, map[string]*fakeServer{"lm": lm, "down": down},
		conn("lm", "http://10.0.0.2:1234/v1", func(c *config.AIConnection) {
			c.Models = []string{"Gemma-*", "qwen*", "text-*"}
			c.Caps = []llm.CapsOverride{{Pattern: "gemma-*", Tools: &f}}
		}),
		conn("down", "http://10.0.0.3:11434/v1", func(c *config.AIConnection) { c.DefaultModel = "llama3.1:8b" }))

	st := cs.Status(context.Background(), false)
	if len(st) != 2 {
		t.Fatalf("status = %+v", st)
	}
	want := []ModelInfo{
		{ID: "gemma-3-12b", Caps: llm.Caps{Tools: false, Reasoning: llm.ReasonNone, ContextTokens: 32768, Vision: true}},
		{ID: "qwen3-14b", Caps: llm.Caps{Tools: true, Reasoning: llm.ReasonSwitch, ContextTokens: 8192}},
	}
	if !st[0].Reachable || st[0].DefaultModel != "qwen3-14b" || len(st[0].Models) != 2 || st[0].Models[0] != want[0] || st[0].Models[1] != want[1] {
		t.Errorf("lm = %+v", st[0])
	}
	if st[1].Reachable || st[1].Err.Kind != llm.KindUnreachable || st[1].DefaultModel != "llama3.1:8b" {
		t.Errorf("down = %+v", st[1])
	}
	// cached: a success for 60 s, a failure for 5 s
	clock.add(6 * time.Second)
	cs.Status(context.Background(), false)
	if lm.calls.Load() != 1 || down.calls.Load() != 2 {
		t.Errorf("calls lm %d down %d", lm.calls.Load(), down.calls.Load())
	}
	clock.add(55 * time.Second)
	cs.Status(context.Background(), false)
	if lm.calls.Load() != 2 {
		t.Errorf("lm not checked again after 60 s: %d", lm.calls.Load())
	}
	cs.Status(context.Background(), true)
	if lm.calls.Load() != 3 {
		t.Error("refresh ignored the cache")
	}
}

func TestStatusChecksOnceForConcurrentCallers(t *testing.T) {
	clock := &fakeClock{t: time.Now()}
	srv := &fakeServer{models: []llm.Model{{ID: "qwen3-8b"}}, block: make(chan struct{})}
	cs := newTestConnections(clock, map[string]*fakeServer{"a": srv}, conn("a", "http://127.0.0.1:1234/v1", nil))
	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() { defer wg.Done(); cs.Status(context.Background(), false) }()
	}
	time.Sleep(50 * time.Millisecond)
	close(srv.block)
	wg.Wait()
	if srv.calls.Load() != 1 {
		t.Errorf("calls = %d", srv.calls.Load())
	}
}

func TestResolve(t *testing.T) {
	clock := &fakeClock{t: time.Now()}
	up := &fakeServer{models: []llm.Model{{ID: "qwen3-14b"}, {ID: "gpt-oss-20b"}}}
	down := &fakeServer{err: &llm.Error{Kind: llm.KindUnreachable, Message: "refused"}}
	empty := &fakeServer{}
	cs := newTestConnections(clock, map[string]*fakeServer{"up": up, "down": down, "empty": empty},
		conn("up", "http://10.0.0.2/v1", nil),
		conn("down", "http://10.0.0.3/v1", func(c *config.AIConnection) { c.Models = []string{"llama*"} }),
		conn("empty", "http://10.0.0.4/v1", nil))
	ctx := context.Background()

	c, m, err := cs.Resolve(ctx, "", "")
	if err != nil || c.Name != "up" || m.ID != "qwen3-14b" {
		t.Errorf("default: %v %+v %v", c, m, err)
	}
	if _, m, _ := cs.Resolve(ctx, "up", "gpt-oss-20b"); m.Caps.Reasoning != llm.ReasonEffort {
		t.Errorf("caps = %+v", m)
	}
	if _, _, err := cs.Resolve(ctx, "up", "mistral"); !errors.Is(err, ErrUnknownModel) {
		t.Errorf("unlisted model on a reachable server: %v", err)
	}
	if _, _, err := cs.Resolve(ctx, "nope", ""); !errors.Is(err, ErrUnknownConnection) {
		t.Errorf("unknown connection: %v", err)
	}
	// a server that is down: an allowed model is tried (the run reports the server's error) …
	if c, m, err := cs.Resolve(ctx, "down", "llama3.1:8b"); err != nil || c.Name != "down" || !m.Caps.Tools {
		t.Errorf("down, allowed: %v %+v %v", c, m, err)
	}
	// … but never one outside the allowlist, and without any model the error is the server's
	if _, _, err := cs.Resolve(ctx, "down", "qwen3"); !errors.Is(err, ErrUnknownModel) {
		t.Errorf("down, not allowed: %v", err)
	}
	if _, _, err := cs.Resolve(ctx, "down", ""); llm.KindOf(err) != llm.KindUnreachable {
		t.Errorf("down, no model: %v", err)
	}
	if _, _, err := cs.Resolve(ctx, "empty", ""); llm.KindOf(err) != llm.KindNotFound || !strings.Contains(err.Error(), "load one") {
		t.Errorf("no models: %v", err)
	}

	// learning: a model that rejected tools is plan-only from then on, in status and resolve
	_, m, _ = cs.Resolve(ctx, "up", "qwen3-14b")
	cs.LearnNoTools("up", "qwen3-14b", m.Caps)
	if _, m, _ := cs.Resolve(ctx, "up", "qwen3-14b"); m.Caps.Tools || m.Caps.Reasoning != llm.ReasonSwitch {
		t.Errorf("learned = %+v", m)
	}
	if st := cs.Status(ctx, false); st[0].Models[0].Caps.Tools || !st[0].Models[1].Caps.Tools {
		t.Errorf("status after learning = %+v", st[0].Models)
	}
}

func TestCheckHosts(t *testing.T) {
	clock := &fakeClock{t: time.Now()}
	cs := NewConnections([]config.AIConnection{
		conn("local", "http://127.0.0.1:1234/v1", nil),
		conn("cloud", "https://api.openai.com/v1", func(c *config.AIConnection) { c.Cloud = true }),
		conn("nowhere", "http://does-not-exist.invalid:1234/v1", nil),
	}, time.Minute, 0, clock, nil)
	warn, err := cs.CheckHosts(context.Background(), nil)
	if err != nil || len(warn) != 1 || !strings.Contains(warn[0], "does-not-exist.invalid") {
		t.Errorf("warn %v err %v", warn, err)
	}
	cs = NewConnections([]config.AIConnection{conn("pub", "http://8.8.8.8/v1", nil)}, time.Minute, 0, clock, nil)
	if _, err := cs.CheckHosts(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "public address") {
		t.Errorf("public: %v", err)
	}
	var none *Connections
	if none.Enabled() || len(none.Status(context.Background(), false)) != 0 {
		t.Error("nil connections")
	}
}

// TestIdleTimeout: FINUANCE_AI_IDLE_TIMEOUT reaches the model client, at most the request timeout,
// and 60 s when unset (a slow GPU reading a long prompt stays silent longer than that).
func TestIdleTimeout(t *testing.T) {
	cases := []struct{ timeout, idle, want time.Duration }{
		{5 * time.Minute, 3 * time.Minute, 3 * time.Minute},
		{time.Minute, 3 * time.Minute, time.Minute},
		{5 * time.Minute, 0, 60 * time.Second},
	}
	for _, c := range cases {
		var got llm.Config
		NewConnections([]config.AIConnection{conn("local", "http://127.0.0.1:11434/v1", nil)}, c.timeout, c.idle,
			&fakeClock{}, func(cfg llm.Config) llm.Client { got = cfg; return &fakeServer{} })
		if got.Timeout != c.timeout || got.IdleTimeout != c.want {
			t.Errorf("timeout %s, idle %s: client got %s / %s, want idle %s", c.timeout, c.idle, got.Timeout, got.IdleTimeout, c.want)
		}
	}
}
