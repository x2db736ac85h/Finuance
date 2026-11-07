# 0036. Personal access tokens: bearer tokens with a read or write scope, never for the sensitive routes

- Status: accepted
- Date: 2026-11-06

## Context

Phase 20 adds the `fin` command line and lets people script their own imports and reports
(an n8n workflow posting statements, a nightly export). The API knows only browser sessions:
an HttpOnly cookie plus a CSRF header (ADR 0011). A script needs a credential it can hold,
send without a browser, scope to what it needs, and lose without losing the account.

## Decision

- **`access_tokens`** (migration 00023): like `sessions`, outside RLS, because a token is resolved
  before its user is known; every query but the lookup filters by `user_id`. Only the SHA-256 of the
  token is stored, with its first 12 characters (`fin_` and 8 more) to recognise it in the list. A
  token is `fin_` plus 32 random bytes in base64url, shown once when it is made.
- **Sent as `Authorization: Bearer fin_…`**. With that header the session layer ignores cookies,
  and the CSRF check doesn't apply (a browser never adds the header by itself, and a cross-site
  page can't set it without CORS, which the API doesn't allow).
- **Two scopes**: `read` reaches GET routes and a reviewed list of read-only POSTs (the transaction
  query, distinct values, ids and export, the rule preview, the insights and the overview);
  `write` reaches everything else that changes data. **No token** reaches the admin routes,
  sign-in, sign-up, sign-out, the password, sessions, tokens, or deleting the account (403). A
  route's class comes from rules in `httpapi/tokens.go`; `TestTokenRoutePolicy` writes every
  route's class to a golden file, so a new route's class is reviewed in its pull request before any
  token can use it, and fails if an admin, auth, session, token or password route is ever reachable.
- **Lifetime**: 1 to 366 days, chosen when made (7, 30, 90 days or a year in the UI); at most 25
  live tokens per user. Revoking is immediate. A new password, an administrator's reset or *Sign
  out everywhere*, and disabling the account end every token. Revoked and expired tokens stay
  listed for 30 days, then the hourly cleanup deletes them.
- **Limits**: 600 requests per minute per token, on top of the user's own limits (uploads,
  assistant questions, the daily token budget). Last use is recorded once a minute.
- Making and revoking a token are in the user's Activity (`auth.token_created` with its name,
  scope and prefix; `auth.token_revoked`), never the token.

## Consequences (positive, negative, follow-ups)

- Scripts and the CLI work without a browser session and without the account's password; a
  leaked read token can read everything the account can (including the data export), but can't
  change anything or take over the account.
- A token is as strong as where it is kept: the CLI stores it in a file only its user can read.
- The golden file adds a small review step to every new route.
- Follow-ups: an MCP server on top of the CLI (`fin mcp`, Backlog).

## Alternatives considered

- **OAuth 2 device flow**: right for third-party apps; heavy for one user's own scripts on a
  self-hosted instance.
- **Per-route scopes**: more precise, but every new route would need a scope decision and the UI a
  long checklist; two scopes plus the never list cover the uses seen.
- **Tokens under RLS**: the lookup happens before the user is known, as with sessions.
