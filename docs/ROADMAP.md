# Roadmap

What has shipped, what is being worked on and what comes later. The task list of the current phase is in [PROGRESS.md](PROGRESS.md); release notes are in the [CHANGELOG](../CHANGELOG.md).

## Shipped

| Version | What it brought |
|---|---|
| **0.1.0** | Everything the single-file prototype could do, as a multi-user web application: CSV import with detection and preview, one history across overlapping exports, the transaction table with filters, groups, duplicate review, insight charts, the monthly overview, the assistant on a local model, and migration from the prototype's backups |
| **0.2.0** | Category management, rules and transfer detection; group reports; the Kubernetes deployment (Helm, CloudNativePG, Argo CD, Sealed Secrets) with a rehearsed restore; dashboards, SLOs and alerts; an activity log; demo mode |
| **0.3.0** | Administration without access to financial data; colour themes and a theme editor; cash accounts; friends and shared groups with splits and settling up |
| **0.4.0** | The assistant proposes changes you can apply and undo; goals; receipts by photo; digests; an advice library; personal access tokens and the `fin` command line |
| **0.4.1** | Administrators switch model servers on and off, load and unload models and choose who picks the model |
| **0.4.2** | Security I: every dependency patched and pinned, PostgreSQL 18.7; limits on imports, amounts and the database work a request may cause; CodeQL, dependency review, OpenSSF Scorecard and a daily scan of the released images; releases approved by the maintainer, with build provenance |
| **0.5.0** | Security II: images deployed by digest and admitted by their build provenance; telemetry that can't carry private data; a guarded transport for cloud models; limits at the ingress; two-step sign-in; a stricter default deny in the cluster; threat model v3 |
| **0.5.1** | Observability: logs joined to traces and collected (Loki and Alloy), so one request can be followed from the browser to its background jobs; panics, model servers, refused requests and errors in the browser counted; synthetic traffic labelled by the server |
| **0.6.0** | A traffic simulator: ten animal-named synthetic users with their own characters use every feature of a lab instance all day through its front door, tuned from a page of their own; a lab on one machine (`make sim-up`) and on Kubernetes; the load test's users synthetic too |
| **0.6.1** | Interface corrections: readable addresses (`/transactions`, `/overview`, `/shared`), Settings regrouped, quick settings with a dark-mode switch, categories archived and unarchived instead of deleted, friends on the Shared page, the assistant's answers in the bell, a half-window layout check; connections added by choosing a provider |

The repository became public in November 2026 with synthetic test data only and a README for people who are new to the project.

## Now

**A cloud model for the assistant (0.7.0).** Anthropic, OpenAI and Google's Vertex AI (short-lived credentials, no stored key) as connections an administrator picks from a list; a local model stays the default. The Vertex AI path is tested against a mock only so far: its first run against Google is in the backlog.

## Next

- **A lab on Google Cloud.** Terraform and GKE Autopilot, created and destroyed on demand, with the simulator as its traffic.

## Later

- **A new interface that works on a phone (0.8.0).** Several design directions and prototypes first, then the build: a bottom tab bar, a two-line transaction list, windows that fit small screens, installing the app on a home screen.
- **1.0.** A public demo with made-up data, and write-ups of the parts worth explaining: the import pipeline and de-duplication, row-level security, the assistant's tools and undo, running it like production on one machine, synthetic users, a cluster you can delete.

**Ideas without a date** are in the backlog at the end of [PROGRESS.md](PROGRESS.md): currency conversion, per-month budgets with rollover, restoring a data export into a new instance, the CLI as an MCP server, passkeys.
