# HazelminX Rust workspace

每台受管主機上的執行端。以 Komodo Periphery 為改造起點（`docs/research/komodo-periphery.md`），但連線模型、權限邊界與資料契約依 `ENG.md` 重寫。

| crate | 執行檔 | 職責 | 票 |
|---|---|---|---|
| `crates/agent` | `hazelminx-agent` | 低權限常駐服務：節點身分、偵測、監控、檔案、容器、工作紀錄；主動連回 Core | 004、007、019 |
| `crates/privileged`（未建立） | `hazelminx-privileged` | 只接受固定動作的高權限執行器 | 012 |
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
