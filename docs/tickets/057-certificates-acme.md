# 057 — 管理員可以申請、匯入、續期憑證並管理 ACME 帳號與 DNS 供應商

**Epic:** 網站入口（`epic:web`）  
**User Story:** 使用者可以對外發布網站  
**Milestone:** M3 網站入口與安全防護  
**Blocked by:** [056 使用者可以建立靜態與反向代理網站、綁定多網域並穩定路由到容器服務](056-websites-and-routing.md)  
**Status:** blocked  
**規格章節:** 19｜網站、Zentinel、網域與 HTTPS  
**Issue:** [#57](https://github.com/HazelnutParadise/hazelminx/issues/57)  

## What it delivers

HTTP／DNS 驗證、wildcard 與 Tunnel 情境、到期提醒、失敗診斷與限額保護、私鑰加密保存。

## Acceptance criteria

- [ ] **WEB-004** 管理員可以管理憑證、ACME 帳號與 DNS 供應商憑證。 — 完成條件：支援申請、匯入、更新、到期提醒與失敗診斷；私鑰遮罩並加密保存。
- [ ] **WEB-005** 使用者可以選擇 HTTP 或 DNS 驗證。 — 完成條件：確認實際網路可達性；Tunnel 與 wildcard 情境使用可行方式，不反覆撞失敗限額。
- [ ] 續期失敗時舊有效憑證保留，不裝入空檔案

## Acceptance tests（第 42 章）

- **T-079** TLS 憑證續期失敗。 → 舊有效憑證保留，到期前告警，不裝入空檔案。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
