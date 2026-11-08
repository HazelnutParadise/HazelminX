# 008 — 使用者可以提交工作後關閉頁面，之後仍能查到進度、證據與結果

**Epic:** 基礎建設（`epic:foundation`）  
**User Story:** 使用者可以安全執行長時間操作  
**Milestone:** M0 基礎與設計  
**Blocked by:** [003 管理員可以啟動 hazelminx-core 並通過健康檢查](003-core-skeleton.md)  
**Status:** done  
**規格章節:** 33｜工作引擎：按一次按鈕後，如何可靠完成  
**Issue:** [#8](https://github.com/HazelnutParadise/hazelminx/issues/8)  

## What it delivers

工作引擎：33.1 的狀態機、請求識別碼去重、資源鎖（認同一磁碟／掛載／父子路徑）、持久事件序號、取消語意、狀態不明對帳、資源預算、每類工作的完成檢查介面。

## Acceptance criteria

證據路徑以 `core/` 為根；測試以 `go test -race ./...` 執行（2026-11-07，macOS arm64，Go 1.26.5）。

- [x] **ENG-001** 使用者可以關閉頁面後再查工作。 — 完成條件：工作不隨 HTTP 連線消失；進度與證據可重新讀取。 — 證據：`internal/jobs/engine_test.go` `TestJobSurvivesCallerAndIsReReadable`（提交後取消請求 context，工作照跑；重開 Engine 後紀錄仍在；事件序號連續且可從任一序號接續）、`internal/api/jobs_test.go` `TestJobsAPISubmitGetCancelFlow`。
- [x] **ENG-002** 系統可以辨識重複要求。 — 完成條件：同一識別碼、相同內容回同一工作；相同碼不同內容回衝突。 — 證據：`TestDuplicateRequestReturnsSameJobAndChangedContentConflicts`（鍵序與空白不同仍視為同內容；不同磁碟目標回 `ErrConflict`，API 回 409 `REQUEST_ID_CONFLICT`）；資料庫 `jobs.request_id UNIQUE` 作後盾。
- [x] **ENG-003** Agent 可以在重新開機後恢復工作紀錄。 — 完成條件：不把所有執行中工作重設成功或從頭重跑；先核對步驟與現場狀態。 — 證據：Core 端 `TestRecoverAfterRestartDoesNotResetOrRerun`（Core 重啟後執行中工作變「狀態不明」、步驟紀錄保留、不重跑、鎖重建；`Reconcile` 收到執行端紀錄後才接續）。Agent 端（票 007）：每個步驟開始前與結束後先寫入 `state_dir/jobs/journal.jsonl` 並 fsync（`agent/crates/agent/src/journal/tests.rs`，含撕裂寫入、中間行損毀、序號跳號）；重開機時開始過但沒結束的步驟記為中斷，不回報完成、不自行重跑（`agent/crates/agent/src/steps/tests.rs` 的中斷情境）；真 Agent 執行檔在步驟中被 SIGKILL 後重啟，Core 先以 `step_reconcile` 查紀錄，不可重試的步驟停在「需要處理」且只跑過一次（`core/internal/nodes/interop_test.go` `TestInteropAgentCrashMidStepIsNotRerunOrMarkedDone`），可重試的步驟對帳後再跑一次並成功（`TestInteropAgentCrashMidRetryableStepRunsAgainAfterReconcile`）。
- [x] **ENG-004** 系統可以鎖住互相衝突的操作。 — 完成條件：同磁碟格式化與搬移、同資料庫升級與還原無法同時執行。 — 證據：`internal/jobs/locks_test.go` `TestLockConflicts`（同裝置、掛載、父子路徑、解析後路徑、跨節點不衝突）、`TestLockTableAllOrNothing`、`TestConflictingJobsAreSerialised`（格式化中的磁碟上搬移資料排隊，理由指向持鎖工作；無關工作照跑）、`TestInjectedDisconnectIsUnknownNotFailed`（同資料庫還原等待升級對帳完成）。
- [x] **ENG-006** Agent 可以在執行前再次檢查環境。 — 完成條件：預檢後被別人占用的連接埠或換掉的磁碟會阻擋操作。 — 證據：`TestRecheckBlocksWhenEnvironmentChanged`（每步執行前呼叫 `Handler.Recheck`；回報連接埠被占用即失敗為 `PORT_IN_USE`、步驟不執行、建議不含 kill）。實際探測由各類工作的 Handler（Agent 端）提供。
- [x] **ENG-007** 使用者可以取消工作。 — 完成條件：可取消步驟停止；不可安全中止的步驟清楚顯示等待原因；不以強殺當作保證。 — 證據：`TestCancelStopsCancellableStep`、`TestCancelWaitsForIrreversibleStep`（不可逆步驟不收到取消 context，`cancel_requested` 期間步驟帶 `waitReason`，完成後才 `cancelled` 並列出已完成未撤回部分）、`TestCancelQueuedJobIsImmediate`。
- [x] **ENG-008** 系統可以區分失敗、已復原及狀態不明。 — 完成條件：注入斷線、磁碟滿與復原失敗時，三者狀態正確不同。 — 證據：`TestInjectedDisconnectIsUnknownNotFailed`（`unknown`）、`TestInjectedDiskFullRollsBack`（`rolled_back`）、`TestRollbackFailureNeedsAttention`（`needs_attention`）、`TestKnownFailureWithoutRollbackMethodStaysFailed`（`failed`）。
- [x] **ENG-009** 使用者可以看到完整但受控的診斷。 — 完成條件：顯示原始錯誤摘要、相關日誌、系統狀態與建議；沒有證據時不捏造根因。 — 證據：`TestDiagnosticsNeverInventRootCause`（無證據的 cause 被丟棄，原始摘要保留）、`TestInjectedDiskFullRollsBack`（摘要、日誌尾、證據、建議齊全）、`TestRollbackFailureNeedsAttention`（原錯誤與復原錯誤都呈現、殘留列出）。
- [x] **ENG-011** 系統可以對不同工作設定資源預算。 — 完成條件：備份、建置、解壓縮不能耗盡管理程序所需 CPU、記憶體與空間。 — 證據：`TestBudgetLimitsConcurrencyPerKind`（每類並行上限，超出者排隊並說明）、`TestBudgetKeepsSpaceReserve`（保留給 hazelminx-core 的空間不夠時以 `BUDGET_SPACE_RESERVED` 拒絕，不啟動）。CPU／記憶體以並行數控制，尚無 cgroup 級限制（見未驗證）。
- [x] **ENG-012** 維護者可以驗證真正的完成條件。 — 完成條件：每類工作有獨立的完成檢查；必要檢查未通過不能標成功。 — 證據：`Handler` 介面強制每類實作 `Verify`（`TestRegisterRejectsDuplicateAndNil`）、`TestRequiredCheckFailurePreventsSuccess`（步驟全成功但必要檢查失敗 → 不標成功，`COMPLETION_CHECK_FAILED`）、`TestOptionalCheckFailureStillSucceeds`；狀態表禁止 `running → succeeded`（`TestStateTableMatchesSpec`）。
- [x] Agent 重開機後不把執行中工作重設成功或從頭重跑 — 同 ENG-003。
- [x] 注入斷線、磁碟滿與復原失敗時，失敗／已復原／狀態不明三者顯示不同 — 同 ENG-008。

## Acceptance tests（第 42 章）

- **T-025** 網頁對同一安裝要求連按或重送。 → 相同請求只形成一個工作。 — 通過：`TestDuplicateRequestReturnsSameJobAndChangedContentConflicts`、API `TestJobsAPISubmitGetCancelFlow`（重送回同 `jobId`、`deduplicated: true`，清單只有一筆）。
- **T-026** 相同請求 ID 改成另一個磁碟目標。 → 拒絕衝突，不沿用原批准。 — 通過：同上（409 `REQUEST_ID_CONFLICT`）。
- **T-027** 工作執行中關閉瀏覽器。 → 工作繼續；重新登入可接續進度。 — 通過（無瀏覽器，以請求 context 取消模擬）：`TestJobSurvivesCallerAndIsReReadable`；事件以 `?after=<seq>` 接續。
- **T-028** Agent 做完操作，但回傳前斷線。 → Core 顯示不明；重連對帳，不再次執行破壞性步驟。 — Core 端通過：`TestInjectedDisconnectIsUnknownNotFailed`、`TestReconcileStartedButUnfinishedIrreversibleStepNeedsAttention`、`TestReconcileNeverStartedStepReruns`。真實 Agent：`TestInteropConnectionLossDuringStepReconcilesWithoutRerun`（票 007，步驟執行中斷線，重連對帳後接受完成紀錄，步驟只跑一次）。
- **T-029** 磁碟格式化中要求取消。 → 不承諾可撤回；在安全點停止並說明結果。 — 通過：`TestCancelWaitsForIrreversibleStep`。
- **T-031** 預檢後讓連接埠被另一程序占用。 → 執行前再檢查並停止，不殺別人的程序。 — 通過（注入）：`TestRecheckBlocksWhenEnvironmentChanged`。
- **T-033** 原操作與復原都失敗。 → 顯示需要處理、殘留與證據，不標已復原。 — 通過：`TestRollbackFailureNeedsAttention`。
- **T-034** 大量日誌輸出淹沒通道。 → 關鍵心跳與撤銷仍可處理，截斷／遺失明示。 — 通過（行程內）：`TestLogFloodIsTruncatedAndCancelStillWorks`（兩萬行日誌下取消仍生效、心跳有紀錄、日誌尾有上限、`logsDropped` 與 `log_truncated` 事件明示）。跨網路通道：協定的 `step_progress` 帶 `logsDropped` 丟棄計數，Core 會把丟棄行數寫進步驟日誌；Agent 目前沒有會大量輸出日誌的動作，送端背壓隨第一個這類動作（例如票 027 終端機）驗。

## Delivery record

- 需求 ID：ENG-001、002、003、004、006、007、008、009、011、012 完成；ENG-003 的 Agent 端由票 007 接上。
- 變更：新增 `core/internal/jobs`（狀態機、鎖表、引擎、回報器、SQLite 儲存）、migration `0002_jobs.sql`（`jobs`、`job_events`）、`core/internal/api/jobs.go`（`/management/v1/jobs` 列出、提交、取得、事件、取消）、`main.go` 接線（啟動時 `Recover`、關閉時 `Stop`）、總覽改為動態列出操作。
- 票 007 對引擎的新增：`StepError.NothingChanged`（執行端拒絕、沒做任何事的這次嘗試不算執行過）與 `Step.Attempts`（步驟開始執行的次數，先前有過中斷的嘗試時，被拒的重試仍照常回滾）；測試 `TestRefusedStepIsNotCountedAsExecuted`、`TestRefusedRetryAfterInterruptedAttemptStillRollsBack`。`TestRecoverAfterRestartDoesNotResetOrRerun` 原本把測試自己排入的另一個同種工作也算進「不得重跑」，不加 `-race` 時穩定失敗，已改成確認它排隊後取消，只檢查被對帳的工作。
- 影響範圍：Core 資料庫 schema 升到 2；`api.Server` 新增 `RegisterJobs`；未動前端、Agent、`docs/design/`。
- 測試環境：macOS arm64、Go 1.26.5、`go vet ./...` 與 `go test -race ./...` 全綠；本機啟動執行檔並以 curl 驗證總覽、清單、400／404 與 `/healthz`。
- 正常與失敗案例：見上方逐條證據；失敗注入涵蓋斷線、磁碟滿、復原失敗、再檢查阻擋、預算不足、必要檢查失敗、日誌洪水。
- 資料與安全影響：新增兩張表，工作紀錄只增不改（終態不可修改）；API 尚無驗權（票 009），不得視為已授權寫入路徑；日誌尾與診斷由 Handler 提供，Handler 須自行遮罩機密。
- 未驗證：控制世代拒絕舊命令（ENG-010，票 090）、等待批准流程（ENG-005，票 016）、cgroup 級 CPU／記憶體限制、Linux 實機執行。
- 需要使用者決策：無。
