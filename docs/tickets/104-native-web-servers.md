# 104 — 維運者可以管理宿主機原生 Apache／Nginx 的虛擬主機與模組，並與 Zentinel 分工

**Epic:** 宿主機服務模組（`epic:host-services`）  
**User Story:** 維運者可以用 HazelminX 取代 Webmin 管理宿主機服務  
**Milestone:** M6 宿主機服務模組（Webmin 等價）  
**Blocked by:** [100](100.md), [060 使用者可以設定 TCP／UDP 代理與連接埠轉送](060-tcp-udp-proxy.md)  
**Status:** blocked  
**規格章節:** 增補 01｜49｜宿主機服務模組（Webmin 等價）  
**Issue:** [#111](https://github.com/HazelnutParadise/hazelminx/issues/111)  

## What it delivers

虛擬主機、模組、configtest／nginx -t、Port 衝突檢查、與 Zentinel 的責任分工說明。

## Acceptance criteria

- [ ] **HSM-008** 維運者可以管理宿主機原生的 Apache／Nginx：虛擬主機、模組、設定檔。 — 完成條件：與 Zentinel 的責任分工明寫（誰終止 TLS、誰對外）；Port 衝突在套用前檢查；`apachectl configtest`／`nginx -t` 通過才重載。

## Acceptance tests（增補 01 · 49.3）

- **T-144** Nginx 虛擬主機要綁 443 而 Zentinel 已占用。 → 套用前 Port 衝突擋下，提示改走 Zentinel 站點或換 Port。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
