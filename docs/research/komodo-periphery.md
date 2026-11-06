> 來源：moghtech/komodo（main，commit 780ac68，2026-09-01）bin/periphery 唯讀分析，2026-11-07，由派工 agent（agy / gemini-3.8-flash-medium）產出，主 agent 核對過 Cargo.toml、src 目錄與授權。報告是線索，實作前固定 commit 重新核對。

以下為針對 Komodo `bin/periphery` 原始碼之深度架構分析與改造成自家 Rust 主機 Agent（對接 Go 控制中心）之評估報告。本報告所有結論皆附帶原始碼檔案相對路徑與行號／函式作為依據。

---

### 改動檔案說明
- **改動檔案**：無（本任務為唯讀調查，未修改、commit、stash 或 reset 任何專案檔案）。

---

### 1. bin/periphery 的目錄與模組結構及職責

`bin/periphery` 之原始碼位於 `bin/periphery/src/`，主要模組結構與職責如下：

1. **進入點與生命週期管理**：[`bin/periphery/src/main.rs`](`bin/periphery/src/main.rs#L23-L119`)
   - 函式：`main()`, `app()`
   - 負責載入設定、初始化日誌、產生/讀取金鑰、安裝 `aws_lc_rs` 加密提供者、啟動系統與 Docker 狀態的背景定時採樣任務、依據配置啟動對 Core 的出站連線（Client）或入站監聽（Server），並攔截 Unix `SIGTERM` 訊號以安全關閉所有執行中的終端機。
2. **配置載入模組**：[`bin/periphery/src/config.rs`](`bin/periphery/src/config.rs#L14-L186`)
   - 函式：`periphery_args()`, `periphery_config()`
   - 透過 `clap` 解析命令列參數、`envy` 解析環境變數，並利用 `mogh_config::ConfigLoader` 載入階層式 TOML 設定檔，處理金鑰檔案讀取與預設值合併。
3. **全域狀態儲存**：[`bin/periphery/src/state.rs`](`bin/periphery/src/state.rs#L1-L157`)
   - 使用 `OnceLock` 與執行緒安全容器（如 `RwLock`, `ArcSwap`, `BufferedChannel`）管理 Periphery 本機公私鑰、受信任 Core 公鑰、活躍連線通道（`core_connections`）、活躍虛擬終端實例（`terminals`）、終端通道映射（`terminal_channels`）與監控快取資料。
4. **連線通訊模組**：`bin/periphery/src/connection/`
   - [`bin/periphery/src/connection/mod.rs`](`bin/periphery/src/connection/mod.rs#L64-L187`)：共用連線處理常式（`handle_socket`, `handle_request`），在單一 WebSocket 連線上多工傳輸 Request/Response 與終端機資料串流，並在請求執行期間定期發送心跳保活。
   - [`bin/periphery/src/connection/client.rs`](`bin/periphery/src/connection/client.rs#L28-L174`)：Periphery 主動對外連向 Core 的 WebSocket 客戶端處理常式，負責斷線重試、自動 Onboarding 流程與 Noise 協定握手。
   - [`bin/periphery/src/connection/server.rs`](`bin/periphery/src/connection/server.rs#L36-L55`)：基於 Axum 開設的入站 HTTP/WebSocket 伺服器，支援 IP 白名單過濾 (`guard_request_by_ip`) 與 Noise/Passkey 身分驗證。
5. **API 請求解算層**：`bin/periphery/src/api/`
   - [`bin/periphery/src/api/mod.rs`](`bin/periphery/src/api/mod.rs#L47-L181`)：定義 Core 派工請求之列舉 `PeripheryRequest`，利用 `mogh_resolver::Resolve` 模式分派至各子模組處理。
   - 子模組包含：`container/`（容器控制與日誌）、`compose.rs`（Compose 管理）、`build/`（映像建置）、`docker.rs`（網路/映像/磁碟卷）、`git.rs`（倉庫管理）、`poll.rs`（狀態輪詢）、`terminal.rs`（終端機介接）、`keys.rs`（金鑰輪替）、`swarm/`（Swarm 叢集資源）。
6. **Docker 引擎介接模組**：`bin/periphery/src/docker/`
   - [`bin/periphery/src/docker/mod.rs`](`bin/periphery/src/docker/mod.rs#L24-L35`)：包裝 `bollard::Docker` 實例為 `DockerClient`。
   - 子檔案涵蓋容器、Compose、映像、網路、Volume、Swarm（服務、節點、堆疊、Secret 等）以及狀態輪詢（[`bin/periphery/src/docker/stats.rs`](`bin/periphery/src/docker/stats.rs#L23-L89`)）。
