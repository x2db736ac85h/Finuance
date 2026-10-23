package assistant

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/llm"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
)

// ProbeResult is one request of `finuance admin llm-probe`: what the adapter made of the answer.
type ProbeResult struct {
	Name      string
	Duration  time.Duration
	FirstByte time.Duration // until the first streamed delta (0: none)
	Content   string
	Reasoning string
	Calls     []llm.ToolCall
	Stop      string
	Usage     llm.Usage
	Err       error
}

// probeFacts is a made-up data set: the probe never sends user data.
func probeFacts() *Facts {
	from, to := platform.MustDate(2024, 1, 1), platform.MustDate(2025, 3, 17)
	col := func(name, sample string) query.ColumnSample {
		c, _ := query.Lookup(name)
		return query.ColumnSample{Column: c, Sample: sample}
	}
	return &Facts{Rows: 917, Min: &from, Max: &to, Currency: "PLN", Accounts: 1,
		Columns: []query.ColumnSample{col("bookedOn", "17.03.2025"), col("payee", "PIEKARNIA ZIARNO WARSZAWA"),
			col("description", "DEMO 83…"), col("amount", "-98,07"), col("category", "Artykuły spożywcze")},
		Categories: []query.Counted{{Value: "Artykuły spożywcze", Count: 403}, {Value: "Transport", Count: 123}},
		Payees:     []query.Counted{{Value: "MARKET NA ROGU", Count: 136}, {Value: "TAXI MIEJSKIE", Count: 40}}}
}

// probeResult is a made-up tool result for the second round.
const probeResult = `{"currency":"PLN","group_by":"category","matched":58,"total_spent":1095.12,"total_income":0,"net":-1095.12,` +
	`"groups":[{"key":"Artykuły spożywcze","count":58,"spent":1095.12,"income":0,"net":-1095.12}],"omitted_groups":0}`

// Probe sends a fixed set of requests to a model and reports how each was understood: plain text,
// a tool call with the full tool set, the answer after a tool result, plan mode and, when the model
// has a reasoning control, high thinking. It is for checking a new model or server by hand.
func Probe(ctx context.Context, c *Connection, m ModelInfo, now time.Time, report func(ProbeResult)) {
	system := SystemPrompt(now, probeFacts(), Scope{}, ToolsetFull)
	question := "How much did I spend on groceries in February 2025?"
	run := func(name string, req llm.Request) ProbeResult {
		req.Model, req.Caps = m.ID, m.Caps
		t0 := time.Now()
		var first time.Duration
		res, err := c.Client.Chat(ctx, req, func(llm.Delta) {
			if first == 0 {
				first = time.Since(t0)
			}
		})
		r := ProbeResult{Name: name, Duration: time.Since(t0), FirstByte: first, Content: res.Content, Reasoning: res.Reasoning,
			Calls: res.ToolCalls, Stop: res.StopReason, Usage: res.Usage, Err: err}
		report(r)
		return r
	}
	temp := 0.2
	run("text", llm.Request{System: "You are a connection test.", Messages: []llm.Message{{Role: llm.RoleUser,
		Content: "Reply with the single word: pong"}}, Thinking: llm.ThinkOff, Temperature: &temp})
	user := llm.Message{Role: llm.RoleUser, Content: question}
	tc := run("tool_call", llm.Request{System: system, Messages: []llm.Message{user}, Tools: ToolDefs(ToolsetFull),
		Thinking: llm.ThinkAuto, Temperature: &temp})
	if tc.Err == nil && len(tc.Calls) > 0 {
		call := tc.Calls[0]
		run("tool_result", llm.Request{System: system, Tools: ToolDefs(ToolsetFull), Thinking: llm.ThinkAuto, Temperature: &temp,
			Messages: []llm.Message{user, {Role: llm.RoleAssistant, Content: tc.Content, ToolCalls: []llm.ToolCall{call}},
				{Role: llm.RoleTool, ToolCallID: call.ID, Content: probeResult}}})
	}
	zero := 0.0
	run("plan", llm.Request{System: system + PlanInstructions(ToolsetCompact), Messages: []llm.Message{user}, Temperature: &zero,
		Thinking: llm.ThinkAuto})
	if m.Caps.Reasoning != llm.ReasonNone {
		run("thinking_high", llm.Request{System: system, Messages: []llm.Message{user}, Tools: ToolDefs(ToolsetFull),
			Thinking: llm.ThinkHigh, Temperature: &temp})
	}
}

// Describe is a one-line summary of a probe result.
func (r ProbeResult) Describe() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-13s %6.1f s", r.Name, r.Duration.Seconds())
	if r.FirstByte > 0 {
		fmt.Fprintf(&b, " (first token %.1f s)", r.FirstByte.Seconds())
	}
	if r.Err != nil {
		fmt.Fprintf(&b, "  ERROR %s: %v", llm.KindOf(r.Err), r.Err)
		return b.String()
	}
	fmt.Fprintf(&b, "  text %d chars, reasoning %d chars", len(r.Content), len(r.Reasoning))
	for _, c := range r.Calls {
		fmt.Fprintf(&b, ", call %s %s", c.Name, clip(c.Arguments, 120))
	}
	if r.Stop != "" {
		fmt.Fprintf(&b, ", stop %s", r.Stop)
	}
	if r.Usage.Input > 0 {
		fmt.Fprintf(&b, ", tokens %d in / %d out", r.Usage.Input, r.Usage.Output)
	}
	return b.String()
}
