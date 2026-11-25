package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// rotating is a token source whose token changes with every Invalidate.
type rotating struct {
	mu          sync.Mutex
	n           int
	asked       int
	invalidated int
	err         error
}

func (r *rotating) Token(context.Context) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.asked++
	if r.err != nil {
		return "", r.err
	}
	return "tok-" + string(rune('a'+r.n)), nil
}

func (r *rotating) Invalidate() {
	r.mu.Lock()
	r.n++
	r.invalidated++
	r.mu.Unlock()
}

const okStream = "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n"

// TestTokenSource (PLAN 26.1): a connection with a token source sends its token as the bearer; a
// 401 drops the token and the request goes once more with a new one; a second 401 is the answer.
func TestTokenSource(t *testing.T) {
	var seen []string
	var mu sync.Mutex
	accept := "tok-a"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get("Authorization"))
		ok := r.Header.Get("Authorization") == "Bearer "+accept
		mu.Unlock()
		if !ok {
			http.Error(w, `{"error":{"message":"the token has expired"}}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(okStream))
	}))
	t.Cleanup(srv.Close)
	src := &rotating{}
	c := New(Config{API: OpenAI, BaseURL: srv.URL, Auth: src, APIKey: "a key that is not used", Timeout: 5 * time.Second})
	req := Request{Model: "m", Messages: []Message{{Role: RoleUser, Content: "x"}}}

	if r, _, err := run(t, c, req); err != nil || r.Content != "hi" {
		t.Fatalf("with a good token: %+v, %v", r, err)
	}
	if len(seen) != 1 || seen[0] != "Bearer tok-a" {
		t.Fatalf("sent %q, want the source's token once (and never the key)", seen)
	}

	// the server stops accepting the token (it ran out, or was revoked): one renewal, one retry
	mu.Lock()
	accept, seen = "tok-b", nil
	mu.Unlock()
	if r, _, err := run(t, c, req); err != nil || r.Content != "hi" {
		t.Fatalf("after the token ran out: %+v, %v", r, err)
	}
	if len(seen) != 2 || seen[0] != "Bearer tok-a" || seen[1] != "Bearer tok-b" || src.invalidated != 1 {
		t.Fatalf("sent %q with %d invalidations, want the old token, then the new one", seen, src.invalidated)
	}

	// a server that refuses the new token too is answered with its 401, not with a loop
	mu.Lock()
	accept, seen = "nothing", nil
	mu.Unlock()
	_, _, err := run(t, c, req)
	if KindOf(err) != KindAuth || len(seen) != 2 {
		t.Fatalf("refused twice: %v after %d requests, want an auth error after 2", err, len(seen))
	}
	if strings.Contains(err.Error(), "tok-") {
		t.Errorf("the error quotes a token: %v", err)
	}
}

// TestTokenSourceFailure: without a token nothing is sent, and the error says credentials are
// missing without the source's secrets.
func TestTokenSourceFailure(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	t.Cleanup(srv.Close)
	src := &rotating{err: errors.New("metadata server: connection refused")}
	c := New(Config{API: OpenAI, BaseURL: srv.URL, Auth: src, Timeout: 5 * time.Second})
	_, _, err := run(t, c, Request{Model: "m", Messages: []Message{{Role: RoleUser, Content: "x"}}})
	var e *Error
	if !errors.As(err, &e) || e.Kind != KindAuth || !strings.Contains(e.Message, "no credentials") || hits.Load() != 0 {
		t.Fatalf("err = %v after %d requests, want an auth error and no request", err, hits.Load())
	}
	if _, err := c.Models(context.Background()); KindOf(err) != KindAuth {
		t.Errorf("Models without a token: %v", err)
	}
}

// TestHostedEndpoint (PLAN 26.3): a provider's endpoint is not probed for LM Studio or Ollama, and
// an endpoint without a model list answers with the connection's own.
func TestHostedEndpoint(t *testing.T) {
	var paths []string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	c := New(Config{API: OpenAI, BaseURL: srv.URL + "/v1", Auth: StaticToken("t"), Hosted: true,
		StaticModels: []string{"google/gemini-3.5-flash-lite", "google/gemini-*", ""}, Timeout: 5 * time.Second})
	models, err := c.Models(context.Background())
	if err != nil || len(models) != 1 || models[0].ID != "google/gemini-3.5-flash-lite" {
		t.Fatalf("models = %+v, %v; want the one id written out (a pattern is not a model)", models, err)
	}
	if len(paths) != 1 || paths[0] != "/v1/models" {
		t.Errorf("requests %q, want the model list alone: nothing beside the base address", paths)
	}
	m, ok := c.(Manager)
	if !ok || m.Server() != ServerOther {
		t.Fatalf("server = %v", m.Server())
	}
	if err := m.Load(context.Background(), "x", 0); !errors.Is(err, ErrCannotManage) {
		t.Errorf("Load on a hosted endpoint: %v", err)
	}

	// an endpoint that does list its models is not probed either
	paths = nil
	listing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-x"}]}`))
	}))
	t.Cleanup(listing.Close)
	c = New(Config{API: OpenAI, BaseURL: listing.URL + "/v1", Hosted: true, Timeout: 5 * time.Second})
	if models, err := c.Models(context.Background()); err != nil || len(models) != 1 {
		t.Fatalf("models = %+v, %v", models, err)
	}
	if len(paths) != 1 {
		t.Errorf("requests %q, want one", paths)
	}
}

