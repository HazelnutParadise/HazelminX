# HazelminX Rust workspace

每台受管主機上的執行端。以 Komodo Periphery 為改造起點（`docs/research/komodo-periphery.md`），但連線模型、權限邊界與資料契約依 `ENG.md` 重寫。

| crate | 執行檔 | 職責 | 票 |
|---|---|---|---|
| `crates/agent` | `hazelminx-agent` | 低權限常駐服務：節點身分、偵測、監控、檔案、容器、工作紀錄；主動連回 Core | 004、007、019 |
| `crates/privileged` | `hazelminx-privileged` | 只接受固定動作的高權限執行器；`client` 模組是 Agent 端客戶端 | 012 |
| `crates/cli`（未建立） | `hazelminx` | 安裝、更新、診斷、救援、卸載 | 013 |
| `crates/protocol`（未建立） | — | Core↔Agent 訊息型別與黃金樣本 | 007 |

## 建置與測試

```bash
cargo build --workspace
cargo test --workspace
cargo clippy --workspace -- -D warnings
```

## hazelminx-agent

```bash
hazelminx-agent --config /etc/hazelminx/agent.toml run
hazelminx-agent identity        # 印出節點指紋與公鑰位置
hazelminx-agent check-config    # 印出生效設定
```

設定檔 `agent.toml`（缺檔即用預設）：

```toml
state_dir = "/var/lib/hazelminx/agent"   # 節點身分、工作紀錄
core_url = "https://core.example:9801"    # 未設定表示尚未加入
node_name = "db-01"                        # 顯示名稱；身分以金鑰指紋為準
log_format = "json"                        # json 或 text
```

旗標與環境變數：`--config`／`HAZELMINX_AGENT_CONFIG`、`--state-dir`／`HAZELMINX_AGENT_STATE_DIR`、`--core-url`／`HAZELMINX_AGENT_CORE_URL`、`--log-format`／`HAZELMINX_AGENT_LOG_FORMAT`。

節點身分是 Ed25519 金鑰，第一次啟動產生在 `state_dir/identity/`（目錄 0700、私鑰 0600），指紋是公鑰的 SHA-256。私鑰損壞時報錯停止，不會悄悄重新產生。

`deploy/hazelminx-agent.service` 是 systemd 單元（低權限帳號、`ProtectSystem=strict`、失敗重啟有上限）；`deploy/sysusers.d/hazelminx.conf` 宣告服務帳號。

## hazelminx-privileged

```bash
hazelminx-privileged --config /etc/hazelminx/privileged.toml run
hazelminx-privileged check-config
hazelminx-privileged snapshot capture   # 安裝前快照，存一次，不覆寫
hazelminx-privileged snapshot compare   # 拿安裝前快照比對主機現況；相同結束碼 0，不同 3
hazelminx-privileged ledger verify      # 檢查帳本擁有者、權限與雜湊鏈
hazelminx-privileged ledger drift       # 列出平台寫過、之後被手動改過的檔案
```

設定檔 `privileged.toml`（缺檔即用預設；`allowed_uid` 沒有預設，缺少就拒絕啟動）：

```toml
socket_path = "/run/hazelminx/privileged.sock"
issuer_public_key = "/etc/hazelminx/privileged/issuer.pub"   # 簽發授權憑證的 Ed25519 公鑰（SPKI PEM）
allowed_uid = 998                                           # hazelminx-agent 帳號的 uid，安裝器以 id -u 填入
max_concurrent_actions = 8
log_format = "json"
ledger_dir = "/var/lib/hazelminx/ledger"                    # 主機變更帳本，只有 root 可讀寫
```

旗標與環境變數：`--config`／`HAZELMINX_PRIVILEGED_CONFIG`、`--socket`／`HAZELMINX_PRIVILEGED_SOCKET`、`--issuer-key`／`HAZELMINX_PRIVILEGED_ISSUER_KEY`、`--allowed-uid`／`HAZELMINX_PRIVILEGED_ALLOWED_UID`、`--log-format`／`HAZELMINX_PRIVILEGED_LOG_FORMAT`、`--ledger-dir`／`HAZELMINX_PRIVILEGED_LEDGER_DIR`。

每個連線一個請求（一行 JSON），回應是零或多個 `output` 幀加一個結束幀。請求有三種：

| type | 內容 | 檢查 |
|---|---|---|
| `health` | 無 | 呼叫者 uid |
| `revoke` | `grant_id` | 呼叫者 uid |
| `action` | `request_id`、`job_id`（選填）、`grant`、`action` | 呼叫者 uid → 憑證簽章、有效期、撤銷 → 動作種類與權限 → 參數 → 帳本宣告 → 名額 → 寫入帳本 |

動作（`action.kind`）與所需權限：

