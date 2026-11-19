#!/usr/bin/env bash
# Checks on a running Compose stack (make test-e2e KEEP=1, or make up) that a request leaves no user
# text in the logs (PLAN task 23.4): a search typed into the query string, the browser's user agent
# and the referrer appear in neither the web server's access log nor the api's log; the web server
# gives the request its own id (whatever the client sent), and that one id is in both logs and in
# the response. Also that PostgreSQL logs statements without their bound values.
#   COMPOSE="docker compose --env-file .env -f deploy/compose/compose.yaml" tools/check-access-log.sh
# Needs curl and jq. BASE_URL defaults to http://localhost:8000.
set -euo pipefail
cd "$(dirname "$0")/.."
read -r -a compose <<<"${COMPOSE:-docker compose --env-file .env -f deploy/compose/compose.yaml}"
base=${BASE_URL:-http://localhost:8000}
marker="MARKER$(date +%s)$$"
fail() { echo "check-access-log: $*" >&2; exit 1; }

id=$(curl -sS -o /dev/null -D - -A "agent-$marker" -e "$base/?ref=$marker" -H "X-Request-ID: client-$marker" \
  "$base/api/v1/meta?q=$marker" | awk 'tolower($1) == "x-request-id:" { print $2 }' | tr -d '\r')
[[ $id =~ ^[0-9a-f]{32}$ ]] || fail "the response's X-Request-ID is '$id', not the web server's own id"
sleep 2 # the logs are written after the response
web=$("${compose[@]}" logs --no-log-prefix --since 2m web 2>&1)
api=$("${compose[@]}" logs --no-log-prefix --since 2m api 2>&1)
if grep -n "$marker" <<<"$web"; then fail "the web server's log carries the query string, the user agent, the referrer or the client's request id"; fi
if grep -n "$marker" <<<"$api"; then fail "the api's log carries the query string, the user agent or the client's request id"; fi
line=$(grep -F "\"request_id\":\"$id\"" <<<"$web" || true)
[ -n "$line" ] || fail "no access log line with request id $id"
jq -e '.uri == "/api/v1/meta" and .status == 200 and .method == "GET" and (.remote_addr | length > 0) and (has("args") | not)' <<<"$line" >/dev/null ||
  fail "unexpected access log line: $line"
grep -qF "$id" <<<"$api" || fail "the api's log does not name request id $id: the two hops can't be joined"

for setting in log_parameter_max_length log_parameter_max_length_on_error; do
  v=$("${compose[@]}" exec -T db psql -U "${POSTGRES_USER:-postgres}" -d finuance -XAtqc "SHOW $setting")
  [ "$v" = 0 ] || fail "PostgreSQL's $setting is '$v', not 0: a logged statement would carry its values"
done
echo "check-access-log: ok (request $id in both logs, no user text; PostgreSQL logs no bound values)"
