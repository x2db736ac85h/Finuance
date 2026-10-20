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
