package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/config"
	"github.com/x2db736ac85h/finuance/backend/internal/llm"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/testutil/llmmock"
)

// runEnv is a runner on the demo data with a mock model server behind two connections (OpenAI and
// Anthropic protocol) and a cloud connection to the same mock.
type runEnv struct {
	*env
	mock   *llmmock.Server
	clock  *fakeClock
	chats  *Chats
	runner *Runner
	user   uuid.UUID
}

func newRunEnv(t *testing.T, maxConcurrent int) *runEnv {
	t.Helper()
	e, _ := demoUser(t)
	mock := llmmock.New()
	srv := httptest.NewServer(mock)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL + "/v1")
	root, _ := url.Parse(srv.URL)
	clock := &fakeClock{t: time.Date(2025, 3, 20, 10, 0, 0, 0, time.UTC)}
	conns := NewConnections([]config.AIConnection{
		{Name: "mock", Label: "Mock", API: llm.OpenAI, BaseURL: u, ContextTokens: 32768},
		{Name: "claude", Label: "Mock Anthropic", API: llm.Anthropic, BaseURL: root, ContextTokens: 32768},
		{Name: "cloud", Label: "Mock cloud", API: llm.OpenAI, BaseURL: u, Cloud: true, ContextTokens: 32768},
	}, 10*time.Second, 0, clock, nil)
	chats := NewChats(e.st, clock, 25)
	runner := NewRunner(RunnerConfig{Connections: conns, Tools: NewTools(e.q), Chats: chats, Clock: clock, MaxConcurrent: maxConcurrent})
	// each test gets its own user with the demo data (copy of the shared demo user's file)
	user := e.user()
	e.importFile(user, "Demo", fixture(t, "fixtures/demo/demo-legacy.csv"))
	return &runEnv{env: e, mock: mock, clock: clock, chats: chats, runner: runner, user: user}
}

// ask runs one question to the end and returns its events.
func (re *runEnv) ask(chat uuid.UUID, text string, mod func(*Request)) []Event {
	re.t.Helper()
	req := Request{ChatID: chat, Text: text, Connection: "mock", Model: "mock-tools"}
	if mod != nil {
		mod(&req)
	}
	run, err := re.runner.Prepare(re.ctx, re.user, req)
	if err != nil {
		re.t.Fatalf("prepare %q: %v", text, err)
	}
	var evs []Event
	run.Execute(re.ctx, func(e Event) { evs = append(evs, e) })
	return evs
}

func (re *runEnv) newChat() uuid.UUID {
	re.t.Helper()
	c, err := re.chats.Create(re.ctx, re.user)
	if err != nil {
		re.t.Fatal(err)
	}
	return c.ID
}

func names(evs []Event) string {
	var out []string
	for _, e := range evs {
		if len(out) == 0 || out[len(out)-1] != e.Name {
			out = append(out, e.Name)
		}
	}
	return strings.Join(out, ",")
}

func text(evs []Event, name string) string {
	var b strings.Builder
	for _, e := range evs {
		if e.Name == name {
			b.WriteString(e.Data.(DeltaEvent).Delta)
		}
	}
	return b.String()
}

func last(evs []Event) Event { return evs[len(evs)-1] }

