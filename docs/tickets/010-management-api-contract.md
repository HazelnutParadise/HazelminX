# 010 — 整合者可以用 /management/v1 的分頁、版本條件與工作回應操作資源

**Epic:** API、CLI 與整合（`epic:integration`）  
**User Story:** 整合者可以用同一套規則操作平台  
**Milestone:** M0 基礎與設計  
**Blocked by:** [003 管理員可以啟動 hazelminx-core 並通過健康檢查](003-core-skeleton.md), [008 使用者可以提交工作後關閉頁面，之後仍能查到進度、證據與結果](008-job-engine.md)  
**Status:** blocked  
**規格章節:** 36｜管理 API 與事件契約  
**Issue:** [#10](https://github.com/HazelnutParadise/hazelminx/issues/10)  

## What it delivers

36 章契約：分頁上限、篩選排序、If-Match 版本條件、202 工作 ID、錯誤格式（code／message／requestId／retryable／evidence／suggestedActions）、OpenAPI JSON 由路由產生、/management/v1 總覽端點。

## Acceptance criteria

- [ ] **INT-001** 使用者可以以版本化 API 查詢與操作授權資源。 — 完成條件：文件與伺服器一致；長工作回工作 ID；錯誤有穩定代碼與白話說明。
- [ ] 新管理 API 與 infra-manager 舊 /api/ 路徑不衝突
- [ ] 錯誤代碼穩定且文件與伺服器一致（契約測試）

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