7. **系統監控模組**：`bin/periphery/src/stats/`
   - [`bin/periphery/src/stats/mod.rs`](`bin/periphery/src/stats/mod.rs#L16-L116`)：透過 `sysinfo` 收集主機 CPU、附載、記憶體、磁碟、網路與程序清單。
   - [`bin/periphery/src/stats/mem.rs`](`bin/periphery/src/stats/mem.rs#L19-L74`)：在 Linux 下直接解析 `/proc/meminfo` 與 `/proc/spl/kstat/zfs/arcstats` 精確計算 Buff/Cache 與 ZFS ARC。
8. **終端機（PTY）底層模組**：[`bin/periphery/src/terminal.rs`](`bin/periphery/src/terminal.rs#L77-L400`)
   - 基於 `portable-pty` 實作 PTY 分配、子程序生成、標準輸入/輸出非同步轉發與輸出歷史快取。
9. **Stack 檔案管理模組**：`bin/periphery/src/stack/`
   - [`bin/periphery/src/stack/write.rs`](`bin/periphery/src/stack/write.rs#L1-L100`)：將 Stack 的 Compose 檔、環境變數與額外設定檔寫入本機硬碟路徑。
10. **輔助工具模組**：[`bin/periphery/src/helpers.rs`](`bin/periphery/src/helpers.rs#L27-L385`)
    - 提供 Docker 命令列標籤與環境變數之字串格式化、利用 OpenDNS 解析外網 IP，以及自動以 OpenSSL 命令建立自簽 SSL 憑證。

---

### 2. periphery 依賴的 workspace 內 lib／client crate 與共用型別

依據 [`bin/periphery/Cargo.toml`](`bin/periphery/Cargo.toml#L16-L31`) 與根目錄 [`Cargo.toml`](`Cargo.toml#L17-L26`)，`periphery` 相依的內部 workspace crate 共 9 個：

1. **`komodo_client`**（路徑：`client/core/rs`）：
   - **用途**：Komodo 核心客戶端與全域資料實體型別庫。
   - **與 Core 共用型別**：**是**。共用型別之核心來源。定義了 `entities::config::periphery::*`（配置）、`entities::docker::*`（Docker 容器/映像/網路/Swarm 資料結構）、`entities::stats::*`（系統指標與程序型別）、`entities::terminal::*`（終端機目標與訊息格式）以及 `entities::update::Log`（命令執行日誌）。
2. **`periphery_client`**（路徑：`client/periphery/rs`）：
   - **用途**：Periphery 專屬 API 定義與通訊傳輸協定封裝。
   - **與 Core 共用型別**：**是**。Core 與 Periphery 兩端皆透過此 crate 取得 `api::*` 下的各項請求參數型別（如 `PollStatus`, `RunContainer` 等）與 `transport::*` 下的傳輸封裝（如 `TransportMessage`, `LoginMessage` 等）。
3. **`command`**（路徑：`lib/command`）：
   - **用途**：命令執行庫。封裝本機行程呼叫、進程組 (process group) 隔離、逾時終止與日誌捕獲。不與 Core 共用型別。
4. **`environment`**（路徑：`lib/environment`）：
   - **用途**：提供 `write_env_file` 將環境變數安全格式化寫入磁碟 `.env`。不與 Core 共用型別。
5. **`interpolate`**（路徑：`lib/interpolate`）：
   - **用途**：字串模板變數插值引擎（提供 `Interpolator` 替換 `${VAR}`）。不與 Core 共用型別。
6. **`formatting`**（路徑：`lib/formatting`）：
   - **用途**：錯誤訊息與字串格式化工具函式。不與 Core 共用型別。
7. **`transport`**（路徑：`lib/transport`）：
   - **用途**：網路傳輸與認證抽象庫。提供 WebSocket 封裝（Axum / Tungstenite）、Channel 訊息分發，以及基於 Noise 協定的雙向非對稱公私鑰認證流程（`LoginFlow`, `ServerLoginFlow`, `ClientLoginFlow`）。
   - **與 Core 共用型別**：通訊握手邏輯兩端共用。
8. **`encoding`**（路徑：`lib/encoding`）：
   - **用途**：定義訊息序列化/反序列化特徵（`CastBytes`, `Encode`, `Decode`, `WithChannel`）及 JSON / 二進位封裝。通訊層兩端共用。
9. **`git`**（路徑：`lib/git`）：
   - **用途**：封裝本機 Git 操作（Clone, Pull, Commit Hash 取得等）。不與 Core 共用型別。

> **補充說明**：`bin/periphery/Cargo.toml` 中出現的 `mogh_*`（如 `mogh_server`, `mogh_pki`, `mogh_config`, `mogh_logger` 等）並非 workspace 本地 crate，而是發布在 crates.io 上的外部共用套件。

---

