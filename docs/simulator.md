# The traffic simulator

`finuance-sim` plays about ten users against a **lab** instance, all day, through the same front door
a browser uses. Dashboards, logs and traces then always have realistic traffic to show, and every
feature is exercised day after day. Everything it causes is labelled synthetic by the server.
Design: [ADR 0043](adr/0043-traffic-simulator.md); the label: [ADR 0041](adr/0041-synthetic-users.md).

> **Never point it at an instance with real data.** It refuses to start against an instance whose
> `GET /meta` does not report `simulation: true`, and such an instance refuses its users anyway.

## The animals

Ten personas, each with a character, habits, a daily curve in the instance's time zone and think
times (`backend/internal/sim/personas.go`). Above ten they repeat as `otter-2`, `owl-2`, … (50 at most).
Each signs in as `<animal>@sim.invalid`.

| Animal | Character | Mostly does |
|---|---|---|
| Otter | the tidy one | imports each new month, categorises, keeps rules in order, opens the overview |
| Owl | the analyst | every chart for long periods, the filter language, searches, exports |
| Badger | the budgeter | budgets and goals, the pace chart |
| Magpie | the collector | groups, bulk selections, group reports and the timeline |
| Heron | the sharer | friend codes, shared groups, expenses, settling up, notifications |
| Hedgehog | cash only | a cash account, expenses typed in, receipts |
| Fox | the one who asks | questions to the assistant, applies and reverts its proposals, digests |
| Squirrel | the hoarder | overlapping statements, deleting and importing again, the duplicates window |
| Mole | the rare visitor | the overview now and then, a mistyped password |
| Sloth | slow and clumsy | broken files, abandoned previews, unknown paths: the 4xx traffic |

Their bank statements are made on demand (`backend/internal/sim/statements`): a day's rows are a
pure function of the animal and the date, so overlapping statements de-duplicate like real exports.
Two accounts each, in the two bank layouts, with salaries, rent, subscriptions and transfers.

## Running it

**On one machine** (Docker Compose, next to your own stack):

```sh
make sim-up                    # the lab on :8020, the simulator on :8090, Grafana on :3000
make sim-password ANIMAL=fox   # sign in to the lab as an animal and look at its screens
make test-e2e-sim              # the tuning page's browser tests against the lab
tools/check-lab.sh             # actions, Prometheus and Loki checked from outside
make sim-down                  # stop it (SIM_RESET=1 drops the lab's database too)
```

`make sim-up` writes `.env.lab` once (the seed and the control token, random) and prints the page's
address with the token in its fragment.

**On Kubernetes**: [`deploy/environments/lab`](../deploy/environments/lab) (a second release of the
app chart with `simulation` on, its database and mock model, and `deploy/helm/finuance-sim` in a
namespace of its own). The rehearsal runs it (`tools/rehearsal.sh lab`). The homelab doesn't.

## Tuning it

The page (port 8090, behind the control token) shows the target, the state and the totals, a card per
animal, and the live feed. You can change:

- **mode**: *realistic*, the characters at their own pace, or *load* (ADR 0044), one target for
  the whole lab in actions a second, with a ramp, a duration after which it returns to realistic
  mode by itself, and "push through server errors" to find a breaking point;
- **intensity** (realistic mode): 0 to 3×;
- **which animals play**: how many, and each one on or off (beyond twenty, one card per animal);
- **randomness**: the run seed (the same seed makes the same choices again; *New seed* draws one),
  timing jitter, bursts of quick actions, and the share of deliberate mistakes;
