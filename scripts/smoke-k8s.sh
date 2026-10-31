#!/usr/bin/env bash
# Runs the smoke scripts (auth, import, query) against a Kubernetes install with sign-up off
# (docs/RUNBOOK.md "Bootstrap on single-node k3s"): for each script a throwaway user made with
# `finuance admin create-user` (random password, only in this process and the script's environment),
# the script through the ingress as that user, and the script's own DELETE /me at the end.
#
#   BASE_URL=https://<host> scripts/smoke-k8s.sh
#
# NAMESPACE (default finuance); ORIGIN (default BASE_URL) is the app's public origin, when the API is
# reached another way (e.g. a port-forward to the api in the restore drill). Needs kubectl access to the namespace, curl and openssl.
set -euo pipefail
cd "$(dirname "$0")/.."
: "${BASE_URL:?set BASE_URL, e.g. https://<host>}"
ORIGIN=${ORIGIN:-$BASE_URL}
ns=${NAMESPACE:-finuance}

# The import smoke reads the worker's metrics.
kubectl -n "$ns" port-forward deploy/finuance-worker "${METRICS_PORT:-19091}":9090 >/dev/null 2>&1 &
pf=$!
email="" pass=""
cleanup() {
  kill "$pf" 2>/dev/null || true
  # A script that failed half-way leaves its user: remove it (auth-smoke may have changed the password).
  [ -n "$email" ] || return 0
  for p in "$pass" "smoke password two"; do
    c=$(mktemp)
    if curl -sS -o /dev/null -c "$c" -H "X-Finuance-Client: web" -H "Origin: $ORIGIN" -H "Content-Type: application/json" \
         --data "$(printf '{"email":"%s","password":"%s"}' "$email" "$p")" -w '%{http_code}' "$BASE_URL/api/v1/auth/login" | grep -q 200; then
      curl -sS -o /dev/null -b "$c" -X DELETE -H "X-Finuance-Client: web" -H "Origin: $ORIGIN" -H "Content-Type: application/json" \
        --data "$(printf '{"password":"%s"}' "$p")" "$BASE_URL/api/v1/me" && echo "smoke-k8s: removed the leftover user $email"
      rm -f "$c"; break
    fi
    rm -f "$c"
  done
}
trap cleanup EXIT
for _ in $(seq 30); do curl -fsS http://localhost:${METRICS_PORT:-19091}/metrics >/dev/null 2>&1 && break; sleep 1; done

for s in auth-smoke import-smoke query-smoke; do
  email="$s-$(date +%s)-$RANDOM@example.test"
  pass=$(openssl rand -base64 24)
  printf '%s' "$pass" | kubectl -n "$ns" exec -i deploy/finuance-api -- \
    /finuance admin create-user -email="$email" -display-name=Smoke -password-stdin >/dev/null
  SMOKE_EMAIL=$email SMOKE_PASSWORD=$pass BASE_URL=$BASE_URL ORIGIN=$ORIGIN \
    METRICS_URL=http://localhost:${METRICS_PORT:-19091}/metrics "scripts/$s.sh"
  email=""   # the script deleted its user
done
echo "smoke-k8s: all three smoke scripts passed against $BASE_URL"
