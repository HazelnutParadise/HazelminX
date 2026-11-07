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
| `internal/store` | SQLite 開啟、WAL、`synchronous=FULL`、開檔前 `quick_check`、只升不降的 migration（`migrations/NNNN_name.sql`） |
| `internal/secrets` | 機密參照（加密值、版本、使用範圍、到期、變更紀錄）、主金鑰檔與輪替、Token 雜湊查驗 |
| `internal/api` | Gin 路由、健康檢查、總覽、介面靜態檔 |
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
