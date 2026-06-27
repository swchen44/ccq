---
name: ccq
description: Use when navigating, understanding, or refactoring a C/C++ codebase — finding who calls a function, what a function calls, a symbol's definition/references, impact of a change, function-pointer dispatch (ops/vtable), or doing a safe symbol-level rename. Triggers on "誰呼叫", "who calls", "what calls X", "find definition of", "references to", "impact of changing", "trace the call graph", "重構/改名 a symbol", or any task that would otherwise need many grep/Read calls over C/C++ code. Prefer ccq over grep/Read for C/C++ structure questions — it is clangd-accurate (handles macros, #ifdef, typedef, _Generic) and token-efficient.
---

# ccq — clangd-powered C/C++ code intelligence

`ccq` answers code-structure questions about C/C++ by driving **clangd** (the compiler's own engine), so it is correct where `grep` is blind: it expands **macros**, evaluates **#ifdef**, resolves **typedef chains** and **_Generic**, and gives **function-level** call graphs across files. It also adds a **function-pointer dispatch heuristic** (ops/vtable structs) that clangd alone won't resolve.

## When to use ccq instead of grep/Read
- "Who calls `X`?" → `ccq callers X`
- "What does `X` call?" → `ccq callees X`
- "What breaks if I change `X`?" → `ccq impact X -d 3`
- "Show me `X` and how it connects" (one shot) → `ccq explore X`
- "Where is `X` defined / used?" → `ccq def X` / `ccq refs X`
- "Find symbols matching ..." → `ccq search <query>`
- "Outline this file" → `ccq symbols <file>`
- "What does this macro expand to / this signature?" → `ccq macro X`
- "Rename `X` to `Y` safely across the project" → `ccq rename X Y --apply`
- "Rewrite the body of `X`" → `ccq replace-body X newbody.txt --apply` (dry-run without `--apply`)
- "Insert code before/after `X`" → `ccq insert-before X snippet.txt` / `ccq insert-after X snippet.txt`
- "Dump the call graph so I can query it with SQL" → `ccq export --format sql | sqlite3 g.db`
- "Find this macro" → `ccq search <MACRO>` (macros are indexed; kind shows `macro`)

## First time in a repo
Run `ccq init` once — it locates or generates `compile_commands.json` (CMake/Meson/bear) and warms clangd. Without it, ccq runs in degraded (same-file) mode.

## Usage
```
ccq <command> [args] [-p <project-dir>] [--json]
```
- Add `-p <dir>` if not running from the project root.
- Add `--json` when you need to parse the result programmatically.
- `callers` / `explore` also report `fnptr` heuristic callers for ops-struct dispatch (marked `fnptr via .field`).
- For fn-pointer blind spots the text scan can't infer (callbacks, indirect dispatch), add a `ccq.fnptr.json` in the project root (`registrations` + `links`) to declare ground truth; `ccq fnptr` validates it. `callees` now also unions a function-body scan, so it no longer under-reports like clangd's raw `outgoingCalls`.

## Guidance for agents
- Prefer **one `ccq explore X`** over multiple grep/Read — it returns source + callers + callees + blast-radius in a single call (token-efficient).
- The first command in a cold repo waits for clangd to index (a few seconds); subsequent calls are fast (cached).
- For C/C++, trust ccq's call graph over text search: indirect/macro-hidden/typedef'd calls that grep misses are resolved here.
- `rename` is **dry-run by default**; only pass `--apply` once the edit list looks right.
