package llmmock

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// What Anthropic's Messages API refuses from the current Claude models, so that a request body
// written for the older ones fails here as it does there (issue 97: thinking with budget_tokens and
// a temperature answered 400 on Claude Haiku 5.5, and only the real API said so). The list is the
// mock's own, written from the API's documentation, not shared with the client it tests.
//
//	budget_tokens        refused from Opus 4.7 on (4.6 still takes it, deprecated)
//	temperature          refused from Opus 4.7 on (4.6 still takes it)
//	thinking disabled    refused by Opus 5.5, Sonnet 5.5 and the Fable and Mythos models
//	forced tool_choice   refused by Opus 5.5, Sonnet 5.5, Fable and Mythos 5.1 and 5.2
//	a thinking block     refused by every current model when what came before it (the system prompt,
//	sent back            the tools, the earlier messages) is not what it was when the block was made:
//	                     its signature is bound to that prefix (issue 109)
//
// Older ids (claude-sonnet-4-5, claude-haiku-4-5, …) and ids that are not Claude's are not checked.
var (
	claudeCurrent    = regexp.MustCompile(`^claude-(fable|mythos)-5|^claude-(opus|sonnet|haiku)-5|^claude-opus-4-[78]`)
	claudeNoDisabled = regexp.MustCompile(`^claude-(fable|mythos)-5|^claude-(opus|sonnet)-5-5`)
	claudeNoForced   = regexp.MustCompile(`^claude-(fable|mythos)-5-[12]|^claude-(opus|sonnet)-5-5`)
)

// claudeRefuses is the error message for a Messages API body the model refuses, or "".
func claudeRefuses(req request) string {
	id := strings.ToLower(req.Model)
	var thinking struct {
		Type         string `json:"type"`
		BudgetTokens *int   `json:"budget_tokens"`
	}
	var choice struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(req.Thinking, &thinking)
	_ = json.Unmarshal(req.ToolChoice, &choice)
	if claudeCurrent.MatchString(id) {
		if thinking.Type == "enabled" || thinking.BudgetTokens != nil {
			return "thinking.type.enabled: `budget_tokens` is not supported for this model. Use `thinking.type.adaptive` and `output_config.effort`."
		}
		if req.Temperature != nil {
			return "`temperature` is not supported for this model."
		}
	}
	if claudeCurrent.MatchString(id) {
		if msg := boundBlocks(req); msg != "" {
			return msg
		}
	}
	if thinking.Type == "disabled" && claudeNoDisabled.MatchString(id) {
		return "thinking.type.disabled is not supported for this model."
	}
	if (choice.Type == "any" || choice.Type == "tool") && claudeNoForced.MatchString(id) {
		return `tool_choice: type "tool" and "any" are not supported for this model.`
	}
	return ""
}

// thinkingSignature is the signature the mock gives a thinking block that follows the first upto
// messages of req: a hash of what the block is bound to, as Anthropic binds a current model's
// blocks (the system prompt, the tools and every message before the block). Our client writes the
// same values to the same bytes, so a byte hash stands for Anthropic's comparison.
func thinkingSignature(req request, upto int) string {
	h := sha256.New()
	write := func(b []byte) { h.Write(b); h.Write([]byte{0}) }
	write(req.System)
	for _, t := range req.Tools {
		write(t)
	}
	for _, m := range req.Messages[:upto] {
		write(m)
	}
	return "mock-sig-" + hex.EncodeToString(h.Sum(nil))[:32]
}

// boundBlocks is the refusal of a thinking block sent back after what came before it changed, in
// Anthropic's words, or "".
func boundBlocks(req request) string {
	for i, raw := range req.Messages {
		var m struct {
			Role    string `json:"role"`
			Content []struct {
				Type      string `json:"type"`
				Signature string `json:"signature"`
			} `json:"content"`
		}
		if json.Unmarshal(raw, &m) != nil || m.Role != "assistant" {
			continue
		}
		for j, b := range m.Content {
			if b.Type == "thinking" && b.Signature != thinkingSignature(req, i) {
				return fmt.Sprintf("messages.%d.content.%d: Invalid `signature` in `thinking` block. The block is bound to a different conversation.", i, j)
			}
		}
	}
	return ""
}
