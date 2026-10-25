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
render app-homelab finuance "$app" finuance deploy/environments/homelab/values.yaml
for f in "$db"/ci/*.yaml; do render "db-$(basename "$f" .yaml)" finuance-db "$db" finuance "$f"; done
render db-homelab finuance-db "$db" finuance deploy/environments/homelab/db-values.yaml

# The assistant's connections file: every container told to read it must mount it (the rehearsal
# found the worker crash-looping on a path only the api had).
echo "== assistant connections file is mounted where it is read"
"$YQ" -e 'select(.kind == "Deployment" or .kind == "Job") | .spec.template.spec.containers[]
  | select(.env[]? | .name == "FINUANCE_AI_CONNECTIONS_FILE") | select([.volumeMounts[]? | .mountPath] | contains(["/etc/finuance"]) | not)
  | .name' "$out/app-homelab-like-values.yaml" >/dev/null 2>&1 && { echo "check-helm: a container reads the connections file without mounting it"; exit 1; }
"$YQ" -e '.data.FINUANCE_AI_CONNECTIONS_FILE' "$out/app-homelab-like-values.yaml" >/dev/null 2>&1 &&
  { echo "check-helm: FINUANCE_AI_CONNECTIONS_FILE is in the shared ConfigMap (every component would read it)"; exit 1; }
echo "ok"

echo "== Argo CD applications"
validate deploy/k8s/argocd/root.yaml deploy/k8s/argocd/apps/

# Wrong settings must stop the render with a message that says what to set.
o=(--set publicOrigin=https://f.example.com --set ingress.host=f.example.com)
must_fail "publicOrigin is empty" "publicOrigin" "$app" --set ingress.host=f.example.com
must_fail "publicOrigin has a path" "publicOrigin" "$app" --set publicOrigin=https://f.example.com/app --set ingress.host=f.example.com
must_fail "the ingress host is empty" "ingress.host is required" "$app" --set publicOrigin=https://f.example.com
must_fail "certManager has no issuer" "ingress.tls.clusterIssuer is required" "$app" "${o[@]}"
must_fail "traefik TLS has no resolver" "ingress.tls.certResolver is required" "$app" "${o[@]}" --set ingress.tls.mode=traefik
must_fail "an unknown TLS mode is set" "ingress.tls.mode" "$app" "${o[@]}" --set ingress.tls.mode=acme
must_fail "extraEnv sets a non-FINUANCE variable" "extraEnv" "$app" "${o[@]}" --set ingress.tls.mode=none --set extraEnv.PATH=/tmp
must_fail "the backup has no bucket" "objectStore.destinationPath is required" "$db"
must_fail "the backup schedule has 5 fields" "backup.schedule" "$db" --set objectStore.destinationPath=s3://b/ --set "backup.schedule=0 2 * * *"
must_fail "a restore archives into its origin's folder" "must archive to another folder" "$db" \
  --set objectStore.destinationPath=s3://b/ --set recovery.enabled=true
[ $failures -eq 0 ] || exit 1
echo "check-helm: ok (rendered manifests in $out)"
