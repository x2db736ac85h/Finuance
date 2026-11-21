# 0041. Synthetic users: the server decides whose traffic is synthetic

- Status: accepted
- Date: 2026-11-21

## Context

Phase 25 adds a traffic simulator: a handful of made-up users that sign in to a lab instance and
use it all day, so that dashboards, alerts and traces have something to show (PLAN Phase 25). Their
requests and jobs must be told apart from people's everywhere: left out of the service-level
objectives on an instance that people use, shown or hidden on every dashboard, and findable in logs
and traces (PLAN Phase 24, task 24.7).

The obvious way, a request header such as `X-Synthetic: true`, puts the decision in the client's
hands. Anyone could send it: a person to keep their failing requests out of the error budget, or
a script to pass load off as ordinary use. THREAT-MODEL already says that nothing a client sends
decides what is traced (B9, T16); the same has to hold for what is counted.

## Decision

Who is synthetic is a fact about the user, stored by the server.

- `users.kind` is `person` (the default, everyone until now) or `synthetic` (migration 00029). The
  table refuses a synthetic administrator.
- A user becomes synthetic through `auth.Service.MarkSynthetic` only, which Phase 25's
  `finuance admin synthetic-users` will call. It works only on an instance started with
  `FINUANCE_SIMULATION=true`, never for an administrator, and can't be undone.
- On an instance without `FINUANCE_SIMULATION` a synthetic user can't sign in, and a session or an
  access token it still holds resolves as if it did not exist. A backup of a lab restored
  somewhere else therefore brings no working synthetic accounts.
- The session and token layer reads the kind with the user and marks the request: `traffic` on the
  access line, the label `traffic` (`real`, `synthetic`) on `http_server_requests_total`,
  `http_server_request_duration_seconds`, `finuance_jobs_total` and the assistant's three counters,
  and the span attributes `finuance.synthetic=true` and `user_agent.synthetic.type=test`. Requests
  without a user (signing in, the probes) are `real`.
- A job inherits the mark from the request that queued it: it is written into the job's metadata
  from the server's own context, next to the trace context, and read by the worker, which labels the
  job's metric, lines and span and passes the mark on to what the job queues.
- No header, cookie, query parameter or body field is read for any of this; a test sends every
  claim it can think of in both directions.
- The service-level rules count `traffic!="synthetic"` unless the observability chart's
  `slo.includeSynthetic` is set, which a lab does. Every dashboard has a Traffic variable.
- Guard rails on the kind: never an administrator; never friends with a person, in either
  direction (so a synthetic user reaches no person's shared groups or notifications); counted on
  its own in Administration → Usage and named in Administration → Users.

`users.is_demo` stays as it is. A demo visitor is a person trying the product and counts as real.

## Consequences (positive, negative, follow-ups)

- The numbers on an instance people use can't be bent by a client, and a lab can decide to count
  its simulator.
- Six metrics gain a label with two values. Until a release with the label runs, their series
  carry none; the rules and dashboards match both forms (`!=` and a regular expression that also
  matches no label), so nothing is renamed and no history is lost.
- The simulator needs real accounts and real sign-ins on the lab, which is also what makes its
  traffic representative.
- Import metrics (`finuance_imports_total`, `finuance_import_rows_total`), the durations of jobs
  and of model requests are not labelled: the plan names the request and job counters, and a
  failing import of a synthetic user should raise the same alert on a lab.
- Follow-up (Backlog): one `users.kind` column instead of `is_demo` plus `kind`, once the demo
  mode is touched again.
- Phase 25 adds the command that creates and marks the users.

## Alternatives considered

- **A request header or a user-agent convention.** Simplest for the simulator, and settable by
  anyone, in either direction. Rejected for the reason in the context.
- **A header trusted from one address or with a shared secret.** It moves the decision to network
  layout or to one more secret, and a job queued by such a request would still need the mark
  carried for it. The user's row needs neither.
- **A separate instance only, with no label.** The lab is a separate instance anyway (the
  maintainer, 2026-11-16), but a label costs little and lets one Prometheus and one Grafana serve
  both without mixing them, and lets a lab mix people and simulator when wanted.
- **Reusing `is_demo`.** Demo visitors are people; their errors are real experience.
