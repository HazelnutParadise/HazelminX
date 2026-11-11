# 025 — 維運者可以重新開機或關機，並看到受影響的工作與服務

**Epic:** 主機管理（`epic:host`）  
**User Story:** 維運者可以管理作業系統  
**Milestone:** M1 單機主機管理  
**Blocked by:** 008 使用者可以提交工作後關閉頁面，之後仍能查到進度、證據與結果, 012 維運者可以執行需要高權限的固定動作，而 Agent 主程序維持低權限  
**規格章節:** 10｜主機、套件、服務與網路  
**Issue:** [#25](https://github.com/HazelnutParadise/hazelminx/issues/25)  

## What it delivers

重啟前列出執行中工作、叢集與應用影響；重啟後核對節點身分與工作恢復。

## Acceptance criteria

- [ ] **SYS-010** 維運者可以重新開機或關機。 — 完成條件：列出執行中工作、叢集與應用影響；重啟後核對節點身分與工作恢復情況。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
