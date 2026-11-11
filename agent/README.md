# HazelminX Rust workspace

每台受管主機上的執行端。以 Komodo Periphery 為改造起點（`docs/research/komodo-periphery.md`），但連線模型、權限邊界與資料契約依 `engineering/architecture.md` 重寫。起點 commit、各 crate 的保留／替換／捨棄與授權見 [`UPSTREAM.md`](UPSTREAM.md)；閒置資源量測見 [`PERFORMANCE.md`](PERFORMANCE.md)。

| crate | 執行檔 | 職責 | 票 |
|---|---|---|---|
| `crates/agent` | `hazelminx-agent` | 低權限常駐服務：節點身分、偵測、監控、檔案、容器、工作紀錄；主動連回 Core | 004、007、019 |
| `crates/privileged` | `hazelminx-privileged` | 只接受固定動作的高權限執行器；`client` 模組是 Agent 端客戶端 | 012 |
| `crates/cli`（未建立） | `hazelminx` | 安裝、更新、診斷、救援、卸載 | 013 |
| `crates/protocol` | — | Core↔Agent 訊息型別、解碼、版本協商、握手簽章內容；以 `engineering/contracts/fixtures` 做契約測試 | 007 |

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
hazelminx-agent enroll --core-url https://core.example:9801 --code HZX-XXXX-XXXX-XXXX-XXXX
```

`enroll` 用管理員產生的一次性註冊碼加入 Core：先取得 Core CA 並以註冊碼驗證，再送出節點公鑰與主機名稱、machine-id、作業系統，等管理員在 Core 核准（預設最多等 30 分鐘，`--wait-seconds` 可改；中斷後用同一個碼重跑會接著等）。核准後寫入 `identity/node.crt`、`identity/core-ca.crt`、`identity/privileged-issuer.pub`（安裝程式要以 root 複製到 `/etc/hazelminx/privileged/issuer.pub`）與 `state_dir/enrollment.json`。要以 Agent 的執行帳號跑，檔案擁有者才會對。`--group` 帶碼的群組；`--new-identity` 把舊金鑰移到 `identity.replaced-<時間>` 再產生新金鑰，給複製出來的主機或金鑰被撤銷、取代的節點用。

設定檔 `agent.toml`（缺檔即用預設）：

```toml
state_dir = "/var/lib/hazelminx/agent"   # 節點身分、工作紀錄
core_url = "https://core.example:9801"    # Core 的 Agent 通道；未設定時用 enroll 記下的網址
node_name = "db-01"                        # 顯示名稱；身分以金鑰指紋為準
log_format = "json"                        # json 或 text
core_transport = "pinned"                  # pinned（預設）或 proxy
proxy_ca_file = "/etc/hazelminx/proxy-ca.pem"  # 只在 proxy 模式使用；不設則用作業系統的信任清單
privileged_socket = "/run/hazelminx/privileged.sock"  # 高權限執行器的 socket
```

`core_url` 只接受 `https://` 或 `wss://`。`pinned` 模式以 `state_dir/identity/core-ca.crt` 驗 Core 的 TLS 憑證，名稱固定是 `core.hazelminx.internal`，所以 Core 換 IP 或主機名不影響信任。`proxy` 模式給會終止 TLS 的代理用：TLS 驗代理本身的憑證，Core 身分改由握手時的 Core 憑證與簽章證明。

旗標與環境變數：`--config`／`HAZELMINX_AGENT_CONFIG`、`--state-dir`／`HAZELMINX_AGENT_STATE_DIR`、`--core-url`／`HAZELMINX_AGENT_CORE_URL`、`--log-format`／`HAZELMINX_AGENT_LOG_FORMAT`。

節點身分是 Ed25519 金鑰，第一次啟動產生在 `state_dir/identity/`（目錄 0700、私鑰 0600），指紋是公鑰的 SHA-256。私鑰損壞時報錯停止，不會悄悄重新產生。連線還需要同目錄的 `node.crt`（Core 簽發的節點憑證）與 `core-ca.crt`（Core CA），由 `enroll` 寫入；缺少時 Agent 會說明「尚未加入」並以退避重試。

