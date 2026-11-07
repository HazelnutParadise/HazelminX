---
version: alpha
name: HazelminX
description: 方向 B「航站」。深暖炭黑為預設、淺色為對等主題，榛果色單一強調，鍵盤優先的三欄控制台；圖形為主，表格為第二層。
colors:
  primary: "#ECE6DA"
  secondary: "#A79E8E"
  tertiary: "#D9A066"
  neutral: "#161412"
  panel: "#1D1B18"
  raised: "#262320"
  line: "#35312B"
  faint: "#9A917F"
  hazel-ink: "#1A1410"
  status-ok: "#9BD3A4"
  status-warn: "#F0B35B"
  status-danger: "#F28B82"
  status-info: "#8DB4F2"
  series-1: "#AF711F"
  series-2: "#5682BB"
  series-3: "#47AA62"
  series-4: "#B55B9A"
  light-primary: "#1B1814"
  light-secondary: "#5E574C"
  light-tertiary: "#8F5B23"
  light-neutral: "#F6F2EA"
  light-panel: "#FFFDF8"
  light-raised: "#EFE9DE"
  light-line: "#D9D1C2"
  light-faint: "#6B6457"
  light-hazel-ink: "#FFFFFF"
  light-status-ok: "#2F6B3C"
  light-status-warn: "#9A5A0B"
  light-status-danger: "#B4382E"
  light-status-info: "#2E5FA8"
  light-series-1: "#7C4B02"
  light-series-2: "#2E598F"
  light-series-3: "#218A45"
  light-series-4: "#883170"
typography:
  h1:
    fontFamily: Geist
    fontSize: 20px
    fontWeight: 600
    lineHeight: 1.3
  h2:
    fontFamily: Geist
    fontSize: 14px
    fontWeight: 600
    lineHeight: 1.4
  body-md:
    fontFamily: Geist
    fontSize: 14px
    fontWeight: 400
    lineHeight: 1.5
  body-sm:
    fontFamily: Geist
    fontSize: 12px
    fontWeight: 400
    lineHeight: 1.5
  label-caps:
    fontFamily: Geist
    fontSize: 11px
    fontWeight: 500
    lineHeight: 1
    letterSpacing: 0.06em
  mono:
    fontFamily: Geist Mono
    fontSize: 13px
    fontWeight: 400
    lineHeight: 1.4
  figure:
    fontFamily: Geist
    fontSize: 28px
    fontWeight: 600
    lineHeight: 1.1
rounded:
  sm: 4px
  md: 6px
  lg: 8px
spacing:
  xs: 4px
  sm: 8px
  md: 12px
  lg: 16px
  xl: 24px
  xxl: 32px
components:
  button-primary:
    backgroundColor: "{colors.tertiary}"
    textColor: "{colors.hazel-ink}"
    rounded: "{rounded.md}"
    minHeight: 36px
    padding: 0 12px
  button-secondary:
    backgroundColor: "transparent"
    textColor: "{colors.primary}"
    borderColor: "{colors.line}"
    rounded: "{rounded.md}"
    minHeight: 36px
    padding: 0 12px
  button-danger:
    backgroundColor: "{colors.status-danger}"
    textColor: "{colors.hazel-ink}"
    rounded: "{rounded.md}"
    minHeight: 36px
    padding: 0 12px
  panel:
    backgroundColor: "{colors.panel}"
    borderColor: "{colors.line}"
    rounded: "{rounded.lg}"
    padding: 0
  card-raised:
    backgroundColor: "{colors.raised}"
    borderColor: "{colors.line}"
    rounded: "{rounded.lg}"
    padding: 12px
  table-row:
    minHeight: 44px
    padding: 0 14px
    borderColor: "{colors.line}"
  nav-item:
    minHeight: 40px
    padding: 0 10px
    rounded: "{rounded.md}"
  input:
    backgroundColor: "{colors.raised}"
    textColor: "{colors.primary}"
    borderColor: "{colors.line}"
    rounded: "{rounded.md}"
    minHeight: 40px
    padding: 0 10px
  kbd:
    fontFamily: Geist Mono
    fontSize: 11px
    borderColor: "{colors.line}"
    rounded: "{rounded.sm}"
---

## Overview

HazelminX 是自架伺服器的管理中心。讀者是熟 Docker 與 Linux 的個人與小團隊管理員，常在深夜處理故障。介面要先回答「這是什麼、在哪裡、現在怎麼了、接下來能做什麼」（規格第 7 章）。

