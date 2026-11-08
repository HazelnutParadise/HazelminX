# HazelminX 工作規則

## Required artifacts

- `docs/spec/HazelminX_Spec_v1.1_zh-TW.md` — 產品規格，449 條需求與 186 個驗收案例的唯一來源。實作或驗收任何功能前，先讀該票列出的章節；不得憑記憶改寫需求。`docs/spec/HazelminX_Requirements_v1.1.json` 是同一份內容的機器可讀版。
- `ENG.md` — 跨票的架構、元件邊界、測試接縫、資料遷移順序與前提假設。改架構、挑測試接縫、寫 migration 或新增元件前先讀。
- `delivery-status.md` — 目前階段、阻礙、下一個可驗證產出與下一張票。進專案第一件事先讀它。
- `docs/tickets/` — 每張票一個檔案，含 Epic、User Story、Blocked by、需求 ID 與驗收條件；`README.md` 是索引，`traceability.json` 是需求→票→測試的機器可讀對照。只領取狀態為 `ready` 的票。
- `docs/research/` — 參考專案（1Panel、Komodo Periphery、Portainer、infra-manager、Zentinel、Beszel）的原始碼研究報告。重用或模仿它們的做法前先讀對應報告；報告是線索，不是規格。
- `docs/baseline/` — 1Panel v2.4.1 OSS 功能基線對照（票 092 產出）。宣稱「功能已保留」前，在這裡登記對應證據。
- `DESIGN.md` — 視覺與互動基準。改任何畫面前先讀；畫面變更須以渲染結果比對它。
- `DESIGN-REVIEW.md` — 設計審查紀錄與未解決項目。重新審查 UI 前先讀。

## Handoff expectations

- 每張票在 `ticket/<id>-<slug>` 分支上做，對應 GitHub issue 與草稿 PR。PR 描述依規格第 46.4 節列：需求 ID、變更內容、影響範圍、測試環境、正常與失敗案例、資料與安全影響、實際證據、未驗證部分。
- 票的驗收條件逐條勾選，附證據；沒有證據的條目保持未勾選。測試通過、尚未測試、受阻、已批准移除分開寫。
- 交接前更新 `delivery-status.md`：階段、阻礙、下一個可驗證產出、下一張票、決策差異。
- 票的狀態只有四種：`ready`、`blocked`、`in_progress`、`done`。改狀態同時更新 `docs/tickets/README.md` 與 `traceability.json`。

## Planning discipline

- 先寫骨架再填實作：入口、資料形狀、每個格子的簽名與想要的結果、兩到三條端到端驗收流程。
- 大改動（跨 UI／API／資料層、改公開契約、核心新功能、高風險路徑）先寫失敗的測試再實作。
- 不縮小已確認範圍。分批交付只改變順序，不改變最終需求；要移除或替代基線功能，必須有批准紀錄（PAR-003）。
- 規格第 46.2 節列出的事項（改既定語言或基底、移除功能、放棄相容、引入付費或外部帳號依賴、對正式環境發布、刪資料、改安全權限）必須先回來確認。

## Update rules

- `ENG.md` 就地更新，不加日期副本、不另開分支版本。
- 票檔是驗收條件的唯一來源；GitHub issue 內容從票檔同步，不在 issue 另寫一套條件。
- 新增或拆分票時，更新 `traceability.json`，確保每條需求 ID 仍只有一張擁有票。
- 規則條文只寫規則，不寫理由或變更紀錄。

## Project-specific constraints

