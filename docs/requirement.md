# ccq — Requirements

## 1. Purpose

Give AI coding agents (Claude Code, Codex, OpenCode) and humans **compiler-accurate,
token-efficient** navigation and refactoring of C/C++ codebases, packaged as a single
zero-dependency binary that is trivial to deploy — including on locked-down / air-gapped
intranets.

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
| FR10 | Safe workspace-wide rename (`rename`, dry-run + `--apply`) |
| FR11 | Resolve **function-pointer dispatch** (ops-struct → handler) that clangd alone won't |
| FR12 | Export symbols + call graph as JSON/SQL for ad-hoc querying (`export`) |
| FR13 | Detect or generate a compile database (`init`): CMake/Meson/bear, or no-build `compile_flags.txt` |
| FR14 | Warm daemon for sub-second repeated queries; `status`/`shutdown` |
| FR15 | Ship an agent skill (`SKILL.md`) usable by Claude Code / Codex / OpenCode |

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
- Full accuracy needs a compile database; without a build, no-build mode trades accuracy
  (`#ifdef` over-inclusion, missing `-D`) for breadth.

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
