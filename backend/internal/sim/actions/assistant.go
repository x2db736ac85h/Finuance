package actions

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/x2db736ac85h/finuance/backend/internal/client/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/sim"
)

// The assistant: a question (the answer read to its end), a question stopped half-way, a proposal
// applied, an applied change reverted, a digest asked for, and a receipt matched to a payment.
// The lab's model is the mock (PLAN Phase 25, decision 8); its words steer it: "slow" streams
// slowly, `put "PAYEE" in Category` proposes a category, `receipt 12.50 "Shop" on 2026-01-02`
// proposes to attach a receipt.
func assistant() []sim.Action {
	return []sim.Action{
		{Name: "assistant.ask", Area: "assistant", Cost: sim.Model, Needs: sim.NeedData, Run: assistantAsk},
		{Name: "assistant.stop", Area: "assistant", Cost: sim.Model, Needs: sim.NeedData, Run: assistantStop},
		{Name: "assistant.apply", Area: "assistant", Cost: sim.Model, Needs: sim.NeedData, Run: assistantApply},
		{Name: "assistant.revert", Area: "assistant", Needs: sim.NeedData, Run: assistantRevert},
		{Name: "assistant.digest", Area: "assistant", Cost: sim.Model, Needs: sim.NeedData, Run: assistantDigest},
		{Name: "cash.receipt", Area: "cash", Cost: sim.Model, Needs: sim.NeedData, Run: assistantReceipt},
	}
}

var questions = []string{
	"How much did I spend by category last month?",
	"What did I pay at the market this month?",
	"Which subscriptions do I have?",
	"How much went to restaurants in the last three months?",
	"What was my biggest expense this year?",
}

// maxChats keeps the persona under the instance's limit on chats (25 by default).
const maxChats = 15

// newChat makes a chat, deleting the persona's oldest when it has many.
func newChat(ctx context.Context, a *sim.Actor) (openapi_types.UUID, error) {
	list, err := a.API.ListAssistantChatsWithResponse(ctx)
	if err != nil {
		return openapi_types.UUID{}, err
	}
	if err := sim.Expect("chats", list.HTTPResponse, http.StatusOK); err != nil {
		return openapi_types.UUID{}, err
	}
	if chats := list.JSON200.Chats; len(chats) >= maxChats {
		oldest := &chats[0]
		for i := range chats {
			if chats[i].UpdatedAt.Before(oldest.UpdatedAt) {
				oldest = &chats[i]
			}
		}
		d, err := a.API.DeleteAssistantChatWithResponse(ctx, oldest.Id)
		if err != nil {
			return openapi_types.UUID{}, err
		}
		if err := sim.Expect("delete a chat", d.HTTPResponse, http.StatusNoContent); err != nil {
			return openapi_types.UUID{}, err
		}
	}
	r, err := a.API.CreateAssistantChatWithResponse(ctx)
	if err != nil {
		return openapi_types.UUID{}, err
	}
	if err := sim.Expect("make a chat", r.HTTPResponse, http.StatusCreated); err != nil {
		return openapi_types.UUID{}, err
	}
	return r.JSON201.Id, nil
}

// answer is what a run's server-sent events brought.
type answer struct {
	content   int // content events
	proposals []gen.ActionProposal
	stopped   bool
}

// errAnswerFailed: the run ended with an error event.
var errAnswerFailed = errors.New("the assistant could not answer")

// ask sends a question in a new chat and reads the answer's events to the end; onContent runs
// after the first piece of the answer (to stop the run).
func ask(ctx context.Context, a *sim.Actor, question string, onContent func(chat openapi_types.UUID) error) (openapi_types.UUID, answer, error) {
	if !a.Every("assistant.question", sim.QuestionEvery) { // the instance's limit per user and hour
		return openapi_types.UUID{}, answer{}, sim.ErrNothingToDo
	}
	chat, err := newChat(ctx, a)
	if err != nil {
		return chat, answer{}, err
	}
	tz := "Europe/Warsaw"
	res, err := a.API.SendAssistantMessage(ctx, chat, gen.SendAssistantMessageJSONRequestBody{Text: question, TimeZone: &tz})
	if err != nil {
		return chat, answer{}, err
	}
	defer func() { _ = res.Body.Close() }()
	if err := sim.Expect("ask", res, http.StatusOK); err != nil {
		return chat, answer{}, err
	}
	var out answer
	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	event := ""
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data := []byte(strings.TrimPrefix(line, "data: "))
			switch event {
			case "content":
				out.content++
				if out.content == 1 && onContent != nil {
					if err := onContent(chat); err != nil {
						return chat, out, err
					}
				}
			case "step":
				var s struct {
					Proposal *gen.ActionProposal `json:"proposal"`
				}
				if json.Unmarshal(data, &s) == nil && s.Proposal != nil {
					out.proposals = append(out.proposals, *s.Proposal)
				}
			case "stopped":
				out.stopped = true
			case "error":
				return chat, out, fmt.Errorf("%w: %s", errAnswerFailed, data)
			case "done":
				return chat, out, nil
			}
		}
	}
	if err := sc.Err(); err != nil {
		return chat, out, err
	}
	if out.stopped {
		return chat, out, nil
	}
	_, _ = io.Copy(io.Discard, res.Body)
	return chat, out, errors.New("the answer ended early")
}

func assistantAsk(ctx context.Context, a *sim.Actor) error {
	_, _, err := ask(ctx, a, sim.Pick(a, questions...), nil)
	return err
}

