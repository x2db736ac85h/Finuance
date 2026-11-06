package httpapi

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/x2db736ac85h/finuance/backend/internal/config"
	"github.com/x2db736ac85h/finuance/backend/internal/llm"
	"github.com/x2db736ac85h/finuance/backend/testutil/llmmock"
)

// aiConn is a local OpenAI-compatible connection to a test server.
func aiConn(t *testing.T, name string, srv *httptest.Server, mod func(*config.AIConnection)) config.AIConnection {
	t.Helper()
	u, _ := url.Parse(srv.URL + "/v1")
	c := config.AIConnection{Name: name, Label: name, API: llm.OpenAI, BaseURL: u, ContextTokens: 8192}
	if mod != nil {
		mod(&c)
	}
	return c
}

func TestAssistantStatusHTTP(t *testing.T) {
	var calls atomic.Int32
	lm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"text-embedding-nomic"},{"id":"qwen3-14b"},{"id":"gpt-oss-20b"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer lm.Close()
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid API key"}}`))
	}))
	defer down.Close()
	e := newTestEnv(t, func(c *config.Config) {
		c.AIConnections = []config.AIConnection{
			aiConn(t, "lmstudio", lm, func(c *config.AIConnection) { c.Label = "LM Studio"; c.DefaultModel = "gpt-oss-20b" }),
			aiConn(t, "keyed", down, nil),
		}
	}, false)

	// public: /meta says the assistant is on; status needs a session
	anon := e.browser()
	if m := anon.do("GET", "/meta", nil); m.body["assistantEnabled"] != true {
		t.Errorf("meta = %s", m.raw)
	}
	expect(t, anon.do("GET", "/assistant/status", nil), 401, "unauthenticated")

	b := signedIn(t, e, testEmail(t))
	st := b.do("GET", "/assistant/status", nil)
	expect(t, st, 200, "")
	conns := st.body["connections"].([]any)
	lmc, keyed := conns[0].(map[string]any), conns[1].(map[string]any)
	models := lmc["models"].([]any)
	if st.body["enabled"] != true || num(st.body["maxRowsLimit"]) != 50 || len(conns) != 2 ||
		lmc["label"] != "LM Studio" || lmc["reachable"] != true || lmc["defaultModel"] != "gpt-oss-20b" || lmc["cloud"] != false ||
		len(models) != 2 || models[0].(map[string]any)["reasoning"] != "switch" || models[1].(map[string]any)["reasoning"] != "effort" {
		t.Errorf("status = %s", st.raw)
	}
	if keyed["reachable"] != false || obj(t, keyed, "error")["kind"] != "auth" || len(keyed["models"].([]any)) != 0 {
		t.Errorf("keyed = %v", keyed)
	}
	// cached for the next request; refresh asks again
	b.do("GET", "/assistant/status", nil)
	if calls.Load() != 3 { // /v1/models + LM Studio's /api/v0/models + Ollama's /api/show (both 404 here)
		t.Errorf("upstream calls = %d", calls.Load())
	}
	b.do("GET", "/assistant/status?refresh=true", nil)
	if calls.Load() != 6 {
		t.Errorf("upstream calls after refresh = %d", calls.Load())
	}
}

func TestAssistantDisabledHTTP(t *testing.T) {
	e := newTestEnv(t, nil, false)
	if m := e.browser().do("GET", "/meta", nil); m.body["assistantEnabled"] != false {
		t.Errorf("meta = %s", m.raw)
	}
	b := signedIn(t, e, testEmail(t))
	st := b.do("GET", "/assistant/status", nil)
	expect(t, st, 200, "")
	if st.body["enabled"] != false || len(st.body["connections"].([]any)) != 0 {
		t.Errorf("status = %s", st.raw)
	}
}

// sseEvent is one parsed server-sent event.
type sseEvent struct {
	name string
	data map[string]any
}

