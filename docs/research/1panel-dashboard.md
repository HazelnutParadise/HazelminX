# 1Panel v2 儀表板與監控畫面研究

來源：1Panel v2.4.1 `frontend/`，派工 agent（opencode / big-pickle 免費模型）唯讀閱讀原始碼，2026-11-07。主 agent 核對了環形圖實作（`Pie.vue` 用極座標 bar）、容器概覽無圖表、GPU 現況只用 `el-progress` 三項關鍵結論。報告是線索，實作前以固定 commit 重新核對。

未修改任何檔案（`git status` 乾淨），以下全部是唯讀查證。

# 1. 首頁 `frontend/src/views/home/`

## 各檔案負責的區塊

- `frontend/src/views/home/index.vue:35-73`：頂部「快速跳轉」卡，資料來自 `baseInfo.quickJump`，最多 4 個數字（`quick/index.vue:58-65` 限制 1 到 4 個）。
- `frontend/src/views/home/index.vue:74-78`：系統狀態環形圖列，掛載 `SystemStatus`（即 `status/index.vue`）。
- `frontend/src/views/home/index.vue:79-164`：「監控」卡，網路／磁碟 I/O 二選一的即時折線圖。
- `frontend/src/views/home/index.vue:166-425`：右欄輪播卡，三個頁面依序是系統資訊（176-328）、備忘錄（329-378）、多節點面板（379-424）。
- `frontend/src/views/home/index.vue:427`：右欄下方 App 啟動器 `AppLauncher`（`app/index.vue`）。
- `frontend/src/views/home/index.vue:433-435`：歡迎頁 Dialog，內容由 `/auth/welcome` 取得（1124-1137）。
- `frontend/src/views/home/status/index.vue:2-474`：狀態環形圖列，含負載、CPU、記憶體、每顆磁碟、GPU／NPU／XPU，以及各自的 hover popover 與 Top 程序表。
- `frontend/src/views/home/app/index.vue:1-140`：App 啟動器卡片列表，提供安裝、啟動、停止、重啟與顯示設定。
- `frontend/src/views/home/quick/index.vue:1-86`：快速跳轉的顯示設定抽屜，逐項開關。
- `frontend/src/views/home/quick/directories.vue:1-105`：首頁「檔案」數字點開後的常用目錄管理 popover（新增、改名、刪除、跳轉）。

## 狀態環形圖用哪種 series、option 怎麼設

- 呼叫端傳 `type="pie"`（`home/status/index.vue:44-50`、`147-153`、`243-249`、`300-307`、`349-356`、`404-411`、`447-454`），但元件的實作不是 `pie` series，而是「極座標上的 `bar` + 一顆遮住中心的 `pie`」。
- 極座標與刻度：`frontend/src/components/v-charts/components/Pie.vue:101-121`，`polar.radius: ['71%','80%']`、`angleAxis.max: 100`、`radiusAxis` 隱藏刻度。
- 主體 series：`Pie.vue:122-148`，`type: 'bar'`、`coordinateSystem: 'polar'`、`roundCap: true`、`barWidth: 30`、`showBackground: true`，背景色為主題色 5% 透明（`Pie.vue:57`）。
- 中心遮罩：`Pie.vue:149-166`，`type: 'pie'`、`radius: ['0%','60%']`，用背景色圓形蓋住中央，做出空心環效果。
- 顏色：沒有顏色門檻。所有環形都用同一組主色漸層，從 `--panel-color-primary-light-3` 到 `--panel-color-primary`（`Pie.vue:46-59`、`132-143`）。資料超過門檻不會變紅或變黃，**顏色門檻未找到**。
- 中央文字：`Pie.vue:73-100`，用 `title` 的 rich text 切成整數與小數兩段（`{a|xx.}{b|y %}`），整數 22px、小數與 `%` 14px，`left: '49%'`、`top: '32%'`、`textAlign: 'center'`，下方 `subtext` 顯示資源名稱（CPU／記憶體／負載／掛載點）。
- 傳入的 option 只有 `{ title, data }`：`status/index.vue:587-598`（CPU、記憶體、負載）與 `604-629`（磁碟、GPU、NPU、XPU）。
- 數值來源：CPU 用 `cpuUsedPercent`、記憶體用 `memoryUsedPercent`、負載用 `loadUsagePercent`、磁碟用 `usedPercent`、GPU 用 `gpuUtil`、NPU 用 `aiCore`、XPU 用 `gpuUtil` 或 `memoryUtil`（`status/index.vue:587-628`）。
- 負載的文字狀態另有門檻：`<30` 順暢、`<70` 正常、`<80` 緩慢、其餘壅塞（`status/index.vue:680-691`）。

