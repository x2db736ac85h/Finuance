# Changelog

All notable changes to Finuance. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
and the project uses [Semantic Versioning](https://semver.org/). Release candidates (`vX.Y.Z-rc.N`) use the
notes of their version.

## [Unreleased]

### Added

- **Archive a category** instead of deleting it (Settings → Categories). An archived category
  behaves as removed: its transactions become uncategorised, rules that set it lose that action, its
  budget goes, and its name is free. *Unarchive* brings it back with the labels you set by hand, its
  rules, bank names, budget and goals, wherever nothing has taken their place since; transactions
  you categorised differently in between stay as they are. *Delete for good* is in the archived
  list. API: `POST /categories/{id}/archive`, `POST /categories/{id}/unarchive`,
  `GET /categories/archived`; `DELETE /categories/{id}` still deletes for good. Migration 00030.
- **The assistant in the bell:** an answer that finishes while the panel is closed or the tab is in
  the background leaves an entry that opens its chat (kept in the browser only). Everything of the
  assistant's (its notifications and its toasts) carries the sparkle icon.
- **A provider choice when adding a connection** (Administration → Assistant): Anthropic, OpenAI,
  Google (with the cloud AI part of this release), a local server, or any compatible address. The
  choice fills in the format and the address and asks only for what that provider needs; the new
  connection's models open right after. A default model its own patterns leave out is refused in
  the form.
- A shared group's colour can be changed in its settings.

### Changed

- **Addresses:** the table is `/transactions/<view>`, the overview `/overview/<view>` and the
  Shared page `/shared`; the old `/t/…`, `/o/…` and `/s…` redirect. The app always opens the
  transactions, whatever was open last.
- **Settings:** Appearance is part of General, and Sessions and Access tokens are parts of
  Security; their old addresses lead there. "Back to transactions" is gone from Settings and
  Administration (the title bar's buttons do that).
- **Quick settings:** "All settings" sits in the header, the colour theme shares a line with a
  dark-mode switch (System, Dark and Light stay on Settings → General), the interface size is a
  plain slider, and the shortcuts have a command-key icon.

- **Friends are on the Shared page** (the code, adding a friend, the list), under *Friends*;
  `/settings/friends` leads there. The page is called "Shared" and exists while shared groups or
  friends are switched on.
- Administration → Assistant shows one thing at a time: a connection's models under the list, or
  the dialog that adds or changes a connection. The list names the provider.
- A personal group on the Shared page opens the table filtered to its transactions.

### Fixed

- The Keyboard shortcuts list can be scrolled with the keyboard when the window is too short to show
  it whole.
- A group's colour dot shows on the Shared page, on a shared group's page and in the group report
  (it had no size outside the groups menu).
- A rule suggestion (or a transfer suggestion) no longer shows over the overview for a moment when
  switching from the table: the page's mode is read from the address alone.
- Forms inside dialogs (group settings, sharing a group, a cash account, a typed-in transaction, a
  shared expense, settling up, creating an account) no longer touch the dialog's edge.

## [0.6.0] - 2026-11-24

### Added

- **A traffic simulator for a lab** (ADR 0043, [docs/simulator.md](docs/simulator.md)): the new
  image `finuance-sim` plays ten synthetic users, each with a character and habits, against a lab
  instance through its front door, all day. Between them they cover 57 actions in twelve feature
  areas: imports by a data calendar, labels, groups, rules, budgets and goals, the duplicates
  window, cash, the assistant (questions, proposals applied and reverted, digests), friends and
  shared groups, and the account. Their bank statements are made on demand in two layouts, and
  overlapping ones de-duplicate like real exports. The simulator refuses an instance whose
  `/meta` does not report `simulation: true`. Ceilings in the code: 50 personas, 3× intensity,
  30 actions a minute per persona, one assistant question at a time, back-off on `429` and on
  `5xx`. Each animal keeps below the instance's limits per user and paces its sign-ins under the
  limit per address that all of them share.
- **Load mode** (ADR 0044): the animals load a lab by one target for the whole lab, in actions a
  second, on a ramp, for a duration after which the simulator returns to realistic mode by itself.
  Its own ceilings in the code: 500 animals, 2 actions a second each, 1,000 a second in all, a day at
  most. Presets ramp, soak and spike; "push through server errors" to find a breaking point.
- **Randomness on the page:** a run seed (the same seed makes the same choices again), timing
  jitter, bursts of quick actions, and the share of deliberate mistakes.
- **History profiles** (ADR 0044): how much the animals' bank history holds (the day's spending up
  to 20 times, the amounts' spread, shops per merchant, years back), kept in their accounts' names.
  *Rebuild history* on the page, `finuance-sim seed` without it, and `finuance-sim users`, the
  animals' credentials for k6.
- **What the load costs:** the dashboard "Finuance · Lab resources" (CPU, memory and disk per
  container, the database's work, the API's answers, the animals' actions over them; on the Compose
  lab from Alloy's own cAdvisor and PostgreSQL exporter), and on the simulator's page a live strip
  of the same figures and *Flow*, each action drawn along its request's path. The lab's
  Administration → Developer links the console and the dashboards for the lab administrator `make sim-up`
  makes.
- **The simulator's tuning page and control API** (`api/sim.openapi.yaml`, port 8090, behind a
  control token):
  - a card per animal (one per kind beyond twenty), the feature areas and weights, schedule and
    limits, load mode, randomness, history, presets;
  - "act now", and a live feed whose request ids lead to the logs and from there to the trace.

  The simulator's own metrics (`finuance_sim_*`) and the dashboard "Finuance · Simulator".
- **A lab instance**: `FINUANCE_SIMULATION` makes `/meta` report `simulation`, and the web app then
  shows a "Lab" chip in the title bar. `finuance admin synthetic-users` makes the animals with
  passwords derived from a seed the simulator gets too.
  - **On one machine:** `make sim-up`, with a lab, the mock model, the simulator and Grafana beside your own stack.
  - **On Kubernetes:** `deploy/environments/lab` and the chart `deploy/helm/finuance-sim`, with the app chart's `simulation` switch and the job that makes the animals.
- The mock model is published as the image `finuance-llmmock` for labs. Both new images are
  built, scanned, signed and attested like the api and web images.

### Changed

- The image admission policy admits exact repositories per namespace: `finuance` runs only the
  api and web images, a lab's namespace also the mock, the simulator's namespace the simulator.
- The load test signs in as the simulator's animals on a lab (`make loadtest-seed` gives them their
  history and writes their credentials), so a load test stays out of the instance's service-level
  numbers; `finuance seed-demo -users` seeds people for an instance that is not a lab. k6 2.4.0.
- On Docker Compose, nginx looks the api's name up again while it runs, so a recreated api
  container is found without restarting the web container.
- `tools/check-private.sh FILE…` reads relative paths from the caller's directory and fails on a
  file it can't read.

### Fixed

- A label edit (a category or a note) made while the same user's transactions were being
  reclassified waited for the whole reclassification: past the 5 s lock timeout it answered
  `409 account_busy`, and when the edit had already changed a row the job was about to change, the
  two deadlocked (`500`). The reclassification now locks the user's accounts as every other writer
  does (`FOR NO KEY UPDATE`, ADR 0013). Found by the simulator's load run.
- The Compose observability profile: its Grafana and Alloy come back after a reboot, and a
  restarted Alloy no longer reads every container's log again from the start (its read positions
  were on a tmpfs). Loki doesn't shard streams, on Compose and in the cluster's chart, so the few
  seconds a restarted Alloy sends again are dropped as duplicates.

