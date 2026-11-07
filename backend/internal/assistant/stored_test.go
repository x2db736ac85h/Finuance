package assistant

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/config"
	"github.com/x2db736ac85h/finuance/backend/internal/llm"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/testutil"
	"github.com/x2db736ac85h/finuance/backend/testutil/llmmock"
)

// keySpy is a model server that lists one model and remembers the API key it was sent.
type keySpy struct {
	mu   sync.Mutex
	keys []string
}

func (k *keySpy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	k.mu.Lock()
	k.keys = append(k.keys, r.Header.Get("Authorization"))
	k.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"spy-model","object":"model"}]}`))
}

func (k *keySpy) last() string {
	k.mu.Lock()
	defer k.mu.Unlock()
	if len(k.keys) == 0 {
		return "(none)"
	}
	return k.keys[len(k.keys)-1]
}

func newStoredEnv(t *testing.T, key []byte, file []config.AIConnection) (*env, *Connections, *ConnectionStore) {
	t.Helper()
	e := newEnv(t)
	if _, err := testutil.Require(t, testDB).Owner.Exec(context.Background(),
		`DELETE FROM assistant_connections; DELETE FROM assistant_model_settings`); err != nil { // instance tables
		t.Fatal(err)
	}
	clock := platform.FixedClock{T: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)}
	conns := NewConnections(file, 10*time.Second, 0, clock, nil)
	s, err := NewConnectionStore(e.st, key, clock, conns, file, 8192, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.resolve = func(_ context.Context, host string) (bool, error) { return host != "public.example", nil }
	return e, conns, s
}

// TestStoredConnections: a connection added in the panel joins the registry with its key sealed;
// the key never comes back, follows only the address it was entered for, and a key sealed under
// another instance key doesn't open (ADR 0035).
func TestStoredConnections(t *testing.T) {
	spy := &keySpy{}
	srv := httptest.NewServer(spy)
	t.Cleanup(srv.Close)
	fileURL, _ := url.Parse("http://127.0.0.1:1/v1")
	file := []config.AIConnection{{Name: "lmstudio", Label: "LM Studio", API: llm.OpenAI, BaseURL: fileURL, ContextTokens: 8192}}
	key := bytes.Repeat([]byte{3}, 32)
	e, conns, s := newStoredEnv(t, key, file)
	ctx := e.ctx

	spec := config.ConnectionSpec{Name: "gpu-box", Label: "GPU box", API: "openai", BaseURL: srv.URL + "/v1", Models: []string{"spy-*"},
		APIKey: "sk-secret-1"}
	c, err := s.Create(ctx, spec)
	if err != nil || !c.HasKey || c.KeyUnreadable || c.Source != SourcePanel || c.Version != 1 || c.ContextTokens != 0 {
		t.Fatalf("created = %+v %v", c, err)
	}
	if _, info, err := conns.Resolve(ctx, "gpu-box", ""); err != nil || info.ID != "spy-model" || spy.last() != "Bearer sk-secret-1" {
		t.Fatalf("resolve = %+v %v, key sent %q", info, err, spy.last())
	}
	list, err := s.List(ctx)
	if err != nil || len(list) != 2 || list[0].Source != SourceFile || list[1].Name != "gpu-box" {
		t.Fatalf("list = %+v %v", list, err)
	}
	var raw []byte
	if err := testutil.Require(t, testDB).Owner.QueryRow(ctx, `SELECT api_key FROM assistant_connections`).Scan(&raw); err != nil ||
		bytes.Contains(raw, []byte("sk-secret")) {
		t.Errorf("stored key = %x %v", raw, err)
	}

	// refused: a file connection's name, a public address for a local one, http for a cloud one,
	// a bad pattern, a taken name
	var ve *platform.ValidationError
	for want, bad := range map[string]config.ConnectionSpec{
		"configuration file": {Name: "lmstudio", API: "openai", BaseURL: srv.URL},
		"public address":     {Name: "x1", API: "openai", BaseURL: "http://public.example/v1"},
		"must use https":     {Name: "x2", API: "openai", BaseURL: "http://api.example/v1", Cloud: true},
		"not a valid":        {Name: "x3", API: "openai", BaseURL: srv.URL, Models: []string{"["}},
		"taken":              {Name: "gpu-box", API: "openai", BaseURL: srv.URL},
		"credentials":        {Name: "x4", API: "openai", BaseURL: "http://u:p@127.0.0.1/v1"},
	} {
		if _, err := s.Create(ctx, bad); !errors.As(err, &ve) || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", want, err)
		}
	}

	// kept on a label change; dropped when the address changes; replaced; removed
	spec.Label = "GPU box (Ollama)"
	c, err = s.Update(ctx, "gpu-box", 1, spec, KeyChange{})
	if err != nil || !c.HasKey || c.Version != 2 || c.Label != "GPU box (Ollama)" {
		t.Fatalf("label = %+v %v", c, err)
	}
	if _, err := s.Update(ctx, "gpu-box", 1, spec, KeyChange{}); !errors.Is(err, platform.ErrStaleVersion) {
		t.Errorf("stale: %v", err)
	}
	spec.BaseURL = strings.Replace(srv.URL, "127.0.0.1", "localhost", 1) + "/v1"
	if c, err = s.Update(ctx, "gpu-box", 2, spec, KeyChange{}); err != nil || c.HasKey {
		t.Errorf("a kept key followed a new address: %+v %v", c, err)
	}
	if c, err = s.Update(ctx, "gpu-box", 3, spec, KeyChange{Set: true, Value: "sk-secret-2"}); err != nil || !c.HasKey {
		t.Fatalf("replace = %+v %v", c, err)
	}
	conns.Status(ctx, true)
	if spy.last() != "Bearer sk-secret-2" {
		t.Errorf("key sent %q", spy.last())
	}

	// another instance key: listed as unreadable, used without a key
	_, conns2, s2 := newStoredEnvShared(t, e, bytes.Repeat([]byte{4}, 32), conns)
	_ = conns2
	if list, _ := s2.List(ctx); len(list) != 1 || !list[0].KeyUnreadable {
		t.Errorf("other key = %+v", list)
	}
	// no instance key: no key can be stored
	_, _, s3 := newStoredEnvShared(t, e, nil, conns)
	if _, err := s3.Create(ctx, config.ConnectionSpec{Name: "x5", API: "openai", BaseURL: srv.URL, APIKey: "k"}); !errors.As(err, &ve) ||
		!strings.Contains(err.Error(), "FINUANCE_INSTANCE_KEY") {
		t.Errorf("no instance key: %v", err)
	}

	if c, err = s.Update(ctx, "gpu-box", 4, spec, KeyChange{Set: true}); err != nil || c.HasKey {
		t.Errorf("remove = %+v %v", c, err)
	}
	if err := s.Delete(ctx, "gpu-box"); err != nil {
		t.Fatal(err)
	}
	if _, err := conns.Get("gpu-box"); !errors.Is(err, ErrUnknownConnection) {
		t.Errorf("deleted connection still offered: %v", err)
	}
	if err := s.Delete(ctx, "gpu-box"); !errors.Is(err, platform.ErrNotFound) {
		t.Errorf("deleted twice: %v", err)
	}
}

// newStoredEnvShared is a second store on the same database with another instance key.
func newStoredEnvShared(t *testing.T, e *env, key []byte, _ *Connections) (*env, *Connections, *ConnectionStore) {
	t.Helper()
	clock := platform.FixedClock{T: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)}
	conns := NewConnections(nil, 10*time.Second, 0, clock, nil)
	s, err := NewConnectionStore(e.st, key, clock, conns, nil, 8192, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.resolve = func(context.Context, string) (bool, error) { return true, nil }
	if err := s.Load(e.ctx); err != nil {
		t.Fatal(err)
	}
	return e, conns, s
}

// TestTuning: a model's tuning sets the run's defaults (temperature, thinking, tool set), the
// context it plans with and a note after the instructions; the question's own choices win;
// setting nothing removes it.
func TestTuning(t *testing.T) {
	re := newRunEnv(t, 1)
	s, err := NewConnectionStore(re.st, nil, re.clock, re.runner.cfg.Connections, nil, 8192, nil)
	if err != nil {
		t.Fatal(err)
	}
	temp, ctxTokens := 0.7, 4096
	tu, err := s.SetTuning(re.ctx, Tuning{Connection: "mock", Model: "mock-tools", Temperature: &temp, ContextTokens: &ctxTokens,
		Think: llm.ThinkOff, Toolset: ToolsetCompact, ExtraPrompt: " Prefer short answers. "})
	if err != nil || tu.ExtraPrompt != "Prefer short answers." {
		t.Fatalf("tuning = %+v %v", tu, err)
	}
	if _, info, err := re.runner.cfg.Connections.Resolve(re.ctx, "mock", "mock-tools"); err != nil || info.Caps.ContextTokens != 4096 || info.Tuning == nil {
		t.Fatalf("resolved = %+v %v", info, err)
	}
	re.mock.Push(llmmock.Reply{Content: "Hi."})
	re.ask(re.newChat(), "Hello", nil)
	req := re.mock.Requests()
	body := string(req[len(req)-1])
	if !strings.Contains(body, `"temperature":0.7`) || !strings.Contains(body, "Prefer short answers.") ||
		strings.Contains(body, `"name":"month_overview"`) {
		t.Errorf("request without the tuning: %s", body)
	}
	// the question's own temperature wins
	re.mock.Push(llmmock.Reply{Content: "Hi."})
	zero := 0.0
	re.ask(re.newChat(), "Hello", func(r *Request) { r.Temperature = &zero })
	if req := re.mock.Requests(); !strings.Contains(string(req[len(req)-1]), `"temperature":0,`) {
		t.Errorf("the question's temperature lost: %s", req[len(req)-1])
	}

	var ve *platform.ValidationError
	bad, big := 3.0, 100
	if _, err := s.SetTuning(re.ctx, Tuning{Connection: "mock", Model: "m", Temperature: &bad, ContextTokens: &big, Think: "max", Toolset: "all"}); !errors.As(err, &ve) ||
		!strings.Contains(err.Error(), "temperature") || !strings.Contains(err.Error(), "contextTokens") || !strings.Contains(err.Error(), "think") ||
		!strings.Contains(err.Error(), "toolset") {
		t.Errorf("invalid: %v", err)
	}
	if _, err := s.SetTuning(re.ctx, Tuning{Connection: "mock", Model: "mock-tools"}); err != nil {
		t.Fatal(err)
	}
	if list, err := s.Tunings(re.ctx); err != nil || len(list) != 0 {
		t.Errorf("after removing = %+v %v", list, err)
	}
	if _, info, _ := re.runner.cfg.Connections.Resolve(re.ctx, "mock", "mock-tools"); info.Tuning != nil || info.Caps.ContextTokens == 4096 {
		t.Errorf("tuning kept: %+v", info)
	}
}
