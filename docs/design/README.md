# 設計過程檔（票 002）

專案根目錄的 `DESIGN.md` 是視覺基準，`DESIGN-REVIEW.md` 是審查紀錄。這個目錄放設計階段的工作檔與原型。

| 路徑 | 內容 |
|---|---|
| `design-brief.md` | 三個方向的共同輸入：產品、讀者、核心資訊、主導覽、情感基調、硬性限制、示例資料（假設） |
| `research-dashboards.md` | 現代 dashboard 與控制台的圖表做法調查（Beszel、Coolify、Railway、Netdata、Grafana、Datadog 等）與各畫面的圖表配置決定 |
| `directions/` | 第一輪三個方向的總覽頁（A 星圖、B 航站、C 榛果）。已選定 B |
| `gap-analysis.md` | 規格 vs 畫面、參考專案 vs 我們的缺口盤點，與「圖形為主」第二輪的改動清單 |
| `screens/` | 方向 B 定稿的 15 個畫板（Claude Design 畫布格式 `.dc.html`），由 `tools/build_screens.py` 產生 |
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
