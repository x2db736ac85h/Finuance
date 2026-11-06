package assistant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/x2db736ac85h/finuance/backend/internal/instance"
	"github.com/x2db736ac85h/finuance/backend/internal/llm"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
)

// Errors that keep a run from starting (the API maps them to problems before any event is sent).
var (
	ErrDisabled = errors.New("the assistant is not configured")
	ErrRunning  = errors.New("a reply is still being written in another chat or tab")
	ErrBusy     = errors.New("the assistant is busy; try again in a moment")
	ErrConsent  = errors.New("this cloud connection needs your agreement first")
	// ErrUnavailable wraps a model server that can't be used before a run starts (down, no model).
	ErrUnavailable = errors.New("the assistant is unavailable")
)

// errStopRequested is the cancel cause of POST …/stop.
var errStopRequested = errors.New("stopped by the user")

// Run modes.
const (
	ModeAuto  = "auto"
	ModeTools = "tools"
	ModePlan  = "plan"
)

// Limits of one run (legacy runTools and PLAN §4.8).
const (
	maxRounds    = 6
	maxPlanCalls = 6
	MaxQuestion  = 4000
	persistGrace = 10 * time.Second
)

// Request is one question.
type Request struct {
	ChatID      uuid.UUID
	Text        string
	Connection  string // "" = the chat's, else the first connection
	Model       string // "" = the chat's, else the connection's default
	Mode        string // auto, tools or plan
	Think       llm.Thinking
	MaxRows     int      // 1..MaxRowsLimit (0 = MaxRowsLimit)
	Temperature *float64 // nil = 0.2 (legacy)
	View        bool     // ask about the table view in Context instead of all accounts
	Context     query.Context
	TimeZone    string // IANA zone for "today"; "" = DefaultTimeZone
	Toolset     string // "" = full in tools mode, compact in plan mode
	// AttachmentIDs are photos waiting in the chat that go with this question (a model that reads
	// images only; at most MaxAttachments).
	AttachmentIDs []uuid.UUID
	// TokensPerDay is the user's daily token budget (0 = none): a run doesn't start once today's
	// tokens reach it (ADR 0033).
	TokensPerDay int64
	// Digest marks a background digest (counted as one in the usage).
	Digest bool
}

// Event is one server-sent event of a run (PLAN §6.3 AssistantEvent): step, reasoning, content,
// mode, done or error. Data is the JSON payload.
type Event struct {
	Name string
	Data any
}

// DeltaEvent is a piece of the answer (content) or of the reasoning.
type DeltaEvent struct {
	Delta string `json:"delta"`
}

// StepEvent is a lookup that just ran.
type StepEvent struct {
	Index int `json:"index"`
	Step
}

// ModeEvent says the run switched to plan mode (the server refused tools).
type ModeEvent struct {
	Mode   string `json:"mode"`
	Reason string `json:"reason"`
}

// DoneEvent ends a run that wrote an answer (or was stopped).
type DoneEvent struct {
	MessageSeq int  `json:"messageSeq"`
	Meta       Meta `json:"meta"`
}

// ErrorEvent ends a run that failed.
type ErrorEvent struct {
	TurnError
	MessageSeq int `json:"messageSeq,omitempty"`
}

// RunnerConfig wires a Runner.
type RunnerConfig struct {
	Connections   *Connections
	Tools         *Tools
	Chats         *Chats
	Clock         platform.Clock
	MaxConcurrent int // runs at once in this process (FINUANCE_AI_MAX_CONCURRENT)
	Metrics       *Metrics
	Tracer        trace.TracerProvider
	Logger        *slog.Logger
}

// Runner runs questions: one at a time per user, at most MaxConcurrent per process.
type Runner struct {
	cfg    RunnerConfig
	tracer trace.Tracer
	sem    chan struct{}

	mu     sync.Mutex
	active map[uuid.UUID]context.CancelCauseFunc
}

// NewRunner returns a runner.
func NewRunner(cfg RunnerConfig) *Runner {
	if cfg.Metrics == nil {
		cfg.Metrics = NewMetrics(nil)
	}
	if cfg.Tracer == nil {
		cfg.Tracer = noop.NewTracerProvider()
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.DiscardHandler)
	}
	if cfg.MaxConcurrent < 1 {
		cfg.MaxConcurrent = 1
	}
	return &Runner{cfg: cfg, tracer: cfg.Tracer.Tracer("finuance/assistant"), sem: make(chan struct{}, cfg.MaxConcurrent),
		active: map[uuid.UUID]context.CancelCauseFunc{}}
}

