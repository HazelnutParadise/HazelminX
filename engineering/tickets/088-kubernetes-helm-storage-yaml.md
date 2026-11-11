# 088 — 使用者可以安裝 Helm Chart、管理儲存、匯入 YAML、備份 datastore 並安排維護

**Epic:** Kubernetes 與叢集（`epic:cluster`）  
**User Story:** 管理員可以管理叢集  
**Milestone:** M5 多節點、叢集、遷移與發布  
**Blocked by:** 086 管理員可以連接既有 Kubernetes／K3s 並管理工作負載、Service、設定與診斷  
**規格章節:** 17｜Kubernetes、K3s 與叢集、13｜磁碟、儲存與資料位置  
**Issue:** [#87](https://github.com/HazelnutParadise/hazelminx/issues/87)  

## What it delivers

固定來源與版本的 Helm、PVC／PV／StorageClass 與 reclaim 後果、乾跑與欄位擁有權、K3s datastore 與 token 備份、PDB 與排空檢查、本機磁碟綁定節點。

## Acceptance criteria

- [ ] **KUB-007** 使用者可以安裝與更新 Helm Chart。 — 完成條件：固定來源與版本，顯示 values、差異、資源與 hooks；保護自訂設定及機密。
- [ ] **KUB-008** 管理員可以管理 PVC、PV、StorageClass 與可用快照。 — 完成條件：區分物件備份與實際資料備份；刪除前顯示 reclaim policy 及資料後果。
- [ ] **KUB-010** 管理員可以備份叢集設定與 K3s datastore。 — 完成條件：同時處理必要 token／金鑰，並與工作負載資料備份分開驗證。
- [ ] **KUB-011** 管理員可以安排不中斷或受控中斷的維護。 — 完成條件：檢查副本、readiness、儲存、資源餘裕與 PodDisruptionBudget；無法排空時說明。
- [ ] **KUB-012** 使用者可以用 YAML 進階編輯與匯入。 — 完成條件：乾跑檢查、差異、欄位擁有權與版本衝突可見，不靜默搶走其他 controller 管理的欄位。
- [ ] **STO-012** 管理員可以為 Kubernetes 選擇儲存類別與存取模式。 — 完成條件：本機磁碟綁定正確節點；多節點存取能力由實際儲存提供，不靠 UI 假裝共用。

## Acceptance tests（第 42 章）

- **T-070** 本機 PVC 的 Pod 被要求搬到另一節點。 → 顯示資料位置限制，不當成資料會自動跟著走。
- **T-071** 排空有不可中斷工作或不足副本的節點。 → 說明阻擋與風險，不強制宣稱零中斷。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
