#!/usr/bin/env bash
# Fails when private data is about to reach git:
#   1. any tracked or staged file under testdata/private/;
#   2. any IBAN-like account number in tracked or untracked (non-ignored) files:
#      26 digits, optionally prefixed with "PL", or the spaced form "PL61 1090 1014 ...".
#      Allowlisted: synthetic all-zero numbers (at least 20 leading zeros, e.g. 00000000000000000000000001);
#   3. any e-mail address outside the reserved domains (example.com/.org/.net, *.test, *.invalid,
#      *.example, *.localhost) and the no-reply addresses of GitHub and Anthropic;
#   4. any tailnet host name (*.ts.net) other than the placeholders (example.ts.net, tailXXXXXX.ts.net);
#   5. any postcode (NN-NNN) outside the invented 00-NNN form in test data and the legacy sample
#      (paths with testdata/ or legacy/).
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
      while (/(?<![\w.+-])([\w.+-]+)@((?:[A-Za-z0-9-]+\.)+[A-Za-z]{2,})(?![\w-])/g) {
        my ($user, $domain) = ($1, lc $2);
        next if $domain =~ /(?:^|\.)(?:example\.(?:com|org|net)|test|invalid|example|localhost)$/;
        next if $domain eq "users.noreply.github.com" || ($domain eq "anthropic.com" && $user eq "noreply");
        next if $user eq "git"; # git@github.com:owner/repo.git
        print STDERR "check-private: e-mail address outside the reserved domains in $ARGV:$.: " . substr($user, 0, 2) . "...\@$domain\n";
        $bad = 1;
      }
      while (/(?<![\w-])((?:[A-Za-z0-9-]+\.)+ts\.net)(?![\w-])/gi) {
        my $host = lc $1;
        next if $host =~ /(?:^|\.)example\.ts\.net$/ || $host =~ /(?:^|\.)tailx+\.ts\.net$/;
        print STDERR "check-private: tailnet host name in $ARGV:$.: " . substr($host, 0, 3) . "...ts.net\n";
        $bad = 1;
      }
      if ($ARGV =~ m{(?:^|/)(?:testdata|legacy)/}) {
        while (/(?<![0-9A-Za-z-])(\d{2})-\d{3}(?![0-9A-Za-z-])/g) {
          next if $1 eq "00";
          print STDERR "check-private: postcode outside the invented 00-NNN form in $ARGV:$.: $1-...\n";
          $bad = 1;
        }
      }
      close ARGV if eof;
      END { exit $bad }
    '; then
    status=1
  fi
fi

if [ "$status" -ne 0 ]; then
  echo "check-private: FAILED. Use synthetic values: all-zero account numbers (00000000000000000000000001), example.com addresses, example.ts.net, 00-NNN postcodes." >&2
  exit 1
fi
echo "check-private: ok (${#files[@]} files scanned)"
