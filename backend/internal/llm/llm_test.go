package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/go-cmp/cmp"
)

// serve answers every request with a fixture file and records the request bodies.
func serve(t *testing.T, status int, contentType, file string) (*httptest.Server, *[]map[string]any) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", file))
	if err != nil {
		t.Fatal(err)
	}
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m map[string]any
		_ = json.NewDecoder(r.Body).Decode(&m)
		bodies = append(bodies, m)
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		_, _ = w.Write(b)
	}))
	t.Cleanup(srv.Close)
	return srv, &bodies
}

func client(api API, url string) Client {
	return New(Config{API: api, BaseURL: url, AllowRemote: true, Timeout: 5 * time.Second, IdleTimeout: 2 * time.Second})
}

func run(t *testing.T, c Client, req Request) (Result, []Delta, error) {
	t.Helper()
	var deltas []Delta
	r, err := c.Chat(context.Background(), req, func(d Delta) { deltas = append(deltas, d) })
	return r, deltas, err
}

func joined(ds []Delta) (content, reasoning string) {
	var c, r strings.Builder
	for _, d := range ds {
		c.WriteString(d.Content)
		r.WriteString(d.Reasoning)
	}
	return c.String(), r.String()
}

// TestRecordedOllamaQwen3 replays two streams recorded with `finuance admin llm-probe` against
// Ollama 0.40 and qwen3:4b on a small GPU (Phase 18.5; the probe's made-up data, never a user's):
// thinking is on by default and streams thousands of characters in Ollama's reasoning field before
// the tool call or the answer. The expected calls and answer were read from the raw stream with jq.
func TestRecordedOllamaQwen3(t *testing.T) {
	cases := []struct {
		name, file, content, reasoningStart string
		reasoningRunes                      int
		calls                               []ToolCall
		stop                                string
	}{
		{name: "tool call after long reasoning", file: "openai-ollama-qwen3-toolcall.sse",
			reasoningStart: "Okay, let's see. The user is asking how much they ", reasoningRunes: 2950,
			calls: []ToolCall{{ID: "call_sqm7qmun", Name: "aggregate_transactions",
				Arguments: `{"category":"Artykuły spożywcze","date_from":"2025-02-01","date_to":"2025-02-28","group_by":"none","type":"expense"}`}},
			stop: "tool_calls"},
		{name: "answer after the tool result", file: "openai-ollama-qwen3-answer.sse",
			content:        "In February 2025 (2025-02-01 to 2025-02-28), you spent **1095.12 PLN** on groceries across 58 transactions.",
			reasoningStart: "Okay, let's see. The user asked how much they spen", reasoningRunes: 2022, stop: "stop"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv, _ := serve(t, 200, "text/event-stream; charset=utf-8", c.file)
			res, deltas, err := run(t, client(OpenAI, srv.URL), Request{Model: "qwen3:4b"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Content != c.content {
				t.Errorf("content %q, want %q", res.Content, c.content)
			}
			if !strings.HasPrefix(res.Reasoning, c.reasoningStart) || utf8.RuneCountInString(res.Reasoning) != c.reasoningRunes {
				t.Errorf("reasoning (%d runes) %.60q…, want %d runes starting %q",
					utf8.RuneCountInString(res.Reasoning), res.Reasoning, c.reasoningRunes, c.reasoningStart)
			}
			if strings.Contains(res.Content, "<think>") || strings.Contains(res.Content, "</think>") {
				t.Errorf("thinking leaked into the answer: %q", res.Content)
			}
			if dc, dr := joined(deltas); dc != res.Content || dr != res.Reasoning {
				t.Errorf("the streamed deltas differ from the result")
			}
			if diff := cmp.Diff(c.calls, res.ToolCalls); diff != "" {
				t.Errorf("tool calls (-want +got):\n%s", diff)
			}
			if res.StopReason != c.stop {
				t.Errorf("stop %q, want %q", res.StopReason, c.stop)
			}
		})
	}
}

func TestRecordedStreams(t *testing.T) {
	cases := []struct {
		name, file, contentType string
		api                     API
		content, reasoning      string
		calls                   []ToolCall // ids "" = generated (checked separately)
		stop                    string
		usage                   Usage
		replay                  bool
	}{
		{name: "LM Studio: tool call streamed in pieces", api: OpenAI, file: "openai-lmstudio-toolcall.sse",
			calls: []ToolCall{{ID: "584613722", Name: "aggregate_transactions",
				Arguments: `{"group_by":"category","date_from":"2025-02-01","date_to":"2025-02-28"}`}}, stop: "tool_calls"},
		{name: "Ollama: reasoning field and two whole tool calls", api: OpenAI, file: "openai-ollama-toolcall.sse",
			reasoning: "The user asks about last month, so February 2025.",
			calls: []ToolCall{{ID: "call_ab12", Name: "search_transactions", Arguments: `{"payee":"kino","limit":5}`},
				{ID: "call_cd34", Name: "find_values", Arguments: `{"field":"payee","query":"kino"}`}}, stop: "tool_calls"},
		{name: "reasoning_content, then the answer, with usage", api: OpenAI, file: "openai-reasoning-content.sse",
			reasoning: "Spending is negative amounts. Sum the March rows.", content: "You spent **5 151,82 PLN** in March.",
			stop: "stop", usage: Usage{Input: 1834, Output: 61}},
		{name: "gpt-oss reasoning field", api: OpenAI, file: "openai-gptoss-reasoning.sse",
			reasoning: "Need totals by category. Call aggregate.",
			calls:     []ToolCall{{ID: "call_1", Name: "aggregate_transactions", Arguments: `{"group_by":"category"}`}}, stop: "tool_calls"},
		{name: "<think> tags split across chunks", api: OpenAI, file: "openai-think-tags.sse",
			reasoning: "\nThe user wants groceries.", content: "Groceries cost 1 095 PLN."},
		{name: "Qwen tool call printed as text", api: OpenAI, file: "openai-qwen-text-toolcall.sse",
			content: "Let me look that up.\n", calls: []ToolCall{{Name: "find_values", Arguments: `{"field": "payee", "query": "amazon"}`}}},
		{name: "CRLF, no blank lines, comments and junk lines", api: OpenAI, file: "openai-crlf-no-blank.sse",
			content: "Hello there"},
		{name: "non-streaming JSON answer", api: OpenAI, file: "openai-nonstream.json", contentType: "application/json",
			reasoning: "plan", content: "Checking.", calls: []ToolCall{{Name: "search_transactions", Arguments: "{}"}},
			stop: "tool_calls", usage: Usage{Input: 10, Output: 5}},
		{name: "Anthropic: thinking, text and a tool call in pieces", api: Anthropic, file: "anthropic-thinking-toolcall.sse",
			reasoning: "Last month is February 2025.", content: "I'll total February by category.",
			calls: []ToolCall{{ID: "toolu_01A", Name: "aggregate_transactions",
				Arguments: `{"group_by": "category", "date_from": "2025-02-01", "date_to": "2025-02-28"}`}},
			stop: "tool_use", usage: Usage{Input: 2045, Output: 89}, replay: true},
		{name: "Anthropic non-streaming", api: Anthropic, file: "anthropic-nonstream.json", contentType: "application/json",
			content: "You spent 97 014,46 PLN.", stop: "end_turn", usage: Usage{Input: 100, Output: 12}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ct := c.contentType
			if ct == "" {
				ct = "text/event-stream; charset=utf-8"
			}
			srv, _ := serve(t, 200, ct, c.file)
			res, deltas, err := run(t, client(c.api, srv.URL), Request{Model: "m"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Content != c.content || res.Reasoning != c.reasoning {
				t.Errorf("content %q reasoning %q, want %q / %q", res.Content, res.Reasoning, c.content, c.reasoning)
			}
			// the streamed deltas add up to the result: the browser sees what is stored
			if dc, dr := joined(deltas); dc != res.Content || dr != res.Reasoning {
				t.Errorf("deltas %q / %q differ from the result", dc, dr)
			}
			got := res.ToolCalls
			for i := range got {
				if i < len(c.calls) && c.calls[i].ID == "" {
					if got[i].ID == "" {
						t.Errorf("call %d has no id", i)
					}
					got[i].ID = ""
				}
			}
			if diff := cmp.Diff(c.calls, got); diff != "" {
				t.Errorf("tool calls (-want +got):\n%s", diff)
			}
			if res.StopReason != c.stop || res.Usage != c.usage {
				t.Errorf("stop %q usage %+v", res.StopReason, res.Usage)
			}
			if (len(res.Replay) > 0) != c.replay {
				t.Errorf("replay %s", res.Replay)
			}
		})
	}
}

func TestAnthropicReplayKeepsThinkingAndSignature(t *testing.T) {
	srv, _ := serve(t, 200, "text/event-stream", "anthropic-thinking-toolcall.sse")
	res, _, err := run(t, client(Anthropic, srv.URL), Request{Model: "claude-sonnet-4-5"})
	if err != nil {
		t.Fatal(err)
	}
	var blocks []map[string]string
	if err := json.Unmarshal(res.Replay, &blocks); err != nil {
		t.Fatal(err)
	}
	want := []map[string]string{{"type": "thinking", "thinking": "Last month is February 2025.", "signature": "EqQBCgIYAhIM1gbcDa9GJwZA2b3h"}}
	if diff := cmp.Diff(want, blocks); diff != "" {
		t.Error(diff)
	}
}

func TestOpenAIRequestBody(t *testing.T) {
	srv, bodies := serve(t, 200, "text/event-stream", "openai-crlf-no-blank.sse")
	temp := 0.2
	req := Request{Model: "gpt-oss-20b", System: "SYS", Temperature: &temp, Thinking: ThinkHigh,
		Caps:  Caps{Reasoning: ReasonEffort},
		Tools: []Tool{{Name: "find_values", Description: "d", Parameters: json.RawMessage(`{"type":"object"}`)}},
		Messages: []Message{{Role: RoleUser, Content: "Q"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c1", Name: "find_values", Arguments: ""}}},
			{Role: RoleTool, ToolCallID: "c1", Content: `{"values":[]}`}}}
	if _, _, err := run(t, client(OpenAI, srv.URL), req); err != nil {
		t.Fatal(err)
	}
	b := (*bodies)[0]
	msgs := b["messages"].([]any)
	if b["reasoning_effort"] != "high" || b["tool_choice"] != "auto" || b["stream"] != true || len(msgs) != 4 ||
		msgs[0].(map[string]any)["role"] != "system" {
		t.Errorf("body = %v", b)
	}
	call := msgs[2].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)
	if call["arguments"] != "{}" || msgs[3].(map[string]any)["tool_call_id"] != "c1" {
		t.Errorf("tool turn = %v / %v", msgs[2], msgs[3])
	}
	// no reasoning control for a model without one
	req.Caps = Caps{}
	if _, _, err := run(t, client(OpenAI, srv.URL), req); err != nil {
		t.Fatal(err)
	}
	if _, ok := (*bodies)[1]["reasoning_effort"]; ok {
		t.Error("reasoning_effort sent to a model without effort control")
	}
}

func TestAnthropicRequestBody(t *testing.T) {
	srv, bodies := serve(t, 200, "application/json", "anthropic-nonstream.json")
	temp := 0.2
	replay := json.RawMessage(`[{"type":"thinking","thinking":"t","signature":"s"}]`)
	req := Request{Model: "claude-sonnet-4-5", System: "SYS", Temperature: &temp, Thinking: ThinkMedium,
		Caps:  Caps{Reasoning: ReasonBudget},
		Tools: []Tool{{Name: "a", Parameters: json.RawMessage(`{"type":"object"}`)}},
		Messages: []Message{{Role: RoleUser, Content: "Q"},
			{Role: RoleAssistant, Content: "x", Replay: replay, ToolCalls: []ToolCall{{ID: "t1", Name: "a", Arguments: `{"k":1}`},
				{ID: "t2", Name: "a", Arguments: "not json"}}},
			{Role: RoleTool, ToolCallID: "t1", Content: "r1"}, {Role: RoleTool, ToolCallID: "t2", Content: "r2"}}}
	if _, _, err := run(t, client(Anthropic, srv.URL+"/v1"), req); err != nil {
		t.Fatal(err)
	}
	b := (*bodies)[0]
	th := b["thinking"].(map[string]any)
	if b["system"] != "SYS" || th["budget_tokens"] != float64(4096) || b["max_tokens"] != float64(4096+4096) {
		t.Errorf("body = %v", b)
	}
	if _, ok := b["temperature"]; ok {
		t.Error("temperature sent with thinking")
	}
	msgs := b["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("messages = %v", msgs)
	}
	asst := msgs[1].(map[string]any)["content"].([]any)
	if asst[0].(map[string]any)["type"] != "thinking" || asst[1].(map[string]any)["type"] != "text" ||
		asst[3].(map[string]any)["input"].(map[string]any) == nil {
		t.Errorf("assistant blocks = %v", asst)
	}
	results := msgs[2].(map[string]any)["content"].([]any)
	if msgs[2].(map[string]any)["role"] != "user" || len(results) != 2 || results[1].(map[string]any)["tool_use_id"] != "t2" {
		t.Errorf("tool results = %v", msgs[2])
	}
}

func TestErrorsAreClassified(t *testing.T) {
	cases := []struct {
		status int
		body   string
		kind   Kind
		tools  bool
	}{
		{401, `{"error":{"message":"Invalid API key"}}`, KindAuth, false},
		{429, `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`, KindRateLimited, false},
		{400, `{"error":{"message":"registry.ollama.ai/library/gemma:2b does not support tools","type":"api_error"}}`, KindToolsRejected, true},
		{400, `{"error":"Trying to keep the first 9127 tokens when context the overflows. However, the model is loaded with context length of only 4096 tokens"}`, KindContextTooLong, false},
		{404, `{"error":{"message":"model 'qwen9' not found"}}`, KindNotFound, false},
		{500, `Internal Server Error`, KindUpstream, true},
		{422, `{"detail":"bad request"}`, KindUpstream, true},
		{503, `{"error":{"message":"busy"}}`, KindUpstream, false},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(c.status)
			_, _ = w.Write([]byte(c.body))
		}))
		_, _, err := run(t, client(OpenAI, srv.URL), Request{Model: "m"})
		srv.Close()
		if KindOf(err) != c.kind || ToolsMayBeRejected(err) != c.tools {
			t.Errorf("%d %s: %v (kind %s, tools %v)", c.status, c.body, err, KindOf(err), ToolsMayBeRejected(err))
		}
	}
	// a stream that ends in an Anthropic error event
	srv, _ := serve(t, 200, "text/event-stream", "anthropic-overloaded.sse")
	if _, _, err := run(t, client(Anthropic, srv.URL), Request{Model: "m"}); KindOf(err) != KindUpstream ||
		!strings.Contains(err.Error(), "Overloaded") {
		t.Errorf("overloaded: %v", err)
	}
	// nothing listening
	if _, _, err := run(t, client(OpenAI, "http://127.0.0.1:1"), Request{Model: "m"}); KindOf(err) != KindUnreachable {
		t.Errorf("unreachable: %v", err)
	}
}

