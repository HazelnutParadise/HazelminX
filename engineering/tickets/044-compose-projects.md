# 044 — 專案管理員可以建立專案與環境，匯入完整 Compose 並驗證後部署

**Epic:** Compose 部署（`epic:compose`）  
**User Story:** 專案管理員可以部署完整 Compose  
**Milestone:** M2 容器、Compose 與 GitHub  
**Blocked by:** 039 管理員可以看到 Docker、Podman、K3s 各自狀態並在頁面一鍵安裝 Docker, 008 使用者可以提交工作後關閉頁面，之後仍能查到進度、證據與結果, 016 管理員可以預覽並批准高風險變更計畫，內容改變時原批准失效  
**規格章節:** 15｜完整 Compose、GitHub 與環境變數  
**Issue:** [#44](https://github.com/HazelnutParadise/hazelminx/issues/44)

## What it delivers

這張票是索引，自己不交付內容，三張子票都在 `done/` 時一起關閉。拆分原因：資料模型、匯入與驗證只在 Core，審查重點是解析使用者上傳的 YAML 與權限；部署要以 root 經高權限執行器建置並啟動容器，審查重點是執行邊界、帳本與 change plan；畫面屬於另一個元件。三者一個 PR 做不完也驗不完。部署的前提是 039.1 的引擎偵測；039.2 的安裝還沒做，沒有可用引擎的主機一律拒絕並寫出原因，不替它安裝。

| 項目 | 子票 |
|---|---|
| 專案、專案環境與 Compose 單元的資料模型與 `/management/v1` API；以上傳或編輯器匯入完整 Compose（指定主檔、多個 `-f` 檔案依序合併、profiles、工作目錄、build context、外部資源宣告），原文無損保存；以 Docker Compose 自己用的解析器驗證，列出接受、拒絕與原因；不需要主機就能判斷的預檢（缺必要變數、相對路徑錯誤、高風險設定、目標引擎）；部署預覽（CMP-001 上傳與編輯器部分、CMP-015，T-052） | 044.1 |
| 需要主機的預檢（映像取得、磁碟、Port 衝突、bind mount 路徑）、change plan、工作、高權限執行器的 Compose 部署動作與帳本；建置來源與持久資料分開（CMP-007、CMP-008、CMP-012） | 044.2 |
| 專案列表、專案詳細、Compose 編輯器、相容性與部署預檢畫面；圖形化與原始編輯切換（UI-008；畫板 Projects、ProjectDetail、ComposeEditor、ComposeCompat、DeployCheck） | 044.3 |

CMP-001 的 Git 來源由 049 交付、商店來源由 052 交付，兩者都使用 044.1 的匯入與驗證。

## Notes

### 票 019 交過來的項目

- NOD-008「節點進入維護時列出受影響服務」等本票有服務的資料模型後補上（使用者 2026-11-12 決定）。目前 `PUT /management/v1/nodes/{id}/maintenance` 的 `affectedJobs` 只列節點上未完成的工作，產生的地方是 `core/internal/nodereg/nodes.go` 的 `SetMaintenance`。服務要部署後才在節點上執行，所以列出受影響服務在 044.2 做；補上後回到票 019 勾 NOD-008，019 才能移到 `done/`。**已完成（044.2）：** 維護回應的 `affectedServices`，019 已移到 `done/`。
