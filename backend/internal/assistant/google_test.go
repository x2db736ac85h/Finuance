package assistant

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/x2db736ac85h/finuance/backend/internal/config"
	"github.com/x2db736ac85h/finuance/backend/internal/instance"
	"github.com/x2db736ac85h/finuance/backend/internal/llm"
	"github.com/x2db736ac85h/finuance/backend/internal/llm/googleauth"
	"github.com/x2db736ac85h/finuance/backend/internal/observability"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/testutil/llmmock"
)

// vertexLab is Vertex AI and Google's metadata server as the mock plays them (PLAN 26.6), and a
// connection with `auth: google` that reaches them: every request the connection makes to its
// Google host lands on the mock, and the standard credentials find the mock's metadata path.
type vertexLab struct {
	mock  *llmmock.Server
	cs    *Connections
	conn  *Connection
	ahead time.Duration // how far the test's clock is ahead of the real one
	mu    sync.Mutex
	paths []string // every request the mock's two addresses saw
	hosts []string // the Host header of the requests to the endpoint
}

func (l *vertexLab) now() time.Time { return time.Now().Add(l.ahead) }

func newVertexLab(t *testing.T) *vertexLab {
	t.Helper()
	l := &vertexLab{mock: llmmock.New()}
	l.mock.Vertex = &llmmock.Vertex{Project: "demo-project", Location: "europe-west4", TokenTTL: 30 * time.Minute, Now: l.now}
	record := func(endpoint bool) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			l.mu.Lock()
			l.paths = append(l.paths, r.URL.Path)
			if endpoint {
				l.hosts = append(l.hosts, r.Host)
			}
			l.mu.Unlock()
			l.mock.ServeHTTP(w, r)
		})
	}
	endpoint := httptest.NewTLSServer(record(true))
	metadata := httptest.NewServer(record(false))
	t.Cleanup(endpoint.Close)
	t.Cleanup(metadata.Close)
	// the standard way to find credentials, pointed at the mock and at nothing of this machine
	t.Setenv("GCE_METADATA_HOST", strings.TrimPrefix(metadata.URL, "http://"))
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLOUDSDK_CONFIG", t.TempDir())

	c, problems := config.NewConnection(config.ConnectionSpec{Name: "vertex", Label: "Gemini", API: "openai", Auth: config.AuthGoogle,
		Project: "demo-project", Location: "europe-west4", Cloud: true, Models: []string{"google/gemini-3.5-flash-lite", "google/gemini-*"}}, 8192)
	if len(problems) > 0 {
		t.Fatalf("the connection is refused: %v", problems)
	}
	if got := c.BaseURL.String(); got != "https://europe-west4-aiplatform.googleapis.com"+l.mock.Vertex.Base() {
		t.Fatalf("derived address = %s", got)
	}
	// whatever Google host the connection asks for, the dial ends at the mock
	toMock := &http.Transport{DialTLSContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		d := tls.Dialer{Config: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec // the test's own server
		return d.DialContext(ctx, "tcp", endpoint.Listener.Addr().String())
	}}
	t.Cleanup(toMock.CloseIdleConnections)
	l.cs = NewConnections(nil, time.Minute, 0, &fakeClock{t: time.Now()}, func(cfg llm.Config) llm.Client {
		cfg.HTTPClient = &http.Client{Transport: toMock}
		return llm.New(cfg)
	})
	l.cs.Google = googleauth.NewWith(nil, l.now)
	l.cs.SetStored([]config.AIConnection{c}, nil, nil)
	var err error
	if l.conn, err = l.cs.Get("vertex"); err != nil {
		t.Fatal(err)
	}
	return l
}

func (l *vertexLab) ask(t *testing.T) (llm.Result, error) {
	t.Helper()
	return l.conn.Client.Chat(context.Background(), llm.Request{Model: "google/gemini-3.5-flash-lite",
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "How much did I spend on coffee?"}}}, nil)
}

