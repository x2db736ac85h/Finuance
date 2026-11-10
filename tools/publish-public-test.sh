#!/usr/bin/env bash
# Tests tools/publish-public.sh with throwaway repositories. Part of `make check`.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
publish="$root/tools/publish-public.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
export TMPDIR="$tmp/work"
mkdir -p "$TMPDIR"
fails=0
fail() { echo "publish-public-test: $*" >&2; fails=$((fails + 1)); }

# The values the guards must refuse are put together here, so this file passes them itself.
at='@'
noreply="1+handle${at}users.noreply.github.com"
real="jan.kowalski${at}gmail.com"

git_q() { git -c user.name=Old -c user.email="old${at}example.com" -c commit.gpgsign=false "$@"; }

# A source repository with two commits: the first holds a file that is later deleted.
src="$tmp/src"
mkdir -p "$src/tools" "$src/docs"
cp "$root/tools/check-private.sh" "$src/tools/"
git -C "$src" init -q -b main
echo "left behind" >"$src/old-secret.txt"
git -C "$src" add -A && git_q -C "$src" commit -q -m "first

Co-Authored-By: Someone <someone${at}example.com>"
old_blob="$(git -C "$src" rev-parse HEAD:old-secret.txt)"
git -C "$src" rm -q old-secret.txt
echo "hello" >"$src/docs/readme.md"
printf '# header\nabc123:file:rule:1\n' >"$src/.gitleaksignore"
git -C "$src" add -A && git_q -C "$src" commit -q -m "second"

run() { (cd "$src" && PUBLISH_NAME=handle PUBLISH_EMAIL="$noreply" "$@"); }

# dry run: a folder with one commit by the given identity, no trailers, fingerprints gone
out="$(run "$publish" --dry-run HEAD 2>&1)" || fail "dry run failed: $out"
dir="$(printf '%s\n' "$out" | sed -n 's/.*read the tree in //p')"
if [ -d "$dir/.git" ]; then
  [ "$(git -C "$dir" rev-list --count HEAD)" = 1 ] || fail "dry run: not exactly one commit"
  [ "$(git -C "$dir" log -1 --format='%an <%ae> %cn <%ce>')" = "handle <$noreply> handle <$noreply>" ] || fail "dry run: wrong identity"
  [ "$(git -C "$dir" log -1 --format=%B | sed '/^$/d' | wc -l | tr -d ' ')" = 1 ] || fail "dry run: the message has more than one line"
  ! grep -q abc123 "$dir/.gitleaksignore" || fail "dry run: fingerprints kept"
  [ ! -e "$dir/old-secret.txt" ] || fail "dry run: a deleted file came back"
else
  fail "dry run left no folder"
fi

# real run: one commit on main, nothing of the old history
bare="$tmp/remote.git"
git init -q --bare "$bare"
out="$(run "$publish" HEAD "$bare" 2>&1)" || fail "publishing into an empty remote failed: $out"
[ "$(git -C "$bare" rev-list --count --all 2>/dev/null)" = 1 ] || fail "the remote doesn't have exactly one commit"
[ "$(git -C "$bare" for-each-ref --format='%(refname)')" = "refs/heads/main" ] || fail "the remote has refs other than main"
! git -C "$bare" cat-file -e "$old_blob" 2>/dev/null || fail "an object of the old history reached the remote"
! git -C "$bare" log --all --format='%an %ae %B' | grep -q -i 'old\|someone' || fail "the old author or trailer reached the remote"

# a remote that has refs is refused
if run "$publish" HEAD "$bare" >/dev/null 2>&1; then fail "a remote with refs was not refused"; fi

# refused: no remote without --dry-run, an address that isn't no-reply, an unknown ref
if run "$publish" HEAD >/dev/null 2>&1; then fail "a missing remote was not refused"; fi
if (cd "$src" && PUBLISH_NAME=handle PUBLISH_EMAIL="handle${at}example.com" "$publish" --dry-run HEAD >/dev/null 2>&1); then fail "an address that isn't no-reply was not refused"; fi
if run "$publish" --dry-run no-such-ref >/dev/null 2>&1; then fail "an unknown ref was not refused"; fi

# refused: the tree's own private-data check fails
echo "write to $real" >"$src/docs/contact.md"
git -C "$src" add -A && git_q -C "$src" commit -q -m "third"
empty="$tmp/empty.git"
git init -q --bare "$empty"
if run "$publish" HEAD "$empty" >/dev/null 2>&1; then fail "a tree with a real mail address was not refused"; fi
[ -z "$(git -C "$empty" for-each-ref)" ] || fail "a refused tree was pushed"
git -C "$src" rm -q docs/contact.md && git_q -C "$src" commit -q -m "fourth"

# refused: a term of the patterns file, in a file and in a file name; the term is never printed
printf '# personal terms\n\nKowalski\n' >"$tmp/patterns.txt"
echo "Thanks, KOWALSKI" >"$src/docs/thanks.md"
git -C "$src" add -A && git_q -C "$src" commit -q -m "fifth"
if out="$(cd "$src" && PUBLISH_NAME=handle PUBLISH_EMAIL="$noreply" PUBLISH_PATTERNS="$tmp/patterns.txt" "$publish" --dry-run HEAD 2>&1)"; then
  fail "a personal term in a file was not refused"
