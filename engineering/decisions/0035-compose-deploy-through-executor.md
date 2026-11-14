# 0035 — Compose 部署經高權限執行器：內容定址的暫存、綁定內容的授權憑證、執行器自己讀 Compose

**狀態：** 已採用（票 044.2，2026-11-14）

## 背景

044.1 讓 Core 只從內容解析 Compose（0034）。部署要以 root 寫檔並執行 `docker compose`，所以必須經 Rust 高權限執行器。限制有三個：

- 一個單元最多 768 KiB，執行器 socket 一次請求只收 64 KiB，而且規則是大量資料不走這條 socket（SEC-009）。
- 授權憑證原本只帶權限名稱，不帶內容；同一張 `host.compose.deploy` 憑證可以拿去部署別的內容。
- 以 root 執行的程式不能接受呼叫端給的路徑或指令；容器能控制的目錄可能被換成符號連結。

## 決定

1. **內容定址的暫存。** Core 在工作步驟的參數裡送出檔案內容（UTF-8 文字直接放字串，其他用 base64）。Agent 把每個檔案寫成自己暫存目錄裡的 `<sha256>` 檔，沒有子目錄。執行器從固定位置打開暫存目錄（`O_NOFOLLOW`、`O_DIRECTORY`，擁有者必須是 Agent 帳號、其他人不可寫），再以 `O_NOFOLLOW` 打開 `<sha256>`，只收一般檔案，大小與雜湊都要和清單一致才使用。路徑從不由呼叫端決定：檔案在主機上的位置由清單裡的相對路徑接在執行器自己的目錄底下，相對路徑逐段檢查。
2. **憑證綁定內容。** 授權憑證多一個可選的 `binding` 欄位（十六進位 SHA-256），有值時簽章內容多一行 `binding:<hex>`。`binding` 是部署清單的逐行文字摘要（Compose 專案名稱、修訂版 ID、引擎環境、主檔、合併檔、profiles、工作目錄、每個檔案的路徑／大小／雜湊），Go 與 Rust 各自照同一個格式計算，不靠 JSON 正規化。`compose_check` 與 `compose_deploy` 要求憑證帶 `binding` 且等於動作的摘要；其他動作不受影響，沒有 `binding` 的舊憑證簽章內容不變。
3. **執行器自己讀 Compose。** 執行器把檔案放好後，用固定的命令列執行 `docker compose config --format json`，從原生 Compose 的結果取映像、主機 Port 與 bind mount 來源；不接受 Core 傳來的路徑、Port 或映像清單。預檢（映像取得、磁碟空間、Port 衝突、bind mount 來源存在、同名 Compose 專案不是平台部署的）都以這份結果為準。
4. **目錄與資料分開。** 平台擁有 `/var/lib/hazelminx/compose/<Compose 專案>/`：每次部署一個 `revisions/<修訂版>/`（來源，寫入後不再改），持久資料放 `data/`。Compose 裡指向不是單元檔案的相對 bind mount（044.1 的 `data-dir`），在修訂版目錄裡是指向 `data/<相對路徑>` 的符號連結，Docker 建立掛載時會解析成 `data/` 底下的實際目錄。舊修訂版保留，運行中的容器不會失去它掛的設定檔；清理留給 046／114。
5. **帳本。** `compose_deploy` 宣告三個目標：修訂版目錄（建立）、資料目錄（建立或修改）、新的帳本目標 `ComposeProject`（修改）。`ComposeProject` 的狀態用 `docker ps -a --filter label=com.docker.compose.project=<名稱>` 讀出每個容器的名稱、映像、狀態。移除方式：`docker compose -p <名稱> down`，再刪掉 `/var/lib/hazelminx/compose/<名稱>/`。
6. **引擎。** 只部署到 root 擁有的引擎 socket（`/var/run/docker.sock`、`/run/docker.sock`、`/run/podman/podman.sock`），環境 ID 對應這份固定清單；Podman 用 Docker Compose CLI 連它的 socket。rootless 環境拒絕並寫出原因。
7. **change plan。** 每次部署都是一個 `compose.deploy` 計畫：計畫內容綁定單元版本與檔案摘要；有 044.1 列為高風險的設定時需要批准，沒有時建立後可直接執行。

## 後果

- 暫存檔在 Agent 帳號底下短暫存在（含標記為機密的檔案），部署步驟結束就刪除；主機上的修訂版目錄是 root 擁有、0700。
- 一個工作步驟訊息上限 1 MiB；編碼後超過上限的單元拒絕部署並寫出原因，分段傳送留到之後的票。
- 執行器在暫存目錄上只信任雜湊，Agent 被入侵最多能讓部署失敗，不能換掉內容或讓 root 讀別的檔案。
