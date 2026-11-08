package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/x2db736ac85h/finuance/backend/internal/config"
	tu "github.com/x2db736ac85h/finuance/backend/testutil"
	"github.com/x2db736ac85h/finuance/backend/testutil/llmmock"
)

// ollamaStub answers like Ollama: a model list, /api/show, /api/ps from what it holds, and
// /api/generate loading (keep_alive) or unloading (keep_alive 0).
type ollamaStub struct {
	mu        sync.Mutex
	loaded    []string
	keepAlive []any
}

func (o *ollamaStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	o.mu.Lock()
	defer o.mu.Unlock()
	switch r.URL.Path {
	case "/v1/models":
		_, _ = w.Write([]byte(`{"data":[{"id":"qwen3-4b"},{"id":"qwen3-vl-4b"},{"id":"gemma3-4b"}]}`))
	case "/api/show":
		_, _ = w.Write([]byte(`{"capabilities":["completion","tools"]}`))
	case "/api/ps":
		var ms []map[string]any
		for _, m := range o.loaded {
			ms = append(ms, map[string]any{"name": m, "model": m, "size": 3 << 30, "size_vram": 3 << 30, "expires_at": "2026-11-08T13:00:00Z"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"models": ms})
	case "/api/generate":
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		o.keepAlive = append(o.keepAlive, b["keep_alive"])
		m, _ := b["model"].(string)
		if b["keep_alive"] == float64(0) {
			o.loaded = slices.DeleteFunc(o.loaded, func(x string) bool { return x == m })
		} else if !slices.Contains(o.loaded, m) {
			o.loaded = append(o.loaded, m)
		}
		_, _ = w.Write([]byte(`{"done":true}`))
	default:
		http.NotFound(w, r)
	}
}

// TestAssistantControlsHTTP: administrators switch connections on and off, set the idle time and
// models, load and unload on Ollama, and decide who picks the model; people see the result in
// their status; every change is audited.
func TestAssistantControlsHTTP(t *testing.T) {
	stub := &ollamaStub{loaded: []string{"qwen3-4b"}}
	ollama := httptest.NewServer(stub)
	t.Cleanup(ollama.Close)
	mock := httptest.NewServer(llmmock.New())
	t.Cleanup(mock.Close)
	// a server that is neither Ollama nor LM Studio: models can't be loaded from here
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			_, _ = w.Write([]byte(`{"data":[{"id":"llama-3"}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(plain.Close)
	e := newTestEnv(t, func(c *config.Config) {}, false)
	reset := func() {
		if _, err := tu.Require(t, testDB).Owner.Exec(context.Background(),
			`DELETE FROM assistant_connections; DELETE FROM assistant_connection_controls; UPDATE instance_settings SET doc = doc - 'assistant'`); err != nil {
			t.Fatal(err)
		}
	}
	reset()
	t.Cleanup(reset)
	boss, _ := e.adminBrowser(t, "boss-"+testEmail(t))
	user := signedIn(t, e, testEmail(t))
	for _, c := range []map[string]any{
		{"name": "gpu-box", "label": "GPU box", "api": "openai", "baseUrl": ollama.URL + "/v1", "cloud": false, "defaultModel": "",
			"models": []string{"qwen3*"}, "contextTokens": 8192},
		{"name": "lab", "label": "Lab", "api": "openai", "baseUrl": mock.URL + "/v1", "cloud": false, "defaultModel": "mock-tools",
			"models": []string{}, "contextTokens": 8192},
		{"name": "plain", "label": "vLLM", "api": "openai", "baseUrl": plain.URL + "/v1", "cloud": false, "defaultModel": "",
			"models": []string{}, "contextTokens": 8192},
	} {
		expect(t, boss.do("POST", "/admin/assistant/connections", c), 201, "")
	}
	list := boss.do("GET", "/admin/assistant/connections", nil).body2(t)
	ctl := list[0]["controls"].(map[string]any)
	if ctl["enabled"] != true || num(ctl["unloadAfterMinutes"]) != 30 || num(ctl["version"]) != 0 {
		t.Errorf("controls before a save = %v", ctl)
	}

	// only administrators
	in := map[string]any{"enabled": true, "unloadAfterMinutes": 15, "defaultModel": "qwen3-4b", "photoModel": "qwen3-vl-4b", "version": 0}
	expect(t, user.do("PUT", "/admin/assistant/connections/gpu-box/controls", in), 404, "not_found")
	expect(t, user.do("GET", "/admin/assistant/connections/gpu-box/models", nil), 404, "not_found")
	expect(t, user.do("POST", "/admin/assistant/connections/gpu-box/load", map[string]any{"model": "qwen3-4b"}), 404, "not_found")

	r := boss.do("PUT", "/admin/assistant/connections/gpu-box/controls", in)
	expect(t, r, 200, "")
	if c := obj(t, r.body, "controls"); num(c["version"]) != 1 || num(c["unloadAfterMinutes"]) != 15 || c["photoModel"] != "qwen3-vl-4b" {
		t.Errorf("saved = %s", r.raw)
	}
	expect(t, boss.do("PUT", "/admin/assistant/connections/gpu-box/controls", in), 409, "stale_version")
	bad := map[string]any{"enabled": true, "unloadAfterMinutes": 20, "defaultModel": "gemma3-4b", "photoModel": "", "version": 1}
	expect(t, boss.do("PUT", "/admin/assistant/connections/gpu-box/controls", bad), 422, "validation_failed")
	expect(t, boss.do("PUT", "/admin/assistant/connections/nowhere/controls", in), 404, "not_found")

	// the server: Ollama, what it offers and holds; load and unload
	m := boss.do("GET", "/admin/assistant/connections/gpu-box/models?refresh=true", nil)
	expect(t, m, 200, "")
	if m.body["server"] != "ollama" || m.body["canManage"] != true || !strings.Contains(m.raw, `"latencyMs"`) {
		t.Errorf("models = %s", m.raw)
	}
	models := m.body["models"].([]any)
	if len(models) != 2 || models[0].(map[string]any)["loaded"] != true || models[1].(map[string]any)["loaded"] != false {
		t.Errorf("offered models with their state = %s", m.raw)
	}
	expect(t, boss.do("POST", "/admin/assistant/connections/gpu-box/load", map[string]any{"model": "gemma3-4b"}), 422, "validation_failed")
	r = boss.do("POST", "/admin/assistant/connections/gpu-box/load", map[string]any{"model": "qwen3-vl-4b"})
	expect(t, r, 200, "")
	if !strings.Contains(r.raw, `"id":"qwen3-vl-4b","loaded":true`) && !strings.Contains(r.raw, `"loaded":true`) {
		t.Errorf("after load = %s", r.raw)
	}
	expect(t, boss.do("POST", "/admin/assistant/connections/gpu-box/unload", map[string]any{"model": "qwen3-4b"}), 200, "")
	if p := boss.do("GET", "/admin/assistant/connections/plain/models", nil); p.body["server"] != "other" || p.body["canManage"] != false {
		t.Errorf("plain server = %s", p.raw)
	}
	expect(t, boss.do("POST", "/admin/assistant/connections/plain/load", map[string]any{"model": "llama-3"}), 422, "validation_failed")

	// people see what is loaded, the model for photos and how long the check took
	st := user.do("GET", "/assistant/status?refresh=true", nil)
	if st.body["choice"] != "people" || !strings.Contains(st.raw, `"photoModel":"qwen3-vl-4b"`) || !strings.Contains(st.raw, `"loaded":true`) ||
		!strings.Contains(st.raw, `"latencyMs"`) {
		t.Errorf("status = %s", st.raw)
	}

	// switched off: gone for people, its models unloaded
	off := map[string]any{"enabled": false, "unloadAfterMinutes": nil, "defaultModel": "", "photoModel": "", "version": 1}
	expect(t, boss.do("PUT", "/admin/assistant/connections/gpu-box/controls", off), 200, "")
	if st := user.do("GET", "/assistant/status", nil); strings.Contains(st.raw, "gpu-box") || !strings.Contains(st.raw, `"name":"lab"`) ||
		!strings.Contains(st.raw, `"name":"plain"`) {
		t.Errorf("status with gpu-box off = %s", st.raw)
	}
	stub.mu.Lock()
	if len(stub.loaded) != 0 || !slices.Equal(stub.keepAlive, []any{"15m0s", float64(0), float64(0)}) {
		t.Errorf("ollama holds %v after switching off; keep_alive sent %v", stub.loaded, stub.keepAlive)
	}
	stub.mu.Unlock()

	// the administrator picks: only the default connection is listed
	s := boss.do("GET", "/admin/settings", nil)
	doc := map[string]any{"version": s.body["version"], "defaults": map[string]any{}, "features": map[string]any{}, "limits": map[string]any{},
		"assistant": map[string]any{"modelChoice": "admin", "defaultConnection": "lab"}}
	expect(t, boss.do("PUT", "/admin/settings", doc), 200, "")
	on := map[string]any{"enabled": true, "unloadAfterMinutes": 30, "defaultModel": "", "photoModel": "", "version": 2}
	expect(t, boss.do("PUT", "/admin/assistant/connections/gpu-box/controls", on), 200, "")
	st = user.do("GET", "/assistant/status", nil)
	conns := st.body["connections"].([]any)
	if st.body["choice"] != "admin" || len(conns) != 1 || conns[0].(map[string]any)["name"] != "lab" {
		t.Errorf("locked status = %s", st.raw)
	}
	doc["version"], doc["assistant"] = boss.do("GET", "/admin/settings", nil).body["version"], map[string]any{"modelChoice": "anyone"}
	expect(t, boss.do("PUT", "/admin/settings", doc), 422, "validation_failed")

	acts := boss.do("GET", "/me/activity", nil)
	n := 0
	for _, a := range acts.body["events"].([]any) {
		if a.(map[string]any)["action"] == "admin.assistant_changed" {
			n++
		}
	}
	if n != 8 { // 3 connections added, 3 control saves, a load and an unload
		t.Errorf("%d assistant changes audited, want 8", n)
	}
}
