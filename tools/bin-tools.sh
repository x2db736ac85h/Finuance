#!/usr/bin/env bash
# Installs pinned, checksum-verified binaries into backend/bin/ (gitignored).
# Usage: tools/bin-tools.sh sqlc|golangci-lint|trivy|actionlint|zizmor|osv-scanner
# Update a version: change it here and the four checksums. They come from each release's checksum
# file; sqlc publishes none and zizmor none as a file, so theirs were computed from the release
# archives when the version was pinned (zizmor's match the digests GitHub shows for its assets).
# CI installs the scanners the same way (PLAN task 22.5: no scanner image, no Docker socket).
set -euo pipefail

SQLC_VERSION=1.31.1
GOLANGCI_VERSION=2.15.0
TRIVY_VERSION=0.76.2
ACTIONLINT_VERSION=1.7.12
ZIZMOR_VERSION=1.32.0
OSV_SCANNER_VERSION=2.7.1

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
    trivy:linux_amd64)           echo bc0fc726e017b8927a62b2d3531d7a84ee01769daf5c99ed866dced54089fdbf ;;
    trivy:linux_arm64)           echo ab9d51f86e6a5fb936f605faff6b593ac51bc378fff4d3e8b9d58358b9c367c3 ;;
    trivy:darwin_amd64)          echo e08bb9ea449ecf66c6bfb03cbb8697c503ff33d35fc585c9747e3eaeb7ed4c7b ;;
    trivy:darwin_arm64)          echo ae7ea358326960406b703c77b2ac490190797d80b306b5b50c2a27221da0a5b8 ;;
    actionlint:linux_amd64)      echo 8aca8db96f1b94770f1b0d72b6dddcb1ebb8123cb3712530b08cc387b349a3d8 ;;
    actionlint:linux_arm64)      echo 325e971b6ba9bfa504672e29be93c24981eeb1c07576d730e9f7c8805afff0c6 ;;
    actionlint:darwin_amd64)     echo 5b44c3bc2255115c9b69e30efc0fecdf498fdb63c5d58e17084fd5f16324c644 ;;
    actionlint:darwin_arm64)     echo aba9ced2dee8d27fecca3dc7feb1a7f9a52caefa1eb46f3271ea66b6e0e6953f ;;
    zizmor:linux_amd64)          echo bb110cc765eca9cd7efb2e284a956be5974c60027b0970f16397d86208a9957b ;;
    zizmor:linux_arm64)          echo cb152b207f815116d7fa1c1ee4614f61578803351395052908095e79adfd9fd0 ;;
    zizmor:darwin_amd64)         echo 6983143c719ada6845a69cc4dd2bacc215f971662fecd8bbd2678388e5ce5969 ;;
    zizmor:darwin_arm64)         echo 6efed18c4539a663fd3c851a2be9533fd8456c4e646d9416206da059154a0d3c ;;
    osv-scanner:linux_amd64)     echo fb7dab19a8bd9bbe5c3189f09d4b7ee1820c9553a23bac202648a56efd2f6ee8 ;;
    osv-scanner:linux_arm64)     echo 983d22fbad9724514e3fcc6acc83b5fa64c5ffa6b0d9f510028f22c1b223ff81 ;;
    osv-scanner:darwin_amd64)    echo b8420d5485ba792626315f1bf9a5a2046e971d38eea75bf9090c38852dbc3b1b ;;
    osv-scanner:darwin_arm64)    echo b22c02877a811c8f78956e8643efa554932b7378f590d13f8e97d4e9448a29f3 ;;
    *) echo "bin-tools: no pinned checksum for $1 on $plat" >&2; exit 1 ;;
  esac
}

tool="${1:?usage: bin-tools.sh sqlc|golangci-lint|trivy|actionlint|zizmor|osv-scanner}"
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
  trivy)
    case "$plat" in linux_amd64) p=Linux-64bit ;; linux_arm64) p=Linux-ARM64 ;; darwin_amd64) p=macOS-64bit ;; darwin_arm64) p=macOS-ARM64 ;; *) p=$plat ;; esac
    url="https://github.com/aquasecurity/trivy/releases/download/v${TRIVY_VERSION}/trivy_${TRIVY_VERSION}_${p}.tar.gz"
    member="trivy" ;;
  actionlint)
    url="https://github.com/rhysd/actionlint/releases/download/v${ACTIONLINT_VERSION}/actionlint_${ACTIONLINT_VERSION}_${plat}.tar.gz"
    member="actionlint" ;;
  zizmor)
    case "$plat" in linux_amd64) p=x86_64-unknown-linux-gnu ;; linux_arm64) p=aarch64-unknown-linux-gnu ;; darwin_amd64) p=x86_64-apple-darwin ;; darwin_arm64) p=aarch64-apple-darwin ;; *) p=$plat ;; esac
    url="https://github.com/zizmorcore/zizmor/releases/download/v${ZIZMOR_VERSION}/zizmor-${p}.tar.gz"
    member="zizmor" ;;
  osv-scanner)
    url="https://github.com/google/osv-scanner/releases/download/v${OSV_SCANNER_VERSION}/osv-scanner_${plat}"
    member="" ;; # a plain binary, not an archive
  *) echo "bin-tools: unknown tool $tool" >&2; exit 1 ;;
esac

curl -sSfL -o "$tmp/archive.tar.gz" "$url"
want="$(checksum "$tool")"
if command -v sha256sum >/dev/null; then got="$(sha256sum "$tmp/archive.tar.gz" | cut -d' ' -f1)"; else got="$(shasum -a 256 "$tmp/archive.tar.gz" | cut -d' ' -f1)"; fi
if [ "$got" != "$want" ]; then echo "bin-tools: checksum mismatch for $tool ($got != $want)" >&2; exit 1; fi
if [ -n "$member" ]; then
  tar -xzf "$tmp/archive.tar.gz" -C "$tmp" "$member"
  install -m 0755 "$tmp/$member" "$bin/$tool"
else
  install -m 0755 "$tmp/archive.tar.gz" "$bin/$tool"
fi
echo "bin-tools: installed $tool to backend/bin/$tool"
