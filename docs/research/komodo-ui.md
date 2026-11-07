# Komodo UI 研究

來源：Komodo main（2026-09）`ui/`，派工 agent（opencode / big-pickle 免費模型）唯讀閱讀原始碼，2026-11-07。主 agent 以 repo 內 `screenshots/` 的官方截圖核對了儀表板環形摘要、Server 頁量表與歷史面積圖三項結論。`mogh_ui` 是外部套件，其內部實作未驗證。

以下是我這輪唯讀分析的結果。沒有修改任何檔案；`ui/node_modules` 不存在，因此 `mogh_ui` 的內部實作與實際畫面無法直接驗證。

## 1. 技術棧與整體架構

- 前端是 React 19 + TypeScript + Vite，UI 以 Mantine 9.5 為主，路由使用 React Router 7，資料層使用 TanStack Query，表格使用 TanStack Table。`ui/package.json:12-39`
- 圖表依賴只有兩個：`recharts` 3.10.1 與 `react-minimal-pie-chart` 9.1.2。`ui/package.json:34-37`
- 共用元件庫 `mogh_ui` 是外部 npm 套件，不是此 repo 內的原始碼。`ui/package.json:29`
- 入口 `ui/src/main.tsx:33-43` 掛載 `ThemeProvider`、Router、Mantine Notifications，並延遲初始化 Monaco。
- 全域外殼是 Mantine `AppShell`：Topbar 高 62px、Sidebar 寬 240px、手機版以 drawer 呈現。`ui/src/app/index.tsx:11-45`
- 路由以 `lazy()` 載入頁面，`/` 是 Dashboard，其餘包含 Containers、Terminals、Stats、Schedules、Settings、Updates、Alerts，以及通用的 `/:type`、`/:type/:id`。`ui/src/router.tsx:15-120`
- 資源詳情頁不是每個資源各寫一套：由 `ResourceComponents` 提供 Table、Icon、State、Info、Config 等介面，通用頁面再組裝。`ui/src/pages/resource.tsx:103-117`

## 2. Dashboard 的資訊架構與摘要視覺

- Dashboard 預設顯示「最近資源」，不是表格。偏好預設值是 `showTables: false`、`showServerStats: false`。`ui/src/lib/hooks.ts:801-810`
- 頁首包含 `DashboardActiveResources`、標題、以及 `ShowTables`、`ServerShowStats`、`ExportToml` 三個動作。`ui/src/pages/dashboard/index.tsx:20-34`
- `showTables` 切換的是整個 Dashboard 的呈現模式，不是單一資源列表的卡片／表格切換。按鈕文字會在 `Show Dashboard` 與 `Show Tables` 間切換。`ui/src/pages/dashboard/show-tables.tsx:5-21`
- 最近資源模式會依 `SIDEBAR_RESOURCES` 逐資源類型建立一列。每列左側是摘要卡，右側是最近項目卡片。`ui/src/pages/dashboard/recents.tsx:28-63`
- 摘要卡是環形圖：`PieChart` 尺寸 150×150、`radius={42}`、`lineWidth={30}`，以 `ColorIntention` 著色，左側列出各狀態數量。`ui/src/components/dashboard-summary/index.tsx:31-55`
- 最近項目卡片使用 `FancyCard`，內容包含資源圖示、名稱、Template 標記、標籤，以及 Stack／Deployment 的更新提示。`ui/src/pages/dashboard/recents.tsx:130-179`
- Server 的最近項目卡片只有在 `showServerStats` 開啟時，才顯示 CPU／Memory／Disk 三條量表。`ui/src/pages/dashboard/recents.tsx:160-171`、`ui/src/resources/server/stats-card.tsx:23-55`
- 最近資源卡片本身沒有獨立的 `StatusBadge`；狀態主要由資源圖示顏色、更新標記，以及 Server 的量表呈現。
- Dashboard 上方另有「Active」表，列出正在部署、重啟、建置、執行中的資源，並使用 `StatusBadge`。`ui/src/pages/dashboard/active.tsx:20-215`
- 表格模式會把每種資源的 `RC.Table` 放進各自的 `Section`，並提供 Template、Tags、Search 篩選。`ui/src/pages/dashboard/tables.tsx:26-52`
- 沒有資源時顯示 `DashboardNoResources` 空狀態。`ui/src/pages/dashboard/no-resources.tsx:5-21`

