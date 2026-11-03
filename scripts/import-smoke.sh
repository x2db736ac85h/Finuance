#!/usr/bin/env bash
# Import smoke test against a running stack (api + worker), PLAN Phase 6:
#   account → upload A, B (profile), C (autoCommit) of the overlap set → the account holds the
#   legacy oracle's merged count → re-upload is a 409 → deleting C leaves the A+B count.
#
#   make up && scripts/import-smoke.sh
#
# BASE_URL defaults to the Compose API (http://localhost:8080); ORIGIN must equal the stack's
# FINUANCE_PUBLIC_ORIGIN (CSRF check). METRICS_URL is the worker's admin port (Compose publishes it
# on 9091). Needs curl only. Each run uses a fresh user, deleted at the end.
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
ORIGIN="${ORIGIN:-http://localhost:8000}"
METRICS_URL="${METRICS_URL:-http://localhost:9091/metrics}"
API="$BASE_URL/api/v1"
EMAIL="import-smoke-$(date +%s)-$RANDOM@example.test"
PASS="import smoke password"
# An existing user instead of a sign-up, for instances with sign-up off (scripts/smoke-k8s.sh makes
# a throwaway user per script); the password comes from the environment only.
if [[ -n ${SMOKE_EMAIL:-} ]]; then EMAIL=$SMOKE_EMAIL; PASS=${SMOKE_PASSWORD:?set SMOKE_PASSWORD with SMOKE_EMAIL}; fi
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
FIX="$ROOT/testdata/fixtures/synthetic/overlap"
ORACLE="$ROOT/testdata/oracle/dedupe"

# The first "rowCount" of an oracle file is its default (merged) result.
rowcount() { grep -m1 '"rowCount"' "$1" | tr -dc '0-9'; }
WANT_ABC="$(rowcount "$ORACLE/overlap-A-B-C.json")"
WANT_AB="$(rowcount "$ORACLE/overlap-A-B.json")"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
jar="$work/jar"
: > "$jar"

step=0
check() { # check STATUS WANT LABEL
  step=$((step + 1))
  if [[ $1 != "$2" ]]; then
    echo "FAIL step $step: $3 → $1, want $2" >&2
    cat "$work/body" >&2; echo >&2
    exit 1
  fi
  printf 'ok %2d  %-40s → %s\n' "$step" "$3" "$1"
}
curl_api() { curl -sS -o "$work/body" -w '%{http_code}' -b "$jar" -c "$jar" -H "X-Finuance-Client: web" -H "Origin: $ORIGIN" "$@"; }
# call METHOD PATH EXPECTED_STATUS [JSON]
call() {
  local args=(-X "$1")
  [[ -n ${4:-} ]] && args+=(-H "Content-Type: application/json" --data "$4")
  check "$(curl_api "${args[@]}" "$API$2")" "$3" "$1 $2"
}
# upload FILE EXPECTED_STATUS [QUERY]
upload() {
  check "$(curl_api -X POST -F "accountId=$account" -F "file=@$FIX/$1;type=text/csv" "$API/imports${3:-}")" "$2" "upload $1${3:-}"
}
field() { grep -o "\"$1\":\"[^\"]*\"" "$work/body" | head -1 | cut -d'"' -f4; }
number() { grep -o "\"$1\":[0-9]*" "$work/body" | head -1 | cut -d: -f2; }
await() { # await IMPORT_ID — poll until the commit job has run
  for _ in $(seq 100); do
    curl_api "$API/imports/$1" > /dev/null
    [[ $(field status) != committing ]] && break
    sleep 0.2
  done
  step=$((step + 1))
  [[ $(field status) == committed ]] || { echo "FAIL step $step: import $1 is $(field status)" >&2; cat "$work/body" >&2; exit 1; }
  printf 'ok %2d  %-40s → committed\n' "$step" "wait for $1"
}
expect_tx() { # expect_tx COUNT
  call GET /accounts 200
  step=$((step + 1))
  [[ $(number txCount) == "$1" ]] || { echo "FAIL step $step: txCount $(number txCount), want $1" >&2; exit 1; }
  printf 'ok %2d  %-40s → %s\n' "$step" "account holds $1 transactions" "$1"
}

if [[ -n ${SMOKE_EMAIL:-} ]]; then
  call POST /auth/login 200 "{\"email\":\"$EMAIL\",\"password\":\"$PASS\"}"
else
  call POST /auth/signup 201 "{\"email\":\"$EMAIL\",\"password\":\"$PASS\"}"
fi
call POST /accounts 201 '{"name":"Import smoke"}'
account="$(field id)"

upload A_jan-mar.csv 201
[[ $(field mappingSource) == detected ]]
a="$(field id)"
call POST "/imports/$a/commit" 202 '{}'
await "$a"

upload B_mar-may.csv 201
[[ $(field mappingSource) == profile ]] || { echo "FAIL: B was not read with the saved profile" >&2; exit 1; }
b="$(field id)"
call POST "/imports/$b/commit" 202 '{}'
await "$b"

upload C_q1_again.csv 202 '?autoCommit=true'   # profile + no errors + account: the fast path
c="$(field id)"
await "$c"
expect_tx "$WANT_ABC"

upload A_jan-mar.csv 409
[[ $(field existingImportId) == "$a" ]] || { echo "FAIL: 409 without the existing import id" >&2; exit 1; }

call DELETE "/imports/$c" 204
expect_tx "$WANT_AB"

step=$((step + 1))
committed="$(curl -sS "$METRICS_URL" | grep '^finuance_imports_total{result="committed"}' | cut -d' ' -f2)"
[[ -n $committed ]] || { echo "FAIL step $step: no finuance_imports_total on $METRICS_URL" >&2; exit 1; }
printf 'ok %2d  %-40s → %s\n' "$step" "worker metrics: imports committed" "$committed"

call DELETE /me 204 "{\"password\":\"$PASS\"}"   # clean up
echo "import smoke: all $step steps passed ($EMAIL; oracle A+B+C = $WANT_ABC, A+B = $WANT_AB)"
