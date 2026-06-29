# ccq — Case Studies

Real-codebase walkthroughs that show ccq in action — *shown, not told*. Each one runs ccq on
actual code, lists the real output, visualizes the structured result, and records any bugs the
exercise found (writing a case study is itself a test).

## Index

| Case study | Codebases | What it shows |
|------------|-----------|---------------|
| [call-graph-redis-wpa](call-graph-redis-wpa/README.md) | redis, wpa_supplicant | the 3-layer model (grep → clangd → agent tools), `explore`/`callers`/`callees`/`export`, **fn-pointer dispatch** in no-build mode, and an **interactive HTML knowledge graph** — plus 5 real bugs found & fixed |
| [safe-refactor](safe-refactor/README.md) | ctest8 | the **editing** dimension (Serena-parity): `impact` → `rename` → `replace-body` — scope-correct rename, symbol-level body rewrite; found & fixed a warm-daemon staleness bug + documented the macro-body rename limit |
| [intranet-no-build](intranet-no-build/README.md) | wpa_supplicant | the **air-gapped / no-build** dimension: zero-dependency install, `ccq init` → `compile_flags.txt`, fn-pointer dispatch without a build, the accuracy tradeoff; found & fixed a hidden no-build-warning bug |
| [multi-target-compdb](multi-target-compdb/README.md) | synthetic 8-folder / 3-exe | the **multi-target build** dimension: `--compdb` merges several renamed `compile_commands.json`, one warm clangd per config, and the **first-`--compdb`-wins** rule for files shared across targets |
| [index-control](index-control/README.md) | redis | the **operations** dimension on a big repo: `ccq.json` allow/deny index filter, `ccq wait-index` (know the index is ready before querying), `ccq cache` (inspect/clean), `ccq doctor` (diagnose) |

## Test rigor

Running these on real repos found **8 real bugs** (all fixed) that the unit/integration suite missed
— see [bugs-found.md](bugs-found.md). Writing a case study is itself a test.

## Layout

Each case study is a self-contained folder so they can grow independently:

```
docs/case-studies/
  README.md                     # this index
  <topic>/
    README.md                   # the narrative (real commands + output + Mermaid)
    *.html                      # interactive graphs from `ccq export --format html`
    *.py / data / assets        # any helpers or captured data
```

To add one: create `docs/case-studies/<topic>/README.md`, drop its assets beside it, and add a
row to the index above.

## Generate the graphs yourself

The HTML knowledge graphs are produced directly by ccq (offline, zero-dependency):

```bash
ccq export --format html --focus <symbol> -d 1 -p <repo> --out graph.html
open graph.html
```
