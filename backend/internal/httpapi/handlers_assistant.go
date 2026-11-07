package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/assistant"
	"github.com/x2db736ac85h/finuance/backend/internal/config"
	"github.com/x2db736ac85h/finuance/backend/internal/httpapi/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/images"
	"github.com/x2db736ac85h/finuance/backend/internal/instance"
	"github.com/x2db736ac85h/finuance/backend/internal/jobs"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/llm"
	"github.com/x2db736ac85h/finuance/backend/internal/observability"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

// heartbeat is how often an idle stream gets a comment line, so proxies keep it open.
const heartbeat = 15 * time.Second

// conns returns the configured connections (nil when the assistant isn't wired, in tests).
func (a *api) conns() *assistant.Connections {
	if a.ai == nil {
		return nil
	}
	return a.ai.Connections
}

// maxRows is the most rows one lookup may return: the legacy cap, or less in the demo (PLAN 19.1).
func (a *api) maxRows() int {
	if a.cfg.DemoMode {
		return config.DemoMaxRows
	}
	return assistant.MaxRowsLimit
}

// AssistantStatus lists the connections and their chat models.
func (a *api) AssistantStatus(ctx context.Context, req gen.AssistantStatusRequestObject) (gen.AssistantStatusResponseObject, error) {
	out := gen.AssistantStatus200JSONResponse{Enabled: a.conns().Enabled(), Connections: []gen.AssistantConnection{}, MaxRowsLimit: a.maxRows()}
	for _, st := range a.conns().Status(ctx, req.Params.Refresh != nil && *req.Params.Refresh) {
		c := gen.AssistantConnection{Name: st.Name, Label: st.Label, Api: gen.AssistantConnectionApi(st.API), Cloud: st.Cloud,
			Reachable: st.Reachable, Models: make([]gen.AssistantModel, 0, len(st.Models)), Error: assistantError(st.Err)}
		if st.DefaultModel != "" {
			c.DefaultModel = &st.DefaultModel
		}
		for _, m := range st.Models {
			c.Models = append(c.Models, gen.AssistantModel{Id: m.ID, Tools: m.Caps.Tools,
				Reasoning: gen.AssistantModelReasoning(m.Caps.Reasoning), ContextTokens: m.Caps.ContextTokens, Vision: m.Caps.Vision})
		}
		out.Connections = append(out.Connections, c)
	}
	return out, nil
}

func assistantError(e *llm.Error) *gen.AssistantError {
	if e == nil {
		return nil
	}
	return &gen.AssistantError{Kind: gen.AssistantErrorKind(e.Kind), Message: e.Message}
}

// errAssistantOff answers the assistant routes of an instance without connections.
var errAssistantOff = fmt.Errorf("%w: no assistant connection is configured", assistant.ErrUnavailable)

func (a *api) chats() (*assistant.Chats, error) {
	if a.ai == nil {
		return nil, errAssistantOff
	}
	return a.ai.Chats, nil
}

// GetAssistantSettings returns the retention and cloud consent.
func (a *api) GetAssistantSettings(ctx context.Context, _ gen.GetAssistantSettingsRequestObject) (gen.GetAssistantSettingsResponseObject, error) {
	c, err := a.chats()
	if err != nil {
		return nil, err
	}
	s, err := c.Settings(ctx, identity(ctx).User.ID)
	if err != nil {
		return nil, err
	}
	return gen.GetAssistantSettings200JSONResponse(settingsDTO(s)), nil
}

// PutAssistantSettings saves them.
func (a *api) PutAssistantSettings(ctx context.Context, req gen.PutAssistantSettingsRequestObject) (gen.PutAssistantSettingsResponseObject, error) {
	c, err := a.chats()
	if err != nil {
		return nil, err
	}
	in := assistant.Settings{RetentionDays: req.Body.RetentionDays, CloudConsent: req.Body.CloudConsent}
	if req.Body.AutoApply != nil {
		in.AutoApply = make([]string, len(*req.Body.AutoApply))
		for i, x := range *req.Body.AutoApply {
			in.AutoApply[i] = string(x)
		}
	}
	if d := req.Body.Digest; d != nil {
		in.Digest = &assistant.DigestSettings{Schedule: string(d.Schedule), Hour: d.Hour, Weekday: d.Weekday, TimeZone: d.TimeZone,
			Connection: d.Connection, Model: d.Model}
		if in.Digest.Schedule != assistant.DigestOff && !a.digestsOffered(ctx) {
			ve := &platform.ValidationError{}
			ve.Add("digest.schedule", "digests are switched off on this instance")
			return nil, ve
		}
	}
	s, err := c.SaveSettings(ctx, identity(ctx).User.ID, in)
	if err != nil {
		return nil, err
	}
	return gen.PutAssistantSettings200JSONResponse(settingsDTO(s)), nil
}