## 3. 資源列表：表格或卡片

- 資源列表頁預設是表格，不是卡片。`ui/src/pages/resources.tsx:29-48`
- 通用 `ResourceTable` 會組出「新增／批次操作／分頁＋搜尋／標籤／Template／更新篩選」工具列，再渲染 `RC.Table`。`ui/src/resources/table.tsx:147-178`
- 各資源表格都使用 `mogh_ui` 的 `DataTable`。例如 Stack 表格欄位是 Name、Source、Host、State、Tags。`ui/src/resources/stack/table.tsx:24-112`
- Deployment 表格欄位是 Name、Image、Host、State、Tags。`ui/src/resources/deployment/table.tsx:24-112`
- Server 列表是唯一有「欄位模式」切換的資源：`showServerStats` 開啟時改用 `StatsServerTable`，顯示 CPU、Memory、Disk、Load Avg、Net，而不是換成卡片。`ui/src/resources/server/table/index.tsx:17-34`
- 資源列表頁沒有卡片／表格切換。卡片只出現在 Dashboard 的摘要與最近資源區。
- Dashboard 摘要卡的視覺元素是環形圖、狀態數量、資源圖示；最近資源卡則有狀態色圖示、名稱、Template 標記、標籤、更新提示，Server 另外有 CPU／Memory／Disk 量表。

## 4. Server 詳情與跨 Server 統計圖表

- Server 詳情頁預設 tab 是 `Config`，不是 `Stats`。`ui/src/resources/server/tabs.tsx:18-22`
- `Stats` tab 的內容順序是：System Info、Current Stats、Historical Stats、Container Stats、Disks、Processes。`ui/src/resources/server/stats/index.tsx:34-51`
- System Info 是 `DataTable`，顯示 Hostname、OS、Kernel、CPU、Arch、Core Count、Total Memory、Total Disk。`ui/src/resources/server/stats/system-info.tsx:25-78`
- Current Stats 使用：
  - Load Average：`InfoCard` + Mantine `Progress`，顯示 1m／5m／15m。`ui/src/resources/server/stats/current/load-average.tsx:37-72`
  - CPU、RAM、Disk：`StatBar`。`ui/src/resources/server/stats/current/index.tsx:26-59`
  - Network：`InfoCard`，顯示 Ingress／Egress 速率。`ui/src/resources/server/stats/current/network-usage.tsx:11-24`
- Historical Stats 使用 `recharts` 的 `AreaChart`，每張圖高 200px，使用 `monotone`、漸層填色、`CartesianGrid`、`Tooltip`、`Legend`。`ui/src/resources/server/stats/historical.tsx:262-408`
- 歷史圖表有六種：Load Average、CPU、Memory、Disk、Network Ingress、Network Egress。`ui/src/resources/server/stats/historical.tsx:32-49`
- Load Average 圖會畫 1m／5m／15m 三條線；Memory 圖是 Used／Cache／ZFS ARC 的堆疊面積。`ui/src/resources/server/stats/historical.tsx:126-181`
- 時間粒度有 5 秒到 1 天共 10 種，並可顯示／隱藏歷史區塊；顯示狀態存在 localStorage。`ui/src/resources/server/stats/historical.tsx:51-95`
- `recharts` 透過 `lazy()` 載入，避免進入初始 bundle。`ui/src/resources/server/stats/index.tsx:12-13`
- 跨 Server 的 Stats 頁面有：
  - 上方 CPU／Memory／Disk 平均用量 `StatBar`。`ui/src/pages/stats.tsx:200-293`
  - 下方六張 `LineChart`，每個 Server 一條線。`ui/src/pages/stats.tsx:387-525`
  - 固定 8 色 palette，最多顯示 8 個 Server。`ui/src/pages/stats.tsx:53-65`
  - 已選 Server、搜尋、排序、時間粒度會存在 localStorage。`ui/src/pages/stats.tsx:88-102`
- Server 統計表的 `StatCell` 會依 CPU／Memory／Disk 門檻給 `Good`／`Warning`／`Critical`。`ui/src/resources/server/table/stats.tsx:142-183`
- 要注意：可讀到的 `ServerStatsCard` 與 Load Average 的 `Progress` 固定使用 `color="bw"`，狀態色主要落在數字文字；`StatBar` 的填色邏輯來自 `mogh_ui`，無法確認。
- 沒有找到溫度、GPU、sparkline、BarChart。全 repo 搜尋 `barchart`、`gpu`、`temperature`、`sparkline` 都沒有結果。`ui/src`

