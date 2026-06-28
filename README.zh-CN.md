<!-- LANG-BAR -->
[English](README.md) · [繁體中文](README.zh-TW.md) · **简体中文**

# ccq — 由 clangd 驱动的 C/C++ 代码智能 CLI（给 AI agent 用）

`ccq` 是一个单一 binary 的 CLI，让 AI 编码代理（Claude Code、Codex、OpenCode）与人类能对 C/C++ 代码做**编译器级精确、token 高效**的导航与重构 —— 底层驱动 **clangd**，再补上 clangd 自己不会做的少数几件事。

它的目标是在三个主流「代码知识」工具的各自强项上对齐或胜出，同时维持一个**零依赖的 Go binary**，可轻松部署到锁定 / 内网（air-gapped）环境。

```
ccq callers lookupCommand      # 谁调用它（函数级、跨文件）
ccq explore processCommand     # 一次给：源码 + callers + callees + blast-radius
ccq impact ssl_init -d 3        # 传递影响范围
ccq rename old_name new_name --apply   # 跨项目安全的符号级改名
```

## 动机

AI 编码代理理解 C/C++ 的方式是猛刷 `grep` 与 `Read` —— 大量工具调用、烧很多 token，而且文本搜索从根本看不到调用图（函数指针、宏、`#ifdef`）。一场对打 benchmark（[`cbm-vs-codegraph-bench`](https://github.com/swchen44/cbm-vs-codegraph-bench)）发现，C 最准的引擎其实就是 **clangd + `compile_commands.json`** —— 它在调用图、`#ifdef`、宏、`typedef`、`_Generic` 都赢 —— 但它没有为 agent 打包、重启很慢、也不解析 runtime 的函数指针分派。

**ccq 的存在就是把这个赢家引擎打包给 agent：** 每个问题一个 token 便宜的命令、warm daemon 给速度、fnptr 启发式补 clangd 唯一缺的、符号级编辑，以及一个能丢进锁定内网的零依赖单一 binary。

## 为什么用 ccq（解决的痛点）

| 对标工具 | 它的痛点 | ccq 怎么解 |
|---------|---------|-----------|
| **codebase-memory-mcp (cbm)** | agent 用 grep+Read 烧 token/工具调用；grep 追不到调用关系；宏看不见；要快 | clangd **函数级**调用图（cbm 的 C 调用图是文件级）；宏展开；warm clangd；精简输出 |
| **CodeGraph** | agent 想**一次问到位**；函数指针分派 grep 看不到；索引要保持新鲜 | `ccq explore` 一次回全部；**fnptr 分派启发式**（CodeGraph 唯一胜过 clangd 的点）；clangd 自动重索引 |
| **Serena** | agent 需要**符号级**导航 + 编辑，而非文本操作 | clangd LSP：定义 / 引用 / **安全 rename** |

**设计论点：** ccq = clangd 的编译器级正确性（赢调用图、`#ifdef`、宏、`typedef`、`_Generic`）**＋** CodeGraph 的 `explore` 与 fnptr 合成 **＋** Serena 的符号编辑 **＋** cbm 的速度（warm clangd）。取各家之长，叠在 clangd 上。

> 由来：本设计来自一场 cbm vs CodeGraph vs clangd vs 传统工具（cscope/ctags/cflow）在 `wpa_supplicant` 与 `redis` 上的对打 benchmark。clangd + `compile_commands.json` 在调用图、`#ifdef`、宏、`typedef`、`_Generic` 维度胜出；ccq 把这个赢面打包，并补上唯一缺口（fnptr 分派）。

## 功能

**导航**
- `search <q>` — 找符号（workspace symbols）
- `def <sym>` / `show <sym>` — 定义源码
- `refs <sym>` — 所有引用
- `callers <sym>` — 谁调用（clangd call hierarchy **+ fnptr 启发式**）
- `callees <sym>` — 它调用了谁（clangd + **body-scan** + fnptr 分派目标）
- `impact <sym> [-d N]` — 传递 callers（影响范围）
- `explore <sym>` — **一次**：源码 + callers + callees + blast-radius
- `symbols <file>` — 文件 outline
- `macro <sym>` — 宏展开 / 签名（clangd hover）

**编辑（符号级，对标 Serena；默认 dry-run 除非 `--apply`）**
- `rename <sym> <new> [--apply]` — 跨项目安全改名
- `replace-body <sym> <file> [--apply]` — 替换符号整段定义
- `insert-before <sym> <file>` / `insert-after <sym> <file>` — 在符号前/后插入内容

**导出（用你自己的工具查）**
- `export [--format json|sql] [--out f]` — 导出符号 + 调用图（含 fnptr 边）。`ccq export --format sql | sqlite3 g.db` 后用纯 SQL 查 —— 零依赖、取代「内置查询语言」。
- `fnptr` — 验证 fn-pointer 对照表（`ccq.fnptr.json`）

**项目**
- `init` — 检测/生成 `compile_commands.json`（CMake / Meson / bear），没有 build 系统时则生成**无 build 的 `compile_flags.txt`**；预热 clangd
- `status`、`shutdown`、`version`

**差异化**
- **fn-pointer 分派** — `callers`/`explore` 解析 ops-struct 注册与 `obj->fn()` 分派，合成 `dispatcher → handler` 边。以 **(struct 类型, 字段)** 为键，不同 struct 同名字段不互相污染；处理 **designated init、positional table `{"n", fn}`、field←field 传递**（移植自 CodeGraph 的合成器）。clangd 自己不做这个。
- **fn-pointer 对照表** — 对于文本扫描追不到的盲区（callback、间接分派），可在项目根放一个 `ccq.fnptr.json` 声明 ground truth，与自动扫描合并（JSON、零依赖）：
  ```json
  {
    "registrations": [ { "struct": "wpa_driver_ops", "field": "scan2", "handlers": ["wpa_driver_bsd_scan"] } ],
    "links":         [ { "from": "eloop_run", "to": ["wext_scan_timeout"], "note": "eloop timer callback" } ]
  }
  ```
  `registrations` 补某 struct.field 的 handlers；`links` 加直接 `dispatcher → handler` 边。`ccq fnptr` 验证对照表。
- **无 build 模式** — 当没有 `compile_commands.json` 也没有 build 系统时，`ccq init` 会写一个 `compile_flags.txt`（自动探测 `-I` include 目录）。clangd 即能**跨文件**解析（配合 ccq 打开文件预热）**而不需 build** —— cbm 式广度，精度较低（`#ifdef` 过度涵盖、缺 `-D`）。精度阶梯：compile_commands.json > compile_flags.txt > 同文件。
- **宏** — clangd 会索引 `#define`；它们会出现在 `ccq search`（kind `macro`），`ccq macro` 可展开。

## 安装

ccq 是单一静态 Go binary。**clangd 是唯一的外部依赖。**

### 下载 prebuilt binary
从 [Releases](https://github.com/swchen44/ccq/releases)（稳定版）或 [nightly](https://github.com/swchen44/ccq/releases/tag/nightly)（每晚最新版）下载对应平台压缩包，解压后跑 `./install.sh`（或 `install.ps1`）。

### 从源码编译（内网 / air-gapped 推荐）
```bash
git clone https://github.com/swchen44/ccq && cd ccq
go build -o ccq ./cmd/ccq      # 单一静态 binary，零 Go 依赖
sudo mv ccq /usr/local/bin/    # （Windows：把 ccq.exe 放上 PATH）
```
因为 ccq **没有第三方 Go 模块**，`go build` 不需网络 —— 适合离线/内网。

### 内网 / air-gapped
prebuilt 与自行编译**两种都可离线**：要么把单一 prebuilt binary 复制进去，要么把整个 repo 复制进去 `go build`（vendored 全在 repo，build 不需网络）。clangd 也是单一 binary，从 LLVM release 复制进去，必要时 `--clangd /path/to/clangd`。

### clangd（必需）
- macOS：`brew install llvm`（clangd 在 `$(brew --prefix llvm)/bin`），或 Xcode 的 `clangd`。
- Linux：`apt install clangd` / `dnf install clang-tools-extra`。
- Windows：安装 LLVM（clangd.exe），或 VS 的 clangd 组件。

### 安装 agent skill
把 `SKILL.md` 复制到 agent 的 skills 目录：
- Claude Code：`~/.claude/skills/ccq/SKILL.md`
- OpenCode / Codex：对应的项目/agent skills 目录（见其文档）。

跑 `./install.sh`（macOS/Linux）或 `install.ps1`（Windows）会放好 binary 并安装 skill。

## 快速上手
```bash
cd your-c-project
ccq init                       # 生成 compile_commands.json + 预热 clangd
ccq explore main               # 看 main：源码 + 谁调用 + 调用了谁
ccq callers some_handler       # 函数级 callers + fnptr 分派
ccq rename old_api new_api      # 预览安全改名（加 --apply 才写入）
```

## Benchmark

ccq 的设计来自一场 C 代码智能工具的对打 benchmark（完整 harness：[`swchen44/cbm-vs-codegraph-bench`](https://github.com/swchen44/cbm-vs-codegraph-bench)；方法论：[docs/benchmark.md](docs/benchmark.md)）。

| 维度 | cbm | CodeGraph | clangd | Serena | **ccq** |
|------|-----|-----------|--------|--------|---------|
| 函数级调用图 | ❌ 文件级 | ✅ | ✅ | ✅ | ✅ |
| fn-pointer 分派（F6） | ❌ | ✅ | ⚠️ | ⚠️ | ✅ |
| 8 项难 C 特性通过 | 2 | 3 | 7 | 7 | **8（唯一）** |
| redis `lookupCommand` callers | 0 | 13 | 13 | 13 | **13** |
| 热查询（重复） | 每次重跑 | 每次重跑 | — | 每次重跑 | **~0.07–0.6s** |
| 符号改名（编辑） | ❌ | ❌ | — | ✅ | ✅ |
| 依赖足迹 | 自编 | ~188 MB | binary | ~890 包 | **单一 Go binary，0 依赖** |

**结论：** ccq 是唯一通过全部 8 项难 C 特性的工具 —— 保留 clangd 的赢面（`#ifdef`、宏、`typedef`、`_Generic`、函数级调用图），再加上 fnptr 启发式（CodeGraph 唯一胜 clangd 的特性）、warm daemon 的速度，并维持零依赖单一 binary。

## 限制（Limitations）

ccq 刻意只是 clangd 之上的薄层；它继承 clangd 的强项，也有几个诚实的限制。

- **函数指针启发式（`fnptr`）** — 纯文本、刻意*过度近似*：它把一个 dispatcher 连到该 `(struct, 字段)` 注册过的**所有** handler（是候选集，不是运行期单一目标）。它**不自动解析**：当作参数传递后在他处被调用的 callback（`eloop_register_timeout(cb, …)` → 之后 `e->cb()`）、间接接收者 `(*p)->fn()`、数组索引分派 `arr[i]->fn()`、返回值分派 `get_fn()()`、或存在普通（非 struct）变量里的函数指针。positional table 与多行注册为尽力而为。**缓解：** 可在 `ccq.fnptr.json` 对照表手动声明这些关联。
- **callees** — clangd 的 `outgoingCalls` 不可靠，所以 `callees` 会 union 函数体扫描（调用点对照符号索引验证）+ fnptr 分派目标。body-scan 仍可能漏掉藏在宏后的调用。
- **callback / 事件分派** — 「先注册、之后调用」流程（eloop/timer/signal）不被解析 —— 这是所有静态工具（含 cscope、clangd）的共同盲区。
- **无 build 模式精度** — `compile_flags.txt` 让你不用 build 也能跨文件，但用猜的 include 且无 `-D`：`#ifdef` 分支会过度涵盖、依赖宏的代码可能错。要 config 精准请用真的 `compile_commands.json`。
- **冷启动与规模** — 第一次查询会启动 daemon 并索引整个 repo（redis 约 ~30s）；clangd 索引也吃与 repo 大小成正比的内存。热查询亚秒。有持久化索引时,`--incremental` 只开 git 变动的文件（热重启约快 2.4×）；在验证稳定前为 opt-in,默认仍走完整 OpenAll。
- **依赖 / 范围** — 需要 `clangd` binary（引擎），且为了最佳精度需要 compile database。**仅 C/C++**（跨语言广度是 cbm 这类 tree-sitter 工具的领域）。

## 发布 / 分发（给别人用）

ccq 每平台都是单一静态 binary —— 分发就是「给 binary + SKILL.md」。对方另需 `clangd`（或 `--clangd <path>`）。

**方式 A — 本机打包全平台**
```bash
./build-release.sh v0.3.0
# → dist/ccq-{darwin,linux,windows}-{amd64,arm64}.{tar.gz,zip} + SHA256SUMS
```
把对应平台压缩包给对方，解压跑 `./install.sh`（或 `install.ps1`）。每包含 binary、SKILL.md、README、LICENSE 与 installer。

**方式 B — 自动 GitHub Release（推荐）**
```bash
git tag v0.3.0 && git push origin v0.3.0
```
`.github/workflows/release.yml` 交叉编译全平台并发布 GitHub Release（附压缩包 + checksum）。对方：
```bash
curl -fsSL -O https://github.com/swchen44/ccq/releases/download/v0.3.0/ccq-linux-amd64.tar.gz
tar xzf ccq-linux-amd64.tar.gz && cd ccq-linux-amd64 && ./install.sh
```

**自带 clangd 的 release（对方零安装）**
```bash
./build-release.sh v0.3.0 --bundle-clangd     # CLANGD_VER=18.1.3 可覆写
```
从 `clangd/clangd` releases 抓对应平台 clangd 打进每个压缩包（放在 ccq 旁边）。ccq 会自动使用「与自己同目录的 clangd」，install.sh 把两者都放上 PATH。（每包增 ~100–350 MB；`linux/arm64` 无 prebuilt clangd 会跳过，那些用户自行安装。）

**方式 C — 内网 / air-gapped**
编译一次（`go build -o ccq ./cmd/ccq`，免网络），把单一 binary + `SKILL.md` + 平台 `clangd` binary 复制到目标机器，放上 PATH，必要时 `--clangd`。

## For Developers（开发者手册）

> 完整设计与需求：[docs/design.md](docs/design.md) · [docs/requirement.md](docs/requirement.md) · [docs/benchmark.md](docs/benchmark.md)

### 环境与编译
```bash
git clone https://github.com/swchen44/ccq && cd ccq
go build -o ccq ./cmd/ccq        # 零第三方依赖；`go list -m all` 只有本模块
```
需 Go 1.23+。无外部 Go 模块 → `go build` 完全离线可跑。

### 项目结构
```
cmd/ccq/          CLI 入口、flag 解析、daemon-或-inline 路由（+ 集成测试）
internal/lsp/     驱动 clangd 的 LSP client（JSON-RPC/stdio）+ path/snippet helper
internal/cmd/     在 lsp.Client 上实现各子命令（导航 / 编辑 / 导出）
internal/fnptr/   函数指针分派启发式（纯文本；F6 差异化）
internal/compdb/  检测/生成 compile_commands.json 或 compile_flags.txt（no-build）
internal/daemon/  warm-clangd daemon + 跨平台 IPC
docs/             design / requirement / benchmark
.github/workflows ci.yml（测试+lint+build）· release.yml（标签）· nightly.yml（cron）
```

### 测试与 lint
```bash
make test              # 单元测试（不需 clangd）
make test-integration  # 经真 clangd 的端到端测试（无 clangd 则自动 skip）
make lint              # go vet + golangci-lint（若有装）
make fmt               # gofmt -w .
```
- **单元测试**零依赖（stdlib `testing`）：`internal/fnptr`（cross-bleed / positional / field←field）、`internal/compdb`、`internal/lsp`、`internal/cmd`。
- **集成测试**用 `//go:build integration` gate，clangd 不在 PATH 时 skip。
- CI（`.github/workflows/ci.yml`）每次 push/PR 跑 gofmt 检查、`go vet`、golangci-lint、单元测试、集成测试（会装 clangd）、全平台交叉编译。

### 发布流程与版本
- **Stable**：`git tag vX.Y.Z && git push origin vX.Y.Z` → `release.yml` 编全平台并发 GitHub Release（SemVer；见 `CHANGELOG.md`）。
- **Nightly**：`nightly.yml` 每晚（18:00 UTC）刷新一个滚动的 `nightly` prerelease，含最新 `main` 各平台 binary。手动：在 *nightly* workflow 点「Run workflow」。
- 本机全平台编译：`make release`（`./build-release.sh`），加 `--bundle-clangd` 内嵌 clangd。

### 贡献
维持零依赖（只用 stdlib）、`gofmt` 干净、`go vet` / golangci-lint 通过；新逻辑加单元测试（碰到 clangd 路径则加集成测试）。

## 版本历史

| 版本 | 日期 | 重点 |
|------|------|------|
| [**0.5.0**](https://github.com/swchen44/ccq/releases/tag/v0.5.0) | 2026-06-28 | --incremental 懒开索引(只开变动文件;热重启 ~2.4× 快,opt-in) |
| [0.4.0](https://github.com/swchen44/ccq/releases/tag/v0.4.0) | 2026-06-28 | fn-pointer 对照表、replace-body/insert、callees body-scan 修正、git-diff 热重启 |
| [0.3.0](https://github.com/swchen44/ccq/releases/tag/v0.3.0)（首个公开版） | 2026-06-27 | fnptr 升级（复合键、positional、field←field）、no-build 模式、宏搜索、graph export |
| 0.2.0（里程碑） | 2026-06-26 | warm-clangd daemon（亚秒热查询） |
| 0.1.0（里程碑） | 2026-06-26 | 导航 + rename + fnptr 启发式 |

完整记录：[CHANGELOG.md](CHANGELOG.md)。最新 binary：[Releases](https://github.com/swchen44/ccq/releases)（稳定）·[nightly](https://github.com/swchen44/ccq/releases/tag/nightly)。

## Roadmap / TODO

- [x] `callees` 改用函数体扫描（clangd 的 `outgoingCalls` 不可靠；改从函数体建）— 0.4 完成
- [x] 更多编辑：`replace-body`、`insert-before/after`（对标 Serena）— 0.4 完成
- [x] fnptr 对照表（`ccq.fnptr.json`）补盲区 — 0.4 完成
- [x] **完整 git-diff 增量**（`--incremental`）— 热重启只开变动的文件;query path 按需开目标文件。redis 冷启动约快 2.4×、结果一致 — *0.5 完成（opt-in）*
- [ ] `ccq init` 支持更多 build 系统（Bazel、xmake）
- [ ] fnptr 启发式：positional table 边界、注释感知的多行注册

## License
MIT。重用了 `troberti/clangd-query`（MIT）、`mpsm/mcp-cpp`、`2015xli/clangd-graph-rag` 验证过的架构想法。
