# 功能覆蓋矩陣（2026-11-07）

目標：HazelminX 要能完全取代 Portainer、1Panel、Webmin。逐項對：那邊的每個功能 → 我們哪個畫板／流程 → 狀態。狀態：**有畫面**＝畫板上有對應區塊；**摘要**＝只有摘要卡，細部設定畫面未畫；**未畫**＝規格有、畫面沒有；**規格外**＝規格沒要求，要你決定。資料來源：票 092 的 1Panel v2.4.1 路由清單（108 條前端、793 條 API）、`docs/research/portainer-ui.md`、Webmin 2.x 模組清單（依記憶整理，待以原始碼核對）。

## 一、1Panel v2.4.1 前端路由（108 條）→ 畫板

完整對照在 `coverage-1panel-frontend.csv`。

| 畫板 | 路由數 |
|---|---|
| Containers | 12 |
| Database | 12 |
| Settings、IAM | 9 |
| Nodes、System | 8 |
| Overview | 7 |
| Logs | 7 |
| Websites（運作環境卡；PHP 擴充與 Supervisor 細部未畫） | 6 |
| AI | 5 |
| Cron | 5 |
| AppStore | 4 |
| Nodes（Port 矩陣）、Security | 4 |
| Websites | 4 |
| NodeDetail | 3 |
| Nodes（SSH 矩陣）、System | 3 |
| NodeDetail（GPU） | 2 |
| AppStore、AppInstall | 2 |
| Nodes（程序排行） | 2 |
| Settings、IAM、Events、Storage | 2 |
| Terminal | 2 |
| Nodes（主機工具卡）、System | 2 |
| 未畫（登入頁） | 1 |
| 未畫：錯誤頁（States 有樣式） | 1 |
| Nodes | 1 |
| Files | 1 |
| Storage、System | 1 |
| Websites（憑證橫條；CA／ACME／DNS 帳號表未畫） | 1 |
| Websites（範本卡） | 1 |

未畫：登入頁 1 條、錯誤頁 1 條（States 有錯誤樣式，獨立頁在票 017）。商業版（xpack）路由不在基線。

## 二、1Panel API 路由群組（793 條）→ 畫板

| 群組 | 條數 | 畫板 | 細部缺口 |
|---|---|---|---|
| `ai` | 106 | AI、NodeDetail（GPU） | MCP 設定直編、Agent 模型池、TensorRT 未畫 |
| `hosts` | 76 | Nodes、System、NodeDetail、Terminal | 防火牆規則編輯、磁碟分割／掛載操作流程、行程網路連線分頁未畫 |
| `containers` | 73 | Containers、Projects | 容器建立表單、映像建置（Dockerfile）、Swarm 未畫 |
| `websites` | 68 | Websites | 站點建立精靈、反向代理／負載／重寫編輯表單未畫（設定矩陣只列現況） |
| `databases` | 63 | Database | 資料列編輯、SQL 主控台、Redis CLI、遠端連線表單未畫 |
| `files` | 57 | Files | wget 遠端下載對話框、壓縮選項、編碼轉換未畫 |
| `settings` | 52 | Settings、IAM、Events、Storage | 備份帳號表單（S3 等供應商欄位）、快照還原流程未畫 |
| `toolbox` | 39 | Nodes（主機工具）、System | 各工具的設定表單未畫（只有現況卡） |
| `apps` | 31 | AppStore、AppInstall | — |
| `runtimes` | 31 | Websites（運作環境） | PHP 擴充管理、Node 模組管理未畫 |
| `backups` | 26 | Storage | — |
| `auth` | 25 | IAM、未畫：登入頁 | 登入／二次驗證頁未畫 |
| `alert` | 16 | Events | — |
| `cronjobs` | 16 | Cron | 任務建立表單未畫 |
| `dashboard` | 16 | Overview | — |
| `websites/ssl` | 14 | Websites（憑證） | CA／ACME／DNS 帳號表未畫 |
| `websites/templates` | 11 | Websites（範本） | 範本編輯未畫 |
| `logs` | 10 | Logs | — |
| `openresty` | 10 | Websites（入口元件：Zentinel 取代 OpenResty） | Zentinel 設定頁未畫（規格 19 章） |
| `commands` | 9 | Terminal（快捷指令） | — |
| `groups` | 8 | Nodes、Websites（群組／標籤） | 群組管理對話框未畫 |
| `websites/ca` | 7 | Websites（憑證：CA 表未畫） | 未畫 |
| `hosts/diagnostics` | 6 | NodeDetail、System | 效能診斷報告未畫 |
| `script` | 6 | Cron（腳本庫） | — |
| `process` | 4 | Nodes（程序） | — |
| `websites/acme` | 4 | Websites（憑證：ACME 帳號表未畫） | 未畫 |
| `websites/dns` | 4 | Websites（憑證：DNS 帳號表未畫） | 未畫 |
| `/share` | 3 | Files（分享連結） | 分享連結管理表未畫 |
| `（檔案層）` | 2 | — | — |

## 三、Portainer CE → 畫板

