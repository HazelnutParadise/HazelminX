# hazelminx-core

HazelminX 的控制中心：管理 API、權限、工作協調、設定版本與嵌入式網頁介面。單一 Go 執行檔，資料放在 SQLite（純 Go 驅動，主機不需要 C 編譯器）。

## 建置與測試

```bash
go build ./cmd/hazelminx-core
go test ./...
```

嵌入前端：`scripts/embed-web.sh` 建置 `web/dist`、複製到 `core/web/dist`，再以 `-tags embedweb` 建置。沒有這個 tag 時，介面路徑回 `503 UI_NOT_EMBEDDED`，管理 API 照常可用。嵌入後 `/assets/*` 回一年的 immutable 快取、找不到時回 404；其他路徑回 `index.html`（`no-store`）交給前端路由。

## 執行

```bash
./hazelminx-core -listen 127.0.0.1:9800 -data-dir /var/lib/hazelminx
```

| 設定 | 旗標 | 環境變數 | 預設 |
|---|---|---|---|
| 管理 API 監聽位址 | `-listen` | `HAZELMINX_LISTEN_ADDR` | `127.0.0.1:9800` |
| 資料目錄 | `-data-dir` | `HAZELMINX_DATA_DIR` | `/var/lib/hazelminx` |
| 日誌格式 | `-log-format` | `HAZELMINX_LOG_FORMAT` | `json` |

端點：`GET /healthz`（含資料庫健康）、`GET /management/v1`（操作總覽，列出所有操作與已註冊的工作種類）、`/management/v1/jobs`（工作：列出、提交、取得、事件接續、取消）。完整契約見票 010。

## 工作引擎

長操作一律建立工作（`POST /management/v1/jobs`，回 `202` 與工作 ID），工作不隨 HTTP 連線消失。每個工作種類向引擎註冊一個 `jobs.Handler`，自己定義步驟規劃、執行前再檢查、執行、復原、對帳與完成檢查；沒有註冊的種類不能提交。目前執行檔內沒有註冊任何種類，Agent 相關的種類隨票 004／007 進來。

- 狀態依規格 33.1，表格在 `internal/jobs/state.go`；終態不可再改。
- 同一 `requestId` 相同內容回同一工作，不同內容回 `409 REQUEST_ID_CONFLICT`。
- 資源鎖以裝置身分、掛載與正規化路徑比對，父子路徑與同磁碟都算衝突；鎖在 Core 重啟後由未完成工作重建。
- 取消：未開始的立即取消；執行中可取消步驟中斷，不可逆步驟做完才停，狀態 `cancel_requested` 期間步驟帶 `waitReason`。
- 失敗若所有已執行步驟都有復原方法則自動復原為 `rolled_back`；復原失敗為 `needs_attention`；執行端斷線或 Core 中途停止為 `unknown`，由 `Engine.Reconcile` 以執行端紀錄接續，不重跑不可逆步驟。
- 預算：每類並行上限（`jobs.Options.Budgets`）與資料目錄空間保留（`ReserveBytes`）。
- 工作端點掛在 IAM 驗證之後：讀取要全域 `platform:read`，提交與取消要全域 `platform:write`；各工作種類自己的權限與範圍隨票 010 接上。

## 首次登入與權限（票 009）

