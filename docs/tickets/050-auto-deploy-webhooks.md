# 050 — 管理員可以設定 Webhook 或定期檢查觸發部署，並決定多環境與連續 commit 政策

**Epic:** GitHub 來源（`epic:github`）  
**User Story:** 使用者可以從 GitHub 部署  
**Milestone:** M2 容器、Compose 與 GitHub  
**Blocked by:** [049 專案管理員可以從 GitHub 以固定 commit 隔離抓取、建置並部署，手動 URL／PAT／SSH 模式照常可用](049-git-deploy.md)  
**Status:** blocked  
**規格章節:** 15｜完整 Compose、GitHub 與環境變數、33｜工作引擎：按一次按鈕後，如何可靠完成  
**Issue:** [#50](https://github.com/HazelnutParadise/hazelminx/issues/50)  

## What it delivers

獨立 Webhook 入口（簽章、大小、頻率）、事件驗證與持久去重、依 Repo ID／環境分派、只建立部署工作不同步建置、同環境單一部署、合併未開始工作、分支刪除／強制推送／外部 PR 政策、唯讀 PAT 的定期檢查。

## Acceptance criteria

- [ ] **CMP-009** 管理員可以設定 GitHub push 自動部署或定期檢查。 — 完成條件：驗證 webhook 簽章、repo、分支與事件類型；重複 delivery 不重複部署。[S19](#source-S19)
- [ ] **CMP-010** 管理員可以選擇遇到連續 commit 的處理方式。 — 完成條件：預設一個環境同時一個部署；尚未開始者可合併到最新，執行中的資料變更不強行取消。
- [ ] **GIT-018** 管理員可以依權限選 Webhook 或定期檢查新 commit。 — 完成條件：唯讀 PAT 無 Webhook 權限仍可手動／定期部署；不重複建立同一套共用 Hook。
- [ ] **GIT-019** 系統可以驗證 GitHub 事件並建立部署工作。 — 完成條件：簽章、App／安裝、Repo ID、分支、事件、授權與送達去重均檢查；不從回呼同步執行建置。
- [ ] **GIT-020** 管理員可以管理同 Repo 的多環境觸發政策。 — 完成條件：同環境不重複並行部署；分支刪除不刪應用；強制推送與外部 PR 不繞過批准。
- [ ] 新 commit 新增特權或公開資料庫時自動部署暫停等待批准，舊版繼續

## Acceptance tests（第 42 章）

- **T-059** Git 新 commit 新增特權與公開資料庫。 → 自動部署暫停等待批准，舊版本繼續。
- **T-060** Webhook 重複、過期、錯簽章與亂序抵達。 → 無效拒絕，有效去重，不自動回到較舊版本。
- **GH-T17** 同一 App 事件送到多個 Repo／環境綁定，再重送相同 delivery。 → 依 Repo ID 與環境分派，只觸發批准範圍；重送不重複建置。
- **GH-T18** 送入錯簽章、陌生安裝、錯 Repo／分支、外部 PR 與已撤銷連線事件。 → 拒絕或安全忽略，不使用正式秘密；有效簽章不等於批准部署。
- **GH-T19** 刪除分支、強制推送，同時 polling 與 Webhook 發現同 commit。 → 不刪既有服務；依政策處理來源變化，同環境同設定只一個有效部署。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
