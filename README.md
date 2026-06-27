<!-- LANG-BAR -->
**English** · [繁體中文](README.zh-TW.md) · [简体中文](README.zh-CN.md)

# ccq — clangd-powered C/C++ code intelligence CLI for AI agents

`ccq` is a single-binary CLI that gives AI coding agents (Claude Code, Codex, OpenCode) and humans **compiler-accurate, token-efficient** navigation and refactoring of C/C++ codebases — by driving **clangd** under the hood and adding the few things clangd alone won't do.

It is built to match and beat the three popular "code knowledge" tools on their own turf, while staying a **zero-dependency Go binary** that is trivial to deploy on a locked-down/air-gapped intranet.

```
ccq callers lookupCommand      # who calls it (function-level, cross-file)
ccq explore processCommand     # source + callers + callees + blast-radius, one shot
ccq impact ssl_init -d 3        # transitive blast radius
ccq rename old_name new_name --apply   # safe symbol-level rename across the repo
```

## Motivation

AI coding agents understand C/C++ by spraying `grep` and `Read` — many tool calls, a lot of
tokens, and a call graph that text search fundamentally can't see (function pointers, macros,
`#ifdef`). A head-to-head benchmark ([`cbm-vs-codegraph-bench`](https://github.com/swchen44/cbm-vs-codegraph-bench))
found the most accurate engine for C is plain **clangd + `compile_commands.json`** — it wins
call graph, `#ifdef`, macros, `typedef`, `_Generic` — but it isn't packaged for agents, it's
slow to restart, and it won't resolve runtime function-pointer dispatch.

**ccq exists to package that winning engine for agents:** one token-cheap command per question,
a warm daemon for speed, a function-pointer heuristic for the one thing clangd misses,
symbol-level editing, and a zero-dependency single binary that drops onto a locked-down intranet.

## Why ccq (the pain points it solves)

| Tool it targets | Their pain point | How ccq solves it |
|-----------------|------------------|-------------------|
| **codebase-memory-mcp (cbm)** | agents burn tokens/tool-calls on grep+Read; grep can't trace call relationships; macros invisible; wants speed | clangd **function-level** call graph (cbm's C call graph is file-level); macro expansion; warm clangd; compact output |
| **CodeGraph** | agents want a **one-shot** answer; function-pointer dispatch is invisible to grep; index must stay fresh | `ccq explore` returns everything in one call; **fnptr dispatch heuristic** (the one thing CodeGraph beats clangd on); clangd auto-reindexes on change |
| **Serena** | agents need **symbol-level** navigation + editing, not text munging | clangd LSP: definition / references / **safe rename** |

**Design thesis:** ccq = clangd's compiler-grade correctness (wins call graph, `#ifdef`, macros, `typedef`, `_Generic`) **+** CodeGraph's `explore` and fn-pointer synthesis **+** Serena's symbol editing **+** cbm's speed (warm clangd). Each tool's best, stacked on clangd.

> Provenance: the design follows a head-to-head benchmark of cbm vs CodeGraph vs clangd vs traditional tools (cscope/ctags/cflow) on `wpa_supplicant` and `redis`. clangd + `compile_commands.json` won the call-graph, `#ifdef`, macro, `typedef` and `_Generic` dimensions; ccq packages that win and closes the one gap (fn-pointer dispatch).

## Features

**Navigate**
- `search <q>` — find symbols (workspace symbols)
- `def <sym>` / `show <sym>` — definition source
- `refs <sym>` — all references
- `callers <sym>` — who calls this (clangd call hierarchy **+ fnptr heuristic**)
- `callees <sym>` — what this calls (clangd + **body-scan** + fnptr dispatch targets)
- `impact <sym> [-d N]` — transitive callers (blast radius)
- `explore <sym>` — **one shot**: source + callers + callees + blast-radius
- `symbols <file>` — file outline
- `macro <sym>` — macro expansion / signature (clangd hover)

**Edit (symbol-level, Serena-parity; dry-run unless `--apply`)**
- `rename <sym> <new> [--apply]` — safe workspace-wide rename
- `replace-body <sym> <file> [--apply]` — replace a symbol's whole definition
- `insert-before <sym> <file>` / `insert-after <sym> <file>` — insert content around a symbol

**Export (query with your own tools)**
- `export [--format json|sql] [--out f]` — dump symbols + call graph (incl. fnptr edges). `ccq export --format sql | sqlite3 g.db` then query with plain SQL — a zero-dependency substitute for an in-tool query language.
- `fnptr` — validate the fn-pointer override table (`ccq.fnptr.json`)

**Project**
- `init` — locate/generate `compile_commands.json` (CMake / Meson / bear), **or a no-build `compile_flags.txt`** if there's no build system; warm clangd
- `status`, `shutdown`, `version`

**Differentiators**
- **fn-pointer dispatch** — `callers`/`explore` parse ops-struct registrations and `obj->fn()` dispatch to synthesize `dispatcher → handler` edges. Keyed by **(struct type, field)** so same-named fields in different structs don't cross-bleed; handles **designated init, positional tables `{"n", fn}`, and field←field propagation** (ported from CodeGraph's synthesizer). clangd alone won't do this.
- **fn-pointer override table** — for the blind spots the text scan can't infer (callbacks, indirect dispatch), drop a `ccq.fnptr.json` in the project root to declare ground truth, merged with the automatic scan (JSON, zero-dependency):
  ```json
  {
    "registrations": [ { "struct": "wpa_driver_ops", "field": "scan2", "handlers": ["wpa_driver_bsd_scan"] } ],
    "links":         [ { "from": "eloop_run", "to": ["wext_scan_timeout"], "note": "eloop timer callback" } ]
  }
  ```
  `registrations` augment a struct.field's handlers; `links` add direct `dispatcher → handler` edges. `ccq fnptr` validates the table.
