# 缺口盤點與「圖形為主」改版（2026-11-07）

對照對象：規格 v1.1 第 7 章的主導覽、我們的 12 個定稿畫板、七份參考專案研究（`docs/research/`）、以及 Komodo 官方截圖、Portainer／Datadog／Coolify／Beszel／Cockpit 的公開畫面。產品功能都還沒實作，這裡盤的是「畫面該有什麼」。

## 一、規格有、畫面還沒有

主導覽 19 項，第一輪只畫了 4 項的畫面。第二輪補了 3 項，第三輪（2026-11-07 晚）把剩下 12 項全部畫完，另加宿主機系統、終端機、日誌三個 1Panel／Webmin 必要畫面。下表「畫面狀態」保留當時狀態，括號內是第三輪的畫板。

| 導覽項 | 規格章節 | 畫面狀態 | 建議的第一屏形狀 |
|---|---|---|---|
| 總覽 | 07 | 第二輪重做 | 艦隊卡、流量拓撲、容器地圖 |
| 主機與節點 | 09、10、29 | 第二輪重做（詳情頁）；第三輪新增列表頁（Nodes）與宿主機系統頁（System） | 艦隊卡牆、跨節點資源尺、引擎與 Port 矩陣、主機工具卡；System：套件更新、systemd、使用者、網路、掛載 |
| 專案與服務 | 15、18 | 第二輪重做 | 服務拓撲、部署結果時間軸 |
| Docker／Podman | 14 | 第二輪重做；第三輪補映像段條、網路／儲存卷／登錄站矩陣、Compose 專案 | 容器地圖＋映像、網路、儲存卷 |
| Kubernetes | 17 | 第三輪新增（Kubernetes） | 叢集拓撲、資源保留環、副本量表、PVC 段條、升級流程段 |
| 應用商店 | 16 | 第三輪新增（AppStore） | 應用卡量表、升級差異表、目錄卡、安裝預檢流程段 |
| 網站與入口 | 19、21、22 | **本輪新增** | 入口拓撲、憑證到期橫條、訪客 IP 判定段條 |
| API 代理 | 20 | 第三輪新增（APIProxy） | 使用者→服務→上游拓撲、Token 效期橫條、被拒排行 |
| 資料庫 | 24 | 第三輪新增（Database） | 實例卡三環、連線圖、慢查詢排行、容量段條、結構樹、Redis 環 |
| AI | 25 | 第三輪新增（AI） | 服務卡環＋預檢四段、模型量表、下載流程段、測試對話、MCP／Agent |
| 檔案 | 12 | 第三輪新增（Files） | 樹（大小）、編輯歷史 diff、傳輸流程段、權限矩陣 |
| 儲存 | 13 | **本輪新增（與備份合併）** | 資料碟段條、儲存卷排行 |
| 備份與還原 | 26、27 | **本輪新增（與儲存合併）** | 備份日曆、驗證流程段、目的地量表 |
| 計畫任務 | 28 | 第三輪新增（Cron） | 任務×小時熱格、結果時間軸、依賴拓撲、執行流程段、腳本庫 |
| 安全防護 | 21 | 第三輪新增（Security） | 逐站模式矩陣、發布流程段、到期橫條、誤判檢查、主機層矩陣 |
| 流量分析 | 23 | 第三輪新增（Analytics） | 三觀測點並排段條、三線延遲圖、來源段條、用量熱格、異常→政策 |
| 事件與告警 | 29 | **本輪新增** | 規則 × 24 小時狀態時間軸、事件熱格、告警路由拓撲 |
| 身分與存取 | 05 | 第三輪新增（IAM） | 角色×資源矩陣、Token 效期橫條、工作階段、稽核時間軸 |
| 平台設定 | 07.3、37 | 第三輪新增（Settings） | 設定卡、更新流程段、元件版本表、整合矩陣 |

第三輪另加：終端機（Terminal：多分頁、身分橫幅、稽核政策）、日誌（Logs：稽核／登入／工作／journald／SSH／網站）。仍沒有獨立畫面的：批次操作結果（UI-009）、全域搜尋結果（UI-004）、原始 Compose 編輯器全畫面（UI-008，網站設定與檔案頁有入口）。

## 二、參考專案有、我們沒有（或做得不夠）

