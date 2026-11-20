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
# Schema errors name the value by its JSON pointer since Helm 3.22 (`at '/ingress/tls/mode'`). Helm
# 3.22 reports a propertyNames error at a wrong location (`/otel` for `extraEnv`), so that case
# matches the pattern instead.
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
# The worker writes the assistant's digests (ADR 0033), so it gets the connections too.
"$YQ" -e 'select(.kind == "Deployment" and .metadata.name == "finuance-worker") | .spec.template.spec.containers[].env[] | select(.name == "FINUANCE_AI_CONNECTIONS_FILE")' \
  "$out/app-homelab-like-values.yaml" >/dev/null 2>&1 || { echo "check-helm: the worker doesn't get the assistant's connections (digests)"; exit 1; }
# The instance key (ADR 0035) reaches both processes that open stored API keys.
[ "$("$YQ" -e 'select(.kind == "Deployment") | .spec.template.spec.containers[].envFrom[]? | select(.secretRef.name == "finuance-instance-key") | .secretRef.name' \
  "$out/app-homelab-like-values.yaml" 2>/dev/null | grep -c finuance-instance-key)" -eq 2 ] || { echo "check-helm: the api and the worker don't both get the instance key"; exit 1; }
# The Developer section's links: only the api reads them, and it mounts them.
"$YQ" -e 'select(.kind == "Deployment" or .kind == "Job") | .spec.template.spec.containers[]
  | select(.env[]? | .name == "FINUANCE_DEVELOPER_LINKS_FILE") | select([.volumeMounts[]? | .mountPath] | contains(["/etc/finuance-developer"]) | not)
  | .name' "$out/app-homelab-like-values.yaml" >/dev/null 2>&1 && { echo "check-helm: a container reads the developer links without mounting them"; exit 1; }
"$YQ" -e 'select(.kind == "Deployment" and .metadata.name != "finuance-api") | .spec.template.spec.containers[].env[]? | select(.name == "FINUANCE_DEVELOPER_LINKS_FILE")' \
  "$out/app-homelab-like-values.yaml" >/dev/null 2>&1 && { echo "check-helm: a component other than the api reads the developer links"; exit 1; }
# NetworkPolicies (register OPS-1, OPS-9, SYS-1): the default deny selects every pod of the
# namespace; names are resolved in kube-system only; the database is a pod of the release's own
# namespace; the metrics port is for Prometheus's pods; the database admits the api, the worker
# and the migration job and says what its own pods may reach.
np="$out/app-homelab-like-values.yaml"
[ "$("$YQ" -N -o json -I0 'select(.kind == "NetworkPolicy" and .metadata.name == "finuance-default-deny") | .spec.podSelector' "$np")" = '{}' ] ||
  { echo "check-helm: the default deny does not select every pod of the namespace"; exit 1; }
[ "$("$YQ" -N 'select(.kind == "NetworkPolicy") | .spec.egress[]?.to[]? | select(.podSelector.matchLabels."k8s-app" == "kube-dns") | .namespaceSelector.matchLabels."kubernetes.io/metadata.name"' "$np" | sort -u)" = kube-system ] ||
  { echo "check-helm: a DNS rule reaches beyond kube-system"; exit 1; }
"$YQ" -N 'select(.kind == "NetworkPolicy") | .spec.egress[]?.to[]? | select(.podSelector.matchLabels."cnpg.io/cluster") | has("namespaceSelector")' "$np" | grep -q true &&
  { echo "check-helm: the database rule reaches other namespaces"; exit 1; }
[ "$("$YQ" -N 'select(.kind == "NetworkPolicy") | .spec.ingress[]? | select(.ports[0].port == 9090) | .from[0].podSelector.matchLabels."app.kubernetes.io/name"' "$np" | sort -u)" = prometheus ] ||
  { echo "check-helm: the metrics port is open to more than Prometheus's pods"; exit 1; }
"$YQ" -N -o json -I0 'select(.kind == "NetworkPolicy") | .spec | .. | select(tag == "!!map" and has("namespaceSelector")) | .namespaceSelector' "$np" "$out/db-backup-values.yaml" | grep -qx '{}' &&
  { echo "check-helm: a NetworkPolicy rule selects every namespace"; exit 1; }
dbnp="$out/db-homelab.yaml"
[ "$("$YQ" -N -o json -I0 'select(.kind == "NetworkPolicy") | .spec.ingress[0].from[0].podSelector.matchExpressions[0].values' "$dbnp")" = '["api","worker","migrate"]' ] ||
  { echo "check-helm: the database admits more than the api, the worker and the migration job"; exit 1; }
