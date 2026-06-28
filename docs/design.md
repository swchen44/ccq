# ccq — Design

This document describes ccq's architecture, modules, data flow, sequences, and protocols.

## 1. Architecture overview

ccq is a thin, fast layer over **clangd**: it speaks LSP to clangd for compiler-accurate
answers, adds a text-based **function-pointer heuristic** that clangd won't do, keeps clangd
**warm** in a daemon, and renders **token-efficient** output for AI agents.

```mermaid
flowchart TB
    subgraph User["Agent / Human"]
        CLI["ccq CLI"]
    end
    subgraph CCQ["ccq (single Go binary)"]
        MAIN["cmd/ccq (flags, dispatch)"]
        DAEMON["daemon (warm clangd, IPC)"]
        CMD["cmd (commands: callers/explore/rename/export...)"]
        LSPC["lsp (LSP client)"]
        FN["fnptr (dispatch heuristic, text)"]
        CDB["compdb (compile_commands / compile_flags)"]
    end
    CLANGD["clangd (external)"]
    CC[("compile_commands.json / compile_flags.txt")]
    CODE[("C/C++ source")]

    CLI --> MAIN --> DAEMON --> CMD
    MAIN -. "--no-daemon" .-> CMD
    CMD --> LSPC --> CLANGD
    CMD --> FN --> CODE
    CDB --> CLANGD
    CLANGD --> CC
    CLANGD --> CODE
```

## 2. Module responsibilities

| Module | File(s) | Responsibility |
|--------|---------|----------------|
| CLI / dispatch | `cmd/ccq/main.go` | parse flags, resolve clangd, route to daemon or inline, subcommands |
| daemon | `internal/daemon/*.go` | keep one clangd warm per project; IPC (Unix socket / TCP); idle shutdown; spawn-on-demand |
| commands | `internal/cmd/run.go`, `edit.go`, `export.go` | implement each subcommand on an `lsp.Client`; output to an `io.Writer` |
| LSP client | `internal/lsp/client.go`, `util.go` | drive clangd over JSON-RPC/stdio: symbols, definition, references, call hierarchy, hover, rename |
| fnptr heuristic | `internal/fnptr/fnptr.go`, `table.go` | resolve `obj->fn()` dispatch to handlers (text only); merge a user `ccq.fnptr.json` override (registrations + links) |
| compile DB | `internal/compdb/compdb.go` | locate/generate `compile_commands.json`, or no-build `compile_flags.txt` |
| git diff | `internal/gitdiff/gitdiff.go` | files changed since last index, to prioritise re-indexing on a warm daemon restart |

## 3. Data flow — answering "who calls X"

```mermaid
flowchart LR
    Q["ccq callers X"] --> R["resolveSymbol(X)\nworkspace/symbol → file+pos"]
    R --> P["prepareCallHierarchy(file,pos)"]
    P --> IC["incomingCalls → function-level callers"]
    Q --> H["fnptr.Callers(root, X)\n(struct,field) dispatch heuristic"]
    IC --> M["merge + dedup"]
    H --> M
    M --> O["token-efficient output / --json"]
```

## 4. Sequence — first (cold) query spawns the daemon

```mermaid
sequenceDiagram
    participant U as user
    participant C as ccq (CLI)
    participant D as ccq daemon
    participant L as clangd
    U->>C: ccq callers add
    C->>D: connect (per-project socket)
    Note over C,D: no daemon yet → spawn detached `ccq __daemon`
    D->>L: start clangd (--compile-commands-dir, --background-index)
    D->>L: didOpen all source files (prime index)
    D->>L: wait for $/progress index end (or baseline)
    C->>D: request {cmd:callers, args:[add]}
    D->>L: prepareCallHierarchy + incomingCalls
    L-->>D: callers
    D-->>C: text output
    C-->>U: callers of add: ...
```

Subsequent (warm) queries skip the spawn/index and return in well under a second:

```mermaid
sequenceDiagram
    participant C as ccq (CLI)
    participant D as ccq daemon (warm)
    participant L as clangd (warm)
    C->>D: request {cmd:explore, args:[X]}
    D->>L: prepareCallHierarchy + incoming/outgoing + hover
    L-->>D: results
    D-->>C: source + callers + callees + blast-radius
```

## 5. Sequence — function-pointer heuristic (no clangd)

