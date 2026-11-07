// Package llmmock is a model server for tests and local UI work (PLAN task 13.8). It speaks the
// OpenAI Chat Completions and Anthropic Messages protocols, streams like the real servers do
// (tool-call arguments split mid-JSON, reasoning fields and <think> tags, Qwen <tool_call> text),
// and never needs a model.
//
// Replies come from a queue (Push) when a test scripts them, else from a small rule-based brain:
// a question gets a lookup, a tool result gets an answer built from it (a markdown table), plan
// mode gets {"calls":[…]}. Models:
//
//	mock-tools       native tool calls, reasoning in reasoning_content
//	mock-text-tools  <think> tags and Qwen <tool_call> text instead of native calls
//	mock-no-tools    rejects requests with tools (HTTP 400), answers plan mode
//	mock-vl          like mock-tools, and reads images by its name (receipts, Phase 20)
//
// Words in the question steer the brain: "slow" streams slowly (for stop), "fail" answers 500,
// "by category" aggregates, "spelled" uses find_values, and a "quoted" word becomes the search query.
package llmmock

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Call is a tool call in a reply.
type Call struct {
	Name      string
	Arguments string // JSON text
}

// Reply is one scripted answer.
type Reply struct {
	Status    int    // non-200: answer with Body as the error
	Body      string // error body
	Reasoning string
	Content   string
	Calls     []Call
	Think     bool          // put Reasoning in <think> tags inside the content
	TextCalls bool          // write calls as <tool_call> text
	Delay     time.Duration // between chunks
	Chunk     int           // characters per content chunk (default 8)
}

// Server is the mock.
type Server struct {
	mu       sync.Mutex
	queue    []Reply
	requests []json.RawMessage
	// Models lists the ids GET /models answers.
	Models []string
}

// New returns a mock with the default models.
func New() *Server {
	return &Server{Models: []string{"mock-tools", "mock-text-tools", "mock-no-tools", "mock-vl"}}
}

// Push queues scripted replies; they are used before the brain.
func (s *Server) Push(r ...Reply) {
	s.mu.Lock()
	s.queue = append(s.queue, r...)
	s.mu.Unlock()
}

// Requests returns the chat request bodies received so far.
func (s *Server) Requests() []json.RawMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]json.RawMessage(nil), s.requests...)
}

func (s *Server) next() (Reply, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queue) == 0 {
		return Reply{}, false
	}
	r := s.queue[0]
	s.queue = s.queue[1:]
	return r, true
}

// ServeHTTP routes /v1/models, /v1/chat/completions and /v1/messages (with or without /v1).
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(r.URL.Path, "/v1")
	switch {
	case r.Method == http.MethodGet && p == "/models":
		data := make([]map[string]string, 0, len(s.Models))
		for _, m := range s.Models {
			data = append(data, map[string]string{"id": m, "object": "model"})
		}
		writeJSON(w, map[string]any{"object": "list", "data": data})
	case r.Method == http.MethodPost && p == "/chat/completions":
		s.chat(w, r, false)
	case r.Method == http.MethodPost && p == "/messages":
		s.chat(w, r, true)
	default:
		http.NotFound(w, r)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// request is the part of both protocols the brain reads.
type request struct {
	Model    string            `json:"model"`
	System   json.RawMessage   `json:"system"`
	Messages []json.RawMessage `json:"messages"`
	Tools    []json.RawMessage `json:"tools"`
	Stream   bool              `json:"stream"`
}

func (s *Server) chat(w http.ResponseWriter, r *http.Request, anthropic bool) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	s.mu.Lock()
	s.requests = append(s.requests, body)
	s.mu.Unlock()
	var req request
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, `{"error":{"message":"bad json"}}`, http.StatusBadRequest)
		return
	}
	reply, ok := s.next()
	if !ok {
		reply = brain(req, anthropic)
	}
	if reply.Status != 0 && reply.Status != http.StatusOK {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(reply.Status)
		_, _ = io.WriteString(w, reply.Body)
		return
	}
	if anthropic {
		streamAnthropic(w, r, reply)
		return
	}
	streamOpenAI(w, r, reply)
}

