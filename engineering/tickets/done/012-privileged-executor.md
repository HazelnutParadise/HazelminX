# 012 — 維運者可以執行需要高權限的固定動作，而 Agent 主程序維持低權限

**Epic:** 節點與 Agent（`epic:nodes`）  
**User Story:** 管理員可以把主機納入管理  
**Milestone:** M0 基礎與設計  
**Blocked by:** 004 管理員可以在主機上以 systemd 執行 hazelminx-agent 並回報身分  
**規格章節:** 32｜節點通訊、信任與高權限操作  
**Issue:** [#12](https://github.com/HazelnutParadise/hazelminx/issues/12)  

## What it delivers

hazelminx-privileged：以本機 socket 接受固定動作與結構化參數（不組 shell 字串）、呼叫者身分驗證、動作白名單、稽核；終端機與腳本列為高風險權限。

## Acceptance criteria

- [x] **SEC-005** 管理員可以限制終端機與腳本權限。 — 完成條件：只有容器唯讀權限的人不能經終端機取得宿主機 root。
  證據：動作集合是封閉 enum，沒有 shell／script／exec 變體（`agent/crates/privileged/src/action.rs` 測試 `there_is_no_shell_script_or_raw_command_action`）；只持 `containers.read` 的憑證要求 `restart_service` 得到 `permission_denied`，送 `{"kind":"shell"}` 得到 `unknown_action`，執行器計數為 0（`tests/server.rs` 測試 `container_read_only_grant_cannot_restart_services_or_get_a_shell`）。未來終端機動作須另要 `host.shell` 權限（`action.rs` 常數 `PERM_SHELL`）。
- [x] **SEC-006** 系統可以安全處理命令參數。 — 完成條件：路徑、使用者名稱與環境變數含空白、引號、換行及 shell 符號時不變成額外命令。
  證據：參數型別 `UnitName`、`UserName`、`HostPath`、`EnvVar` 各有驗證測試（`src/params.rs`）；命令只經 `CommandSpec { program, args, env }` 以 argv 陣列交給 OS（`src/command.rs`），真實程序測試 `tests/argv_safety.rs` 四條通過。
- [x] **SEC-009** 系統可以限制管理通道的負載。 — 完成條件：大型下載、檔案傳輸、終端機輸出不阻塞授權撤銷與健康回報。
  證據：輸出走容量 32 的有界通道（`OUTPUT_CHANNEL_CAPACITY`），讀端不讀就讓產生端停下；`health`／`revoke` 不佔動作名額。測試 `saturated_slots_and_slow_readers_do_not_block_health_or_revocation`：兩個名額都被各串流 8 MiB 且無人讀的動作占滿時，health 與 revoke 在 1 秒內回應，被撤銷的憑證立即被拒，產生端完成數為 0（被背壓卡住而不是緩衝）。請求上限 64 KiB，大檔案不走此 socket。
- [x] **SEC-010** 管理員可以查看安全事件但不看到不該看的秘密。 — 完成條件：授權失敗、憑證異常、參數拒絕有紀錄；不寫完整私鑰或登入碼。
  證據：`SecurityEvent`（`src/audit.rs`）只有識別碼、規則名稱與大小欄位，沒有能放簽章或原始參數值的欄位；測試 `bad_signature_expired_and_revoked_grants_are_refused_and_logged_without_secrets` 驗證事件序列並斷言簽章與其前 16 字元都不在序列化後的紀錄裡；`parameter_rejection_is_logged_by_rule_not_value` 斷言含 `evil` 的參數值不出現在紀錄；`tests/binary.rs` 斷言真實行程的日誌不含簽章。
- [x] 檔名含空白、引號、換行與 shell 符號時不會變成額外命令
  證據：`tests/argv_safety.rs` 測試 `path_argument_with_shell_syntax_is_one_argv_entry` 以真實 `ls` 列出名為 `it's "quoted" name; rm -rf ~ && touch PWNED | cat\n$(touch PWNED2) ...` 的檔案，輸出與原檔名逐位元相同，`PWNED`、`PWNED2`、`PWNED3`、`out` 都沒被建立；`printf_echoes_the_argument_byte_for_byte` 與 `environment_values_are_passed_verbatim_and_nothing_else_leaks` 覆蓋參數與環境變數。
- [x] 大型下載與終端機輸出不阻塞授權撤銷與健康回報
  證據：同 SEC-009。

## Acceptance tests（第 42 章）

- **T-039** 檔名包含空白、引號、換行及 shell 符號。 → 正確處理原檔名，沒有執行額外命令。 — 通過：`agent/crates/privileged/tests/argv_safety.rs`（macOS 開發機與 Docker `buildpack-deps:trixie` + rustup 1.97.0 的 Linux 容器皆執行）。

## Delivery record

- **需求 ID**：SEC-005、SEC-006、SEC-009、SEC-010；T-039。
- **變更**：新增 crate `agent/crates/privileged`（執行檔 `hazelminx-privileged`，函式庫 `hazelminx_privileged`）：具名動作 `system_info`、`list_package_updates`、`restart_service`；Ed25519 簽章的授權憑證（每個動作對應一個權限名稱，憑證有效期上限 24 小時，可撤銷）；Unix socket 伺服器（SO_PEERCRED 驗 uid、請求上限 64 KiB、動作名額、控制訊息不排隊）；安全事件紀錄；Agent 端客戶端模組；systemd 單元 `agent/deploy/hazelminx-privileged.service`。
- **影響範圍**：只新增檔案與 workspace 依賴（serde_json、base64、time；tokio 加 net／io-util／sync／process）。`hazelminx-agent` 行為不變，尚未接上客戶端（票 007／022 接）。
- **測試環境**：macOS 開發機 `cargo test --workspace`（57 個測試）、`cargo clippy --workspace --all-targets -- -D warnings`、`cargo fmt --check`；Docker `buildpack-deps:trixie` + rustup 1.97.0 的 Linux 容器（aarch64）同一套測試、clippy 與 fmt（結果見 PR）。
- **正常案例**：health；簽章有效且權限相符的動作執行並串流輸出；撤銷後拒絕；二進位以 SIGTERM 結束並移除 socket。
- **失敗案例**：錯 uid（讀任何資料前拒絕）、超大請求、非 JSON、10 秒內沒送完請求行、簽章被竄改、過期、尚未生效、壽命過長、已撤銷、撤銷 id 不是 token、權限不足、未知動作、參數不合法（紀錄裡的值會被遮掉）、處理器失敗、名額滿（30 秒後 `busy`）。
- **資料與安全影響**：執行器以 root 執行，但只接受上述三個動作；憑證簽發金鑰由安裝器放在 `/etc/hazelminx/privileged/issuer.pub`，執行器不持有任何私鑰；日誌不含簽章與參數值。
- **實際證據**：測試檔 `src/*.rs`（33 個單元測試）、`tests/server.rs`（11）、`tests/argv_safety.rs`（4）、`tests/binary.rs`（1）；agent crate 原有 8 個不變。
- **未驗證部分**：在真實 Linux 主機上以 root 執行 `systemctl restart` 與 `apt-get -s upgrade`／`dnf check-update`（本機無 VM，CI 是票 006）；systemd 單元實際安裝與 `RuntimeDirectory` 權限（票 013 安裝器驗證）；Core（Go）端簽發憑證（票 007／019）。已知限制：連線保持開啟但不讀輸出的呼叫者會讓該動作與其子程序停在背壓上並占住一個名額（呼叫者只有 agent 帳號；整體動作逾時留給後續票）；憑證尚未綁定節點指紋（票 019 加入後補）。
- **需要使用者決策**：無。
