#!/usr/bin/env bash
# OpenVAS (Greenbone Community Edition) against a deployed Finuance instance (Phase 18.8;
# docs/security-scan.md "OpenVAS", docs/RUNBOOK.md "Security scans").
#
#   tools/openvas-scan.sh up                     download Greenbone's compose file (checked against
#                                                its SHA-256), start the stack with
#                                                tools/openvas/compose.override.yaml, set a random
#                                                admin password (kept in $OPENVAS_DIR)
#   tools/openvas-scan.sh scan <host> [ports]    wait for the feeds, scan <host> ("Full and fast",
#                                                TCP ports, default 80,443), write the report
#   tools/openvas-scan.sh lan-view <address>     every TCP port of <address> from Docker's bridge
#                                                network, as another device on that network sees it
#   tools/openvas-scan.sh down [--volumes]       stop the stack (--volumes also drops the feeds, ~10 GB)
#
# The scanner runs on the host's network, so <host> is what the host reaches: the instance on the
# tailnet (THREAT-MODEL T7 keeps it closed to the LAN, which lan-view checks). Reports go to
# $OPENVAS_DIR/reports/ (XML and a CSV of the results, plus a summary on stdout); they name the
# host, so they stay out of git. OPENVAS_DIR defaults to ~/.local/state/finuance-openvas.
set -euo pipefail
cd "$(dirname "$0")/.."

COMPOSE_URL=https://greenbone.github.io/docs/latest/_static/compose.yaml
COMPOSE_SHA256=5b5117a7c200491de13bc7b79c97dc858c016c51017982d8f1de7ca077c1a868
NMAP_IMAGE=instrumentisto/nmap:7.95@sha256:3cca6ece8de5a571c956022ec6c2cf343da8c4416fa36e1891e8c33623cfc845
# Greenbone's fixed ids: the "Full and fast" scan config, the OpenVAS scanner, the XML and CSV
# (results) report formats.
FULL_AND_FAST=daba56c8-73ec-11df-a475-002264764cea
OPENVAS_SCANNER=08b69003-5fc2-4037-a479-93b440211c73
XML_FORMAT=a994b278-1f62-11e1-96ac-406186ea4fc5
CSV_FORMAT=c1645568-627a-11e3-a660-406186ea4fc5

dir=${OPENVAS_DIR:-$HOME/.local/state/finuance-openvas}
mkdir -p "$dir/reports" && chmod 700 "$dir"
compose=(docker compose -p finuance-openvas -f "$dir/compose.yaml" -f tools/openvas/compose.override.yaml)

usage() { sed -n '2,19p' "$0" | sed 's/^# \{0,1\}//'; exit 2; }

fetch_compose() {
  if ! echo "$COMPOSE_SHA256  $dir/compose.yaml" | sha256sum -c --status 2>/dev/null; then
    curl -fsSL -o "$dir/compose.yaml.new" "$COMPOSE_URL"
    echo "$COMPOSE_SHA256  $dir/compose.yaml.new" | sha256sum -c --status || {
      echo "openvas-scan: Greenbone's compose.yaml changed upstream; review it, then update COMPOSE_SHA256 and the override" >&2
      exit 1
    }
    mv "$dir/compose.yaml.new" "$dir/compose.yaml"
  fi
}

# gmp <xml>: one GMP request through gvmd's socket; the password reaches gvm-cli in a config file.
gmp() {
  "${compose[@]}" run --rm -T --user gvm --entrypoint gvm-cli -v "$dir/gvm-tools.conf:/home/gvm/gvm-tools.conf:ro" gvm-tools \
    --config /home/gvm/gvm-tools.conf socket --socketpath /run/gvmd/gvmd.sock --xml "$1" 2>/dev/null
}
attr() { grep -oE "$1=\"[^\"]+\"" | head -1 | cut -d'"' -f2; }
tag() { grep -oE "<$1>[^<]*</$1>" | head -1 | sed -E "s#</?$1>##g"; }