### 3. Core 與 periphery 之間的通訊架構

依據程式碼證據，通訊架構細節如下：

1. **連線發起方向（支援雙向模式）**：
   - **出站連線（Periphery 主動連 Core）**：若配置了 `core_addresses`，Periphery 會主動發起對 Core 的 WebSocket 連線，連向 `{address}/ws/periphery?server={connect_as}`（依據 [`bin/periphery/src/connection/client.rs:28-48`](`bin/periphery/src/connection/client.rs#L28-L48`)）。
   - **入站連線（Periphery 開 Server 被 Core 連）**：若 `server_enabled = true`（預設或手動開啟），Periphery 會使用 Axum 啟動 HTTP/WebSocket Server，監聽指定埠（預設 8120），由 Core 連入根路徑 `/` 並升級為 WebSocket（依據 [`bin/periphery/src/connection/server.rs:36-55`](`bin/periphery/src/connection/server.rs#L36-L55`)）。
2. **傳輸協定**：
   - 雙向全雙工 **WebSocket** 長連線（入站使用 `axum::extract::WebSocketUpgrade`，出站使用 `tokio-tungstenite`）。
3. **請求格式與分派（Resolver Pattern）**：
   - 外層訊息封裝為 `periphery_client::transport::TransportMessage::Request(EncodedRequestMessage)`，內含通道 ID（UUID）與編碼資料（依據 [`bin/periphery/src/connection/mod.rs:143-186`](`bin/periphery/src/connection/mod.rs#L143-L186`) 函式 `handle_request`）。
   - 請求主體為 JSON 格式的 `PeripheryRequest` 列舉，標記 `#[serde(tag = "type", content = "params")]`，例如 `{"type": "RunContainer", "params": {...}}`。
   - 採用 **Resolver Pattern**：透過 `mogh_resolver::Resolve<Args>` trait 非同步解算，每個請求會在獨立的 tokio 任務中執行，且在處理期間每隔 4 秒發送一次 `send_in_progress` 保活訊號，直到最終將結果封裝回傳。
4. **身分驗證機制**：
   - **Mutual Noise Handshake（主要認證）**：依據 [`lib/transport/src/auth.rs:45-100`](`lib/transport/src/auth.rs#L45-L100`)，雙方使用非對稱公私鑰（基於 `mogh_pki` 的 Ed25519 / SPKI）在 WebSocket 建立後進行 3 步 Noise 協定握手。握手過程使用連線綁定特徵（Host、Query、Sec-WebSocket-Accept、Nonce）計算 Prologue Hash 以防止重送與中間人攻擊。
   - **Onboarding 自動註冊**：若連入 Core 時 Core 尚無此 Server，且 Periphery 提供了 `PERIPHERY_ONBOARDING_KEY`，則會走自動註冊握手並將 Periphery 公鑰交付 Core（依據 [`bin/periphery/src/connection/client.rs:176-217`](`bin/periphery/src/connection/client.rs#L176-L217`)）。
   - **Passkey 驗證（舊版相容）**：若未配置公鑰，入站模式可退回比對純文字 passkey（依據 [`bin/periphery/src/connection/server.rs:176-240`](`bin/periphery/src/connection/server.rs#L176-L240`)）。
   - **IP Allowlist**：入站伺服器透過 `guard_request_by_ip` 中介軟體比對來源 IP 是否落在 `allowed_ips` 網段（依據 [`bin/periphery/src/connection/server.rs:242-274`](`bin/periphery/src/connection/server.rs#L242-L274`)）。
   - **mTLS**：**未實作**。TLS 僅用於伺服器端單向傳輸加密，雙向驗證完全在應用層由 Noise Protocol 完成。
5. **TLS 設定**：
   - 入站 Server 若開啟 `ssl_enabled`，會檢查憑證路徑，若無憑證則呼叫本機 `openssl req -x509 ...` 自動產生自簽 SSL 憑證（依據 [`bin/periphery/src/helpers.rs:342-385`](`bin/periphery/src/helpers.rs#L342-L385`)）。
   - 出站 Client 支援 `core_tls_insecure_skip_verify` 跳過對 Core TLS 憑證之有效性檢查（依據 [`bin/periphery/src/connection/client.rs:223`](`bin/periphery/src/connection/client.rs#L223`)）。

---

### 4. periphery 全部操作類型清單

`PeripheryRequest` 定義於 [`bin/periphery/src/api/mod.rs:47-181`](`bin/periphery/src/api/mod.rs#L47-L181`)，共 61 種操作型別：

