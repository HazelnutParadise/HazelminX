# 128 — 控制中心的資料庫拒收型別錯誤的值，檔案與日誌大小不會無限增加

**Epic:** 基礎建設（`epic:foundation`）
**User Story:** 平台的狀態保存在一個可靠、不會默默變質的資料庫
**Milestone:** M1 單機主機管理
**Blocked by:** —
**規格章節:** 35｜資料保存（35.1）
**Issue:** [#191](https://github.com/HazelnutParadise/hazelminx/issues/191)

## What it delivers

控制中心的 SQLite 補上要手動打開的設定：所有資料表改成 STRICT、限制 WAL 檔大小、改用增量自動回收空間，並依 SQLite 現行建議執行 `PRAGMA optimize`。`synchronous=FULL` 與單一寫入連線不變。

到 0018 為止的 44 張資料表都不是 STRICT（`pragma_table_list` 的 `strict` 欄全為 0），不只 0001、0003、0013 建的那幾張。

## 決定

- **STRICT 重建**：migration 0019 依 SQLite 文件第 8 節的 12 步重建每張非 STRICT 表。關外鍵檢查必須在交易外，所以這個 migration 在固定的一條連線上關掉 `foreign_keys`、在交易裡重建、提交前跑 `foreign_key_check`，之後再打開。每張表照原本的 `CREATE TABLE` 文字加上 `STRICT`，索引與觸發器照原文重建，`AUTOINCREMENT` 的序號保留。複製前逐欄檢查每個值的實際型別：`INTEGER` 欄只接受整數、`REAL` 欄接受整數或實數、`TEXT` 欄只接受文字、`BLOB` 欄只接受位元組，都允許 NULL；有不符就中止並列出表、欄與筆數，不讓 SQLite 轉型。
- **`journal_size_limit` = 64 MiB**：平常 WAL 每 1000 頁（約 4 MiB）就檢查點歸零重用，不會長大；只有一次很大的交易（這次重建、VACUUM、一次清掉很多主機指標）會把 WAL 撐到接近資料庫大小，之後也不會縮回。64 MiB 是平常用量的十幾倍，平常不會截斷，大交易之後在下一次檢查點把檔案縮回 64 MiB。
- **`auto_vacuum=INCREMENTAL`**：新資料庫在建表前就設定。既有資料庫只能靠一次 `VACUUM` 轉換：開啟時發現還是 NONE，且資料目錄可用空間大於資料庫兩倍再加 64 MiB，就執行 `PRAGMA auto_vacuum=INCREMENTAL; VACUUM`；空間不夠就跳過、照常啟動並記一筆警告，下次開啟再試。`VACUUM` 失敗不會改壞資料庫（它本身是交易），同樣只記警告。
- **`PRAGMA optimize`**：控制中心只有一條長期連線，依 SQLite 文件對長期連線的建議，開啟時跑 `PRAGMA optimize=0x10002`，之後由每小時一次的主機指標清理迴圈接著跑 `PRAGMA optimize`。同一個迴圈在清理後跑 `PRAGMA incremental_vacuum`，把刪掉的頁還給檔案系統。

## Acceptance criteria

- [ ] 0019 之後每張資料表都是 STRICT；之後新增的表不是 STRICT 時測試會失敗。
- [ ] 從 0018 升級時，每張表的資料、索引、外鍵、觸發器與 `AUTOINCREMENT` 序號都保留，有測試。
- [ ] 舊資料有型別不符的值時，升級中止並指出表與欄，資料庫維持在 0018，有測試。
- [ ] 型別錯誤的寫入被拒絕，有測試。
- [ ] `journal_size_limit`、`auto_vacuum`、`synchronous` 在開啟後生效，有測試。
- [ ] 既有的 NONE 資料庫開啟後轉成 INCREMENTAL，空間不夠時跳過並照常開啟，有測試。
- [ ] 開啟時跑 `PRAGMA optimize=0x10002`，清理迴圈跑 `PRAGMA optimize` 與 `incremental_vacuum`。
- [ ] `go test -race ./...`（`core/`）通過。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
