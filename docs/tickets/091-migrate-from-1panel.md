# 091 — 管理員可以從 1Panel 乾跑匯入並分批接管，原服務未批准前不停止

**Epic:** 遷移與接管（`epic:migration`）  
**User Story:** 管理員可以從 1Panel 遷移  
**Milestone:** M5 多節點、叢集、遷移與發布  
**Blocked by:** [053 管理員可以編輯、更新與卸載商店應用，自訂設定不被新版覆寫](053-appstore-update-uninstall.md), [056 使用者可以建立靜態與反向代理網站、綁定多網域並穩定路由到容器服務](056-websites-and-routing.md), [069 管理員可以安裝或接管資料庫服務，管理帳號、連線、健康與對外開放](069-database-services.md), [078 使用者可以備份應用、資料庫、目錄與平台設定，含一致性、多目的地、保留與加密](078-backup-jobs.md)  
**Status:** blocked  
**規格章節:** 44｜遷移、授權與長期維護  
**Issue:** [#90](https://github.com/HazelnutParadise/hazelminx/issues/90)  

## What it delivers

44.1 流程：固定來源版本、唯讀盤點、原 ID 對應、現場差異偵測、匯入報告五類、分資源群接管、停止原排程、失敗回到原工具；44.2.1 既有部署憑證遷移。

## Acceptance criteria

- [ ] **PAR-005** 管理員可以遷移既有設定，而非被迫重新建立所有應用。 — 完成條件：先產出乾跑報告，保留原始資料與映射關係，未批准不停止原服務。
- [ ] **PAR-006** 使用者可保有原資料庫、模型與網站資料。 — 完成條件：遷移測試核對資料、權限、路徑、連接埠與主要請求行為。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