- **feature areas**: each one on or off;
- **weights**: per action, for everyone or for one animal;
- **pacing**: active hours, the data calendar's speed (up to a data day a minute), and the daily cap on questions to the assistant;
- **presets**: quiet day (back to realistic mode), month end, assistant heavy, import flood, error storm, and the load presets ramp (20 minutes up to 200 a second, through errors), soak (50 a second for an hour) and spike (bursts of a second's worth for ten minutes).

**Act now** makes one animal do one action at once.

A change reaches the animals within five seconds. Whatever is sent is held to the ceilings in the
code:
- realistic mode: 50 personas, 3× intensity, 30 actions a minute per persona;
- load mode: 500 personas, 2 actions a second per persona, 1,000 a second for the lab, and a
  duration of a day at most;
- one assistant question at a time, and a daily cap;
- `Retry-After` on `429`, and a back-off after five `5xx` in a row (unless load mode pushes through);
- sign-ins paced under the instance's limit per address (25 at once, then one every 2.5 seconds).

Load mode can only play the animals the lab made: `make sim-up` makes 100 (`SIM_ANIMALS`, at most
500). An animal the lab doesn't have shows as "not in the lab" and waits for the next change.

Each animal is a user of its own, so it keeps below the instance's limits per user, with token
buckets like the instance's: a question to the assistant every two minutes, an upload every 2.5
seconds (it waits), an invitation every 2.5 minutes, each with a burst below the instance's 30.

The control API is [`api/sim.openapi.yaml`](../api/sim.openapi.yaml).

## How much data the animals have

The **history profile** (the page's *History* section, ADR 0044) says how much a new or rebuilt
history holds: the day's spending drawn up to 20 times (about 1.7 rows each), the amounts' spread,
how many shops each merchant has, and how many years back it reaches. The page estimates the
transactions per animal (density 11 over three years is about 20,000, the size the load test used
before). The default profile writes the statements the simulator always wrote.

A profile is written into the accounts' names (`Current account · d11 s100 v1 y3`), so a restarted
simulator reads it back and overlapping statements still de-duplicate. New animals start with the
configured profile; the others keep theirs until *Rebuild history*: each animal that plays deletes
its two bank accounts at its next turn, makes them again and imports the history through the normal
upload, eight at a time.

Without the page:

```sh
finuance-sim seed -target=http://localhost:8020 -count=50 -rows=20000 -years=3   # FINUANCE_SIM_SEED set
finuance-sim users -count=50 -out=users.json                                      # e-mails and passwords for k6
```

`seed` rebuilds each animal's history, or imports only the days since for an animal that already has
that profile. `make loadtest-seed` uses both on a lab, so k6 signs in as the animals
([loadtest/README.md](../loadtest/README.md)).

## What the load costs

- **On the page:** *What the load costs* shows the animals' actions and the API's requests a
  second, its p95 and its server errors, and each container's CPU and memory, over the last 15
  minutes (`GET /api/resources`: fixed queries against the lab's Prometheus, `-prometheus`; every
  five seconds, one query for every viewer). *Flow* draws each action as a dot from the animals
  through the web server and the API to the database, the worker (imports) or the mock model
  (questions), in its result's colour; with reduced motion it shows the counts of the last minute.
- **In Grafana:** the dashboard "Finuance · Lab resources": CPU, memory and disk per container,
  the database's transactions, cache hits and connections, the pool's waits, and the animals'
  actions next to the API's requests and latency.
- **Where the figures come from:** on the Compose lab, Alloy's own cAdvisor (the host's cgroups,
  containerd's socket and, read only, Docker's root for its classic storage; like the Docker socket
  it already reads, a development machine only) and
  its PostgreSQL exporter as the application role (`deploy/compose/observability/metrics.alloy`);
  on a cluster, the kubelet's cAdvisor and CloudNativePG's metrics, with the simulator chart's
  `simulator.prometheus` and an egress entry for Prometheus.
- **From the lab itself:** Administration → Developer links the console and the two dashboards. `make
  sim-up` makes the lab's administrator (`admin@lab.invalid`, a person; `make sim-password
  ANIMAL=admin`), who sees that section.

## Finding its traffic

- **Metrics:** the `traffic` label, `synthetic` or `real`, on the request, job and assistant counters; every dashboard's Traffic variable. Requests without a user (signing in, the probes, unknown paths) count as real.
- **Logs:** `traffic=synthetic` on the api's access line. In Loki on Kubernetes it is the `traffic` label (`{namespace="finuance-lab", traffic="synthetic"}`).
- **Traces:** the span attributes `finuance.synthetic=true` and `user_agent.synthetic.type=test`.
- **The simulator's own view:** `finuance_sim_*` metrics and the dashboard "Finuance · Simulator" (actions by animal and by area, errors, the latency it saw next to the server's); what its load costs in "Finuance · Lab resources".
- **One action:** the feed shows each action's request id, which is the id the server answered (on Compose the web server assigns its own). It finds the action's lines in Loki, and a line's `trace_id` opens its trace.

The service-level objectives leave synthetic traffic out (`traffic!="synthetic"`) unless the
observability chart's `slo.includeSynthetic` is set, which a lab does.

## What it must never be pointed at

An instance with real data. Three guards: the simulator checks `/meta`; the instance refuses
synthetic users without `FINUANCE_SIMULATION`; and the admission policy admits the simulator's
and the mock's images only in the lab's namespaces (`finuance-sim`, `finuance-lab`), never in
`finuance`. The seed and the control token are secrets: whoever has the seed can sign in as the
animals, on a lab only (THREAT-MODEL T19, D14, I23).