Core 換發憑證時（剩不到 30 天自動換，或管理員要求），Agent 驗過新憑證後存成 `node.crt.next` 並重連；新憑證握手成功才換成 `node.crt`，舊的留在 `node.crt.prev`。新憑證連不上時刪掉 `node.crt.next`，改回舊憑證。

連線與工作紀錄：

- Agent 主動連 Core，握手交換協定版本與能力（`engineering/contracts/README.md`）。斷線後重連的等待時間在 0.1 秒到上限之間隨機取，上限從 2 秒起每次加倍、最多 60 秒，連線撐過一分鐘後歸零；協定不相容、節點被撤銷、身分無效或身分衝突（同一把金鑰在兩台主機上同時運作）時等 5 到 10 分鐘，並在日誌寫出雙方的協定範圍。握手後每則訊息都帶序號與 HMAC，收到驗不過的訊息就斷線。
- 每個步驟開始前與結束後都先寫進 `state_dir/jobs/journal.jsonl` 並 fsync，再送給 Core。Core 確認過的序號記在 `state_dir/jobs/acked`；重連後重送未確認的事件。
- 同一工作步驟已有完成或失敗紀錄時不重跑，直接重送那筆紀錄。寫不了紀錄時拒絕步驟（`JOURNAL_UNAVAILABLE`），步驟跑完卻寫不進結果時主動斷線，讓 Core 重連後對帳。結果超過 256 KiB 記為失敗 `RESULT_TOO_LARGE`。Agent 重開機時正在跑的步驟記為中斷，不會被當成完成；只有 Core 對帳後送來、而且標記可重試的步驟才會再跑一次，否則回 `OUTCOME_UNKNOWN`。
- 別的節點、已過期、控制世代倒退、未宣告的動作、參數有多餘欄位的步驟一律拒絕，不執行。動作碰到工作沒保留的資源時回 `WRONG_RESOURCE_SCOPE`：每個動作在 `ActionSpec.touches` 宣告它的參數會碰哪些資源，唯讀動作列在 `READ_ONLY`，其他動作沒宣告會讓測試失敗。
- Core 送來 `ledger_pull` 時，Agent 把裡面的授權憑證交給高權限執行器的 `ledger_export`，再把拿到的帳本原始行原樣回成 `ledger_page`。執行器連不上或拒絕時回 `ledger_unavailable`。Agent 自己讀不到帳本，只負責轉交（`src/ledger.rs`，能力 `host.ledger` v1）。
- 目前的動作：`agent.info`、`host.health`（都唯讀；`host.health` 讀 `/proc` 與根目錄的 `statvfs`，讀不到的值回 `null`）。`--features test-actions` 另外編入只供互通測試用的 `test.sleep`，正式建置沒有它。

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
| `ledger_export` | `after_seq`、`limit`（選填，上限 500）、`max_bytes`（選填） | `host.ledger.read` | 回傳 `after_seq` 之後的帳本原始行，給 Core 保存副本；有 `max_bytes` 時，行加起來超過就提早停（至少一行） |

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

Core 的副本：Core 經 Core↔Agent 通道送 `ledger_pull`，Agent 用裡面的授權憑證（權限 `host.ledger.read`）呼叫 `ledger_export`，執行器回傳原始行，Core 驗證雜湊鏈後保存（`core/internal/hostledger`）。`ledger_export` 的 `max_bytes` 讓一頁不超過通道的訊息上限。訊息與驗證規則寫在 `engineering/contracts/README.md` 的「主機變更帳本」一節。

安全事件以 tracing target `hazelminx.security` 輸出，欄位只有識別碼、規則名稱與大小，不含簽章、金鑰或參數值。

`deploy/hazelminx-privileged.service` 以 root 執行但帶 `hazelminx-agent` 群組：`RuntimeDirectory=hazelminx` 是 root:hazelminx-agent 0750，socket 是 0660，所以只有 agent 帳號連得上。`StateDirectory=hazelminx/ledger` 以 0700 建立帳本目錄。
