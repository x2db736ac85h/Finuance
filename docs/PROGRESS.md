# Progress

**Current phase:** 0 — Repository bootstrap · **Branch:** phase/00-bootstrap · **Status:** in review (gate)

## Decisions from Irwin
- Name: **Finuance** · GitHub owner: **x2db736ac85h** (module path `github.com/x2db736ac85h/finuance`) · License: **MIT**
- Git workflow: one branch per phase (`phase/NN-name`), merged into `main` with a merge commit so every phase stays visible in history. `main` may be rewritten once, to drop the unscrubbed legacy commit.
- Banks for fixtures (Phase 1): **Pekao**, **PKO BP**, **mBank**. Pekao worked best with the MVP. Samples sit in `testdata/private/` (gitignored, never committed).
- Demo hosting: TBD (ask in Phase 18)
- Homelab (ingress, cert-manager issuer, storage class, backup bucket): TBD (ask in Phase 16)
- Assistant LLM endpoint and model: TBD (ask in Phase 13)

## Current phase tasks
- [x] 0.1 Open questions asked; answers recorded here and in ADR 0001
- [x] 0.2 Repo layout (§3.3); legacy files in `legacy/`, `SAMPLE` scrubbed; `tools/check-private.sh`
- [x] 0.3 Root files: README stub, LICENSE (MIT), `.editorconfig`, `.gitignore`, `.gitattributes`
- [x] 0.4 Agent docs: `CLAUDE.md`, `backend/CLAUDE.md`, `web/CLAUDE.md`, `docs/PLAN.md`, this file, ADRs 0000–0009
- [x] 0.5 Claude Code commands: `.claude/commands/phase-start.md`, `phase-close.md`
- [x] 0.6 `Makefile` with every Appendix F target (stubs print `not yet implemented (Phase N)`)
- [x] 0.7 Local DB: `deploy/compose/compose.yaml`, `deploy/db/bootstrap.sql` + init wrapper, `.env.example`, `db-*` targets
- [x] 0.8 CI skeleton: `.github/workflows/ci.yml` (guardrails + placeholder jobs), `renovate.json`, PR template

## Deviations from the plan (with reason)
- 0.2: the scrub also replaced digit runs of 6–7 digits in `SAMPLE` (card fragments `*********0229197`, reference parts), not only runs of 8 or more, plus the P24 reference and merchant addresses. 26-digit account numbers became the allowlisted all-zero form (`'00000000000000000000000001`), so `check-private.sh` stays strict. One example id in `legacy/DESIGN.md` §7.1 (`0229197`) became `0123456`.
- 0.2: `check-private.sh` also catches the spaced IBAN form (`PL61 1090 1014 …`), and scans untracked, non-ignored files too, so it fails before a commit, not after.
- 0.7: Docker isn't available in the cloud session that built Phase 0. `bootstrap.sql` and the init wrapper were run against a throwaway PostgreSQL 16 cluster: roles and extensions were created, a password containing a quote worked, `finuance_app` can't create tables but gets DML on tables the owner creates. The Compose file was checked with `docker compose config`. The CI job `db` runs the real `make db-up` and the role/extension checks on `postgres:18.6`.
- 0.7: the acceptance command is `make db-psql SQL='\du'`, not `make db-psql -c '\du'`, because make treats `-c` as its own flag.
- 0.1: the Go module path is `github.com/x2db736ac85h/finuance/backend`, not `…/finuance`, because `go.mod` lives in `backend/` (§3.3). A module path should match its directory so `go` tooling and imports resolve. Recorded in ADR 0001.
- 0.8: gitleaks runs as the pinned release binary (checksum verified) instead of `gitleaks-action`, which needs a license key for organisation accounts.

## Follow-ups (must be done in a later phase)
- Phase 1: bank notes from the private samples, for the fixtures and the anonymiser:
  - **Pekao**: UTF-8 without BOM, LF, `;`, `dd.mm.yyyy`, comma decimals, `'`-prefixed account numbers and references, payee padded with spaces before the city. This is the same layout as the legacy `SAMPLE`.
  - **PKO BP**: Windows-1250, CRLF, `,`, ISO dates, dot decimals with no fixed scale (`-10`, `-43.52`). The header has 6 named columns plus 5 blank ones; extra description fields (`Lokalizacja: …`, `Numer karty: …`) spill into the unnamed columns. There are blank lines between rows.
  - **mBank**: UTF-8 with BOM, CRLF, `;`. A ~25-line preamble holds the client name, period and account summary. The header is `#Data operacji;#Opis operacji;…` with `#`-prefixed names. Amounts carry the currency suffix (`-800,00 PLN`), rows end in a trailing `;;`, and descriptions are quoted and padded with runs of spaces.
- Phase 0 gate: once Irwin allows it, force-update `main` to the clean root of `phase/00-bootstrap`, then merge the phase branch (see the PR description).

## Backlog (ideas, not scheduled)
-

## Session log
- 2026-10-20 — Phase 0 built on `phase/00-bootstrap` (clean orphan root, legacy scrub, guardrails, docs, ADRs, Makefile, local DB, CI). Next: Irwin reviews the gate; then Phase 1 (legacy extraction).
