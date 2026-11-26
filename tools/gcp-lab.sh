#!/usr/bin/env bash
# The Google Cloud lab, up and down (PLAN 27.5, docs/gcp.md). It costs money while it is up, so
# nothing here runs by accident:
#   - the values come from a variables file that is never committed (GCP_TFVARS, default
#     deploy/terraform/lab.tfvars), the state bucket's name from GCP_STATE_BUCKET;
#   - `up` and `down` ask for the project's id to be typed (or CONFIRM=<project id>);
#   - `install` puts Argo CD on the new cluster (the homelab's chart and values), the lab's
#     namespaces and Secrets, and the lab's root application (deploy/k8s/argocd/gke-lab), which
#     installs the rest from main;
#   - `down` first deletes through Kubernetes what Google made for the cluster outside Terraform
#     (the Gateway's load balancer, the database's disk), which a destroyed cluster leaves behind;
#   - `verify-empty` lists what is left of the lab after `down`, and fails if anything is.
#
#   tools/gcp-lab.sh plan | up | install | down | verify-empty | outputs
#
# Needs: Terraform (tools/bin-tools.sh terraform); for install, down and verify-empty the gcloud CLI,
# signed in as someone who may use the project (gcloud auth login; for Terraform: gcloud auth
# application-default login), and kubectl and helm for install. Not yet run against a real project:
# docs/gcp.md says what to expect.
set -euo pipefail
cd "$(dirname "$0")/.."

cmd=${1:-}
tf=backend/bin/terraform
dir=deploy/terraform
vars=${GCP_TFVARS:-$dir/lab.tfvars}
fail() { echo "gcp-lab: $*" >&2; exit 1; }

case "$cmd" in plan|up|install|down|verify-empty|outputs) ;; *) fail "usage: tools/gcp-lab.sh plan|up|install|down|verify-empty|outputs" ;; esac
[ -f "$vars" ] || fail "no variables file at $vars. Copy $dir/example.tfvars to it (it is ignored by git) and fill it in, or set GCP_TFVARS."
case "$vars" in /*) abs=$vars ;; *) abs=$PWD/$vars ;; esac

# one value of the variables file: `name = "value"` on a line of its own
value() { sed -nE "s/^[[:space:]]*$1[[:space:]]*=[[:space:]]*\"([^\"]*)\".*/\1/p" "$vars" | head -1; }
project=$(value project)
name=$(value name); name=${name:-finuance-lab}
region=$(value region); region=${region:-europe-west4}
# Argo CD's chart, the homelab's version (docs/RUNBOOK.md "Argo CD"; tools/check-helm.sh keeps them equal)
ARGOCD_CHART=10.11.6
[ -n "$project" ] || fail "$vars names no project"
[ "$project" != example-project-1 ] || fail "$vars still has the example's project"

init() {
  [ -n "${GCP_STATE_BUCKET:-}" ] || fail "set GCP_STATE_BUCKET to the bucket that holds the lab's state (made once by hand, docs/gcp.md)"
  [ -x "$tf" ] || tools/bin-tools.sh terraform
  "$tf" -chdir="$dir" init -input=false -lockfile=readonly -backend-config="bucket=$GCP_STATE_BUCKET" >/dev/null
}

# the cluster's credentials for kubectl, through its DNS endpoint (the address endpoint is off)
credentials() {
  gcloud container clusters get-credentials "$name" --region "$region" --project "$project" --dns-endpoint >/dev/null
}

# db_secret <name> <role>: a role's password for the database, made here and never shown (as
# tools/seal-secrets.sh db does for the homelab); an existing one is kept
db_secret() {
  kubectl -n finuance-lab get secret "$1" >/dev/null 2>&1 && return 0
  local pw
  pw=$(LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom 2>/dev/null | head -c 32 || true)
  # printf is the shell's own: the value is on no command line
  printf 'apiVersion: v1\nkind: Secret\ntype: kubernetes.io/basic-auth\nmetadata:\n  name: %s\n  namespace: finuance-lab\n  labels:\n    app.kubernetes.io/part-of: finuance\n    cnpg.io/reload: "true"\nstringData:\n  username: %s\n  password: %s\n' \
    "$1" "$2" "$pw" | kubectl apply -f - >/dev/null
}

