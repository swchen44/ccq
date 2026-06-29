#!/usr/bin/env node
// Thin launcher: find the prebuilt ccq binary for this platform (shipped as an
// optional, os/cpu-gated dependency) and exec it, passing through args + exit code.
// No download, no postinstall — npm installs only the matching platform package.
"use strict";
const { spawnSync } = require("child_process");

const platform = process.platform; // darwin | linux | win32
const arch = process.arch; // x64 | arm64
const pkg = `@swchen44/ccq-${platform}-${arch}`;
const binName = platform === "win32" ? "ccq.exe" : "ccq";

let binPath;
try {
  binPath = require.resolve(`${pkg}/bin/${binName}`);
} catch (_) {
  console.error(
    `[ccq] no prebuilt binary for ${platform}-${arch} (expected package ${pkg}).\n` +
      `[ccq] supported: darwin & linux (x64, arm64), win32 (x64).\n` +
      `[ccq] build from source instead: https://github.com/swchen44/ccq`
  );
  process.exit(1);
}

// Note: ccq still needs `clangd` on PATH (its one external runtime dependency).
const r = spawnSync(binPath, process.argv.slice(2), { stdio: "inherit" });
if (r.error) {
  console.error(r.error);
  process.exit(1);
}
process.exit(r.status === null ? 1 : r.status);
