> 來源：1Panel v2.4.1（commit f9f9169）frontend/ 唯讀盤點，2026-11-07，由派工 agent（agy / gemini-3.8-flash-medium）產出，主 agent 核對過路由模組清單與選單結構。這份清單是票 092 基線盤點的前端輸入；報告是線索。

這是一份針對 **1Panel OSS v2.4.1** 前端程式碼（`frontend/`，Vue 3 + Vite）的功能基線盤點與架構分析報告，旨在作為後續使用 React 進行全新介面重構時的完整 OSS 規格基線。

---

# 1. 路由盤點

依據 [frontend/src/routers/router.ts](`frontend/src/routers/router.ts`) 與 [frontend/src/routers/modules/](`frontend/src/routers/modules/`) 模組定義，所有路由模組與視圖元件對應如下：

### 靜態與系統基礎路由 ([frontend/src/routers/router.ts](`frontend/src/routers/router.ts#L12-L99`))
| 路由路徑 (Path) | 路由名稱 (Name) | 對應 View 元件路徑 | 子路由 / 隱藏路由屬性 |
| :--- | :--- | :--- | :--- |
| `/` | `Home-Menu` | `Layout` ([frontend/src/routers/constant.ts](`frontend/src/routers/constant.ts#L1`)) | 頂層重定向至 `/` |
| `/` | `home` | `@/views/home/index.vue` | `Home-Menu` 之子路由，非隱藏 |
| `/login` | `login` | `@/views/login/index.vue` | 獨立頂層路由，非選單路由 |
| `/enterprise/license-required` | `EnterpriseLicenseRequired` | `@/views/setting/license-required/index.vue` | 獨立頂層路由（商業版授權攔截頁） |
| `/s/:code` | `file-share` | `@/views/share/index.vue` | 獨立頂層公開檔案分享頁 |
| `/:code?` | `entrance` | `@/views/login/index.vue` | 獨立頂層安全入口登入頁 |
| `/:pathMatch(.*)` | - | 重定向至 `404` | 通配錯誤捕捉 |

---

### 各模組路由表（`frontend/src/routers/modules/*.ts`）

#### 1. AI 模組 ([frontend/src/routers/modules/ai.ts](`frontend/src/routers/modules/ai.ts`))
| 路由路徑 (Path) | 路由名稱 (Name) | 對應 View 元件路徑 | 子路由 / 隱藏狀態 |
| :--- | :--- | :--- | :--- |
| `/ai` | `AI-Menu` | `Layout` | 頂層父容器（`sort: 4`） |
| `/ai/agents/agent` | `Agents` | `@/views/ai/agents/agent/index.vue` | 子路由，顯示於側邊欄 |
| `/ai/model/account` | `AIModel` | `@/views/ai/model/index.vue` | 子路由，顯示於側邊欄 |
| `/ai/model/local` | `LocalModel` | `@/views/ai/model/index.vue` | 子路由，`hidden: true`（隱藏） |
| `/ai/mcp` | `MCPServer` | `@/views/ai/mcp/server/index.vue` | 子路由，顯示於側邊欄 |
| `/ai/gpu/current` | `GPU` | `@/views/ai/gpu/current/index.vue` | 子路由，顯示於側邊欄 |
| `/ai/gpu/history` | `GPUHistory` | `@/views/ai/gpu/history/index.vue` | 子路由，`hidden: true`（隱藏） |

#### 2. 應用商店模組 ([frontend/src/routers/modules/app-store.ts](`frontend/src/routers/modules/app-store.ts`))
| 路由路徑 (Path) | 路由名稱 (Name) | 對應 View 元件路徑 | 子路由 / 隱藏狀態 |
| :--- | :--- | :--- | :--- |
| `/apps` | `App-Menu` | `Layout` | 頂層父容器（`sort: 2`） |
| `/apps` | `App` | `@/views/app-store/index.vue` | 一級子路由，側邊欄單一項目 |
| `all` | `AppAll` | `@/views/app-store/apps/index.vue` | 二級子路由，`hidden: true` |
| `installed` | `AppInstalled` | `@/views/app-store/installed/index.vue` | 二級子路由，`hidden: true` |
| `upgrade` | `AppUpgrade` | `@/views/app-store/installed/index.vue` | 二級子路由，`hidden: true` |
| `setting` | `AppStoreSetting` | `@/views/app-store/setting/index.vue` | 二級子路由，`hidden: true` |

#### 3. 網站模組 ([frontend/src/routers/modules/website.ts](`frontend/src/routers/modules/website.ts`))
| 路由路徑 (Path) | 路由名稱 (Name) | 對應 View 元件路徑 | 子路由 / 隱藏狀態 |
| :--- | :--- | :--- | :--- |
| `/websites` | `Website-Menu` | `Layout` | 頂層父容器（`sort: 3`） |
| `/websites` | `Website` | `@/views/website/website/index.vue` | 子路由，顯示於側邊欄 |
| `/websites/:id/config/:tab` | `WebsiteConfig` | `@/views/website/website/config/index.vue` | 子路由，`hidden: true` |
| `/websites/ssl` | `SSL` | `@/views/website/ssl/index.vue` | 子路由，顯示於側邊欄 |
| `/websites/templates` | `WebsiteTemplate` | `@/views/website/template/index.vue` | 子路由，顯示於側邊欄 |
| `/websites/runtimes/php` | `PHP` | `@/views/website/runtime/php/index.vue` | 子路由，顯示於側邊欄（標題為 Runtime） |
| `/websites/runtimes/node` | `node` | `@/views/website/runtime/node/index.vue` | 子路由，`hidden: true` |
| `/websites/runtimes/java` | `java` | `@/views/website/runtime/java/index.vue` | 子路由，`hidden: true` |
| `/websites/runtimes/go` | `go` | `@/views/website/runtime/go/index.vue` | 子路由，`hidden: true` |
| `/websites/runtimes/python` | `python` | `@/views/website/runtime/python/index.vue` | 子路由，`hidden: true` |
| `/websites/runtimes/dotnet` | `dotNet` | `@/views/website/runtime/dotnet/index.vue` | 子路由，`hidden: true` |

#### 4. 資料庫模組 ([frontend/src/routers/modules/database.ts](`frontend/src/routers/modules/database.ts`))
| 路由路徑 (Path) | 路由名稱 (Name) | 對應 View 元件路徑 | 子路由 / 隱藏狀態 |
| :--- | :--- | :--- | :--- |
| `/databases` | `Database-Menu` | `Layout` | 頂層父容器（`sort: 5`） |
| `/databases` | `Database` | `@/views/database/index.vue` | 一級子路由，側邊欄單一項目 |
| `mysql` | `MySQL` | `@/views/database/mysql/index.vue` | 二級子路由，`hidden: true` |
| `mysql/setting/:type/:database` | `MySQL-Setting` | `@/views/database/mysql/setting/index.vue` | 二級子路由，`hidden: true` |
| `mysql/remote` | `MySQL-Remote` | `@/views/database/mysql/remote/index.vue` | 二級子路由，`hidden: true` |
| `postgresql` | `PostgreSQL` | `@/views/database/postgresql/index.vue` | 二級子路由，`hidden: true` |
| `postgresql/remote` | `PostgreSQL-Remote` | `@/views/database/postgresql/remote/index.vue` | 二級子路由，`hidden: true` |
| `postgresql/setting/:type/:database` | `PostgreSQL-Setting` | `@/views/database/postgresql/setting/index.vue` | 二級子路由，`hidden: true` |
| `redis` | `Redis` | `@/views/database/redis/index.vue` | 二級子路由，`hidden: true` |
| `redis/remote` | `Redis-Remote` | `@/views/database/redis/remote/index.vue` | 二級子路由，`hidden: true` |
| `mongodb` | `MongoDB` | `@/views/database/mongodb/index.vue` | 二級子路由，`hidden: true` |
| `mongodb/remote` | `MongoDB-Remote` | `@/views/database/mongodb/remote/index.vue` | 二級子路由，`hidden: true` |

