#!/usr/bin/env bash
# Keeps the local ports of the admin tools' port-forwards (Grafana, Prometheus, Alertmanager, Argo
# CD; docs/RUNBOOK.md "Local ports") the same everywhere they are written: tools/observe.sh, the
# RUNBOOK, the homelab Developer links and the example links file. Each link opens the port its
# command forwards, no two tools share a port, and none takes a port the Compose stack publishes on
# this machine (Grafana's 3000 is the documented exception: `make up-observability` uses it too, run
# one at a time). Found in Phase 20: Prometheus's 9090 was the Compose api's admin port.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

OBSERVE=tools/observe.sh
RUNBOOK=docs/RUNBOOK.md
LINKS=(deploy/environments/homelab/values.yaml deploy/developer/links.example.json)
ALLOWED_SHARED="3000" # Grafana, shared with make up-observability (one at a time)

problems=()

# target → local port, from observe.sh's table (name|namespace|target|local|remote|url|sign-in)
declare -A port_of=()
while IFS='|' read -r _ _ target lport _; do
  port_of[$target]=$lport
done < <(grep -oE '[a-z]+\|[a-z-]+\|svc/[a-z0-9-]+\|[0-9]+\|[0-9]+' "$OBSERVE")
[ ${#port_of[@]} -eq 4 ] || problems+=("$OBSERVE: expected 4 tools in its table, found ${#port_of[@]}")

# every port-forward of those targets uses that local port
for f in "$RUNBOOK" "${LINKS[@]}"; do
  while read -r target lport; do
    [ -n "${port_of[$target]:-}" ] || continue
    [ "$lport" = "${port_of[$target]}" ] ||
      problems+=("$f: $target is forwarded to local port $lport, $OBSERVE uses ${port_of[$target]}")
  done < <(grep -oE 'port-forward svc/[a-z0-9-]+ [0-9]+:[0-9]+' "$f" | sed -E 's/port-forward (svc\/[a-z0-9-]+) ([0-9]+):[0-9]+/\1 \2/')
done

# in the links files, a link opens the port its command forwards
for f in "${LINKS[@]}"; do
  while IFS= read -r line; do problems+=("$f: $line"); done < <(awk '
    match($0, /"?url"?: *"?https?:\/\/localhost:[0-9]+/) {
      u = substr($0, RSTART, RLENGTH); sub(/.*:/, "", u); url = u; next
    }
    /port-forward svc\// {
      match($0, /[0-9]+:[0-9]+/); lp = substr($0, RSTART, RLENGTH); sub(/:.*/, "", lp)
      if (url != "" && lp != url) printf "a link opens localhost:%s but its command forwards local port %s\n", url, lp
    }' "$f")
done

# no two tools on one port, and none on a port the Compose stack publishes
declare -A owner=()
compose_ports=$(grep -ohE '"127\.0\.0\.1:[0-9]+:' deploy/compose/*.yaml | grep -oE ':[0-9]+:' | tr -d ':' | sort -u)
for target in "${!port_of[@]}"; do
  p=${port_of[$target]}
  [ -z "${owner[$p]:-}" ] || problems+=("$target and ${owner[$p]} both use local port $p")
  owner[$p]=$target
  if grep -qx "$p" <<<"$compose_ports" && [[ " $ALLOWED_SHARED " != *" $p "* ]]; then
    problems+=("$target uses local port $p, which the Compose stack publishes (deploy/compose)")
  fi
done

if [ ${#problems[@]} -gt 0 ]; then
  printf 'check-port-forwards: %s\n' "${problems[@]}" >&2
  exit 1
fi
echo "check-port-forwards: ok (${#port_of[@]} tools: $(for t in "${!port_of[@]}"; do printf '%s ' "${port_of[$t]}"; done | xargs))"
