#!/usr/bin/env bash
# Checks the deployment manifests (PLAN tasks 16.1–16.4): lints and renders both charts with every
# values file the repository uses, validates the rendered manifests and the Argo CD applications
# with kubeconform (strict, CRD schemas from the datree catalog), and checks that wrong settings
# fail to render. RENDER_DIR keeps the rendered manifests (the CI helm job scans them with Trivy).
# Needs helm, kubeconform and mikefarah's yq ($YQ) (versions in .github/workflows/ci.yml) and network access for schemas.
set -euo pipefail
cd "$(dirname "$0")/.."
K8S=1.33.0
YQ=${YQ:-yq} # mikefarah's yq v4
out=${RENDER_DIR:-$(mktemp -d)}
mkdir -p "$out"
CRDS='https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json'

validate() {
  kubeconform -strict -summary -kubernetes-version "$K8S" -schema-location default -schema-location "$CRDS" "$@"
}

# render <file name> <release> <chart> <namespace> [values files...]
render() {
  local file=$1 release=$2 chart=$3 ns=$4; shift 4
  local args=(--kube-version "$K8S")
  for v in "$@"; do args+=(-f "$v"); done
  echo "== $chart ${*:-(defaults)}"
  helm lint "$chart" --strict "${args[@]}" | grep -vE '^(==> Linting|\[INFO\]|$|1 chart\(s\) linted)' || true
  helm lint "$chart" --strict "${args[@]}" >/dev/null
  helm template "$release" "$chart" -n "$ns" "${args[@]}" >"$out/$file.yaml"
  validate "$out/$file.yaml"
}

# must_fail <what> <expected message part> <chart> [helm args...]
failures=0
must_fail() {
  local what=$1 want=$2 chart=$3; shift 3
  local msg
  if msg=$(helm template t "$chart" --kube-version "$K8S" "$@" 2>&1); then
    echo "check-helm: rendered although $what"; failures=$((failures + 1))
  elif ! grep -qF -- "$want" <<<"$msg"; then
    echo "check-helm: $what failed with an unexpected message:"; echo "$msg" | tail -3; failures=$((failures + 1))
  fi
}

