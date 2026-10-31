# Runbook

How to run Finuance on Kubernetes: the first install, deploys and rollbacks, secrets, scaling, backups and restores, and what to do when something breaks. The Compose install is in the [main README](../README.md#deploying).

> **Status (Phase 16).** The manifests are written and checked in CI: `make helm-lint` and the `helm` job render, validate and scan them; the kind rehearsal (`tools/rehearsal.sh`, workflow `k8s-rehearsal`) installs everything on a throwaway cluster and runs the drills below. Nothing is deployed on the homelab yet. Values marked *Phase 18* in `deploy/environments/homelab/` are set during the homelab rollout, and the timings below come from the CI rehearsal until then.

## What runs where

| Piece | Where | Source | Sync |
|---|---|---|---|
| Argo CD app-of-apps `finuance-root` | `argocd` | [`deploy/k8s/argocd/root.yaml`](../deploy/k8s/argocd/root.yaml) | automated |
| CloudNativePG operator | `cnpg-system` | Helm chart `cloudnative-pg` (pinned in [`apps/cnpg-operator.yaml`](../deploy/k8s/argocd/apps/cnpg-operator.yaml)) | automated |
| Barman Cloud plugin (backups) | `cnpg-system` | Helm chart `plugin-barman-cloud` | automated |
| Sealed Secrets controller | `kube-system` | Helm chart `sealed-secrets` | automated |
| Sealed secrets | `finuance` | [`deploy/environments/homelab/secrets/`](../deploy/environments/homelab/secrets) | automated, no prune |
| PostgreSQL cluster `finuance-db` (2 instances) | `finuance` | [`deploy/k8s/db`](../deploy/k8s/db) + `homelab/db-values.yaml` | **manual** |
| api (2–4 pods, HPA), worker, web (2 pods), migrations Job | `finuance` | [`deploy/helm/finuance`](../deploy/helm/finuance) + `homelab/values.yaml` | automated, prune + self-heal |

Traffic goes Traefik → Ingress: `/api/` → api:8080 and `/` → web:8080. NetworkPolicies deny everything else: only Traefik may reach web and api, only api, worker and the migrations Job may reach PostgreSQL, and the api reaches a model server only when the assistant is on. The namespace enforces the `restricted` Pod Security Standard.

Database logins are two CloudNativePG managed roles: `finuance_owner` (owns the schema, runs migrations) and `finuance_app` (api and worker, subject to row-level security). Their passwords live in the basic-auth secrets `finuance-db-owner` and `finuance-db-app`. The pods get them as `PGPASSWORD`, so database URLs never contain a password.

## Prerequisites

- Kubernetes ≥ 1.30 (k3s is fine), `kubectl` access as cluster admin.
- Traefik as the ingress controller (k3s ships it in `kube-system`).
- **cert-manager**: the Barman Cloud plugin needs it for its certificates, and `ingress.tls.mode: certManager` uses a ClusterIssuer (Let's Encrypt). Not part of the app-of-apps, because a homelab usually runs one already.
- Argo CD (`argocd` namespace).
- A storage class for the database volumes (k3s: `local-path`).
- An S3-compatible bucket for backups, **outside the cluster's disks** (MinIO on another machine, Backblaze B2, S3, …).
- Tools on your machine: `kubectl`, `helm` (3.18), `kubeseal` (0.40), mikefarah's `yq` (v4), optionally the `kubectl cnpg` plugin.

## Bootstrap (first install)

1. **Argo CD knows how healthy a child app is.** By default an Application has no health, so sync waves don't wait for the child apps. Add this to the `argocd-cm` ConfigMap (Argo CD's documented snippet):

   ```yaml
   data:
     resource.customizations.health.argoproj.io_Application: |
       hs = {}
       hs.status = "Progressing"
       hs.message = ""
       if obj.status ~= nil and obj.status.health ~= nil then
         hs.status = obj.status.health.status
         if obj.status.health.message ~= nil then hs.message = obj.status.health.message end
       end
       return hs
   ```

2. **Set the homelab values** in `deploy/environments/homelab/values.yaml` and `db-values.yaml` (every *Phase 18* comment: host, issuer, pod CIDR, storage class, bucket), then merge them to `main`.
3. **Apply the root app** once: `kubectl apply -f deploy/k8s/argocd/root.yaml`. Argo CD creates the projects, the operators and the Sealed Secrets controller. `finuance-db` waits for a manual sync, so `finuance` (wave 0) waits too: the Application health check counts a child app that has never synced (`Missing`) as `Progressing` (`homelab/argocd-values.yaml`); with Argo CD's documented snippet alone the next wave went ahead and the migrations Job ran before the database existed (Phase 18).
4. **Back up the Sealed Secrets key** as soon as the controller runs (see [Secrets](#secrets)).
5. **Seal the secrets** and commit them:

   ```sh
   tools/seal-secrets.sh homelab db                                      # random passwords for both roles
   ACCESS_KEY_ID=… ACCESS_SECRET_KEY=… tools/seal-secrets.sh homelab backup
   git add deploy/environments/homelab/secrets && git commit -m "chore(deploy): homelab secrets"
   ```

6. **Sync the database** in Argo CD (`finuance-db` → Sync) and wait for `kubectl -n finuance get cluster finuance-db` to report *Cluster in healthy state*. The first base backup starts right away (`backup.immediate`); check it with `kubectl -n finuance get backups`.
7. **The app syncs by itself**: the migrations Job runs (PreSync), then api, worker and web roll out.
8. **Create the first account** (sign-up is off):

   ```sh
   kubectl -n finuance exec -i deploy/finuance-api -- /finuance admin create-user --email you@example.com --password-stdin <<<'a long password'
   ```

9. Open the public origin, sign in, import a file, and check `kubectl -n finuance get pods`: no restarts.

## Bootstrap on single-node k3s

The Phase 18 reference environment (ADR 0022): single-node k3s on a personal machine with a small NVIDIA GPU, reachable over Tailscale, with a host firewall (ufw) that drops input and forwarded traffic by default. Each step was run there; the versions are pinned. Machine-specific values (hardware, network, host name, measurements) live in the private environment repository (`finuance-env`); here they are placeholders:

| Placeholder | Meaning | How to find it |
|---|---|---|
| `<host>` | the machine's tailnet name, `<machine>.<tailnet>.ts.net` | `tailscale status --json \| jq -r .Self.DNSName` |
| `<tailnet-ip>` | its tailnet address | `tailscale ip -4` |
| `<lan-ip>` | its address on the home network | `ip -br addr` |
| `<router-ip>` | the DNS server CoreDNS forwards to (usually the router) | first `nameserver` in `/run/systemd/resolve/resolv.conf` |

| Tool | Version | Installed with |
|---|---|---|
| helm | v3.18.4 | `go install helm.sh/helm/v3/cmd/helm@v3.18.4` |
| kubeseal | v0.40.0 | `go install github.com/bitnami/sealed-secrets/cmd/kubeseal@v0.40.0` |
| yq (mikefarah) | v4.54.1 | `go install github.com/mikefarah/yq/v4@v4.54.1` |
| kubeconform | v0.7.0 | `go install github.com/yannh/kubeconform/cmd/kubeconform@v0.7.0` |
| k3s (+ `kubectl`) | v1.36.5+k3s1 | official install script, `INSTALL_K3S_VERSION` |
| Tailscale | 1.104 | distribution package |
| cert-manager | chart v1.21.3 | helm (`jetstack`) |
| Argo CD | chart `argo-cd` 10.11.6 (v3.5.6) | helm (`argo`), `homelab/argocd-values.yaml` |
| Ollama | 0.40 | distribution package (+ its Vulkan build for older NVIDIA cards), `host/ollama.conf` |
| Model | `qwen3:4b` (Q4, 2.5 GB) | `ollama pull` |

The commands use `pacman` (Arch); other distributions have the same packages under their own tool.

### 1. Client tools (no root)

The same versions as CI (`.github/workflows/ci.yml`), so `make helm-lint` behaves the same locally. Distribution packages may ship Helm 4 or a different `yq`, so they are not used.

```sh
go install helm.sh/helm/v3/cmd/helm@v3.18.4
go install github.com/bitnami/sealed-secrets/cmd/kubeseal@v0.40.0
go install github.com/mikefarah/yq/v4@v4.54.1
go install github.com/yannh/kubeconform/cmd/kubeconform@v0.7.0
```

With Go from mise the binaries land in the active Go version's own `bin` folder and disappear when that version changes. A fixed `GOBIN` avoids it, e.g. in mise's global config: `[env]` `GOBIN = "{{env.HOME}}/go/bin"` and `_.path = ["{{env.HOME}}/go/bin"]` (otherwise reinstall the tools after a Go update). Inside this repository, `mise.toml` selects CI's Go (the `toolchain` in `backend/go.mod`) and Node 22 (`mise install` once, `mise trust` for the file); `make check-toolchain` fails when `go.mod`, `mise.toml` and `Dockerfile.backend` name different Go versions, and Renovate updates the three in one pull request.

### 2. k3s

v1.36.5+k3s1 (k3s stable channel, 2026-10-30): CloudNativePG 1.30 supports Kubernetes 1.34–1.36. Bundled Traefik, `local-path` storage and metrics-server (the api's HPA) stay on; secrets are encrypted at rest. k3s runs with swap on (it doesn't set `failSwapOn`).

```sh
curl -sfL https://get.k3s.io | sudo INSTALL_K3S_VERSION='v1.36.5+k3s1' sh -s - server \
  --secrets-encryption --write-kubeconfig-mode 600
mkdir -p ~/.kube && sudo install -m 600 -o "$USER" -g "$USER" /etc/rancher/k3s/k3s.yaml ~/.kube/config
export KUBECONFIG=~/.kube/config   # k3s's kubectl otherwise reads /etc/rancher/k3s/k3s.yaml (root only)
```

The script installs `kubectl` and `crictl` as links to the k3s binary (same version); an existing `ctr` is left alone.

**Firewall.** ufw drops input and forwarded traffic by default. Pods must reach the host (DNS, the API server behind `10.43.0.1`, the kubelet for metrics-server, later Ollama), so one input rule allows the pod range on the bridge only. Port 6443 is **not** opened: `kubectl` on the machine uses `127.0.0.1`, and nothing else needs the API. The service range needs no rule (service addresses are rewritten before delivery).

```sh
sudo ufw allow in on cni0 from 10.42.0.0/16 comment 'k3s pods to host'
```

**Pods stay off the home network.** Without a NetworkPolicy, every pod can reach the router, the LAN and the Docker networks, masqueraded as the host. `ufw route deny` doesn't help: k3s puts `KUBE-ROUTER-FORWARD` (which accepts every pod packet no policy denies) at the top of `FORWARD`, before `ufw-before-forward` (`sudo iptables -S FORWARD` shows the order). [`host/k3s-lan.nft`](../deploy/environments/homelab/host/k3s-lan.nft) hooks forwarding at priority −10, ahead of both, and drops pod traffic to private, Docker, tailnet and link-local ranges, except DNS to `<router-ip>`. It is a template: copy it, set `UPSTREAM_DNS`, keep the copy with the environment's private files, and load it before k3s at every boot:

```sh
sudo nft -c -f k3s-lan.nft                                   # dry run of the filled-in copy
sudo install -m 644 k3s-lan.nft /etc/k3s-lan.nft
sudo install -m 644 deploy/environments/homelab/host/k3s-lan.service /etc/systemd/system/k3s-lan.service
sudo systemctl daemon-reload && sudo systemctl enable --now k3s-lan.service
```

If the router or the LAN changes, edit the copy and repeat the install. `sudo nft list table inet k3s_lan` shows the drop counter.

**Check:** the node is `Ready`; coredns, local-path-provisioner, metrics-server, traefik and its svclb pod run; `local-path` is the default storage class; Traefik's LoadBalancer is on `<lan-ip>` (80/443; restricted to the tailnet in step 10); `kubectl top node` works. From a throwaway pod, DNS, the API service, another pod, the internet and the host itself are reachable, and the router is not (it was before the nftables table, on the reference machine):

```sh
kubectl create ns nettest && kubectl -n nettest run t --image=busybox:1.37 --restart=Never --command -- sleep 300
kubectl -n nettest wait --for=condition=Ready pod/t
kubectl -n nettest exec t -- nslookup github.com              # works
kubectl -n nettest exec t -- nc -z -w5 github.com 443         # works
kubectl -n nettest exec t -- nc -z -w5 <router-ip> 80         # must fail
kubectl delete ns nettest
```

To remove k3s completely: `sudo /usr/local/bin/k3s-uninstall.sh`, then `sudo systemctl disable --now k3s-lan.service` and the ufw rule.

### 3. Tailscale

```sh
sudo pacman -S --needed tailscale
sudo systemctl enable --now tailscaled
sudo tailscale up --operator="$USER"       # log in through the printed URL
```

In the admin console (DNS page): **MagicDNS** on, **HTTPS Certificates** on.

**Names are public.** Tailscale's certificates come from Let's Encrypt, so the full name of every machine that gets one is written to the public Certificate Transparency logs for good (not its address, and the name only resolves inside the tailnet). Keep the tailnet's random name (`tailXXXXXX`, no custom or "fun" name), give the machine a meaningless one (no OS, project or personal name), and request a certificate on this machine only.

**Machine names stay out of this repository** (`<host>`, `<tailnet-ip>`). The deployed host name lives in the private environment repository (18.2). A private CA would publish nothing but needs its root trusted on every device; an own domain with a wildcard certificate is the Phase 19 option.

**Check:** `tailscale status` shows the machine online at `<tailnet-ip>`; MagicDNS on; one certificate domain. MagicDNS only adds a `ts.net` route to systemd-resolved (`tailscale0`: `100.100.100.100`), so CoreDNS still forwards to the router and the nftables table (which drops `100.64.0.0/10`) doesn't break pod DNS. A pod reaches `<tailnet-ip>` (local delivery, not forwarded), which the Ollama connection uses. No ufw rule for `tailscale0` yet: Traefik's traffic is forwarded, not input; 18.2 tests from a tailnet and a LAN device and adds only what that needs.

### 4. cert-manager

The Barman Cloud plugin needs it for its own certificates (the app's certificate comes from Tailscale, TLS mode `secret`). Same chart and settings as the CI rehearsal:

```sh
helm repo add jetstack https://charts.jetstack.io
helm upgrade --install cert-manager jetstack/cert-manager --version v1.21.3 \
  -n cert-manager --create-namespace --set crds.enabled=true --wait
```

**Check:** controller, cainjector and webhook running; a self-signed `Issuer` + `Certificate` in a scratch namespace becomes `Ready` with a `kubernetes.io/tls` secret.

### 5. Argo CD

Chart `argo-cd` 10.11.6 (Argo CD v3.5.6) with [`homelab/argocd-values.yaml`](../deploy/environments/homelab/argocd-values.yaml): the Application health check from [Bootstrap](#bootstrap-first-install) step 1 is part of the values (no hand edit of `argocd-cm`), no Dex (the built-in admin is the only user; sign-in through Dex and GitHub is planned for Phase 19, once the UI is reachable beyond the port-forward), no notifications (alerts go through Alertmanager), the unused ApplicationSet controller at 0 replicas. The chart's own NetworkPolicies stay on.

```sh
helm repo add argo https://argoproj.github.io/argo-helm
helm upgrade --install argocd argo/argo-cd --version 10.11.6 -n argocd --create-namespace \
  -f deploy/environments/homelab/argocd-values.yaml --wait
```

The UI and API are not exposed; reach them from the machine only:

```sh
kubectl -n argocd port-forward svc/argocd-server 8443:443        # https://localhost:8443, user admin
kubectl -n argocd get secret argocd-initial-admin-secret -o jsonpath='{.data.password}' | base64 -d; echo
```

Change the admin password in the UI (*User Info → Update password*) and then delete `argocd-initial-admin-secret` (Argo CD's advice; it is only read once).

**Check:** application controller, repo server, server and redis running; `/healthz` 200 and `/api/version` v3.5.6 through the port-forward; `argocd-cm` holds `resource.customizations.health.argoproj.io_Application`; 4 NetworkPolicies; ApplicationSet controller at 0 replicas. The health check is exercised in 18.3, when the root app's sync waves wait for the child apps.

### 6. Ollama (model server on the host)

Outside the cluster (ADR 0020/0022), from the distribution's packages.

**Older NVIDIA cards.** CUDA 13 builds no longer support GPUs below compute capability 7.5 (generations before Turing). Such a card is skipped (*skipping CUDA device — compute capability not in compiled architectures* in the log) and the model runs on the CPU. Two ways onto the GPU:
- the **Vulkan** build (Arch: `ollama-vulkan`; the NVIDIA driver ships the Vulkan driver): a distribution package with normal updates; this is what the reference environment runs;
- a **CUDA 12** build (the upstream release's `cuda_v12` engine still includes those generations): on a 4 GB card it read prompts about twice as fast as Vulkan and wrote slightly slower, but it is installed outside the package manager and needs every layer forced onto the GPU (a derived model with `PARAMETER num_gpu 999`). Measured on the reference machine; the choice is made with the 18.5 evals.

```sh
sudo pacman -S --needed ollama-cuda ollama-vulkan
sudo install -D -m 644 deploy/environments/homelab/host/ollama.conf /etc/systemd/system/ollama.service.d/finuance.conf
sudo systemctl daemon-reload && sudo systemctl enable --now ollama
ollama pull qwen3:4b
journalctl -u ollama -b | grep 'inference compute'     # names the GPU and the library (CUDA or Vulkan)
```

[`host/ollama.conf`](../deploy/environments/homelab/host/ollama.conf):
- listens on every address: pods reach it on `<tailnet-ip>:11434` (allowed by the `cni0` input rule), the LAN can't (ufw's default drop; check from a phone on the Wi-Fi without Tailscale), tailnet devices can (Tailscale accepts `tailscale0` before ufw; Ollama has no authentication, THREAT-MODEL §5);
- flash attention + `q8_0` KV cache: with the default fp16 cache, a ~4B model at the app's 8,192-token context doesn't fit in 4 GB and spills layers to the CPU, which roughly halves the output speed;
- the context for OpenAI-style requests (`OLLAMA_CONTEXT_LENGTH=8192`): the app can't set it per request, and Ollama's default (4,096) silently cuts longer prompts;
- keep-alive 30 min (a reload takes seconds).

**Measure** on the machine (the reference machine's results are in the private environment repository): `ollama ps` must show `100% GPU` after a request with `num_ctx` 8192, and the generate API reports the speeds:

```sh
curl -s localhost:11434/api/generate -d '{"model":"qwen3:4b","prompt":"/no_think List three budget categories.","stream":false,"options":{"num_ctx":8192,"num_predict":128}}' \
  | jq '{prompt_tok_s: (.prompt_eval_count/(.prompt_eval_duration/1e9)), output_tok_s: (.eval_count/(.eval_duration/1e9))}'
ollama ps
```

Use a prompt of a few thousand tokens too: on small GPUs prompt reading, not writing, decides how long an assistant turn with tool results takes. Ollama reuses an already-read prefix (system prompt, earlier turns), so follow-up rounds only read what is new.

### 7. The per-machine overlay (private environment repository)

Machine-specific values stay out of this repository (ADR 0023). The `finuance` Argo CD application reads the chart and [`homelab/values.yaml`](../deploy/environments/homelab/values.yaml) from here, then `homelab/host-values.yaml` from the private environment repository (`finuance-env`, second source `ref: env`); the later file wins. The overlay sets `publicOrigin` and `ingress.host` (`https://<host>`, the machine's own Tailscale name: Tailscale certifies nothing else) and the assistant's connections file and egress rule (`http://<tailnet-ip>:11434/v1`, `<tailnet-ip>/32` port 11434); [`host-values.example.yaml`](../deploy/environments/homelab/host-values.example.yaml) shows its shape.

Clone the environment repository next to this one (`../finuance-env`). `make helm-lint` then renders the chart with the real overlay too (in memory, not kept), and `make check-env-details` reads its `private-patterns.txt`. Argo CD reads both repositories with read-only deploy keys (18.3).

### 8. The backup bucket in the cluster (RustFS)

Without a bucket outside the machine, backups go to RustFS inside the cluster ([`deploy/k8s/rustfs`](../deploy/k8s/rustfs/rustfs.yaml), Argo CD application `rustfs`, namespace `s3`, wave −3 before the secrets and the database): RustFS 1.0.1 on a 20 Gi `local-path` volume (never pruned or deleted by Argo CD), a PostSync job that creates `finuance-backups` when it is missing, and default-deny NetworkPolicies (only PostgreSQL instance pods in `finuance` and `finuance-restore`, which run the Barman Cloud plugin's sidecar, and the bucket job reach port 9000; RustFS itself connects nowhere). **It shares the node's disk with the database**: it protects against mistakes and pod or volume loss, not against losing the disk; an off-machine bucket is the first change for an always-on host (`objectStore` in `homelab/db-values.yaml`).

Its keys and the database's backup credentials are one random pair, sealed twice (18.3; values never shown or saved):

```sh
tools/seal-secrets.sh homelab backup-store   # finuance-db-backup (finuance) + rustfs (s3, deploy/environments/homelab/rustfs/)
```

**Check** (2026-10-30, applied by hand into a scratch `s3` namespace with throwaway keys, then deleted): accepted under the `restricted` Pod Security Standard; volume bound; the job created the bucket, and after a pod restart a second run found it (the data is on the volume); a pod in `finuance` with `cnpg.io/cluster` reaches port 9000, the same pod without the label and a pod in `default` don't.

### 9. TLS: the host's Tailscale certificate

The ingress runs in TLS mode `secret`: [`tools/tailscale-cert.sh`](../tools/tailscale-cert.sh) fetches the certificate for `<host>` with `tailscale cert` (as the operator user, no root) and applies it as `finuance/finuance-tls` (the key only passes through a private temporary folder in memory; never on disk or in git). It creates the `finuance` namespace with the `restricted` Pod Security Standard when Argo CD hasn't yet. `tailscale cert` returns its cached certificate while it is fresh and renews it when about a third of its 90 days is left, so the script is safe to repeat; a weekly timer runs it ([`host/finuance-tls@.service`](../deploy/environments/homelab/host/finuance-tls@.service) and `.timer`, 10 minutes after boot and weekly). Traefik picks up a changed secret by itself.

**The first run publishes `<host>` in the Certificate Transparency logs** (step 3).

```sh
tools/tailscale-cert.sh                                   # first certificate (asks for nothing)
sudo install -m 755 tools/tailscale-cert.sh /usr/local/bin/finuance-tailscale-cert
sudo install -m 644 'deploy/environments/homelab/host/finuance-tls@.service' 'deploy/environments/homelab/host/finuance-tls@.timer' /etc/systemd/system/
sudo systemctl daemon-reload && sudo systemctl enable --now "finuance-tls@$USER.timer"
sudo systemctl start "finuance-tls@$USER.service"         # one run through systemd
```

**Check** (2026-10-30): the secret is `kubernetes.io/tls` with a Let's Encrypt certificate for `<host>` (90 days) whose key matches; a second run kept the same certificate and left the secret untouched; the run through systemd succeeded and the timer is scheduled. `systemctl list-timers 'finuance-tls@*'` shows the next run, `journalctl -u "finuance-tls@$USER.service"` the last one.

### 10. Only the tailnet reaches the ingress, with the client's real address

k3s publishes Traefik through its service load balancer (klipper-lb), which forwards every connection from its own pod and replaces the client's address with that pod's: the activity log and the per-address rate limits saw one address for everyone (found in 18.3), and `externalTrafficPolicy: Local` doesn't change that. [`host/traefik-config.yaml`](../deploy/environments/homelab/host/traefik-config.yaml) (a `HelmChartConfig` for the bundled Traefik; template, filled-in copy with the environment's private files) therefore:

- runs Traefik on **host ports 80/443** with a `ClusterIP` service, so klipper-lb isn't used and the client's address reaches Traefik, which passes it in `X-Forwarded-For` (it ignores the header from clients; the app trusts it from the pod network only). In this chart version (40.x) the service type is `service.spec.type`; `service.type` is silently ignored. One node can't run two pods holding the same host ports, so the update strategy replaces the pod instead of surging (a few seconds without ingress on each Traefik change);
- redirects plain HTTP to HTTPS permanently (`ports.web.http.redirections`; HSTS then keeps browsers on HTTPS);
- writes the host's tailnet address into the Ingress status (`providers.kubernetesIngress.ingressEndpoint.ip`, `publishedService` off): without a load balancer nothing else does, and Argo CD reports an Ingress without an address as `Progressing`.

Only the tailnet may connect: the nftables table from step 2 drops connections forwarded into the pod network for ports 80/443 that don't arrive on `tailscale0`. Connections the host makes to itself aren't forwarded, so test from another device.

```sh
sudo nft -c -f k3s-lan.nft && sudo install -m 644 k3s-lan.nft /etc/k3s-lan.nft && sudo systemctl restart k3s-lan
kubectl apply -f traefik-config.yaml                     # the filled-in copy
kubectl -n kube-system get svc traefik -o jsonpath='{.spec.type}'                       # ClusterIP
kubectl -n finuance get ingress finuance -o jsonpath='{.status.loadBalancer.ingress}'   # [{"ip":"<tailnet-ip>"}]
```

**Check** (2026-10-31): no `svclb-traefik` pod, Traefik holds 80→8000 and 443→8443; from a phone on the home Wi-Fi without Tailscale `<lan-ip>` doesn't connect, with Tailscale the app works; a new sign-in is recorded in Settings → Activity with the phone's tailnet address (before: the load balancer pod's address); the Ingress status holds `<tailnet-ip>` and Argo CD reports the app `Healthy`; `http://<host>/…` answers 301 to the same `https://` address.

### 11. Argo CD reads the private repositories

Both repositories are private, so Argo CD reads them over SSH with one **read-only deploy key** each. The key pairs are generated in a private temporary folder; the private halves go straight into Argo CD as repository secrets (`kubectl`, not git: the Sealed Secrets controller only arrives through Argo CD) and the files are deleted; the public halves are added to each repository as read-only deploy keys (*Settings → Deploy keys*, or `gh api`). A lost key is simply replaced the same way.

```sh
d=$(mktemp -d "$XDG_RUNTIME_DIR/argocd-keys.XXXXXX")
for r in Finuance finuance-env; do
  n=$(echo "$r" | tr 'A-Z' 'a-z')
  ssh-keygen -q -t ed25519 -N '' -C "argocd-homelab-read-only@$n" -f "$d/$n"
  kubectl -n argocd create secret generic "repo-$n" --from-literal=type=git \
    --from-literal=url="git@github.com:x2db736ac85h/$r.git" --from-file=sshPrivateKey="$d/$n" --dry-run=client -o yaml \
    | kubectl label --local -f - argocd.argoproj.io/secret-type=repository -o yaml | kubectl apply -f -
  gh api -X POST "repos/x2db736ac85h/$r/keys" -f title="argocd-homelab (read-only)" -f key="$(cat "$d/$n.pub")" -F read_only=true
done
rm -rf "$d"
```

The Argo CD applications use the SSH URLs. **While a phase is tried on the cluster, they follow its branch** (`targetRevision: phase/NN-…` for this repository; the private overlay always follows `main`); `make helm-lint` fails on `main` and on pull requests into `main` unless every application follows `main` again.

**Check** (2026-10-30): Argo CD's API reports both repositories `Successful` (`GET /api/v1/repositories?forceRefresh=true`).

### Note: NetworkPolicies on k3s and a pod's first connection

k3s enforces NetworkPolicies with kube-router, which adds a new pod's address to a policy's allow lists about a second after the pod starts. Until then that pod's connections to a protected port (e.g. PostgreSQL's 5432, allowed only for the app's pods) are refused. The migrations Job connected within milliseconds and failed every attempt, because each retry was a new pod; it now uses `restartPolicy: OnFailure`, so a retry runs in the same pod (checked 2026-10-30: a probe pod's first connection refused, every one from 2 s later open). Long-running pods (api, worker) just retry their connection.

### 12. GitOps bootstrap (Phase 18.3)

The order that worked, with what to look at between the steps:

1. **Root app:** `kubectl apply -f deploy/k8s/argocd/root.yaml`. The CNPG operator, the Barman Cloud plugin and the Sealed Secrets controller come up (wave −5/−4, about a minute); `rustfs` waits for its secret.
2. **Back up the controller key** at once (see [Secrets](#back-up-the-controller-key)): into a file readable only by you (e.g. `~/.local/share/finuance/`, mode 600), outside both repositories, and the public certificate into `deploy/environments/homelab/sealed-secrets.pem`. The controller adds a key every 30 days: back up again from time to time.
3. **Seal and commit:** `tools/seal-secrets.sh homelab db`, `… backup-store`, and `… pull` (a classic GitHub token with only `read:packages`, typed on stdin; note its expiry). Push.
4. **RustFS and the secrets sync by themselves** (refresh in Argo CD). If `rustfs` started before its secret was in git, its sync retries the old revision until it gives up; it then syncs the new one.
5. **Sync `finuance-db` by hand** after reading the diff (UI *Sync*, or `POST /api/v1/applications/finuance-db/sync`): 2 instances healthy in about 70 s, WAL archiving on, the immediate first backup completed. Check the bootstrap SQL: `citext`, `pg_trgm`, `textlike` leakproof, PUBLIC can't connect.
6. **The app syncs by itself:** the migrations Job (may restart once, see the note above), then api, worker and web.
7. **First account** (sign-up is off), the password typed without echo and passed on stdin:

   ```sh
   read -rsp 'Password: ' PW; echo
   printf '%s' "$PW" | kubectl -n finuance exec -i deploy/finuance-api -- /finuance admin create-user --email you@example.com --password-stdin; unset PW
   ```

8. **Smoke tests through the ingress:** `BASE_URL=https://<host> scripts/smoke-k8s.sh` makes a throwaway user per script with `admin create-user`, runs the auth, import and query smoke scripts as that user over HTTPS and deletes it.

**Check** (2026-10-31, `v0.2.0-rc.1`): every Argo CD application `Synced`/`Healthy`; schema version 11; `https://<host>` with the Let's Encrypt certificate (`curl` verifies it), CSP, HSTS, `nosniff`; smoke scripts 15 + 18 + 17 steps, no smoke user left; a pod with the api's NetworkPolicy reaches Ollama on `<tailnet-ip>:11434` and lists the model; api and worker restarted once at their first start (the kube-router race; Backlog).

### 13. Monitoring (Phase 18.6)

Three Argo CD apps in the namespace `monitoring`, none of them exposed (port-forward only, like Argo CD):

| App | Wave | What |
|---|---|---|
| `tempo` | −6 | Tempo 2.9.0 from `deploy/k8s/tempo` (the Helm chart is deprecated): OTLP/HTTP on 4318 only, traces kept 72 h on a 5 Gi volume, read-only as uid 10001; its NetworkPolicy admits 4318 from Finuance's api and worker and 3200 from Grafana |
| `kube-prometheus-stack` | −4 | chart 92.16.1 with `homelab/monitoring-values.yaml`: Prometheus (7 days or 8 GB on a 10 Gi volume), Alertmanager, Grafana (dashboard sidecar, Tempo data source), kube-state-metrics, node-exporter; k3s's built-in etcd/scheduler/controller-manager/proxy aren't scraped (nothing separate to scrape). After Sealed Secrets, because it also syncs Grafana's sealed admin login |
| `finuance-observability` | 1 | `deploy/k8s/observability` (the six dashboards, SLO and alert rules, the e-mail receiver) with `homelab/observability-values.yaml` and the overlay's addresses (`$env/homelab/observability-values.yaml`, template `observability-values.example.yaml`), plus the sealed SMTP password |

Before pushing the apps:

1. **A sending account for the alerts:** a separate mailbox only for sending (e.g. a new Gmail account with 2-step verification and an *app password*), so a leaked password can only send mail, and can be revoked without touching a personal account. Seal its app password: `tools/seal-secrets.sh homelab smtp` (asked without echo; spaces removed).
2. **Grafana's admin login:** `tools/seal-secrets.sh homelab grafana` (user `admin`, 32 random characters). The chart's default password is public.
3. **Addresses** (sender, recipient, `smtp.gmail.com:587`) in the overlay, committed to the private repository first: Argo CD can't render `finuance-observability` without it.
4. The app's values point traces at Tempo with the **full URL** `http://tempo.monitoring.svc:4318/v1/traces` (the app sends to exactly that URL; the chart refuses one without a path) and turn the PodMonitors on; `db-values.yaml` turns on the database's PodMonitor and admits Prometheus in its NetworkPolicy.

**Database metrics on a cluster created before Phase 18.6:** the bootstrap SQL revokes `CONNECT` from `PUBLIC`, and CloudNativePG's metrics role (`cnpg_metrics_exporter`, a `pg_monitor` member) runs its queries inside the `finuance` database, so connections, transactions, cache hit ratio, database size, replication lag and archiver panels stay empty (the instance logs `permission denied for database "finuance"`). New clusters get `GRANT CONNECT ON DATABASE finuance TO pg_monitor` from the bootstrap SQL; an existing one needs it once, on the primary:

```sh
P=$(kubectl -n finuance get cluster finuance-db -o jsonpath='{.status.currentPrimary}')
kubectl -n finuance exec "$P" -c postgres -- psql -X -d finuance -c 'GRANT CONNECT ON DATABASE finuance TO pg_monitor;'
```

It replicates to the standby, and restores carry it. `pg_monitor` members read statistics only (no table grants; RLS).

After the push the three apps sync by themselves (about 8 minutes on the reference machine). **`finuance-db` then shows `OutOfSync`** for its PodMonitor and NetworkPolicy only: sync those two (UI *Sync* with just them ticked); the PostgreSQL pods don't restart.

**Access** (then `http://localhost:3000`, user `admin`):

```sh
kubectl -n monitoring get secret grafana-admin -o jsonpath='{.data.admin-password}' | base64 -d; echo
kubectl -n monitoring port-forward svc/kube-prometheus-stack-grafana 3000:80
kubectl -n monitoring port-forward svc/kube-prometheus-stack-prometheus 9090:9090      # targets, rules
kubectl -n monitoring port-forward svc/kube-prometheus-stack-alertmanager 9093:9093    # alerts, silences
```

**Test alert** (an e-mail now and a *resolved* one after about 5 minutes; Alertmanager's `alertmanager_notifications_total{integration="email"}` counts the sent ones, `…_failed_total` the failures by reason):

```sh
kubectl -n monitoring port-forward svc/kube-prometheus-stack-alertmanager 9093:9093 &
curl -XPOST localhost:9093/api/v2/alerts -H 'Content-Type: application/json' -d "[{
  \"labels\": {\"alertname\": \"FinuanceTestAlert\", \"severity\": \"ticket\", \"namespace\": \"finuance\"},
  \"annotations\": {\"summary\": \"Test alert\", \"description\": \"Sent by hand to check e-mail delivery.\"},
  \"endsAt\": \"$(date -u -d '+5 min' +%Y-%m-%dT%H:%M:%SZ)\"}]"
```

The first mail from a new sending account may land in spam: mark it *Not spam* once.

**Check** (2026-10-31): every app `Synced`/`Healthy`; all Prometheus targets up (api ×2, worker, database ×2, kubelet, API server, CoreDNS, node-exporter, kube-state-metrics, the stack's own); the 27 Finuance rules (6 groups) healthy, only `Watchdog` firing (it always fires, to show the pipeline works); Grafana with the six Finuance dashboards in the *Finuance* folder, Prometheus and Tempo data sources healthy, the default password refused; one trace in Tempo from `POST /api/v1/imports/{id}/commit` (api) through `job commit_import` and `job reclassify` (worker) with their SQL; the test alert sent by Alertmanager (1 e-mail, 0 failed).

### After a reboot

On a single node a reboot is the node-failure drill (also after a system update that rebuilds the GPU driver):

```sh
systemctl show -p ActiveEnterTimestampMonotonic k3s-lan k3s   # k3s-lan must be the earlier one
kubectl get nodes; kubectl get pods -A                         # Ready; Running/Completed only
nvidia-smi; ollama ps                                          # GPU visible; model on the GPU after a request
```

Then repeat the pod test from step 2 (router blocked, DNS and internet working), check Argo CD's `/healthz` and run `make check`. On the reference machine everything came back by itself in the right order.

## Deploy

1. A `vX.Y.Z` tag runs `release.yml`: images, scans, signatures, the GitHub release, then a pull request **chore(deploy): homelab to vX.Y.Z** that changes `image.tag` in `deploy/environments/homelab/values.yaml`. Release candidates (`-rc.N`) get no pull request.
   Tag a commit whose message has no `[skip ci]`: GitHub skips every push-triggered workflow for such a commit, the tag push included, and the release never starts (it happened with the first `v0.2.0-rc.1` tag).
2. The pull request is opened with the workflow token, so CI doesn't start on its own. Close and reopen it to run the checks.
3. Merge it. Argo CD syncs `main` (within its polling interval, or press *Refresh*):
   - the migrations Job `finuance-migrate` runs first (Helm `pre-upgrade` hook = Argo CD `PreSync`; the previous Job is deleted before the new one starts);
   - then the Deployments roll one pod at a time (`maxUnavailable: 0`). A new api pod gets traffic only once `/readyz` passes, which needs the database reachable and the schema at least at the version the binary expects.
4. Watch it: `kubectl -n finuance get pods -w`, Argo CD's app view, `kubectl -n finuance logs job/finuance-migrate`.

A change to the chart or to the values deploys the same way: any merged change under `deploy/helm/finuance` or `deploy/environments/homelab/values.yaml`.

## Rollback

**Revert the pull request** that deployed the version and merge the revert. Argo CD rolls the Deployments back to the previous image.

Migrations are **not** rolled back. That is safe because:

- the readiness check only requires the schema to be *at least* the binary's version, so an older binary runs against a newer schema;
- migrations are written expand-then-contract: a release adds columns and tables, and a column is dropped only in a later release, once no deployed version reads it.

Undo a migration only when it is itself the problem, and only by hand with a backup taken first ([On-demand backup](#on-demand-backup)). Run it **before** reverting, while the values still name the release that has the migration. The chart's own Job is rendered with `migrate down` instead of `migrate up`, so it keeps the security context, the network policy labels and the owner's password:

```sh
helm template finuance deploy/helm/finuance -n finuance -f deploy/environments/homelab/values.yaml -s templates/migrate-job.yaml \
  | yq '.metadata.name = "finuance-migrate-down" | del(.metadata.annotations) | .spec.template.spec.containers[0].args = ["migrate", "down"]' \
  | kubectl -n finuance apply -f -
kubectl -n finuance logs -f job/finuance-migrate-down
```

`migrate down` undoes one migration; `migrate status` lists them. Pause Argo CD's auto-sync for `finuance` while you do this, or self-heal starts the newer release again.

## Failed migration

A failed `finuance-migrate` Job fails the sync. **The old pods keep serving**, because the hook runs before anything else changes.

1. Read why: `kubectl -n finuance logs job/finuance-migrate`. Each migration runs in one transaction (goose), so a failed migration leaves nothing half-done; earlier migrations of the same run stay applied.
2. A **database problem** (unreachable, disk full, lock timeout): fix it, then *Sync* again. The hook Job is recreated.
3. A **bug in the migration**: revert the release's pull request (the database is still at the previous release's schema, plus any migrations before the failing one) and ship a fixed release.
4. The Job gives up after `migrations.backoffLimit` (2) retries or `activeDeadlineSeconds` (600 s). It is kept for a day (`ttlSecondsAfterFinished`) so its logs can still be read.

## Secrets

All secrets are SealedSecrets in `deploy/environments/homelab/secrets/`. They are encrypted with the controller's public key and bound to their name and namespace (strict scope). Only the controller in the cluster can decrypt them. [`tools/seal-secrets.sh`](../tools/seal-secrets.sh) writes each Secret into `kubeseal` through a pipe, so plain values never touch the disk or a command line.

| Secret | Made with | Rotation |
|---|---|---|
| `finuance-db-owner`, `finuance-db-app` | `tools/seal-secrets.sh homelab db` (32 random characters each) | below |
| `finuance-db-backup` | `ACCESS_KEY_ID=… ACCESS_SECRET_KEY=… tools/seal-secrets.sh homelab backup` | new key at the store, re-seal, merge; the plugin reloads it (`cnpg.io/reload`) |
| `ghcr-pull` | `tools/seal-secrets.sh homelab pull` (a `read:packages` token on stdin) | new token before it expires, re-seal, merge |
| `monitoring/finuance-alert-smtp` | `tools/seal-secrets.sh homelab smtp` (the sending account's app password on stdin), in `homelab/monitoring/` | new app password, revoke the old one, re-seal, merge |
| `monitoring/grafana-admin` | `tools/seal-secrets.sh homelab grafana` (random), in `homelab/monitoring/` | re-seal, merge, then `kubectl -n monitoring rollout restart deploy/kube-prometheus-stack-grafana` |
| `finuance-assistant` (optional) | `tools/seal-secrets.sh homelab assistant keys.env` (`KEY=value` lines, the connections file's `apiKeyEnv` names) | re-seal, merge, then `kubectl -n finuance rollout restart deploy/finuance-api` |

### Back up the controller key

Without it, nobody can decrypt the committed secrets after a cluster loss. Right after the controller starts, and after each key renewal (every 30 days by default):

```sh
kubectl -n kube-system get secret -l sealedsecrets.bitnami.com/sealed-secrets-key -o yaml > sealed-secrets-keys.yaml
```

Keep the file **outside git and outside the cluster**, in a password manager or on an encrypted drive. Save the public certificate too, so you can seal offline: `kubeseal --fetch-cert > deploy/environments/homelab/sealed-secrets.pem` (public, may be committed); then `SEAL_CERT=deploy/environments/homelab/sealed-secrets.pem tools/seal-secrets.sh …`.

### Rotate the database passwords

1. `tools/seal-secrets.sh homelab db`, commit, merge. Argo CD updates the two Secrets.
2. CloudNativePG notices the change (`cnpg.io/reload`) and sets the new passwords on the roles within seconds. Connections that are already open stay up; new connections need the new password.
3. Restart the processes so they read the new `PGPASSWORD`: `kubectl -n finuance rollout restart deploy/finuance-api deploy/finuance-worker`. The rollout replaces one pod at a time, so the app stays up.

Rotate one role at a time if you want no window at all. Seal just that role by editing the script's `db` case, or run the steps above twice.

### Restore after losing the cluster or the key

- **Cluster lost, key backed up:** before Argo CD syncs the secrets, `kubectl apply -f sealed-secrets-keys.yaml` and restart the controller (`kubectl -n kube-system delete pod -l app.kubernetes.io/name=sealed-secrets`). The committed secrets decrypt as before.
- **Key lost:** the committed secrets can't be decrypted. Make new ones: run `tools/seal-secrets.sh` again against the new controller (backup keys from the store; new database passwords) and commit them. A database restored from backup brings the old passwords, and the managed roles then set the new ones from the new secrets.
- After a key renewal, old secrets keep working (old keys are kept). To re-encrypt them with the newest key: `kubeseal --re-encrypt < f.sealed.yaml > f.tmp && mv f.tmp f.sealed.yaml`.

## Scaling

- **api**: the HPA keeps 2–4 pods at 70 % CPU (`api.autoscaling`). The PodDisruptionBudget keeps one pod during node drains. Each pod opens at most `database.maxConns` (10) connections, so 4 api pods + the worker stay well under `max_connections` (100).
- **worker**: `worker.replicas` (1 by default). River spreads jobs over any number of workers; imports for one account are serialised by a row lock (ADR 0013).
- **web**: `web.replicas` (2); static files only.
- **PostgreSQL instances**: `cluster.instances` in `db-values.yaml`, then a manual sync. Two instances = a primary plus a streaming replica that takes over automatically.
- **Database volume**: raise `cluster.storage.size`. CNPG expands the volumes when the storage class allows expansion. **`local-path` does not**: there, restore into a new cluster with a larger size ([Restore](#restore)).

## Backups

- **Continuous WAL archiving** plus a **daily base backup** at 02:15 (`backup.schedule`, cron with seconds), both through the Barman Cloud plugin to `objectStore.destinationPath/<serverName>/`.
- **Retention**: a 14-day recovery window (`objectStore.retention`). Older backups are deleted after the next one completes.
- Check: `kubectl -n finuance get backups`, `kubectl -n finuance get scheduledbackup`, and `kubectl cnpg status finuance-db -n finuance` (shows WAL archiving). The `finuance-db-store` ObjectStore status shows the first and last recoverability points.
- **If no bucket is available**, CloudNativePG can take Kubernetes volume snapshots instead (`method: volumeSnapshot`, needs a CSI driver with snapshots; not `local-path`). This is the fallback decided in Phase 18 if there is no S3 store.

### On-demand backup

Before a risky change (a migration down, a major upgrade):

```sh
kubectl -n finuance apply -f - <<'EOF'
apiVersion: postgresql.cnpg.io/v1
kind: Backup
metadata: { name: finuance-db-manual-1 }
spec:
  cluster: { name: finuance-db }
  method: plugin
  pluginConfiguration: { name: barman-cloud.cloudnative-pg.io }
EOF
kubectl -n finuance get backup finuance-db-manual-1 -w   # until phase: completed
```

## Restore

A restore creates a **new** cluster from the bucket (CNPG never restores in place). The new cluster gets the database, its roles, the extensions and the `textlike` LEAKPROOF flag from the backup. It must archive to **another folder** than its origin (`backup.serverName`); the chart refuses to render otherwise.

### Restore drill (scratch namespace)

**On a cluster with sign-up off, run the scripted drill:** `tools/homelab-drill.sh restore` (marker users made with the admin CLI, an on-demand backup between them, the restore with the bucket's keys and the registry login copied inside the cluster and fresh role passwords sealed for the scratch namespace, a temporary app reached by port-forward, both markers, LEAKPROOF, marker B's sign-in, `scripts/smoke-k8s.sh` against it; then the markers and the namespace are deleted, `KEEP_RESTORE=1` keeps the namespace). The manual steps below are what it does.

```sh
kubectl create namespace finuance-restore
kubectl label namespace finuance-restore pod-security.kubernetes.io/enforce=restricted
# the secrets: seal copies for the scratch namespace (strict scope binds them to it)
NAMESPACE=finuance-restore SECRETS_DIR=/tmp/restore-secrets tools/seal-secrets.sh homelab db
ACCESS_KEY_ID=… ACCESS_SECRET_KEY=… NAMESPACE=finuance-restore SECRETS_DIR=/tmp/restore-secrets tools/seal-secrets.sh homelab backup
kubectl apply -n finuance-restore -f /tmp/restore-secrets/
# the cluster, from the homelab's backups, archiving into a separate folder
helm install finuance-db deploy/k8s/db -n finuance-restore -f deploy/environments/homelab/db-values.yaml \
  --set recovery.enabled=true --set recovery.serverName=finuance-db \
  --set backup.serverName=finuance-db-drill-$(date +%Y%m%d) --set cluster.instances=1
kubectl -n finuance-restore get cluster finuance-db -w     # until "Cluster in healthy state"
```

Then point a temporary app at it and run the smoke scripts:

```sh
helm install finuance deploy/helm/finuance -n finuance-restore -f deploy/environments/homelab/values.yaml \
  --set publicOrigin=http://localhost:8000 --set ingress.enabled=false --set allowSignup=true \
  --set networkPolicy.enabled=false --set api.autoscaling.enabled=false --set api.replicas=1 --set web.replicas=1
kubectl -n finuance-restore port-forward svc/finuance-api 8080:8080 &
kubectl -n finuance-restore port-forward deploy/finuance-worker 9091:9090 &
scripts/auth-smoke.sh && scripts/import-smoke.sh && scripts/query-smoke.sh
```

Sign in with a real account, too. The data is the backup's: check that the last import before the backup is there. Then clean up: `kubectl delete namespace finuance-restore` (and the drill's folder in the bucket).

Record the times in the table below: **RTO** is from `helm install finuance-db` to the smoke scripts passing; **RPO** is the time between the last write before the backup and the newest data that came back (with WAL archiving, minutes at most).

| Drill | Where | Data | RTO | RPO | Notes |
|---|---|---|---|---|---|
| 2026-10-25 | CI rehearsal (kind, 3 nodes, RustFS bucket) | smoke-test data | 75 s (restored cluster ready after 56 s) | 51 s (a write until its WAL was archived; at most `archive_timeout`, 5 min) | on-demand base backup 159 s; both markers back (before the base backup, and only in the WAL after it); `textlike` still LEAKPROOF; the restored roles took the scratch namespace's new passwords |
| 2026-10-31 | reference machine (single-node k3s, RustFS in the cluster on the same disk; `tools/homelab-drill.sh restore`) | production database with one real account, two marker users | **71 s** (restored cluster ready after 55 s) | **96 s** (a write until its WAL was archived; at most `archive_timeout`, 5 min) | on-demand base backup 8 s; both markers back (before the base backup, and only in the WAL after it); `textlike` still LEAKPROOF; marker B signed in on the restored database; smoke scripts 15 + 18 + 17 steps on the temporary app; markers and namespace deleted afterwards |

### Pod kills under load

`tools/homelab-drill.sh podkill` runs k6 (`grafana/k6:1.8.2`, the rehearsal's `podkill.js` with a drill user made by the admin CLI and the certificate checked) through the ingress for 90 s while it deletes an api pod, rolls the api, deletes a web pod and rolls web and the worker. No request may fail (rollouts are `maxUnavailable: 0`, pods leave the endpoints before they stop, the api drains in-flight requests).

| Drill | Where | Result |
|---|---|---|
| 2026-10-25 | CI rehearsal (kind, 3 nodes) | 19,241 requests, 0 failed, p95 66 ms |
| 2026-10-31 | reference machine (single-node k3s, through Traefik on host ports, TLS verified) | **23,177 requests, 0 failed, p95 29 ms**, max 560 ms; the api's HPA scaled to 4 during the run |

### Point-in-time recovery

Add `--set recovery.targetTime=2026-10-24T10:00:00Z` (RFC 3339; must be after the first base backup in the retention window). Use it when data was deleted by mistake: restore to just before, then copy what is needed (or switch over).

### Switch the app to a restored cluster

After a real loss: restore as above, but into `finuance` with the same cluster name `finuance-db` (delete the broken Cluster first; its volumes go with it) and a new `backup.serverName` (e.g. `finuance-db-r1`). Put `recovery.*` and the new `backup.serverName` into `db-values.yaml`, so the manual sync keeps them. The app's `database.host` (`finuance-db-rw`) stays the same. Take a new on-demand backup once it is up.

### Logical restore into another PostgreSQL

A `pg_dump` / `pg_restore` (or CNPG's `initdb.import`) into a cluster that was not made by this chart loses one setting that lives in the catalog, not in the dump. Run as a superuser in the `finuance` database:

```sql
ALTER FUNCTION pg_catalog.textlike(text, text) LEAKPROOF;
```

Without it, search still works but cannot use the trigram index under row-level security (docs/perf.md, Phase 7). The chart's bootstrap SQL already does this for new clusters.

## Playbooks

### PostgreSQL down

Symptoms: api pods not ready (`/readyz` 503), the app shows errors, `finuance-db-rw` has no endpoints.

1. `kubectl cnpg status finuance-db -n finuance` (or `kubectl -n finuance get cluster finuance-db -o yaml`): which instance is primary, and is a failover running?
2. With two instances, CNPG promotes the replica on its own (usually under a minute). The api reconnects by itself; no restart needed.
3. Both instances down: `kubectl -n finuance get pods -l cnpg.io/cluster=finuance-db`, `kubectl -n finuance describe pod …` (scheduling, volume, image pull), `kubectl -n finuance logs <pod> -c postgres`.
4. A volume that is lost or broken beyond repair: [restore](#switch-the-app-to-a-restored-cluster).

### Disk full

- **Database volume**: PostgreSQL stops accepting writes. The most common cause is **WAL piling up because archiving fails** (bucket unreachable, wrong keys). Check `kubectl cnpg status` (WAL archiving / last failed WAL) and the logs of the instance pod's plugin sidecar (`kubectl -n finuance logs <instance> --all-containers`). Fix the store or the keys; archiving catches up and PostgreSQL removes the archived WAL. If the volume can't grow (`local-path`), restore into a larger cluster.
- **Node disk** (images, logs, local-path volumes): `kubectl describe node` (DiskPressure); prune unused images on the node (`crictl rmi --prune` on k3s).
- **Bucket full**: lower `objectStore.retention` only after checking that the remaining window is enough; better, add space.

### api not ready / crash-looping

- `kubectl -n finuance describe pod -l app.kubernetes.io/component=api` and `kubectl -n finuance logs deploy/finuance-api`. Configuration problems are listed all at once at start-up (for example `FINUANCE_PUBLIC_ORIGIN`).
- `/readyz` 503 with "migrations": the migrations Job didn't run or failed ([Failed migration](#failed-migration)).
- Password errors after a rotation: restart the pods ([Rotate the database passwords](#rotate-the-database-passwords)).

### Imports stuck in "committing"

The worker runs commits: `kubectl -n finuance logs deploy/finuance-worker`, and the `finuance_jobs_total` / `finuance_imports_total` metrics on its port 9090. A failed job is retried, then the import is marked failed and can be retried from the UI (*Try again*).

## PostgreSQL major upgrade

CloudNativePG (≥ 1.26) upgrades in place when `cluster.imageName` moves to a newer major (for example `…/postgresql:19.x-standard-trixie`). It runs `pg_upgrade` offline: **the database is down for the duration** (minutes at this size).

1. Read the CNPG release notes for the target major. Check the extensions (`pg_trgm`, `citext`) and the app's tests against it: bump `postgres:` in `deploy/compose/compose.yaml` and run `make test-integration` and the e2e suite.
2. Take an [on-demand backup](#on-demand-backup) and note the time.
3. Change `cluster.imageName` and `backup.serverName` (a new major starts a new backup chain), merge, sync `finuance-db` by hand.
4. Watch `kubectl cnpg status`; the api is unready during the upgrade and comes back by itself.
5. Check `SELECT proleakproof FROM pg_proc WHERE proname = 'textlike'` is still `t`, run the smoke scripts, take a new backup.

If the in-place upgrade fails, CNPG keeps the old data directory and the cluster can be rolled back to the old image. Otherwise, restore the pre-upgrade backup into a new cluster with the old image.

## Upgrading the operators

The CloudNativePG, Barman Cloud plugin and Sealed Secrets charts are pinned in `deploy/k8s/argocd/apps/`. Renovate proposes bumps. Read the release notes, and for CloudNativePG check that the plugin version supports the operator version. Merging a bump upgrades them through Argo CD. The database pods restart with the new instance manager, one at a time (`primaryUpdateStrategy: unsupervised`), with a switchover for the primary.