func settingsDTO(s assistant.Settings) gen.AssistantSettings {
	auto := make([]gen.AssistantSettingsAutoApply, len(s.AutoApply))
	for i, x := range s.AutoApply {
		auto[i] = gen.AssistantSettingsAutoApply(x)
	}
	out := gen.AssistantSettings{RetentionDays: s.RetentionDays, CloudConsent: s.CloudConsent, AutoApply: &auto,
		DigestNextAt: s.DigestNextAt, TokensToday: &s.TokensToday}
	if d := s.Digest; d != nil {
		out.Digest = &gen.AssistantDigest{Schedule: gen.AssistantDigestSchedule(d.Schedule), Hour: d.Hour, Weekday: d.Weekday,
			TimeZone: d.TimeZone, Connection: d.Connection, Model: d.Model}
	}
	return out
}

// digestsOffered: the signed-in user may have digests (the instance switch, no demo).
func (a *api) digestsOffered(ctx context.Context) bool {
	return a.features(ctx).AssistantDigests && !a.cfg.DemoMode && !identity(ctx).User.IsDemo
}

// RequestAssistantDigest queues a digest now (ADR 0033).
func (a *api) RequestAssistantDigest(ctx context.Context, _ gen.RequestAssistantDigestRequestObject) (gen.RequestAssistantDigestResponseObject, error) {
	if !a.digestsOffered(ctx) {
		return nil, platform.ErrNotFound
	}
	if a.ai == nil || !a.ai.Connections.Enabled() {
		return nil, errAssistantOff
	}
	u := identity(ctx).User
	if limit := a.effectiveLimits(ctx, u).AssistantTokensPerDay; limit > 0 {
		used, err := a.ai.Chats.TokensToday(ctx, u.ID)
		if err != nil {
			return nil, err
		}
		if used >= limit {
			return nil, &instance.LimitError{Limit: "assistantTokensPerDay", Value: limit}
		}
	}
	if _, err := a.jobs.Insert(ctx, jobs.AssistantDigestArgs{UserID: u.ID, Manual: true}, nil); err != nil {
		return nil, err
	}
	return gen.RequestAssistantDigest202Response{}, nil
}

func chatDTO(c assistant.Chat) gen.AssistantChat {
	return gen.AssistantChat{Id: c.ID, Title: c.Title, Kind: gen.AssistantChatKind(c.Kind), Connection: c.Connection, Model: c.Model,
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt}
}

// ListAssistantChats lists the chats, newest first.
func (a *api) ListAssistantChats(ctx context.Context, _ gen.ListAssistantChatsRequestObject) (gen.ListAssistantChatsResponseObject, error) {
	c, err := a.chats()
	if err != nil {
		return nil, err
	}
	list, err := c.List(ctx, identity(ctx).User.ID)
	if err != nil {
		return nil, err
	}
	out := gen.ListAssistantChats200JSONResponse{Chats: make([]gen.AssistantChat, 0, len(list)), MaxChats: a.cfg.AIMaxChats}
	for i := range list {
		out.Chats = append(out.Chats, chatDTO(list[i]))
	}
	return out, nil
}

// CreateAssistantChat starts a chat.
func (a *api) CreateAssistantChat(ctx context.Context, _ gen.CreateAssistantChatRequestObject) (gen.CreateAssistantChatResponseObject, error) {
	c, err := a.chats()
	if err != nil {
		return nil, err
	}
	ch, err := c.Create(ctx, identity(ctx).User.ID)
	if err != nil {
		return nil, err
	}
	return gen.CreateAssistantChat201JSONResponse(chatDTO(ch)), nil
}

