> 來源：henrygd/beszel（commit 826b52b，2026-11-05）唯讀分析，2026-11-07，由派工 agent（agy / gemini-3.8-flash-medium）產出，主 agent 核對過目錄結構與授權。報告是線索。

### 改了哪些檔案
- 無（本任務為唯讀程式碼分析，未修改、建立或刪除任何檔案）。

---

### 1. hub 與 agent 的職責與目錄結構

#### (1) Hub 與 Agent 的職責分工
- **Hub 職責**：
  - **集中控制與排程**：維護所有受監控系統清單（Systems），排程定時向 Agent 拉取指標（預設 60 秒輪詢）或接收反向連線（依據：`internal/hub/systems/system_manager.go:47` `SystemManager`、`internal/hub/systems/system.go:106` `StartUpdater`）。
  - **即時串流分發（Realtime Streaming）**：當使用者在前端 UI 檢視特定節點時，啟動 1 秒間隔的即時更新 Worker，並透過 PocketBase Realtime WebSocket 廣播至瀏覽器（依據：`internal/hub/systems/system_realtime.go:141` `startRealtimeWorker`）。
  - **資料持久化與歷史聚合**：基於 PocketBase（SQLite）儲存時序指標，定時執行階梯式 Rollup 彙總（1m → 10m → 20m → 120m → 480m）與過期資料清理（依據：`internal/hub/hub.go:143` `registerCronJobs`、`internal/records/records.go:42` `CreateLongerRecords`）。
  - **告警評估與通知發送**：評估指標閾值與系統狀態（CPU、記憶體、磁碟、狀態離線、容器、SMART、ZFS、網路探測），透過 Shoutrrr 或 SMTP 發送通知（依據：`internal/hub/hub.go:44` `hub.AlertManager`、`internal/alerts/alerts.go:22` `AlertManager`）。
  - **Web 管理介面與 API 伺服器**：嵌入靜態 Web UI 資產，提供 REST API、使用者認證與 OAuth 管理（依據：`internal/hub/server.go`、`internal/site/embed.go`、`internal/users/users.go`）。
- **Agent 職責**：
  - **輕量邊緣端採集器**：採集主機硬體指標（CPU、記憶體、Swap、磁碟 I/O、網路頻寬、溫度、風扇、負載、電池、WiFi、SMART、ZFS、Storage Pool、Systemd、容器統計及各廠牌 GPU 指標）（依據：`agent/agent.go:27` `Agent`、`agent/system.go`）。
  - **雙模式傳輸協定支援**：同時具備嵌入式 SSH Server（等待 Hub 主動連線）與 WebSocket Client（主動向 Hub 建立反向長連線）能力，具備自動容錯切換（依據：`agent/connection_manager.go:23` `ConnectionManager`）。
  - **採樣週期感知之快取與差值追蹤（Delta Tracking）**：隔離長週期常規採樣（60s）與高頻即時採樣（1s）的計數器狀態，避免即時監控干擾常規時序聚合（依據：`agent/agent_cache.go:20` `systemDataCache`、`agent/deltatracker/deltatracker.go`）。

#### (2) 目錄結構
- `internal/cmd/agent/`：Agent 主程式進入點（`main.go`、命令列參數與公開金鑰載入）。
- `internal/cmd/hub/`：Hub 主程式進入點（`main.go`、PocketBase 初始化與健康檢查）。
- `agent/`：Agent 核心邏輯與各硬體/服務採集器：
  - `agent.go`、`agent_cache.go`：Agent 主結構與快取管理。
  - `server.go`、`client.go`、`connection_manager.go`：SSH 伺服端、WebSocket 客戶端與連線狀態管理器。
  - `cpu*.go`、`memory*.go`、`disk*.go`、`network*.go`、`sensors*.go`、`system.go`：核心主機指標採集。
  - `gpu*.go`：各平台 GPU 收集器（含 NVML、AMD sysfs、Intel、Apple 等）。
  - `docker*.go`：Docker 與 Podman API 介面與統計計算。
  - `smart*.go`、`storage_pool.go`、`systemd*.go`、`uptime*.go`、`package_updates.go`：專屬子系統採集。
  - `battery/`、`btrfs/`、`deltatracker/`、`health/`、`lhm/`、`utils/`、`wifi/`、`zfs/`：Agent 專用子套件。
- `internal/hub/`：Hub 核心管理平面：
  - `hub.go`、`server.go`、`api.go`：Hub 實例初始化、伺服器啟動與 API 路由註冊。
  - `agent_connect.go`：處理來自 Agent 的反向 WebSocket 握手、金鑰質詢與指紋登記。
  - `systems/`：被監控節點生命週期管理、定時輪詢與即時數據訂閱（`system.go`, `system_manager.go`, `system_realtime.go`）。
  - `transport/`：SSH Transport 與 WebSocket Transport 通訊抽象層。
  - `ws/`：底層 WebSocket 連線與請求管理。
  - `config/`、`heartbeat/`、`expirymap/`：組態同步、心跳機制與 TTL 快取。