## 即時折線圖的資料來源、更新間隔、dataZoom

- 資料 API：`GET /dashboard/current/{ioOption}/{netOption}`（`frontend/src/api/modules/dashboard.ts:25-33`），首屏基底資料是 `GET /dashboard/base/{ioOption}/{netOption}`（同檔 19-23）。
- 更新間隔：掛載後先等 500ms 取第一次差值樣本，之後每 3000ms 打一次 `loadCurrentInfo`（`home/index.vue:799-816`）；分頁失焦時暫停（`1083-1089`）。
- 速率算法：用兩次快照的位元組差除以 `shotTime` 時間差，網路換成 KB/s、I/O 換成 MB/s（`home/index.vue:851-910`）。
- 保留點數：每個序列固定保留最近 20 點，超過就 `splice(0,1)`（`home/index.vue:875-878`、`884-887`、`893-896`、`902-905`、`912-919`）。
- option 組裝：`home/index.vue:505-536`，`xData` 是時間字串、`yData` 是上傳／下載或讀／寫兩條線，`grid` 固定 `{left:65,right:65,bottom:'20%'}`，`legend` 在頂部，`formatStr` 分別是 `KB/s` 與 `MB`。
- 卡片上方另放即時數值標籤：網路顯示 up／down／累計送出／累計接收，I/O 顯示讀／寫／每秒次數／延遲（`home/index.vue:124-142`）。
- dataZoom：呼叫端傳 `:dataZoom="true"`（`home/index.vue:150`、`159`）。`Line.vue:312-321` 產生單一 `dataZoom`，`left`／`right` 跟 `grid` 對齊，`show` 綁 props，`startValue` 取 `xAxis.min` 或 `xData[0]`。因為只有 20 點，預設等於全部顯示，實務上幾乎不需要拖動。
- 折線樣式：`Line.vue:42-105` 的漸層 `seriesStyle`，第 1、2 條線用不同漸層面積；`Line.vue:230-322` 集中處理 tooltip、legend、xAxis、yAxis，tooltip 依 `formatStr` 轉 KB/s、KB、MB 或百分比（`177-227`）。

# 2. 多節點概覽

- 位置：首頁右欄輪播的第三張卡，`frontend/src/views/home/index.vue:379-424`，標題是「面板」（`setting.panel`）。
- 顯示條件：`simpleNodeCarouselSetting === 'Enable'`、`isXpackOrEE` 為真、節點清單非空（`home/index.vue:828-830`）；設定值來自 `/settings/base`（`1038-1063`），開關寫回 `DashboardSimpleNodeVisible`（`971-990`）。
- 資料 API：`GET /core/nodes/simple/all`（`frontend/src/api/modules/setting.ts:49-51`），載入函式在 `home/index.vue:740-747`，跟著 3 秒刷新迴圈一起更新（`806-808`）。
- 顯示欄位：節點名稱 `row.name`、狀態 `row.status` 與訊息 `row.message`（`home/index.vue:396-399`，狀態由 `components/status/index.vue:1-30` 的 `el-tag` 呈現）、CPU 核心數與使用率、記憶體總量與使用率（`loadSource`，`home/index.vue:1065-1081`）、右側「訪問」按鈕（`406-417`）。
- 訪問行為：把節點 `addr` 加上 `securityEntrance` 組成的網址用新分頁開啟（`jumpPanel`，`home/index.vue:844-849`），不是站內路由。
- 每個節點是一列 `el-row`，最多 21 格名稱與詳情、1 格訪問按鈕，列間用 `.h-app-divider` 分隔（`393-420`）。
- 不確定：前端 interface `Setting.SimpleNodeItem` 只宣告 `id/name/addr/description/systemVersion/securityEntrance/cpuUsedPercent/cpuTotal/memoryTotal/memoryUsedPercent`（`frontend/src/api/interface/setting.ts:330-341`），沒有 `status`、`message`，但模板有使用。可能是後端多回傳欄位或 interface 未同步，後端 simple node 端點不在這個 repo 內，無法確認。

