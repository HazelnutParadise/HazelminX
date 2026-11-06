# 082 — 管理員可以監控 HazelminX 自己並輸出標準監控指標

**Epic:** 監控與告警（`epic:monitoring`）  
**User Story:** 管理員可以掌握平台與服務健康  
**Milestone:** M4 資料庫、AI、備份與監控  
**Blocked by:** [020 維運者可以查看主機資訊、資源指標與歷史趨勢](020-host-overview-monitoring.md)  
**Status:** blocked  
**規格章節:** 29｜監控、GPU、事件與告警、31｜整體架構：每一部分負責什麼  
**Issue:** [#81](https://github.com/HazelnutParadise/hazelminx/issues/81)  

## What it delivers

Core、Agent、WAF、Zentinel、工作佇列、資料庫、日誌容量與同步延遲的健康訊號；受保護的 metrics 端點；控制／執行／代理／儲存各層級健康分開顯示。

## Acceptance criteria

- [ ] **MON-009** 管理員可以監控 HazelminX 自己。 — 完成條件：Core、Agent、WAF、Zentinel、工作佇列、資料庫、日誌容量與同步延遲都有健康訊號。
- [ ] **MON-010** 管理員可以輸出標準監控指標與追蹤資料。 — 完成條件：端點受保護；不把 URL、Token 或任意使用者輸入當無限制指標標籤。
- [ ] **ARC-008** 管理員可以看到控制、執行、代理與儲存各自的健康狀態。 — 完成條件：不能以 Core 健康代替 Agent、WAF 或備份目的地健康；故障顯示正確層級。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