// GetAssistantChat returns a chat as turns: each question, and each answer with its lookups.
func (a *api) GetAssistantChat(ctx context.Context, req gen.GetAssistantChatRequestObject) (gen.GetAssistantChatResponseObject, error) {
	c, err := a.chats()
	if err != nil {
		return nil, err
	}
	ch, msgs, err := c.Get(ctx, identity(ctx).User.ID, req.Id)
	if err != nil {
		return nil, err
	}
	applied, err := a.ledger.AppliedProposals(ctx, identity(ctx).User.ID, req.Id)
	if err != nil {
		return nil, err
	}
	if applied == nil {
		applied = []string{}
	}
	out := gen.GetAssistantChat200JSONResponse{Id: ch.ID, Title: ch.Title, Kind: gen.AssistantChatKind(ch.Kind), Connection: ch.Connection, Model: ch.Model,
		CreatedAt: ch.CreatedAt, UpdatedAt: ch.UpdatedAt, NoTools: ch.NoTools, Turns: turnsOf(msgs), AppliedProposals: applied}
	return out, nil
}

// turnsOf groups messages into turns. An answer's text and reasoning are its assistant messages'
// joined (the text written before each lookup, then the answer), as the panel streamed them.
func turnsOf(msgs []assistant.Message) []gen.AssistantTurn {
	out := []gen.AssistantTurn{}
	for i := 0; i < len(msgs); {
		m := msgs[i]
		if m.Role == llm.RoleUser {
			t := gen.AssistantTurn{Role: gen.AssistantTurnRoleUser, Seq: m.Seq, Content: m.Content, CreatedAt: m.CreatedAt}
			if len(m.Attachments) > 0 {
				as := make([]gen.AssistantAttachment, len(m.Attachments))
				for i, a := range m.Attachments {
					as[i] = gen.AssistantAttachment{Id: a.ID, Width: a.Width, Height: a.Height}
				}
				t.Attachments = &as
			}
			out = append(out, t)
			i++
			continue
		}
		j := i
		var content, reasoning []string
		for j < len(msgs) && msgs[j].Role != llm.RoleUser {
			if x := msgs[j]; x.Role == llm.RoleAssistant {
				if x.Content != "" {
					content = append(content, x.Content)
				}
				if x.Reasoning != "" {
					reasoning = append(reasoning, x.Reasoning)
				}
			}
			j++
		}
		fin := msgs[j-1]
		t := gen.AssistantTurn{Role: gen.AssistantTurnRoleAssistant, Seq: fin.Seq, Content: strings.Join(content, "\n\n"), CreatedAt: fin.CreatedAt}
		if r := strings.Join(reasoning, "\n\n"); r != "" {
			t.Reasoning = &r
		}
		steps := make([]gen.AssistantStep, 0, len(fin.Steps))
		for _, s := range fin.Steps {
			steps = append(steps, stepDTO(s))
		}
		t.Steps = &steps
		if fin.Meta != nil {
			m := metaDTO(*fin.Meta)
			t.Meta = &m
		} else {
			t.Error = &gen.AssistantTurnError{Code: "internal", Message: "The reply was interrupted."}
		}
		if fin.Error != nil {
			t.Error = &gen.AssistantTurnError{Code: fin.Error.Code, Message: fin.Error.Message}
		}
		out = append(out, t)
		i = j
	}
	return out
}

func stepDTO(s assistant.Step) gen.AssistantStep {
	var args map[string]any
	if json.Unmarshal(s.Args, &args) != nil || args == nil {
		args = map[string]any{}
	}
	out := gen.AssistantStep{Tool: s.Tool, Args: args, Summary: s.Summary, Result: s.Result, Matched: s.Matched}
	if s.Truncated {
		out.Truncated = &s.Truncated
	}
	out.ToolFilter = toolFilterDTO(s.ToolFilter)
	if p := s.Proposal; p != nil {
		out.Proposal = proposalDTO(p)
	}
	return out
}

// toolFilterDTO is a step's lookup filter in the API form; nil for none.
func toolFilterDTO(f *assistant.StepFilter) *gen.ToolFilter {
	if f == nil {
		return nil
	}
	tf := gen.ToolFilter{Label: &f.Label, MinAmount: f.MinAmount, MaxAmount: f.MaxAmount}
	for _, p := range []struct {
		v   string
		dst **string
	}{{f.Query, &tf.Query}, {f.Payee, &tf.Payee}, {f.Category, &tf.Category}, {f.Group, &tf.Group}} {
		if p.v != "" {
			v := p.v
			*p.dst = &v
		}
	}
	if f.Type != "" {
		t := gen.ToolFilterType(f.Type)
		tf.Type = &t
	}
	if f.DateFrom != "" {
		if d, err := platform.ParseDate(f.DateFrom); err == nil {
			tf.DateFrom = dateDTO(&d)
		}
	}
	if f.DateTo != "" {
		if d, err := platform.ParseDate(f.DateTo); err == nil {
			tf.DateTo = dateDTO(&d)
		}
	}
	return &tf
}

