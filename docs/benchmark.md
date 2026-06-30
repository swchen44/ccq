# ccq — Benchmark & Methodology (full, honest)

ccq's design follows a head-to-head benchmark of code-intelligence tools for C. This document
covers **everything**: features, call-graph accuracy, **indexing speed**, **installation &
dependencies**, **usage / integration**, and — separated explicitly — **each tool's strengths
and weaknesses, including ccq's own**. Full harness, fixtures and raw numbers live in the
companion repo **[`swchen44/cbm-vs-codegraph-bench`](https://github.com/swchen44/cbm-vs-codegraph-bench)**
(`REPORT.md`, `results/timing.md`, `results/ccq/ccq-bench.md`).

> TL;DR — ccq is the **most accurate** (only tool passing all 8 hard-C features) and has the
> **fastest repeated queries** (warm daemon) and the **lightest binary** (zero-dependency Go), but
> it is the **slowest to index the first time** (it needs a real build + clangd background index)
> and **depends on a `clangd` binary**. It trades cold-start cost for accuracy, and mitigates the
> cost with the daemon and `--incremental`.

## 1. Tools compared

| Tool | Approach | Protocol (how an agent uses it) | Language / runtime |
|------|----------|----------------------------------|--------------------|
| **codebase-memory-mcp (cbm)** | tree-sitter → SQLite graph, Cypher, macro nodes | MCP server | C (vendored deps) |
| **CodeGraph** | tree-sitter → graph, `explore` one-shot, fn-pointer synthesis | MCP server | Node/TypeScript |
| **Serena** | LSP (clangd for C/C++) wrapped as MCP; navigation + editing | MCP server (+ CLI) | Python |
| **clangd + compile_commands.json** | compiler frontend (the accuracy ceiling) | LSP (in an editor) | C++ binary |
| **cscope / ctags / cflow** | text index; cscope/cflow build a call graph | CLI | tiny C binaries |
| **ccq** | clangd engine + fn-pointer heuristic + warm daemon + editing + export | **CLI + agent skill** (not MCP) | Go (zero deps) |

## 2. Subjects

- **wpa_supplicant** (~620 C/H files) — heavy function-pointer dispatch (`wpa_driver_ops` has 128
  fn-pointer fields) and conditional compilation (3,141 `#ifdef`, 174 `CONFIG_*`).
- **redis/src** (~216 C/H files) — clean medium codebase; canonical `lookupCommand` call graph.
- **ctest8** — a hand-written fixture exercising 8 hard C features (one ground-truth each).

## 3. Method

- Index each tool on the same machine (macOS Apple Silicon).
- **Ground truth** for "who calls X": `cscope -L3`, `cflow`, and `grep` for registration sites
  (`.field = handler`); manual reading for the fixtures.
- **Metrics**: function-level caller recall, the 8 C-feature pass/fail, **index/build time**,
  warm query latency, **install/dependency footprint**, and **integration protocol**.

## 4. Results

### 4.1 Feature coverage (8 hard-C features, ctest8)
| # | Feature | cbm | CodeGraph | clangd | **ccq** |
|---|---------|-----|-----------|--------|---------|
| F1 | tricky declarator | partial | partial | ✅ | ✅ |
| F2 | typedef-chain dispatch | ❌ | ❌ | ✅ | ✅ |
| F3 | X-macro / macro-generated fn | ❌ | ❌ | ✅ | ✅ |
| F4 | macro-body call | file-level | ❌ | ✅ | ✅ |
| F5 | cross-file static same-name | ✅ | ✅ | ✅ | ✅ |
| F6 | **fn-pointer dispatch** | ❌ | ✅ | ⚠️ | ✅ |
| F7 | `_Generic` | ❌ | ❌ | ✅ | ✅ |
| F8 | forward-decl dedup | ✅ | ✅ | ✅ | ✅ |
| **pass** | | 2 | 3 | 7 | **8 (only tool passing all)** |

### 4.2 Call-graph recall
| Query | cscope | cbm | CodeGraph | clangd | **ccq** | ground truth |
|-------|--------|-----|-----------|--------|---------|--------------|
| redis `lookupCommand` (direct calls) | **14** | 0 (file-level) | 13 | 13 | **13** | 13 |
| wpa `wext_scan` (**fn-pointer dispatch**) | 0 | 0 | 3/5 synth | not at runtime | **3/5 synth (5/5 with override table)** | 5 |

cscope finds direct calls superbly (and fast); **every purely-static tool, cscope included, misses
fn-pointer dispatch** — only CodeGraph's synthesizer (and ccq's port of it, plus ccq's
`ccq.fnptr.json` override) recovers it.

### 4.3 Indexing / build speed — **ccq's weakest dimension** ⚠️
Same machine; wpa_supplicant (620 files) and redis (216 files):