| 分類 | Request 型別 | 處理檔案與位置 (相對於 repo 根目錄) |
| :--- | :--- | :--- |
| **系統資訊與輪詢** | `PollStatus` | [`bin/periphery/src/api/poll.rs`](`bin/periphery/src/api/poll.rs`) |
| | `GetHealth` | [`bin/periphery/src/api/mod.rs:185`](`bin/periphery/src/api/mod.rs#L185`) |
| | `GetVersion` | [`bin/periphery/src/api/mod.rs:196`](`bin/periphery/src/api/mod.rs#L196`) |
| | `GetSystemProcesses` | [`bin/periphery/src/api/mod.rs:209`](`bin/periphery/src/api/mod.rs#L209`) |
| **全域設定查詢** | `ListGitProviders` | [`bin/periphery/src/api/mod.rs:220`](`bin/periphery/src/api/mod.rs#L220`) |
| | `ListImageRegistries` | [`bin/periphery/src/api/mod.rs:229`](`bin/periphery/src/api/mod.rs#L229`) |
| | `ListSecrets` | [`bin/periphery/src/api/mod.rs:240`](`bin/periphery/src/api/mod.rs#L240`) |
| **系統清理** | `PruneSystem` | [`bin/periphery/src/api/mod.rs:252`](`bin/periphery/src/api/mod.rs#L252`) |
| **Git 倉庫管理** | `CloneRepo`, `PullRepo`, `PullOrCloneRepo`, `RenameRepo`, `DeleteRepo`, `GetLatestCommit` | [`bin/periphery/src/api/git.rs`](`bin/periphery/src/api/git.rs`) |
| **映像建置與檔案** | `GetDockerfileContentsOnHost`, `WriteDockerfileContentsToHost`, `Build`, `CancelBuild`, `PruneBuilders`, `PruneBuildx` | [`bin/periphery/src/api/build/mod.rs`](`bin/periphery/src/api/build/mod.rs`) |
| **Compose 與檔案** | `GetComposeContentsOnHost`, `WriteComposeContentsToHost`, `WriteCommitComposeContents`, `GetComposeLog`, `GetComposeLogSearch`, `ComposePull`, `ComposeUp`, `ComposeExecution`, `ComposeRun` | [`bin/periphery/src/api/compose.rs`](`bin/periphery/src/api/compose.rs`) |
| **容器查詢與操作** | `RunContainer` | [`bin/periphery/src/api/container/run.rs`](`bin/periphery/src/api/container/run.rs`) |
| | `InspectContainer`, `GetContainerLog`, `GetContainerLogSearch`, `GetContainerStats`, `GetContainerStatsList`, `GetFullContainerStats`, `StartContainer`, `RestartContainer`, `PauseContainer`, `UnpauseContainer`, `StopContainer`, `StartAllContainers`, `RestartAllContainers`, `PauseAllContainers`, `UnpauseAllContainers`, `StopAllContainers`, `RemoveContainer`, `RenameContainer`, `PruneContainers` | [`bin/periphery/src/api/container/mod.rs`](`bin/periphery/src/api/container/mod.rs`) |
| **網路管理** | `InspectNetwork`, `CreateNetwork`, `DeleteNetwork`, `PruneNetworks` | [`bin/periphery/src/api/docker.rs`](`bin/periphery/src/api/docker.rs`) |
| **映像檔管理** | `InspectImage`, `ImageHistory`, `GetLatestImageDigest`, `PullImage`, `DeleteImage`, `PruneImages` | [`bin/periphery/src/api/docker.rs`](`bin/periphery/src/api/docker.rs`) |
| **Volume 管理** | `InspectVolume`, `DeleteVolume`, `PruneVolumes` | [`bin/periphery/src/api/docker.rs`](`bin/periphery/src/api/docker.rs`) |
| **Docker Swarm** | `PollSwarmStatus`, `InspectSwarmNode`, `UpdateSwarmNode`, `RemoveSwarmNodes`, `InspectSwarmTask` | [`bin/periphery/src/api/swarm/mod.rs`](`bin/periphery/src/api/swarm/mod.rs`) |
| | `InspectSwarmStack`, `DeploySwarmStack`, `RemoveSwarmStacks` | [`bin/periphery/src/api/swarm/stack.rs`](`bin/periphery/src/api/swarm/stack.rs`) |
| | `InspectSwarmService`, `GetSwarmServiceLog`, `GetSwarmServiceLogSearch`, `CreateSwarmService`, `UpdateSwarmService`, `RollbackSwarmService`, `RemoveSwarmServices` | [`bin/periphery/src/api/swarm/service.rs`](`bin/periphery/src/api/swarm/service.rs`) |
| | `InspectSwarmConfig`, `CreateSwarmConfig`, `RotateSwarmConfig`, `RemoveSwarmConfigs` | [`bin/periphery/src/api/swarm/config.rs`](`bin/periphery/src/api/swarm/config.rs`) |
| | `InspectSwarmSecret`, `CreateSwarmSecret`, `RotateSwarmSecret`, `RemoveSwarmSecrets` | [`bin/periphery/src/api/swarm/secret.rs`](`bin/periphery/src/api/swarm/secret.rs`) |
| **虛擬終端機** | `ListTerminals`, `CreateServerTerminal`, `CreateContainerExecTerminal`, `CreateContainerAttachTerminal`, `DeleteTerminal`, `DeleteAllTerminals`, `ConnectTerminal`, `DisconnectTerminal`, `ExecuteTerminal` | [`bin/periphery/src/api/terminal.rs`](`bin/periphery/src/api/terminal.rs`) |
| **金鑰輪替** | `RotatePrivateKey`, `RotateCorePublicKey` | [`bin/periphery/src/api/keys.rs`](`bin/periphery/src/api/keys.rs`) |

