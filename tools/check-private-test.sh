#!/usr/bin/env bash
# Tests tools/check-private.sh on throwaway files: each line that must be refused is refused, and
# the placeholders the repository uses pass. Part of `make check`.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/testdata" "$tmp/docs"
fails=0

# refused NAME FILE-RELATIVE-TO-TMP CONTENT
refused() {
  printf '%s\n' "$3" >"$tmp/$2"
  if "$root/tools/check-private.sh" "$tmp/$2" >/dev/null 2>&1; then
    echo "check-private-test: $1 was not refused" >&2
    fails=$((fails + 1))
  fi
}
# passes NAME FILE-RELATIVE-TO-TMP CONTENT
passes() {
  printf '%s\n' "$3" >"$tmp/$2"
  if ! out="$("$root/tools/check-private.sh" "$tmp/$2" 2>&1)"; then
    echo "check-private-test: $1 was refused: $out" >&2
    fails=$((fails + 1))
  fi
}

# The values this test needs are put together here, so this file itself passes the check.
at='@'
tsnet='ts'.net
digits="6110901014$(printf '%014d' 712198128)74"
mail="jan.kowalski${at}gmail.com"

refused "a 26-digit account number" docs/a.md "konto $digits"
refused "a spaced IBAN" docs/a.md "PL61 1090 1014 $(printf '%04d %04d %04d %04d' 0 712 1981 2874)"
refused "a mail address at a real provider" docs/a.md "write to $mail"
refused "a mail address in a commit trailer" docs/a.md "Co-authored-by: Someone <someone${at}company.io>"
refused "a tailnet host name" docs/a.md "https://box.tail1a2b3c.$tsnet"
refused "a short tailnet host name" docs/a.md "host: finuance.cat-dog.$tsnet"
refused "a postcode in test data" testdata/a.csv "UL. DLUGA 1 60-198 MIASTO"

passes "the all-zero account number" docs/b.md "'00000000000000000000000001"
passes "reserved mail domains" docs/b.md "alex@example.com a@b.example.test demo-1@demo.invalid root@localhost.localhost"
passes "the no-reply addresses" docs/b.md "1+x@users.noreply.github.com noreply@anthropic.com"
passes "a git remote and versions" docs/b.md "git@github.com:owner/repo.git actions/checkout@v7.0.1 yq/v4@v4.54.1 @types/react"
passes "the tailnet placeholders" docs/b.md "finuance.example.ts.net machine.tailXXXXXX.ts.net"
passes "an invented postcode in test data" testdata/b.csv "UL. FIRMOWA 1 00-002 WARSZAWA 2026-03-10 28.02-28.03"
passes "a postcode-like number outside test data" docs/b.md "pages 12-345"

# the message must not repeat the value it found
printf '%s\n' "$mail" >"$tmp/docs/c.md"
if "$root/tools/check-private.sh" "$tmp/docs/c.md" 2>&1 | grep -q "jan.kowalski"; then
  echo "check-private-test: the message prints the address it found" >&2
  fails=$((fails + 1))
fi

[ "$fails" -eq 0 ] || { echo "check-private-test: $fails failed" >&2; exit 1; }
echo "check-private-test: ok"
