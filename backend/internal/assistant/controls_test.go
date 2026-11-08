package assistant

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/config"
	"github.com/x2db736ac85h/finuance/backend/internal/llm"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/testutil/llmmock"
)

// fakeManager is a fakeServer that loads and unloads like Ollama and records what it was asked.
type fakeManager struct {
	*fakeServer
	server llm.Server

	mu       sync.Mutex
	loaded   []string
	loads    []string // "model keep"
	unloads  []string
	loadDone chan struct{}
}

func (f *fakeManager) Server() llm.Server { return f.server }
func (f *fakeManager) Loaded(context.Context) ([]llm.LoadedModel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]llm.LoadedModel, 0, len(f.loaded))
	for _, id := range f.loaded {
		out = append(out, llm.LoadedModel{ID: id, SizeBytes: 1 << 30})
	}
	return out, nil
}
func (f *fakeManager) Load(_ context.Context, model string, keep time.Duration) error {
	f.mu.Lock()
	f.loads = append(f.loads, model+" "+keep.String())
	if !slices.Contains(f.loaded, model) {
		f.loaded = append(f.loaded, model)
	}
	f.mu.Unlock()
	if f.loadDone != nil {
		f.loadDone <- struct{}{}
	}
	return nil
}
func (f *fakeManager) Unload(_ context.Context, model string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unloads = append(f.unloads, model)
	f.loaded = slices.DeleteFunc(f.loaded, func(m string) bool { return m == model })
	return nil
}

func newManagedConnections(clock *fakeClock, clients map[string]llm.Client, cfgs ...config.AIConnection) *Connections {
	return NewConnections(cfgs, time.Minute, 0, clock, func(c llm.Config) llm.Client {
		for i := range cfgs {
			if cfgs[i].BaseURL.String() == c.BaseURL {
				return clients[cfgs[i].Name]
			}
		}
		panic("no server for " + c.BaseURL)
	})
}

// TestControls: a connection switched off is gone for people (status, Get, the default) but still
// there for Administration; the administrator's default model and model for photos show in the
// status, a model for photos only when the connection offers it.
func TestControls(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)}
	a := &fakeServer{models: []llm.Model{{ID: "qwen3-4b"}, {ID: "qwen3-vl-4b"}}}
	b := &fakeServer{models: []llm.Model{{ID: "llama-3"}}}
	cs := newTestConnections(clock, map[string]*fakeServer{"a": a, "b": b},
		conn("a", "http://10.0.0.2:11434/v1", func(c *config.AIConnection) { c.Models = []string{"qwen3-*"} }),
		conn("b", "http://10.0.0.3:1234/v1", nil))
	ctx := context.Background()

	if c := cs.ControlsOf("a"); c != DefaultControls || c.UnloadAfter != 30*time.Minute || !c.Enabled {
		t.Errorf("controls before any save = %+v", c)
	}
	cs.SetStored(nil, nil, map[string]Controls{"a": {Enabled: false, Version: 1}})
	if st := cs.Status(ctx, false); len(st) != 1 || st[0].Name != "b" {
		t.Errorf("status with a off = %+v", st)
	}
	if _, err := cs.Get("a"); !errors.Is(err, ErrUnknownConnection) {
		t.Errorf("Get(a) off: %v", err)
	}
	if c, err := cs.Get(""); err != nil || c.Name != "b" {
		t.Errorf("default with a off = %v %v", c, err)
	}
	if c, err := cs.GetAny("a"); err != nil || c.Name != "a" {
		t.Errorf("GetAny(a) = %v %v", c, err)
	}
	if st, err := cs.StatusOf(ctx, "a", false); err != nil || !st.Reachable {
		t.Errorf("StatusOf(a) = %+v %v", st, err)
	}
	if !cs.Disabled("a") || cs.Disabled("b") || cs.Disabled("") {
		t.Error("Disabled")
	}
	cs.SetStored(nil, nil, map[string]Controls{"a": {Enabled: false, Version: 1}, "b": {Enabled: false, Version: 1}})
	if cs.Enabled() {
		t.Error("Enabled with every connection off")
	}

	// back on, with a default model and a model for photos; a new version drops the cached list
	cs.SetStored(nil, nil, map[string]Controls{"a": {Enabled: true, DefaultModel: "qwen3-vl-4b", PhotoModel: "qwen3-vl-4b", Version: 2}})
	st := cs.Status(ctx, false)
	if len(st) != 2 || st[0].DefaultModel != "qwen3-vl-4b" || st[0].PhotoModel != "qwen3-vl-4b" {
		t.Errorf("status = %+v", st)
	}
	cs.SetStored(nil, nil, map[string]Controls{"a": {Enabled: true, PhotoModel: "llama-3", Version: 3}})
	if st := cs.Status(ctx, false); st[0].PhotoModel != "" || st[0].DefaultModel != "qwen3-4b" {
		t.Errorf("a photo model the connection doesn't offer: %+v", st[0])
	}
}