// conversation is what the brain needs from a request.
type conversation struct {
	system     string
	lastUser   string
	toolResult string // the last message's tool result, if the last message is one
	tools      bool
	model      string
}

func read(req request, anthropic bool) conversation {
	c := conversation{tools: len(req.Tools) > 0, model: req.Model}
	if anthropic {
		_ = json.Unmarshal(req.System, &c.system)
	}
	for i, raw := range req.Messages {
		var m struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		}
		_ = json.Unmarshal(raw, &m)
		var text string
		if json.Unmarshal(m.Content, &text) != nil {
			// Anthropic blocks: text or tool_result
			var blocks []struct {
				Type    string          `json:"type"`
				Text    string          `json:"text"`
				Content json.RawMessage `json:"content"`
			}
			_ = json.Unmarshal(m.Content, &blocks)
			for _, b := range blocks {
				switch b.Type {
				case "text":
					text += b.Text
				case "tool_result":
					var s string
					_ = json.Unmarshal(b.Content, &s)
					if i == len(req.Messages)-1 {
						c.toolResult = s
					}
				}
			}
		}
		switch m.Role {
		case "system":
			c.system = text
		case "user":
			if text != "" {
				c.lastUser = text
			}
		case "tool":
			if i == len(req.Messages)-1 {
				c.toolResult = text
			}
		}
	}
	return c
}

var (
	quotedRe = regexp.MustCompile(`["“„]([^"”]+)["”]`)
	// `put "PAYEE" in Category` proposes a category; `rule "PAYEE" as Category` proposes a rule.
	proposeRe = regexp.MustCompile(`(?i)^\s*(put|rule)\s+["“„]([^"”]+)["”]\s+(?:in|as)\s+(.+?)\s*$`)
	// Phase 20 proposals: `budget "Category" 800`, `spent 25.50 at "Payee" on 2026-11-05`,
	// `share "PAYEE" with "Shared group"`, `save 10000 by 2027-06-30`, `spend 20% less on "Category" by 2027-04-30`,
	// `receipt 54.30 "Shop" on 2026-11-03`
	budgetRe  = regexp.MustCompile(`(?i)^\s*budget\s+["“„]([^"”]+)["”]\s+([0-9.,]+)\s*$`)
	expenseRe = regexp.MustCompile(`(?i)^\s*spent\s+([0-9.,]+)\s+at\s+["“„]([^"”]+)["”]\s+on\s+(\d{4}-\d{2}-\d{2})\s*$`)
	shareRe   = regexp.MustCompile(`(?i)^\s*share\s+["“„]([^"”]+)["”]\s+with\s+["“„]([^"”]+)["”]\s*$`)
	saveRe    = regexp.MustCompile(`(?i)^\s*save\s+([0-9.,]+)\s+by\s+(\d{4}-\d{2}-\d{2})\s*$`)
	receiptRe = regexp.MustCompile(`(?i)^\s*receipt\s+([0-9.,]+)\s+["“„]([^"”]+)["”]\s+on\s+(\d{4}-\d{2}-\d{2})\s*$`)
	reduceRe  = regexp.MustCompile(`(?i)^\s*spend\s+(\d+)\s*%\s+less\s+on\s+["“„]([^"”]+)["”]\s+by\s+(\d{4}-\d{2}-\d{2})\s*$`)
)

