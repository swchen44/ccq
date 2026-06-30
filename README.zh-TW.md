<!-- LANG-BAR -->
[English](README.md) · **繁體中文** · [简体中文](README.zh-CN.md)

# ccq — 由 clangd 驅動的 C/C++ 程式碼智慧 CLI（給 AI agent 用）

`ccq` 是一個單一 binary 的 CLI，讓 AI 編碼代理（Claude Code、Codex、OpenCode）與人類能對 C/C++ 程式碼做**編譯器級精確、token 高效**的導航與重構 —— 底層驅動 **clangd**，再補上 clangd 自己不會做的少數幾件事。

它的目標是在三個主流「程式碼知識」工具的各自強項上對齊或勝出，同時維持一個**零相依的 Go binary**，可輕鬆部署到鎖定 / 內網（air-gapped）環境。

> **支援語言：** C / C++（以及 clangd 也能解析的 Objective-C 與 CUDA）。其他語言 —— Rust、Go、Python… —— **刻意不在範圍內**；它們有各自的 language server（rust-analyzer、gopls…）。ccq 是 clangd 專精工具。

```
ccq callers lookupCommand      # 誰呼叫它（函式級、跨檔）
ccq explore processCommand     # 一次給：源碼 + callers + callees + blast-radius
ccq impact ssl_init -d 3        # 遞移影響範圍
ccq rename old_name new_name --apply   # 跨專案安全的符號級改名
ccq export --format html --focus lookupCommand --out graph.html   # 互動知識圖（離線、零相依）
```

## 動機