#### 5. 容器模組 ([frontend/src/routers/modules/container.ts](`frontend/src/routers/modules/container.ts`))
| 路由路徑 (Path) | 路由名稱 (Name) | 對應 View 元件路徑 | 子路由 / 隱藏狀態 |
| :--- | :--- | :--- | :--- |
| `/containers` | `Container-Menu` | `Layout` | 頂層父容器（`sort: 6`） |
| `/containers` | `Container` | `@/views/container/index.vue` | 一級子路由，側邊欄單一項目 |
| `dashboard` | `ContainerDashboard` | `@/views/container/dashboard/index.vue` | 二級子路由，`hidden: true` |
| `container` | `ContainerItem` | `@/views/container/container/index.vue` | 二級子路由，`hidden: true` |
| `container/operate` | `ContainerCreate` | `@/views/container/container/operate/index.vue` | 二級子路由，`hidden: true` |
| `image` | `Image` | `@/views/container/image/index.vue` | 二級子路由，`hidden: true` |
| `network` | `Network` | `@/views/container/network/index.vue` | 二級子路由，`hidden: true` |
| `volume` | `Volume` | `@/views/container/volume/index.vue` | 二級子路由，`hidden: true` |
| `repo` | `Repo` | `@/views/container/repo/index.vue` | 二級子路由，`hidden: true` |
| `compose` | `Compose` | `@/views/container/compose/index.vue` | 二級子路由，`hidden: true` |
| `template` | `ComposeTemplate` | `@/views/container/template/index.vue` | 二級子路由，`hidden: true` |
| `setting` | `ContainerSetting` | `@/views/container/setting/index.vue` | 二級子路由，`hidden: true` |

#### 6. 主機與檔案模組 ([frontend/src/routers/modules/host.ts](`frontend/src/routers/modules/host.ts`))
| 路由路徑 (Path) | 路由名稱 (Name) | 對應 View 元件路徑 | 子路由 / 隱藏狀態 |
| :--- | :--- | :--- | :--- |
| `/hosts` | `System-Menu` | `Layout` | 頂層父容器（`sort: 7`） |
| `/hosts/files` | `File` | `@/views/host/file-management/index.vue` | 子路由，顯示於側邊欄（檔案管理） |
| `/hosts/monitor/monitor` | `Monitorx` | `@/views/host/monitor/monitor/index.vue` | 子路由，顯示於側邊欄（主機監控） |
| `/hosts/monitor/export` | `HostMonitorExport` | `@/views/host/monitor/export/index.vue` | 子路由，`hidden: true` |
| `/hosts/monitor/setting` | `HostMonitorSetting` | `@/views/host/monitor/setting/index.vue` | 子路由，`hidden: true` |
| `/hosts/firewall/rules` | `FirewallPort` | `@/views/host/firewall/rule/index.vue` | 子路由，顯示於側邊欄（防火牆） |
| `/hosts/firewall/docker` | `FirewallDockerGuard` | `@/views/host/firewall/docker/index.vue` | 子路由，`hidden: true` |
| `/hosts/firewall/forward` | `FirewallForward` | `@/views/host/firewall/forward/index.vue` | 子路由，`hidden: true` |
| `/hosts/firewall/setting` | `FirewallSetting` | `@/views/host/firewall/setting/index.vue` | 子路由，`hidden: true` |
| `/hosts/disk` | `Disk` | `@/views/host/disk-management/disk/index.vue` | 子路由，顯示於側邊欄（磁碟管理） |
| `/hosts/process/process` | `Process` | `@/views/host/process/process/index.vue` | 子路由，顯示於側邊欄（行程管理） |
| `/hosts/process/network` | `ProcessNetwork` | `@/views/host/process/network/index.vue` | 子路由，`hidden: true` |
| `/hosts/ssh/ssh` | `SSH` | `@/views/host/ssh/ssh/index.vue` | 子路由，顯示於側邊欄（SSH 管理） |
| `/hosts/ssh/log` | `SSHLog` | `@/views/host/ssh/log/index.vue` | 子路由，`hidden: true` |
| `/hosts/ssh/session` | `SSHSession` | `@/views/host/ssh/session/index.vue` | 子路由，`hidden: true` |

#### 7. 終端機模組 ([frontend/src/routers/modules/terminal.ts](`frontend/src/routers/modules/terminal.ts`))
| 路由路徑 (Path) | 路由名稱 (Name) | 對應 View 元件路徑 | 子路由 / 隱藏狀態 |
| :--- | :--- | :--- | :--- |
| `/terminal` | `Terminal-Menu` | `Layout` | 頂層父容器（`sort: 8`） |
| `/terminal` | `Terminal` | `@/views/terminal/index.vue` | 子路由，側邊欄單一項目 |

#### 8. 計畫任務模組 ([frontend/src/routers/modules/cronjob.ts](`frontend/src/routers/modules/cronjob.ts`))
| 路由路徑 (Path) | 路由名稱 (Name) | 對應 View 元件路徑 | 子路由 / 隱藏狀態 |
| :--- | :--- | :--- | :--- |
| `/cronjobs` | `Cronjob-Menu` | `Layout` | 頂層父容器（`sort: 9`） |
| `/cronjobs` | `Cronjob` | `@/views/cronjob/index.vue` | 一級子路由，側邊欄單一項目 |
| `cronjob` | `CronjobItem` | `@/views/cronjob/cronjob/index.vue` | 二級子路由，`hidden: true` |
| `cronjob/operate` | `CronjobOperate` | `@/views/cronjob/cronjob/operate/index.vue` | 二級子路由，`hidden: true` |
| `library` | `Library` | `@/views/cronjob/library/index.vue` | 二級子路由，`hidden: true` |

#### 9. 工具箱模組 ([frontend/src/routers/modules/toolbox.ts](`frontend/src/routers/modules/toolbox.ts`))
| 路由路徑 (Path) | 路由名稱 (Name) | 對應 View 元件路徑 | 子路由 / 隱藏狀態 |
| :--- | :--- | :--- | :--- |
| `/toolbox` | `Toolbox-Menu` | `Layout` | 頂層父容器（`sort: 9`） |
| `/toolbox` | `Toolbox` | `@/views/toolbox/index.vue` | 一級子路由，側邊欄單一項目 |
| `device` | `Device` | `@/views/toolbox/device/index.vue` | 二級子路由，`hidden: true` |
| `supervisor` | `Supervisor` | `@/views/toolbox/supervisor/index.vue` | 二級子路由，`hidden: true` |
| `clam` | `Clam` | `@/views/toolbox/clam/index.vue` | 二級子路由，`hidden: true` |
| `clam/setting` | `Clam-Setting` | `@/views/toolbox/clam/setting/index.vue` | 二級子路由，`hidden: true` |
| `ftp` | `FTP` | `@/views/toolbox/ftp/index.vue` | 二級子路由，`hidden: true` |
| `fail2ban` | `Fail2ban` | `@/views/toolbox/fail2ban/index.vue` | 二級子路由，`hidden: true` |
| `clean` | `Clean` | `@/views/toolbox/clean/index.vue` | 二級子路由，`hidden: true` |

#### 10. 日誌模組 ([frontend/src/routers/modules/log.ts](`frontend/src/routers/modules/log.ts`))
| 路由路徑 (Path) | 路由名稱 (Name) | 對應 View 元件路徑 | 子路由 / 隱藏狀態 |
| :--- | :--- | :--- | :--- |
| `/logs` | `Log-Menu` | `Layout` | 頂層父容器（`sort: 11`） |
| `/logs` | `Log` | `@/views/log/index.vue` | 一級子路由，側邊欄單一項目 |
| `operation` | `OperationLog` | `@/views/log/operation/index.vue` | 二級子路由，`hidden: true` |
| `login` | `LoginLog` | `@/views/log/login/index.vue` | 二級子路由，`hidden: true` |
| `website` | `WebsiteLog` | `@/views/log/website/index.vue` | 二級子路由，`hidden: true` |
| `system` | `SystemLog` | `@/views/log/system/index.vue` | 二級子路由，`hidden: true` |
| `host` | `HostSystemLog` | `@/views/log/host-system/index.vue` | 二級子路由，`hidden: true` |
| `ssh` | `SSHLog2` | `@/views/host/ssh/log/log.vue` | 二級子路由，`hidden: true` |
| `task` | `Task` | `@/views/log/task/index.vue` | 二級子路由，`hidden: true` |

