package llmmock

import (
	"encoding/json"
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
	if thinking.Type == "disabled" && claudeNoDisabled.MatchString(id) {
		return "thinking.type.disabled is not supported for this model."
	}
	if (choice.Type == "any" || choice.Type == "tool") && claudeNoForced.MatchString(id) {
		return `tool_choice: type "tool" and "any" are not supported for this model.`
	}
	return ""
}
