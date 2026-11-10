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

## Now

**Going public.** Synthetic test data only, a README for people who are new to the project, and the move to a public repository.

## Next

**Phone and touch (0.5.0).** A bottom tab bar, a two-line transaction list, windows that fit small screens, and installing the app on a phone's home screen.

## Later

**1.0.**
- More security checks in CI: CodeQL, dependency review, workflow linting, OpenSSF Scorecard.
- Only signed images run in the cluster, deployed by digest.
- A public demo with made-up data.
- Write-ups of the parts worth explaining: the import pipeline and de-duplication, row-level security, the assistant's tools and undo, running it like production on one machine.

**Ideas without a date** are in the backlog at the end of [PROGRESS.md](PROGRESS.md): currency conversion, per-month budgets with rollover, restoring a data export into a new instance, the CLI as an MCP server.