#### 11. 面板設定模組 ([frontend/src/routers/modules/setting.ts](`frontend/src/routers/modules/setting.ts`))
| 路由路徑 (Path) | 路由名稱 (Name) | 對應 View 元件路徑 | 子路由 / 隱藏狀態 |
| :--- | :--- | :--- | :--- |
| `/settings` | `Setting-Menu` | `Layout` | 頂層父容器（`sort: 12`） |
| `/settings` | `Setting` | `@/views/setting/index.vue` | 一級子路由，側邊欄單一項目 |
| `apikeys` | `APIKeys` | `@/views/setting/api-keys/index.vue` | 二級子路由，`hidden: true` |
| `panel` | `Panel` | `@/views/setting/panel/index.vue` | 二級子路由，`hidden: true` |
| `alert` | `Alert` | `@/views/setting/alert/index.vue` | 二級子路由，`hidden: true` |
| `backupaccount` | `BackupAccount` | `@/views/setting/backup-account/index.vue` | 二級子路由，`hidden: true` |
| `license` | `License` | `@/views/setting/license/index.vue` | 二級子路由，`hidden: true` |
| `about` | `About` | `@/views/setting/about/index.vue` | 二級子路由，`hidden: true` |
| `safe` | `Safe` | `@/views/setting/safe/index.vue` | 二級子路由，`hidden: true` |
| `snapshot` | `Snapshot` | `@/views/setting/snapshot/index.vue` | 二級子路由，`hidden: true` |
| `expired` | `Expired` | `@/views/setting/expired.vue` | 二級子路由，`hidden: true` |

#### 12. 錯誤捕捉模組 ([frontend/src/routers/modules/error.ts](`frontend/src/routers/modules/error.ts`))
| 路由路徑 (Path) | 路由名稱 (Name) | 對應 View 元件路徑 | 子路由 / 隱藏狀態 |
| :--- | :--- | :--- | :--- |
| `/error` | - | `Layout` | 頂層父容器（無 sort） |
| `404` | `404` | `@/components/error-message/404.vue` | 子路由，`hidden: true`（不進選單） |

---

# 2. 主選單結構

### 側邊欄分類、排序與階層推算依據
- 排序與資料來源：[frontend/src/routers/router.ts](`frontend/src/routers/router.ts#L29-L56`)（先依據各模組 `sort` 欄位升冪排序，並將 `homeRouter` 透過 `unshift` 強制置頂為第一順位）。
- 頂層 vs. 子選單判斷規則：依據 [frontend/src/layout/components/Sidebar/components/SubItem.vue](`frontend/src/layout/components/Sidebar/components/SubItem.vue#L3-L27`)：
  - 當過濾掉 `hidden: true` 後，子項目數 `children.length > 1` 時，渲染為展開式子選單 `<el-sub-menu>`。
  - 當 `children.length === 1` 時，將單一子項目提昇為頂層平鋪選單 `<el-menu-item>`，直達其路徑。

### 側邊欄完整排序與樹狀結構（OSS 預設狀態）
1. **首頁 / 儀表板 (`/`)**
   - 頂層平鋪項目（`Home-Menu`，圖示 `p-home`）
2. **應用商店 (`/apps`)**
   - 頂層平鋪項目（`sort: 2`，圖示 `p-appstore`，內部子項目全為 `hidden: true`）
3. **網站 (`/websites`)**
   - 頂層展開式選單（`sort: 3`，圖示 `p-website`，包含 4 個二級子項目）：
     - 網站清單 (`/websites`)
     - SSL 憑證 (`/websites/ssl`)
     - 網站範本 (`/websites/templates`)
     - 運作環境 (`/websites/runtimes/php`，名稱顯示為 Runtime)
4. **AI 工具箱 (`/ai`)**
   - 頂層展開式選單（`sort: 4`，圖示 `p-jiqiren2`，包含 4 個二級子項目）：
     - Agent 智能體 (`/ai/agents/agent`)
     - 大模型 (`/ai/model/account`)
     - MCP 服務 (`/ai/mcp`)
     - GPU 監控 (`/ai/gpu/current`)
5. **資料庫 (`/databases`)**
   - 頂層平鋪項目（`sort: 5`，圖示 `p-database`，導向 `/databases/mysql`）
6. **容器 (`/containers`)**
   - 頂層平鋪項目（`sort: 6`，圖示 `p-docker1`，導向 `/containers/dashboard`）
7. **主機 / 系統 (`/hosts`)**
   - 頂層展開式選單（`sort: 7`，圖示 `p-host`，包含 6 個二級子項目）：
     - 檔案管理 (`/hosts/files`)
     - 主機監控 (`/hosts/monitor/monitor`)
     - 防火牆 (`/hosts/firewall/rules`)
     - 磁碟管理 (`/hosts/disk`)
     - 行程管理 (`/hosts/process/process`)
     - SSH 管理 (`/hosts/ssh/ssh`)
8. **終端機 (`/terminal`)**
   - 頂層平鋪項目（`sort: 8`，圖示 `p-terminal2`）
9. **計畫任務 (`/cronjobs`)**
   - 頂層平鋪項目（`sort: 9`，圖示 `p-plan`，導向 `/cronjobs/cronjob`）
10. **工具箱 (`/toolbox`)**
    - 頂層平鋪項目（`sort: 9`，圖示 `p-toolbox`，導向 `/toolbox/device`）
11. **日誌 (`/logs`)**
    - 頂層平鋪項目（`sort: 11`，圖示 `p-log`，導向 `/logs/operation`）
12. **面板設定 (`/settings`)**
    - 頂層平鋪項目（`sort: 12`，圖示 `p-config`，依權限重定向至 `/settings/panel` 等）

---

# 3. 各頂層功能頁內部分頁與子功能（以 views/ 目錄與檔名為據）

### 1. 儀表板 (Dashboard)
- 檔案路徑：[frontend/src/views/home/index.vue](`frontend/src/views/home/index.vue`)
- 頁面區塊與子功能：
  - 快速跳轉列與自訂快捷目錄（[views/home/quick/index.vue](`frontend/src/views/home/quick/index.vue`)、[directories.vue](`frontend/src/views/home/quick/directories.vue`)）
  - 系統實時狀態環狀圖（[views/home/status/index.vue](`frontend/src/views/home/status/index.vue`)，CPU、記憶體、磁碟、負載率）
  - 實時動態監控折線圖（網路傳輸量 / 磁碟 I/O 曲線，支援網卡與磁碟切換及 dataZoom）
  - 右側輪播卡片（系統軟硬體基礎資訊、重啟面板/伺服器、備忘錄 Memo、多節點概覽）
  - 已安裝應用快速列表（[views/home/app/index.vue](`frontend/src/views/home/app/index.vue`)）
  - 初次登入歡迎對話框（`DialogPro id="welcomeDialog"`）

### 2. 應用商店 (App Store)
- 檔案路徑：[frontend/src/views/app-store/index.vue](`frontend/src/views/app-store/index.vue`)
- 頂部次級頁籤（RouterButton）：
  1. 全部應用 (`/apps/all` -> [views/app-store/apps/index.vue](`frontend/src/views/app-store/apps/index.vue`))：分類標籤、關鍵字搜尋、應用詳情/安裝抽屜（[views/app-store/detail/index.vue](`frontend/src/views/app-store/detail/index.vue`) 內含 `form/`、`install/`、`params/`）
  2. 已安裝 (`/apps/installed` -> [views/app-store/installed/index.vue](`frontend/src/views/app-store/installed/index.vue`))：卡片/列表模式（`card.vue`, `info.vue`）、啟動/停止/重啟操作、刪除對話框（`delete/`）、升級對話框（`upgrade/` 內含 `diff/`）、忽略升級設定（`ignore/`）、通訊埠衝突檢查（`check/`）、詳細參數檢視（`detail/`）
  3. 可升級 (`/apps/upgrade` -> 同上，帶待升級過濾參數)
  4. 應用設定 (`/apps/setting` -> [views/app-store/setting/index.vue](`frontend/src/views/app-store/setting/index.vue`))：自訂應用倉庫設定

