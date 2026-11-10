# Progress

**Current phase:** 21 — Going public · **Branch:** `claude/wizardly-goodall-pu69jr` · **Status:** in progress (21.0–21.6 done; next the move to the public repository, which only the maintainer can do)

**Released:** `v0.4.1` (2026-11-09). Phases 0–20 are complete; what each delivered is in [ROADMAP.md](ROADMAP.md) and the [CHANGELOG](../CHANGELOG.md). The detailed record of those phases (every task, decision, deviation and session) is kept outside this repository; this file starts again with Phase 21.

## Decisions that still apply

Project
- Name **Finuance**, MIT license, Go module path `github.com/x2db736ac85h/finuance/backend`.
- Changes reach `main` only through pull requests with every CI job green, merged with a merge commit. A pull request's head commit never carries `[skip ci]`.
- Conventional Commits. New architectural decisions get an ADR (`docs/adr/`, 0001–0036 so far).
- No new dependencies without the maintainer's approval. Every accepted vulnerability needs a reason and an expiry (`.trivyignore` is empty).

Data and tests
- **All committed test data is synthetic.** Real bank exports stay under `testdata/private/` (gitignored), also after the anonymiser: its output keeps a real spending pattern. The bank look-alikes in `testdata/fixtures/synthetic/banks/` copy the structure of real files with every value invented.
- `tools/check-private.sh` (in `make check` and CI) refuses account numbers, e-mail addresses outside the reserved domains, tailnet host names and postcodes in test data.
- The legacy oracle pins Playwright 1.56.1. Expectations are regenerated with `make oracle` (two runs, zero diff); deliberate differences are in `testdata/oracle/ALLOWED_DIFFS.md`.
- Money is `int64` minor units, never floats. Amounts and dates read as text use `-79,99` and `dd.mm.yyyy`.

Backend
- Row-level security is enabled and forced on every user-owned table (ADR 0007); shared tables are scoped by membership (ADR 0028); administrators see counts, never financial data (ADR 0025).
- The binary fails closed: without `FINUANCE_ENV` it is a production instance, sign-up is off unless `FINUANCE_ALLOW_SIGNUP=true`, and `/debug/pprof` needs `FINUANCE_PPROF=true`.
- The assistant is provider- and model-agnostic (ADR 0015). Its tools are read-only; changes are proposals applied through the normal endpoints, recorded and undoable (ADR 0030). Prompts, model output and tool results are never logged.

Deployment
- Environments are described without machine details: this repository uses placeholders (`<host>`, `<tailnet-ip>`), its host files are templates, and per-machine values come from a private overlay repository that Argo CD reads as a second source (ADR 0023).
- Releases: `vX.Y.Z-rc.N` first, deployed through the `gitops-bump` pull request; rollback is a revert. Images are signed with cosign and carry SBOMs; the cluster checks registries now and signatures in Phase 23 (ADR 0024).
- `make check` before every commit; `make test-integration` and the E2E suite before a pull request.

## Current phase tasks

Phase 21 — Going public (PLAN §9, regrouping note of 2026-11-09). Only what must happen before the repository is readable by anyone.

- [x] 21.0 regrouping: the public release comes first with a small scope; Phone and touch becomes Phase 22 and the rest of the release work Phase 23 (PLAN note, this file, ROADMAP)
- [x] 21.1 synthetic bank fixtures replace the anonymised real exports: `testdata/fixtures/real/` and its oracle and golden twins removed; `oracle_test.go` (header fixes), `imports_http_test.go` and `quality.spec.ts` moved onto the look-alikes with the same assertions; a new `banks/mbank-like-long.csv` covers the legacy's second header failure (the first transaction taken as the header); oracle regenerated twice with zero diff, goldens updated
- [x] 21.2 details that describe one person or machine: a store code and place names in samples and test strings replaced with invented ones (oracle and goldens follow); `check-private.sh` gains the e-mail, tailnet and postcode rules with `tools/check-private-test.sh`
- [x] 21.3 documents: neutral wording ("the maintainer"), this lean PROGRESS and `docs/ROADMAP.md`; SECURITY.md's support policy; THREAT-MODEL I3 says what is open
- [x] 21.4 security hotfixes: fail-closed defaults (`TestDefaults`), profiling behind `FINUANCE_PPROF` (`TestNoopTracingAndAdmin`), empty passwords in the production env template (the release smoke test generates them), `compose.prod.yaml` with its own subnet as the only trusted proxy range and a hardened `migrate`, the pod-kill script checks the certificate by default, Renovate pins actions and images by digest
- [x] 21.5 README rewrite: about 1,700 words instead of 4,100, first person, the overview screenshot first, why it might interest you, tech stack, design decisions with their ADRs, operations with a Grafana screenshot, testing, roadmap, how it was built; the long feature list and key concepts moved to `docs/features.md`, deployment to `docs/deploying.md`; CHANGELOG release dates; CONTRIBUTING; issue templates (bug, bank layout); every relative link checked
- [x] 21.6 `tools/publish-public.sh [--dry-run] <ref> [<empty-remote>]`: exports the tree, runs its guards (and gitleaks when installed), fails on a term of `PUBLISH_PATTERNS` without printing it, empties `.gitleaksignore`, one root commit by a no-reply identity, refuses a remote with refs; `tools/publish-public-test.sh` in `make check` (one commit, identity, no old objects or trailers on the remote, each refusal); a dry run on this repository passes; RUNBOOK "Moving to the public repository". `--history` keeps the history of the ref with every address that is not a no-reply address rewritten (the full history of `main` was searched first: the private address is only in the author field of 9 merge commits, never in a file or a message, and no machine detail is in it): tests for the rewritten address, kept names and messages, moved fingerprints, tags and branches left behind, a term in a deleted file
- [ ] 21.7 the move (the maintainer): the current repository renamed and kept private, a new public one from the publish script, repository settings, Argo CD on the new URL, packages public
- [ ] Gate (the maintainer): the exported tree read through, `make check`, `make test-integration` and E2E green, the quick start from a fresh clone

