package llm

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"
)

// anthropicClient speaks the Messages API (Anthropic, LM Studio ≥ 0.4.1 /v1/messages).
type anthropicClient struct{ c Config }

func (a *anthropicClient) API() API { return Anthropic }

// url accepts a base with or without the /v1 suffix.
func (a *anthropicClient) url(p string) string {
	return strings.TrimSuffix(a.c.BaseURL, "/v1") + "/v1/" + p
}

func (a *anthropicClient) header() http.Header {
	h := http.Header{"Content-Type": {"application/json"}, "Accept": {"text/event-stream, application/json"},
		"Anthropic-Version": {"2023-06-01"}}
	if a.c.APIKey != "" {
		h.Set("X-Api-Key", a.c.APIKey)
	}
	return h
}

type anBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
}

type anMessage struct {
	Role    string            `json:"role"`
	Content []json.RawMessage `json:"content"`
}

type anRequest struct {
	Model       string      `json:"model"`
	MaxTokens   int         `json:"max_tokens"`
	System      string      `json:"system,omitempty"`
	Messages    []anMessage `json:"messages"`
	Tools       []anTool    `json:"tools,omitempty"`
	Temperature *float64    `json:"temperature,omitempty"`
	Thinking    *anThinking `json:"thinking,omitempty"`
	Stream      bool        `json:"stream"`
}

type anTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type anThinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens"`
}

// thinkingBudget maps a level to Anthropic's token budget (≥ 1024).
var thinkingBudget = map[Thinking]int{ThinkAuto: 2048, ThinkLow: 1024, ThinkMedium: 4096, ThinkHigh: 16000}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func (a *anthropicClient) body(req Request) ([]byte, error) {
	r := anRequest{Model: req.Model, System: req.System, Temperature: req.Temperature, Stream: true, MaxTokens: req.MaxTokens}
	if r.MaxTokens <= 0 {
		r.MaxTokens = 4096
	}
	if req.Caps.Reasoning == ReasonBudget && req.Thinking != ThinkOff {
		b := thinkingBudget[req.Thinking]
		if b == 0 {
			b = thinkingBudget[ThinkAuto]
		}
		r.Thinking = &anThinking{Type: "enabled", BudgetTokens: b}
		r.MaxTokens += b
		r.Temperature = nil // thinking requires the default temperature
	}
	add := func(role string, blocks ...json.RawMessage) {
		if n := len(r.Messages); n > 0 && r.Messages[n-1].Role == role {
			r.Messages[n-1].Content = append(r.Messages[n-1].Content, blocks...)
			return
		}
		r.Messages = append(r.Messages, anMessage{Role: role, Content: blocks})
	}
	for _, m := range req.Messages {
		switch m.Role {
		case RoleTool: // tool results are user content, consecutive ones in one message
			add("user", mustJSON(anBlock{Type: "tool_result", ToolUseID: m.ToolCallID, Content: m.Content}))
		case RoleAssistant:
			var blocks []json.RawMessage
			var replay []json.RawMessage
			if len(m.Replay) > 0 && json.Unmarshal(m.Replay, &replay) == nil {
				blocks = append(blocks, replay...) // thinking blocks first, unchanged
			}
			if m.Content != "" {
				blocks = append(blocks, mustJSON(anBlock{Type: "text", Text: m.Content}))
			}
			for _, tc := range m.ToolCalls {
				in := json.RawMessage(tc.Arguments)
				var obj map[string]any
				if json.Unmarshal(in, &obj) != nil {
					in = json.RawMessage("{}")
				}
				blocks = append(blocks, mustJSON(anBlock{Type: "tool_use", ID: tc.ID, Name: tc.Name, Input: in}))
			}
			if len(blocks) == 0 {
				blocks = append(blocks, mustJSON(anBlock{Type: "text", Text: "…"}))
			}
			add("assistant", blocks...)
		default:
			add("user", mustJSON(anBlock{Type: "text", Text: m.Content}))
		}
	}
	for _, t := range req.Tools {
		r.Tools = append(r.Tools, anTool{Name: t.Name, Description: t.Description, InputSchema: t.Parameters})
	}
	return json.Marshal(r)
}