- `internal/alerts/`：告警引擎（閾值判定、防抖計時、安靜時段、SSRF 防護與 Shoutrrr / Mailer 通知）。
- `internal/records/`：指標時序紀錄建立、定時 Rollup 聚合計算與過期保留清理。
- `internal/entities/`：跨 Hub 與 Agent 的共用資料結構模型（`system/`, `container/`, `monitor/`, `smart/`, `systemd/`, `zfs/`）。
- `internal/common/`：傳輸協定封裝定義（CBOR 訊息格式、WebSocket Action 動作常數）。
- `internal/users/`：PocketBase 系統使用者與 OAuth 權限。
- `internal/migrations/`：PocketBase 資料表遷移快照與升級檔案。
- `internal/site/`：前端 Web UI 編譯產物嵌入（`embed.go`）。
- `internal/ghupdate/`：Agent 與 Hub 的自我更新模組。

---

### 2. hub 與 agent 之間的通訊方式、身分驗證、連線方向、重連策略

#### (1) 通訊方式（SSH / WebSocket 雙軌並存）
- **SSH 通道**：Hub 透過 SSH 連入 Agent，通訊封裝於 SSH Channel/Session，傳輸 CBOR 編碼的二進位資料（依據：`internal/hub/transport/ssh.go:37` `SSHTransport`、`agent/server.go:24` `ServerOptions`）。
- **WebSocket 通道**：Agent 主動連入 Hub 的 `/api/beszel/agent-connect`，升級為 WebSocket 後傳輸二進位 CBOR 訊息（依據：`internal/hub/agent_connect.go:53` `handleAgentConnect`、`agent/client.go:56` `WebSocketClient`）。

#### (2) 是誰主動連誰
- **WebSocket 模式（預設優先）**：由 **Agent 主動連向 Hub**。若 Agent 啟動時配置了 `HUB_URL`，Agent 會主動向 Hub 發起連線（適用於 Agent 位於 NAT/防火牆後或動態 IP 環境）（依據：`agent/connection_manager.go:171-187`、`agent/client.go:77` `newWebSocketClient`）。
- **SSH 模式**：由 **Hub 主動連向 Agent**。Hub 依據資料庫中登記的 `Host:Port` 連線至 Agent（適用於 Hub 可直連 Agent 的內部網路）（依據：`internal/hub/systems/system.go:739` `getSSHTransport`）。
- **動態優先權**：
  - **Agent 端**：若 WebSocket 連線成功，Agent 會主動停止本地 SSH Server 監聽；若 WebSocket 斷開，則立即啟動 SSH 監聽作為備援（依據：`agent/connection_manager.go:289-301`）。
  - **Hub 端**：更新指標時優先走現有已連線之 WebSocket（`sys.WsConn`）；若 WebSocket 逾時或斷開，自動切換為 SSH 連線重試（依據：`internal/hub/systems/system.go:694-713` `request`、`shouldFallbackToSSH`）。

#### (3) 身分驗證機制
- **SSH 身分驗證**：
  - 基於 **Ed25519 非對稱金鑰**。Hub 首次啟動時於資料目錄生成 `id_ed25519` 私鑰與公鑰（依據：`internal/hub/hub.go:161` `GetSSHKey`）。
  - Agent 在啟動時透過 CLI `--key`、環境變數 `KEY` 或 `KEY_FILE` 載入 Hub 的 Public Key（依據：`internal/cmd/agent/agent.go:148` `loadPublicKeys`）。
  - Agent 的內建 SSH Server 透過 `gliderlabs/ssh.PublicKeyHandler` 驗證 Hub 連線時提供的公鑰簽章（依據：`agent/server.go:77`）。
- **WebSocket 身分驗證（Token 質詢與公鑰簽名驗證）**：
  - Agent 連線至 Hub 時，在 HTTP Header 帶入 `X-Agent-Token`（或 URL query `token`）（依據：`agent/client.go:210` `getOptions`、`internal/hub/agent_connect.go:72`）。
  - 連線升級後，Hub 不僅驗證 Token，還會使用 Hub 自身的 SSH 私鑰對該 Token 產生數位簽章（`authRequest.Signature`），並發送質詢請求給 Agent（依據：`internal/hub/agent_connect.go:176`）。
  - Agent 收到質詢後，調用 `verifySignature` 使用本地配置的 Hub Public Key 驗證簽章真實性（依據：`agent/client.go:394` `verifySignature`）。確認 Hub 持有對應私鑰後，Agent 才回傳主機指紋（Fingerprint）建立信任關係（依據：`agent/client.go:357` `handleAuthChallenge`）。

#### (4) 重連策略
- **Agent 端重連策略**（`agent/connection_manager.go`）：
  - 設有 `wsTicker`，每 10 秒（`wsTickerInterval = 10 * time.Second`）週期性檢查並嘗試重連 WebSocket（依據：`agent/connection_manager.go:71`、`agent/connection_manager.go:197`）。
  - 連線限速保護：兩次連線嘗試之間強制要求至少 5 秒冷卻（`time.Since(lastConnectAttempt) < 5*time.Second`）（依據：`agent/connection_manager.go:327`）。
  - 狀態轉移：斷線時觸發 `handleStateChange(Disconnected)`，立即在背景重新啟動 SSH 監聽以接受連線，並重啟 `wsTicker` 與重連任務（依據：`agent/connection_manager.go:297-318`）。