// TestModelManagement: loaded models show in the status; after a question the model's unload time
// moves to the connection's idle time on Ollama only; administrators load offered models and
// unload any loaded one; switching off unloads everything.
func TestModelManagement(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)}
	ollama := &fakeManager{fakeServer: &fakeServer{models: []llm.Model{{ID: "qwen3-4b"}, {ID: "qwen3-vl-4b"}, {ID: "gemma3-4b"}}},
		server: llm.ServerOllama, loaded: []string{"qwen3-4b"}, loadDone: make(chan struct{}, 4)}
	other := &fakeManager{fakeServer: &fakeServer{models: []llm.Model{{ID: "llama-3"}}}, server: llm.ServerOther, loadDone: make(chan struct{}, 4)}
	cs := newManagedConnections(clock, map[string]llm.Client{"ollama": ollama, "vllm": other},
		conn("ollama", "http://10.0.0.2:11434/v1", func(c *config.AIConnection) { c.Models = []string{"qwen3*"} }),
		conn("vllm", "http://10.0.0.3:8000/v1", nil))
	ctx := context.Background()

	st, _ := cs.StatusOf(ctx, "ollama", false)
	if !st.LoadedKnown || len(st.Loaded) != 1 || st.Models[0].Loaded == nil || !*st.Models[0].Loaded || *st.Models[1].Loaded {
		t.Errorf("loaded state = %+v %+v", st.Loaded, st.Models)
	}
	if st, _ := cs.StatusOf(ctx, "vllm", false); st.LoadedKnown || st.Models[0].Loaded != nil {
		t.Errorf("a server that doesn't say: %+v", st)
	}

	c, _ := cs.Get("ollama")
	cs.KeepWarm(ctx, c, "qwen3-4b")
	<-ollama.loadDone
	cs.SetStored(nil, nil, map[string]Controls{"ollama": {Enabled: true, UnloadAfter: 10 * time.Minute, Version: 1}})
	cs.KeepWarm(ctx, c, "qwen3-4b")
	<-ollama.loadDone
	cs.SetStored(nil, nil, map[string]Controls{"ollama": {Enabled: true, Version: 2}}) // the server's own setting
	cs.KeepWarm(ctx, c, "qwen3-4b")
	v, _ := cs.Get("vllm")
	cs.KeepWarm(ctx, v, "llama-3")
	time.Sleep(50 * time.Millisecond)
	ollama.mu.Lock()
	if want := []string{"qwen3-4b 30m0s", "qwen3-4b 10m0s"}; !slices.Equal(ollama.loads, want) {
		t.Errorf("unload times moved: %v, want %v", ollama.loads, want)
	}
	ollama.mu.Unlock()
	if len(other.loads) != 0 {
		t.Errorf("a server that isn't Ollama was asked to load: %v", other.loads)
	}

	if err := cs.LoadModel(ctx, "ollama", "gemma3-4b"); !errors.Is(err, ErrUnknownModel) {
		t.Errorf("loading a model not offered: %v", err)
	}
	if err := cs.LoadModel(ctx, "ollama", "qwen3-vl-4b"); err != nil {
		t.Fatal(err)
	}
	<-ollama.loadDone
	if err := cs.LoadModel(ctx, "vllm", "llama-3"); !errors.Is(err, llm.ErrCannotManage) {
		t.Errorf("loading on a server that can't: %v", err)
	}
	if err := cs.UnloadModel(ctx, "ollama", "qwen3-4b"); err != nil {
		t.Fatal(err)
	}
	if st, _ := cs.StatusOf(ctx, "ollama", false); len(st.Loaded) != 1 || st.Loaded[0].ID != "qwen3-vl-4b" {
		t.Errorf("after load and unload, loaded = %+v (the cache is dropped)", st.Loaded)
	}
	if err := cs.UnloadAll(ctx, "ollama"); err != nil {
		t.Fatal(err)
	}
	ollama.mu.Lock()
	defer ollama.mu.Unlock()
	if len(ollama.loaded) != 0 || !slices.Equal(ollama.unloads, []string{"qwen3-4b", "qwen3-vl-4b"}) {
		t.Errorf("after unloading everything: loaded %v, unloads %v", ollama.loaded, ollama.unloads)
	}
}

