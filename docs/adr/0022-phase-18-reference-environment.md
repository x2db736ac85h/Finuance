# 0022. Phase 18 runs on a single-node k3s reference environment

- Status: accepted
- Date: 2026-10-27 (machine details moved to the private environment repository on 2026-10-30)

## Context

Phase 18 (homelab rollout) deploys the Phase 16 and 17 artifacts on a real cluster: Argo CD,
sealed secrets, CloudNativePG with backups, TLS, the restore drill, the model endpoint,
observability, a load test and an OpenVAS scan. The homelab is still out of reach. Our Product
Engineer has a personal machine that can run the whole stack: enough memory for the cluster and a
small NVIDIA GPU for a local model.

The cloud session that builds Finuance can't reach a private machine, so the hands-on work runs in
a Claude Code session on that machine.

Finuance is meant to run on any Kubernetes homelab, so this repository describes the kind of
environment and the steps, not the particular machine. Its hardware, network and measurements live
in a private environment repository (`finuance-env`), which also holds the deployed host name
(18.2).

## Decision

For Phase 18 the "homelab" is **single-node k3s on a personal machine with a small NVIDIA GPU**:

- **Cluster:** k3s with its bundled Traefik (`kube-system/traefik`), storage class `local-path`,
  pod CIDR `10.42.0.0/16`. Argo CD, cert-manager (needed by the Barman Cloud plugin), CloudNativePG
  and Sealed Secrets come from the Phase 16 app-of-apps.
- **TLS and access:** a Tailscale `*.ts.net` certificate for `<host>` (`<machine>.<tailnet>.ts.net`).
  The chart runs in TLS mode `secret`, and a small script on the host renews the certificate (90
  days) and updates the secret. The app is reachable from the tailnet only.
- **Backups:** RustFS inside the cluster as the S3-compatible bucket, from the hardened rehearsal
  manifests. WAL archiving and the daily backups work exactly as on a real cluster.
- **Model:** Ollama on the host, outside k3s, as ADR 0020 describes for LM Studio: one `openai`
  connection to `http://<tailnet-ip>:11434/v1` and one `cidr` egress rule. A 4 GB GPU fits a model
  of about 4B parameters (for example `qwen3:4b`) with an 8k context when the KV cache is quantised.
- **Observability:** kube-prometheus-stack (Prometheus Operator, Alertmanager, Grafana with the
  dashboard sidecar) and Tempo. The charts' PodMonitors, PrometheusRule and AlertmanagerConfig
  apply unchanged.
- **Scans:** Greenbone Community Edition (OpenVAS) containers against the deployed instance.

Every tool is pinned by version in `docs/RUNBOOK.md`. None of them is an app dependency.

## Consequences (positive, negative, follow-ups)

- Every Phase 18 task runs on a real k3s, not kind: Argo CD from Git, sealed secrets, backups to a
  bucket, TLS through Traefik, a model on a GPU.
- A personal machine sleeps and reboots. While it does, backups, alerts and scheduled jobs pause;
  that is expected and not a gate failure. A reboot doubles as the node-failure drill.
- The backups share the database's disk. They protect against mistakes and pod or volume loss,
  not against losing the machine's disk. The RUNBOOK says so, and an off-machine bucket is the
  first change for an always-on host.
- With one node, the second CloudNativePG instance and the api replicas share the hardware.
  Pod-kill and restore timings are not comparable to the kind rehearsal's, and a node failure
  takes everything down.
- The repository and the images are private. Argo CD needs a read-only deploy key and the cluster
  an image pull secret, both sealed.
- Machine-specific values (hardware, network, host name, measurements) are kept out of this
  repository; the RUNBOOK uses placeholders and the host files are templates.
- Follow-up: Phase 19 chooses an always-on host for the instance and the public demo. These values
  then move there with only host, storage and bucket changing.

## Alternatives considered

- **Wait for the homelab:** blocks Phases 18 and 19 for an unknown time.
- **kind on the same machine:** the CI rehearsal already covers kind. k3s is what the homelab runs,
  with its Traefik, storage and networking.
- **A cloud VM:** costs money, holds the real data off-premises, and has no GPU for the model.
