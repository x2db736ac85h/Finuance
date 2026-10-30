#!/usr/bin/env bash
# Query smoke test against a running stack (api + worker), PLAN Phase 7:
#   import the legacy demo year → filters, search, period, sort, distinct values, categories,
#   detail → a CSV export saved for opening in Excel / LibreOffice.
#
#   make up && scripts/query-smoke.sh
#
# BASE_URL defaults to the Compose API (http://localhost:8080); ORIGIN must equal the stack's
# FINUANCE_PUBLIC_ORIGIN (CSRF check). The export lands in $EXPORT (default ./query-smoke-export.csv).
# Needs curl only. Each run uses a fresh user, deleted at the end.
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
ORIGIN="${ORIGIN:-http://localhost:8000}"
EXPORT="${EXPORT:-$PWD/query-smoke-export.csv}"
API="$BASE_URL/api/v1"
EMAIL="query-smoke-$(date +%s)-$RANDOM@example.test"
PASS="query smoke password"
# An existing user instead of a sign-up, for instances with sign-up off (scripts/smoke-k8s.sh makes
# a throwaway user per script); the password comes from the environment only.
if [[ -n ${SMOKE_EMAIL:-} ]]; then EMAIL=$SMOKE_EMAIL; PASS=${SMOKE_PASSWORD:?set SMOKE_PASSWORD with SMOKE_EMAIL}; fi
ROOT="$(cd "$(dirname "$0")/.." && pwd)"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
jar="$work/jar"
: > "$jar"

step=0
check() { # check STATUS WANT LABEL
  step=$((step + 1))
  if [[ $1 != "$2" ]]; then
    echo "FAIL step $step: $3 → $1, want $2" >&2
    cat "$work/body" >&2; echo >&2
    exit 1
  fi
}
curl_api() { curl -sS -o "$work/body" -w '%{http_code}' -b "$jar" -c "$jar" -H "X-Finuance-Client: web" -H "Origin: $ORIGIN" "$@"; }
call() { # call METHOD PATH EXPECTED_STATUS [JSON]
  local args=(-X "$1")
  [[ -n ${4:-} ]] && args+=(-H "Content-Type: application/json" --data "$4")
  check "$(curl_api "${args[@]}" "$API$2")" "$3" "$1 $2"
}
field() { grep -o "\"$1\":\"[^\"]*\"" "$work/body" | head -1 | cut -d'"' -f4; }
number() { grep -o "\"$1\":-\?[0-9]*" "$work/body" | head -1 | cut -d: -f2; }
# query LABEL JSON: POST /transactions/query and print the total with the first row's payee
query() {
  call POST /transactions/query 200 "$2"
  printf 'ok %2d  %-46s total %6s  first: %s %s\n' "$step" "$1" "$(number total)" "$(field bookedOn)" "$(field payee)"
}

if [[ -n ${SMOKE_EMAIL:-} ]]; then
  call POST /auth/login 200 "{\"email\":\"$EMAIL\",\"password\":\"$PASS\"}"
else
  call POST /auth/signup 201 "{\"email\":\"$EMAIL\",\"password\":\"$PASS\"}"
fi
call POST /accounts 201 '{"name":"Demo"}'
account="$(field id)"
check "$(curl_api -X POST -F "accountId=$account" -F "file=@$ROOT/testdata/fixtures/demo/demo-legacy.csv;type=text/csv" "$API/imports")" 201 "upload demo"
imp="$(field id)"
call POST "/imports/$imp/commit" 202 '{}'
for _ in $(seq 100); do
  curl_api "$API/imports/$imp" > /dev/null
  [[ $(field status) != committing ]] && break
  sleep 0.2
done
[[ $(field status) == committed ]] || { echo "FAIL: import is $(field status)" >&2; exit 1; }
printf 'ok %2d  %-46s\n' "$step" "demo year imported and committed"

query "everything"                          '{"context":{},"limit":5}'
query "amount <= -100"                      '{"context":{"filters":[{"column":"amount","cond":"<=-100"}]},"limit":5}'
query "payee contains \"kino\""             '{"context":{"filters":[{"column":"payee","cond":"kino"}]},"limit":5}'
query "search \"płatność blik\""            '{"context":{"search":"płatność blik"},"limit":5}'
query "category = Rachunki, sorted by amount" '{"context":{"filters":[{"column":"category","cond":"=Rachunki"}]},"sort":{"column":"amount"},"limit":5}'
query "March 2025"                          '{"context":{"period":{"month":"2025-03"}},"limit":5}'
printf '         %-46s %s\n' "" "period: $(field label) $(field from)..$(field to)"
query "last 3 months, no Artykuły spożywcze" '{"context":{"period":{"preset":"l3m"},"filters":[{"column":"category","excluded":["Artykuły spożywcze"]}]},"limit":5}'

call POST /transactions/distinct 200 '{"context":{},"column":"category","limit":3}'
printf 'ok %2d  %-46s %s\n' "$step" "top 3 categories by count" "$(tr -d '\n' < "$work/body")"
call GET "/categories?accountIds=$account" 200
printf 'ok %2d  %-46s %s\n' "$step" "categories with colour slots" "$(grep -o '"colorSlot":1,[^}]*' "$work/body" | grep -o '"name":"[^"]*"' | head -1)"
query "first row (for the detail)" '{"context":{},"limit":1}'
tx="$(grep -o '"id":"[^"]*","isTransfer"' "$work/body" | head -1 | cut -d'"' -f4)"   # the row's id, not its category's
call GET "/transactions/$tx" 200
printf 'ok %2d  %-46s %s\n' "$step" "transaction detail" "sources from $(field filename)"

check "$(curl_api -X POST -H "Content-Type: application/json" \
  --data '{"context":{"filters":[{"column":"category","cond":"=Rachunki"}]},"columns":["bookedOn","payee","description","amount","category"],"view":"rachunki"}' \
  "$API/transactions/export")" 200 "export"
cp "$work/body" "$EXPORT"
printf 'ok %2d  %-46s %s (%s rows)\n' "$step" "CSV export saved" "$EXPORT" "$(wc -l < "$EXPORT" | tr -d ' ')"

call DELETE /me 204 "{\"password\":\"$PASS\"}"   # clean up
echo "query smoke: all $step steps passed ($EMAIL)"
