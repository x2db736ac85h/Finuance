#!/usr/bin/env bash
# Makes the SealedSecrets of an environment (PLAN task 16.3, docs/RUNBOOK.md "Secrets") into
# deploy/environments/<env>/secrets/<name>.sealed.yaml. Plain values never touch the disk or a
# command line: each Secret is written to kubeseal's stdin, and only the encrypted result is saved.
#
#   tools/seal-secrets.sh <env> db                      random passwords for finuance-db-owner and finuance-db-app
#   tools/seal-secrets.sh <env> backup                  finuance-db-backup from $ACCESS_KEY_ID and $ACCESS_SECRET_KEY
#   tools/seal-secrets.sh <env> backup-store            a random key pair for the in-cluster RustFS, sealed twice:
#                                                       finuance-db-backup (finuance) and rustfs (s3, into <env>/rustfs/)
#   tools/seal-secrets.sh <env> assistant <file>        finuance-assistant from KEY=value lines (API keys)
#   tools/seal-secrets.sh <env> instance-key            finuance-instance-key: a random FINUANCE_INSTANCE_KEY, which
#                                                       seals API keys entered in Administration → Assistant
#   tools/seal-secrets.sh <env> pull                    ghcr-pull (image pull secret) from a token on stdin
#                                                       (read:packages; GHCR_USER, default x2db736ac85h)
#   tools/seal-secrets.sh <env> smtp                    finuance-alert-smtp (monitoring) from the alert mail
#                                                       account's app password on stdin
#   tools/seal-secrets.sh <env> grafana                 grafana-admin (monitoring): user admin, a random password
#
# kubeseal fetches the controller's public certificate from the cluster (kube-system /
# sealed-secrets-controller); set SEAL_CERT=<file> to seal offline with a saved certificate
# (kubeseal --fetch-cert > cert.pem). NAMESPACE (default finuance) is the namespace the secrets are
# bound to (strict scope: a sealed secret only decrypts under its own name and namespace).
# SECRETS_DIR overrides the output folder (the CI rehearsal seals into a scratch folder).
set -euo pipefail
cd "$(dirname "$0")/.."