func brain(req request, anthropic bool) Reply {
	c := read(req, anthropic)
	q := strings.ToLower(c.lastUser)
	if c.model == "mock-no-tools" && c.tools {
		return Reply{Status: http.StatusBadRequest, Body: `{"error":{"message":"registry.mock/mock-no-tools does not support tools","type":"api_error"}}`}
	}
	switch {
	case strings.Contains(q, "fail"):
		return Reply{Status: http.StatusInternalServerError, Body: `{"error":{"message":"mock failure"}}`}
	case strings.Contains(q, "slow"):
		return Reply{Content: strings.Repeat("Still thinking about it. ", 40), Delay: 100 * time.Millisecond, Chunk: 6}
	}
	if c.toolResult != "" {
		return Reply{Content: answer(c.toolResult)}
	}
	if i := strings.Index(c.lastUser, "Data looked up locally by the app for my last question:\n"); i >= 0 {
		var results []struct {
			Result json.RawMessage `json:"result"`
		}
		rest := c.lastUser[i+len("Data looked up locally by the app for my last question:\n"):]
		if j := strings.LastIndex(rest, "]"); j >= 0 {
			_ = json.Unmarshal([]byte(rest[:j+1]), &results)
		}
		if len(results) > 0 {
			return Reply{Content: answer(string(results[0].Result))}
		}
		return Reply{Content: "I found nothing to answer with."}
	}
	call := pick(c.lastUser)
	if strings.Contains(c.system, "You cannot call functions directly in this mode") {
		return Reply{Content: fmt.Sprintf(`{"calls":[{"name":%q,"arguments":%s}]}`, call.Name, call.Arguments)}
	}
	if !c.tools {
		return Reply{Content: "Hello from the mock model."}
	}
	r := Reply{Reasoning: "The user wants data; I'll look it up.", Content: "", Calls: []Call{call}, Chunk: 5}
	if c.model == "mock-text-tools" {
		r.Think, r.TextCalls = true, true
	}
	return r
}

// pick chooses the lookup for a question.
func pick(question string) Call {
	q := strings.ToLower(question)
	if m := budgetRe.FindStringSubmatch(question); m != nil {
		return Call{Name: "propose_action", Arguments: fmt.Sprintf(`{"action":"set_budget","to_category":%q,"amount":%q,"reason":"You asked for it"}`, m[1], m[2])}
	}
	if m := expenseRe.FindStringSubmatch(question); m != nil {
		return Call{Name: "propose_action", Arguments: fmt.Sprintf(`{"action":"add_expense","amount":%q,"payee":%q,"date":%q,"reason":"You asked for it"}`, m[1], m[2], m[3])}
	}
	if m := shareRe.FindStringSubmatch(question); m != nil {
		return Call{Name: "propose_action", Arguments: fmt.Sprintf(`{"action":"add_to_shared_group","payee":%q,"shared_group":%q,"reason":"You asked for it"}`, m[1], m[2])}
	}
	if m := receiptRe.FindStringSubmatch(question); m != nil {
		return Call{Name: "propose_action", Arguments: fmt.Sprintf(`{"action":"attach_receipt","amount":%q,"shop":%q,"date":%q,"items":"bread, butter","reason":"You asked for it"}`, m[1], m[2], m[3])}
	}
	if m := saveRe.FindStringSubmatch(question); m != nil {
		return Call{Name: "propose_action", Arguments: fmt.Sprintf(`{"action":"create_goal","goal_kind":"save","goal_name":"Savings","amount":%q,"deadline":%q,"reason":"You asked for it"}`, m[1], m[2])}
	}
	if m := reduceRe.FindStringSubmatch(question); m != nil {
		return Call{Name: "propose_action", Arguments: fmt.Sprintf(`{"action":"create_goal","goal_kind":"reduce","goal_name":"Less on %s","to_category":%q,"reduce_pct":%s,"deadline":%q,"reason":"You asked for it"}`, m[2], m[2], m[1], m[3])}
	}
	if m := proposeRe.FindStringSubmatch(question); m != nil {
		action := "set_category"
		if strings.EqualFold(m[1], "rule") {
			action = "create_rule"
		}
		return Call{Name: "propose_action", Arguments: fmt.Sprintf(`{"action":%q,"payee":%q,"to_category":%q,"reason":"You asked for it"}`, action, m[2], m[3])}
	}
	switch {
	case strings.Contains(q, "by category"), strings.HasPrefix(q, "write my daily digest"), strings.HasPrefix(q, "write my weekly digest"):
		// a digest (ADR 0033) looks at the spending by category
		return Call{Name: "aggregate_transactions", Arguments: `{"group_by":"category","type":"expense"}`}
	case strings.Contains(q, "spelled"):
		term := "a"
		if m := quotedRe.FindStringSubmatch(question); m != nil {
			term = m[1]
		}
		return Call{Name: "find_values", Arguments: fmt.Sprintf(`{"field":"payee","query":%q}`, term)}
	}
	if m := quotedRe.FindStringSubmatch(question); m != nil {
		return Call{Name: "search_transactions", Arguments: fmt.Sprintf(`{"query":%q,"limit":5}`, m[1])}
	}
	return Call{Name: "search_transactions", Arguments: `{"limit":5}`}
}

