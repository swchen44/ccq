# 探索計畫：用 tree-sitter 取代 fnptr/cindex 的 regex 結構解析(+ benchmark)

> 給未來 session 接手。**只信 git 與測試/benchmark 的真實輸出。**

## Context(為什麼)

ccq 有兩個純文字、`#ifdef`-blind 的層(補 clangd 在 no-build 的洞):
- `internal/fnptr` — 函式指標分派合成(ops 表 registration + `obj->fn()` dispatch)。
- `internal/cindex` — 定義索引(no-build 下 `def`/`search` 的 fallback)。

兩者都用 **regex 啟發式** 解析 C 結構,因此有 **edge-case 漏洞**——本 session 就靠 benchmark 抓到並修了兩個:
- **column-0 函式名**(回傳型別在前一行,`reFuncDefHdr` 的 `^[A-Za-z_]` 吃掉首字母)→ commit `780bf5a`。
- **指令式 `obj.field = handler;` 註冊**(非大括號初始化)→ commit `5d76dbc`。

這類 bug 是 regex 解析 C 宣告形式的本質脆弱性。**tree-sitter 的 AST 對這些宣告形式天生穩健**,能一次消掉一整類 edge-case,不用逐一補 regex。

### 關鍵界線(先講清楚,免得走錯方向)
- tree-sitter **只幫「結構解析穩健度」**,**不幫 macro**:tree-sitter **不展開 macro**(把 `#define`/`MACRO(x)` 當 token),macro-heavy C 品質只有 0.58(codebase-memory arxiv 論文原文)。macro 展開仍是 **clangd** 的事。所以本探索**不碰 macro**,clangd 仍是精度/macro 引擎。
- tree-sitter **對 `#ifdef` 跟現況一樣**(都不評估,兩分支全看)——維持 `#ifdef`-blind 優勢,不變。
- **工程硬約束**:ccq 是 pure-Go / `CGO_ENABLED=0` / 五平台靜態 binary + npm。**原生 tree-sitter Go binding 需 cgo → 否決**(打掉發佈模型)。唯一可行路徑是 **wazero**(純 Go WASM runtime)跑 `tree-sitter-c.wasm`——保住 pure-Go。

## 目標
評估「tree-sitter(via wazero)做結構解析」能否在**不破壞 pure-Go 發佈**下,**顯著減少 regex edge-case 漏洞**,且速度/binary 大小可接受;用 benchmark 數字決定要不要採用。

## 範圍
- **做**:tree-sitter-c 抽「定義」(func/struct/union/enum/typedef)與「ops 表 registration / dispatch site」的結構,對照現有 regex 版。
- **不做**:macro 展開(clangd 的事)、`#ifdef` 評估(維持 blind)、把 clangd 換掉(clangd 仍是呼叫圖/型別精度主力)。

## 階段

### Phase 0 — wazero + tree-sitter-c spike(可行性關卡)
- 引入 `github.com/tetratelabs/wazero`(純 Go,無 cgo);取得/vendored `tree-sitter-c.wasm` + tree-sitter runtime wasm(或用已編好的 web-tree-sitter 產物)。
- 寫最小 PoC:parse 一個 `.c`,walk AST,列出 `function_definition` / `struct_specifier` 的名字+行號。
- **關卡指標**:① `CGO_ENABLED=0 go build` 仍過、五平台交叉編仍過;② binary 增加多少(bundled .wasm 大小);③ 解析速度(wazero WASM 對比 regex,單檔 + 全 repo)。
- 若 ① 破功或 ②③ 不可接受 → **停,記錄結論**(regex 續用)。

### Phase 1 — tree-sitter 版定義索引(cindex-ts),與 regex 版對打
- 新增 `internal/cindexts`(或 cindex 的 backend 選項),用 tree-sitter AST 產 `Def{name,file,line,kind}`。
- **benchmark**(擴充現有 harness / 新增 fixture 集):
  - **edge-case fixture 集**:蒐集 regex 已知會漏的宣告形式(column-0 名、指令式賦值、fn-ptr typedef、跨行簽名、K&R 舊式參數、`__attribute__`/巨集修飾的簽名…),各一個 fixture,比對 regex 版 vs tree-sitter 版的定義召回。
  - **真實 repo**:wpa/redis 上,兩版的定義召回 / fnptr 5/5 是否維持 / 有無新誤報。
  - **成本**:index 速度、binary 大小、記憶體。
- **判準**:tree-sitter 版要 (a) 修掉一批 regex edge-case、(b) 不新增誤報(守門測試全綠)、(c) 速度/大小可接受、(d) 保 pure-Go。

### Phase 2 — 若 Phase 1 勝出,推廣到 fnptr 的結構解析
- 把 fnptr 的 struct-layout(`reStructAny`/`reFieldFnPtr`)、registration(`reInitHdr`/`scanRow`)、dispatch site(`reArrowCall`)改用 tree-sitter AST。
- 重跑 `internal/fnptr` 全套件(目前 43 PASS / 3 SKIP)+ wpa `.scan2` 5/5 + redis 13,確認不回歸、edge-case 減少。

