# 0029. Transactions added by hand (cash accounts)

- Status: accepted
- Date: 2026-11-04

## Context

Everything in the ledger came from bank files. Cash spending never reaches one, and shared trips
(PR 3) often include it. Phase 19 (19.6) adds transactions typed by the user. They must not
disturb what the import pipeline guarantees: re-importing a file finds the same transactions,
deleting an import removes exactly its rows, and duplicate decisions rebuild from the staged rows.

## Decision

- **Cash accounts.** `accounts.kind` is `bank` (the default) or `cash`, chosen when the account is
  created and never changed. Files go into bank accounts only (`422` for a cash account); typed
  transactions into cash accounts only. Typing into a bank account and matching a typed entry
  with a later import come with receipts in Phase 20.
- **Ordinary transactions with `origin = 'manual'`** (the column existed since 00002) and a random
  32-byte fingerprint, so no import ever matches one. Re-materialisation, deleting an import and the
  duplicate counts already only touch `origin = 'import'`. The search text, payee group and
  normalised fields are built like an imported row's, so search, filters, charts and the overview
  treat them alike.
- **Rules and transfer detection apply to them** (decided 2026-11-03): a rule categorises a typed
  expense, and an ATM withdrawal pairs with the cash it put in the wallet. A category or transfer
  decision set by hand still wins, and the job never changes a typed field or deletes a row.
- **API.** `POST /transactions`, `PATCH /transactions/{id}` (the date, amount, payee and description
  of typed rows only; `422` for imported ones), `DELETE /transactions/{id}` (typed rows only,
  `409` for imported ones, which go with their import). Each takes the account's writer lock
  (ADR 0013) and queues a reclassify. Every change is in the Activity, without amounts or payees.
- **The web app**: *New cash account* in the sidebar and the empty state, *Add* (or `N`) in a cash
  account's toolbar, Edit and Delete in the inspector.

## Consequences (positive, negative, follow-ups)

- The import guarantees are unchanged; the tests that prove them still pass with typed rows around.
- A typed row's annotation (group, category, note) is mirrored like an imported one's (harmless:
  nothing re-imports it) and deleted with the row.
- The data export carries the account kind; the transaction CSV keeps its columns (ADR 0014), so a
  cash account's rows are its typed ones.
- Follow-ups: typing into bank accounts and merging with imports (Phase 20, receipts).

## Alternatives considered

- **A separate table for typed expenses**: every query, chart and the assistant would need a union.
- **Typed rows in any account**: the next import would duplicate them; that needs the matching that
  Phase 20 brings.
- **Keeping typed rows away from the reclassify job** (the plan's first wording): rules would then
  never categorise cash, and a withdrawal could never pair with its cash.