func parseSSE(t *testing.T, raw string) []sseEvent {
	t.Helper()
	var out []sseEvent
	for _, block := range strings.Split(raw, "\n\n") {
		var ev sseEvent
		for _, line := range strings.Split(block, "\n") {
			switch {
			case strings.HasPrefix(line, "event: "):
				ev.name = line[7:]
			case strings.HasPrefix(line, "data: "):
				if err := json.Unmarshal([]byte(line[6:]), &ev.data); err != nil {
					t.Fatalf("bad event data %q", line)
				}
			}
		}
		if ev.name != "" {
			out = append(out, ev)
		}
	}
	return out
}

func mockEnv(t *testing.T, mod func(*config.Config)) (*authEnv, *llmmock.Server) {
	t.Helper()
	mock := llmmock.New()
	srv := httptest.NewServer(mock)
	t.Cleanup(srv.Close)
	e := newTestEnv(t, func(c *config.Config) {
		c.AIConnections = []config.AIConnection{
			aiConn(t, "mock", srv, func(c *config.AIConnection) { c.DefaultModel = "mock-tools" }),
			aiConn(t, "cloud", srv, func(c *config.AIConnection) { c.Cloud = true }),
		}
		c.AIMaxChats, c.AIRatePerHour, c.AIMaxConcurrent = 25, 30, 2
		if mod != nil {
			mod(c)
		}
	}, true)
	return e, mock
}

func TestAssistantChatHTTP(t *testing.T) {
	e, mock := mockEnv(t, nil)
	b, accID := demoBrowser(t, e)

	st := b.do("GET", "/assistant/settings", nil)
	expect(t, st, 200, "")
	if num(st.body["retentionDays"]) != 2 || len(st.body["cloudConsent"].([]any)) != 0 {
		t.Errorf("settings = %s", st.raw)
	}
	chat := b.do("POST", "/assistant/chats", nil)
	expect(t, chat, 201, "")
	id := chat.body["id"].(string)

	// a question about one account's view, streamed
	ask := b.do("POST", "/assistant/chats/"+id+"/messages", map[string]any{"text": `Show "kino"`, "scope": "view",
		"context": map[string]any{"accountIds": []string{accID}}, "timeZone": "Europe/Warsaw",
		"settings": map[string]any{"mode": "auto", "think": "auto", "maxRows": 10, "temperature": 0.2}})
	expect(t, ask, 200, "")
	if ct := ask.header.Get("Content-Type"); ct != "text/event-stream" || ask.header.Get("X-Accel-Buffering") != "no" {
		t.Errorf("headers %v", ask.header)
	}
	evs := parseSSE(t, ask.raw)
	var kinds []string
	for _, ev := range evs {
		if len(kinds) == 0 || kinds[len(kinds)-1] != ev.name {
			kinds = append(kinds, ev.name)
		}
	}
	if strings.Join(kinds, ",") != "reasoning,step,content,done" {
		t.Fatalf("events %v\n%s", kinds, ask.raw)
	}
	step := evs[slicesIndexEv(evs, "step")].data
	if step["tool"] != "search_transactions" || num(step["matched"]) != 10 || obj(t, step, "toolFilter")["query"] != "kino" ||
		obj(t, step, "args")["query"] != "kino" {
		t.Errorf("step = %v", step)
	}
	done := evs[len(evs)-1].data
	if meta := obj(t, done, "meta"); meta["scope"] != "view" || meta["mode"] != "tools" || len(meta["accountIds"].([]any)) != 1 {
		t.Errorf("done = %v", done)
	}

	// the stored chat as turns
	got := b.do("GET", "/assistant/chats/"+id, nil)
	expect(t, got, 200, "")
	turns := got.body["turns"].([]any)
	if got.body["title"] != `Show "kino"` || got.body["connection"] != "mock" || len(turns) != 2 {
		t.Fatalf("chat = %s", got.raw)
	}
	ans := turns[1].(map[string]any)
	if ans["role"] != "assistant" || !strings.HasPrefix(ans["content"].(string), "Found **10**") || len(ans["steps"].([]any)) != 1 ||
		ans["meta"] == nil || ans["error"] != nil {
		t.Errorf("answer turn = %v", ans)
	}
	if list := b.do("GET", "/assistant/chats", nil); len(list.body["chats"].([]any)) != 1 || num(list.body["maxChats"]) != 25 {
		t.Errorf("list = %s", list.raw)
	}
	if n := len(mock.Requests()); n != 2 {
		t.Errorf("model requests = %d", n)
	}

	// validation and unknown models are problems before any event
	expect(t, b.do("POST", "/assistant/chats/"+id+"/messages", map[string]any{"text": ""}), 422, "validation_failed")
	expect(t, b.do("POST", "/assistant/chats/"+id+"/messages", map[string]any{"text": "x", "settings": map[string]any{"model": "gpt-9"}}),
		422, "validation_failed")
	// a cloud connection needs the user's agreement, given in the settings
	expect(t, b.do("POST", "/assistant/chats/"+id+"/messages", map[string]any{"text": "x", "settings": map[string]any{"connection": "cloud", "model": "mock-tools"}}),
		403, "consent_required")
	expect(t, b.do("PUT", "/assistant/settings", map[string]any{"retentionDays": nil, "cloudConsent": []string{"cloud"}}), 200, "")
	cloud := b.do("POST", "/assistant/chats/"+id+"/messages", map[string]any{"text": "Hello", "settings": map[string]any{"connection": "cloud", "model": "mock-tools"}})
	expect(t, cloud, 200, "")
	if evs := parseSSE(t, cloud.raw); evs[len(evs)-1].name != "done" {
		t.Errorf("cloud run: %s", cloud.raw)
	}
	expect(t, b.do("PUT", "/assistant/settings", map[string]any{"retentionDays": 9, "cloudConsent": []string{}}), 422, "validation_failed")

	// another user sees none of it
	other := signedIn(t, e, "other-"+testEmail(t))
	expect(t, other.do("GET", "/assistant/chats/"+id, nil), 404, "not_found")
	expect(t, other.do("POST", "/assistant/chats/"+id+"/messages", map[string]any{"text": "hi"}), 404, "not_found")
	expect(t, other.do("DELETE", "/assistant/chats/"+id, nil), 404, "not_found")
	if l := other.do("GET", "/assistant/chats", nil); len(l.body["chats"].([]any)) != 0 {
		t.Errorf("other's list = %s", l.raw)
	}

	expect(t, b.do("POST", "/assistant/chats/"+id+"/stop", nil), 204, "")
	expect(t, b.do("DELETE", "/assistant/chats/"+id, nil), 204, "")
	expect(t, b.do("GET", "/assistant/chats/"+id, nil), 404, "not_found")
}

