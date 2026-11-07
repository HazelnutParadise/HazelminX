# 101 — 維運者可以管理 BIND 權威 DNS 與 DHCP（ISC／Kea）的 zone、紀錄、子網、租約

**Epic:** 宿主機服務模組（`epic:host-services`）  
**User Story:** 維運者可以用 HazelminX 取代 Webmin 管理宿主機服務  
**Milestone:** M6 宿主機服務模組（Webmin 等價）  
**Blocked by:** [100](100.md)  
**Status:** blocked  
**規格章節:** 增補 01｜49｜宿主機服務模組（Webmin 等價）  
**Issue:** [#108](https://github.com/HazelnutParadise/hazelminx/issues/108)  

## What it delivers

BIND：zone、紀錄、序號、轉送、checkzone／checkconf；DHCP：子網、範圍、固定分配、選項、租約表。

## Acceptance criteria

- [ ] **HSM-004** 維運者可以管理 BIND 權威 DNS：zone、紀錄（A／AAAA／CNAME／MX／TXT／SRV／PTR）、序號、轉送。 — 完成條件：寫入前 `named-checkzone`／`named-checkconf` 通過；序號自動遞增；可比對實際解析結果。
- [ ] **HSM-005** 維運者可以管理 DHCP（ISC／Kea）：子網、範圍、固定分配、選項，並查看租約。 — 完成條件：設定檔驗證通過才重載；租約表可搜尋 MAC／IP／主機名。

## Acceptance tests（增補 01 · 49.3）

- **T-140** 在 BIND 新增一筆 A 紀錄並套用。 → checkzone 通過、序號遞增、`dig` 查到新值；回滾到上一版後查不到。
- **T-141** 寫錯 DHCP 子網遮罩。 → 驗證失敗、不重載、原始錯誤可見、舊設定仍在服務。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