// TestTokenGoesOnlyToItsHosts (PLAN 26.2): with an allowlist the token is not asked for, and
// nothing is sent, when the address is another host or not https; and a redirect to another host
// is refused, token or not (the guard of 23.5).
func TestTokenGoesOnlyToItsHosts(t *testing.T) {
	var hits atomic.Int32
	other := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	t.Cleanup(other.Close)
	src := &rotating{}
	req := Request{Model: "m", Messages: []Message{{Role: RoleUser, Content: "x"}}}
	for _, base := range []string{other.URL + "/v1", "http://europe-west4-aiplatform.googleapis.com/v1"} {
		c := New(Config{API: OpenAI, BaseURL: base, Auth: src, TokenHost: GoogleAPIHost, HTTPClient: other.Client(), Timeout: 5 * time.Second})
		if _, _, err := run(t, c, req); err == nil || !strings.Contains(err.Error(), "not sent to that address") {
			t.Errorf("%s: err = %v, want a refusal", base, err)
		}
	}
	if src.asked != 0 || hits.Load() != 0 {
		t.Fatalf("%d tokens asked for, %d requests sent; want none of either", src.asked, hits.Load())
	}

	// a server that redirects elsewhere: the client stops, and the other host hears nothing
	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redirecting.Close)
	c := New(Config{API: OpenAI, BaseURL: redirecting.URL, Auth: src, Timeout: 5 * time.Second})
	if _, _, err := run(t, c, req); err == nil {
		t.Error("a redirect to another host was followed")
	}
	if hits.Load() != 0 {
		t.Errorf("the other host got %d requests", hits.Load())
	}
}

// TestClaudeGenerations (issue 97): which request shape a Claude id takes. The list of models that
// think with a token budget is closed; any other id that is not old, an unknown newer one
// included, is a current model.
func TestClaudeGenerations(t *testing.T) {
	for id, want := range map[string]string{
		"claude-haiku-5-5":           ClaudeCurrent,
		"claude-sonnet-5-5":          ClaudeCurrent,
		"claude-sonnet-5":            ClaudeCurrent,
		"claude-opus-5-5":            ClaudeCurrent,
		"claude-opus-5":              ClaudeCurrent,
		"claude-opus-4-8":            ClaudeCurrent,
		"claude-opus-4-7":            ClaudeCurrent,
		"claude-opus-4-6":            ClaudeCurrent,
		"claude-sonnet-4-6":          ClaudeCurrent,
		"claude-fable-5-2":           ClaudeCurrent,
		"claude-mythos-5-1":          ClaudeCurrent,
		"claude-opus-6":              ClaudeCurrent, // not released: newer, so current
		"claude-haiku-4-5":           ClaudeBudget,
		"claude-haiku-4-5-20251001":  ClaudeBudget,
		"claude-sonnet-4-5":          ClaudeBudget,
		"claude-opus-4-5-20251101":   ClaudeBudget,
		"claude-opus-4-5@20251101":   ClaudeBudget, // as Vertex AI names it
		"claude-opus-4-1":            ClaudeBudget,
		"claude-opus-4-1-20250805":   ClaudeBudget,
		"claude-sonnet-4-20250514":   ClaudeBudget,
		"claude-opus-4":              ClaudeBudget,
		"claude-3-7-sonnet-20250219": ClaudeBudget,
		"claude-3-5-sonnet-20241022": ClaudeOld,
		"claude-3-haiku-20240307":    ClaudeOld,
		"claude-2.1":                 ClaudeOld,
		"qwen3:4b":                   "",
		"google/gemini-3.5-flash":    "",
	} {
		if got := ClaudeGeneration(id); got != want {
			t.Errorf("ClaudeGeneration(%q) = %q, want %q", id, got, want)
		}
		wantReasoning := map[string]Reasoning{ClaudeCurrent: ReasonAdaptive, ClaudeBudget: ReasonBudget, ClaudeOld: ReasonNone}[want]
		if want != "" {
			if c := DetectCaps(Anthropic, Model{ID: id}, 8192, nil); c.Reasoning != wantReasoning {
				t.Errorf("DetectCaps(%q).Reasoning = %q, want %q", id, c.Reasoning, wantReasoning)
			}
		}
	}
}

