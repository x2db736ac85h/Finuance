#!/usr/bin/env bash
# Every *.sealed.yaml must be a SealedSecret with only encrypted values: gitleaks allowlists these
# files by name (.gitleaks.toml), so a plain Secret saved under such a name by mistake would
# otherwise go unnoticed. Checks this repository and, when it is cloned next to it, the private
# environment repository that holds the homelab's SealedSecrets ($FINUANCE_ENV_DIR, default
# ../finuance-env; ADR 0023). Part of make check.
set -euo pipefail
cd "$(dirname "$0")/.."
YQ=${YQ:-yq}
env_dir=${FINUANCE_ENV_DIR:-../finuance-env}
fail=0 n=0
check() {
  local f=$1 kinds plain
  n=$((n + 1))
  kinds=$("$YQ" -N '.kind' "$f" | sort -u | tr '\n' ' ')
  [ "$kinds" = "SealedSecret " ] || { echo "check-sealed: $f is not (only) a SealedSecret: $kinds"; fail=1; }
  plain=$("$YQ" -N '[.. | select(has("data") or has("stringData")) | path | join(".")] | .[]' "$f" || true)
  [ -z "$plain" ] || { echo "check-sealed: $f has plain values at: $plain"; fail=1; }
  [ "$("$YQ" -N '.spec.encryptedData | length' "$f")" -gt 0 ] || { echo "check-sealed: $f has no encryptedData"; fail=1; }
}
while IFS= read -r f; do check "$f"; done < <(git ls-files --cached --others --exclude-standard '*.sealed.yaml')
if git -C "$env_dir" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  while IFS= read -r f; do check "$env_dir/$f"; done < <(git -C "$env_dir" ls-files --cached --others --exclude-standard '*.sealed.yaml')
fi
[ "$fail" = 0 ] || exit 1
echo "check-sealed: ok ($n SealedSecrets)"