### 3. 網站 (Websites)
- 包含 4 個子頁面：
  1. 網站列表 ([views/website/website/index.vue](`frontend/src/views/website/website/index.vue`))：
     - 操作對話框/抽屜：建立網站抽屜（`create/`）、OpenResty 安裝檢驗（`check/`）、預設網站設定（`default/`）、Nginx 全域設定（`nginx/` 內含 `source/`, `status/`, `performance/`, `other/`）、CORS 設定（`cors/`）、批次操作（`batch-op/group.vue`, `batch-op/https.vue`）、域名管理（`domain/`, `domain-create/`）、刪除網站（`delete/`）
     - 網站配置獨立頁面 ([views/website/website/config/index.vue](`frontend/src/views/website/website/config/index.vue`))，含三大 Tab：
       - 基本配置 (`basic/`)：域名管理、網站目錄、預設文件、防盜鏈、流量限制、反向代理、負載平衡、密碼保護、跨域 CORS、HTTPS 設定、真實 IP (RealIP)、偽靜態 (Rewrite)、重定向、Stream 配置、配置檔直編
       - 網站日誌 (`log/`)
       - 網站原始碼 (`resource/`)
  2. SSL 憑證 ([views/website/ssl/index.vue](`frontend/src/views/website/ssl/index.vue`))：申請憑證（`apply/`, `create/`）、上傳憑證（`upload/`）、取得現有憑證（`obtain/`）、憑證詳情（`detail/`）、CA 憑證機構管理（`ca/`）、DNS API 帳號管理（`dns-account/`）、ACME 帳號管理（`acme-account/`）
  3. 網站範本 ([views/website/template/index.vue](`frontend/src/views/website/template/index.vue`))：建立/編輯範本（`operate/`）、匯出範本（`output/`）
  4. 運作環境 ([views/website/runtime/](`frontend/src/views/website/runtime/`))：
     - PHP 運作環境 (`php/`)：建立環境、設定檔修改、擴展管理、擴展範本、Supervisor 行程、相依性檢驗
     - Node.js (`node/`)：建立/啟動、模組 (npm/yarn/pnpm) 管理
     - Java (`java/`), Go (`go/`), Python (`python/`), .NET (`dotnet/`) 運作環境建立與參數配置

### 4. 資料庫 (Databases)
- 檔案路徑：[frontend/src/views/database/index.vue](`frontend/src/views/database/index.vue`)
- 頂部次級頁籤（RouterButton）：
  1. MySQL (`/databases/mysql` -> [views/database/mysql/index.vue](`frontend/src/views/database/mysql/index.vue`))：本地庫管理、遠端資料庫管理（`remote/`）、建立資料庫（`create/`）、刪除（`delete/`）、使用者權限維護（`user/`）、綁定應用（`bind/`）、連線資訊（`conn/`）、服務參數調優/慢查詢日誌/狀態監控（`setting/`）
  2. PostgreSQL (`/databases/postgresql` -> [views/database/postgresql/index.vue](`frontend/src/views/database/postgresql/index.vue`))：本地庫與遠端庫、建立/刪除、密碼重設、權限管理（`privileges/`）、參數設定
  3. Redis (`/databases/redis` -> [views/database/redis/index.vue](`frontend/src/views/database/redis/index.vue`))：連線檢測、Redis CLI 指令互動終端（`command/`）、持久化配置與運行狀態統計（`setting/`）
  4. MongoDB (`/databases/mongodb` -> [views/database/mongodb/index.vue](`frontend/src/views/database/mongodb/index.vue`))：本地與遠端庫建立/刪除、密碼修改、角色權限（`permission/`）

### 5. 容器 (Containers)
- 檔案路徑：[frontend/src/views/container/index.vue](`frontend/src/views/container/index.vue`)
- 頂部次級頁籤（RouterButton）：
  1. 概覽 (`dashboard` -> [views/container/dashboard/index.vue](`frontend/src/views/container/dashboard/index.vue`))：Docker 守護行程狀態、容器/映像檔/儲存卷/網路總覽統計
  2. 容器 (`container` -> [views/container/container/index.vue](`frontend/src/views/container/container/index.vue`))：建立/編輯容器（`operate/`）、更名（`rename/`）、提交為映像檔（`commit/`）、容器映像檔更新/升級（`upgrade/`）、效能監控（`monitor/`）、Inspect 元數據（`inspect/`）、容器終端 WebExec（`terminal/`）、容器內部檔案瀏覽器（`file-browser/`）、清理孤立容器（`prune/`）
  3. Compose (`compose` -> [views/container/compose/index.vue](`frontend/src/views/container/compose/index.vue`))：Compose 專案建立、YAML 編輯、日誌追蹤、刪除（`delete/`）
  4. 映像檔 (`image` -> [views/container/image/index.vue](`frontend/src/views/container/image/index.vue`))：拉取（`pull/`）、推送（`push/`）、匯入 tar（`load/`）、匯出（`save/`）、建置 Dockerfile（`build/`）、打標籤（`tag/`）、清理無用映像檔（`prune/`）
  5. 網路 (`network` -> [views/container/network/index.vue](`frontend/src/views/container/network/index.vue`))：建立自訂網路（`create/`）、檢視詳情與關聯容器（`detail/`）
  6. 儲存卷 (`volume` -> [views/container/volume/index.vue](`frontend/src/views/container/volume/index.vue`))：建立命名卷（`create/`）
  7. 映像倉庫 (`repo` -> [views/container/repo/index.vue](`frontend/src/views/container/repo/index.vue`))：配置 Docker Registry 帳密憑證（`operator/`）
  8. 編排範本 (`template` -> [views/container/template/index.vue](`frontend/src/views/container/template/index.vue`))：匯入與維護 Compose 範本（`operator/`, `import/`）
  9. 設定 (`setting` -> [views/container/setting/index.vue](`frontend/src/views/container/setting/index.vue`))：官方/自訂鏡像加速器（`mirror/`）、日誌配置（`log/`）、Sock 路徑（`sock-path/`）、IPv6 支援（`ipv6/`）

### 6. 主機與檔案管理 (Hosts & Files)
- 包含 6 個二級子功能：
  1. 檔案管理 ([views/host/file-management/index.vue](`frontend/src/views/host/file-management/index.vue`))：目錄樹導航、建立檔案/目錄（`create/`）、分片上傳檔案/目錄（`upload/`）、遠端下載（`wget/`，含 WebSocket 進度追蹤 `process/`）、下載（`download/`）、移動/複製（`move/`）、重新命名（`rename/`）、權限變更（`chown/`, `change-role/`, `batch-role/`）、壓縮/解壓縮（`compress/`, `decompress/`）、回收筒機制（`recycle-bin/`）、快速收藏夾（`favorite/`）、外鏈分享（`share/`, `share-list/`）、編碼轉換（`convert/`）、多頁籤 Monaco 程式碼編輯器（`code-editor/`，含歷史版本比較 `history/`）、AI 語意檔案搜尋抽屜（`ai-search/`）、檔案終端機直接切入（`terminal/`）
  2. 主機監控 ([views/host/monitor/monitor/index.vue](`frontend/src/views/host/monitor/monitor/index.vue`))：CPU、記憶體、磁碟、負載、網路 I/O、磁碟 I/O 歷史曲線、資料匯出（`export/`）、監控參數設定（`setting/`，含保存天數、統計頻率）
  3. 防火牆 ([views/host/firewall/rule/index.vue](`frontend/src/views/host/firewall/rule/index.vue`))：通訊埠放行規則（`operate/`, `import/`）、Docker 網路防護（`docker/`）、端口轉發規則（`forward/`）、防火牆全域狀態與 IP 白名單（`setting/`）
  4. 磁碟管理 ([views/host/disk-management/disk/index.vue](`frontend/src/views/host/disk-management/disk/index.vue`))：實體磁碟資訊、分割區掛載管理（`partition/`）
  5. 行程管理 ([views/host/process/process/index.vue](`frontend/src/views/host/process/process/index.vue`))：行程樹/行程清單（支援 kill）、行程詳細監控（`detail/`）、效能診斷（`diagnostics/`）、即時網路連線監聽狀態（`network/`）
  6. SSH 管理 ([views/host/ssh/ssh/index.vue](`frontend/src/views/host/ssh/ssh/index.vue`))：SSH 守護服務配置（連接埠、Root 登入限制、密碼/金鑰認證、監聽地址）、連線金鑰管理（`auth-keys/`）、SSH 登入日誌（`log/`）、作用中 Session 即時踢除（`session/`，WebSocket 連線）

### 7. 終端機 (Terminal)
- 檔案路徑：[frontend/src/views/terminal/index.vue](`frontend/src/views/terminal/index.vue`)
- 頂部單選按鈕分頁（`el-radio-group`）：
  1. 終端 (`terminal` -> [views/terminal/terminal/index.vue](`frontend/src/views/terminal/terminal/index.vue`))：主機與容器的 xterm.js 互動操作，支援多分頁 Session 與 Dock 停靠
  2. 主機連線 (`host` -> [views/terminal/host/index.vue](`frontend/src/views/terminal/host/index.vue`))：新增/編輯 SSH 主機資產（`operate/`）
  3. 快捷指令 (`command` -> [views/terminal/command/index.vue](`frontend/src/views/terminal/command/index.vue`))：常用 Shell 指令庫維護（`operate/`, `import/`）
  4. 終端設定 (`setting` -> [views/terminal/setting/index.vue](`frontend/src/views/terminal/setting/index.vue`))：字型、字級、行高、色彩主題設定，預設連線主機（`default-conn/`），AI 終端輔助配置（`ai/`）