func slicesIndexEv(evs []sseEvent, name string) int {
	for i, e := range evs {
		if e.name == name {
			return i
		}
	}
	return -1
}

func TestAssistantStreamStopAndLimits(t *testing.T) {
	e, _ := mockEnv(t, func(c *config.Config) { c.AIRatePerHour = 3 })
	b, _ := demoBrowser(t, e)
	id := b.do("POST", "/assistant/chats", nil).body["id"].(string)

	// a slow answer: a second question meanwhile is refused, stop ends the first
	body, _ := json.Marshal(map[string]any{"text": "something slow"})
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/v1/assistant/chats/"+id+"/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range b.header {
		req.Header.Set(k, v)
	}
	res, err := b.c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	rd := bufio.NewReader(res.Body)
	if line, _ := rd.ReadString('\n'); !strings.HasPrefix(line, "event: content") {
		t.Fatalf("first line %q", line)
	}
	expect(t, b.do("POST", "/assistant/chats/"+id+"/messages", map[string]any{"text": "again"}), 409, "run_in_progress")
	expect(t, b.do("POST", "/assistant/chats/"+id+"/stop", nil), 204, "")
	rest, _ := io.ReadAll(rd)
	evs := parseSSE(t, "event: content\n"+string(rest))
	last := evs[len(evs)-1]
	if last.name != "done" || obj(t, last.data, "meta")["stopped"] != true {
		t.Errorf("after stop: %v", last)
	}
	// three questions an hour in this config: the next is limited
	expect(t, b.do("POST", "/assistant/chats/"+id+"/messages", map[string]any{"text": "hi"}), 200, "")
	r := b.do("POST", "/assistant/chats/"+id+"/messages", map[string]any{"text": "hi"})
	expect(t, r, 429, "rate_limited")
	if r.header.Get("Retry-After") == "" {
		t.Error("no Retry-After")
	}
}

