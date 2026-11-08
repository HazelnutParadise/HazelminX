# 003 — 管理員可以啟動 hazelminx-core 並通過健康檢查

**Epic:** 基礎建設（`epic:foundation`）  
**User Story:** 維護者可以接手專案  
**Milestone:** M0 基礎與設計  
**Blocked by:** [001 維護者可以在單一 repo 取得規格、架構決策、票務與工作規則](001-repo-scaffold.md)  
**Status:** done  
**規格章節:** 31｜整體架構：每一部分負責什麼、35｜平台資料儲存、機密與復原包  
**Issue:** [#3](https://github.com/HazelnutParadise/hazelminx/issues/3)  

## What it delivers

Go 模組 core/ 可建置成單一執行檔：載入設定、開啟 SQLite（WAL）、執行 migration 框架、提供 /healthz 與嵌入式前端靜態檔、結構化日誌、優雅關閉。

## Acceptance criteria

- [x] 乾淨 Linux 上只放執行檔即可啟動，不需 Node.js 或外部資料庫 — 證據：`CGO_ENABLED=0 GOOS=linux GOARCH=arm64` 建置出靜態連結執行檔，放進沒有 Node.js、Go、sqlite3、資料庫服務的 `debian:stable-slim`（Debian 13）容器啟動；`/healthz` 回 `200 {"status":"ok","checks":{"store":"ok"}}`，schema 版本 4，資料檔皆 0600，SIGTERM 後記錄 shutting down 並以 0 結束（2026-11-07）。CI（票 006）在 x86_64 與 ARM64 的 Ubuntu 24.04 上，都以 `CGO_ENABLED=0` 建置並實際啟動，`/healthz` 回 ok，`file` 確認是靜態連結（[CI 執行 42443690986](https://github.com/HazelnutParadise/HazelminX/actions/runs/42443690986)）。
- [x] migration 有版本表與向上／拒絕向下的檢查 — 證據：`core/internal/store/store_test.go` `TestOpenCreatesAndMigrates`、`TestReopenIsIdempotent`、`TestRefusesNewerSchema`（資料庫比執行檔新時拒絕開啟且不覆寫）、`TestMigrationsAreContiguous`。
- [x] 單元測試與 go vet 在 CI 通過 — 證據：`.github/workflows/ci.yml` 的 `core` job 在 x86_64 與 ARM64 都跑了 `gofmt`、`go mod verify`、`go vet ./...`、`go test -race ./...`，全部通過（[CI 執行 42443690986](https://github.com/HazelnutParadise/HazelminX/actions/runs/42443690986)，PR #127）。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