### 8. 計畫任務 (Cronjobs)
- 檔案路徑：[frontend/src/views/cronjob/index.vue](`frontend/src/views/cronjob/index.vue`)
- 頂部次級頁籤（RouterButton）：
  1. 計畫任務 (`cronjob` -> [views/cronjob/cronjob/index.vue](`frontend/src/views/cronjob/cronjob/index.vue`))：任務清單、建立/編輯任務（`operate/`）、執行歷史與日誌（`record/`）、任務產出的備份檔列表與即時還原（`backup/`）、任務匯入（`import/`）、日誌自動清理策略（`config/clean-log.vue`）
  2. 腳本庫 (`library` -> [views/cronjob/library/index.vue](`frontend/src/views/cronjob/library/index.vue`))：維護自訂 Shell 腳本範本（`operate/`）、手動立即執行（`run/`）

### 9. 工具箱 (Toolbox)
- 檔案路徑：[frontend/src/views/toolbox/index.vue](`frontend/src/views/toolbox/index.vue`)
- 頂部次級頁籤（RouterButton）：
  1. 常用工具 (`device` -> [views/toolbox/device/index.vue](`frontend/src/views/toolbox/device/index.vue`))：伺服器時區（`time-zone/`）、Swap 虛擬記憶體配置（`swap/`）、NTP 網路對時（`ntp/`）、主機名稱（`hostname/`）、Hosts 映射解析（`hosts/`）、DNS 伺服器配置（`dns/`）、root 系統密碼重設（`passwd/`）
  2. 磁碟清理 (`clean` -> [views/toolbox/clean/index.vue](`frontend/src/views/toolbox/clean/index.vue`))：掃描並清理系統快取、日誌文件、Docker 無效容器/映像檔
  3. Supervisor 行程守護 (`supervisor` -> [views/toolbox/supervisor/index.vue](`frontend/src/views/toolbox/supervisor/index.vue`))：服務建立（`create/`）、守護行程啟停重啟、服務狀態（`status/`）、設定檔編輯與日誌檢視（`config/`, `file/`, `log/`）
  4. ClamAV 防毒掃描 (`clam` -> [views/toolbox/clam/index.vue](`frontend/src/views/toolbox/clam/index.vue`))：掃描任務配置（`operate/`）、感染紀錄（`record/`）、防毒服務全域設定（`setting/`）
  5. FTP 伺服器 (`ftp` -> [views/toolbox/ftp/index.vue](`frontend/src/views/toolbox/ftp/index.vue`))：FTP 使用者帳號新增/修改/密碼調整（`operate/`）、傳輸日誌（`log/`）
  6. Fail2ban 防暴力破解 (`fail2ban` -> [views/toolbox/fail2ban/index.vue](`frontend/src/views/toolbox/fail2ban/index.vue`))：封鎖 IP 清單解封（`ips/`）、封鎖時長（`ban-time/`）、最大嘗試次數（`max-retry/`）、比對時間範圍（`find-time/`）、日誌監控來源（`log-path/`）、受保護連接埠（`port/`）

### 10. AI 工具箱 (AI Tools)
- 包含 4 個二級子頁面：
  1. Agent 智能體 ([views/ai/agents/agent/index.vue](`frontend/src/views/ai/agents/agent/index.vue`))：新增 Agent 抽屜（`add/`）、設定檔分頁維護（`config/`）、綁定對外 Web 網站（`website/`）、內嵌式對話測試視窗（`components/hermes-chat.vue`）、容器終端（`components/terminal.vue`）、模型池配置（`agents/model/pool/`）
  2. 大模型管理 ([views/ai/model/index.vue](`frontend/src/views/ai/model/index.vue`))：
     - 本地大模型部署（`local/`）：Ollama 框架服務（`ollama/`，支援模型拉取下載 `add/`、刪除 `del/`、連線設定 `conn/`、網域名稱對外反代 `domain/`、終端機即時除錯 `terminal/`）
     - TensorRT-LLM 部署管理（`tensorrt/`）
  3. MCP 服務 ([views/ai/mcp/server/index.vue](`frontend/src/views/ai/mcp/server/index.vue`))：Model Context Protocol 服務註冊、啟停維護（`operate/`）、JSON 設定直編（`config/`）、匯入定義（`import/`）、本機目錄/儲存卷掛載（`volume/`）、綁定關聯（`bind/`）
  4. GPU 監控 ([views/ai/gpu/current/index.vue](`frontend/src/views/ai/gpu/current/index.vue`))：顯卡型號、驅動/CUDA 版本、算力核心使用率、顯存 (VRAM) 佔用、功率與溫度詳情卡片（`DeviceDetails.vue`）、歷史趨勢曲線（`history/`）

### 11. 日誌 (Logs)
- 檔案路徑：[frontend/src/views/log/index.vue](`frontend/src/views/log/index.vue`)
- 頂部次級頁籤（RouterButton）：
  1. 面板日誌 (`/logs/operation` -> [views/log/operation/index.vue](`frontend/src/views/log/operation/index.vue`))：操作審計日誌（支援狀態、模組過濾）、管理員登入日誌（`login/`）、系統後端除錯日誌（`system/`）、非同步排程/長時間執行任務進度追蹤（`task/`）
  2. 主機系統日誌 (`/logs/host` -> [views/log/host-system/index.vue](`frontend/src/views/log/host-system/index.vue`))：Linux journald 與系統 message 日誌
  3. SSH 登入日誌 (`/logs/ssh` -> [views/host/ssh/log/log.vue](`frontend/src/views/host/ssh/log/log.vue`))：系統 SSH 連線成功/失敗 IP 審計
  4. 網站日誌 (`/logs/website` -> [views/log/website/index.vue](`frontend/src/views/log/website/index.vue`))：OpenResty / Nginx 存取日誌 (Access Log) 與錯誤日誌 (Error Log)

### 12. 面板設定 (Settings)
- 檔案路徑：[frontend/src/views/setting/index.vue](`frontend/src/views/setting/index.vue`)
- 頂部次級頁籤（RouterButton 依身分動態篩選）：
  1. 面板設定 (`/settings/panel` -> [views/setting/panel/index.vue](`frontend/src/views/setting/panel/index.vue`))：面板名稱、側邊欄選單項自訂顯示/隱藏（`hidemenu/`）、伺服器外部 IP（`systemip/`）、HTTP/SOCKS5 外部代理（`proxy/`）、分頁快取（`cache/`）、登入超時（`timeout/`）、版本檢查與更新升級（`edition/`）、面板色彩主題（`theme-color/`）
  2. 安全設定 (`/settings/safe` -> [views/setting/safe/index.vue](`frontend/src/views/setting/safe/index.vue`))：面板通訊埠修改（`port/`）、安全入口 URL Token（`entrance/`）、面板 HTTPS 憑證自簽/配置（`ssl/`）、限制面板存取網域名稱（`domain/`）、授權 IP 白名單（`allowips/`）、監聽網卡 IP 綁定（`bind/`）、密碼時效政策（`expiration/`）、無權限回應遮蔽（`response/`）
  3. 告警通知 (`/settings/alert` -> [views/setting/alert/index.vue](`frontend/src/views/setting/alert/index.vue`))：告警大盤（`dash/`）、告警事件日誌（`log/`）、告警通知設定與通知媒介管理（`setting/`，含抽屜 `drawer/` 與時間免打擾區間 `time-range/`）
  4. 備份帳號 (`/settings/backupaccount` -> [views/setting/backup-account/index.vue](`frontend/src/views/setting/backup-account/index.vue`))：第三方對象儲存與本機備份目錄帳號新增/修改抽屜（`operate/`，支援 AWS S3, 阿里雲 OSS, 騰訊雲 COS, 七牛雲 Kodo, 華為雲 OBS, 又拍雲, WebDAV, SFTP, 本機目錄等）
  5. 快照 (`/settings/snapshot` -> [views/setting/snapshot/index.vue](`frontend/src/views/setting/snapshot/index.vue`))：全系統/面板快照建立（`create/`）、快照匯入（`import/`）、還原快照（`recover/`）、快照備份狀態（`status/`）
  6. 授權 (`/settings/license` -> [views/setting/license/index.vue](`frontend/src/views/setting/license/index.vue`))：展示當前社群版/專業版/企業版授權資訊與綁定
  7. API 金鑰 (`/settings/apikeys` -> [views/setting/api-keys/index.vue](`frontend/src/views/setting/api-keys/index.vue`))：API Key 新增、權限關聯、啟用/停用、撤銷
  8. 關於 (`/settings/about` -> [views/setting/about/index.vue](`frontend/src/views/setting/about/index.vue`))：系統版本、開源協議與社群連結