- **Hub 端重連與逾時策略**（`internal/hub/systems/system.go`、`internal/hub/transport/ssh.go`）：
  - 輪詢間隔為 60 秒（`interval = 60_000`）。
  - 首次 WebSocket 連線啟動時注入隨機抖動 Jitter（0~60 秒隨機，`getJitter()`），防止所有 Agent 同時連線造成驚群效應（依據：`internal/hub/systems/system.go:115`、`1083`）。
  - SSH 連線逾時限制：連線超時 `sessionTimeout = 4 * time.Second`（`internal/hub/systems/system_manager.go:39`），握手逾時 `sshHandshakeTimeout = 10 * time.Second`（`internal/hub/transport/ssh.go:28`）。
  - WebSocket 斷線時透過 `downChan` 通知 Hub 將系統標記為 `down`，並於下個更新週期嘗試重連或改採 SSH fallback（依據：`internal/hub/systems/system.go:148`）。

#### (5) 程式位置
- Agent 連線狀態管理：`agent/connection_manager.go`
- Agent WebSocket 客戶端：`agent/client.go`
- Agent SSH 伺服端：`agent/server.go`
- Hub WebSocket 處理與驗證：`internal/hub/agent_connect.go`
- Hub 傳輸實作：`internal/hub/transport/ssh.go`、`internal/hub/transport/websocket.go`
- Hub 輪詢與連線調度：`internal/hub/systems/system.go`

---

### 3. agent 收集的全部指標清單、函式庫與採樣間隔

#### (1) 全部指標清單
依據 `internal/entities/system/system.go:42`（`Stats`）、`internal/entities/system/system.go:153`（`Info`）、`internal/entities/system/system.go:174`（`Details`）與 `internal/entities/container/container.go:30`（`ApiStats`）：
1. **CPU 指標**（`agent/cpu.go`, `agent/cpu_linux.go`）：
   - 整體 CPU 使用率（`cpu`，%）
   - CPU 核心負載細分（`cpub` Breakdown: User, System, Idle, Nice, IOWait, IRQ, SoftIRQ, Steal, Guest）
   - 個別 CPU 核心使用率（`cpus`，%）
   - 靜態硬體規格（型號 ModelName、實體核心 Cores、執行緒 Threads、時脈 Mhz、Sockets、快取 CacheL1/L2/L3）
2. **記憶體與 Swap 指標**（`agent/memory.go`, `agent/memory_linux.go`）：
   - 總量（Total）、已用量（Used）、使用率百分比（`mp`，MemPct）
   - Buffers / Cached 記憶體量（`mb`）
   - ZFS ARC 記憶體佔用量（`mz`，若啟用 ZFS）
   - Swap 總量、已用量、百分比
   - 支援自訂計算公式（透過環境變數 `MEM_CALC`）
3. **磁碟與檔案系統指標**（`agent/disk.go`, `agent/disk_io_linux_test.go`）：
   - 根目錄磁碟容量（Total、Used、使用率百分比 `dp`）
   - 額外掛載檔案系統（`efs` ExtraFs）：每個分區的 Total、Used、累計讀寫位元組（TotalRead, TotalWrite）
   - 磁碟 I/O 速率（`dio` DiskIO: [Read Bytes, Write Bytes] 差異統計）
   - 支援休眠磁碟保護（`DISK_USAGE_CACHE` 快取時長）
4. **網路與頻寬指標**（`agent/network.go`, `agent/network_counters_linux.go`, `agent/wifi/`）：
   - 系統整體頻寬（`b` Bandwidth: [Sent Bytes, Recv Bytes]）
   - 網卡等級詳細統計（`ni` NetworkInterfaces: [Upload Bytes, Download Bytes, Total Upload, Total Download]）
   - WiFi 狀態（`wf` WiFi: SSID、連線訊號原生 dBm）
5. **系統負載與運作資訊**（`agent/system.go`, `agent/uptime_linux.go`）：
   - 系統平均負載（`la` LoadAvg: 1 分鐘, 5 分鐘, 15 分鐘）
   - 系統開機時間（Uptime）、主機名稱（Hostname）、OS 名稱與核心版本
6. **溫度與風扇感測器**（`agent/sensors.go`, `agent/fans.go`）：
   - 溫度感測器矩陣（`t` Temperatures map: 各硬體名稱對應溫度 °C）
   - 主儀表板選定溫度（`DashboardTemp`）
   - 風扇轉速矩陣（Fans map: RPM）
7. **電池狀態**（`agent/battery/`）：
   - 電池電量百分比（Percent）、充電與交流電狀態（Charge state）、多電池各自電量（Batteries）
8. **容器統計（Docker / Podman）**（`agent/docker.go`, `internal/entities/container/container.go`）：
   - 容器 ID、名稱（支援自訂模板）、映像檔標籤、執行狀態（State/Status/Health）
   - 容器 CPU 使用率（%）
   - 容器記憶體用量（Memory Usage）與上限（Memory Limit）
   - 容器網路 I/O 速率（Upload / Download Bytes）
   - 映像檔更新狀態（檢查遠端 Registry 是否有新版本）
9. **擴充子系統統計**：
   - **GPU 數據**（`g` GPUData: 使用率、顯存、功耗、引擎負載等，詳見問題 4）
   - **Systemd 服務狀態**（`agent/systemd.go`）：服務 ActiveState、SubState、CPU、Memory
   - **SMART 磁碟健康**（`agent/smart.go`）：裝置型號、序號、健康狀態、警告計數
   - **ZFS 儲存池**（`agent/zfs/`）：Pool 容量、健康狀態、錯誤計數
   - **軟體套件更新**（`agent/package_updates.go`）：待更新套件清單、安全性更新標記
   - **網路端點探測**（`agent/network_monitor*.go`）：Ping RTT、Packet Loss、TLS 憑證剩餘天數

