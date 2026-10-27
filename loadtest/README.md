# Load tests

k6 scripts for the Finuance API (PLAN task 17.5), checked against the SLOs in
[docs/SLO.md](../docs/SLO.md): 95 % of table and chart requests under 300 ms, under 0.5 % failed.
k6 runs as its Docker image (`grafana/k6`, pinned in the Makefile), so nothing is installed.

```sh
make up                      # or make up-observability, to watch it in Grafana
make loadtest-seed           # 50 users × ~20,000 demo rows (LOADTEST_USERS, LOADTEST_ROWS)
make loadtest                # PROFILE=load (default), smoke or ramp
```

| Profile | What | Used by |
|---|---|---|
| `load` | 5 minutes: 30 users browsing the table (filters, sorts, scrolling), 15 loading all six charts, 5 on the overview, 2 imports of 5,000 rows a minute; think time between actions | the SLO check before a release; docs/perf.md |
| `smoke` | 1 minute, a few users, one import | the weekly workflow (Phase 17 push 3) |
| `ramp` | browsing without think time, 10 → 320 table views per second over 5 minutes | finding where the stack breaks |

Every virtual user signs in once in `setup()` as one of the seeded users
(`loadtest+001@example.test` …, password `LOADTEST_PASSWORD`), paced under the sign-in limit of 30
per minute and address. Imports are deleted again after they commit, so repeated runs keep the
same data. Results: the k6 summary on the terminal and `results/summary-<profile>.json`
(gitignored). The thresholds make `make loadtest` fail when an SLO is missed.

The sandbox numbers and the bottleneck found with these scripts are in
[docs/perf.md](../docs/perf.md) "Phase 17"; the homelab run comes in Phase 18.
