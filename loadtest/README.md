# Load tests

k6 scripts for the Finuance API (PLAN task 17.5), checked against the SLOs in
[docs/SLO.md](../docs/SLO.md): 95 % of table and chart requests under 300 ms, under 0.5 % failed.
k6 runs as its Docker image (`grafana/k6` 2.x, pinned in the Makefile), so nothing is installed.

```sh
FINUANCE_SIMULATION=true make up     # a lab: the load test's users are synthetic (or make sim-up)
make loadtest-seed                   # 50 synthetic users × ~20,000 demo rows (LOADTEST_USERS, LOADTEST_ROWS)
make loadtest                        # PROFILE=load (default), smoke or ramp
```

| Profile | What | Used by |
|---|---|---|
| `load` | 5 minutes: 30 users browsing the table (filters, sorts, scrolling), 15 loading all six charts, 5 on the overview, 2 imports of 5,000 rows a minute; think time between actions | the SLO check before a release; docs/perf.md |
| `smoke` | 1 minute, a few users, one import | the weekly workflow (Phase 17 push 3) |
| `ramp` | browsing without think time, 10 → 320 table views per second over 5 minutes | finding where the stack breaks |

Every virtual user signs in once in `setup()` as one of the seeded users, paced under the sign-in
limit of 30 per minute and address. Imports are deleted again after they commit, so repeated runs keep the
same data. Results: the k6 summary on the terminal and `results/summary-<profile>.json`
(gitignored). The thresholds make `make loadtest` fail when an SLO is missed.

**The users are the simulator's animals** (ADR 0044). On a lab (`FINUANCE_SIMULATION=true make up`),
`make loadtest-seed` makes `LOADTEST_USERS` animals from a seed of its own (`loadtest/.sim-seed`,
random, written once), gives each about `LOADTEST_ROWS` transactions over `LOADTEST_YEARS` through
the normal upload (`finuance-sim seed`: a rebuild, or only the days since on a second run) and
writes their e-mail addresses and passwords for k6 (`finuance-sim users`, `loadtest/users.json`,
mode 0600; both files gitignored). The server labels their requests `traffic="synthetic"`, so a load
test does not burn the error budget of the instance it runs against, and Grafana's Traffic variable
shows or hides it. On an instance that is not a lab, `make loadtest-seed LOADTEST_PEOPLE=1` seeds
people (`loadtest+001@example.test` …, password `LOADTEST_PASSWORD`, `finuance seed-demo -users`);
their traffic counts as real.

The sandbox numbers and the bottleneck found with these scripts are in
[docs/perf.md](../docs/perf.md) "Phase 17"; the homelab run is in "Phase 18".

## Which tool is for what

| Tool | Question it answers | Where |
|---|---|---|
| **k6** (`loadtest/`, `make loadtest`) | How much load does the stack take, and where does it break? Short, scripted, many virtual users, thresholds that fail the run | before a release, the weekly smoke run, a breaking-point test |
| **k6** (`deploy/k8s/rehearsal/podkill.js`) | Does a rollout or a lost pod fail a request? | the rehearsal, `tools/homelab-drill.sh podkill` (a person's account there: the homelab is not a lab) |
| **The traffic simulator** (`finuance-sim`, [docs/simulator.md](../docs/simulator.md)) | Does every feature keep working, day after day, as people use it? Ten characters with habits and state, all day, tuned from a page; something to look at on every dashboard | a lab: `make sim-up`, the rehearsal's `lab` stage, a cluster's lab |

k6 2.0 removed the externally controlled executor, which a page to tune a load would have needed;
that is one reason the simulator is its own service (ADR 0043).
