#!/usr/bin/env bash
# The deployment rehearsal (PLAN task 16.6): the homelab's stack on a throwaway kind cluster, from
# the images built from this checkout. Traefik, cert-manager, CloudNativePG with the Barman Cloud
# plugin, Sealed Secrets, an S3 stand-in (RustFS) and the mock model server, then the database and
# app charts as the homelab runs them. It checks:
#   - the smoke scripts through the ingress (HTTPS, a throwaway CA);
#   - the NetworkPolicies (a pod outside the app can't reach the database or the api);
#   - an assistant answer streamed through Traefik without buffering (the mock's slow answer);
#   - no failed request under load while api and web pods are deleted and rolled (k6);
#   - backup → restore into a scratch namespace → a temporary app on it → the smoke scripts,
#     with the restored data checked and the RPO/RTO measured;
#   - the limits at the ingress switched on: the smoke scripts and the stream still pass, and a
#     burst of sign-ins from one address gets 429 from Traefik;
#   - image signatures (Sigstore policy-controller with the homelab's policies): the released
#     images are admitted in a labelled namespace, an image without the release workflow's
#     signature and an image no policy names are refused (needs ghcr.io and Sigstore's services).
# Timings go to $SUMMARY (and the GitHub job summary). Needs docker, kind, kubectl, helm, kubeseal,
# mikefarah's yq ($YQ), curl, openssl and python3.
#
#   tools/rehearsal.sh [all|images|cluster|platform|deploy|smoke|netpol|sse|podkill|restore|limits|signatures|summary|diagnostics|report|down]
set -euo pipefail
cd "$(dirname "$0")/.."

R=deploy/k8s/rehearsal
CLUSTER=${CLUSTER:-finuance-rehearsal}
KIND_NODE_IMAGE=${KIND_NODE_IMAGE:-kindest/node:v1.33.1@sha256:050072256b9a903bd914c0b2866828150cb229cea0efe5892e2b644d5dd3b34f}
WORK=${WORK:-$PWD/.rehearsal}
SUMMARY=${SUMMARY:-$WORK/summary.md}
YQ=${YQ:-yq}
TRAEFIK_CHART=42.1.3
CERT_MANAGER_CHART=v1.21.3
K6_IMAGE=grafana/k6:1.8.2@sha256:00fdcebafbc86cb0137d429529b1caa401a02e09025f02fc74866fa6e2b004d8
AWS_CLI_IMAGE=amazon/aws-cli:2.38.1@sha256:733d5be1015825e4038c6016f3fa9954868ddee1f611b73be795a9cc2e706bfd
BUSYBOX_IMAGE=busybox:1.37.0@sha256:a7dbc319b1438115a207e11930dc4bb3e643dcb3ec0bdd694bb36cd8fad289ec
# an image this repository's release workflow never signed (the signatures step)
PAUSE_IMAGE=registry.k8s.io/pause:3.10.1@sha256:278fb9dbcca9518083ad1e11276933a2e96f23de604a3a08cc3c80002767d24c
API_IMAGE=ghcr.io/x2db736ac85h/finuance-api:rehearsal
WEB_IMAGE=ghcr.io/x2db736ac85h/finuance-web:rehearsal
MOCK_IMAGE=finuance-llmmock:rehearsal
BASE=https://localhost:8443
RESTORE_BASE=https://restore.localhost:8443
export KUBECONFIG=${KUBECONFIG:-$WORK/kubeconfig}
mkdir -p "$WORK"

