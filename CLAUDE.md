# Finuance — agent instructions

Finuance is a self-hosted personal-finance app for bank CSV exports: Go API + worker, PostgreSQL, React SPA.
It is a rebuild of the single-file legacy app in `legacy/`.

## Start of every session
1. Read `docs/PROGRESS.md` (current phase, open tasks, decisions) — imported below.
2. Read the current phase in `docs/PLAN.md` §9 and the sections it lists under "Read first".
3. Start in plan mode. Propose tasks and wait for approval before editing code.

@docs/PROGRESS.md

## Sources of truth
- Plan and specs: `docs/PLAN.md` (§4 domain rules, §5 schema, §6 API, §7 frontend).
- Behaviour not covered there: `legacy/DESIGN.md` and `legacy/finuance.html`.
- API contract: `api/openapi.yaml`. Decisions: `docs/adr/`. Legacy parity: `docs/PARITY.md`.

## Hard rules
- Only work on the current phase. Out-of-scope ideas go to the Backlog in PROGRESS.md.
- Every change ships with tests. Never skip, delete or weaken a failing test; ask instead.
- Run `make check` before every commit; `make test-integration` before closing a phase.
- Never edit generated code: `backend/internal/httpapi/gen/`, `backend/internal/store/db/`, `web/src/api/schema.d.ts`.
  Change `api/openapi.yaml` or `backend/internal/store/queries/*.sql`, then run `make gen`.
- No new dependencies without asking. No float64 for money. No `innerHTML`/`dangerouslySetInnerHTML`.
- Assistant tools are read-only and run under `store.WithUserReadOnly`. Never log prompts, model output or tool results.
- All user-data DB access goes through `store.WithUser`. Every query filters by `user_id` (for `friendships`, by either side; ADR 0027).
- Never open, print or commit anything under `testdata/private/`.
- Conventional Commits. Branch `phase/NN-name`. Update `docs/PROGRESS.md` after each task.
- New architectural decision → ADR in `docs/adr/` (template `0000-template.md`).

## Commands
- `make check` — lint, typecheck, unit tests, codegen drift, private-data check
- `make test-integration` — Go tests with Postgres (Docker required)
- `make gen` — sqlc + oapi-codegen + openapi-typescript
- `make up` / `make down` — full stack via Docker Compose (web on http://localhost:8000)
- `make dev-api`, `make dev-worker`, `make dev-web` — local dev processes
- `make oracle` — regenerate legacy expectations (Playwright)
- `make fixtures` — regenerate the synthetic CSV fixtures (`make fixtures-check` is part of `make check`)
- `make eval-assistant MODEL=…` — assistant eval suite against a real local model (not part of CI)
