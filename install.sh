#!/usr/bin/env bash
# ccq installer (macOS/Linux): build, place binary on PATH, install agent skill.
set -euo pipefail
cd "$(dirname "$0")"
echo "[ccq] building (zero-dependency Go build, works offline)..."
go build -o ccq ./cmd/ccq
BIN="${PREFIX:-$HOME/.local/bin}"; mkdir -p "$BIN"
cp ccq "$BIN/ccq"; echo "[ccq] installed -> $BIN/ccq (ensure $BIN is on PATH)"
# install skill for Claude Code
SK="$HOME/.claude/skills/ccq"; mkdir -p "$SK"; cp SKILL.md "$SK/SKILL.md"
echo "[ccq] skill -> $SK/SKILL.md"
command -v clangd >/dev/null 2>&1 && echo "[ccq] clangd: $(command -v clangd)" || \
  echo "[ccq] WARNING: clangd not found — install LLVM/clangd, or use --clangd <path>"
echo "[ccq] done. Try: ccq init && ccq explore main"