// TestGoogleConnection (PLAN 26.1–26.3, 26.6): a connection with `auth: google` gets its token from
// the metadata server, renews it before it runs out without a failed request, gets a new one once
// after a 401, is not treated as a local model server, and stores no key.
func TestGoogleConnection(t *testing.T) {
	l := newVertexLab(t)
	v := l.mock.Vertex
	if l.conn.APIKey != "" {
		t.Fatal("a Google connection holds a key")
	}

	// the model list is the connection's own: the endpoint has none, and nothing beside it is asked
	st, err := l.cs.StatusOf(context.Background(), "vertex", true)
	if err != nil || !st.Reachable || len(st.Models) != 1 || st.Models[0].ID != "google/gemini-3.5-flash-lite" {
		t.Fatalf("status = %+v, %v", st, err)
	}
	if m := st.Models[0]; !m.Caps.Vision || m.Caps.Reasoning != llm.ReasonEffort || !m.Caps.Tools {
		t.Errorf("caps of a Gemini model = %+v, want vision, tools and reasoning effort", m.Caps)
	}
	for _, p := range l.paths {
		if strings.HasPrefix(p, "/api/") || strings.HasSuffix(p, "/api/v0/models") {
			t.Errorf("the endpoint was probed like a local server: %s", p)
		}
	}
	// loading and unloading don't apply to a provider's endpoint
	if err := l.cs.LoadModel(context.Background(), "vertex", "google/gemini-3.5-flash-lite"); !errors.Is(err, llm.ErrCannotManage) {
		t.Errorf("LoadModel = %v, want ErrCannotManage", err)
	}
	if err := l.cs.UnloadAll(context.Background(), "vertex"); err != nil && !errors.Is(err, llm.ErrCannotManage) {
		t.Errorf("UnloadAll = %v", err)
	}

	// a question: answered with the first token, sent to the Google host
	if r, err := l.ask(t); err != nil || (r.Content == "" && len(r.ToolCalls) == 0) {
		t.Fatalf("first question: %+v, %v", r, err)
	}
	if v.Issued() != 1 || v.Denied() != 0 {
		t.Fatalf("after the first question: %d tokens issued, %d requests refused; want 1 and 0", v.Issued(), v.Denied())
	}
	hdr := l.mock.Headers()
	if got := hdr[len(hdr)-1].Get("Authorization"); got != "Bearer ya29.mock-1" {
		t.Errorf("the question carried %q", got)
	}
	for _, h := range l.hosts {
		if h != "europe-west4-aiplatform.googleapis.com" {
			t.Errorf("a request went to host %q", h)
		}
	}

	// 26 of the token's 30 minutes pass: it is replaced before any request fails with it
	l.ahead = 26 * time.Minute
	if _, err := l.ask(t); err != nil {
		t.Fatalf("with 4 minutes left on the token: %v", err)
	}
	if v.Issued() != 2 || v.Denied() != 0 {
		t.Errorf("renewal: %d tokens issued, %d requests refused; want 2 and 0", v.Issued(), v.Denied())
	}

	// the token is revoked while the cache still trusts it: one 401, one new token, one more try
	v.Revoke()
	if _, err := l.ask(t); err != nil {
		t.Fatalf("after the token was revoked: %v", err)
	}
	if v.Issued() != 3 || v.Denied() != 1 {
		t.Errorf("after a revoked token: %d issued, %d refused; want 3 and 1", v.Issued(), v.Denied())
	}

	// many questions at once after the token ran out: one renewal for all of them
	l.ahead = 2 * time.Hour
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			if _, err := l.ask(t); err != nil {
				t.Errorf("a concurrent question: %v", err)
			}
		})
	}
	wg.Wait()
	if v.Issued() != 4 || v.Denied() != 1 {
		t.Errorf("12 questions after expiry: %d issued, %d refused; want one more token (4) and no more refusals (1)", v.Issued(), v.Denied())
	}
}

