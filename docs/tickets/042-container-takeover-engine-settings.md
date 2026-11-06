# 042 — 管理員可以匯入既有容器選擇接管，並管理引擎設定、儲存位置與 GPU 裝置

**Epic:** 容器引擎（`epic:containers`）  
**User Story:** 管理員可以管理容器引擎  
**Milestone:** M2 容器、Compose 與 GitHub  
**Blocked by:** [040 容器管理員可以建立、檢視、操作容器並查看日誌、終端機與暴露範圍](040-container-management.md), [032 管理員可以設定資料位置並以裝置身分綁定，掛錯磁碟時服務不啟動](032-storage-bindings.md)  
**Status:** blocked  
**規格章節:** 14｜Docker、Podman、映像與登錄站、44｜遷移、授權與長期維護  
**Issue:** [#42](https://github.com/HazelnutParadise/hazelminx/issues/42)  

## What it delivers

44.2 唯讀盤點與接管、缺原 Compose 的提示、引擎設定與資料搬移的計畫、GPU／裝置授權（不以 privileged 當通用解法）。

## Acceptance criteria

- [ ] **CTR-009** 管理員可以匯入既有容器並選擇唯讀觀察或正式接管。 — 完成條件：不先重建容器；缺少原 Compose 時明示無法完整還原來源設定。
- [ ] **CTR-010** 管理員可以變更引擎設定與儲存位置。 — 完成條件：驗證設定、容量及需重啟影響；搬移引擎資料不是單純改一個路徑。
- [ ] **CTR-011** 管理員可以管理 GPU 與其他硬體裝置的容器授權。 — 完成條件：顯示實際裝置與驅動需求；不以 `privileged` 作所有問題的通用解法。
- [ ] 接管不重建健康容器；外部資源標記後不隨應用刪除

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
