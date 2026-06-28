# Bugs found by writing the case studies

The [case studies](README.md) are run on **real codebases** (redis, wpa_supplicant, ctest8). Doing
that — not just unit-testing — surfaced **8 real bugs**, all now fixed. The pattern is consistent:
the unit + integration tests stayed green throughout, because these are **clangd-integration and
daemon-lifecycle** issues that only appear when you drive real repos end-to-end.

> Writing a case study *is* a test. Every bug below was found by running ccq for the narrative, not
> by a failing assertion.

## The 8

| # | Symptom (on a real repo) | Root cause | Fix | Surfaced by |
|---|---------------------------|-----------|-----|-------------|
| 1 | `explore` showed **0 callees** for an fn-pointer dispatcher | `explore` still used clangd's unreliable `outgoingCalls` instead of the body-scan + fnptr path | shared `calleeNames` helper · [`56fa4fb`](https://github.com/swchen44/ccq/commit/56fa4fb) | call-graph (ctest8) |
| 2 | `explore lookupCommand` showed the **header prototype** and **0 callees** | clangd go-to-definition jumps definition→declaration; we displayed/scanned the decl | `def`/`explore`/`callees` use `symbolRange` (prefer the `.c` definition) · [`5e4846b`](https://github.com/swchen44/ccq/commit/5e4846b) | call-graph (redis) |
| 3 | `explore` **slow** on redis (~tens of seconds warm) | `fnptr.build(root)` rescanned all 472 files on every query | per-root cache · [`84c03e1`](https://github.com/swchen44/ccq/commit/84c03e1) | call-graph (redis) |
| 4 | `ccq export` (whole repo) **timed out** on 472 files | export ran call-hierarchy per function across the whole tree | `export --focus <sym>` neighborhood BFS · [`72e4e88`](https://github.com/swchen44/ccq/commit/72e4e88) | call-graph (redis) |
| 5 | `ccq export --format html` produced a **broken graph** | template JS keyed nodes by `n.id`, but `exNode` serializes as `name` | `n.id = n.id \|\| n.name` normalization · [`d22e466`](https://github.com/swchen44/ccq/commit/d22e466) | building export-html |
| 6 | `export`/`symbols` **line numbers always 1**; `replace-body` targeted the wrong range | clangd returns flat `SymbolInformation` (range in `location.range`), code read a top-level `range` (always 0) | parse `location.range` · [`8d6870d`](https://github.com/swchen44/ccq/commit/8d6870d) | building replace-body |
| 7 | After `rename --apply`, the same daemon returned `callers <new>` = **(none)** and `replace-body <new>` = **not found** | apply wrote files on disk but didn't tell clangd; the warm index was pre-edit | re-sync clangd (`didChange`) + drop fnptr cache after apply · [`61b580d`](https://github.com/swchen44/ccq/commit/61b580d) | safe-refactor (ctest8) |
| 8 | No-build / degraded **warning hidden** in the default daemon path | the note printed only on the `--no-daemon` inline path | warn for every query, daemon + inline · [`8fec1e5`](https://github.com/swchen44/ccq/commit/8fec1e5) | intranet-no-build (wpa) |

## Why unit tests missed them

These cluster into three areas that a unit test on a fixture won't catch:

- **clangd's real responses** (#1, #2, #6) — `outgoingCalls` being empty, go-to-definition jumping
  to a header, `documentSymbol` returning the flat `SymbolInformation` shape. You only see these
  against a real clangd over real code.
- **Warm-daemon lifecycle** (#3 perf, #7 staleness) — caching and post-edit re-sync only matter
  across *repeated* queries on a *persistent* process; a one-shot test never exercises them.
- **Scale & UX** (#4 timeout, #5 broken render, #8 hidden warning) — only a large repo times out,
  only a browser shows a broken graph, only the default path reveals a missing warning.

## Takeaway

Every fix above shipped with the case study that found it, and the relevant unit/integration test
was added where it could be (e.g. fnptr, edit, gitdiff). The case studies are kept partly *as a
test surface*: re-running them on a new ccq version re-exercises the real-repo paths that
fixtures can't.
