# 041 — 管理員可以管理映像、登錄站、容器網路與 Volume 並採用安全更新

**Epic:** 容器引擎（`epic:containers`）  
**User Story:** 管理員可以管理容器引擎  
**Milestone:** M2 容器、Compose 與 GitHub  
**Blocked by:** 039 管理員可以看到 Docker、Podman、K3s 各自狀態並在頁面一鍵安裝 Docker  
**規格章節:** 14｜Docker、Podman、映像與登錄站  
**Issue:** [#41](https://github.com/HazelnutParadise/hazelminx/issues/41)  

## What it delivers

映像下載／建置／標記／推送／清理（引用關係）、多登錄站憑證範圍、網路 CRUD 與衝突、Volume 類型區分與刪除確認、新舊 digest 比較。

## Acceptance criteria

- [ ] **CTR-005** 管理員可以管理映像的下載、建置、標記、推送與清理。 — 完成條件：記錄來源、架構與 digest；清理前列出引用關係，保留仍需回復的版本。
- [ ] **CTR-006** 管理員可以設定多個公開或私有映像登錄站。 — 完成條件：憑證依登錄站與專案範圍保存；測試讀／推權限，不把一組登入交給所有工作。
- [ ] **CTR-007** 管理員可以建立與管理容器網路。 — 完成條件：顯示驅動、子網、閘道、IPv6、使用服務與衝突；不刪除仍被使用的網路。
- [ ] **CTR-008** 管理員可以建立與管理 Volume。 — 完成條件：明確區分命名 Volume、bind mount 與外部儲存；刪除需確認資料影響。
- [ ] **CTR-014** 管理員可以比較與採用安全更新。 — 完成條件：看到新舊 image digest、版本與部署影響；同名可變 tag 也保留實際版本紀錄。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
