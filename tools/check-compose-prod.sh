#!/usr/bin/env bash
# compose.prod.yaml carries copies of the database bootstrap (it must work with only itself and a
# .env). This fails when a copy differs from deploy/db/bootstrap.sql or deploy/db/initdb/00-bootstrap.sh.
set -euo pipefail
cd "$(dirname "$0")/.."
f=deploy/compose/compose.prod.yaml
block() { # the content of configs.<name>, without its 6-space indent
  awk -v name="  $1:" '
    $0 == name { inside = 1; next }
    inside && /^  [a-z#]/ { exit }
    inside && /^    content: \|$/ { body = 1; next }
    inside && body { sub(/^      /, ""); print }
  ' "$f"
}
status=0
diff -u deploy/db/bootstrap.sql <(block bootstrap-sql) || { echo "check-compose-prod: bootstrap-sql differs from deploy/db/bootstrap.sql"; status=1; }
diff -u deploy/db/initdb/00-bootstrap.sh <(block bootstrap-sh | sed 's/\$\$/$/g') || { echo "check-compose-prod: bootstrap-sh differs from deploy/db/initdb/00-bootstrap.sh"; status=1; }
[ $status -eq 0 ] && echo "check-compose-prod: ok"
exit $status