---

# 4. 商業版授權控制功能清單（非 OSS 基線）

### 判斷方式與全域變數依據
- 狀態儲存：[frontend/src/store/modules/global.ts](`frontend/src/store/modules/global.ts#L71-L106`)
  - `isProductPro`: 當前節點已綁定 Pro 授權（[frontend/src/utils/xpack.ts:102](`frontend/src/utils/xpack.ts#L102`) 判定 `status === 'Bound'`）。
  - `isMasterProductPro`: 主節點是否綁定 Pro 授權。
  - `isEnterprise` & `isEnterpriseLicensed`: 系統為 Enterprise 版本且已啟用。
  - `isXpackOrEE`: `(state.isEnterprise && state.isEnterpriseLicensed) || state.isMasterProductPro`。
  - 商業版動態元件加載函式：[frontend/src/extensions/optional.ts:18](`frontend/src/extensions/optional.ts#L18`) `loadOptionalComponent()`。在 OSS 版本中對應的 `@/xpack/views/` 或 `@/enterprise/views/` 實體檔案並不存在，加載時回傳空元件 `EmptyComponent`。

### 受商業授權控制的功能清單（React 重構時應排除或預留擴充）
1. **多節點切換與多節點大盤 (Multi-node Switcher & Overview)**：
   - [frontend/src/layout/components/Sidebar/components/Collapse.vue:35](`frontend/src/layout/components/Sidebar/components/Collapse.vue#L35`) 與 [L244](`frontend/src/layout/components/Sidebar/components/Collapse.vue#L244`)：`openNodeDashboard` 及節點下拉切換選單受 `isXpackOrEE` 嚴格限制；非 Pro/EE 強制執行 `changeToLocal()`。
   - [frontend/src/views/home/index.vue:829](`frontend/src/views/home/index.vue#L829`)：首頁多節點簡易切換卡片。
   - [frontend/src/views/log/operation/index.vue:117](`frontend/src/views/log/operation/index.vue#L117`)：操作日誌中的「節點 (Node)」篩選器與表格列。
   - [frontend/src/components/terminal/connection-menu/index.vue:137](`frontend/src/components/terminal/connection-menu/index.vue#L137`)：終端機跨節點主機清單。
2. **自訂應用倉庫與跨節點應用批次安裝 (App Store Pro)**：
   - [frontend/src/views/app-store/setting/index.vue:65-92](`frontend/src/views/app-store/setting/index.vue#L65-L92`)：`CustomSetting` 元件透過 `loadOptionalComponent('/src/xpack/views/appstore/index.vue')` 動態載入，受 `isXpackOrEE` 控制。
   - [frontend/src/views/app-store/apps/index.vue:198-264](`frontend/src/views/app-store/apps/index.vue#L198-L264`)：自訂應用商店同步與主節點同步配置。
   - [frontend/src/views/app-store/detail/form/index.vue:126](`frontend/src/views/app-store/detail/form/index.vue#L126`)：跨子節點批次安裝選項 (`batchInstallSupport`)。
   - [frontend/src/utils/app.ts:42-61](`frontend/src/utils/app.ts#L42-L61`)：叢集式資料庫跳轉 (`/xpack/cluster/mysql` 等)。
3. **SSL 憑證多節點分發與代理申請 (SSL Pro)**：
   - [frontend/src/views/website/ssl/index.vue:194](`frontend/src/views/website/ssl/index.vue#L194`) 與 [L376](`frontend/src/views/website/ssl/index.vue#L376`)：憑證「分發到子節點」按鈕。
   - [frontend/src/views/website/ssl/acme-account/index.vue:30](`frontend/src/views/website/ssl/acme-account/index.vue#L30`) 與 [acme-account/create/index.vue:9](`frontend/src/views/website/ssl/acme-account/create/index.vue#L9`)：ACME 帳號使用 Proxy 代理申請。
   - [frontend/src/views/website/ssl/create/index.vue:159](`frontend/src/views/website/ssl/create/index.vue#L159`)：建立憑證時「同步到全部節點」開關。
4. **AI vLLM 與批次安裝 (AI Pro / EE)**：
   - [frontend/src/views/ai/model/local/index.vue:34-72](`frontend/src/views/ai/model/local/index.vue#L34-L72`)：`vLLM` 引擎支援與模型下載器（`isProductPro ? 'vllm' : 'ollama'`）。
   - [frontend/src/views/ai/agents/agent/index.vue:327](`frontend/src/views/ai/agents/agent/index.vue#L327`)：企業版 Agent 批次安裝 (`/src/enterprise/views/ai/agents/batch-install/index.vue`)。
5. **公共備份帳號 (Public Backup Account)**：
   - [frontend/src/views/setting/backup-account/index.vue:49](`frontend/src/views/setting/backup-account/index.vue#L49`) 與 [backup-account/operate/index.vue:11](`frontend/src/views/setting/backup-account/operate/index.vue#L11`)：`isPublic`（所有節點公用備份帳號）設定項與列表欄位。
   - [frontend/src/api/modules/backup.ts:21-30](`frontend/src/api/modules/backup.ts#L21-L30`)：API 層限制非 Pro 傳遞 `isPublic`。
6. **進階告警通知通道 (Enterprise Alert Channels)**：
   - [frontend/src/views/setting/alert/dash/index.vue:28](`frontend/src/views/setting/alert/dash/index.vue#L28`) 及 [setting/index.vue:471](`frontend/src/views/setting/alert/setting/index.vue#L471`)：企業微信 (`weCom`)、釘釘 (`dingTalk`)、飛書 (`feiShu`)、簡訊 (`sms`) 通知通道。
   - [frontend/src/views/setting/alert/log/index.vue:7](`frontend/src/views/setting/alert/log/index.vue#L7`)：告警跨節點「全部同步」按鈕。
7. **商業規格與白標客製化 (Whitelabel & Branding)**：
   - [frontend/src/views/setting/panel/index.vue:39-62](`frontend/src/views/setting/panel/index.vue#L39-L62`)：自訂浮水印 (`watermark`)、自訂 Logo、自訂標題與登入背景。
   - [frontend/src/views/setting/panel/theme-color/index.vue:250](`frontend/src/views/setting/panel/theme-color/index.vue#L250`)：黑金色商務主題 (`isDarkGoldTheme`)。
   - [frontend/src/views/setting/panel/proxy/index.vue:50](`frontend/src/views/setting/panel/proxy/index.vue#L50`)：Docker 專用獨立代理 (`proxyDocker`)。

---

# 5. UI 技術規格盤點

依據 [frontend/package.json](`frontend/package.json`) 與原始碼實現：

1. **元件庫 (Component Library)**：
   - `element-plus` v2.14.7（全域引入，搭配 `@element-plus/icons-vue` v1.1.4）。
   - 樣式框架：Tailwind CSS（`@tailwindcss/postcss` v4.3.3）+ Sass（`sass` v1.105.0）。
   - 核心自製通用包裝元件：`DrawerPro`（標準抽屜）、`DialogPro`（標準對話框）、`RouterButton`（分頁導航按鈕）、`CardWithHeader`（儀表板卡片）、`LayoutContent` / `MainDiv`。
2. **狀態管理 (State Management)**：
   - `pinia` v3.0.4 + `pinia-plugin-persistedstate` v4.7.1。
   - 包含 6 個 Store 模組（位於 [frontend/src/store/modules/](`frontend/src/store/modules/`)）：
     - `global.ts`：全域主題、權限、當前節點、授權狀態、快取配置。
     - `menu.ts`：側邊欄收合、動態選單列表。
     - `process.ts`：主機行程樹與即時狀態。
     - `tabs.ts`：頁籤快取管理。
     - `terminal-session.ts`：多終端 Session 與 Dock 停靠管理。
     - `terminal.ts`：終端字型/外觀配置。