## 5. Stack／Deployment 詳情頁

| 功能 | Stack | Deployment |
|---|---|---|
| 服務清單 | 有，`Services` tab，`DataTable`，顯示 service、state、image、ports 等。`ui/src/resources/stack/services.tsx:26-40` | 沒有獨立服務清單；Swarm 模式有 `Tasks` tab。`ui/src/resources/deployment/tabs.tsx:56-85` |
| 日誌 | 有，`Log` tab，使用 `StackLog`。`ui/src/resources/stack/tabs.tsx:126-127` | 有，`Log` tab，使用 `LogSection`。`ui/src/resources/deployment/tabs.tsx:113-119` |
| 終端機 | 有，`Terminals` tab，使用 `TerminalSection`。`ui/src/resources/stack/tabs.tsx:129-136` | 有，`Terminals` tab，使用 `TerminalSection`。`ui/src/resources/deployment/tabs.tsx:124-126` |
| 部署歷史 | 沒有專屬版本歷史；資源頁有 `ResourceUpdates`，另有全站 `/updates`。`ui/src/components/updates/resource.tsx:7-24` | 同上；`Tasks` 只是 Swarm task 清單，不是版本歷史。`ui/src/resources/deployment/tasks.tsx:17-47` |
| 設定編輯器 | 有，`Config` tab 預設開啟，使用 `Config`、`ConfigList`、`ConfigSwitch`、`MonacoEditor`。`ui/src/resources/stack/tabs.tsx:17-23` | 有，`Config` tab 預設開啟，使用 `Config`、`MonacoEditor` 與各子設定。`ui/src/resources/deployment/tabs.tsx:18-24` |
| 流程圖／依賴圖 | 沒有。服務之間只有表格，沒有 graph。`ui/src/resources/stack/services.tsx` | 沒有。`ui/src/resources/deployment/tabs.tsx` |

- Stack config 支援三種模式：`UI Defined`、`Files On Server`、`Git Repo`。`ui/src/resources/stack/config/index.tsx:48-61`
- Stack config 也包含 secrets、webhooks、branch、extra args 等設定。`ui/src/resources/stack/config/index.tsx:77-122`
- Deployment config 包含 image／build、network、ports、restart、termination、healthcheck、labels 等。`ui/src/resources/deployment/config/index.tsx:138-430`
- 日誌檢視器支援 stdout／stderr 切換、tail 行數、搜尋詞、invert、timestamps、poll、重新整理，並把 ANSI 轉成 HTML。`ui/src/components/log-section.tsx:45-208`
- 日誌使用 `contentVisibility` 降低大量行的渲染成本，並有自動滾到底部。`ui/src/components/log-viewer.tsx:74-123`
- 終端機使用 xterm + WebSocket，支援多個 terminal、重新連線、刪除，並有 light／dark theme。`ui/src/components/terminal/section.tsx:18-160`、`ui/src/components/terminal/xterm.tsx:8-168`
- Stack／Deployment 的 tab 顏色會跟著資源狀態意圖變化。`ui/src/resources/stack/tabs.tsx:140-150`、`ui/src/resources/deployment/tabs.tsx:129-135`
- Procedure 有「stage 內平行、stage 間循序」的執行模型，但 UI 是表格加上移／上下插入，不是流程圖。`ui/src/resources/procedure/config/index.tsx:75-90`

## 6. 全域導覽、搜尋與通知

- Sidebar 分成幾段：
  - 主要頁面：Dashboard、Containers、Terminals、Stats。
  - `Resources`：由 `SIDEBAR_RESOURCES` 動態產生。
  - `Notifications`：Alerts、Updates。
  - 其他：Schedules、Settings。
  `ui/src/app/sidebar.tsx:16-98`