func proposalGoalDTO(g *assistant.ProposalGoal) *gen.ProposalGoal {
	if g == nil {
		return nil
	}
	out := &gen.ProposalGoal{Id: g.ID, Name: g.Name, Kind: gen.ProposalGoalKind(g.Kind), TargetMinor: g.TargetMinor, Currency: g.Currency,
		AccountId: g.AccountID, CategoryId: g.CategoryID, BaselineMonths: g.BaselineMonths}
	if d, err := platform.ParseDate(g.Deadline); err == nil {
		out.Deadline = *dateDTO(&d)
	}
	if g.AccountName != "" {
		out.AccountName = &g.AccountName
	}
	if g.CategoryName != "" {
		out.CategoryName = &g.CategoryName
	}
	if g.ReducePct > 0 {
		out.ReducePct = &g.ReducePct
	}
	if g.Status != "" {
		st := gen.ProposalGoalStatus(g.Status)
		out.Status = &st
	}
	if g.Version > 0 {
		out.Version = &g.Version
	}
	return out
}

func proposalDTO(p *assistant.Proposal) *gen.ActionProposal {
	out := &gen.ActionProposal{Key: p.Key(), Action: gen.ActionProposalAction(p.Action), Matched: p.Matched, CategoryId: p.CategoryID, GroupId: p.GroupID,
		Sample: make([]gen.ProposalRow, len(p.Sample))}
	for _, f := range []struct {
		v   string
		dst **string
	}{{p.Reason, &out.Reason}, {p.CategoryName, &out.CategoryName}, {p.GroupName, &out.GroupName}, {p.RuleName, &out.RuleName},
		{p.AccountName, &out.AccountName}, {p.Payee, &out.Payee}, {p.Note, &out.Note}, {p.SharedGroupName, &out.SharedGroupName}} {
		if f.v != "" {
			v := f.v
			*f.dst = &v
		}
	}
	if len(p.IDs) > 0 {
		out.Ids = &p.IDs
	}
	// Phase 20 proposals (ADR 0030)
	out.AccountId, out.SharedGroupId = p.AccountID, p.SharedGroupID
	if p.RemoveBudget {
		out.RemoveBudget = &p.RemoveBudget
	}
	if p.BudgetMinor != nil {
		out.Budget = &gen.Money{AmountMinor: *p.BudgetMinor, Currency: p.Currency}
	}
	if p.BudgetBeforeMinor != nil {
		out.BudgetBefore = &gen.Money{AmountMinor: *p.BudgetBeforeMinor, Currency: p.Currency}
	}
	out.ExpenseId, out.PaidBy = p.ExpenseID, p.PaidBy
	if p.ExpenseCategory != "" {
		out.ExpenseCategory = &p.ExpenseCategory
	}
	if sp := p.Split; sp != nil {
		ms := make([]gen.ProposalSplitMember, len(sp.Members))
		for i, m := range sp.Members {
			ms[i] = gen.ProposalSplitMember{MemberId: m.MemberID, Name: m.Name}
			if m.Weight > 0 {
				w := m.Weight
				ms[i].Weight = &w
			}
			if m.AmountMinor != 0 {
				v := m.AmountMinor
				ms[i].AmountMinor = &v
			}
		}
		out.Split = &gen.ProposalSplit{Mode: gen.ProposalSplitMode(sp.Mode), Members: ms}
	}
	if p.ReceiptLine != "" {
		out.ReceiptLine = &p.ReceiptLine
	}
	if p.Action == assistant.ActionAddExpense || p.Action == assistant.ActionSplitExpense || p.Action == assistant.ActionAttachReceipt {
		out.Amount = &gen.Money{AmountMinor: p.AmountMinor, Currency: p.Currency}
		if d, err := platform.ParseDate(p.Date); err == nil {
			out.Date = dateDTO(&d)
		}
	}
	out.Goal, out.GoalBefore = proposalGoalDTO(p.Goal), proposalGoalDTO(p.GoalBefore)
	if m := p.Match; m != nil {
		rm := gen.RuleMatch{}
		if m.Search != "" {
			rm.Search = &m.Search
		}
		if len(m.Filters) > 0 {
			fs := make([]gen.ColumnFilter, len(m.Filters))
			for i, f := range m.Filters {
				fs[i] = gen.ColumnFilter{Column: gen.FilterColumn(f.Column), Cond: &f.Cond}
			}
			rm.Filters = &fs
		}
		out.Match = &rm
	}
	for i, r := range p.Sample {
		out.Sample[i] = gen.ProposalRow{Id: r.ID, Payee: r.Payee, Amount: gen.Money{AmountMinor: r.AmountMinor, Currency: r.Currency}}
		if d, err := platform.ParseDate(r.Date); err == nil {
			out.Sample[i].BookedOn = *dateDTO(&d)
		}
		if r.Category != "" {
			out.Sample[i].Category = &r.Category
		}
	}
	out.ToolFilter = toolFilterDTO(p.Filter)
	return out
}

