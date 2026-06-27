#!/usr/bin/env bash
# ccq installer (macOS/Linux): place binary on PATH, install agent skill.
# Works from a source checkout (builds) OR a release archive (prebuilt ccq).
set -euo pipefail
cd "$(dirname "$0")"
BIN="${PREFIX:-$HOME/.local/bin}"; mkdir -p "$BIN"
if [ -f ./ccq ]; then
  echo "[ccq] using prebuilt binary"
else
  echo "[ccq] building (zero-dependency Go build, works offline)..."
  go build -o ccq ./cmd/ccq
fi
cp ccq "$BIN/ccq"; echo "[ccq] installed -> $BIN/ccq (ensure $BIN is on PATH)"
# bundled clangd (from build-release.sh --bundle-clangd), if present
if [ -f ./clangd ]; then
  cp ./clangd "$BIN/clangd"; chmod +x "$BIN/clangd"
  echo "[ccq] bundled clangd -> $BIN/clangd (ccq auto-finds it next to itself)"
fi
# install skill for Claude Code
SK="$HOME/.claude/skills/ccq"; mkdir -p "$SK"; cp SKILL.md "$SK/SKILL.md"
echo "[ccq] skill -> $SK/SKILL.md"
command -v clangd >/dev/null 2>&1 || [ -f "$BIN/clangd" ] && echo "[ccq] clangd: ok" || \
  echo "[ccq] WARNING: clangd not found — install LLVM/clangd, or use --clangd <path>"
echo "[ccq] done. Try: ccq init && ccq explore main"
