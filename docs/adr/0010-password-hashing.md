# 0010. Password hashing: argon2id in PHC format, OWASP baseline parameters

- Status: accepted
- Date: 2026-10-21

## Context

Finuance stores email + password accounts (PLAN §2, §8.1). If the database leaks, the password hashes must be expensive to brute-force. A login must still be cheap enough for the API and for a small homelab node. The threat model also asks for:
- the same timing for a known and an unknown email;
- a constant-time comparison;
- a way to strengthen the parameters later without forcing password resets.

## Decision

- **Algorithm:** argon2id, from `golang.org/x/crypto/argon2`.
- **Storage format:** the PHC string `$argon2id$v=19$m=<KiB>,t=<passes>,p=<lanes>$<salt>$<hash>`, with a random 16-byte salt and a 32-byte key. Unpadded standard base64. Each hash carries its own parameters.
- **Default parameters:** the OWASP baseline, **m = 19 MiB, t = 2, p = 1**. They are configurable with `FINUANCE_ARGON2_MEMORY_KIB`, `FINUANCE_ARGON2_TIME` and `FINUANCE_ARGON2_THREADS`.
- **Upgrades:** `NeedsRehash` compares a stored hash with the configured parameters. After a successful login with outdated parameters, the password is rehashed and saved.
- **Unknown emails:** login verifies against a dummy hash made with the current parameters, so the response time doesn't reveal whether the account exists. The error message is the same in both cases.
- **Length cap:** passwords are capped at 1,024 bytes (`MaxPasswordBytes`), because argon2 reads the whole input. The minimum is 12 characters, with no composition rules (NIST SP 800-63B).
- **Concurrency cap:** at most 4 hashes run at once per process, so a burst of logins can't allocate more than ~80 MB. Further requests wait for a slot or for their context to end.

## Benchmark

`go test -run '^$' -bench Hash ./internal/auth/`, cloud sandbox (Intel Xeon @ 2.80 GHz, 4 shared vCPUs, Go 1.27.3), 3 runs each:

| Parameters | Time per hash | Memory per hash |
|---|---|---|
| **m=19 MiB, t=2, p=1 (chosen)** | **47 ms** | 19 MiB |
| m=46 MiB, t=1, p=1 (OWASP alternative) | 60–66 ms | 46 MiB |
| m=64 MiB, t=3, p=1 | 244–265 ms | 64 MiB |
| m=64 MiB, t=3, p=4 | 116–266 ms (noisy) | 64 MiB |

47 ms per login is unnoticeable to a user and keeps CPU and memory small on a homelab node. An attacker with a leaked hash still pays 19 MiB and two passes per guess. The stronger settings cost 5× more server time for a household-scale app, and they can be switched on through configuration without a migration.

## Consequences

- Raising the parameters later is a configuration change. Users get the new parameters at their next login.
- Each login allocates ~19 MiB for ~50 ms. Rate limits (ADR 0012) and the concurrency cap bound the total.
- Signup necessarily reveals whether an email is registered (`409`). That is acceptable for a self-hosted app, and signup is rate-limited like login.

## Alternatives considered

- **bcrypt:** a 72-byte input limit, and not memory-hard. OWASP prefers argon2id.
- **scrypt:** fine, but argon2id is the current recommendation, and x/crypto has both.
- **A custom format:** PHC strings are standard, self-describing and portable to other tools.
