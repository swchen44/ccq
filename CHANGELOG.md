# Changelog

All notable changes to ccq are documented here. Format follows
[Keep a Changelog](https://keepachangelog.com/); versions follow [SemVer](https://semver.org/).

## [Unreleased]
### Fixed
- `explore` now computes callees with the same body-scan + fn-pointer logic as the
  standalone `callees` command (it was still using clangd's unreliable `outgoingCalls`
  and under-reporting — e.g. an fn-pointer dispatcher showed 0 callees).
- `def` / `explore` / `callees` now show/scan the **definition** (the `.c` body), not a
  header **prototype**: clangd's go-to-definition can jump from the definition to the
  declaration, which made `explore lookupCommand` show the prototype and report 0 callees.
  They now use `symbolRange` (source-file definition preferred).

## [0.5.0] — 2026-06-28
### Added
- **`--incremental` (opt-in lazy indexing)** — on a warm daemon restart with a
  persisted clangd index, open *only* git-changed files (plus one anchor) and let
  the static index answer the rest; the query path opens target files on demand.
  ~2.4× faster cold start on redis (25s → 10s) with identical results; bigger wins
  on larger repos. Off by default (full `OpenAll` stays the safe default).
### Fixed
- `symbols` line numbers (same flat-`SymbolInformation` parsing bug as `export`).

## [0.4.0] — 2026-06-28
### Added
- **fn-pointer override table** — `ccq.fnptr.json` (JSON, zero-dependency) lets you
  declare ground-truth associations the text scan can't infer: `registrations`
  (augment a struct.field's handlers) and `links` (direct dispatcher→handler, for
  callbacks / indirect dispatch). Merged with the automatic scan. `ccq fnptr` validates it.
- **`replace-body` / `insert-before` / `insert-after`** — symbol-level editing
  (Serena-parity): replace a symbol's whole definition or insert around it
  (dry-run by default, `--apply` to write).
- **git-diff warm restart** — the daemon detects clangd's persisted index and
  prioritises re-indexing files changed since the last index (shorter index wait).
### Fixed
- **`callees`** now unions clangd `outgoingCalls` with a function-body scan (verified
  against the symbol index) and fn-pointer dispatch targets — clangd's `outgoingCalls`
  alone was unreliable and often empty.
- **`export`** node line numbers and call-hierarchy positions: clangd returns flat
  `SymbolInformation` (`location.range`), which was being read as a hierarchical
  `range` (always 0). Now parsed correctly.

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

[Unreleased]: https://github.com/swchen44/ccq/compare/v0.5.0...HEAD
[0.5.0]: https://github.com/swchen44/ccq/releases/tag/v0.5.0
[0.4.0]: https://github.com/swchen44/ccq/releases/tag/v0.4.0
[0.3.0]: https://github.com/swchen44/ccq/releases/tag/v0.3.0
[0.2.0]: https://github.com/swchen44/ccq/releases/tag/v0.2.0
[0.1.0]: https://github.com/swchen44/ccq/releases/tag/v0.1.0
