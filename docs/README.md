# Documentation

## For people who want to use or understand Finuance

| Document | What it covers |
|---|---|
| [Main README](../README.md) | What Finuance does, screenshots, key concepts, quick start, deployment, status |
| [ARCHITECTURE.md](ARCHITECTURE.md) | Components, backend packages, data model (ERD), row-level security, request flow, the web app |
| [RUNBOOK.md](RUNBOOK.md) | Running it on Kubernetes: bootstrap (also on single-node k3s, step by step: TLS, GitOps, monitoring, scans, image admission), deploy, rollback, failed migrations, secrets, scaling, backups and restores, drills, playbooks |
| [THREAT-MODEL.md](THREAT-MODEL.md) | Assets, attackers, threats and the controls (with the tests that prove them), and the security scans |
| [security-scan.md](security-scan.md) | The latest results of every security scan (Trivy, npm audit, ZAP, OpenVAS, …) and the decision on each finding |
| [SLO.md](SLO.md) | The service level objectives, how they are measured and alerted on, and the error budget policy |
| [adr/](adr/) | Architecture decision records: why Go + PostgreSQL, money in minor units, row-level security, sessions and CSRF, the CSV export format, … |
| [../api/openapi.yaml](../api/openapi.yaml) | The HTTP API, specification first (OpenAPI 3) |
| [assistant.md](assistant.md) | Setting up the assistant: model servers (local and cloud), capabilities (images included), limits and the daily token budget, retention, receipts, digests, the advice library, the probe and the evals |
| [migration.md](migration.md) | Moving a backup of the single-file app into Finuance |
| [admin.md](admin.md) | Administrators: the first one, the panel (accounts, limits, defaults and features, colour themes, the assistant's connections, tuning and advice library, usage) |
| [cli.md](cli.md) | The `fin` command line: installing it, signing in with an access token, the commands and their output |
| [sharing.md](sharing.md) | Shared groups: sharing a trip with friends, splits, balances and settling up, the Shared tab, what the others see |
| [evals.md](evals.md) | Assistant quality per model, thinking mode and toolset on the golden questions, and per model on the receipt set |
| [perf.md](perf.md) | Performance measurements: parsing, staging, queries, the table, charts and overview with 100k rows, the bundle, the load tests (Compose and the reference cluster) |
| [a11y.md](a11y.md) | Accessibility: what axe checks on every screen, what was fixed, the keyboard-only checklist |
| [parity-report.md](parity-report.md) | The legacy app and Finuance on the same data, number by number (written by the parity test) |

## Working documents (how the rebuild is planned and tracked)

These are written for the maintainer and the AI agent that writes most of the code. They are detailed, and they describe future phases as well as finished ones.

| Document | What it covers |
|---|---|
| [PLAN.md](PLAN.md) | The full specification and the phase plan, with tasks, tests and the review gate of each phase |
| [PROGRESS.md](PROGRESS.md) | The current phase and its tasks, the decisions that still apply, follow-ups and the backlog |
| [ROADMAP.md](ROADMAP.md) | What has shipped, what is next and what comes later |
| [PARITY.md](PARITY.md) | Every capability of the legacy prototype and the phase that ports (or retires) it |
| [perf-phase7-plans.md](perf-phase7-plans.md) | Raw `EXPLAIN ANALYZE` output behind the Phase 7 query measurements |
| [../testdata/oracle/ALLOWED_DIFFS.md](../testdata/oracle/ALLOWED_DIFFS.md) | The deliberate differences from the legacy prototype's behaviour |
| [../legacy/DESIGN.md](../legacy/DESIGN.md) | Design notes of the single-file prototype that Finuance replaces |

## Screenshots

[`screenshots/`](screenshots/) holds the README screenshots (made-up demo data, dark and light) and the Grafana dashboards during the load tests (`screenshots/grafana/`, `screenshots/grafana/homelab/`).