// answer writes a markdown answer from a tool result: a table of rows, groups or values.
func answer(result string) string {
	var r struct {
		Currency   string           `json:"currency"`
		Matched    *int             `json:"matched"`
		TotalSpent json.Number      `json:"total_spent"`
		Rows       []map[string]any `json:"rows"`
		Groups     []map[string]any `json:"groups"`
		Values     []map[string]any `json:"values"`
		Error      string           `json:"error"`
		Status     string           `json:"status"`
		Action     string           `json:"action"`
		ToCategory string           `json:"to_category"`
	}
	if json.Unmarshal([]byte(result), &r) != nil {
		return "The lookup returned something I can't read."
	}
	var b strings.Builder
	switch {
	case r.Error != "":
		fmt.Fprintf(&b, "The lookup failed: %s", r.Error)
	case r.Status != "":
		fmt.Fprintf(&b, "I prepared a change for you to review (%s → %s, %d transactions). Nothing has changed yet: use **Apply** on the card.",
			strings.ReplaceAll(r.Action, "_", " "), r.ToCategory, *r.Matched)
	case r.Rows != nil:
		fmt.Fprintf(&b, "Found **%d** matching transactions; spent **%s %s**.\n\n| Date | Payee | Amount |\n|---|---|---:|\n", *r.Matched, r.TotalSpent, r.Currency)
		for _, row := range r.Rows {
			fmt.Fprintf(&b, "| %v | %v | %v |\n", row["date"], row["payee"], row["amount"])
		}
	case r.Groups != nil:
		b.WriteString("| Group | Spent |\n|---|---:|\n")
		for _, g := range r.Groups {
			fmt.Fprintf(&b, "| %v | %v |\n", g["key"], g["spent"])
		}
	default:
		b.WriteString("Values:\n")
		for _, v := range r.Values {
			fmt.Fprintf(&b, "- %v (%v)\n", v["value"], v["count"])
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func chunks(s string, n int) []string {
	if n <= 0 {
		n = 8
	}
	var out []string
	rs := []rune(s)
	for len(rs) > 0 {
		k := min(n, len(rs))
		out = append(out, string(rs[:k]))
		rs = rs[k:]
	}
	return out
}

type sse struct {
	w     http.ResponseWriter
	r     *http.Request
	delay time.Duration
}

func newSSE(w http.ResponseWriter, r *http.Request, delay time.Duration) *sse {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	return &sse{w: w, r: r, delay: delay}
}

// send writes one event; false when the client went away.
func (s *sse) send(event string, v any) bool {
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-s.r.Context().Done():
			return false
		}
	}
	b, _ := json.Marshal(v)
	if event != "" {
		_, _ = fmt.Fprintf(s.w, "event: %s\n", event)
	}
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", b); err != nil {
		return false
	}
	if f, ok := s.w.(http.Flusher); ok {
		f.Flush()
	}
	return s.r.Context().Err() == nil
}

