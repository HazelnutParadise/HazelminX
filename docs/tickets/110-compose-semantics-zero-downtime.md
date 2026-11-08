# 110 — 部署照 Compose 與 Helm 的實際讀法處理多份 compose 檔、profile、建置變數、設定檔與 values，並且每次建置、建置完才無縫替換

**Epic:** Compose 部署（`epic:compose`）  
**User Story:** 部署結果跟自己在主機上跑 compose 一致，而且更新時網站不斷線  
**Milestone:** M2 容器、Compose 與 GitHub  
**Blocked by:** [044](044-compose-projects.md), [046](046-deploy-revisions.md), [108](108-project-units-connections.md), [109](109-env-groups-and-files.md)  
**Status:** blocked  
**規格章節:** 增補 04｜52｜專案的部署單元、單元之間的連線與 env 檔  
**Issue:** [#117](https://github.com/HazelnutParadise/hazelminx/issues/117)  

## What it delivers

單元的 Compose 檔清單：沒有自動選檔，一定要像 Portainer 一樣指定主檔，再選其他要合併的檔並排序；掃描時新舊檔名（`compose.*`、`docker-compose.*`）都列出來；預檢標出疑似開發用的 override 設定。另外處理 profile、替換變數的必填與預設、建置時變數、設定檔與 secrets、Helm values 分層。部署時每次重新建置或拉取（可以關掉，改成沒變時沿用映像），建置完成前不動舊容器；符合條件的服務用新舊並行、健康檢查、Zentinel 切換、排空的方式無縫替換，不符合的寫出 compose 的哪一行造成與預估中斷秒數，單一服務也可以改成先停再起。細化第 18 章 DEP-001～005、DEP-008。

## Acceptance criteria

- [ ] **PRJ-011**（增補 04）部署單元可以依 52.5 設定 Compose 檔、profile、替換變數、建置變數、設定檔與 Helm values。 — 完成條件：52.5 表中的每一列都有對應的畫面與預檢，預設值與工具的預設行為相同。單元一定要指定 Compose 主檔，候選清單依 52.5.1 掃描新舊檔名；指定的檔案在新 commit 不存在時擋下部署。跨目錄合併、`include:` 與同一份主檔被多個單元使用，依 52.5.1 預檢。`COMPOSE_*` 與 `name:` 不採用時會提示。執行環境是乾淨的。
- [ ] **PRJ-014**（增補 04）每次部署都重新建置或重新拉取，建置完成後才替換；沒變的服務不替換。這是 HazelminX 規則。 — 完成條件：有 `build:` 的服務每次都建置，可以選擇不用快取，也可以關掉，改成「沒變時沿用上次的映像」。`image:` 每次重新拉取並記錄 digest。Compose 檔寫了 `pull_policy` 的服務照它的設定。digest 與有效設定都沒變的服務不替換。建置、拉取或預檢失敗時，舊容器沒有被重建或停止。
- [ ] **PRJ-015**（增補 04）符合條件的服務無縫替換；不符合的寫出原因與預估中斷時間。 — 完成條件：依 52.6 的表標出每個服務的替換方式，可以同時有多個標示；單一服務可以改成先停再起。無縫替換時，部署期間持續送出的外部請求沒有失敗。新版不健康時不切換，舊版繼續服務。舊版保留到觀察期結束，期間可以切回。容量不足時拒絕並讓使用者選擇，不會默默改成先停再起。預估中斷秒數依 52.6 的算法。

## Acceptance tests

- **T-202** repo 根目錄有 `compose.yaml` 與 `compose.override.yaml`，override 把 `./src` 掛進 shop-api；另有一個服務帶 `profiles: [debug]`。單元指定主檔 `compose.yaml`，再勾選 `compose.override.yaml`。 → 兩個檔案依序合併；預檢提示「override 把 repo 目錄 ./src 掛進 shop-api，疑似開發用設定」，但不擋下；debug 服務標「profile 未啟用」，不啟動也不算失敗；啟用 debug 後才啟動。
- **T-203** compose 有 `image: shop:${TAG:?必須指定 TAG}`，替換用組沒有 `TAG`。 → 預檢擋下並顯示「必須指定 TAG」；補上後通過。
- **T-204** Next.js 單元的 `NEXT_PUBLIC_API_URL` 放在標「建置時」的組，同一組還有一個機密 `SENTRY_AUTH_TOKEN`。 → `NEXT_PUBLIC_API_URL` 以 build arg 提供，產出的檔案含正確網址；`SENTRY_AUTH_TOKEN` 以 `build.secrets` 提供，`docker history` 看不到它；執行時用的組沒有被當成 build args。
- **T-208** 部署 shop-api（2 個副本，經 Zentinel）時，持續每秒送 50 個外部請求。 → 建置期間舊版照常服務；新版健康後才切換；整段期間沒有失敗的請求；舊版排空後保留到觀察期結束，期間可以一鍵切回。
- **T-209** 新版的 shop-api 健康檢查一直失敗。 → 不切換流量，舊版繼續服務；部署標為失敗，寫出健康檢查的回應。
- **T-210** shop-db 在 compose 第 23 行寫死 `container_name: shop-db`。 → 部署前同時標「先停再起」與「需要批准」。原因指到第 23 行，並說明 PostgreSQL 被判定為單寫入資料；顯示預估中斷秒數，沒有紀錄時寫「首次部署，無法預估」。
- **T-211** 建置時 `npm ci` 失敗。 → 舊容器沒有被重建或停止；部署失敗並附上建置日誌。
- **T-212** 單元選「指定檔案」，主檔 `deploy/prod.yml`，再依序加入 `deploy/monitoring.yml`；repo 根目錄另有 `compose.yaml` 與 `compose.override.yaml`。 → 只合併 `deploy/prod.yml` 與 `deploy/monitoring.yml`，順序依設定；根目錄的兩個檔不會被讀；專案目錄是 `deploy/`，`.env` 組對應 `deploy/.env`；部署紀錄記下實際使用的檔案清單。
- **T-216** 再次部署 web，`postgres:16.4` 的 digest 與 shop-db 的設定都沒變，只有 shop-api 的程式碼變了。 → shop-db、shop-redis 沒有被重建或替換；只有 shop-api 替換；部署紀錄寫出未替換的原因。
- **T-217** tw-01 的可用記憶體不夠同時跑兩份 shop-api。 → 部署前拒絕無縫替換並寫出原因；讓使用者選「先停再起（約 N 秒）」或排進維護時段；不會默默改成先停再起。
- **T-219** search 的 values 依序是 `values-production.yaml`，chart 內附的 `values.yaml` 有 `tolerations: [a, b]`，`values-production.yaml` 有 `tolerations: [c]` 與 `replicas: null`。 → 合併預覽標出 `tolerations` 被整個取代成 `[c]`、`replicas` 被 `null` 刪除，並顯示產生的 manifest 差異。
- **T-220** web 的 `.env` 組有 `COMPOSE_PROJECT_NAME=other`，compose 檔頂層有 `name: legacy`。 → 預檢提示「平台設定優先，以下值不採用」並列出兩者；實際使用的專案名稱仍是 `shop-production-web`。
- **T-276** repo 根目錄同時有 `docker-compose.yml` 與 `compose.yaml`，正在建立單元。 → 兩份都列出，都沒有預先選；`compose.yaml` 標「Compose 預設會用」；沒選主檔時「儲存」與「部署」停用。
- **T-277** repo 根目錄有 `docker-compose.yaml`、`docker-compose.yml` 與 `compose.override.yml`，`apps/api/` 另有 `compose.yml`。 → 四份都列出：根目錄一組排在前面，主檔依序是 `docker-compose.yml`、`docker-compose.yaml`，`docker-compose.yml` 標「Compose 預設會用」；`compose.override.yml` 列在 override，沒有預先勾選；`apps/api/compose.yml` 另成一組。
- **T-278** web 單元指定 `docker-compose.yml`。新 commit 9b3e7f1 把它改名成 `compose.yaml`，推到 production 監聽的 `main`。 → 預檢擋下並寫「指定的 docker-compose.yml 在 commit 9b3e7f1 不存在」，推送觸發的部署不執行；畫面列出新 commit 的候選檔，改指定 `compose.yaml` 後才能部署。
- **T-279** repo 根目錄沒有 Compose 檔，`apps/web/compose.yaml` 與 `apps/api/docker-compose.yml` 各有一份。 → 匯入頁兩份都列出，依目錄分組，不預先選；可以選一份，或建立成兩個單元。只剩 `apps/web/compose.yaml` 一份時仍然要使用者選，不自動帶入。
- **T-280** repo 的 `.env` 有 `COMPOSE_FILE=compose.yaml:compose.prod.yaml`，單元指定的是 `compose.yaml`。 → 部署只用單元指定的檔案；預檢列出「COMPOSE_FILE 不採用」；按「照 COMPOSE_FILE 帶入」後，`compose.yaml`、`compose.prod.yaml` 依序帶入，確認後才儲存。
- **T-281** web 單元的主檔是 `deploy/compose.yaml`，再勾選根目錄的 `compose.override.yaml`；override 第 8 行把 `./src` 掛進 shop-api。 → 預檢提示「compose.override.yaml 第 8 行 ./src → deploy/src」，不擋下；合併預覽顯示的掛載來源是 `deploy/src`。
- **T-282** `deploy/compose.yaml` 有 `include: [../db/compose.yaml]`，`db/compose.yaml` 定義 `shop-db`；之後新 commit 把 `db/compose.yaml` 刪掉。 → 候選清單裡 `db/compose.yaml` 掛在 `deploy/compose.yaml` 底下，標「由 include 帶入」，勾選框固定為已勾選、不能取消；刪掉後預檢擋下部署並寫出是哪份 include 不見了。
- **T-283** n8n-stack 的 production 單元已經用 `compose.yml` 部署在 tw-01；從同一個 repo 建立 staging 單元，也指定 `compose.yml`、也部署到 tw-01。 → 預檢列出「production 的 n8n 單元也用這份主檔」，`container_name: n8n` 與 Port 5678 標擋下並寫出行號；staging 改部署到 lab-mini 時只剩提示。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