// TestGoogleConnectionRules (PLAN 26.2): what `auth: google` requires, and where its token may go.
func TestGoogleConnectionRules(t *testing.T) {
	ok := config.ConnectionSpec{Name: "vertex", API: "openai", Auth: config.AuthGoogle, Project: "demo-project", Location: "eu", Cloud: true}
	c, problems := config.NewConnection(ok, 8192)
	if len(problems) > 0 || c.BaseURL.Host != "aiplatform.eu.rep.googleapis.com" {
		t.Fatalf("a multi-region connection: %v, %v", c.BaseURL, problems)
	}
	global := ok
	global.Location = "global"
	if c, problems := config.NewConnection(global, 8192); len(problems) > 0 || c.BaseURL.Host != "aiplatform.googleapis.com" {
		t.Errorf("global, when named: %v, %v", c.BaseURL, problems)
	}
	for name, tc := range map[string]struct {
		mod   func(*config.ConnectionSpec)
		field string
	}{
		"no location: there is no default":    {func(s *config.ConnectionSpec) { s.Location = "" }, "location"},
		"a location that is none":             {func(s *config.ConnectionSpec) { s.Location = "Europe West" }, "location"},
		"no project":                          {func(s *config.ConnectionSpec) { s.Project = "" }, "project"},
		"not marked as cloud":                 {func(s *config.ConnectionSpec) { s.Cloud = false }, "cloud"},
		"with a key":                          {func(s *config.ConnectionSpec) { s.APIKey = "sk-1" }, "apiKey"},
		"another host":                        {func(s *config.ConnectionSpec) { s.BaseURL = "https://example.test/v1" }, "baseUrl"},
		"a host that only ends like Google's": {func(s *config.ConnectionSpec) { s.BaseURL = "https://evil-aiplatform.googleapis.com.example.test/v1" }, "baseUrl"},
		"a host with Google's name in front":  {func(s *config.ConnectionSpec) { s.BaseURL = "https://aiplatform.googleapis.com.example.test/v1" }, "baseUrl"},
		"plain http":                          {func(s *config.ConnectionSpec) { s.BaseURL = "http://europe-west4-aiplatform.googleapis.com/v1" }, "baseUrl"},
		"another port":                        {func(s *config.ConnectionSpec) { s.BaseURL = "https://europe-west4-aiplatform.googleapis.com:8443/v1" }, "baseUrl"},
		"credentials in the address": {func(s *config.ConnectionSpec) {
			s.BaseURL = "https://user:secret" + "@" + "europe-west4-aiplatform.googleapis.com/v1"
		}, "baseUrl"},
		"an unknown way to authenticate": {func(s *config.ConnectionSpec) { s.Auth = "azure" }, "auth"},
	} {
		spec := ok
		tc.mod(&spec)
		if _, problems := config.NewConnection(spec, 8192); problems[tc.field] == "" {
			t.Errorf("%s: problems = %v, want one for %s", name, problems, tc.field)
		}
	}
	// project and location mean nothing without Google
	if _, problems := config.NewConnection(config.ConnectionSpec{Name: "x", API: "openai", BaseURL: "http://10.0.0.5:1234/v1", Project: "demo-project"}, 8192); problems["project"] == "" {
		t.Errorf("a key connection with a project: %v", problems)
	}
	for host, want := range map[string]bool{
		"aiplatform.googleapis.com":                         true,
		"europe-west4-aiplatform.googleapis.com":            true,
		"us-central1-aiplatform.googleapis.com":             true,
		"northamerica-northeast1-aiplatform.googleapis.com": true,
		"aiplatform.eu.rep.googleapis.com":                  true,
		"AIPLATFORM.googleapis.com.":                        true,
		"evilaiplatform.googleapis.com":                     false,
		"x.aiplatform.googleapis.com":                       false,
		"aiplatform.googleapis.com.example.test":            false,
		"europe-west4-aiplatform.googleapis.com.evil.test":  false,
		"generativelanguage.googleapis.com":                 false,
		"storage.googleapis.com":                            false,
		"169.254.169.254":                                   false,
		"":                                                  false,
	} {
		if got := llm.GoogleAPIHost(host); got != want {
			t.Errorf("GoogleAPIHost(%q) = %v, want %v", host, got, want)
		}
	}
}