func TestRunToolsMode(t *testing.T) {
	re := newRunEnv(t, 2)
	chat := re.newChat()
	evs := re.ask(chat, `What did I pay at "kino"?`, nil)
	if got := names(evs); got != "reasoning,step,content,done" {
		t.Fatalf("events %s", got)
	}
	step := evs[slicesIndex(evs, "step")].Data.(StepEvent)
	if step.Tool != ToolSearch || string(step.Args) != `{"query":"kino","limit":5}` || *step.Matched != 10 ||
		step.ToolFilter.Query != "kino" || step.ToolFilter.Label != "query: kino" || !strings.Contains(step.Result, `"matched":10`) {
		t.Errorf("step = %+v", step)
	}
	answer := text(evs, "content")
	if !strings.HasPrefix(answer, "Found **10** matching transactions; spent **") || !strings.Contains(answer, "| Date | Payee | Amount |") {
		t.Errorf("answer = %q", answer)
	}
	done := last(evs).Data.(DoneEvent)
	if done.Meta.Mode != ModeTools || done.Meta.Lookups != 1 || done.Meta.Rounds != 2 || done.Meta.Model != "mock-tools" ||
		done.Meta.Connection != "mock" || done.Meta.ApproxTokensSent == 0 || done.Meta.InputTokens != 200 || done.MessageSeq != 4 {
		t.Errorf("meta = %+v", done)
	}
	// the tool result went back to the model, in the protocol's tool message
	reqs := re.mock.Requests()
	if len(reqs) != 2 || !strings.Contains(string(reqs[1]), `"role":"tool"`) || !strings.Contains(string(reqs[1]), `\"matched\":10`) ||
		!strings.Contains(string(reqs[0]), `"name":"month_overview"`) {
		t.Errorf("requests: %d", len(reqs))
	}
	c, msgs, err := re.chats.Get(re.ctx, re.user, chat)
	if err != nil {
		t.Fatal(err)
	}
	if c.Title != `What did I pay at "kino"?` || c.Connection != "mock" || c.Model != "mock-tools" || len(msgs) != 4 ||
		msgs[1].ToolCalls[0].Name != ToolSearch || msgs[2].Role != llm.RoleTool || msgs[3].Meta == nil || len(msgs[3].Steps) != 1 ||
		msgs[1].Reasoning != "The user wants data; I'll look it up." {
		t.Errorf("chat %+v, messages %+v", c, msgs)
	}

	// the next question carries the history (question, call, result, answer)
	re.ask(chat, `And "taxi"?`, nil)
	reqs = re.mock.Requests()
	var body struct {
		Messages []map[string]any `json:"messages"`
	}
	_ = json.Unmarshal(reqs[2], &body)
	if len(body.Messages) != 6 || body.Messages[5]["content"] != `And "taxi"?` || body.Messages[0]["role"] != "system" {
		t.Errorf("history sent: %v", body.Messages)
	}
}

func slicesIndex(evs []Event, name string) int {
	for i, e := range evs {
		if e.Name == name {
			return i
		}
	}
	return -1
}

func TestRunAutoFallsBackToPlanMode(t *testing.T) {
	re := newRunEnv(t, 2)
	chat := re.newChat()
	evs := re.ask(chat, `Show "kino"`, func(r *Request) { r.Model = "mock-no-tools" })
	if got := names(evs); got != "mode,step,content,done" {
		t.Fatalf("events %s", got)
	}
	if m := evs[0].Data.(ModeEvent); m.Mode != ModePlan || !strings.Contains(m.Reason, "does not support tools") {
		t.Errorf("mode = %+v", m)
	}
	if !strings.HasPrefix(text(evs, "content"), "Found **10**") {
		t.Errorf("answer = %q", text(evs, "content"))
	}
	done := last(evs).Data.(DoneEvent)
	if done.Meta.Mode != ModePlan || !done.Meta.Fallback || done.Meta.Lookups != 1 {
		t.Errorf("meta = %+v", done.Meta)
	}
	// rejected request, plan request, answer request; the answer request has the results in the question
	reqs := re.mock.Requests()
	if len(reqs) != 3 || !strings.Contains(string(reqs[1]), "You cannot call functions directly") ||
		!strings.Contains(string(reqs[2]), "Data looked up locally by the app for my last question") {
		t.Errorf("requests = %d", len(reqs))
	}
	c, _, _ := re.chats.Get(re.ctx, re.user, chat)
	if !c.NoTools {
		t.Error("the chat is not switched to plan mode")
	}
	// the next question goes straight to plan mode (no rejected request)
	re.ask(chat, `Show "taxi"`, func(r *Request) { r.Model = "mock-no-tools" })
	if reqs := re.mock.Requests(); len(reqs) != 5 || strings.Contains(string(reqs[3]), `"tools"`) {
		t.Errorf("second question: %d requests", len(reqs))
	}
}