// TestAnthropicBodyForCurrentModels (issue 97): a current Claude model gets adaptive thinking with
// the level as an effort, no token budget and no temperature; "off" is the lowest effort, never
// "disabled"; and a model of the budget generations keeps its budget.
func TestAnthropicBodyForCurrentModels(t *testing.T) {
	srv, bodies := serve(t, 200, "application/json", "anthropic-nonstream.json")
	temp := 0.2
	ask := func(model string, think Thinking, caps Caps) map[string]any {
		t.Helper()
		*bodies = nil
		req := Request{Model: model, Temperature: &temp, Thinking: think, Caps: caps,
			Tools:    []Tool{{Name: "a", Parameters: json.RawMessage(`{"type":"object"}`)}},
			Messages: []Message{{Role: RoleUser, Content: "Q"}}}
		if _, _, err := run(t, client(Anthropic, srv.URL+"/v1"), req); err != nil {
			t.Fatal(err)
		}
		return (*bodies)[0]
	}
	adaptive := Caps{Reasoning: ReasonAdaptive}
	for think, effort := range map[Thinking]any{ThinkOff: "low", ThinkLow: "low", ThinkMedium: "medium", ThinkHigh: "high", ThinkAuto: nil} {
		b := ask("claude-haiku-5-5", think, adaptive)
		th, _ := b["thinking"].(map[string]any)
		if th["type"] != "adaptive" || th["display"] != "summarized" {
			t.Errorf("thinking %s: thinking = %v, want adaptive with a summary", think, b["thinking"])
		}
		if _, ok := th["budget_tokens"]; ok {
			t.Errorf("thinking %s: a token budget was sent", think)
		}
		var got any
		if oc, ok := b["output_config"].(map[string]any); ok {
			got = oc["effort"]
		}
		if got != effort {
			t.Errorf("thinking %s: effort = %v, want %v", think, got, effort)
		}
		if _, ok := b["temperature"]; ok {
			t.Errorf("thinking %s: a temperature was sent", think)
		}
		if _, ok := b["tool_choice"]; ok {
			t.Errorf("thinking %s: a tool choice was sent (the default, auto, is the only one every model takes)", think)
		}
		if b["max_tokens"] != float64(adaptiveMaxTokens) {
			t.Errorf("thinking %s: max_tokens = %v", think, b["max_tokens"])
		}
	}
	// a current model whose capabilities say "budget" or "none" (an administrator's correction)
	// still gets no budget and no temperature
	for _, caps := range []Caps{{Reasoning: ReasonBudget}, {Reasoning: ReasonNone}} {
		b := ask("claude-opus-5-5", ThinkHigh, caps)
		if b["thinking"] != nil || b["temperature"] != nil {
			t.Errorf("caps %s on a current model: thinking = %v, temperature = %v", caps.Reasoning, b["thinking"], b["temperature"])
		}
	}
	// the budget generations are unchanged: a budget when thinking, the temperature when not
	b := ask("claude-haiku-4-5", ThinkMedium, Caps{Reasoning: ReasonBudget})
	if th := b["thinking"].(map[string]any); th["type"] != "enabled" || th["budget_tokens"] != float64(4096) || b["output_config"] != nil {
		t.Errorf("claude-haiku-4-5 thinking: %v", b)
	}
	if b := ask("claude-haiku-4-5", ThinkOff, Caps{Reasoning: ReasonBudget}); b["thinking"] != nil || b["temperature"] != 0.2 {
		t.Errorf("claude-haiku-4-5 without thinking: thinking = %v, temperature = %v", b["thinking"], b["temperature"])
	}
}