# 3. 主機監控頁 `frontend/src/views/host/monitor/`

- 頁面入口：`frontend/src/views/host/monitor/index.vue:13-26` 三個分頁，分別是監控、匯出、設定。
- 圖表清單（全部是折線圖 `type="line"`）：
  - 平均負載：`monitor/index.vue:43-50`，四條線（load1、load5、load15、資源使用率），雙 Y 軸（`474-512`）。
  - CPU：`monitor/index.vue:75-82`，單線，`formatStr: '%'`（`515-539`）。
  - 記憶體：`monitor/index.vue:105-112`，單線（`541-565`）。
  - 磁碟 I/O：`monitor/index.vue:156-163`，四條線（讀、寫、讀寫次數、讀寫時間），雙 Y 軸（`610-689`），可切換裝置（`123-140`、`401-409`）。
  - 網路：`monitor/index.vue:205-212`，上傳／下載兩條線（`567-608`），可切換網卡（`172-189`）。
- GPU：主監控頁**沒有** GPU 圖表；只有「匯出」頁的匯出項目有 GPU 選項（`frontend/src/views/host/monitor/export/index.vue:30-35`、`62-70`）。GPU 歷史圖表在 `frontend/src/views/ai/gpu/history/index.vue`。
- 時間範圍：每張卡都有自己的 `datetimerange` 日期選擇器（`monitor/index.vue:29-40`、`61-72`、`91-102`、`142-153`、`191-202`），另外最上方有一個全域時間範圍（`8-21`），改全域會同步五張卡（`324-331`）。預設是今天 00:00 到現在（`234-239`）。
- 快捷區間：今天、昨天、近 3／7／30 天、近 3 個月、近半年（`frontend/src/utils/shortcuts.ts:3-62`）。
- 資料 API：`POST /hosts/monitor/search`（`frontend/src/api/modules/host.ts:9-16`），參數是 `{param, io, network, startTime, endTime}`（`api/interface/host.ts:85-91`）；網卡與 I/O 裝置選項分別是 `GET /hosts/monitor/netoptions`、`GET /hosts/monitor/iooptions`（`api/modules/host.ts:17-30`）。
- Tooltip 會依序插入當下時間點的 Top CPU／Top 記憶體程序表（`monitor/index.vue:706-799`）。

# 4. 容器概覽與容器監控

## `frontend/src/views/container/dashboard/index.vue`

- 顯示統計：
  - 容器總數與各狀態數量：全部、執行中、已建立、暫停、重啟中、移除中、已結束、Dead（`11-41`）。
  - 六張統計卡：Compose、Compose 範本、映像、映像倉庫、網路、儲存卷（`42-89`）。
  - 磁碟用量：映像、容器、本地儲存卷、建置快取的使用量與可回收量（`91-175`）。
  - Docker 設定：socket 路徑與 registry mirrors（`176-195`）。
- 有沒有圖表：**沒有**。全頁沒有引用 `v-charts`，用量區是 `el-descriptions` 文字（`93-173`），狀態數量是 `el-tag`（`15-40`）。
- 資料 API：`GET /containers/status`（`api/modules/container.ts:49-55`）、`POST /containers/item/stats`（`86-94`）、`GET /containers/daemonjson` 類的 `loadDaemonJson`（`frontend/src/views/container/dashboard/index.vue:297-303`）。

## `frontend/src/views/container/container/monitor/index.vue`

- 圖表：四張折線圖，都是 `type="line"`、`renderer="svg"`、高 200px。
  - CPU（`20-29`）
  - 記憶體與 cache（`30-39`、`173-187`）
  - 磁碟 I/O 讀寫（`40-49`、`189-203`）
  - 網路 up／down（`50-59`、`205-219`）
- 更新間隔：可選 3／5／10／30／60 秒（`10-18`），開啟時預設 5 秒（`96`），用 `setInterval` 輪詢（`99-105`、`117-124`）。
- 資料來源：`GET /containers/stats/{containerID}`（`api/modules/container.ts:102-104`），欄位是 `cpuPercent/memory/cache/ioRead/ioWrite/networkRX/networkTX/shotTime`（`api/interface/container.ts:189-198`）。
- 與首頁即時圖的差異：固定保留 20 點（`128-159`）、**沒有** dataZoom、沒有雙 Y 軸、沒有 top 程序表。