func TestRunTextToolCallsAndThinkTags(t *testing.T) {
	re := newRunEnv(t, 2)
	evs := re.ask(re.newChat(), `How is "PIZZERIA" spelled?`, func(r *Request) { r.Model = "mock-text-tools" })
	if got := names(evs); got != "reasoning,step,content,done" {
		t.Fatalf("events %s", got)
	}
	if text(evs, "reasoning") != "The user wants data; I'll look it up." {
		t.Errorf("reasoning = %q", text(evs, "reasoning"))
	}
	if s := evs[slicesIndex(evs, "step")].Data.(StepEvent); s.Tool != ToolValues || !strings.Contains(s.Result, "PIZZERIA SŁONECZNA") {
		t.Errorf("step = %+v", s)
	}
	if a := text(evs, "content"); !strings.Contains(a, "PIZZERIA SŁONECZNA (62)") || strings.Contains(a, "tool_call") {
		t.Errorf("answer = %q", a)
	}
}

func TestRunAnthropicProtocol(t *testing.T) {
	re := newRunEnv(t, 2)
	chat := re.newChat()
	evs := re.ask(chat, "Spending by category?", func(r *Request) { r.Connection, r.Model = "claude", "mock-tools" })
	if got := names(evs); got != "reasoning,step,content,done" {
		t.Fatalf("events %s", got)
	}
	if a := text(evs, "content"); !strings.Contains(a, "| Artykuły spożywcze | 19771.41 |") {
		t.Errorf("answer = %q", a)
	}
	reqs := re.mock.Requests()
	// the second request replays the thinking block and sends the result as a tool_result block
	if !strings.Contains(string(reqs[1]), `"type":"thinking"`) || !strings.Contains(string(reqs[1]), `"signature":"mock-signature"`) ||
		!strings.Contains(string(reqs[1]), `"type":"tool_result"`) {
		t.Errorf("second request = %s", reqs[1])
	}
	_, msgs, _ := re.chats.Get(re.ctx, re.user, chat)
	if len(msgs[1].Replay) == 0 {
		t.Error("replay not stored")
	}
}

