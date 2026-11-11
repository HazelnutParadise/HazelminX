# 099 — 維護者可以依 38 章情境量測效能並附測試條件

**Epic:** 品質與驗收（`epic:quality`）  
**User Story:** 驗收者可以證明功能沒有遺漏  
**Milestone:** M5 多節點、叢集、遷移與發布  
**Blocked by:** 084 管理員可以在單一畫面查看與批次操作跨節點資源, 066 使用者可以依時間、節點、網域與入口查看流量、來源、路徑、裝置與 API 用量  
**規格章節:** 38｜效能、資源與可觀測品質要求  
**Issue:** [#98](https://github.com/HazelnutParadise/hazelminx/issues/98)  

## What it delivers

小型／擴充控制中心、列表 p95、工作提交、UI 首次可操作、Agent 空閒、失聯顯示、日誌查詢、Core 重啟不影響網站。

## Acceptance criteria

- [ ] 見上方交付內容；驗證證據附在 PR。

## Acceptance tests（第 42 章）

- **T-119** 資源與日誌數量達壓測規模。 → 分頁、取消、搜尋、事件串流仍符合目標。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。

## Notes

- （後續事項 2026-11-11）訂效能預算時的基準：`web/` 的主 bundle 在 2026-11-08 壓縮後約 255 kB，大部分是 Kumo 與 React。
