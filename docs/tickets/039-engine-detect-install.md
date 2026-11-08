# 039 — 管理員可以看到 Docker、Podman、K3s 各自狀態並在頁面一鍵安裝 Docker

**Epic:** 容器引擎（`epic:containers`）  
**User Story:** 管理員可以管理容器引擎  
**Milestone:** M2 容器、Compose 與 GitHub  
**Blocked by:** [019 管理員可以用一次性註冊碼加入節點，並看到離線、過期、維護與撤銷狀態](019-node-enrollment.md), [016 管理員可以預覽並批准高風險變更計畫，內容改變時原批准失效](016-change-plans.md), [012 維運者可以執行需要高權限的固定動作，而 Agent 主程序維持低權限](012-privileged-executor.md)  
**Status:** blocked  
**規格章節:** 09｜節點加入、自動偵測與多節點、39｜跨功能流程：從按鈕到真正完成  
**Issue:** [#39](https://github.com/HazelnutParadise/hazelminx/issues/39)  

## What it delivers

引擎偵測（未安裝／已停止／不相容／無權限分開）、rootless Podman 多使用者環境辨識、39.2 的安裝計畫與批准、安裝後驗證（Engine、Compose、Buildx、Socket 權限）。

## Acceptance criteria

- [ ] **NOD-003** 管理員可以看到各引擎的安裝、執行、版本與可管理能力。 — 完成條件：分開顯示 Docker、Podman、Kubernetes／K3s、Swarm 與相關 runtime，不重複計數同一工作負載。
- [ ] **NOD-004** 管理員可以管理不同 Linux 使用者的 rootless Podman 環境。 — 完成條件：明示擁有者與 socket；甲使用者容器不誤顯示為乙使用者資源。[S17](#source-S17)
- [ ] **INS-008** 管理員可以在 Docker、Podman 或 K8s 頁面安裝缺少的元件。 — 完成條件：顯示未安裝、已停止、不相容與無權限的不同狀態；不把全部問題當成沒安裝。
- [ ] 已有 K3s 的 containerd 不自動移除
- [ ] 失敗時不自動關閉防火牆、停掉另一個引擎或把 Socket 改成所有人可寫
- [ ] 選到沒有容器引擎的節點時，Docker／Podman 頁顯示未安裝狀態：Docker、Podman 選擇卡與導向 Kubernetes 頁的 K3s 卡；選定後顯示預檢、變更計畫（套件、服務、網路與防火牆、daemon.json、群組、下載量與時間）與安裝流程（畫板 ContainersEmpty）。
- [ ] 已安裝的引擎可以停止、啟動、重新安裝、移除；移除先列出受影響容器與資料目錄，選擇保留到哪一層（只停用／移除套件保留資料／連資料一併移除，後者先備份）並輸入名稱確認。
- [ ] **ENV-001／ENV-002**（增補 02）每個節點列出所有環境（引擎＋擁有者＋連接端點），同種引擎多個時各自一列；以端點實際回應的引擎判斷種類（`podman-docker` 的 docker.sock 辨識為 Podman）。
- [ ] **ENV-006** 安裝或建立環境前檢查與既有環境的 Port、網段、cgroup 驅動、防火牆後端、containerd 衝突，列出佔用者，不自動停用任一方。

## Acceptance tests（第 42 章）

- **T-051** 同主機有 Docker、rootful 與兩個 rootless Podman。 → 分開辨識四個環境與使用者，不混淆容器。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
