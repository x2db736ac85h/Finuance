# 0006. River for background jobs; transactional enqueue

- Status: accepted
- Date: 2026-10-20

## Context

Some work shouldn't run in the request path: committing large imports, re-materialising dedupe overrides, purging expired sessions, demo resets, exports. The job must be enqueued **if and only if** the triggering state change commits. Otherwise we get orphan jobs, or state changes that never get processed. Postgres is already our only stateful dependency (ADR 0001).

## Decision

- Use **River** (`riverqueue/river`) with the `riverpgxv5` driver as the job queue.
- Jobs are enqueued with `InsertTx` **inside the same transaction** as the state change (outbox semantics without an outbox table).
- A separate `finuance worker` process (the same binary, a different subcommand) runs workers. The API process only inserts.
- River's schema migrations run as part of `finuance migrate up`, after goose, as `finuance_owner`.
- Every job kind sets `MaxAttempts`, is idempotent, and exports metrics (count, duration, failures). Periodic jobs (session purge, demo reset) use River's periodic job support.
- Job arguments carry ids, never user data, so payloads in the queue table contain nothing sensitive.

## Consequences (positive, negative, follow-ups)

- No Redis, broker or extra container. Backups of Postgres include the queue.
- Transactional enqueue removes a whole class of consistency bugs.
- Queue throughput is bounded by Postgres. That is ample for a personal-finance workload.
- River's tables live outside our RLS model. They are accessed only by the worker, and payloads contain no user data.
- We depend on River's maintenance and its migration line. Renovate keeps it current; major upgrades get an ADR note.

## Alternatives considered

- **Goroutines in the API process:** work is lost on restart; there are no retries and no visibility.
- **Asynq / Machinery (Redis):** add a stateful dependency.
- **A hand-rolled `SKIP LOCKED` queue:** feasible, but River already provides retries, scheduling, uniqueness and a UI.
- **Temporal:** powerful, and far too heavy for this scale.
