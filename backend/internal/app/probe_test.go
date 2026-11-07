package app

import (
	"context"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/config"
	"github.com/x2db736ac85h/finuance/backend/internal/llm"
	"github.com/x2db736ac85h/finuance/backend/testutil/llmmock"
)

func TestProbe(t *testing.T) {
	srv := httptest.NewServer(llmmock.New())
	defer srv.Close()
	u, _ := url.Parse(srv.URL + "/v1")
	cfg := config.Config{AITimeout: 10 * time.Second, AIConnections: []config.AIConnection{
		{Name: "mock", Label: "Mock", API: llm.OpenAI, BaseURL: u, ContextTokens: 8192, DefaultModel: "mock-tools"}}}
	dir := t.TempDir()
	var out strings.Builder
	err := Probe(context.Background(), cfg, ProbeInput{OutDir: dir}, func(f string, a ...any) { fmt.Fprintf(&out, f+"\n", a...) })
	if err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{"mock (Mock, openai, local)", "mock-tools", "[default]", "probing mock / mock-tools",
		"text ", "tool_call", "call search_transactions", "tool_result", "plan "} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q:\n%s", want, got)
		}
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.txt"))
	if len(files) < 5 {
		t.Fatalf("%d files recorded", len(files))
	}
	var chat []byte
	for _, f := range files {
		if strings.Contains(f, "chat_completions") {
			chat, _ = os.ReadFile(f)
			break
		}
	}
	if !strings.Contains(string(chat), "--- response 200 text/event-stream") || !strings.Contains(string(chat), "data: ") ||
		strings.Contains(string(chat), "Authorization") {
		t.Errorf("recorded exchange:\n%s", chat)
	}
	// a model that reads images gets a picture too (Phase 20)
	mock := llmmock.New()
	srv2 := httptest.NewServer(mock)
	defer srv2.Close()
	u2, _ := url.Parse(srv2.URL + "/v1")
	cfg.AIConnections[0].BaseURL = u2
	out.Reset()
	if err := Probe(context.Background(), cfg, ProbeInput{Model: "mock-vl"}, func(f string, a ...any) { fmt.Fprintf(&out, f+"\n", a...) }); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "vision=true") || !strings.Contains(out.String(), "  vision ") {
		t.Errorf("vision probe missing:\n%s", out.String())
	}
	images := 0
	for _, r := range mock.Requests() {
		images += strings.Count(string(r), "data:image/jpeg;base64,")
	}
	if images != 1 {
		t.Errorf("%d images sent", images)
	}
	// nothing configured
	if err := Probe(context.Background(), config.Config{}, ProbeInput{}, func(string, ...any) {}); err == nil {
		t.Error("no connection accepted")
	}
}
