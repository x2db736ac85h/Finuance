# Performance

Benchmark numbers and how they were reached. Each entry says which machine produced it, because the cloud sandbox is noisy and shares its CPUs.

## Ingest: `BenchmarkParse_100k` (Phase 3, task 3.14)

**What it measures:** the whole import pipeline on 100 000 rows (≈ 16.3 MB, Pekao layout) from the ported legacy demo generator (`internal/demo`):

1. decode;
2. delimiter detection;
3. tokenize;
4. shape;
5. column typing;
6. role detection;
7. `Stage` with fingerprints.

The benchmark is `backend/internal/ingest/bench_test.go`. Run it with:

```sh
cd backend && go test -short -run='^$' -bench='^BenchmarkParse_100k$' -benchmem -count=6 ./internal/ingest/
```

CI runs it with `-count=3` and writes the result to the `backend (Go)` job summary.

**Target (PLAN §3.14):** ≤ 500 ms per run, with allocations reported.

### Results — 2026-10-20, cloud sandbox

The sandbox is an Intel Xeon @ 2.80 GHz with 4 vCPUs (shared), running Go 1.27.3 on linux/amd64.

| Run | ns/op (6 runs) | Median | B/op | allocs/op |
|---|---|---|---|---|
| Before optimisation (`f69844e`) | 7.5–9.7 s | ≈ 8.5 s | — | — |
| After, 4 cores (`-cpu=4`) | 509–669 ms | **≈ 563 ms** | 272.7 MB | 2.49 M |
| After, 1 core (`-cpu=1`, 4 runs) | 1.22–1.41 s | ≈ 1.24 s | 272.7 MB | 2.49 M |

**Status:** the target is **not met on this machine**. The best runs reach 509–511 ms, and the median is ≈ 13 % over. The sandbox shares its CPUs, and the same binary varied by 30 % between runs. A dedicated 4-core machine should come in close to 500 ms, but that has not been measured yet. Compare with the CI job summary for a second data point.

### Where the time goes (one run, 4 cores, after optimisation)

| Step | Time |
|---|---|
| Decode (UTF-8 validation) | 12–24 ms |
| Delimiter detection | < 0.1 ms |
| Tokenize | 34–37 ms |
| Shape (public `Shape`, which copies rows; `Parse` trims in place) | 57–64 ms |
| Column typing (parallel per column) | 92–103 ms |
| Role detection | 10 ms |
| Stage + fingerprints (parallel chunks) | 212–224 ms |
| **Total** | **≈ 440 ms** |

The first run of the breakdown took 985 ms (a cold heap and GC warm-up). The benchmark loop sits between the warm and the cold runs, because it allocates 272 MB per run and the GC runs during the measurement.

### What changed (profile-driven, behaviour-preserving)

Every oracle comparison, golden file and fuzz invariant still holds after each change.

1. **`money.Split` without regexes.** The legacy-shaped currency regex backtracked on every cell. Hand-written checks replaced it:
   - a plausibility pre-check (a digit is present, at most 3 ASCII letters);
   - prefix/suffix currency stripping;
   - number-shape checks;
   - no copy when there is nothing to strip.
2. **`Norm` fast paths.** Pure ASCII is lowercased in one pass. Other text is decomposed per rune with NFD properties. Full NFD + lowercasing remains the fallback, which handles combining marks, Hangul, `Σ` and `İ`. `TestNormFastPathsMatchFull` checks the paths agree on the corpus and the edge cases.
3. **Byte tokenizer.** For ASCII delimiters (all real ones), plain fields are zero-copy substrings, and only quoted fields are assembled in a buffer. The rune state machine stays for other delimiters. `FuzzTokenize` checks the two produce identical output (≈ 3 M inputs in 5 minutes).
4. **Hand-written date scanners.** They mirror the legacy `isoDate`/`dmyDate` regexes, which stay in `values_test.go` as the reference.
5. **Cheap pre-checks.** `ParseDate` rejects cells shorter than 8 or not starting with a digit, and `ParseNumber` rejects text without digits, before any real parsing.
6. **Parallelism.** Columns are typed concurrently, bounded by `GOMAXPROCS`. `Stage` works in chunks of ≥ 2000 rows and merges in order, so the output is deterministic.
7. **Pre-sizing.** Records are counted by newline before tokenizing, row slices are sized from the previous row, and `Raw` is sized from the column count. `Parse` trims rows in place because it owns the tokenizer output.