#### (2) 使用的函式庫
- `github.com/shirou/gopsutil/v4`：核心採集函式庫（廣泛用於 `cpu`、`mem`、`disk`、`net`、`host`、`load`、`sensors`）（依據：`go.mod:21`、`agent/system.go:22-25`、`agent/memory.go:7`、`agent/disk.go:16`、`agent/network.go:13`、`agent/sensors.go:21-22`）。
- **底層虛擬檔案系統直接讀取**（針對 Linux 繞過 gopsutil 性能限制）：
  - `/proc/stat`, `/proc/cpuinfo`（`agent/cpu_linux.go`）
  - `/proc/meminfo`（`agent/memory_linux.go`）
  - `/proc/diskstats`（`agent/disk.go`）
  - `/proc/net/dev`（`agent/network_counters_linux.go`）
  - `/sys/class/hwmon/`, `/sys/class/thermal/`（`agent/sensors.go`）
- `github.com/mdlayher/wifi`、`github.com/mdlayher/netlink`、`github.com/safchain/ethtool`：無線網卡與乙太網路介面資訊讀取（依據：`go.mod:13-15, 20`、`agent/wifi/wifi_linux.go`）。
- `github.com/coreos/go-systemd/v22`：Systemd D-Bus 連線介面（依據：`go.mod:7`、`agent/systemd.go`）。
- `howett.net/plist`：macOS plist 解析（依據：`go.mod:32`、`agent/battery/battery_darwin.go`）。

#### (3) 採樣間隔
- **後台常規採樣間隔**：**60 秒**（`interval = 60_000` ms = 1 分鐘）（依據：`internal/hub/systems/system_manager.go:35`、`internal/hub/systems/system.go:136`）。
- **前端即時監控採樣間隔（Realtime Mode）**：**1 秒**（`time.NewTicker(time.Second)`）（依據：`internal/hub/systems/system_realtime.go:143`）。
- **快取策略**：Agent 端 `systemDataCache` 快取有效時間為傳入的 `cacheTimeMs / 2`（允許 50% skew 防抖，例如 60 秒間隔在 30 秒內不重複採樣靜態項）（依據：`agent/agent_cache.go:39`）。
- **低頻專屬模組採樣間隔**：
  - SMART 健康狀態：預設每 1 小時（`internal/hub/systems/system.go:216`）
  - ZFS Pool 狀態：預設每 1 小時（`internal/hub/systems/system.go:230`）
  - 軟體套件更新檢查：預設每 4 小時（`agent/package_updates.go`）

---

### 4. GPU 指標：廠牌、取得方式、取得欄位與程式位置

#### (1) 支援廠牌
1. **NVIDIA**（包含一般獨立顯卡與 Jetson 嵌入式平台）
2. **AMD**（Radeon 獨立顯卡與 APU）
3. **Intel**（Intel 核心顯卡 Arc / Iris / HD Graphics）
4. **Apple Silicon**（macOS M 系列 GPU，代號 `collectorGroupApple`）
（依據：`agent/gpu.go:102-105`）

#### (2) 取得方式
- **NVIDIA**：
  - **NVML（純 Go 動態載入，優先推薦）**：支援 `amd64` Linux (glibc) 與 Windows，使用 `github.com/ebitengine/purego` 在執行期動態載入 `libnvidia-ml.so` 或 `nvml.dll`，**完全不需要 CGO 編譯**，直接調用 NVML API 抓取效能與記憶體（依據：`agent/gpu_nvml.go:1-30`、`agent/gpu_nvml_linux.go`、`agent/gpu_nvml_windows.go`）。
  - **CLI `nvidia-smi`**：若無法使用 NVML，則退回執行指令 `nvidia-smi --query-gpu=index,name,temperature.gpu,memory.used,memory.total,utilization.gpu,power.draw --format=csv,noheader,nounits` 解析 CSV 輸出（依據：`agent/gpu.go:24`、`agent/gpu.go:313` `parseNvidiaData`）。
  - **CLI `tegrastats`**：針對 NVIDIA Jetson 嵌入式主板，啟動 `tegrastats` 程序解析標準輸出字串（依據：`agent/gpu.go:26`、`agent/gpu.go:270` `parseTegrastatsData`）。
- **AMD**：
  - **Linux Sysfs（優先推薦）**：直接讀取 Linux 核心 `/sys/class/drm/card*/device/` 節點（包含 `gpu_busy_percent`, `mem_info_vram_used`, `mem_info_vram_total`, 及 hwmon 底下的 `power1_average` 或 `power1_input`），免除頻繁 fork 子程序的負擔（依據：`agent/gpu_amd_linux.go:40-75` `hasAmdSysfs`、`collectAmdStats`）。
  - **CLI `rocm-smi`**：若 Sysfs 不可用，則調用 `rocm-smi --showid --showtemp --showuse --showpower --showmeminfo vram --json` 解析 JSON（依據：`agent/gpu.go:25`、`agent/gpu.go:349` `parseAmdData`）。
