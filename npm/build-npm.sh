#!/usr/bin/env bash
# Build the npm packages for ccq (esbuild-style: one os/cpu-gated package per
# platform + a launcher package). Consumes the binaries that build-release.sh
# already produced under dist/.
#
#   ./build-release.sh v0.6.2           # builds dist/ccq-<os>-<arch>/ccq[.exe]
#   npm/build-npm.sh   v0.6.2           # -> npm/dist/<pkg>/ ready to `npm publish`
#
# It does NOT publish (that needs your npm credentials). See npm/PUBLISHING.md.
set -euo pipefail
cd "$(dirname "$0")/.."   # repo root

VER="${1:?usage: npm/build-npm.sh <version, e.g. 0.6.2 or v0.6.2>}"
VER="${VER#v}"            # strip leading v
SCOPE="@swchen44"
SRC="dist"               # build-release.sh output
OUT="npm/dist"
rm -rf "$OUT"; mkdir -p "$OUT"

# map: dist dir suffix (GOOS-GOARCH) -> node platform/arch + os/cpu
node_platform() { case "$1" in windows) echo win32;; *) echo "$1";; esac; }
node_arch()     { case "$1" in amd64) echo x64;; *) echo "$1";; esac; }

mains_deps=""
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
# Consume the release archives that build-release.sh produced (dist/ keeps only
# the .tar.gz/.zip, not the unpacked dirs), extracting each platform's binary.
shopt -s nullglob
for arch_file in "$SRC"/ccq-*.tar.gz "$SRC"/ccq-*.zip; do
  base="$(basename "$arch_file")"; base="${base%.tar.gz}"; base="${base%.zip}"  # ccq-darwin-arm64
  goos="$(echo "$base" | cut -d- -f2)"
  goarch="$(echo "$base" | cut -d- -f3)"
  nplat="$(node_platform "$goos")"
  narch="$(node_arch "$goarch")"
  bin="ccq"; [ "$goos" = windows ] && bin="ccq.exe"
  ex="$TMP/$base"; rm -rf "$ex"; mkdir -p "$ex"
  case "$arch_file" in
    *.tar.gz) tar xzf "$arch_file" -C "$ex" ;;
    *.zip)    unzip -qo "$arch_file" -d "$ex" ;;
  esac
  binsrc="$ex/$base/$bin"
  [ -f "$binsrc" ] || { echo "  skip $base (no $bin in archive)"; continue; }

  pkg="ccq-$nplat-$narch"                  # ccq-darwin-arm64
  pdir="$OUT/$pkg"
  mkdir -p "$pdir/bin"
  cp "$binsrc" "$pdir/bin/$bin"
  chmod +x "$pdir/bin/$bin"
  cat > "$pdir/package.json" <<JSON
{
  "name": "$SCOPE/$pkg",
  "version": "$VER",
  "description": "ccq prebuilt binary for $nplat-$narch",
  "homepage": "https://github.com/swchen44/ccq",
  "repository": { "type": "git", "url": "https://github.com/swchen44/ccq.git" },
  "license": "MIT",
  "os": ["$nplat"],
  "cpu": ["$narch"],
  "files": ["bin/$bin"]
}
JSON
  echo "  built $SCOPE/$pkg ($VER)"
  mains_deps="$mains_deps    \"$SCOPE/$pkg\": \"$VER\",\n"
done

# launcher package: copy + stamp version & optionalDependencies versions
ldir="$OUT/ccq"
mkdir -p "$ldir/bin"
cp npm/ccq/bin/ccq.js "$ldir/bin/ccq.js"
cp npm/ccq/README.md "$ldir/README.md"
deps="$(printf "%b" "$mains_deps" | sed '$ s/,$//')"   # drop trailing comma
cat > "$ldir/package.json" <<JSON
{
  "name": "$SCOPE/ccq",
  "version": "$VER",
  "description": "clangd-powered C/C++ code intelligence CLI for AI agents (zero-dependency Go binary)",
  "keywords": ["clangd", "c", "cpp", "call-graph", "lsp", "code-intelligence", "ai-agent", "cli"],
  "homepage": "https://github.com/swchen44/ccq",
  "repository": { "type": "git", "url": "https://github.com/swchen44/ccq.git" },
  "license": "MIT",
  "bin": { "ccq": "bin/ccq.js" },
  "files": ["bin/ccq.js"],
  "optionalDependencies": {
$deps
  }
}
JSON
echo "  built $SCOPE/ccq ($VER) launcher"
echo "done -> $OUT/ (publish platform packages first, then the launcher; see npm/PUBLISHING.md)"
