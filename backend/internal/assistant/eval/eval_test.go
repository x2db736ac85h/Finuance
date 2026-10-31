package eval

import (
	"context"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/assistant"
	"github.com/x2db736ac85h/finuance/backend/internal/config"
	"github.com/x2db736ac85h/finuance/backend/internal/llm"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
	"github.com/x2db736ac85h/finuance/backend/testutil"
	"github.com/x2db736ac85h/finuance/backend/testutil/llmmock"
)

var testDB *testutil.DB

func TestMain(m *testing.M) { testutil.Main(m, &testDB) }

const questionsPath = "../../../../evals/assistant/questions.json"

func TestNumbers(t *testing.T) {
	for _, c := range []struct {
		text string
		want []int64
	}{
		{"Wydałeś 5 569,62 zł w lutym.", []int64{556962}},
		{"You spent PLN 5,569.62.", []int64{556962}},
		{"about 5570 PLN", []int64{557000}},
		{"Stopa oszczędności: 41,9 %", []int64{4190}},
		{"128 transakcji", []int64{12800}},
		{"1.234.567,89 and 2\u00a0850,00", []int64{123456789, 285000}},
	} {
		var got []int64
		for _, n := range Numbers(c.text) {
			got = append(got, n.V)
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("Numbers(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

func TestGrade(t *testing.T) {
	q := Question{Tools: []string{"aggregate_transactions"}, Args: []map[string]string{{"date_from": "2025-02-01", "date_to": "2025-02-28"},
		{"month": "2025-02"}}, Mention: "luty|february"}
	spent := Cents{V: 556962, Fraction: true}
	exp := []Expected{{Path: "0:total_spent", Number: &spent}, {Path: "0:groups.0.key", Text: "Artykuły spożywcze"}}
	good := []Lookup{{Tool: "find_values", Args: map[string]any{}}, {Tool: "aggregate_transactions",
		Args: map[string]any{"date_from": "2025-02-01", "date_to": "2025-02-28T23:59:59", "group_by": "category"}}}
	if s := Grade(q, good, "In February you spent **5 569,62 PLN**, mostly on artykuly spozywcze.", exp); !s.OK() {
		t.Errorf("good run = %+v", s)
	}
	if s := Grade(q, good, "In February you spent about 5 570 PLN on Artykuły spożywcze.", exp); !s.OK() {
		t.Errorf("rounded = %+v", s)
	}
	if s := Grade(q, good, "In February you spent 5 600 PLN on Artykuły spożywcze.", exp); s.AnswerOK || s.Missing[0] != "0:total_spent" {
		t.Errorf("wrong number = %+v", s)
	}
	if s := Grade(q, []Lookup{{Tool: "aggregate_transactions", Args: map[string]any{"date_from": "2025-01-01"}}}, "", nil); s.ArgsOK || !s.ToolOK {
		t.Errorf("wrong dates = %+v", s)
	}
	if s := Grade(q, []Lookup{{Tool: "month_overview", Args: map[string]any{"month": "2025-02"}}}, "February: 5569.62 artykuły spożywcze", exp); s.ToolOK || !s.ArgsOK {
		t.Errorf("other tool = %+v", s)
	}
	contains := Question{Args: []map[string]string{{"payee|query": "~kawiarnia"}}}
	if !Grade(contains, []Lookup{{Tool: "search_transactions", Args: map[string]any{"query": "KAWIARNIA Podwórko"}}}, "", nil).ArgsOK {
		t.Error("contains on either key")
	}
}

// demo is one user with the demo year and the oracle's groups.
var demo struct {
	once sync.Once
	user uuid.UUID
	st   *store.Store
}

func demoUser(t *testing.T) (*store.Store, uuid.UUID) {
	t.Helper()
	tdb := testutil.Require(t, testDB)
	demo.once.Do(func() {
		ctx := context.Background()
		st := store.New(tdb.App)
		demo.st = st
		u, err := SetupDemo(ctx, st, filepath.Join("..", "..", "..", "..", "testdata", "fixtures", "demo", "demo-legacy.csv"))
		if err != nil {
			t.Fatal(err)
		}
		demo.user = u
	})
	if demo.user == uuid.Nil {
		t.Fatal("demo setup failed earlier")
	}
	return demo.st, demo.user
}

// TestQuestionsFile checks the suite itself: enough questions in both languages, unique ids, and
// every reference lookup resolves on the demo data.
func TestQuestionsFile(t *testing.T) {
	st, u := demoUser(t)
	f, err := Load(questionsPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Questions) < 40 || !f.Today.Equal(time.Date(2025, 3, 20, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("%d questions, today %s", len(f.Questions), f.Today)
	}
	tools := assistant.NewTools(query.New(st))
	ids, langs := map[string]bool{}, map[string]int{}
	for _, q := range f.Questions {
		if ids[q.ID] {
			t.Errorf("duplicate id %s", q.ID)
		}
		ids[q.ID] = true
		langs[q.Lang]++
		if len(q.Expect) == 0 && q.Mention == "" {
			t.Errorf("%s checks nothing in the answer", q.ID)
		}
		exp, err := Expectations(context.Background(), tools, u, q)
		if err != nil {
			t.Errorf("%v", err)
			continue
		}
		for _, e := range exp {
			if e.Number != nil && e.Number.V == 0 {
				t.Errorf("%s: %s is zero; the question would pass on any answer with a 0", q.ID, e.Path)
			}
		}
	}
	if langs["pl"] < 15 || langs["en"] < 15 {
		t.Errorf("languages = %v", langs)
	}
}

// TestRunWithMock runs two questions through the real run loop against scripted mock answers:
// one right, one wrong.
func TestRunWithMock(t *testing.T) {
	st, u := demoUser(t)
	mock := llmmock.New()
	srv := httptest.NewServer(mock)
	defer srv.Close()
	base, _ := url.Parse(srv.URL + "/v1")
	clock := platform.FixedClock{T: time.Date(2025, 3, 20, 10, 0, 0, 0, time.UTC)}
	a := assistant.New(assistant.Config{Store: st, Query: query.New(st), Clock: clock, MaxChats: 100, MaxConcurrent: 1,
		Connections: assistant.NewConnections([]config.AIConnection{{Name: "mock", API: llm.OpenAI, BaseURL: base, ContextTokens: 32768}},
			10*time.Second, 0, clock, nil)})
	f, err := Load(questionsPath)
	if err != nil {
		t.Fatal(err)
	}
	var sel []Question
	for _, q := range f.Questions {
		if q.ID == "rel-last-month-en" || q.ID == "cat-top-feb-en" {
			sel = append(sel, q)
		}
	}
	f.Questions = sel
	mock.Push(
		llmmock.Reply{Calls: []llmmock.Call{{Name: "aggregate_transactions", Arguments: `{"date_from":"2025-02-01","date_to":"2025-02-28"}`}}},
		llmmock.Reply{Content: "Last month (2025-02-01 → 2025-02-28) you spent **5 569,62 PLN**."},
		llmmock.Reply{Content: "Probably groceries, about 900 PLN."}, // no lookup, wrong number
	)
	cases, err := Run(context.Background(), a, u, f, assistant.Request{Connection: "mock", Model: "mock-tools"}, func(Case) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 2 || !cases[0].OK || cases[1].OK || cases[1].ToolOK || cases[1].AnswerOK {
		t.Fatalf("cases = %+v", cases)
	}
	r := Result{Connection: "mock", Model: "mock-tools", Think: "auto", RanAt: clock.T, Cases: cases}
	rep := Report([]Result{r})
	for _, want := range []string{"| mock | mock-tools | auto | full | 1 / 2 (50 %)", "## Misses: mock-tools", "`cat-top-feb-en`: tool; arguments; answer"} {
		if !strings.Contains(rep, want) {
			t.Errorf("report lacks %q:\n%s", want, rep)
		}
	}
	if s := Summarize(r); s.ByCategory["relative dates"] != [2]int{1, 1} {
		t.Errorf("summary = %+v", s)
	}
}

func TestRunsOfOneModelStayApart(t *testing.T) {
	full := Result{Connection: "ollama", Model: "qwen3:4b", Think: "off"}
	compact := full
	compact.Toolset = "compact"
	plan := full
	plan.Mode = "plan"
	if full.FileName() != "ollama__qwen3_4b__off.json" { // unchanged for the default toolset and mode
		t.Errorf("full: %s", full.FileName())
	}
	if compact.FileName() != "ollama__qwen3_4b__off__compact.json" || plan.FileName() != "ollama__qwen3_4b__off__plan.json" {
		t.Errorf("compact: %s, plan: %s", compact.FileName(), plan.FileName())
	}
	rep := Report([]Result{full, compact})
	for _, want := range []string{"| ollama | qwen3:4b | off | full |", "| ollama | qwen3:4b | off | compact |",
		"## Misses: qwen3:4b\n", "## Misses: qwen3:4b · compact\n"} {
		if !strings.Contains(rep, want) {
			t.Errorf("report lacks %q:\n%s", want, rep)
		}
	}
}

func TestReportWithoutRuns(t *testing.T) {
	if !strings.Contains(Report(nil), "No runs yet") {
		t.Error(Report(nil))
	}
}
