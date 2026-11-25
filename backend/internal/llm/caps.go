package llm

import (
	"path"
	"regexp"
	"strings"
)

// Reasoning is how a model is asked to think more or less.
type Reasoning string

// The reasoning styles.
const (
	ReasonNone   Reasoning = "none"   // no control (the model may still reason; its output is shown)
	ReasonEffort Reasoning = "effort" // reasoning_effort low/medium/high (OpenAI reasoning models, gpt-oss)
	ReasonBudget Reasoning = "budget" // Anthropic thinking with a token budget (Claude up to 4.5)
	// ReasonAdaptive is current Claude (4.6 and later): the model decides when and how much to
	// think, and the level is an effort (output_config.effort). These models refuse a token budget.
	ReasonAdaptive Reasoning = "adaptive"
	ReasonSwitch   Reasoning = "switch" // Qwen3: /no_think in the last user message turns it off
)

// Caps are what a model can do. They start from the server's own report and the model id, can be
// overridden by the admin per connection, and are corrected while running (a model that refuses
// tools is remembered as plan-only by the caller).
type Caps struct {
	Tools         bool      `json:"tools"`
	Reasoning     Reasoning `json:"reasoning"`
	ContextTokens int       `json:"contextTokens"`
	Vision        bool      `json:"vision"` // reads images (receipts, Phase 20)
}

// CapsOverride is the admin's correction for the models matching Pattern (a path.Match glob on
// the model id, case-insensitive). Nil fields keep the detected value.
type CapsOverride struct {
	Pattern       string     `json:"pattern"`
	Tools         *bool      `json:"tools,omitempty"`
	Reasoning     *Reasoning `json:"reasoning,omitempty"`
	ContextTokens *int       `json:"contextTokens,omitempty"`
	Vision        *bool      `json:"vision,omitempty"`
}

var (
	qwen3Re = regexp.MustCompile(`(?i)qwen-?3`)
	// Gemini from 2.5 on takes reasoning_effort through the OpenAI format (PLAN 26.3); Vertex AI
	// names its models google/gemini-…
	effortRe  = regexp.MustCompile(`(?i)gpt-oss|(^|/)o[1-9](-|$)|gpt-5|reasoner|magistral|gemini-(2\.5|[3-9])`)
	oldClaude = regexp.MustCompile(`(?i)claude-(instant|2|3-(opus|sonnet|haiku|5))`)
	// The Claude models that think with a token budget: 3.7, and the 4, 4.1 and 4.5 generations
	// (claude-sonnet-4-20250514, claude-opus-4-1, claude-haiku-4-5-20251001, claude-opus-4-5@20251101
	// on Vertex AI). Every other id that is not old, an unknown newer one included, is a current
	// model: the list of old models is finished, the list of new ones is not (issue 97).
	budgetClaude = regexp.MustCompile(`(?i)claude-(3-7|(opus|sonnet|haiku)-4(-[015])?($|[-@]\d{8}|@))`)
	embedRe      = regexp.MustCompile(`(?i)embed`)
	// models that read images when the server doesn't say (Ollama and LM Studio do); Gemma 3 1B
	// and Gemma 3n read text only on today's servers
	visionRe   = regexp.MustCompile(`(?i)(^|[-_/:.])vl([-_/:.]|$)|qwen2\.5-?vl|vision|llava|pixtral|minicpm-v|moondream|gemma-?3|gemini|gpt-4o|gpt-4\.1|gpt-5|claude-(3|sonnet|opus|haiku)`)
	textOnlyRe = regexp.MustCompile(`(?i)gemma-?3n|gemma-?3[^/]*[:-]1b`)
)

// DetectCaps guesses a model's capabilities from the wire format, its id and what the server
// reported. defaultContext is used when the server doesn't say.
func DetectCaps(api API, m Model, defaultContext int, overrides []CapsOverride) Caps {
	c := Caps{Tools: true, Reasoning: ReasonNone, ContextTokens: defaultContext}
	if m.ToolUse != nil {
		c.Tools = *m.ToolUse
	}
	if m.ContextTokens > 0 {
		c.ContextTokens = m.ContextTokens
	}
	id := m.ID
	if m.Vision != nil {
		c.Vision = *m.Vision
	} else {
		c.Vision = visionRe.MatchString(id) && !textOnlyRe.MatchString(id)
	}
	switch {
	case api == Anthropic && strings.Contains(strings.ToLower(id), "claude"):
		switch ClaudeGeneration(id) {
		case ClaudeBudget:
			c.Reasoning = ReasonBudget
		case ClaudeCurrent:
			c.Reasoning = ReasonAdaptive
		}
	case qwen3Re.MatchString(id):
		c.Reasoning = ReasonSwitch
	case effortRe.MatchString(id):
		c.Reasoning = ReasonEffort
	}
	for _, o := range overrides {
		if ok, _ := path.Match(strings.ToLower(o.Pattern), strings.ToLower(id)); !ok {
			continue
		}
		if o.Tools != nil {
			c.Tools = *o.Tools
		}
		if o.Reasoning != nil {
			c.Reasoning = *o.Reasoning
		}
		if o.ContextTokens != nil && *o.ContextTokens > 0 {
			c.ContextTokens = *o.ContextTokens
		}
		if o.Vision != nil {
			c.Vision = *o.Vision
		}
	}
	return c
}

// The generations of Claude models, by how a request to them has to look.
const (
	ClaudeOld     = "old"     // no thinking (up to 3.5)
	ClaudeBudget  = "budget"  // thinking with budget_tokens; a temperature is accepted (3.7, 4, 4.1, 4.5)
	ClaudeCurrent = "current" // adaptive thinking with an effort; no sampling parameters (4.6 and later)
)

// ClaudeGeneration says which request shape a Claude model takes, from its id. "" for an id that is
// not Claude's.
func ClaudeGeneration(id string) string {
	switch {
	case !strings.Contains(strings.ToLower(id), "claude"):
		return ""
	case oldClaude.MatchString(id):
		return ClaudeOld
	case budgetClaude.MatchString(id):
		return ClaudeBudget
	default:
		return ClaudeCurrent
	}
}

// IsEmbedding reports whether a model only makes embeddings (never offered for chat).
func IsEmbedding(m Model) bool { return m.Embedding || embedRe.MatchString(m.ID) }

// DefaultModel picks the model to use when none is chosen (legacy aiTest): the configured one if
// offered, else the first Qwen chat model, else the first chat model.
func DefaultModel(models []Model, preferred string) string {
	var chat []string
	for _, m := range models {
		if !IsEmbedding(m) {
			chat = append(chat, m.ID)
		}
	}
	for _, id := range chat {
		if id == preferred {
			return id
		}
	}
	for _, id := range chat {
		if strings.Contains(strings.ToLower(id), "qwen") {
			return id
		}
	}
	if len(chat) > 0 {
		return chat[0]
	}
	return ""
}