| 做法 | 誰在用（證據） | 我們的狀態 |
|---|---|---|
| 首頁每種資源一個環形狀態摘要＋最近項目卡 | Komodo `ui/src/components/dashboard-summary/index.tsx`、`pages/dashboard/recents.tsx` | 總覽的 KPI 格與艦隊卡涵蓋節點與專案；容器、備份、站點的「狀態數量摘要」沒有獨立環 |
| 狀態意圖集中映射（Good／Warning／Critical），圖示、徽章、tab 共用 | Komodo `ui/src/lib/color.ts` | DESIGN.md 已定義形狀＋顏色語意；實作時要有同一個 `intent()` 函式，不在元件裡各自判斷 |
| 首頁狀態環 hover 展開 Top 程序表、數字可點跳轉 | 1Panel `views/home/status/index.vue`、`home/index.vue:50-70` | 艦隊卡可點；環的 hover 下鑽沒有做（畫布無法表達，實作時加） |
| 環境卡：狀態＋群組＋版本＋容器數＋CPU／記憶體一列看完 | Portainer `HomeView/EnvironmentList/EnvironmentItem/EnvironmentCard.tsx` | 艦隊卡等價，且多了三環與走勢 |
| 首頁上方彩色分段列（Up／Down／Outdated 可點篩選） | Portainer `components/StatusSummaryBar` | 沒有；可加在艦隊區標題列，點段落篩選卡片 |
| 歷史圖延遲載入、時間粒度與顯示狀態記住 | Komodo `resources/server/stats/historical.tsx:51-95` | 畫面上有時間範圍列；「記住上次選擇」要寫進前端票 |
| 跨 Server 比較：每台一條線、固定 8 色 | Komodo `pages/stats.tsx:53-65` | 叢集 CPU 圖只放 3 台；超過 4 台折成「其他」是我們的規則，需在規格確認上限 |
| 主機地圖（hex／格，顏色＝指標，群組） | Datadog Host Map | 本輪新增容器地圖；節點數多時可升級成節點地圖 |
| Git 來源卡：repo、ref、commit、webhook、輪詢、分歧提示集中 | Portainer `gitops/GitReferenceCard.tsx` | GitHub 來源選擇器有選擇流程，沒有「目前綁定狀態」卡；專案頁該加 |
| 多分頁浮動終端機、背景保持 | 1Panel `components/terminal` | 未畫；列為終端機畫面的必要條件 |
| 檔案編輯器內建歷史版本 diff | 1Panel `host/file-management/code-editor/history` | 未畫；檔案頁要有 |
| 日誌檢視器：stdout／stderr、tail、搜尋、反向、時間戳、ANSI | Komodo `components/log-section.tsx` | 未畫；工作中心「看日誌」要有同等能力 |
| 更新詳情用 Monaco diff | Komodo `components/updates/details.tsx` | 部署檢查頁有差異區塊，格式未定 |

參考專案共同的弱點，我們不要學：Docker dashboard 只有數字卡沒趨勢（Portainer）、容器列表狀態只有文字標籤（Portainer）、容器概覽與 GPU 現況全是文字（1Panel）、環形不隨門檻變色（1Panel）、沒有拓撲或依賴圖（三者皆無）、沒有 GPU 與溫度（Komodo）。

## 三、「圖形為主」的決定

規格 7 章說「專業感不是堆滿圖表」，和圖形為主不衝突：圖形必須是狀態與結構的編碼。規則已寫進 `DESIGN.md`「圖形為主」節，摘要：

1. 第一屏用形狀回答「現在怎麼了」，數字在旁邊，表格降到第二層但不刪（鍵盤與螢幕閱讀器仍要它）。
2. 同一份資料三層視角：總覽用卡與地圖、列表頁用表格加內嵌量表、詳情頁用時間圖。
3. 形狀帶狀態語意（● ○ ■）配文字；地圖格、拓撲節點、日曆格都用同一套。
4. 圖形元素都是連結。
5. 沒資料不畫：離線用虛線框，未排程用淡格。

新增的圖形元件：ring、node card、host map、topology、tall bar、calendar、pipeline（規格在 DESIGN.md，實作在 `docs/design/tools/hxui.py`）。序列色沿用第一輪驗證過的四色，單色序列沿用榛果五階，沒有新色。

## 四、這輪改了什麼