// TestCanaryATokenIsNeverLoggedOrTraced (PLAN 26.1; the canary of 23.4, for credentials): a whole
// question through a Google connection, a renewal after a 401 and a failed token request among
// them, with every log line at debug level and every span captured before redaction. No token the
// metadata server issued appears in any of them. The control: the tokens did travel, in the
// requests' Authorization header.
func TestCanaryATokenIsNeverLoggedOrTraced(t *testing.T) {
	re := newRunEnv(t, 2)
	l := newVertexLab(t)
	rec := tracetest.NewSpanRecorder()
	var logs bytes.Buffer
	re.runner = NewRunner(RunnerConfig{Connections: l.cs, Tools: NewTools(re.q), Chats: re.chats, Clock: re.clock, MaxConcurrent: 2,
		Tracer: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)), Logger: observability.NewLogger(&logs, config.EnvDev, slog.LevelDebug)})
	if _, err := re.chats.SaveSettings(re.ctx, re.user, Settings{CloudConsent: []string{"vertex"}}); err != nil {
		t.Fatal(err)
	}
	vertex := func(r *Request) { r.Connection, r.Model = "vertex", "google/gemini-3.5-flash-lite" }
	if evs := re.ask(re.newChat(), "How much did I spend on coffee?", vertex); last(evs).Name != "done" {
		t.Fatalf("a question through the Google connection: %s", names(evs))
	}
	l.mock.Vertex.Revoke() // the next question meets a 401 and renews
	if evs := re.ask(re.newChat(), "And on books?", vertex); last(evs).Name != "done" {
		t.Fatalf("after a revoked token: %s", names(evs))
	}
	// credentials that can't be had: the run ends with an error the logs describe
	t.Setenv("GCE_METADATA_HOST", "127.0.0.1:1")
	l.mock.Vertex.Revoke()
	l.cs.Google.Invalidate()
	if evs := re.ask(re.newChat(), "And on tea?", vertex); last(evs).Name != "error" {
		t.Fatalf("without credentials: %s", names(evs))
	}

	var sent []string
	for _, h := range l.mock.Headers() {
		sent = append(sent, strings.TrimPrefix(h.Get("Authorization"), "Bearer "))
	}
	if len(sent) < 2 || l.mock.Vertex.Issued() < 2 || !strings.HasPrefix(sent[0], "ya29.mock-") {
		t.Fatalf("tokens sent: %q, issued: %d: the test proves nothing", sent, l.mock.Vertex.Issued())
	}
	out := logs.String()
	if strings.Count(out, "assistant run") < 3 {
		t.Fatalf("the logger is not connected:\n%s", out)
	}
	var traced strings.Builder
	for _, s := range rec.Ended() {
		fmt.Fprintf(&traced, "%s %v %v %+v\n", s.Name(), s.Attributes(), s.Events(), s.Status())
	}
	if len(rec.Ended()) < 4 {
		t.Fatal("the tracer is not connected")
	}
	for _, marker := range []string{"ya29.", "Bearer "} {
		if strings.Contains(out, marker) {
			t.Errorf("a log line carries %q:\n%s", marker, out)
		}
		if strings.Contains(traced.String(), marker) {
			t.Errorf("a span carries %q:\n%s", marker, traced.String())
		}
	}
}

