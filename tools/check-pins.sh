#!/usr/bin/env bash
# Fails when something the repository runs but does not build is referenced by a name that can be
# repointed (PLAN task 22.4):
#   1. a workflow step `uses:` an action without a full 40-character commit SHA (local `./` actions
#      and `docker://…@sha256:` excepted); the version goes into a comment: `@<sha> # v7.0.1`;
#   2. an image without `@sha256:` in an `image:` value, a Dockerfile `FROM`, a `…IMAGE=`,
#      `…IMAGE :=` or `…IMAGE:` (workflow `env:`) setting, or a `docker run` line.
# Allowed without a digest: the images this repository builds itself (`finuance-*:dev`,
# `finuance-*:rehearsal`, `ghcr.io/x2db736ac85h/finuance-*`, which Phase 23 deploys by digest),
# Dockerfile stages (`FROM build`), template expressions (`{{ … }}`) and shell variables (`$X`).
#
# Usage: tools/check-pins.sh            # scan the repository
#        tools/check-pins.sh FILE...    # scan only the given files
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"

if [ "$#" -gt 0 ]; then
  files=("$@")
else
  files=()
  while IFS= read -r -d '' f; do
    case "$f" in
      tools/check-pins-test.sh) continue ;; # its cases are meant to be refused
      .github/workflows/*.yml | .github/workflows/*.yaml | Makefile | tools/*.sh | scripts/*.sh) ;;
      */Dockerfile* | Dockerfile*) case "$f" in *.dockerignore) continue ;; esac ;;
      deploy/helm/*/templates/*) continue ;;
      deploy/*.yaml | deploy/*.yml | tools/*.yaml | tools/*.yml) ;;
      *) continue ;;
    esac
    [ -f "$f" ] && files+=("$f")
  done < <(git ls-files -z --cached --others --exclude-standard)
fi

[ "${#files[@]}" -gt 0 ] || { echo "check-pins: ok (no files)"; exit 0; }

if ! perl -ne '
    BEGIN { $bad = 0 }
    sub own { my $i = shift; return $i =~ m{^(?:[\w.-]+/)*finuance-[\w-]+:(?:dev|rehearsal)$} || $i =~ m{^ghcr\.io/x2db736ac85h/finuance-} }
    sub check {
      my ($img, $what) = @_;
      $img =~ s/^["\x27]|["\x27]$//g;
      return if $img eq "" || $img =~ /^[\$\{]/ || $img =~ /\{\{/ || $img =~ /\@sha256:[0-9a-f]{64}$/ || own($img);
      print STDERR "check-pins: $ARGV:$.: $what $img has no \@sha256: digest\n";
      $bad = 1;
    }
    next if /^\s*#/;
    if (/^\s*-?\s*uses:\s*(\S+)/) {
      my $u = $1;
      unless ($u =~ m{^\./} || $u =~ m{^docker://.*\@sha256:[0-9a-f]{64}$} || $u =~ m{^[\w.-]+/[\w./-]+\@[0-9a-f]{40}$}) {
        print STDERR "check-pins: $ARGV:$.: uses $u is not pinned to a 40-character commit SHA\n";
        $bad = 1;
      }
    }
    check($1, "image") if /^\s*-?\s*image:\s*(\S+)\s*$/;
    if ($ARGV =~ m{(^|/)Dockerfile[^/]*$} && /^\s*FROM\s+(?:--platform=\S+\s+)?(\S+)/i) {
      my $from = $1;
      check($from, "FROM") if $from =~ m{[:/]};    # a bare name is an earlier stage
    }
    check($1, "image") if /\b[A-Z0-9_]*IMAGE\s*(?::?=|:)\s*(?:\$\{[A-Z_]+:-)?([^\s}]+)/;
    if (/\bdocker\s+run\b/) {
      for my $tok (split /\s+/) {
        check($tok, "docker run") if $tok =~ m{^[a-z0-9][a-z0-9._-]*(?:/[a-z0-9._-]+)+:[\w.-]+(?:\@sha256:[0-9a-f]+)?$};
      }
    }
    close ARGV if eof;
    END { exit $bad }
  ' "${files[@]}"; then
  echo "check-pins: FAILED. Pin actions by commit SHA (\`uses: owner/repo@<sha> # vX.Y.Z\`) and images by digest (\`name:tag@sha256:…\`)." >&2
  exit 1
fi
echo "check-pins: ok (${#files[@]} files scanned)"
