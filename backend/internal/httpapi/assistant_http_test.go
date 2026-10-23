package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/x2db736ac85h/finuance/backend/internal/config"
	"github.com/x2db736ac85h/finuance/backend/internal/llm"
)

// aiConn is a local OpenAI-compatible connection to a test server.
func aiConn(t *testing.T, name string, srv *httptest.Server, mod func(*config.AIConnection)) config.AIConnection {
	t.Helper()
	u, _ := url.Parse(srv.URL + "/v1")
	c := config.AIConnection{Name: name, Label: name, API: llm.OpenAI, BaseURL: u, ContextTokens: 8192}
	if mod != nil {
		mod(&c)
	}
	return c
}

func TestAssistantStatusHTTP(t *testing.T) {
	var calls atomic.Int32
	lm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"text-embedding-nomic"},{"id":"qwen3-14b"},{"id":"gpt-oss-20b"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer lm.Close()
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid API key"}}`))
	}))
	defer down.Close()
	e := newTestEnv(t, func(c *config.Config) {
		c.AIConnections = []config.AIConnection{
			aiConn(t, "lmstudio", lm, func(c *config.AIConnection) { c.Label = "LM Studio"; c.DefaultModel = "gpt-oss-20b" }),
			aiConn(t, "keyed", down, nil),
		}
	}, false)

	// public: /meta says the assistant is on; status needs a session
	anon := e.browser()
	if m := anon.do("GET", "/meta", nil); m.body["assistantEnabled"] != true {
		t.Errorf("meta = %s", m.raw)
	}
	expect(t, anon.do("GET", "/assistant/status", nil), 401, "unauthenticated")

	b := signedIn(t, e, testEmail(t))
	st := b.do("GET", "/assistant/status", nil)
	expect(t, st, 200, "")
	conns := st.body["connections"].([]any)
	lmc, keyed := conns[0].(map[string]any), conns[1].(map[string]any)
	models := lmc["models"].([]any)
	if st.body["enabled"] != true || num(st.body["maxRowsLimit"]) != 50 || len(conns) != 2 ||
		lmc["label"] != "LM Studio" || lmc["reachable"] != true || lmc["defaultModel"] != "gpt-oss-20b" || lmc["cloud"] != false ||
		len(models) != 2 || models[0].(map[string]any)["reasoning"] != "switch" || models[1].(map[string]any)["reasoning"] != "effort" {
		t.Errorf("status = %s", st.raw)
	}
	if keyed["reachable"] != false || obj(t, keyed, "error")["kind"] != "auth" || len(keyed["models"].([]any)) != 0 {
		t.Errorf("keyed = %v", keyed)
	}
	// cached for the next request; refresh asks again
	b.do("GET", "/assistant/status", nil)
	if calls.Load() != 2 { // /v1/models + LM Studio's /api/v0/models
		t.Errorf("upstream calls = %d", calls.Load())
	}
	b.do("GET", "/assistant/status?refresh=true", nil)
	if calls.Load() != 4 {
		t.Errorf("upstream calls after refresh = %d", calls.Load())
	}
}

func TestAssistantDisabledHTTP(t *testing.T) {
	e := newTestEnv(t, nil, false)
	if m := e.browser().do("GET", "/meta", nil); m.body["assistantEnabled"] != false {
		t.Errorf("meta = %s", m.raw)
	}
	b := signedIn(t, e, testEmail(t))
	st := b.do("GET", "/assistant/status", nil)
	expect(t, st, 200, "")
	if st.body["enabled"] != false || len(st.body["connections"].([]any)) != 0 {
		t.Errorf("status = %s", st.raw)
	}
}
