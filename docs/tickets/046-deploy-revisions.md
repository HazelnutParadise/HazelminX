# 046 — 管理員可以查看部署版本、三方比較並回到相容舊版

**Epic:** Compose 部署（`epic:compose`）  
**User Story:** 專案管理員可以部署完整 Compose  
**Milestone:** M2 容器、Compose 與 GitHub  
**Blocked by:** [045 管理員可以分層管理 .env、介面變數與覆寫層，並預覽每個服務實際收到的值](045-env-layers.md)  
**Status:** blocked  
**規格章節:** 15｜完整 Compose、GitHub 與環境變數、34｜資料模型、設定版本與資料格式  
**Issue:** [#46](https://github.com/HazelnutParadise/hazelminx/issues/46)  

## What it delivers

34.2 的四份記錄、版本包含 commit／Compose／覆寫／變數版本／digest、三方比較與衝突待處理、只更新受影響服務、secrets／configs／建置秘密生命週期、同 repo 多環境隔離、個別服務調整回寫。

## Acceptance criteria

- [ ] **CMP-011** 管理員可以檢視部署記錄並回到相容舊版。 — 完成條件：版本包含 commit、Compose、覆寫、變數版本與 image digest；不只記一個 tag。
- [ ] **CMP-014** 管理員可以管理 Compose 建立的個別服務。 — 完成條件：調整會回寫正確的受管理設定或標記外部差異，不能改容器後下次部署又消失。
- [ ] **CMP-016** 管理員可以選擇只更新受影響服務。 — 完成條件：不重建無關資料庫；相依服務需要變動時列入計畫，不無條件使用 `--no-deps`。
- [ ] **CMP-017** 管理員可以使用 Compose secrets／configs 與建置秘密。 — 完成條件：建置密鑰不放一般 build args 或映像層；來源、掛載與刪除有獨立生命週期。
- [ ] **CMP-018** 管理員可以將單一 repo 部署至不同環境與節點。 — 完成條件：正式與測試環境的機密、網域、資料與資源名稱相互隔離。
- [ ] 上游與使用者同時改同一掛載目標時顯示三方衝突，不任意選一邊

## Acceptance tests（第 42 章）

- **T-057** 商店升級應用，已自訂 network、port 與資料碟。 → 除批准版本差異外完全保留，沒有預設覆寫。
- **T-058** 上游與使用者同時改同一掛載目標。 → 顯示三方衝突，不任意選一邊。
- **T-062** Git 更新後舊工作仍使用相對掛載檔案。 → 舊版本需要的來源目錄不提前清理。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
