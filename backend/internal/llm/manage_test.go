package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// fakeOllama answers the model list, /api/show, /api/ps and /api/generate like Ollama and records
// the generate requests.
type fakeOllama struct {
	mu        sync.Mutex
	generates []map[string]any
}

func (f *fakeOllama) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"qwen3:4b"},{"id":"qwen3-vl:4b"}]}`))
		case "/api/show":
			_, _ = w.Write([]byte(`{"capabilities":["completion","tools"]}`))
		case "/api/ps":
			_, _ = w.Write([]byte(`{"models":[
				{"name":"qwen3:4b","model":"qwen3:4b","size":3500000000,"size_vram":3400000000,"expires_at":"2026-11-08T03:40:00Z"},
				{"name":"qwen3-vl:4b","model":"qwen3-vl:4b","size":4000000000,"size_vram":0,"expires_at":"2318-02-20T00:00:00Z"}]}`))
		case "/api/generate":
			var b map[string]any
			_ = json.NewDecoder(r.Body).Decode(&b)
			f.mu.Lock()
			f.generates = append(f.generates, b)
			f.mu.Unlock()
			_, _ = w.Write([]byte(`{"model":"qwen3:4b","done":true,"done_reason":"load"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestOllamaManager(t *testing.T) {
	f := &fakeOllama{}
	c := client(OpenAI, f.server(t).URL+"/v1")
	m, ok := c.(Manager)
	if !ok {
		t.Fatal("the OpenAI client is no Manager")
	}
	ctx := context.Background()
	if m.Server() != ServerOther {
		t.Errorf("server before a model list = %q, want other", m.Server())
	}
	if _, err := m.Loaded(ctx); !errors.Is(err, ErrCannotManage) {
		t.Errorf("Loaded before a model list: %v, want ErrCannotManage", err)
	}
	if _, err := c.Models(ctx); err != nil {
		t.Fatal(err)
	}
	if m.Server() != ServerOllama {
		t.Fatalf("server = %q, want ollama", m.Server())
	}
	loaded, err := m.Loaded(ctx)
	if err != nil || len(loaded) != 2 {
		t.Fatalf("loaded %+v, %v", loaded, err)
	}
	if l := loaded[0]; l.ID != "qwen3:4b" || l.SizeBytes != 3500000000 || l.VRAMBytes != 3400000000 ||
		!l.Until.Equal(time.Date(2026, 11, 8, 3, 40, 0, 0, time.UTC)) {
		t.Errorf("first loaded = %+v", l)
	}
	if !loaded[1].Until.IsZero() {
		t.Errorf("a model kept forever unloads at %v, want zero", loaded[1].Until)
	}
	if err := m.Load(ctx, "qwen3:4b", 45*time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := m.Load(ctx, "qwen3:4b", 0); err != nil {
		t.Fatal(err)
	}
	if err := m.Unload(ctx, "qwen3-vl:4b"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	want := []map[string]any{
		{"model": "qwen3:4b", "keep_alive": "45m0s", "stream": false},
		{"model": "qwen3:4b", "stream": false}, // the server's own keep_alive
		{"model": "qwen3-vl:4b", "keep_alive": float64(0), "stream": false},
	}
	if len(f.generates) != len(want) {
		t.Fatalf("generate requests %v", f.generates)
	}
	for i := range want {
		for k, v := range want[i] {
			if f.generates[i][k] != v {
				t.Errorf("request %d: %s = %v, want %v (%v)", i, k, f.generates[i][k], v, f.generates[i])
			}
		}
		if _, ok := f.generates[i]["prompt"]; ok {
			t.Errorf("request %d has a prompt: %v", i, f.generates[i])
		}
	}
	if _, ok := f.generates[1]["keep_alive"]; ok {
		t.Errorf("keep 0 sent a keep_alive: %v", f.generates[1])
	}
}

func TestOllamaWithoutModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[]}`))
		case "/api/version":
			_, _ = w.Write([]byte(`{"version":"0.40.0"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := client(OpenAI, srv.URL+"/v1")
	if _, err := c.Models(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s := c.(Manager).Server(); s != ServerOllama {
		t.Errorf("server = %q, want ollama (told by /api/version)", s)
	}
}

func TestLMStudioManager(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"qwen3-14b"},{"id":"gemma-3-4b"}]}`))
		case "/api/v0/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"qwen3-14b","type":"llm","state":"loaded"},{"id":"gemma-3-4b","type":"vlm","state":"not-loaded"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := client(OpenAI, srv.URL+"/v1")
	ctx := context.Background()
	if _, err := c.Models(ctx); err != nil {
		t.Fatal(err)
	}
	m := c.(Manager)
	if m.Server() != ServerLMStudio {
		t.Fatalf("server = %q, want lmstudio", m.Server())
	}
	loaded, err := m.Loaded(ctx)
	if err != nil || len(loaded) != 1 || loaded[0].ID != "qwen3-14b" {
		t.Errorf("loaded %+v, %v", loaded, err)
	}
	if err := m.Load(ctx, "gemma-3-4b", time.Hour); !errors.Is(err, ErrCannotManage) {
		t.Errorf("Load: %v, want ErrCannotManage", err)
	}
	if err := m.Unload(ctx, "qwen3-14b"); !errors.Is(err, ErrCannotManage) {
		t.Errorf("Unload: %v, want ErrCannotManage", err)
	}
	// the idle time travels with each request as LM Studio's ttl, in seconds
	o := c.(*openAIClient)
	for _, tc := range []struct {
		keep time.Duration
		ttl  any
	}{{30 * time.Minute, float64(1800)}, {0, nil}} {
		raw, err := o.body(Request{Model: "qwen3-14b", Messages: []Message{{Role: RoleUser, Content: "hi"}}, KeepAlive: tc.keep})
		if err != nil {
			t.Fatal(err)
		}
		var b map[string]any
		_ = json.Unmarshal(raw, &b)
		if b["ttl"] != tc.ttl {
			t.Errorf("keep %v: ttl = %v, want %v", tc.keep, b["ttl"], tc.ttl)
		}
	}
}

func TestOtherServerHasNoManager(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			_, _ = w.Write([]byte(`{"data":[{"id":"llama-3"}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	c := client(OpenAI, srv.URL+"/v1")
	ctx := context.Background()
	if _, err := c.Models(ctx); err != nil {
		t.Fatal(err)
	}
	m := c.(Manager)
	if m.Server() != ServerOther {
		t.Errorf("server = %q, want other", m.Server())
	}
	if _, err := m.Loaded(ctx); !errors.Is(err, ErrCannotManage) {
		t.Errorf("Loaded: %v", err)
	}
	if err := m.Load(ctx, "llama-3", time.Hour); !errors.Is(err, ErrCannotManage) {
		t.Errorf("Load: %v", err)
	}
	// an Ollama idle time never goes into a request: Ollama's is moved after the run
	raw, _ := c.(*openAIClient).body(Request{Model: "llama-3", KeepAlive: time.Hour})
	var b map[string]any
	_ = json.Unmarshal(raw, &b)
	if _, ok := b["ttl"]; ok {
		t.Errorf("ttl sent to a server that isn't LM Studio: %s", raw)
	}
}