[ "$("$YQ" -N -o json -I0 'select(.kind == "NetworkPolicy") | .spec.policyTypes' "$dbnp")" = '["Ingress","Egress"]' ] ||
  { echo "check-helm: the database's policy has no egress rule (the namespace's default deny would cut its pods off)"; exit 1; }
"$YQ" -N 'select(.kind == "NetworkPolicy") | .spec.ingress[] | select(.from[0].namespaceSelector) | has("ports")' "$dbnp" | grep -q false &&
  { echo "check-helm: a namespace reaches every port of the database pods"; exit 1; }
# Limits at the ingress (PLAN 23.6): off by default (no Middleware but the HSTS one, one Ingress);
# on: /api/v1/auth/ has its own Ingress with the strict limit, the rest the general one, both the
# in-flight cap, and every Middleware an Ingress names exists.
[ "$("$YQ" -N 'select(.kind == "Ingress" or (.kind == "Middleware" and .metadata.name != "finuance-headers")) | .metadata.name' "$out/app-homelab-like-values.yaml" | tr '\n' ' ')" = "finuance " ] ||
  { echo "check-helm: the ingress limits are rendered although they are off"; exit 1; }
lim="$out/app-limits-values.yaml"
[ "$("$YQ" -N 'select(.kind == "Ingress" and .metadata.name == "finuance-auth") | .spec.rules[0].http.paths[0].path' "$lim")" = /api/v1/auth/ ] ||
  { echo "check-helm: no Ingress for /api/v1/auth/ with the limits on"; exit 1; }
for pair in "finuance:finuance-ratelimit" "finuance-auth:finuance-ratelimit-auth" "finuance:finuance-inflight" "finuance-auth:finuance-inflight"; do
  "$YQ" -N "select(.kind == \"Ingress\" and .metadata.name == \"${pair%%:*}\") | .metadata.annotations.\"traefik.ingress.kubernetes.io/router.middlewares\"" "$lim" |
    grep -q "finuance-${pair##*:}@kubernetescrd" || { echo "check-helm: Ingress ${pair%%:*} does not use Middleware ${pair##*:}"; exit 1; }
done
"$YQ" -N 'select(.kind == "Ingress" and .metadata.name == "finuance-auth") | .metadata.annotations."traefik.ingress.kubernetes.io/router.middlewares"' "$lim" |
  grep -q 'finuance-ratelimit@' && { echo "check-helm: the auth Ingress also has the general limit"; exit 1; }
