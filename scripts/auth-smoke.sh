#!/usr/bin/env bash
# Auth smoke test against a running stack (PLAN task 5.9):
#   signup → me → logout → login → change password → the old session is rejected.
#
#   make up && scripts/auth-smoke.sh
#
# BASE_URL defaults to the Compose API (http://localhost:8080); ORIGIN must equal the stack's
# FINUANCE_PUBLIC_ORIGIN (CSRF check). Needs curl only. Each run uses a fresh email address.
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
ORIGIN="${ORIGIN:-http://localhost:8000}"
API="$BASE_URL/api/v1"
EMAIL="smoke-$(date +%s)-$RANDOM@example.test"
PASS1="smoke password one"
PASS2="smoke password two"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
jar="$work/jar"      # the "browser"
old="$work/old-jar"  # a copy of the session before the password change

step=0
# call METHOD PATH EXPECTED_STATUS [JSON] [JAR] — prints the body, fails on a status mismatch.
call() {
  local method=$1 path=$2 want=$3 body=${4:-} cj=${5:-$jar}
  local args=(-sS -o "$work/body" -w '%{http_code}' -X "$method" -b "$cj" -c "$cj"
    -H "X-Finuance-Client: web" -H "Origin: $ORIGIN")
  [[ -n $body ]] && args+=(-H "Content-Type: application/json" --data "$body")
  local got
  got="$(curl "${args[@]}" "$API$path")"
  step=$((step + 1))
  if [[ $got != "$want" ]]; then
    echo "FAIL step $step: $method $path → $got, want $want" >&2
    cat "$work/body" >&2; echo >&2
    exit 1
  fi
  printf 'ok %2d  %-6s %-16s → %s\n' "$step" "$method" "$path" "$got"
}

: > "$jar"
call GET  /meta 200
call POST /auth/signup 201 "{\"email\":\"$EMAIL\",\"password\":\"$PASS1\",\"displayName\":\"Smoke\"}"
grep -q '"displayName":"Smoke"' "$work/body"
call GET  /me 200
grep -q "\"email\":\"$EMAIL\"" "$work/body"
call POST /auth/logout 204
call GET  /me 401
call POST /auth/login 401 "{\"email\":\"$EMAIL\",\"password\":\"wrong password!\"}"
call POST /auth/login 200 "{\"email\":\"$EMAIL\",\"password\":\"$PASS1\"}"
call GET  /me 200
cp "$jar" "$old"
call POST /me/password 204 "{\"currentPassword\":\"$PASS1\",\"newPassword\":\"$PASS2\"}"
call GET  /me 200                 # this browser got a fresh session
call GET  /me 401 "" "$old"       # the session from before the change is dead
call POST /auth/login 401 "{\"email\":\"$EMAIL\",\"password\":\"$PASS1\"}"
call POST /auth/login 200 "{\"email\":\"$EMAIL\",\"password\":\"$PASS2\"}"
call DELETE /me 204 "{\"password\":\"$PASS2\"}"   # clean up
call GET  /me 401
echo "auth smoke: all $step steps passed ($EMAIL)"
