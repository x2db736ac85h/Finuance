#!/usr/bin/env bash
# Every *.sealed.yaml in the repository must be a SealedSecret with only encrypted values: gitleaks
# allowlists these files by name (.gitleaks.toml), so a plain Secret saved under such a name by
# mistake would otherwise go unnoticed. Part of make check.
set -euo pipefail
cd "$(dirname "$0")/.."
YQ=${YQ:-yq}
fail=0 n=0
while IFS= read -r f; do
  n=$((n + 1))
  kinds=$("$YQ" -N '.kind' "$f" | sort -u | tr '\n' ' ')
  [ "$kinds" = "SealedSecret " ] || { echo "check-sealed: $f is not (only) a SealedSecret: $kinds"; fail=1; }
  plain=$("$YQ" -N '[.. | select(has("data") or has("stringData")) | path | join(".")] | .[]' "$f" || true)
  [ -z "$plain" ] || { echo "check-sealed: $f has plain values at: $plain"; fail=1; }
  [ "$("$YQ" -N '.spec.encryptedData | length' "$f")" -gt 0 ] || { echo "check-sealed: $f has no encryptedData"; fail=1; }
done < <(git ls-files --cached --others --exclude-standard '*.sealed.yaml')
[ "$fail" = 0 ] || exit 1
echo "check-sealed: ok ($n SealedSecrets)"
