# 0028. Shared groups: copies, splits and row-level security by membership

- Status: accepted
- Date: 2026-11-04

## Context

Phase 19 (19.9) lets friends (ADR 0027) share a group of expenses (a trip, a household) and settle
up. Until now every row belonged to one user, and forced row-level security on `user_id` kept users
apart (ADR 0007). Sharing must not open a member's accounts, bank descriptions or raw rows to
anyone else, must keep balances right when a member deletes their account, and must survive the
import pipeline rebuilding transactions (delete + re-import, duplicate decisions).

## Decision

- **Copies, not references.** A shared group holds its own `shared_expenses`: date, amount, payee,
  category name, note, who paid. A member's transaction becomes a copy when they put it into the
  personal group linked to the shared group (`groups.shared_group_id`). The copy is created, updated
  (category, note, a typed row's amount, date or payee, a rule's category) and deleted in the same
  database transaction as that change. Expenses can also be added in the group without a
  transaction. Members never see where a copy came from.
- **A member-private link** (`shared_expense_links`, user-owned) ties a member's transaction to its
  copy by the transaction's identity (account, fingerprint, occurrence, split scope), like
  annotations. Deleting the import keeps the copy; re-importing re-points the link instead of
  adding a second copy. A copy whose source is gone is marked for its owner only.
- **Splits** (`shared_expense_splits`, minor units) always add up to the expense's amount (a deferred
  constraint trigger checks it at commit). Equal is the default (the remainder cents go to the first
  members by id); shares (weights) and exact amounts are the other modes; "for someone else" is one
  member taking all of it. Group currency only (FX is in the backlog).
- **Balances** per member: paid − share + settlements paid − settlements received; they sum to zero.
  "Who owes whom" pairs the largest debtor with the largest creditor until everyone is even (at most
  n − 1 transfers). Settlements are recorded by the payer or the payee.
- **Membership.** The owner invites friends only; an invitee accepts (their personal group of the
  same name is created and linked) or declines; leaving needs a zero balance; the owner can archive
  the group. Member states only move forward (invited → active or left; active → left), enforced by
  a trigger. A deleted user's member row stays with its display name (`user_id` becomes NULL): the
  others see "Former member" and their balances don't change.
- **Row-level security by membership.** Policies ask `shared_member_role(group)`, a `SECURITY
  DEFINER STABLE` function owned by the schema owner, for the caller's role (`owner`, `member`,
  `invited` or none). The function must read `shared_group_members` without RLS: a policy that
  queries its own table recurses, and FORCE RLS binds the owner too (the lesson of ADR 0025). So
  **`shared_group_members` has RLS enabled but not forced**: the app role is still bound by its
  policies; only the schema owner, which owns the function and runs migrations, is not. Every other
  shared table keeps FORCE RLS: members read the group, its members, expenses, splits and
  settlements; they write only their own expenses (and the splits of those); the owner manages
  members and the group; links stay private to their user.
- **Switches.** `sharedGroups` (instance feature) and demo mode take the routes away (`404`); the
  number of shared groups a user owns is a limit (`maxSharedGroups`).

## Consequences (positive, negative, follow-ups)

- A member sees exactly what was shared and nothing else, by construction and by the database.
- One table is an exception to "forced on every user-owned table"; the schema test lists it with its
  reason, and the shared RLS suite covers it (non-member, invited, active, left, former member).
- Copies can drift from their source only through paths that don't call the sync (none today; a
  test covers each path that changes a linked transaction).
- A deleted owner leaves a group without an owner: members keep sharing expenses and settling up,
  but nobody can invite. Acceptable for now.
- Follow-ups: other currencies (FX backlog), editing someone else's expense (not allowed), receipts
  and merging typed entries with imports (Phase 20).

## Alternatives considered

- **Let members read each other's transactions through a view**: exposes accounts and bank
  descriptions, and couples the group to the import pipeline's row lifetimes.
- **A `BYPASSRLS` role for the membership check**: a database role that sees everything is exactly
  what the app must not need.
- **Membership checks in the application only**: loses the database's guarantee that ADR 0007
  relies on.
