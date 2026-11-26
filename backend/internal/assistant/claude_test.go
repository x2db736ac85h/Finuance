package assistant

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/x2db736ac85h/finuance/backend/internal/config"
	"github.com/x2db736ac85h/finuance/backend/internal/llm"
	"github.com/x2db736ac85h/finuance/backend/internal/observability"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
	"github.com/x2db736ac85h/finuance/backend/testutil/llmmock"
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

// claudeNow is a run on a current Claude model through the mock, which binds every thinking block
// to what came before it and refuses a block sent back after that changed, as Anthropic's API does.
func (re *runEnv) claudeNow() func(*Request) {
	re.t.Helper()
	re.mock.Models = append(re.mock.Models, "claude-haiku-5-5")
	re.conns.Forget("claude")
	return func(r *Request) { r.Connection, r.Model = "claude", "claude-haiku-5-5" }
}

// thinkingBlocks counts the thinking blocks a request to the mock sent back.
func thinkingBlocks(body []byte) int { return strings.Count(string(body), `"type":"thinking"`) }

// TestClaudeSecondQuestionAfterAChange (issue 109): the second question in a chat, after a first
// that looked something up, ends in an answer although something changed in between: the day, or
// the panel's scope (an applied proposal or an import does the same: they change the facts in the
// system prompt). The mock refused the first question's thinking block, which went back with every
// later question although the system prompt before it was no longer the one it was made with.
// Within a question its own block still goes back, unchanged: the API needs it while the tool
// round is open. The refusal also sent the chat to plan mode and taught the connection that the
// model had no tools, so each case has an environment of its own.
func TestClaudeSecondQuestionAfterAChange(t *testing.T) {
	for _, c := range []struct {
		name   string
		change func(re *runEnv)
		view   bool
	}{
		{"the next day", func(re *runEnv) { re.clock.add(24 * time.Hour) }, false},
		{"the table's view as the scope", func(*runEnv) {}, true},
		{"nothing changed", func(*runEnv) {}, false},
	} {
		re := newRunEnv(t, 2)
		claude := re.claudeNow()
		second := claude
		if c.view {
			second = func(r *Request) {
				claude(r)
				r.View, r.Context = true, query.Context{Period: &query.Period{Year: 2025}}
			}
		}
		chat := re.newChat()
		if evs := re.ask(chat, "How much did I spend on coffee?", claude); last(evs).Name != "done" {
			t.Fatalf("%s: the first question: events %s", c.name, names(evs))
		}
		_, msgs, _ := re.chats.Get(re.ctx, re.user, chat)
		if len(msgs) < 2 || len(msgs[1].Replay) == 0 {
			t.Fatalf("%s: the first question stored no thinking block, so this proves nothing", c.name)
		}
		c.change(re)
		before := len(re.mock.Requests())
		evs := re.ask(chat, "And on taxi?", second)
		if last(evs).Name != "done" {
			t.Errorf("%s: the second question: events %s (%v)", c.name, names(evs), last(evs).Data)
			continue
		}
		reqs := re.mock.Requests()[before:]
		if len(reqs) != 2 || slicesIndex(evs, "mode") >= 0 {
			t.Errorf("%s: the second question made %d requests (events %s), want a lookup and the answer", c.name, len(reqs), names(evs))
			continue
		}
		// the earlier question's block stays behind; this question's goes back in its own second round
		if n := thinkingBlocks(reqs[0]); n != 0 {
			t.Errorf("%s: the second question's first request sent %d thinking blocks back", c.name, n)
		}
		if n := thinkingBlocks(reqs[1]); n != 1 {
			t.Errorf("%s: the second question's second round sent %d thinking blocks, want its own one", c.name, n)
		}
	}
}

// TestClaudeRoundsKeepWhatCameBefore (issue 109, finding 2): a question that pushes a chat past the
// 30 messages kept for the model, in the middle of its tool rounds, still ends in an answer. The
// window was trimmed again before every round, so the second round dropped the oldest message and
// sent this question's own thinking block after a changed conversation.
func TestClaudeRoundsKeepWhatCameBefore(t *testing.T) {
	re := newRunEnv(t, 2)
	claude := re.claudeNow()
	chat := re.newChat()
	// fourteen finished turns: 28 messages, and the question is the 29th
	for i := range 14 {
		if _, err := re.chats.Append(re.ctx, re.user, chat, []Message{
			{Role: llm.RoleUser, Content: fmt.Sprintf("Question %d", i)},
			{Role: llm.RoleAssistant, Content: fmt.Sprintf("Answer %d", i), Meta: &Meta{}},
		}, ChatUpdate{}); err != nil {
			t.Fatal(err)
		}
	}
	before := len(re.mock.Requests())
	evs := re.ask(chat, "How much did I spend on coffee?", claude)
	if last(evs).Name != "done" {
		t.Fatalf("events %s (%v)", names(evs), last(evs).Data)
	}
	reqs := re.mock.Requests()[before:]
	if len(reqs) != 2 || thinkingBlocks(reqs[1]) != 1 {
		t.Fatalf("%d requests; the second sent %d thinking blocks", len(reqs), thinkingBlocks(reqs[len(reqs)-1]))
	}
	// both rounds began with the same messages: the window moved neither way
	first := func(b []byte) string {
		_, after, ok := bytes.Cut(b, []byte(`"messages":`))
		if !ok || len(after) < 120 {
			t.Fatalf("a request without messages: %.200s", b)
		}
		return string(after[:120])
	}
	if first(reqs[0]) != first(reqs[1]) {
		t.Errorf("the rounds began with different messages:\n%s\n%s", first(reqs[0]), first(reqs[1]))
	}
}

