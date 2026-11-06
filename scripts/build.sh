#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

VERSION="${VERSION:-dev}"
OUT_DIR="${OUT_DIR:-dist}"
GO="${GO:-go}"

if [[ "${1:-}" == "--all" ]]; then
  TARGETS=("windows/amd64" "linux/amd64" "darwin/arm64")
else
  TARGETS=("$(go env GOOS)/$(go env GOARCH)")
fi

mkdir -p "$OUT_DIR"
export CGO_ENABLED=0

for target in "${TARGETS[@]}"; do
  os="${target%/*}"
  arch="${target#*/}"
  ext=""
  if [[ "$os" == "windows" ]]; then ext=".exe"; fi
  out="$OUT_DIR/jptranslate-$os-$arch$ext"
  echo "building $out"
  GOOS="$os" GOARCH="$arch" "$GO" build -trimpath -buildvcs=false \
    -ldflags "-s -w -X main.version=$VERSION" \
    -o "$out" ./cmd/jptranslate
done
