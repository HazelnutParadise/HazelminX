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

端點：`GET /healthz`（含資料庫健康）、`GET /management/v1`（操作總覽）。完整契約見票 010。

## 目錄

| 路徑 | 內容 |
|---|---|
| `cmd/hazelminx-core` | 進入點：設定、儲存、HTTP、優雅關閉 |
| `internal/config` | 旗標與環境變數載入、驗證 |
| `internal/store` | SQLite 開啟、WAL、只升不降的 migration（`migrations/NNNN_name.sql`） |
| `internal/api` | Gin 路由、健康檢查、總覽、介面靜態檔 |
| `web` | 嵌入式前端（`embedweb` build tag） |

資料庫 schema 比執行檔新時拒絕啟動，不會建立空白資料庫（REL-006、T-009、T-012）。