// TestConnectionBudget (PLAN 26.4): a connection with a budget of tokens per day stops taking
// questions once the tokens its server reported reach it, for every user, until the next UTC day;
// other connections go on. The count is the provider's own: the request asks for the usage of the
// stream (stream_options.include_usage), which a server reports only when asked.
func TestConnectionBudget(t *testing.T) {
	re := newRunEnv(t, 2)
	if _, err := testDB.Owner.Exec(re.ctx, `DELETE FROM assistant_connection_usage`); err != nil {
		t.Fatal(err)
	}
	re.conns.SetStored(nil, nil, map[string]Controls{"mock": {Enabled: true, DailyTokens: 1000}})
	ask := func(mod func(*Request)) (string, error) {
		req := Request{ChatID: re.newChat(), Text: "How much did I spend on coffee?", Connection: "mock", Model: "mock-tools"}
		if mod != nil {
			mod(&req)
		}
		run, err := re.runner.Prepare(re.ctx, re.user, req)
		if err != nil {
			return "", err
		}
		var evs []Event
		run.Execute(re.ctx, func(e Event) { evs = append(evs, e) })
		return last(evs).Name, nil
	}
	if name, err := ask(nil); err != nil || name != "done" {
		t.Fatalf("the first question: %s, %v", name, err)
	}
	// the request asked for the stream's usage, and what came back is what was counted
	reqs := re.mock.Requests()
	if !strings.Contains(string(reqs[len(reqs)-1]), `"stream_options":{"include_usage":true}`) {
		t.Fatalf("the request does not ask for usage: %s", reqs[len(reqs)-1])
	}
	used, err := re.chats.ConnectionTokensToday(re.ctx, "mock")
	if err != nil || used == 0 || used%120 != 0 {
		t.Fatalf("tokens counted for the connection = %d, %v; want the mock's 120 a request", used, err)
	}
	// more questions, until the day's budget is gone
	for i := 0; used < 1000 && i < 20; i++ {
		if _, err := ask(nil); err != nil {
			t.Fatalf("question %d with %d of 1000 tokens used: %v", i, used, err)
		}
		used, _ = re.chats.ConnectionTokensToday(re.ctx, "mock")
	}
	var le *instance.LimitError
	if _, err := ask(nil); !errors.As(err, &le) || le.Limit != LimitConnectionTokens || le.Value != 1000 || !errors.Is(err, platform.ErrLimitReached) {
		t.Fatalf("with the budget used up (%d): %v, want limit_reached for the connection", used, err)
	}
	// it holds for everyone, not for the person who used it up
	mine := re.user
	re.user = re.env.user()
	re.importFile(re.user, "Demo", fixture(t, "fixtures/demo/demo-legacy.csv"))
	if _, err := ask(nil); !errors.As(err, &le) {
		t.Errorf("another user on the same connection: %v", err)
	}
	re.user = mine
	// another connection has its own count
	if name, err := ask(func(r *Request) { r.Connection, r.Model = "claude", "" }); err != nil || name != "done" {
		t.Errorf("another connection: %s, %v", name, err)
	}
	// the next UTC day starts at nothing
	re.clock.add(24 * time.Hour)
	if name, err := ask(nil); err != nil || name != "done" {
		t.Errorf("the next day: %s, %v", name, err)
	}
	// and without a budget nothing is refused
	re.conns.SetStored(nil, nil, nil)
	for range 3 {
		if _, err := ask(nil); err != nil {
			t.Fatalf("without a budget: %v", err)
		}
	}
}