// Run is a validated question with its reserved slot. Execute or Release it.
type Run struct {
	r        *Runner
	userID   uuid.UUID
	req      Request
	chat     Chat
	history  []llm.Message
	conn     *Connection
	model    ModelInfo
	images   []llm.Image // the question's photos
	released sync.Once
}

// Prepare checks a question and reserves the user's run slot.
func (r *Runner) Prepare(ctx context.Context, userID uuid.UUID, req Request) (*Run, error) {
	if !r.cfg.Connections.Enabled() {
		return nil, ErrDisabled
	}
	req.Text = strings.TrimSpace(req.Text)
	ve := &platform.ValidationError{}
	switch {
	case req.Text == "":
		ve.Add("text", "is empty")
	case utf8.RuneCountInString(req.Text) > MaxQuestion:
		ve.Add("text", fmt.Sprintf("is longer than %d characters", MaxQuestion))
	case strings.ContainsRune(req.Text, 0):
		ve.Add("text", "contains a NUL character")
	}
	if req.Mode == "" {
		req.Mode = ModeAuto
	}
	if req.Mode != ModeAuto && req.Mode != ModeTools && req.Mode != ModePlan {
		ve.Add("settings.mode", "must be auto, tools or plan")
	}
	if req.Think == "" {
		req.Think = llm.ThinkAuto
	}
	switch req.Think {
	case llm.ThinkOff, llm.ThinkAuto, llm.ThinkLow, llm.ThinkMedium, llm.ThinkHigh:
	default:
		ve.Add("settings.think", "must be off, auto, low, medium or high")
	}
	if req.MaxRows == 0 {
		req.MaxRows = MaxRowsLimit
	}
	if req.MaxRows < 1 || req.MaxRows > MaxRowsLimit {
		ve.Add("settings.maxRows", fmt.Sprintf("must be 1 to %d", MaxRowsLimit))
	}
	if t := req.Temperature; t != nil && (*t < 0 || *t > 2) {
		ve.Add("settings.temperature", "must be between 0 and 2")
	}
	if req.Toolset != "" && req.Toolset != ToolsetFull && req.Toolset != ToolsetCompact {
		ve.Add("settings.toolset", "must be full or compact")
	}
	req.AttachmentIDs = uniqueIDs(req.AttachmentIDs)
	if len(req.AttachmentIDs) > MaxAttachments {
		ve.Add("attachmentIds", fmt.Sprintf("at most %d photos per question", MaxAttachments))
	}
	if req.View {
		req.Context.ToolFilter = nil
		if err := query.Validate(userID, req.Context, nil); err != nil {
			return nil, err
		}
	}
	if err := ve.Err(); err != nil {
		return nil, err
	}
	if req.TokensPerDay > 0 {
		used, err := r.cfg.Chats.TokensToday(ctx, userID)
		if err != nil {
			return nil, err
		}
		if used >= req.TokensPerDay {
			return nil, &instance.LimitError{Limit: "assistantTokensPerDay", Value: req.TokensPerDay}
		}
	}
	chat, msgs, err := r.cfg.Chats.Get(ctx, userID, req.ChatID)
	if err != nil {
		return nil, err
	}
	connName, model := req.Connection, req.Model
	if connName == "" {
		connName = chat.Connection
		if model == "" {
			model = chat.Model
		}
	}
	if _, err := r.cfg.Connections.Get(connName); err != nil && req.Connection == "" {
		connName, model = "", "" // the chat's connection was removed from the config
	}
	conn, info, err := r.cfg.Connections.Resolve(ctx, connName, model)
	if err != nil {
		return nil, err
	}
	if conn.Cloud {
		s, err := r.cfg.Chats.Settings(ctx, userID)
		if err != nil {
			return nil, err
		}
		if !s.Consented(conn.Name) {
			return nil, ErrConsent
		}
	}
	var imgs []llm.Image
	if len(req.AttachmentIDs) > 0 {
		if !info.Caps.Vision {
			return nil, fieldError("attachmentIds", fmt.Sprintf("%s doesn't read images; pick a model that does", info.ID))
		}
		if imgs, err = r.cfg.Chats.unsentImages(ctx, userID, req.ChatID, req.AttachmentIDs); err != nil {
			return nil, err
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.active[userID]; ok {
		return nil, ErrRunning
	}
	select {
	case r.sem <- struct{}{}:
	default:
		return nil, ErrBusy
	}
	r.active[userID] = nil
	return &Run{r: r, userID: userID, req: req, chat: chat, history: historyOf(msgs), conn: conn, model: info, images: imgs}, nil
}

// Release gives the slot back (Execute does it itself).
func (run *Run) Release() {
	run.released.Do(func() {
		run.r.mu.Lock()
		delete(run.r.active, run.userID)
		run.r.mu.Unlock()
		<-run.r.sem
	})
}

// Stop cancels the user's run; false when none is running.
func (r *Runner) Stop(userID uuid.UUID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	cancel, ok := r.active[userID]
	if ok && cancel != nil {
		cancel(errStopRequested)
	}
	return ok
}

// turn is the state of the answer being written.
type turn struct {
	msgs      []Message // assistant and tool messages before the final one
	steps     []Step
	reasoning strings.Builder // the current round's
	content   strings.Builder // the current round's answer text
	sent      int
	rounds    int
	usage     llm.Usage
	cache     map[string]Outcome // the loop guard: a repeated call reuses its result
	tools     []string
}

func (t *turn) reset() {
	t.msgs, t.steps, t.cache, t.tools = nil, nil, map[string]Outcome{}, nil
	t.reasoning.Reset()
	t.content.Reset()
}

// Execute runs the question, sending events as they happen. Failures are events too; the turn is
// stored whatever happens (a failed turn with its error, which later requests leave out).
func (run *Run) Execute(ctx context.Context, emit func(Event)) {
	defer run.Release()
	r, req := run.r, run.req
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	r.mu.Lock()
	r.active[run.userID] = cancel
	r.mu.Unlock()

	t0 := r.cfg.Clock.Now()
	ctx, span := r.tracer.Start(ctx, "assistant.run", trace.WithAttributes(attribute.String("assistant.connection", run.conn.Name),
		attribute.String("assistant.model", run.model.ID)))
	defer span.End()

	store := context.WithoutCancel(ctx)
	question := Message{Role: llm.RoleUser, Content: req.Text}
	seq, err := r.cfg.Chats.Append(store, run.userID, req.ChatID, []Message{question},
		ChatUpdate{Title: req.Text, Connection: run.conn.Name, Model: run.model.ID, NoTools: run.chat.NoTools})
	if err == nil && len(req.AttachmentIDs) > 0 {
		err = r.cfg.Chats.markSent(store, run.userID, req.ChatID, req.AttachmentIDs, seq)
	}
	if err != nil {
		run.fail(ctx, emit, span, "internal", err, ModeTools, t0, nil)
		return
	}
	// the photos go with this question only; later questions read the answer, not the image again
	history := append(append([]llm.Message{}, run.history...), llm.Message{Role: llm.RoleUser, Content: req.Text, Images: run.images})

	sc := Scope{Context: query.Context{}, View: req.View, MaxRows: req.MaxRows, Mask: run.conn.Cloud}
	if req.View {
		sc.Context = req.Context
	}
	facts, err := r.cfg.Tools.Facts(ctx, run.userID, sc)
	if err != nil {
		run.fail(ctx, emit, span, "internal", err, ModeTools, t0, nil)
		return
	}
	sc.MultiAccount = facts != nil && facts.Accounts > 1

	mode := req.Mode
	if mode == ModeAuto {
		mode = ModeTools
		if run.chat.NoTools || !run.model.Caps.Tools {
			mode = ModePlan
		}
	}
	toolset := req.Toolset
	t := &turn{cache: map[string]Outcome{}}
	fallback := false
	var final Message
	if mode == ModeTools {
		final, err = run.runTools(ctx, emit, sc, facts, toolset, history, t)
		if err != nil && req.Mode == ModeAuto && len(t.steps) == 0 && ctx.Err() == nil && llm.ToolsMayBeRejected(err) {
			// legacy aiSend: the server refused tools before any lookup → plan mode for this chat
			fallback, mode = true, ModePlan
			emit(Event{"mode", ModeEvent{Mode: ModePlan, Reason: errText(err)}})
			t.reset()
			final, err = run.runPlan(ctx, emit, sc, facts, toolset, history, t)
			if err == nil {
				// remembered only when plan mode worked: a server that is simply down (500 to
				// both) must not switch the chat for good
				run.chat.NoTools = true
				r.cfg.Connections.LearnNoTools(run.conn.Name, run.model.ID, run.model.Caps)
			}
		}
	} else {
		final, err = run.runPlan(ctx, emit, sc, facts, toolset, history, t)
	}
	if err != nil && context.Cause(ctx) != nil && ctx.Err() != nil {
		// stopped (POST …/stop or the browser went away): keep what was written (legacy)
		final = Message{Role: llm.RoleAssistant, Content: appendNote(t.content.String(), stoppedNote), Reasoning: t.reasoning.String()}
		emit(Event{"content", DeltaEvent{Delta: strings.TrimPrefix(final.Content, t.content.String())}})
		run.finish(ctx, emit, span, final, t, mode, fallback, true, t0)
		return
	}
	if err != nil {
		run.fail(ctx, emit, span, kindOf(err), err, mode, t0, t)
		return
	}
	run.finish(ctx, emit, span, final, t, mode, fallback, false, t0)
}

func kindOf(err error) string {
	if k := llm.KindOf(err); k != "" {
		return string(k)
	}
	return "internal"
}

// errText is the message a user sees for an error.
func errText(err error) string {
	var le *llm.Error
	if errors.As(err, &le) {
		return le.Message
	}
	return "Something went wrong while answering. Try again."
}

func (run *Run) meta(t *turn, mode string, fallback, stopped bool, t0 time.Time) Meta {
	m := Meta{DurationMs: run.r.cfg.Clock.Now().Sub(t0).Milliseconds(), Connection: run.conn.Name, Model: run.model.ID, Mode: mode,
		Fallback: fallback, Stopped: stopped, Scope: "all"}
	if run.req.View {
		m.Scope, m.AccountIDs = "view", run.req.Context.AccountIDs
	}
	if t != nil {
		m.ApproxTokensSent, m.Lookups, m.Rounds = t.sent, len(t.steps), t.rounds
		m.InputTokens, m.OutputTokens = t.usage.Input, t.usage.Output
	}
	return m
}

func (run *Run) finish(ctx context.Context, emit func(Event), span trace.Span, final Message, t *turn, mode string, fallback, stopped bool, t0 time.Time) {
	m := run.meta(t, mode, fallback, stopped, t0)
	final.Steps, final.Meta = t.steps, &m
	if final.Steps == nil {
		final.Steps = []Step{}
	}
	seq, err := run.store(ctx, append(t.msgs, final), run.usage(t, final))
	if err != nil {
		run.fail(ctx, emit, span, "internal", err, mode, t0, t)
		return
	}
	emit(Event{"done", DoneEvent{MessageSeq: seq, Meta: m}})
	result := "ok"
	switch {
	case stopped:
		result = "stopped"
	case fallback:
		result = "fallback_plan"
	}
	run.observe(span, mode, result, "", t, t0)
}

func (run *Run) fail(ctx context.Context, emit func(Event), span trace.Span, code string, err error, mode string, t0 time.Time, t *turn) {
	te := TurnError{Code: code, Message: errText(err)}
	m := run.meta(t, mode, false, false, t0)
	final := Message{Role: llm.RoleAssistant, Error: &te, Meta: &m, Steps: []Step{}}
	var msgs []Message
	if t != nil {
		final.Content, final.Reasoning, final.Steps = t.content.String(), t.reasoning.String(), t.steps
		if final.Steps == nil {
			final.Steps = []Step{}
		}
		msgs = t.msgs
	}
	seq, serr := run.store(ctx, append(msgs, final), run.usage(t, final))
	if serr != nil {
		seq = 0
	}
	emit(Event{"error", ErrorEvent{TurnError: te, MessageSeq: seq}})
	span.SetStatus(codes.Error, code)
	if code == "internal" {
		run.r.cfg.Logger.ErrorContext(ctx, "assistant run failed", "error", err)
	}
	run.observe(span, mode, "error", code, t, t0)
}

func (run *Run) store(ctx context.Context, msgs []Message, use *RunUsage) (int, error) {
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), persistGrace)
	defer cancel()
	return run.r.cfg.Chats.Append(sctx, run.userID, run.req.ChatID, msgs,
		ChatUpdate{Connection: run.conn.Name, Model: run.model.ID, NoTools: run.chat.NoTools, Usage: use})
}

