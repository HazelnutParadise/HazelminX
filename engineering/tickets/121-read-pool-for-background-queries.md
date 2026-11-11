# 121 — 大型稽核表上的背景列表查詢執行時，其他 API 不會跟著變慢

**Epic:** 基礎建設（`epic:foundation`）
**User Story:** 一個慢查詢不會拖住整個管理介面
**Milestone:** M1 單機主機管理
**Blocked by:** —
**規格章節:** 無（來自 2026-11-11 之前記在 AGENTS.md 的後續事項）
**Issue:** [#139](https://github.com/HazelnutParadise/hazelminx/issues/139)

## What it delivers

`store` 設 `SetMaxOpenConns(1)`，背景列表查詢執行期間會輪流佔用唯一的 SQLite 連線，沒有索引的篩選會拖慢其他 API。加一個讀取用的第二個連線池，背景查詢改用它。

## Acceptance criteria

- [ ] 背景列表查詢走讀取用的連線池，不佔用寫入連線。
- [ ] 一個長時間的背景查詢執行期間，其他讀寫 API 的回應時間不受影響，有測試。
- [ ] 寫入仍然只有一個連線，既有的交易行為不變。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
