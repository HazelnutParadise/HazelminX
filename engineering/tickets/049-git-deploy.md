# 049 — 專案管理員可以從 GitHub 以固定 commit 隔離抓取、建置並部署，手動 URL／PAT／SSH 模式照常可用

**Epic:** GitHub 來源（`epic:github`）  
**User Story:** 使用者可以從 GitHub 部署  
**Milestone:** M2 容器、Compose 與 GitHub  
**Blocked by:** 048 使用者可以瀏覽、搜尋、分頁載入可用 Repo，並選分支、commit 與 Compose 檔案, 044 專案管理員可以建立專案與環境，匯入完整 Compose 並驗證後部署, 012 維運者可以執行需要高權限的固定動作，而 Agent 主程序維持低權限  
**規格章節:** 15｜完整 Compose、GitHub 與環境變數、31｜整體架構：每一部分負責什麼  
**Issue:** [#49](https://github.com/HazelnutParadise/hazelminx/issues/49)  

## What it delivers

這張票是索引，自己不交付內容，三張子票都在 `done/` 時一起關閉。拆分原因：

- 抓取來源與部署綁定只在 Core，審查重點是憑證與「讀到的、選的、部署的是同一份」。它接上 044.1 的驗證與 044.2 的部署，不另開一條部署路徑（決策 0036）。
- 在主機上建置要把整個 build context 送到節點、以 root 執行不可信的 Dockerfile，還有 submodule 與 LFS 的逐項驗權，審查重點是建置隔離與跨站抓取，和第一部分的風險不同，一個 PR 審不完。
- 平台 `.env` 與客製設定要等票 045 的變數分層。

| 項目 | 子票 |
|---|---|
| Compose 單元的來源可以是 Git：GitHub 連線（App 或 PAT 連線）或手動 URL（不帶憑證、單元自己的 PAT、SSH Deploy Key 加主機指紋）；Core 把 ref 固定成 commit、隔離抓取那一個 commit，把 compose 檔與它引用的檔案變成 044.1 驗證、044.2 部署的單元內容；部署紀錄寫下 commit；預覽與部署綁定同一份來源，連線或來源改變時重新檢查；submodule、LFS、符號連結與 `build:` 在預檢明確擋下（CMP-002、CMP-003、GIT-009、GIT-011、GIT-038，GH-T09） | 049.1 |
| 從 Git 在主機上建置：build context 送到節點、不可信建置的隔離、submodule 與 Git LFS 逐項驗權抓取（CMP-013、GIT-025、GIT-027，T-063、T-066、GH-T24、GH-T26） | 049.2 |
| 為 Git 來源的單元套用平台 `.env` 與客製設定，新 commit 不覆寫（GIT-010，GH-T10） | 049.3 |

畫面由 048.2（Repo 選擇器）與 044.3（匯入設定、相容性、部署預檢）接上，不另開子票。
