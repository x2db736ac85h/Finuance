#!/usr/bin/env bash
# Checks the Google Cloud lab's Terraform (deploy/terraform, PLAN 27.4) without any cloud account:
# its formatting, that it is valid against the pinned providers' real schemas, its tests on a plan
# with made-up providers, that no service-account key is created anywhere, that no value of a
# real project is committed, and a misconfiguration scan with nothing of HIGH or above.
# Needs: tools/bin-tools.sh terraform and trivy (installed here when missing). Network: the
# providers are downloaded once from the registry (checked against the committed lock file).
set -euo pipefail
cd "$(dirname "$0")/.."

tf=backend/bin/terraform
trivy=backend/bin/trivy
[ -x "$tf" ] || tools/bin-tools.sh terraform
[ -x "$trivy" ] || tools/bin-tools.sh trivy
dir=deploy/terraform
fail() { echo "check-terraform: $*" >&2; exit 1; }

echo "== format"
"$tf" -chdir="$dir" fmt -check -recursive -diff || fail "run: $tf -chdir=$dir fmt -recursive"

echo "== providers as locked, configuration valid"
[ -f "$dir/.terraform.lock.hcl" ] || fail "the provider lock file is missing"
# -backend=false: no state bucket is touched; -lockfile=readonly: a provider that differs from the
# committed checksums stops here
"$tf" -chdir="$dir" init -backend=false -input=false -lockfile=readonly >/dev/null
"$tf" -chdir="$dir" validate -no-color | sed 's/^/   /'

echo "== tests (a plan with made-up providers)"
"$tf" -chdir="$dir" test -no-color | grep -E '^(tests/|  run |Success|Failure)' | sed 's/^/   /'
"$tf" -chdir="$dir" test -no-color >/dev/null

echo "== no key of any service account, no project's own values"
# an identity here is used through workload identity; a key would be a long-lived secret in the state
if grep -rnE '^\s*(resource|data|ephemeral)\s+"google_(service_account_key|iam_workload_identity_pool_provider_key)"' "$dir" --include='*.tf'; then
  fail "a service-account key is created (see above): use workload identity instead"
fi
# variables' values come from a file that is never committed (*.tfvars is ignored), not from defaults
if git ls-files "$dir" | grep -E '\.tfvars$|\.tfstate' | grep -v '^deploy/terraform/example\.tfvars$'; then
  fail "a variables or state file is committed (see above)"
fi
if grep -nE '^\s*default\s*=' "$dir/variables.tf" | grep -iE 'project|[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+'; then
  fail "variables.tf has a default that looks like a project or an address (see above)"
fi
echo "   ok"

echo "== misconfiguration scan (HIGH and CRITICAL fail)"
"$trivy" config --severity HIGH,CRITICAL --exit-code 1 --disable-telemetry --quiet "$dir"
echo "   ok"

echo "check-terraform: ok"