# 5. GPU 監控 `frontend/src/views/ai/gpu/`

- 入口：`index.vue:13-22` 兩個分頁，「目前」與「歷史」。
- 「目前」頁 `current/index.vue`：
  - 頂部概覽：驅動版本、CUDA 版本、資料收集時間、裝置數量文字（`10-25`、`425-438`）。
  - 每張裝置卡欄位：利用率（NVIDIA 是 gpuUtil、Ascend 是 AICore）、記憶體使用量與百分比、溫度、功耗、頻率／aiCPUUtil、風扇轉速或健康狀態（`86-149`）。
  - 圖表：**沒有 ECharts**，只有 `el-progress` 進度條（`92-97`、`102-107`、`202-225`）。
  - 另有「查看」抽屜，用 key-value 區塊列出執行資訊、記憶體、引擎利用率、ECC、裝置資訊（`253-266`、`501-722`，呈現元件 `current/components/DeviceDetails.vue:1-30`），以及程序清單 Dialog（`267-303`）。
  - 更新方式：只有手動重新整理，沒有輪詢（`475-490`、`872-874`）。
  - API：`GET /ai/gpu/load`（`api/modules/ai.ts:41-43`）。
- 「歷史」頁 `history/index.vue`：
  - 圖表：全部是折線圖，`v-charts`、高 320px、`dataZoom`（`58-65`）。
  - 圖表定義 15 組（`116-227`）：利用率、記憶體、功耗、溫度、頻率、風扇、程序數、記憶體活動、引擎利用率、記憶體頻寬、NPU CPU、DDR、HBM、Hugepages、風扇 RPM。只會渲染有資料的組（`229-299`）。
  - 控制項：時間範圍、裝置下拉、`avg`／`max` 聚合切換（`6-27`）。
  - API：`GET /ai/gpu/options`、`POST /ai/gpu/search`（`api/modules/ai.ts:44-58`）。
- 不確定：歷史頁的裝置型別（npu、xpu、legacy）會影響標題與欄位名稱，實際後端回傳哪些欄位未逐一比對後端程式。

# 6. 共用圖表元件

- 目錄：`frontend/src/components/v-charts/`，只有兩個子元件：`components/Line.vue`、`components/Pie.vue`（`index.vue:12-13`、`35`）。
- 外層 `v-charts/index.vue` props（`15-33`）：
  - `type`：`'line'` 或 `'pie'`，預設 `line`
  - `height`：字串，預設 `200px`
  - `dataZoom`：Boolean
  - `renderer`：字串，可傳 `canvas` 或 `svg`
  - `option`：必填物件
- `Line.vue` 自己的 props（`15-40`）：`id`、`width`（預設 100%）、`height`、`dataZoom`、`renderer`、`option`。
  - `option` 接受的鍵：`xData`、`yData`（陣列，每項 `{name,data,itemStyle,areaStyle,showSymbol,symbolSize,yAxisIndex}`）、`xAxis`、`yAxis`、`grid`、`legend`、`tooltip`、`title`、`titleLeft`、`formatStr`（`KB/s`、`KB`、`MB` 或單位字串）。
- `Pie.vue` 自己的 props（`12-29`）：`id`、`width`、`height`、`option`，`option` 只接受 `{title, data}`，`data` 是 0 到 100 的數字。
- 量表（gauge）：**未找到**。`utils/echarts.ts:3-35` 只註冊 `LineChart/BarChart/PieChart/MapChart` 與對應元件，`v-charts` 也沒有 gauge 封裝。`BarChart` 有註冊，但唯一使用處是 `Pie.vue:124` 的極座標 bar。

# 7. 首頁「數字可點擊跳轉」的做法

