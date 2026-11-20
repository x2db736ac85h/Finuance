# 0040. Two-step sign-in with one-time codes (TOTP)

- Status: accepted
- Date: 2026-11-20

## Context

Signing in needed a password only. THREAT-MODEL listed "no MFA" as accepted while the repository
was private and instances were personal; with a public repository and instances that may face the
internet it was the largest remaining product gap (PLAN Phase 23, task 23.7; approved by the
maintainer on 2026-11-17). The project takes no dependency without a reason, sends no e-mail and
no text messages, and has an instance key for sealing secrets in the database (ADR 0035).

## Decision

- **Time-based one-time passwords (RFC 6238)** as every authenticator app implements them:
  HMAC-SHA1, six digits, 30-second steps. The algorithm is a dozen lines over the standard library
  (`auth/totp.go`); the RFC's test vectors are its tests. One step of clock drift either way is
  accepted, and a time step is never accepted twice (the last accepted step is stored), so a code
  that was seen can't be used again.
- **Storage**, outside row-level security like sessions (the second step runs before a session
  exists; every statement names the user): the secret sealed with the instance key, the user id
  part of the seal (`user_second_factors`); ten **recovery codes** of 50 random bits each, stored
  as SHA-256 hashes, each usable once (`recovery_codes`); and the state between the two steps
  (`login_challenges`). Without an instance key two-step sign-in can't be set up.
- **Signing in:** the password is checked as before. For an account with a confirmed second factor
  the answer is `202` with a **challenge** (a random token for that browser, five minutes, five
  attempts) instead of a session; `POST /auth/login/second-step` answers it with a code from the
  app or a recovery code. A limiter per user sits in front (ten codes in ten minutes). A failed
  second step is an event in the user's activity log: it means the password is known to someone.
- **Setting up** (Settings → Security, session only): start returns the secret once, as text and
  as an `otpauth://` address shown as a QR code; nothing changes until a code from the app confirms
  it. Confirming ends the account's other sessions and shows the recovery codes once. Switching
  off needs the password and a code. New recovery codes need a code from the app.
- **The QR code** is drawn by an encoder written here (`web/src/lib/qr.ts`: byte mode, level M,
  versions 1 to 15), not by a library. Its tests compare its symbols with libqrencode's module for
  module, for every version range and each of the eight masks.
- **Administrators:** an instance setting requires two-step sign-in of administrators. One who
  hasn't set it up can still sign in and use their own data, but every `/admin` route answers
  `403 second_factor_required` and the app sends them to Settings → Security. Only an administrator
  who uses two-step sign-in can switch the setting on, so nobody locks every administrator out. An
  administrator can remove another account's second factor (both activity logs record it), never
  their own that way; `finuance admin remove-second-factor` does it on the machine.
- **Access tokens** are unchanged: they are created inside a signed-in session and never reach the
  sign-in or second-factor routes (ADR 0036). **Demo accounts** can't set it up.

## Consequences (positive, negative, follow-ups)

- A password that leaks no longer opens an account that uses two-step sign-in.
- Losing the phone and the recovery codes needs an administrator, or the command on the machine
  for the last administrator. An administrator's reset is as strong as that administrator's own
  account, which is why the instance rule exists.
- **Losing the instance key** makes every stored secret unreadable: affected accounts can't finish
  signing in until their second factor is removed with the command. The key's backup matters more
  than before (RUNBOOK, Secrets).
- A personal access token still opens the data it was made for without a second step; that is its
  purpose, and it can't change the password, the tokens or the second factor.
- SHA-1 appears in the code. HMAC-SHA1 is not affected by SHA-1's collision weakness, and the apps
  people already have implement nothing else by default.
- The encoder is code to maintain. It is small, frozen by its reference tests, and used on one
  screen; a text that doesn't fit shows the key as text only.
- Follow-ups: passkeys (WebAuthn) need a library and stay in the backlog; remembering a browser
  ("don't ask on this device for 30 days") was left out on purpose.

## Alternatives considered

- **Passkeys (WebAuthn) first:** stronger and nicer to use, but a library for the attestation
  formats and a second flow to build; TOTP covers every phone today.
- **Codes by e-mail or text message:** the application sends neither, and both are weaker.
- **A QR library:** a dependency for one screen; and **no QR code**, text only, makes setting up
  from a desktop a matter of typing 32 characters.
- **The session cookie holds a "password ok" state** instead of a challenge in the request body:
  a half-signed-in session is easy to mistake for a session somewhere in the code.
- **Refusing sign-in for an administrator without a second factor:** simpler to state, and one
  misstep away from an instance nobody can administer.
