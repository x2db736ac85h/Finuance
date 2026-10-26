#!/usr/bin/env bash
# Sets image.tag in deploy/environments/homelab/values.yaml (release.yml's GitOps bump, PLAN 16.4).
# The line is edited in place so the file keeps its comments and layout; yq then checks that the
# tag is the new one and that nothing else changed.
#   tools/bump-homelab-tag.sh 0.2.0 [values file]
# Needs mikefarah's yq v4 (YQ overrides the command; GitHub's runners ship it as yq).
set -euo pipefail
cd "$(dirname "$0")/.."
YQ=${YQ:-yq}
"$YQ" --version 2>/dev/null | grep -q mikefarah || { echo "bump-homelab-tag: $YQ is not mikefarah's yq v4" >&2; exit 1; }
v=${1:?give the version, e.g. 0.2.0}
f=${2:-deploy/environments/homelab/values.yaml}
[[ $v =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "bump-homelab-tag: $v is not X.Y.Z" >&2; exit 2; }
before=$(mktemp); cp "$f" "$before"
# the first `  tag:` line inside the top-level `image:` block
awk -v v="$v" '
  /^[^ #]/ { in_image = ($0 ~ /^image:/) }
  in_image && !done && /^  tag:/ { sub(/^  tag:.*/, "  tag: \"" v "\""); done = 1 }
  { print }
  END { if (!done) exit 3 }
' "$before" >"$f" || { cp "$before" "$f"; echo "bump-homelab-tag: no image.tag in $f" >&2; exit 1; }
[ "$($YQ '.image.tag' "$f")" = "$v" ] || { cp "$before" "$f"; echo "bump-homelab-tag: image.tag did not change" >&2; exit 1; }
if [ "$($YQ 'del(.image.tag)' "$before")" != "$($YQ 'del(.image.tag)' "$f")" ]; then
  cp "$before" "$f"; echo "bump-homelab-tag: the edit touched more than image.tag" >&2; exit 1
fi
rm -f "$before"
echo "bump-homelab-tag: image.tag = $v in $f"