// assistantStop asks a question that streams slowly and stops it after the first piece.
func assistantStop(ctx context.Context, a *sim.Actor) error {
	_, _, err := ask(ctx, a, "Tell me slowly how my spending changed this year", func(chat openapi_types.UUID) error {
		r, err := a.API.StopAssistantWithResponse(ctx, chat)
		if err != nil {
			return err
		}
		return sim.Expect("stop", r.HTTPResponse, http.StatusNoContent, http.StatusAccepted, http.StatusOK, http.StatusConflict)
	})
	return err
}

// headers are the two headers an applied proposal carries (ADR 0030).
func headers(chat openapi_types.UUID, key string) (*gen.AssistantChatHeader, *gen.AssistantProposalHeader) {
	return &chat, &key
}

// assistantApply asks the assistant to put a payee of the persona's into a category and applies
// the proposal it makes, as the panel's Apply does: the category first when it is new, then the
// bulk change, both with the chat's and the proposal's headers.
func assistantApply(ctx context.Context, a *sim.Actor) error {
	row, err := someRow(ctx, a)
	if err != nil {
		return err
	}
	payee := strings.Fields(row.Payee)
	if len(payee) == 0 {
		return sim.ErrNothingToDo
	}
	chat, ans, err := ask(ctx, a, fmt.Sprintf(`put %q in %s`, payee[0], sim.Pick(a, "Zakupy", "Jedzenie", "Rozrywka", "Transport")), nil)
	if err != nil {
		return err
	}
	for i := range ans.proposals {
		p := &ans.proposals[i]
		if p.Action != gen.ActionProposalActionSetCategory || p.Ids == nil || len(*p.Ids) == 0 {
			continue
		}
		hc, hp := headers(chat, p.Key)
		id := p.CategoryId
		if id == nil && p.CategoryName != nil {
			c, err := a.API.CreateCategoryWithResponse(ctx, &gen.CreateCategoryParams{XAssistantChat: hc, XAssistantProposal: hp},
				gen.CreateCategoryJSONRequestBody{Name: *p.CategoryName})
			if err != nil {
				return err
			}
			if err := sim.Expect("make the proposed category", c.HTTPResponse, http.StatusCreated); err != nil {
				return err
			}
			id = &c.JSON201.Id
		}
		body, err := bulk(*p.Ids, "category", map[string]any{"id": id})
		if err != nil {
			return err
		}
		r, err := a.API.BulkUpdateTransactionsWithResponse(ctx, &gen.BulkUpdateTransactionsParams{XAssistantChat: hc, XAssistantProposal: hp}, body)
		if err != nil {
			return err
		}
		return sim.Expect("apply a proposal", r.HTTPResponse, http.StatusOK)
	}
	return sim.ErrNothingToDo
}

// assistantRevert undoes the newest change the assistant applied that is still in place.
func assistantRevert(ctx context.Context, a *sim.Actor) error {
	r, err := a.API.ListAIChangesWithResponse(ctx, &gen.ListAIChangesParams{Limit: ptr(int32(20))})
	if err != nil {
		return err
	}
	if err := sim.Expect("assistant's changes", r.HTTPResponse, http.StatusOK); err != nil {
		return err
	}
	for _, c := range r.JSON200.Changes {
		if c.RevertedAt != nil {
			continue
		}
		rv, err := a.API.RevertAIChangeWithResponse(ctx, c.Id)
		if err != nil {
			return err
		}
		return sim.Expect("revert a change", rv.HTTPResponse, http.StatusOK, http.StatusConflict)
	}
	return sim.ErrNothingToDo
}

// assistantDigest asks for a digest (the worker writes it), once a day at most per persona.
func assistantDigest(ctx context.Context, a *sim.Actor) error {
	if !a.Every("assistant.digest", 24*time.Hour) {
		return sim.ErrNothingToDo
	}
	r, err := a.API.RequestAssistantDigestWithResponse(ctx)
	if err != nil {
		return err
	}
	return sim.Expect("ask for a digest", r.HTTPResponse, http.StatusAccepted, http.StatusConflict)
}

// assistantReceipt tells the assistant about a receipt for one of the persona's card payments
// and applies the match it proposes: the receipt goes into the payment's note. (In the web app a
// photo comes with it; the lab's mock reads no photos, so the receipt is described in words.)
func assistantReceipt(ctx context.Context, a *sim.Actor) error {
	list, err := rows(ctx, a, gen.QueryContext{Period: &gen.Period{Preset: ptr(gen.L30)}, Filters: &[]gen.ColumnFilter{
		{Column: gen.FilterColumnAmount, Cond: ptr("<0")}}}, 30)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		return sim.ErrNothingToDo
	}
	row := list[a.IntN(len(list))]
	shop := strings.Fields(row.Payee)
	if len(shop) == 0 {
		return sim.ErrNothingToDo
	}
	amount := -row.Amount.AmountMinor
	q := fmt.Sprintf(`receipt %d.%02d %q on %s`, amount/100, amount%100, shop[0], row.BookedOn.Format(time.DateOnly))
	chat, ans, err := ask(ctx, a, q, nil)
	if err != nil {
		return err
	}
	for i := range ans.proposals {
		p := &ans.proposals[i]
		if p.Action != gen.ActionProposalActionAttachReceipt || p.Ids == nil || len(*p.Ids) == 0 {
			continue
		}
		hc, hp := headers(chat, p.Key)
		note := ""
		if p.Note != nil {
			note = *p.Note
		}
		r, err := a.API.UpdateTransactionWithResponse(ctx, (*p.Ids)[0], &gen.UpdateTransactionParams{XAssistantChat: hc, XAssistantProposal: hp},
			gen.UpdateTransactionJSONRequestBody{Note: &note})
		if err != nil {
			return err
		}
		return sim.Expect("attach a receipt", r.HTTPResponse, http.StatusOK)
	}
	return sim.ErrNothingToDo
}