- Sidebar 連結會依目前路徑切換 active 樣式。`ui/src/app/sidebar.tsx:117-147`
- Topbar 左側是 logo／KOMODO，中間是 Omni Search，右側有 API、Docs、版本、鍵盤快捷鍵、WebSocket 狀態、Alerts、Updates、主題切換、使用者選單。`ui/src/app/topbar/index.tsx:45-96`
- Omni Search 使用 Mantine `Spotlight`，快捷鍵 `shift + s`，可搜尋頁面、資源名稱、Stack Service、Container、Terminal。`ui/src/app/topbar/omni-search/index.tsx:10-90`、`ui/src/app/topbar/omni-search/hooks.tsx:121-291`
- Alerts 是 Topbar 的 `Menu`，只有未解決警報存在時才顯示，並在圖示上顯示紅色數量。`ui/src/app/topbar/alerts.tsx:10-61`
- Updates 也是 Topbar 的 `Menu`，用藍色圓點表示有未讀更新；開啟後呼叫 `SetLastSeenUpdate`。`ui/src/app/topbar/updates.tsx:15-73`
- `/updates` 頁是「歷史更新」表格，可依資源類型、資源、操作篩選，欄位包含結果、開始時間、操作者，點列開啟詳情 drawer。`ui/src/pages/updates.tsx:29-229`
- `/alerts` 頁同樣是表格，可依資源、警報類型、是否只看未解決來篩選。`ui/src/pages/alerts.tsx:40-171`
- 更新詳情 drawer 使用 `MonacoDiffEditor`，可看變更內容、log、操作者、時間與版本。`ui/src/components/updates/details.tsx:55-100`
- 全站另有 Mantine toast 通知，例如重新命名、儲存設定、終端機刪除等操作。`ui/src/resources/header.tsx:20-27`
- 沒有單一「通知中心」把 Alerts 與 Updates 合併；Sidebar 只是把它們放在同一個 `Notifications` 分類下。

## 7. 共用 UI 元件與狀態呈現

- 表格：幾乎所有清單都用 `mogh_ui` 的 `DataTable`。本地只負責傳入 `tableKey`、`data`、`columns`、`selectOptions`、排序回呼。`ui/src/resources/table.tsx:133-140`
- 狀態徽章：`StatusBadge` 來自 `mogh_ui`，本地用法是 `text` + `intent`。例如 Server state、Stack state、警報等級。`ui/src/resources/server/index.tsx:152-163`、`ui/src/components/alerts/level.tsx:5-12`
- 狀態意圖：`ui/src/lib/color.ts` 把 Server／Stack／Deployment／Build／Repo／Procedure／Action／Container／Swarm 狀態轉成 `ColorIntention`。`ui/src/lib/color.ts:12-237`
- 狀態顏色：`hexColorByIntention` 來自 `mogh_ui`；本地用它替資源圖示、警示圖示、更新圖示、數字著色。`ui/src/resources/link.tsx:44-48`、`ui/src/components/alerts/card.tsx:83-92`
- `StatBar`：來自 `mogh_ui`，本地用於 CPU／RAM／Disk 與 Stats 頁平均用量；目前看到的參數包括 `title`、`icon`、`description`、`percentage`、`warning`、`critical`、`flex`。`ui/src/resources/server/stats/current/index.tsx:30-56`
- `StatCell`：來自 `mogh_ui`，本地用於 Server 統計表格，參數包括 `value`、`intent`、`info`、`infoDisabled`。`ui/src/resources/server/table/stats.tsx:142-183`
- `Progress`：直接使用 Mantine，出現在 Server 統計卡與 Load Average。`ui/src/resources/server/stats-card.tsx:192`、`ui/src/resources/server/stats/current/load-average.tsx:51`
- 資源連結：本地 `ResourceLink` 會取得 `RC.Icon` 並用狀態色著色，還會顯示 Template 標記。`ui/src/resources/link.tsx:18-59`
- Docker／Swarm 連結：`DockerResourceLink` 會依 container state 著色，並顯示對應 icon。`ui/src/components/docker/link.tsx:24-70`
- 圖表：
  - `DashboardSummary`：本地環形圖元件。`ui/src/components/dashboard-summary/index.tsx`
  - `ServerHistoricalStats`：本地 `AreaChart` 元件。`ui/src/resources/server/stats/historical.tsx`
  - `Stats` 頁：本地 `LineChart` 元件。`ui/src/pages/stats.tsx`