> **檔案操作備註**：Periphery **未提供通用檔案系統讀寫 API**，僅在 Build/Compose/Stack 模組提供特定 Dockerfile、Compose 與環境變數檔案之寫入/讀取。

---

### 5. 系統監控實作細節

1. **收集的指標清單**：
   - **CPU**：全域 CPU 使用百分比（`global_cpu_usage`）、1/5/15 分鐘系統附載（`load_average`）。
   - **記憶體**：實體總量 (`mem_total_gb`)、空閒 (`mem_free_gb`)、已用 (`mem_used_gb`)、緩存快取 (`mem_buff_cache_gb`)、ZFS ARC 快取大小 (`mem_zfs_arc_gb`)、Swap 總量與已用量。
   - **磁碟**：各分割區掛載點 (`mount`)、已用 GB (`used_gb`)、總容量 GB (`total_gb`)、檔案系統型態（自動過濾 `overlay`，支援 `include_disk_mounts` 與 `exclude_disk_mounts`）。
   - **網路**：全系統累計輸入位元組 (`network_ingress_bytes`) 與輸出位元組 (`network_egress_bytes`)。
   - **行程 (Processes)**：PID、執行檔名稱、路徑、完整啟動指令、啟動時間戳、CPU%、記憶體使用 MB、磁碟讀取/寫入 KB。
   - **主機靜態資訊**：作業系統名稱、核心版本、主機名稱、實體核心數、邏輯核心數、CPU 型號與架構。
   - **GPU 指標**：**無**（原始碼全文搜尋 `gpu`, `nvidia`, `cuda`，確認未採樣任何 GPU 數據）。
2. **使用的 crate**：
   - `sysinfo`（版本 0.39.6）
   - `async_timing_util`（版本 1.1.0）
3. **採樣方式**：
   - 背景定時輪詢：在 [`bin/periphery/src/stats/mod.rs:16-32`](`bin/periphery/src/stats/mod.rs#L16-L32`) 啟動 `spawn_polling_thread`，每隔 `stats_polling_rate` 週期自動呼叫 `refresh()` 並快取至記憶體中的全域 `stats_client`。
   - 記憶體採樣特化：依據 [`bin/periphery/src/stats/mem.rs:19-74`](`bin/periphery/src/stats/mem.rs#L19-L74`)，在 Linux 系統直接讀取 `/proc/meminfo` 與 `/proc/spl/kstat/zfs/arcstats` 解析，避免 ZFS ARC 導致記憶體誤報為耗盡；非 Linux 則降級使用 `sysinfo`。

---

### 6. 終端機／exec 功能實作

1. **PTY 核心 crate**：
   - 使用 `portable-pty`（版本 0.9.0）。
2. **執行機制**：
   - **主機終端（Server Terminal）**：呼叫 `native_pty_system().openpty()` 建立虛擬終端 master/slave，以 `shlex::split` 解析指令（預設為本機 shell），透過 `slave.spawn_command()` 啟動子行程（依據 [`bin/periphery/src/terminal.rs:256-279`](`bin/periphery/src/terminal.rs#L256-L279`)）。
   - **容器 Exec 終端**：不是透過 Docker API 的 hijack，而是直接在主機 PTY 中執行 `docker exec -it {container} {command}`（依據 [`bin/periphery/src/api/terminal.rs:128`](`bin/periphery/src/api/terminal.rs#L128`)）。
   - **容器 Attach 終端**：在主機 PTY 中執行 `docker attach {container} --sig-proxy=false`（依據 [`bin/periphery/src/api/terminal.rs:183`](`bin/periphery/src/api/terminal.rs#L183`)）。
