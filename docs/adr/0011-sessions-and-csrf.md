# 0011. Server-side sessions in an HttpOnly cookie; CSRF by custom header + Origin check

- Status: accepted
- Date: 2026-10-21

## Context

The web app and the API share one origin (PLAN §3.1). Users sign in with email and password and stay signed in for weeks. Requirements from PLAN §6.1 and §8.1:
- a revocable session, with a list of sessions in the UI;
- idle and absolute expiry;
- rotation on login and on password change;
- nothing usable if the database leaks;
- protection against cross-site request forgery without a token round-trip.

## Decision

**Sessions**
- **Token:** 32 random bytes from `crypto/rand`, sent as `finuance_session=<base64url>`. The cookie is `HttpOnly; SameSite=Lax; Path=/; Max-Age=<FINUANCE_SESSION_TTL>`. It is `Secure` unless `FINUANCE_COOKIE_SECURE=false`, which config allows only in dev and test.
- **Storage:** the `sessions` table holds only `SHA-256(token)`, never the token. A leaked table can't be replayed as cookies.
- **Expiry:** `expires_at` is the absolute expiry (login + `FINUANCE_SESSION_TTL`, default 30 days). `last_seen_at` drives the idle expiry (`FINUANCE_SESSION_IDLE_TTL`, default 7 days). A session must pass both. `last_seen_at` is written at most every 5 minutes, so an active session doesn't write on every request.
- **Rotation:**
  - Login always creates a new session and deletes the one the browser presented (session fixation).
  - A password change deletes every session of the user, then opens a new one for the current browser.
  - `finuance admin set-password` deletes all of them.
- **Public session id:** the hex of the first 8 bytes of the token hash. It addresses one session in `GET`/`DELETE /me/sessions`, but it is useless as a credential.
- **Cleanup:** an hourly River job deletes expired sessions.
- **Request flow:** a per-route middleware (`requireSession`) resolves the cookie. Routes not on the public list (`/meta`, signup, login, logout) answer `401` without a valid session. The access log records only a hash of the user id.

**CSRF**
- Every request with a method other than GET, HEAD or OPTIONS must carry `X-Finuance-Client: web`. A request that sends an `Origin` header must send exactly `FINUANCE_PUBLIC_ORIGIN`. Otherwise the answer is `403 forbidden`.
- A cross-site HTML form can't set custom headers. Cross-origin JavaScript would need a CORS preflight, which the API never grants. `SameSite=Lax` already keeps the cookie off cross-site POSTs. The header is the second layer, and the Origin check is the third.
- Login and signup are covered too (login CSRF).

## Consequences

- Sessions are revocable instantly (logout, the sessions list, a password change), unlike stateless tokens.
- Each authenticated request costs one indexed lookup by primary key, plus at most one write every 5 minutes.
- Non-browser clients (scripts, the smoke test) must send the header. That is documented in the OpenAPI security scheme.
- The cookie carries no user data. Signing out everywhere is one `DELETE`.

## Alternatives considered

- **JWT in a cookie or `Authorization` header:** not revocable without a denylist, which is a session table by another name. It is also larger, and easy to get wrong.
- **Synchronizer CSRF tokens:** they need a token endpoint and per-form state. With same-origin, SameSite and a custom header they add nothing.
- **Storing the raw token:** a database leak would hand out live sessions.
