# 105 — 維運者可以管理 logrotate 與磁碟配額

**Epic:** 宿主機服務模組（`epic:host-services`）  
**User Story:** 維運者可以用 HazelminX 取代 Webmin 管理宿主機服務  
**Milestone:** M6 宿主機服務模組（Webmin 等價）  
**Blocked by:** [100](100.md), [030 維運者可以查看磁碟、分割區、檔案系統、健康與容量告警](030-disk-overview-alerts.md)  
**Status:** blocked  
**規格章節:** 增補 01｜49｜宿主機服務模組（Webmin 等價）  
**Issue:** [#112](https://github.com/HazelnutParadise/hazelminx/issues/112)  

## What it delivers

logrotate 每組日誌的頻率、保留、壓縮、乾跑；配額啟用、使用者／群組軟硬限制、寬限、目前用量。

## Acceptance criteria

- [ ] **HSM-009** 維運者可以管理 logrotate：每組日誌的輪替頻率、保留數、壓縮、輪替後動作。 — 完成條件：可乾跑（`logrotate -d`）看結果；不影響平台自己的日誌保留政策（35 章）。
- [ ] **HSM-010** 維運者可以管理磁碟配額：每個檔案系統啟用配額、每使用者／群組的軟硬限制與寬限。 — 完成條件：顯示目前用量對限制；檔案系統未啟用配額時說明要怎麼啟用（重新掛載）。

## Acceptance tests（增補 01 · 49.3）

- **T-145** logrotate 乾跑。 → 顯示會輪替哪些檔與保留幾份，不實際動檔案。
- **T-146** 使用者超過配額軟限制。 → 配額頁標示超限與寬限剩餘時間。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
