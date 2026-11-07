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
- 所有危險操作都在後端驗權；React 的按鈕狀態不是安全邊界。
- 長操作一律建立工作（job），回 `202` 與工作 ID；不把長操作綁在一次 HTTP 連線。
- 機密只存參照與加密值；日誌、錯誤訊息、URL、Git remote、映像層、診斷包都不得出現明文機密。
- 時間以 UTC RFC 3339 儲存與傳遞；容量以位元組儲存；路徑保存原值與解析結果。
- 新管理 API 使用 `/management/v1/` 前綴；infra-manager 的 `/use/`、`/api/` 契約（`docs/research/infra-manager-contract.md`）不得改動回應碼與優先順序。
- 1Panel v2.4.1 OSS 是功能基線；商業版（xpack／Pro）程式碼不得重用，也不得以移除付費檢查冒充取得授權。
- 衍生自 1Panel（GPL-3.0）與 Komodo（GPL-3.0-or-later）的程式保留原作者授權與告知；`GPL-3.0-only` 或 `or-later` 的最終宣告待核定，未核定前不改授權欄位。
- Windows Server 節點（WIN 系列、`scope:pending-approval`）是建議範圍；未經核定與實測不得在 UI 或文件顯示為正式支援。
- Docker Compose 檔案以 `${VAR:?必須設定 VAR}` 宣告必要變數，不用 `env_file` 屬性；交付前執行 `docker compose config --quiet`。
- Commit 訊息採 Conventional Commits，英文主旨；以 `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>` 結尾。

## Follow-ups

- 第一次正式發版前，把 `core/internal/store/migrations/` 的所有 migration 合併成單一個 `0001` 檔，`store` 的連號檢查與 schema 版本相關測試一起更新。已經有使用者安裝的版本發出去之後，就不能再這樣合併。