Not run in the session that did 21.1–21.4 (no Docker there): `make test-integration`, the E2E suite and `make helm-lint`. CI runs them on the pull request.

## Next phases

- **Phase 22 — Phone and touch**: a bottom tab bar, a two-line list, windows that fit small screens, installable without offline data; `v0.5.0`.
- **Phase 23 — Release 1.0** (PLAN Phase 21 tasks that remain): CodeQL, dependency review, workflow linting and Scorecard; signed images enforced in the cluster and deployed by digest; the demo host; write-ups; screenshots in every theme; `v1.0.0`.

## Deviations from the plan (with reason)

- Phase 21 (2026-11-09): `docs/PLAN.md` stays in the repository (about 210 files refer to its sections) instead of being archived; only its wording was made neutral.
- 21.2: `192.168.1.20` stays as the example address of a model server on a home network (it has to be a private address for the dial guard's tests, and it names no one). The time zone (`Europe/Warsaw`) stays too: the product is built around Polish bank exports.
- 21.4: actions and images are not pinned by digest by hand (the session had no registry access to look digests up); Renovate's `helpers:pinGitHubActionDigests` and `docker:pinDigests` open that pull request. The Argo CD applications keep the overlay repository's real URL, because the cluster reads them from git (a private repository's name reveals nothing in it).

## Follow-ups (must be done in a later phase)

- **Before 21.7:** a copy of the Sealed Secrets controller key outside the machine.
- **Every pull request until fixed:** `minimatch` (MEDIUM, development tools only; `docs/security-scan.md`): upgrade as soon as ESLint and openapi-typescript accept the fixed major.
- **Phase 23:** the full security scan with nothing ignored before the demo is public; cosign verification in the cluster and deploy by digest; Argo CD sign-in through an identity provider as soon as its UI is reachable beyond a port-forward.
- Renew the registry pull token before it expires (not needed once the images are public).
- Move to Node 26 LTS before Node 22's end of life (2027-04-30): CI, `Dockerfile.web`, `mise.toml`, and the two upload tests that fail there.

## Backlog (ideas, not scheduled)

- A cheaper first render for the Vitest files that time out when the full parallel suite shares a busy machine; the duplicates window's first render of many groups.
- `fin mcp`: the CLI as an MCP server over stdio.
- A thinking toggle next to the question field; evals on the model server actually used and on candidate models.
- An alert path that survives an outage of the machine itself (today a daily digest e-mail shows it by its absence).
- The api and worker retry their first database connection instead of restarting once.
- FX conversion with central-bank rates; the assistant names the category of a suggested rule.
- Restore a data export into a new instance; per-user model API keys; per-month budgets with rollover; balance reconciliation against the bank's balance column.
- Compose: nginx re-resolves the api upstream after the api container is recreated.
- Overview under load: cache the month aggregates.
- A `profiling` value in the Helm chart for `FINUANCE_PPROF`.

## Session log

- 2026-11-09 — Audit before going public (private data, security, first impression): no credentials and no serious flaw, but the history and a few files must not be published. Phase 21 regrouped to "going public" and 21.0–21.4 done: synthetic fixtures only, neutral wording, new guard rules, fail-closed defaults and the other security hotfixes. Then the README rewritten for readers new to the project (21.5) and the publish script with its tests (21.6). Next: the move (21.7), by the maintainer.