3. **資料串流傳輸**：
   - 複用 Core 與 Periphery 既有的單一 WebSocket 連線，以 `TransportMessage::Terminal` 帶有 channel UUID 進行多工串流（依據 [`bin/periphery/src/connection/mod.rs:128`](`bin/periphery/src/connection/mod.rs#L128`)）。
   - 輸入：WebSocket 接收 `TerminalStdinMessage::Forward(bytes)` 或 `Resize(dimensions)`，透過 `tokio::sync::mpsc` 派給專門的 `spawn_blocking` 執行緒寫入 PTY writer。
   - 輸出：PTY reader 於獨立阻塞執行緒讀取後，將資料寫入 `tokio::sync::broadcast` 通道，同時存入 `History` 環形緩衝區供斷線重連補齊歷史輸出。

---

### 7. 命令執行：lib/command 的設計

1. **執行方式（Shell 字串 vs 參數陣列）**：
   - **`run_standard_command`**（依據 [`lib/command/src/lib.rs:125-144`](`lib/command/src/lib.rs#L125-L144`)）：接收單一字串，使用 `shlex::split(command)` 拆解出主程式與參數陣列後呼叫 `tokio::process::Command::new(&lexed[0]).args(&lexed[1..])`。
   - **`run_shell_command`**（依據 [`lib/command/src/lib.rs:168-177`](`lib/command/src/lib.rs#L168-L177`)）：接收單一字串，包裝為 `sh -c "<command>"` 執行。Shell 依存在性挑選 `/bin/bash`、`/usr/bin/bash`、`/bin/sh`、`/usr/bin/sh` 或 `"sh"`。
   - **`run_komodo_multiline_command`**：解析多行指令並以 `&&` 串接後交由 `run_shell_command`。
   - **參數陣列模式**：**無**（函式庫未暴露直接傳入 `&[&str]` 參數切片的 API，調用者一律傳送字串）。
2. **root／sudo 處理**：
   - **無**。`lib/command` 原始碼中完全沒有 `sudo`、`setuid`、`setgid` 或提權邏輯。執行權限完全繼承自 Periphery 程式啟動時的 OS 使用者權限。
3. **進程組 (Process Group) 與終止控制**：
   - 依據 [`lib/command/src/lib.rs:228-308`](`lib/command/src/lib.rs#L228-L308`)，當啟用 `timeout` 或 `cancel` token 時，設定 `cmd.process_group(0)`；逾時或取消時透過 `libc::kill(-(pid as libc::pid_t), libc::SIGKILL)` 確保連同衍生子行程（例如 shell 啟動的孫行程）一併銷毀。

---

### 8. 設定檔、環境變數與部署方式

1. **設定檔格式與位置**：
   - 格式：TOML 格式。
   - 原生安裝路徑：`/etc/komodo/periphery.config.toml`（若使用 `--user` 則為 `~/.config/komodo/periphery.config.toml`，依據 [`scripts/setup-periphery.py:84,95`](`scripts/setup-periphery.py#L84-L95`)）。
   - 容器內路徑：預設掛載於 `/config`，載入規則為比對包含 `config` 關鍵字之 TOML 檔案（依據 [`bin/periphery/aio.Dockerfile:33`](`bin/periphery/aio.Dockerfile#L33`)）。
   - 亦支援 CLI 參數 `-c, --config-path` 指定單一或多個路徑。
2. **環境變數**：
   - 依據 [`client/core/rs/src/entities/config/periphery.rs:105-250`](`client/core/rs/src/entities/config/periphery.rs#L105-L250`)，前綴為 `PERIPHERY_`：
     - `PERIPHERY_CORE_ADDRESS` / `PERIPHERY_CORE_ADDRESSES`：Core 伺服器出站位址。
     - `PERIPHERY_CONNECT_AS`：向 Core 註冊的主機名稱。
     - `PERIPHERY_PRIVATE_KEY` / `PERIPHERY_PRIVATE_KEY_FILE`：Periphery 私鑰。
     - `PERIPHERY_CORE_PUBLIC_KEYS`：受信任的 Core 公鑰清單。
     - `PERIPHERY_ONBOARDING_KEY`：自動註冊金鑰。
     - `PERIPHERY_PORT`（預設 8120）：入站監聽埠。
     - `PERIPHERY_ROOT_DIRECTORY`（預設 `/etc/komodo`）：工作檔案與目錄根路徑。
     - `PERIPHERY_ALLOWED_IPS`：入站白名單 IP。
     - `PERIPHERY_DISABLE_TERMINALS` / `PERIPHERY_DISABLE_CONTAINER_EXEC`：功能禁用開關。
3. **systemd 安裝腳本**：
   - 腳本位置：[`scripts/setup-periphery.py`](`scripts/setup-periphery.py#L1-L265`)
   - 支援自動下載 GitHub Release 二進位檔至 `/usr/local/bin/periphery`，產生服務檔 `/etc/systemd/system/periphery.service`，使用 `ExecStart=/bin/sh -lc "{bin_dir}/periphery --config-path {config_dir}/periphery.config.toml"` 並透過 `systemctl daemon-reload` 與 `systemctl start periphery` 啟動。
