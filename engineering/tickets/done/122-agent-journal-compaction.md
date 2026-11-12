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

測試在 `agent/crates/agent/src/journal/compaction_tests.rs`，以 `cargo test --workspace`（`agent/`）執行。2026-11-12，macOS arm64，Rust 1.97.0。

- [x] Core 確認過且工作已是終態的步驟紀錄會移出 `journal.jsonl` 與記憶體，進封存檔。 — 證據：`compact_moves_only_acked_finished_jobs_past_retention_to_the_archive`（已確認、已結束、超過 7 天的工作移出記憶體與 `journal.jsonl`，封存檔裡每個欄位都在；最近完成的、中斷的、未確認的都留下）；只移整個工作：`a_job_with_one_unacked_record_is_kept_whole`、`a_job_with_an_unfinished_step_is_kept_whole`、`a_job_is_kept_while_its_newest_record_is_within_retention`；7 天的邊界：`retention_boundary_is_exactly_retain_age`；1 萬筆上限：`recent_finished_records_beyond_the_count_cap_move_oldest_job_first`；啟動時與 ack 時都會壓縮：`open_compacts`、`ack_compacts_when_the_interval_has_passed`。
- [x] 壓縮後重新啟動，尚未確認的紀錄仍然會重送，最近完成的紀錄仍然可以回應重送要求。 — 證據：`after_compaction_and_restart_unacked_records_are_resent_and_recent_ones_answer`（重開後 `unacked()`、`last_seq`、`acked`、最高 controlGeneration 和壓縮前相同，最近完成的步驟仍是 `Completed`）；seq 接著編：`appends_after_compaction_continue_the_seq_across_restarts`；被壓縮的步驟仍然回答得出結果，不會重跑：`compacted_steps_keep_their_outcome_in_memory_and_across_restart`、`a_reconcile_of_a_compacted_step_reports_its_recorded_outcome`、`a_repeated_execute_of_a_compacted_step_is_refused_as_outcome_unknown`、`a_repeated_execute_of_a_compacted_failed_step_resends_its_failure`、`a_compacted_step_refuses_a_new_start_even_after_restart`。
- [x] 壓縮中途斷電不會遺失尚未確認的紀錄，有測試。 — 證據：`a_crash_at_any_point_of_compaction_loses_no_unacked_or_kept_record` 逐一模擬 7 個中斷點：封存檔寫一半、封存檔 fsync 後、結果檔改名後、暫存日誌寫一半、暫存日誌 fsync 後未改名、改名後未 fsync 目錄、封存檔換代後。每個中斷點都檢查：重開後未確認紀錄、`last_seq`、`acked`、最高 controlGeneration 不變；被移出的步驟仍有結果；殘留暫存檔被刪掉；下一次壓縮完成後封存檔每筆只有一份；再重開一次仍相同。另有 `a_torn_archive_tail_is_cut_before_the_next_append`、`a_corrupt_archive_line_stops_compaction_and_leaves_the_journal_alone`、`a_failed_compaction_does_not_fail_the_ack`。測試用中途回傳錯誤模擬程序中斷，沒有模擬作業系統的頁快取遺失；寫入順序與每一步的 fsync 由兩輪獨立審查逐行確認。
- [x] 封存檔有大小或保留期限上限。 — 證據：`a_full_archive_is_rotated_and_the_older_generation_is_dropped`（滿 16 MiB 改名成 `journal.archive.1.jsonl`，再前一代刪掉）、`an_archive_under_the_cap_is_appended_to`；結果檔的上限：`outcomes_beyond_the_cap_drop_the_oldest_first`。上限值與理由見 Notes。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。

## Notes

**判斷工作已結束的方式**

Agent 看不到 Core 的工作狀態，協定沒有這個欄位，這張票也不改訊息契約。所以 Agent 端把「工作已是終態」判斷成：這個工作在日誌裡的每一筆紀錄都已被 Core 確認，而且每個步驟最後一筆都是 completed 或 failed。符合的工作整批移出。

**被壓縮的步驟為什麼還要留結果**

Core 收到事件就先回 ack，之後才存工作狀態（`core/internal/nodes/session.go` 處理 `event` 的分支）。Core 若在兩者之間當掉，重啟後會用 `step_reconcile` 問這一步。Agent 如果回「沒開始過」，`core/internal/jobs/engine.go` 的 `Reconcile` 會重跑這一步。所以每個被移出的步驟把最後一筆紀錄（不含 `result`）留在 `journal.outcomes.jsonl`，對帳照這份回答。細節見決策 0032 與 `engineering/contracts/README.md` 的步驟拒絕碼一節。

**上限值與理由**

| 項目 | 值 | 理由 |
|---|---|---|
| 已結束工作留在 `journal.jsonl` 的時間（`RETAIN_AGE`） | 最後一筆滿 7 天 | Core 停機或節點斷線一週內恢復時，重送與對帳都還拿得到含 `result` 的完整紀錄 |
| 已結束紀錄留在 `journal.jsonl` 的筆數（`RETAIN_MAX_RECORDS`） | 1 萬筆，從最新的工作算起 | 一筆約 300 到 500 位元組，記憶體約 5 MB 以內。步驟很多的節點，記憶體不會因為 7 天的保留時間跟著步驟數成長 |
| 被壓縮步驟的結果（`OUTCOMES_MAX_STEPS`） | 5 萬個步驟 | 每個約 120 位元組，約 6 MB。以每分鐘一個步驟計，大約可以涵蓋一個月 |
| 封存檔（`ARCHIVE_MAX_BYTES`） | 16 MiB 換一代，只留前一代 | 封存檔只做稽核用，Agent 不讀它回答 Core。磁碟上最多約 32 MiB 加一批 |
| 壓縮頻率 | 啟動時一次，之後 ack 時每小時最多一次 | 壓縮要改寫整個 `journal.jsonl`，每小時一次就足以讓檔案維持在上面的上限內 |

封存檔的 16 MiB 是在每批寫入前檢查的軟上限。從舊版升級後第一次壓縮一份累積很久的日誌，那一批可能超過 16 MiB，下一次換代會把它丟掉。

**審查留下、這次不修的問題**

- 被壓縮步驟的結果超過 5 萬個後，最舊的會刪掉，那些步驟會再被當成沒開始過。要出事必須同時發生兩件事：Core 在確認後、存檔前當掉，而且這個工作在之後至少 5 萬個步驟結束的期間都沒對帳成功（例如 Core 從舊備份還原）。根治要讓 Core 存下工作狀態後才回 ack，這要改 Core，建議另開一張票。
- Core 收到 `OUTCOME_UNKNOWN` 時記的日誌寫著「步驟開始過但沒有結束」（`core/internal/nodes/runner.go`）。完成後被壓縮的步驟也會回這個碼，行為正確，只是日誌文字不準。這張票不改 `core/`。
- 不可重試的步驟中斷後，Agent 日誌裡永遠不會有 completed 或 failed，這個工作也就永遠不會被壓縮。每發生一次中斷多留一個工作，數量很小。
- 壓縮在 `ack` 裡執行，期間持有連線模組的 `steps` 鎖。每小時一次，審查者在 release 版實測，從 1 萬 2 千筆裡移出 2 千筆約 26 毫秒。
- 舊版 Agent 讀不懂壓縮後 `journal.jsonl` 的第一行，降版後開檔會失敗，但不會遺失資料。
