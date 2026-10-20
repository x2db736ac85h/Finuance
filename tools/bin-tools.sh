#!/usr/bin/env bash
# Installs pinned, checksum-verified binaries into backend/bin/ (gitignored).
# Usage: tools/bin-tools.sh sqlc|golangci-lint
# Update a version: change it here and the four checksums (sqlc publishes none; they were computed
# from the release archives when the version was pinned).
set -euo pipefail

SQLC_VERSION=1.31.1
GOLANGCI_VERSION=2.15.0

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
case "$arch" in x86_64) arch=amd64 ;; aarch64|arm64) arch=arm64 ;; esac
plat="${os}_${arch}"

checksum() {
  case "$1:$plat" in
    sqlc:linux_amd64)            echo 497ae4fcdfa64c5b0c311ffe4c2bd991e43991e82e5367792ed78bc2dca27354 ;;
    sqlc:linux_arm64)            echo b7cae247740d0c51a1e657479e5b2d21e6fef428f596682a01bc55bf4ab8a23d ;;
    sqlc:darwin_amd64)           echo c5af76772e3785d21663a62697056b383f07629979b1bd25b93872e73dbd519b ;;
    sqlc:darwin_arm64)           echo 21602158c99eb1f2bae197a66abfb1941d1e9e50b23125bb193349c6b1acc71e ;;
    golangci-lint:linux_amd64)   echo 9b7fe6a6bf6334b98decf8d86b11415ea7889afdabdd1959fdce23af2a9ebcea ;;
    golangci-lint:linux_arm64)   echo b4d8f17068b35ee6060cc2f69a16fb48740d706fbba81661c318649c114a6998 ;;
    golangci-lint:darwin_amd64)  echo 767b899a28285e88ded2823d5dcc4bc5ef681173eb2d80f029a7b17ee4ece3b8 ;;
    golangci-lint:darwin_arm64)  echo d1e409481a8ca846c4bc97e781eb5cb1bd5c23353a0b3977a1cbda1b1b6447d5 ;;
    *) echo "bin-tools: no pinned checksum for $1 on $plat" >&2; exit 1 ;;
  esac
}

tool="${1:?usage: bin-tools.sh sqlc|golangci-lint}"
root="$(cd "$(dirname "$0")/.." && pwd)"
bin="$root/backend/bin"
mkdir -p "$bin"
tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT

case "$tool" in
  sqlc)
    url="https://github.com/sqlc-dev/sqlc/releases/download/v${SQLC_VERSION}/sqlc_${SQLC_VERSION}_${plat}.tar.gz"
    member="sqlc" ;;
  golangci-lint)
    url="https://github.com/golangci/golangci-lint/releases/download/v${GOLANGCI_VERSION}/golangci-lint-${GOLANGCI_VERSION}-${os}-${arch}.tar.gz"
    member="golangci-lint-${GOLANGCI_VERSION}-${os}-${arch}/golangci-lint" ;;
  *) echo "bin-tools: unknown tool $tool" >&2; exit 1 ;;
esac

curl -sSfL -o "$tmp/archive.tar.gz" "$url"
want="$(checksum "$tool")"
if command -v sha256sum >/dev/null; then got="$(sha256sum "$tmp/archive.tar.gz" | cut -d' ' -f1)"; else got="$(shasum -a 256 "$tmp/archive.tar.gz" | cut -d' ' -f1)"; fi
if [ "$got" != "$want" ]; then echo "bin-tools: checksum mismatch for $tool ($got != $want)" >&2; exit 1; fi
tar -xzf "$tmp/archive.tar.gz" -C "$tmp" "$member"
install -m 0755 "$tmp/$member" "$bin/$tool"
echo "bin-tools: installed $tool to backend/bin/$tool"
