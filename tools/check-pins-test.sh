#!/usr/bin/env bash
# Tests tools/check-pins.sh on throwaway files: each reference that can be repointed is refused,
# and the pinned forms and the repository's own images pass. Part of `make check`.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/.github/workflows" "$tmp/deploy/docker" "$tmp/tools"
fails=0
sha=3d3c42e5aac5ba805825da76410c181273ba90b1
digest=sha256:$(printf 'a%.0s' $(seq 64))

# refused NAME FILE-RELATIVE-TO-TMP CONTENT
refused() {
  printf '%s\n' "$3" >"$tmp/$2"
  if "$root/tools/check-pins.sh" "$tmp/$2" >/dev/null 2>&1; then
    echo "check-pins-test: $1 was not refused" >&2
    fails=$((fails + 1))
  fi
}
# passes NAME FILE-RELATIVE-TO-TMP CONTENT
passes() {
  printf '%s\n' "$3" >"$tmp/$2"
  if ! out="$("$root/tools/check-pins.sh" "$tmp/$2" 2>&1)"; then
    echo "check-pins-test: $1 was refused: $out" >&2
    fails=$((fails + 1))
  fi
}

refused "an action by tag" .github/workflows/a.yml "      - uses: actions/checkout@v7.0.1"
refused "an action by a short SHA" .github/workflows/a.yml "      - uses: actions/checkout@3d3c42e"
refused "an action by branch" .github/workflows/a.yml "        uses: owner/action/sub@main"
refused "an image by tag" deploy/a.yaml "    image: postgres:18.7-bookworm"
refused "a FROM by tag" deploy/docker/Dockerfile.x "FROM --platform=\$BUILDPLATFORM node:22-alpine AS build"
refused "an IMAGE= by tag" tools/a.sh "K6_IMAGE=grafana/k6:1.8.2"
refused "an IMAGE := by tag" tools/a.sh "K6_IMAGE := grafana/k6:1.8.2"
refused "a defaulted IMAGE by tag" tools/a.sh "NODE_IMAGE=\${NODE_IMAGE:-kindest/node:v1.33.1}"
refused "a workflow env image by tag" .github/workflows/a.yml "  TRIVY_IMAGE: aquasec/trivy:0.76.2"
refused "a docker run by tag" tools/a.sh "docker run --rm -v \"\$PWD:/repo:ro\" aquasec/trivy:0.76.2 fs ."

passes "an action by SHA" .github/workflows/b.yml "      - uses: actions/checkout@$sha # v7.0.1"
passes "a local action" .github/workflows/b.yml "      - uses: ./.github/actions/setup"
passes "an image by digest" deploy/b.yaml "    image: postgres:18.7-bookworm@$digest"
passes "a FROM by digest" deploy/docker/Dockerfile.y "FROM node:22.23.5-alpine@$digest AS build"
passes "a FROM of an earlier stage" deploy/docker/Dockerfile.y "FROM build AS llmmock-build"
passes "an IMAGE= by digest" tools/b.sh "K6_IMAGE=grafana/k6:1.8.2@$digest"
passes "the repository's own images" deploy/b.yaml "    image: finuance-backend:dev
    image: ghcr.io/x2db736ac85h/finuance-api:\${FINUANCE_VERSION:?set it}
API_IMAGE=ghcr.io/x2db736ac85h/finuance-api:rehearsal
MOCK_IMAGE=finuance-llmmock:rehearsal"
passes "templates and variables" deploy/b.yaml "          image: {{ include \"finuance.image\" . }}
    image: \"\$API_IMAGE\""
passes "a docker run of an own image and a port" tools/b.sh "docker run -d -p 127.0.0.1:8001:8080 -v trivy-cache:/root/.cache finuance-web:dev"
passes "a comment" tools/b.sh "# FROM node:22-alpine and image: postgres:18 in a comment"

if [ "$fails" -gt 0 ]; then
  echo "check-pins-test: $fails case(s) failed" >&2
  exit 1
fi
echo "check-pins-test: ok"
