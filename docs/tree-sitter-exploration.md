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

**先講結論:先不採用 tree-sitter。** 它修得掉「宣告形式」的 regex edge-case,但**對真實 macro-heavy C 反而不可靠**,加上 +6MB binary、速度較慢——CP 值不划算。ccq 現行「regex + 逐案修 edge-case」(wpa `.scan2` 已 5/5)是更好的取捨。

### 實測數據
| 面向 | 結果 | 判定 |
|---|---|---|
| pure-Go / `CGO_ENABLED=0` build | ✅ 過(gotreesitter 無 cgo、無 wazero/wasm glue) | 可行 |
| `#ifdef`-blind | ✅ `hidden_fn`(在 `#ifdef NEVER_DEFINED` 內)看得到 | 符合需求 |
| regex edge-case(col-0 K&R 名字) | ✅ `kr_style_fn`(回傳型別在前一行)正確解析 | tree-sitter 勝 regex |
| binary 大小 | 空 Go 1.7MB → +C grammar **7.7MB**(+6MB;只嵌 C 可控) | 可接受但明顯 |
| 速度 | 237KB 檔 ~244ms(~1MB/s) | 比 regex 慢 |
| **macro-heavy 真實 C 可靠度** | ❌ `driver_nl80211.c`(極 macro-heavy):gotreesitter **20** vs cflow ~87(嚴重低估);`driver_bsd.c`:**54** vs cflow ~24(高估)。兩方向都對不上 | **不可靠** |

### 關鍵洞察
- tree-sitter 對**乾淨宣告形式**比 regex 穩健(修掉 col-0 K&R 這類 edge-case),**但對 macro-heavy C(nl80211 這種)會嚴重誤解析**——正是它 C=0.58 的已知弱點(macro 不進 AST)。**它是「換一組 edge-case」,不是「消滅 edge-case」。**
- wazero+tree-sitter 路線(原計畫)另外還不成熟(malivvan 3⭐/ngavinsir WIP,emscripten glue);gotreesitter 是更好的純 Go 路線,但卡在上述 macro 可靠度。
- **macro 精度本來就要 clangd**——tree-sitter 幫不上,這點不變。

### 決策
**不採用**(現況 regex + 逐案修 + clangd 精度已在 benchmark 勝出:wpa 5/5、呼叫圖 96%)。**重啟條件**:gotreesitter(或同類純 Go runtime)對 macro-heavy C 的解析品質明顯改善、或找到可設定的解法(已排除:node budget `GOT_PARSE_NODE_LIMIT_SCALE` 無效)。clangd 仍為精度/macro 引擎。

> 實測 spike 在 `$CLAUDE_JOB_DIR/tmp/tsspike`(隔離 module,未動 ccq go.mod;job 清理時自動消失)。