for name in $("$YQ" -N 'select(.kind == "Ingress") | .metadata.annotations."traefik.ingress.kubernetes.io/router.middlewares"' "$lim" | tr ',' '\n' | sed 's/^finuance-//; s/@kubernetescrd$//' | sort -u); do
  [ "$("$YQ" -N "select(.kind == \"Middleware\" and .metadata.name == \"$name\") | .metadata.name" "$lim")" = "$name" ] ||
    { echo "check-helm: an Ingress names Middleware $name, which the chart does not render"; exit 1; }
done
[ "$("$YQ" -N 'select(.kind == "Middleware" and .metadata.name == "finuance-ratelimit-auth") | .spec.rateLimit.sourceCriterion.ipStrategy.depth' "$lim")" = 1 ] ||
  { echo "check-helm: forwardedDepth does not reach the Middlewares"; exit 1; }
# Deploy by digest (PLAN 23.1): with image.digests set every container runs registry/name@sha256:…
# and no tag; without them, the tag. The homelab's values carry digests.
d1="sha256:$(printf '1%.0s' $(seq 64))" d2="sha256:$(printf '2%.0s' $(seq 64))"
by_digest=$(helm template t "$app" --kube-version "$K8S" -f "$app/ci/homelab-like-values.yaml" --set image.digests.api="$d1" --set image.digests.web="$d2" | grep -E '^ +image: ' | sort | uniq -c | tr -s ' ')
want=" 3 image: ghcr.io/x2db736ac85h/finuance-api@$d1
 1 image: ghcr.io/x2db736ac85h/finuance-web@$d2"
[ "$by_digest" = "$want" ] || { echo "check-helm: with image.digests the images are:"; echo "$by_digest"; exit 1; }
grep -E '^ +image: ' "$out/app-homelab-like-values.yaml" | grep -q '@sha256:' && { echo "check-helm: an image has a digest without image.digests"; exit 1; }
[ "$(grep -E '^ +image: ' "$out/app-homelab.yaml" | grep -cE 'finuance-(api|web)@sha256:[0-9a-f]{64}$')" -eq 4 ] ||
  { echo "check-helm: the homelab's values don't deploy every image by digest"; exit 1; }
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

echo "== image signature policies (Sigstore policy-controller)"
# checked against the CRD of the pinned chart itself (the catalog's copy is older than the chart)
pc_version=$("$YQ" '.spec.sources[0].targetRevision' deploy/k8s/argocd/apps/policy-controller.yaml)
helm template policy-controller policy-controller --repo https://sigstore.github.io/helm-charts --version "$pc_version" \
  --show-only templates/crds/clusterimagepolicy.yaml |
  "$YQ" -o json '.spec.versions[] | select(.name == "v1beta1") | .schema.openAPIV3Schema' >"$out/clusterimagepolicy_v1beta1.json"
[ -s "$out/clusterimagepolicy_v1beta1.json" ] || { echo "check-helm: no ClusterImagePolicy schema in chart $pc_version"; exit 1; }
kubeconform -summary -schema-location "$out/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json" \
  deploy/k8s/policy-controller/policies/ deploy/k8s/rehearsal/signature-test-policy.yaml
# the chart renders with this repository's values
helm template policy-controller policy-controller --repo https://sigstore.github.io/helm-charts --version "$pc_version" \
  -n cosign-system --kube-version "$K8S" -f deploy/k8s/policy-controller/values.yaml | grep -E '^ +image: ' | grep -v '@sha256:' &&
  { echo "check-helm: policy-controller renders an image without a digest"; exit 1; }
# the rehearsal's test policy must hold its image to the same signer as Finuance's own images
[ "$("$YQ" 'select(.metadata.name == "finuance-images") | .spec.authorities[0].keyless.identities[0]' deploy/k8s/policy-controller/policies/finuance-images.yaml)" = \
  "$("$YQ" '.spec.authorities[0].keyless.identities[0]' deploy/k8s/rehearsal/signature-test-policy.yaml)" ] ||
  { echo "check-helm: the rehearsal's test policy and finuance-images name different signers"; exit 1; }
# the signer is the identity release.yml verifies before it publishes
signer=$("$YQ" 'select(.metadata.name == "finuance-images") | .spec.authorities[0].keyless.identities[0].subjectRegExp' deploy/k8s/policy-controller/policies/finuance-images.yaml | sed 's/\.\*\$$//')
[ "${#signer}" -gt 40 ] || { echo "check-helm: finuance-images names no signer"; exit 1; }
grep -qF -- "$signer" .github/workflows/release.yml ||
  { echo "check-helm: finuance-images' signer is not the identity in release.yml"; exit 1; }
echo "ok"

# Wrong settings must stop the render with a message that says what to set.
o=(--set publicOrigin=https://f.example.com --set ingress.host=f.example.com)
must_fail "publicOrigin is empty" "publicOrigin" "$app" --set ingress.host=f.example.com
must_fail "a range of trusted proxies without network policies" "trustedProxies names a range" "$app" -f "$app/ci/minimal-values.yaml" --set trustedProxies=10.42.0.0/16
must_fail "publicOrigin has a path" "publicOrigin" "$app" --set publicOrigin=https://f.example.com/app --set ingress.host=f.example.com
must_fail "the ingress host is empty" "ingress.host is required" "$app" --set publicOrigin=https://f.example.com
must_fail "certManager has no issuer" "ingress.tls.clusterIssuer is required" "$app" "${o[@]}"
must_fail "traefik TLS has no resolver" "ingress.tls.certResolver is required" "$app" "${o[@]}" --set ingress.tls.mode=traefik
must_fail "an unknown TLS mode is set" "/ingress/tls/mode" "$app" "${o[@]}" --set ingress.tls.mode=acme
must_fail "the OTLP endpoint has no path" "/otel/endpoint" "$app" "${o[@]}" --set ingress.tls.mode=none --set otel.endpoint=http://tempo.monitoring.svc:4318
must_fail "a developer link carries a password" "developer" "$app" "${o[@]}" --set ingress.tls.mode=none \
  --set developer.enabled=true --set 'developer.links[0].name=Grafana' --set 'developer.links[0].url=https://admin:pw@grafana.example.com'
must_fail "a developer link isn't http(s)" "developer" "$app" "${o[@]}" --set ingress.tls.mode=none \
  --set developer.enabled=true --set 'developer.links[0].name=X' --set 'developer.links[0].url=javascript:alert(1)'
must_fail "an image digest is malformed" "/image/digests/api" "$app" "${o[@]}" --set ingress.tls.mode=none --set image.digests.api=sha256:abc --set image.digests.web="$d2"
must_fail "an image digest carries a tag" "/image/digests/web" "$app" "${o[@]}" --set ingress.tls.mode=none --set image.digests.api="$d1" --set image.digests.web="0.4.2@$d2"
must_fail "only one image digest is set" "image.digests needs both" "$app" "${o[@]}" --set ingress.tls.mode=none --set image.digests.api="$d1"
must_fail "a rate limit's period has no unit" "/ingress/limits/requests/period" "$app" "${o[@]}" --set ingress.tls.mode=none --set ingress.limits.enabled=true --set ingress.limits.requests.period=60
must_fail "a rate limit is zero" "/ingress/limits/auth/average" "$app" "${o[@]}" --set ingress.tls.mode=none --set ingress.limits.enabled=true --set ingress.limits.auth.average=0
must_fail "extraEnv sets a non-FINUANCE variable" "^FINUANCE_[A-Z0-9_]+$" "$app" "${o[@]}" --set ingress.tls.mode=none --set extraEnv.PATH=/tmp
must_fail "the backup has no bucket" "objectStore.destinationPath is required" "$db"
must_fail "the backup schedule has 5 fields" "/backup/schedule" "$db" --set objectStore.destinationPath=s3://b/ --set "backup.schedule=0 2 * * *"
must_fail "a restore archives into its origin's folder" "must archive to another folder" "$db" \
  --set objectStore.destinationPath=s3://b/ --set recovery.enabled=true
# Argo CD projects (register OPS-4): the project for this repository's plain manifests lists
# exactly the kinds those manifests use, nothing with a wildcard, and its applications use it.
echo "== the platform-manifests project lists the kinds of its manifests"
projects=deploy/k8s/argocd/apps/projects.yaml
pm='select(.kind == "AppProject" and .metadata.name == "platform-manifests") | .spec'
"$YQ" -N "$pm | (.clusterResourceWhitelist + .namespaceResourceWhitelist + .destinations)[] | .[]" "$projects" | grep -qx '\*' &&
  { echo "check-helm: the platform-manifests project has a wildcard"; failures=$((failures + 1)); }
[ "$("$YQ" -N "$pm | .sourceRepos | length" "$projects")" = 1 ] || { echo "check-helm: the platform-manifests project reads more than this repository"; failures=$((failures + 1)); }
allowed=$("$YQ" -N "$pm | (.clusterResourceWhitelist + .namespaceResourceWhitelist)[] | .group + \"/\" + .kind" "$projects" | grep -v '/Namespace$' | sort)
used=$("$YQ" -N '(.apiVersion | sub("/?[^/]+$", "")) + "/" + .kind' deploy/k8s/admission/*.yaml deploy/k8s/tempo/*.yaml | sort -u)
[ "$allowed" = "$used" ] || { echo "check-helm: the platform-manifests project and its manifests differ:"; diff <(echo "$allowed") <(echo "$used") || true; failures=$((failures + 1)); }
for a in admission-policy tempo; do
  [ "$("$YQ" -N '.spec.project' "deploy/k8s/argocd/apps/$a.yaml")" = platform-manifests ] || { echo "check-helm: application $a is not in the platform-manifests project"; failures=$((failures + 1)); }
done
for a in deploy/k8s/argocd/apps/*.yaml; do
  [ "$("$YQ" -N 'select(.kind == "Application") | select(.spec.project == "platform") | [.spec.source, .spec.sources[]] | map(select(. != null and has("path"))) | map(.path) | map(select(test("policy-controller/policies$|^homelab/monitoring$") | not)) | length' "$a")" = 0 ] 2>/dev/null ||
    [ -z "$("$YQ" -N 'select(.kind == "Application") | select(.spec.project == "platform") | .metadata.name' "$a")" ] ||
    { echo "check-helm: $a (project platform) syncs a folder of manifests; it belongs in platform-manifests"; failures=$((failures + 1)); }
done
echo ok

[ $failures -eq 0 ] || exit 1
echo "check-helm: ok (rendered manifests in $out)"