- 沒有任何帳號時，啟動會把一次性設定碼印到 **stdout**（不進日誌），一小時內用 `POST /management/v1/setup` 建立擁有者；之後這個端點回 `409 SETUP_DONE`。
- `POST /management/v1/auth/login` 以 argon2id 驗證密碼，成功後設 `hazelminx_session`（HttpOnly、SameSite=Strict）與 `hazelminx_csrf` Cookie，並回 `csrfToken`。寫入類請求必須帶 `X-CSRF-Token`，且 `Origin`／`Referer` 須是本站。
- `/management/v1` 下除了 `setup` 與 `auth/login`，沒有工作階段一律 `401`。角色表在 `GET /management/v1/roles`；沒有對應授權的呼叫回 `403`，猜到別人範圍的資源 ID 一律回 `404`（與不存在的資源回應完全相同）。
- 登入失敗依帳號與來源分別限制（帳號冷卻最長 15 分鐘、來源 15 分鐘內 20 次即封鎖 15 分鐘）；曾成功登入該帳號的來源不受帳號冷卻影響，所以攻擊者無法永久鎖死管理員。不存在的帳號也套同一規則，不洩漏帳號存在與否。
- 每次登入、建帳號、指派或撤銷角色、拒絕存取都寫入 `audit_events`（`GET /management/v1/audit-events`，只回有 `audit:read` 的範圍）。寫入前會遮罩 `password`、`token`、`secret`、`key` 這類鍵。
- 每個 `/management/v1` 回應都帶 `X-Trace-ID`（呼叫端有送就沿用，沒送就產生），錯誤內容與稽核事件以 `traceId` 記錄同一個值。`requestId` 只用來指使用者操作的請求識別碼，也就是工作重送去重用的那個。
- `X-Forwarded-For` 預設不信任（`api.IAMOptions.TrustedProxies` 為空），來源 IP 取連線位址；接 Zentinel 後由票 065 設定可信代理。

## 目錄

| 路徑 | 內容 |
|---|---|
| `cmd/hazelminx-core` | 進入點：設定、儲存、HTTP、優雅關閉 |
| `internal/config` | 旗標與環境變數載入、驗證 |
| `internal/store` | SQLite 開啟、WAL、`synchronous=FULL`、開檔前 `quick_check`、只升不降的 migration（`migrations/NNNN_name.sql`） |
| `internal/api` | Gin 路由、健康檢查、總覽、工作 API、介面靜態檔；`iam_routes.go` 是工作階段、CSRF 與授權中介層（`RequireGlobal`、`RequireScoped`）與 IAM 端點 |
| `internal/jobs` | 工作引擎：狀態機、冪等鍵、資源鎖、預算、事件序號、診斷、SQLite 儲存 |
| `internal/iam` | 帳號、角色與資源範圍、密碼雜湊、工作階段、預設拒絕的授權判定、稽核紀錄、登入嘗試限制 |
| `internal/secrets` | 機密參照（加密值、版本、使用範圍、到期、變更紀錄）、主金鑰檔與輪替、Token 雜湊查驗 |
| `web` | 嵌入式前端（`embedweb` build tag） |

## 資料目錄與復原狀態

| 檔案 | 內容 | 權限 |
|---|---|---|
| `core.db`（含 `-wal`、`-shm`） | 平台資料庫 | 0600 |
| `master.key` | 主金鑰檔：各世代金鑰、目前作用中的世代、復原材料已取得的確認。不在資料庫內。 | 0600 |

啟動時依序檢查，任一不過就以非零結束並說明原因，不會建立空白資料庫冒充全新安裝（DAT-002、REL-006、T-009、T-012）：

- `master.key` 存在但 `core.db` 不在：資料庫遺失，進復原狀態。
- `core.db` 無法通過 `PRAGMA quick_check`：資料庫損壞，檔案原樣保留。
- schema 版本比執行檔新：拒絕開啟。
- `master.key` 可被擁有者以外讀取、或無法解析：拒絕啟動。
- 資料庫裡有密文引用主金鑰檔沒有的世代：拒絕啟動並列出缺少的世代（舊備份搭配新主金鑰檔時會在這裡被攔下，而不是還原時才發現）。

主金鑰輪替（`secrets.Service.RotateMasterKey`）分三步：新世代先寫進 `master.key` 標為 pending → 一個交易內把所有密文重新包裝並用新舊兩把金鑰驗證 → `master.key` 把新世代設為作用中、舊世代標為 retired。舊世代永遠保留在檔案裡，所以輪替前的備份仍可解密。中途失敗時交易回滾、舊世代維持作用中；中途斷電由下次啟動依資料庫實際引用的世代收尾。