// TestRunModelChoice: when the administrator picks, a question runs on the default connection and
// its default model whatever it asks for (digests go through the same Prepare); a connection
// switched off falls back to the first one on; a question with photos goes to the connection's
// model for photos when the chosen model can't read images.
func TestRunModelChoice(t *testing.T) {
	re := newRunEnv(t, 2)
	conns := re.runner.cfg.Connections
	model := func(chat uuid.UUID) (string, string) {
		c, _, err := re.chats.Get(re.ctx, re.user, chat)
		if err != nil {
			t.Fatal(err)
		}
		return c.Connection, c.Model
	}

	re.runner.cfg.Choice = func(context.Context) Choice { return Choice{Connection: "claude"} }
	conns.SetStored(nil, nil, map[string]Controls{"claude": {Enabled: true, DefaultModel: "mock-text-tools", Version: 1}})
	chat := re.newChat()
	re.mock.Push(llmmock.Reply{Content: "Locked."})
	re.ask(chat, "Which model?", nil) // asks for mock / mock-tools
	if c, m := model(chat); c != "claude" || m != "mock-text-tools" {
		t.Errorf("locked: ran on %s / %s, want claude / mock-text-tools", c, m)
	}

	// each person picks again: their choice counts; a connection switched off falls back
	re.runner.cfg.Choice = func(context.Context) Choice { return Choice{People: true} }
	chat = re.newChat()
	re.mock.Push(llmmock.Reply{Content: "Open."})
	re.ask(chat, "Which model?", nil)
	if c, m := model(chat); c != "mock" || m != "mock-tools" {
		t.Errorf("open: ran on %s / %s", c, m)
	}
	conns.SetStored(nil, nil, map[string]Controls{"mock": {Enabled: false, Version: 1}})
	chat = re.newChat()
	re.mock.Push(llmmock.Reply{Content: "Fallback."})
	re.ask(chat, "Which model?", nil)
	if c, _ := model(chat); c != "claude" {
		t.Errorf("mock switched off: ran on %s, want claude (the first one on)", c)
	}

	// photos: mock-tools can't read images, the model for photos can
	conns.SetStored(nil, nil, map[string]Controls{"mock": {Enabled: true, PhotoModel: "mock-vl", Version: 2}})
	chat = re.newChat()
	a, err := re.chats.AddAttachment(re.ctx, re.user, chat, testPhoto(t))
	if err != nil {
		t.Fatal(err)
	}
	re.mock.Push(llmmock.Reply{Content: "A receipt."})
	re.ask(chat, "What is on this receipt?", func(r *Request) { r.AttachmentIDs = []uuid.UUID{a.ID} })
	if c, m := model(chat); c != "mock" || m != "mock-vl" {
		t.Errorf("photo: ran on %s / %s, want mock / mock-vl", c, m)
	}
}

// TestSetControls: controls are saved for the file's connections too, versioned, validated against
// the connection's models and idle times, read back by List and loaded into the registry.
func TestSetControls(t *testing.T) {
	file := []config.AIConnection{conn("homelab", "http://10.0.0.2:11434/v1", func(c *config.AIConnection) { c.Models = []string{"qwen3*"} })}
	e, conns, s := newStoredEnv(t, nil, file)
	ctx := e.ctx
	ten := 10

	c, err := s.SetControls(ctx, "homelab", ControlsInput{Enabled: false, UnloadAfterMinutes: &ten, DefaultModel: "qwen3-4b", PhotoModel: "qwen3-vl-4b"})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Controls; got.Enabled || got.UnloadAfter != 10*time.Minute || got.DefaultModel != "qwen3-4b" || got.PhotoModel != "qwen3-vl-4b" || got.Version != 1 {
		t.Errorf("saved = %+v", got)
	}
	if !conns.Disabled("homelab") || conns.ControlsOf("homelab").UnloadAfter != 10*time.Minute {
		t.Errorf("registry = %+v", conns.ControlsOf("homelab"))
	}
	// the first save again, and a stale version
	for _, v := range []int32{0, 7} {
		if _, err := s.SetControls(ctx, "homelab", ControlsInput{Enabled: true, Version: v}); !errors.Is(err, platform.ErrStaleVersion) {
			t.Errorf("version %d: %v, want stale", v, err)
		}
	}
	c, err = s.SetControls(ctx, "homelab", ControlsInput{Enabled: true, Version: 1}) // nil: the server's own idle time
	if err != nil || !c.Controls.Enabled || c.Controls.UnloadAfter != 0 || c.Controls.Version != 2 {
		t.Errorf("second save = %+v %v", c.Controls, err)
	}
	list, err := s.List(ctx)
	if err != nil || len(list) != 1 || list[0].Controls.Version != 2 {
		t.Errorf("list = %+v %v", list, err)
	}

	var ve *platform.ValidationError
	seven := 7
	if _, err := s.SetControls(ctx, "homelab", ControlsInput{Enabled: true, UnloadAfterMinutes: &seven, DefaultModel: "llama-3", Version: 2}); !errors.As(err, &ve) ||
		!strings.Contains(err.Error(), "unloadAfterMinutes") || !strings.Contains(err.Error(), "defaultModel") {
		t.Errorf("invalid: %v", err)
	}
	if _, err := s.SetControls(ctx, "nowhere", ControlsInput{Enabled: true}); !errors.Is(err, platform.ErrNotFound) {
		t.Errorf("unknown connection: %v", err)
	}
}