4. **Docker 部署方式**：
   - 範本位置：[`compose/periphery.compose.yaml`](`compose/periphery.compose.yaml#L8-L60`) 與 [`bin/periphery/aio.Dockerfile`](`bin/periphery/aio.Dockerfile#L1-L45`)。
   - 採用 `ghcr.io/moghtech/komodo-periphery:2` 映像，需將宿主機的 `/var/run/docker.sock`、`/proc`、金鑰 Volume 及工作目錄 `/etc/komodo` 掛載進容器。

---

### 9. 平台相依性：只支援 Linux 與 Windows 條件編譯

1. **明顯只支援 Linux 的部分**：
   - **記憶體精確計算**：[`bin/periphery/src/stats/mem.rs:20,47,76,91,141`](`bin/periphery/src/stats/mem.rs#L20`) 顯式標記 `#[cfg(target_os = "linux")]`，讀取 `/proc/meminfo` 及 `/proc/spl/kstat/zfs/arcstats`。
   - **Unix 進程組與信號**：[`lib/command/src/lib.rs:231,306`](`lib/command/src/lib.rs#L231`) 使用 Unix 專屬的 `cmd.process_group(0)` 與 `libc::kill`；[`lib/command/src/output.rs:3`](`lib/command/src/output.rs#L3`) 引用 `std::os::unix::process::ExitStatusExt`。
   - **主進程 Terminate 信號監聽**：[`bin/periphery/src/main.rs:108`](`bin/periphery/src/main.rs#L108`) 呼叫 `tokio::signal::unix::signal`，無法在 Windows 平台上編譯。
   - **Shell 跳脫字元處理**：多處模組引用 `shell_escape::unix::escape`（如 [`bin/periphery/src/helpers.rs:22`](`bin/periphery/src/helpers.rs#L22`)）。
   - **安裝系統**：`scripts/setup-periphery.py` 硬性要求 systemd (`/run/systemd/system/`)。
2. **有條件編譯 Windows 的部分**：
   - **完全沒有（未找到）**。全文檢索 `bin/periphery`、`lib/` 與 `client/`，沒有任何 `#[cfg(windows)]` 或 `#[cfg(target_os = "windows")]` 的實作。此 Agent 現狀為純 Unix / Linux 設計。

---

### 10. 授權與第三方相依合規分析

1. **專案本體授權**：
   - 依據根目錄 [`LICENSE`](`LICENSE`) 與 [`Cargo.toml:13`](`Cargo.toml#L13`)，Komodo 採用 **`GPL-3.0-or-later`**。
   - 包含 `komodo_periphery`、`komodo_client`、`periphery_client`、`command`、`interpolate`、`formatting`、`transport`、`encoding`、`git` 等 workspace 內部 crate，全部宣告為 `GPL-3.0-or-later`。
2. **商業改造合規風險（重要）**：
   - **強傳染性（Copyleft）**：若貴司以 `bin/periphery` 或其 workspace lib 作為起點修改並將編譯後二進位檔「分發（Distribute）」給客戶或外部伺服器使用，依 GPL-3.0 規定，貴司的 Rust 主機 Agent 原始碼必須以 GPL-3.0 授權向使用者開放。
3. **第三方相依授權分類**：
   - **MPL-2.0**（弱 Copyleft，共 16 個 crate）：`mogh_resolver`、`mogh_logger`、`mogh_pki`、`mogh_server`、`colored` 等。僅在修改這些 crate 原始碼本身時需開源，以函式庫引用時允許組合進專有作品。
   - **LGPL-2.1-or-later**：`r-efi` 帶有雙授權（MIT OR Apache-2.0 OR LGPL-2.1-or-later），可選用寬鬆授權。
   - **特殊密碼學授權**：`aws-lc-sys` / `aws-lc-rs` 帶有 ISC / OpenSSL / Apache-2.0 混合授權，需保留版權宣告。
   - **寬鬆授權（Permissive）**：MIT / Apache-2.0 / BSD / ISC 等（佔其餘 500+ 個相依套件）。

---

### 11. 模組重用、改造與捨棄建議

以對接自家 Go 控制中心的主機 Agent 為目標：

#### A. 可直接重用（3 個模組）
1. **[`lib/command`](`lib/command`)**
   - **理由**：指令執行架構健全，原生提供進程組隔離（`process_group(0)`）、`libc::kill` 徹底銷毀孫行程、逾時處理、取消 Token 與 stdin 管線輸入，適合作為主機 Agent 執行 Shell 指令的共用基礎。
