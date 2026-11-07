# 0034. Advice library: admin-written articles searched with PostgreSQL full-text search

- Status: accepted
- Date: 2026-11-06

## Context

Questions such as "how do I start an emergency fund" or "should I pay off this card first" have no
answer in the user's transactions. A small local model answers them from what it remembers, which
is unreliable and impossible to review. Phase 20 adds a library of short articles the instance's
administrator writes and publishes, which the assistant searches and names in its answer, plus a
short addition to the system prompt for instance-wide instructions. Articles can be in any
language; the instance must not need a new PostgreSQL extension (no `unaccent`, no `pg_trgm` beyond
what is there) or an external search service.

## Decision

- **`advice_articles`**, an instance table (no `user_id`, outside RLS like `themes`; migration
  00021): slug and language (unique together), title, markdown body (≤ 20,000 characters), tags
  (≤ 10), `draft` or `published`, a version for edits. Administrators write them in Admin →
  Assistant; every change is audited. Anyone signed in reads published ones (`GET /advice/{slug}`).
- **Search** with PostgreSQL's full-text search and the `simple` configuration over text the app
  normalised (lower case, accents folded, `ł` → `l`: `ingest.Norm`), in two app-filled columns:
  the title and tags (weight A) and the body (weight B) make a generated `tsvector` with a GIN
  index. A question becomes an OR of prefix terms (`emergen:* | fund:*`): words of 3 or more
  letters, a short English stop list left out, at most 12. Ranked by `ts_rank_cd`; the asked
  language first, any language when it has no match. No stemming, so it behaves the same in
  every language; prefixes cover plural and inflected endings.
- **`search_advice`** (full tool set) returns at most 3 articles: slug, title, language and the
  first 1,000 characters, with a note that they are general information and should be named.
  The prompt asks the model to use it for general money questions and to say "not financial
  advice" when it relies on one. A step lists the articles found, and each opens in a window.
- **Instructions from the administrator**: `assistant.instructions` in the instance settings
  (≤ 2,000 characters), appended to the system prompt under its own heading, after the app's rules,
  which it can't override. It is the administrator's text for the whole instance (tone, language,
  pointers to articles), never a user's.
- **A starter library** of eight original English articles (emergency fund, a 50/30/20 split,
  subscriptions, paying down debt, saving for a goal, irregular expenses, spending by category,
  sharing costs), embedded in the binary. *Add the starter articles* in Admin → Assistant inserts the
  missing ones as drafts, so an administrator reads them before anyone sees them. Each ends with a
  "general information, not financial advice" note.

## Consequences (positive, negative, follow-ups)

- Works for any language with no extension; the cost is weaker recall than a stemming dictionary
  (Polish and German inflections only match by prefix).
- An administrator's text reaches every user's model: a careless or hostile administrator could
  steer answers. The instructions are visible in Admin → Assistant and audited when changed; the
  app's rules (read-only tools, data only from lookups) stay first.
- Articles are instance content, not part of a user's data export.

## Alternatives considered

- **`pg_trgm` similarity**: already installed, but it ranks short titles over relevant bodies and
  needs per-word thresholds tuned per language.
- **A language's snowball configuration**: PostgreSQL ships none for Polish and several other
  languages the app is used in; one behaviour for all languages was preferred.
- **Retrieval by embeddings**: needs an embedding model on every instance and a vector extension.
