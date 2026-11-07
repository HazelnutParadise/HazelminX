# hazelminx-core

HazelminX 的控制中心：管理 API、權限、工作協調、設定版本與嵌入式網頁介面。單一 Go 執行檔，資料放在 SQLite（純 Go 驅動，主機不需要 C 編譯器）。

## 建置與測試

```bash
go build ./cmd/hazelminx-core
go test ./...
```

嵌入前端：CI 先建置 `web/dist`，複製到 `core/web/dist`，再以 `-tags embedweb` 建置。沒有這個 tag 時，介面路徑回 `503 UI_NOT_EMBEDDED`，管理 API 照常可用。

## 執行

```bash
./hazelminx-core -listen 127.0.0.1:9800 -data-dir /var/lib/hazelminx
```

| 設定 | 旗標 | 環境變數 | 預設 |
|---|---|---|---|
| 管理 API 監聽位址 | `-listen` | `HAZELMINX_LISTEN_ADDR` | `127.0.0.1:9800` |
| 資料目錄 | `-data-dir` | `HAZELMINX_DATA_DIR` | `/var/lib/hazelminx` |
| 日誌格式 | `-log-format` | `HAZELMINX_LOG_FORMAT` | `json` |

端點：`GET /healthz`（含資料庫健康）、`GET /management/v1`（操作總覽，列出全部操作）。完整契約見票 010。

## 首次登入與權限（票 009）

- 沒有任何帳號時，啟動會把一次性設定碼印到 **stdout**（不進日誌），一小時內用 `POST /management/v1/setup` 建立擁有者；之後這個端點回 `409 SETUP_DONE`。
- `POST /management/v1/auth/login` 以 argon2id 驗證密碼，成功後設 `hazelminx_session`（HttpOnly、SameSite=Strict）與 `hazelminx_csrf` Cookie，並回 `csrfToken`。寫入類請求必須帶 `X-CSRF-Token`，且 `Origin`／`Referer` 須是本站。
- `/management/v1` 下除了 `setup` 與 `auth/login`，沒有工作階段一律 `401`。角色表在 `GET /management/v1/roles`；沒有對應授權的呼叫回 `403`，猜到別人範圍的資源 ID 一律回 `404`（與不存在的資源回應完全相同）。
- 登入失敗依帳號與來源分別限制（帳號冷卻最長 15 分鐘、來源 15 分鐘內 20 次即封鎖 15 分鐘）；曾成功登入該帳號的來源不受帳號冷卻影響，所以攻擊者無法永久鎖死管理員。不存在的帳號也套同一規則，不洩漏帳號存在與否。
- 每次登入、建帳號、指派或撤銷角色、拒絕存取都寫入 `audit_events`（`GET /management/v1/audit-events`，只回有 `audit:read` 的範圍）。寫入前會遮罩 `password`、`token`、`secret`、`key` 這類鍵。
- `X-Forwarded-For` 預設不信任（`api.IAMOptions.TrustedProxies` 為空），來源 IP 取連線位址；接 Zentinel 後由票 065 設定可信代理。

## 目錄

| 路徑 | 內容 |
|---|---|
| `cmd/hazelminx-core` | 進入點：設定、儲存、HTTP、優雅關閉 |
| `internal/config` | 旗標與環境變數載入、驗證 |
| `internal/store` | SQLite 開啟、WAL、只升不降的 migration（`migrations/NNNN_name.sql`） |
| `internal/api` | Gin 路由、健康檢查、總覽、介面靜態檔；`iam_routes.go` 是工作階段、CSRF 與授權中介層（`RequireGlobal`、`RequireScoped`）與 IAM 端點 |
| `internal/iam` | 帳號、角色與資源範圍、密碼雜湊、工作階段、預設拒絕的授權判定、稽核紀錄、登入嘗試限制 |
| `web` | 嵌入式前端（`embedweb` build tag） |

資料庫 schema 比執行檔新時拒絕啟動，不會建立空白資料庫（REL-006、T-009、T-012）。