app=deploy/helm/finuance
db=deploy/k8s/db
for f in "$app"/ci/*.yaml; do render "app-$(basename "$f" .yaml)" finuance "$app" finuance "$f"; done
# the homelab layering as Argo CD applies it (ADR 0023): generic values, then the per-machine overlay;
# the example stands in for the private one, which is rendered too when it is cloned next to this
# repository (local runs only; its render is not kept)
render app-homelab finuance "$app" finuance deploy/environments/homelab/values.yaml deploy/environments/homelab/host-values.example.yaml
env_overlay="${FINUANCE_ENV_DIR:-../finuance-env}/homelab/host-values.yaml"
if [ -f "$env_overlay" ]; then
  echo "== $app with the private overlay"
  helm lint "$app" --strict --kube-version "$K8S" -f deploy/environments/homelab/values.yaml -f "$env_overlay" >/dev/null
  helm template finuance "$app" -n finuance --kube-version "$K8S" -f deploy/environments/homelab/values.yaml -f "$env_overlay" | validate -
fi
for f in "$db"/ci/*.yaml; do render "db-$(basename "$f" .yaml)" finuance-db "$db" finuance "$f"; done
render db-homelab finuance-db "$db" finuance deploy/environments/homelab/db-values.yaml
# the restore drill on the homelab (tools/homelab-drill.sh)
render db-homelab-restore finuance-db "$db" finuance-restore deploy/environments/homelab/restore-db-values.yaml
render app-homelab-restore finuance "$app" finuance-restore deploy/environments/homelab/restore-app-values.yaml

# The assistant's connections file: every container told to read it must mount it (the rehearsal
# found the worker crash-looping on a path only the api had).
echo "== assistant connections file is mounted where it is read"
"$YQ" -e 'select(.kind == "Deployment" or .kind == "Job") | .spec.template.spec.containers[]
  | select(.env[]? | .name == "FINUANCE_AI_CONNECTIONS_FILE") | select([.volumeMounts[]? | .mountPath] | contains(["/etc/finuance"]) | not)
  | .name' "$out/app-homelab-like-values.yaml" >/dev/null 2>&1 && { echo "check-helm: a container reads the connections file without mounting it"; exit 1; }
"$YQ" -e '.data.FINUANCE_AI_CONNECTIONS_FILE' "$out/app-homelab-like-values.yaml" >/dev/null 2>&1 &&
  { echo "check-helm: FINUANCE_AI_CONNECTIONS_FILE is in the shared ConfigMap (every component would read it)"; exit 1; }
# The Developer section's links: only the api reads them, and it mounts them.
"$YQ" -e 'select(.kind == "Deployment" or .kind == "Job") | .spec.template.spec.containers[]
  | select(.env[]? | .name == "FINUANCE_DEVELOPER_LINKS_FILE") | select([.volumeMounts[]? | .mountPath] | contains(["/etc/finuance-developer"]) | not)
  | .name' "$out/app-homelab-like-values.yaml" >/dev/null 2>&1 && { echo "check-helm: a container reads the developer links without mounting them"; exit 1; }
"$YQ" -e 'select(.kind == "Deployment" and .metadata.name != "finuance-api") | .spec.template.spec.containers[].env[]? | select(.name == "FINUANCE_DEVELOPER_LINKS_FILE")' \
  "$out/app-homelab-like-values.yaml" >/dev/null 2>&1 && { echo "check-helm: a component other than the api reads the developer links"; exit 1; }
echo "ok"

echo "== Argo CD applications"
validate deploy/k8s/argocd/root.yaml deploy/k8s/argocd/apps/

# A phase may point the apps at its branch to try it on the cluster; what reaches main must follow main.
branch=${GITHUB_BASE_REF:-${GITHUB_REF_NAME:-$(git rev-parse --abbrev-ref HEAD)}}
if [ "$branch" = main ]; then
  echo "== Argo CD apps follow main"
  off=$("$YQ" -N '(.spec.source, .spec.sources[]?) | select(. != null and (.repoURL | test("x2db736ac85h/")) and .targetRevision != "main")
    | filename + ": " + .repoURL + " @ " + .targetRevision' deploy/k8s/argocd/root.yaml deploy/k8s/argocd/apps/*.yaml)
  [ -z "$off" ] || { echo "check-helm: Argo CD apps must follow main on main:"; echo "$off"; exit 1; }
  echo "ok"
fi

echo "== RustFS (in-cluster backup bucket)"
validate deploy/k8s/rustfs/

echo "== Tempo (traces)"
validate deploy/k8s/tempo/

echo "== image admission policy"
validate deploy/k8s/admission/

# Wrong settings must stop the render with a message that says what to set.
o=(--set publicOrigin=https://f.example.com --set ingress.host=f.example.com)
must_fail "publicOrigin is empty" "publicOrigin" "$app" --set ingress.host=f.example.com
must_fail "publicOrigin has a path" "publicOrigin" "$app" --set publicOrigin=https://f.example.com/app --set ingress.host=f.example.com
must_fail "the ingress host is empty" "ingress.host is required" "$app" --set publicOrigin=https://f.example.com
must_fail "certManager has no issuer" "ingress.tls.clusterIssuer is required" "$app" "${o[@]}"
must_fail "traefik TLS has no resolver" "ingress.tls.certResolver is required" "$app" "${o[@]}" --set ingress.tls.mode=traefik
must_fail "an unknown TLS mode is set" "ingress.tls.mode" "$app" "${o[@]}" --set ingress.tls.mode=acme
must_fail "the OTLP endpoint has no path" "otel.endpoint" "$app" "${o[@]}" --set ingress.tls.mode=none --set otel.endpoint=http://tempo.monitoring.svc:4318
must_fail "a developer link carries a password" "developer" "$app" "${o[@]}" --set ingress.tls.mode=none \
  --set developer.enabled=true --set 'developer.links[0].name=Grafana' --set 'developer.links[0].url=https://admin:pw@grafana.example.com'
must_fail "a developer link isn't http(s)" "developer" "$app" "${o[@]}" --set ingress.tls.mode=none \
  --set developer.enabled=true --set 'developer.links[0].name=X' --set 'developer.links[0].url=javascript:alert(1)'
must_fail "extraEnv sets a non-FINUANCE variable" "extraEnv" "$app" "${o[@]}" --set ingress.tls.mode=none --set extraEnv.PATH=/tmp
must_fail "the backup has no bucket" "objectStore.destinationPath is required" "$db"
must_fail "the backup schedule has 5 fields" "backup.schedule" "$db" --set objectStore.destinationPath=s3://b/ --set "backup.schedule=0 2 * * *"
must_fail "a restore archives into its origin's folder" "must archive to another folder" "$db" \
  --set objectStore.destinationPath=s3://b/ --set recovery.enabled=true
[ $failures -eq 0 ] || exit 1
echo "check-helm: ok (rendered manifests in $out)"