| kind | 參數 | 權限 | 做什麼 |
|---|---|---|---|
| `system_info` | 無 | `host.info.read` | 讀 `/etc/hostname`、`/etc/os-release`、`/proc/sys/kernel/osrelease`、`/proc/uptime` |
| `list_package_updates` | 無 | `host.packages.read` | `apt-get -q -s upgrade` 或 `dnf -q check-update`，解析成清單 |
| `restart_service` | `unit`（systemd 單元名） | `host.services.manage` | `systemctl restart -- <unit>` 再 `systemctl is-active -- <unit>` |
| `ledger_export` | `after_seq`、`limit`（選填，上限 500） | `host.ledger.read` | 回傳 `after_seq` 之後的帳本原始行，給 Core 保存副本 |

沒有 shell、腳本或任意命令動作；將來若加終端機動作，必須另要 `host.shell` 權限。新增動作的步驟：在 `src/action.rs` 加變體（參數用 `src/params.rs` 的驗證型別）、權限名稱，以及 `Action::ledger_decl` 的一個分支（只讀，或會改哪些對象、改法是什麼），在 `src/handlers/` 加處理器，在 `src/executor.rs` 的 `HostExecutor` 加一個分支。動作改到的對象若是 `src/ledger/entry.rs` 的 `Target` 還沒有的種類（例如防火牆、容器、sysctl），同一張票要加 `Target` 變體與 `src/ledger/probe.rs` 的讀取方式。

授權憑證（`grant`）是 Core 簽發的 Ed25519 簽章物件：`id`、`subject`、`permissions`、`not_before`、`expires_at`（RFC 3339 UTC）、`signature`（hex）。簽章內容是固定的逐行格式 `hazelminx-grant-v1\n{id}\n{subject}\n{not_before}\n{expires_at}\n{permissions 以逗號連接}\n`，Go 端可直接重現。壽命上限 24 小時，撤銷以 `id` 為準，撤銷紀錄保留到該憑證不可能再被接受為止。

### 主機變更帳本

平台對主機的每個變更都寫進帳本（規格增補 07 第 55.1 節）。會改主機的動作，執行器先讀出每個對象現在的狀態，把「意圖」那一行寫進帳本並同步到磁碟，才開始執行。執行完再讀一次狀態，寫「結果」那一行。兩行都記：對象、改法、變更前與變更後的狀態、時間、`job_id`、`request_id`、憑證 id 與擁有權。

以下情況執行器拒絕動作，回 `ledger_refused`，並記一筆 `ledger_refused` 安全事件，動作完全不會執行：

- 執行器對這個動作沒有帳本描述。
- 帳本打不開，或之前有一次寫入失敗。
- 讀不到變更前的狀態，例如 systemd 沒有在執行。

擁有權依 55.1 分三種。對象原本不存在就是 `platform_installed`。原本存在、平台改過內容或狀態是 `platform_modified`。原本存在、狀態沒變（例如重啟一個本來就在跑的服務）是 `existing`。同一個對象之後的紀錄只會往更強的擁有權調整，不會調回去。

帳本放在 `ledger_dir`：目錄 0700、檔案 0600，擁有者都是 root，上層每一層也必須是 root 擁有、群組與其他人不可寫，否則執行器不開帳本並拒絕所有變更。`ledger.jsonl` 每行是 `{"hash":…,"entry":{…}}`，`hash` 是 `entry` 原始位元組的 SHA-256，每筆 `entry` 帶前一行的雜湊，第一行接 `sha256("hazelminx-ledger-v1\n" + ledger_id)`。改動、刪除、調換任何一行，`ledger verify` 和執行器啟動時都會發現。當機時寫到一半的最後一行，下次啟動時會截掉，因為意圖行要先寫完並同步，變更才會開始，所以那一行描述的變更並沒有執行。

檔案類對象在變更前會把原內容複製到 `backups/<sha256>`（64 MiB 以下），帳本記雜湊、權限、擁有者，符號連結只記連結本身，不跟隨。`ledger drift` 拿目前的檔案和帳本最後記錄的狀態比對，列出內容、權限、擁有者或存在與否不同的檔案，完全移除（票 037）時讓使用者決定要還原還是保留。

安裝前快照由安裝器（票 013）在寫入任何東西之前執行 `snapshot capture`，記錄已安裝套件、已啟用的 systemd 單元、監聽中的 Port、使用者與群組、防火牆規則數，以及平台會寫入的路徑是否存在（內建清單在 `src/ledger/snapshot.rs`，`--path` 可再加）。讀不到的項目記為 `unavailable` 並附原因，比對時列為無法比較，不算相同。

Core 的副本：Agent 以 `ledger_export` 分頁取原始行，Core 用 `LedgerBatch::verify` 的同一套規則檢查後保存。訊息型別在 `src/ledger/sync.rs`，範例在 `docs/contracts/fixtures/host-ledger-batch.json`；Core↔Agent 通道接上之前（票 007），這段同步還沒有實際跑起來。

安全事件以 tracing target `hazelminx.security` 輸出，欄位只有識別碼、規則名稱與大小，不含簽章、金鑰或參數值。

`deploy/hazelminx-privileged.service` 以 root 執行但帶 `hazelminx-agent` 群組：`RuntimeDirectory=hazelminx` 是 root:hazelminx-agent 0750，socket 是 0660，所以只有 agent 帳號連得上。`StateDirectory=hazelminx/ledger` 以 0700 建立帳本目錄。