AI 編碼代理理解 C/C++ 的方式是猛刷 `grep` 與 `Read` —— 大量工具呼叫、燒很多 token，而且文字搜尋從根本看不到呼叫圖（函式指標、巨集、`#ifdef`）。一場對打 benchmark（[`cbm-vs-codegraph-bench`](https://github.com/swchen44/cbm-vs-codegraph-bench)）發現，C 最準的引擎其實就是 **clangd + `compile_commands.json`** —— 它在呼叫圖、`#ifdef`、巨集、`typedef`、`_Generic` 都贏 —— 但它沒有為 agent 打包、重啟很慢、也不解析 runtime 的函式指標分派。

**ccq 的存在就是把這個贏家引擎打包給 agent：** 每個問題一個 token 便宜的指令、warm daemon 給速度、fnptr 啟發式補 clangd 唯一缺的、符號級編輯，以及一個能丟進鎖定內網的零相依單一 binary。

**與 CodeGraph 的關係。** ccq 是 *受 CodeGraph 啟發*，並**移植了它的 `c-fnptr-synthesizer.ts`** 啟發式 —— 但跑在 clangd 的編譯器級引擎上（而非 tree-sitter），且維持零相依 Go binary（無 Node bundle）。為了讓熟 CodeGraph 的人無痛上手，`ccq mcp` 提供 **CodeGraph 相容**的 MCP 介面，以 `explore` 為頭牌工具。我們刻意**沒有** fork CodeGraph（它是 MIT，我們可以）—— 把 clangd 後端加進它 5,689 行的 tree-sitter extractor 是「換引擎」，終點只是一個更重的 ccq；見 [docs/benchmark.md §6](docs/benchmark.md)。

## 為什麼用 ccq（解決的痛點）

| 對標工具 | 它的痛點 | ccq 怎麼解 |
|---------|---------|-----------|
| **codebase-memory-mcp (cbm)** | agent 用 grep+Read 燒 token/工具呼叫；grep 追不到呼叫關係；巨集看不見；要快 | clangd **函式級**呼叫圖（cbm 的 C 呼叫圖是檔案級）；巨集展開；warm clangd；精簡輸出 |
| **CodeGraph** | agent 想**一次問到位**；函式指標分派 grep 看不到；索引要保持新鮮 | `ccq explore` 一次回全部；**fnptr 分派啟發式**（CodeGraph 唯一勝過 clangd 的點）；clangd 自動重索引 |
| **Serena** | agent 需要**符號級**導航 + 編輯，而非文字操作 | clangd LSP：定義 / 參照 / **安全 rename** |

**設計論點：** ccq = clangd 的編譯器級正確性（贏呼叫圖、`#ifdef`、巨集、`typedef`、`_Generic`）**＋** CodeGraph 的 `explore` 與 fnptr 合成 **＋** Serena 的符號編輯 **＋** cbm 的速度（warm clangd）。取各家之長，疊在 clangd 上。

> 由來：本設計來自一場 cbm vs CodeGraph vs clangd vs 傳統工具（cscope/ctags/cflow）在 `wpa_supplicant` 與 `redis` 上的對打 benchmark。clangd + `compile_commands.json` 在呼叫圖、`#ifdef`、巨集、`typedef`、`_Generic` 維度勝出；ccq 把這個贏面打包，並補上唯一缺口（fnptr 分派）。

### 它值多少（實測，非宣稱）

一場真實的 Claude Code A/B —— **同一 model、同一 prompt，唯一差別是 `ccq` 有沒有在 `$PATH` 上** —— 直接從每次執行的 token/成本 JSON 量測（[token-cost case study](docs/case-studies/token-cost/README.md)）：

| | 用 ccq vs grep+read |
|---|---|
| **每任務 token** | **少 1.8–7.9×** |
| **每任務成本** | **省 2.1–12.4×**（全套 6.7×） |
| **速度** | ~**快 6×**（例：162s → 13s） |
| **可預測性** | 同樣的事跑三次，沒 ccq 成本最多差 **15×**;有 ccq 幾乎**持平** |
| **完成度** | 一個 **no-build** 函式指標任務，沒 ccq 得 **0%、有 ccq 得 100%** |

也就是說：用 `ccq` 驅動 C/C++ 的 agent，每個問題便宜數倍、快數倍 —— 而在 no-build 程式碼（驅動、ops 表、callback）的函式指標分派上，它能答出 grep+read agent **靜默答錯**的問題。相對地，ccq 只是一個零相依 Go binary 要維護；即使用量打到 1/10，仍回本數倍。完整 ROI 模型與誠實 caveat 見 [case study](docs/case-studies/token-cost/README.md)。

## 功能

**導航**
- `search <q>` — 找符號（workspace symbols）
- `def <sym>` / `show <sym>` — 定義源碼
- `refs <sym>` — 所有參照
- `callers <sym>` — 誰呼叫（clangd call hierarchy **+ fnptr 啟發式**）
- `callees <sym>` — 它呼叫了誰（clangd + **body-scan** + fnptr 分派目標）
- `impact <sym> [-d N]` — 遞移 callers（影響範圍）
- `explore <sym>` — **一次**：源碼 + callers + callees + blast-radius
- `symbols <file>` — 檔案 outline
- `macro <sym>` — 巨集展開 / 簽名（clangd hover）

**編輯（符號級，對標 Serena；dry-run 除非 `--apply`）**
- `rename <sym> <new> [--apply]` — 跨專案安全改名
- `replace-body <sym> <file> [--apply]` — 取代符號整段定義
- `insert-before <sym> <file>` / `insert-after <sym> <file>` — 在符號前/後插入內容

**匯出（用你自己的工具查）**
- `export [--format json|sql|html] [--focus <sym> [-d N]] [--out f]` — 匯出呼叫圖（含 fnptr 邊）。`--format sql | sqlite3 g.db` 用純 SQL 查（零相依、取代 Cypher）；**`--format html` 輸出自包式、離線的互動知識圖**（像 CodeGraph 那種,但由 clangd 驅動）；`--focus <sym>` 只建鄰域（大 repo 快,html 推薦搭配）。
- `fnptr` — 驗證 fn-pointer 對照表（`ccq.fnptr.json`）

**Serve（CodeGraph 相容）**
- `mcp` — 用 **Model Context Protocol**（JSON-RPC/stdio）服務 ccq，零額外相依。提供 `explore`（頭牌）、`callers`、`callees`、`def`、`refs`、`search`、`impact`、`symbols`、`macro` —— 讓 MCP 客戶端、以及熟 CodeGraph 的人零學習成本驅動 ccq。

**專案**
- `init` — 偵測/產生 `compile_commands.json`（CMake / Meson / bear），沒有 build 系統時則產生**無 build 的 `compile_flags.txt`**；暖機 clangd
- `--compdb a.json,b.json` — 指定一個或多個**任意檔名**的 compile database（多 target build 會產生多份、常被改名）。會自動合併;每個不同的組合各自獲得**獨立常駐 clangd**（一個 build config 一個 daemon —— 切 config 不重索引）。見 [docs/design.md §6](docs/design.md)。
- `--config <p>` / `ccq.json` — 設定檔(`./ccq.json` > `~/.config/ccq/ccq.json` > `--config`),用 **`allow`/`deny` regex** 控制哪些檔要索引;`ccq config` 顯示生效設定。
- `wait-index` — **阻塞到索引完成才返回**(agent 先跑這個,避免在索引途中查到不完整結果);`--background` 立即返回、`--rebuild` 強制重建索引。`status` 回報 `ready`/`indexing…`/`not running`。
- `cache [list|clean|path]` — 觀察/清理索引快取(daemon state、staged DB、clangd 的 `.cache/clangd`);顯示大小/日期/專案。`clean --older-than N | --project p | --all [--index]`。
- `doctor` — dump 版本、設定檔、compile DB 模式、cache 大小與 daemon 狀態,幫 debug。
- `status`、`shutdown`、`version`

**差異化**
- **fn-pointer 分派** — `callers`/`explore` 解析 ops-struct 註冊與 `obj->fn()` 分派，合成 `dispatcher → handler` 邊。以 **(struct 型別, 欄位)** 為鍵，不同 struct 同名欄位不互相污染；處理 **designated init、positional table `{"n", fn}`、field←field 傳遞**（移植自 CodeGraph 的合成器），另含 **`union` 函式指標欄位、巢狀 struct 初始化 `.a = { .b = h }`、陣列索引 designator `[N] = {…}`、pointer typedef receiver、以及 deref／cast／跨行分派（`(*pp)->f()`、`((struct T*)v)->f()`、`p->`⏎`f()`）**。clangd 自己不做這個。
- **fn-pointer 對照表** — 對於文字掃描追不到的盲區（callback、間接分派），可在專案根放一個 `ccq.fnptr.json` 宣告 ground truth，與自動掃描合併（JSON、零相依）：
  ```json
  {
    "registrations": [ { "struct": "wpa_driver_ops", "field": "scan2", "handlers": ["wpa_driver_bsd_scan"] } ],
    "links":         [ { "from": "eloop_run", "to": ["wext_scan_timeout"], "note": "eloop timer callback" } ]
  }
  ```
  `registrations` 補某 struct.field 的 handlers；`links` 加直接 `dispatcher → handler` 邊。`ccq fnptr` 驗證對照表。
- **無 build 模式** — 當沒有 `compile_commands.json` 也沒有 build 系統時，`ccq init` 會寫一個 `compile_flags.txt`（自動探測 `-I` include 目錄）。clangd 即能**跨檔**解析（配合 ccq 開檔暖機）**而不需 build** —— cbm 式廣度，精度較低。被過度納入的是**猜出來的 `-I` 集合**；`#ifdef` 則相反——因為**沒有 `-D`**，clangd 以預設值評估條件，**被停用 config 包住的 code 會 inactive → 找不到**（不是「過度涵蓋」）。ccq 的文字路徑不評估 `#ifdef`，補上這個洞：fnptr 啟發式仍看得到停用分支裡的 dispatch，`ccq def`/`search` 也會 fallback 到純文字[定義索引](#differentiators)（明確標示）去找 clangd 漏掉的符號。精度階梯：compile_commands.json > compile_flags.txt > 同檔。
- **`#ifdef`-blind 定義索引(no-build fallback)** — 一個純文字、不評估前處理器的「定義位置」索引(函式/struct/union/enum/typedef/巨集),掃全部檔案、不評估 `#ifdef`。`ccq def`/`search` **只在 clangd 找不到時**才查它,所以藏在**停用** config 裡(no-build 下被 clangd 丟掉)的符號仍找得到 —— 明確標示為近似,絕不混進 clangd 的精確結果。
- **巨集** — clangd 會索引 `#define`；它們會出現在 `ccq search`（kind `macro`），`ccq macro` 可展開。

## 安裝

ccq 是單一靜態 Go binary。**clangd 是唯一的外部相依。**

### npm(最快,適合連網機器）
```bash
npm i -g @swchen44/ccq      # 或:npx @swchen44/ccq <cmd>
```
npm 只會裝符合你平台的 prebuilt binary(darwin/linux x64+arm64、win32 x64)。仍需 PATH 上有 `clangd`。內網/air-gapped 請改用下面的原始碼或 prebuilt binary 安裝(npm 需要 registry)。

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
把 `skills/ccq/SKILL.md` 複製到 agent 的 skills 目錄：
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
| **初次索引 / build** ⚠️ | ~4s | ~11–14s | 需完整 build + 索引 | 需完整 build + 索引 | **需完整 build + ~30s 索引（ccq 最弱項）** |
| 暖重複查詢 | 每次重跑 | 每次重跑 | — | 每次重跑 | **~0.07–0.6s** |
| 編輯 | — | — | rename | ✅ LSP | ✅ rename / replace-body / insert |
| 整合方式 | MCP | MCP | LSP（編輯器） | MCP | **CLI + agent skill + MCP**（`ccq mcp`） |
| 安裝足跡（內網） | 257 MB，vendored build | 188 MB Node bundle | ~100–350 MB binary | 🔴 ~890 套件 + 下載 clangd | **單一 Go binary，0 Go 相依**（需 clangd） |

**強項** ✅ — 唯一通過全部 8 項難 C 特性；保留 clangd 的贏面（`#ifdef`、巨集、`typedef`、`_Generic`、函式級呼叫圖）**並**加上 fn-pointer 分派（+ `ccq.fnptr.json` 對照表補盲區）；重複查詢最快（warm daemon）；「智慧工具」中足跡最輕（零相依 Go binary——內網最佳智慧選項）；符號級編輯 + graph export。

**弱項** ⚠️ — **初次索引最慢**（需真正 build 出 `compile_commands.json` + ~30s clangd 背景索引；tree-sitter 工具數秒、cscope ~0.5s 就好）；**依賴外部 clangd binary**；fn-pointer 啟發式是過度近似（盲區請在 `ccq.fnptr.json` 宣告）；**僅 C/C++**、**預設無 MCP**。ccq 以冷啟動成本換精度，並用 warm daemon 與 `--incremental` 緩解。

完整數據——特性、呼叫圖召回、**索引速度 vs cscope/ctags/cflow**、安裝與相依足跡、使用方式、每個工具的強弱項——都在 **[docs/benchmark.md](docs/benchmark.md)**。

## 限制（Limitations）

ccq 刻意只是 clangd 之上的薄層；它繼承 clangd 的強項，也有幾個誠實的限制。

- **函式指標啟發式（`fnptr`）** — 純文字、刻意*過度近似*：它把一個 dispatcher 連到該 `(struct, 欄位)` 註冊過的**所有** handler（是候選集，不是執行期單一目標）。它**不自動解析**：當作引數傳遞後在他處被呼叫的 callback（`eloop_register_timeout(cb, …)` → 之後 `e->cb()`）、陣列索引分派 `arr[i]->fn()`、回傳值分派 `get_fn()()`、藏在 function-like macro 裡的分派、存在普通（非 struct）變數裡的函式指標、或定義在其他 translation unit 的 extern handler。positional table 與多行註冊為盡力而為。**緩解：** 可在 `ccq.fnptr.json` 對照表手動宣告這些關聯。
- **callees** — clangd 的 `outgoingCalls` 不可靠，所以 `callees` 會 union 函式體掃描（呼叫點對照符號索引驗證）+ fnptr 分派目標。body-scan 仍可能漏掉藏在巨集後的呼叫。
- **callback / 事件分派** — 「先註冊、之後呼叫」流程（eloop/timer/signal）不被解析 —— 這是所有靜態工具（含 cscope、clangd）的共同盲區。
- **無 build 模式精度** — `compile_flags.txt` 讓你不用 build 也能跨檔，但用猜的 include 且無 `-D`：clangd 以預設值評估 `#ifdef`，**被停用 config 包住的 code 會 inactive、找不到**（clangd 端的漏看），依賴巨集的碼也可能錯。緩解：`ccq def`/`search` 會 fallback 到純文字、不評估 `#ifdef` 的[定義索引](#differentiators),仍能定位這類符號（標示為近似）。要 config 精準請用真的 `compile_commands.json`。
- **冷啟動與規模** — 第一次查詢會啟動 daemon 並索引整個 repo（redis 約 ~30s）；clangd 索引也吃與 repo 大小成正比的記憶體。暖查詢亞秒。有持久化索引時,`--incremental` 只開 git 變動的檔(暖重啟約快 2.4×);在驗證穩定前為 opt-in,預設仍走完整 OpenAll。
- **相依 / 範圍** — 需要 `clangd` binary（引擎），且為了最佳精度需要 compile database。**僅 C/C++**（跨語言廣度是 cbm 這類 tree-sitter 工具的領域）。
- **多份 compile DB（`--compdb`）** — clangd 對同一檔只取**一筆** entry,所以當一個檔被多個 target 用不同 `-D` 編譯時,**排第一的 `--compdb` 勝出**,其他 config 的 `#ifdef` 分支會 inactive。請依此排序 `--compdb`,或逐 target 各查一份 `--compdb` 取精確視圖。實例:[docs/case-studies/multi-target-compdb](docs/case-studies/multi-target-compdb/README.md)。

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

> 文件：[case studies](docs/case-studies/)（實例 + 圖,用看的不用聽的）· [design.md](docs/design.md) · [requirement.md](docs/requirement.md) · [benchmark.md](docs/benchmark.md)

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
- 除了測試套件，[case studies](docs/case-studies/) 在真實 repo（redis、wpa_supplicant）實跑 ccq —— **抓到並修了 9 個單元測試漏掉的真 bug**（[bugs-found.md](docs/case-studies/bugs-found.md)）。寫 case study 本身就是測試。

### 釋出流程與版本
- **Stable**：`git tag vX.Y.Z && git push origin vX.Y.Z` → `release.yml` 編全平台並發 GitHub Release（SemVer；見 `CHANGELOG.md`）。
- **Nightly**：`nightly.yml` 每晚（18:00 UTC）刷新一個滾動的 `nightly` prerelease，含最新 `main` 各平台 binary。手動：在 *nightly* workflow 點「Run workflow」。
- 本機全平台編譯：`make release`（`./build-release.sh`），加 `--bundle-clangd` 內嵌 clangd。

### 貢獻
維持零相依（只用 stdlib）、`gofmt` 乾淨、`go vet` / golangci-lint 通過；新邏輯加單元測試（碰到 clangd 路徑則加整合測試）。

## 版本歷史

| 版本 | 日期 | 重點 |
|------|------|------|
| [**0.6.4**](https://github.com/swchen44/ccq/releases/tag/v0.6.4) | 2026-07-01 | fnptr 解析更多分派寫法(union 欄位、巢狀 struct 初始化、陣列索引 designator、pointer typedef receiver、deref/cast/跨行分派);無誤報回歸 |
| [0.6.3](https://github.com/swchen44/ccq/releases/tag/v0.6.3) | 2026-06-30 | fnptr 涵蓋更多註冊寫法(typedef 表、巢狀、混用、cast/macro);release 加測試關卡 |
| [0.6.2](https://github.com/swchen44/ccq/releases/tag/v0.6.2) | 2026-06-30 | npm 安裝(`npm i -g @swchen44/ccq`);skill 移到 `skills/ccq/`;明確 C/C++ only 語言範圍 |
| [0.6.1](https://github.com/swchen44/ccq/releases/tag/v0.6.1) | 2026-06-29 | 文件:實測 ROI case study(token/成本/完成度/可預測性 A/B);requirement 與 design 同步 |
| [0.6.0](https://github.com/swchen44/ccq/releases/tag/v0.6.0) | 2026-06-29 | `ccq mcp`;`--compdb`(多 target);`ccq.json` allow/deny 索引過濾;`wait-index`;`cache`;`doctor` |
| [0.5.0](https://github.com/swchen44/ccq/releases/tag/v0.5.0) | 2026-06-28 | --incremental 懶開索引(只開變動檔;暖重啟 ~2.4× 快,opt-in) |
| [0.4.0](https://github.com/swchen44/ccq/releases/tag/v0.4.0) | 2026-06-28 | fn-pointer 對照表、replace-body/insert、callees body-scan 修正、git-diff 暖重啟 |
| [0.3.0](https://github.com/swchen44/ccq/releases/tag/v0.3.0)（首個公開版） | 2026-06-27 | fnptr 升級（複合鍵、positional、field←field）、no-build 模式、巨集搜尋、graph export |
| 0.2.0（里程碑） | 2026-06-26 | warm-clangd daemon（亞秒暖查詢） |
| 0.1.0（里程碑） | 2026-06-26 | 導航 + rename + fnptr 啟發式 |

完整紀錄：[CHANGELOG.md](CHANGELOG.md)。最新 binary：[Releases](https://github.com/swchen44/ccq/releases)（穩定）·[nightly](https://github.com/swchen44/ccq/releases/tag/nightly)。

## Roadmap / TODO

- [x] `callees` 改用函式體掃描（clangd 的 `outgoingCalls` 不可靠；改從函式體建）— 0.4 完成
- [x] 更多編輯：`replace-body`、`insert-before/after`（對標 Serena）— 0.4 完成
- [x] fn-pointer 對照表（`ccq.fnptr.json`）補盲區 — 0.4 完成
- [x] **完整 git-diff 增量**（`--incremental`）— 暖重啟只開變動的檔;query path 按需開目標檔。redis 冷啟動約快 2.4×、結果一致 — *0.5 完成（opt-in）*
- [ ] `ccq init` 支援更多 build 系統（Bazel、xmake）
- [x] fnptr 啟發式：positional table 邊界、typedef 表、巢狀 row、混用、cast/macro handler + 註解感知多行解析 — *0.6.3 完成*
- [x] fnptr 啟發式：更多分派寫法（union 欄位、巢狀 struct 初始化、陣列索引 designator、pointer typedef receiver、deref/cast/跨行分派）— *0.6.4 完成*

## License
MIT。重用了 `troberti/clangd-query`（MIT）、`mpsm/mcp-cpp`、`2015xli/clangd-graph-rag` 驗證過的架構想法。
