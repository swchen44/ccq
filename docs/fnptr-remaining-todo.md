# ccq fnptr 修復進度（對抗性測試挖出的漏報）

> 給未來 session 接手用。**只信 git 與測試的真實輸出，不要從任何對話歷史推斷狀態。**

## 現況（2026-07-01，v0.6.4，已完成）
- repo：`~/git/ccq`（GitHub `swchen44/ccq`），版本 **0.6.4**，已 tag `v0.6.4` 並發 npm（`dist-tags.latest = 0.6.4`）。
- 環境：`export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"`；clangd 在 `/usr/bin/clangd`；macOS 無 `timeout`。
- 對抗性測試：`internal/fnptr/adv_test.go` + `internal/fnptr/testdata/adv_*.c`（**純 unit test，無 build tag，每次 `go test ./...` 都會跑**）。
- 現況：`go test ./internal/fnptr/` = **41 PASS / 3 SKIP / 0 FAIL**（起點是 34 PASS / 10 SKIP）。

## 已修（v0.6.4，7 個漏報，TDD 逐一修、單獨 commit）
對每個 case 的做法都是：把該測試的 `t.Skip` 改真 assert →先看 FAIL →改 `internal/fnptr/fnptr.go` →PASS →全套件不回歸 + `-race` 綠。

| commit | case | 修法 |
|---|---|---|
| `adf80a7` | **B7 union** | `reStructAny`/`reInitHdr` 加 `union`，union 的 fn-ptr 欄位才會被掃進 layout |
| `7b6b6e1` | **B6 array designator** | `scanRow` 認 `[N] = {…}`，剝前綴後右側當 row 遞迴 |
| `4ce0e8f` | **B5 nested struct init** | `fieldInfo.StructType` 記錄欄位持有的內層 struct；designated 值是 `{…}` 時用內層 layout 遞迴 |
| `5d6e06d` | **A3 pointer typedef recv** | 掃 `typedef struct TAG *ALIAS;` 建 alias→tag 表，`recvType` 解析時映射 |
| `7e829c0` | **A5/A6/A7**（deref / cast / 跨行） | dispatch 掃描從「逐行 + 只認裸 ident」重構成**全檔（`stripCodeLine` 後 join）掃描**：`reArrowCall` 容忍跨行（`\s` 含 `\n`），`parseReceiver`/`parseInner` 解析括號接收者（剝 cast `(struct T*)`、deref `*`、`&`）。`resolveDispatch` 保守優先序：**cast 型別（須∈owners）→ recvType → 唯一 owner → 否則 drop** |

### 真實召回抽查（A5/A6/A7 重構後，無回歸）
- wpa：`./ccq callers wpa_driver_wext_scan -p ~/git/cbm-vs-codegraph-bench/repos/wpa_supplicant` 仍 **3** 個 scan2 分派者。
- redis：`./ccq callers lookupCommand -p ~/git/cbm-vs-codegraph-bench/repos/redis` 仍 **13** 個 caller。
- （此抽查目前是**人工**驗，尚未 codify 成自動回歸測試。）

## 最高原則（永遠成立）
**寧可漏報，絕不誤報。** 任何修復若讓防誤報守門測試變紅，就不做、維持 SKIP。
守門測試（必須自始至終 PASS）：`TestCrossbleed`、`TestDataFieldNotBridged`、`TestAdvFuncNameCollision`、`TestAdvSameHandlerTwoKeys`、`TestAdvNullRegistration`、`TestAdvRegToFnPtrVar`、`TestAdvAmbiguousConservative`。

## 維持 SKIP（3 個，by design / deferred，**刻意保留，硬修會誤報**）

這 3 個是「**已知的、被接受的漏報**」。共通邏輯：把它們補起來，會違反「寧可漏報絕不誤報」——要嘛放寬守門（製造誤報），要嘛需要超出純文字啟發式的能力（preprocessor / 資料流分析）。維持 SKIP，並用 `ccq.fnptr.json` 覆寫表作為使用者的逃生口。

