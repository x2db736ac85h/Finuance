package assistant

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/x2db736ac85h/finuance/backend/internal/config"
	"github.com/x2db736ac85h/finuance/backend/internal/observability"
)

// TestCanaryNothingOfAChatIsLoggedOrTraced is the test behind the rule "never log prompts, model
// output or tool results" (PLAN 23.4; until now kept by convention). A chat carries marker strings
// in the question, in a transaction that a tool returns, in the tool call the model makes and in
// the model's answer; a second question makes the model server fail. Every log line (at debug
// level, in both formats) and every span, before any redaction, is captured: none of the markers
// may appear in them. The events sent to the user are the control: they do carry the markers.
func TestCanaryNothingOfAChatIsLoggedOrTraced(t *testing.T) {
	const inQuestion, inSearch, inPayee = "CANARYQUESTION", "CANARYSEARCH", "CANARYPAYEE"
	re := newRunEnv(t, 2)
	re.importFile(re.user, "Canary", []byte("Data księgowania;Data waluty;Nadawca / Odbiorca;Adres nadawcy / odbiorcy;Rachunek źródłowy;Rachunek docelowy;Tytułem;Kwota operacji;Waluta;Numer referencyjny;Typ operacji;Kategoria\n"+
		"17.03.2025;17.03.2025;"+inSearch+" "+inPayee+" WARSZAWA;;00000000000000000000000000;;DEMO 1;-12,34;PLN;DEMO0000000001;TRANSAKCJA KARTĄ PŁATNICZĄ;Inne\n"))

	rec := tracetest.NewSpanRecorder() // the spans as recorded, before the exporter's redaction
	var logs bytes.Buffer
	for _, env := range []config.Env{config.EnvProd, config.EnvDev} {
		re.runner = NewRunner(RunnerConfig{Connections: re.conns, Tools: NewTools(re.q), Chats: re.chats, Clock: re.clock, MaxConcurrent: 2,
			Tracer: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)), Logger: observability.NewLogger(&logs, env, slog.LevelDebug)})
		chat := re.newChat()
		evs := re.ask(chat, `What did I pay at "`+inSearch+`"? `+inQuestion, nil)
		if got := names(evs); got != "reasoning,step,content,done" {
			t.Fatalf("%s: events %s", env, got)
		}
		// the control: the markers did travel through the tool call, its result and the answer
		step := evs[slicesIndex(evs, "step")].Data.(StepEvent)
		if !strings.Contains(string(step.Args), inSearch) || !strings.Contains(step.Result, inPayee) || !strings.Contains(text(evs, "content"), inPayee) {
			t.Fatalf("%s: the markers did not reach the tool call (%s), its result or the answer: the test proves nothing", env, step.Args)
		}
		if reqs := re.mock.Requests(); !strings.Contains(string(reqs[len(reqs)-2]), inQuestion) {
			t.Fatalf("%s: the question did not reach the model", env)
		}
		// a failing model server: the error path logs too
		if evs := re.ask(re.newChat(), "please fail "+inQuestion, nil); last(evs).Name != "error" {
			t.Fatalf("%s: events %s", env, names(evs))
		}
	}

	out := logs.String()
	if n := strings.Count(out, "assistant run"); n < 4 {
		t.Fatalf("%d \"assistant run\" lines, want one per question: the logger is not connected\n%s", n, out)
	}
	spans := rec.Ended()
	if len(spans) < 8 {
		t.Fatalf("%d spans: the tracer is not connected", len(spans))
	}
	var traced strings.Builder
	for _, s := range spans {
		fmt.Fprintf(&traced, "%s %v %v %+v\n", s.Name(), s.Attributes(), s.Events(), s.Status())
	}
	for _, marker := range []string{inQuestion, inSearch, inPayee, "mock failure", "Found **"} {
		if strings.Contains(out, marker) {
			t.Errorf("a log line carries %q:\n%s", marker, out)
		}
		if strings.Contains(traced.String(), marker) {
			t.Errorf("a span carries %q:\n%s", marker, traced.String())
		}
	}
}