// Chat streams one message.
func (a *anthropicClient) Chat(ctx context.Context, req Request, onDelta func(Delta)) (Result, error) {
	body, err := a.body(req)
	if err != nil {
		return Result{}, &Error{Kind: KindBadResponse, Message: "can't encode the request"}
	}
	res, done, err := a.c.call(ctx, http.MethodPost, a.url("messages"), a.header(), body)
	if err != nil {
		return Result{}, err
	}
	defer done()
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode/100 != 2 {
		return Result{}, readError(res)
	}
	sp := newSplitter(onDelta)
	var reasoning strings.Builder
	addReasoning := func(s string) {
		if s == "" {
			return
		}
		reasoning.WriteString(s)
		if onDelta != nil {
			onDelta(Delta{Reasoning: s})
		}
	}
	// blocks in stream order: thinking blocks are kept for the replay, tool_use blocks collect their
	// JSON input in pieces
	type block struct {
		typ       string
		thinking  strings.Builder
		signature string
		data      string
		call      *ToolCall
	}
	blocks := map[int]*block{}
	var order []int
	out := Result{}
	mt, _, _ := mime.ParseMediaType(res.Header.Get("Content-Type"))
	if mt != "text/event-stream" {
		var j struct {
			Content []struct {
				Type      string          `json:"type"`
				Text      string          `json:"text"`
				Thinking  string          `json:"thinking"`
				Signature string          `json:"signature"`
				Data      string          `json:"data"`
				ID        string          `json:"id"`
				Name      string          `json:"name"`
				Input     json.RawMessage `json:"input"`
			} `json:"content"`
			StopReason string `json:"stop_reason"`
			Usage      struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		b, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
		if err != nil {
			return Result{}, transportError(ctx, err)
		}
		if json.Unmarshal(b, &j) != nil {
			return Result{}, &Error{Kind: KindBadResponse, Status: res.StatusCode, Message: "the answer is not a message"}
		}
		for i := range j.Content {
			c := &j.Content[i]
			bl := &block{typ: c.Type, signature: c.Signature, data: c.Data}
			switch c.Type {
			case "text":
				sp.write(c.Text)
			case "thinking":
				bl.thinking.WriteString(c.Thinking)
				addReasoning(c.Thinking)
			case "tool_use":
				bl.call = &ToolCall{ID: c.ID, Name: c.Name, Arguments: string(c.Input)}
			}
			blocks[i] = bl
			order = append(order, i)
		}
		out.StopReason = j.StopReason
		out.Usage = Usage{Input: j.Usage.InputTokens, Output: j.Usage.OutputTokens}
	} else {
		err := readSSE(res.Body, func(ev sseEvent) (bool, error) {
			var j struct {
				Type  string `json:"type"`
				Index int    `json:"index"`
				Block struct {
					Type      string `json:"type"`
					ID        string `json:"id"`
					Name      string `json:"name"`
					Text      string `json:"text"`
					Thinking  string `json:"thinking"`
					Signature string `json:"signature"`
					Data      string `json:"data"`
				} `json:"content_block"`
				Delta struct {
					Type        string `json:"type"`
					Text        string `json:"text"`
					Thinking    string `json:"thinking"`
					Signature   string `json:"signature"`
					PartialJSON string `json:"partial_json"`
					StopReason  string `json:"stop_reason"`
				} `json:"delta"`
				Message struct {
					Usage struct {
						InputTokens int `json:"input_tokens"`
					} `json:"usage"`
				} `json:"message"`
				Usage struct {
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			}
			if json.Unmarshal([]byte(ev.Data), &j) != nil {
				return false, nil
			}
			typ := j.Type
			if typ == "" {
				typ = ev.Name
			}
			switch typ {
			case "message_start":
				out.Usage.Input = j.Message.Usage.InputTokens
			case "content_block_start":
				bl := &block{typ: j.Block.Type, signature: j.Block.Signature, data: j.Block.Data}
				switch j.Block.Type {
				case "tool_use":
					bl.call = &ToolCall{ID: j.Block.ID, Name: j.Block.Name}
				case "text":
					sp.write(j.Block.Text)
				case "thinking":
					bl.thinking.WriteString(j.Block.Thinking)
					addReasoning(j.Block.Thinking)
				}
				blocks[j.Index] = bl
				order = append(order, j.Index)
			case "content_block_delta":
				bl := blocks[j.Index]
				if bl == nil {
					return false, nil
				}
				switch j.Delta.Type {
				case "text_delta":
					sp.write(j.Delta.Text)
				case "thinking_delta":
					bl.thinking.WriteString(j.Delta.Thinking)
					addReasoning(j.Delta.Thinking)
				case "signature_delta":
					bl.signature += j.Delta.Signature
				case "input_json_delta":
					if bl.call != nil {
						bl.call.Arguments += j.Delta.PartialJSON
					}
				}
			case "message_delta":
				if j.Delta.StopReason != "" {
					out.StopReason = j.Delta.StopReason
				}
				out.Usage.Output = j.Usage.OutputTokens
			case "message_stop":
				return true, nil
			case "error":
				return true, statusError(529, []byte(ev.Data))
			}
			return false, nil
		})
		if err != nil {
			return Result{}, transportError(ctx, err)
		}
	}
	sp.close()
	out.Content = sp.answer.String()
	out.Reasoning = reasoning.String() + sp.reason.String()
	var replay []json.RawMessage
	for _, i := range order {
		bl := blocks[i]
		switch bl.typ {
		case "thinking":
			replay = append(replay, mustJSON(map[string]string{"type": "thinking", "thinking": bl.thinking.String(), "signature": bl.signature}))
		case "redacted_thinking":
			replay = append(replay, mustJSON(map[string]string{"type": "redacted_thinking", "data": bl.data}))
		case "tool_use":
			if bl.call.Name != "" {
				if strings.TrimSpace(bl.call.Arguments) == "" {
					bl.call.Arguments = "{}"
				}
				out.ToolCalls = append(out.ToolCalls, *bl.call)
			}
		}
	}
	if len(replay) > 0 {
		out.Replay = mustJSON(replay)
	}
	if len(out.ToolCalls) == 0 {
		out.ToolCalls = sp.calls
	}
	for i := range out.ToolCalls {
		if out.ToolCalls[i].ID == "" {
			out.ToolCalls[i].ID = "toolu_" + callID()[5:]
		}
	}
	return out, nil
}

// Models lists the server's models (GET /v1/models).
func (a *anthropicClient) Models(ctx context.Context) ([]Model, error) {
	res, done, err := a.c.call(ctx, http.MethodGet, a.url("models?limit=100"), a.header(), nil)
	if err != nil {
		return nil, err
	}
	defer done()
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode/100 != 2 {
		return nil, readError(res)
	}
	var j struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&j); err != nil {
		return nil, &Error{Kind: KindBadResponse, Status: res.StatusCode, Message: "the model list is not JSON"}
	}
	out := make([]Model, 0, len(j.Data))
	for _, m := range j.Data {
		if m.ID != "" {
			out = append(out, Model{ID: m.ID})
		}
	}
	return out, nil
}