- 快速跳轉數字：`home/index.vue:50-70`。`baseInfo.quickJump` 每項有 `name/title/detail/router`（`api/interface/dashboard.ts:17-26`），非檔案項目用 `el-button link type="primary"` 包住 `item.detail`，點擊呼叫 `quickJump(item)`，再 `routerToPath(item.router)`（`home/index.vue:824-826`）。
- 可跳轉的項目與權限：Agent、Website、Database、Cronjob、AppInstalled、File，各自對應 `ai_agent_view`、`website_view`、`database_view`、`cronjob_view`、`app_view`、`host_file_view`；沒權限就 disable（`home/index.vue:571-584`、`59-68`）。
- 「檔案」數字是特例：不是直接跳頁，而是開 `QuickDirectories` popover（`home/index.vue:53-58`）。popover 內每一列有跳轉圖示，點擊 `routerToFileWithPath(row.savedPath)` 跳到檔案頁並帶 `path` 參數（`quick/directories.vue:76-85`、`utils/router.ts:26-28`）。
- 狀態環形圖也可點：
  - 磁碟環點擊跳檔案頁該掛載路徑（`home/status/index.vue:300-307`）。
  - GPU、NPU、XPU 環點擊都呼叫 `goGPU()`，跳到 `routerToName('GPU')`（`349-356`、`404-411`、`447-454`、`693-695`）。
- 容器概覽的數字：`container/dashboard/index.vue:13`（容器 → `ContainerItem`）、`46`（Compose）、`53`（ComposeTemplate）、`62`（Image）、`71`（Repo）、`78`（Network）、`85`（Volume），全部是 `span.count` 加 `@click` 呼叫 `routerToName`。
- 樣式做法：首頁數字用 `.count .el-button { font-size: 18px; line-height: 32px }`（`home/index.vue:1249-1256`），容器頁用 `.count` 的 25px 主色文字加 `cursor: pointer`（`container/dashboard/index.vue:351-358`）。

# 8. 值得保留與可圖形化的判斷

## 值得保留的三個圖形化做法

- 環形進度加中央大字與 hover 下鑽。`status/index.vue:44-52` 的環形佔位小，滑過就能看到完整明細與 Top 程序表，適合首頁一次掃多個資源。實作在 `Pie.vue:73-100` 的中央 rich text 與 `122-167` 的極座標環。
- 即時折線的滾動視窗加 tooltip 附表。`home/index.vue:875-919` 固定 20 點，`Line.vue:177-227` 的 tooltip 可直接列出當下 Top 程序；`host/monitor/monitor/index.vue:706-799` 把同一招用在歷史資料。缺點是首頁 3 秒輪詢與 20 點上限，適合「看趨勢」不適合「查歷史」。
- 一圖一時間範圍加雙 Y 軸。`host/monitor/monitor/index.vue:29-40` 每張卡各自選時間，`474-512` 與 `631-688` 用右軸放使用率、次數或延遲。這種配置讓使用者在同一頁追不同時段的不同指標，不必整頁重查。

補充：GPU 歷史頁 `history/index.vue:116-300` 用「定義清單 + 有資料才長圖」的做法也值得保留，新增指標只要加一組定義。

## 三個以文字表格為主、可以圖形化的頁面

- `frontend/src/views/container/dashboard/index.vue:91-175`：映像、容器、儲存卷、建置快取的使用量與可回收量目前是四格文字。可改成堆疊長條或環形，直接看出「已用／可回收／剩餘」。
- `frontend/src/views/ai/gpu/current/index.vue:86-149` 加 `current/components/DeviceDetails.vue:1-30`：裝置卡與明細抽屜大量使用 key-value 文字，只有兩條 `el-progress`。可把利用率、記憶體、溫度、功耗改成儀表或 sparkline，明細抽屜的 ECC、引擎利用率、頻寬也適合長條圖。
- `frontend/src/views/container/container/index.vue:181-208`：容器列表只在文字行顯示 `CPU: x%`、`Memory: y%`，展開後是 `el-descriptions` 的數值。可加行內迷你長條或環形，讓幾十個容器的高低差一眼看得出來。

# 未驗證項目

- 沒有啟動前端或執行 build／dev server，所有渲染結果、互動與深色主題表现都只根據原始碼推斷。
- 沒有逐一比對後端 API 的回傳欄位，特別是 simple node 的 `status`、`message`，以及 GPU 歷史各裝置型別的欄位。
- 沒有確認 `frontend/src/views/host/monitor/export/index.vue` 匯出的實際檔案內容，只讀了表單與 API 呼叫。
- 沒有測試 `dataZoom` 在只有 20 點時的使用者體驗，推論是幾乎用不到。
- 沒有查證 `Pie.vue` 在資料超過 100 或為負值時的行為，元件沒有做 clamp。

Skills：human-writing

## 本輪經驗

無。