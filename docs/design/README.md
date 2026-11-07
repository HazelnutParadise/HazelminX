# 設計過程檔（票 002）

專案根目錄的 `DESIGN.md` 是視覺基準，`DESIGN-REVIEW.md` 是審查紀錄。這個目錄放設計階段的工作檔與原型。

| 路徑 | 內容 |
|---|---|
| `design-brief.md` | 三個方向的共同輸入：產品、讀者、核心資訊、主導覽、情感基調、硬性限制、示例資料（假設） |
| `research-dashboards.md` | 現代 dashboard 與控制台的圖表做法調查（Beszel、Coolify、Railway、Netdata、Grafana、Datadog 等）與各畫面的圖表配置決定 |
| `directions/` | 第一輪三個方向的總覽頁（A 星圖、B 航站、C 榛果）。已選定 B |
| `coverage-matrix.md` | 1Panel（108 路由、793 API）、Portainer、Webmin 功能 → 畫板的覆蓋矩陣與缺口 |
| `coverage-1panel-frontend.csv` | 108 條 1Panel 前端路由的逐條對照 |
| `edge-cases.md` | 16 個邊界情境與畫面規則 |
| `component-mapping.md` | 元件庫決定（Cloudflare Kumo）、token 對映、畫板元件 → Kumo |
| `ux-evaluation.md` | UX 評估方法清單（專家審查、量測、真人測試）、門檻與改前改後數字、畫面文字來源規則 |
| `ux-walkthrough.md` | 認知走查（六個任務）與啟發式掃描，以及市面面板版面慣例對照 |
| `gap-analysis.md` | 規格 vs 畫面、參考專案 vs 我們的缺口盤點，與「圖形為主」第二輪的改動清單 |
| `screens/` | 方向 B 定稿的 30 個畫板（Claude Design 畫布格式 `.dc.html`），由 `tools/build_screens.py` 產生 |
| `tools/hxui.py` | 共用 token CSS、導覽、圖表元件（純 SVG，CSS hover，不用 JavaScript） |
| `tools/build_screens.py` | 示例資料與各畫面內容；執行後重建 `screens/` |
| `screenshots/` | Playwright 渲染結果：桌面 1440 深淺色、手機 360 |

畫布連結：<https://claude.ai/artifact/NBXgFGYEiZVnKLraoa7Nt8>（私人，需擁有者分享）。第一頁「方向探索」是三個方向，第二頁「方向 B 定稿」是 12 個畫板。

## 畫板清單

