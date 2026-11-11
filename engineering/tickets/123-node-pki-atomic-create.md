# 123 — 節點憑證第一次建立到一半失敗時，Core 下次啟動可以自己恢復

**Epic:** 節點與 Agent（`epic:nodes`）
**User Story:** 管理員不會因為第一次啟動時的一次寫入失敗而卡在無法啟動
**Milestone:** M1 單機主機管理
**Blocked by:** —
**規格章節:** 無（來自 2026-11-11 之前記在 AGENTS.md 的後續事項）
**Issue:** [#141](https://github.com/HazelnutParadise/hazelminx/issues/141)

## What it delivers

`nodes.LoadOrCreatePKI` 第一次建立時依序寫入 `node-pki/ca`、`node-pki/core` 兩筆機密，第二筆失敗會留下半套，之後啟動一律拒絕。改成同一個交易建立兩筆，或提供清除半套的復原指令。

## Acceptance criteria

- [ ] 第二筆機密寫入失敗時，兩筆都不留下，下次啟動會重新建立。
- [ ] 既有安裝裡已經存在的半套狀態有明確的錯誤訊息與復原方式。
- [ ] 有一條模擬第二筆寫入失敗的測試。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
