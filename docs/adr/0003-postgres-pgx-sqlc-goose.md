# 0003. PostgreSQL with `pgx`, `sqlc` and `goose`; dynamic filters via a whitelisted builder

- Status: accepted
- Date: 2026-10-20

## Context

The legacy app filtered and aggregated in the browser over in-memory rows. The rebuild moves that work to the server. We need integrity constraints (de-duplication), row-level security, SQL analytics, and a migration story that runs from the shipped binary. Most queries are static, but table filtering is dynamic: any column, any operator from the legacy filter language, any sort.

## Decision

- **PostgreSQL ≥ 17**, official image, with extensions `pg_trgm` (search) and `citext` (emails). The app connects as `finuance_app`, which owns no tables, so RLS always applies. Migrations run as `finuance_owner`.
- **Driver:** `pgx` v5 with `pgxpool`.
- **Static queries:** `sqlc`. SQL lives in `backend/internal/store/queries/*.sql`, and generated code goes into `backend/internal/store/db/` (never edited by hand). Domain packages wrap sqlc and return domain types.
- **Dynamic queries:** a small hand-written builder in `internal/query`. It compiles a `QueryContext` into parameterised SQL. Columns and operators come from a whitelist (`ColumnRegistry`). User input is only ever bound as a parameter, never concatenated. The builder has fuzz tests.
- **Migrations:** `goose` v3 with plain SQL files, embedded via `embed.FS` and run by `finuance migrate`. Every migration has a working `Down`, and CI runs up → down → up.
- **Bulk inserts:** `INSERT … SELECT FROM unnest(…)` rather than `COPY`, because RLS `WITH CHECK` policies must still apply.

## Consequences (positive, negative, follow-ups)

- SQL stays explicit and reviewable (`EXPLAIN` in PRs), with no ORM magic. Generated types catch schema/query mismatches at compile time.
- Two query styles exist side by side. Contributors must learn when to use which (`backend/CLAUDE.md` recipes).
- The dynamic builder is security-critical code: it needs fuzzing, a column whitelist and review discipline.
- Search normalisation happens in Go (shadow `_n` columns). It doesn't depend on SQL `unaccent`, so Go and SQL agree.

## Alternatives considered

- **GORM / ent:** hides SQL, struggles with RLS session settings and analytic queries, and generates hard-to-tune SQL.
- **sqlc for everything:** sqlc can't express arbitrary WHERE and ORDER BY clauses without awkward `CASE` trees.
- **squirrel / goqu builders:** general-purpose, but a builder scoped to our filter language is smaller and easier to audit.
- **golang-migrate / Atlas:** both fine. goose embeds cleanly, supports Go migrations if ever needed, and has the simplest model.
- **SQLite:** simpler to host, but no RLS and weaker concurrency for the worker.
