package assistant

import (
	"strings"
	"testing"

	"github.com/x2db736ac85h/finuance/backend/internal/llm"
)

// TestCurrentClaudeModelsThroughTheRunner (issue 97): a question to a current Claude model ends in
// an answer. The mock refuses, as Anthropic's API does, a token budget and a temperature from those
// models and "disabled" thinking from some: the runner's default temperature of 0.2 and the budget
// every Claude id used to get made every such question fail with "the model server reported an
// error". Every thinking level is asked for, "off" among them.
func TestCurrentClaudeModelsThroughTheRunner(t *testing.T) {
	re := newRunEnv(t, 2)
	re.mock.Models = append(re.mock.Models, "claude-haiku-5-5", "claude-opus-5-5", "claude-sonnet-5-5", "claude-fable-5-2", "claude-haiku-4-5")
	re.conns.Forget("claude")
	for _, model := range []string{"claude-haiku-5-5", "claude-opus-5-5", "claude-sonnet-5-5", "claude-fable-5-2", "claude-haiku-4-5"} {
		for _, think := range []llm.Thinking{llm.ThinkAuto, llm.ThinkOff, llm.ThinkHigh} {
			evs := re.ask(re.newChat(), "How much did I spend on coffee?", func(r *Request) {
				r.Connection, r.Model, r.Think = "claude", model, think
			})
			if last(evs).Name != "done" {
				t.Errorf("%s, thinking %s: events %s", model, think, names(evs))
				continue
			}
			reqs := re.mock.Requests()
			body := string(reqs[len(reqs)-1])
			current := model != "claude-haiku-4-5"
			if current && (strings.Contains(body, `"temperature"`) || strings.Contains(body, `"budget_tokens"`) ||
				!strings.Contains(body, `"thinking":{"type":"adaptive"`) || strings.Contains(body, `"disabled"`)) {
				t.Errorf("%s, thinking %s: the body is not a current model's: %.300s", model, think, body)
			}
			if !current && think == llm.ThinkHigh && !strings.Contains(body, `"budget_tokens":16000`) {
				t.Errorf("%s keeps its token budget: %.300s", model, body)
			}
		}
	}
}
