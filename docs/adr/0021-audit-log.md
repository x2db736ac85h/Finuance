# 0021. Audit log (Settings → Activity)

- Status: accepted
- Date: 2026-10-26

## Context

PLAN 17.6 and THREAT-MODEL R1 ask for a record of security-relevant actions, so a user can see
who signed in, from where, and what was exported or deleted. It must be trustworthy: the app
itself (or a bug in it) should not be able to rewrite or erase recent history. Our Product
Engineer chose a retention of one year with the full client address (2026-10-26). Applied
assistant proposals (ADR 0019) were waiting for it.

## Decision

- One table, `audit_log` (migration 00010): `user_id`, `at` (the database's clock), `action`
  (`area.verb`), an optional `entity_id`, `ip`, a short browser name ("Firefox on Linux", not the
  User-Agent) and a small `meta` object (file names, counts, kinds; never transaction fields).
- **Append-only for the app role.** `UPDATE` and `TRUNCATE` are revoked from `finuance_app`.
  Row level security is forced with one policy per command: read and insert own rows; the insert
  policy only accepts `at` within a minute of `now()` (no backdating); delete only own rows older
  than 365 days. There is no update policy, so even the owner role can't change a row.
- Events: sign-in, failed sign-in (only for an existing account; an unknown email has no log to
  write to, and the response and timing are unchanged), sign-out, password change, session
  revoked, import committed (inside the commit transaction, by the job: no address), import and
  account deleted, CSV and data export, rules applied, legacy migration started, duplicate
  decisions, applied assistant proposals. The handlers record after the change succeeded; a
  failed record is logged and doesn't fail the request.
- A daily River job (`cleanup_audit_log`) deletes expired rows, one user at a time under that
  user's RLS context. Deleting the account deletes the rows (`ON DELETE CASCADE`).
- `GET /me/activity` pages newest first (`before` = an event id); the data export has
  `activity.json`; the OpenAPI enum of actions equals `ledger.AuditActions` (test).

## Consequences (positive, negative, follow-ups)

- A compromised or buggy app can add misleading events but can't hide recent ones; only a
  database superuser can (backups keep their own copy).
- Failed sign-ins to a known email show up for that user, so a password-guessing attempt is
  visible; the login rate limits bound how many rows it can create.
- Addresses are personal data: kept a year, shown only to the user, exported with their data,
  deleted with the account (THREAT-MODEL I-rows updated).
- Most events are recorded right after the change rather than in its transaction (only the
  import commit is in-transaction); a crash between the two loses an event, never a change.
- Follow-up: admin-side views or alerts on failed sign-ins (not planned).

## Alternatives considered

- Log lines only (as Phase 15 did): not visible to the user, and log retention is an operator
  setting, not a promise.
- Recording every event in the change's transaction: needs the audit call inside each ledger
  method (accounts, imports, exports, rules, duplicates, auth); the handler helper keeps the
  domain code unchanged, and the one event made by a job is in its transaction.
- A separate audit database or append-only file: stronger against a superuser, but a second
  store to back up and restore; out of scope for a self-hosted single-node app.
- Storing the full User-Agent: a fingerprinting aid with little extra use for the user.
