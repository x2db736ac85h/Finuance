#!/usr/bin/env bash
# The locks of tools/gcp-lab.sh (PLAN 27.5), tried without a Google account: each case must be
# refused with its own message before anything is planned, created, installed or destroyed. Run by
# tools/check-terraform.sh. No case reaches Google: each refusal comes before Terraform's init, and
# gcloud is kept off the PATH.
set -euo pipefail
cd "$(dirname "$0")/.."

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
# a PATH without gcloud, kubectl or helm: the script's own tools only
mkdir -p "$work/bin"
for t in bash dirname sed head grep cat env mktemp tr; do
  p=$(command -v "$t" 2>/dev/null) && ln -sf "$p" "$work/bin/$t"
done
real=$work/real.tfvars example=$work/example.tfvars
printf 'project       = "lab-project-7"\nhost          = "lab.example.org"\nallowed_cidrs = ["192.0.2.10/32"]\n' >"$real"
printf 'project       = "example-project-1"\n' >"$example"
printf 'not a key\n' >"$work/key"

failures=0
# refused <what> <expected message part> <env assignments and arguments…>
refused() {
  local what=$1 want=$2; shift 2
  local out status=0
  out=$(env -i PATH="$work/bin" HOME="$work" "$@" bash tools/gcp-lab.sh "${args[@]}" </dev/null 2>&1) || status=$?
  if [ "$status" -eq 0 ]; then echo "gcp-lab-test: not refused: $what"; failures=$((failures + 1))
  elif ! grep -qF -- "$want" <<<"$out"; then echo "gcp-lab-test: $what refused with another message: $out"; failures=$((failures + 1))
  else echo "   refused: $what"; fi
}
args=(launch);       refused "an unknown command" "usage: tools/gcp-lab.sh" GCP_TFVARS="$real"
args=(plan);         refused "no variables file" "no variables file" GCP_TFVARS="$work/missing.tfvars"
args=(up);           refused "the example's project" "still has the example's project" GCP_TFVARS="$example"
args=(up);           refused "up without a terminal or CONFIRM" "needs a confirmation" GCP_TFVARS="$real"
args=(down);         refused "down without a terminal or CONFIRM" "needs a confirmation" GCP_TFVARS="$real"
args=(up);           refused "up with another project's id" "not confirmed" GCP_TFVARS="$real" CONFIRM=lab-project-8
args=(down);         refused "down with another project's id" "not confirmed" GCP_TFVARS="$real" CONFIRM=lab-project-8
args=(up);           refused "up without the state bucket" "set GCP_STATE_BUCKET" GCP_TFVARS="$real" CONFIRM=lab-project-7
args=(install);      refused "install without the overlay's deploy key" "set ENV_DEPLOY_KEY" GCP_TFVARS="$real"
args=(install);      refused "install without gcloud" "install needs gcloud" GCP_TFVARS="$real" ENV_DEPLOY_KEY="$work/key"
args=(verify-empty); refused "verify-empty without gcloud" "verify-empty needs the gcloud CLI" GCP_TFVARS="$real"
[ "$failures" -eq 0 ] || exit 1
echo "gcp-lab-test: ok"
