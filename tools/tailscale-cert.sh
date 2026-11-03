#!/usr/bin/env bash
# Puts the host's Tailscale certificate into the cluster as the ingress's TLS secret
# (docs/RUNBOOK.md "Bootstrap on single-node k3s", ADR 0022: chart TLS mode `secret`).
#
#   tools/tailscale-cert.sh [host]    default host: this machine's tailnet name
#
# `tailscale cert` returns the cached certificate while it is fresh and renews it when about a third
# of its 90 days is left, so this runs from a weekly timer (host/finuance-tls@.service/.timer).
# The key only exists in a private temporary folder (tmpfs when available) and in the cluster
# secret; it is never written to the repository. Needs tailscale (this user as its operator:
# `tailscale up --operator=$USER`), kubectl with access to the cluster, and jq.
#
# Note: the first certificate for a name puts that name into the public Certificate Transparency
# logs for good.
#
# Settings: NAMESPACE (default finuance), SECRET (default finuance-tls).
set -euo pipefail

ns=${NAMESPACE:-finuance}
secret=${SECRET:-finuance-tls}
host=${1:-$(tailscale status --json | jq -r '.Self.DNSName' | sed 's/\.$//')}
[[ $host == *.ts.net ]] || { echo "tailscale-cert: '$host' is not a *.ts.net name" >&2; exit 1; }

tmp=$(mktemp -d "${XDG_RUNTIME_DIR:-/tmp}/finuance-tls.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
chmod 700 "$tmp"

tailscale cert --cert-file "$tmp/tls.crt" --key-file "$tmp/tls.key" "$host" >/dev/null

# The namespace normally comes from Argo CD; create it the same way (restricted Pod Security
# Standard) when the certificate is put in first.
if ! kubectl get namespace "$ns" >/dev/null 2>&1; then
  kubectl create namespace "$ns" >/dev/null
  kubectl label namespace "$ns" pod-security.kubernetes.io/enforce=restricted \
    pod-security.kubernetes.io/enforce-version=latest >/dev/null
fi

kubectl -n "$ns" create secret tls "$secret" --cert "$tmp/tls.crt" --key "$tmp/tls.key" \
  --dry-run=client -o yaml \
  | kubectl label --local -f - app.kubernetes.io/part-of=finuance app.kubernetes.io/managed-by=tailscale-cert -o yaml \
  | kubectl apply -f - >/dev/null

until=$(openssl x509 -in "$tmp/tls.crt" -noout -enddate | cut -d= -f2)
echo "tailscale-cert: $ns/$secret holds the certificate for $host (valid until $until)"
