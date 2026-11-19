#!/usr/bin/env bash
# Sets image.tag and image.digests in deploy/environments/homelab/values.yaml (release.yml's GitOps
# bump, PLAN 16.4 and 23.1): the version and the digests of the two images that release pushed.
# The lines are edited in place so the file keeps its comments and layout; yq then checks that the
# three values are the new ones and that nothing else changed.
#   tools/bump-homelab-tag.sh 0.5.0 sha256:<api digest> sha256:<web digest> [values file]
# The version is X.Y.Z, or X.Y.Z-rc.N for a release candidate deployed by hand (release.yml opens
# its pull request for X.Y.Z only; the digests are in the release's notes).
# Needs mikefarah's yq v4 (YQ overrides the command; GitHub's runners ship it as yq).
set -euo pipefail
cd "$(dirname "$0")/.."
YQ=${YQ:-yq}
"$YQ" --version 2>/dev/null | grep -q mikefarah || { echo "bump-homelab-tag: $YQ is not mikefarah's yq v4" >&2; exit 1; }
v=${1:?give the version, e.g. 0.5.0}
api=${2:?give the api image digest (sha256:…)}
web=${3:?give the web image digest (sha256:…)}
f=${4:-deploy/environments/homelab/values.yaml}
[[ $v =~ ^[0-9]+\.[0-9]+\.[0-9]+(-rc\.[0-9]+)?$ ]] || { echo "bump-homelab-tag: $v is not X.Y.Z or X.Y.Z-rc.N" >&2; exit 2; }
for d in "$api" "$web"; do
  [[ $d =~ ^sha256:[0-9a-f]{64}$ ]] || { echo "bump-homelab-tag: $d is not a sha256 digest" >&2; exit 2; }
done
before=$(mktemp); cp "$f" "$before"
# inside the top-level `image:` block: the first `  tag:` line, and `    api:` / `    web:` under `  digests:`
awk -v v="$v" -v api="$api" -v web="$web" '
  /^[^ #]/ { in_image = ($0 ~ /^image:/); in_digests = 0 }
  in_image && /^  [^ #]/ { in_digests = ($0 ~ /^  digests:/) }
  in_image && !t && /^  tag:/ { sub(/^  tag:.*/, "  tag: \"" v "\""); t = 1 }
  in_digests && !a && /^    api:/ { sub(/^    api:.*/, "    api: \"" api "\""); a = 1 }
  in_digests && !w && /^    web:/ { sub(/^    web:.*/, "    web: \"" web "\""); w = 1 }
  { print }
  END { if (!t || !a || !w) exit 3 }
' "$before" >"$f" || { cp "$before" "$f"; echo "bump-homelab-tag: no image.tag or image.digests.api/web in $f" >&2; exit 1; }
if [ "$($YQ '.image.tag' "$f")" != "$v" ] || [ "$($YQ '.image.digests.api' "$f")" != "$api" ] || [ "$($YQ '.image.digests.web' "$f")" != "$web" ]; then
  cp "$before" "$f"; echo "bump-homelab-tag: the tag or a digest did not change" >&2; exit 1
fi
if [ "$($YQ 'del(.image.tag) | del(.image.digests)' "$before")" != "$($YQ 'del(.image.tag) | del(.image.digests)' "$f")" ]; then
  cp "$before" "$f"; echo "bump-homelab-tag: the edit touched more than image.tag and image.digests" >&2; exit 1
fi
rm -f "$before"
echo "bump-homelab-tag: image.tag = $v, digests api ${api:7:12}… web ${web:7:12}… in $f"
