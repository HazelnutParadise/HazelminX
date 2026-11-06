# 067 — 管理員可以連接 Cloudflare Analytics 並分開看邊緣與來源站

**Epic:** 流量分析（`epic:analytics`）  
**User Story:** 使用者可以理解流量來源  
**Milestone:** M3 網站入口與安全防護  
**Blocked by:** [066 使用者可以依時間、節點、網域與入口查看流量、來源、路徑、裝置與 API 用量](066-traffic-analytics.md)  
**Status:** blocked  
**規格章節:** 23｜流量分析、網站來源與 API 用量  
**Issue:** [#66](https://github.com/HazelnutParadise/hazelminx/issues/66)  

## What it delivers

GraphQL 資料集、方案限制、取樣、時間範圍、最後同步；三種視角不直接相加。

## Acceptance criteria

- [ ] **ANA-007** 管理員可以連接 Cloudflare Analytics。 — 完成條件：顯示資料集、方案限制、取樣、時間範圍與最後同步；來源站與邊緣數據不直接相加。[S26](#source-S26)

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