// usage is the run's tokens: what the server counted, else the runner's estimate of what was sent
// plus the answer's length / 4.
func (run *Run) usage(t *turn, final Message) *RunUsage {
	u := &RunUsage{Digest: run.req.Digest}
	if t == nil {
		return u
	}
	if n := t.usage.Input + t.usage.Output; n > 0 {
		u.Tokens = int64(n)
		return u
	}
	out := len(final.Content) + len(final.Reasoning)
	for i := range t.msgs {
		if t.msgs[i].Role == llm.RoleAssistant {
			out += len(t.msgs[i].Content) + len(t.msgs[i].Reasoning)
		}
	}
	u.Tokens = int64(t.sent + out/4)
	return u
}

// observe records the run's metrics and its one log line (no prompt, answer or tool data).
func (run *Run) observe(span trace.Span, mode, result, errKind string, t *turn, t0 time.Time) {
	m := run.r.cfg.Metrics
	d := run.r.cfg.Clock.Now().Sub(t0)
	m.Runs.WithLabelValues(mode, result).Inc()
	m.Duration.WithLabelValues(mode).Observe(d.Seconds())
	rounds, sent, tools := 0, 0, []string(nil)
	if t != nil {
		rounds, sent, tools = t.rounds, t.sent, t.tools
	}
	m.Rounds.Observe(float64(rounds))
	m.Tokens.Observe(float64(sent))
	span.SetAttributes(attribute.String("assistant.mode", mode), attribute.String("assistant.result", result), attribute.Int("assistant.rounds", rounds))
	h := sha256.Sum256(run.userID[:])
	run.r.cfg.Logger.Info("assistant run", "user", hex.EncodeToString(h[:6]), "connection", run.conn.Name, "model", run.model.ID,
		"mode", mode, "result", result, "error_kind", errKind, "rounds", rounds, "tools", tools, "approx_tokens", sent,
		"duration_ms", d.Milliseconds())
}