- **No-build mode** — when there's no `compile_commands.json` and no build system, `ccq init` writes a `compile_flags.txt` (auto-discovered `-I` include dirs). clangd then resolves **cross-file** (with ccq's file priming) **without a build** — cbm-style breadth, at lower accuracy (`#ifdef` over-included, no `-D`). Accuracy ladder: compile_commands.json > compile_flags.txt > same-file.
- **Macros** — clangd indexes `#define`s; they appear in `ccq search` (kind `macro`) and `ccq macro` expands them.

## Install

ccq is a single static Go binary. **clangd is the only external dependency.**

### From source (recommended for intranet/air-gapped)
```bash
git clone https://github.com/swchen44/ccq && cd ccq
go build -o ccq ./cmd/ccq      # produces a single static binary, zero Go deps
sudo mv ccq /usr/local/bin/    # (Windows: put ccq.exe on PATH)
```
Because ccq has **no third-party Go modules**, `go build` needs no network — ideal for offline/intranet builds.

### clangd (required)
- macOS: `brew install llvm` (clangd in `$(brew --prefix llvm)/bin`), or Xcode's `clangd`.
- Linux: `apt install clangd` / `dnf install clang-tools-extra`.
- Windows: install LLVM (clangd.exe), or VS clangd component.
- **Air-gapped:** copy the single `clangd` binary for your platform from an LLVM release; point ccq at it with `--clangd /path/to/clangd`.

### Install the agent skill
Copy `SKILL.md` to your agent's skills directory:
- Claude Code: `~/.claude/skills/ccq/SKILL.md`
- OpenCode / Codex: the project/agent skills directory (see their docs).

Run `./install.sh` (macOS/Linux) or `install.ps1` (Windows) to build, place the binary, and install the skill.

## Cross-platform
ccq cross-compiles to a single binary on every platform:
```bash
GOOS=darwin  GOARCH=arm64 go build -o ccq-darwin-arm64 ./cmd/ccq
GOOS=windows GOARCH=amd64 go build -o ccq-windows-amd64.exe ./cmd/ccq
GOOS=linux   GOARCH=amd64 go build -o ccq-linux-amd64 ./cmd/ccq
```
macOS and Windows are both supported. (The optional warm-clangd daemon uses a Unix socket on macOS/Linux and a localhost TCP port on Windows.)

## Quick start
```bash
cd your-c-project
ccq init                       # generate compile_commands.json + warm clangd
ccq explore main               # see main: source + who calls it + what it calls
ccq callers some_handler       # function-level callers + fnptr dispatch
ccq rename old_api new_api      # preview a safe rename (add --apply to write)
```

## How it works
```
ccq (Go) ── LSP (JSON-RPC/stdio) ──► clangd ──► compile_commands.json + your code
   │                                   └─ macros expanded, #ifdef evaluated, types resolved
   └── fnptr heuristic (text): ops-struct designated-init → dispatch→handler edges
```
- `ccq init` finds `compile_commands.json` (or generates it via CMake/Meson/bear). With it, clangd is compiler-accurate; without it, clangd runs in degraded same-file mode (ccq warns).
- The first query in a cold repo waits for clangd to index (seconds); clangd caches the index on disk, so later queries are fast.

## Benchmark

ccq's design comes from a head-to-head benchmark of C code-intelligence tools (full harness:
[`swchen44/cbm-vs-codegraph-bench`](https://github.com/swchen44/cbm-vs-codegraph-bench);
methodology: [docs/benchmark.md](docs/benchmark.md)).

