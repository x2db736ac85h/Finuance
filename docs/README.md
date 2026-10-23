# Documentation

## For people who want to use or understand Finuance

| Document | What it covers |
|---|---|
| [Main README](../README.md) | What Finuance does, screenshots, key concepts, quick start, deployment, status |
| [ARCHITECTURE.md](ARCHITECTURE.md) | Components, backend packages, data model (ERD), row-level security, request flow, the web app |
| [THREAT-MODEL.md](THREAT-MODEL.md) | Assets, attackers, threats and the controls (with the tests that prove them) |
| [adr/](adr/) | Architecture decision records: why Go + PostgreSQL, money in minor units, row-level security, sessions and CSRF, the CSV export format, … |
| [../api/openapi.yaml](../api/openapi.yaml) | The HTTP API, specification first (OpenAPI 3) |
| [assistant.md](assistant.md) | Setting up the assistant: model servers (local and cloud), capabilities, limits, retention, the probe and the evals |
| [migration.md](migration.md) | Moving a backup of the single-file app into Finuance |
| [evals.md](evals.md) | Assistant quality per model on the 46 golden questions |
| [perf.md](perf.md) | Performance measurements: parsing, staging, queries, the table, charts and overview with 100k rows, the bundle |
| [a11y.md](a11y.md) | Accessibility: what axe checks on every screen, what was fixed, the keyboard-only checklist |
| [parity-report.md](parity-report.md) | The legacy app and Finuance on the same data, number by number (written by the parity test) |

## Working documents (how the rebuild is planned and tracked)

These are written for the team and the AI agent that builds Finuance. They are detailed, and they describe future phases as well as finished ones.

| Document | What it covers |
|---|---|
| [PLAN.md](PLAN.md) | The full specification and the 18-phase plan, with tasks, tests and the review gate of each phase |
| [PROGRESS.md](PROGRESS.md) | The current phase, every decision taken by our Product Engineer, deviations from the plan, follow-ups |
| [PARITY.md](PARITY.md) | Every capability of the legacy prototype and the phase that ports (or retires) it |
| [perf-phase7-plans.md](perf-phase7-plans.md) | Raw `EXPLAIN ANALYZE` output behind the Phase 7 query measurements |
| [../testdata/oracle/ALLOWED_DIFFS.md](../testdata/oracle/ALLOWED_DIFFS.md) | The deliberate differences from the legacy prototype's behaviour |
| [../legacy/DESIGN.md](../legacy/DESIGN.md) | Design notes of the single-file prototype that Finuance replaces |

## Screenshots

[`screenshots/`](screenshots/) holds the README screenshots (made-up demo data, dark and light).