func TestTimeoutsAndCancel(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Hi\"}}]}\n\n"))
		w.(http.Flusher).Flush()
		select { // then silence
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	c := New(Config{API: OpenAI, BaseURL: srv.URL, AllowRemote: true, Timeout: 5 * time.Second, IdleTimeout: 200 * time.Millisecond})
	t0 := time.Now()
	_, err := c.Chat(context.Background(), Request{Model: "m"}, nil)
	if KindOf(err) != KindTimeout || time.Since(t0) > 2*time.Second {
		t.Errorf("idle stream: %v after %s", err, time.Since(t0))
	}
	ctx, cancel := context.WithCancel(context.Background())
	c = New(Config{API: OpenAI, BaseURL: srv.URL, AllowRemote: true, Timeout: 5 * time.Second, IdleTimeout: 5 * time.Second})
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()
	if _, err := c.Chat(ctx, Request{Model: "m"}, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("cancel: %v", err)
	}
}

func TestPrivateAddressGuard(t *testing.T) {
	for _, c := range []struct {
		ip string
		ok bool
	}{{"127.0.0.1", true}, {"10.1.2.3", true}, {"192.168.1.20", true}, {"172.20.0.5", true}, {"100.101.102.103", true},
		{"169.254.1.1", true}, {"::1", true}, {"fd7a:115c:a1e0::1", true}, {"8.8.8.8", false}, {"100.128.0.1", false},
		{"2606:4700::1111", false}, {"::ffff:10.0.0.1", true}} {
		if PrivateAddr(netip.MustParseAddr(c.ip)) != c.ok {
			t.Errorf("%s: want %v", c.ip, c.ok)
		}
	}
	if ok, err := CheckHost(context.Background(), nil, "localhost"); err != nil || !ok {
		t.Errorf("localhost: %v %v", ok, err)
	}
	// a local-only client refuses to dial a public address even if the URL says so
	c := New(Config{API: OpenAI, BaseURL: "http://1.1.1.1:9", Timeout: time.Second})
	if _, err := c.Models(context.Background()); KindOf(err) != KindUnreachable || !strings.Contains(err.Error(), "not a private address") {
		t.Errorf("public dial: %v", err)
	}
}

