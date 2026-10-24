# 0017. Rules apply from scratch in order; categories keep their bank names through aliases

- Status: accepted
- Date: 2026-10-23

## Context

Phase 15 adds categorisation rules (PLAN task 15.2) and category management (15.1). A rule matches
with the table's filter language (`match = {search, filters}`) and sets a category and/or marks
transfers. Three sources can categorise a transaction: the bank's category column (`bank`), a rule
(`rule`) and the user (`manual`, kept in `tx_annotations` so it survives re-imports). Rules change
over time — they are edited, reordered, disabled, deleted — and transactions are re-materialised on
re-imports and duplicate decisions, which resets their category to the bank's.

Categories were found by their normalised name: every import upserts a category for each bank
category name and assigns it. Renaming, merging or deleting a category would be undone by the next
import of the same bank category.

## Decision

1. **Precedence is manual > rule > bank.** A rule never changes a category set by hand.
2. **Rules apply in position order and the last matching rule wins** (the plan's rule), per action:
   a later category action overwrites an earlier rule's category; a transfer action marks only rows
   that aren't marked yet.
3. **Reclassification is computed from scratch**, never incrementally (`rules.Reclassify`): in one
   transaction it resets every rule-set category to the bank's, unmarks rule transfers and
   half-pairs, runs the enabled rules in order, then detects transfers (ADR 0018). The result depends
   only on the current rules, transactions and manual decisions. Each rule-set category records the
   rule (`transactions.category_rule_id`), shown in the inspector.
4. **One job, `reclassify`**, runs it (the plan's `ApplyRules` and `DetectTransfers` together,
   because rules can mark transfers and detection must see the result). It is enqueued after every
   commit, legacy migration, import delete, duplicate decision, demo seed, category merge/delete,
   rule change (in the same database transaction) and "Apply rules now". Runs are idempotent; it
   takes every account's writer lock (ADR 0013) in id order, so it never interleaves with a commit.
   The plan's scopes (`import:<id>`, `uncategorised`) are not needed: a from-scratch run over 100k
   rows is a handful of set-based statements.
5. **A rule's match compiles with the table's compiler** (`query.ApplyRuleCategory`), over every
   account and all time, transfers included. A match must have a search or a column filter; a rule
   that would match everything is refused.
6. **Category identity:** `category_aliases (user_id, name_n → category_id | NULL)` maps a bank
   category name to the category it belongs to now. Renaming leaves an alias for the old name,
   merging moves the merged category's name and aliases to the target, deleting leaves an alias to
   NULL (the bank name stays uncategorised). Every lookup of a bank name goes through the
   `bank_category_map` view (alias first, then the category of that name; `security_invoker`, so RLS
   applies). Creating or renaming a category to a name removes that name's alias.

## Consequences

- Editing, reordering or deleting a rule always gives the same table as if the rules had been
  written that way from the start; there is no history to get wrong.
- A reclassification rewrites every rule-set row twice (reset, then the rules). Fine at 100k rows;
  a large rule set on a large user could later move to one statement per action with `DISTINCT ON`.
- The result appears a moment after the request (a job). The web app refetches the affected views
  right away and again after 1.2 s and 4 s.
- A rule matching on the `category` column sees the categories of the earlier rules in the order
  (chaining is deterministic, but confusing); the editor doesn't forbid it.

## Alternatives considered

- **Apply each rule once, when created, to the matching rows** (the plan's literal `UPDATE`): a
  deleted or edited rule leaves its categories behind, and the result depends on history.
- **First matching rule wins:** common in banking apps, but the plan specifies the last match, and
  "a lower rule wins" reads naturally with a top-to-bottom list.
- **Store the bank category id on transactions and rename in place:** a merge or delete still loses
  the mapping on re-import, and re-imports re-create the old name.
