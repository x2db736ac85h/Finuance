#!/usr/bin/env bash
# Fails when money code or the fingerprint path uses floating point (PLAN §4.4, ADR 0004).
# Comments are ignored, so doc text may still say "float64". Test files are not checked.
set -euo pipefail
cd "$(dirname "$0")/.."

files=(backend/internal/money/*.go backend/internal/ingest/fingerprint.go)
bad=0
n=0
for f in "${files[@]}"; do
  [[ $f == *_test.go ]] && continue
  n=$((n + 1))
  # Drop // comments (good enough for this code: no "//" inside string literals), then look for
  # float types, float conversions from strconv and the float functions of package math
  # (its integer constants such as math.MaxInt64 are fine).
  if hits="$(sed -E 's://.*$::' "$f" | grep -nE '\bfloat(32|64)\b|strconv\.(ParseFloat|FormatFloat)|\bmath\.(Float|Round|Floor|Ceil|Trunc|Mod|Pow|Abs|Inf|NaN|IsNaN|IsInf)' )"; then
    echo "check-no-float: $f uses floating point:"; echo "$hits"; bad=1
  fi
done
[[ $bad == 0 ]] && echo "check-no-float: ok ($n files)"
exit $bad
