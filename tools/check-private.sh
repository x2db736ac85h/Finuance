#!/usr/bin/env bash
# Fails when private data is about to reach git:
#   1. any tracked or staged file under testdata/private/;
#   2. any IBAN-like account number in tracked or untracked (non-ignored) files:
#      26 digits, optionally prefixed with "PL", or the spaced form "PL61 1090 1014 ...".
# Allowlisted: synthetic all-zero numbers (at least 20 leading zeros, e.g. 00000000000000000000000001).
#
# Usage: tools/check-private.sh            # scan the repository
#        tools/check-private.sh FILE...    # scan only the given files
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"

status=0

if [ "$#" -gt 0 ]; then
  files=("$@")
else
  private="$(git ls-files --cached -- testdata/private/ | grep -v '^testdata/private/\.gitkeep$' || true)"
  if [ -n "$private" ]; then
    echo "check-private: files under testdata/private/ must never be committed:" >&2
    echo "$private" | sed 's/^/  /' >&2
    status=1
  fi
  files=()
  while IFS= read -r -d '' f; do
    case "$f" in
      testdata/private/*) continue ;;
    esac
    [ -f "$f" ] && files+=("$f")
  done < <(git ls-files -z --cached --others --exclude-standard)
fi

if [ "${#files[@]}" -gt 0 ]; then
  # perl (not grep -P) so the script also works with BSD tools on macOS.
  if ! printf '%s\0' "${files[@]}" | xargs -0 perl -ne '
      BEGIN { $bad = 0 }
      while (/(?<![0-9A-Za-z])((?:PL)?(\d{26})|PL\d{2}(?: \d{4}){6})(?![0-9])/g) {
        my ($m, $digits) = ($1, $2);
        ($digits = $m) =~ s/\D//g unless defined $digits;
        $digits = substr($digits, -26) if length($digits) > 26;
        next if $digits =~ /^0{20}/;
        my $shown = substr($m, 0, 4) . "..." . substr($m, -2);
        print STDERR "check-private: IBAN-like number in $ARGV:$.: $shown\n";
        $bad = 1;
      }
      close ARGV if eof;
      END { exit $bad }
    '; then
    status=1
  fi
fi

if [ "$status" -ne 0 ]; then
  echo "check-private: FAILED. Use synthetic all-zero numbers (e.g. 00000000000000000000000001) in fixtures." >&2
  exit 1
fi
echo "check-private: ok (${#files[@]} files scanned)"