方向 B「航站」的立意：像機場航管的工作台，密度高但每個元素都承載差異化資訊，沒有裝飾。三欄骨架是這個方向的簽名：左欄導覽（鍵盤可達）、中欄主表與圖表、右欄「需要處理」與「進行中的工作」。視覺母題是「榛果」與「層」：榛果色是唯一的強調色，深淺三層平面（底、面板、浮起）承擔分區，不用陰影。

預設深色，淺色是對等主題而不是反轉；兩種主題都要能認出榛果色。

## Colors

深色是基準，淺色對照。所有文字對比都 ≥ 4.5:1（含 11px 標籤），狀態色不單獨承載意義，一定配文字或形狀。

| 角色 | 深色 | 淺色 | 用途 |
|---|---|---|---|
| neutral（底） | `#161412` | `#F6F2EA` | 頁面底；暖炭黑而不是 GitHub 深藍 |
| panel | `#1D1B18` | `#FFFDF8` | 導覽欄、表格、圖表容器 |
| raised | `#262320` | `#EFE9DE` | 卡片、列 hover、輸入框 |
| line | `#35312B` | `#D9D1C2` | 1px 分隔線與邊框；也是圖表格線 |
| primary（ink） | `#ECE6DA` | `#1B1814` | 主要文字 |
| secondary（muted） | `#A79E8E` | `#5E574C` | 說明文字、次要欄位（對比 6.5 / 7.0） |
| faint | `#9A917F` | `#6B6457` | 11–12px 標籤、欄位標題、註腳（對比 ≥ 4.5 於 panel 與 raised） |
| tertiary（hazel） | `#D9A066` | `#8F5B23` | 唯一強調：主要按鈕、目前導覽、連結 hover、單序列圖表 |
| hazel-ink | `#1A1410` | `#FFFFFF` | 榛果底上的文字 |
| status-ok | `#9BD3A4` | `#2F6B3C` | 連線中、成功、校驗通過；配實心圓 |
| status-warn | `#F0B35B` | `#9A5A0B` | 過期、待批准、即將到期；配空心圓 |
| status-danger | `#F28B82` | `#B4382E` | 失敗、超過門檻、危險操作；配實心方塊 |
| status-info | `#8DB4F2` | `#2E5FA8` | 執行中、驗證中、維護中；配實心方塊 |

### 圖表序列色（固定順序，跟著實體不跟著名次）

| 序號 | 深色 | 淺色 | 固定對應 |
|---|---|---|---|
| series-1 | `#AF711F` | `#7C4B02` | 第一個節點／Cloudflare 邊緣／讀取／進站／1 分鐘負載 |
| series-2 | `#5682BB` | `#2E598F` | 第二個節點／Zentinel 來源站／寫入／出站／5 分鐘負載 |
| series-3 | `#47AA62` | `#218A45` | 第三個節點／15 分鐘負載 |
| series-4 | `#B55B9A` | `#883170` | 第四個節點 |

兩組都用 dataviz 驗證器跑過：明度帶、彩度下限、色弱相鄰差（深色最差 ΔE 11.8、淺色 14.0）、一般視覺相鄰差（20.3 / 20.0）、對平面對比 ≥ 3:1 全部通過。超過四個序列不生新色：折成「其他」或拆小倍數。

### 單色序列（熱格、量表軌道）

榛果色由淺到深。深色主題：`#2A241C` `#4A3A22` `#6E5228` `#9A6F33` `#D9A066`。淺色主題：`#F3E6D3` `#E3C9A4` `#CFA772` `#B38347` `#8F5B23`。最淺一階代表「接近零」，可以貼近平面。

## Typography

- **UI 與標題：Geist**，中文回落 Noto Sans TC → PingFang TC → Microsoft JhengHei。`font-synthesis: none; line-break: strict`。不用襯線，資料層一律無襯線或等寬。
- **數值、識別碼、時間：Geist Mono**。表格欄位與軸刻度加 `font-variant-numeric: tabular-nums`；stat tile 與英雄數字用預設比例字距，不加 tabular。
- 層級靠粗細與顏色，不靠放大：h1 20/600 只用於頁面標題；h2 14/600 是區塊標題；正文 14；次要 12；欄位標題 11 大寫、字距 0.06em（只用在表格欄首，一頁不超過這一種大寫標籤）。
- 中文引號用「」，數字與單位之間半形空格（`11.2 GiB`）。