// budget is the prompt size the history is trimmed to: the model's context minus room for the
// answer.
func (run *Run) budget() int {
	c := run.model.Caps.ContextTokens
	if c <= 0 {
		return 0
	}
	return c - max(1024, c/4)
}

// noThink: thinking off for a Qwen 3 model is the /no_think switch in the question.
func (run *Run) noThink() bool {
	return run.req.Think == llm.ThinkOff && run.model.Caps.Reasoning == llm.ReasonSwitch
}

func (run *Run) temperature() *float64 {
	if run.req.Temperature != nil {
		return run.req.Temperature
	}
	t := 0.2
	return &t
}

// complete sends one model request, streaming its text to emit. A server that rejects the
// temperature or the reasoning setting (some reasoning models do) is asked again without it, once.
func (run *Run) complete(ctx context.Context, emit func(Event), t *turn, req llm.Request, show bool) (llm.Result, error) {
	ctx, span := run.r.tracer.Start(ctx, "assistant.model_request", trace.WithAttributes(attribute.Int("assistant.round", t.rounds+1)))
	defer span.End()
	req.Model, req.Caps, req.Thinking = run.model.ID, run.model.Caps, run.req.Think
	t.content.Reset()
	t.reasoning.Reset()
	streamed := false
	onDelta := func(d llm.Delta) {
		if d.Reasoning != "" {
			streamed = true
			t.reasoning.WriteString(d.Reasoning)
			emit(Event{"reasoning", DeltaEvent{Delta: d.Reasoning}})
		}
		if d.Content != "" && show {
			streamed = true
			t.content.WriteString(d.Content)
			emit(Event{"content", DeltaEvent{Delta: d.Content}})
		}
	}
	t.sent += estTokens(req.System, req.Messages)
	res, err := run.conn.Client.Chat(ctx, req, onDelta)
	if err != nil && !streamed && retryWithout(err, &req) {
		res, err = run.conn.Client.Chat(ctx, req, onDelta)
	}
	t.rounds++
	result := "ok"
	if err != nil {
		result = kindOf(err)
		span.SetStatus(codes.Error, result)
	}
	run.r.cfg.Metrics.Upstream.WithLabelValues(run.conn.Name, result).Inc()
	t.usage.Input += res.Usage.Input
	t.usage.Output += res.Usage.Output
	return res, err
}

