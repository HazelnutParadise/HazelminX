# 068 — 管理員可以把 OpenResty 網站遷移到 Zentinel，無法轉換的項目先列出

**Epic:** 遷移與接管（`epic:migration`）  
**User Story:** 管理員可以從 1Panel 遷移  
**Milestone:** M3 網站入口與安全防護  
**Blocked by:** [056 使用者可以建立靜態與反向代理網站、綁定多網域並穩定路由到容器服務](056-websites-and-routing.md), [059 使用者可以建立 PHP、Java、Node.js、Go、Python、.NET 執行環境網站](059-language-runtimes.md), [060 使用者可以設定 TCP／UDP 代理與連接埠轉送](060-tcp-udp-proxy.md)  
**Status:** blocked  
**規格章節:** 19｜網站、Zentinel、網域與 HTTPS、39｜跨功能流程：從按鈕到真正完成  
**Issue:** [#67](https://github.com/HazelnutParadise/hazelminx/issues/67)  

## What it delivers

39.5 流程：盤點、可轉換／需替代／無法轉換清單、測試位址驗證、核准時段切換、失敗恢復原入口。

## Acceptance criteria

- [ ] **WEB-010** 管理員可以遷移 OpenResty 網站。 — 完成條件：先建立等效設定與測試入口；不先關舊服務；同 Port 切換有維護與回復計畫。

## Acceptance tests（第 42 章）

- **T-067** 原生 Zentinel 已占 80／443，再裝 K3s。 → 先呈現入口分配方案，不安裝到衝突才失敗。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
