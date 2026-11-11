# 073 — 管理員可以執行資料庫備份、還原與大版本升級

**Epic:** 資料庫（`epic:database`）  
**User Story:** 使用者可以在平台內管理資料庫  
**Milestone:** M4 資料庫、AI、備份與監控  
**Blocked by:** 070 使用者可以瀏覽與編輯 PostgreSQL 與 MySQL 的結構與資料並執行 SQL, 016 管理員可以預覽並批准高風險變更計畫，內容改變時原批准失效  
**規格章節:** 24｜資料庫管理中心、18｜不中斷部署、版本回復與資料變更  
**Issue:** [#72](https://github.com/HazelnutParadise/hazelminx/issues/72)  

## What it delivers

引擎適用一致性方法、隔離還原目標、pg_upgrade 檢查（extension、locale、工具、資料路徑）、角色與全域物件備份、schema migration 與引擎升級分開、固定執行一次的 migration 工作。

## Acceptance criteria

- [ ] **DB-007** 管理員可以執行備份、還原與版本升級。 — 完成條件：使用資料庫適用的一致性方法；還原預設建立隔離目標，不直接覆寫正式資料。
- [ ] **PG-007** 管理員可以執行資料庫大版本升級。 — 完成條件：檢查 extension、locale、工具版本、資料路徑與官方升級方式；不以一般 migration 代替引擎升級。[S13](#source-S13)
- [ ] **PG-008** 管理員可以備份角色、權限與必要全域物件。 — 完成條件：不把單一資料庫 dump 當成完整叢集備份；可檢查還原後帳號與相依物件。[S27](#source-S27)
- [ ] **DEP-006** 管理員可以執行資料庫升級與 migration。 — 完成條件：固定執行一次的工作身分、鎖、備份與版本檢查；不把 schema migration 與資料庫引擎升級混為一談。
- [ ] 新版 migration 不相容時拒絕直接降回舊容器讀壞資料

## Acceptance tests（第 42 章）

- **T-076** 新版 migration 不相容，要求回舊容器。 → 檢查資料版本，不能直接降回去讀壞資料。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