- **Intel**：
  - **Linux Sysfs / Hwmon（優先）**：讀取 `/sys/class/drm/card*/device/` 與 hwmon 之 `energy1_input` 或 `power1_average` 換算即時功率（依據：`agent/gpu_intel_sysfs_linux.go:35` `hasIntelSysfs`、`collectIntelSysfsStats`）。
  - **CLI `intel_gpu_top`**：執行 `intel_gpu_top -J -s 3300` 獲取 JSON 串流，監控渲染與運算引擎狀態（依據：`agent/gpu_intel.go:17` `intelGpuStatsCmd`、`updateIntelFromStats`）。
- **Apple Silicon (macOS)**：
  - **CLI `macmon`（優先）**：執行 `macmon pipe -i 2500` 解析 JSON 格式輸出（依據：`agent/gpu_darwin.go:26`、`agent/gpu_darwin.go:172`）。
  - **CLI `powermetrics`**：執行 `powermetrics -i 500 -n 1 --samplers gpu_power` 解析文字（需 root/sudo 權限）（依據：`agent/gpu_darwin.go:22`、`agent/gpu_darwin.go:33`）。
- **跨平台多廠牌 CLI 工具**：
  - **CLI `nvtop`**：執行 `nvtop -b -i 3.3 -o json` 批次獲取多廠牌 GPU JSON 快照（依據：`agent/gpu_nvtop.go:27`、`agent/gpu_nvtop.go:44` `parseNvtopData`）。

#### (3) 取得欄位
依據 `internal/entities/system/system.go:115-125` 定義之 `type GPUData struct`：
- `Name`（JSON: `n`）：顯卡型號名稱。
- `Usage`（JSON: `u`）：GPU 核心利用率（%）。
- `MemoryUsed`（JSON: `mu`）：顯存已用量（MB）。
- `MemoryTotal`（JSON: `mt`）：顯存總量（MB）。
- `Power`（JSON: `p`）：GPU 核心功耗（W）。
- `PowerPkg`（JSON: `pp`）：整體封裝功耗（W，主要用於 Intel 核心顯卡）。
- `Engines`（JSON: `e`）：各硬體加速引擎負載矩陣（map[string]float64，如 Render/3D、Video、VideoEnhance 等，用於 Intel）。
- `Temperature`：核心溫度（°C，透過感測器或 GPU API 回報）。

#### (4) 程式位置
- GPU 主控管理器與 CLI 解析：`agent/gpu.go`
- NVIDIA NVML 免 CGO 動態載入：`agent/gpu_nvml.go`、`agent/gpu_nvml_linux.go`、`agent/gpu_nvml_windows.go`
- AMD Sysfs 採集：`agent/gpu_amd_linux.go`
- Intel 採集（Top & Sysfs）：`agent/gpu_intel.go`、`agent/gpu_intel_sysfs_linux.go`
- Apple macOS 採集：`agent/gpu_darwin.go`
- Nvtop 支援：`agent/gpu_nvtop.go`
- GPU 資料結構定義：`internal/entities/system/system.go`

---

### 5. Docker 容器統計如何取得

#### (1) 取得機制
- **直接調用 Docker Engine REST API**：Beszel **沒有引入官方肥大的 Docker SDK**，而是自行使用 Go 標準庫 `net/http.Client`，配合自訂的 `http.Transport` 與 `DialContext`，直接發送 HTTP 請求（依據：`agent/docker.go:57`、`agent/docker.go:656` `newDockerManager`）。
- **連線端點（透過環境變數 `DOCKER_HOST`）**：
  - Linux/macOS：Unix Domain Socket（預設 `/var/run/docker.sock`）（依據：`agent/docker.go:678-681`）。
  - Windows：Named Pipe（預設 `npipe:////./pipe/docker_engine`）。
  - 遠端主機：支援 TCP/HTTP/HTTPS（`agent/docker.go:682-685`）。
- **調用的 REST API 清單**：
  1. `GET http://localhost/containers/json`：列出所有執行中容器，取得 ID、Name、Image、Status、Ports 等基本資訊（依據：`agent/docker.go:143` `getDockerStats`）。
  2. `GET http://localhost/containers/{id}/stats?stream=false`（或單次串流）：抓取個別容器的資源用量快照（`ApiStats`）（依據：`agent/docker.go:275` `updateContainerStats`）。
  3. `GET http://localhost/version`：偵測 Docker 版本（是否 >= 25.0.0 支援最佳化單次 stats）並識別是否為 Podman 引擎（依據：`agent/docker.go:98` `dockerVersionResponse`）。
  4. `GET http://localhost/containers/{id}/logs?stdout=1&stderr=1&tail=200`：即時調閱容器日誌（依據：`agent/docker.go:46`、`agent/docker.go:375`）。
- **並發保護**：在查詢所有容器 stats 時，透過 Channel Semaphore（`dm.sem`）進行並發數量限制，避免打跨 Docker daemon（依據：`agent/docker.go:59`、`113-126` `queue/dequeue`）。

#### (2) 指標計算方式
依據 `internal/entities/container/container.go`：
- **CPU 使用率**：
  - Linux Docker（`CalculateCpuPercentLinux`）：依據公式 `(cpuDelta / systemDelta) * 100.0` 計算，其中 `cpuDelta` 為容器累計 CPU 時間差，`systemDelta` 為系統累計 CPU 時間差。
  - Podman（`CalculateCpuPercentPodman`）：由於 Podman 的 `system_cpu_usage` 僅代表 cgroup 配額而非整機容量，Beszel 改用壁鐘時間（`elapsedNs * online_cpus`）作為分母計算，解決 Podman CPU 百分比虛高問題。