// auditAppliedAction records an applied assistant proposal in the activity log (PLAN 15.7/17.6,
// ADR 0019): which chat, which proposal, what kind of change and how many rows. Never names,
// filters or any other content. A request without X-Assistant-Chat is an ordinary edit.
func (a *api) auditAppliedAction(ctx context.Context, chat *gen.AssistantChatHeader, proposal *gen.AssistantProposalHeader, action string, rows int) {
	if chat == nil {
		return
	}
	meta := map[string]any{"action": action, "rows": rows}
	if proposal != nil {
		meta["proposal"] = *proposal
	}
	a.auditMe(ctx, ledger.AuditAssistantApplied, chat.String(), meta)
}

func metaDTO(m assistant.Meta) gen.AssistantMeta {
	out := gen.AssistantMeta{DurationMs: m.DurationMs, ApproxTokensSent: m.ApproxTokensSent, Lookups: m.Lookups, Rounds: m.Rounds,
		Connection: m.Connection, Model: m.Model, Mode: gen.AssistantMetaMode(m.Mode), Scope: gen.AssistantMetaScope(m.Scope)}
	if m.Fallback {
		out.Fallback = &m.Fallback
	}
	if m.Stopped {
		out.Stopped = &m.Stopped
	}
	if len(m.AccountIDs) > 0 {
		ids := m.AccountIDs
		out.AccountIds = &ids
	}
	if m.InputTokens > 0 {
		out.InputTokens = &m.InputTokens
	}
	if m.OutputTokens > 0 {
		out.OutputTokens = &m.OutputTokens
	}
	return out
}

// DeleteAssistantChat deletes a chat.
func (a *api) DeleteAssistantChat(ctx context.Context, req gen.DeleteAssistantChatRequestObject) (gen.DeleteAssistantChatResponseObject, error) {
	c, err := a.chats()
	if err != nil {
		return nil, err
	}
	if err := c.Delete(ctx, identity(ctx).User.ID, req.Id); err != nil {
		return nil, err
	}
	return gen.DeleteAssistantChat204Response{}, nil
}

// StopAssistant cancels the user's run.
func (a *api) StopAssistant(ctx context.Context, _ gen.StopAssistantRequestObject) (gen.StopAssistantResponseObject, error) {
	if a.ai != nil {
		a.ai.Runner.Stop(identity(ctx).User.ID)
	}
	return gen.StopAssistant204Response{}, nil
}

// SendAssistantMessage checks the question, then streams the run as server-sent events.
func (a *api) SendAssistantMessage(ctx context.Context, req gen.SendAssistantMessageRequestObject) (gen.SendAssistantMessageResponseObject, error) {
	if a.ai == nil || !a.ai.Connections.Enabled() {
		return nil, errAssistantOff
	}
	u := identity(ctx).User.ID
	if err := a.limits.assistantFor(a.effectiveLimits(ctx, identity(ctx).User).AssistantPerHour).Allow(u.String()); err != nil {
		return nil, err
	}
	if a.cfg.DemoMode {
		if err := a.limits.demoAsk.Allow(u.String()); err != nil {
			return nil, err
		}
	}
	b := req.Body
	r := assistant.Request{ChatID: req.Id, Text: b.Text, TimeZone: deref(b.TimeZone), AttachmentIDs: deref(b.AttachmentIds),
		TokensPerDay: a.effectiveLimits(ctx, identity(ctx).User).AssistantTokensPerDay}
	if s := b.Settings; s != nil {
		r.Connection, r.Model, r.Temperature = deref(s.Connection), deref(s.Model), s.Temperature
		if s.Mode != nil {
			r.Mode = string(*s.Mode)
		}
		if s.Think != nil {
			r.Think = llm.Thinking(*s.Think)
		}
		if s.MaxRows != nil {
			r.MaxRows = *s.MaxRows
		}
		if s.Toolset != nil {
			r.Toolset = string(*s.Toolset)
		}
	}
	if a.cfg.DemoMode && (r.MaxRows == 0 || r.MaxRows > config.DemoMaxRows) {
		r.MaxRows = config.DemoMaxRows
	}
	if b.Scope != nil && *b.Scope == gen.AssistantMessageRequestScopeView {
		r.View = true
		if b.Context != nil {
			r.Context = contextIn(*b.Context)
		}
		r.Context.ToolFilter = nil
	}
	run, err := a.ai.Runner.Prepare(ctx, u, r)
	if err != nil {
		var le *llm.Error
		if errors.As(err, &le) {
			return nil, fmt.Errorf("%w: %s", assistant.ErrUnavailable, le.Message)
		}
		if errors.Is(err, assistant.ErrUnknownConnection) || errors.Is(err, assistant.ErrUnknownModel) {
			ve := &platform.ValidationError{}
			ve.Add("settings.model", err.Error())
			return nil, ve
		}
		return nil, err
	}
	return sseResponse{ctx: ctx, run: run}, nil
}