3. **i18n 多國語系清單與檔案位置**：
   - 框架：`vue-i18n` v11.4.4，實作於 [frontend/src/lang/index.ts](`frontend/src/lang/index.ts`)。
   - 語言檔目錄：[frontend/src/lang/modules/](`frontend/src/lang/modules/`)，共支援 12 種語言：
     1. `zh` (簡體中文): `modules/zh.ts`
     2. `zh-Hant` (繁體中文): `modules/zh-Hant.ts`
     3. `en` (English): `modules/en.ts`
     4. `pt-BR` (Português do Brasil): `modules/pt-br.ts`
     5. `ja` (日本語): `modules/ja.ts`
     6. `ru` (Русский): `modules/ru.ts`
     7. `ms` (Bahasa Melayu): `modules/ms.ts`
     8. `ko` (한국어): `modules/ko.ts`
     9. `lo` (ພາສາລາວ): `modules/lo.ts`
     10. `tr` (Türkçe): `modules/tr.ts`
     11. `fa` (فارسی): `modules/fa.ts`
     12. `es-ES` (Español): `modules/es-es.ts`
4. **圖表庫 (Chart Library)**：
   - `echarts` v6.1.0，工具類位於 [frontend/src/utils/echarts.ts](`frontend/src/utils/echarts.ts`)。
   - 自製封裝元件庫：`frontend/src/components/v-charts/`（包含 `Line.vue` 折線圖、`Pie.vue` 環狀圖、`Gauge.vue` 儀表盤等）。
5. **終端機元件 (Terminal)**：
   - 核心：`@xterm/xterm` v6.0.0，搭配自我調適插件 `@xterm/addon-fit` v0.11.0 與色彩解析器 `anser` v2.3.5。
   - 實現位置：[frontend/src/components/terminal/index.vue](`frontend/src/components/terminal/index.vue`)、[frontend/src/components/log/container/index.vue](`frontend/src/components/log/container/index.vue`) 與 [frontend/src/views/terminal/setting/index.vue](`frontend/src/views/terminal/setting/index.vue`)。
6. **程式碼編輯器 (Code Editors - 雙引擎做法)**：
   - **Monaco Editor**（`monaco-editor` v0.56.0）：用於重型 IDE 檔案編輯與歷史差異比較。配置於 [frontend/src/utils/monaco.ts](`frontend/src/utils/monaco.ts`)，啟用 Web Workers（JSON、CSS、HTML、TS、Editor）。使用於：
     1. 主機檔案管理多頁籤編輯器（[views/host/file-management/code-editor/index.vue](`frontend/src/views/host/file-management/code-editor/index.vue`)）
     2. 檔案歷史版本 side-by-side Diff 檢視（[views/host/file-management/code-editor/history/index.vue](`frontend/src/views/host/file-management/code-editor/history/index.vue`)）
     3. 應用升級配置 Diff（[views/app-store/installed/upgrade/diff/index.vue](`frontend/src/views/app-store/installed/upgrade/diff/index.vue`)）
   - **CodeMirror 6**（`codemirror` v6.0.2 / `vue-codemirror` v6.1.1）：封裝為通用彈跳視窗與表單元件 `CodemirrorPro`（[frontend/src/components/codemirror-pro/index.vue](`frontend/src/components/codemirror-pro/index.vue`)）與 `CodemirrorDrawer`，用於 Nginx 設定檔、Compose YAML、Dockerfile、Cronjob 腳本、SQL 配置等輕量檢視/編輯。
7. **檔案上傳做法 (File Upload)**：
   - 實作位置：[frontend/src/views/host/file-management/upload/index.vue](`frontend/src/views/host/file-management/upload/index.vue`) 與 [frontend/src/utils/chunk-upload.ts](`frontend/src/utils/chunk-upload.ts`)。
   - 特色機制：
     - 使用 Element Plus `<el-upload :auto-upload="false">` 支援檔案與目錄拖曳。
     - **分片上傳 (Chunked Upload)**：單片大小 `CHUNK_SIZE = 2MB`（`1024 * 1024 * 2`），由前端產生 `uploadID = newUUID()`。
     - 具備自動重試機制（`uploadChunkWithRetry`，支援指數延遲退避）、單檔與全域進度條計算，以及隨時中斷取消請求（呼叫後端 `stopChunkUpload` 並觸發 `AbortController`）。
8. **WebSocket 使用處 (WebSocket Endpoints)**：
   - 專案內使用原生 `new WebSocket(url)` 且皆先經 `checkStreamAuth()` 進行權限驗證：
     1. **Web 終端機與 Docker Exec 連線**：
        - 檔案：[frontend/src/components/terminal/index.vue:343](`frontend/src/components/terminal/index.vue#L343`)
        - 端點：`/api/v2/hosts/terminal` 或 `/api/v2/containers/terminal`；每 10 秒發送一次 `{ type: 'heartbeat', timestamp }` 心跳包。
     2. **主機行程即時串流與 SSH 作用中連線**：
        - 檔案：[frontend/src/store/modules/process.ts:173](`frontend/src/store/modules/process.ts#L173`) 與 [frontend/src/views/host/ssh/session/index.vue:107](`frontend/src/views/host/ssh/session/index.vue#L107`)
        - 端點：`/api/v2/process/ws?operateNode=${currentNode}`
     3. **檔案遠端下載 (Wget) 後台即時進度**：
        - 檔案：[frontend/src/views/host/file-management/process/index.vue:287](`frontend/src/views/host/file-management/process/index.vue#L287`)
        - 端點：`/api/v2/files/wget/process?operateNode=${currentNode}`

---

# 6. 對「多節點」的 UI 支援程度

- **結論：多節點切換器完全屬於商業版 (Pro / Enterprise)，在開源社群版 (OSS) 中不對使用者開放。**
- **依據檔案與行號**：
  1. 節點切換觸發入口：[frontend/src/layout/components/Sidebar/components/Collapse.vue](`frontend/src/layout/components/Sidebar/components/Collapse.vue`)
     - 第 35 行：多節點概覽按鈕受 `v-if="isXpackOrEE"` 限制。
     - 第 42 行與第 244 行：節點下拉列表受 `v-if="showNodes()"` 限制，其定義為：
       ```ts
       const showNodes = () => {
           return nodes.value.length > 0 && isXpackOrEE.value;
       };
       ```
     - 第 163 行：載入節點邏輯 `loadNodes()` 明確檢查：
       ```ts
       if (!isXpackOrEE.value) {
           changeToLocal();
           return;
       }
       ```
  2. 節點切換工具：[frontend/src/utils/node.ts](`frontend/src/utils/node.ts#L5-L19`)
     - 非 Pro 環境一律呼叫 `changeToLocal()`，將 `currentNode` 固定為 `'local'`，位址固定為 `'127.0.0.1'`。
  3. 側邊欄左下角僅顯示當前主機名稱（`loadCurrentName()`），但在 OSS 環境中點開浮動 Popover 僅有「任務中心」與「多節點概覽/節點清單被隱藏」，使用者無法切換任何節點。

---

# 7. 使用者常用核心流程畫面路徑（檔案層級）

1. **安裝應用流程**：
   - 瀏覽選取：[frontend/src/views/app-store/apps/index.vue](`frontend/src/views/app-store/apps/index.vue`)
   - 詳情與表單抽屜：[frontend/src/views/app-store/detail/index.vue](`frontend/src/views/app-store/detail/index.vue`)
   - 參數與進階選項輸入：[frontend/src/views/app-store/detail/form/index.vue](`frontend/src/views/app-store/detail/form/index.vue`) 及 [params/index.vue](`frontend/src/views/app-store/detail/params/index.vue`)
   - 安裝執行與日誌輸出：[frontend/src/views/app-store/detail/install/index.vue](`frontend/src/views/app-store/detail/install/index.vue`)（觸發 [frontend/src/api/modules/app.ts](`frontend/src/api/modules/app.ts`) 之 `installApp`）
2. **建立網站流程**：
   - 網站首頁入口：[frontend/src/views/website/website/index.vue](`frontend/src/views/website/website/index.vue`)
   - OpenResty 相依檢查：[frontend/src/views/website/website/check/index.vue](`frontend/src/views/website/website/check/index.vue`)
   - 建立網站設定抽屜：[frontend/src/views/website/website/create/index.vue](`frontend/src/views/website/website/create/index.vue`)（設定網域名稱、連接埠、靜態/PHP/Node/代理類型、資料庫綁定與 SSL）
   - 網站細部設定維護：[frontend/src/views/website/website/config/index.vue](`frontend/src/views/website/website/config/index.vue`)