var (
	tempRe   = regexp.MustCompile(`(?i)temperature`)
	effortRe = regexp.MustCompile(`(?i)reasoning[_ ]effort|thinking`)
)

// retryWithout drops a parameter the server named in a 400 answer; false when there is nothing
// to drop.
func retryWithout(err error, req *llm.Request) bool {
	var le *llm.Error
	if !errors.As(err, &le) || le.Status != 400 {
		return false
	}
	switch {
	case req.Temperature != nil && tempRe.MatchString(le.Message):
		req.Temperature = nil
	case req.Caps.Reasoning != llm.ReasonNone && effortRe.MatchString(le.Message):
		req.Caps.Reasoning = llm.ReasonNone
	default:
		return false
	}
	return true
}

// runTools is legacy runTools: up to six rounds of tool calls, then the answer.
func (run *Run) runTools(ctx context.Context, emit func(Event), sc Scope, facts *Facts, toolset string, history []llm.Message, t *turn) (Message, error) {
	if toolset == "" {
		toolset = ToolsetFull
	}
	system := SystemPrompt(UserTime(run.r.cfg.Clock.Now(), run.req.TimeZone), facts, sc, toolset)
	defs := ToolDefs(toolset)
	var conv []llm.Message // this turn's assistant and tool messages
	for range maxRounds {
		msgs := trimHistory(append(append([]llm.Message{}, history...), conv...), false, run.noThink(), system, run.budget())
		res, err := run.complete(ctx, emit, t, llm.Request{System: system, Messages: msgs, Tools: defs, Temperature: run.temperature()}, true)
		if err != nil {
			return Message{}, err
		}
		if len(res.ToolCalls) == 0 {
			return Message{Role: llm.RoleAssistant, Content: res.Content, Reasoning: res.Reasoning}, nil
		}
		am := Message{Role: llm.RoleAssistant, Content: res.Content, Reasoning: res.Reasoning, ToolCalls: res.ToolCalls, Replay: res.Replay}
		t.msgs = append(t.msgs, am)
		conv = append(conv, llm.Message{Role: llm.RoleAssistant, Content: res.Content, ToolCalls: res.ToolCalls, Replay: res.Replay})
		if res.Content != "" {
			emit(Event{"content", DeltaEvent{Delta: "\n\n"}})
		}
		t.content.Reset() // stored with the message above; a stop now must not repeat it
		t.reasoning.Reset()
		for _, c := range res.ToolCalls {
			out, err := run.step(ctx, emit, sc, t, c.Name, c.Arguments)
			if err != nil {
				return Message{}, err
			}
			t.msgs = append(t.msgs, Message{Role: llm.RoleTool, ToolCallID: c.ID, Content: out.JSON})
			conv = append(conv, llm.Message{Role: llm.RoleTool, ToolCallID: c.ID, Content: out.JSON})
		}
	}
	t.content.Reset()
	t.reasoning.Reset()
	emit(Event{"content", DeltaEvent{Delta: roundsNote}})
	return Message{Role: llm.RoleAssistant, Content: roundsNote}, nil
}

