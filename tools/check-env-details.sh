#!/usr/bin/env bash
# Fails when details of a private environment (hardware, network, host names) appear in this
# repository. Finuance runs on any Kubernetes homelab; machine-specific values belong in the private
# environment repository (docs/RUNBOOK.md "Bootstrap on single-node k3s", ADR 0022).
#
# The patterns can't live here (they would publish what they protect), so they are read from
# $FINUANCE_ENV_DIR/private-patterns.txt (default: ../finuance-env, the private repository cloned
# next to this one): one extended regular expression per line, `#` comments and blank lines ignored.
# Without that file (CI, other contributors) the check is skipped.
#
# Scans tracked and untracked (non-ignored) files, and the messages of commits not yet on
# origin/main, since a commit message is pushed as well.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"
env_dir="${FINUANCE_ENV_DIR:-$root/../finuance-env}"
patterns="$env_dir/private-patterns.txt"

if [ ! -f "$patterns" ]; then
  echo "check-env-details: skipped (no $patterns)"
  exit 0
fi

regex="$(grep -vE '^[[:space:]]*(#|$)' "$patterns" | paste -sd'|')"
[ -n "$regex" ] || { echo "check-env-details: $patterns has no patterns" >&2; exit 1; }

status=0
files="$(git ls-files --cached --others --exclude-standard -z | xargs -0 -r grep -lIE -- "$regex" 2>/dev/null || true)"
if [ -n "$files" ]; then
  echo "check-env-details: environment details found in (move them to the private environment repository):" >&2
  echo "$files" | sed 's/^/  /' >&2
  status=1
fi

if git rev-parse -q --verify origin/main >/dev/null; then
  commits="$(git log --format='%h %s' origin/main..HEAD --grep="$regex" -E || true)"
  if [ -n "$commits" ]; then
    echo "check-env-details: environment details in commit messages not yet on main:" >&2
    echo "$commits" | sed 's/^/  /' >&2
    status=1
  fi
fi

[ "$status" -eq 0 ] && echo "check-env-details: ok"
exit "$status"
