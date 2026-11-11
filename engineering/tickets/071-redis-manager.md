# 071 — 使用者可以管理 Redis 鍵、TTL、ACL、持久化與拓樸

**Epic:** 資料庫（`epic:database`）  
**User Story:** 使用者可以在平台內管理資料庫  
**Milestone:** M4 資料庫、AI、備份與監控  
**Blocked by:** 069 管理員可以安裝或接管資料庫服務，管理帳號、連線、健康與對外開放  
**規格章節:** 24｜資料庫管理中心  
**Issue:** [#70](https://github.com/HazelnutParadise/hazelminx/issues/70)  

## What it delivers

SCAN 漸進查詢、各型別檢視與分段讀取、TTL 保留、危險指令獨立授權、RDB／AOF、單機／Sentinel／Cluster 差異、監控有時限。

## Acceptance criteria

- [ ] **RED-001** 使用者可以瀏覽鍵與 String、Hash、List、Set、Sorted Set、Stream 等支援型別。 — 完成條件：大型值分段讀取；未知模組型別明示，不能錯誤序列化後覆寫。
- [ ] **RED-002** 使用者可以搜尋與逐步掃描鍵。 — 完成條件：預設使用 SCAN 類操作，不對正式大資料庫直接執行 `KEYS *`；說明掃描不是固定快照。[S29](#source-S29)
- [ ] **RED-003** 使用者可以設定或保留 TTL。 — 完成條件：修改值時明示是否保留到期時間；-1 的不過期與 -2 的不存在分開顯示。
- [ ] **RED-004** 管理員可以管理 ACL 與受限制指令。 — 完成條件：危險指令獨立授權；FLUSHDB、FLUSHALL 等顯示實際目標與影響。
- [ ] **RED-005** 管理員可以管理持久化與備份。 — 完成條件：依實例版本正確處理 RDB／AOF 及重寫狀態；記憶體資料不能只靠拷貝目錄當備份。
- [ ] **RED-006** 使用者可以查看單機、Sentinel 或 Cluster 的實際拓樸。 — 完成條件：Cluster 不提供無效的資料庫切換功能；slot、節點及連線差異可見。
- [ ] **RED-007** 管理員可以查看記憶體、淘汰、命中、慢查詢與客戶端。 — 完成條件：高成本監控有時間限制；不預設長期開 MONITOR 洩漏內容與拖慢服務。

## Acceptance tests（第 42 章）

- **T-101** Redis 大量 Key、二進位內容與 TTL。 → 漸進查詢；不以 KEYS 阻塞；型別與到期行為正確。
- **T-102** Redis Cluster 切換非 0 資料庫。 → 介面與後端正確限制，不宣稱任意資料庫可建立。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
