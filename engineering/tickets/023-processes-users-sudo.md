# 023 — 維運者可以查看程序並管理 Linux 使用者、群組與 sudo

**Epic:** 主機管理（`epic:host`）  
**User Story:** 維運者可以管理作業系統  
**Milestone:** M1 單機主機管理  
**Blocked by:** 012 維運者可以執行需要高權限的固定動作，而 Agent 主程序維持低權限  
**規格章節:** 10｜主機、套件、服務與網路  
**Issue:** [#23](https://github.com/HazelnutParadise/hazelminx/issues/23)  

## What it delivers

程序清單（PID、帳號、資源、啟動時間）、停止前重新核對身分、使用者／群組 CRUD、sudo 設定語法檢查與救援途徑保留。

## Acceptance criteria

- [ ] **SYS-006** 維運者可以查看與管理程序。 — 完成條件：顯示 PID、帳號、資源與啟動時間；停止前重新核對程序身分，避免 PID 已重用。
- [ ] **SYS-007** 維運者可以管理 Linux 使用者、群組與 sudo 權限。 — 完成條件：不刪除服務仍使用的帳號而不警告；sudo 設定先語法檢查，保留救援途徑。
- [ ] 不刪除服務仍使用的帳號而不警告

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