- 日誌／終端機：
  - `LogSection`、`LogViewer` 是本地元件。`ui/src/components/log-section.tsx`、`ui/src/components/log-viewer.tsx`
  - `TerminalSection`、`TargetTerminal`、`NewTerminal`、xterm 包裝是本地元件。`ui/src/components/terminal/`
- 版面與容器：`Page`、`Section`、`InfoCard`、`FancyCard`、`EntityHeader`、`EntityPage`、`Config`、`ConfigList`、`ConfigSwitch`、`MonacoEditor` 都來自 `mogh_ui`，本地只組合。
- 其他本地視覺元件：`AlertCard`、`UpdateCard`、`Tags`、`TemplateMarker`、`HashCompare`、`FileSource`、`ResourceHeader`、`ResourceSubPage`。
- `mogh_ui` 的 `StatusBadge`、`StatBar`、`StatCell`、`DataTable` 等內部 DOM、CSS、無障礙屬性與實際動畫，無法在此 repo 驗證。

## 8. 可借鏡的三個做法

1. **用「狀態意圖」統一所有顏色，而不是每個頁面各自決定紅黃綠。**  
   `lib/color.ts` 把各種資源狀態集中映射成 `Good`／`Warning`／`Critical`／`Neutral`／`Unknown`，再讓圖示、`StatusBadge`、`StatCell`、tab 顏色共用同一套語意。這樣新增資源狀態時只需要改一處，視覺也不會前後不一致。`ui/src/lib/color.ts:12-237`

2. **Dashboard 採「摘要 + 最近 + 表格」三層，而不是只有一種清單。**  
   每個資源類型先用環形圖呈現狀態數量，再用最近項目卡片提供快速入口，最後用 `Show Tables` 切到完整表格。這讓首頁同時具備總覽、回流與深度操作三種用途；預設值是 Recents，偏好也會持久化。`ui/src/pages/dashboard/index.tsx:20-34`、`ui/src/pages/dashboard/recents.tsx:28-63`

3. **監控資料分層：即時量表、歷史趨勢、跨 Server 比較。**  
   即時資料用 `StatBar`／`Progress`，歷史資料用 `AreaChart`，多 Server 比較用 `LineChart`；歷史圖表延遲載入，時間粒度與顯示狀態可記住。這種分層比把所有指標塞進一張大圖更容易閱讀，也比較不會拖慢首頁。`ui/src/resources/server/stats/index.tsx:34-51`、`ui/src/resources/server/stats/historical.tsx:51-95`、`ui/src/pages/stats.tsx:295-384`

## 未驗證項目

- `mogh_ui` 的 `StatusBadge`、`StatBar`、`StatCell`、`DataTable`、`FancyCard`、`InfoCard`、`Section`、`Page`、`EntityHeader`、`Config`、`MonacoEditor`、`ThemeProvider` 內部實作與實際樣式。
- `bw`、`accent` 等自訂顏色的完整定義；此 repo 只看到 `accent-border` 與 Tag 顏色的區域用法。`ui/src/lib/color.ts:4-10`
- 沒有啟動前端、沒有截圖、沒有實際互動驗證；本輪是原始碼閱讀。
- 沒有執行測試，因為沒有改動程式，且依賴未安裝。
- 沒有逐一展開所有資源特有頁面（Build、Repo、Swarm、Docker 等）的每個細節；圖表型態是以主要頁面與全 repo import 搜尋交叉確認。
- 全 repo 搜尋沒有找到 BarChart、Mermaid、React Flow、Cytoscape、topology、dependency graph、GPU、temperature、sparkline；但不代表外部套件內部沒有其他視覺元件。

## 本輪經驗

- 先查 `package.json` 再找元件原始碼；`mogh_ui` 是外部套件，不能預設 repo 內看得到 `StatusBadge` 或 `StatBar` 的實作。
- Dashboard 的預設值要看 `useDashboardPreferences`，不能從畫面名稱推測；實際預設是 Recents，且 Server 統計默认隱藏。`ui/src/lib/hooks.ts:801-810`
- `Progress` 使用 `color="bw"` 時，量表本身是中性色，狀態主要由數字文字或 `StatCell` 的 intent 表達；不要直接把「有量表」寫成「量表會依門檻變紅黃綠」。
- 使用者說「卡片」時，要區分 Dashboard 的摘要／最近資源卡與資源列表頁；這個專案的資源列表預設是表格，卡片只出現在 Dashboard。