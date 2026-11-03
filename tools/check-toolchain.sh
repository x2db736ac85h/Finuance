#!/usr/bin/env bash
# Fails when the Go version differs between backend/go.mod's toolchain (what CI uses through
# go-version-file), the repository's mise.toml (what local builds use) and Dockerfile.backend's build
# image, so local runs, CI and the images build with the same Go.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

gomod="$(sed -nE 's/^toolchain go([0-9.]+)$/\1/p' backend/go.mod)"
[ -n "$gomod" ] || gomod="$(sed -nE 's/^go ([0-9.]+)$/\1/p' backend/go.mod)"
mise="$(sed -nE 's/^go = "([0-9.]+)"$/\1/p' mise.toml)"
docker="$(sed -nE 's/^FROM .*golang:([0-9.]+)-.*$/\1/p' deploy/docker/Dockerfile.backend | head -1)"

if [ -z "$gomod" ] || [ -z "$mise" ] || [ -z "$docker" ]; then
  echo "check-toolchain: could not read the Go version (go.mod: '$gomod', mise.toml: '$mise', Dockerfile.backend: '$docker')" >&2
  exit 1
fi
if [ "$gomod" != "$mise" ] || [ "$gomod" != "$docker" ]; then
  echo "check-toolchain: Go versions differ: backend/go.mod $gomod, mise.toml $mise, Dockerfile.backend $docker" >&2
  exit 1
fi
echo "check-toolchain: ok (Go $gomod)"
