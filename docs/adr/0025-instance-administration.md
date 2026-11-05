# 0025. Instance administration without access to financial data

- Status: accepted
- Date: 2026-11-03

## Context

Until Phase 19 an instance had no administrators: accounts were made with `finuance admin
create-user`, and everything else (limits, which parts of the app exist, how a new user's view
looks) was environment configuration or each user's own preference. Phase 19 adds an admin panel
(PLAN §9 Phase 19, 19.1–19.3): manage accounts, set limits, choose defaults and switch features
off. The instance holds people's financial history, so the panel must not become a way to read it:
an administrator runs the instance, not the users' money.

Two facts shaped the design:

- `users` and `sessions` were already outside row-level security (`00003_rls.sql`); only the
  auth package reaches them, through `store.WithoutUser`.
- RLS is forced on every user-owned table (ADR 0007), which binds the table owner too. A
  `SECURITY DEFINER` function owned by `finuance_owner` would therefore see no rows, and the
  approved plan's idea of counting usage through one would have counted nothing.

## Decision

- **Roles.** `users.is_admin`; the first administrator comes from the CLI
  (`finuance admin create-user -admin`, `grant-admin`, `revoke-admin`), later ones from the panel.
  The last active administrator can't be demoted, disabled or deleted (`409 last_admin`, also when
  they delete their own account); demo users are never administrators.
- **Where the data lives.** Everything the panel manages is instance-level and outside RLS like
  `users`: `users.is_admin`, `disabled_at`, `must_change_password`, `limits` (per-user overrides),
  `instance_settings` (one versioned document) and `themes` (ADR 0026). Migration 00012.
- **No financial data, by construction.** The `internal/admin` package uses `store.WithoutUser`
  for those tables and may not import the packages that read financial data (`ledger`, `query`,
  `insights`, `rules`, `assistant`, …; `TestAdminImportsNoFinancialPackage`). Usage is counts from
  one fixed query per user (`UserUsage`, `UserActivityByDay`), run under that user's own context in
  a read-only transaction, the pattern the cleanup jobs already use (`ledger.CleanupAudit`).
  `TestAdminCannotReadFinancialData` checks that no admin response carries financial fields.
- **Routes.** `/api/v1/admin/*` answer `404` to anyone but an administrator (the routes don't
  announce themselves); the role is re-read with the session on every request.
- **Accounts.** Create (an administrator-set password, or a generated one-time password shown once),
  role, disable (signs the account out everywhere; signing in again is `403 account_disabled`,
  only after the right password), reset password (a new one-time password), sign out everywhere,
  per-account limits, delete (the e-mail must be repeated). A one-time password sets
  `must_change_password`: until the user picks their own, every route but `/me`, `/me/password`,
  `/preferences` and sign-out answers `403 password_change_required`.
- **Instance settings.** One document, versioned like preferences (`409 stale_version`):
  - *defaults* the browser lays under each user's saved preferences (colour theme, mode, size,
    charts, insights pane, sidebar, fast path), so only keys a user never saved take them;
  - *features* nobody can override (Overview, Assistant now; Friends and Shared groups in Phase
    19's later parts): a switched-off feature's routes answer `404` and its UI disappears;
  - *limit defaults*.
  Each replica caches the document for 10 s.
- **Limits** resolve user → instance → the environment's bound (`FINUANCE_MAX_UPLOAD_BYTES`,
  `FINUANCE_AI_RATE_PER_HOUR`), so an administrator can only tighten what the server allows. A
  reached limit is `422 limit_reached` with the limit's name and value. Imports per day count the
  last 24 hours.
- **Audit.** Every administrator action is an event in the administrator's Activity (with the
  account's e-mail) and in the affected account's Activity with `{"by": "administrator"}`, which
  says that an administrator acted but not which one.

## Addendum (2026-11-05): the Developer section

Administrators of an instance in **developer mode** (`FINUANCE_DEVELOPER_MODE`, chart
`developer.enabled`) get a *Developer* section: the build (version, commit, time, Go, environment,
the answering replica, schema version), the source repository and links to the tools around the
instance with notes on how to get in (docs/admin.md). Decisions:

- **Configuration, not data.** The links come from a file the operator writes (the chart renders
  it from values), read and validated at start like the assistant's connections file; nothing is
  stored in the database or editable in the panel, so the panel can't be used to plant a link.
- **Hidden like every admin route, and then some.** `GET /admin/developer` answers `404` to
  non-administrators (the gate) and to administrators while the mode is off, so only an
  administrator of an instance with the mode on learns it exists. `/meta` and `/me` don't carry the
  flag; the web app asks only for administrators and shows the section only when that answer comes.
  The bundle holds no URL or note.
- **No secrets.** Links are `http`/`https` without user info (no `javascript:` or `data:`, no
  `user:password@`), text fields are plain single-line or multi-line text with length limits, and
  notes say how to fetch a credential (typically a `kubectl` command, which needs cluster access)
  rather than holding one. The schema of the chart and the api both check it.
- **Not on a public demo**: developer mode with demo mode is a configuration error.
- Viewing it is not audited: it changes nothing, like the other admin reads.

## Consequences (positive, negative, follow-ups)

- Running an instance for several people needs no shell access after the first administrator.
- What keeps administrators from financial data is the API and the package boundary, not the
  database: the app role can set any user's context, as it always could (RLS guards against query
  mistakes, not a compromised server). The THREAT-MODEL says so.
- Usage of a large instance costs one transaction per user; fine for homelab sizes, and the
  instance page reads 30 days at most.
- Settings changes reach other replicas within 10 s.
- Follow-ups: the friends and shared-groups switches and limits arrive with PR 2 and PR 3; the
  assistant's connections, tuning and token budgets join the panel in Phase 20.

## Alternatives considered

- **Admin policies on user tables, or a `BYPASSRLS` role behind a definer function**: a database
  role that reads every row is exactly what the panel must not need; it would also need superuser
  bootstrap changes on every deployment.
- **Counters maintained by triggers**: correct and fast to read, but a trigger on every
  transaction insert slows the bulk imports the load tests tuned.
- **Settings in environment variables only**: no restart-free changes and no per-user limits.