fi
printf '%s\n' "$out" | grep -q 'docs/thanks.md:1' || fail "the refusal doesn't say where the term is"
! printf '%s\n' "$out" | grep -q -i kowalski || fail "the refusal prints the term"
git -C "$src" mv docs/thanks.md docs/kowalski.md && echo "Thanks" >"$src/docs/kowalski.md"
git -C "$src" add -A && git_q -C "$src" commit -q -m "sixth"
if (cd "$src" && PUBLISH_NAME=handle PUBLISH_EMAIL="$noreply" PUBLISH_PATTERNS="$tmp/patterns.txt" "$publish" --dry-run HEAD >/dev/null 2>&1); then
  fail "a personal term in a file name was not refused"
fi
# … and the same tree passes without the patterns file
git -C "$src" mv docs/kowalski.md docs/thanks.md && git_q -C "$src" commit -q -m "seventh"
(cd "$src" && PUBLISH_NAME=handle PUBLISH_EMAIL="$noreply" PUBLISH_PATTERNS="$tmp/patterns.txt" "$publish" --dry-run HEAD >/dev/null 2>&1) || fail "a clean tree was refused with a patterns file"

# --history: every commit goes along, a private address becomes the no-reply one, names and
# messages stay, the fingerprints follow their commits, other refs stay behind
hist="$tmp/hist"
mkdir -p "$hist/tools"
cp "$root/tools/check-private.sh" "$hist/tools/"
git -C "$hist" init -q -b main
echo one >"$hist/a.txt"
git -C "$hist" add -A && git -C "$hist" -c user.name=Handle -c user.email="$noreply" -c commit.gpgsign=false commit -q -m "one"
echo two >"$hist/a.txt"
git -C "$hist" add -A && git -C "$hist" -c user.name=Jan -c user.email="$real" -c commit.gpgsign=false commit -q -m "two, by a private address"
private_commit="$(git -C "$hist" rev-parse HEAD)"
printf '# header\n%s:a.txt:rule:1\n' "$private_commit" >"$hist/.gitleaksignore"
git -C "$hist" add -A && git -C "$hist" -c user.name=Handle -c user.email="$noreply" -c commit.gpgsign=false commit -q -m "three"
git -C "$hist" tag v0.0.1
git -C "$hist" branch side HEAD~1
hbare="$tmp/hist.git"
git init -q --bare "$hbare"
out="$(cd "$hist" && PUBLISH_NAME=handle PUBLISH_EMAIL="$noreply" "$publish" --history HEAD "$hbare" 2>&1)" || fail "publishing with history failed: $out"
[ "$(git -C "$hbare" rev-list --count main 2>/dev/null)" = 4 ] || fail "history: expected the three commits and the fingerprint commit"
[ "$(git -C "$hbare" for-each-ref --format='%(refname)')" = "refs/heads/main" ] || fail "history: a tag or another branch reached the remote"
! git -C "$hbare" log main --format='%ae %ce' | grep -q -v 'users.noreply.github.com' || fail "history: a private address reached the remote"
git -C "$hbare" log main --format='%an|%s' | grep -q '^Jan|two, by a private address$' || fail "history: the author name or message changed"
! git -C "$hbare" cat-file -e "$private_commit" 2>/dev/null || fail "history: the commit with the private address reached the remote"
new_commit="$(git -C "$hbare" log main --format='%H %s' | sed -n 's/ two, by a private address$//p')"
git -C "$hbare" show main:.gitleaksignore | grep -q "^$new_commit:a.txt:rule:1$" || fail "history: the fingerprint doesn't name the rewritten commit"
[ "$(git -C "$hbare" show main:a.txt)" = two ] || fail "history: the tree changed"
# a term that only an old version of a file had is found
printf 'Kowalski\n' >"$tmp/patterns2.txt"
echo "by KOWALSKI" >"$hist/b.txt"
git -C "$hist" add -A && git -C "$hist" -c user.name=Handle -c user.email="$noreply" -c commit.gpgsign=false commit -q -m "four"
git -C "$hist" rm -q b.txt && git -C "$hist" -c user.name=Handle -c user.email="$noreply" -c commit.gpgsign=false commit -q -m "five"
if (cd "$hist" && PUBLISH_NAME=handle PUBLISH_EMAIL="$noreply" PUBLISH_PATTERNS="$tmp/patterns2.txt" "$publish" --dry-run --history HEAD >/dev/null 2>&1); then
  fail "history: a personal term in a deleted file was not refused"
fi
(cd "$hist" && PUBLISH_NAME=handle PUBLISH_EMAIL="$noreply" PUBLISH_PATTERNS="$tmp/patterns2.txt" "$publish" --dry-run HEAD >/dev/null 2>&1) || fail "the same tree without its history was refused"

[ "$fails" -eq 0 ] || { echo "publish-public-test: $fails failed" >&2; exit 1; }
echo "publish-public-test: ok"
