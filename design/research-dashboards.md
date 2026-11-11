# 現代 dashboard 與控制台的圖表做法（票 002 研究筆記）

調查日期：2026-11-07。目的：決定 HazelminX 各畫面要放哪些圖表、怎麼放。來源分三類：參考專案原始碼（`scratchpad/refs`）、官方文件、第三方整理。第三方整理只當線索。

## 各產品的實際做法

| 產品 | 看到的做法 | 出處 |
|---|---|---|
| Beszel | 系統列表每列內嵌 CPU／記憶體／磁碟量表（meter），門檻 65% 警告、90% 嚴重；系統詳情頁兩欄圖表格（`xl:grid-cols-2`），面積圖單色、`monotoneX`；時間範圍 1 分鐘（即時）／1 小時／12 小時／24 小時／1 週／30 天；圖表無資料時有專用狀態元件 | `refs/beszel/internal/site/src/components/systems-table/systems-table-columns.tsx:94`、`components/charts/area-chart.tsx`、`lib/utils.ts:130` |
| Coolify | 機隊頁 = 摘要數字＋趨勢圖（CPU／記憶體／磁碟／網路／負載）＋可排序伺服器表＋「最熱容器」排行；表格數值依壓力標示（CPU／記憶體 ≥70%、磁碟 ≥80%、≥90% 最強）；tooltip 同時顯示本地時間與 UTC；範圍 5 分／10 分（即時）／30 分／1 小時／12 小時／1 週／30 天；只顯示保留期內的歷史 | [coolify.io metrics 文件](https://coolify.io/docs/core/observability/monitoring/metrics) |
| Railway | 每個服務的 CPU／記憶體／磁碟／網路時間圖上以虛線標出部署時間，讓尖峰對得上 commit；多副本可切換「加總」與「逐副本」 | [docs.railway.com metrics](https://docs.railway.com/guides/metrics) |
| Netdata | 每張圖有標題列（標題、指標、單位、狀態 icon）、異常率色帶、維度圖例可依名稱／數值排序；hover 顯示每個序列的數值與「部分資料／計數器溢位／空資料」旗標；hover 暫停、點擊鎖定時間 | [learn.netdata.cloud charts](https://learn.netdata.cloud/docs/dashboards-and-charts/netdata-charts) |
| Grafana 官方 | 由總覽往下鑽的階層；每個服務一列（左請求與錯誤率、右延遲），列的順序照資料流；CPU 以核心數正規化成百分比；堆疊圖多數情況關掉；只顯示需要注意的實體；避免不必要的自動更新 | [grafana best practices](https://grafana.com/docs/grafana/latest/dashboards/build-dashboards/best-practices/) |
| Grafana 視覺化清單 | Stat（單一數值可附 sparkline）、Bar gauge（值在區間內的位置）、State timeline（離散狀態隨時間）、Status history、Heatmap、Histogram | [grafana visualizations](https://grafana.com/docs/grafana/latest/panels-visualizations/visualizations/) |
| Datadog | 時間序列至少佔 4 欄，否則被壓扁；Query value 要有時間序列背景（如長條）而不是空白數字；用 Group 當章節標題；事件流放最後避免困住捲動；Top list 找最吃資源的主機；Heatgrid 一次掃很多主機狀態；Change 顯示相對前一期的變化 | [datadog widgets](https://docs.datadoghq.com/dashboards/widgets/?lang_pref=en)、[widget types](https://docs.datadoghq.com/dashboards/widgets/types.md) |
| Percona（Grafana 實務） | 筆電尺寸一列 2–3 張圖；每張圖設單位、小數固定一位；重要序列固定顏色，同類資料跨面板用同色；平均值與最大值一起畫；避免雙 Y 軸；量級差很多的指標拆圖 | [percona designing grafana dashboards](https://www.percona.com/blog/designing-grafana-dashboards) |
| Proxmox | 節點摘要頁把 CPU、負載、記憶體、網路畫在一起，時間 1 小時到 1 年；使用者抱怨改版後垂直空間太大、記憶體圖被擠出 1080p 畫面 | [pve-user 2016 討論](https://lists.proxmox.com/pipermail/pve-user/2016-April/010289.html) |
| Vercel Observability | 查詢結果可選折線、量（volume）、表格、大數字四種呈現 | [vercel changelog](https://vercel.com/changelog/new-custom-visualization-in-vercel-observability) |
| 1Panel | 首頁用 ECharts，只有 line／bar／pie 各一處 | `refs/1panel/frontend/src/views/home/` |

其他搜尋來源（只做背景）：[Grafana 24 欄網格與面板數建議](https://www.metricfire.com/blog/7-best-practices-for-grafana-dashboard-design)、[sparkline 在表格中的用法](https://aws.amazon.com/about-aws/whats-new/2026/04/quick-sparklines-tables)、[Dokploy 監控](https://docs.dokploy.com/en/docs/core/monitoring/overview)、[Beszel 介紹](https://beszel.dev/guide/what-is-beszel)。

## 從中抽出的規則（寫進 DESIGN.md「資料視覺化」節）

1. **數字先於圖**：單一數值用 stat tile（數值＋變化＋12 點 sparkline），不是一根長條；一個比例對上限用量表（meter），不是兩片圓餅。
2. **表格內嵌量表與 sparkline**（Beszel、Coolify）：節點表、容器表每列放 CPU／記憶體／磁碟量表，門檻 70／90。sparkline 只放一欄（CPU 1 小時），其他趨勢進詳情頁。
3. **詳情頁兩欄圖表格**（Beszel、Percona）：1440 寬一列兩張，每張最少 4 欄寬；360 寬一列一張。
4. **時間範圍是一列、在圖表之上、掃全部圖**（dataviz skill、Coolify）：5 分（即時）／1 小時／12 小時／24 小時／7 天／30 天。只顯示保留期內的歷史，缺的部分畫空。
5. **部署標記線**（Railway）：時間圖上用細直線標部署、重啟、維護事件，hover 顯示 commit 短碼。
6. **狀態時間軸**（Grafana state timeline）：工作與部署歷史用橫向狀態條，不用折線。
7. **最熱排行**（Datadog Top list、Coolify）：容器與服務用排序長條清單找最吃資源的，不用多序列折線。
8. **熱格**（Datadog Heatgrid、Grafana heatmap）：容器 × 小時的 CPU 用單色深淺格，掃出哪個容器哪個時段熱。
9. **CPU 正規化成百分比**（Grafana）：顯示「100% = 所有核心」，並在圖旁寫核心數。
10. **部分資料要標**（Netdata）：過期、取樣、缺段都在圖上標，不補假線。流量分析顯示「資料完整度」與取樣比例。
11. **一張圖一個 Y 軸**（Percona、dataviz skill）：讀寫、進出量級差很多就拆成兩張或共用索引。
12. **序列顏色固定跟著實體**（Percona、dataviz skill）：tw-01 永遠是第一色，篩掉其他節點也不重新上色。
13. **圖表不堆疊為預設**（Grafana）：只有「部分加總等於整體」才堆（記憶體已用／快取／可用）。
14. **筆電尺寸一列 2–3 張**、**總覽頁圖表數受控**（Percona、Datadog）：總覽頁只放兩張時間圖（叢集 CPU、今日請求），其餘以 sparkline 與量表承載。

## HazelminX 各畫面的圖表配置

| 畫面 | 圖表 | 形式 |
|---|---|---|
| 總覽 | KPI 列（節點連線、服務、今日請求、進行中工作） | stat tile＋24 小時 sparkline |
| 總覽 | 叢集 CPU 24 小時（3 節點） | 多序列折線，過期節點的尾段用空心點＋「上次觀測」 |
| 總覽 | 今日請求：邊緣 vs 來源站 | 兩序列長條（每小時），部署標記線，資料完整度註記 |
| 總覽 | 節點表 | 每列 CPU／記憶體／資料碟量表＋CPU 1 小時 sparkline |
| 總覽 | 儲存與備份 | 磁碟量表、備份驗證層級四段進度 |
| 節點詳情 | CPU、記憶體、磁碟 I/O、網路、負載平均、溫度 | 兩欄時間圖格；記憶體堆疊（已用／快取／可用）；其餘不堆疊 |
| 節點詳情 | 磁碟使用 | 每個掛載點一條量表＋裝置身分 |
| 節點詳情 | 最熱容器 | 排序長條清單（CPU／記憶體切換） |
| 專案與服務 | 服務表 | 每列 CPU／記憶體 sparkline、請求與錯誤率數字 |
| 專案與服務 | 部署歷史 | 狀態時間軸（成功／失敗／回滾／進行中） |
| 專案與服務 | 請求與錯誤率 | 每個服務一組小倍數（small multiples），一個 Y 軸 |
| 容器 | 容器表 | 量表＋重啟次數 |
| 容器 | 容器 × 小時 CPU | 熱格（單色深淺） |
| 容器 | 最熱容器 | 排序長條清單 |
| 工作中心 | 佇列統計 | stat tile（執行中、等待批准、24 小時失敗） |
| 工作中心 | 工作步驟 | 步驟狀態條（預檢→批准→執行→驗證），有時長 |
| 工作中心 | 備份傳輸 | 傳輸速率 sparkline＋已傳／總量量表，總量未知時不顯示百分比 |
| 部署檢查 | 預檢結果 | 清單，不是圖；影響範圍列實際節點／服務／磁碟名稱 |
| 錯誤與空狀態 | 圖表無資料、部分資料、離線 | 骨架沿用圖表形狀；離線保留上次渲染並降低不透明度 |
