# Progress

**Current phase:** 22 — Security I: patches, pins and scanners · **Branch:** `phase/22-security-patches` (not created yet) · **Status:** planned on 2026-11-16, not started. Phase 21 is closed except for the settings check that moved into 22.1.

**Released:** `v0.4.1` (2026-11-09). Phases 0–21 are complete; what each delivered is in [ROADMAP.md](ROADMAP.md) and the [CHANGELOG](../CHANGELOG.md). The detailed record of Phases 0–20 (every task, decision, deviation and session) is kept outside this repository; this file starts again with Phase 21.

**The plan from here:** `docs/PLAN.md` §9, "Rev 5 — the plan after going public" and Phases 22–29. A map of the system as built: [COMPONENTS.md](COMPONENTS.md).

## Decisions that still apply

Project
- Name **Finuance**, MIT license, Go module path `github.com/x2db736ac85h/finuance/backend`.
- Changes reach `main` only through pull requests with every CI job green, merged with a merge commit. A pull request's head commit never carries `[skip ci]`.
- Conventional Commits. New architectural decisions get an ADR (`docs/adr/`, 0001–0036 so far).
- No new dependencies without the maintainer's approval. Every accepted vulnerability needs a reason and an expiry. **Findings are fixed, not ignored** (the maintainer, 2026-11-18): an entry in `.trivyignore` is only for a finding that cannot be fixed yet, it is revisited in every phase, and an expiry is never extended without trying the fix first. One entry exists: `picomatch` (development tools, no fixed version), until 2026-12-02.

Order of work (the maintainer, 2026-11-16)
- **Security first:** Phases 22 and 23 before anything else, because the repository is public.
- Then observability (24), the traffic simulator (25), cloud AI (26), the Google Cloud lab (27), the new interface and phone (28, design-led, its own large phase), Release 1.0 (29).
- The simulator runs against a **separate lab instance**, never the instance with real data.
- A cloud model sees **synthetic data only** at first; the instance with real data keeps its local model.
- The Google Cloud environment is an **on-demand lab** with synthetic data, created and destroyed with Terraform. The public demo stays on a small VPS (decision of 2026-11-03).
- Approved on 2026-11-17: **two-step sign-in with one-time codes (TOTP)** in Phase 23 (task 23.7), and the dependency **`golang.org/x/oauth2`** for Google credentials in Phase 26 (task 26.0).

What is written where (the maintainer, 2026-11-17)
- **Sensitive detail lives in the private environment repository** (`finuance-env`): findings that are not fixed yet (its `security/` folder, with a register whose ids the plan cites), anything that describes one machine, and the housekeeping list for the homelab. This repository says what to build. An agent working on Phases 22 and 23 needs the private repository attached.
- Since 2026-11-18 that includes **this cluster's SealedSecrets and the controller's public certificate** (`finuance-env`: `homelab/secrets/`, `homelab/monitoring/`, `homelab/rustfs/`), which Argo CD reads from there (ADR 0023 addendum). Plaintext credentials (the controller key backup, tokens, app passwords) go into neither repository.
- The history keeps the old test fixtures: they are not real data as they stand, so no rewrite.
- Backups on the same disk as the database are accepted for now; `docs/RUNBOOK.md` must say how to connect storage outside the machine (an S3-compatible bucket or a second machine) so that it can be done on demand.
- The maintainer installs the Renovate app. The browser-error endpoint of task 24.8 is approved.

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

Phase 22 — Security I: patches, pins and scanners (PLAN §9, with the findings of 2026-11-16 and every task in detail). To be confirmed with `/phase-start 22`.