| Portainer 功能 | 畫板 | 狀態 |
|---|---|---|
| Home 環境列表（狀態、群組、容器數、CPU／記憶體） | Nodes（艦隊卡） | 有畫面 |
| Environment dashboard（Stacks／Services／Containers／Images／Volumes／Networks／GPU 數字卡） | Containers（KPI 格、映像段條、網路／儲存卷矩陣） | 有畫面 |
| Containers 列表＋Quick actions（logs／inspect／stats／console／attach） | Containers（列尾動作） | 有畫面；inspect、attach 在「更多」 |
| Container Stats（CPU／記憶體／網路／IO 圖） | Containers（熱格、排行）、容器詳情頁 | 未畫：容器詳情頁（票 040） |
| Images（pull／build／import／export／tag／prune） | Containers（映像段條、清理） | 摘要：build／tag 對話框未畫 |
| Volumes、Networks | Containers（矩陣） | 摘要：建立表單未畫 |
| Stacks（Git 來源、自動更新、webhook、GitReferenceCard） | Projects、GitHubSource、DeployCheck | 有畫面 |
| Services／Swarm | — | 未畫（規格票 043） |
| Events | Events、Logs | 有畫面 |
| Host／Nodes 詳情 | NodeDetail、System | 有畫面 |
| Kubernetes（dashboard、namespaces、applications、cluster） | Kubernetes | 有畫面；Namespace 配額頁未畫 |
| Users／Teams／RBAC roles | IAM | 有畫面 |
| Registries | Containers（登錄站矩陣） | 摘要 |
| Settings、Notifications | Settings、Events | 有畫面 |
| Edge agents／Edge jobs | — | 規格外（Business／Edge 功能） |

## 四、Webmin 2.x 核心模組 → 畫板（模組清單待以原始碼核對）

| Webmin 模組 | 畫板 | 狀態 |
|---|---|---|
| Software Packages（apt／dnf） | System（套件與更新） | 有畫面 |
| Bootup and Shutdown（systemd） | System（服務與計時器） | 有畫面 |
| Running Processes | Nodes（程序排行） | 摘要：行程樹、kill 對話框未畫 |
| Scheduled Cron Jobs、Scheduled Commands | Cron | 有畫面 |
| Users and Groups | System（使用者與 sudo） | 有畫面；建立表單未畫 |
| Change Passwords | System（root 密碼卡） | 摘要 |
| System Logs（journald） | Logs | 有畫面 |
| Log File Rotation | HostServices | 納入（增補 01、票 105）；編輯表單未畫 |
| Disk and Network Filesystems（fstab、NFS／SMB 掛載） | System（掛載）、Storage | 有畫面；掛載操作流程未畫（票 031） |
| Partitions、LVM、RAID、LUKS | Storage | 摘要：操作流程未畫（票 031） |
| Disk Quotas | HostServices | 納入（票 105） |
| Filesystem Backup | Storage（備份任務） | 有畫面 |
| Network Configuration（介面、路由、DNS、hosts） | System（網路） | 有畫面；編輯表單未畫 |
| Linux Firewall（iptables／nftables） | Nodes（Port 矩陣）、Security | 摘要：規則編輯未畫 |
| SSH Server、SSH login | Nodes（SSH 矩陣）、System | 有畫面 |
| System Time（NTP、時區） | System（時間） | 有畫面 |
| File Manager、Upload and Download | Files | 有畫面 |
| Command Shell／Terminal | Terminal | 有畫面 |
| Custom Commands | Terminal（快捷指令） | 有畫面 |
| Webmin Users／ACL、Webmin Configuration | IAM、Settings | 有畫面 |
| System and Server Status | Overview、NodeDetail | 有畫面 |
| Apache／Nginx config | HostServices | 納入（票 104）；與 Zentinel 分工見增補 01 |
| BIND DNS Server | HostServices（zone、紀錄、套用流程、版本） | 納入（票 101）；有畫面 |
| DHCP Server | HostServices | 納入（票 101）；租約表未畫 |
| Postfix／Dovecot／Sendmail | HostServices | 納入（票 102）；佇列頁未畫 |
| Samba、NFS Exports（伺服器端） | HostServices | 納入（票 103） |
| ProFTPD | Nodes（FTP／SFTP 卡） | 摘要 |
| MySQL／PostgreSQL Database Server | Database | 有畫面 |
| PAM、MIME、Printer | HostServices | 納入（票 106）；Perl modules 不納入 |

## 五、已決定的範圍

2026-11-07 使用者決定納入 Webmin 九類規格外模組。規格增補 `docs/spec/addendum-01-host-service-modules.md`、票 100–106（M6）、畫板 HostServices。

## 六、下一批要畫的細部畫面（依矩陣優先序）

1. 建立／編輯表單：容器、站點（含反向代理、重寫）、計畫任務、資料庫帳號、備份帳號（S3 等）、防火牆規則、網路介面、Linux 使用者。
2. 容器詳情頁（統計圖、inspect、attach）。
3. 登入與二次驗證頁、錯誤頁。
4. 磁碟操作流程（分割、格式化、掛載、LVM／RAID／LUKS）。
5. Zentinel 入口設定頁、憑證帳號表（CA／ACME／DNS）。
6. 應用商店的升級 diff 對話框、Port 衝突對話框、忽略版本。
