# Service level objectives

Two SLOs for one Finuance installation (PLAN task 17.4), measured from the api's own metrics over a
rolling 30 days. The recording rules, burn-rate alerts and their tests are code:
`deploy/k8s/observability/templates/prometheusrule.yaml` and `tests/rules.test.yaml`
(`make helm-lint` runs `promtool check rules` and `promtool test rules`). The *Finuance · SLOs*
dashboard shows both, the budget left and the burn rates.

| SLO | Objective | SLI | Error budget (30 days) |
|---|---|---|---|
| Availability | 99.5 % of API requests don't fail with a 5xx | `http_server_requests_total`, `status_class="5xx"` ÷ all | 0.5 % of requests ≈ 3 h 36 min of full outage |
| Latency | 95 % of table and chart requests answer in under 300 ms | `http_server_request_duration_seconds`, bucket `le="0.3"` ÷ count | 5 % of those requests |

## What counts

- **Availability** counts every API route (`job="finuance-api"`) except the health checks
  (`GET /healthz`, `GET /readyz`), unmatched paths and the assistant's routes. The assistant
  depends on a model server outside the app (ADR 0020); its failures are watched by
  `FinuanceAssistantModelErrors` instead, so a sleeping LM Studio doesn't burn the app's budget.
  4xx answers are the client's (validation, conflicts, rate limits) and count as good, as does
  499 for a request whose client went away before the answer (a cancelled fetch).
- **Latency** counts `POST /api/v1/transactions/query` and `POST /api/v1/insights/*`: the requests a
  user waits for while browsing the table and the charts. 300 ms is the target of PLAN §8.3 for
  100k-row users; the latency histogram has a bucket at exactly 0.3 s
  (`TestLatencyHistogramHasTheSLOBucket`). Exports, imports and the assistant are long by design and
  have their own dashboards.
- Measured at the api, so time in Traefik and nginx isn't included; the load test (`make loadtest`)
  measures end to end. Requests the ingress refuses before they reach the api (503 when no api pod
  is ready, timeouts) don't count either: the Phase 18 load test had 29 % of requests fail that way
  while this availability read 100 %. Where the chart can read Traefik (`ingress.traefik`, the
  homelab), the SLO dashboard's "At the ingress" row shows both, and `FinuanceIngressErrors` and
  `FinuanceApiNoReadyPods` page on them.

## Alerts

Multi-window, multi-burn-rate alerts (Google SRE workbook, chapter 5). A burn rate of 1 spends the
budget in exactly 30 days.

| Alert | Severity | Fires when | Budget spent before it fires |
|---|---|---|---|
| `FinuanceAvailabilityBurnFast` | page | error ratio > 14.4 × 0.5 % over 1 h **and** 5 m | 2 % |
| `FinuanceAvailabilityBurnSlow` | ticket | error ratio > 6 × 0.5 % over 6 h **and** 30 m | 5 % |
| `FinuanceLatencyBurnFast` | page | slow ratio > 14.4 × 5 % over 1 h **and** 5 m | 2 % |
| `FinuanceLatencyBurnSlow` | ticket | slow ratio > 6 × 5 % over 6 h **and** 30 m | 5 % |

The short window makes an alert stop soon after the problem does. Other alerts in the same chart:
targets down or absent, import commits failing after their retries, job kinds failing over 10 %,
the connection pool over 90 %, assistant model errors, PostgreSQL down or absent, replication lag,
WAL archiving failing, the last backup older than 26 hours (or none reported), pods in
CrashLoopBackOff, no api pod ready for a minute, and over 5 % of Finuance's requests failing at the
ingress for 5 minutes (with `ingress.traefik`). Every alert goes by e-mail (Phase 17 decision): pages at once and every hour,
tickets grouped and once a day (`alerting.email`, an `AlertmanagerConfig`).

## Daily digest

With `digest.enabled` (the homelab has it), one e-mail a day at 12:00 Central European Time
(`digest.hour`, `utcOffsetHours`, `euSummerTime`) sums up the last 24 hours:

- **Service:** availability and the share of table and chart requests within 300 ms against the
  SLOs above, p95 latency, requests and the peak rate (15-minute average), server errors at the api
  and at the ingress.
- **Activity:** imports committed and failed, failed background jobs by kind, assistant runs.
- **Database:** size and growth, the replication lag's maximum, WAL archiving, the last backup's age.
- **Machine:** CPU and memory peaks, the root disk, container restarts by namespace.
- **Alerts that fired**, with how long.
- **Outside the usual:** every figure more than 3 standard deviations from its previous 7 days, or,
  where that history is flat or shorter than 3 days, more than 50 % from its average
  (`digest.anomalies`). A figure with no history yet isn't judged, so the first week stays quiet.

It is the `FinuanceDailyDigest` alert (severity `digest`): the `finuance-digest.rules` group keeps
each figure over the trailing 24 hours every 10 minutes (`finuance:digest_24h{indicator, unit}`),
with its 7-day average and spread, and the alert fires for the first 15 minutes of 12:00 local time
(the expression works out EU summer time itself, as Alertmanager's time intervals can't take a time
zone here). Its text is built from queries when it fires and goes out through the e-mail receiver
on its own route: once, no "resolved". `tests/digest.test.yaml` checks the text on 9 days of data,
`tests/digest-time.test.yaml` the hour across both switches of summer time.

The digest runs on the same machine as everything it reports on: a day without it means the machine,
Prometheus, Alertmanager or the mail path was down (as on 2026-11-05, when the machine lost its
network for 9½ hours). There is deliberately no outside heartbeat (our Product Engineer, 2026-11-06).

## Error budget policy

- Budget left: ship as usual.
- Under 25 % left: no risky changes (migrations, dependency upgrades) until it recovers; fix
  what burned it first.
- Spent: only fixes and rollbacks; write down what happened in the session log.

For a single-person homelab this is a guide, not a contract; the numbers are there so a bad
release or a slow query shows up as a number rather than a feeling.

## Where it runs

The dashboards and rules need the Prometheus Operator and Grafana's dashboard sidecar
(kube-prometheus-stack), installed in Phase 18 with the alert receiver's SMTP secret. Until then,
`make up-observability` runs the same dashboards locally against Compose (Grafana's `otel-lgtm`
image), and `make loadtest` checks the latency SLO under load (docs/perf.md "Phase 17").
