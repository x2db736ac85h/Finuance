#!/usr/bin/env bash
# Checks the running lab (make sim-up; PLAN 25.8, ADR 0043) from the outside:
#   1. animals act through the simulator's control API ("act now"): imports, a question to the
#      assistant, a deliberate mistake, the overview; each answers ok;
#   2. Prometheus counts synthetic requests on the lab and scrapes the simulator's own metrics;
#   3. Loki holds the api's line of the last action, found by the request id the feed shows,
#      and the line says traffic=synthetic;
#   4. what the load costs (ADR 0044): Alloy's cAdvisor and PostgreSQL figures in Prometheus, the
#      simulator's live strip, and the console's link in the lab administrator's Developer section.
# Needs curl and jq; reads the control token and the administrator's password from .env.lab and
# never prints them.
set -euo pipefail
cd "$(dirname "$0")/.."
set -a
# shellcheck disable=SC1091 # written by make sim-up
. ./.env.lab
set +a
sim=http://localhost:${FINUANCE_SIM_PORT:-8090}
prom=${PROMETHEUS_URL:-http://localhost:9095}
grafana=${GRAFANA_URL:-http://localhost:3000}
fail() { echo "check-lab: $*" >&2; exit 1; }
call() {
  curl -fsS -H "Authorization: Bearer $FINUANCE_SIM_CONTROL_TOKEN" -H "X-Finuance-Client: web" -H 'Content-Type: application/json' "$@"
}

echo "== the animals act now"
ev=
for pair in otter:imports.month owl:imports.month fox:imports.month fox:assistant.ask sloth:account.unknown-path otter:insights.overview; do
  ev=$(call -X POST "$sim/api/act" -d "$(jq -cn --arg p "${pair%%:*}" --arg a "${pair#*:}" '{persona: $p, action: $a}')")
  [ "$(jq -r .result <<<"$ev")" = ok ] || fail "$pair: $ev"
  echo "   $pair: ok ($(jq -r '.requestId // "-"' <<<"$ev"), $(jq -r .durationMs <<<"$ev") ms)"
done
id=$(jq -r .requestId <<<"$ev")
[ -n "$id" ] && [ "$id" != null ] || fail "the last action carries no request id: $ev"

echo "== Prometheus"
prom_has() {
  local v
  for _ in $(seq 30); do
    v=$(curl -fsS "$prom/api/v1/query" --data-urlencode "query=$1" | jq -r '.data.result[0].value[1] // "0"')
    [ "${v%.*}" -gt 0 ] && { echo "   $1 = $v"; return 0; }
    sleep 3
  done
  return 1
}
prom_has 'sum(http_server_requests_total{traffic="synthetic"})' || fail "Prometheus counts no synthetic request"
prom_has 'sum(finuance_sim_actions_total{result="ok"})' || fail "Prometheus has no finuance_sim_actions_total (is the simulator scraped?)"

echo "== Loki: the api's line of request $id"
loki() {
  curl -fsS -X POST "$grafana/api/ds/query" -H 'Content-Type: application/json' \
    -d "$(jq -cn --arg e "$1" '{queries: [{refId: "A", datasource: {type: "loki", uid: "loki"}, expr: $e, queryType: "range", maxLines: 5}], from: "now-30m", to: "now"}')" |
    jq -r '.results.A.frames[0].data.values[2][]?'
}
line=
for _ in $(seq 30); do
  line=$(loki "{service_name=\"api\"} |= \"$id\"" | head -1)
  [ -n "$line" ] && break
  sleep 3
done
[ -n "$line" ] || fail "Loki has no api line with request id $id"
grep -qE 'traffic=synthetic|"traffic":"synthetic"' <<<"$line" || fail "the api's line is not labelled synthetic: $line"

echo "== what the load costs"
prom_has 'count(container_memory_working_set_bytes{namespace="finuance",container="api"})' ||
  fail "Prometheus has no container figures (Alloy's cAdvisor: the host's cgroups, containerd's socket)"
prom_has 'sum(pg_up{job="finuance-db"})' || fail "Prometheus has no PostgreSQL figures (Alloy's PostgreSQL exporter)"
res=
for _ in $(seq 30); do
  res=$(call "$sim/api/resources")
  [ "$(jq '.available and (.containers | map(.name) | index("api")) != null' <<<"$res")" = true ] && break
  sleep 3
done
[ "$(jq .available <<<"$res")" = true ] || fail "the simulator's live strip has no figures: $(jq -c '{available, error}' <<<"$res")"
echo "   live strip: $(jq -r '[.containers[].name] | join(", ")' <<<"$res")"
web=http://localhost:${FINUANCE_LAB_PORT:-8020}
jar=$(mktemp)
trap 'rm -f "$jar"' EXIT
jq -cn '{email: "admin@lab.invalid", password: env.FINUANCE_LAB_ADMIN_PASSWORD}' | # not on a command line
  curl -fsS -c "$jar" -H 'X-Finuance-Client: web' -H "Origin: $web" -H 'Content-Type: application/json' \
    -X POST "$web/api/v1/auth/login" -d @- -o /dev/null || fail "the lab administrator can't sign in"
curl -fsS -b "$jar" -H 'X-Finuance-Client: web' -H "Origin: $web" "$web/api/v1/admin/developer" |
  jq -e '.links | map(.name) | index("Synthetic users console") != null' >/dev/null ||
  fail "the Developer section has no link to the synthetic users console"
echo "   Settings → Developer links the synthetic users console"
echo "check-lab: ok"
