# 024 — 維運者可以管理網路、DNS、主機名稱、時間服務與防火牆

**Epic:** 主機管理（`epic:host`）  
**User Story:** 維運者可以管理作業系統  
**Milestone:** M1 單機主機管理  
**Blocked by:** [012 維運者可以執行需要高權限的固定動作，而 Agent 主程序維持低權限](012-privileged-executor.md), [008 使用者可以提交工作後關閉頁面，之後仍能查到進度、證據與結果](008-job-engine.md)  
**Status:** ready  
**規格章節:** 10｜主機、套件、服務與網路  
**Issue:** [#24](https://github.com/HazelnutParadise/hazelminx/issues/24)  

## What it delivers

網卡／IP／DNS／路由／主機名稱／時間服務變更與逾時回復；防火牆規則與 Docker／Podman／K3s 鏈的責任邊界；外部可達性測試。

## Acceptance criteria

- [ ] **SYS-008** 維運者可以管理網卡、IP、DNS、路由、主機名稱及時間服務。 — 完成條件：變更前列明可能斷線影響；連通性未確認時依逾時規則恢復可逆設定。
- [ ] **SYS-009** 維運者可以管理主機防火牆及容器對外暴露。 — 完成條件：顯示系統規則、Docker／Podman／K3s 規則的責任邊界，不清空引擎管理的鏈。[S09](#source-S09)
- [ ] 連通性未確認時依逾時規則恢復可逆設定
- [ ] Docker 發布的連接埠被 UFW 顯示拒絕時，外部測試能發現實際暴露

## Acceptance tests（第 42 章）

- **T-037** 修改網路後無法回連。 → 可逆部分在核准逾時政策下復原，顯示事件。
- **T-038** Docker 發布服務但 UFW 顯示拒絕。 → 外部測試能發現實際暴露，不虛報已封鎖。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