3. **新增資料庫流程**：
   - 資料庫列表入口：[frontend/src/views/database/mysql/index.vue](`frontend/src/views/database/mysql/index.vue`)（或 `postgresql/`, `mongodb/`）
   - 資料庫伺服器連通檢查：[frontend/src/views/database/mysql/check/index.vue](`frontend/src/views/database/mysql/check/index.vue`)
   - 建立資料庫對話框：[frontend/src/views/database/mysql/create/index.vue](`frontend/src/views/database/mysql/create/index.vue`)（填寫名稱、編碼、使用者帳號密碼、遠端訪問權限）
4. **容器建立流程**：
   - 容器列表入口：[frontend/src/views/container/container/index.vue](`frontend/src/views/container/container/index.vue`)
   - 獨立建立頁面（路由 `/containers/container/operate`）：[frontend/src/views/container/container/operate/index.vue](`frontend/src/views/container/container/operate/index.vue`)
   - 儲存卷配置：[frontend/src/views/container/container/operate/volume.vue](`frontend/src/views/container/container/operate/volume.vue`)
   - 網路配置：[frontend/src/views/container/container/operate/network.vue](`frontend/src/views/container/container/operate/network.vue`)
   - 建立前摘要確認：[frontend/src/views/container/container/operate/confirm.vue](`frontend/src/views/container/container/operate/confirm.vue`)
5. **備份與還原流程**：
   - 先決條件（建立備份儲存端點）：[frontend/src/views/setting/backup-account/operate/index.vue](`frontend/src/views/setting/backup-account/operate/index.vue`)
   - 快照備份與還原（面板與系統等級）：
     - 建立快照對話框：[frontend/src/views/setting/snapshot/create/index.vue](`frontend/src/views/setting/snapshot/create/index.vue`)
     - 快照還原確認與重啟：[frontend/src/views/setting/snapshot/recover/index.vue](`frontend/src/views/setting/snapshot/recover/index.vue`)
   - 業務資料備份還原（網站/資料庫/目錄備份）：
     - 歷史備份清單與還原：[frontend/src/views/cronjob/cronjob/backup/index.vue](`frontend/src/views/cronjob/cronjob/backup/index.vue`)
6. **計畫任務建立流程**：
   - 任務列表入口：[frontend/src/views/cronjob/cronjob/index.vue](`frontend/src/views/cronjob/cronjob/index.vue`)
   - 獨立建立頁面（路由 `/cronjobs/cronjob/operate`）：[frontend/src/views/cronjob/cronjob/operate/index.vue](`frontend/src/views/cronjob/cronjob/operate/index.vue`)（選擇 Shell/備份網站/備份資料庫/日誌切分/記憶體清理等任務類型、設定 Cron 週期與通知通道）

---

# 8. UX 問題盤點與值得保留的互動體驗

### 三個最明顯的 UX 問題
1. **雙重導航概念混淆與路由快取脫節**
   - 依據檔案：[frontend/src/routers/index.ts:107-133](`frontend/src/routers/index.ts#L107-L133`)（`panelCache` 機制）、[frontend/src/routers/router.ts:44-55](`frontend/src/routers/router.ts#L44-L55`)
   - 問題分析：側邊欄各功能項目的資訊階層極度不一致。「網站」與「AI 工具箱」在側邊欄展開為二級選單；但「容器」、「資料庫」、「工具箱」在側邊欄卻只是單一項目，點入後在頁面頂端橫向堆疊 6~9 個自製 `<RouterButton>` 頁籤。更嚴重的是，系統在 `router.beforeEach` 與 `afterEach` 引入了 `localStorage` 的 `panelCache`，記憶使用者上次離開的頁籤。當使用者點擊側邊欄「容器」時，常被自動帶回上次瀏覽的「網路」或「儲存卷」而不是概覽或容器列表，造成強烈的心智模型斷層與迷航。
2. **巨幅垂直長表單缺乏清晰的分步導引 (Wizard Stepper)**
   - 依據檔案：[frontend/src/views/container/container/operate/index.vue](`frontend/src/views/container/container/operate/index.vue`)（1060+ 行）、[frontend/src/views/cronjob/cronjob/operate/index.vue](`frontend/src/views/cronjob/cronjob/operate/index.vue`)（935+ 行）、[frontend/src/views/website/website/config/basic/index.vue](`frontend/src/views/website/website/config/basic/index.vue`)
   - 問題分析：以「建立容器」與「建立計畫任務」為例，在單一畫面內塞入了超過 30 個設定輸入項（連接埠、環境變數、特權模式、Volume、重啟策略、限制配額等），使用者必須不斷大幅向下滾動，缺乏階段式校驗與錨點指引。此外，網站基本設定一口氣列出 14 個 Vertical Tabs，在筆電或低解析度螢幕上檢視極為擁擠。
3. **破壞性操作缺乏安全防護一致性 (ConfirmDialog vs. ElMessageBox 混用)**
   - 依據檔案：[frontend/src/views/toolbox/index.vue:46-55](`frontend/src/views/toolbox/index.vue#L46-L55`)、[frontend/src/layout/components/Sidebar/components/Collapse.vue:270-285](`frontend/src/layout/components/Sidebar/components/Collapse.vue#L270-L285`)、[frontend/src/views/host/file-management/delete/index.vue](`frontend/src/views/host/file-management/delete/index.vue`)
   - 問題分析：部分危險操作（如重啟系統）採用 `ConfirmDialog` 要求輸入「立即重啟」確認；但大量其他關鍵操作（如刪除容器、刪除資料庫、刪除網站）僅使用標準的 `ElMessageBox.confirm`，預設焦點甚至可能停在確認按鈕上，使用者誤觸 Enter 即可觸發不可逆的刪除行為，缺乏依據影響程度分級的安全防護模型。

### 三個值得保留且精緻的互動設計
1. **多 Session 浮動終端機與背景保持機制 (Terminal Dock & Multi-session)**
   - 依據檔案：[frontend/src/components/terminal/index.vue](`frontend/src/components/terminal/index.vue`)、[frontend/src/store/modules/terminal-session.ts](`frontend/src/store/modules/terminal-session.ts`)
   - 優勢分析：管理員在任何業務畫面（例如排查容器、檢視網站、除錯日誌）隨時都能透過全域快捷喚出終端機，支援多個終端連線分頁並行、全螢幕放大切換、縮小為底部 Dock 欄位且保持背景 WebSocket 心跳不中斷，切換其他分頁回來後輸出依然完整，對維運工程師的體驗極佳。
2. **檔案管理中內建的多分頁 Monaco Editor 與歷史修訂 Diff (Monaco IDE & Version History Diff)**
   - 依據檔案：[frontend/src/views/host/file-management/code-editor/index.vue](`frontend/src/views/host/file-management/code-editor/index.vue`)、[frontend/src/views/host/file-management/code-editor/history/index.vue](`frontend/src/views/host/file-management/code-editor/history/index.vue`)
   - 優勢分析：不同於傳統管理後台僅提供簡陋的 `<textarea>`，1Panel 在網頁內完整整合了 Monaco Editor，支援多檔案標籤頁、自動辨識副檔用語法高亮、快速儲存快捷鍵，更具備即時歷史版本比較與復原功能，大幅降低誤改主機配置檔時的災難風險。
3. **儀表板「極簡狀態摘要 + 快速跳轉自訂 + 實時動態指標曲線」**
   - 依據檔案：[frontend/src/views/home/index.vue:39-160](`frontend/src/views/home/index.vue#L39-L160`)、[frontend/src/views/home/quick/index.vue](`frontend/src/views/home/quick/index.vue`)
   - 優勢分析：將 CPU、記憶體、磁碟、負載四大核心指標以醒目儀表呈現，搭配可自訂的業務統計數字摘要（點擊數字直接跳轉對應業務篩選頁），右側折線圖在單一卡片內無縫切換網卡頻寬與磁碟 I/O，版面緊湊、資訊密度高且流暢。

---

## 改了哪些檔案
- **無**（嚴格遵守只讀限制，未修改任何原始碼或專案檔案，工作區保持完全乾淨）。

## 未驗證項目
1. `@/xpack/` 與 `@/enterprise/` 的具體原始碼：由於本 repo 為 1Panel OSS 開源版本，商業版擴展目錄（`frontend/src/xpack`、`frontend/src/enterprise`）未包含在專案原始碼中，此部分行為是透過 `extensions/optional.ts`、`utils/xpack.ts` 及各 View 中的動態載入判斷逆向推導確認。
2. 實際執行時期各後端 API 的回傳欄位相容性：未啟動後端 Golang 服務連線測試真實 API，僅對前端 TypeScript 介面定義（`frontend/src/api/interface/`）進行靜態語意分析。

## 本輪經驗
無。