// step executes one tool call and reports it.
func (run *Run) step(ctx context.Context, emit func(Event), sc Scope, t *turn, name, rawArgs string) (Outcome, error) {
	name, rawArgs = asProposal(name, rawArgs)
	ctx, span := run.r.tracer.Start(ctx, "assistant.tool", trace.WithAttributes(attribute.String("assistant.tool", knownTool(name))))
	defer span.End()
	args := json.RawMessage(rawArgs)
	if !json.Valid(args) {
		args = json.RawMessage(`{}`)
	}
	key := name + "\x00" + compactJSON(args)
	out, repeated := t.cache[key]
	result := "repeated"
	if !repeated {
		var err error
		out, err = run.r.cfg.Tools.Run(ctx, run.userID, sc, name, rawArgs)
		if err != nil {
			run.r.cfg.Metrics.ToolCalls.WithLabelValues(knownTool(name), "error").Inc()
			return Outcome{}, err
		}
		t.cache[key] = out
		result = "ok"
		if knownTool(name) == "unknown" {
			result = "unknown"
		}
	}
	run.r.cfg.Metrics.ToolCalls.WithLabelValues(knownTool(name), result).Inc()
	st := Step{Tool: name, Args: args, Summary: out.Summary, Result: cutBytes(out.JSON, MaxStepResult), Matched: out.Matched,
		Truncated: len(out.JSON) > MaxStepResult, ToolFilter: stepFilter(out.Filter), Proposal: out.Proposal}
	t.steps = append(t.steps, st)
	t.tools = append(t.tools, knownTool(name))
	emit(Event{"step", StepEvent{Index: len(t.steps) - 1, Step: st}})
	return out, nil
}