- **記憶體用量**：取自 `memory_stats.usage`，並扣除快取（inactive_file / cache），得出實際 Working Set。
- **網路 I/O**：遍歷 `networks` map 累加各虛擬網卡的 `rx_bytes` 與 `tx_bytes`，經由 `deltatracker` 計算每秒頻寬速率。

#### (3) 程式位置
- Docker API 客戶端與輪詢調度：`agent/docker.go`
- 容器統計結構體與 CPU 計算演算法：`internal/entities/container/container.go`
- 映像檔更新探測：`agent/docker_image_updates.go`、`agent/docker_registry.go`

---

### 6. 資料儲存、彙總與保留政策

#### (1) 資料儲存
- **儲存引擎**：採用 **PocketBase**（`github.com/pocketbase/pocketbase`）作為整合後端，底層為純 Go 實作的嵌入式 **SQLite**（`modernc.org/sqlite`）（依據：`internal/cmd/hub/hub.go:15`、`go.mod:19, 81`）。
- **儲存位置**：預設儲存於 `beszel_data/data.db`（SQLite 資料庫檔案）（依據：`internal/cmd/hub/hub.go:43`）。
- **主要 Collections（資料表）**：
  - `systems`：納管主機清單、金鑰與目前狀態（up/down/paused/pending）。
  - `system_stats`：主機效能時序記錄（以 JSON 存放各項指標）。
  - `container_stats`：容器效能時序記錄。
  - `network_monitor_stats`：網路探測時序記錄。
  - `system_details`：主機靜態硬體與核心資訊。
  - `alerts`、`alerts_history`：告警規則與歷次觸發歷史。

#### (2) 彙總政策（Rollup）
- **排程頻率**：Hub 內建 Cron 任務**每 10 分鐘**執行一次 `CreateLongerRecords`（Cron 表達式：`*/10 * * * *`）（依據：`internal/hub/hub.go:147`）。
- **多層級彙總階梯**（依據：`internal/records/records.go:44-70` `longerRecordData`）：
  1. `1m` → `10m`：每 10 分鐘將最近 10 分鐘內至少 9 筆 `1m` 資料平均彙總為 1 筆 `10m` 記錄。
  2. `10m` → `20m`：每 20 分鐘將至少 2 筆 `10m` 資料平均彙總為 1 筆 `20m` 記錄。
  3. `20m` → `120m`（2 小時）：每 120 分鐘將至少 6 筆 `20m` 資料平均彙總為 1 筆 `120m` 記錄。
  4. `120m` → `480m`（8 小時）：每 480 分鐘將至少 4 筆 `120m` 資料平均彙總為 1 筆 `480m` 記錄。

#### (3) 保留政策（Retention Policy）
- **排程頻率**：Hub 內建 Cron 任務**每小時第 8 分鐘**執行一次 `DeleteOldRecords`（Cron 表達式：`8 * * * *`）（依據：`internal/hub/hub.go:145`）。
- **各階時序資料保留時間**（依據：`internal/records/records_deletion.go:69-75` `deleteOldSystemStats`，套用於 `system_stats`, `container_stats`, `network_monitor_stats`）：
  - **`1m` 原始記錄**：保留 **1 小時**（`time.Hour`）。
  - **`10m` 彙總記錄**：保留 **12 小時**（`12 * time.Hour`）。
  - **`20m` 彙總記錄**：保留 **24 小時（1 天）**（`24 * time.Hour`）。
  - **`120m` 彙總記錄**：保留 **7 天**（`7 * 24 * time.Hour`）。
  - **`480m` 彙總記錄**：保留 **30 天**（`30 * 24 * time.Hour`）。
- **非時序資料保留**：
  - **告警歷史（Alerts History）**：每位使用者最多保留最新 **200 筆**，當超過 250 筆時觸發自動清理修剪（依據：`internal/records/records_deletion.go:28`、`41-57` `deleteOldAlertsHistory`）。
  - **Systemd 服務記錄**：刪除超過 20 分鐘未更新之無效服務紀錄（依據：`internal/records/records_deletion.go:95` `deleteOldSystemdServiceRecords`）。
  - **免打擾時段（Quiet Hours）**：自動刪除已過期的單次靜音規則（依據：`internal/records/records_deletion.go:32` `deleteOldQuietHours`）。

---

### 7. 告警：種類、閾值、通知管道

#### (1) 告警種類
1. **主機資源指標告警**（`internal/alerts/alerts_system.go:64-116`）：
   - `CPU`：整體 CPU 使用率（%）。
   - `Memory`：記憶體使用率（%）。
   - `Disk`：磁碟分區或 ZFS 池的最大使用率（%）。
   - `Bandwidth`：整體網路頻寬用量（MB/s）。
   - `Temperature`：儀表板感測器溫度（°C）。
   - `LoadAvg1`、`LoadAvg5`、`LoadAvg15`：1 / 5 / 15 分鐘系統平均負載。
   - `GPU`：GPU 核心利用率（%）。
   - `Battery`：電池電量（%）。
   - `CPUIOWait`：CPU I/O 等待時間比例（%）。
   - `CPUSteal`：CPU 虛擬化 Steal 時間比例（%）。
2. **主機連線狀態告警**（`internal/alerts/alerts_status.go`）：
   - `Status`：主機由 up 變為 down（離線），或由 down 恢復為 up。
