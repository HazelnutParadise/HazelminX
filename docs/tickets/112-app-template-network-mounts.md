# 112 — 從 1Panel 商店安裝時轉換範本（網路、Port、資料路徑、資料庫連線），應用的網路與資料位置安裝時能設、之後也能隨時改

**Epic:** 應用商店（`epic:appstore`）  
**User Story:** 用 1Panel 商店裝的應用不會被塞進一個所有應用互通的網路，也不會把 Port 對外開放；網路和資料放哪，裝的時候能選，之後也能改  
**Milestone:** M2 容器、Compose 與 GitHub  
**Blocked by:** [052](052-appstore-install.md), [053](053-appstore-update-uninstall.md), [109](109-env-groups-and-files.md)  
**Status:** blocked  
**規格章節:** 增補 06｜54｜1Panel 商店範本的轉換，以及應用的網路與資料掛載  
**Issue:** [#119](https://github.com/HazelnutParadise/hazelminx/issues/119)  

## What it delivers

網路模型：每個應用有自己的網路；平台管理的共用服務（資料庫、Ollama）各自一個網路，誰要用就接上；不使用 `1panel-network` 這種全部互通的網路，只在使用者勾選相容模式時才加入。安裝 1Panel 範本時，在覆寫層把 `1panel-network`、`${CONTAINER_NAME}`、對所有位址開放的 Port、`./data` 相對路徑、`PANEL_DB_HOST` 相依服務、預設時區轉成平台的做法，並在預覽並排列出。安裝後可以隨時加入或移出網路（不重建容器），以及改資料位置（先複製、短暫停機、核對、保留舊資料 7 天）。既有的 1Panel 應用原地保留，可以一個一個轉成平台管理。

## Acceptance criteria

- [ ] **APX-001**（增補 06）平台安裝的每個應用都有自己的網路，不預設加入任何共用網路。 — 完成條件：應用的服務都在 `<應用名稱>_default`；沒有勾選相容模式時，不加入 `1panel-network`；平台不主動建立 `1panel-network`。
- [ ] **APX-002**（增補 06）管理員從 1Panel 商店安裝時，平台依 54.2 轉換範本。 — 完成條件：54.2 表中每一列都有處理；調整只寫在覆寫層；安裝預覽並排列出範本原本與平台調整；預檢失敗不留下半套服務。
- [ ] **APX-003**（增補 06）管理員可以讓應用重用平台管理的資料庫等相依服務。 — 完成條件：選項列出平台管理的服務；選既有的時，變數填入它的內部位址，應用接上它的網路；選新建專用的時，建在應用自己的網路。
- [ ] **APX-004**（增補 06）管理員安裝時可以選要加入的網路，以及每個掛載的位置。 — 完成條件：每顆資料磁碟有預設位置，第一次用才建立；可以用資料夾選擇器；空間不足在安裝前擋下。
- [ ] **APX-005**（增補 06）管理員可以隨時把應用加入或移出網路。 — 完成條件：立即生效，不必重建容器；寫進覆寫層，部署或更新後仍保留；應用自己的網路不能移出；移出相依服務的網路前列出會斷掉的連線。
- [ ] **APX-006**（增補 06）管理員可以隨時改應用的資料位置。 — 完成條件：依 54.3 的搬移資料或只改掛載點；寫出預估停機；舊資料預設保留 7 天，期間可以一鍵改回；儲存卷與主機資料夾可以互轉。
- [ ] **APX-007**（增補 06）範本更新時，使用者改過的網路與掛載不被蓋掉。 — 完成條件：重新套用 54.2 的轉換；預覽分開標出範本本身的變更與平台調整。
- [ ] **APX-008**（增補 06）管理員可以把既有的 1Panel 應用一個一個轉成平台管理。 — 完成條件：依 54.4 原地保留、預覽、處理相依順序；失敗或回復時恢復原本的設定與網路。

## Acceptance tests

- **T-234** 從 1Panel 商店安裝 Nextcloud 35.0.2，主機上沒有 `1panel-network`。 → 安裝成功；容器在 `nextcloud_default`；主機上沒有出現 `1panel-network`；預覽並排列出 `networks`、`container_name`、Port、`./data` 與時區的調整。
- **T-235** 安裝時 HTTPS Port 用範本預設的 40069，網域設 `cloud.example.tw`。 → 容器的 443 只綁在 `127.0.0.1`；從外部連主機的 40069 連不到；`https://cloud.example.tw` 經 Zentinel 可以開啟。
- **T-236** 安裝 WordPress 7.1.4，資料庫選既有的 shop-db（平台管理，網路 `hzx-db-shop`）。 → `WORDPRESS_DB_HOST` 是 shop-db 的內部位址；wordpress 容器同時在 `wordpress_default` 與 `hzx-db-shop`；連得到 shop-db；連不到其他應用的資料庫。
- **T-237** 已安裝的 nextcloud 加入網路 `hzx-ollama`，再移出。 → 加入與移出都沒有重建容器；加入後連得到 Ollama，移出後連不到；之後更新 nextcloud，網路設定照舊。
- **T-238** 嘗試把 wordpress 移出 `hzx-db-shop`。 → 移出前列出「wordpress → shop-db 的連線會斷掉」，確認後才移出。
- **T-239** 把 nextcloud 的 `/data` 從 `/mnt/data` 搬到 `/mnt/fast`（212 GiB），搬移期間持續上傳檔案。 → 先複製時服務照常；只在最後同步時停機；檔案數與大小核對相符；啟動後新舊上傳的檔案都在；舊資料保留 7 天，期間可以一鍵改回。
- **T-240** 範本出了新版，新版把 `./data` 改成 `./data/app`；使用者之前已把資料位置改到 `/mnt/fast`。 → 預覽分開列出範本的變更與平台調整；更新後資料位置仍是 `/mnt/fast`。
- **T-241** 主機上已有 1Panel 安裝的 WordPress 與 MySQL，把 WordPress 轉成平台管理，MySQL 仍由 1Panel 管理。 → 預檢寫出 MySQL 仍由 1Panel 管理，WordPress 要用相容模式暫時留在 `1panel-network`；轉換後 WordPress 照常連得到 MySQL；回復後恢復原本的容器設定。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
