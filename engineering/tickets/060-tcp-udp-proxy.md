# 060 — 使用者可以設定 TCP／UDP 代理與連接埠轉送

**Epic:** 網站入口（`epic:web`）  
**User Story:** 使用者可以對外發布網站  
**Milestone:** M3 網站入口與安全防護  
**Blocked by:** 055 管理員可以從介面安裝原生 Zentinel，Core 重啟不影響既有網站，救援入口不依賴它  
**規格章節:** 19｜網站、Zentinel、網域與 HTTPS  
**Issue:** [#60](https://github.com/HazelnutParadise/hazelminx/issues/60)  

## What it delivers

非 HTTP 協定的來源限制、上游與健康檢查；Zentinel 未覆蓋時提供經驗證的相容元件。

## Acceptance criteria

- [ ] **WEB-013** 使用者可以保留基線 TCP／UDP 代理與連接埠轉送。 — 完成條件：分開管理非 HTTP 協定、來源限制、上游與健康檢查；Zentinel 選定版未覆蓋時提供經驗證相容元件，不能拿 HTTP Token 代理冒充。[S38](#source-S38)

## Acceptance tests（第 42 章）

- **T-127** 原 TCP／UDP 代理進行遷移。 → 用真正對應協定測通，沒有被錯誤轉成 HTTP。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
