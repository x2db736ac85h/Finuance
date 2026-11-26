#!/usr/bin/env bash
# The Google Cloud lab, up and down (PLAN 27.5, docs/gcp.md). It costs money while it is up, so
# nothing here runs by accident:
#   - the values come from a variables file that is never committed (GCP_TFVARS, default
#     deploy/terraform/lab.tfvars), the state bucket's name from GCP_STATE_BUCKET;
#   - `up` and `down` ask for the project's id to be typed (or CONFIRM=<project id>);
#   - `verify-empty` lists what is left under the lab's name after `down`, and fails if anything is.
#
#   tools/gcp-lab.sh plan | up | down | verify-empty | outputs
#
# Needs: Terraform (tools/bin-tools.sh terraform), and for verify-empty the gcloud CLI, signed in
# as someone who may read the project (gcloud auth login; for Terraform: gcloud auth
# application-default login). Not yet run against a real project: docs/gcp.md says what to expect.
set -euo pipefail
cd "$(dirname "$0")/.."

cmd=${1:-}
tf=backend/bin/terraform
dir=deploy/terraform
vars=${GCP_TFVARS:-$dir/lab.tfvars}
fail() { echo "gcp-lab: $*" >&2; exit 1; }

case "$cmd" in plan|up|down|verify-empty|outputs) ;; *) fail "usage: tools/gcp-lab.sh plan|up|down|verify-empty|outputs" ;; esac
[ -f "$vars" ] || fail "no variables file at $vars. Copy $dir/example.tfvars to it (it is ignored by git) and fill it in, or set GCP_TFVARS."
case "$vars" in /*) abs=$vars ;; *) abs=$PWD/$vars ;; esac

# one value of the variables file: `name = "value"` on a line of its own
value() { sed -nE "s/^[[:space:]]*$1[[:space:]]*=[[:space:]]*\"([^\"]*)\".*/\1/p" "$vars" | head -1; }
project=$(value project)
name=$(value name); name=${name:-finuance-lab}
[ -n "$project" ] || fail "$vars names no project"
[ "$project" != example-project-1 ] || fail "$vars still has the example's project"

init() {
  [ -x "$tf" ] || tools/bin-tools.sh terraform
  [ -n "${GCP_STATE_BUCKET:-}" ] || fail "set GCP_STATE_BUCKET to the bucket that holds the lab's state (made once by hand, docs/gcp.md)"
  "$tf" -chdir="$dir" init -input=false -lockfile=readonly -backend-config="bucket=$GCP_STATE_BUCKET" >/dev/null
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
  down)
    confirm "This destroys the lab and everything in it"
    init
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
    # lab's own (everything of the lab starts with its name)
    look() {
      local what=$1; shift
      local found
      found=$("$@" --project "$project" --format='value(name)' 2>/dev/null | grep -E "^(.*/)?${name}" || true)
      if [ -n "$found" ]; then
        left=$((left + 1))
        echo "   left: $what"; echo "$found" | sed 's/^/      /'
      fi
    }
    echo "== what is left of $name in $project"
    look "clusters"            gcloud container clusters list
    look "disks"               gcloud compute disks list --filter="name~^(gke-)?${name}"
    look "addresses"           gcloud compute addresses list
    look "forwarding rules"    gcloud compute forwarding-rules list
    look "backend services"    gcloud compute backend-services list
    look "network endpoint groups" gcloud compute network-endpoint-groups list
    look "routers (NAT)"       gcloud compute routers list
    look "networks"            gcloud compute networks list
    look "security policies"   gcloud compute security-policies list
    look "DNS zones"           gcloud dns managed-zones list
    look "certificates"        gcloud certificate-manager certificates list
    look "certificate maps"    gcloud certificate-manager maps list
    look "secrets"             gcloud secrets list
    look "service accounts"    gcloud iam service-accounts list --filter="email~^${name}"
    if [ "$left" -gt 0 ]; then fail "$left kinds of resources are left (above). Remove them, or run make gcp-down again."; fi
    echo "   nothing: no cluster, disk, address, load balancer, network, policy, zone, certificate, secret or identity of the lab."
    echo "   (The state bucket and the enabled services stay on purpose; neither is billed by the hour.)"
    ;;
esac
