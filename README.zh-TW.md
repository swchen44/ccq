<!-- LANG-BAR -->
[English](README.md) · **繁體中文** · [简体中文](README.zh-CN.md)

# ccq — 由 clangd 驅動的 C/C++ 程式碼智慧 CLI（給 AI agent 用）

`ccq` 是一個單一 binary 的 CLI，讓 AI 編碼代理（Claude Code、Codex、OpenCode）與人類能對 C/C++ 程式碼做**編譯器級精確、token 高效**的導航與重構 —— 底層驅動 **clangd**，再補上 clangd 自己不會做的少數幾件事。

它的目標是在三個主流「程式碼知識」工具的各自強項上對齊或勝出，同時維持一個**零相依的 Go binary**，可輕鬆部署到鎖定 / 內網（air-gapped）環境。

```
ccq callers lookupCommand      # 誰呼叫它（函式級、跨檔）
ccq explore processCommand     # 一次給：源碼 + callers + callees + blast-radius
ccq impact ssl_init -d 3        # 遞移影響範圍
ccq rename old_name new_name --apply   # 跨專案安全的符號級改名
```

## 動機

AI 編碼代理理解 C/C++ 的方式是猛刷 `grep` 與 `Read` —— 大量工具呼叫、燒很多 token，而且文字搜尋從根本看不到呼叫圖（函式指標、巨集、`#ifdef`）。一場對打 benchmark（[`cbm-vs-codegraph-bench`](https://github.com/swchen44/cbm-vs-codegraph-bench)）發現，C 最準的引擎其實就是 **clangd + `compile_commands.json`** —— 它在呼叫圖、`#ifdef`、巨集、`typedef`、`_Generic` 都贏 —— 但它沒有為 agent 打包、重啟很慢、也不解析 runtime 的函式指標分派。

**ccq 的存在就是把這個贏家引擎打包給 agent：** 每個問題一個 token 便宜的指令、warm daemon 給速度、fnptr 啟發式補 clangd 唯一缺的、符號級編輯，以及一個能丟進鎖定內網的零相依單一 binary。

## 為什麼用 ccq（解決的痛點）

| 對標工具 | 它的痛點 | ccq 怎麼解 |
|---------|---------|-----------|
| **codebase-memory-mcp (cbm)** | agent 用 grep+Read 燒 token/工具呼叫；grep 追不到呼叫關係；巨集看不見；要快 | clangd **函式級**呼叫圖（cbm 的 C 呼叫圖是檔案級）；巨集展開；warm clangd；精簡輸出 |
| **CodeGraph** | agent 想**一次問到位**；函式指標分派 grep 看不到；索引要保持新鮮 | `ccq explore` 一次回全部；**fnptr 分派啟發式**（CodeGraph 唯一勝過 clangd 的點）；clangd 自動重索引 |
| **Serena** | agent 需要**符號級**導航 + 編輯，而非文字操作 | clangd LSP：定義 / 參照 / **安全 rename** |

**設計論點：** ccq = clangd 的編譯器級正確性（贏呼叫圖、`#ifdef`、巨集、`typedef`、`_Generic`）**＋** CodeGraph 的 `explore` 與 fnptr 合成 **＋** Serena 的符號編輯 **＋** cbm 的速度（warm clangd）。取各家之長，疊在 clangd 上。

> 由來：本設計來自一場 cbm vs CodeGraph vs clangd vs 傳統工具（cscope/ctags/cflow）在 `wpa_supplicant` 與 `redis` 上的對打 benchmark。clangd + `compile_commands.json` 在呼叫圖、`#ifdef`、巨集、`typedef`、`_Generic` 維度勝出；ccq 把這個贏面打包，並補上唯一缺口（fnptr 分派）。

## 功能

**導航**
- `search <q>` — 找符號（workspace symbols）
- `def <sym>` / `show <sym>` — 定義源碼
- `refs <sym>` — 所有參照
- `callers <sym>` — 誰呼叫（clangd call hierarchy **+ fnptr 啟發式**）
- `callees <sym>` — 它呼叫了誰
- `impact <sym> [-d N]` — 遞移 callers（影響範圍）
- `explore <sym>` — **一次**：源碼 + callers + callees + blast-radius
- `symbols <file>` — 檔案 outline
- `macro <sym>` — 巨集展開 / 簽名（clangd hover）

**編輯（符號級，對標 Serena）**
- `rename <sym> <new> [--apply]` — 跨專案安全改名（預設 dry-run）

**匯出（用你自己的工具查）**
- `export [--format json|sql] [--out f]` — 匯出符號 + 呼叫圖（含 fnptr 邊）。`ccq export --format sql | sqlite3 g.db` 後用純 SQL 查 —— 零相依、取代「內建查詢語言」。

**專案**
- `init` — 偵測/產生 `compile_commands.json`（CMake / Meson / bear），沒有 build 系統時則產生**無 build 的 `compile_flags.txt`**；暖機 clangd
- `status`、`shutdown`、`version`

**差異化**
- **fn-pointer 分派** — `callers`/`explore` 解析 ops-struct 註冊與 `obj->fn()` 分派，合成 `dispatcher → handler` 邊。以 **(struct 型別, 欄位)** 為鍵，不同 struct 同名欄位不互相污染；處理 **designated init、positional table `{"n", fn}`、field←field 傳遞**（移植自 CodeGraph 的合成器）。clangd 自己不做這個。
- **無 build 模式** — 當沒有 `compile_commands.json` 也沒有 build 系統時，`ccq init` 會寫一個 `compile_flags.txt`（自動探測 `-I` include 目錄）。clangd 即能**跨檔**解析（配合 ccq 開檔暖機）**而不需 build** —— cbm 式廣度，精度較低（`#ifdef` 過度涵蓋、缺 `-D`）。精度階梯：compile_commands.json > compile_flags.txt > 同檔。
- **巨集** — clangd 會索引 `#define`；它們會出現在 `ccq search`（kind `macro`），`ccq macro` 可展開。

## 安裝

ccq 是單一靜態 Go binary。**clangd 是唯一的外部相依。**

### 下載 prebuilt binary
從 [Releases](https://github.com/swchen44/ccq/releases)（穩定版）或 [nightly](https://github.com/swchen44/ccq/releases/tag/nightly)（每晚最新版）下載對應平台壓縮檔，解壓後跑 `./install.sh`（或 `install.ps1`）。

### 從原始碼編譯（內網 / air-gapped 推薦）
```bash
git clone https://github.com/swchen44/ccq && cd ccq
go build -o ccq ./cmd/ccq      # 單一靜態 binary，零 Go 相依
sudo mv ccq /usr/local/bin/    # （Windows：把 ccq.exe 放上 PATH）
```
因為 ccq **沒有第三方 Go 模組**，`go build` 不需網路 —— 適合離線/內網。

### 內網 / air-gapped
prebuilt 與自行編譯**兩種都可離線**：要嘛把單一 prebuilt binary 複製進去，要嘛把整個 repo 複製進去 `go build`（vendored 全在 repo，build 不需網路）。clangd 也是單一 binary，從 LLVM release 複製進去，必要時 `--clangd /path/to/clangd`。

### clangd（必需）
- macOS：`brew install llvm`（clangd 在 `$(brew --prefix llvm)/bin`），或 Xcode 的 `clangd`。
- Linux：`apt install clangd` / `dnf install clang-tools-extra`。
- Windows：安裝 LLVM（clangd.exe），或 VS 的 clangd 元件。

### 安裝 agent skill
把 `SKILL.md` 複製到 agent 的 skills 目錄：
- Claude Code：`~/.claude/skills/ccq/SKILL.md`
- OpenCode / Codex：對應的專案/agent skills 目錄（見其文件）。

跑 `./install.sh`（macOS/Linux）或 `install.ps1`（Windows）會放好 binary 並安裝 skill。

## 快速上手
```bash
cd your-c-project
ccq init                       # 產生 compile_commands.json + 暖機 clangd
ccq explore main               # 看 main：源碼 + 誰呼叫 + 呼叫了誰
ccq callers some_handler       # 函式級 callers + fnptr 分派
ccq rename old_api new_api      # 預覽安全改名（加 --apply 才寫入）
```

## Benchmark

ccq 的設計來自一場 C 程式碼智慧工具的對打 benchmark（完整 harness：[`swchen44/cbm-vs-codegraph-bench`](https://github.com/swchen44/cbm-vs-codegraph-bench)；方法論：[docs/benchmark.md](docs/benchmark.md)）。

| 維度 | cbm | CodeGraph | clangd | Serena | **ccq** |
|------|-----|-----------|--------|--------|---------|
| 函式級呼叫圖 | ❌ 檔案級 | ✅ | ✅ | ✅ | ✅ |
| fn-pointer 分派（F6） | ❌ | ✅ | ⚠️ | ⚠️ | ✅ |
| 8 項難 C 特性通過 | 2 | 3 | 7 | 7 | **8（唯一）** |
| redis `lookupCommand` callers | 0 | 13 | 13 | 13 | **13** |
| 暖查詢（重複） | 每次重跑 | 每次重跑 | — | 每次重跑 | **~0.07–0.6s** |
| 符號改名（編輯） | ❌ | ❌ | — | ✅ | ✅ |
| 相依足跡 | 自編 | ~188 MB | binary | ~890 套件 | **單一 Go binary，0 相依** |

**結論：** ccq 是唯一通過全部 8 項難 C 特性的工具 —— 保留 clangd 的贏面（`#ifdef`、巨集、`typedef`、`_Generic`、函式級呼叫圖），再加上 fnptr 啟發式（CodeGraph 唯一勝 clangd 的特性）、warm daemon 的速度，並維持零相依單一 binary。

## 限制（Limitations）

ccq 刻意只是 clangd 之上的薄層；它繼承 clangd 的強項，也有幾個誠實的限制。

- **函式指標啟發式（`fnptr`）** — 純文字、刻意*過度近似*：它把一個 dispatcher 連到該 `(struct, 欄位)` 註冊過的**所有** handler（是候選集，不是執行期單一目標）。它**不**解析：當作引數傳遞後在他處被呼叫的 callback（`eloop_register_timeout(cb, …)` → 之後 `e->cb()`）、間接接收者 `(*p)->fn()`、陣列索引分派 `arr[i]->fn()`、回傳值分派 `get_fn()()`、或存在普通（非 struct）變數裡的函式指標。positional table 與多行註冊為盡力而為。
- **callees** — clangd 的 `outgoingCalls` 不可靠，所以 `callees`（與 `explore` 的 callees 部分）可能少報。`callers`/`impact`/`export` 走可靠的 `incomingCalls`。（Roadmap：改用函式體掃描建 callees。）
- **callback / 事件分派** — 「先註冊、之後呼叫」流程（eloop/timer/signal）不被解析 —— 這是所有靜態工具（含 cscope、clangd）的共同盲區。
- **無 build 模式精度** — `compile_flags.txt` 讓你不用 build 也能跨檔，但用猜的 include 且無 `-D`：`#ifdef` 分支會過度涵蓋、依賴巨集的碼可能錯。要 config 精準請用真的 `compile_commands.json`。
- **冷啟動與規模** — 第一次查詢會啟動 daemon 並索引整個 repo（redis 約 ~30s）；clangd 索引也吃與 repo 大小成正比的記憶體。暖查詢則亞秒。
- **相依 / 範圍** — 需要 `clangd` binary（引擎），且為了最佳精度需要 compile database。**僅 C/C++**（跨語言廣度是 cbm 這類 tree-sitter 工具的領域）。

## 釋出 / 散布（給別人用）

ccq 每平台都是單一靜態 binary —— 散布就是「給 binary + SKILL.md」。對方另需 `clangd`（或 `--clangd <path>`）。

**方式 A — 本機打包全平台**
```bash
./build-release.sh v0.3.0
# → dist/ccq-{darwin,linux,windows}-{amd64,arm64}.{tar.gz,zip} + SHA256SUMS
```
把對應平台壓縮檔給對方，解壓跑 `./install.sh`（或 `install.ps1`）。每包含 binary、SKILL.md、README、LICENSE 與 installer。

**方式 B — 自動 GitHub Release（推薦）**
```bash
git tag v0.3.0 && git push origin v0.3.0
```
`.github/workflows/release.yml` 交叉編譯全平台並發佈 GitHub Release（附壓縮檔 + checksum）。對方：
```bash
curl -fsSL -O https://github.com/swchen44/ccq/releases/download/v0.3.0/ccq-linux-amd64.tar.gz
tar xzf ccq-linux-amd64.tar.gz && cd ccq-linux-amd64 && ./install.sh
```

**自帶 clangd 的 release（對方零安裝）**
```bash
./build-release.sh v0.3.0 --bundle-clangd     # CLANGD_VER=18.1.3 可覆寫
```
從 `clangd/clangd` releases 抓對應平台 clangd 打進每個壓縮檔（放在 ccq 旁邊）。ccq 會自動使用「與自己同目錄的 clangd」，install.sh 把兩者都放上 PATH。（每包增 ~100–350 MB；`linux/arm64` 無 prebuilt clangd 會跳過，那些使用者自行安裝。）

**方式 C — 內網 / air-gapped**
編譯一次（`go build -o ccq ./cmd/ccq`，免網路），把單一 binary + `SKILL.md` + 平台 `clangd` binary 複製到目標機器，放上 PATH，必要時 `--clangd`。

## For Developers（開發者手冊）

> 完整設計與需求：[docs/design.md](docs/design.md) · [docs/requirement.md](docs/requirement.md) · [docs/benchmark.md](docs/benchmark.md)

### 環境與編譯
```bash
git clone https://github.com/swchen44/ccq && cd ccq
go build -o ccq ./cmd/ccq        # 零第三方相依；`go list -m all` 只有本模組
```
需 Go 1.23+。無外部 Go 模組 → `go build` 完全離線可跑。

### 專案結構
```
cmd/ccq/          CLI 入口、flag 解析、daemon-或-inline 路由（+ 整合測試）
internal/lsp/     驅動 clangd 的 LSP client（JSON-RPC/stdio）+ path/snippet helper
internal/cmd/     在 lsp.Client 上實作各子命令（導航 / 編輯 / 匯出）
internal/fnptr/   函式指標分派啟發式（純文字；F6 差異化）
internal/compdb/  偵測/產生 compile_commands.json 或 compile_flags.txt（no-build）
internal/daemon/  warm-clangd daemon + 跨平台 IPC
docs/             design / requirement / benchmark
.github/workflows ci.yml（測試+lint+build）· release.yml（標籤）· nightly.yml（cron）
```

### 測試與 lint
```bash
make test              # 單元測試（不需 clangd）
make test-integration  # 經真 clangd 的端到端測試（無 clangd 則自動 skip）
make lint              # go vet + golangci-lint（若有裝）
make fmt               # gofmt -w .
```
- **單元測試**零相依（stdlib `testing`）：`internal/fnptr`（cross-bleed / positional / field←field）、`internal/compdb`、`internal/lsp`、`internal/cmd`。
- **整合測試**用 `//go:build integration` gate，clangd 不在 PATH 時 skip。
- CI（`.github/workflows/ci.yml`）每次 push/PR 跑 gofmt 檢查、`go vet`、golangci-lint、單元測試、整合測試（會裝 clangd）、全平台交叉編譯。

### 釋出流程與版本
- **Stable**：`git tag vX.Y.Z && git push origin vX.Y.Z` → `release.yml` 編全平台並發 GitHub Release（SemVer；見 `CHANGELOG.md`）。
- **Nightly**：`nightly.yml` 每晚（18:00 UTC）刷新一個滾動的 `nightly` prerelease，含最新 `main` 各平台 binary。手動：在 *nightly* workflow 點「Run workflow」。
- 本機全平台編譯：`make release`（`./build-release.sh`），加 `--bundle-clangd` 內嵌 clangd。

### 貢獻
維持零相依（只用 stdlib）、`gofmt` 乾淨、`go vet` / golangci-lint 通過；新邏輯加單元測試（碰到 clangd 路徑則加整合測試）。

## 版本歷史

| 版本 | 日期 | 重點 |
|------|------|------|
| [**0.3.0**](https://github.com/swchen44/ccq/releases/tag/v0.3.0)（首個公開版） | 2026-06-27 | fnptr 升級（複合鍵、positional、field←field）、no-build 模式、巨集搜尋、graph export |
| 0.2.0（里程碑） | 2026-06-26 | warm-clangd daemon（亞秒暖查詢） |
| 0.1.0（里程碑） | 2026-06-26 | 導航 + rename + fnptr 啟發式 |

完整紀錄：[CHANGELOG.md](CHANGELOG.md)。最新 binary：[Releases](https://github.com/swchen44/ccq/releases)（穩定）·[nightly](https://github.com/swchen44/ccq/releases/tag/nightly)。

## Roadmap / TODO

- [ ] `callees` 改用函式體掃描（clangd 的 `outgoingCalls` 不可靠；改從函式體建）
- [ ] 更多編輯：`replace-body`、`insert-before/after`（對標 Serena）
- [ ] git-diff 增量重索引（給超大 repo）
- [ ] `ccq init` 支援更多 build 系統（Bazel、xmake）
- [ ] fnptr 啟發式：positional table 邊界、註解感知的多行註冊

## License
MIT。重用了 `troberti/clangd-query`（MIT）、`mpsm/mcp-cpp`、`2015xli/clangd-graph-rag` 驗證過的架構想法。
