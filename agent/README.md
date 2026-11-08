# HazelminX Rust workspace

每台受管主機上的執行端。以 Komodo Periphery 為改造起點（`docs/research/komodo-periphery.md`），但連線模型、權限邊界與資料契約依 `ENG.md` 重寫。

| crate | 執行檔 | 職責 | 票 |
|---|---|---|---|
| `crates/agent` | `hazelminx-agent` | 低權限常駐服務：節點身分、偵測、監控、檔案、容器、工作紀錄；主動連回 Core | 004、007、019 |
| `crates/privileged` | `hazelminx-privileged` | 只接受固定動作的高權限執行器；`client` 模組是 Agent 端客戶端 | 012 |
| `crates/cli`（未建立） | `hazelminx` | 安裝、更新、診斷、救援、卸載 | 013 |
| `crates/protocol` | — | Core↔Agent 訊息型別、解碼、版本協商、握手簽章內容；以 `docs/contracts/fixtures` 做契約測試 | 007 |

## 建置與測試

```bash
cargo build --workspace
cargo test --workspace
cargo clippy --workspace -- -D warnings
```

Core↔Agent 互通測試（真的 Agent 執行檔對 Go 的 hub）：

```bash
cargo build -p hazelminx-agent --features test-actions
cd ../core && HAZELMINX_AGENT_BIN=<target>/debug/hazelminx-agent go test ./internal/nodes -run Interop -v
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
core_url = "https://core.example:9801"    # Core 的 Agent 通道；未設定表示尚未加入
node_name = "db-01"                        # 顯示名稱；身分以金鑰指紋為準
log_format = "json"                        # json 或 text
core_transport = "pinned"                  # pinned（預設）或 proxy
proxy_ca_file = "/etc/hazelminx/proxy-ca.pem"  # 只在 proxy 模式使用；不設則用內建公開根憑證
```

`core_url` 只接受 `https://` 或 `wss://`。`pinned` 模式以 `state_dir/identity/core-ca.crt` 驗 Core 的 TLS 憑證，名稱固定是 `core.hazelminx.internal`，所以 Core 換 IP 或主機名不影響信任。`proxy` 模式給會終止 TLS 的代理用：TLS 驗代理本身的憑證，Core 身分改由握手時的 Core 憑證與簽章證明。

旗標與環境變數：`--config`／`HAZELMINX_AGENT_CONFIG`、`--state-dir`／`HAZELMINX_AGENT_STATE_DIR`、`--core-url`／`HAZELMINX_AGENT_CORE_URL`、`--log-format`／`HAZELMINX_AGENT_LOG_FORMAT`。

節點身分是 Ed25519 金鑰，第一次啟動產生在 `state_dir/identity/`（目錄 0700、私鑰 0600），指紋是公鑰的 SHA-256。私鑰損壞時報錯停止，不會悄悄重新產生。連線還需要同目錄的 `node.crt`（Core 簽發的節點憑證）與 `core-ca.crt`（Core CA），由註冊流程（票 019）寫入；缺少時 Agent 會說明「尚未加入」並以退避重試。

連線與工作紀錄：

- Agent 主動連 Core，握手交換協定版本與能力（`docs/contracts/README.md`）。斷線後重連的等待時間在 0.1 秒到上限之間隨機取，上限從 2 秒起每次加倍、最多 60 秒，連線撐過一分鐘後歸零；協定不相容、節點被撤銷或身分無效時等 5 到 10 分鐘，並在日誌寫出雙方的協定範圍。握手後每則訊息都帶序號與 HMAC，收到驗不過的訊息就斷線。
- 每個步驟開始前與結束後都先寫進 `state_dir/jobs/journal.jsonl` 並 fsync，再送給 Core。Core 確認過的序號記在 `state_dir/jobs/acked`；重連後重送未確認的事件。
- 同一工作步驟已有完成或失敗紀錄時不重跑，直接重送那筆紀錄。寫不了紀錄時拒絕步驟（`JOURNAL_UNAVAILABLE`），步驟跑完卻寫不進結果時主動斷線，讓 Core 重連後對帳。結果超過 256 KiB 記為失敗 `RESULT_TOO_LARGE`。Agent 重開機時正在跑的步驟記為中斷，不會被當成完成；只有 Core 對帳後送來、而且標記可重試的步驟才會再跑一次，否則回 `OUTCOME_UNKNOWN`。
- 別的節點、已過期、控制世代倒退、未宣告的動作、參數有多餘欄位的步驟一律拒絕，不執行。
- 目前的動作：`agent.info`（唯讀）。`--features test-actions` 另外編入只供互通測試用的 `test.sleep`，正式建置沒有它。