// TestRefusedRequestSaysWhy (issue 109, finding 3): a request the provider refuses puts its HTTP
// status and its error type, which are the provider's fixed words, on the run line and the
// request's span, so the failure can be told apart without a database read. The provider's
// message, which can quote the request, goes on neither (AI-4). A refused thinking block is
// neither "no tools" (no plan mode, nothing learnt about the model) nor "no thinking" (no retry
// without it).
func TestRefusedRequestSaysWhy(t *testing.T) {
	re := newRunEnv(t, 2)
	claude := re.claudeNow()
	rec := tracetest.NewSpanRecorder()
	var logs bytes.Buffer
	re.runner = NewRunner(RunnerConfig{Connections: re.conns, Tools: NewTools(re.q), Chats: re.chats, Clock: re.clock, MaxConcurrent: 2,
		Tracer: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)), Logger: observability.NewLogger(&logs, config.EnvProd, slog.LevelDebug)})
	const quoted = "QUOTEDREQUEST"
	re.mock.Push(llmmock.Reply{Status: 400, Body: `{"type":"error","error":{"type":"invalid_request_error","message":"messages.1.content.0: Invalid ` +
		"`signature`" + ` in ` + "`thinking`" + ` block. The block is bound to a different conversation. ` + quoted + `"}}`})
	before := len(re.mock.Requests())
	evs := re.ask(re.newChat(), "How much did I spend on coffee?", claude)
	if last(evs).Name != "error" || slicesIndex(evs, "mode") >= 0 {
		t.Fatalf("events %s: a refused block must end the question, not send it to plan mode", names(evs))
	}
	if n := len(re.mock.Requests()) - before; n != 1 {
		t.Errorf("%d requests: a refused block is not retried without thinking", n)
	}
	out := logs.String()
	if !strings.Contains(out, `"upstream_status":400`) || !strings.Contains(out, `"upstream_error_type":"invalid_request_error"`) {
		t.Errorf("the run line doesn't say what the provider answered:\n%s", out)
	}
	var span string
	for _, s := range rec.Ended() {
		if s.Name() == "llm.request" {
			span = fmt.Sprint(s.Attributes())
		}
	}
	if !strings.Contains(span, "http.response.status_code") || !strings.Contains(span, "invalid_request_error") {
		t.Errorf("the request's span doesn't say what the provider answered: %s", span)
	}
	if strings.Contains(out, quoted) || strings.Contains(span, quoted) {
		t.Error("the provider's message reached a log line or a span")
	}
	// nothing was learnt about the model: the next question uses tools
	if evs := re.ask(re.newChat(), "How much did I spend on coffee?", claude); names(evs) != "reasoning,step,content,done" {
		t.Errorf("the next question: events %s", names(evs))
	}
}

// TestQuestionThatOutgrowsTheContext (issue 109): when a question's own lookups no longer fit the
// model's context, the messages before them can't be cut while one of its rounds carries a
// thinking block (the block is bound to them), so the question ends with a note. Without a block
// the window moves as before and the question is answered. A context of 1,500 tokens is smaller
// than any lookup's result.
func TestQuestionThatOutgrowsTheContext(t *testing.T) {
	re := newRunEnv(t, 2)
	re.mock.Models = append(re.mock.Models, "claude-haiku-5-5")
	u, _ := url.Parse(re.srv.URL + "/v1")
	root, _ := url.Parse(re.srv.URL)
	conns := NewConnections([]config.AIConnection{
		{Name: "claude", Label: "Small Anthropic", API: llm.Anthropic, BaseURL: root, ContextTokens: 1500},
		{Name: "mock", Label: "Small mock", API: llm.OpenAI, BaseURL: u, ContextTokens: 1500},
	}, 10*time.Second, 0, re.clock, func(c llm.Config) llm.Client { c.HTTPClient = re.srv.Client(); return llm.New(c) })
	re.runner = NewRunner(RunnerConfig{Connections: conns, Tools: NewTools(re.q), Chats: re.chats, Clock: re.clock, MaxConcurrent: 2})

	evs := re.ask(re.newChat(), `Show "taxi"`, func(r *Request) { r.Connection, r.Model = "claude", "claude-haiku-5-5" })
	if last(evs).Name != "done" || !strings.HasSuffix(text(evs, "content"), fullNote) {
		t.Errorf("a bound block: events %s, answer %q", names(evs), text(evs, "content"))
	}
	evs = re.ask(re.newChat(), `Show "taxi"`, nil) // the OpenAI-format mock: no blocks to keep
	if last(evs).Name != "done" || strings.Contains(text(evs, "content"), fullNote) {
		t.Errorf("no block: events %s, answer %q", names(evs), text(evs, "content"))
	}
}
