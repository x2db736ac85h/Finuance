#!/usr/bin/env bash
# Publishes a commit into a new, empty repository, in one of two ways:
#
#   - by default as a single root commit: nothing of this repository's history goes along (no
#     earlier commits, authors, messages, branches or tags);
#   - with --history, with the history of <ref>, rewritten so that no commit carries a private
#     e-mail address: every author or committer address that is not a no-reply address becomes
#     PUBLISH_EMAIL (names and messages stay). Other branches, tags and pull-request refs stay behind.
#
#   PUBLISH_NAME=<handle> PUBLISH_EMAIL=<id+handle@users.noreply.github.com> \
#     tools/publish-public.sh [--dry-run] [--history] <ref> [<empty-remote-url>]
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
#      with --history it also searches every commit message and every line the history ever added;
#   5. makes one commit on main as PUBLISH_NAME <PUBLISH_EMAIL>, with no trailers; with --history it
#      rewrites the addresses instead, checks that only no-reply addresses are left, and carries
#      the .gitleaksignore fingerprints over to the rewritten commit ids;
#   6. checks that the remote has no refs at all, and pushes main.
#
# --dry-run does steps 1-5 and leaves the folder for reading; it pushes nothing and needs no remote.
# PUBLISH_EMAIL must be a GitHub no-reply address. PUBLISH_MESSAGE replaces the commit message.
set -euo pipefail

dry=0 history=0
while :; do
  case "${1:-}" in
    --dry-run) dry=1; shift ;;
    --history) history=1; shift ;;
    *) break ;;
  esac
done
ref="${1:-}"
remote="${2:-}"
die() { echo "publish-public: $*" >&2; exit 1; }

[ -n "$ref" ] || die "usage: tools/publish-public.sh [--dry-run] [--history] <ref> [<empty-remote-url>]"
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

# Addresses that may stay in a published history.
noreply='(@users\.noreply\.github\.com|^noreply@github\.com|^noreply@anthropic\.com)$'

if [ "$history" = 1 ]; then
  [ "$(git -C "$src" rev-parse --is-shallow-repository)" = false ] || die "--history needs the full history; this clone is shallow"
  # 1. the history of <ref>, and no other ref
  git init -q -b main "$work"
  cd "$work"
  git fetch -q --no-tags "$src" "$commit"
  git update-ref refs/heads/main FETCH_HEAD
  git update-ref refs/publish/before FETCH_HEAD
  git checkout -q main
  # 5a. private addresses become the no-reply address
  FILTER_BRANCH_SQUELCH_WARNING=1 PUBLISH_KEEP="$noreply" git filter-branch -f --env-filter '
    if ! printf "%s" "$GIT_AUTHOR_EMAIL" | grep -Eq "$PUBLISH_KEEP"; then GIT_AUTHOR_EMAIL="$PUBLISH_EMAIL"; fi
    if ! printf "%s" "$GIT_COMMITTER_EMAIL" | grep -Eq "$PUBLISH_KEEP"; then GIT_COMMITTER_EMAIL="$PUBLISH_EMAIL"; fi
    export GIT_AUTHOR_EMAIL GIT_COMMITTER_EMAIL
  ' -- main >/dev/null 2>&1 || die "rewriting the addresses failed"
  left="$(git log main --format='%ae%n%ce' | sort -u | grep -Ev "$noreply" || true)"
  [ -z "$left" ] || die "an address that is not a no-reply address is still in $(printf '%s\n' "$left" | wc -l | tr -d ' ') identities"
  # 2a. fingerprints follow their commits to the new ids (the same order in both histories)
  if [ -f .gitleaksignore ]; then
    paste -d' ' <(git rev-list --topo-order --reverse refs/publish/before) <(git rev-list --topo-order --reverse main) >"$work/.git/publish-map"
    while read -r old new; do
      [ "$old" = "$new" ] || sed -i "s/^$old:/$new:/" .gitleaksignore
    done <"$work/.git/publish-map"
  fi
  git update-ref -d refs/publish/before
  git for-each-ref --format='%(refname)' refs/original | while read -r r; do git update-ref -d "$r"; done
else
# 1. the tree, and nothing else
git -C "$src" archive --format=tar "$commit" | tar -x -C "$work"
cd "$work"
fi

# 2. fingerprints of commits that stay behind
if [ "$history" = 0 ] && [ -f .gitleaksignore ]; then
  cat >.gitleaksignore <<'EOF'
# Findings gitleaks reports that are not secrets. Each line is a finding's fingerprint
# (commit:path:rule:line), so only that exact occurrence is skipped; a new one still fails.
EOF
fi

[ "$history" = 1 ] || git init -q -b main
git add -A

# 3. the tree's own guards
[ -x tools/check-private.sh ] || die "the exported tree has no tools/check-private.sh"
tools/check-private.sh || die "the private-data check failed on the exported tree"
if [ -x tools/check-sealed.sh ]; then
  tools/check-sealed.sh || die "the sealed-secret check failed on the exported tree"
fi
if command -v gitleaks >/dev/null 2>&1; then
  if [ "$history" = 1 ]; then
    gitleaks git --no-banner --redact . || die "gitleaks found something in the history"
  else
    gitleaks dir --no-banner --redact . || die "gitleaks found something in the exported tree"
  fi
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
    if [ "$history" = 1 ]; then
      inmsg="$(git log main --format='%h%n%B' | grep -i -c -F -f <(printf '%s\n' "$terms") || true)"
      inlines="$(git log main -p --no-color --format= | grep '^+' | grep -i -c -F -f <(printf '%s\n' "$terms") || true)"
      [ "$inmsg" = 0 ] && [ "$inlines" = 0 ] || die "personal terms in the history: $inmsg commit-message lines, $inlines added lines"
    fi
  fi
fi

# 5. one commit, one identity, no trailers
if [ "$history" = 1 ]; then
  if ! git diff --cached --quiet; then
    git -c user.name="$PUBLISH_NAME" -c user.email="$PUBLISH_EMAIL" -c commit.gpgsign=false \
      commit -q --no-verify -m "chore: gitleaks fingerprints follow the rewritten commit ids"
  fi
else
  message="${PUBLISH_MESSAGE:-Finuance: initial public commit}"
  git -c user.name="$PUBLISH_NAME" -c user.email="$PUBLISH_EMAIL" -c commit.gpgsign=false \
    commit -q --no-verify -m "$message"
fi
files="$(git ls-files | wc -l | tr -d ' ')"
commits="$(git rev-list --count main)"

if [ "$dry" = 1 ]; then
  echo "publish-public: dry run ok ($files files, $commits commit(s)); read the tree in $work"
  exit 0
fi

# 6. into the empty remote
git push -q "$remote" main:main
echo "publish-public: pushed $files files in $commits commit(s) to main"
