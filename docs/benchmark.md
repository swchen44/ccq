# ccq — Benchmark & Methodology

ccq's design follows a head-to-head benchmark of code-intelligence tools for C. This
documents how the benchmark was run and how ccq compares. Full harness, fixtures and raw
results live in the companion repo **`swchen44/cbm-vs-codegraph-bench`**.

## 1. Tools compared

| Tool | Approach |
|------|----------|
| **codebase-memory-mcp (cbm)** | tree-sitter → SQLite graph, MCP server, Cypher, macro nodes |
| **CodeGraph** | tree-sitter → graph, MCP, `explore` one-shot, fn-pointer synthesis |
| **Serena** | LSP (clangd for C/C++) wrapped as MCP; navigation + editing |
| **clangd + compile_commands.json** | compiler frontend, LSP |
| **traditional**: cscope / ctags / cflow | text index, call graph (cscope/cflow) |
| **ccq** | clangd engine + fn-pointer heuristic + warm daemon + editing + export |

## 2. Subjects

- **wpa_supplicant** (~620 C/H files) — heavy function-pointer dispatch (`wpa_driver_ops` has
  128 fn-pointer fields) and conditional compilation (3,141 `#ifdef`, 174 `CONFIG_*`).
- **redis/src** (~216 C/H files) — clean medium codebase; canonical `lookupCommand` call graph.
- **ctest8** — a hand-written fixture exercising 8 hard C features (one ground-truth each).

## 3. Method

- **Index each tool** on the subject (same machine).
- **Ground truth** for "who calls X": `cscope -L3`, `cflow`, and `grep` for registration sites
  (`.field = handler`); manual reading for fixtures.
- **Metrics**: function-level caller recall, the 8 C-feature pass/fail, index/query time,
  dependency footprint.
- **8 C features** (ctest8): tricky declarators, typedef-chain dispatch, X-macro / macro-
  generated functions, macro-body calls, cross-file `static` same-name, ops-struct dispatch
  (F6), `_Generic`, forward-declaration dedup.

## 4. Headline results

### 8 C-feature coverage
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

ccq inherits clangd's wins (F1–F5, F7, F8) **and** adds the fn-pointer heuristic for F6 —
the one feature CodeGraph beats clangd on.

### Call-graph recall (redis, `lookupCommand`)
| cbm | CodeGraph | clangd | ccq | ground truth |
|-----|-----------|--------|-----|--------------|
| 0 (file-level) | 13 | 13 | **13** | 13 |

### Speed & footprint
| | index / cold | warm repeated query | dependency footprint |
|--|--------------|---------------------|----------------------|
| cbm | ~3.8s | per-run | self-contained build |
| CodeGraph | ~11s | per-run | ~188 MB bundle |
| Serena | clangd + index | per-run | ~890 Python pkgs + downloads clangd |
| **ccq** | ~30s cold (spawn+index once) | **~0.07–0.6s (daemon)** | **single Go binary, 0 deps** |

## 5. Why ccq

- **cbm's** C call graph is file-level (root cause: tree-sitter C `function_definition` has no
  `name` field); ccq is function-level via clangd.
- **CodeGraph's** unique win is fn-pointer dispatch; ccq ports that heuristic (keyed by
  `(struct, field)`).
- **Serena's** value is LSP precision + editing; ccq is clangd-backed and adds `rename`.
- **clangd** alone won't resolve runtime fn-pointer dispatch; ccq's heuristic fills it.
- ccq adds a **warm daemon** for cbm-class speed and stays a **zero-dependency** single binary
  (best for intranet).

## 6. Reproduce

```bash
git clone https://github.com/swchen44/cbm-vs-codegraph-bench
cd cbm-vs-codegraph-bench
./setup.sh                                   # installs the compared tools + ground-truth tools
bash bench/bench.sh repos/redis/src redis    # structural + question set
# ccq column:
ccq callers lookupCommand -p repos/redis/src
```

See `results/ccq/ccq-bench.md` in that repo for the ccq comparison, and `REPORT.md` for the
full write-up.
