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