func TestRunStop(t *testing.T) {
	re := newRunEnv(t, 2)
	chat := re.newChat()
	run, err := re.runner.Prepare(re.ctx, re.user, Request{ChatID: chat, Text: "Tell me something slow", Connection: "mock", Model: "mock-tools"})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var evs []Event
	stopped := make(chan struct{})
	go func() {
		for {
			mu.Lock()
			n := len(evs)
			mu.Unlock()
			if n >= 3 {
				re.runner.Stop(re.user)
				close(stopped)
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	run.Execute(re.ctx, func(e Event) { mu.Lock(); evs = append(evs, e); mu.Unlock() })
	<-stopped
	done := last(evs)
	if done.Name != "done" || !done.Data.(DoneEvent).Meta.Stopped || !strings.HasSuffix(text(evs, "content"), "\n\n_Stopped._") {
		t.Fatalf("last event %+v, content %q", done, text(evs, "content"))
	}
	_, msgs, _ := re.chats.Get(re.ctx, re.user, chat)
	if fin := msgs[len(msgs)-1]; !strings.HasPrefix(fin.Content, "Still") || !strings.HasSuffix(fin.Content, "_Stopped._") {
		t.Errorf("stored = %q", fin.Content)
	}
	if re.runner.Stop(re.user) {
		t.Error("a run is still registered")
	}
}

func TestRunErrorIsStoredAndLeftOutOfHistory(t *testing.T) {
	re := newRunEnv(t, 2)
	chat := re.newChat()
	evs := re.ask(chat, "please fail", nil)
	// a 500 before any lookup tries plan mode (legacy), which fails as well
	if names(evs) != "mode,error" {
		t.Fatalf("events %s", names(evs))
	}
	if e := last(evs).Data.(ErrorEvent); e.Code != "upstream" || !strings.Contains(e.Message, "mock failure") {
		t.Errorf("error = %+v", e)
	}
	c, msgs, _ := re.chats.Get(re.ctx, re.user, chat)
	if len(msgs) != 2 || msgs[1].Error == nil || c.NoTools {
		t.Fatalf("chat %+v stored %+v", c, msgs)
	}
	re.ask(chat, "Hello", nil)
	var body struct {
		Messages []map[string]any `json:"messages"`
	}
	reqs := re.mock.Requests()
	_ = json.Unmarshal(reqs[len(reqs)-2], &body)
	if len(body.Messages) != 2 || body.Messages[1]["content"] != "Hello" {
		t.Errorf("the failed turn was sent again: %v", body.Messages)
	}
}

func TestRunRoundsLimitAndLoopGuard(t *testing.T) {
	re := newRunEnv(t, 2)
	for range 7 {
		re.mock.Push(llmmock.Reply{Calls: []llmmock.Call{{Name: ToolAggregate, Arguments: `{"group_by":"month"}`}}})
	}
	evs := re.ask(re.newChat(), "Loop forever", nil)
	done := last(evs).Data.(DoneEvent)
	if done.Meta.Rounds != 6 || done.Meta.Lookups != 6 || !strings.HasSuffix(text(evs, "content"), roundsNote) {
		t.Errorf("meta %+v content %q", done.Meta, text(evs, "content"))
	}
	if n := len(re.mock.Requests()); n != 6 {
		t.Errorf("%d model requests", n)
	}
}

func TestRunRetriesWithoutTemperature(t *testing.T) {
	re := newRunEnv(t, 2)
	re.mock.Push(llmmock.Reply{Status: 400, Body: `{"error":{"message":"Unsupported parameter: 'temperature' is not supported with this model."}}`},
		llmmock.Reply{Content: "Hi."})
	evs := re.ask(re.newChat(), "Hi", nil)
	if names(evs) != "content,done" || text(evs, "content") != "Hi." {
		t.Fatalf("events %s %q", names(evs), text(evs, "content"))
	}
	reqs := re.mock.Requests()
	if !strings.Contains(string(reqs[0]), `"temperature":0.2`) || strings.Contains(string(reqs[1]), "temperature") {
		t.Errorf("requests %s / %s", reqs[0], reqs[1])
	}
}

func TestRunNoThinkForQwen3(t *testing.T) {
	re := newRunEnv(t, 2)
	re.mock.Models = append(re.mock.Models, "qwen3-mock")
	re.mock.Push(llmmock.Reply{Content: "ok"})
	re.ask(re.newChat(), "Hi", func(r *Request) { r.Model, r.Think = "qwen3-mock", llm.ThinkOff })
	if reqs := re.mock.Requests(); !strings.Contains(string(reqs[0]), `"content":"Hi /no_think"`) {
		t.Errorf("request = %s", reqs[0])
	}
}

func TestRunLimitsAndConsent(t *testing.T) {
	re := newRunEnv(t, 1)
	chat := re.newChat()
	req := Request{ChatID: chat, Text: "Hi", Connection: "mock", Model: "mock-tools"}
	run, err := re.runner.Prepare(re.ctx, re.user, req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := re.runner.Prepare(re.ctx, re.user, req); !errors.Is(err, ErrRunning) {
		t.Errorf("second run of the same user: %v", err)
	}
	other := re.user
	re.user = re.env.user()
	otherChat := re.newChat()
	if _, err := re.runner.Prepare(re.ctx, re.user, Request{ChatID: otherChat, Text: "Hi", Connection: "mock"}); !errors.Is(err, ErrBusy) {
		t.Errorf("replica full: %v", err)
	}
	run.Release()
	// another user's chat is not found
	if _, err := re.runner.Prepare(re.ctx, re.user, Request{ChatID: chat, Text: "Hi"}); !errors.Is(err, platform.ErrNotFound) {
		t.Errorf("foreign chat: %v", err)
	}
	re.user = other
	for _, c := range []struct {
		mod  func(*Request)
		want string
	}{
		{func(r *Request) { r.Text = "  " }, "text"},
		{func(r *Request) { r.Mode = "yolo" }, "settings.mode"},
		{func(r *Request) { r.MaxRows = 51 }, "settings.maxRows"},
		{func(r *Request) { r.Model = "gpt-9" }, "model not offered"},
		{func(r *Request) { r.Connection = "nope" }, "unknown assistant connection"},
	} {
		r := req
		c.mod(&r)
		if _, err := re.runner.Prepare(re.ctx, re.user, r); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("want %q, got %v", c.want, err)
		}
	}
	// a cloud connection needs the user's agreement
	cloud := req
	cloud.Connection = "cloud"
	if _, err := re.runner.Prepare(re.ctx, re.user, cloud); !errors.Is(err, ErrConsent) {
		t.Fatalf("cloud without consent: %v", err)
	}
	if _, err := re.chats.SaveSettings(re.ctx, re.user, Settings{CloudConsent: []string{"cloud"}}); err != nil {
		t.Fatal(err)
	}
	run, err = re.runner.Prepare(re.ctx, re.user, cloud)
	if err != nil {
		t.Fatalf("cloud with consent: %v", err)
	}
	re.mock.Push(llmmock.Reply{Calls: []llmmock.Call{{Name: ToolSearch, Arguments: `{"limit":1}`}}}, llmmock.Reply{Content: "ok"})
	var evs []Event
	run.Execute(re.ctx, func(e Event) { evs = append(evs, e) })
	// tool results to a cloud model have long digit runs masked
	if s := evs[slicesIndex(evs, "step")].Data.(StepEvent); !strings.Contains(s.Result, `"title":"DEMO 83…"`) {
		t.Errorf("cloud step = %s", s.Result)
	}
}

func TestRetentionAndChatLimit(t *testing.T) {
	re := newRunEnv(t, 2)
	s, err := re.chats.Settings(re.ctx, re.user)
	if err != nil || *s.RetentionDays != DefaultRetentionDays || len(s.CloudConsent) != 0 {
		t.Fatalf("defaults = %+v %v", s, err)
	}
	old := re.newChat()
	re.clock.add(36 * time.Hour)
	fresh := re.newChat()
	re.clock.add(13 * time.Hour) // old: 49 h, fresh: 13 h
	list, _ := re.chats.List(re.ctx, re.user)
	if len(list) != 1 || list[0].ID != fresh {
		t.Fatalf("after 49 h: %+v", list)
	}
	if _, _, err := re.chats.Get(re.ctx, re.user, old); !errors.Is(err, platform.ErrNotFound) {
		t.Errorf("expired chat: %v", err)
	}
	// forever keeps chats; the job then deletes nothing for this user
	if s, err := re.chats.SaveSettings(re.ctx, re.user, Settings{RetentionDays: nil}); err != nil || s.RetentionDays != nil {
		t.Fatalf("forever: %+v %v", s, err)
	}
	re.clock.add(30 * 24 * time.Hour)
	if n, err := re.chats.Cleanup(re.ctx); err != nil || len(mustList(t, re)) != 1 {
		t.Fatalf("cleanup with forever: %d %v", n, err)
	}
	// one day: the job deletes it
	one := 1
	if _, err := re.chats.SaveSettings(re.ctx, re.user, Settings{RetentionDays: &one}); err != nil {
		t.Fatal(err)
	}
	if len(mustList(t, re)) != 0 {
		t.Error("saving a shorter retention applies it")
	}
	re.newChat()
	re.clock.add(25 * time.Hour)
	n, err := re.chats.Cleanup(re.ctx)
	if err != nil || n < 1 {
		t.Errorf("cleanup: %d %v", n, err)
	}
	for _, bad := range []int{0, 8} {
		if _, err := re.chats.SaveSettings(re.ctx, re.user, Settings{RetentionDays: &bad}); err == nil {
			t.Errorf("retention %d accepted", bad)
		}
	}
	// the newest 25 chats are kept
	for range 27 {
		re.newChat()
		re.clock.add(time.Minute)
	}
	if n := len(mustList(t, re)); n != 25 {
		t.Errorf("%d chats kept", n)
	}
}

func mustList(t *testing.T, re *runEnv) []Chat {
	t.Helper()
	l, err := re.chats.List(context.Background(), re.user)
	if err != nil {
		t.Fatal(err)
	}
	return l
}
