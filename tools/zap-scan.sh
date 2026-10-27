#!/usr/bin/env bash
# OWASP ZAP against a running stack (PLAN 17.6; the weekly workflow and `make zap`):
#   1. the baseline scan of the web app (passive: headers, cookies, CSP, information leaks);
#   2. the API scan from api/openapi.yaml (active attacks on every operation), signed in as a
#      fresh user through .zap/hooks.py.
# Rules and the reason for each ignored one are in .zap/rules.tsv. Fails when either scan reports
# a WARN or FAIL that the rules don't ignore. Reports: $ZAP_OUT/baseline.{json,html}, $ZAP_OUT/api.json
# (ZAP's HTML report of the much larger API scan sometimes fails to render).
# Needs Docker and a stack with sign-ups at $BASE_URL (make up, or compose.ci.yaml in CI).
set -euo pipefail
cd "$(dirname "$0")/.."
BASE_URL=${BASE_URL:-http://localhost:8000}
ZAP_OUT=${ZAP_OUT:-zap-reports}
ZAP_IMAGE=zaproxy/zap-stable:2.17.0@sha256:030d3df57735dacbb83464854758e674dd9ad01d79d12a129a8e58b1fd384238
mkdir -p "$ZAP_OUT" && chmod 777 "$ZAP_OUT" # ZAP runs as uid 1000
out=$(cd "$ZAP_OUT" && pwd)

# The API scan's user: a new account, so a scan never touches anyone's data.
email="zap-$(date +%s)-$RANDOM@example.test"
jar=$(mktemp)
trap 'rm -f "$jar"' EXIT
code=$(curl -s -o /dev/null -w '%{http_code}' -c "$jar" -H 'Content-Type: application/json' -H 'X-Finuance-Client: web' \
  -H "Origin: $BASE_URL" -d "{\"email\":\"$email\",\"password\":\"zap scan password $RANDOM\"}" "$BASE_URL/api/v1/auth/signup")
[ "$code" = 201 ] || { echo "zap-scan: sign-up answered $code (the stack needs FINUANCE_ALLOW_SIGNUP)"; exit 1; }
session=$(awk '$6 == "finuance_session" {print $7}' "$jar")
curl -sf -o /dev/null -b "$jar" -H 'X-Finuance-Client: web' -H "Origin: $BASE_URL" -X POST "$BASE_URL/api/v1/demo-data"

zap() {
  docker run --rm --network host -e ZAP_BASE_URL="$BASE_URL" -e ZAP_SESSION="$session" \
    -v "$out:/zap/wrk:rw" -v "$PWD/.zap:/zap/config:ro" -v "$PWD/api:/zap/api:ro" "$ZAP_IMAGE" "$@"
}
# The API scan attacks every operation in a copy of the spec without the ones that would end its
# own session (ZAP calls each operation while importing, so excluding URLs isn't enough).
zap python3 -c '
import yaml
spec = yaml.safe_load(open("/zap/api/openapi.yaml"))
paths = spec["paths"]
for p in ("/auth/logout", "/me/password", "/me/sessions/{id}"):
    paths.pop(p, None)
paths["/me"].pop("delete", None)
yaml.safe_dump(spec, open("/zap/wrk/openapi-scan.yaml", "w"))
'
status=0
echo "== baseline scan of $BASE_URL"
zap zap-baseline.py -t "$BASE_URL" -c /zap/config/rules.tsv -J baseline.json -r baseline.html || status=$?
echo "== API scan from api/openapi.yaml"
zap zap-api-scan.py -t /zap/wrk/openapi-scan.yaml -f openapi -O "$BASE_URL" -c /zap/config/rules.tsv \
  --hook /zap/config/hooks.py -J api.json || status=$((status | $?))
# exit codes: 1 = a FAIL, 2 = a WARN, 3 = the scan itself failed
[ "$status" -eq 0 ] && echo "zap-scan: ok" || echo "zap-scan: findings (exit $status); see $ZAP_OUT/*.html"
exit "$status"