## Layout

- **頂列 56px**（市面面板的共同慣例）：品牌 220px 與側欄同寬 → 面包屑「節點 / 頁名」→ 全域搜尋 ⌘K（最寬 420px）→ 右側：觀測時間與自動更新頻率、告警鈴（紅色計數）、進行中的工作（榛果計數）、終端機、主題切換、使用者。任何頁面都看得到有幾件事要處理。
- **側欄 220px**：每項一個 16px 線條圖示（同一套 1.8px 線寬）＋文字，群組標題 11px 大寫，底部「收合」鈕；手機改成一列橫向捲動的 chip。
- **頁面標題列**：h1 ＋ 右側動作按鈕，不再放觀測時間；需要節點範圍的頁面緊接「範圍列」。
- 三欄：導覽 232px、主欄 `flex: 999 1 560px`、右欄 340px。主欄內距 `18px 24px 40px`，區塊間距 22px。
- 1100px 以下右欄落到主欄之下；768px 以下導覽改成可換行的橫列，表格隱藏第三到第五欄、圖表一列一張、按鈕換行。
- 表格列 min-height 44px（觸控目標），欄首 34px；按鈕 36px，列內小按鈕與圖示按鈕 32px；導覽項 38px；卡片標題列 46px、內距 16px；區塊間距 16px、卡片間距 12px。
- **表格欄數依內容**：每張表只留有資料的欄（產生器會把整欄空白的拿掉），模板 `cols-3`～`cols-6`；有列尾快速動作的表用 `acts-end`（最後一欄 130px 靠右）。列尾快速動作固定順序：日誌、終端機、重啟、更多；手機只留前兩個。
- 名稱欄第二行最多兩行、長 token 可斷行（映像名、路徑、查詢），不用省略號吃掉關鍵字；排行名稱同樣可斷行。
- 圖表格：1440 寬一列兩張（每張至少 12 欄中的 4 欄），360 寬一列一張。時間範圍列放在所有圖表之上，一列掃全部。
- 間距階：4 / 8 / 12 / 16 / 24 / 32。

## Elevation & Depth

沒有陰影。深度只來自三層平面（底 → panel → raised）加 1px `line`。浮層（下拉、確認框）用 raised 底加 1px line，外圍加 `rgba(0,0,0,.45)` 遮罩（淺色 `.25`）。焦點環：2px 榛果色 `outline`，offset 2px，所有可操作元素一致。

## Shapes

- 4px：kbd、chip、量表、長條頂端。
- 6px：按鈕、導覽項、輸入框。
- 8px：panel、卡片。
- 狀態形狀固定：實心圓 = 正常／成功，空心圓 = 過期／待批准，實心方塊 = 執行中／維護中／危險。形狀永遠配文字。

## Components

- **button-primary**：榛果底、hazel-ink 字、600 粗、36px 高、`padding 0 12px`。一個區塊只有一個。
- **button-secondary**：透明底、1px line、ink 字。
- **button-danger**：danger 底、hazel-ink 字；只出現在確認框內，而且確認框標題寫出實際節點／磁碟／環境名稱。
- **panel**：panel 底、1px line、8px 圓角、內容不加 padding，由表頭列（`10px 14px`）與列（`0 14px`）各自負責。
- **card-raised**：raised 底、1px line、8px 圓角、12px 內距；用在「需要處理」卡片與工作卡片。
- **table-row**：grid 欄、44px 高、底線 1px、hover raised；第一欄名稱用等寬 500 粗，第二行 12px faint 說明。
- **status pill**：12px/500、圓角 999、底色＝該狀態色 13%、左側形狀（● ○ ■）＋文字；圖例與矩陣裡用較窄的 padding。
- **icon button**（`.ib`）：36px 方形、透明底、hover raised；頂列與列尾快速動作共用；有計數時右上角 16px badge。
- **stat tile**：標籤 12 muted → 數值 28/600 比例字距 → 變化（有方向的符號與相對期間）→ 12 點 sparkline（faint 線，最新一段榛果色）。
- **meter**：高 6px、4px 圓角；軌道是序列色最淺階，填充依門檻換色（≥70% warn、≥90% danger），旁邊一定有數字。
- **sparkline**：高 24px、寬 96px、2px 線，不畫軸；表格一列只有一欄 sparkline。
- **時間序列圖**：高 180px；格線 1px `line` 實線、4 條；Y 軸刻度 11px faint、靠左；單序列用榛果色，多序列用 series-1..4，固定對應；面積填充 10%；2px 線、圓角接合；部署／重啟事件以 1px 虛直線標記並在頂端寫 commit 短碼。hover 顯示所有序列數值（CSS 十字線）。過期資料尾段改空心點。
- **長條圖**：寬 ≤ 24px、相鄰 2px 間隙、頂端 4px 圓角、底邊方角。
- **堆疊面積**：只用於「部分加總等於整體」（記憶體已用／快取／可用），段間 2px 平面色間隙。
- **狀態時間軸**：高 10px 橫條，每段一個狀態色，段間 2px 間隙；hover 顯示時間與結果。
- **熱格**：單色序列五階、格間 2px、格內不寫字；旁邊有色階圖例與表格檢視入口。
- **排行清單**：名稱 + 細長條 + 數值一列，長條用 series-1；只排前十。
- **四段式訊息**（錯誤、告警、待辦）：標題色帶狀態 → 發生什麼（14/500）→ 影響與證據（12 muted）→ 下一步按鈕。
- **輸入框**：raised 底、1px line、40px 高；標籤在上、錯誤在下、不用 placeholder 當標籤。
- **kbd**：Geist Mono 11px、1px line、4px 圓角。

