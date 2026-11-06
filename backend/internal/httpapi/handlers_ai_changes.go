package httpapi

import (
	"context"
	"errors"

	"github.com/x2db736ac85h/finuance/backend/internal/aichanges"
	"github.com/x2db736ac85h/finuance/backend/internal/httpapi/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

// aiOrigin marks ctx's writes as an applied assistant proposal (ADR 0030) when the request carries
// X-Assistant-Chat and X-Assistant-Proposal: the key must be one of that chat's proposals (the
// user's own, under RLS), whose action the change is recorded under. Without the headers the
// request is an ordinary edit.
func (a *api) aiOrigin(ctx context.Context, chat *gen.AssistantChatHeader, proposal *gen.AssistantProposalHeader, auto *gen.AssistantAutoHeader) (context.Context, error) {
	if chat == nil || proposal == nil || a.ai == nil {
		return ctx, nil
	}
	p, err := a.ai.Chats.FindProposal(ctx, identity(ctx).User.ID, *chat, *proposal)
	if errors.Is(err, platform.ErrNotFound) {
		return ctx, validation("X-Assistant-Proposal", "is not a proposal of this chat")
	} else if err != nil {
		return ctx, err
	}
	return ledger.WithAIOrigin(ctx, ledger.AIOrigin{ChatID: *chat, ProposalKey: *proposal, Action: p.Action, Auto: deref(auto)}), nil
}

func aiChangeDTO(c aichanges.Change) gen.AIChange {
	return gen.AIChange{Id: c.ID, ChatId: c.ChatID, ProposalKey: c.ProposalKey, Action: c.Action, Auto: c.Auto,
		AppliedAt: c.AppliedAt, RevertedAt: c.RevertedAt, Items: c.Items}
}

// ListAIChanges returns the changes the assistant made, newest first.
func (a *api) ListAIChanges(ctx context.Context, req gen.ListAIChangesRequestObject) (gen.ListAIChangesResponseObject, error) {
	var cursor string
	var limit int32
	if req.Params.Cursor != nil {
		cursor = *req.Params.Cursor
	}
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	page, err := a.aiChanges.List(ctx, identity(ctx).User.ID, cursor, limit)
	if err != nil {
		return nil, err
	}
	out := gen.AIChangesPage{Changes: make([]gen.AIChange, 0, len(page.Changes))}
	for _, c := range page.Changes {
		out.Changes = append(out.Changes, aiChangeDTO(c))
	}
	if page.Next != "" {
		out.Next = &page.Next
	}
	return gen.ListAIChanges200JSONResponse(out), nil
}

// RevertAIChange undoes one change the assistant made and records it in the activity log.
func (a *api) RevertAIChange(ctx context.Context, req gen.RevertAIChangeRequestObject) (gen.RevertAIChangeResponseObject, error) {
	u := identity(ctx).User.ID
	res, err := a.aiChanges.Revert(ctx, u, req.Id, a.reclassifyIn(u))
	if err != nil {
		return nil, err
	}
	a.auditMe(ctx, ledger.AuditAssistantReverted, req.Id.String(), map[string]any{"action": res.Change.Action, "rows": res.Rows})
	return gen.RevertAIChange200JSONResponse{Change: aiChangeDTO(res.Change), Rows: res.Rows, Deleted: res.Deleted, Kept: res.Kept}, nil
}
