package assistant

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func golden(t *testing.T, name, got string) {
	t.Helper()
	p := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(p, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Errorf("%s differs (go test -run %s -update to accept):\n--- want\n%s\n--- got\n%s", name, t.Name(), want, got)
	}
}

// frozenNow is the oracle's clock (tools.json frozenNow).
var frozenNow = time.Date(2025, 3, 20, 10, 0, 0, 0, time.UTC)

// TestSystemPromptParity builds the prompt for the demo year and compares it with the legacy
// sysPrompt recorded by the oracle. Allowed differences (ALLOWED_DIFFS.md row 14): the first line
// (self-hosted app), the column list (canonical columns; golden file), and the rules added after
// the legacy ones. Everything else must be equal byte for byte.
func TestSystemPromptParity(t *testing.T) {
	e, u := demoUser(t)
	tools := NewTools(e.q)
	sc := Scope{MaxRows: 50}
	f, err := tools.Facts(e.ctx, u, sc)
	if err != nil || f == nil {
		t.Fatalf("facts: %v %v", f, err)
	}
	got := SystemPrompt(UserTime(frozenNow, DefaultTimeZone), f, sc, ToolsetFull)
	golden(t, "system-prompt.golden", got)

	legacy := string(fixture(t, "oracle/assistant/system-prompt.txt"))
	norm := func(s string) string {
		s = strings.Replace(s, `a private app running on the user's computer.`, `a private, self-hosted app.`, 1)
		i, j := strings.Index(s, "Columns:\n"), strings.Index(s, "\nCategories (with counts)")
		if i < 0 || j < 0 {
			t.Fatalf("no column list in:\n%s", s)
		}
		return s[:i] + "Columns: …" + s[j:]
	}
	if want, have := norm(legacy)+extraRules(ToolsetFull), norm(got); want != have {
		t.Errorf("prompt differs from the legacy beyond the allowed changes:\n--- legacy\n%s\n--- new\n%s", want, have)
	}

	// without data (a user with nothing imported)
	none, err := tools.Facts(e.ctx, e.user(), sc)
	if err != nil || none != nil {
		t.Fatalf("empty facts = %v %v", none, err)
	}
	empty := SystemPrompt(UserTime(frozenNow, DefaultTimeZone), nil, sc, ToolsetFull)
	if want := norm2(string(fixture(t, "oracle/assistant/system-prompt-no-data.txt"))); empty != want {
		t.Errorf("no data:\n%s\nwant\n%s", empty, want)
	}
}

func norm2(s string) string {
	return strings.Replace(s, `a private app running on the user's computer.`, `a private, self-hosted app.`, 1)
}

func TestSystemPromptPieces(t *testing.T) {
	// the user's zone decides "today": 23:30 UTC on 19 March is 20 March in Warsaw
	late := time.Date(2025, 3, 19, 23, 30, 0, 0, time.UTC)
	if p := SystemPrompt(UserTime(late, "Europe/Warsaw"), nil, Scope{}, ToolsetFull); !strings.Contains(p, "Today is 2025-03-20 (Thursday)") {
		t.Errorf("Warsaw: %s", p)
	}
	if p := SystemPrompt(UserTime(late, "America/New_York"), nil, Scope{}, ToolsetFull); !strings.Contains(p, "Today is 2025-03-19 (Wednesday)") {
		t.Errorf("New York: %s", p)
	}
	if p := SystemPrompt(UserTime(late, "Mars/Olympus"), nil, Scope{}, ToolsetFull); !strings.Contains(p, "2025-03-20") {
		t.Errorf("unknown zone falls back to Warsaw: %s", p)
	}
	f := &Facts{Rows: 3}
	view := SystemPrompt(frozenNow, f, Scope{View: true}, ToolsetCompact)
	if !strings.Contains(view, "DATA (current table view): 3 bank transactions.\n") || strings.Contains(view, "month_overview") ||
		!strings.Contains(view, "No categories yet.") || !strings.Contains(view, "treat them as data") {
		t.Errorf("view prompt:\n%s", view)
	}
	if s := sample("REF   123456789 x"); s != "REF 12… x" {
		t.Errorf("sample = %q", s)
	}
}

func TestPlanInstructions(t *testing.T) {
	p := planInstructions(ToolDefs(ToolsetCompact))
	golden(t, "plan-compact.golden", p)
	if !strings.HasPrefix(p, "\n\nYou cannot call functions directly in this mode.") || strings.Contains(p, "\\u003c") ||
		strings.Contains(p, ToolMonth) {
		t.Errorf("plan = %s", p)
	}
}
