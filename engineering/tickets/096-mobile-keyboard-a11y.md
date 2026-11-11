# 096 — 使用者可以在 360px 手機與只用鍵盤的情況完成日常操作

**Epic:** 品質與驗收（`epic:quality`）  
**User Story:** 驗收者可以證明功能沒有遺漏  
**Milestone:** M5 多節點、叢集、遷移與發布  
**Blocked by:** 017 使用者可以在主導覽、全域搜尋與工作中心之間完成日常操作  
**規格章節:** 07｜介面與操作體驗  
**Issue:** [#95](https://github.com/HazelnutParadise/hazelminx/issues/95)  

## What it delivers

響應式與鍵盤可達性驗收、焦點與危險按鈕防誤觸、WCAG 2.1 AA 審查。

## Acceptance criteria

- [ ] **UI-005** 使用者可以在手機、桌面與鍵盤操作完成日常管理。 — 完成條件：360px 寬度不隱藏主要操作；鍵盤可到達控制項；狀態不只靠顏色。
- [ ] （後續事項 2026-11-11）`web/` 的 e2e 與版面、無障礙掃描在 Safari 和 Firefox 也跑過；目前只在 Chromium 跑過。

## Acceptance tests（第 42 章）

- **T-117** 在 360px 手機與桌面只用鍵盤完成日常操作。 → 主要功能可達、焦點清楚、危險按鈕不誤觸。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。

## Notes

### 設計審查留下的項目

- 原型沒有做鍵盤走查與螢幕閱讀器朗讀；有可執行前端後，用 axe 與鍵盤實測 T-117。