`deploy/hazelminx-agent.service` 是 systemd 單元（低權限帳號、`ProtectSystem=strict`、失敗重啟有上限）；`deploy/sysusers.d/hazelminx.conf` 宣告服務帳號。

## hazelminx-privileged

```bash
hazelminx-privileged --config /etc/hazelminx/privileged.toml run
hazelminx-privileged check-config
```

設定檔 `privileged.toml`（缺檔即用預設；`allowed_uid` 沒有預設，缺少就拒絕啟動）：

```toml
socket_path = "/run/hazelminx/privileged.sock"
issuer_public_key = "/etc/hazelminx/privileged/issuer.pub"   # 簽發授權憑證的 Ed25519 公鑰（SPKI PEM）
allowed_uid = 998                                           # hazelminx-agent 帳號的 uid，安裝器以 id -u 填入
max_concurrent_actions = 8
log_format = "json"
```

旗標與環境變數：`--config`／`HAZELMINX_PRIVILEGED_CONFIG`、`--socket`／`HAZELMINX_PRIVILEGED_SOCKET`、`--issuer-key`／`HAZELMINX_PRIVILEGED_ISSUER_KEY`、`--allowed-uid`／`HAZELMINX_PRIVILEGED_ALLOWED_UID`、`--log-format`／`HAZELMINX_PRIVILEGED_LOG_FORMAT`。

每個連線一個請求（一行 JSON），回應是零或多個 `output` 幀加一個結束幀。請求有三種：

| type | 內容 | 檢查 |
|---|---|---|
| `health` | 無 | 呼叫者 uid |
| `revoke` | `grant_id` | 呼叫者 uid |
| `action` | `request_id`、`grant`、`action` | 呼叫者 uid → 憑證簽章、有效期、撤銷 → 動作種類與權限 → 參數 → 名額 |

動作（`action.kind`）與所需權限：

| kind | 參數 | 權限 | 做什麼 |
|---|---|---|---|
| `system_info` | 無 | `host.info.read` | 讀 `/etc/hostname`、`/etc/os-release`、`/proc/sys/kernel/osrelease`、`/proc/uptime` |
| `list_package_updates` | 無 | `host.packages.read` | `apt-get -q -s upgrade` 或 `dnf -q check-update`，解析成清單 |
| `restart_service` | `unit`（systemd 單元名） | `host.services.manage` | `systemctl restart -- <unit>` 再 `systemctl is-active -- <unit>` |

沒有 shell、腳本或任意命令動作；將來若加終端機動作，必須另要 `host.shell` 權限。新增動作的步驟：在 `src/action.rs` 加變體（參數用 `src/params.rs` 的驗證型別）與權限名稱，在 `src/handlers/` 加處理器，在 `src/executor.rs` 的 `HostExecutor` 加一個分支。

授權憑證（`grant`）是 Core 簽發的 Ed25519 簽章物件：`id`、`subject`、`permissions`、`not_before`、`expires_at`（RFC 3339 UTC）、`signature`（hex）。簽章內容是固定的逐行格式 `hazelminx-grant-v1\n{id}\n{subject}\n{not_before}\n{expires_at}\n{permissions 以逗號連接}\n`，Go 端可直接重現。壽命上限 24 小時，撤銷以 `id` 為準，撤銷紀錄保留到該憑證不可能再被接受為止。

安全事件以 tracing target `hazelminx.security` 輸出，欄位只有識別碼、規則名稱與大小，不含簽章、金鑰或參數值。

`deploy/hazelminx-privileged.service` 以 root 執行但帶 `hazelminx-agent` 群組：`RuntimeDirectory=hazelminx` 是 root:hazelminx-agent 0750，socket 是 0660，所以只有 agent 帳號連得上。