2. **[`bin/periphery/src/terminal.rs`](`bin/periphery/src/terminal.rs`)**
   - **理由**：基於 `portable-pty` 封裝之 PTY 終端機邏輯完整，子行程生命週期管理、輸入 mpsc 與輸出 broadcast 通道設計良好，且自帶輸出歷史緩衝區，無論上層串流協議為 WebSocket 或 gRPC 皆可直接調用。
3. **[`bin/periphery/src/stats`](`bin/periphery/src/stats`)**
   - **理由**：主機效能監控採樣器已處理諸多 Linux 實務痛點（如讀取 `/proc/meminfo` 與扣除 ZFS ARC 避免記憶體誤報、過濾 overlay 磁碟等），採樣邏輯高度內聚，只需轉接自家的資料結構即可直接使用。

#### B. 需改造（3 個模組）
1. **[`bin/periphery/src/connection`](`bin/periphery/src/connection`)**
   - **理由**：現狀深度綁定 Komodo 專有的 `mogh_pki`（Noise 協定握手）與舊版 passkey 邏輯。由於控制中心是 Go，建議改造成業界標準的 gRPC + mTLS（或標準 WSS + JWT/Token 驗證），大幅簡化 Go 與 Rust 雙方的通訊實作難度。
2. **[`bin/periphery/src/api`](`bin/periphery/src/api`)**
   - **理由**：現狀透過 `mogh_resolver::Resolve` 分派，並高度依賴 `komodo_client::entities` 資料合約。需改為對接自家 Go 控制中心的 Protobuf 或 JSON-RPC 分派機制，並自定義回傳型別。
3. **[`bin/periphery/src/docker`](`bin/periphery/src/docker`)**
   - **理由**：程式碼中混用了 `bollard` 與命令列字串拼接（如 `docker stats --no-stream`、`docker compose ...`）。為提高穩定性與安全性，建議收斂全面改用 `bollard` 進行容器操作，並將 Compose 呼叫參數結構化。

#### C. 應捨棄（3 個模組）
1. **`bin/periphery/src/api/swarm` 與 `bin/periphery/src/docker/swarm.rs`（及 node/service/stack/task/config/secret 等）**
   - **理由**：Docker Swarm 在現代主機管理中需求較少，且佔據 Periphery 大量程式碼（多達 7 個檔案）。除非自家架構有明確 Swarm 需求，否則應全面拔除以大幅精簡 codebase 與編譯時間。
2. **[`bin/periphery/src/stack`](`bin/periphery/src/stack`)**
   - **理由**：此模組完全遵循 Komodo Core 的 Stack 命名結構與磁碟同步規範（如 `/etc/komodo/stacks/...`）。自家 Go 控制中心必然有專屬的部署配置或 GitOps 規則，保留此模組只會增加相依性包袱。
3. **[`bin/periphery/src/api/keys.rs`](`bin/periphery/src/api/keys.rs`) 與 `config.rs` 中的 Onboarding / Passkey 邏輯**
   - **理由**：這是 Komodo 專用的公私鑰派發、動態私鑰輪替與首次自動註冊流程。自建系統建議採用成熟的憑證管理（如 Vault 派發 mTLS 憑證或靜態 API Key），可直接捨棄這套客製化的 Noise 認證流程。

---

## 未驗證項目

1. **實際執行建置與二進位檔產出測試**：未執行 `cargo build -p komodo_periphery` 進行完整編譯測試，尚未驗證目前的原始碼在現有本機環境下編譯是否需要額外的系統 C 函式庫（例如 `openssl` 或 `aws-lc-sys` 之相依鏈結環境）。
2. **Go 與 `mogh_pki` 的互通性**：若不改造通訊層而硬要沿用 Komodo 原有之 Mutual Noise 握手協議，Go 端是否有相容的 SPKI / Noise Protocol 實作函式庫尚未進行驗證。

---

## 本輪經驗

- **觸發情境**：在唯讀環境下執行 `cargo tree` 或 `cargo metadata` 指令以分析依賴與授權。
- **失敗路徑**：直接執行 `cargo metadata` 會引發 cargo 重新解析依賴關係並自動微調 `Cargo.lock`（刪除部分多餘相依標記），造成工作目錄出現非預期的 `modified: Cargo.lock` 變更。
- **已確認原因**：本機工作區的 `Cargo.lock` 與 crates.io 最新的解析狀態存在極小差異，導致唯讀查詢指令意外寫入檔案。
- **有效做法與驗證證據**：分析完成後立即透過 `git restore Cargo.lock` 還原檔案，並以 `git status` 確認輸出 `nothing to commit, working tree clean`，嚴格落實不得更動任何檔案之規範。
