# 003 — 管理員可以啟動 hazelminx-core 並通過健康檢查

**Epic:** 基礎建設（`epic:foundation`）  
**User Story:** 維護者可以接手專案  
**Milestone:** M0 基礎與設計  
**Blocked by:** [001 維護者可以在單一 repo 取得規格、架構決策、票務與工作規則](001-repo-scaffold.md)  
**Status:** ready  
**規格章節:** 31｜整體架構：每一部分負責什麼、35｜平台資料儲存、機密與復原包  
**Issue:** [#3](https://github.com/HazelnutParadise/hazelminx/issues/3)  

## What it delivers

Go 模組 core/ 可建置成單一執行檔：載入設定、開啟 SQLite（WAL）、執行 migration 框架、提供 /healthz 與嵌入式前端靜態檔、結構化日誌、優雅關閉。

## Acceptance criteria

- [ ] 乾淨 Linux 上只放執行檔即可啟動，不需 Node.js 或外部資料庫
- [ ] migration 有版本表與向上／拒絕向下的檢查
- [ ] 單元測試與 go vet 在 CI 通過

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
