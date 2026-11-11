# 063 — 管理員可以匯入既有 infra-manager 的服務、Token 與使用者

**Epic:** API 代理（`epic:api-proxy`）  
**User Story:** 管理員可以把內部 API 交給特定人使用  
**Milestone:** M3 網站入口與安全防護  
**Blocked by:** 062 管理員可以發布 infra-manager 相容的 Token API 代理，既有客戶端不需修改  
**規格章節:** 20｜infra-manager 式服務代理與存取管理、44｜遷移、授權與長期維護  
**Issue:** [#62](https://github.com/HazelnutParadise/hazelminx/issues/62)  

## What it delivers

不變更有效 Token；以摘要保存可驗證舊 Token；保留舊優先順序所需辨識資訊；匯入報告。

## Acceptance criteria

- [ ] **API-008** 管理員可以匯入既有服務、Token 與使用者資料。 — 完成條件：不變更有效 Token；使用摘要保存可驗證舊 Token，保留舊優先順序所需的辨識資訊。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
