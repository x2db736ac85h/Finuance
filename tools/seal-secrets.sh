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
#
# kubeseal fetches the controller's public certificate from the cluster (kube-system /
# sealed-secrets-controller); set SEAL_CERT=<file> to seal offline with a saved certificate
# (kubeseal --fetch-cert > cert.pem). NAMESPACE (default finuance) is the namespace the secrets are
# bound to (strict scope: a sealed secret only decrypts under its own name and namespace).
# SECRETS_DIR overrides the output folder (the CI rehearsal seals into a scratch folder).
set -euo pipefail
cd "$(dirname "$0")/.."

usage() { sed -n '2,16p' "$0" | sed 's/^# \{0,1\}//'; exit 2; }
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
    [ "$what" = assistant ] || [ "$name" = rustfs ] || printf '    cnpg.io/reload: "true"\n'
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
  *) usage ;;
esac