func TestAssistantDisabledChatsHTTP(t *testing.T) {
	e := newTestEnv(t, nil, false)
	b := signedIn(t, e, testEmail(t))
	id := b.do("POST", "/assistant/chats", nil)
	expect(t, id, 201, "")
	expect(t, b.do("POST", "/assistant/chats/"+id.body["id"].(string)+"/messages", map[string]any{"text": "hi"}), 503, "assistant_unavailable")
}

func TestAssistantInDataExport(t *testing.T) {
	e, _ := mockEnv(t, nil)
	b, _ := demoBrowser(t, e)
	id := b.do("POST", "/assistant/chats", nil).body["id"].(string)
	expect(t, b.do("POST", "/assistant/chats/"+id+"/messages", map[string]any{"text": `Show "kino"`}), 200, "")
	files := unzip(t, b.do("GET", "/me/export", nil).raw)
	var doc struct {
		Settings map[string]any   `json:"settings"`
		Chats    []map[string]any `json:"chats"`
	}
	if err := json.Unmarshal(files["assistant.json"], &doc); err != nil {
		t.Fatalf("assistant.json: %v", err)
	}
	if num(doc.Settings["retentionDays"]) != 2 || len(doc.Chats) != 1 || doc.Chats[0]["title"] != `Show "kino"` ||
		len(doc.Chats[0]["turns"].([]any)) != 2 {
		t.Errorf("assistant.json = %s", files["assistant.json"])
	}
}