case ${1:-} in
  up)
    fetch_compose
    "${compose[@]}" up -d
    if [ ! -s "$dir/admin-password" ]; then
      (umask 077; LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom | head -c 32 >"$dir/admin-password")
      until "${compose[@]}" exec -T -u gvmd gvmd true 2>/dev/null; do sleep 5; done
      "${compose[@]}" exec -T -u gvmd gvmd sh -c 'read -r p; gvmd --user=admin --new-password="$p"' <"$dir/admin-password"
    fi
    (umask 077; printf '[gmp]\nusername=admin\npassword=%s\n' "$(cat "$dir/admin-password")" >"$dir/gvm-tools.conf")
    chmod 644 "$dir/gvm-tools.conf" # read by the container's gvm user; the folder itself is 700
    echo "openvas-scan: up; the feeds load for 30-60 minutes on the first start (web UI: http://127.0.0.1:9392, user admin)"
    ;;
  scan)
    [ $# -ge 2 ] || usage
    host=$2 ports=${3:-80,443}
    [ -s "$dir/gvm-tools.conf" ] || { echo "openvas-scan: run 'up' first" >&2; exit 1; }
    echo "openvas-scan: waiting for the feeds (scan config and NVTs loaded)"
    until n=$(gmp "<get_configs config_id=\"$FULL_AND_FAST\" details=\"1\"/>" | grep -oE '<nvt_count>[0-9]+' | head -1 | tr -dc 0-9) && [ "${n:-0}" -gt 50000 ]; do
      sleep 60
    done
    echo "openvas-scan: feeds ready ($n NVTs in Full and fast)"
    stamp=$(date -u +%Y%m%dT%H%M%SZ)
    pl=$(gmp "<create_port_list><name>finuance-$stamp</name><port_range>T:$ports</port_range></create_port_list>" | attr id)
    tg=$(gmp "<create_target><name>finuance-$stamp</name><hosts>$host</hosts><port_list id=\"$pl\"/><alive_tests>Consider Alive</alive_tests></create_target>" | attr id)
    task=$(gmp "<create_task><name>finuance-$stamp</name><target id=\"$tg\"/><config id=\"$FULL_AND_FAST\"/><scanner id=\"$OPENVAS_SCANNER\"/></create_task>" | attr id)
    report=$(gmp "<start_task task_id=\"$task\"/>" | tag report_id)
    [ -n "$report" ] || { echo "openvas-scan: the task didn't start" >&2; exit 1; }
    echo "openvas-scan: scanning $host (ports $ports), task $task"
    start=$(date +%s)
    while :; do
      t=$(gmp "<get_tasks task_id=\"$task\"/>")
      status=$(tag status <<<"$t") progress=$(tag progress <<<"$t")
      case $status in Done) break ;; Stopped|Interrupted) echo "openvas-scan: scan $status" >&2; exit 1 ;; esac
      echo "  $status ${progress:-0} % ($(( ($(date +%s) - start) / 60 )) min)"
      sleep 60
    done
    out="$dir/reports/finuance-$stamp"
    gmp "<get_reports report_id=\"$report\" format_id=\"$XML_FORMAT\" details=\"1\" ignore_pagination=\"1\" filter=\"apply_overrides=0 min_qod=70 levels=chmlg rows=-1\"/>" >"$out.xml"
    gmp "<get_reports report_id=\"$report\" format_id=\"$CSV_FORMAT\" details=\"1\" ignore_pagination=\"1\" filter=\"apply_overrides=0 min_qod=70 levels=chmlg rows=-1\"/>" \
      | grep -oE '</report_format>[A-Za-z0-9+/=]+' | sed 's#</report_format>##' | base64 -d >"$out.csv" || true
    echo "openvas-scan: done in $(( ($(date +%s) - start) / 60 )) min; report $out.xml / .csv"
    # summary: severity, port, name of every result (QoD ≥ 70, like Greenbone's default view)
    python3 -I - "$out.xml" <<'PY'
import sys, xml.etree.ElementTree as ET
root = ET.parse(sys.argv[1]).getroot()
rows = []
for r in root.iter("result"):
    if not (r.findtext("name") or "").strip():
        continue  # CPE inventory entries ("TLS is present"), not findings
    sev = float(r.findtext("severity") or 0)
    rows.append((sev, r.findtext("threat") or "", r.findtext("port") or "", r.findtext("name") or ""))
rows.sort(reverse=True)
counts = {}
for sev, threat, *_ in rows: counts[threat] = counts.get(threat, 0) + 1
print("results:", ", ".join(f"{k} {v}" for k, v in sorted(counts.items())) or "none")
for sev, threat, port, name in rows: print(f"  {sev:4.1f} {threat:<6} {port:<12} {name}")
PY
    ;;
  lan-view)
    [ $# -eq 2 ] || usage
    out="$dir/reports/lan-view-$(date -u +%Y%m%dT%H%M%SZ).txt"
    docker run --rm "$NMAP_IMAGE" -Pn -sT -p- --open -T4 "$2" | tee "$out"
    ;;
  down)
    "${compose[@]}" down ${2:+"$2"}
    ;;
  *) usage ;;
esac
