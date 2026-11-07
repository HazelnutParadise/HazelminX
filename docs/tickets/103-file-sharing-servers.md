# 103 — 維運者可以管理 Samba 分享與 NFS exports 並看目前連線

**Epic:** 宿主機服務模組（`epic:host-services`）  
**User Story:** 維運者可以用 HazelminX 取代 Webmin 管理宿主機服務  
**Milestone:** M6 宿主機服務模組（Webmin 等價）  
**Blocked by:** [100](100.md), [023 維運者可以查看程序並管理 Linux 使用者、群組與 sudo](023-processes-users-sudo.md)  
**Status:** blocked  
**規格章節:** 增補 01｜49｜宿主機服務模組（Webmin 等價）  
**Issue:** [#110](https://github.com/HazelnutParadise/hazelminx/issues/110)  

## What it delivers

smb.conf 分享、權限、允許來源、testparm；exports、exportfs -r；目前連線。

## Acceptance criteria

- [ ] **HSM-007** 維運者可以管理 Samba 分享與 NFS exports（伺服器端）。 — 完成條件：分享路徑、權限、允許來源逐項顯示；`testparm`／`exportfs -r` 驗證通過才套用；列出目前連線。

## Acceptance tests（增補 01 · 49.3）

- **T-143** 新增 Samba 分享到不存在的路徑。 → 套用前擋下並說明；不寫入 smb.conf。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
