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

sha256_hex() {
  local line
  if command -v sha256sum >/dev/null 2>&1; then
    line="$(sha256sum "$1")" || return 1
  elif command -v shasum >/dev/null 2>&1; then
    line="$(shasum -a 256 "$1")" || return 1
  else
    echo 'sha256sum or shasum is required' >&2
    return 1
  fi
  printf '%s' "${line%% *}"
}

bash ./scripts/build.sh --all

stage_root="$(mktemp -d "${TMPDIR:-/tmp}/jptranslate-package.XXXXXX")"
trap 'rm -rf "$stage_root"' EXIT

archives=()
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
  archives+=("$archive")
  echo "created $archive"
done

{
  for archive in "${archives[@]}"; do
    hash="$(sha256_hex "$archive")" || exit 1
    printf '%s  %s\n' "$hash" "${archive##*/}"
  done
} > "$OUT_DIR/SHA256SUMS.txt"
echo "created $OUT_DIR/SHA256SUMS.txt"
