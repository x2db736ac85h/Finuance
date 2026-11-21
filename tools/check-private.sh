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
#      (paths with testdata/ or legacy/);
#   6. any payment card number: 13-19 digits (alone, or in groups of four, or 4-6-5) with a card
#      network's prefix and length that pass the Luhn check. Allowlisted: the networks' documented
#      test numbers (4111 1111 1111 1111 and the like).
#
# Usage: tools/check-private.sh            # scan the repository
#        tools/check-private.sh FILE...    # scan only the given files
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
here="$PWD"
cd "$root"

status=0

if [ "$#" -gt 0 ]; then
  # paths are the caller's, relative to where it was called from; a file that can't be read fails
  # (perl would only warn and scan nothing)
  files=()
  for f in "$@"; do
    case "$f" in /*) ;; *) f="$here/$f" ;; esac
    if [ ! -f "$f" ] || [ ! -r "$f" ]; then
      echo "check-private: can't read $f" >&2
      status=1
      continue
    fi
    files+=("$f")
  done
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
      BEGIN {
        $bad = 0;
        %test_cards = map { $_ => 1 } qw(4111111111111111 4242424242424242 4012888888881881 4222222222222
          4000056655665556 5555555555554444 5105105105105100 2223003122003222 378282246310005 371449635398431
          6011111111111117 6011000990139424 3530111333300000 3566002020360505);
      }
      sub card_network {
        my $d = shift; my $n = length $d; my $p4 = substr($d, 0, 4);
        return $d =~ /^4/ && ($n == 13 || $n == 16 || $n == 19)
          || $n == 16 && ($d =~ /^5[1-5]/ || ($p4 >= 2221 && $p4 <= 2720))
          || $n == 15 && $d =~ /^3[47]/
          || $n >= 16 && $n <= 19 && ($d =~ /^(?:6011|65|64[4-9])/ || ($p4 >= 3528 && $p4 <= 3589));
      }
      sub luhn {
        my @d = reverse split //, shift; my $s = 0;
        for my $i (0 .. $#d) { my $x = $d[$i]; if ($i % 2) { $x *= 2; $x -= 9 if $x > 9 } $s += $x }
        return $s % 10 == 0;
      }
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
      while (/(?<![\w.-])(\d{13,19}|\d{4}(?:[ -]\d{4}){2,3}(?:[ -]\d{1,3})?|\d{4}[ -]\d{6}[ -]\d{5})(?![\w.-]?\d)/g) {
        (my $d = $1) =~ s/\D//g;
        next unless card_network($d) && luhn($d) && !$test_cards{$d};
        print STDERR "check-private: payment card number in $ARGV:$.: " . substr($d, 0, 4) . " ... (" . length($d) . " digits)\n";
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
  echo "check-private: FAILED. Use synthetic values: all-zero account numbers (00000000000000000000000001), example.com addresses, example.ts.net, 00-NNN postcodes, the card networks' test numbers." >&2
  exit 1
fi
echo "check-private: ok (${#files[@]} files scanned)"
