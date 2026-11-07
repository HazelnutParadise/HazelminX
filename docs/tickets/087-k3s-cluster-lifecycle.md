# 087 — 管理員可以建立 K3s 叢集、加入／移除／升級節點並建立高可用

**Epic:** Kubernetes 與叢集（`epic:cluster`）  
**User Story:** 管理員可以管理叢集  
**Milestone:** M5 多節點、叢集、遷移與發布  
**Blocked by:** [086 管理員可以連接既有 Kubernetes／K3s 並管理工作負載、Service、設定與診斷](086-kubernetes-connect.md), [055 管理員可以從介面安裝原生 Zentinel，Core 重啟不影響既有網站，救援入口不依賴它](055-zentinel-install.md), [016 管理員可以預覽並批准高風險變更計畫，內容改變時原批准失效](016-change-plans.md)  
**Status:** blocked  
**規格章節:** 17｜Kubernetes、K3s 與叢集  
**Issue:** [#86](https://github.com/HazelnutParadise/hazelminx/issues/86)  

## What it delivers

預檢（OS、架構、核心、時間、資源、網段、Port）、入口分配避免 Traefik／ServiceLB 與 Zentinel 搶 80／443、排空與多數決保護、datastore 與拓樸驗證。

## Acceptance criteria

- [ ] **KUB-002** 管理員可以從 K8s 頁建立 K3s 叢集。 — 完成條件：預檢 OS、架構、核心、時間、主機名、資源、網段及 Port；不擅自關閉防火牆。[S21](#source-S21)
- [ ] **KUB-003** 管理員可以加入、移除與升級節點。 — 完成條件：檢查版本相容與資料角色；先排空可搬移工作；不失去控制節點多數決能力。
- [ ] **KUB-004** 管理員可以建立多控制節點高可用叢集。 — 完成條件：依選定 datastore 與拓樸驗證；不能把兩個獨立單節點畫成高可用。
- [ ] **KUB-013** 管理員可以明確設定 Zentinel 與 K3s 入口的責任。 — 完成條件：安裝前避免 Traefik／ServiceLB 與原生 Zentinel 搶 80／443；現有控制器不擅自移除。[S10](#source-S10)
- [ ] 沒有叢集時 Kubernetes 頁顯示「連接既有叢集／建立 K3s 叢集」兩個選擇，建立流程依序是選節點與角色、預檢（OS、時間、主機名、Port、網段、80／443 與 Zentinel、防火牆、與既有引擎共存）、批准建立（畫板 KubernetesEmpty）。

## Acceptance tests（第 42 章）

- **T-067** 原生 Zentinel 已占 80／443，再裝 K3s。 → 先呈現入口分配方案，不安裝到衝突才失敗。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
