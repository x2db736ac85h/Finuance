# 0031. Goals: savings targets and reductions, with progress worked out on read

- Status: accepted
- Date: 2026-11-06

## Context

Phase 20 adds goals that the user, or the assistant through a proposal, can set in plain words:
"save 10 000 by June", "spend 20 % less on eating out". Category limits already exist as budgets
(Phase 12), so a goal must not be a second kind of budget. Goals have to show their progress on the
Overview, in the assistant (`goal_status`) and in the data export, stay correct when transactions
are imported, recategorised or deleted later, and be undoable when the assistant set them
(ADR 0030).

## Decision

- **Two kinds** in one user-owned table `goals` (forced RLS, the usual tenant policy; migration
  00017):
  - `save`: put `target_minor` aside between `starts_on` and `deadline`. Saved is income minus
    spending of every account in the goal's currency since the start, transfers between the
    user's accounts left out; or, with `account_id`, everything that came in and went out of that
    one account (its growth).
  - `reduce`: spend `reduce_pct` % less on `category_id` than the average of the `baseline_months`
    (1–12, default 3) full months before the month of `starts_on`, every month until the deadline.
  Category limits stay budgets (`set_budget`); a reduction is relative to the user's own past.
- **Progress is computed on read**, never stored: `internal/goals` holds pure rules
  (`SaveProgress`, `ReduceProgress`, table-driven tests) over a few aggregate queries run under
  `store.WithUserReadOnly`. A save goal is *on track* at 95 % or more of a steady pace (target ×
  elapsed days / all days), *done* once saved reaches the target, *missed* after the deadline. A
  reduce goal is *on track* while this month's spending is within the month's allowance so far
  (limit × day / days in month), *done* after the deadline when every full month kept the limit.
  A goal whose category was deleted, or a reduction with no spending in its baseline, *needs
  attention*. Days are UTC.
- **Versioned edits.** `PATCH /goals/{id}` replaces the goal's fields with the version read
  (`409 stale_version` otherwise); kind, currency and start never change. Statuses: `active`,
  `done` (closed by the user), `archived`. At most 50 goals per user.
- **The assistant** reads goals with `goal_status` and proposes `create_goal` / `update_goal`
  (resolved read-only: the category, account and deadline are checked when proposed; an update
  carries the whole goal after the change and the version it read). The panel applies them through
  `POST` / `PATCH /goals`; the write records a goal item in the AI change (created, or before/after
  with the version). Undo deletes a created goal, or puts an updated one back, only while its
  version is the one the change left (any later save bumps it).
- **References**: a deleted account or category leaves the goal without one (`ON DELETE SET
  NULL`); merging a category moves its goals along. Goals are in the data export (`goals.json`) and
  go with the account.

## Consequences (positive, negative, follow-ups)

- No stored progress to keep in sync with imports, rules, transfers or deletes; each read costs
  one aggregate per goal (indexed by user and date; at most 50 goals).
- "Saved" is a cash-flow measure, not a balance: money moved to an account without a file
  (investments, cash under the mattress) counts as spent unless the goal follows one account.
- A reduction's baseline is fixed by its start month; changing the baseline months recomputes it.
- Goals don't follow the overview's month or filters; the card always shows today.

## Alternatives considered

- **Goals as budgets with a deadline**: budgets are monthly limits per category already; a second
  meaning for them would blur both.
- **Stored progress updated by the reclassify job**: one more thing every write path would have to
  keep right, for a value that is cheap to compute.
- **Balance-based savings for every goal**: most accounts have no reliable balance column, and
  opening balances are optional.