**Tried and reverted:** normalising each cell before the join in `SearchText`. It saved CPU but raised allocations, and the total got slower.

### Further options (not done; the next phase that touches ingest can pick them up)

- `Stage` dominates. `SearchText` normalises the joined row, so `Norm` runs again over text that typing already looked at. Caching the per-cell lowercase/NFD form during typing would avoid the second pass.
- The 272 MB/op is mostly `Raw` pairs and the staged strings. An arena per import, or streaming staged rows to the database in batches instead of holding all of them, would cut GC time.
- Column typing parses every cell as a number *and* as a date. Stopping once a column can no longer reach the 90 % threshold would skip most of the work for text columns. `Column.Numbers`/`Dates` would then have to be filled lazily for the columns that need them.

## Store: staging insert of 100k rows (Phase 4, task 4.4)

**What it measures:** `ledger.CreateImport` for the 100k-row demo file (Pekao layout, 12 columns), already parsed and staged outside the timer. It covers:

- converting the rows to the `import_rows` form;
- the `imports` and `import_files` rows;
- 20 `INSERT … SELECT FROM unnest(…)` statements of 5,000 rows each, in one transaction under `WithUser`, against the testcontainers Postgres 18.6.

The benchmark is `backend/internal/ledger/bench_test.go`. Run it with:

```sh
cd backend && go test -run '^$' -bench 'CreateImport_100k' -benchtime=3x -count=3 ./internal/ledger/
```

It needs Docker. It isn't part of `make test`.

**Target (PLAN §9 Phase 4):** ≤ 3 s on a dev machine.

### Results — 2026-10-20, cloud sandbox (same machine as above; Postgres in Docker on the same 4 vCPUs)

| Run | ns/op (3 × 3 runs) | B/op | allocs/op |
|---|---|---|---|
| First version | 8.5–10.7 s | 1.30 GB | 4.60 M |
| UUIDs encoded as `[16]byte` | **8.0–9.7 s** | 1.24 GB | 2.80 M |

**Status:** the target is **not met in the sandbox**, by roughly 3×. The time is almost all server-side.

### Where the time goes

Measured with `EXPLAIN (ANALYZE, BUFFERS, WAL)` on one 5,000-row batch of demo rows, and with timers around each step of 100k rows:

| Step | Time |
|---|---|
| Convert `StagedRow` → `store.ImportRow` (raw JSON encoding) | ≈ 0.5 s / 100k |
| pgx encoding of the 27 parameter arrays | ≈ 0.4–0.6 s / 100k |
| Server: execute one 5,000-row batch | 285 ms (≈ 5.7 s / 100k) |
| — of which: parsing the `raw` text into `jsonb` | ≈ 95 ms (same batch with a tiny `raw`: 136 ms) |
| — of which: FK triggers (`import_id`, `user_id`, `transaction_id`), per row | ≈ 90 ms |
| Reference: copy the same 100k rows inside Postgres (temp table: no WAL, no FKs, no RLS) | 2.1 s |

The last row is the floor for this database container on this machine: even a server-side copy with no constraints takes 2.1 s. A dev machine with native Docker or a local Postgres should be faster, but that has not been measured yet.

### Tried and rejected

- **Batch size.** Batches of 1,000, 2,000, 5,000 and 20,000 rows, each on a fresh import in alternating order, were all within the run-to-run noise (±25 %). 5,000 is kept.
- **Pipelining** all 20 statements in one `pgx.Batch`: 11–14 s, slower than the loop (the whole batch is encoded before anything is sent).

### Options (need a decision; both change the §5.2 schema)

1. **Store `raw` as an array of values only** (`["02.01.2025", "-12,50", …]`), with the header names once per import (`imports.headers`). About half the server time goes into parsing the repeated `{"h": …, "v": …}` objects. Transactions would need their header names from the source import. That's a data-model change for an ADR.
2. **Drop the `user_id` foreign key on `import_rows`.** The row's import already belongs to the user, and RLS checks `user_id`. That saves one FK trigger per row (≈ 15 %).

Neither is done. The recommendation is to measure on a dev machine first (`make test-integration` already needs Docker there; the command above runs the benchmark), and to choose an option only if that also misses 3 s.
