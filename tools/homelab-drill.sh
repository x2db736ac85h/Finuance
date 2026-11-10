#!/usr/bin/env bash
# Drills on a real cluster with sign-up off (Phase 18.4; docs/RUNBOOK.md "Restore drill" and
# "Pod kills under load"), the homelab counterpart of tools/rehearsal.sh's restore and podkill:
#
#   tools/homelab-drill.sh restore    backup → marker users → restore into finuance-restore →
#                                     markers, LEAKPROOF, sign-in, smoke scripts (RPO, RTO)
#   tools/homelab-drill.sh podkill    k6 through the ingress while api/web pods are deleted and
#                                     api/web/worker are rolled: no request may fail
#   tools/homelab-drill.sh sse        one assistant question through the ingress (a drill user with the
#                                     demo year): when each streamed event arrives, so a buffering
#                                     proxy would show (everything at the end)
#   tools/homelab-drill.sh all
#
# Every user it needs is made with `finuance admin create-user` (random passwords, kept in this
# process only) and deleted again at the end, also after a failure; the restore namespace is deleted
# unless KEEP_RESTORE=1. BASE_URL (default: https://<this machine's tailnet name>). Needs kubectl,
# helm, curl, jq, yq (mikefarah), openssl, docker (k6) and kubeseal (tools/seal-secrets.sh).
set -euo pipefail
cd "$(dirname "$0")/.."

