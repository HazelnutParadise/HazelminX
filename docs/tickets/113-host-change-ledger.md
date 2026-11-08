# 113 — 平台對主機的每個變更都寫進變更帳本，並在安裝時記錄安裝前快照

**Epic:** 主機（`epic:host`）  
**User Story:** 之後要完全移除時，平台能證明哪些東西是自己裝的、哪些是改過的、哪些原本就有  
**Milestone:** M1 單機主機管理  
**Blocked by:** [012](012-privileged-executor.md)  
**Status:** in_progress  
**規格章節:** 增補 07｜55.1｜主機變更帳本  
**Issue:** [#120](https://github.com/HazelnutParadise/hazelminx/issues/120)  

## What it delivers

高權限執行器在每個動作前後寫變更帳本：對象、類型、變更前與變更後的狀態、時間、造成的工作；擁有權分成平台安裝、平台修改、既有。沒寫帳本的變更一律拒絕。帳本在主機本機（Agent 資料目錄，只有 root 可讀）與 Core 各一份。安裝時記錄安裝前快照（套件、systemd 單元、監聽 Port、使用者與群組、防火牆規則數、平台會寫入的路徑）。之後所有會改主機的票（021～026、030、039 等）都經這個機制，不另外記。

## Acceptance criteria

- [ ] **UNX-001**（增補 07）平台對主機的每個變更都寫進變更帳本。 — 完成條件：依 55.1 記錄對象、類型、變更前與變更後的狀態、時間與工作；沒寫進帳本的變更，高權限執行器拒絕執行；帳本在主機本機與 Core 各一份。
  已完成，附證據：
  - 記錄內容：每個會改主機的動作在 `Action::ledger_decl` 宣告對象與改法，執行器先寫意圖行、執行、再寫結果行，兩行都有對象、改法、變更前後狀態、時間、`job_id`、`request_id`、憑證 id 與擁有權（`agent/crates/privileged/src/ledger/`）。測試 `tests/ledger.rs` 的 `a_host_change_is_recorded_before_and_after_with_its_job`，以及在 systemd 容器裡真的重啟單元的 `tests/linux_host.rs` `executor_records_refuses_and_snapshots_on_a_real_host`：變更前 `inactive`、變更後 `active`、擁有權 `platform_modified`。
  - 沒寫帳本就拒絕：見下方 T-250。
  - 主機本機那份：`/var/lib/hazelminx/ledger`，目錄 0700、檔案 0600、root 擁有，雜湊鏈驗證完整性。測試 `ledger::store::tests` 驗證改動、刪除、調換、搬到別台主機都會被發現，當機寫到一半的最後一行會被截掉。真實主機測試驗證 `nobody` 帳號讀不到也寫不進帳本。
  - 檔案的原內容雜湊與備份：`ledger::probe::tests::path_state_with_backup_and_no_follow`、`real_probes_and_user_edit_detection`。
  - 使用者後來改過的偵測：`ledger::drift::tests::user_edits_after_the_platform_wrote_are_reported`，以及真實主機上的 `hazelminx-privileged ledger drift`。

  未完成：Core 那份。Agent 端的匯出介面 `ledger_export` 與 Core 端的驗證規則 `LedgerBatch::verify` 已完成並有測試（`ledger_export_pages_raw_lines_for_core`、`ledger::sync::tests`，範例在 `docs/contracts/fixtures/host-ledger-batch.json`）。Core↔Agent 通道（票 007）合併後，還要接上同步並在 Core 保存副本，所以這條先不勾。
- [x] **UNX-002**（增補 07）平台在安裝時記錄安裝前快照。 — 完成條件：內容依 55.1；移除後可以拿來比對。
  證據：`hazelminx-privileged snapshot capture` 記錄已安裝套件、已啟用的 systemd 單元、監聽中的 Port、使用者與群組、防火牆規則數，以及平台會寫入的路徑是否存在，只存一次，不覆寫。`snapshot compare` 逐項列出相同、新增、移除與變更（`ledger::snapshot::tests`）。真實主機測試：在 systemd 容器裡快照後新增使用者與目錄，比對列出 `hzx-e2e-user` 與 `/etc/hzx-e2e`，結束碼 3；移除後再比對，結束碼 0。安裝器在第一步呼叫它，屬於票 013 的範圍。

## Acceptance tests

- **T-250** 嘗試在沒有經過高權限執行器、也沒有寫帳本的情況下修改主機設定。 → 執行器拒絕；稽核紀錄有這次拒絕。 — 通過：
  - 沒有經過執行器：Agent 帳號讀寫不到帳本，主機變更的權限也只在執行器手上（012）。
  - 沒寫帳本：以下三種情況，執行器都回 `ledger_refused`，記一筆 `ledger_refused` 安全事件，動作完全沒有執行。
    - 動作沒有帳本描述：`t250_change_without_a_ledger_description_is_refused_and_audited`。
    - 帳本不可用：`t250_change_while_the_ledger_is_unavailable_is_refused_and_audited`。
    - 讀不到變更前的狀態：`t250_change_with_an_unreadable_before_state_is_refused_and_audited`。
  - 真實主機：竄改帳本後重新啟動執行器，重啟要求被拒，日誌有 `ledger_refused`，單元維持 `inactive`。

## Delivery record

依規格第 46.4 節，完整內容在 PR 描述。

- **需求 ID**：UNX-001（部分）、UNX-002、T-250。
- **測試環境**：macOS 開發機，以及 `buildpack-deps:trixie` 加 systemd 開機的 Linux 容器（aarch64、root）。容器內跑 `cargo fmt --check`、`clippy -D warnings`、`cargo test --workspace`，以及會改主機的 `tests/linux_host.rs`（加 `HAZELMINX_DISPOSABLE_HOST=1`）。
- **未驗證部分**：
  - Core 的帳本副本，等票 007。
  - 安裝器在第一步拍快照，並用同一個 `Recorder` 記錄安裝步驟，屬於票 013。
  - dnf／rpm 主機只用模擬輸出測過。
  - x86_64 主機沒有實測。
