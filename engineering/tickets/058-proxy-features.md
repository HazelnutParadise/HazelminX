# 058 — 使用者可以設定重導、重寫、標頭、快取、壓縮、上傳限制、多後端與串流代理

**Epic:** 網站入口（`epic:web`）  
**User Story:** 使用者可以對外發布網站  
**Milestone:** M3 網站入口與安全防護  
**Blocked by:** 056 使用者可以建立靜態與反向代理網站、綁定多網域並穩定路由到容器服務  
**規格章節:** 19｜網站、Zentinel、網域與 HTTPS  
**Issue:** [#58](https://github.com/HazelnutParadise/hazelminx/issues/58)  

## What it delivers

各功能對應測試；不支援的原 Nginx 指令列為轉換缺口；WebSocket／SSE／gRPC／大檔的逾時、Range、中斷測試；健康檢查與負載分配。

## Acceptance criteria

- [ ] **WEB-006** 使用者可以設定重新導向、重寫、標頭、快取、壓縮與上傳限制。 — 完成條件：各功能有對應測試；不支援的原 Nginx 指令被列為轉換缺口，不能默默省略。
- [ ] **WEB-007** 使用者可以代理 WebSocket、SSE、gRPC 與大型檔案。 — 完成條件：以選定版本測試逾時、流量控制、Range、錯誤中斷與串流延遲；未通過不標正式支援。
- [ ] **WEB-008** 使用者可以設定多個後端、健康檢查與負載分配。 — 完成條件：失效後端移出流量，恢復後受控加入；不把健康檢查誤算成真人訪問。

## Acceptance tests（第 42 章）

- **T-085** 代理串流、大檔及客戶端中途斷線。 → 不無限緩衝；上游取消與錯誤來源可追蹤。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