func streamOpenAI(w http.ResponseWriter, r *http.Request, rep Reply) {
	s := newSSE(w, r, rep.Delay)
	delta := func(d map[string]any, finish any) map[string]any {
		return map[string]any{"object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": d, "finish_reason": finish}}}
	}
	content := rep.Content
	if rep.Think && rep.Reasoning != "" {
		content = "<think>" + rep.Reasoning + "</think>" + content
	} else if rep.Reasoning != "" {
		for _, p := range chunks(rep.Reasoning, rep.Chunk) {
			if !s.send("", delta(map[string]any{"reasoning_content": p}, nil)) {
				return
			}
		}
	}
	if rep.TextCalls {
		for _, c := range rep.Calls {
			content += fmt.Sprintf("<tool_call>\n{\"name\": %q, \"arguments\": %s}\n</tool_call>", c.Name, c.Arguments)
		}
	}
	for _, p := range chunks(content, rep.Chunk) {
		if !s.send("", delta(map[string]any{"content": p}, nil)) {
			return
		}
	}
	finish := "stop"
	if !rep.TextCalls && len(rep.Calls) > 0 {
		finish = "tool_calls"
		for i, c := range rep.Calls {
			id := fmt.Sprintf("call_mock_%d", i)
			first := true
			for _, p := range chunks(c.Arguments, max(3, rep.Chunk)) { // split mid-JSON, like LM Studio
				tc := map[string]any{"index": i, "function": map[string]any{"arguments": p}}
				if first {
					tc["id"], tc["type"] = id, "function"
					tc["function"].(map[string]any)["name"] = c.Name
					first = false
				}
				if !s.send("", delta(map[string]any{"tool_calls": []any{tc}}, nil)) {
					return
				}
			}
		}
	}
	s.send("", delta(map[string]any{}, finish))
	s.send("", map[string]any{"object": "chat.completion.chunk", "choices": []any{}, "usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 20}})
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
}

func streamAnthropic(w http.ResponseWriter, r *http.Request, rep Reply) {
	s := newSSE(w, r, rep.Delay)
	if !s.send("message_start", map[string]any{"type": "message_start", "message": map[string]any{"usage": map[string]int{"input_tokens": 100}}}) {
		return
	}
	idx := 0
	block := func(start map[string]any, deltas []map[string]any) bool {
		if !s.send("content_block_start", map[string]any{"type": "content_block_start", "index": idx, "content_block": start}) {
			return false
		}
		for _, d := range deltas {
			if !s.send("content_block_delta", map[string]any{"type": "content_block_delta", "index": idx, "delta": d}) {
				return false
			}
		}
		ok := s.send("content_block_stop", map[string]any{"type": "content_block_stop", "index": idx})
		idx++
		return ok
	}
	if rep.Reasoning != "" {
		var ds []map[string]any
		for _, p := range chunks(rep.Reasoning, rep.Chunk) {
			ds = append(ds, map[string]any{"type": "thinking_delta", "thinking": p})
		}
		ds = append(ds, map[string]any{"type": "signature_delta", "signature": "mock-signature"})
		if !block(map[string]any{"type": "thinking", "thinking": ""}, ds) {
			return
		}
	}
	if rep.Content != "" {
		var ds []map[string]any
		for _, p := range chunks(rep.Content, rep.Chunk) {
			ds = append(ds, map[string]any{"type": "text_delta", "text": p})
		}
		if !block(map[string]any{"type": "text", "text": ""}, ds) {
			return
		}
	}
	stop := "end_turn"
	for i, c := range rep.Calls {
		stop = "tool_use"
		var ds []map[string]any
		for _, p := range chunks(c.Arguments, max(3, rep.Chunk)) {
			ds = append(ds, map[string]any{"type": "input_json_delta", "partial_json": p})
		}
		if !block(map[string]any{"type": "tool_use", "id": fmt.Sprintf("toolu_mock_%d", i), "name": c.Name, "input": map[string]any{}}, ds) {
			return
		}
	}
	s.send("message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": stop}, "usage": map[string]int{"output_tokens": 20}})
	s.send("message_stop", map[string]any{"type": "message_stop"})
}
