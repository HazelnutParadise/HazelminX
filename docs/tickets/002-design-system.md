# 002 — 設計師與開發者可以依 DESIGN.md 與互動原型審查主要畫面

**Epic:** 設計系統（`epic:design`）  
**User Story:** 使用者可以在一致的介面完成日常管理  
**Milestone:** M0 基礎與設計  
**Blocked by:** 無  
**Status:** ready  
**規格章節:** 07｜介面與操作體驗  
**Issue:** [#2](https://github.com/HazelnutParadise/hazelminx/issues/2)  

## What it delivers

DESIGN.md 定義品牌、色彩、字體、間距、元件狀態、淺深色 token；設計原型涵蓋主導覽、總覽、節點、專案／服務、容器、工作中心、錯誤與空狀態、GitHub 來源選擇器、部署檢查頁，並有 360px 手機版。

## Acceptance criteria

- [ ] 每個主要畫面回答「這是什麼、在哪裡、現在怎麼了、接下來能做什麼」
- [ ] 狀態不只靠顏色；危險操作顯示實際節點／磁碟／環境名稱
- [ ] 原型經設計審查（design-review）記錄於 DESIGN-REVIEW.md

## Acceptance tests（第 42 章）

- **T-117** 在 360px 手機與桌面只用鍵盤完成日常操作。 → 主要功能可達、焦點清楚、危險按鈕不誤觸。

## Related requirements（由其他票擁有）

- UI-005（擁有票 096）：使用者可以在手機、桌面與鍵盤操作完成日常管理。
- UI-006（擁有票 016）：使用者修改設定前可看差異與受影響服務。
- UI-007（擁有票 017）：使用者可以知道載入、空資料、錯誤、部分結果與離線的差別。
- UI-011（擁有票 017）：使用者可以讀懂主要錯誤，不需先懂錯誤代碼。
- UI-012（擁有票 017）：使用者操作大型表格與日誌時不會讓整頁失去回應。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