| 畫板 | 回答的問題 | 主要圖表 |
|---|---|---|
| Overview | 整個叢集現在怎麼了、要處理什麼 | KPI stat tile、艦隊卡（三環＋走勢）、流量拓撲、叢集 CPU 折線、今日請求長條、容器地圖、專案卡（部署時間軸）、磁碟段條＋備份日曆 |
| NodeDetail | 這台主機現在怎麼了 | 兩欄時間圖格（CPU、記憶體堆疊、磁碟 I/O、網路、負載、溫度 CPU＋GPU）、磁碟量表、最熱容器排行、GPU 區塊（使用率、顯示記憶體、功耗、程序清單、容器 GPU 授權） |
| Projects | 這個專案的服務跑在哪、最近部署如何 | 服務拓撲（入口 → 服務 → 資料 → 儲存）、服務表 sparkline、請求／錯誤率／延遲、部署狀態時間軸 |
| Containers | 哪個容器在哪台、用哪個 Port、誰最吃資源 | 容器地圖、表內量表、容器 × 小時熱格、記憶體排行 |
| WorkCenter | 長操作進行到哪、能不能取消 | 部署流程段、步驟狀態條、傳輸速率、24 小時狀態時間軸 |
| GitHubSource | 從 GitHub 匯入時選倉庫、分支、觸發 | 無圖表 |
| DeployCheck／DeployConfirm | 批准前看得到什麼風險、批准會發生什麼 | 預檢清單、差異、確認框 |
| States | 載入、空、錯誤、部分結果、離線、無資料、危險確認 | 骨架、降不透明度的上次渲染 |
| Websites | 入口怎麼接、憑證撐多久、WAF 擋了什麼 | 入口拓撲、憑證到期橫條、WAF 長條、規則排行、訪客 IP 判定段條 |
| Storage | 磁碟被誰吃掉、備份有沒有真的可還原 | 資料碟段條、儲存卷排行、I/O 圖、備份日曆、驗證流程段、目的地量表 |
| Events | 哪條規則在響、事件從哪來、告警送去哪 | 規則 × 24 小時狀態時間軸、事件熱格、來源排行、告警路由拓撲、開啟中卡片 |
| Nodes | 全部節點現在怎麼了、主機工具與防火牆 | 艦隊卡牆、跨節點資源尺、引擎矩陣、主機工具卡、Port 矩陣、程序排行、SSH 矩陣 |
| System | 一台宿主機的 OS 管理（套件更新、服務、使用者、網路、掛載、時間、開關機） | 可更新套件表（影響欄）、套用流程段、systemd 表、使用者 sudo 矩陣、網卡矩陣、掛載表 |
| Terminal | 多分頁終端機、連線目標、快捷指令、稽核政策 | 終端面板（身分橫幅）、目標表、指令排行 |
| Kubernetes | 叢集接法、資源保留、工作負載、儲存、Helm、維護 | 叢集拓撲、保留環、副本量表、PVC 段條、升級流程段 |
| AppStore | 已安裝／可升級／全部／來源 | 應用卡（量表）、升級差異表、目錄卡（引擎與架構 chip）、安裝預檢流程段 |
| APIProxy | 誰透過哪個服務打到哪、Token 效期、被拒原因 | 使用者→服務→上游拓撲、Token 效期橫條＋用量量表、允許／被拒長條、被拒排行 |
| Database | 實例、連線與查詢、慢查詢、資料庫與帳號、結構、Redis、稽核 | 實例卡（三環）、時間圖、排行、容量段條、結構樹、Redis 環 |
| AI | 推論服務、模型、下載、測試對話、MCP／Agent、推論 API | 服務卡（環＋預檢四段）、模型表量表、下載流程段、對話面板、用量環 |
| Files | 目錄樹、列表、編輯歷史、傳輸、權限與分享 | 樹（大小）、diff 區、傳輸流程段、權限矩陣 |
| Cron | 24 小時排程、任務、依賴、執行紀錄、腳本庫 | 任務×小時熱格、結果時間軸、依賴拓撲、執行流程段 |
| Security | 逐站 WAF 模式、發布、命中、封鎖到期、誤判檢查、主機層 | 模式矩陣、發布流程段、命中長條、到期橫條、檢查面板、主機矩陣 |
| Analytics | 三個觀測點、延遲、路徑、來源、API 用量、異常 | 三欄段條、三線延遲圖、排行、來源段條、用量熱格 |
| IAM | 使用者、角色×資源、Token、工作階段、稽核 | 二次驗證狀態、角色矩陣、Token 效期橫條、稽核時間軸 |
| Settings | 基本、入口與 HTTPS、通知、更新、快照、整合與遙測 | 設定卡、更新流程段、元件版本表、整合矩陣 |
| Logs | 日誌量、journald 檢視器、稽核、SSH 登入、網站存取 | 四序列長條、終端式檢視器、回應碼段條 |
| AppInstall | 用 1Panel 市集格式（data.yml formFields）安裝一個應用 | 參數表單、資源與網路、compose 原始檔、安裝前檢查流程段 |
| EdgeCases | 12 個意料外情境的畫面 | 虛擬捲動表、超長名稱、衝突 banner、403、半完成流程段、時鐘偏差、續傳、磁碟滿、部分失敗、未知版本、被引用、多分頁 |
| M-Overview／M-WorkCenter／M-DeployConfirm | 360px 手機版 | 同上，單欄 |

## 重建與驗證

```bash
python3 docs/design/tools/build_screens.py --render-dir /tmp/hx-render
```

`--render-dir` 會另外輸出可直接用瀏覽器開的 `<畫板>-{dark,light}.html`。要在畫布外預覽 `.dc.html`，拿掉 `support.js`、`<x-dc>`、`<helmet>` 標籤並把 `{{theme}}` 換成 `light` 或 `dark` 即可。

## 三版與定稿共同遵守的規則

- 同一組示例資料（標示為假設）。
- 狀態不只靠顏色：連線中實心圓、過期空心圓、維護中與執行中方塊，並配文字。
- 錯誤與待辦四段式：發生什麼、影響什麼、證據、下一步。
- 離線不等於應用已停止；過期資料標示「上次觀測」與時間。
- 未知總量不顯示假百分比；不可取消的步驟寫明原因。
- 淺色與深色都設計，由 `light` 參數切換。
- 360px 寬：導覽改橫向可捲動、次要欄位隱藏、圖表一列一張；觸控目標 ≥ 44px。
