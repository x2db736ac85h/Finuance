package llm

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"
)

// openAIClient speaks Chat Completions (legacy complete()).
type openAIClient struct{ c Config }

func (o *openAIClient) API() API { return OpenAI }

func (o *openAIClient) header() http.Header {
	h := http.Header{"Content-Type": {"application/json"}, "Accept": {"text/event-stream, application/json"}}
	if o.c.APIKey != "" {
		h.Set("Authorization", "Bearer "+o.c.APIKey)
	}
	return h
}

type oaMessage struct {
	Role       string       `json:"role"`
	Content    any          `json:"content"` // *string, or []oaPart for a user message with images
	ToolCalls  []oaToolCall `json:"tool_calls,omitempty"`
	ToolCallID string       `json:"tool_call_id,omitempty"`
}

// oaPart is one part of a user message with images (text, then image_url data URLs).
type oaPart struct {
	Type     string   `json:"type"`
	Text     string   `json:"text,omitempty"`
	ImageURL *oaImage `json:"image_url,omitempty"`
}

type oaImage struct {
	URL string `json:"url"`
}

type oaToolCall struct {
	Index    *int   `json:"index,omitempty"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

type oaTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

type oaRequest struct {
	Model           string      `json:"model"`
	Messages        []oaMessage `json:"messages"`
	Tools           []oaTool    `json:"tools,omitempty"`
	ToolChoice      string      `json:"tool_choice,omitempty"`
	Temperature     *float64    `json:"temperature,omitempty"`
	ReasoningEffort string      `json:"reasoning_effort,omitempty"`
	MaxTokens       int         `json:"max_tokens,omitempty"`
	Stream          bool        `json:"stream"`
}

func strp(s string) *string { return &s }

func (o *openAIClient) body(req Request) ([]byte, error) {
	r := oaRequest{Model: req.Model, Temperature: req.Temperature, Stream: true}
	if req.MaxTokens > 0 {
		r.MaxTokens = req.MaxTokens
	}
	if req.System != "" {
		r.Messages = append(r.Messages, oaMessage{Role: "system", Content: strp(req.System)})
	}
	for _, m := range req.Messages {
		switch m.Role {
		case RoleTool:
			r.Messages = append(r.Messages, oaMessage{Role: "tool", Content: strp(m.Content), ToolCallID: m.ToolCallID})
		case RoleAssistant:
			om := oaMessage{Role: "assistant", Content: strp(m.Content)}
			for _, tc := range m.ToolCalls {
				c := oaToolCall{ID: tc.ID, Type: "function"}
				c.Function.Name = tc.Name
				c.Function.Arguments = tc.Arguments
				if c.Function.Arguments == "" {
					c.Function.Arguments = "{}"
				}
				om.ToolCalls = append(om.ToolCalls, c)
			}
			r.Messages = append(r.Messages, om)
		default:
			if len(m.Images) == 0 {
				r.Messages = append(r.Messages, oaMessage{Role: "user", Content: strp(m.Content)})
				continue
			}
			parts := make([]oaPart, 0, 1+len(m.Images))
			parts = append(parts, oaPart{Type: "text", Text: m.Content})
			for _, im := range m.Images {
				parts = append(parts, oaPart{Type: "image_url", ImageURL: &oaImage{URL: im.dataURL()}})
			}
			r.Messages = append(r.Messages, oaMessage{Role: "user", Content: parts})
		}
	}
	for _, t := range req.Tools {
		ot := oaTool{Type: "function"}
		ot.Function.Name, ot.Function.Description, ot.Function.Parameters = t.Name, t.Description, t.Parameters
		r.Tools = append(r.Tools, ot)
	}
	if len(r.Tools) > 0 {
		r.ToolChoice = "auto"
	}
	if req.Caps.Reasoning == ReasonEffort {
		switch req.Thinking {
		case ThinkOff, ThinkLow:
			r.ReasoningEffort = "low"
		case ThinkMedium:
			r.ReasoningEffort = "medium"
		case ThinkHigh:
			r.ReasoningEffort = "high"
		}
	}
	return json.Marshal(r)
}

// Chat streams one completion.
func (o *openAIClient) Chat(ctx context.Context, req Request, onDelta func(Delta)) (Result, error) {
	body, err := o.body(req)
	if err != nil {
		return Result{}, &Error{Kind: KindBadResponse, Message: "can't encode the request"}
	}
	res, done, err := o.c.call(ctx, http.MethodPost, o.c.BaseURL+"/chat/completions", o.header(), body)
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
	var calls []*ToolCall
	out := Result{}
	mt, _, _ := mime.ParseMediaType(res.Header.Get("Content-Type"))
	if mt != "text/event-stream" {
		// a server that ignores stream:true answers with one JSON document
		var j struct {
			Choices []struct {
				Message struct {
					Content          *string      `json:"content"`
					ReasoningContent string       `json:"reasoning_content"`
					Reasoning        string       `json:"reasoning"`
					ToolCalls        []oaToolCall `json:"tool_calls"`
				} `json:"message"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		}
		b, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
		if err != nil {
			return Result{}, transportError(ctx, err)
		}
		if json.Unmarshal(b, &j) != nil || len(j.Choices) == 0 {
			return Result{}, &Error{Kind: KindBadResponse, Status: res.StatusCode, Message: "the answer is not a chat completion"}
		}
		m := j.Choices[0].Message
		addReasoning(m.ReasoningContent + m.Reasoning)
		if m.Content != nil {
			sp.write(*m.Content)
		}
		for _, tc := range m.ToolCalls {
			calls = append(calls, &ToolCall{ID: tc.ID, Name: tc.Function.Name, Arguments: tc.Function.Arguments})
		}
		out.StopReason = j.Choices[0].FinishReason
		if j.Usage != nil {
			out.Usage = Usage{Input: j.Usage.PromptTokens, Output: j.Usage.CompletionTokens}
		}
	} else {
		byIndex := map[int]*ToolCall{}
		err := readSSE(res.Body, func(ev sseEvent) (bool, error) {
			if ev.Data == "[DONE]" {
				return true, nil
			}
			var j struct {
				Choices []struct {
					Delta struct {
						Content          *string      `json:"content"`
						ReasoningContent string       `json:"reasoning_content"`
						Reasoning        string       `json:"reasoning"`
						ToolCalls        []oaToolCall `json:"tool_calls"`
					} `json:"delta"`
					FinishReason *string `json:"finish_reason"`
				} `json:"choices"`
				Usage *struct {
					PromptTokens     int `json:"prompt_tokens"`
					CompletionTokens int `json:"completion_tokens"`
				} `json:"usage"`
				Error json.RawMessage `json:"error"`
			}
			if json.Unmarshal([]byte(ev.Data), &j) != nil {
				return false, nil // legacy: lines that aren't JSON are skipped
			}
			if len(j.Error) > 0 && string(j.Error) != "null" {
				return true, statusError(500, []byte(ev.Data))
			}
			if j.Usage != nil {
				out.Usage = Usage{Input: j.Usage.PromptTokens, Output: j.Usage.CompletionTokens}
			}
			if len(j.Choices) == 0 {
				return false, nil
			}
			d := j.Choices[0].Delta
			addReasoning(d.ReasoningContent + d.Reasoning)
			if d.Content != nil {
				sp.write(*d.Content)
			}
			for _, tc := range d.ToolCalls {
				k := len(byIndex)
				if tc.Index != nil {
					k = *tc.Index
				}
				c := byIndex[k]
				if c == nil {
					c = &ToolCall{}
					byIndex[k] = c
					calls = append(calls, c)
				}
				if tc.ID != "" {
					c.ID = tc.ID
				}
				c.Name += tc.Function.Name
				c.Arguments += tc.Function.Arguments
			}
			if f := j.Choices[0].FinishReason; f != nil && *f != "" {
				out.StopReason = *f
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
	for _, c := range calls {
		if c.Name != "" {
			out.ToolCalls = append(out.ToolCalls, *c)
		}
	}
	if len(out.ToolCalls) == 0 { // models without native tool parsing print <tool_call> blocks
		out.ToolCalls = sp.calls
	}
	for i := range out.ToolCalls {
		if out.ToolCalls[i].ID == "" {
			out.ToolCalls[i].ID = callID()
		}
		if strings.TrimSpace(out.ToolCalls[i].Arguments) == "" {
			out.ToolCalls[i].Arguments = "{}"
		}
	}
	return out, nil
}

// Models lists the server's models. LM Studio's native list adds the context length and whether a
// model is an embedding model; other servers answer only the OpenAI list.
func (o *openAIClient) Models(ctx context.Context) ([]Model, error) {
	h := o.header()
	res, done, err := o.c.call(ctx, http.MethodGet, o.c.BaseURL+"/models", h, nil)
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
	if !o.lmStudioDetails(ctx, out) {
		o.ollamaDetails(ctx, out)
	}
	return out, nil
}

// ollamaShowMax caps the /api/show requests of one model list.
const ollamaShowMax = 30

// ollamaDetails asks Ollama's native API (/api/show next to /v1) what each model can do: tools,
// vision, embeddings (the context length is Ollama's own setting, OLLAMA_CONTEXT_LENGTH). Ollama
// refuses tools to a model whose template has none, so its list is taken as it is, both ways.
// Any error leaves a model as it is.
func (o *openAIClient) ollamaDetails(ctx context.Context, models []Model) {
	root := strings.TrimSuffix(o.c.BaseURL, "/v1")
	if root == o.c.BaseURL {
		return
	}
	for i := range models {
		if i == ollamaShowMax || ctx.Err() != nil {
			return
		}
		body, _ := json.Marshal(map[string]string{"model": models[i].ID})
		res, done, err := o.c.call(ctx, http.MethodPost, root+"/api/show", o.header(), body)
		if err != nil {
			return // not Ollama, or unreachable: the next ones won't answer either
		}
		var j struct {
			Capabilities []string `json:"capabilities"`
		}
		ok := res.StatusCode/100 == 2 && json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&j) == nil
		_ = res.Body.Close()
		done()
		if !ok {
			if i == 0 {
				return
			}
			continue
		}
		if j.Capabilities != nil {
			tools, vision := false, false
			for _, c := range j.Capabilities {
				switch c {
				case "tools":
					tools = true
				case "vision":
					vision = true
				case "embedding":
					models[i].Embedding = true
				}
			}
			models[i].ToolUse, models[i].Vision = &tools, &vision
		}
	}
}

// lmStudioDetails asks LM Studio's native REST API (/api/v0/models next to /v1) for each model's
// type, context length, tool support and vision. Any error leaves the list as it is; false when
// the server isn't LM Studio.
func (o *openAIClient) lmStudioDetails(ctx context.Context, models []Model) bool {
	root := strings.TrimSuffix(o.c.BaseURL, "/v1")
	if root == o.c.BaseURL || len(models) == 0 {
		return false
	}
	res, done, err := o.c.call(ctx, http.MethodGet, root+"/api/v0/models", o.header(), nil)
	if err != nil {
		return false
	}
	defer done()
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode/100 != 2 {
		return false
	}
	var j struct {
		Data []struct {
			ID               string   `json:"id"`
			Type             string   `json:"type"`
			MaxContextLength int      `json:"max_context_length"`
			LoadedContext    int      `json:"loaded_context_length"`
			Capabilities     []string `json:"capabilities"`
		} `json:"data"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&j) != nil {
		return false
	}
	for _, d := range j.Data {
		for i := range models {
			if models[i].ID != d.ID {
				continue
			}
			models[i].Embedding = d.Type == "embeddings"
			if d.LoadedContext > 0 {
				models[i].ContextTokens = d.LoadedContext
			} else if d.MaxContextLength > 0 {
				models[i].ContextTokens = d.MaxContextLength
			}
			// only a positive report counts: LM Studio also offers tools to models without native
			// support (it describes them in the prompt), so a missing tool_use doesn't mean plan mode
			for _, c := range d.Capabilities {
				if c == "tool_use" {
					t := true
					models[i].ToolUse = &t
				}
			}
			v := d.Type == "vlm" // LM Studio's type for vision models
			models[i].Vision = &v
		}
	}
	return true
}

func callID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return "call_" + hex.EncodeToString(b[:])
}
