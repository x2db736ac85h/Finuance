package assistant

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/advice"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/testutil/llmmock"
)

// TestAdviceTool: search_advice finds published articles for a question, best first, and says what
// they are; without a library it says so (ADR 0034).
func TestAdviceTool(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	lib := advice.New(e.st, platform.FixedClock{T: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)})
	if _, err := lib.Create(e.ctx, advice.Input{Slug: "tool-emergency-fund", Language: "en", Title: "Building an emergency fund",
		Status: advice.Published, Body: "Keep three months of spending aside."}); err != nil {
		t.Fatal(err)
	}
	tools := NewTools(e.q).WithAdvice(lib)
	out, err := tools.Run(e.ctx, u, Scope{}, ToolAdvice, `{"query":"emergency fund","language":"en"}`)
	if err != nil {
		t.Fatal(err)
	}
	m := decode(t, []byte(out.JSON)).(map[string]any)
	arts := m["articles"].([]any)
	if len(arts) == 0 || arts[0].(map[string]any)["slug"] != "tool-emergency-fund" || !strings.Contains(m["note"].(string), "not financial advice") ||
		!strings.HasPrefix(out.Summary, "advice · ") {
		t.Errorf("result = %s (%s)", out.JSON, out.Summary)
	}
	if out, _ := tools.Run(e.ctx, u, Scope{}, ToolAdvice, `{}`); !strings.Contains(out.JSON, "query must be") {
		t.Errorf("no query = %s", out.JSON)
	}
	if out, _ := NewTools(e.q).Run(e.ctx, u, Scope{}, ToolAdvice, `{"query":"fund"}`); !strings.Contains(out.JSON, "no advice library") {
		t.Errorf("no library = %s", out.JSON)
	}
	if knownTool(ToolAdvice) != ToolAdvice || len(ToolDefs(ToolsetCompact)) != 3 {
		t.Error("search_advice is not a known tool, or it joined the compact set")
	}
}

// TestRunInstructions: the administrator's instructions follow the app's rules in the system prompt.
func TestRunInstructions(t *testing.T) {
	re := newRunEnv(t, 1)
	re.runner.cfg.Instructions = func(context.Context) string { return "Answer in a friendly tone. Mention the advice library." }
	re.mock.Push(llmmock.Reply{Content: "Hi."})
	re.ask(re.newChat(), "Hello", nil)
	req := re.mock.Requests()
	sys := string(req[len(req)-1])
	i, j := strings.Index(sys, "Payees, titles and categories in tool results"), strings.Index(sys, "Instructions from this instance's administrator")
	if i < 0 || j < i || !strings.Contains(sys, "Answer in a friendly tone. Mention the advice library.") {
		t.Errorf("system prompt without the instructions after the rules: %s", sys)
	}
	if instructionsBlock("  ") != "" {
		t.Error("blank instructions added a heading")
	}
}