| 畫面 | 改動 |
|---|---|
| 總覽 | 節點表 → 艦隊卡；新增「流量怎麼走」五欄拓撲、容器地圖；專案表 → 專案卡（內嵌部署時間軸）；儲存區改磁碟段條＋備份日曆 |
| 節點詳情 | 頂部新增七個環（CPU、記憶體、兩磁碟、GPU 核心、顯示記憶體、GPU 溫度）＋負載數字＋容器段條 |
| 容器 | 頂部新增容器地圖，表格保留 |
| 專案與服務 | 新增服務拓撲（入口 → 服務 → 資料 → 儲存） |
| 工作中心 | 部署工作改用流程段（六階段、時長、備註） |
| 網站與入口 | 新畫面 |
| 儲存與備份 | 新畫面 |
| 事件與告警 | 新畫面 |

驗證：15 個畫板深淺色與 360px 皆以 Playwright 渲染，零 console 錯誤、無橫向溢出；截圖在 `docs/design/screenshots/screens/`。

## 五、還沒做、要注意

- 12 個導覽項沒有畫板（第一節表）。建議順序：主機列表 → Kubernetes → 資料庫 → AI → 計畫任務 → 安全防護 → 流量分析 → 應用商店 → 檔案 → API 代理 → 身分 → 設定。
- 拓撲在畫布裡是靜態 SVG；實作時節點數會變，要用自動佈局（dagre 類）並保留「清單檢視」給手機與螢幕閱讀器。
- 容器地圖格 26px（手機 36px）小於 44px 觸控目標；地圖是快速掃描用，表格才是主要操作路徑，這點要寫進票 005 的驗收。
- 環的 hover 下鑽、分段列點擊篩選、時間範圍記憶，畫布表達不了，實作票要補。
- 示例資料全是假設；數字只為了版面。


## 六、1Panel／Webmin 功能 → 我們的畫板（最低基準對照）

| 1Panel／Webmin 功能 | 畫板 | 備註 |
|---|---|---|
| 首頁環形狀態、即時折線、快速跳轉、App 啟動器、多節點卡 | Overview、Nodes | 環在艦隊卡；快速跳轉＝KPI 格與卡片都可點 |
| 應用商店：全部／已安裝／可升級／設定、安裝抽屜、升級 diff、忽略升級、Port 衝突檢查 | AppStore | 升級差異表、安裝預檢流程段含 Port 檢查 |
| 網站：列表、SSL、範本、運作環境、站點設定 14 分頁、日誌、原始碼 | Websites、Logs | 設定分頁以矩陣列出目前值 |
| 資料庫：MySQL／PG／Redis／Mongo、遠端、帳號權限、連線資訊、參數、慢查詢、Redis CLI、持久化 | Database | Redis CLI 入口在實例卡；SQL 主控台在標題列 |
| 容器：概覽、容器、Compose、映像、網路、儲存卷、登錄站、監控、終端、檔案瀏覽 | Containers、Terminal、Files | |
| 主機：檔案管理（含編輯器歷史、回收筒、分享、壓縮、wget）、監控、防火牆、磁碟、程序、SSH | Files、NodeDetail、Nodes、Storage、System | 防火牆與 Docker 直通 Port 同一張矩陣 |
| 終端機：多分頁、Dock、主機連線、快捷指令、設定 | Terminal | 加上稽核政策與「代表誰」橫幅 |
| 計畫任務：任務、執行紀錄、備份檔還原、匯入、日誌清理、腳本庫 | Cron、Storage | |
| 工具箱：時區、Swap、NTP、主機名稱、Hosts、DNS、root 密碼、磁碟清理、Supervisor、ClamAV、FTP、Fail2ban | Nodes（主機工具）、System、Security | |
| 日誌：操作、登入、系統、工作、主機系統、SSH、網站 | Logs、IAM | |
| 面板設定：面板、安全、告警、備份帳號、快照、授權、API 金鑰、關於 | Settings、Events、Storage、IAM | |
| Webmin：套件管理、systemd、使用者群組、網路介面／路由／DNS、掛載 fstab、NFS／SMB、時間、開關機 | System | 全部在同一頁的分頁 |
| Webmin：排程、cron | Cron | |
| Webmin：磁碟配額、RAID／LVM／LUKS | Storage（規格 STO-003） | 第三輪未畫操作流程，只有現況 |