- 元件語言與職責固定：React＋TypeScript＋Vite+ 前端（`web/`）、Go Core（`core/`）、Rust Agent／CLI／高權限執行器（`agent/`）、原生 systemd Zentinel 作網站入口。Go 不直接以 root 執行任意宿主機命令；宿主機變更一律經 Rust 執行邊界。
- 高權限執行器只接受固定動作與結構化參數，不接受拼接的 shell 字串。
- 宿主機的每個變更都要寫進主機變更帳本（增補 07 第 55.1 節，票 113）：記錄變更前後狀態與擁有權（平台安裝／平台修改／既有）。沒寫帳本的變更，執行器拒絕；新增的高權限動作要一併定義它的帳本內容與移除方式。
- 所有危險操作都在後端驗權；React 的按鈕狀態不是安全邊界。
- 長操作一律建立工作（job），回 `202` 與工作 ID；不把長操作綁在一次 HTTP 連線。
- 機密只存參照與加密值；日誌、錯誤訊息、URL、Git remote、映像層、診斷包都不得出現明文機密。
- 時間以 UTC RFC 3339 儲存與傳遞；容量以位元組儲存；路徑保存原值與解析結果。
- 新管理 API 使用 `/management/v1/` 前綴；infra-manager 的 `/use/`、`/api/` 契約（`docs/research/infra-manager-contract.md`）不得改動回應碼與優先順序。
- 1Panel v2.4.1 OSS 是功能基線；商業版（xpack／Pro）程式碼不得重用，也不得以移除付費檢查冒充取得授權。
- 衍生自 1Panel（GPL-3.0）與 Komodo（GPL-3.0-or-later）的程式保留原作者授權與告知；`GPL-3.0-only` 或 `or-later` 的最終宣告待核定，未核定前不改授權欄位。
- Windows Server 節點（WIN 系列、`scope:pending-approval`）是建議範圍；未經核定與實測不得在 UI 或文件顯示為正式支援。
- Docker Compose 檔案以 `${VAR:?必須設定 VAR}` 宣告必要變數，不用 `env_file` 屬性；交付前執行 `docker compose config --quiet`。
- Commit 訊息採 Conventional Commits，英文主旨；以當次實際使用模型的 `Co-Authored-By` 行結尾。
- 設計與實作並行（2026-11-08 使用者決定）：介面不等全部設計完才實作。實作中發現設計缺少畫面、狀態或元件規格時，不自行設計，在回報與 PR 描述用「設計缺口」標題逐條列出（哪一頁、缺什麼）；設計負責人補畫後同步回 `docs/design/` 與 DESIGN.md。

## Follow-ups

