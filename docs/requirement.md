# ccq — Requirements

## 1. Purpose

Give AI coding agents (Claude Code, Codex, OpenCode) and humans **compiler-accurate,
token-efficient** navigation and refactoring of C/C++ codebases, packaged as a single
zero-dependency binary that is trivial to deploy — including on locked-down / air-gapped
intranets.

## 1.1 Business case (why build & maintain it)

The decision-maker's question — *"why another code tool, and what does maintaining it buy me?"* —
is answered with a **measured** A/B, not assertion. Running the *same* Claude Code agent (same model,
same prompt) with vs without `ccq` on `$PATH`, reading actual token/cost from each run
([token-cost case study](case-studies/token-cost/README.md), N=3):

- **Cost:** **2.1–12.4× cheaper per query** (6.7× over the suite); call-graph questions save the most.
- **Tokens:** **1.8–7.9× fewer** per query. **Speed:** ~**6× faster** wall-clock.
- **Completion:** on a **no-build** function-pointer task the agent scored **0% without ccq, 100%
  with** — i.e. some questions are *silently answered wrong* without it (quality/risk, not just cost).

**ROI vs maintenance:** at an illustrative 50 engineers × 10 queries/day, token savings alone are
~$35k/yr (≈$175k on a frontier model) plus ~2,500 engineer-hours/yr; **even at one-tenth** of that it
still clears ~$3.5k/yr + 250 hrs. The thing being maintained is **one zero-dependency Go binary + one
skill file** built in CI — a small, fixed cost dwarfed by the per-query savings. **Half of target C
code is no-build** (firmware/drivers with no `compile_commands.json`), which is exactly where the
completion gap (fn-pointer dispatch) bites — so the no-build path is a first-class requirement, not an
afterthought. All ROI assumptions are explicit and adjustable in the case study.

## 2. Personas

| Persona | Need |
|---------|------|
| **AI coding agent** | answer "who calls X / what breaks if I change X / show me X" in **one** token-cheap call instead of many grep+Read |
| **Developer (human)** | fast, precise call graph and safe refactors at the symbol level |
| **Intranet / air-gapped user** | install without internet — either a prebuilt binary copied in, or an offline `go build` |

## 3. Functional requirements

| ID | Requirement |
|----|-------------|
| FR1 | Find symbols by name (`search`) |
| FR2 | Show a symbol's definition source (`def`/`show`) |
| FR3 | List all references (`refs`) |
| FR4 | List function-level callers (`callers`) — cross-file |
| FR5 | List callees (`callees`) |
| FR6 | Transitive blast radius (`impact`) |
| FR7 | One-shot context: source + callers + callees + blast-radius (`explore`) |
| FR8 | File outline (`symbols`) |
| FR9 | Macro expansion / signature (`macro`); macros searchable |
| FR10 | Symbol-level edits, dry-run + `--apply`: `rename` (workspace-wide), `replace-body`, `insert-before`/`insert-after` |
| FR11 | Resolve **function-pointer dispatch** (ops-struct → handler) that clangd alone won't; optional `ccq.fnptr.json` override table |
| FR12 | Export symbols + call graph as JSON/SQL **and a self-contained interactive HTML graph** (`export`); `--focus <sym>` for a neighborhood subgraph |
| FR13 | Detect or generate a compile database (`init`): CMake/Meson/bear, or no-build `compile_flags.txt` |
| FR14 | Warm daemon for sub-second repeated queries; `status`/`shutdown` |
| FR15 | Ship an agent skill (`SKILL.md`) usable by Claude Code / Codex / OpenCode |
| FR16 | Use compile databases of **any name** and **merge several** (multi-target builds) via `--compdb`; one warm clangd per config |
| FR17 | **Index filter** via `ccq.json` `allow`/`deny` regex (`--config`), applied globally and to the compile DB handed to clangd; `ccq config` shows it |
| FR18 | **Index-ready signal**: `wait-index` blocks until indexing is complete (`--background`, `--rebuild`); `status` reports `ready`/`indexing…`/`not running` |
| FR19 | **Inspect/clean caches** (`cache list/clean/path`): daemon state, staged DBs, and clangd's `.cache/clangd` (warns on editor sharing) |
| FR20 | **Diagnostics** (`doctor`): versions, config (+ regex errors), compile-DB mode, cache sizes, daemon state, with fix-it hints |
| FR21 | **Serve over MCP** (`mcp`, JSON-RPC/stdio), CodeGraph-compatible tools, zero extra deps |

## 4. Non-functional requirements

| ID | Requirement | Target |
|----|-------------|--------|
| NFR1 | **Zero third-party dependencies** | `go list -m all` = the module itself only |
| NFR2 | **Single static binary** | one file per platform, ~4 MB |
| NFR3 | **Cross-platform** | macOS, Linux, Windows (amd64 + arm64 where applicable) |
| NFR4 | **Offline / air-gapped install** | `go build` needs no network; prebuilt binary copyable |
| NFR5 | **Warm query latency** | sub-second on a mid-size repo (e.g. redis warm `callers` ~0.6s) |
| NFR6 | **Correctness on hard C** | macros, `#ifdef`, `typedef`, `_Generic`, fn-pointer dispatch |
| NFR7 | **Tested & linted** | unit + integration tests; `go vet` + golangci-lint clean |

## 5. Constraints

- **clangd is an external runtime dependency** (the engine). ccq ships zero Go deps but
  needs a `clangd` binary on PATH (or `--clangd <path>`, or bundled via release).
- Full accuracy needs a compile database; without a build, no-build mode trades accuracy for breadth:
  guessed `-I` over-inclusion, and missing `-D` makes clangd treat disabled-`#ifdef` code as inactive
  (not found) — a pure-text definition index backs `def`/`search` to recover those symbols.

## 6. Out of scope (by design)

- A full graph database + query language (Cypher) — `export` to SQLite/JSON covers the need
  at zero dependency cost.
- Semantic-similarity edges / embeddings — off-mission and would break zero-dependency.
- Languages other than C/C++ — that is clangd's domain; breadth across languages is what
  tree-sitter tools (e.g. cbm) are for.

## 7. Acceptance (selected)

- `ccq callers <fn>` returns function-level callers matching a clangd reference run.
- `ccq callers` resolves ops-struct dispatch (e.g. F6 fixture) that clangd alone misses.
- `ccq init` with no build system produces a working `compile_flags.txt` enabling cross-file
  queries.
- `go test ./...` green; `go test -tags integration ./...` green with clangd installed.
