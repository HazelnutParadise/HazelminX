# 069 — 管理員可以安裝或接管資料庫服務，管理帳號、連線、健康與對外開放

**Epic:** 資料庫（`epic:database`）  
**User Story:** 使用者可以在平台內管理資料庫  
**Milestone:** M4 資料庫、AI、備份與監控  
**Blocked by:** [052 管理員可以同步官方商店並安裝整套應用，選擇節點、網路、Port 與資料位置](052-appstore-install.md), [040 容器管理員可以建立、檢視、操作容器並查看日誌、終端機與暴露範圍](040-container-management.md)  
**Status:** blocked  
**規格章節:** 24｜資料庫管理中心  
**Issue:** [#68](https://github.com/HazelnutParadise/hazelminx/issues/68)  

## What it delivers

容器化安裝、既有容器與遠端實例接管、服務生命週期與資料權限分開、帳號與權限與 TLS、健康分類（網路／驗證／權限／TLS／未就緒）、對外開放政策、MySQL／MariaDB／PostgreSQL／Redis／MongoDB 基線對照。

## Acceptance criteria

- [ ] **DB-001** 管理員可以安裝容器化資料庫、連接既有容器或遠端實例。 — 完成條件：接管不重建資料；版本、來源、TLS 與實際連線角色可見。
- [ ] **DB-002** 管理員可以分開管理服務生命週期與資料內容權限。 — 完成條件：可查詢資料者不自動取得刪除容器、掛載主機或讀取 root 密碼的權限。
- [ ] **DB-004** 管理員可以建立資料庫帳號、權限及連線設定。 — 完成條件：最小權限與 TLS 驗證可用；密碼更換不只改 Compose 環境變數而忽略實際資料庫。
- [ ] **DB-005** 管理員可以查看連線、容量、版本與健康狀態。 — 完成條件：能區分網路不通、驗證失敗、權限不足、TLS 不符與資料庫未就緒。
- [ ] **DB-008** 管理員可以決定資料庫是否對外開放。 — 完成條件：預設只允許授權內網通道；不把 HTTP Zentinel 路由當 PostgreSQL／Redis 協定代理。
- [ ] **DB-010** 管理員可以保留 1Panel 基線其他資料庫能力。 — 完成條件：MySQL、MariaDB、PostgreSQL、Redis 與基線 OSS 其他引擎逐項對照；不因本書例子聚焦兩種就刪除其餘。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
