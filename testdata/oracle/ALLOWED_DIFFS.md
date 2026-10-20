# Allowed differences: legacy oracle vs rebuild

The oracle records what the **legacy** app does. Where the rebuild intentionally behaves differently, the difference is listed here with a reason and a plan reference. A differential test may skip or transform an oracle expectation **only** if a line here covers it.

> `make oracle` regenerates every folder in `testdata/oracle/` but never touches this file.

| # | Area | Legacy behaviour (oracle) | Rebuild behaviour | Reason / reference |
|---|---|---|---|---|
| 1 | Dates | Impossible dates roll over (`31.02.2025` → `2025-03-03`, via `Date.UTC`) | Rejected: the value is not a date (the column may still be typed `date` when ≥ 90 % parse) | PLAN §4.2 [CHANGE]; fixture `synthetic/27-invalid-date.csv` |
| 2 | Dates | Dates are UTC epoch milliseconds; the oracle prints them as ISO strings | `platform.Date` / SQL `date`, `YYYY-MM-DD` | PLAN §4.1 [CHANGE], ADR 0005. The oracle's ISO output is the comparison format |
| 3 | De-duplication | All files merge into one *All data* view; the multiset bucket is the file | Dedupe is per account, and the bucket is the import into that account. Files in different accounts are never merged | PLAN §1.3, §4.4 [CHANGE]. Compare `dedupe/*.json` only for sets that belong to one account |
| 4 | Header detection | `buildTable` auto mode picks a data row as header for PKO BP (`banks/pko-bp-like.csv`, `real/pko-bp/`), and the bank address line or the first transaction for mBank (`banks/mbank-like.csv`, `real/mbank/`) | Phase 3 ports the legacy rule exactly (no difference yet). Phase 9 must find the real header (or the preview asks) | MVP bugs reported by Irwin; fix decided for Phase 9. Its tests use `ingest-cfg/banks/*/header-*.json` as the target |
| 5 | Money | `float64`; amounts like `-127.13` | `int64` minor units (`-12713`) | PLAN §4.1, ADR 0004. Compare after converting the oracle value with `round(x × 100)` |
| 6 | Search normalisation | `rowNorm` joins cells with `\u0001` | Shadow `_n` columns per field | PLAN §4.5. Compare match results, not the joined string |
| 7 | Assistant | `execTool` runs in the browser; row ids are row indices; "Show in table" uses the matched id set | Tools run as SQL under RLS; ids are UUIDs; "Show in table" applies the stored `toolFilter` | PLAN §4.8 [CHANGE]. Compare counts, totals and groups; map row indices → transaction ids |
| 8 | Overview formatting | Card tables contain locale-formatted strings (`76,046.07 PLN`, en-US) | The API returns numbers; formatting happens in the frontend | PLAN §4.1. Compare after parsing the strings |
| 9 | Dates with a time | `parseDate` keeps the time, and an out-of-range time moves the date (`02.01.2025 24:00` → `2025-01-03`; `12:60` → `13:00`, which crosses midnight at `23:60`) | The time is ignored; `booked_on` is the written calendar date | PLAN §4.1: transactions carry a date, not a timestamp. Test: `dateDiffReason` "time rolls the date" (`values_test.go`, `oracle_test.go`) |
| 10 | Years 0–99 | `Date.UTC` maps years 0–99 to 1900–1999 (`looseDate("0001")` → the year 1901) | Rejected: not a date | No bank writes two-digit or zero-padded ancient years; the legacy result is silently wrong. Test: "year below 100" |
| 11 | Query dates with a time | `looseDate("2025-01-02 07:21")` matches that one minute | Matches the whole day `2025-01-02..2025-01-03` | Follows from row 9: rows have no time to compare with. Test: `looseDiffReason` "time widens to the day" |

Every difference above is checked by a strict classifier in the tests: it re-derives what the rule predicts from the input and accepts our value only if it is exactly that, so a classifier cannot hide an unrelated mismatch. The differential tests log how many cells each reason covered.

## Oracle artefacts (not behaviour differences)

- The oracle cuts ISO dates to 10 characters, and JavaScript writes years beyond 9999 in extended form, so the upper bound of `looseDate("9999-12-31")` is recorded as `+010000-01`. The test maps it back to `10000-01-01` before comparing.

## Function names

The plan's injection list was verified against `legacy/finuance.html` (Finuance release, 2026-10-20). All 32 names exist under the same names. Nothing is renamed or missing.