3. **Docker 容器狀態告警**（`internal/alerts/alerts_container.go`）：
   - `Container`：容器非預期退出、重啟或處於 unhealthy 狀態。
4. **Systemd 服務告警**（`internal/alerts/alerts_systemd.go`）：
   - `Systemd Failed`：被監控的特定 systemd service 進入 failed 狀態。
5. **硬碟健康 SMART 告警**（`internal/alerts/alerts_smart.go`）：
   - `SMART`：磁碟自我檢測回報 FAILED 或警告屬性增加。
6. **ZFS 儲存池告警**（`internal/alerts/alerts_zfs.go`）：
   - `ZFS Pool`：儲存池狀態變更為 DEGRADED 或 FAULTED。
7. **網路端點品質探測告警**（`internal/alerts/alerts_network_monitors.go`）：
   - `Packet Loss`：網路 Ping 探測丟包率超標。
   - `SSL/TLS Expiration`：網站 SSL/TLS 憑證即將到期（剩餘天數）。

#### (2) 閾值機制
- **數值閾值（Threshold）**：使用者設定之浮點數（例如 CPU > 85%、Disk > 90%、Bandwidth > 50 MB/s；Battery 則為反向邏輯：小於閾值如 < 20% 時觸發）（依據：`internal/alerts/alerts_system.go:121-135`）。
- **時間持續次數（`Min`）**：設定需**持續超標幾分鐘/幾次採樣**才正式發送告警（例如連續 5 分鐘超標），避免瞬時突波誤報（依據：`internal/alerts/alerts_system.go:137`、`150`）。
- **狀態判定（Binary/Enum State）**：針對非數值型告警（Status 離線、Container 異常、Systemd 失敗、SMART 損壞、ZFS 降級），只要狀態轉為異常且符合防抖條件即觸發。
- **免打擾時段（Quiet Hours）**：支援按使用者或指定主機設定特定時段靜音抑制（依據：`internal/alerts/alerts.go:147` `IsNotificationSilenced`）。

#### (3) 通知管道
- **電子郵件（Email）**：透過 PocketBase 內建的 SMTP Mailer 發送 HTML/Text 格式郵件（依據：`internal/alerts/alerts.go:14`、`internal/alerts/alerts.go:275` `sendEmailNotification`）。
- **Shoutrrr 整合多管道通知**（依據：`internal/alerts/alerts.go:11`、`internal/alerts/notification_client.go:15`）：
  - 整合 `github.com/nicholas-fedor/shoutrrr`，支援超過 15 種推送協議：
    - Discord, Telegram, Slack, Pushover, Gotify, Ntfy, Bark, Matrix, Microsoft Teams, Zulip, Pushbullet, OpsGenie, IFTTT, Lark。
    - 通用 Webhooks（`generic+https://...`）。
  - **SSRF 安全防禦機制**：特別客製化 `notificationClient` 與 `checkNotificationAddress`，禁止非管理者將告警 Webhook 發送至內網私有 IP、Loopback 或 Link-local 地址（依據：`internal/alerts/notification_client.go:43`）。

---

### 8. 授權（LICENSE）與主要依賴

#### (1) 授權條款
- **MIT License**（Copyright (c) 2024 henrygd，見專案根目錄 `LICENSE`）。極度寬鬆，商業與內部使用皆友善。

#### (2) 主要依賴套件（見 `go.mod`）
- **核心後端與資料庫**：
  - `github.com/pocketbase/pocketbase v0.40.6`：提供 Hub Web 伺服器、REST API、管理者認證與嵌入式資料庫框架。
  - `modernc.org/sqlite v1.60.1`：純 Go 實作的 SQLite 驅動，無須 CGO 即可運作。
  - `github.com/pocketbase/dbx v1.12.0`：SQL 查詢產生器與 ORM 層。
- **主機與系統監控**：
  - `github.com/shirou/gopsutil/v4 v4.26.9`：跨平台硬體指標採集核心庫（CPU, Mem, Disk, Net, Host, Load, Sensors）。
  - `github.com/ebitengine/purego v0.11.2`：在 Linux/Windows amd64 平台動態加載 NVIDIA NVML 共享庫（免 CGO）。
  - `github.com/coreos/go-systemd/v22 v22.7.0`：Systemd D-Bus 通訊。
  - `github.com/mdlayher/wifi v0.9.0`、`github.com/mdlayher/netlink v1.11.2`、`github.com/safchain/ethtool v0.7.0`：網路底層介面與 WiFi 狀態讀取。
- **通訊與傳輸協定**：
  - `github.com/gliderlabs/ssh v0.3.8` 與 `golang.org/x/crypto v0.58.0`：Agent 端的嵌入式 SSH 伺服器與非對稱金鑰加解密驗證。
  - `github.com/lxzan/gws v1.10.2`：高效能純 Go WebSocket 客戶端與伺服端庫。
  - `github.com/fxamacker/cbor/v2 v2.10.0`：CBOR 二進位序列化，具備極高解碼效能與低網路頻寬消耗。
- **告警通知與工具**：
  - `github.com/nicholas-fedor/shoutrrr v0.22.1`：跨平台通知與 Webhook 發送引擎。
  - `github.com/spf13/cobra v1.10.2` 與 `github.com/spf13/pflag v1.0.10`：CLI 命令列解析。
  - `github.com/blang/semver v3.5.1`：跨版本協定相容性語意化版本比對。

---

### 9. 值得我們借鏡的三個設計與三個限制

