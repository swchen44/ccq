#!/usr/bin/env bash
# build-release.sh — cross-compile ccq for all platforms into dist/, package
# each with SKILL.md + README + installer, and write SHA256SUMS.
# Usage: ./build-release.sh [version] [--bundle-clangd]
#   --bundle-clangd  also download a matching clangd binary into each archive
#                    (from clangd/clangd releases) so recipients need nothing.
#                    Override version with CLANGD_VER=18.1.3.
set -euo pipefail
cd "$(dirname "$0")"

VER=""
BUNDLE=0
for a in "$@"; do
  case "$a" in
    --bundle-clangd) BUNDLE=1 ;;
    *) VER="$a" ;;
  esac
done
[ -n "$VER" ] || VER="$(git describe --tags --always 2>/dev/null || echo dev)"
CLANGD_VER="${CLANGD_VER:-18.1.3}"
OUT="dist"
rm -rf "$OUT"; mkdir -p "$OUT"

# clangd_platform GOOS GOARCH -> clangd/clangd asset platform ("" if none)
clangd_platform() {
  case "$1/$2" in
    darwin/*)      echo mac ;;
    linux/amd64)   echo linux ;;
    windows/amd64) echo windows ;;
    *)             echo "" ;;   # e.g. linux/arm64 has no prebuilt from clangd/clangd
  esac
}

# bundle_clangd <dest_dir> <GOOS> <GOARCH>
bundle_clangd() {
  local dest="$1" goos="$2" goarch="$3"
  local plat; plat="$(clangd_platform "$goos" "$goarch")"
  if [ -z "$plat" ]; then
    echo "    (no prebuilt clangd for $goos/$goarch — skipped; recipient installs clangd)"
    return
  fi
  local zip="clangd-$plat-$CLANGD_VER.zip"
  local url="https://github.com/clangd/clangd/releases/download/$CLANGD_VER/$zip"
  local tmp; tmp="$(mktemp -d)"
  echo "    bundling clangd $CLANGD_VER ($plat)"
  if ! curl -fsSL -o "$tmp/$zip" "$url"; then
    echo "    WARNING: could not download $url — skipped"; rm -rf "$tmp"; return
  fi
  ( cd "$tmp" && unzip -q "$zip" )
  local cbin; cbin="$(find "$tmp" -type f -name 'clangd' -o -name 'clangd.exe' | head -1)"
  if [ -n "$cbin" ]; then
    cp "$cbin" "$dest/$(basename "$cbin")"
    chmod +x "$dest/$(basename "$cbin")" 2>/dev/null || true
  fi
  rm -rf "$tmp"
}

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
  # -s -w strips symbols → smaller binary; CGO off → fully static single file.
  # -tags 'grammar_subset grammar_subset_c' links ONLY the tree-sitter C grammar
  # (for the opt-in --treesitter backend); without it gotreesitter's builtin set
  # would bloat the binary to ~28MB instead of ~7MB.
  CGO_ENABLED=0 GOOS=$GOOS GOARCH=$GOARCH \
    go build -trimpath -tags 'grammar_subset grammar_subset_c' -ldflags "-s -w" -o "$OUT/$bin" ./cmd/ccq

  pkg="$OUT/$name"
  mkdir -p "$pkg"
  mv "$OUT/$bin" "$pkg/$bin"
  cp skills/ccq/SKILL.md README.md LICENSE "$pkg/"  # flatten SKILL.md to archive root
  [ "$GOOS" = windows ] && cp install.ps1 "$pkg/" || cp install.sh "$pkg/"
  [ "$BUNDLE" = 1 ] && bundle_clangd "$pkg" "$GOOS" "$GOARCH"

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
