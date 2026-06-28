# Changelog

All notable changes to ccq are documented here. Format follows
[Keep a Changelog](https://keepachangelog.com/); versions follow [SemVer](https://semver.org/).

## [Unreleased]
### Added
- **`ccq.json` settings + `--config`** — a project/user config (`./ccq.json` >
  `~/.config/ccq/ccq.json` > `--config <path>`) with `allow`/`deny` **regex index filters**
  applied globally (OpenAll, fn-pointer scan, export). `ccq config` shows the effective settings.
  Distinct filters get distinct warm daemons.
- **`ccq wait-index`** — block until the index is ready, so an agent can wait before querying
  (avoids partial results). Reports the mode + file count. `--background` returns at once (poll
  `ccq status`); `--rebuild` forces a fresh index (deletes `<root>/.cache/clangd` — **shared with
  editor clangd, warned**). `ccq status` now reports `ready` / `indexing…` / `not running`.
- **`ccq cache list|clean|path`** — inspect and clean ccq's caches (daemon state, staged compile
  DBs, and each project's `.cache/clangd`), with sizes/dates/project paths. `clean --all|--project
  p|--older-than N`; `--index` also clears clangd's index (**shared with VS Code, warned**).
- **`ccq doctor`** — dump versions (ccq, clangd), the effective config (+ regex errors), the
  compile-database mode/entry count, cache sizes, and daemon state — with fix-it hints.
- **`--compdb a.json,b.json`** — use compile databases of **any name**, and **merge** several
  (multi-target builds emit several `compile_commands.json`, often renamed). The compile DB is
  decoupled from the source root (`-p`), and the warm daemon is keyed by `(root, compdb set)` so
  distinct configs get **distinct warm clangds** — switch build configs with no re-index. See
  [docs/design.md §6](docs/design.md) (incl. the cost of running a clangd per config).
### Fixed
- **No-build / degraded-mode warning was hidden in the default daemon path** — it only printed
  with `--no-daemon`, so an intranet user in `compile_flags.txt` (no-build) mode never saw that
  accuracy was reduced. The warning now prints for every query (daemon and inline). (Found while
  writing the intranet-no-build case study.)
- **Warm daemon served stale results after `--apply` edits** — `rename`/`replace-body`/`insert`
  wrote files on disk but didn't tell clangd, so the next query on the same daemon missed the new
  symbol (e.g. `callers <newName>` returned `(none)`, `replace-body <newName>` said "not found").
  After an apply, ccq now re-syncs clangd (`textDocument/didChange`) for the changed files and drops
  the fn-pointer cache. (Found while writing the safe-refactor case study.)
### Added
- **`ccq export --format html`** — emit a self-contained, offline, zero-dependency
  interactive knowledge graph (vanilla-JS force-directed SVG; no CDN). Same idea as
  CodeGraph's shared HTML graph, driven by the clangd-grade call graph.
- **`ccq export --focus <sym> [-d N]`** — build just a neighborhood (BFS over
  callers + callees to depth N) instead of the whole repo; fast on large trees and
  the recommended path for `--format html`. (Whole-repo export stays for json/sql.)
### Performance
- `fnptr.build` is cached per project root, so repeated `callers`/`callees`/`explore`
  on a warm daemon no longer rescan the whole repo each query (warm `explore` on
  redis ≈ 0.85s).
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
