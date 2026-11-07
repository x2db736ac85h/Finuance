#!/usr/bin/env bash
# make observe: port-forwards to the cluster's admin tools on this machine's loopback only, with the
# local ports of docs/RUNBOOK.md "Local ports": Grafana, Prometheus, Alertmanager and Argo CD.
#
#   tools/observe.sh [grafana|prometheus|alertmanager|argocd …]   (default: all four)
#
# Before starting, every local port is checked on both loopback addresses: `kubectl port-forward`
# would otherwise keep going on whichever one is free, and the URL would reach another program. A
# forward that stops (its pod restarted, the cluster went away) starts again after a pause that grows
# to 30 s. Ctrl-C stops them all. Only 127.0.0.1 and [::1] are ever bound, and passwords are never
# printed: the sign-in column says where to find them.
#
# Tests (tools/observe-test.sh) replace the table with OBSERVE_TABLE and the pause with OBSERVE_RETRY.
set -euo pipefail

# name|namespace|target|local port|remote port|url|sign-in
DEFAULT_TABLE='grafana|monitoring|svc/kube-prometheus-stack-grafana|3000|80|http://localhost:3000|user admin; password: kubectl -n monitoring get secret grafana-admin -o jsonpath='"'"'{.data.admin-password}'"'"' | base64 -d
prometheus|monitoring|svc/kube-prometheus-stack-prometheus|19090|9090|http://localhost:19090|no sign-in
alertmanager|monitoring|svc/kube-prometheus-stack-alertmanager|9093|9093|http://localhost:9093|no sign-in
argocd|argocd|svc/argocd-server|18443|443|https://localhost:18443|user admin, the password from the bootstrap (self-signed certificate)'
TABLE=${OBSERVE_TABLE:-$DEFAULT_TABLE}
RETRY=${OBSERVE_RETRY:-2}

command -v kubectl >/dev/null || { echo "observe: kubectl is not installed" >&2; exit 1; }

# in_use <port>: something on this machine answers on the port at a loopback address (a socket bound
# to every address answers there too)
in_use() {
  local p=$1 host
  for host in 127.0.0.1 ::1; do
    if (exec 3<>"/dev/tcp/$host/$p") 2>/dev/null; then return 0; fi
  done
  return 1
}

# the tools asked for (all when none is named); a string, so macOS's bash 3.2 copes under set -u
wanted=" $* "
selected=""
while IFS='|' read -r name ns target lport rport url signin; do
  [ -n "$name" ] || continue
  if [ "$wanted" != "  " ] && [[ "$wanted" != *" $name "* ]]; then continue; fi
  selected+="$name|$ns|$target|$lport|$rport|$url|$signin"$'\n'
done <<<"$TABLE"
[ -n "$selected" ] || { echo "observe: nothing to forward (choose from: grafana prometheus alertmanager argocd)" >&2; exit 2; }

problems=0
while IFS='|' read -r name ns target lport rport url signin; do
  [ -n "$name" ] || continue
  if in_use "$lport"; then
    echo "observe: port $lport ($name) is already in use on this machine. Stop what holds it (ss -ltnp 'sport = :$lport'; make up-observability holds 3000), or leave $name out." >&2
    problems=1
  elif ! kubectl -n "$ns" get "$target" --request-timeout=10s >/dev/null 2>&1; then
    echo "observe: $ns/$target isn't reachable (no cluster access, or not installed here)" >&2
    problems=1
  fi
done <<<"$selected"
[ "$problems" -eq 0 ] || exit 1

# forward <name> <namespace> <target> <local> <remote>: one port-forward, started again when it stops
forward() {
  local name=$1 ns=$2 target=$3 lport=$4 rport=$5 pause=$RETRY start
  while true; do
    start=$SECONDS
    kubectl -n "$ns" port-forward --address 127.0.0.1,::1 "$target" "$lport:$rport" >/dev/null 2>&1 || true
    # a forward that ran for a while starts the pauses from the beginning
    if [ $((SECONDS - start)) -ge 60 ]; then pause=$RETRY; fi
    echo "observe: $name stopped; starting it again in ${pause}s" >&2
    sleep "$pause"
    pause=$(awk -v p="$pause" 'BEGIN { p *= 2; print (p > 30 ? 30 : p) }')
  done
}

# stop: the forward loops and their kubectl processes, nothing else (make shares our process group)
pids=""
stop() {
  trap - EXIT INT TERM
  local pid kids
  for pid in $pids; do
    kids=$(pgrep -P "$pid" || true)
    kill "$pid" 2>/dev/null || true
    # shellcheck disable=SC2086 # one pid per word
    [ -z "$kids" ] || kill $kids 2>/dev/null || true
  done
}
trap 'stop; exit 0' INT TERM
trap stop EXIT

printf '%-13s %-26s %s\n' TOOL OPEN SIGN-IN
while IFS='|' read -r name ns target lport rport url signin; do
  [ -n "$name" ] || continue
  forward "$name" "$ns" "$target" "$lport" "$rport" &
  pids+=" $!"
  printf '%-13s %-26s %s\n' "$name" "$url" "$signin"
done <<<"$selected"
echo "observe: forwarding on 127.0.0.1 and [::1] only; Ctrl-C stops them all (docs/RUNBOOK.md \"Local ports\")"
wait
