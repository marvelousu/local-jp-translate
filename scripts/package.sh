#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

VERSION="${VERSION:-dev}"
OUT_DIR="${OUT_DIR:-dist}"
GO="${GO:-go}"
mkdir -p "$OUT_DIR"
OUT_DIR="$(cd "$OUT_DIR" && pwd)"
export OUT_DIR
safe_version="$(printf '%s' "$VERSION" | sed 's/[^A-Za-z0-9._-]/-/g')"

bash ./scripts/build.sh --all

stage_root="$(mktemp -d "${TMPDIR:-/tmp}/jptranslate-package.XXXXXX")"
trap 'rm -rf "$stage_root"' EXIT

for target in windows/amd64 linux/amd64 darwin/arm64; do
  os="${target%/*}"
  arch="${target#*/}"
  name="local-jp-translate_${safe_version}_${os}_${arch}"
  bundle="$stage_root/$name"
  mkdir -p "$bundle/docs"

  ext=""
  if [[ "$os" == "windows" ]]; then ext=".exe"; fi
  cp "$OUT_DIR/jptranslate-$os-$arch$ext" "$bundle/jptranslate$ext"
  cp README.md LICENSE THIRD_PARTY_NOTICES.md "$bundle/"
  cp docs/API.md "$bundle/docs/"
  if [[ -d LICENSES ]]; then cp -R LICENSES "$bundle/"; fi
  if [[ "$os" == "windows" && -f scripts/setup.ps1 ]]; then
    mkdir -p "$bundle/scripts"
    cp scripts/setup.ps1 "$bundle/scripts/"
  fi

  if [[ "$os" == "windows" ]]; then
    archive="$OUT_DIR/$name.zip"
    rm -f "$archive"
    (cd "$stage_root" && zip -qr "$archive" "$name")
  else
    archive="$OUT_DIR/$name.tar.gz"
    rm -f "$archive"
    tar -czf "$archive" -C "$stage_root" "$name"
  fi
  echo "created $archive"
done