## Benchmark 方法(用數字比較,可複現)
沿用 `~/git/cbm-vs-codegraph-bench`:
- 新增 `bench/edge_cases/` fixture 集(上述宣告形式),兩版跑「定義召回」數字。
- `bench/ccq_adapter.sh` 加一欄 **ccq (tree-sitter)** vs **ccq (regex)**:定義召回 / fnptr `.scan2` 5/5 / index 速度 / binary 大小。
- 產出對照表寫進 REPORT.md,誠實列出 tradeoff(edge-case 少了幾個 vs 速度/大小/複雜度代價)。

## 決策準則(採用 or 不採用)
**採用**若同時滿足:pure-Go 保住 + edge-case 明顯減少 + 守門測試不回歸 + 速度/大小可接受。
**不採用**(regex 續用 + 逐案補)若:wazero 破壞交叉編 / WASM 太慢 / binary 膨脹過大 / edge-case 減少有限。
—— 不確定就**留在 regex**(現況 wpa 5/5、呼叫圖 96% 已勝過 cbm/CodeGraph,tree-sitter 是「更穩健」不是「更能幹」)。

## 現況錨點(2026-07)
- ccq 已發 **v0.6.5**(npm `@swchen44/ccq` latest=0.6.5)。fnptr 43 PASS / 3 SKIP;wpa `.scan2` 5/5、redis 13(整合測試固化於 `cmd/ccq/bench_test.go`)。
- benchmark:`swchen44/cbm-vs-codegraph-bench`,ccq scorecard 在 `results/{wpa,redis}/ccq-scorecard.md`,REPORT §0.5 / §3.7。

---

## Phase 0 結果與結論(2026-07,已實跑)

在隔離 scratch module 實測 `odvcencio/gotreesitter@v0.20.7`(純 Go tree-sitter runtime,build tag `grammar_subset grammar_subset_c`,`CGO_ENABLED=0`)。

**先講結論:先不採用 tree-sitter。** 它修得掉「宣告形式」的 regex edge-case,但**一個迭代巨集就會讓它級聯崩潰、吞掉整個檔尾**(nl80211.c 254 個函式只抓到 20;見「第 6 項深追」),加上 +6MB binary、速度較慢——CP 值不划算。ccq 現行「regex + 逐案修 edge-case」(wpa `.scan2` 已 5/5)是更好的取捨。

### 實測數據

**注意:這六項不是同一種比較,分三類**——先看「對照基準（和什麼比）」欄再讀,才不會誤會：
- **A 可行性關卡**（第 1、2 項）：和 **ccq 的硬需求** 比，pass/fail，不是跟對手比。
- **B vs 現行 regex**（第 3、4、5 項）：和 **ccq 目前的 regex 做法** 比優劣/成本。
- **C vs 多個基準**（第 6 項）：和 **clangd（compiler-grade 權威）、ccq-regex、cflow** 算出的函式數比，看它在真實大檔準不準。

| # | 面向 | 對照基準（和什麼比） | gotreesitter 實測 | 判定 |
|---|---|---|---|---|
| 1 | pure-Go / `CGO_ENABLED=0` build | **A** ccq 硬需求：無 C toolchain、五平台交叉編（原生 cgo binding **過不了**這關） | ✅ build 過（無 cgo、無 wazero/wasm glue） | 通過關卡 |
| 2 | `#ifdef`-blind | **A** 需求：要看得到停用 config 內的 code（**clangd no-build 看不到**） | ✅ `#ifdef NEVER_DEFINED` 內的 `hidden_fn` 抓得到 | 符合需求 |
| 3 | 宣告形式穩健度 | **B vs ccq 的 regex**：col-0 K&R 名字（回傳型別在前一行）是 regex 這輪才手動修好的 edge-case | ✅ 免修就正確解析 `kr_style_fn` | tree-sitter 勝 regex |
| 4 | binary 大小 | **B vs 現行 ccq**（純 regex、無此依賴）：空 Go 1.7MB → 加 gotreesitter+C grammar | **7.7MB**（+6MB；只嵌 C 可控） | 成本：明顯變大 |
| 5 | 解析速度 | **B vs 現行 regex 掃描**（regex 掃全 repo 很快） | 237KB 檔 ~244ms（~1MB/s） | 成本：較慢 |
| 6 | 真實大檔結構解析準不準 | **C 四方對照**：clangd（權威）/ ccq-regex / cflow / gotreesitter，比每檔的函式數 | **nl80211.c**（8813 行）：clangd 254 · regex 139 · cflow ~87 · **gotreesitter 20**；**bsd.c**：42 · 46 · ~24 · 54 | **不可靠**（見下深追） |

> 為什麼要多個基準：A 先確認「這條路對 ccq 合法嗎」（pure-Go、#ifdef-blind）；B 問「比現行 regex 划算嗎」（穩健度賺一點、大小/速度賠）；C 問「它在真實大檔準不準」。**第 6 項是致命傷**：nl80211.c 有 254 個函式（clangd 權威）、regex 也抓到 139，但 **gotreesitter 只抓到 20** —— 差一個數量級。（四個工具數字不同,因為各自定義「函式」略異——clangd 含宣告、regex 抓「有 body 的定義」、cflow 只算它解析得到的——但 gotreesitter 的 20 是**唯一離譜的低**。）