H=deploy/environments/homelab
BASE=${BASE_URL:-https://$(tailscale status --json | jq -r '.Self.DNSName' | sed 's/\.$//')}
TAG=$(yq '.image.tag' "$H/values.yaml")
K6_IMAGE=grafana/k6:1.8.2
RNS=finuance-restore
RAPI=http://localhost:18081       # port-forward to the restored api
RORIGIN=http://localhost:18080    # the temporary app's public origin (restore-app-values.yaml)

work=$(mktemp -d "${XDG_RUNTIME_DIR:-/tmp}/homelab-drill.XXXXXX")
chmod 700 "$work"
results="$work/results.tsv"
: >"$results"
users=()   # "email password origin base" of every user to delete at the end
pids=()    # port-forwards

log() { printf '\n== %s\n' "$*"; }
result() { printf '%s\t%s\n' "$1" "$2" >>"$results"; echo "   $1: $2"; }
wait_for() { # wait_for <seconds> <what> <command…>
  local limit=$1 what=$2; shift 2
  local end=$(($(date +%s) + limit))
  until "$@" >/dev/null 2>&1; do
    [ "$(date +%s)" -lt "$end" ] || { echo "timed out after ${limit}s waiting for $what" >&2; return 1; }
    sleep 2
  done
}
psql_in() { # psql_in <namespace> <sql>: on the cluster's primary, as postgres over the pod socket
  local primary
  primary=$(kubectl -n "$1" get cluster finuance-db -o jsonpath='{.status.currentPrimary}')
  kubectl -n "$1" exec "$primary" -c postgres -- psql -U postgres -d finuance -XAtqc "$2"
}
password() { openssl rand -base64 24; }
mkuser() { # mkuser <namespace> <email> <password>
  printf '%s' "$3" | kubectl -n "$1" exec -i deploy/finuance-api -- \
    /finuance admin create-user -email="$2" -password-stdin >/dev/null
}
signin() { # signin <base> <origin> <email> <password> <cookie jar>: prints the status
  curl -sS -o /dev/null -w '%{http_code}' -c "$5" -H "X-Finuance-Client: web" -H "Origin: $2" \
    -H "Content-Type: application/json" --data "$(jq -cn --arg e "$3" --arg p "$4" '{email: $e, password: $p}')" \
    "$1/api/v1/auth/login"
}
deluser() { # deluser <email> <password> <origin> <base>
  local jar="$work/del-jar"
  [ "$(signin "$4" "$3" "$1" "$2" "$jar")" = 200 ] || { echo "   could not sign in as $1 to delete it" >&2; return 1; }
  curl -sS -o /dev/null -b "$jar" -X DELETE -H "X-Finuance-Client: web" -H "Origin: $3" \
    -H "Content-Type: application/json" --data "$(jq -cn --arg p "$2" '{password: $p}')" "$4/api/v1/me"
  echo "   deleted $1"
}
cleanup() {
  local status=$?
  for p in "${pids[@]}"; do kill "$p" 2>/dev/null || true; done
  if [ ${#users[@]} -gt 0 ]; then
    log "clean-up: drill users"
    for u in "${users[@]}"; do
      # shellcheck disable=SC2086
      set -- $u
      [ "$3" = "$BASE" ] || continue   # the restore namespace's users go with the namespace
      deluser "$1" "$2" "$3" "$4" || status=1
    done
  fi
  if kubectl get namespace "$RNS" >/dev/null 2>&1 && [ "${KEEP_RESTORE:-}" != 1 ]; then
    log "clean-up: $RNS"
    helm uninstall finuance -n "$RNS" >/dev/null 2>&1 || true
    helm uninstall finuance-db -n "$RNS" >/dev/null 2>&1 || true
    kubectl delete namespace "$RNS" --wait=true --timeout=300s >/dev/null && echo "   deleted (with its volume)"
  fi
  if [ -s "$results" ]; then log "results"; column -t -s $'\t' "$results"; fi
  rm -rf "$work"
  exit "$status"
}
trap cleanup EXIT

restore() {
  log "restore drill: backup → markers → restore into $RNS → app → smoke"
  local a b pa pb t0 tb
  a="drill-a-$(date +%s)@example.test" b="drill-b-$(date +%s)@example.test"
  pa=$(password) pb=$(password)

  # marker A exists before the base backup
  mkuser finuance "$a" "$pa"; users+=("$a $pa $BASE $BASE")
  t0=$(date +%s)
  kubectl -n finuance apply -f - >/dev/null <<EOF
apiVersion: postgresql.cnpg.io/v1
kind: Backup
metadata:
  name: drill-$t0
spec:
  cluster: { name: finuance-db }
  method: plugin
  pluginConfiguration: { name: barman-cloud.cloudnative-pg.io }
EOF
  wait_for 900 "the on-demand backup" \
    sh -c "[ \"\$(kubectl -n finuance get backups.postgresql.cnpg.io drill-$t0 -o jsonpath='{.status.phase}')\" = completed ]"
  result "on-demand base backup" "$(($(date +%s) - t0)) s"

  # marker B only exists after the backup: it comes back through the archived WAL
  mkuser finuance "$b" "$pb"; users+=("$b $pb $BASE $BASE")
  tb=$(date +%s)
  wait_for 420 "the WAL with marker B archived" \
    sh -c "[ \"\$(kubectl -n finuance exec \$(kubectl -n finuance get cluster finuance-db -o jsonpath='{.status.currentPrimary}') -c postgres -- psql -U postgres -d finuance -XAtqc \"SELECT coalesce(extract(epoch FROM last_archived_time)::bigint, 0) FROM pg_stat_archiver\")\" -ge $tb ]"
  result "RPO (a write until its WAL is archived; archive_timeout 5 min)" "$(($(date +%s) - tb)) s"

  # the scratch namespace: the bucket's keys and the registry login copied from finuance (they are
  # read, never shown), fresh random role passwords sealed for this namespace
  t0=$(date +%s)
  kubectl create namespace "$RNS" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  kubectl label namespace "$RNS" --overwrite pod-security.kubernetes.io/enforce=restricted \
    pod-security.kubernetes.io/enforce-version=latest >/dev/null
  for s in finuance-db-backup ghcr-pull; do
    kubectl -n finuance get secret "$s" -o json \
      | jq --arg ns "$RNS" '{apiVersion, kind, type, data, metadata: {name: .metadata.name, namespace: $ns, labels: .metadata.labels}}' \
      | kubectl apply -f - >/dev/null
  done
  mkdir -p "$work/sealed"
  NAMESPACE=$RNS SECRETS_DIR="$work/sealed" tools/seal-secrets.sh homelab db >/dev/null
  kubectl apply -n "$RNS" -f "$work/sealed" >/dev/null
  for s in finuance-db-owner finuance-db-app; do
    wait_for 120 "secret $RNS/$s unsealed" kubectl -n "$RNS" get secret "$s"
  done

  helm upgrade --install finuance-db deploy/k8s/db -n "$RNS" -f "$H/restore-db-values.yaml" >/dev/null
  wait_for 900 "the restored cluster" kubectl -n "$RNS" wait cluster/finuance-db --for=condition=Ready --timeout=10s
  result "restored cluster ready" "$(($(date +%s) - t0)) s"
  local found
  found=$(psql_in "$RNS" "SELECT count(*) FROM users WHERE email IN ('$a', '$b')")
  [ "$found" = 2 ] || { echo "the restored database has $found of the 2 markers" >&2; return 1; }
  [ "$(psql_in "$RNS" "SELECT proleakproof FROM pg_proc WHERE proname = 'textlike'")" = t ] \
    || { echo "textlike lost LEAKPROOF in the restore" >&2; return 1; }
  result "restored data" "both markers (before the backup, and only in the WAL after it); textlike LEAKPROOF"

  helm upgrade --install finuance deploy/helm/finuance -n "$RNS" -f "$H/restore-app-values.yaml" \
    --set image.tag="$TAG" --wait --timeout 10m >/dev/null
  kubectl -n "$RNS" port-forward svc/finuance-api 18081:8080 >/dev/null 2>&1 &
  pids+=($!)
  wait_for 60 "the restored api" curl -fsS "$RAPI/readyz"
  [ "$(signin "$RAPI" "$RORIGIN" "$b" "$pb" "$work/b-jar")" = 200 ] \
    || { echo "marker B can't sign in on the restored database" >&2; return 1; }
  BASE_URL=$RAPI ORIGIN=$RORIGIN NAMESPACE=$RNS METRICS_PORT=19092 scripts/smoke-k8s.sh | grep -E 'passed|FAIL'
  result "RTO (restore started until the smoke scripts pass on it)" "$(($(date +%s) - t0)) s"
}

podkill() {
  log "pod kills under load: k6 through the ingress while api, web and worker pods go"
  local email pass
  email="drill-k6-$(date +%s)@example.test" pass=$(password)
  mkuser finuance "$email" "$pass"; users+=("$email $pass $BASE $BASE")
  mkdir -p "$work/k6" && chmod 777 "$work/k6"
  # the password reaches k6 through the environment (-e NAME without a value), not the command line
  K6_EMAIL=$email K6_PASSWORD=$pass docker run --rm --network host \
    -v "$PWD/deploy/k8s/rehearsal:/scripts:ro" -v "$work/k6:/out" \
    -e BASE_URL="$BASE" -e DURATION=90s -e K6_EMAIL -e K6_PASSWORD \
    "$K6_IMAGE" run --quiet --summary-export=/out/summary.json /scripts/podkill.js >"$work/k6/out.txt" 2>&1 &
  local k6=$! pod
  sleep 15
  pod=$(kubectl -n finuance get pods -l app.kubernetes.io/component=api -o jsonpath='{.items[0].metadata.name}')
  echo "   delete $pod"; kubectl -n finuance delete pod "$pod" --wait=false >/dev/null
  sleep 10
  echo "   roll the api"; kubectl -n finuance rollout restart deploy/finuance-api >/dev/null
  kubectl -n finuance rollout status deploy/finuance-api --timeout=300s >/dev/null
  pod=$(kubectl -n finuance get pods -l app.kubernetes.io/component=web -o jsonpath='{.items[0].metadata.name}')
  echo "   delete $pod"; kubectl -n finuance delete pod "$pod" --wait=false >/dev/null
  sleep 5
  echo "   roll web and the worker"; kubectl -n finuance rollout restart deploy/finuance-web deploy/finuance-worker >/dev/null
  kubectl -n finuance rollout status deploy/finuance-web --timeout=300s >/dev/null
  kubectl -n finuance rollout status deploy/finuance-worker --timeout=300s >/dev/null
  local status=0
  wait "$k6" || status=$?
  local line
  line=$(jq -r '.metrics as $m | ($m.http_reqs.count) as $n
    | "\($n) requests, \($m.http_req_failed.passes // (($m.http_req_failed.value * $n) | round)) failed, p95 \($m.http_req_duration["p(95)"] | round) ms, max \($m.http_req_duration.max | round) ms"' \
    "$work/k6/summary.json" 2>/dev/null || echo "no k6 summary")
  result "k6 during pod kills (1 api pod deleted, api/web/worker rolled; TLS verified)" "$line"
  [ $status -eq 0 ] || { echo "k6 reported failures:" >&2; tail -25 "$work/k6/out.txt" >&2; }
  return $status
}

sse() {
  log "assistant answer streamed through the ingress"
  local email pass jar="$work/sse-jar" h chat
  email="drill-sse-$(date +%s)@example.test" pass=$(password)
  mkuser finuance "$email" "$pass"; users+=("$email $pass $BASE $BASE")
  [ "$(signin "$BASE" "$BASE" "$email" "$pass" "$jar")" = 200 ] || { echo "the drill user can't sign in" >&2; return 1; }
  h=(-sS -b "$jar" -c "$jar" -H "X-Finuance-Client: web" -H "Origin: $BASE" -H "Content-Type: application/json")
  curl "${h[@]}" -o /dev/null -X POST "$BASE/api/v1/demo-data"
  chat=$(curl "${h[@]}" -X POST "$BASE/api/v1/assistant/chats" | jq -r .id)
  # each line of the stream with the seconds since the request started
  local t0 q=${SSE_QUESTION:-How much did I spend on groceries in February 2025?}
  t0=$(date +%s.%N)
  curl "${h[@]}" -N --max-time "${SSE_TIMEOUT:-600}" -X POST "$BASE/api/v1/assistant/chats/$chat/messages" \
    --data "$(jq -cn --arg q "$q" '{text: $q, timeZone: "Europe/Warsaw", settings: {connection: "ollama", think: "off"}}')" \
    | while IFS= read -r line; do printf '%s\t%s\n' "$(awk -v a="$(date +%s.%N)" -v b="$t0" 'BEGIN { printf "%.2f", a - b }')" "$line"; done >"$work/sse.tsv"
  local events first content last kinds
  events=$(grep -c $'\tevent:' "$work/sse.tsv" || true)
  first=$(grep -m1 $'\tevent:' "$work/sse.tsv" | cut -f1)
  content=$(grep -m1 $'\tevent: content' "$work/sse.tsv" | cut -f1)
  last=$(grep $'\tevent:' "$work/sse.tsv" | tail -1 | cut -f1)
  kinds=$(grep -o $'\tevent: [a-z]*' "$work/sse.tsv" | cut -d' ' -f2 | sort | uniq -c | awk '{printf "%s %s, ", $1, $2}')
  grep -q $'\tevent: done' "$work/sse.tsv" || { echo "the stream didn't end with done:" >&2; tail -5 "$work/sse.tsv" >&2; return 1; }
  result "SSE through the ingress" "$(printf '%s events (%s) first after %.1f s, first answer text after %.1f s, done after %.1f s' "$events" "${kinds%, }" "$first" "${content:-0}" "$last")"
  result "SSE answer (first 200 characters)" "$(grep -A1 $'\tevent: content' "$work/sse.tsv" | grep -o $'\tdata: .*' | cut -d' ' -f2- | jq -rj '.delta // empty' 2>/dev/null | tr '\n' ' ' | cut -c1-200)"
}

case ${1:-} in
  restore) restore ;;
  podkill) podkill ;;
  sse) sse ;;
  all) restore; podkill; sse ;;
  *) sed -n '2,18p' "$0" | sed 's/^# \{0,1\}//'; exit 2 ;;
esac
