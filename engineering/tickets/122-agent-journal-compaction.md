# 122 — Agent 長期運作時，工作日誌的檔案與記憶體用量不會無限增加

**Epic:** 節點與 Agent（`epic:nodes`）
**User Story:** 節點可以連續運作幾個月而不用人工清理
**Milestone:** M1 單機主機管理
**Blocked by:** —
**規格章節:** 無（來自 2026-11-11 之前記在 AGENTS.md 的後續事項）
**Issue:** [#140](https://github.com/HazelnutParadise/hazelminx/issues/140)

## What it delivers

Agent 的 `journal.jsonl` 只增不減，`Journal` 也把所有紀錄留在記憶體。加上壓縮：Core 確認過、而且工作已是終態的步驟紀錄移到封存檔，保留最近的完成紀錄供重送。

## Acceptance criteria

- [ ] Core 確認過且工作已是終態的步驟紀錄會移出 `journal.jsonl` 與記憶體，進封存檔。
- [ ] 壓縮後重新啟動，尚未確認的紀錄仍然會重送，最近完成的紀錄仍然可以回應重送要求。
- [ ] 壓縮中途斷電不會遺失尚未確認的紀錄，有測試。
- [ ] 封存檔有大小或保留期限上限。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