### 關鍵洞察
- tree-sitter 對**乾淨宣告形式**比 regex 穩健(修掉 col-0 K&R 這類 edge-case),**但一個「迭代巨集」就讓它級聯崩潰、吞掉整個檔尾**(見下深追)——不是「換一組 edge-case」,是**引入了 regex 沒有的「級聯失敗」風險**。
- wazero+tree-sitter 路線(原計畫)另外還不成熟(malivvan 3⭐/ngavinsir WIP,emscripten glue);gotreesitter 是更好的純 Go 路線,但卡在上述 macro 可靠度。
- **macro 精度本來就要 clangd**——tree-sitter 幫不上,這點不變。

### 決策
**不採用**(現況 regex + 逐案修 + clangd 精度已在 benchmark 勝出:wpa 5/5、呼叫圖 96%)。**重啟條件**:gotreesitter(或同類純 Go runtime)對 macro-heavy C 的解析品質明顯改善、或找到可設定的解法(已排除:node budget `GOT_PARSE_NODE_LIMIT_SCALE` 無效)。clangd 仍為精度/macro 引擎。

> 實測 spike 在 `$CLAUDE_JOB_DIR/tmp/tsspike`(隔離 module,未動 ccq go.mod;job 清理時自動消失)。

### 第 6 項深追:「這側線」不是 macro 密度,是迭代巨集造成的級聯崩潰

一開始以為 nl80211.c 是「macro-heavy」才解析不好,**查了發現它只有 3 個 `#define`——根本不是 macro-heavy**。真正原因用 bisect 釘到了:

- `head -N` nl80211.c,function 數在 **line ~400 卡在 20,之後(800/1600/3200 行)永遠是 20**。→ 約 line 408 有個構造讓 parser 崩,**後面 ~230 個函式全被吞進 ERROR 節點**。
- breaker 是 `family_handler()` 裡的:
  ```c
  nla_for_each_nested(mcgrp, tb[...], i) {   /* 展開成 for 的「迭代巨集」 */
      ...
      break;
  };
  ```
  `nla_for_each_nested(...)` 是**展開成 `for` 迴圈的巨集**。tree-sitter **不展開 macro**,只看到「`call_expression` + `{區塊}` + `;`」,這破壞 parse,error-recovery **無法在函式邊界 resync → 級聯吞掉檔尾**。

**最小重現(已實跑):**
```c
int before(void){ return 1; }
static int f(void *l){ foreach_thing(x, l, i) { do(x); }; return 0; }
int after1(void){...} int after2(void){...} int after3(void){...}
```
- 有迭代巨集 → gotreesitter 只抓到 **1**(`before`),f/after1/2/3 全被吞。
- 把 `foreach_thing(...)` 換成真 `for(...)` → **5 個全抓到**。

這類迭代/控制流巨集(`nla_for_each_nested`、`list_for_each`、`for_each_*`)在 kernel/netlink/driver code **極其常見,一個就夠**。

**反直覺的關鍵結論(這才是真正的 takeaway):**
> **regex 是「局部失敗」——壞一行只影響那一行;tree-sitter(無前處理)是「級聯失敗」——一個沒展開的巨集構造殺掉整個檔尾。** 對雜亂的真實 C,**regex 的局部失敗模型反而比 tree-sitter 的級聯失敗更穩健**。這推翻了「tree-sitter 一定更穩健」的直覺——沒有 preprocessing 的 tree-sitter,在真實 driver code 上**可能比 regex 更差**。

### 如果真要用 gotreesitter,怎麼改善?

按「修得掉/修不掉」排序:

1. **前置中和迭代巨集(治標)**:parse 前用 regex 把已知 `*_for_each*` / `foreach*` 巨集呼叫換成真 `for(;;)`。問題:(a) 要維護巨集名單、per-codebase 脆弱;(b) **這又是 regex 前處理**,把我們想逃離的 regex 脆弱性請回來;(c) 未知的迭代巨集照樣崩。
2. **ERROR 節點 resync(治標)**:偵測到 ERROR 就從下一個檔案範圍 `}` 或「型別開頭」重啟 parse。複雜,且 gotreesitter 的 recovery 已經沒在函式邊界 resync 了,要自己補。
3. **輕量 preprocessing(治本但變質)**:先展開巨集再 parse(就是 clangd / cbm 的 simplecpp 做的)。這**治本**,但(a)違背「不前處理、純結構」的初衷,(b)重、複雜——等於把 ccq 變成另一個 cbm。macro 精度本來就該交給 **clangd**。
4. **hybrid**:乾淨檔用 tree-sitter,ERROR 比例高就 fallback 回 regex。為一個 regex 已能處理的層加複雜度,邊際效益低。

**結論**:治本的唯一辦法是 preprocessing(clangd 的事),而那違背初衷;治標的都把 regex 脆弱性請回來。**所以 gotreesitter 沒有乾淨地贏過 ccq 的 regex——regex 的「局部失敗」對雜亂真實 C 反而是優點**。維持 no-go。