## Data visualization

依 dataviz 技能與 `docs/design/research-dashboards.md`：

- 數字先於圖：單一數值用 stat tile，比例用 meter，整體拆分用細段條，超過七類用表格。
- 一張圖一個 Y 軸；量級差很多就拆圖或共用索引。
- 序列顏色固定跟實體，篩選後不重新上色；超過四個序列折成「其他」。
- 多於一個序列一定有圖例；直接標籤只標端點或極值，不在每個點寫數字。
- 文字永遠用文字色，序列色只在標記上；深淺兩種主題各自驗證。
- 時間範圍：即時（5 分）／1 小時／12 小時／24 小時／7 天／30 天，一列、在圖之上、掃全部圖。
- 部分資料要標：過期尾段、取樣比例、保留期外空白都在圖上寫明，不補假線。
- 每張圖都有表格檢視入口；tooltip 只是補充。
- 重新讀取時保留上一次渲染並降低不透明度，不閃骨架。

## 圖形為主（Graphics first）

每個畫面先用形狀回答「現在怎麼了」，表格是第二層。規格第 7 章說專業感不是堆圖表：這裡的圖形都是狀態與結構的編碼，不是裝飾。順序固定：

1. **先形狀，再數字，最後表格。** 一個頁面的第一屏是艦隊卡、地圖、拓撲或日曆這類「掃一眼」的元件；數字放在形狀旁邊；完整表格放在下方或「表格檢視」連結後面，不刪，因為鍵盤與螢幕閱讀器仍需要它。
2. **同一份資料的三層視角。** 總覽用卡與地圖、列表頁用表格加內嵌量表、詳情頁用時間圖。三層讀同一個識別碼，不各存一份狀態。
3. **形狀永遠帶狀態語意。** ● 正常、○ 過期／待處理、■ 失敗／維護／危險，配文字；地圖格與拓撲節點也用同一套形狀，不另創。
4. **圖形元素可點。** 艦隊卡、地圖格、拓撲節點、日曆格都是連結，點了到該資源，不另做「查看」按鈕。
5. **沒資料就不畫。** 離線節點的格子用虛線框、未排程的日子用淡格，不把零畫成滿格或空白。

### 範圍列與圖例