| Tool | wpa index | redis index | needs a build first? | builds what |
|------|-----------|-------------|----------------------|-------------|
| ctags | 0.08s | 0.04s | no | definition tags only |
| **cscope** | 0.08s | 0.53s | no | symbols + text-level call graph |
| cflow | — | 0.37s | no | call graph |
| cbm | 4.17s | 3.77s | no | semantic graph + macro/semantic nodes |
| CodeGraph | 14.02s | 11.09s | no | semantic graph + fnptr synthesis |
| **clangd / ccq** | **needs `bear -- make` (full compile, minutes) + background index (~30s)** | same | **YES** | full compiler AST |

**The honest tradeoff:** clangd (hence ccq) is the **slowest to get started** — it requires a
working `compile_commands.json` (a full build) and then a background index. tree-sitter tools
(cbm ~4s, CodeGraph ~11–14s) and traditional tools (cscope/ctags ~0.5s) are far faster to index
because they parse text and skip the compiler. What that buys clangd/ccq is the **most accurate**
result (macros expanded, `#ifdef` evaluated, `typedef`/`_Generic` resolved, true function-level
call graph).

**How ccq mitigates it:**
- **Warm daemon** — index once; later queries are sub-second (you don't re-pay the index).
- **`--incremental`** (v0.5, opt-in) — on a warm restart with a persisted index, open only
  git-changed files: redis cold start **25s → 10s (~2.4×)**, identical results.
- **No-build mode** — `compile_flags.txt` skips the build at the cost of accuracy: guessed `-I`
  over-included; no `-D`, so clangd treats disabled-`#ifdef` code as inactive (not found). The
  pure-text definition index backs `def`/`search` to recover those symbols.

### 4.4 Warm / repeated query latency
| | cbm | CodeGraph | Serena | **ccq** |
|--|-----|-----------|--------|---------|
| repeated query | per-run (re-reads its DB) | per-run | per-run | **~0.07–0.6s (warm daemon)** |

On redis, warm `ccq callers` ≈ 0.6s and warm `ccq explore` ≈ 0.07s, versus ~25s for a cold start.

### 4.5 Installation & dependency footprint — **intranet / air-gapped** (★)
A locked-down intranet must copy every package in by hand, so dependency complexity matters a lot.

| Tool | Footprint | Offline / intranet difficulty | Notes |
|------|-----------|-------------------------------|-------|
| cscope / ctags / cflow | single small binary (KB) | 🟢 easiest | distro-stock or single-file copy; zero external deps |
| **cbm** | 257 MB static binary | 🟡 medium | build deps fully **vendored in the repo** (sqlite/tree-sitter/yyjson/zstd/mimalloc) → **build needs no network**; only system clang/gcc. Copy the repo in and build. |
| **CodeGraph** | 188 MB bundled (ships Node) | 🟡 medium | single tarball with its own Node; official installer pulls from GitHub releases → bring the tarball in. |
| **clangd** | one large binary (~100–350 MB) | 🟡 medium | LLVM component, single file; **but still needs `bear`/CMake on a build-capable machine** to make `compile_commands.json`. |
| **Serena** | ~**890 uv/pip packages** + auto-downloads ~348 MB clangd + language servers | 🔴 hardest | intranet nightmare: mirror a PyPI subset, pre-stage clangd and language-server binaries. |
| **ccq** | **single Go binary, 0 third-party Go deps** (build needs no network) | 🟢 easy to ship, 🟡 needs clangd | copy one binary (or `go build` offline) + one `clangd` binary; `--clangd <path>` if not on PATH. |

**Takeaways:** traditional tools are the lightest; **ccq is the lightest of the "smart" tools to
ship** (one static binary, zero Go deps) but, like all clangd-based options, **needs a `clangd`
binary and ideally a compile DB**; **Serena is the worst fit for an intranet**.

### 4.6 Usage / integration
| | cbm | CodeGraph | Serena | clangd | **ccq** |
|--|-----|-----------|--------|--------|---------|
| How an agent calls it | MCP | MCP | MCP (+ CLI) | LSP (editor) | **CLI + agent skill** |
| One-shot "show me X" | — | `explore` | — | — | `ccq explore X` |
| Editing | — | — | ✅ (LSP edits) | ✅ rename | ✅ rename / replace-body / insert |
| Ad-hoc query language | Cypher | — | — | — | `ccq export --format sql \| sqlite3` |

ccq deliberately uses a **CLI + skill** instead of MCP: no server to keep alive, trivial to script,
and the only thing missing (an MCP wrapper) is easy to add if needed.

## 5. Strengths & weaknesses — per tool, separated

**cbm (codebase-memory-mcp)**
- ✅ Fast index (~4s), macro nodes, Cypher queries, self-contained vendored build (intranet-friendly).
- ⚠️ C call graph is **file-level** (tree-sitter C `function_definition` has no `name` field); misses fn-pointer dispatch; 257 MB binary.

**CodeGraph**
- ✅ `explore` one-shot, **fn-pointer synthesis** (the one thing it beats clangd on), zero-config `npm i -g`, RAM-light, single bundled tarball.
- ⚠️ Misses typedef-chain / X-macro / `_Generic` / macro-body calls; slowest tree-sitter index (~11–14s); ships a 188 MB Node bundle.

**Serena**
- ✅ Real LSP precision (clangd for C/C++) + editing, multi-language, mature MCP.
- ⚠️ **Heaviest install by far** (~890 Python pkgs + auto-downloads clangd) → worst for intranet; MCP startup cost.

**clangd + compile_commands.json**
- ✅ The **accuracy ceiling**: correct macros, `#ifdef`, `typedef`, `_Generic`, function-level call graph.
- ⚠️ Needs a **full build** to produce `compile_commands.json`; won't resolve runtime fn-pointer dispatch; meant for an editor, not an agent CLI.

**cscope / ctags / cflow**
- ✅ Astonishing CP value: cscope finds 14 direct callers of `lookupCommand` in **0.53s**, zero deps, no build.
- ⚠️ Text-level only: no macro expansion, no `#ifdef`, no fn-pointer dispatch, no `_Generic`.

**ccq — strengths** ✅
- Only tool passing **all 8** hard-C features; function-level call graph; fn-pointer dispatch **plus** a `ccq.fnptr.json` override table for the blind spots.
- **Fastest repeated queries** (warm daemon, sub-second) and `--incremental` warm restart.
- **Lightest smart-tool footprint**: single static Go binary, **zero Go dependencies**, offline build — best "smart" option for an intranet.
- Symbol-level **editing** (rename / replace-body / insert) and **graph export** (json/sql) as a zero-dependency Cypher substitute.

**ccq — weaknesses** ⚠️
- **Slowest first index**: needs `compile_commands.json` (a real build, minutes) + clangd background index (~30s) — far heavier than tree-sitter/traditional tools.
- **Depends on an external `clangd` binary** (and, for best accuracy, a compile DB).
- fn-pointer heuristic is an **over-approximation** and misses callbacks/indirect dispatch unless declared in the override table; `callees` body-scan can miss macro-hidden calls.
- **C/C++ only** (by design); primary interface is CLI + agent skill (an MCP server is also
  provided via `ccq mcp`, but ccq is not a long-running graph service like cbm/CodeGraph).

## 6. "Could you just fork CodeGraph and bolt clangd on?"

CodeGraph is the most-trusted name in this space, so it's tempting to fork it and add a
clangd backend rather than build ccq. We investigated it directly; here's what we found and
why we didn't.

| Check | Finding |
|-------|---------|
| **License** | MIT — forking/redistribution is fine (keep the notice). ✅ |
| **Architecture** | Extraction is one **5,689-line** `TreeSitterExtractor` (`src/extraction/tree-sitter.ts`); resolution/graph layers depend on tree-sitter node identity (`generateNodeId`; framework handlers reason in tree-sitter terms). **No backend abstraction.** ⚠️ |
| **compile_commands.json** | CodeGraph already reads it — but only in `import-resolver.ts` to harvest **include directories** for import heuristics, *not* to drive a compiler. A foothold, not a clangd engine. |
| **Runtime** | Node `>=20 <25` + `web-tree-sitter` + bundled `.wasm` grammars → the 188 MB Node bundle and intranet weight ccq avoids. |

**Conclusion:** "add clangd" to CodeGraph isn't a feature — it's an **engine transplant**: you'd
rewrite the extraction pipeline to reproduce tree-sitter's node-id scheme from clangd's LSP data,
keep the whole downstream working, carry permanent upstream-merge debt, *and* re-import the
Node/bundle cost. The endpoint of that work is, functionally, **ccq** (clangd engine + ported
fn-pointer synthesis + `explore`). So instead of forking, ccq:

- **ports** CodeGraph's `c-fnptr-synthesizer.ts` heuristic (provenance kept), and
- ships a **CodeGraph-compatible MCP server** (`ccq mcp`, headline tool `explore`) so anyone
  comfortable with CodeGraph can adopt ccq with no new dependencies and no relearning.

You get CodeGraph's fn-pointer win *on a compiler-grade engine*, without the tree-sitter accuracy
ceiling or the Node footprint. (CodeGraph passes 3/8 hard-C features and indexes redis in ~11s at
tree-sitter accuracy; ccq passes 8/8 on the same fixtures — see §4.1–4.2.)

## 7. Reproduce

```bash
git clone https://github.com/swchen44/cbm-vs-codegraph-bench
cd cbm-vs-codegraph-bench
./setup.sh                                   # installs the compared tools + ground-truth tools
bash bench/bench.sh repos/redis/src redis    # structural + question set
ccq callers lookupCommand -p repos/redis/src # ccq column
```

Raw data: `results/timing.md` (index speed), `results/ctest8/features8.md` (8 features),
`results/ccq/ccq-bench.md` (ccq runs), `REPORT.md` (full write-up incl. §9 intranet footprint).
