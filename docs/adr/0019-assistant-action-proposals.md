# 0019. Assistant action proposals

- Status: accepted
- Date: 2026-10-24

## Context

PLAN 15.7 lets the assistant help with changes ("label these as Italy trip", "create a rule
ŻABKA → Groceries", "set the category of these 12 rows") while keeping it read-only: its tools
run under `store.WithUserReadOnly` (ADR 0015), a model can be wrong or be steered by text in the
user's own bank files (THREAT-MODEL I5), and a cloud model may be involved. The change has to be
the user's decision, visible before it happens, and go through the same code and checks as a
change made by hand. The audit log arrives in Phase 17.

## Decision

- A fourth kind of tool, `propose_action` (`set_category`, `set_group`, `create_rule`), only
  **reads**: under the same read-only RLS context as the other tools it resolves the target
  (an existing category or group, found ignoring case and accents, or "new") and what the change
  would touch, and returns a proposal. The server never applies it.
- A `set_*` proposal carries the **ids** of the affected transactions, read when it was proposed,
  within the chat's scope (what the user reviews is exactly what Apply changes), at most 1,000
  (the model is told to narrow larger selections). A `create_rule` proposal carries the rule's
  match ("payee contains …" and/or search words) and its current match count; rules apply to
  every account, all time, and never touch manual categories (ADR 0017).
- The proposal is stored with the step (`AssistantStep.proposal`), so the card survives a reload.
  The model receives the count, the target and five sample rows (masked for cloud connections),
  never the ids, and is told that nothing has changed.
- The panel shows a confirmation card (what changes, why, the sample, "Show in table") and
  **Apply** sends the change through the normal endpoints with the user's session and CSRF
  header: `POST /categories` or `POST /groups` when the target is new, then
  `POST /transactions/bulk` or `POST /rules`. The main request carries `X-Assistant-Chat`, and
  the server writes one structured log line (`assistant.action_applied`: user, chat, action,
  rows; no names or other content) until Phase 17 turns it into an audit event.
- The prompt tells the model to propose changes with the tool and never to claim it changed
  anything.

## Consequences (positive, negative, follow-ups)

- No new write path: validation, ownership checks, RLS, annotations (changes survive re-imports)
  and the reclassification job are the ones every manual change uses.
- Prompt injection can at most put a card in front of the user; it can't change data.
- An applied card shows "Applied" for the rest of the session only; after a reload Apply is
  offered again (applying twice sets the same category or group again; a second rule would be a
  duplicate). Recording the applied state server-side can come with the audit log (Phase 17).
- The ids make a stored step larger (up to ~37 KB for 1,000 ids); chats are capped and expire.
- Follow-up (Phase 17): audit events for applied proposals, replacing the log line.

## Alternatives considered

- **The server executes the action after a confirmation endpoint**: a second write path to keep
  in step with the normal endpoints, and a tool run would need a write transaction.
- **Re-resolve the filter at Apply time**: rows imported or changed since the proposal would be
  changed without being reviewed.
- **Let the model write the rule in the filter language**: harder for small models and easy to
  get subtly wrong; "payee contains" and search words cover the common cases, and the user can
  edit the rule afterwards in Settings → Rules.