### 1. E1 `TestAdvExternGate`（extern-only handler）
fixture `adv_extern_gate.c`：
```c
struct bxt { int (*xtop)(int); };
extern int bxt_ext(int x);              // 只有宣告，專案內無定義
static struct bxt BXT = { .xtop = bxt_ext };
int bxt_d(struct bxt *p){ return p->xtop(1); }
```
- **為何漏**：`addReg` 有一道 **real-function gate**——只有「專案內找得到定義」的名字才會被登記成 handler。`bxt_ext` 只有 `extern` 宣告、定義在別的 translation unit，所以被 gate 丟掉。
- **為何不修**：這道 gate 正是**防誤報的核心**。它擋掉「初始化值長得像函式名、其實是巨集常數/型別/變數」的一大票假 handler。一旦放寬成「任何 extern 名都收」，等於把 gate 拆了，誤報會大量湧入。
- **代價**：少數「真的定義在另一個 TU 的 extern handler」會漏。可接受（漏報），且使用者可用 `ccq.fnptr.json` 的 `registrations` 手動補。

### 2. C3 `TestAdvMacroHiddenDispatch`（巨集藏分派）
fixture `adv_macro_dispatch.c`：
```c
#define CALL(p) p->cmfire()
struct cmd2 { int (*cmfire)(void); };
static struct cmd2 CMD2 = { .cmfire = cmd2_h };
int cmd2_user(struct cmd2 *p){ return CALL(p); }   // 呼叫點只看得到 CALL(p)
```
- **為何漏**：dispatch（`p->cmfire()`）藏在 function-like macro `CALL` 的展開結果裡。原始碼的呼叫點只有 `CALL(p)`，純文字掃描看不到 `->cmfire(`。
- **為何不修**：要看到它必須做**前處理器展開**，那是 clangd 的領域，不是純文字啟發式該重做的事（自己做 macro 展開既重又脆）。
- **代價**：藏在 macro 裡的分派會漏。clangd 本身的 call hierarchy 在有 `compile_commands.json` 時可能補到一部分；否則用 `ccq.fnptr.json` 的 `links` 手動補。

### 3. B4b `TestAdvRegToFnPtrVarIndirect`（註冊到 fn-pointer 變數）
fixture `adv_fnptr_var.c`：
```c
struct bfv { int (*fvop)(void); };
static int bfv_target(void){ return 1; }
static int (*bfv_gvar)(void) = bfv_target;   // 變數指向真正的 target
static struct bfv BFV = { .fvop = bfv_gvar };// 欄位註冊到「變數」，不是函式
int bfv_d(struct bfv *p){ return p->fvop(); }
```
- **為何漏**：`.fvop = bfv_gvar` 把欄位註冊到一個 **fn-pointer 變數**，而不是函式。real-function gate 看到 `bfv_gvar` 不是已定義函式 → 丟掉（這同時也**防止**「無中生有一條指向變數的假邊」）。真正的目標 `bfv_target` 要再追一層「變數→它的初始值」才看得到。
- **為何不修**：追「變數指向哪個函式」需要**資料流分析**（變數可能在別處被重新賦值、條件賦值…），超出文字啟發式範圍，且容易誤報。
- **代價**：經由 fn-pointer 變數間接註冊的 handler 會漏。用 `ccq.fnptr.json` 手動補。

> 這 3 個的 `t.Skip` 訊息已標 **"BY DESIGN / deferred"**，不要改 `fnptr.go` 去「修」它們。

## 尚未做（非漏報，視需要才做）
1. **對抗 fnptr 案例補 integration/E2E**：目前對抗矩陣只在 **unit** 層（直接呼叫 `fnptr.Callers/Callees`，跟 CLI 同一函式）。integration（`cmd/ccq`，`-tags integration`）只跑基本 callers/callees。邊際價值低——clangd 不影響 fnptr 的純文字啟發式。
2. **wpa/redis 召回自動化**：把人工抽查寫成回歸測試（但依賴外部 repo 的固定路徑，不易移植）。

## 驗證指令（每項都要看真實輸出）
1. `gofmt -l internal/fnptr/`（空）、`go vet ./internal/fnptr/`、`go test ./internal/fnptr/ -v`、`go test -race ./internal/fnptr/`、`go test ./...`、`go test -tags integration ./...`。
2. 真實召回抽查見上（wpa 3 / redis 13），跑完 `pkill -f clangd`。

## 誠實備註
更早的 session 曾出現**未實際執行卻報告成功**的情況。請務必每步用 git/測試真實輸出佐證、交叉驗證（`git rev-parse HEAD origin/main`、`gh run view`、`curl` npm），不要相信「我記得做過」。
