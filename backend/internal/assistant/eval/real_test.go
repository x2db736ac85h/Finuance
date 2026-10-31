//go:build eval

package eval

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/assistant"
	"github.com/x2db736ac85h/finuance/backend/internal/config"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
)

// TestEvalAssistant is `make eval-assistant CONNECTION=… MODEL=…`: the whole suite against a real
// model, configured like the server (FINUANCE_AI_BASE_URL or FINUANCE_AI_CONNECTIONS_FILE). The
// result goes to evals/assistant/results/ and docs/evals.md is rebuilt from every result there.
func TestEvalAssistant(t *testing.T) {
	cfg, err := config.Load(os.LookupEnv, config.ForHealthcheck)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.AIConnections) == 0 {
		t.Fatal("set FINUANCE_AI_BASE_URL (or FINUANCE_AI_CONNECTIONS_FILE) to the model server")
	}
	st, u := demoUser(t)
	f, err := Load(questionsPath)
	if err != nil {
		t.Fatal(err)
	}
	clock := platform.FixedClock{T: f.Today}
	conns := assistant.NewConnections(cfg.AIConnections, cfg.AITimeout, clock, nil)
	a := assistant.New(assistant.Config{Store: st, Query: query.New(st), Connections: conns, Clock: clock, MaxChats: 1000, MaxConcurrent: 1})
	ctx := context.Background()
	conn, model, err := conns.Resolve(ctx, os.Getenv("FINUANCE_EVAL_CONNECTION"), os.Getenv("FINUANCE_EVAL_MODEL"))
	if err != nil {
		t.Fatal(err)
	}
	if conn.Cloud { // the eval user agrees to the cloud connection it was pointed at
		if _, err := a.Chats.SaveSettings(ctx, u, assistant.Settings{CloudConsent: []string{conn.Name}}); err != nil {
			t.Fatal(err)
		}
	}
	think := os.Getenv("FINUANCE_EVAL_THINK")
	if think == "" {
		think = "auto"
	}
	req := assistant.Request{Connection: conn.Name, Model: model.ID, Think: Think(think), Mode: os.Getenv("FINUANCE_EVAL_MODE"),
		Toolset: os.Getenv("FINUANCE_EVAL_TOOLSET"), TimeZone: assistant.DefaultTimeZone}
	t.Logf("%s / %s, thinking %s, %d questions", conn.Name, model.ID, think, len(f.Questions))
	cases, err := Run(ctx, a, u, f, req, func(c Case) {
		mark := "ok  "
		if !c.OK {
			mark = "MISS"
		}
		t.Logf("%s %-24s %5.1f s  %s %s", mark, c.ID, float64(c.Ms)/1000, strings.Join(c.Tools, ","), c.Error)
	})
	if err != nil {
		t.Fatal(err)
	}
	res := Result{Connection: conn.Name, Model: model.ID, Think: think, Toolset: req.Toolset, Mode: req.Mode,
		RanAt: time.Now().UTC(), Cases: cases}
	dir := filepath.Join("..", "..", "..", "..", "evals", "assistant", "results")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	name := res.FileName()
	b, _ := json.MarshalIndent(res, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, name), append(b, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	all, err := LoadResults(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("..", "..", "..", "..", "docs", "evals.md"), []byte(Report(all)), 0o600); err != nil {
		t.Fatal(err)
	}
	s := Summarize(res)
	t.Logf("correct %d / %d; tool %d, arguments %d, answer %d; p50 %.1f s, p95 %.1f s", s.OK, s.N, s.ToolOK, s.ArgsOK, s.AnswerOK,
		float64(s.P50)/1000, float64(s.P95)/1000)
}