## [0.5.1] - 2026-11-21

### Added

- **Logs are collected on Kubernetes** (ADR 0042): the chart `deploy/k8s/telemetry` runs Grafana
  Alloy and Loki. Alloy reads the logs of the app's pods through the Kubernetes API (a read-only
  Role, no host path) and passes the app's traces on to Tempo; Loki keeps the lines 14 days.
  Before a line reaches Loki, nginx's error lines lose the request line, the upstream address and
  the referrer, and PostgreSQL's lines lose an error's detail and statement. Grafana links a line to
  its trace and a trace to its lines; the API and jobs dashboards have log panels.
  `logs.enabled: false` switches the logs off and keeps Alloy for the traces. The Compose profile
  `make up-observability` collects the containers' logs the same way.
- Every log line carries `trace_id` and `span_id`, and a job's lines carry `job_id`, `kind` and
  `attempt` and the `request_id` of the request that queued it; the fields are listed in
  `docs/observability.md`. A failed import or migration shows that request id as its reference.
- A panicking request is counted: one access line with status 500, one 5xx, and
  `finuance_panics_total` with the alert `FinuancePanic`.
- Traces: a client span `llm.request` around each request to a model server (model, provider,
  time to the first chunk, reported tokens, the error's kind; never a prompt or an answer); a
  failing tool marks its span; the resource names the environment, the instance, the pod and the
  namespace. `FINUANCE_OTEL_SAMPLE_RATIO` (the chart's `otel.sampleRatio`) samples a share of the
  requests.
- Metrics: `finuance_llm_request_duration_seconds`, `finuance_llm_time_to_first_token_seconds`,
  `finuance_llm_tokens_total`, `finuance_rate_limited_total`, `finuance_auth_failures_total` and
  `finuance_client_errors_total`. The latency histogram carries a request's trace id as an
  exemplar (the admin port serves OpenMetrics).
- Errors in the browser: a failure on the server's side shows its reference (the request id) in
  the message; the web app reports its own uncaught errors to `POST /api/v1/client-events`, and the
  content security policy reports to `POST /api/v1/csp-reports`. Both only count.
- Synthetic users for a lab instance (ADR 0041): on an instance started with `FINUANCE_SIMULATION`
  a user can be marked as synthetic, and its requests and jobs are labelled `traffic="synthetic"`
  in logs, metrics and traces. The label comes from the user's row, never from the request. The
  service-level rules leave such traffic out unless `slo.includeSynthetic` is set, and every
  dashboard has a Traffic selector. Migration 00029.
- The chart's `profiling` value switches `/debug/pprof` on the admin port on (off by default).

### Changed

- `http_server_requests_total`, `http_server_request_duration_seconds`, `finuance_jobs_total` and
  the assistant's run, tool and model-request counters have a new label `traffic`. Rules or
  dashboards of your own that name every label of these series need it added.
- The kubelet's probes are no longer traced, and their access lines are written at debug level.
- The assistant's dashboard and its model-error alert watch the worker as well as the api.
- The web image is built with Node 26.

## [0.5.0] - 2026-11-20

### Changed

- **Everyone is signed out once by this release:** over https the session cookie is now named
  `__Host-finuance_session`, which a browser accepts only from the exact host, with `Secure`.
- Making an access token and changing an administrator's role ask for the current password again
  (`FINUANCE_CONFIRM_PASSWORD=false` switches that off). A demoted administrator is signed out.
- Failed sign-ins are also counted per e-mail address whatever client they come from (twenty an
  hour), IPv6 clients are counted per /64, and signing out clears what the app held in memory.

### Fixed

- Shared groups: only active members can pay or be part of a payment, either side of a payment can
  remove it, a group whose owner deletes their account passes to the member who joined first, and
  its owner can correct an expense whose author is gone. The database enforces the same rules
  (migration 00028). A group holds at most 20,000 expenses.
- The assistant: applying a change without a click is decided by the server (the kinds you switched
  on, at most 50 transactions, never from a digest); a cloud model gets numbers written in groups
  and e-mail addresses masked too, also in earlier answers of the same chat; a failing model
  server is described in one fixed sentence to everyone but administrators.
- Imports: a second migration from the single-file app is refused before its upload is read; file
  names lose control characters and characters that reorder text.
- A friend code works once even when two people type it at the same moment, and what a friend or
  an administrator did appears in your activity without their address or browser.
- Helm chart: the default deny covers every pod of the namespace (`networkPolicy.namespaceWide`),
  the metrics ports are open to Prometheus's pods only, names are resolved in kube-system only,
  and the database chart admits just the api, the worker and the migration job. **Upgrading:**
  sync the database chart before the app chart (RUNBOOK, "The namespace-wide default deny").
  The chart refuses a range in `trustedProxies` while the policies are off.
- Helm chart: `database.sslmode: verify-full` with `database.caSecret` checks the database's
  certificate and name.
- The web server's content security policy closes plugins and frames and requires Trusted Types;
  the migration from the single-file app has its own upload size at the web server, so a large
  backup is no longer refused there under Compose.
- The API warns once in its log when the proxy in front passes no client address
  (docs/deploying.md says what that proxy must do).
- New databases no longer grant the application's role temporary tables; for an existing one see
  RUNBOOK, "Taking back the TEMPORARY grant".

### Added

- **Two-step sign-in.** Settings → Security sets up a second step after the password: a six-digit
  code from an authenticator app (scan the QR code or type the key), with ten recovery codes.
  Administrators can require it of administrators and remove it for someone who lost their phone;
  `finuance admin remove-second-factor` does the same on the machine. It needs the instance key
  (`FINUANCE_INSTANCE_KEY`), and losing that key locks those accounts until the command is run:
  keep its backup (ADR 0040). Migration 00027.

### Security

- The threat model is at version 3 (`docs/THREAT-MODEL.md`): the public repository and the release
  path, the admission chain, telemetry as a way data can leave, and the risks accepted on purpose.
  `SECURITY.md` names 0.5 as the supported line. The full scan of the release candidate found
  nothing and ignores nothing (`docs/security-scan.md`).
- The Helm chart can deploy by digest: with `image.digests.api` and `image.digests.web` set, the
  images are `name@sha256:…` and the tag only labels the release. A release writes both digests
  into the reference environment's values, and that environment must have them.
- Image signatures can be enforced in the cluster: an Argo CD application for Sigstore
  policy-controller with a policy that admits Finuance's images only with the release workflow's
  signature (ADR 0039). It applies to namespaces that carry the label; the deployment rehearsal
  proves both directions.
- Telemetry carries less: a trace id sent by a client is not continued (it becomes a link) and
  baggage is not propagated; spans leave without the caller's address, the user agent, query
  strings and the text of errors; a PostgreSQL error is logged as its SQLSTATE and the names of the
  constraint and table, never its message or detail; the database logs statements without their
  bound values; the web server's access log names the path without its query string, and one
  request id covers the web server and the API.
- The assistant's connections are held to their kind: a cloud connection reaches public addresses
  only and only over https (TLS 1.2 or newer), a local one no longer reaches link-local addresses,
  and neither follows a redirect to another host. **If you run a model server on a link-local
  address (169.254.x.x, fe80::), move it to a private one.**
- The Helm chart can limit requests per client address at the ingress (`ingress.limits`, off by
  default): a strict limit for sign-in and sign-up, a generous one for the rest and a cap on
  requests in flight, for instances the internet can reach.

## [0.4.2] - 2026-11-19

### Security

- **Imports can no longer exhaust the api's memory**: a signed-in user could upload a file within the
  size limit whose shape made the api allocate many times its size until its container restarted.
  The import limits now apply while a file is read, and a single cell (4 KB) and a row's search text
  (16 KB) have a size limit; a longer cell makes its row an error. Upgrade if people you don't fully
  trust have accounts, or sign-ups are on.
- **Time limits for the api's database work**: a request waits at most 5 s for a lock (an import
  being committed holds its account's), runs a statement at most 30 s and ends a transaction left
  idle after a minute, so one user can no longer keep the api's database connections busy. Answers
  `409 account_busy` and `503 timeout` (with `Retry-After`); a streamed export may take 15 minutes.
  The worker keeps waiting as before.
- **Every amount has a bound**: amounts in requests and in imported files are at most ±10¹² minor
  units (ten billion in a two-decimal currency), so totals of the shared and personal views can't
  overflow; a larger amount in a file is a row error.
- **The assistant never shares by itself**: sharing with a shared group and changing a shared split
  are no longer kinds the panel may apply automatically (others read the result); choices made
  earlier are removed. A proposal that lacks the id it acts on is refused before anything is sent.
- **PostgreSQL 18.7** (row-level security fixes among others; Compose and the Helm chart). It asks
  for a reindex: see `docs/RUNBOOK.md`, "Upgrading PostgreSQL (minor release)".
- **nginx 1.30.7 and OpenSSL 3.5.9-r1** in the web image, **`golang.org/x/crypto` 0.59.0**, **Node
  22.23.5** for the web build; for Kubernetes deployments, **Grafana 13.3.1** (kube-prometheus-stack
  93.4.1) and **Tempo 2.9.5** in the monitoring stack.
- **Verifiable releases**: images now carry SLSA build provenance next to the SBOM, and the
  signature check names the release workflow exactly. `SECURITY.md` shows both commands.
- **Go 1.27.4**: the release fixes a crash of HTTP/1 servers in the standard library's request-size
  limit (GO-2026-6859), which Finuance applies to every request body. Images built from this
  version on are not affected.

### Changed

- **Safe when started without settings**: an instance without `FINUANCE_ENV` now runs in production
  mode, and sign-up is off unless `FINUANCE_ALLOW_SIGNUP=true`. Compose and the Helm chart already
  set both; if you start the binary yourself for development, add `FINUANCE_ENV=dev` and
  `FINUANCE_ALLOW_SIGNUP=true` to your `.env` (the new `.env.example` has them).
- **Profiling endpoints are off by default**: `/debug/pprof` on the admin port needs
  `FINUANCE_PPROF=true`.
- **`compose.prod.yaml`**: the env template's passwords are empty, so the stack refuses to start
  until you set them; the stack has its own subnet (`FINUANCE_SUBNET`, default `172.30.42.0/24`),
  and only that subnet is trusted to pass on the visitor's address.
- **`tools/seal-secrets.sh` writes into the environment repository**: sealed files now go to
  `${FINUANCE_ENV_DIR:-../finuance-env}/<env>/…` instead of `deploy/environments/<env>/`, where the
  homelab's SealedSecrets moved too (ADR 0023). `SECRETS_DIR`, `RUSTFS_SECRETS_DIR`,
  `SMTP_SECRETS_DIR` and `GRAFANA_SECRETS_DIR` still choose another folder.
- **Searches** take at most 16 words, and table offsets at most 1,000,000 (`422` beyond).
- **Upgrading from 0.4.1**: migrations 00025 and 00026 run quickly and cannot fail on existing
  data; `docs/RUNBOOK.md`, "Validating the amount checks", confirms the existing rows once.

### Fixed

- **`502` under heavy traffic in Compose**: the web server opened a new connection to the api for
  every request and could run out of local ports; it now keeps its connections to the api open
  (found by the weekly security scan). Kubernetes deployments weren't affected.
- **A NUL character in a request's text** (such as the sign-in e-mail) is answered
  `422 validation_failed` on every route instead of `500`.
- **The web image build** names a native package that `npm ci` left out, and tries once more,
  instead of failing later in the build.

## [0.4.1] - 2026-11-09

The assistant's model servers under the administrator's control, receipts that find the model that
reads them, and the admin tools' port-forwards in one command.

### Added

- **The assistant's connections under the administrator's control**: Administration → Assistant →
  *Models* switches a connection on or off (also the configuration file's), sets the model questions
  use and a model for photos, and how long the server keeps a model loaded after the last question
  (10 minutes to 4 hours, 30 by default, or the server's own setting). For Ollama it lists the
  loaded models with their memory and unload time, with *Load* and *Unload*. Every change is in the
  Activity.
- **Who picks the model**: the administrator (the default) or each person. When the administrator
  picks, everyone's questions and digests use the default connection and its model, shown read only
  in the assistant's settings.
- **`make observe`**: the port-forwards to Grafana, Prometheus, Alertmanager and Argo CD in one
  command, on this machine's loopback only. Ports another program holds are refused before
  anything starts, a forward that stops starts again, and Ctrl-C ends them all (RUNBOOK "Local
  ports"). `make check` keeps the forwards' local ports the same in the RUNBOOK, the Developer links
  and `make observe`, and away from the ports the Compose stack publishes.

### Changed

- **Photos go to the model that reads them**: a question with a photo of a receipt runs on the
  connection's model for photos when the model in use can't read images, so receipts no longer need
  a switch to another model by hand.
- **Test says what it found**: in the assistant's settings, *Test* now shows "Connected to … · N
  models · time" or why the server can't be reached, and the panel says when the model is unloaded
  (its first answer takes a little longer).

## [0.4.0] - 2026-11-08

An assistant that acts with your consent and an undo, receipts by photo, goals, digests, an advice
library, the assistant in the admin panel, personal access tokens and the `fin` command line.

### Added

- **The assistant's changes, marked and undoable** (Phase 20): everything applied from an assistant
  proposal is recorded with the change, shows ✦ in the table (and a *Source* column: Bank, Manual
  or Assistant), filters with "Only what the assistant changed", and can be undone from the card
  or Settings → Activity → Assistant changes while nothing changed since.
- **More assistant proposals**: set or remove a category's budget, add a cash expense, share
  transactions with a shared group, change how a shared expense is split, set or change a goal.
  Each is still a card you apply.
- **Apply by itself**: in the assistant's settings, tick the kinds of proposals the panel may apply
  without asking (none by default; never more than 50 transactions at once); the card says
  *Applied automatically* and offers Undo.
- **Goals**: save an amount by a deadline, or spend a percentage less on a category than the months
  before. The Overview's *Goals* card shows each goal against a steady pace; the assistant answers
  "how are my goals doing" and proposes new ones. Goals are in the data export.
- **Receipts by photo**: with a model that reads images, the panel's camera button sends a photo of
  a receipt (shrunk and stripped of its location in the browser); the assistant finds the payment
  and proposes the receipt in its note, or proposes the expense. Card payments typed into a bank
  account wait for the bank's file, then a card offers to merge them.
- **Digests**: the assistant can write a short daily or weekly summary in the background (off by
  default; the hour and day in the panel's settings, or *Write one now*). The bell says when it is
  ready; its proposals wait for *Apply*. Administrators can switch digests off for everyone.
- **Advice library**: administrators write short articles (and can start from eight English
  starter articles) that the assistant searches for general money questions and names in its
  answer; each opens in a window from the lookup. Administrators can also add a few lines of
  instructions to every question.
- **Administration → Assistant**: add model connections in the panel next to the configuration
  file's (API keys sealed under `FINUANCE_INSTANCE_KEY`, never shown again), tune a model's
  defaults (temperature, thinking, tools, context, a note), and see each person's assistant runs
  and tokens (never what they asked).
- **Personal access tokens** (Settings → Access tokens): read-only or read-and-write tokens for
  scripts and the `fin` command line, sent as `Authorization: Bearer fin_…`, valid up to a year,
  revocable; no token reaches sign-in, the password, sessions, tokens or Administration.
- **`fin`, the command line** (docs/cli.md): accounts, transactions (with the CSV export), import,
  the month's overview, groups, shared groups, budgets, goals and `fin ask` for the assistant, as a
  table, CSV or JSON; release binaries for Linux, macOS and Windows with checksums.
- **A daily token budget** for the assistant per user, questions and digests together
  (`FINUANCE_AI_TOKENS_PER_DAY`, 200,000 by default; a limit administrators can lower per account).

### Changed

- **Settings, simpler to find**: ⚙ opens *Quick settings* (the colour theme as one button that
  unfolds the others, Dark/Light/System, the interface size, Keyboard shortcuts and **All
  settings**); the import option moved to the new Settings → General; the account menu shows who is
  signed in with Profile and Sign out only, and Administration is in the Settings list.
- **Interface size as a slider**: Auto in the middle, two steps of 10 % smaller and larger around
  it, still the same size at any browser zoom (a size picked from the old list keeps applying until
  the slider is moved).
- **One look for every form**: fields side by side line up and have one height, action buttons have
  one size, and Settings and Administration use the same page width (wide tables scroll).
- Nine migrations (00015–00023: assistant changes, auto-apply, goals, attachments, merge
  dismissals, digests, the advice library, stored connections, access tokens) run with the
  migrations Job as before; take an on-demand backup first (RUNBOOK "On-demand backup").
- The worker writes digests, so it now gets the model connections and, in the Helm chart, the
  egress to the model servers (also in Compose).
- Bank accounts take typed transactions too (a card payment before the bank's file arrives, or from
  a receipt); the import that brings the same payment offers to merge them.
- Helm chart: `assistant.instanceKeySecret` gives the api and the worker `FINUANCE_INSTANCE_KEY`,
  needed only once connections are added in the admin panel (`tools/seal-secrets.sh <env>
  instance-key`).
- A model name the assistant calls as if it were a tool (`set_budget` instead of `propose_action`)
  is read as the proposal it means; small models did this often.
- RUNBOOK "Local ports": the Argo CD and Prometheus port-forwards use local ports 18443 and 19090,
  with the reason for every port-forward's port (another app on 8443, the Compose stack on 9090).
- The kind rehearsal waits for the api through Traefik before its first api request.

### Fixed

- Administration → Assistant: a browser could fill the signed-in account's e-mail into *Models
  offered* and a saved password into *API key* of a new connection; the form now asks browsers and
  password managers not to fill anything in.

### Security

- Trivy, govulncheck and `npm audit` at every severity before the release, the charts' renders
  included: nothing new apart from zlib CVE-2026-4738 in a web image built from the local cache
  (a fresh build, as in the release, upgrades it to 1.4-r0). `golang.org/x/crypto` GO-2026-5932 is
  still not reachable and stays on the tagged v0.58.0, since its fix is only an untagged commit
  ([docs/security-scan.md](docs/security-scan.md)).
- THREAT-MODEL: auto-apply and image injection (T12, T13), photos and decompression bombs (I18,
  D9), background digests (D5), sealed connection keys and the administrator's instructions (I17,
  T11), access tokens (S9, E7).

## [0.3.0] - 2026-11-06

Administrators, colour themes, cash accounts, friends, notifications and shared groups with
settle-up.

### Added

- **Administrators** (Phase 19): an admin panel for accounts (create with a one-time password,
  role, disable, reset password, sign out everywhere, delete), per-account and default limits
  (accounts, groups, imports per day, largest file, assistant questions per hour), where a new
  user's view starts, feature switches (Overview, Assistant) and instance usage. Administrators see
  counts, never anyone's financial data; their actions appear in the affected account's Activity.
  `finuance admin grant-admin|revoke-admin` and `create-user -admin`.
- **Colour themes**: Indigo, Fjord, Grove and Ember, each dark and light, picked in Settings →
  Appearance; administrators make more in a theme editor with a live preview and approve them only
  when every text colour reaches WCAG AA.
- **Cash accounts**: a wallet whose transactions you add by hand (Add or `N` in its toolbar; edit
  and delete in the inspector). Rules categorise them and an ATM withdrawal pairs with the cash it
  put in the wallet; files go into bank accounts only.
- **Friends**: show a short code (valid for the hour) or enter someone's in Settings → Friends;
  friends see each other's display name, never the e-mail address. Administrators can switch it
  off; it is off in demo mode.
- **Notifications** in the title bar (a bell with the unread count): a friend added or removed you,
  invited you to a shared group, joined, left, added an expense or recorded a payment.
- **Shared groups** and the **Shared tab** (`S`): share a personal group with friends (Groups menu
  → Share…) or start an empty one; members see copies of what is shared (date, amount, payee,
  category, note, who paid), never accounts or bank descriptions. Expenses from your transactions
  follow them (category, note, delete + re-import), typed expenses and cash count too; equal,
  shares or exact splits; balances with the fewest payments to settle up; CSV export; a timeline of
  your trips and groups with per-year sums and "next time" hints. Switchable by administrators,
  limited per user (`maxSharedGroups`), off in demo mode; shared groups are in the data export.

- **Daily digest** (observability chart `digest.*`, on for the homelab): one e-mail a day at 12:00
  Central European Time with the last 24 hours (availability and latency against the SLOs, traffic
  and its peak, imports, failed jobs, the database and backups, CPU, memory, disk, restarts, the
  alerts that fired) and every figure far from its previous 7 days.
- **Developer section** for administrators (`FINUANCE_DEVELOPER_MODE`, chart `developer.*`; off by
  default, refused in demo mode): the build, the source repository and links to the tools around
  the instance (dashboards, Argo CD, CI/CD), each with how to get in and commands to copy. Anyone but
  an administrator gets `404`, and nothing else reveals it.

### Changed

- The assistant's send button uses the solid accent colour, like the other filled buttons.
- Settings and Administration are centred beside their section list; Settings links to
  Administration for administrators, and back.

## [0.2.0] - 2026-11-03

Categories, rules and transfers, group reports, assistant actions, an activity log, demo mode, and
what's needed to run Finuance on Kubernetes with observability and security scans.

### Added

- **Categories and rules**: rename, merge or delete categories (re-imports follow); rules from any
  search and filters, applied in order to every import with a live preview, never overriding a
  category set by hand; rule suggestions after repeated manual categorisation; bulk category and
  transfer actions on a selection.
- **Transfers** between the user's own accounts are detected, shown with a badge (and hideable), and
  left out of charts, the overview and the assistant's totals.
- **Group reports** with totals, span, spending per day, categories, top payees, an optional budget
  and CSV export; weekly and yearly recurring payments besides monthly ones.
- **Assistant actions**: the assistant can propose categorising rows, grouping them or creating a
  rule, applied only when the user presses *Apply*; two more tools (budget status, group summary).
- **Activity log** (Settings → Activity): sign-ins and failed sign-ins, sign-outs, password changes,
  revoked sessions, imports, deletions, exports, rule runs, migrations, duplicate decisions and
  applied assistant proposals, with address and browser, kept for 365 days.
- **Demo mode** (`FINUANCE_DEMO_MODE`): "Try the demo" gives each visitor a throwaway user with a demo
  year, deleted after 24 hours, with limits on uploads, demo creation and the assistant.
- **Kubernetes deployment**: Helm chart, CloudNativePG database chart with backups and point-in-time
  recovery, Sealed Secrets, an Argo CD app-of-apps, and a weekly rehearsal on kind in CI.
- **Observability**: traces from requests into background jobs and their SQL, job metrics, six Grafana
  dashboards and SLO alert rules as code, and a local Grafana profile (`make up-observability`).
- **Load test** (k6, `make loadtest`) and **security scans**: OWASP ZAP weekly, Trivy on images and
  the repository at HIGH and CRITICAL, `npm audit` ([docs/security-scan.md](docs/security-scan.md)).
- **Running on single-node k3s** (the reference environment, step by step in
  [docs/RUNBOOK.md](docs/RUNBOOK.md#bootstrap-on-single-node-k3s)): a Tailscale `*.ts.net` certificate
  renewed by a host timer, the ingress reachable only from the tailnet, an in-cluster backup bucket
  (RustFS), per-machine values from a private repository as a second Argo CD source, the monitoring
  stack (kube-prometheus-stack, Tempo, Finuance's dashboards and e-mail alerts) through Argo CD, and
  an image admission policy (a built-in `ValidatingAdmissionPolicy`: per-namespace registries).
- `FINUANCE_AI_IDLE_TIMEOUT` (default 60 s): how long a model stream may stay silent, for model
  servers that read a long prompt for over a minute before the first token.
- Ingress monitoring: Traefik's request metrics, an "At the ingress" row on the SLO dashboard, and
  alerts when requests fail at the ingress or no api pod is ready.
- Drills and scans for a deployed cluster: `tools/homelab-drill.sh` (restore, pod kills under load,
  SSE timing) and `tools/openvas-scan.sh` (Greenbone OpenVAS, and a port scan from the LAN).

### Changed

- Search uses a per-user trigram index (migration 00011, needs the `btree_gin` extension, created by
  the migration): its cost no longer grows with the number of users. Building the index blocks writes
  to transactions for about 25 s per million rows.
- A new user's first large import no longer slows down quadratically (20,000 rows: minutes → seconds).
- The web container sends each security header once and uses relative redirects.
- The api's readiness check reports a database that only answers slowly as busy for 30 seconds
  instead of failing, so a busy database no longer takes every replica out of the Service at once
  (the probe also allows 3 s); a real error or a schema behind still fails at once.
- The database bootstrap lets PostgreSQL's monitoring role (`pg_monitor`, statistics only) connect,
  so CloudNativePG's database metrics work. An existing cluster needs the grant once (RUNBOOK
  "Monitoring").
- Helm chart: `ingress.entryPoints` (e.g. HTTPS only); `otel.endpoint` must be the full traces URL;
  the migrations Job retries in the same pod.
- The duplicates window no longer re-renders every group while you type in its filter.

### Security

- `golang.org/x/net` 0.62.0 (GO-2026-6866, GO-2026-6860; not reachable from Finuance's code).
- The web image upgrades zlib (CVE-2026-85091) and musl (CVE-2026-107891) from the Alpine mirror, since
  its nginx base image hasn't been rebuilt with the fixes (`--build-arg APK_OFFLINE=1` skips this for
  builds without network access).
- Trivy, govulncheck and `npm audit` at every severity before the release: nothing open apart from
  `golang.org/x/crypto` GO-2026-5932, which has no fix and isn't reachable ([docs/security-scan.md](docs/security-scan.md)).

## [0.1.0] - 2026-10-23

The first release: the single-file prototype rebuilt as a self-hosted, multi-user web application,
with every feature of the prototype and a way to move its backups over.

### Added

- **Import bank exports** (CSV/TSV): drag and drop or ⌘/Ctrl O. An import window shows the raw file and a
  preview before anything is saved; encoding (UTF-8, Windows-1250, ISO-8859-2, Windows-1252), delimiter,
  header row (also after a long preamble, as in mBank exports, or with blank header columns, as in PKO BP
  exports), date order, one signed amount or debit/credit columns, inverted signs and summary rows are
  detected and can be changed. Remembered layouts and a fast path for known files.
- **Overlapping exports are merged**: a transaction is kept as often as it occurs in any one file; a
  duplicates window shows every version and lets you pick one or keep both.
- **The transaction table** for 100,000+ rows: accent-insensitive search, a per-column filter language
  (`>100`, `-50..50`, `2025-03`, `!uber`, `=`), value checklists, sorting, resizable and hideable
  columns, CSV export, an inspector with the bank's original row, categories and notes, keyboard
  navigation, multi-selection with a quick bar, and colour-coded groups that survive re-imports.
- **Insights**: summary, spending by category, cash flow, month pace, top payees and a daily calendar,
  following the table's filters and period; each chart opens larger with its numbers as a table.
- **Monthly overview**: income, spending, savings and projection, highlights, budgets per category,
  recurring payments, money flow, largest expenses, savings rate, weekday spending, the account
  balance, and investment, yield-on-cost and FI/RE cards.
- **AI assistant** (optional): questions in English or Polish, answered by a model the admin connects
  (LM Studio, Ollama, vLLM or any OpenAI- or Anthropic-compatible server, local or cloud) through
  read-only lookups shown with each answer; cloud models need each user's consent; chats are deleted
  after 2 days unless the user chooses otherwise.
- **Migration from the single-file app**: Settings → Data reads a backup (decrypting a locked one in
  the browser) and moves the files, groups and labels, duplicate decisions, budgets, opening balance,
  investments, chats and appearance settings, with a report of anything that didn't match.
- **Accounts and privacy**: sign-up or admin-created users, Argon2id passwords, server-side sessions,
  CSRF protection, rate limits, row-level security in PostgreSQL for every user table, a strict
  Content Security Policy, data export (zip) and account deletion.
- **Operations**: one Go binary (API, worker, migrations, admin CLI), PostgreSQL, a React web app
  behind nginx; Prometheus metrics and OpenTelemetry traces; Docker Compose for development and
  production (`compose.prod.yaml` with the released images).
- **Quality**: a legacy oracle compares the import engine, filters, charts, overview and assistant
  tools with the prototype, and a parity harness compares both apps on the same data
  (`docs/parity-report.md`); accessibility checked with axe (WCAG 2.1 AA, `docs/a11y.md`).

### Release artefacts

- Images `ghcr.io/x2db736ac85h/finuance-api` and `ghcr.io/x2db736ac85h/finuance-web` for linux/amd64 and
  linux/arm64, signed with cosign (keyless), with SPDX SBOM attestations.
- `compose.prod.yaml` and `finuance.env.example` (copy it to `.env`): everything needed to run it with Docker Compose.
