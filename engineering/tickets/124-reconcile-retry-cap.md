# 124 — 一個步驟在對帳時反覆重跑超過上限後會停下來，轉成需要處理

**Epic:** 基礎建設（`epic:foundation`）
**User Story:** 磁碟快滿的節點不會讓同一個步驟無止盡重跑
**Milestone:** M1 單機主機管理
**Blocked by:** —
**規格章節:** 無（來自 2026-11-11 之前記在 AGENTS.md 的後續事項）
**Issue:** [#142](https://github.com/HazelnutParadise/hazelminx/issues/142)

## What it delivers

`jobs.Engine.Reconcile` 對「開始過、沒結束、可重試」的步驟每次都重跑，沒有次數上限。Agent 磁碟只剩一點空間時，開始紀錄寫得進去、結束紀錄寫不進去，同一步驟會一直重跑。在引擎加對帳重跑次數上限，超過就轉「需要處理」。

## Acceptance criteria

- [ ] 同一步驟的對帳重跑次數有上限，超過後工作轉「需要處理」，並說明原因。
- [ ] 重跑次數跨 Core 重新啟動仍然保留。
- [ ] 有一條模擬結束紀錄一直寫不進去的測試。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
