#!/usr/bin/env bash
# build-release.sh — cross-compile ccq for all platforms into dist/, package
# each with SKILL.md + README + installer, and write SHA256SUMS.
# Usage: ./build-release.sh [version]   (version defaults to `git describe`)
set -euo pipefail
cd "$(dirname "$0")"

VER="${1:-$(git describe --tags --always 2>/dev/null || echo dev)}"
OUT="dist"
rm -rf "$OUT"; mkdir -p "$OUT"

# os/arch matrix
TARGETS=(
  "darwin amd64"
  "darwin arm64"
  "linux amd64"
  "linux arm64"
  "windows amd64"
)

echo "[release] ccq $VER"
for t in "${TARGETS[@]}"; do
  set -- $t; GOOS=$1; GOARCH=$2
  name="ccq-$GOOS-$GOARCH"
  bin="ccq"; [ "$GOOS" = windows ] && bin="ccq.exe"
  echo "  building $name"
  # -s -w strips symbols → smaller binary; CGO off → fully static single file
  CGO_ENABLED=0 GOOS=$GOOS GOARCH=$GOARCH \
    go build -trimpath -ldflags "-s -w" -o "$OUT/$bin" ./cmd/ccq

  pkg="$OUT/$name"
  mkdir -p "$pkg"
  mv "$OUT/$bin" "$pkg/$bin"
  cp SKILL.md README.md LICENSE "$pkg/"
  [ "$GOOS" = windows ] && cp install.ps1 "$pkg/" || cp install.sh "$pkg/"

  if [ "$GOOS" = windows ]; then
    ( cd "$OUT" && zip -qr "$name.zip" "$name" )
  else
    ( cd "$OUT" && tar -czf "$name.tar.gz" "$name" )
  fi
  rm -rf "$pkg"
done

echo "[release] checksums"
( cd "$OUT" && shasum -a 256 *.tar.gz *.zip 2>/dev/null > SHA256SUMS || sha256sum *.tar.gz *.zip > SHA256SUMS )
ls -lh "$OUT"
echo "[release] done -> $OUT/ (upload these to a GitHub Release)"