# sm_secret <namespace> <name> <key> <Secret Manager id>: a value Terraform wrote to Secret Manager
# (deploy/terraform/secrets.tf), read with the operator's own login
sm_secret() {
  local v
  v=$(gcloud secrets versions access latest --secret="$4" --project="$project")
  printf 'apiVersion: v1\nkind: Secret\nmetadata:\n  name: %s\n  namespace: %s\n  labels:\n    app.kubernetes.io/part-of: finuance\nstringData:\n  %s: %s\n' \
    "$2" "$1" "$3" "$v" | kubectl apply -f - >/dev/null
}

confirm() {
  local what=$1 typed=${CONFIRM:-}
  if [ -z "$typed" ]; then
    [ -t 0 ] || fail "$what needs a confirmation: run it in a terminal, or set CONFIRM=$project"
    printf '%s in the project %s. Type the project id to go on: ' "$what" "$project" >&2
    read -r typed
  fi
  [ "$typed" = "$project" ] || fail "not confirmed (the project id didn't match)"
}

case "$cmd" in
  plan)
    init
    "$tf" -chdir="$dir" plan -input=false -var-file="$abs" ;;
  up)
    confirm "This creates the lab, which is billed until it is destroyed,"
    init
    "$tf" -chdir="$dir" apply -input=false -auto-approve -var-file="$abs"
    echo
    echo "gcp-lab: the infrastructure is up. Next (docs/gcp.md): point the host's DNS record at the address"
    echo "above, then install the lab on the cluster. When you are done: make gcp-down, then make gcp-verify-empty."
    ;;
  install)
    # the private overlay (ADR 0023) is read with a deploy key of its own (read-only, made by hand
    # on finuance-env); its file never enters the repository or a command line
    [ -f "${ENV_DEPLOY_KEY:-}" ] || fail "set ENV_DEPLOY_KEY to the file with the private half of finuance-env's read-only deploy key (docs/gcp.md)"
    for c in gcloud kubectl helm; do command -v "$c" >/dev/null || fail "install needs $c"; done
    credentials
    echo "== Argo CD $ARGOCD_CHART (the homelab's chart and values)"
    helm upgrade --install argocd argo-cd --repo https://argoproj.github.io/argo-helm --version "$ARGOCD_CHART" \
      -n argocd --create-namespace -f deploy/environments/homelab/argocd-values.yaml --wait --timeout 10m
    kubectl -n argocd create secret generic finuance-env-repo --from-literal=type=git \
      --from-literal=url=git@github.com:x2db736ac85h/finuance-env.git --from-file=sshPrivateKey="$ENV_DEPLOY_KEY" \
      --dry-run=client -o yaml | kubectl label --local -f - argocd.argoproj.io/secret-type=repository -o yaml | kubectl apply -f -
    echo "== the lab's namespaces (restricted Pod Security) and Secrets"
    for ns in finuance-lab finuance-sim; do
      kubectl create namespace "$ns" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
      kubectl label namespace "$ns" --overwrite pod-security.kubernetes.io/enforce=restricted pod-security.kubernetes.io/enforce-version=latest >/dev/null
    done
    db_secret finuance-db-owner finuance_owner
    db_secret finuance-db-app finuance_app
    # the personas' passwords come from the seed: the instance's job and the simulator both need it
    sm_secret finuance-lab finuance-sim-seed seed "$name-sim-seed"
    sm_secret finuance-sim finuance-sim-seed seed "$name-sim-seed"
    sm_secret finuance-sim finuance-sim-control token "$name-sim-control"
    echo "== the lab's root application"
    kubectl apply -f deploy/k8s/argocd/gke-lab/root.yaml
    echo
    echo "gcp-lab: Argo CD installs the lab from main now (kubectl -n argocd get applications). Then:"
    echo "TARGET_BASE=https://<host> TARGET_NAMESPACE=finuance-lab tools/rehearsal.sh target"
    ;;
  down)
    confirm "This destroys the lab and everything in it"
    init
    # Google makes the Gateway's load balancer and the database's disk for the cluster, outside
    # Terraform, and removes them only when their Kubernetes objects go: delete those first (Argo CD's
    # applications, so that nothing makes them again, then the lab's namespaces), and wait.
    if command -v kubectl >/dev/null && credentials 2>/dev/null; then
      echo "== the lab's objects in the cluster (the load balancer and the disk go with them)"
      kubectl -n argocd delete applications --all --wait=true --timeout=2m || true
      kubectl delete namespace finuance-gateway finuance-lab finuance-sim --ignore-not-found --wait=true --timeout=15m ||
        fail "the lab's namespaces are still being deleted; run make gcp-down again in a few minutes"
      kubectl wait --for=delete pv --all --timeout=10m 2>/dev/null || true
    else
      echo "gcp-lab: the cluster can't be reached; if it still exists, its load balancer and disks may be left (make gcp-verify-empty shows them)" >&2
    fi
    "$tf" -chdir="$dir" destroy -input=false -auto-approve -var-file="$abs"
    echo "gcp-lab: destroyed. Run make gcp-verify-empty to see that nothing with a cost is left."
    ;;
  outputs)
    init
    "$tf" -chdir="$dir" output -json lab_values ;;
  verify-empty)
    command -v gcloud >/dev/null || fail "verify-empty needs the gcloud CLI"
    left=0
    # each line: a kind of resource that costs money or holds an identity, and how to list the
    # lab's own. Terraform's names start with the lab's name; what Google makes for the cluster has
    # it inside the name (gkegw1-…-finuance-gateway-<name>-…, k8s1-…-finuance-lab-…) or, for a
    # volume's disk (pvc-…), in the description, which names the claim's namespace.
    look() {
      local what=$1; shift
      local found
      found=$("$@" --project "$project" --format='value(name,description)' 2>/dev/null | grep -F -- "$name" | cut -f1 |
        grep -vxF -- "${GCP_STATE_BUCKET:-/}" || true)
      if [ -n "$found" ]; then
        left=$((left + 1))
        echo "   left: $what"; echo "$found" | sed 's/^/      /'
      fi
    }
    echo "== what is left of $name in $project"
    look "clusters"            gcloud container clusters list
    look "disks"               gcloud compute disks list
    look "addresses"           gcloud compute addresses list
    look "forwarding rules"    gcloud compute forwarding-rules list
    look "target proxies"      gcloud compute target-https-proxies list
    look "URL maps"            gcloud compute url-maps list
    look "backend services"    gcloud compute backend-services list
    look "health checks"       gcloud compute health-checks list
    look "network endpoint groups" gcloud compute network-endpoint-groups list
    look "firewall rules"      gcloud compute firewall-rules list
    look "routers (NAT)"       gcloud compute routers list
    look "networks"            gcloud compute networks list
    look "security policies"   gcloud compute security-policies list
    look "DNS zones"           gcloud dns managed-zones list
    look "certificates"        gcloud certificate-manager certificates list
    look "certificate maps"    gcloud certificate-manager maps list
    look "buckets (not the state's)" gcloud storage buckets list
    look "secrets"             gcloud secrets list
    look "service accounts"    gcloud iam service-accounts list --filter="email~^${name}"
    if [ "$left" -gt 0 ]; then fail "$left kinds of resources are left (above). Remove them, or run make gcp-down again."; fi
    echo "   nothing: no cluster, disk, address, load balancer, network, policy, zone, certificate, bucket, secret or identity of the lab."
    echo "   (The state bucket and the enabled services stay on purpose; neither is billed by the hour.)"
    ;;
esac