func compactJSON(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	b, _ := json.Marshal(v) // map keys sorted: {"a":1,"b":2} equals {"b":2,"a":1}
	return string(b)
}

// stepAmountRe is the API's ToolFilter amount pattern; a model's "1e3" or "lots" can't be sent
// back as a filter, so such a bound is left out of the step's filter.
var stepAmountRe = regexp.MustCompile(`^-?[0-9]{1,15}([.][0-9]{1,6})?$`)

func stepFilter(f *query.ToolFilter) *StepFilter {
	if f == nil {
		return nil
	}
	amount := func(v *string) *string {
		if v == nil || !stepAmountRe.MatchString(*v) {
			return nil
		}
		return v
	}
	out := &StepFilter{Query: clip(f.Query, 500), Payee: clip(f.Payee, 500), Category: clip(f.Category, 500), Group: clip(f.Group, 500),
		MinAmount: amount(f.MinAmount), MaxAmount: amount(f.MaxAmount), Label: clip(f.Label, 200)}
	if f.Type == "expense" || f.Type == "income" {
		out.Type = f.Type
	}
	if f.DateFrom != nil {
		out.DateFrom = f.DateFrom.String()
	}
	if f.DateTo != nil {
		out.DateTo = f.DateTo.String()
	}
	return out
}

var jsonObjectRe = regexp.MustCompile(`(?s)\{.*\}`)

// runPlan is legacy runPlan for models without tool calling: ask for the calls as JSON, run them,
// then ask for the answer with the results in the question.
func (run *Run) runPlan(ctx context.Context, emit func(Event), sc Scope, facts *Facts, toolset string, history []llm.Message, t *turn) (Message, error) {
	if toolset == "" {
		toolset = ToolsetCompact
	}
	system := SystemPrompt(UserTime(run.r.cfg.Clock.Now(), run.req.TimeZone), facts, sc, toolset)
	hist := trimHistory(history, true, run.noThink(), system, run.budget())
	zero := 0.0
	plan, err := run.complete(ctx, emit, t, llm.Request{System: system + planInstructions(ToolDefs(toolset)), Messages: hist, Temperature: &zero}, false)
	if err != nil {
		return Message{}, err
	}
	planReasoning := t.reasoning.String()
	calls := plan.ToolCalls
	if len(calls) == 0 {
		if m := jsonObjectRe.FindString(plan.Content); m != "" {
			var o struct {
				Calls []struct {
					Name      string          `json:"name"`
					Arguments json.RawMessage `json:"arguments"`
				} `json:"calls"`
			}
			if json.Unmarshal([]byte(m), &o) == nil {
				for _, c := range o.Calls {
					calls = append(calls, llm.ToolCall{Name: c.Name, Arguments: planArgs(c.Arguments)})
				}
			}
		}
	}
	var results []string
	for _, c := range calls[:min(len(calls), maxPlanCalls)] {
		out, err := run.step(ctx, emit, sc, t, c.Name, c.Arguments)
		if err != nil {
			return Message{}, err
		}
		args := c.Arguments
		if !json.Valid([]byte(args)) {
			args = "{}"
		}
		results = append(results, `{"tool":`+jsString(c.Name)+`,"arguments":`+args+`,"result":`+out.JSON+`}`)
	}
	ans := append([]llm.Message{}, hist...)
	if len(results) > 0 {
		q := "Data looked up locally by the app for my last question:\n[" + strings.Join(results, ",") + "]\nAnswer my last question using only this data."
		if run.noThink() {
			q += " /no_think"
		}
		ans = append(ans, llm.Message{Role: llm.RoleUser, Content: q})
	}
	res, err := run.complete(ctx, emit, t, llm.Request{System: system, Messages: ans, Temperature: run.temperature()}, true)
	if err != nil {
		return Message{}, err
	}
	reasoning := planReasoning + res.Reasoning
	t.reasoning.Reset()
	t.reasoning.WriteString(reasoning)
	return Message{Role: llm.RoleAssistant, Content: res.Content, Reasoning: reasoning}, nil
}

// planArgs accepts arguments written as an object or as a JSON string holding one.
func planArgs(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	if len(raw) == 0 || string(raw) == "null" {
		return "{}"
	}
	return string(raw)
}