func TestModelsAndLMStudioDetails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"qwen3-8b"},{"id":"text-embedding-nomic"},{"id":"gemma-3-12b"}]}`))
		case "/api/v0/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"qwen3-8b","type":"llm","max_context_length":32768,"loaded_context_length":8192,"capabilities":["tool_use"]},
				{"id":"text-embedding-nomic","type":"embeddings"},{"id":"gemma-3-12b","type":"vlm","max_context_length":131072}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	ms, err := client(OpenAI, srv.URL+"/v1").Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 3 || ms[0].ContextTokens != 8192 || ms[0].ToolUse == nil || !*ms[0].ToolUse || !ms[1].Embedding ||
		ms[2].ContextTokens != 131072 || ms[2].ToolUse != nil {
		t.Errorf("models = %+v", ms)
	}
	if DefaultModel(ms, "") != "qwen3-8b" || DefaultModel(ms, "gemma-3-12b") != "gemma-3-12b" ||
		DefaultModel(ms, "text-embedding-nomic") != "qwen3-8b" || DefaultModel([]Model{{ID: "nomic-embed"}}, "") != "" {
		t.Error("default model choice")
	}
	// a server without the native list (Ollama, OpenAI) still lists its models
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"llama3.1:8b"}]}`))
	}))
	defer srv2.Close()
	if ms, err := client(OpenAI, srv2.URL+"/v1").Models(context.Background()); err != nil || len(ms) != 1 || ms[0].ContextTokens != 0 {
		t.Errorf("plain list: %+v %v", ms, err)
	}
}

