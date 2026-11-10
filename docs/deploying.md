# Deploying

Two ways to run Finuance: Docker Compose on one host, or Kubernetes with Helm, CloudNativePG and Argo CD. Day-to-day operations (backups, restores, rollbacks, playbooks) are in the [runbook](RUNBOOK.md).

**Docker Compose.** [`deploy/compose/compose.prod.yaml`](../deploy/compose/compose.prod.yaml) runs the released images (`ghcr.io/x2db736ac85h/finuance-api` and `finuance-web`, amd64 and arm64, signed with cosign). It needs only itself and a `.env` ([`.env.prod.example`](../deploy/compose/.env.prod.example), attached to every [release](https://github.com/x2db736ac85h/Finuance/releases) as `finuance.env.example`). Only the web container is published, on `127.0.0.1:8000` by default; the database, the API and the admin ports stay on the internal Compose network.

- Put a reverse proxy with TLS in front of port 8000 (Caddy, Traefik, nginx) and set `FINUANCE_PUBLIC_ORIGIN` to its URL. Production mode sends secure cookies only, so the app works over HTTPS, or over plain http on `localhost` (e.g. through an ssh tunnel to a server).
- Sign-up is off by default. Create accounts with the admin CLI: `docker compose … exec -T api /finuance admin create-user --email you@example.com --password-stdin` (it reads the password from standard input), or set `FINUANCE_ALLOW_SIGNUP=true`.
- Demo mode (`FINUANCE_DEMO_MODE=true`): visitors start their own throwaway demo user from the sign-in page ("Try the demo"), deleted with its data after 24 hours. Sign-up is off, uploads are limited to 2 MiB, the migration from the single-file app is refused, at most 5 demo users start per hour and address, and the assistant stays off unless `FINUANCE_DEMO_ASSISTANT=true` (then 10 questions per visitor). `make test-e2e-demo` runs it with [`compose.demo.yaml`](../deploy/compose/compose.demo.yaml).
- Verify an image: `cosign verify ghcr.io/x2db736ac85h/finuance-api:<version> --certificate-identity-regexp 'https://github.com/x2db736ac85h/Finuance/' --certificate-oidc-issuer https://token.actions.githubusercontent.com`.
- Back up the database volume regularly. It holds your original CSV files too.

**Kubernetes with Helm, CloudNativePG and Argo CD** (rehearsed weekly in CI on a kind cluster, including a backup restore and pod kills; running on a reference environment, single-node k3s with a small NVIDIA GPU, since Phase 18):
- The app chart [`deploy/helm/finuance`](../deploy/helm/finuance) runs the api (with an HPA), the worker, the web front end, a migrations Job before every upgrade, and NetworkPolicies.
- The database chart [`deploy/k8s/db`](../deploy/k8s/db) runs a CloudNativePG cluster, with WAL archiving and daily backups to S3-compatible storage.
- [`deploy/k8s/argocd`](../deploy/k8s/argocd) is an Argo CD app-of-apps with Sealed Secrets.
- A release opens a pull request that bumps the image tag in [`deploy/environments/homelab`](../deploy/environments/homelab), and merging it deploys.
- [`deploy/k8s/observability`](../deploy/k8s/observability) holds six Grafana dashboards and the alert rules as code: SLO burn rates ([docs/SLO.md](SLO.md)), failing jobs and imports, PostgreSQL, WAL archiving and backup age, failures at the ingress, with an e-mail receiver. The homelab runs it with kube-prometheus-stack and Tempo, all through Argo CD.
- Single-node k3s step by step (Tailscale certificates, an in-cluster backup bucket, the model server on the host, the per-machine values in a private repository, an image admission policy): [RUNBOOK](RUNBOOK.md#bootstrap-on-single-node-k3s). Measured there: restore in 71 s (RPO 96 s), no failed request while pods are killed, the latency SLO held up to about 77 table requests per second, an OpenVAS scan with nothing above "Log" ([docs/perf.md](perf.md), [docs/security-scan.md](security-scan.md)).

Operating it: [docs/RUNBOOK.md](RUNBOOK.md).