```mermaid
sequenceDiagram
    participant F as fnptr.Callers(root, handler)
    participant S as source files (text)
    F->>S: Pass A: scan fn-pointer typedefs + function defs
    F->>S: Pass B: struct layouts (mark fn-pointer fields)
    F->>S: Pass C: registrations (.field=fn / positional {"n",fn})
    F->>S: Pass D: field←field propagation (a->f = b->g), 3x to converge
    F->>S: Pass E: dispatch sites recv->field() → enclosing function
    Note over F: keyed by (struct,field); FANOUT_CAP; real-function gate
    F-->>F: dispatcher→handler edges (heuristic)
```

## 6. Protocols

### LSP methods used (ccq → clangd, JSON-RPC over stdio)
| Need | LSP method |
|------|-----------|
| find symbols | `workspace/symbol` |
| definition | `textDocument/definition` |
| references | `textDocument/references` |
| who calls | `textDocument/prepareCallHierarchy` → `callHierarchy/incomingCalls` |
| what it calls | `callHierarchy/outgoingCalls` (clangd-limited; export uses incoming instead) |
| file outline | `textDocument/documentSymbol` |
| macro / signature | `textDocument/hover` |
| rename | `textDocument/rename` |
| index readiness | `$/progress` (window/workDoneProgress) |

### Compile database & accuracy ladder
| Config | How clangd behaves | Accuracy |
|--------|-------------------|----------|
| `compile_commands.json` | full background index, real flags + `-D` | highest (correct `#ifdef`, includes) |
| `compile_flags.txt` (no-build) | flat flags, no background index; ccq primes via OpenAll | cross-file works; `#ifdef` over-included, no `-D` |
| none | `clang foo.c` guess | same-file only |

### `--compdb` — multiple / renamed compile databases (multi-target builds)

clangd takes **exactly one** database named `compile_commands.json` in a directory (it doesn't
merge several or accept arbitrary names). Builds that emit several executables produce several
databases, often renamed and scattered. `--compdb` bridges that:

```
ccq callers foo --compdb build1.json,build2.json    # any names; comma-separated
```

```mermaid
flowchart LR
    A["--compdb a.json,b.json"] --> M["compdb.Stage:<br/>merge arrays → one<br/>compile_commands.json<br/>in a stable cache dir"]
    M --> CC["clangd --compile-commands-dir=&lt;staged&gt;"]
    P["-p &lt;source root&gt;"] --> OA["OpenAll / fnptr<br/>(source stays the root)"]
    A --> K["daemon key = hash(root + compdb set)"]
    K --> D["one warm clangd per compile-DB set"]
```

- **Decoupling**: the compile DB (`--compdb`) and the source root (`-p`) are separate — source
  scanning (OpenAll, fnptr) stays on `-p`, while clangd's flags come from the staged DB.
- **Merge semantics & priority**: arrays are concatenated **in `--compdb` order**. A file built
  several ways keeps all its entries; for a duplicated `file`, clangd silently uses the **first**
  matching entry (verified empirically — same `dup.c` with `-DCONFIG_A` vs `-DCONFIG_B`: whichever
  `--compdb` is listed first wins; the other branch is inactive). So **order `--compdb` so the
  config you want for overlapping files comes first**, or pass a single `--compdb` for an exact
  per-config view. `compdb.Stage` preserves input order (pinned by `TestStageMerge`).
- **Daemon scoping**: the daemon socket/state is keyed by `(root, compdb set)` (see
  `daemon.SetKey`). Distinct `--compdb` sets therefore get **distinct warm clangds** — switching
  configs hits a different warm instance with **no re-index** (vs symlinking one
  `compile_commands.json`, which makes clangd re-index on every swap).

**Tradeoff — running a clangd per config is not free:** each instance holds its **own** in-memory
index (RAM ×N, no sharing), pays its **own** cold-index cost, and may contend on the on-disk
`.cache/clangd`; edits must be re-synced to each. So ccq keeps the default "one warm clangd per
root" and only opens extra instances **on demand** per distinct `--compdb`. Use a few configs, not
dozens.

## 7. Key implementation notes (gotchas)

- `CallHierarchyItem.Data` (clangd's opaque payload) **must be round-tripped** or
  incoming/outgoing calls silently return nothing.
- clangd's `workspace/symbol` only returns symbols from **opened** files on a cold project →
  ccq opens all source files at startup (`OpenAll`).
- The call-hierarchy cursor must sit on the **symbol name**, not the line start; ccq adjusts
  the column via `nameColumn`.
- clangd's `outgoingCalls` is unreliable; `export` builds the call graph from `incomingCalls`.