func TestDetectCaps(t *testing.T) {
	f := false
	r := ReasonEffort
	ctx := 65536
	over := []CapsOverride{{Pattern: "gemma-*", Tools: &f}, {Pattern: "my-reasoner", Reasoning: &r, ContextTokens: &ctx}}
	cases := []struct {
		api  API
		m    Model
		want Caps
	}{
		{OpenAI, Model{ID: "qwen3-8b", ContextTokens: 8192}, Caps{Tools: true, Reasoning: ReasonSwitch, ContextTokens: 8192}},
		{OpenAI, Model{ID: "openai/gpt-oss-20b"}, Caps{Tools: true, Reasoning: ReasonEffort, ContextTokens: 16000}},
		{OpenAI, Model{ID: "o4-mini"}, Caps{Tools: true, Reasoning: ReasonEffort, ContextTokens: 16000}},
		{OpenAI, Model{ID: "gpt-4.1-mini"}, Caps{Tools: true, Reasoning: ReasonNone, ContextTokens: 16000}},
		{OpenAI, Model{ID: "Gemma-3-12B"}, Caps{Tools: false, Reasoning: ReasonNone, ContextTokens: 16000}},
		{OpenAI, Model{ID: "my-reasoner"}, Caps{Tools: true, Reasoning: ReasonEffort, ContextTokens: 65536}},
		{Anthropic, Model{ID: "claude-sonnet-4-5"}, Caps{Tools: true, Reasoning: ReasonBudget, ContextTokens: 16000}},
		{Anthropic, Model{ID: "claude-3-5-haiku-latest"}, Caps{Tools: true, Reasoning: ReasonNone, ContextTokens: 16000}},
		{Anthropic, Model{ID: "qwen3-8b"}, Caps{Tools: true, Reasoning: ReasonSwitch, ContextTokens: 16000}},
	}
	for _, c := range cases {
		if got := DetectCaps(c.api, c.m, 16000, over); got != c.want {
			t.Errorf("%s %s: %+v, want %+v", c.api, c.m.ID, got, c.want)
		}
	}
}

func TestSplitterHoldsBackPartialTags(t *testing.T) {
	var got []Delta
	s := newSplitter(func(d Delta) { got = append(got, d) })
	for _, piece := range []string{"  ", "\n<", "think>a<", "/think>b <", "tool_call>{\"name\":\"x\"}</tool_call", ">c <thi", "nk>open"} {
		s.write(piece)
	}
	s.close()
	if s.answer.String() != "b c " || s.reason.String() != "aopen" || len(s.calls) != 1 || s.calls[0].Arguments != "{}" {
		t.Errorf("answer %q reasoning %q calls %+v", s.answer.String(), s.reason.String(), s.calls)
	}
	for _, d := range got {
		if strings.Contains(d.Content, "<") {
			t.Errorf("a tag leaked to the stream: %q", d.Content)
		}
	}
	// a broken <tool_call> stays in the answer as written
	s = newSplitter(nil)
	s.write("x <tool_call>{broken</tool_call> y")
	s.close()
	if s.answer.String() != "x <tool_call>{broken</tool_call> y" || len(s.calls) != 0 {
		t.Errorf("broken call: %q %+v", s.answer.String(), s.calls)
	}
}
