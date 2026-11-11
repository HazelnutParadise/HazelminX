# 010 — 整合者可以用 /management/v1 的分頁、版本條件與工作回應操作資源

**Epic:** API、CLI 與整合（`epic:integration`）  
**User Story:** 整合者可以用同一套規則操作平台  
**Milestone:** M0 基礎與設計  
**Blocked by:** 003 管理員可以啟動 hazelminx-core 並通過健康檢查, 008 使用者可以提交工作後關閉頁面，之後仍能查到進度、證據與結果  
**規格章節:** 36｜管理 API 與事件契約  
**Issue:** [#10](https://github.com/HazelnutParadise/hazelminx/issues/10)  

## What it delivers

36 章契約：分頁上限、篩選排序、If-Match 版本條件、202 工作 ID、錯誤格式（code／message／requestId／retryable／evidence／suggestedActions）、OpenAPI JSON 由路由產生、/management/v1 總覽端點。

## Acceptance criteria

- [x] **INT-001** 使用者可以以版本化 API 查詢與操作授權資源。 — 完成條件：文件與伺服器一致；長工作回工作 ID；錯誤有穩定代碼與白話說明。 — 證據：每個路由都經 `Server.handle` 登記一筆 `Operation`，總覽與 `GET /management/v1/openapi.json` 都由同一份註冊表產生（`core/internal/api/operations.go`、`openapi.go`）。`TestRoutesMatchOperations` 檢查實際路由與註冊表一一對應，`TestOpenAPIDocumentMatchesServer` 檢查每個操作的 operationId、dryRun／版本／列表標記與錯誤代碼都寫在文件裡。長操作回 `202`、`Location` 與 `{jobId, job, links}`（`TestLongOperationAnswer`）。錯誤代碼表 `errcodes.go` 每個代碼都有固定狀態與白話說明。
- [x] 新管理 API 與 infra-manager 舊 /api/ 路徑不衝突 — 證據：`TestRoutesMatchOperations` 確認所有路由都在 `/management/v1` 或 `/healthz` 底下，`/api/...`、`/use/...` 不會進到管理 API 的驗證與處理流程。
- [x] 錯誤代碼穩定且文件與伺服器一致（契約測試） — 證據：`TestErrorCodesComeFromTheTable` 掃描原始碼，確認交給 `fail`、`newError` 的代碼都在表內。`TestErrorResponsesFollowTheTable` 實際送出 11 種失敗請求，比對 HTTP 狀態、預設可否重試與 `traceId`。OpenAPI 的 `Error.code` 列舉與錯誤表完全相同。
- [x] （增補 03 ROB-008，擁有票 017）列表端點的回應中繼資料包含每頁筆數、查詢上限與逾時；超過上限或逾時時可改成背景工作。 — 證據：`GET /jobs`、`/users`、`/audit-events` 回 `{items, page}`，`page` 含 `limit`、`queryCap`、`timeoutSeconds`、`total`（逾時為 `null`）、`capped`、`timedOut`、`backgroundQuery`（`listing_test.go` 的 `TestRunList_*`，包含 25 筆分三頁走完、上限 30 時第 30 筆後停止並回 `QUERY_LIMIT_EXCEEDED`、計數逾時、讀取逾時）。把 `backgroundQuery` 送到 `POST /management/v1/list-queries` 會建立 `list.query` 工作，結果可分頁讀取，只有提出者看得到，7 天後回 `410`（`TestBackgroundListQuery`）。實際執行檔也以 curl 跑過一次。
- [x] （增補 03 51.1）畫面停用的動作，對應的 API 一律拒絕，並回傳 51.1 錯誤代碼表的代碼。 — 證據：51.1 的六個代碼都在錯誤表，狀態與增補相同。「控制者與能不能寫入」表只寫在 `WriteBlockers`（`writegate.go`），畫面與 API 都問同一個函式（`writegate_test.go` 涵蓋表中每一列與不認得的組合）。在 008 的 `POST /jobs` 上示範：平台降級回 `503 PLATFORM_DEGRADED`，但已接受的 requestId 照樣回原工作（`TestPlatformDegradedRefusesNewJobs`）。同一資源有待確認變更時回 `409 CONFIRMATION_REQUIRED`，錯誤內容不含確認 ID（`TestPendingConfirmationBlocksSameResource`）。`RESOURCE_READ_ONLY`、`CONTROLLER_CONFLICT`、`SCOPE_DEGRADED`、`RESOURCE_REFERENCED` 要等有受管資源端點的票接上 `ResourceState`。
- [x] （增補 03 ROB-003，擁有票 107）會改變受管資源的端點支援 `dryRun=true` 影響預覽，並在 API 文件標記。 — 證據：`Operation.DryRun` 標記的端點在 OpenAPI 帶 `x-hazelminx-dry-run` 與 `dryRun` 參數，作為核對清單。目前會改變受管資源的端點只有 `POST /jobs`，它的預覽會寫出是否視為重送、會排在哪些工作後面與空間是否足夠，不建立工作、不保留 requestId、不佔鎖，權限與真實寫入相同（`TestJobSubmitDryRunChangesNothing`、`TestPreviewDoesNotRecordOrLock`）。沒標記的寫入端點收到 `dryRun` 回 `400 DRY_RUN_UNSUPPORTED`，不會照常執行（`TestErrorResponsesFollowTheTable`）。影響圖本身由票 107 提供。
- [x] （增補 03 ROB-010，擁有票 024）提供 `POST /management/v1/confirmations/{id}` 確認端點。 — 證據：`GET`／`POST /management/v1/confirmations/{id}` 與 `core/internal/confirm`。在套用前就存在的連線上確認會被拒絕（T-188），換新連線才會接受；其他帳號拿到 `404`；重送確認回同樣結果（`TestConfirmationNeedsANewConnection`，以真實 HTTP 伺服器與 `ConnContext` 驗證）。過期、指定入口 Host 或本機位址、並行確認、回復都由 `confirm_test.go` 驗證。倒數、自動回復與重啟續算屬票 024。
- [x] （增補 03 51.6）設計時決定並寫進本票：同時符合多種錯誤時的錯誤代碼優先順序、`CONFIRMATION_REQUIRED` 的資源範圍、預檢擋下時的錯誤代碼、非 UTF-8 檔名在 API 的原始位元組欄位。 — 證據：見下方「51.6 決定」，以及 `errcodes.go` 的 `blockingOrder`、`confirm.PendingFor`、`rawname.go`。

## 51.6 決定

- **錯誤代碼優先順序**：未登入 → CSRF 或來源不符 → 不支援的 `dryRun` → 不存在或看不到（`404`）→ 無權（`403`）→ 輸入錯誤 → 內容太大 → 缺版本條件 → requestId 重送判定 → 平台降級 → 節點或引擎降級 → 控制者衝突 → 資源唯讀 → 等待確認 → 版本不符 → 被引用 → 預檢沒過。回最前面的一個，其餘列在 `alsoBlockedBy`。前五項不透露資源存在與否。不支援的 `dryRun` 只看請求本身就能判斷，所以排在查資源之前，確保什麼都不會執行。已接受的 requestId 在平台降級時照樣回原工作。資源狀態擋下時，版本對了也不能寫，所以排在版本之前。被引用與預檢要看目前內容，所以排最後。
- **`CONFIRMATION_REQUIRED` 的資源範圍**：待確認變更鎖住的資源，比對方式與工作鎖相同（規格 33.2：同裝置、同掛載、父子路徑、同節點上的同種類與 ID）。只擋寫入，讀取、確認本身與回復不受影響。錯誤內容只寫出等待結束的時間，不含確認 ID，因為確認 ID 只有套用者看得到。
- **預檢擋下時的錯誤代碼**：`PRECHECK_FAILED`。同步寫入在建立工作前就預檢沒過時回 HTTP `409`，`details.checks` 寫出沒通過的檢查。工作內的預檢沒過時，它是工作的 `errorCode`，兩者用同一個名稱。
- **非 UTF-8 檔名的原始位元組欄位**：名稱一律以 `RawName` 傳遞，`{"text": "...", "bytes": "..."}`。`text` 一定是有效 UTF-8，無效位元組換成 U+FFFD。原始內容不是有效 UTF-8 時另附 `bytes`，放原始位元組的標準 base64。兩個顯示相同的名稱以 `bytes` 區分，改名或下載時把整個 `RawName` 送回來，有 `bytes` 就以它為準。
- **背景查詢的結果上限、呈現方式與保存期限**（與票 017 共同擁有）：每筆查詢最多保存 100,000 筆結果，存在 `list_query_items`，不放進工作紀錄。以 `GET /management/v1/list-queries/{id}/items` 用同一套列表格式分頁讀取。保存 7 天，由每小時一次的清理刪除結果、保留查詢紀錄，之後讀取回 `410 LIST_QUERY_EXPIRED`。每個帳號最多同時保存 10 筆查詢。只有提出者、而且可見範圍與執行時相同時才讀得到。
- **版本不符的狀態碼**：缺少或帶 `*` 回 `428 VERSION_REQUIRED`，過期回 `412 VERSION_MISMATCH`（RFC 9110 13.1.1）。`409` 留給資源狀態擋下的情況。
- **確認的新連線判定**：確認請求所在的 TCP 連線，建立時間必須晚於變更套用時間（`api.ConnContext` 記錄）。只看「連線 ID 不同」不夠，因為瀏覽器會同時開好幾條連線。Core 前面若有反向代理，Core 看不到瀏覽器那段連線，這個判定就不成立，目前的前提是管理入口由瀏覽器直連 Core。

## 設計缺口

這張票的 API 已經回得出下列狀態，但 `docs/design/` 還沒有對應的畫面，由設計端補畫：

- 列表底部的每頁筆數、查詢上限與逾時，以及「可瀏覽前 N 筆／總數 M」和「總數未知」兩種狀態。設計裡目前只有一顆「改成背景工作」按鈕。
- 背景查詢的結果頁，以及結果過了 7 天被刪除（`410 LIST_QUERY_EXPIRED`）、帳號已保存 10 筆查詢（`409 LIST_QUERY_LIMIT_REACHED`）時的畫面。
- 錯誤物件在畫面上的呈現：`fields` 對到表單欄位、`suggestedActions` 的按鈕、`alsoBlockedBy` 是否列出，以及是否把 `traceId` 給使用者回報問題用。
- 帳號編輯（顯示名稱）時版本過期（`412 VERSION_MISMATCH`）的畫面。目前只有 compose 檔的版本衝突卡片。
- `dryRun` 影響預覽面板：`blockers`、直接與間接影響、「會排在某個工作後面」。DESIGN.md 提到影響預覽，但沒有畫板。
- ROB-010 的套用確認倒數：要求「從新連線或新網域開啟再按確認」的提示，以及從舊連線送出（`CONFIRMATION_NEW_CONNECTION_REQUIRED`）、已過期（`CONFIRMATION_EXPIRED`）兩種被拒狀態。
- 其他人改同一資源時被 `CONFIRMATION_REQUIRED` 擋下的畫面，要寫出等待結束的時間。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