// An action proposal (PLAN 15.7): the step carries what the card shows; nothing changes until the
// browser applies it through the normal endpoints, which record it in the activity log, so the
// chat reads it back as applied.
func TestAssistantProposalHTTP(t *testing.T) {
	e, _ := mockEnv(t, nil)
	b, _ := demoBrowser(t, e)
	chat := b.do("POST", "/assistant/chats", nil)
	id := chat.body["id"].(string)
	ask := b.do("POST", "/assistant/chats/"+id+"/messages", map[string]any{"text": `put "OPERATOR INTERNETU" in Internet`,
		"settings": map[string]any{"mode": "auto", "think": "auto"}})
	expect(t, ask, 200, "")
	evs := parseSSE(t, ask.raw)
	i := slicesIndexEv(evs, "step")
	if i < 0 {
		t.Fatalf("no step: %s", ask.raw)
	}
	step := evs[i].data
	p := obj(t, step, "proposal")
	if step["tool"] != "propose_action" || p["action"] != "set_category" || num(p["matched"]) != 14 || len(p["ids"].([]any)) != 14 ||
		p["categoryName"] != "Internet" || p["categoryId"] != nil || len(p["sample"].([]any)) != 5 || p["reason"] != "You asked for it" ||
		obj(t, p, "toolFilter")["payee"] != "OPERATOR INTERNETU" {
		t.Fatalf("proposal = %v", p)
	}
	if s := p["sample"].([]any)[0].(map[string]any); s["payee"] != "OPERATOR INTERNETU" || num(obj(t, s, "amount")["amountMinor"]) != -7999 {
		t.Errorf("sample = %v", s)
	}
	// nothing changed; the stored chat keeps the proposal
	if n := totalWhere(t, b, map[string]any{"column": "category", "cond": "=Internet"}); n != 0 {
		t.Fatalf("applied by the server: %d", n)
	}
	turns := b.do("GET", "/assistant/chats/"+id, nil).body["turns"].([]any)
	if c := turns[1].(map[string]any)["content"].(string); !strings.Contains(c, "Nothing has changed yet") {
		t.Errorf("answer = %q", c)
	}
	stored := turns[1].(map[string]any)["steps"].([]any)[0].(map[string]any)
	key, _ := p["key"].(string)
	if sp := obj(t, stored, "proposal"); len(sp["ids"].([]any)) != 14 || len(key) != 16 || sp["key"] != key {
		t.Errorf("stored proposal = %v, streamed key %q (the same key both ways)", sp, key)
	}

	// Apply as the panel does: create the category, then the bulk change marked with the chat
	nc := b.do("POST", "/categories", map[string]any{"name": "Internet"})
	expect(t, nc, 201, "")
	ids := p["ids"].([]any)
	b.header["X-Assistant-Chat"], b.header["X-Assistant-Proposal"] = id, key
	bu := b.do("POST", "/transactions/bulk", map[string]any{"ids": ids, "category": map[string]any{"id": nc.body["id"]}})
	delete(b.header, "X-Assistant-Chat")
	delete(b.header, "X-Assistant-Proposal")
	expect(t, bu, 200, "")
	if num(bu.body["updated"]) != 14 || totalWhere(t, b, map[string]any{"column": "category", "cond": "=Internet"}) != 14 {
		t.Fatalf("applied = %s", bu.raw)
	}
	applied := func() []any {
		t.Helper()
		return b.do("GET", "/assistant/chats/"+id, nil).body["appliedProposals"].([]any)
	}
	if got := applied(); len(got) != 1 || got[0] != key {
		t.Errorf("appliedProposals = %v, want [%s]", got, key)
	}
	events := func() []map[string]any {
		t.Helper()
		var out []map[string]any
		for _, ev := range b.do("GET", "/me/activity", nil).body["events"].([]any) {
			if ev := ev.(map[string]any); ev["action"] == "assistant.action_applied" {
				out = append(out, ev)
			}
		}
		return out
	}
	ev := events()
	if len(ev) != 1 || ev[0]["entityId"] != id {
		t.Fatalf("activity = %v", ev)
	}
	if m := obj(t, ev[0], "meta"); m["action"] != "set_category" || num(m["rows"]) != 14 || m["proposal"] != key || len(m) != 3 {
		t.Errorf("meta = %v (no names or filters)", m)
	}
	// create_rule is recorded too (without a proposal key it stays out of appliedProposals);
	// requests without the header are ordinary edits
	b.header["X-Assistant-Chat"] = id
	expect(t, b.do("POST", "/rules", map[string]any{"name": "Internet", "match": map[string]any{"filters": []any{
		map[string]any{"column": "payee", "cond": "OPERATOR INTERNETU"}}}, "setCategoryId": nc.body["id"]}), 201, "")
	delete(b.header, "X-Assistant-Chat")
	expect(t, b.do("POST", "/transactions/bulk", map[string]any{"ids": ids[:1], "category": map[string]any{"reset": true}}), 200, "")
	if ev := events(); len(ev) != 2 || obj(t, ev[0], "meta")["action"] != "create_rule" || len(applied()) != 1 {
		t.Errorf("activity after create_rule and a plain edit = %v", ev)
	}
	if strings.Contains(e.logs.String(), "OPERATOR") {
		t.Error("a payee reached the logs")
	}
	b.header["X-Assistant-Proposal"] = "XYZ"
	if r := b.do("POST", "/transactions/bulk", map[string]any{"ids": ids[:1], "category": map[string]any{"reset": true}}); r.code < 400 || r.code >= 500 {
		t.Errorf("bad proposal header = %d", r.code)
	}
	delete(b.header, "X-Assistant-Proposal")
	b.header["X-Assistant-Chat"] = "not-a-uuid"
	if r := b.do("POST", "/transactions/bulk", map[string]any{"ids": ids[:1], "category": map[string]any{"reset": true}}); r.code < 400 || r.code >= 500 {
		t.Errorf("bad header = %d", r.code)
	}
	delete(b.header, "X-Assistant-Chat")
}