// TestProvider (PLAN 26.5): who a person is told gets their data, and where.
func TestProvider(t *testing.T) {
	google, _ := config.NewConnection(config.ConnectionSpec{Name: "vertex", API: "openai", Auth: config.AuthGoogle, Project: "demo-project",
		Location: "europe-west4", Cloud: true}, 8192)
	for _, tc := range []struct {
		c                  config.AIConnection
		provider, location string
	}{
		{google, "Google Cloud (Vertex AI)", "europe-west4"},
		{conn("a", "https://api.anthropic.com", func(c *config.AIConnection) { c.Cloud = true }), "Anthropic", ""},
		{conn("o", "https://api.openai.com/v1", func(c *config.AIConnection) { c.Cloud = true }), "OpenAI", ""},
		{conn("r", "https://openrouter.ai/api/v1", func(c *config.AIConnection) { c.Cloud = true }), "openrouter.ai", ""},
		{conn("l", "http://10.0.0.5:11434/v1", nil), "", ""},
	} {
		if p, l := Provider(&tc.c); p != tc.provider || l != tc.location {
			t.Errorf("%s: provider %q, location %q; want %q, %q", tc.c.Name, p, l, tc.provider, tc.location)
		}
	}
}

// TestClaudeOnVertex (PLAN 26.9): a Google connection in the anthropic format speaks Vertex AI's
// variant of the Messages API: the model in the address, the version in the body, the bearer token
// from Google's credentials, the connection's own model ids as the list.
func TestClaudeOnVertex(t *testing.T) {
	l := newVertexLab(t)
	c, problems := config.NewConnection(config.ConnectionSpec{Name: "claude-vertex", Label: "Claude", API: "anthropic", Auth: config.AuthGoogle,
		Project: "demo-project", Location: "europe-west4", Cloud: true, Models: []string{"claude-sonnet-5-5"}}, 8192)
	if len(problems) > 0 {
		t.Fatalf("the connection is refused: %v", problems)
	}
	if got, want := c.BaseURL.String(), "https://europe-west4-aiplatform.googleapis.com/v1/projects/demo-project/locations/europe-west4"; got != want {
		t.Fatalf("derived address = %s, want %s", got, want)
	}
	gemini, _ := l.cs.Get("vertex")
	l.cs.SetStored([]config.AIConnection{gemini.AIConnection, c}, nil, nil)
	st, err := l.cs.StatusOf(context.Background(), "claude-vertex", true)
	if err != nil || !st.Reachable || len(st.Models) != 1 || st.Models[0].ID != "claude-sonnet-5-5" || st.Models[0].Caps.Reasoning != llm.ReasonAdaptive {
		t.Fatalf("status = %+v, %v", st, err)
	}
	conn, _ := l.cs.Get("claude-vertex")
	l.mu.Lock()
	l.paths = nil
	l.mu.Unlock()
	res, err := conn.Client.Chat(context.Background(), llm.Request{Model: "claude-sonnet-5-5", System: "Be brief.",
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "How much did I spend on coffee?"}}}, nil)
	if err != nil || (res.Content == "" && len(res.ToolCalls) == 0) {
		t.Fatalf("a question to Claude on Vertex: %+v, %v", res, err)
	}
	if want := l.mock.Vertex.Claude() + "claude-sonnet-5-5:streamRawPredict"; len(l.paths) == 0 || l.paths[len(l.paths)-1] != want {
		t.Errorf("the question went to %q, want %s", l.paths, want)
	}
	reqs, hdrs := l.mock.Requests(), l.mock.Headers()
	body, hdr := string(reqs[len(reqs)-1]), hdrs[len(hdrs)-1]
	if !strings.Contains(body, `"anthropic_version":"vertex-2023-10-16"`) || strings.Contains(body, `"model"`) {
		t.Errorf("the body must carry the version and no model: %s", body)
	}
	if !strings.HasPrefix(hdr.Get("Authorization"), "Bearer ya29.mock-") || hdr.Get("X-Api-Key") != "" || hdr.Get("Anthropic-Version") != "" {
		t.Errorf("headers = %v", hdr)
	}
	// a revoked token is renewed once here too
	l.mock.Vertex.Revoke()
	if _, err := conn.Client.Chat(context.Background(), llm.Request{Model: "claude-sonnet-5-5",
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "And on books?"}}}, nil); err != nil {
		t.Errorf("after a revoked token: %v", err)
	}
}
