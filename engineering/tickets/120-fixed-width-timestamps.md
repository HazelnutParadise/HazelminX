# 120 — 同一秒內建立的工作與稽核紀錄在列表裡依實際時間順序排列

**Epic:** 基礎建設（`epic:foundation`）
**User Story:** 使用者照時間排序時看到的順序是對的
**Milestone:** M1 單機主機管理
**Blocked by:** —
**規格章節:** 無（來自 2026-11-11 之前記在 AGENTS.md 的後續事項）
**Issue:** [#138](https://github.com/HazelnutParadise/hazelminx/issues/138)

## What it delivers

`jobs` 與 `iam` 的時間欄位用 `time.RFC3339Nano` 寫入，小數尾端的 0 會被去掉，同一秒內的文字排序不一定等於時間順序（`…05.12Z` 會排在 `…05.123Z` 後面）。改成固定 9 位小數，並為既有資料做一次轉換。`confirm` 與 `list_queries` 已經用固定格式。keyset 翻頁本身不受影響。

## Acceptance criteria

- [ ] `jobs` 與 `iam` 寫入的時間一律是固定 9 位小數。
- [ ] 一個新的 migration 把既有資料轉成固定格式，轉換前後的時間值相同。
- [ ] 同一秒內建立、小數位數不同的多筆紀錄，照 `createdAt` 與 `at` 排序的結果等於實際時間順序，有測試。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