- **範圍列（scope bar）**：所有跟節點、叢集或入口有關的頁面，標題正下方固定一列：「範圍 · 全部節點（n）｜hazel-tw-01｜hazel-jp-02｜lab-mini」，Kubernetes 用「叢集」、網站與安全用「入口」、檔案與宿主機系統用「節點」。沒有這一列的頁面（API 代理、身分、設定）是平台層級。表格與卡片仍寫出節點名稱，不靠範圍列才知道在哪台。
- **圖例是圖例**：編碼（形狀、顏色、線寬）用 `legend()` 以色塊、形狀與線樣本呈現，放在區塊標題列右側；不用句子解釋。區塊標題旁只允許 14 字內的短標籤（例如「顯示 8，依 CPU 排序」），註腳只保留短標籤與連結。
- **導覽**：終端機是頂層項目（⌘`），日誌在「入口與安全」群組。

### 圖形元件

- **ring（環形量表）**：單一比例；直徑 64（卡片）或 72（詳情頁頂部）、6px 線、軌道 q2、填充依門檻換色（70／90，磁碟 80／90）；中央 12px 等寬數值，下方 11px 標籤。一個卡片最多三個環。
- **node card（艦隊卡）**：名稱＋狀態（形狀＋文字）→ 一列三環（CPU、記憶體、資料碟）→ CPU 1 小時 sparkline 與最後聯絡 → 容器執行中／總數的細段條。寬 250–320px，auto-fill 排列。
- **host map（主機／容器地圖）**：每節點一組，每容器一格 26×26、間距 3px；顏色＝CPU 單色序列五階；異常格 danger 邊框＋實心方塊、過期格空心圓、已停止格虛線框。圖例固定在底部。
- **topology（拓撲）**：等距欄（邊緣 → 入口 → 站點 → 服務 → 節點，或入口 → 服務 → 資料 → 儲存）；節點是 raised 底 1px line 的圓角盒，左側狀態形狀，兩行文字（名稱 12px、副標 11px 等寬）；連線為三次曲線，粗細 1.5–7.5px 依佔比，正常 series-1 55% 不透明度、失敗 danger、次要 q3。手機版改成「來源 → 目的 · 備註」清單。
- **tall bar（高段條）**：28px 高，各段印數值（寬度 ≥ 9% 才印），可用段 q1；用於磁碟組成與比例拆分，上限六段。
- **calendar（日曆熱格）**：12px 格、2px 間距、七列一週、直向填；成功 ok、失敗 danger（中央留 panel 色小方塊）、部分 warn、未排 q1、未來虛線框。
- **pipeline（流程段）**：每階段一個 1px line 的方塊（名稱＋時長＋備註），狀態色在邊框與圖示（● done、■ 進行中／失敗、○ 等待），階段間箭頭 18px；手機版橫向捲動。
- **狀態時間軸（規則列表）**：每格一小時，24 格，顏色對應結果，並配「現在」狀態文字欄。

### 哪些畫面用什麼

| 畫面 | 第一屏的形狀 | 第二層 |
|---|---|---|
| 總覽 | 艦隊卡、流量拓撲、容器地圖、專案卡、儲存段條＋備份日曆 | 叢集 CPU／請求圖、表格檢視連結 |
| 節點詳情 | 七個環（CPU、記憶體、兩個磁碟、GPU 核心、顯示記憶體、GPU 溫度）＋負載數字＋容器段條 | 時間圖格、磁碟表、GPU 程序表 |
| 容器 | 容器地圖 | 表格（內嵌量表）、熱格、排行 |
| 專案 | 服務拓撲（入口 → 服務 → 資料 → 儲存）、部署結果時間軸 | 服務表、請求／錯誤率／延遲圖 |
| 工作中心 | 部署流程段、備份進度 | 步驟條、歷史表 |
| 網站與入口 | 入口拓撲、憑證到期橫條、訪客 IP 判定段條 | WAF 長條圖、規則排行、站點表 |
| 儲存與備份 | 資料碟段條、備份日曆、驗證流程段 | 儲存卷排行、I/O 圖、目的地量表 |
| 事件與告警 | 規則 × 24 小時狀態時間軸、事件熱格、告警路由拓撲 | 來源排行、開啟中卡片 |

## Do's and Don'ts

- Do：狀態一定「顏色＋形狀＋文字」。
- Do：危險操作寫出實際名稱（「將停止 hazel-tw-01 的 shop-api、shop-worker」）。
- Do：離線與過期分開講，過期標「上次觀測」時間。
- Do：未知總量就不顯示百分比；不可取消的步驟寫明原因。
- Do：360px 寬主要操作仍可達，觸控目標 ≥ 44px。
- Don't：紫色漸層、霓虹光暈、emoji 當 icon、圓角卡片加左色條、裝飾性狀態點、裝飾性 icon。
- Don't：Inter／Roboto／系統字當標題字；襯線用在資料層。
- Don't：雙 Y 軸、彩虹序列、圓餅圖比較相近值、每個點都標數字。
- Don't：為了「圖形化」畫裝飾性的圖；每個形狀都要能回答「現在怎麼了」，否則用數字或文字。
- Don't：編造完美數字（示例資料一律標示「假設」）。
- Don't：複製 1Panel、Portainer、Zeabur 的品牌素材或版面。
