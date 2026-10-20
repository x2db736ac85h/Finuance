# 0001. Project foundations: name, license, monorepo, self-hosted positioning

- Status: accepted
- Date: 2026-10-20

## Context

Finuance started as a single offline HTML file (formerly *CSV Inspector*). It works well for one person in one browser, but it can't grow into a multi-user, server-backed product, and as a portfolio piece it says little about backend or operations skills. The rebuild (`docs/PLAN.md`) needs a stable name, a code-ownership path, a license and a repository shape before any code exists.

## Decision

- **Name:** Finuance. Code identifiers, env vars and database roles use `finuance` / `FINUANCE_`.
- **Repository:** `github.com/x2db736ac85h/finuance`. The Go module path is `github.com/x2db736ac85h/finuance/backend`. GitHub is the primary remote, with CI on GitHub Actions; a Gitea pull-mirror serves the homelab.
- **License:** MIT.
- **One monorepo:** `api/` (OpenAPI contract), `backend/` (Go), `web/` (React), `deploy/`, `tools/`, `testdata/`, `legacy/`, `docs/`. One root `Makefile` is the entry point for every workflow.
- **Positioning:** self-hosted and privacy-first. Postgres is the only stateful dependency. The AI assistant targets self-hosted LLMs only, unless an admin explicitly allows otherwise. No telemetry leaves the instance.
- **Legacy as specification:** the scrubbed MVP and its design doc live in `legacy/`. Where `docs/PLAN.md` §4 doesn't change a behaviour, the legacy behaviour is the spec.
- **Workflow:** one branch per phase (`phase/NN-name`), merged into `main` with a merge commit, so the history shows each phase as a unit. Commits follow Conventional Commits.

## Consequences (positive, negative, follow-ups)

- MIT keeps adoption friction low for users and recruiters. It doesn't stop someone from hosting a closed fork. That is acceptable for a personal project.
- A monorepo lets one PR change the contract, the server and the client together, and lets CI check codegen drift. The cost is that CI must path-filter, so slow jobs don't run for docs-only changes.
- The legacy file must never contain private data. `tools/check-private.sh` and gitleaks guard this in `make check` and CI.

## Alternatives considered

- **AGPL-3.0:** protects the self-hosted product from closed SaaS forks, but is unusual in portfolio code and puts some contributors and employers off. Rejected by the owner.
- **Separate repos for backend and frontend:** contract changes would need coordinated PRs and versioned client packages. That is too much overhead for one maintainer.