usage() { sed -n '2,23p' "$0" | sed 's/^# \{0,1\}//'; exit 2; }
[ $# -ge 2 ] || usage
env=$1 what=$2
ns=${NAMESPACE:-finuance}
dir=${SECRETS_DIR:-deploy/environments/$env/secrets}
[ -d "$dir" ] || { echo "seal-secrets: $dir does not exist" >&2; exit 1; }
command -v kubeseal >/dev/null || { echo "seal-secrets: kubeseal is not installed (docs/RUNBOOK.md)" >&2; exit 1; }
seal_args=(--format yaml)
[ -n "${SEAL_CERT:-}" ] && seal_args+=(--cert "$SEAL_CERT")

b64() { printf '%s' "$1" | base64 | tr -d '\n'; }
# 32 characters of [A-Za-z0-9] (no quoting trouble in URLs or shells).
password() { LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom 2>/dev/null | head -c 32 || true; }

# seal <name> <type> <key> <value> [<key> <value> ...]
seal() {
  local name=$1 type=$2; shift 2
  local out="$dir/$name.sealed.yaml"
  {
    printf 'apiVersion: v1\nkind: Secret\ntype: %s\nmetadata:\n  name: %s\n  namespace: %s\n' "$type" "$name" "$ns"
    printf '  labels:\n    app.kubernetes.io/part-of: finuance\n'
    # CloudNativePG reloads a secret it uses (a role's password, the store's keys) on change only
    # when the secret carries this label.
    [ "$what" = assistant ] || [ "$what" = instance-key ] || [ "$what" = pull ] || [ "$what" = smtp ] || [ "$what" = grafana ] || [ "$name" = rustfs ] || printf '    cnpg.io/reload: "true"\n'
    printf 'data:\n'
    while [ $# -gt 0 ]; do printf '  %s: %s\n' "$1" "$(b64 "$2")"; shift 2; done
  } | kubeseal "${seal_args[@]}" >"$out.tmp"
  mv "$out.tmp" "$out"
  echo "seal-secrets: wrote $out"
}

case $what in
  db)
    seal finuance-db-owner kubernetes.io/basic-auth username finuance_owner password "$(password)"
    seal finuance-db-app kubernetes.io/basic-auth username finuance_app password "$(password)"
    ;;
  backup)
    : "${ACCESS_KEY_ID:?set ACCESS_KEY_ID (the backup store's access key)}"
    : "${ACCESS_SECRET_KEY:?set ACCESS_SECRET_KEY (the backup store's secret key)}"
    seal finuance-db-backup Opaque ACCESS_KEY_ID "$ACCESS_KEY_ID" ACCESS_SECRET_KEY "$ACCESS_SECRET_KEY"
    ;;
  backup-store)
    # One key pair, generated here and never shown: the database's backup credentials and the
    # store's own keys must be the same.
    key_id=$(password | head -c 20) key_secret=$(password)
    seal finuance-db-backup Opaque ACCESS_KEY_ID "$key_id" ACCESS_SECRET_KEY "$key_secret"
    ns=s3 dir=${RUSTFS_SECRETS_DIR:-deploy/environments/$env/rustfs}
    [ -d "$dir" ] || { echo "seal-secrets: $dir does not exist" >&2; exit 1; }
    seal rustfs Opaque RUSTFS_ACCESS_KEY "$key_id" RUSTFS_SECRET_KEY "$key_secret"
    ;;
  pull)
    # The registry login for the private images: a token that may only read packages, typed or piped
    # on stdin (never an argument or a file).
    [ -t 0 ] && printf 'GitHub token (read:packages), not shown: ' >&2
    IFS= read -rs token || true; [ -t 0 ] && echo >&2
    [ -n "$token" ] || { echo "seal-secrets: no token on stdin" >&2; exit 2; }
    user=${GHCR_USER:-x2db736ac85h}
    auth=$(b64 "$user:$token")
    config=$(printf '{"auths":{"ghcr.io":{"username":"%s","auth":"%s"}}}' "$user" "$auth")
    seal ghcr-pull kubernetes.io/dockerconfigjson .dockerconfigjson "$config"
    ;;
  smtp)
    # The password of the account that sends the alert e-mails (an app password), typed or piped on
    # stdin; bound to the monitoring namespace, where the AlertmanagerConfig reads it.
    [ -t 0 ] && printf 'SMTP (app) password, not shown: ' >&2
    IFS= read -rs secret || true; [ -t 0 ] && echo >&2
    [ -n "$secret" ] || { echo "seal-secrets: no password on stdin" >&2; exit 2; }
    secret=${secret// /}   # Google shows app passwords in groups of four with spaces
    ns=${NAMESPACE:-monitoring} dir=${SMTP_SECRETS_DIR:-deploy/environments/$env/monitoring}
    [ -d "$dir" ] || { echo "seal-secrets: $dir does not exist" >&2; exit 1; }
    seal finuance-alert-smtp Opaque password "$secret"
    ;;
  grafana)
    # Grafana's admin login (the chart's default password is public). Read it back with kubectl when
    # needed (docs/RUNBOOK.md "Monitoring"); a new run replaces it.
    ns=${NAMESPACE:-monitoring} dir=${GRAFANA_SECRETS_DIR:-deploy/environments/$env/monitoring}
    [ -d "$dir" ] || { echo "seal-secrets: $dir does not exist" >&2; exit 1; }
    seal grafana-admin Opaque admin-user admin admin-password "$(password)"
    ;;
  assistant)
    [ $# -eq 3 ] && [ -f "$3" ] || { echo "seal-secrets: give the KEY=value file (e.g. OPENAI_API_KEY=…)" >&2; exit 2; }
    pairs=()
    while IFS= read -r line || [ -n "$line" ]; do
      case $line in ''|'#'*) continue ;; esac
      key=${line%%=*}
      [[ $key =~ ^[A-Z_][A-Z0-9_]*$ ]] || { echo "seal-secrets: not KEY=value: ${key:0:20}…" >&2; exit 2; }
      pairs+=("$key" "${line#*=}")
    done <"$3"
    [ ${#pairs[@]} -gt 0 ] || { echo "seal-secrets: $3 has no KEY=value lines" >&2; exit 2; }
    seal finuance-assistant Opaque "${pairs[@]}"
    ;;
  instance-key)
    # FINUANCE_INSTANCE_KEY (ADR 0035): 32 random bytes in base64. It seals the API keys entered in
    # Administration → Assistant; sealing a new one makes those keys unreadable (enter them again).
    seal finuance-instance-key Opaque FINUANCE_INSTANCE_KEY "$(head -c 32 /dev/urandom | base64 | tr -d '\n')"
    ;;
  *) usage ;;
esac
