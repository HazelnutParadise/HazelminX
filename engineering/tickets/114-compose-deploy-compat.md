# 114 — 部署前讀懂既有 compose 的寫法（外部網路、容器名稱、主機路徑、主機建置、寫死的機密、對外 Port、GPU、浮動標籤），不用改 repo 就能安全部署

**Epic:** Compose 部署（`epic:compose`）  
**User Story:** 使用者把現有 GitHub repo 裡的 docker-compose.yml 直接拿來部署，不用為了平台改 repo，也不會因為外部網路、寫死的名稱或路徑在部署後才出事  
**Milestone:** M2 容器、Compose 與 GitHub  
**Blocked by:** 044, 049, 107, 109  
**規格章節:** 增補 08｜56｜既有 Compose 專案的部署相容、分支觸發與物件儲存  
**Issue:** [#130](https://github.com/HazelnutParadise/hazelminx/issues/130)  

## What it delivers

依增補 08 的 56.1～56.2：預檢讀出外部網路（含別名）、跨專案容器名稱、`container_name` 衝突、bind mount 路徑與掛載點、寫死的機密、開到所有位址的 Port、GPU 需求與浮動標籤；能自動處理的寫在覆寫層，會出事的擋下並指到 compose 行號。平台建置的映像帶 commit 標籤，回滾不必重建；多個建置目錄只重建有改動的；每天檢查映像新版並通知。

## Acceptance criteria

- [ ] **GDP-001**（增補 08）部署前，平台列出 compose 用到的外部網路，目標環境沒有時提供建立、對應或改用依賴連線。 — 完成條件：含別名（`name:`）；建立的網路寫進主機變更帳本；不動 repo。
- [ ] **GDP-002**（增補 08）平台把共用外部網路與環境變數裡的跨專案容器名稱記成依賴。 — 完成條件：停止、改名、刪除或搬移被依賴的單元時，影響預覽列出依賴者；搬主機時預檢說明連不到並提供內部位址。
- [ ] **GDP-003**（增補 08）寫死的 `container_name` 與同環境既有容器衝突時，部署前擋下並指出行號。 — 完成條件：沒有外部引用者時提供一鍵加環境字尾；有引用者時列出引用者。
- [ ] **GDP-004**（增補 08）資料目錄的 bind mount 路徑不存在或掛載點沒掛上時，部署前擋下，不讓 Docker 建空目錄。 — 完成條件：提供選既有路徑、搬資料、或明確確認從空的開始；掛載點未掛上一律擋下。
- [ ] **GDP-005**（增補 08）平台建置的映像都帶 commit 標籤，回滾可以切回上一個 commit 的映像。 — 完成條件：同時保留 repo 寫的標籤；清理預設保留每個服務最近 3 個部署版本。
- [ ] **GDP-006**（增補 08）一個 repo 有多個建置目錄時，只重建這次有改動的服務。 — 完成條件：比對 commit 改到的檔案與 `build.context`；可以關掉。
- [ ] **GDP-007**（增補 08）平台偵測 compose 裡寫死的機密，並提供移到平台變數。 — 完成條件：不擋部署；移動後在覆寫層改用機密參照；提示從 repo 刪除並輪換。
- [ ] **GDP-008**（增補 08）資料庫類映像的 Port 開到所有位址時，標高風險並提供改綁 127.0.0.1。 — 完成條件：自動部署暫停等批准；改法在覆寫層；維持原樣時記入部署紀錄。
- [ ] **GDP-009**（增補 08）浮動標籤的映像記下實際 digest，回滾回到同一個 digest。 — 完成條件：部署紀錄顯示 digest。
- [ ] **GDP-010**（增補 08）平台每天檢查映像新版並通知，不自動更新。 — 完成條件：版本號比對同主版本新版、浮動標籤比對 digest；更新是一次普通部署；可對單一服務開自動套用小版本。

## Acceptance tests

- **T-252** 匯入 super-minutes 到 hazel-jp-02，那台沒有 `infra-net`。 → 預檢列出 `infra-net`（external），提供建立、對應、改用依賴連線三個選項；選建立後，主機變更帳本多一筆「平台安裝：network infra-net」。
- **T-253** 停止 whisper-api，super-minutes 的變數裡有 `http://whisper-gateway:5000`。 → 影響預覽列出 super-minutes，原因是「環境變數 WHISPER_GATEWAY_URL 指向 whisper-gateway」。
- **T-254** 在 hazel-tw-01 部署 n8n-stack 的 staging 環境，production 已經在跑。 → 預檢擋下，指出 compose 第 N 行 `container_name: mysql` 與 production 衝突；沒有外部引用者，提供一鍵改成 `mysql-staging`。
- **T-255** 部署 StoreCoach 到新主機，`/mnt/hdd0/storecoach/db` 不存在。 → 擋下並說明 mongo 會初始化成空資料庫；提供選既有路徑、搬資料、確認從空的開始。
- **T-256** `/mnt/ssd1` 目錄存在但磁碟沒掛上。 → 擋下，說明「/mnt/ssd1 在根分割區上，不是預期的磁碟」。
- **T-257** infra-manager 連續部署兩個 commit 後，回滾到前一個。 → 不重新建置，直接用 `hzx/infra-manager/app:<前一個 commit>` 啟動。
- **T-258** surtopya 的 commit 只改了 `web/`。 → 只重建 web；api 沿用上一個映像，部署紀錄寫「api：沿用，context 沒有變動」。
- **T-259** 匯入 n8n-stack。 → 預檢警告 `MYSQL_ROOT_PASSWORD` 等 4 個寫死的機密；選「移到平台變數」後，覆寫層改用機密參照，原值不出現在部署紀錄。
- **T-260** jelly-cms 的 postgres 沒有寫 Port；改成 `"5432:5432"` 後 push 到 main。 → 自動部署暫停等批准，原因是「postgres 的 5432 開到所有位址」；提供改綁 127.0.0.1。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