log() { printf '\n== %s\n' "$*"; }
# timing <label> <seconds>: one row of the summary
timing() { printf '%s\t%s\n' "$1" "$2" >>"$WORK/timings.tsv"; echo "   $1: $2 s"; }
fact() { printf '%s\t%s\n' "$1" "$2" >>"$WORK/facts.tsv"; echo "   $1: $2"; }
# chart version pinned in an Argo CD application (one source of truth for the homelab and here)
argo_version() { "$YQ" '(.spec.source // .spec.sources[0]).targetRevision' "deploy/k8s/argocd/apps/$1.yaml"; }
# wait_for <timeout s> <description> <command…>
wait_for() {
  local limit=$1 what=$2; shift 2
  local start; start=$(date +%s)
  until "$@" >/dev/null 2>&1; do
    if (($(date +%s) - start > limit)); then echo "timed out after ${limit}s: $what" >&2; return 1; fi
    sleep 3
  done
}
psql_in() { # psql_in <namespace> <sql>: on the cluster's primary, as postgres over the pod socket
  local primary
  primary=$(kubectl -n "$1" get cluster finuance-db -o jsonpath='{.status.currentPrimary}')
  kubectl -n "$1" exec "$primary" -c postgres -- psql -U postgres -d finuance -XAtqc "$2"
}
curl_ca() { curl -sS --cacert "$WORK/ca.pem" "$@"; }
# archived_since <epoch>: the primary of finuance has archived a WAL file since then
archived_since() {
  local t
  t=$(psql_in finuance "SELECT coalesce(extract(epoch FROM last_archived_time)::bigint, 0) FROM pg_stat_archiver")
  [ "$t" -ge "$1" ]
}
# backup_done <namespace> [name]: a CNPG backup of the namespace (or the named one) has completed
backup_done() {
  if [ $# -eq 2 ]; then
    [ "$(kubectl -n "$1" get backups.postgresql.cnpg.io "$2" -o jsonpath='{.status.phase}')" = completed ]
  else
    kubectl -n "$1" get backups.postgresql.cnpg.io -o jsonpath='{.items[*].status.phase}' | grep -qw completed
  fi
}

images() {
  log "images from this checkout"
  make images
  docker build -q -f deploy/docker/Dockerfile.backend --target llmmock -t "$MOCK_IMAGE" .
  docker tag finuance-backend:dev "$API_IMAGE"
  docker tag finuance-web:dev "$WEB_IMAGE"
  # the build cache isn't needed again; the kind nodes need the disk
  docker builder prune -af >/dev/null
  df -h / | tail -1
}

cluster() {
  log "kind cluster $CLUSTER"
  local t0; t0=$(date +%s)
  kind create cluster --name "$CLUSTER" --image "$KIND_NODE_IMAGE" --config "$R/kind.yaml" --kubeconfig "$KUBECONFIG" --wait 180s
  kind load docker-image --name "$CLUSTER" "$API_IMAGE" "$WEB_IMAGE" "$MOCK_IMAGE"
  timing "kind cluster with the images loaded" $(($(date +%s) - t0))
}

platform() {
  log "platform: Traefik, cert-manager, CloudNativePG + Barman Cloud plugin, Sealed Secrets, stand-ins"
  local t0; t0=$(date +%s)
  helm repo add traefik https://traefik.github.io/charts --force-update >/dev/null
  helm repo add jetstack https://charts.jetstack.io --force-update >/dev/null
  helm repo add cnpg https://cloudnative-pg.github.io/charts --force-update >/dev/null
  helm repo add sealed-secrets https://bitnami.github.io/sealed-secrets --force-update >/dev/null
  helm repo update >/dev/null
  # every pinned chart version must exist before anything is installed (all problems at once)
  local missing=0 chart
  for chart in "traefik/traefik $TRAEFIK_CHART" "jetstack/cert-manager $CERT_MANAGER_CHART" \
    "cnpg/cloudnative-pg $(argo_version cnpg-operator)" "cnpg/plugin-barman-cloud $(argo_version barman-cloud-plugin)" \
    "sealed-secrets/sealed-secrets $(argo_version sealed-secrets)"; do
    set -- $chart
    if helm show chart "$1" --version "$2" >/dev/null 2>&1; then echo "   chart $1 $2: ok"
    else echo "   chart $1 $2: NOT FOUND (newest: $(helm search repo "$1" -o json | "$YQ" -p json '.[0].version'))"; missing=1; fi
  done
  [ $missing -eq 0 ] || return 1
  helm upgrade --install traefik traefik/traefik --version "$TRAEFIK_CHART" -n kube-system -f "$R/traefik-values.yaml" --wait
  helm upgrade --install cert-manager jetstack/cert-manager --version "$CERT_MANAGER_CHART" -n cert-manager --create-namespace \
    --set crds.enabled=true --wait
  helm upgrade --install cnpg cnpg/cloudnative-pg --version "$(argo_version cnpg-operator)" -n cnpg-system --create-namespace --wait
  helm upgrade --install plugin-barman-cloud cnpg/plugin-barman-cloud --version "$(argo_version barman-cloud-plugin)" -n cnpg-system --wait
  helm upgrade --install sealed-secrets-controller sealed-secrets/sealed-secrets --version "$(argo_version sealed-secrets)" -n kube-system \
    --set fullnameOverride=sealed-secrets-controller --wait
  kubectl apply -f "$R/stand-ins.yaml"
  kubectl -n s3 rollout status deploy/rustfs --timeout=180s
  kubectl -n llm rollout status deploy/llmmock --timeout=180s
  kubectl -n s3 run mkbucket --rm -i --restart=Never --image "$AWS_CLI_IMAGE" \
    --env AWS_ACCESS_KEY_ID=rehearsal --env AWS_SECRET_ACCESS_KEY=rehearsal-secret-key --env AWS_DEFAULT_REGION=us-east-1 \
    -- --endpoint-url http://rustfs.s3.svc.cluster.local:9000 s3 mb s3://finuance-backups
  timing "platform charts and stand-ins" $(($(date +%s) - t0))
}

# namespace <name>: restricted Pod Security, the TLS secret, and the sealed secrets made for it
namespace() {
  local ns=$1
  kubectl create namespace "$ns" --dry-run=client -o yaml | kubectl apply -f -
  kubectl label namespace "$ns" --overwrite pod-security.kubernetes.io/enforce=restricted pod-security.kubernetes.io/enforce-version=latest
  kubectl -n "$ns" create secret tls finuance-tls --cert "$WORK/tls.crt" --key "$WORK/tls.key" --dry-run=client -o yaml | kubectl apply -f -
  local dir="$WORK/secrets-$ns"
  rm -rf "$dir" && mkdir -p "$dir"
  # kubeseal fetches the controller's certificate, as on the homelab
  NAMESPACE=$ns SECRETS_DIR=$dir tools/seal-secrets.sh rehearsal db
  ACCESS_KEY_ID=rehearsal ACCESS_SECRET_KEY=rehearsal-secret-key NAMESPACE=$ns SECRETS_DIR=$dir tools/seal-secrets.sh rehearsal backup
  kubectl apply -n "$ns" -f "$dir"
  for s in finuance-db-owner finuance-db-app finuance-db-backup; do
    wait_for 120 "secret $ns/$s unsealed" kubectl -n "$ns" get secret "$s"
  done
}

tls() {
  [ -f "$WORK/tls.crt" ] && return
  openssl req -x509 -newkey rsa:2048 -nodes -days 2 -subj "/CN=Finuance rehearsal CA" \
    -keyout "$WORK/ca.key" -out "$WORK/ca.pem" -addext basicConstraints=critical,CA:TRUE -addext keyUsage=critical,keyCertSign 2>/dev/null
  openssl req -newkey rsa:2048 -nodes -subj "/CN=localhost" -keyout "$WORK/tls.key" -out "$WORK/tls.csr" 2>/dev/null
  printf 'subjectAltName=DNS:localhost,DNS:restore.localhost\nextendedKeyUsage=serverAuth\n' >"$WORK/tls.ext"
  openssl x509 -req -in "$WORK/tls.csr" -CA "$WORK/ca.pem" -CAkey "$WORK/ca.key" -CAcreateserial -days 2 \
    -extfile "$WORK/tls.ext" -out "$WORK/tls.crt" 2>/dev/null
}

deploy() {
  log "deploy: database and app charts into finuance"
  tls
  namespace finuance
  local t0; t0=$(date +%s)
  helm upgrade --install finuance-db deploy/k8s/db -n finuance -f "$R/db-values.yaml"
  kubectl -n finuance wait cluster/finuance-db --for=condition=Ready --timeout=900s
  timing "database cluster ready (2 instances)" $(($(date +%s) - t0))
  t0=$(date +%s)
  wait_for 600 "the first base backup" backup_done finuance
  timing "first base backup completed" $(($(date +%s) - t0))
  [ "$(psql_in finuance "SELECT proleakproof FROM pg_proc WHERE proname = 'textlike'")" = t ] || { echo "textlike is not LEAKPROOF" >&2; return 1; }
  fact "bootstrap SQL" "textlike LEAKPROOF, $(psql_in finuance "SELECT string_agg(extname, ', ' ORDER BY extname) FROM pg_extension WHERE extname <> 'plpgsql'")"
  t0=$(date +%s)
  helm upgrade --install finuance deploy/helm/finuance -n finuance -f "$R/app-values.yaml" --wait --timeout 10m
  timing "app chart installed (migrations, rollout)" $(($(date +%s) - t0))
  wait_for 120 "https://localhost:8443/healthz through Traefik" curl_ca -f "$BASE/healthz"
  # /healthz is the web pod's; Traefik learns the api's endpoints on its own schedule
  wait_for 120 "the api through Traefik" curl_ca -f -o /dev/null "$BASE/api/v1/meta"
}

# run_smoke <base url> <namespace>
run_smoke() {
  local base=$1 ns=$2 pf
  kubectl -n "$ns" port-forward deploy/finuance-worker 9091:9090 >/dev/null 2>&1 &
  pf=$!
  wait_for 30 "worker metrics port-forward" curl -fsS http://localhost:9091/metrics
  local status=0
  for s in auth-smoke import-smoke query-smoke; do
    CURL_CA_BUNDLE="$WORK/ca.pem" BASE_URL=$base ORIGIN=$base METRICS_URL=http://localhost:9091/metrics "scripts/$s.sh" || { status=1; break; }
  done
  kill "$pf" 2>/dev/null || true
  return $status
}

smoke() {
  log "smoke scripts through the ingress"
  local t0; t0=$(date +%s)
  run_smoke "$BASE" finuance
  timing "smoke scripts (auth, import, query) through Traefik" $(($(date +%s) - t0))
}

netpol() {
  log "NetworkPolicies: pods outside the app, without its labels, and with labels that are not enough"
  local ns
  for ns in netpol-probe monitoring; do kubectl create namespace "$ns" --dry-run=client -o yaml | kubectl apply -f - >/dev/null; done
  # probe <namespace> <name> <labels> <shell command>: the command's exit status in a throwaway
  # pod that passes the restricted Pod Security level
  probe() {
    kubectl -n "$1" run "$2" --rm -i --restart=Never --quiet --image "$BUSYBOX_IMAGE" --labels "$3" --override-type=strategic \
      --overrides "{\"spec\":{\"securityContext\":{\"runAsNonRoot\":true,\"runAsUser\":65534,\"seccompProfile\":{\"type\":\"RuntimeDefault\"}},\"containers\":[{\"name\":\"$2\",\"securityContext\":{\"allowPrivilegeEscalation\":false,\"capabilities\":{\"drop\":[\"ALL\"]}}}]}}" \
      -- sh -c "$4" >/dev/null 2>&1
  }
  # addresses, not names: a pod without a policy of its own can't resolve anything
  local db api s3 as_api='app.kubernetes.io/name=finuance,app.kubernetes.io/instance=finuance,app.kubernetes.io/component=api'
  db=$(kubectl -n finuance get pod finuance-db-1 -o jsonpath='{.status.podIP}')
  api=$(kubectl -n finuance get pod -l app.kubernetes.io/component=api -o jsonpath='{.items[0].status.podIP}')
  s3=$(kubectl -n s3 get svc rustfs -o jsonpath='{.spec.clusterIP}')
  [ -n "$db" ] && [ -n "$api" ] && [ -n "$s3" ] || { echo "no address for the database, the api or RustFS" >&2; return 1; }
  # the controls: a pod elsewhere reaches a service without a policy, and Prometheus's label opens
  # the metrics port
  probe netpol-probe control probe=control "nc -z -w 5 $s3 9000" || { echo "the probe can't reach RustFS: the probe itself is broken" >&2; return 1; }
  probe monitoring scrape app.kubernetes.io/name=prometheus "nc -z -w 5 $api 9090" || { echo "a pod with Prometheus's label in monitoring can't reach the metrics port" >&2; return 1; }
  # a pod outside the app's namespace
  if probe netpol-probe db probe=db "nc -z -w 5 $db 5432"; then echo "a pod outside the app reached PostgreSQL" >&2; return 1; fi
  if probe netpol-probe api probe=api "nc -z -w 5 $api 8080"; then echo "a pod outside the app reached the api" >&2; return 1; fi
  if probe netpol-probe metrics probe=metrics "nc -z -w 5 $api 9090"; then echo "a pod outside the monitoring namespace reached the metrics port" >&2; return 1; fi
  # labels that are not enough: the api's labels in another namespace, any other pod of the
  # monitoring namespace
  if probe netpol-probe as-api "$as_api" "nc -z -w 5 $db 5432"; then echo "a pod with the api's labels in another namespace reached PostgreSQL" >&2; return 1; fi
  # the chart's name without a component that uses the database: the probe gets a policy that
  # opens its egress, so that only the database's own rule stands in its way
  kubectl -n finuance apply -f - >/dev/null <<'EOF'
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata: { name: netpol-probe-open-egress }
spec:
  podSelector: { matchLabels: { probe: open-egress } }
  policyTypes: [Egress]
  egress: [{}]
EOF
  local named='app.kubernetes.io/name=finuance,app.kubernetes.io/instance=finuance,probe=open-egress'
  probe finuance named-out "$named" "nc -z -w 5 $s3 9000" || { echo "the probe with open egress can't reach RustFS: the probe itself is broken" >&2; return 1; }
  if probe finuance named-db "$named" "nc -z -w 5 $db 5432"; then echo "a pod with the chart's name but not the api's, worker's or migration's component reached PostgreSQL" >&2; return 1; fi
  kubectl -n finuance delete networkpolicy netpol-probe-open-egress >/dev/null
  if probe monitoring other app.kubernetes.io/name=grafana "nc -z -w 5 $api 9090"; then echo "a pod of monitoring that is not Prometheus reached the metrics port" >&2; return 1; fi
  # a pod in the app's namespace that no policy names: nothing in, nothing out (register OPS-1)
  # A policy reaches a new pod a moment after it starts (Kubernetes promises no more), so a probe
  # that tests the pod's own egress waits before it tries; the first run without the wait got out.
  if probe finuance stray-db probe=stray "sleep 8; nc -z -w 5 $db 5432"; then echo "an unlabelled pod in the app's namespace reached PostgreSQL" >&2; return 1; fi
  if probe finuance stray-out probe=stray "sleep 8; nc -z -w 5 $s3 9000"; then echo "an unlabelled pod in the app's namespace reached a service outside it" >&2; return 1; fi
  if probe finuance stray-api probe=stray "sleep 8; nc -z -w 5 $api 8080"; then echo "an unlabelled pod in the app's namespace reached the api" >&2; return 1; fi
  kubectl delete namespace netpol-probe monitoring --wait=false >/dev/null
  fact "NetworkPolicies" "blocked: an outside pod from PostgreSQL 5432, api 8080 and metrics 9090; the api's labels in another namespace and the chart's name without a database component from 5432; a pod of monitoring that is not Prometheus from 9090; an unlabelled pod in the app's namespace from PostgreSQL, the api and a service outside. Controls: an outside pod reached RustFS, Prometheus's label reached 9090"
}

sse() {
  log "assistant answer streamed through Traefik"
  local jar="$WORK/sse.jar" h=(-H "X-Finuance-Client: web" -H "Origin: $BASE" -H "Content-Type: application/json")
  rm -f "$jar"
  curl_ca -f -o /dev/null -c "$jar" "${h[@]}" -d "{\"email\":\"sse-$(date +%s)@example.test\",\"password\":\"sse password here\"}" "$BASE/api/v1/auth/signup"
  local chat
  chat=$(curl_ca -f -b "$jar" "${h[@]}" -X POST "$BASE/api/v1/assistant/chats" | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')
  # the mock streams its "slow" answer in ~170 pieces, 100 ms apart
  curl_ca -fN -b "$jar" "${h[@]}" -H "Accept: text/event-stream" \
    -d '{"text":"Tell me something slow","settings":{"connection":"mock","model":"mock-tools"}}' \
    "$BASE/api/v1/assistant/chats/$chat/messages" | python3 -u -c '
import sys, time
start = time.monotonic(); first = done = None; events = 0
for line in sys.stdin:
    if line.startswith("event: content"):
        events += 1
        first = first if first is not None else time.monotonic() - start
    if line.startswith("event: done"):
        done = time.monotonic() - start
        break
if first is None or done is None:
    sys.exit(f"stream incomplete: first content {first}, done {done}")
print(f"{events}\t{first:.1f}\t{done:.1f}")
' >"$WORK/sse.tsv"
  local events first done
  IFS=$'\t' read -r events first done <"$WORK/sse.tsv"
  # buffered, everything would arrive at once at the end
  python3 -c "import sys; sys.exit(0 if $first < 3 and $done - $first > 5 and $events > 20 else 1)" ||
    { echo "the stream was buffered: $events content events, first after ${first}s, done after ${done}s" >&2; return 1; }
  fact "SSE through Traefik" "$events content events; first after ${first} s, done after ${done} s (not buffered)"
}

podkill() {
  log "load while api and web pods are deleted and rolled"
  mkdir -p "$WORK/k6" # k6 runs as this user (--user), so the folder needs no wider rights
  docker run --rm --network host --user "$(id -u):$(id -g)" -v "$PWD/$R:/scripts:ro" -v "$WORK/k6:/out" -e BASE_URL="$BASE" -e DURATION=90s -e SKIP_TLS_VERIFY=true \
    "$K6_IMAGE" run --quiet --summary-export=/out/summary.json /scripts/podkill.js >"$WORK/k6/out.txt" 2>&1 &
  local k6=$!
  sleep 15
  local pod
  pod=$(kubectl -n finuance get pods -l app.kubernetes.io/component=api -o jsonpath='{.items[0].metadata.name}')
  echo "   delete $pod"; kubectl -n finuance delete pod "$pod" --wait=false
  sleep 10
  echo "   roll the api"; kubectl -n finuance rollout restart deploy/finuance-api
  kubectl -n finuance rollout status deploy/finuance-api --timeout=180s
  pod=$(kubectl -n finuance get pods -l app.kubernetes.io/component=web -o jsonpath='{.items[0].metadata.name}')
  echo "   delete $pod"; kubectl -n finuance delete pod "$pod" --wait=false
  sleep 5
  echo "   roll web and the worker"; kubectl -n finuance rollout restart deploy/finuance-web deploy/finuance-worker
  kubectl -n finuance rollout status deploy/finuance-web --timeout=180s
  kubectl -n finuance rollout status deploy/finuance-worker --timeout=180s
  local status=0
  wait "$k6" || status=$?
  tail -40 "$WORK/k6/out.txt"
  local line
  line=$(python3 - "$WORK/k6/summary.json" <<'PY' 2>/dev/null || echo "no k6 summary"
import json, sys
m = json.load(open(sys.argv[1]))["metrics"]
reqs = m["http_reqs"]["count"]
failed = m["http_req_failed"].get("passes", round(m["http_req_failed"]["value"] * reqs))
p95, worst = m["http_req_duration"]["p(95)"], m["http_req_duration"]["max"]
print(f"{reqs} requests, {failed} failed, p95 {p95:.0f} ms, max {worst:.0f} ms")
PY
)
  fact "k6 during pod kills (1 api pod deleted, api/web/worker rolled)" "$line"
  return $status
}

restore() {
  log "backup → restore into finuance-restore → temporary app → smoke"
  local h=(-H "X-Finuance-Client: web" -H "Origin: $BASE" -H "Content-Type: application/json")
  local a="marker-a-$(date +%s)@example.test" b="marker-b-$(date +%s)@example.test"
  # A: before the base backup
  curl_ca -f -o /dev/null "${h[@]}" -d "{\"email\":\"$a\",\"password\":\"marker password a\"}" "$BASE/api/v1/auth/signup"
  local t0; t0=$(date +%s)
  kubectl -n finuance apply -f - <<EOF
apiVersion: postgresql.cnpg.io/v1
kind: Backup
metadata:
  name: rehearsal-$t0
spec:
  cluster: { name: finuance-db }
  method: plugin
  pluginConfiguration: { name: barman-cloud.cloudnative-pg.io }
EOF
  wait_for 600 "the on-demand backup" backup_done finuance "rehearsal-$t0"
  timing "on-demand base backup" $(($(date +%s) - t0))
  # B: after it; it comes back only through the archived WAL
  curl_ca -f -o /dev/null "${h[@]}" -d "{\"email\":\"$b\",\"password\":\"marker password b\"}" "$BASE/api/v1/auth/signup"
  local tb; tb=$(date +%s)
  # how long until B's WAL is in the bucket (bounded by archive_timeout, 5 min): the RPO for a
  # loss of the whole cluster
  wait_for 420 "the WAL with marker B archived" archived_since "$tb"
  local rpo=$(($(date +%s) - tb))
  timing "RPO: a write until its WAL is archived (archive_timeout 5 min)" "$rpo"

  namespace finuance-restore
  t0=$(date +%s)
  helm upgrade --install finuance-db deploy/k8s/db -n finuance-restore -f "$R/restore-db-values.yaml"
  kubectl -n finuance-restore wait cluster/finuance-db --for=condition=Ready --timeout=900s
  timing "restored cluster ready" $(($(date +%s) - t0))
  local found
  found=$(psql_in finuance-restore "SELECT count(*) FROM users WHERE email IN ('$a', '$b')")
  [ "$found" = 2 ] || { echo "restored database has $found of the 2 markers" >&2; return 1; }
  [ "$(psql_in finuance-restore "SELECT proleakproof FROM pg_proc WHERE proname = 'textlike'")" = t ] || { echo "textlike lost LEAKPROOF in the restore" >&2; return 1; }
  fact "restored data" "both markers (before the base backup and in the WAL after it), textlike LEAKPROOF"
  helm upgrade --install finuance deploy/helm/finuance -n finuance-restore -f "$R/restore-app-values.yaml" --wait --timeout 10m
  wait_for 120 "$RESTORE_BASE/healthz" curl_ca -f "$RESTORE_BASE/healthz"
  wait_for 120 "the restored api through Traefik" curl_ca -f -o /dev/null "$RESTORE_BASE/api/v1/meta"
  # the managed roles set the restore namespace's own passwords, and marker B can sign in
  curl_ca -f -o /dev/null -H "X-Finuance-Client: web" -H "Origin: $RESTORE_BASE" -H "Content-Type: application/json" \
    -d "{\"email\":\"$b\",\"password\":\"marker password b\"}" "$RESTORE_BASE/api/v1/auth/login"
  run_smoke "$RESTORE_BASE" finuance-restore
  timing "RTO: restore started until the smoke scripts pass on it" $(($(date +%s) - t0))
}

limits() {
  log "limits at the ingress (PLAN 23.6)"
  local t0; t0=$(date +%s)
  helm upgrade finuance deploy/helm/finuance -n finuance -f "$R/app-values.yaml" --set ingress.limits.enabled=true --wait --timeout 5m
  wait_for 120 "the api through Traefik with the limits on" curl_ca -f -o /dev/null "$BASE/api/v1/meta"
  sleep 5 # Traefik picks the new routers up on its own schedule
  # normal use is not affected: the smoke scripts and a streamed answer
  run_smoke "$BASE" finuance
  sse
  # a burst of sign-ins from one address meets the strict limit
  local h=(-H "X-Finuance-Client: web" -H "Origin: $BASE" -H "Content-Type: application/json") codes
  codes=$(for _ in $(seq 60); do
    curl_ca -o /dev/null -w '%{http_code}\n' "${h[@]}" -d '{"email":"nobody@example.test","password":"not the password"}' "$BASE/api/v1/auth/login"
  done | sort | uniq -c | tr -s ' ' | tr '\n' ';')
  echo "   60 sign-ins: $codes"
  grep -q ' 429' <<<"$codes" || { echo "no 429 at the ingress for 60 sign-ins in a row" >&2; return 1; }
  grep -q ' 401' <<<"$codes" || { echo "no sign-in reached the api before the limit" >&2; return 1; }
  # the limit is the auth path's own: the rest of the API still answers
  curl_ca -f -o /dev/null "$BASE/api/v1/meta" || { echo "the API is limited outside /api/v1/auth/ after the sign-in burst" >&2; return 1; }
  fact "Ingress limits" "smoke scripts and the streamed answer pass with the limits on; 60 sign-ins in a row:$codes the rest of the API still answers"
  timing "ingress limits (upgrade, smoke, stream, burst)" $(($(date +%s) - t0))
}

# try_image <namespace> <image>: would a pod with this image be created? (a server-side dry run goes
# through admission and creates nothing); prints the API server's answer
try_image() { kubectl -n "$1" run signature-probe --image "$2" --restart=Never --dry-run=server -o name 2>&1; }

signatures() {
  log "image signatures: Sigstore policy-controller (PLAN 23.2)"
  local t0; t0=$(date +%s)
  helm repo add sigstore https://sigstore.github.io/helm-charts --force-update >/dev/null
  helm upgrade --install policy-controller sigstore/policy-controller --version "$(argo_version policy-controller)" \
    -n cosign-system --create-namespace -f deploy/k8s/policy-controller/values.yaml --wait --timeout 5m
  kubectl apply -f deploy/k8s/policy-controller/policies/ -f "$R/signature-test-policy.yaml"
  kubectl create namespace signed --dry-run=client -o yaml | kubectl apply -f -
  kubectl label namespace signed --overwrite policy.sigstore.dev/include=true
  timing "policy-controller installed" $(($(date +%s) - t0))

  # the released images, by the digests the homelab deploys
  local api web out
  api="ghcr.io/x2db736ac85h/finuance-api@$("$YQ" '.image.digests.api' deploy/environments/homelab/values.yaml)"
  web="ghcr.io/x2db736ac85h/finuance-web@$("$YQ" '.image.digests.web' deploy/environments/homelab/values.yaml)"
  # the policies reach the webhook a moment after they are applied
  wait_for 180 "the released api image is admitted" try_image signed "$api" || { try_image signed "$api" >&2; return 1; }
  try_image signed "$web" >/dev/null || { echo "the released web image was refused:" >&2; try_image signed "$web" >&2; return 1; }

  # refused: an image the release workflow did not sign (held to the same authority by the test policy)
  refused_by_policy() { ! out=$(try_image signed "$PAUSE_IMAGE") && grep -q 'rehearsal-not-ours' <<<"$out"; }
  wait_for 120 "an image without the release workflow's signature is refused" refused_by_policy ||
    { echo "expected a refusal naming the policy rehearsal-not-ours, got:" >&2; try_image signed "$PAUSE_IMAGE" >&2; return 1; }
  # refused: this checkout's own build, which nobody signed (it is not in the registry at all)
  if out=$(try_image signed "$API_IMAGE"); then echo "the unsigned local build was admitted in a labelled namespace" >&2; return 1; fi
  # refused: an image no policy names
  if out=$(try_image signed "$BUSYBOX_IMAGE"); then echo "an image no policy names was admitted" >&2; return 1; fi
  grep -q 'no matching policies' <<<"$out" || { echo "expected 'no matching policies', got: $out" >&2; return 1; }
  # CloudNativePG's images pass on the registry list (the static policy)
  try_image signed "$("$YQ" '.cluster.imageName // .imageName' deploy/k8s/db/values.yaml)" >/dev/null ||
    { echo "the PostgreSQL image was refused in a labelled namespace" >&2; return 1; }
  # a namespace without the label is not checked
  try_image default "$PAUSE_IMAGE" >/dev/null || { echo "a namespace without the label was checked" >&2; return 1; }
  kubectl delete namespace signed --wait=false >/dev/null
  fact "Image signatures" "released api and web admitted by digest; refused: an image without the release workflow's signature, the unsigned local build, an image no policy names; unlabelled namespace not checked"
}

summary() {
  {
    echo "### Deployment rehearsal (kind, $(date -u +%Y-%m-%d))"
    echo
    echo "| Step | Seconds |"
    echo "|---|---:|"
    [ -f "$WORK/timings.tsv" ] && awk -F'\t' '{ printf "| %s | %s |\n", $1, $2 }' "$WORK/timings.tsv"
    echo
    echo "| Check | Result |"
    echo "|---|---|"
    [ -f "$WORK/facts.tsv" ] && awk -F'\t' '{ printf "| %s | %s |\n", $1, $2 }' "$WORK/facts.tsv"
  } >"$SUMMARY"
  cat "$SUMMARY"
  if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
    cat "$SUMMARY" >>"$GITHUB_STEP_SUMMARY"
    # also as an annotation: readable through the API where the job summary isn't
    annotate notice "rehearsal summary" "$SUMMARY"
  fi
  return 0
}

diagnostics() {
  log "diagnostics"
  kubectl get nodes -o wide || true
  kubectl get pods -A -o wide || true
  kubectl get events -A --sort-by=.lastTimestamp | tail -60 || true
  for ns in finuance finuance-restore; do
    kubectl -n "$ns" get clusters.postgresql.cnpg.io,backups.postgresql.cnpg.io,objectstores,sealedsecrets,secrets,ingress,networkpolicies 2>/dev/null || true
    kubectl -n "$ns" describe cluster finuance-db 2>/dev/null | tail -40 || true
    for p in $(kubectl -n "$ns" get pods -o name 2>/dev/null); do
      echo "--- $ns $p"; kubectl -n "$ns" logs "$p" --all-containers --tail=60 2>/dev/null || true
    done
  done
  kubectl -n cnpg-system logs deploy/cnpg-cloudnative-pg --tail=80 2>/dev/null || true
  kubectl -n cnpg-system logs deploy/barman-cloud --tail=80 2>/dev/null || kubectl -n cnpg-system logs -l app.kubernetes.io/name=plugin-barman-cloud --tail=80 2>/dev/null || true
}

# report: GitHub annotations with the failing step's last lines and the cluster's state. The job
# log can't be read everywhere the results are needed (the cloud session that builds Finuance gets
# 403 on logs and artifacts), annotations can. At most 10 per kind and step, a few KB each.
annotate() { # annotate <error|warning> <title> <file>
  [ -s "$3" ] || return 0
  python3 - "$1" "$2" "$3" <<'PY'
import re, sys
level, title, path = sys.argv[1:4]
text = re.sub(r"\x1b\[[0-9;]*[A-Za-z]", "", open(path, errors="replace").read())[-7000:]
esc = text.replace("%", "%25").replace("\r", "").replace("\n", "%0A")
print(f"::{level} title={title}::{esc}")
PY
}

report() {
  local last
  last=$(ls -t "$WORK"/logs/*.log 2>/dev/null | head -1 || true)
  if [ -n "$last" ]; then
    tail -120 "$last" >"$WORK/report-step.txt"
    annotate error "rehearsal step $(basename "$last" .log) (last lines)" "$WORK/report-step.txt"
  fi
  kubectl get pods -A -o wide >"$WORK/report-pods.txt" 2>&1 || true
  annotate warning "pods" "$WORK/report-pods.txt"
  kubectl get events -A --sort-by=.lastTimestamp 2>&1 | grep -v ' Normal ' | tail -50 >"$WORK/report-events.txt" || true
  annotate warning "warning events" "$WORK/report-events.txt"
  local n=0
  # pods that aren't running and ready: describe + logs
  kubectl get pods -A --no-headers 2>/dev/null | awk '{ split($3, r, "/") } ($4 != "Running" && $4 != "Completed") || ($4 == "Running" && r[1] != r[2])' | head -4 |
    while read -r ns pod _; do
      { kubectl -n "$ns" describe pod "$pod" | sed -n '/^Containers:/,$p' | tail -60; kubectl -n "$ns" logs "$pod" --all-containers --tail=40; } >"$WORK/report-pod-$n.txt" 2>&1 || true
      annotate warning "pod $ns/$pod" "$WORK/report-pod-$n.txt"
      n=$((n + 1))
    done
  for ns in finuance finuance-restore; do
    kubectl -n "$ns" get clusters.postgresql.cnpg.io finuance-db -o yaml 2>/dev/null | sed -n '/^status:/,$p' | head -80 >"$WORK/report-cluster-$ns.txt" || true
    annotate warning "CNPG cluster status ($ns)" "$WORK/report-cluster-$ns.txt"
  done
  { kubectl -n cnpg-system logs deploy/cnpg-cloudnative-pg --tail=30; kubectl -n cnpg-system logs -l app.kubernetes.io/name=plugin-barman-cloud --tail=30; } 2>&1 | grep -iE 'error|fail|warn' | tail -40 >"$WORK/report-operator.txt" || true
  annotate warning "operator and plugin errors" "$WORK/report-operator.txt"
}

down() { kind delete cluster --name "$CLUSTER" || true; }

case ${1:-all} in
  all)
    rm -f "$WORK/timings.tsv" "$WORK/facts.tsv"
    images; cluster; platform; deploy; smoke; netpol; sse; podkill; restore; limits; signatures; summary
    ;;
  images | cluster | platform | deploy | smoke | netpol | sse | podkill | restore | limits | signatures)
    mkdir -p "$WORK/logs"
    "$1" 2>&1 | tee "$WORK/logs/$1.log"
    ;;
  summary | diagnostics | report | down) "$1" ;;
  *) sed -n '2,15p' "$0" | sed 's/^# \{0,1\}//'; exit 2 ;;
esac
