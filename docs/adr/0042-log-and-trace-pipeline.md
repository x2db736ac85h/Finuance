# 0042. The log and trace pipeline: Alloy in front of Loki and Tempo

- Status: accepted
- Date: 2026-11-21

## Context

Until Phase 24 the app's logs were only on the containers' standard output: a pod's lines went with
the pod, and nothing joined a line to the trace of the same request (PLAN Phase 24, task 24.6;
Phase 17 had planned Loki and Alloy and never built them). The api and the worker sent their traces
straight to Tempo.

Two of the accepted risks of THREAT-MODEL v3 waited for a collector: nginx's error log names the
request line with its query string, and PostgreSQL's log of a failing statement keeps the error's
detail and the statement, both of which can quote a value. A central log store keeps lines longer
and shows them to more people than the pod's own log does, so the collector has to remove them.

The reference machine is a single node that also runs the database, the monitoring stack and a
local model server (PLAN Appendix H): whatever is added has to be small and switchable.

## Decision

- **Grafana Alloy is the one collector**, for logs and for traces. The api and the worker send their
  traces to Alloy (OTLP over HTTP), which deletes the same span attributes as the app's own
  `observability.Redact` (a second line of defence; `TestCollectorDropsTheSameAttributes` keeps the
  two lists equal) and sends them on to Tempo. Phase 27 adds a second destination there.
- **Alloy reads the pods' logs through the Kubernetes API** (`loki.source.kubernetes`), not from
  the node's files: no host path, no root user, no DaemonSet. A Role in each namespace it reads
  (only `finuance` on the homelab) lets it list pods and read their logs, nothing else; the chart
  checks that in `tools/check-helm.sh`.
- **The log stages** (`deploy/k8s/telemetry/files/stages.alloy`) are the only place a line is
  changed: the app's JSON lines get `trace_id` and `request_id` as structured metadata and `traffic`
  as a label (ADR 0041); nginx's error lines lose the request line, the upstream address and the
  referrer; PostgreSQL's lines lose an error's detail, its statement, its context and an internal
  query (the message and the SQLSTATE stay). A test runs the stages on sample lines and compares
  the result with an expectation (`tools/check-observability.sh`); the rehearsal makes a failing
  statement on a real cluster and checks that its value does not reach Loki.
- **Loki runs as a single binary** with its index and chunks on a local volume and 14 days of
  retention, one tenant, reachable only from Alloy (pushes) and Grafana (queries). Labels are the
  namespace, the pod, the container and `traffic`; everything else stays in the line.
- **Both are one chart in this repository** (`deploy/k8s/telemetry`), one Argo CD application in
  the project `platform-manifests`, images by digest. `logs.enabled: false` switches the logs off
  (Loki, the Role, the log pipeline and Grafana's Loki data source); Alloy then still carries the
  traces. On the homelab the logs stay on while Loki and Alloy together use less than 1 GiB of
  memory and a fifth of a CPU (the maintainer, 2026-11-21).
- Grafana links both ways: a line's `trace_id` opens the trace in Tempo, and a span's "logs for
  this span" queries Loki by the namespace and the trace id. The API and jobs dashboards have log
  panels.
- **Compose** gets the same stages: an Alloy container in `compose.observability.yaml` reads the
  project's containers through the Docker socket and writes to the Loki inside `otel-lgtm`.

## Consequences (positive, negative, follow-ups)

- From a request id a user quotes, an operator finds the request's lines, its trace and the job it
  started (the reference in the toast is the `request_id` field).
- Traces now take one more hop: while Alloy is down or being replaced, the app's exporter retries
  and then drops the spans of that moment. kube-prometheus-stack's rules for a pod that is not ready
  cover Alloy and Loki like any other pod.
- Reading logs through the API costs the kubelet and the API server a little more than reading
  files. A new Alloy pod starts again from what the API still has, so a few lines can arrive twice.
- The `platform-manifests` project now holds one grant: a read-only Role in the app's namespace.
  The check that its manifests use exactly the listed kinds covers the rendered chart.
- The Compose profile mounts the Docker socket, which is root on the machine: it is for a
  development machine only, and its file says so.
- Not done: Prometheus does not scrape Alloy's and Loki's own metrics (no alert on dropped spans or
  refused pushes); their volume and memory are watched through the container metrics for now.

## Alternatives considered

- **Grafana's Helm charts for Loki and Alloy.** Loki's chart runs a gateway, caches and a canary
  next to the single binary, more than one machine needs; Alloy's chart defaults to a DaemonSet
  with host paths. A small chart here is hardened like the app's pods and is checked by the same
  tools, as Tempo's manifests are (Phase 18).
- **Reading the node's log files** (`/var/log/pods` with a host path). Cheaper for the kubelet, but
  it needs a root user or a capability to read files the container runtime writes as root, and
  sees every namespace on the node.
- **A separate OpenTelemetry Collector for traces.** One more component with the same job; Alloy
  embeds the collector's pieces (PLAN 24.6: not needed at this size).
- **Leaving traces on the direct path to Tempo.** One hop fewer, but no single place to drop
  attributes or add a destination.
- **Logs through the app's OTLP exporter** (an `slog` bridge). A new Go dependency, and the lines
  of nginx and PostgreSQL would still need a collector.
