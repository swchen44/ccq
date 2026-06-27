# Changelog

All notable changes to ccq are documented here. Format follows
[Keep a Changelog](https://keepachangelog.com/); versions follow [SemVer](https://semver.org/).

## [0.3.0] — 2026-06-27 — first public release
### Added
- **fn-pointer dispatch (upgraded)** — keyed by `(struct type, field)` so same-named
  fields in different structs no longer cross-bleed; handles positional tables
  `{"n", fn}` and field←field propagation (ported from CodeGraph's synthesizer).
- **No-build mode** — `ccq init` generates a `compile_flags.txt` (auto-discovered
  `-I` dirs) when there's no build system; clangd resolves cross-file without a build.
- **Macro search** — `#define`s appear in `ccq search` (kind `macro`).
- **Graph export** — `ccq export --format json|sql`; pipe SQL to `sqlite3` and query
  with plain SQL (zero-dependency substitute for a query language).
- **Quality & docs** — unit + integration tests, golangci-lint, Makefile, CI/nightly/release
  workflows; design/requirement/benchmark docs; multi-language READMEs (en/zh-TW/zh-CN).

## [0.2.0] — 2026-06-26
### Added
- **Warm-clangd daemon** — first query spawns it; later queries are sub-second
  (redis `callers` ~0.6s, `explore` ~0.07s warm vs ~30s cold). Cross-platform IPC
  (Unix socket / localhost TCP). `ccq status` / `shutdown`; `--no-daemon` for inline.

## [0.1.0] — 2026-06-26
### Added
- Initial release: clangd-driven navigation (`search`/`def`/`refs`/`callers`/`callees`/
  `impact`/`explore`/`symbols`/`macro`), symbol-level `rename`, fn-pointer heuristic,
  `compile_commands.json` auto-detect (CMake/Meson/bear), agent SKILL.md.
  Single static Go binary, zero dependencies, cross-platform.

[Unreleased]: https://github.com/swchen44/ccq/compare/v0.3.0...HEAD
[0.3.0]: https://github.com/swchen44/ccq/releases/tag/v0.3.0
[0.2.0]: https://github.com/swchen44/ccq/releases/tag/v0.2.0
[0.1.0]: https://github.com/swchen44/ccq/releases/tag/v0.1.0
