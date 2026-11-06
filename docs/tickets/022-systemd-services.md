# 022 — 維運者可以管理 systemd 服務、計時器與其設定檔

**Epic:** 主機管理（`epic:host`）  
**User Story:** 維運者可以管理作業系統  
**Milestone:** M1 單機主機管理  
**Blocked by:** [012 維運者可以執行需要高權限的固定動作，而 Agent 主程序維持低權限](012-privileged-executor.md)  
**Status:** blocked  
**規格章節:** 10｜主機、套件、服務與網路  
**Issue:** [#22](https://github.com/HazelnutParadise/hazelminx/issues/22)  

## What it delivers

unit 清單與相依、啟停／重啟／重載／開機啟用、設定檔編輯（原檔備份、語法檢查、版本衝突、套用紀錄）。

## Acceptance criteria

- [ ] **SYS-004** 維運者可以管理 systemd 服務與計時器。 — 完成條件：能查看實際 unit、啟停、重啟、重載、開機啟用及相依關係；不支援重載時不假裝成功。
- [ ] **SYS-005** 維運者可以修改系統服務設定。 — 完成條件：原檔備份、語法檢查、版本衝突偵測與套用結果完整記錄；不無差別覆寫整個檔案。
- [ ] 不支援重載時不假裝成功
- [ ] 不無差別覆寫整個檔案

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
