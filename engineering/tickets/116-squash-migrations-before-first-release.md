# 116 — 維護者在第一次正式發版前把所有 migration 合併成單一個 0001，全新安裝直接建出最終結構

**Epic:** 發布與供應鏈（`epic:release`）
**User Story:** 第一次發版的安裝不必重跑開發期間的來回修改
**Milestone:** M5 多節點、叢集、遷移與發布
**Blocked by:** —
**規格章節:** 無（來自 2026-11-11 之前記在 AGENTS.md 的後續事項）
**Issue:** [#134](https://github.com/HazelnutParadise/hazelminx/issues/134)

## What it delivers

把 `core/internal/store/migrations/` 的所有 migration 合併成單一個 `0001` 檔，直接建出最終形狀，拿掉重工（例如 0005 把 0003 的 `request_id` 改成 `trace_id`，合併後直接建 `trace_id`）。`store` 的連號檢查與 schema 版本相關測試一起更新。這是 migration 不可變原則在首次發版前的唯一例外：已經有使用者安裝的版本發出去之後不能再合併，之後只能新增 migration。

## Acceptance criteria

- [ ] 用舊的整串 migration 與新的 `0001` 各建一個全新資料庫，兩邊的 schema（表、欄位、索引、觸發器）完全相同。
- [ ] `store` 的連號檢查與 schema 版本測試在只有 `0001` 時通過。
- [ ] 發版前開發與測試用的資料庫一律重建，不提供從舊 migration 串升級的路徑，並在發版說明寫明。
- [ ] 這張票在第一次正式發版的版本凍結之前完成；發版之後不再執行。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
