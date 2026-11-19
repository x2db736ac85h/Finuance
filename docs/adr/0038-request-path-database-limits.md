# 0038. Time limits for the request path's database work

- Status: accepted
- Date: 2026-11-19

## Context

A request could wait without limit: for an account's lock that an import's commit holds in the
worker for minutes, for a statement whose cost the request chooses (a huge offset, a search of many
short words, a sort on a text expression), or inside a transaction left open. Each wait keeps one of
the api's pool connections (ten by default), and `/readyz` pings through the same pool, so a few
such requests could take a replica out of service for everyone (the security register, `DB-4` and
`IMP-2`; PLAN task 22.12).

## Decision

- **One place sets the limits**: `store.Open` takes `store.Timeouts` and sets them as session
  parameters on every connection of the pool. There is no `ALTER ROLE`, so existing installations
  get the limits without a database change.
- **The api** (`store.RequestTimeouts`): a lock is waited for at most 5 s, a statement runs at most
  30 s, a transaction left idle ends after a minute. **The worker and the admin commands** keep the
  server's defaults: an import's commit must be able to wait for its locks and run long statements.
- **Exceptions are local**: the streamed export sets a 15-minute statement limit for its own
  transaction (`SET LOCAL`), since its rows go out at the client's pace.
- **The answers say what happened**: a lock timeout (SQLSTATE 55P03) is `409 account_busy`, a
  statement timeout (57014) is `503 timeout` with `Retry-After: 5`, each with its own wording and
  never PostgreSQL's message. Both are problem codes in `api/openapi.yaml`.
- **The cost a request can choose is bounded** as well: offsets at most 1,000,000, searches at most
  16 words.

## Consequences (positive, negative, follow-ups)

- A slow or blocked request ends and gives its connection back; one user can no longer drain a
  replica's pool.
- Deleting or reprocessing an import while another import of the same account commits now answers
  `409 account_busy` after 5 s instead of waiting; the user tries again.
- A statement that legitimately needs more than 30 s on the request path would fail with `503`;
  none was found (`docs/perf.md`), and such a query belongs in a job.
- The values are constants. If a deployment needs others, they become configuration.

## Alternatives considered

- **`ALTER ROLE finuance_app SET statement_timeout`**: applies to the worker too (it uses the same
  role) and needs a change in the bootstrap SQL, in Compose and in CloudNativePG's managed roles.
- **`SET LOCAL` in `store.WithUser`**: one more round trip per transaction, and it misses statements
  outside it.
- **A pool-acquire timeout alone**: frees the waiting request, not the connection the blocked
  statement holds.
