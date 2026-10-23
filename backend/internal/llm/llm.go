// Package llm talks to chat models over the two wire formats that cover nearly every server
// (PLAN §4.8, ADR 0015): OpenAI Chat Completions (LM Studio, Ollama, vLLM, llama.cpp, OpenAI,
// Gemini, OpenRouter, Mistral, …) and Anthropic Messages (Anthropic, LM Studio /v1/messages).
//
// Callers see one provider-neutral chat model: messages with text, reasoning, tool calls and tool
// results, a stream of text and reasoning deltas, and errors classified by kind. Each adapter
// tolerates its servers' quirks the way the legacy client did: reasoning as a separate field or in
// <think> tags, tool calls streamed in pieces or printed as Qwen <tool_call> text, and
// non-streaming JSON answers.
//
// Nothing here logs prompts, model output or tool results.
package llm

import (
	"context"
	"encoding/json"
)

// API is a wire format.
type API string

// The supported wire formats.
const (
	OpenAI    API = "openai"    // POST {base}/chat/completions, GET {base}/models
	Anthropic API = "anthropic" // POST {base}/v1/messages, GET {base}/v1/models
)

// Role of a message in the conversation; the system prompt travels in Request.System.
type Role string

// The roles.
const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ToolCall is one function call the model asked for. Arguments is the raw JSON text as the model
// wrote it (it may not be valid JSON; the caller decides what to do with it).
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Message is one turn of the conversation.
type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content,omitempty"`
	// ToolCalls are the calls of an assistant message.
	ToolCalls []ToolCall `json:"toolCalls,omitempty"`
	// ToolCallID links a tool message to its call.
	ToolCallID string `json:"toolCallId,omitempty"`
	// Replay is provider data that must be sent back unchanged with this assistant message, such
	// as Anthropic thinking blocks and their signatures inside a tool loop.
	Replay json.RawMessage `json:"replay,omitempty"`
}

// Tool is a function the model may call; Parameters is a JSON schema object.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// Thinking is how much the model should reason, when it can.
type Thinking string

// The thinking levels. Auto leaves the model's default; Off asks it not to reason where possible.
const (
	ThinkOff    Thinking = "off"
	ThinkAuto   Thinking = "auto"
	ThinkLow    Thinking = "low"
	ThinkMedium Thinking = "medium"
	ThinkHigh   Thinking = "high"
)

// Request is one model call.
type Request struct {
	Model       string
	System      string
	Messages    []Message
	Tools       []Tool
	Temperature *float64
	Thinking    Thinking
	Caps        Caps // what the model can do (reasoning style decides how Thinking is sent)
	MaxTokens   int  // answer budget; 0 = the adapter's default
}

// Delta is a piece of streamed output.
type Delta struct {
	Content   string
	Reasoning string
}

// Usage counts tokens when the server reports them (0 otherwise).
type Usage struct {
	Input  int `json:"input"`
	Output int `json:"output"`
}

// Result is a finished model call.
type Result struct {
	Content    string
	Reasoning  string
	ToolCalls  []ToolCall
	Replay     json.RawMessage
	StopReason string
	Usage      Usage
}

// Model is one model a server offers.
type Model struct {
	ID            string `json:"id"`
	ContextTokens int    `json:"contextTokens,omitempty"` // 0 = unknown
	Embedding     bool   `json:"-"`
	ToolUse       *bool  `json:"-"` // what the server says, when it says it
}

// Client is one connection to a model server.
type Client interface {
	API() API
	// Models lists the server's models (embedding models included, marked).
	Models(ctx context.Context) ([]Model, error)
	// Chat runs one model call, streaming deltas to onDelta (which may be nil).
	Chat(ctx context.Context, req Request, onDelta func(Delta)) (Result, error)
}
