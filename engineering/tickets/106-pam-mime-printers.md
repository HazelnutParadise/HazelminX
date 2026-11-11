# 106 — 維運者可以管理 PAM、MIME 對映與 CUPS 印表機

**Epic:** 宿主機服務模組（`epic:host-services`）  
**User Story:** 維運者可以用 HazelminX 取代 Webmin 管理宿主機服務  
**Milestone:** M6 宿主機服務模組（Webmin 等價）  
**Blocked by:** [100](100.md), 009 擁有者可以登入、建立帳號與角色，未授權的 API 呼叫一律被拒絕  
**規格章節:** 增補 01｜49｜宿主機服務模組（Webmin 等價）  
**Issue:** [#113](https://github.com/HazelnutParadise/hazelminx/issues/113)  

## What it delivers

PAM 服務設定（含鎖死風險警告與救援路徑檢查）、MIME 類型、CUPS 印表機與佇列。

## Acceptance criteria

- [ ] **HSM-011** 維運者可以查看與編輯 PAM 服務設定與 MIME 類型對映。 — 完成條件：編輯 PAM 前顯示影響的登入方式與鎖死風險；保留救援路徑（IAM-008）。
- [ ] **HSM-012** 維運者可以管理 CUPS 印表機：新增、佇列、暫停、預設。 — 完成條件：未安裝 CUPS 時顯示安裝入口而不是空頁。

## Acceptance tests（增補 01 · 49.3）

- **T-147** 編輯 PAM sshd 把自己鎖在外面。 → 套用前警告並要求確認救援路徑存在；Agent 通道不受 sshd 影響。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
