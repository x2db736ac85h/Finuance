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

# luhn_digit DIGITS: the check digit that makes DIGITS followed by it pass the Luhn check.
luhn_digit() {
  local s=0 i d n=${#1}
  for ((i = 0; i < n; i++)); do
    d=${1:n-1-i:1}
    if ((i % 2 == 0)); then d=$((d * 2)) && ((d > 9)) && d=$((d - 9)); fi
    s=$((s + d))
  done
  echo $(((10 - s % 10) % 10))
}

# The values this test needs are put together here, so this file itself passes the check.
at='@'
tsnet='ts'.net
digits="6110901014$(printf '%014d' 712198128)74"
mail="jan.kowalski${at}gmail.com"
visa="453914880343646$(luhn_digit 453914880343646)"
master="542523343010990$(luhn_digit 542523343010990)"
amex="37828224631001$(luhn_digit 37828224631001)"
noprefix="987654321098765$(luhn_digit 987654321098765)"
wrong="453914880343646$((($(luhn_digit 453914880343646) + 1) % 10))"

refused "a 26-digit account number" docs/a.md "konto $digits"
refused "a spaced IBAN" docs/a.md "PL61 1090 1014 $(printf '%04d %04d %04d %04d' 0 712 1981 2874)"
refused "a mail address at a real provider" docs/a.md "write to $mail"
refused "a mail address in a commit trailer" docs/a.md "Co-authored-by: Someone <someone${at}company.io>"
refused "a tailnet host name" docs/a.md "https://box.tail1a2b3c.$tsnet"
refused "a short tailnet host name" docs/a.md "host: finuance.cat-dog.$tsnet"
refused "a postcode in test data" testdata/a.csv "UL. DLUGA 1 60-198 MIASTO"
refused "a Visa card number" docs/a.md "card $visa"
refused "a Mastercard number in groups of four" testdata/a.csv "${master:0:4} ${master:4:4} ${master:8:4} ${master:12:4};-12,00"
refused "an Amex number in groups 4-6-5" docs/a.md "${amex:0:4}-${amex:4:6}-${amex:10:5}"

passes "the all-zero account number" docs/b.md "'00000000000000000000000001"
passes "reserved mail domains" docs/b.md "alex@example.com a@b.example.test demo-1@demo.invalid root@localhost.localhost"
passes "the no-reply addresses" docs/b.md "1+x@users.noreply.github.com noreply@anthropic.com"
passes "a git remote and versions" docs/b.md "git@github.com:owner/repo.git actions/checkout@v7.0.1 yq/v4@v4.54.1 @types/react"
passes "the tailnet placeholders" docs/b.md "finuance.example.ts.net machine.tailXXXXXX.ts.net"
passes "an invented postcode in test data" testdata/b.csv "UL. FIRMOWA 1 00-002 WARSZAWA 2026-03-10 28.02-28.03"
passes "a postcode-like number outside test data" docs/b.md "pages 12-345"
passes "the card networks' test numbers" docs/b.md "4111 1111 1111 1111, 4242424242424242, 378282246310005"
passes "a card-like number that fails the Luhn check" docs/b.md "ref $wrong"
passes "a Luhn-valid number without a card prefix" testdata/b.csv "ID $noprefix"

# paths relative to the caller's directory are read there, and a file that can't be read fails
printf '%s\n' "konto $digits" >"$tmp/docs/d.md"
if (cd "$tmp" && "$root/tools/check-private.sh" docs/d.md >/dev/null 2>&1); then
  echo "check-private-test: a relative path from another directory was not read" >&2
  fails=$((fails + 1))
fi
if "$root/tools/check-private.sh" "$tmp/docs/missing.md" >/dev/null 2>&1; then
  echo "check-private-test: a file that does not exist passed" >&2
  fails=$((fails + 1))
fi

# the message must not repeat the value it found
printf '%s\n' "$mail" >"$tmp/docs/c.md"
if "$root/tools/check-private.sh" "$tmp/docs/c.md" 2>&1 | grep -q "jan.kowalski"; then
  echo "check-private-test: the message prints the address it found" >&2
  fails=$((fails + 1))
fi
printf '%s\n' "$visa" >"$tmp/docs/c.md"
if "$root/tools/check-private.sh" "$tmp/docs/c.md" 2>&1 | grep -q "${visa:4}"; then
  echo "check-private-test: the message prints the card number it found" >&2
  fails=$((fails + 1))
fi

[ "$fails" -eq 0 ] || { echo "check-private-test: $fails failed" >&2; exit 1; }
echo "check-private-test: ok"
