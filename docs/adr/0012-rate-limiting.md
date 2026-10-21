# 0012. Rate limiting: in-memory token buckets per replica

- Status: accepted
- Date: 2026-10-21

## Context

Login and signup are the brute-force and credential-stuffing surface (PLAN §8.1). Uploads (Phase 6) and assistant runs (Phase 13) are expensive. PLAN §6.1 sets the limits:
- login and signup: 10/min per (IP, email) and 30/min per IP;
- uploads: 30/min per user.

Finuance runs as one or two api replicas on a homelab or a small host. There is no Redis.

## Decision

- **Mechanism:** token buckets from `golang.org/x/time/rate`, one per key, held in memory by `middleware.Limiter`. A bucket allows `Events` per `Per` with bursts up to `Events`.
- **Rejected requests** get `429 rate_limited` with `Retry-After` (whole seconds until the next token) and a problem+json body. They don't consume a token.
- **Policies:**

  | Policy | Key | Limit |
  |---|---|---|
  | `LoginPerIP` | client IP | 30/min (signup + login) |
  | `LoginPerIPEmail` | client IP + lower-cased email | 10/min |
  | `PasswordPerUser` | user id | 10/min (password change, account deletion: both verify a password) |
  | `UploadPerUser` | user id | 30/min (Phase 6) |

- **Order of checks:** both login limits are checked before any argon2 work, so a flood costs no hashing.
- **Client IP:** comes from `RealIP`. `X-Forwarded-For` counts only when the direct peer is in `FINUANCE_TRUSTED_PROXIES`, so clients can't choose their own key.
- **Memory:** at most 100,000 buckets per limiter. When full, buckets idle for a whole period are evicted (they are full again, so forgetting them changes nothing). If that frees nothing, the map is reset. Under a flood of distinct keys, starting everyone over is better than refusing legitimate users.
- **Clock:** the limiter takes `platform.Clock`, so tests drive time.

## Consequences

- **Limits are per replica.** With N replicas an attacker gets up to N× the budget. That is acceptable for N ≤ 2 combined with argon2id. If Finuance ever runs many replicas, move the buckets to Postgres or Redis (a new ADR).
- **Restarts reset all buckets.** That is harmless at these limits.
- **Shared IPs:** users behind one NAT (a household) share the per-IP budget. 30/min is far above what humans type.

## Alternatives considered

- **Postgres-backed counters:** shared across replicas, but a write per attempt on the hot path, plus cleanup.
- **Redis:** new infrastructure for a homelab app.
- **Fixed windows:** bursty at window edges. Token buckets are smoother and `x/time/rate` is well tested.
- **Ingress-level limits (nginx, Traefik):** useful as an outer layer, but they can't key on the email in the body.
