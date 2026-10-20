# Legacy oracle

This tool turns the legacy single-file app (`legacy/finuance.html`) into **executable expectations**. It runs the real legacy code in headless Chromium, feeds it fixtures, and writes what the legacy functions return as JSON. The Go port (Phase 3 onwards) is tested against these files.

```sh
make oracle            # = cd tools/legacy-oracle && npm ci && node oracle.mjs
make fixtures          # regenerate testdata/fixtures/synthetic/ (make-fixtures.mjs)
```

## How it works

1. **Injection.** The tool copies `legacy/finuance.html` in memory and inserts `window.__legacy = {…}` immediately before the IIFE's final `updateToggles();`. The anchor must appear exactly once at the start of a line, otherwise the run fails. It exposes the functions listed in PLAN Phase 1 task 1.4, plus `CHARTS`, `ovCtx`, `drawChart`, `view`, `setActive`, `addFiles`, `setPassphrase`, `savePrivate`, `saveChatsNow` and `INV0`, which the overview, duplicates and backup outputs need.
2. **Serving.** A local HTTP server on `127.0.0.1` serves the page, so IndexedDB works for the backups. Each task gets a fresh browser context with `bypassCSP`, locale `en-US` and time zone `UTC`.
3. **Determinism.** An init script replaces `Math.random` and `crypto.getRandomValues` with a seeded xorshift32. Playwright's clock freezes `Date.now()` at `2025-03-20T10:00:00Z`. Ids, salts, IVs and timestamps, including those in the encrypted backup, are therefore identical on every run. JSON is written with sorted keys and 2-space indentation.
4. **Page-side work.** `page-helpers.js` is added after boot. It only uses `window.__legacy` and converts results to plain JSON: dates become ISO `YYYY-MM-DD` (or a full ISO timestamp when there is a time), and `NaN` becomes `null`.

Playwright is pinned to **1.56.1**. Its Chromium build is the one preinstalled in the cloud sessions, and CI installs the same build. `ORACLE_CHROMIUM=/path/to/chrome` overrides the browser.

## Outputs

| Path | Content |
|---|---|
| `testdata/oracle/ingest/**.json` | Every fixture under `testdata/fixtures/{synthetic,demo,real}` with default options (`enc=auto`, `delim=auto`, `strip=true`). Each file holds the encoding, delimiter, `headerAt`, `dropped`, headers, rows, per-column type / parsed numbers / parsed dates, `roles` and `rowNorm`. |
| `testdata/oracle/ingest-cfg/<fixture>/<variant>.json` | The same, with the explicit per-file settings (`e.cfg`) from `cases/cfg.mjs`: header row, none + skip, `dropShort`, `dateOrder`, invert, full mappings incl. debit/credit/ignore, manual encodings and delimiters. The cfg used is recorded in each file. |
| `testdata/oracle/corpus/*.json` | `parseNum`, `parseDate`, `looseDate`, `norm` and `payeeName`, each over 150+ inputs from `cases/corpus.mjs`. |
| `testdata/oracle/dedupe/*.json` | `getDataset('merged')` for the overlap, same-name, duplicate-versions and two-bank sets: kept rows (sorted), `dupes`, and the `ds.dup` groups (versions, files, kept flags). Most sets are then rebuilt via `dupChanged()` with `S.dupPick` (every group prefers its last file) and with `S.dupKeep` (keep both). |
| `testdata/oracle/txkeys/*.json` | `txKeys` per file and for the merged view: the `#n` twin rule that `occurrence` must match. |
| `testdata/oracle/filters/demo.json` | 84 `(column, cond)` cases through `compileCond` on the demo dataset: match count and the first 20 row indices. `column-map.json` maps the legacy demo headers to canonical columns. |
| `testdata/oracle/recurring-periods/demo.json` | `findRecurring`, `resolvePeriod` for every preset, and `roundNice`. |
| `testdata/oracle/assistant/` | 67 `execTool` calls (every filter, sort, `group_by`, both `find_values` fields) on the demo data with two groups assigned, plus `sysPrompt` with today frozen. |
| `testdata/oracle/investments/*.json` | Table twins of `o_inv`, `o_yoc` and `o_fire` for 5 portfolios (`cases/investments.mjs`), each over two months: all-zero, typical, no annual income (transaction fallback), short history, FI not reachable. |
| `testdata/fixtures/demo/demo-legacy.csv` | The legacy `demoCSV()` output. |
| `testdata/fixtures/legacy-backup/` | `exportBackup()` downloads, plain and locked with the test passphrase `oracle-test-passphrase`. |

Known and accepted differences between legacy and rebuild behaviour are listed in `testdata/oracle/ALLOWED_DIFFS.md`.

CI job `oracle-fresh` reruns the oracle and fails if `git diff` is non-empty.
