# 102 — 維運者可以管理 Postfix／Dovecot 的網域、信箱、別名、TLS 與佇列

**Epic:** 宿主機服務模組（`epic:host-services`）  
**User Story:** 維運者可以用 HazelminX 取代 Webmin 管理宿主機服務  
**Milestone:** M6 宿主機服務模組（Webmin 等價）  
**Blocked by:** [100](100.md), 011 管理員可以安全保存機密，且主金鑰可輪替並驗證舊備份仍可解密  
**規格章節:** 增補 01｜49｜宿主機服務模組（Webmin 等價）  
**Issue:** [#109](https://github.com/HazelnutParadise/hazelminx/issues/109)  

## What it delivers

網域、信箱、別名、轉寄、TLS（可沿用 Zentinel 憑證）、佇列查看／重送／刪除；密碼走機密庫。

## Acceptance criteria

- [ ] **HSM-006** 維運者可以管理 Postfix／Dovecot：網域、信箱、別名、轉寄、TLS、佇列。 — 完成條件：佇列可查看、重送、刪除；TLS 憑證可沿用 Zentinel 的憑證；不把密碼寫進變更紀錄。

## Acceptance tests（增補 01 · 49.3）

- **T-142** Postfix 佇列卡 50 封信。 → 可列出、重送、刪除；操作在稽核紀錄。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