- 第一次正式發版前，把 `core/internal/store/migrations/` 的所有 migration 合併成單一個 `0001` 檔，直接建出最終形狀，拿掉重工（例如先建欄位再改名、先加再刪的步驟，像 0005 把 0003 的 `request_id` 改成 `trace_id`，合併後直接建 `trace_id`）。`store` 的連號檢查與 schema 版本相關測試一起更新。驗證方式：用舊的整串 migration 與新的 `0001` 各建一個全新資料庫，比對 schema（表、欄位、索引、觸發器）必須完全相同。發版前開發與測試用的資料庫一律重建，不做升級。已經有使用者安裝的版本發出去之後，就不能再這樣合併，之後只能新增 migration。這條是 migration 不可變原則在首次發版前的唯一例外。
- 密碼登入（`iam.Service.Login`）先 `limiter.Check` 再雜湊密碼、最後才記失敗，同時送出大量猜測可以超過帳號冷卻的次數；比照票 014 的第二步，同一帳號的嘗試要一次處理一個。
- 有密碼的攻擊者可以持續送錯的第二驗證碼，讓帳號的第二步一直被冷卻擋住（`#2fa:` 計數按帳號、不分來源），與 IAM-012 衝突；使用者已決定票 014 先合併，另開 PR 修。候選改法：票 065 接上可信代理後改成「帳號＋來源」計數加較高的帳號總上限，或讓 Passkey 成功不受這個計數影響。
- 管理網域改變後，只剩舊網域 Passkey 的強制角色帳號，系統仍算它「已有第二驗證」，不會要求重新綁定；票 018 實作網域變更時，要依 `auth/passkeys/domain-impact` 的結果處理這些帳號。
- 沒有任何票負責「使用者自己改密碼」（規格第 05 章與各票都沒有），密碼外洩時已登入的使用者無法自救；建議補進票 015 或另開票。
- 票 014 的 Passkey 只用軟體驗證器（`core/internal/iam/softauth_test.go`）測過；票 005 有登入與綁定畫面後，用真實瀏覽器與實體驗證器（含同步型 Passkey）補測註冊、第二步登入與再次驗證。
- `secureRequest`（`core/internal/api/iam_routes.go`）無條件相信 `X-Forwarded-Proto`，Cookie 的 Secure 旗標與沒設 `-public-origin` 時的 Passkey origin 都依它判斷；票 065 設定可信代理時一併只信任代理送來的值。
- `jobs` 與 `iam` 的時間欄位用 `time.RFC3339Nano` 寫入，小數尾端的 0 會被去掉，所以同一秒內的文字排序不一定等於時間順序（`…05.12Z` 會排在 `…05.123Z` 後面）。列表照 `createdAt`、`at` 排序時可能有少量順序錯誤，keyset 翻頁本身不受影響。要改成固定 9 位小數，並為既有資料做一次轉換；`confirm` 與 `list_queries` 已經用固定格式。
- 背景列表查詢在執行期間會輪流佔用唯一的 SQLite 連線（`store` 設 `SetMaxOpenConns(1)`），大型稽核表上沒有索引的篩選會拖慢其他 API。加讀取用的第二個連線池時，背景查詢要改用它。
- IAM 的寫入端點（建帳號、改帳號、指派與撤銷角色）還沒經過 `Server.writeBlockers`，平台資料庫唯讀時會回 `INTERNAL` 而不是 `PLATFORM_DEGRADED`。票 017 接上降級偵測時一起處理。
- ROB-010 確認比對的 `Host` 是用戶端送來的標頭。瀏覽器無法偽造，但腳本或 API Token 可以從舊入口送新的 Host，確認一個其實連不到的網域變更。票 024 決定是否改比 TLS SNI，或限制網域變更只接受瀏覽器工作階段確認。
- 票 010 的「設計缺口」一節列出 API 已有、畫面還沒畫的狀態（列表邊界、背景查詢、錯誤呈現、版本過期、影響預覽、確認倒數），由設計端補進 `docs/design/`。
- 寫入閘門的 `RESOURCE_READ_ONLY`、`CONTROLLER_CONFLICT`、`SCOPE_DEGRADED`、`RESOURCE_REFERENCED` 目前只有單元測試；第一個有受管資源端點的票接上 `ResourceState` 時，補一條經 API 拒絕的測試。
- Issue #20 比票檔 `020-host-overview-monitoring.md` 多出幾條驗收：GPU 欄位對齊 1Panel、GPU 程序對回所屬容器、硬體依類別分組成卡片、ROB-009，以及驗收案例 T-165、T-172、T-183。票檔是驗收條件的唯一來源，要決定把這些補進票檔，或從 issue 拿掉。
- `agent/UPSTREAM.md` 把幾件事交給其他票決定，但那些票的驗收還沒寫：票 027 要決定終端機用哪個 Linux 帳號執行、`host.shell` 權限在 Agent 還是 `hazelminx-privileged` 檢查，以及收到 SIGTERM 時結束終端機；票 040 要決定低權限 Agent 怎麼存取 Docker socket；命令層（Komodo `lib/command`）由票 020、040、044 中最先需要的那張移植。
- Agent 的 `journal.jsonl` 只增不減，`Journal` 也把所有紀錄留在記憶體；長期運作前要加壓縮：Core 確認過、而且工作已是終態的步驟紀錄可以移到封存檔，保留最近的完成紀錄供重送。
- `nodes.LoadOrCreatePKI` 第一次建立時依序寫入 `node-pki/ca`、`node-pki/core` 兩筆機密，第二筆失敗會留下半套，之後啟動一律拒絕；要改成同一個交易建立兩筆，或提供清除半套的復原指令。
- `jobs.Engine.Reconcile` 對「開始過、沒結束、可重試」的步驟每次都重跑，沒有次數上限；Agent 磁碟只剩一點空間時（開始紀錄寫得進去、結束紀錄寫不進去）會一直重跑同一步驟。要在引擎加對帳重跑次數上限，超過就轉「需要處理」。
