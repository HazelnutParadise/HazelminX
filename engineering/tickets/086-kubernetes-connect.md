# 086 — 管理員可以連接既有 Kubernetes／K3s 並管理工作負載、Service、設定與診斷

**Epic:** Kubernetes 與叢集（`epic:cluster`）  
**User Story:** 管理員可以管理叢集  
**Milestone:** M5 多節點、叢集、遷移與發布  
**Blocked by:** 019 管理員可以用一次性註冊碼加入節點，並看到離線、過期、維護與撤銷狀態  
**規格章節:** 17｜Kubernetes、K3s 與叢集  
**Issue:** [#85](https://github.com/HazelnutParadise/hazelminx/issues/85)  

## What it delivers

CA 與身分驗證、預設唯讀、Namespace／Deployment／StatefulSet／DaemonSet／Pod／Job／CronJob、Service／Ingress／Gateway／ConfigMap／Secret／網路政策、整合 Events 與日誌的診斷、標籤／污點／配額。

## Acceptance criteria

- [ ] **KUB-001** 管理員可以連接既有 Kubernetes／K3s。 — 完成條件：驗證 CA、叢集身分與實際權限；預設先唯讀，不把 kubeconfig 任意開給瀏覽器。
- [ ] **KUB-005** 使用者可以管理 Namespace、Deployment、StatefulSet、DaemonSet、Pod、Job 與 CronJob。 — 完成條件：提供物件狀態、事件、日誌、設定、更新與回復；尊重叢集 RBAC。
- [ ] **KUB-006** 使用者可以管理 Service、Ingress、Gateway、ConfigMap、Secret 與網路政策。 — 完成條件：顯示實際控制器支援能力；Secrets 不只以 Base64 就宣稱加密。[S22](#source-S22)
- [ ] **KUB-009** 使用者可以診斷排程、映像、探針、掛載、DNS 與網路失敗。 — 完成條件：同頁整合 Events、狀態與相關日誌，不能只顯示 CrashLoopBackOff 字樣。
- [ ] **KUB-014** 使用者可以查看節點標籤、污點、資源配額與擴縮設定。 — 完成條件：能理解服務為何不能放到某節點；不可用容量不當作可部署容量。
- [ ] 以 Namespace 限定權限操作別的 Namespace 時後端與叢集都拒絕
- [ ] **ENV-005** 同一台主機的多個叢集（K3s、MicroK8s、kind、k3d、minikube）各自是管理目標，Kubernetes 頁範圍列可切換；跑在容器裡的叢集標出承載它的環境。

## Acceptance tests（第 42 章）

- **T-068** 接管已有其他 Ingress Controller 的叢集。 → 不自動刪除或改現有預設入口。
- **T-069** 以 Namespace 限定權限操作別的 Namespace。 → 後端與 Kubernetes 權限都拒絕。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