- [ ] 22.0 re-run the audit's scans on the current `main` and compare with the findings table
- [ ] 22.1 the public repository's settings (the maintainer): ruleset, secret scanning with push protection, private vulnerability reporting, Actions settings, stale branches, public packages, and the housekeeping list in the private repository's README
- [ ] 22.2 patches: ~~Go 1.27.4~~ (done 2026-11-17, pull request 34, ahead of the phase start because it failed every CI run), `x/crypto` 0.59.0, PostgreSQL 18.7 (RLS suite and the de-duplication property test on it), ESLint and typescript-eslint (`minimatch`), the Node build image, Tempo, Grafana, the Helm CLI; `v0.4.2`
- [ ] 22.3 Renovate installed (the maintainer) and seeing every kind of pin
- [ ] 22.4 everything pinned: actions by SHA, images by digest, `tools/check-pins.sh`
- [ ] 22.5 workflow hardening: permissions per job, no token on a command line, Trivy without the Docker socket, zizmor and actionlint
- [ ] 22.6 CodeQL, dependency review with a licence allowlist, osv-scanner, SARIF, OpenSSF Scorecard, trufflehog once
- [ ] 22.7 a daily scan of the released images that opens an issue
- [ ] 22.8 build provenance next to the SBOM
- [ ] 22.9 `CODEOWNERS`, `SECURITY.md`, the card-number rule, `check-env-details` in CI
- [ ] 22.10 Node 26 LTS (moves to Phase 24 if the two upload tests need more than a day)
- [ ] 22.11 `docs/security-scan.md` with every finding decided
- [ ] 22.12 first fixes from the private register (`IMP-1`, `IMP-6`, `API-4`, `AI-1` first half, `WEB-2`, `OPS-5` first half, `DB-4`, `IMP-2`), after marking every row fixed, open or not applicable
- [ ] 22.13 sealed secrets move to the private overlay; the maintainer rotates the values — so far (2026-11-18, ahead of the phase start, branch `chore/env-split`): the eight SealedSecrets and the public certificate copied byte for byte into `finuance-env` and pushed there first (`kube-prometheus-stack` and `finuance-observability` prune); the Argo CD apps `finuance-secrets`, `rustfs`, `kube-prometheus-stack` and `finuance-observability` read them from there (`platform` allows the repository); `tools/seal-secrets.sh` writes there (the folder is checked only where a file is written, so CI's round trip needs none); `make check-sealed` checks both repositories; the folders and their READMEs removed here, `deploy/environments/homelab/README.md` and RUNBOOK "Secrets" explain the flow; THREAT-MODEL I10, ADR 0023 addendum. Next: the merge, every Argo CD app `Synced`/`Healthy` with the Secrets unchanged, then the rotation (**after** the merge: until then sealing writes into this repository)
- [ ] 22.14 the web image build fails clearly when a native optional package is missing
- [ ] 22.15 accepted findings revisited: every entry of `.trivyignore` fixed or justified again; the daily scan warns before an entry expires; CI lists the entries in its summary
- [ ] Gate (the maintainer): the settings checked by hand, the scan section read, one Renovate pull request merged

Phase 21 — Going public (closed)

- [x] 21.0 regrouping: the public release comes first with a small scope
- [x] 21.1 synthetic bank fixtures replace the anonymised real exports
- [x] 21.2 details that describe one person or machine replaced; `check-private.sh` gains the e-mail, tailnet and postcode rules
- [x] 21.3 documents: neutral wording, this lean PROGRESS and `docs/ROADMAP.md`; SECURITY.md's support policy
- [x] 21.4 security hotfixes: fail-closed defaults, profiling behind `FINUANCE_PPROF`, empty passwords in the production env template, `compose.prod.yaml` hardened, the pod-kill script checks the certificate
- [x] 21.5 README rewrite; `docs/features.md`, `docs/deploying.md`; CONTRIBUTING; issue templates
- [x] 21.6 `tools/publish-public.sh` with `--history`, tested, as the alternative that was not needed
- [x] 21.7 the switch: the repository is public (the maintainer, reported 2026-11-16). Its checklist (stale branches, rulesets, secret scanning, private vulnerability reporting, Actions settings, public packages) is verified in 22.1; on 2026-11-16 the 45 old branches still existed
- [x] Gate: the pull request was merged with every CI job green; the nightly, weekly and rehearsal runs on `main` have been green since

## Next phases

In `docs/PLAN.md` §9, each with its findings, decisions, tasks, acceptance criteria and gate.

- **Phase 23 — Security II: runtime, cluster and privacy hardening**: images by digest with signatures enforced; trace context and baggage from clients not trusted; no user text in spans, the nginx log or the database's slow-query log; the cloud transport guard; limits at the ingress; two-step sign-in (TOTP, approved 2026-11-17); the rest of the private register (23.10); THREAT-MODEL v3; the full scan with nothing ignored; `v0.5.0`.
- **Phase 24 — Observability**: panics counted; trace ids on every log line; Loki and Alloy; complete traces; the missing metrics; the trusted marker for synthetic traffic.
- **Phase 25 — The traffic simulator**: ten animals on a lab instance, a separate service and image with its own tuning page; `v0.6.0`.
- **Phase 26 — Cloud AI**: a Vertex AI connection with short-lived tokens, on synthetic data; `v0.7.0`.
- **Phase 27 — Google Cloud lab**: Terraform and GKE Autopilot, up and down on demand.
- **Phase 28 — New interface and phone**: several design directions and prototypes first, then the build; `v0.8.0`.
- **Phase 29 — Release 1.0**: the demo host, write-ups, screenshots, `v1.0.0`.

## Deviations from the plan (with reason)

- Phase 21 (2026-11-09): `docs/PLAN.md` stays in the repository (about 210 files refer to its sections) instead of being archived; only its wording was made neutral.
- 21.2: `192.168.1.20` stays as the example address of a model server on a home network (it has to be a private address for the dial guard's tests, and it names no one). The time zone (`Europe/Warsaw`) stays too: the product is built around Polish bank exports.
- 21.4: actions and images were not pinned by digest by hand; the task relied on Renovate's pinning pull request, which needs the app installed. Phase 22 tasks 22.3 and 22.4 do both. The Argo CD applications keep the overlay repository's real URL, because the cluster reads them from git (a private repository's name reveals nothing in it).
- 21.7 (2026-11-10, the maintainer): no new repository. Every branch and the whole history were searched; the maintainer's e-mail address is in the author field of 9 merge commits, which they accept (the account has two-factor authentication and a passkey), and the anonymised fixtures removed in 21.1 remain in the history, which they also accept (2026-11-17: not real data as they stand). The existing repository keeps its pull requests, releases and CI history, which a new one would lose. `tools/publish-public.sh` stays as the tested alternative.
- Phase 17, found 2026-11-16: task 17.1 planned Loki and Alloy for logs; neither was deployed and nothing recorded it. Logs are on the containers' standard output only. Phase 24 task 24.6 builds it.
- Rev 5 (2026-11-16): the old Phase 22 (Phone and touch) and Phase 23 (Release 1.0) are now Phases 28 and 29, and the security tasks of the old Phase 23 moved forward into Phases 22 and 23.

## Follow-ups (must be done in a later phase)

- **Every phase, first task:** read `.trivyignore`. For each entry, check whether a fixed version exists now and apply it; remove the entry. Today: `picomatch` 4.0.7 (CVE-2026-112366, accepted 2026-11-18 until 2026-12-02; `docs/security-scan.md`).
- **Phase 22:** everything in the findings table of PLAN Phase 22 except its first row: Go 1.27.4 is on `main` since 2026-11-17 and `govulncheck` passes again. Images released before that (`v0.4.1` and earlier) are still built with Go 1.27.3 until `v0.4.2`.
- **The intermittent accessibility check in `web/e2e/admin/admin.spec.ts`** ("the Developer section is for administrators only", `color-contrast` on `.toast`, light pass): explained on 2026-11-18 by the diagnostics added the day before. axe measured the toast half-way through the theme switch's colour fade (`#7f7f83` on `#8c8c8e`) although no animation was running when the wait returned; right afterwards the toast was settled and passes. `web/e2e/axe.ts` now waits for five consecutive frames without an animation. If it fails again, the diagnostics are still there: read them, do not re-run it away.
- **Heartbeat outside the machine (24.9):** the maintainer chooses the service; options were listed on 2026-11-17.
- **Left from the repository review of 2026-11-17** (the rest was fixed on 2026-11-18): the three Helm charts live in two places (`deploy/helm/finuance`, `deploy/k8s/db`, `deploy/k8s/observability`); `web/src/shared` means shared groups and `web/src/legacy` the migration wizard; `scripts/` and `tools/` have no stated rule; PLAN §3.3 and its Appendices A and F have drifted from the tree; the unused `todo` macro in the `Makefile`; `web/CLAUDE.md` asks for visual parity with the legacy app, which Phase 28 will change. Renames wait for a phase that touches those files. The RUNBOOK and ADR 0022 still describe the reference environment's software in detail; it is the project's install guide, kept on purpose.
- **Phase 23:** the full security scan with nothing ignored; cosign verification in the cluster and deploy by digest; Argo CD sign-in through an identity provider as soon as its UI is reachable beyond a port-forward.
- **Phase 22 (22.10) or 24:** Node 26 LTS before Node 22's end of life (2027-04-30): CI, `Dockerfile.web`, `mise.toml`, and the two upload tests that fail there.
- **Phase 24:** `docs/ARCHITECTURE.md` still describes the system "after Phase 11"; bring it up to date with `docs/COMPONENTS.md` as its map.

## Backlog (ideas, not scheduled)

Reviewed on 2026-11-16. Scheduled items moved into a phase; the rest stay here with a note.

Scheduled by Rev 5
- A `profiling` value in the Helm chart for `FINUANCE_PPROF` → 24.9.
- An alert path that survives an outage of the machine itself → 24.9 (a heartbeat outside the machine; the maintainer chooses the service).
- Compose: nginx re-resolves the api upstream after the api container is recreated → 25.8.
- Overview under load: cache the month aggregates → decided on the numbers of 25.12.
- The api and worker retry their first database connection instead of restarting once → 27.10.
- Evals on the model server actually used and on candidate models → 26.7.
- A thinking toggle next to the question field; the duplicates window's first render of many groups; a cheaper first render for the Vitest files that time out → Phase 28.

Still ideas
- `fin mcp`: the CLI as an MCP server over stdio. Worth a decision after Phase 26: it needs either a library or a hand-written JSON-RPC layer.
- Per-user model API keys. More interesting once a cloud model exists (Phase 26): each person would pay for and consent to their own provider.
- Passkeys (WebAuthn) after the one-time-code second factor of Phase 23; needs a library.
- Composite foreign keys on `(user_id, id)`, so the schema itself refuses a reference to another user's row (considered in Phase 23 and left out).
- One `users.kind` column instead of `is_demo` plus `kind` (after Phase 24).
- A cloud model for the instance with real data: a token source from a cluster outside Google, stronger masking (after Phase 26, a separate decision).
- FX conversion with central-bank rates; the assistant names the category of a suggested rule.
- Restore a data export into a new instance; per-month budgets with rollover; balance reconciliation against the bank's balance column.
- Interface languages (Polish): decided in Phase 28, stage A.

## Session log

- 2026-11-09 — Audit before going public (private data, security, first impression): no credentials and no serious flaw, but the history and a few files must not be published. Phase 21 regrouped to "going public" and 21.0–21.4 done: synthetic fixtures only, neutral wording, new guard rules, fail-closed defaults and the other security hotfixes. Then the README rewritten for readers new to the project (21.5) and the publish script with its tests (21.6). Next: the move (21.7), by the maintainer.
- 2026-11-10 — The publish script learned `--history`; then a search of every branch showed one private value only (a commit author address), so the plan changed to the simple one: this repository becomes public as it is. RUNBOOK section rewritten. Next: the pull request into `main`, then the switch (21.7).
- 2026-11-16 — Planning session, no code changed. An audit of the public repository: a scan of every pinned version, the logging and tracing code (good metrics and traces, weak logs, a panic that is not counted), the deployment and assistant layers (what a second kind of cluster and a cloud model need), the API surface for a simulator, and the backlog. With the maintainer's four decisions this became PLAN Rev 5: Phases 22–29, security first. `docs/COMPONENTS.md` added as the component map. Next: `/phase-start 22`.
- 2026-11-17 — The maintainer approved two-step sign-in (23.7) and `golang.org/x/oauth2` (26.0); both recorded in the plan. The plan goes to `main` as a pull request. Still open: the backup target outside the machine, the browser-error endpoint (24.8) and the heartbeat service (24.9). The plan's pull request failed at `govulncheck` exactly as the audit predicted, so the Go 1.27.4 bump went ahead on its own (`fix/go-1.27.4`, pull request 34, merged the same day) and `main` was merged into the plan's branch. The plan's pull request then failed twice in the E2E job on the accessibility check described under Follow-ups; diagnostics were added to the axe helper, and the next run was green.
- 2026-11-17 (later) — A review of the repository's structure and of what it shows in public. An unmerged branch with a detailed security review was removed from the public repository; the review and the detailed wording of this plan's security sections are now in the private environment repository, and Phases 22 and 23 cite the register's ids (tasks 22.12 and 23.10). Decisions recorded above. A second unrelated CI failure (the web image's install skipped a native package) passed on its one re-run and became task 22.14.
- 2026-11-18 — The plan was merged. Documentation fixes from the repository review: phase numbers after the renumbering (ADRs 0022–0024, THREAT-MODEL, security-scan, RUNBOOK, ARCHITECTURE, the admission policy), update notes on ADRs 0020, 0022 and 0024, an ADR index, the PLAN header, contents and a note on the superseded Phase 21 section, agent instruction references, the README's roadmap line, the docs index, and `docs/RUNBOOK.md` "Connecting backup storage outside the machine". On the same day a new advisory without a fix (`picomatch`, development tools) was accepted with an expiry and the rule "fixed, not ignored" recorded (pull request 36), and the intermittent accessibility check was explained and fixed.
- 2026-11-18 (later) — Task 22.13 taken ahead of the phase start (the maintainer): an audit of both repositories for credentials and details of the environment (this repository's tracked files are clean), then the homelab's SealedSecrets and public certificate moved to `finuance-env`, the Argo CD sources switched and sealing pointed there. The private repository's README became one list of what to fix on the homelab (the controller key backup off the machine and its 30-day key renewals, the rotation order, the pull token's expiry). Next: push `finuance-env`, then the pull request and the merge on the maintainer's OK, then the rotation.
