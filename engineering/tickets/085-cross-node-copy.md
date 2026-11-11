# 085 — 使用者可以跨節點複製資料並確認 UID／GID 映射

**Epic:** 檔案管理（`epic:files`）  
**User Story:** 使用者可以管理主機檔案  
**Milestone:** M5 多節點、叢集、遷移與發布  
**Blocked by:** 028 使用者可以瀏覽、整理、授權與追蹤宿主機檔案, 084 管理員可以在單一畫面查看與批次操作跨節點資源  
**規格章節:** 12｜宿主機檔案與跨節點傳輸  
**Issue:** [#84](https://github.com/HazelnutParadise/hazelminx/issues/84)  

## What it delivers

校驗內容、權限與屬性；UID／GID 不同時先確認映射。

## Acceptance criteria

- [ ] **FIL-008** 使用者可以跨節點複製資料。 — 完成條件：校驗內容、權限及必要屬性；UID／GID 不同時先確認映射，不能只看相同帳號名稱。

## Acceptance tests（第 42 章）

- **T-043** 跨節點搬移遇到 UID／GID 不一致。 → 顯示對應與確認，不默默給錯擁有者。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