| Dimension | cbm | CodeGraph | clangd | Serena | **ccq** |
|-----------|-----|-----------|--------|--------|---------|
| Function-level call graph | ❌ file-level | ✅ | ✅ | ✅ | ✅ |
| fn-pointer dispatch (F6) | ❌ | ✅ | ⚠️ | ⚠️ | ✅ |
| 8 hard-C features passed | 2 | 3 | 7 | 7 | **8 (only one)** |
| redis `lookupCommand` callers | 0 | 13 | 13 | 13 | **13** |
| Warm repeated query | per-run | per-run | — | per-run | **~0.07–0.6s** |
| Symbol rename (editing) | ❌ | ❌ | — | ✅ | ✅ |
| Dependency footprint | self-build | ~188 MB | binary | ~890 pkgs | **single Go binary, 0 deps** |

**Summary:** ccq is the only tool passing all 8 hard-C features — it keeps clangd's wins
(`#ifdef`, macros, `typedef`, `_Generic`, function-level call graph) and adds the fn-pointer
heuristic (the one feature CodeGraph beats clangd on), a warm daemon for speed, and stays a
zero-dependency single binary.

## Limitations

ccq is deliberately a thin layer over clangd; it inherits clangd's strengths and a few honest limits.

- **Function-pointer heuristic (`fnptr`)** — text-based and intentionally *over-approximating*:
  it links a dispatcher to **all** handlers registered to that `(struct, field)` (candidates, not
  the single runtime target). It does **not** auto-resolve: callbacks passed as arguments then invoked
  elsewhere (`eloop_register_timeout(cb, …)` → later `e->cb()`), indirect receivers `(*p)->fn()`,
  array-indexed dispatch `arr[i]->fn()`, return-value dispatch `get_fn()()`, or fn-pointers stored
  in plain (non-struct) variables. Positional tables and multi-line registrations are best-effort.
  **Mitigation:** declare these in a [`ccq.fnptr.json`](#differentiators) override table.
- **Callees** — clangd's `outgoingCalls` is unreliable, so `callees` unions it with a function-body
  scan (call sites verified against the symbol index) and fn-pointer dispatch targets. Body-scan can
  still miss calls hidden behind macros.
- **Callback / event dispatch** — "register now, call later" flows (eloop/timer/signal) aren't
  resolved — a blind spot shared by all static tools (cscope, clangd included).
- **No-build mode accuracy** — `compile_flags.txt` gives cross-file reach without a build, but with
  guessed includes and no `-D`: `#ifdef` branches are over-included and macro-dependent code may be
  wrong. Use a real `compile_commands.json` for config-accurate results.
- **Cold start & scale** — the first query spawns the daemon and indexes the repo (~30s on redis);
  clangd's index uses RAM proportional to repo size. Warm queries are sub-second, and a warm restart
  re-indexes changed files first. (A full "open only changed files" mode is on the v0.5 roadmap.)
- **Dependencies / scope** — needs a `clangd` binary (the engine) and, for best accuracy, a compile
  database. **C/C++ only** by design (cross-language breadth is what tree-sitter tools like cbm are for).

## Release / distribution (giving ccq to others)

ccq is a single static binary per platform — distribution is just "ship the binary + SKILL.md". Recipients also need `clangd` (or pass `--clangd <path>`).

**Option A — build all platforms locally**
```bash
./build-release.sh v0.3.0
# → dist/ccq-{darwin,linux,windows}-{amd64,arm64}.{tar.gz,zip} + SHA256SUMS
```
Hand the matching archive to each user; they extract and run `./install.sh` (or `install.ps1`). Each archive bundles the binary, SKILL.md, README, LICENSE and the installer.

**Option B — automated GitHub Release (recommended)**
```bash
git tag v0.3.0 && git push origin v0.3.0
```
`.github/workflows/release.yml` cross-compiles all platforms and publishes a GitHub Release with the archives + checksums. Users then:
```bash
curl -fsSL -O https://github.com/swchen44/ccq/releases/download/v0.3.0/ccq-linux-amd64.tar.gz
tar xzf ccq-linux-amd64.tar.gz && cd ccq-linux-amd64 && ./install.sh
```

**Self-contained release (bundle clangd)** — make recipients need *nothing*:
```bash
./build-release.sh v0.3.0 --bundle-clangd     # CLANGD_VER=18.1.3 to override
```
Downloads a matching `clangd` (from `clangd/clangd` releases) into each archive next to `ccq`. ccq auto-uses a `clangd` sitting beside its own binary, and `install.sh` places both on PATH. (Adds ~100–350 MB per archive; `linux/arm64` has no prebuilt clangd from that source and is skipped — those users install clangd themselves.)

**Option C — intranet / air-gapped**
Build once (`go build -o ccq ./cmd/ccq`, no network needed), copy the single binary + `SKILL.md` + a platform `clangd` binary onto the target machine, put both on PATH, and point ccq at clangd with `--clangd` if needed.

**Recipient setup (any option)**
1. Put `ccq` (or `ccq.exe`) on PATH.
2. Copy `SKILL.md` to `~/.claude/skills/ccq/SKILL.md` (Claude Code) or the agent's skills dir.
3. Ensure `clangd` is installed (or `ccq --clangd /path/to/clangd ...`).
4. In a C/C++ repo: `ccq init` then `ccq explore main`.

## For Developers

> Full design & requirements: [docs/design.md](docs/design.md) ·
> [docs/requirement.md](docs/requirement.md) · [docs/benchmark.md](docs/benchmark.md)

### Setup & build
```bash
git clone https://github.com/swchen44/ccq && cd ccq
go build -o ccq ./cmd/ccq        # zero third-party deps; `go list -m all` = just this module
```
Requires Go 1.23+. No external Go modules → `go build` works fully offline.

### Project layout
```
cmd/ccq/          CLI entry, flag parsing, daemon-or-inline routing (+ integration tests)
internal/lsp/     LSP client driving clangd (JSON-RPC/stdio) + path/snippet helpers
internal/cmd/     subcommands (navigate / edit / export) on an lsp.Client
internal/fnptr/   function-pointer dispatch heuristic (pure text; the F6 differentiator)
internal/compdb/  locate/generate compile_commands.json or compile_flags.txt (no-build)
internal/daemon/  warm-clangd daemon + cross-platform IPC
docs/             design / requirement / benchmark
.github/workflows ci.yml (test+lint+build) · release.yml (tags) · nightly.yml (cron)
```

### Test & lint
```bash
make test              # unit tests (no clangd needed)
make test-integration  # end-to-end via real clangd (auto-skips if clangd absent)
make lint              # go vet + golangci-lint (if installed)
make fmt               # gofmt -w .
```
- **Unit tests** are zero-dependency (stdlib `testing`): `internal/fnptr` (cross-bleed /
  positional / field←field), `internal/compdb`, `internal/lsp`, `internal/cmd`.
- **Integration tests** are gated by `//go:build integration` and skip when `clangd` is not on
  PATH.
- CI (`.github/workflows/ci.yml`) runs gofmt check, `go vet`, golangci-lint, unit tests,
  integration tests (installs clangd), and cross-compiles all platforms on every push/PR.

### Release process & versions
- **Stable**: `git tag vX.Y.Z && git push origin vX.Y.Z` → `release.yml` builds all platforms
  and publishes a GitHub Release (SemVer; see `CHANGELOG.md`).
- **Nightly**: `nightly.yml` runs every night (18:00 UTC) and refreshes a rolling `nightly`
  prerelease with the latest `main` binaries for every platform. Manual: run the *nightly*
  workflow via "Run workflow".
- Local all-platform build: `make release` (`./build-release.sh`), add `--bundle-clangd` to
  embed clangd.

### Contributing
Keep it zero-dependency (stdlib only), `gofmt`-clean, and `go vet` / golangci-lint green; add a
unit test for new logic (and an integration test if it touches the clangd path).

## Version history

| Version | Date | Highlights |
|---------|------|-----------|
| [**0.4.0**](https://github.com/swchen44/ccq/releases/tag/v0.4.0) | 2026-06-28 | fn-pointer override table, `replace-body`/`insert`, callees body-scan fix, git-diff warm restart |
| [0.3.0](https://github.com/swchen44/ccq/releases/tag/v0.3.0) (first public release) | 2026-06-27 | fn-pointer upgrade (struct-keyed, positional, field←field), no-build mode, macro search, graph export |
| 0.2.0 (milestone) | 2026-06-26 | warm-clangd daemon (sub-second warm queries) |
| 0.1.0 (milestone) | 2026-06-26 | navigation + rename + fnptr heuristic |

Full notes: [CHANGELOG.md](CHANGELOG.md). Latest binaries: [Releases](https://github.com/swchen44/ccq/releases) (stable) · [nightly](https://github.com/swchen44/ccq/releases/tag/nightly).

## Roadmap / TODO

- [x] `callees` via function-body scan (clangd's `outgoingCalls` is unreliable) — *done in 0.4*
- [x] More editing: `replace-body`, `insert-before/after` (Serena parity) — *done in 0.4*
- [x] fn-pointer override table (`ccq.fnptr.json`) for blind spots — *done in 0.4*
- [ ] **v0.5: full git-diff incremental** — open *only* changed files on warm restart (needs an
      open-on-demand query path; 0.4 ships the safe version: changed-files-first + shorter index wait)
- [ ] More build systems (Bazel, xmake) for `ccq init`
- [ ] fn-pointer heuristic: positional-table edge cases, comment-aware multi-line registrations

## License
MIT. Reuses architecture ideas validated by `troberti/clangd-query` (MIT), `mpsm/mcp-cpp`, and `2015xli/clangd-graph-rag`.
