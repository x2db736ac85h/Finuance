#!/usr/bin/env bash
# Publishes one commit's tree as the single root commit of a new, empty repository. Nothing of this
# repository's history goes along: no earlier commits, authors, messages, branches or tags.
#
#   PUBLISH_NAME=<handle> PUBLISH_EMAIL=<id+handle@users.noreply.github.com> \
#     tools/publish-public.sh [--dry-run] <ref> [<empty-remote-url>]
#
# What it does, in order (it stops at the first step that fails):
#   1. exports the tree of <ref> into a new folder (git archive: tracked files only);
#   2. empties .gitleaksignore there (its fingerprints name commits that don't exist in the new
#      repository);
#   3. runs the exported tree's own guards on it: tools/check-private.sh and tools/check-sealed.sh,
#      and gitleaks when it is installed;
#   4. with PUBLISH_PATTERNS=<file> (one term per line, e.g. the private patterns file that lives
#      outside this repository), fails when any term occurs in a file or a file name, ignoring case;
#      it prints file:line only, never the term;
#   5. makes one commit on main as PUBLISH_NAME <PUBLISH_EMAIL>, with no trailers;
#   6. checks that the remote has no refs at all, and pushes main.
#
# --dry-run does steps 1-5 and leaves the folder for reading; it pushes nothing and needs no remote.
# PUBLISH_EMAIL must be a GitHub no-reply address. PUBLISH_MESSAGE replaces the commit message.
set -euo pipefail

dry=0
if [ "${1:-}" = "--dry-run" ]; then dry=1; shift; fi
ref="${1:-}"
remote="${2:-}"
die() { echo "publish-public: $*" >&2; exit 1; }

[ -n "$ref" ] || die "usage: tools/publish-public.sh [--dry-run] <ref> [<empty-remote-url>]"
[ "$dry" = 1 ] || [ -n "$remote" ] || die "a remote is required unless --dry-run"
: "${PUBLISH_NAME:?set PUBLISH_NAME (the public handle)}"
: "${PUBLISH_EMAIL:?set PUBLISH_EMAIL (the GitHub no-reply address)}"
case "$PUBLISH_EMAIL" in
  *@users.noreply.github.com) ;;
  *) die "PUBLISH_EMAIL must be a GitHub no-reply address (…@users.noreply.github.com)" ;;
esac
if [ -n "${PUBLISH_PATTERNS:-}" ]; then
  [ -f "$PUBLISH_PATTERNS" ] || die "PUBLISH_PATTERNS is not a file"
  patterns="$(cd "$(dirname "$PUBLISH_PATTERNS")" && pwd)/$(basename "$PUBLISH_PATTERNS")"
fi

src="$(git rev-parse --show-toplevel)"
commit="$(git -C "$src" rev-parse --verify --quiet "$ref^{commit}")" || die "unknown ref: $ref"

# The remote must be empty before any work is done on it.
if [ "$dry" = 0 ]; then
  refs="$(git ls-remote "$remote")" || die "cannot read the remote"
  [ -z "$refs" ] || die "the remote already has refs; publish only into a new, empty repository"
fi

work="$(mktemp -d "${TMPDIR:-/tmp}/finuance-public.XXXXXX")"
[ "$dry" = 1 ] || trap 'rm -rf "$work"' EXIT

# 1. the tree, and nothing else
git -C "$src" archive --format=tar "$commit" | tar -x -C "$work"
cd "$work"

# 2. fingerprints of commits that stay behind
if [ -f .gitleaksignore ]; then
  cat >.gitleaksignore <<'EOF'
# Findings gitleaks reports that are not secrets. Each line is a finding's fingerprint
# (commit:path:rule:line), so only that exact occurrence is skipped; a new one still fails.
EOF
fi

git init -q -b main
git add -A

# 3. the tree's own guards
[ -x tools/check-private.sh ] || die "the exported tree has no tools/check-private.sh"
tools/check-private.sh || die "the private-data check failed on the exported tree"
if [ -x tools/check-sealed.sh ]; then
  tools/check-sealed.sh || die "the sealed-secret check failed on the exported tree"
fi
if command -v gitleaks >/dev/null 2>&1; then
  gitleaks dir --no-banner --redact . || die "gitleaks found something in the exported tree"
else
  echo "publish-public: gitleaks is not installed; its scan was skipped" >&2
fi

# 4. personal terms: where, never what
if [ -n "${patterns:-}" ]; then
  terms="$(grep -v '^[[:space:]]*\(#.*\)\?$' "$patterns" || true)"
  if [ -n "$terms" ]; then
    hits="$(git ls-files -z | xargs -0 grep -I -i -n -F -f <(printf '%s\n' "$terms") -- 2>/dev/null | cut -d: -f1,2 || true)"
    names="$(git ls-files | grep -i -n -F -f <(printf '%s\n' "$terms") | cut -d: -f1 | sed 's/^/file name, line of `git ls-files`: /' || true)"
    if [ -n "$hits$names" ]; then
      echo "publish-public: a term from PUBLISH_PATTERNS occurs at:" >&2
      printf '%s\n' "$hits" "$names" | sed '/^$/d; s/^/  /' >&2
      die "personal terms in the exported tree"
    fi
  fi
fi

# 5. one commit, one identity, no trailers
message="${PUBLISH_MESSAGE:-Finuance: initial public commit}"
git -c user.name="$PUBLISH_NAME" -c user.email="$PUBLISH_EMAIL" -c commit.gpgsign=false \
  commit -q --no-verify -m "$message"
files="$(git ls-files | wc -l | tr -d ' ')"

if [ "$dry" = 1 ]; then
  echo "publish-public: dry run ok ($files files, 1 commit by $PUBLISH_NAME); read the tree in $work"
  exit 0
fi

# 6. into the empty remote
git push -q "$remote" main:main
echo "publish-public: pushed $files files as one commit to main"
