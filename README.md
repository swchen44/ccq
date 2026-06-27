# ccq — clangd-powered C/C++ code intelligence CLI for AI agents

`ccq` is a single-binary CLI that gives AI coding agents (Claude Code, Codex, OpenCode) and humans **compiler-accurate, token-efficient** navigation and refactoring of C/C++ codebases — by driving **clangd** under the hood and adding the few things clangd alone won't do.

It is built to match and beat the three popular "code knowledge" tools on their own turf, while staying a **zero-dependency Go binary** that is trivial to deploy on a locked-down/air-gapped intranet.

```
ccq callers lookupCommand      # who calls it (function-level, cross-file)
ccq explore processCommand     # source + callers + callees + blast-radius, one shot
ccq impact ssl_init -d 3        # transitive blast radius
ccq rename old_name new_name --apply   # safe symbol-level rename across the repo
```

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
- `callees <sym>` — what this calls
- `impact <sym> [-d N]` — transitive callers (blast radius)
- `explore <sym>` — **one shot**: source + callers + callees + blast-radius
- `symbols <file>` — file outline
- `macro <sym>` — macro expansion / signature (clangd hover)

**Edit (symbol-level, Serena-parity)**
- `rename <sym> <new> [--apply]` — safe workspace-wide rename (dry-run by default)

**Project**
- `init` — locate/generate `compile_commands.json` (CMake / Meson / bear) and warm clangd
- `status`, `version`

**Differentiator** — `callers`/`explore` resolve **function-pointer dispatch**: they parse `struct ops X = { .fn = handler }` registrations and `obj->fn()` dispatch sites to synthesize the `dispatcher → handler` edge (marked `fnptr`). This is what CodeGraph's synthesizer does and clangd does not.

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

## Status
v0.2 — navigation (search/def/refs/callers/callees/impact/explore/symbols/macro), fnptr heuristic, `rename` editing, and a **warm-clangd daemon** are working. The first query in a repo spawns the daemon (indexes once); subsequent queries are **sub-second** (e.g. redis `callers` 0.6s, `explore` 0.07s warm vs ~30s cold). `ccq status` / `ccq shutdown` manage it; `--no-daemon` runs inline. Roadmap: `replace-body`/`insert` edits, git-diff incremental, graph export.

## License
MIT. Reuses architecture ideas validated by `troberti/clangd-query` (MIT), `mpsm/mcp-cpp`, and `2015xli/clangd-graph-rag`.
