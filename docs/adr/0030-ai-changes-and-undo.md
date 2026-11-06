# 0030. The assistant's changes: recorded with the change, marked, and undoable

- Status: accepted
- Date: 2026-11-06

## Context

Phase 20 lets the assistant do more (new proposal kinds, and applying the kinds a user opts into
by itself). Proposals are still applied by the panel through the normal endpoints (ADR 0019), so
there is no second write path; but a user has to be able to see what the assistant changed, filter
for it and take it back, also days later and after a reload. The activity log (ADR 0021) records
that a proposal was applied, not what it replaced.

## Decision

- **Recorded by the write itself.** A request that applies a proposal carries `X-Assistant-Chat`
  and `X-Assistant-Proposal` (and `X-Assistant-Auto` when the panel applied it by itself). The
  handler checks that the key is one of that chat's proposals (under the user's RLS) and puts an
  *AI origin* into the request context. Every write path that sees it records what it changed with
  `ledger.RecordAIChange`, **in the same database transaction** as the change: a failed write
  leaves no record, and no record can be missing for a write that happened.
- **One change per applied proposal** (`ai_changes`, keyed by user, chat and proposal while not
  undone), with its items (`ai_change_items`: entity, id, and the fields the action set, before
  and after). The two requests of one Apply (creating the target category or group, then the bulk
  change or rule) add to the same change. Applying again after an undo starts a new one. The chat
  id is not a foreign key: chats expire, the record stays until the account is deleted.
- **Undo** (`POST /ai-changes/{id}/revert`) runs in one transaction: every item must still have its
  "after" state (rules are compared by content, not by timestamp), otherwise `409 changed_since`
  and nothing changes. Then rows get their category (by hand, or back to the bank's and the rules)
  and group back through the ledger's own annotation code, rules the change created are deleted,
  and groups and categories it created are deleted only when nothing uses them any more (what the
  user put into them since keeps them). A reclassification is queued when categories or rules
  changed; the undo is an activity event (`assistant.change_reverted`).
- **Marked.** Transaction rows carry `ai` (✦ in the grid) and `source` (Bank, Manual, or
  Assistant for a row the assistant added); `QueryContext.aiAssisted` and the "AI assisted" chip
  show only rows with an un-undone change; the `source` column filters and exports like any other.
  Settings → Activity lists the changes with Undo, the proposal card offers Undo and Apply again.

- **Applied by itself** only for the kinds a user ticks (`assistant_settings.auto_apply`, empty by
  default): the panel applies such a proposal when the answer to the user's own question is
  complete, with `X-Assistant-Auto` (recorded as `auto`), never one touching more than 50
  transactions and never a background digest's (those run on the server and only wait for Apply).
  The card says *Applied automatically* and offers Undo like any other.

## Consequences (positive, negative, follow-ups)

- Each new write path (Phase 20's new proposal kinds) adds one `RecordAIChange` call and the
  matching undo case; a test per path checks both.
- Undo refuses rather than merges when anything changed since: simple and safe, at the cost of an
  occasional "do it by hand".
- Rows deleted and re-imported get new ids, so their old changes can no longer be undone (they
  count as changed since).
- The ✦ marker and the filter cost one indexed `EXISTS` per row; the table's windows are 200 rows.

## Alternatives considered

- **Record in the handler after the write**: a crash between the two loses the record, and the
  "before" state would have to be read outside the change's transaction.
- **Store only the activity event and recompute the "before" from history**: there is no history
  of categories and groups to recompute from.
- **A server-side "apply proposal" endpoint that also records**: a second write path (rejected in
  ADR 0019 already).
