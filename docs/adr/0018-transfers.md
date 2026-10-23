# 0018. Transfers: detected pairs, rule marks and the user's decision; left out of charts

- Status: accepted
- Date: 2026-10-23

## Context

Moving money between one's own accounts shows up twice — an outgoing row in one account and an
incoming one in another — and inflates spending and income (PLAN task 15.3, legacy §27 idea). The
schema had `is_transfer`, `transfer_peer_id` and `tx_annotations.transfer_override` since Phase 4.

## Decision

1. **Detection** (in the reclassify job, ADR 0017): an unmarked outgoing transaction pairs with an
   unmarked incoming one of the **opposite amount**, the **same currency**, in **another account of
   the same user**, booked **at most 3 days apart**. Candidates come smallest date gap first, then the
   older rows (`seq`); each transaction joins at most one pair (greedy). Pairs point at each other.
2. **Why a row is a transfer** is stored in `transactions.transfer_source`: `pair` (detected), `rule`
   (a rule's transfer action), `manual` (the user, with `tx_annotations.transfer_override`), `none`.
   `manual` with `is_transfer = false` means "not a transfer": detection and rules leave it alone.
3. **The user decides in the inspector:** *Not a transfer* unpairs both halves of a pair (both become
   `manual`, false); *Mark as transfer* marks a row by hand; *Automatic* hands it back to the rules
   and the detection. The decision survives delete + re-import (annotations).
4. **A pair that lost a half** (its import deleted, the row re-materialised) is released on the next
   run and may pair again. Existing pairs stay stable across runs.
5. **Defaults:** insights, the overview and the assistant's view scope leave transfers out
   (`includeTransfers: false`, the API default). The table shows them with a *Transfer* badge; the
   Columns menu hides them (a "transfers hidden" chip shows them again; Clear all resets it). Group
   stats keep including them (legacy `groupStats`). The parity harness compares with transfers
   included, because the legacy app has none.

## Consequences

- Spending and income in the charts and the overview stop counting money that only moved between
  the user's accounts.
- Two unrelated transactions of equal opposite amounts within 3 days are paired by mistake; the
  inspector's *Not a transfer* fixes it for good.
- Transfers to accounts that aren't imported (a broker, a savings account at another bank) aren't
  detected; a rule with *Mark as transfer* covers them.

## Alternatives considered

- **Optimal matching (minimum total gap)**: same result in every realistic case, more code, and less
  predictable to explain than "closest dates first".
- **Hide transfers in the table too:** the user couldn't see or correct the pairs.
- **Keep the detection's choice only in `is_transfer`:** rules, detection and the user would
  overwrite each other's marks with no way to recompute from scratch.
