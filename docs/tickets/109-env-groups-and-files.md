# 109 — 使用者可以把環境變數分成以檔名命名的組，在每個填變數的地方上傳任意檔名的 env 檔，並看到每個服務實際收到的值

**Epic:** Compose 部署（`epic:compose`）  
**User Story:** 環境變數的管理方式與 Compose 的 env_file 讀法一致  
**Milestone:** M2 容器、Compose 與 GitHub  
**Blocked by:** [045](045-env-layers.md), [017](017-ui-shell.md)  
**Status:** blocked  
**規格章節:** 增補 04｜52｜專案的部署單元、單元之間的連線與 env 檔  
**Issue:** [#116](https://github.com/HazelnutParadise/hazelminx/issues/116)  

## What it delivers

變數組：一組就是一個 env 檔，組名就是檔名，放在專案、專案環境、部署單元、服務四層（`.env` 只做替換，其他組依 `env_file` 名稱進入容器，Kubernetes 單元轉成 ConfigMap 與 Secret，名稱依 Kubernetes 規則轉換）。共用的上傳元件：上傳、拖放、貼上或原始文字編輯；上傳時要填組名，預設是上傳的檔名；解析 dotenv 並預覽新增、變更、相同；同名組可以合併或改名；組可以隨時改名，Git 裡的引用保留別名。每個服務的「實際收到的值」依 environment → env_file → 映像 ENV 算出並標來源。可以匯出成同名 env 檔。這個元件用在專案環境、部署單元、匯入流程、建立容器、計畫任務與應用安裝。

## Acceptance criteria

- [ ] **PRJ-007**（增補 04）專案管理員可以把環境變數分成有名稱的組，組名就是相對於專案目錄的路徑，放在專案、專案環境、部署單元或服務四層。 — 完成條件：四層與合併依 52.4 運作，並標成 HazelminX 規則。專案層：修改要有所有專案環境的權限，拒收機密。`.env` 預設只替換，被 `env_file` 明確引用時也進入容器。與 repo 同名檔逐一變數合併（15.2）。缺檔時，repo 有就用 repo，`required: false` 只提示，都沒有才擋下。Kubernetes 單元依 52.4 的名稱轉換、`envFrom` 順序、post-renderer 與摘要註記運作。
- [ ] **PRJ-008**（增補 04）使用者可以在 52.4 列出的每個位置上傳、拖放或貼上 env 檔；上傳時要填組名，預設是上傳的檔名。 — 完成條件：檔名不限。解析依 52.4，區分空字串、未設定與刪除。預覽分出新增、變更、相同，不自動刪除現有變數。機密依規則判定並遮罩。同一層已有同名的組時，可以選合併或改名。超過 1 MiB 或 2,000 個變數時報錯，不會只匯入一部分。
- [ ] **PRJ-009**（增補 04）使用者可以隨時替變數組或設定檔改名。 — 完成條件：改名前列出所有引用。覆寫層的引用一起更新。Git 裡的引用保留舊名稱當別名並標示，下一次部署不會因找不到檔案而失敗。別名期間，同名不能被新的組使用；Git 已經沒有引用時，別名自動移除。
- [ ] **PRJ-010**（增補 04）使用者可以看到每個服務實際收到的環境變數。 — 完成條件：依 52.4「同名時誰優先」算出最終值。每個值標出來源（哪一層、哪一組的第幾行、repo、上傳檔或介面、Compose 的哪一行、映像），並列出被蓋掉的值；機密遮罩。Kubernetes 單元依 `env` 與 `envFrom` 的順序計算。
- [ ] **PRJ-013**（增補 04）使用者可以把變數組匯出成同名的 env 檔。 — 完成條件：機密預設不含值，勾選含值會記入稽核。

## Acceptance tests

- **T-197** web 的 compose 寫 `env_file: [app.env, ./config/mail.env]`。平台有 `app.env` 組，沒有 `config/mail.env`，repo 裡也沒有。 → 預檢擋下並寫出缺 `config/mail.env`，旁邊有「建立這一組」與「上傳 env 檔」。建立後再部署，shop-api 收到兩組的變數。
- **T-198** 在 web 單元上傳 `prod.env`。檔案裡有 `export DB_PASSWORD="a b"`、一個跨行的雙引號值、一行 `=oops`，並且有重複的 `PORT`。 → 組名預設 `prod.env`，建在 web 單元。前兩個正確解析；`=oops` 列出行號與原因，其餘照常匯入；`PORT` 以後出現的為準並標出；`DB_PASSWORD` 預設標成機密並遮罩。
- **T-199** 在 web 單元上傳 `app.env`，而 web 單元已經有 `app.env` 組。 → 讓使用者選合併或改名。選合併時，預覽新增、變更、相同，現有但檔案沒有的變數保留。
- **T-200** 把 `app.env` 改名為 `web.env`，web 的 compose 在 Git 裡仍寫 `env_file: app.env`。 → 改名前列出這個引用；改名後 `app.env` 保留為別名並標「Git 仍使用舊名稱」；下一次部署照常成功；別名期間不能新建名為 `app.env` 的組。
- **T-201** web 的 compose 有 `environment: LOG_LEVEL=warn`，`app.env` 有 `LOG_LEVEL=debug`，映像的 ENV 是 `LOG_LEVEL=info`。 → 「實際收到的值」顯示 `warn`，來源是 compose 的 `environment`，並列出被蓋過的兩個值。
- **T-206** 匯出 shop production 的 `app.env` 組。 → 匯出的檔名是 `app.env`；機密只有名稱沒有值；勾選「含機密值」後才有值，稽核紀錄有這次匯出。
- **T-207** 上傳一份 3 MiB 的 env 檔。 → 回報超過上限，不會只匯入前面一部分。
- **T-213** 專案層有 `shared.env` 組，內容 `APP_NAME=Shop`；專案環境有 `app.env` 組，`LOG_LEVEL=info`；web 單元的 `app.env` 組有 `LOG_LEVEL=debug`；服務覆寫替 shop-api 加了 `LOG_LEVEL=warn`。shop-api 的 compose 寫 `env_file: [shared.env, app.env]`，worker 寫 `env_file: [app.env]`。接著試著在專案層加入 `DB_PASSWORD`。 → shop-api 實際收到 `APP_NAME=Shop` 與 `LOG_LEVEL=warn`，畫面列出被蓋掉的 debug 與 info 及各自來源。worker 收到 `LOG_LEVEL=info`。`DB_PASSWORD` 預設標成機密，專案層拒收。
- **T-214** search（Helm）單元引用專案環境的 `app.env` 組，選擇掛給 `search` Deployment；之後修改 `app.env`。 → 叢集裡出現 ConfigMap `app-env`；`search` Deployment 的容器有對應的 `envFrom`；修改後 Pod 範本的摘要註記改變並觸發滾動更新，新 Pod 收到新值。
- **T-215** 某個值是 `pa$$w'rd`，分別放在 `app.env` 組與服務覆寫，主機上同時有 shell 變數 `LOG_LEVEL=trace`。 → 容器裡兩處的值都是原樣的 `pa$$w'rd`；`LOG_LEVEL` 沒有變成 `trace`。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
