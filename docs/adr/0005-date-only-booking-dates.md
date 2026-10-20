# 0005. Booking dates as date-only values (`platform.Date`)

- Status: accepted
- Date: 2026-10-20

## Context

Bank booking and value dates are **calendar dates**, not instants. The legacy app stored them as UTC-midnight epoch milliseconds, which works only while every computation stays in UTC. A `time.Time` silently picks up a location and turns `2025-03-01` into `2025-02-28T23:00` the moment someone calls `.Local()` or formats it in another zone. PLAN §4.1 asks us to pick one date representation and use it consistently.

## Decision

- PostgreSQL stores dates as SQL **`date`** (`booked_on`, `value_on`). There is no `timestamptz` for domain dates.
- In Go, domain code uses a small value type **`platform.Date`**:
  - `struct{ Y int; M time.Month; D int }`: comparable with `==`, usable as a map key, has no zone;
  - constructors validate the date (`31.02.2025` is rejected);
  - helpers: `AddDays`, `AddMonths` (clamped to the end of the month), `Before`/`After`/`Compare`, `Weekday`, `FirstOfMonth`, `DaysBetween`;
  - JSON and text form `YYYY-MM-DD`;
  - conversion to `time.Time` only at UTC midnight, and only inside the type's own methods.
- Conversion to and from `pgtype.Date` happens only in the `store` layer. sqlc is configured to override `date` to `platform.Date`, or the store maps it.
- The API uses `format: date` (`YYYY-MM-DD`) strings. The frontend has date-only helpers in `src/lib/dates.ts` and never calls `new Date(string)` on them.
- "Today" comes from the injected `platform.Clock`, in the user's preferred time zone (or `Europe/Warsaw` by default), converted to a `platform.Date` once at the edge.
- Periods are half-open `[from, to)` intervals of `platform.Date`.

## Consequences (positive, negative, follow-ups)

- Time-zone bugs become type errors: a `Date` has no zone to get wrong.
- Date maths (month stepping, weekday buckets) is explicit and testable.
- We maintain a small custom type and its tests instead of using `time.Time` directly. It is about 150 lines.
- sqlc/pgx need a mapping for `date`. If an override proves awkward, the store converts by hand.
- Legacy oracle outputs use ISO date strings instead of epoch ms. This is listed in `testdata/oracle/ALLOWED_DIFFS.md` (Phase 1).

## Alternatives considered

- **`time.Time` at UTC midnight everywhere:** no new type, but nothing stops zone leaks; equality needs `Equal`, and map keys include the location.
- **`cloud.google.com/go/civil.Date`:** essentially what we want, but it pulls in a large module family for one struct.
- **`int32` days since epoch:** compact and fast, but unreadable in logs and tests.
