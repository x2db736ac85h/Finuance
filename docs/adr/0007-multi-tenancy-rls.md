# 0007. Multi-tenancy: `user_id` everywhere + Postgres RLS as defence in depth

- Status: accepted
- Date: 2026-10-20

## Context

The rebuild is multi-user (a household, plus a public demo). Financial data must never leak between users. A missing `WHERE user_id = $1` in one query, or in one dynamically built filter, would expose everything. Application-level checks alone fail open.

## Decision

- Every user-owned table has a non-null **`user_id`** column, and every query filters by it explicitly.
- **Row-level security** is enabled and **forced** on every user-owned table. Policies compare `user_id` with `current_setting('app.user_id', true)::uuid` for `USING` and `WITH CHECK`.
- The application role `finuance_app` owns no tables, so RLS can't be bypassed by ownership. Migrations run as `finuance_owner`.
- All user-data access goes through `store.WithUser(ctx, userID, fn)`. It opens a transaction, runs `SET LOCAL app.user_id = …`, and passes a querier to `fn`. `store.WithUserReadOnly` also sets `transaction_read_only`, for assistant tools.
- Unscoped access (auth lookups by email or session token, background maintenance) uses narrowly named functions with dedicated policies.
- Every new user-owned table ships with an RLS test: user A can't read, update or delete user B's rows, even with a query that omits the `user_id` filter.

## Consequences (positive, negative, follow-ups)

- A forgotten filter becomes a test failure or an empty result, not a data leak. This is a strong review and interview talking point.
- `SET LOCAL` means every user-data operation runs inside a transaction. That is fine for our workload, and the pool must not leak settings between requests (guaranteed by `SET LOCAL`).
- RLS predicates can affect query plans. Indexes lead with `user_id`, and `EXPLAIN` reviews include RLS.
- Bulk loads can't use `COPY`; we use `INSERT … SELECT FROM unnest` (ADR 0003).

## Alternatives considered

- **Application filtering only:** simpler, but one bug leaks data.
- **Schema or database per tenant:** strong isolation, but migrations and connection pools multiply. That is overkill for households.
- **Separate deployments per user:** operationally heavy, and it defeats the shared demo.
