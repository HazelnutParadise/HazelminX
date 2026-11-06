# 034 — 維運者可以管理 FTP／SFTP、Fail2ban、防毒等基線工具

**Epic:** 主機管理（`epic:host`）  
**User Story:** 維運者可以管理作業系統  
**Milestone:** M1 單機主機管理  
**Blocked by:** [021 維運者可以搜尋、安裝、更新、移除套件並套用安全更新](021-packages-and-updates.md), [022 維運者可以管理 systemd 服務、計時器與其設定檔](022-systemd-services.md)  
**Status:** blocked  
**規格章節:** 10｜主機、套件、服務與網路  
**Issue:** [#34](https://github.com/HazelnutParadise/hazelminx/issues/34)  

## What it delivers

與 1Panel 基線對照的安裝、設定、紀錄與失敗處理；明確標示外部工具與所需套件。

## Acceptance criteria

- [ ] **SYS-011** 維運者可以管理 FTP／SFTP、Fail2ban、防毒等基線既有工具。 — 完成條件：OSS 已有功能逐一保留；明確標示外部工具與所需套件，不把 SFTP 混稱 FTP。

## Acceptance tests（第 42 章）

- **T-134** 支援的 FTP／Fail2ban／防毒工具操作。 → 與基線對照的安裝、設定、紀錄與失敗處理完整，不用空按鈕替代。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
