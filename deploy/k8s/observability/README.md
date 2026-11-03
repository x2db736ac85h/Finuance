# finuance-observability

Dashboards, SLO recording rules, alerts and the e-mail receiver for Finuance (PLAN 17.3, 17.4;
`docs/SLO.md`). Install it next to kube-prometheus-stack (Phase 18):

```sh
helm upgrade --install finuance-observability deploy/k8s/observability -n monitoring \
  --set appNamespace=finuance --set rules.labels.release=kube-prometheus-stack \
  --set alerting.email.enabled=true --set alerting.email.to=… --set alerting.email.from=… \
  --set alerting.email.smarthost=smtp.example.com:587 --set alerting.email.authUsername=…
```

It needs the app chart's `podMonitor.enabled=true` (api and worker, `job` = `finuance-api` /
`finuance-worker`) and the database chart's `cluster.podMonitor=true` (`job` = `finuance-db`). With
`ingress.traefik.enabled` it also scrapes Traefik (job `traefik`; Traefik needs
`metrics.prometheus.addRoutersLabels`) for what users get at the ingress, including requests refused
before they reach the api: the SLO dashboard's "At the ingress" row and `FinuanceIngressErrors`. The
homelab's values: `deploy/environments/homelab/observability-values.yaml`.

| Part | What |
|---|---|
| `files/dashboards/*.json` | Six Grafana dashboards (API, imports and jobs, assistant, PostgreSQL, Go runtime and pool, SLOs), written by `tools/observability/dashboards.mjs`; ConfigMaps labelled for the Grafana sidecar |
| `templates/prometheusrule.yaml` | SLO recording rules (5 m … 3 d), burn-rate alerts, app, database, backup and pod alerts (crash loops, no api pod ready), and the ingress alert; groups switched by `rules.database`, `rules.backups`, `rules.kubernetes`, `ingress.traefik.enabled` |
| `templates/traefik-podmonitor.yaml` | Traefik's request metrics (`ingress.traefik`) |
| `templates/alertmanagerconfig.yaml` | Every `Finuance*` alert by e-mail; the monitoring stack's own alerts of `alerting.platform.severities` (default `critical`); a weekly `Watchdog` mail as a sign of life (`alerting.heartbeat`, Mondays 06:00–08:00 UTC); the SMTP password from a Secret |
| `tests/rules.test.yaml` | promtool unit tests: each alert fires on the right series and stays quiet below its threshold |
| `metrics.txt`, `external-metrics.txt` | The metrics the dashboards and rules may use (ours from `TestMetricNames`; CloudNativePG 1.30.1, Barman Cloud plugin v0.15.1, kube-state-metrics, Traefik) |

`make helm-lint` runs `tools/check-observability.sh`: dashboards equal the generator's output, the
chart renders and validates, `promtool check rules` and `promtool test rules` pass, and every metric
used exists.