// sseResponse streams a prepared run (text/event-stream, one event per line pair).
type sseResponse struct {
	ctx context.Context
	run *assistant.Run
}

func (s sseResponse) VisitSendAssistantMessageResponse(w http.ResponseWriter) error {
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil { // the server's write timeout would cut long answers
		observability.Logger(s.ctx).WarnContext(s.ctx, "assistant stream keeps the write deadline", "error", err)
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no") // nginx: don't buffer
	w.WriteHeader(http.StatusOK)
	_ = rc.Flush()
	var mu sync.Mutex
	write := func(b []byte) {
		mu.Lock()
		defer mu.Unlock()
		if _, err := w.Write(b); err == nil {
			_ = rc.Flush()
		}
	}
	stop := make(chan struct{})
	go func() {
		t := time.NewTicker(heartbeat)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				write([]byte(": keep-alive\n\n"))
			case <-stop:
				return
			}
		}
	}()
	s.run.Execute(s.ctx, func(e assistant.Event) {
		data, err := json.Marshal(eventDTO(e))
		if err != nil {
			return
		}
		write([]byte("event: " + e.Name + "\ndata: " + string(data) + "\n\n"))
	})
	close(stop)
	return nil
}

// eventDTO shapes event payloads like the API schemas (steps and meta in their API form).
func eventDTO(e assistant.Event) any {
	switch d := e.Data.(type) {
	case assistant.StepEvent:
		return struct {
			Index int `json:"index"`
			gen.AssistantStep
		}{d.Index, stepDTO(d.Step)}
	case assistant.DoneEvent:
		return gen.AssistantDoneEvent{MessageSeq: d.MessageSeq, Meta: metaDTO(d.Meta)}
	}
	return e.Data
}

// AddAssistantAttachment stores a photo for the chat's next question (checked and re-encoded).
func (a *api) AddAssistantAttachment(ctx context.Context, req gen.AddAssistantAttachmentRequestObject) (gen.AddAssistantAttachmentResponseObject, error) {
	if a.ai == nil {
		return nil, assistant.ErrDisabled
	}
	data, err := io.ReadAll(io.LimitReader(req.Body, images.MaxBytes+1))
	if err != nil {
		return nil, err // the body limit (413) or a broken upload
	}
	if len(data) > images.MaxBytes {
		return nil, fmt.Errorf("the photo is larger than %d bytes: %w", images.MaxBytes, platform.ErrPayloadTooLarge)
	}
	at, err := a.ai.Chats.AddAttachment(ctx, identity(ctx).User.ID, req.Id, data)
	if err != nil {
		return nil, err
	}
	return gen.AddAssistantAttachment201JSONResponse{Id: at.ID, Width: at.Width, Height: at.Height}, nil
}

// GetAssistantAttachment returns a photo of the chat (the panel's thumbnail).
func (a *api) GetAssistantAttachment(ctx context.Context, req gen.GetAssistantAttachmentRequestObject) (gen.GetAssistantAttachmentResponseObject, error) {
	if a.ai == nil {
		return nil, assistant.ErrDisabled
	}
	data, err := a.ai.Chats.AttachmentData(ctx, identity(ctx).User.ID, req.Id, req.AttachmentId)
	if err != nil {
		return nil, err
	}
	return gen.GetAssistantAttachment200ImagejpegResponse{Body: bytes.NewReader(data), ContentLength: int64(len(data))}, nil
}