#### (1) 值得借鏡的三個設計
1. **雙軌通道與零信任密碼學質詢（Dual-Transport with Zero-Trust Challenge Handshake）**：
   - **設計細節**：多數監控系統（如 Prometheus 預設僅能 pull，或 Datadog 僅能 push）架構單一。Beszel 巧妙結合了 SSH（Hub 主動 pull）與 WebSocket（Agent 主動 push 穿透 NAT），並在連線狀態管理器（`ConnectionManager`）中做到平滑故障轉移。
   - **驗證亮點**：在 WebSocket 反向連線中，避免單純明文 Token 洩漏風險，設計了以 Hub SSH 私鑰對 Token 簽名的質詢機制（`agent/client.go:394`），Agent 驗證 Hub 的 Public Key 通過後才信任 Hub。這套兼顧穿透彈性與相互驗證的架構極適合作為邊緣節點管理的參考。
2. **輕量級二進位協議（CBOR Integer Keys）與多採樣間隔感知差值追蹤（DeltaTracker）**：
   - **設計細節**：通訊協議全面捨棄肥大的 JSON，採用 `cbor:"...,keyasint"`（如 `Stats` 欄位以整數代表鍵名），傳輸體積與序列化 CPU 耗能極小。
   - **快取隔離亮點**：系統需同時支援後台 60 秒定期彙總與前端 1 秒即時監控。若使用共用計數器，1 秒的高頻讀取會洗掉 60 秒的差值基準。Beszel 設計了以 `cacheTimeMs` 為鍵的 `DeltaTracker`（`agent/deltatracker/`）與 `systemDataCache`，使即時檢視與長週期聚合互不干擾，架構十分優雅。
3. **純 Go 免 CGO 動態載入硬體庫（PureGo NVML & Direct Sysfs Probing）**：
   - **設計細節**：在 Go 語言中採集 GPU/硬體指標通常需依賴 CGO 綁定（導致跨平台交叉編譯困難）或呼叫命令列（造成大量 fork 開銷）。
   - **實踐亮點**：Beszel 對於 NVIDIA 使用 `ebitengine/purego` 在執行期動態加載 `libnvidia-ml.so` / `nvml.dll`，在維持單一靜態二進位檔編譯的同時享有 C 函式庫的速度；對 AMD 與 Intel 則優先直接解析 `/sys/class/drm/` 與 `/sys/class/hwmon/` 核心虛擬檔案，徹底兼顧了免 CGO 便捷性與極致的系統效能。

#### (2) 三個架構限制與取捨
1. **單機嵌入式 SQLite 後端帶來的橫向擴展與高併發寫入瓶頸**：
   - **限制細節**：Hub 完全建構在 PocketBase 內嵌的 SQLite 上。SQLite 本質上是單一寫入鎖（Write Lock），當管理規模擴大（例如數百到數千台主機同時回報，或多人同時開啟 1 秒即時監控），資料庫鎖定競爭（Lock Contention）與 I/O 寫入延遲會迅速升高。
   - **架構影響**：無法直接做到多 Hub 負載平衡、讀寫分離或水平擴展，若要擴充到企業級叢集規模，必須將儲存層重構成 ClickHouse、VictoriaMetrics 或 PostgreSQL 等分散式時序資料庫。
2. **Pull 驅動的定時輪詢架構缺乏邊緣端離線緩衝（No Edge Spooling / WAL）**：
   - **限制細節**：指標收集主要由 Hub 每 60 秒主動拉取（Request-Response），Agent 本地僅維護最新一次快照快取（`systemDataCache`），並無磁碟 WAL（Write-Ahead Log）或本地隊列暫存。
   - **架構影響**：一旦網路發生短暫斷線、NAT 抖動或 Hub 重新啟動，斷線期間的歷史指標即告永久遺失，無法在連線恢復後自動回補（Backfill）該時段的時序數據。
3. **異質硬體採集依賴 Shell CLI 與外部 Process 刮取，健壯性受限（External CLI Scraping Fallback）**：
   - **限制細節**：在非 x86、macOS、舊款 AMD 或 ARM Jetson 環境中，指標採集退回使用背景外部程序（`rocm-smi`、`intel_gpu_top`、`powermetrics`、`macmon`、`nvtop`、`tegrastats`）並依賴正則表達式解析 stdout。
   - **架構影響**：這些子程序容易受到第三方工具未安裝、系統套件版本輸出格式變更、權限不足（例如 macOS powermetrics 需 sudo）、以及程序異常崩潰或成為殭屍程序的影響，維護成本較高且容錯能力較原生核心介面脆弱。

---

## 未驗證項目
1. **Windows 平台上的 Named Pipe 與 LHM 感測器實際運作效果**：本分析完全依據程式碼靜態研讀（`agent/sensors_windows.go`、`agent/docker.go:680`），因目前環境為 macOS，未於實際 Windows 主機上運行驗證 LibreHardwareMonitor (LHM) 與 Windows Docker Named Pipe。
2. **大於 1,000 台節點時 SQLite 的具體併發寫入極限數值**：依據程式架構分析可推斷 SQLite 鎖競爭，但未進行大規模壓力測試以量化確切的崩潰臨界值。

---

## 本輪經驗
- **無**（本輪為針對既有 Go 程式碼庫之靜態架構與實作檔案研讀分析，流程順暢，未觸發反覆試錯情境）。
