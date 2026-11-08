# 002 — 設計師與開發者可以依 DESIGN.md 與互動原型審查主要畫面

**Epic:** 設計系統（`epic:design`）  
**User Story:** 使用者可以在一致的介面完成日常管理  
**Milestone:** M0 基礎與設計  
**Blocked by:** 無  
**Status:** in_progress  
**規格章節:** 07｜介面與操作體驗  
**Issue:** [#2](https://github.com/HazelnutParadise/hazelminx/issues/2)  

## What it delivers

DESIGN.md 定義品牌、色彩、字體、間距、元件狀態、淺深色 token；設計原型涵蓋主導覽、總覽、節點、專案／服務、容器、工作中心、錯誤與空狀態、GitHub 來源選擇器、部署檢查頁，並有 360px 手機版。

## Acceptance criteria

- [x] 每個主要畫面回答「這是什麼、在哪裡、現在怎麼了、接下來能做什麼」（證據：`docs/design/screenshots/screens/`，12 個畫板每頁標題列含觀測時間與狀態、每張卡片有下一步）
- [x] 狀態不只靠顏色；危險操作顯示實際節點／磁碟／環境名稱（證據：`DESIGN.md` Shapes 節；`DeployConfirm`、`States` 畫板確認框寫出 hazel-tw-01、production、sda1 並要求輸入）
- [x] 原型經設計審查（design-review）記錄於 DESIGN-REVIEW.md（2026-11-07 第一輪，總評 7.8，未解決項目已指派票 005、017、096）

## Acceptance tests（第 42 章）

- **T-117** 在 360px 手機與桌面只用鍵盤完成日常操作。 → 主要功能可達、焦點清楚、危險按鈕不誤觸。

## Related requirements（由其他票擁有）

- UI-005（擁有票 096）：使用者可以在手機、桌面與鍵盤操作完成日常管理。
- UI-006（擁有票 016）：使用者修改設定前可看差異與受影響服務。
- UI-007（擁有票 017）：使用者可以知道載入、空資料、錯誤、部分結果與離線的差別。
- UI-011（擁有票 017）：使用者可以讀懂主要錯誤，不需先懂錯誤代碼。
- UI-012（擁有票 017）：使用者操作大型表格與日誌時不會讓整頁失去回應。

## Delivery record

- 需求：第 07 章介面與操作體驗（UI-005、UI-006、UI-007、UI-011、UI-012 由其他票擁有，本票提供設計基準）。
- 變更：`DESIGN.md`（方向 B 航站，含圖表規則與驗證過的序列色）、`DESIGN-REVIEW.md`、`docs/design/research-dashboards.md`、`docs/design/screens/` 12 個畫板、`docs/design/tools/` 產生器、`docs/design/screenshots/`。
- 測試環境：Playwright 1.65（Chromium headless）1440×900 與 360×900，深淺兩主題，全頁截圖；dataviz 驗證器檢查序列色；WCAG 對比公式檢查文字色。
- 正常案例：12 畫板 × 2 主題渲染零 console 錯誤、無橫向溢出（scrollWidth = viewport）。
- 失敗案例：第一輪發現 9 個缺陷（長條壓軸、標記文字裁切、寬圖字放大、註腳樣式、欄位重疊、X 標籤重疊、手機導覽直排、差異溢出、小字對比不足），已修正並重新渲染。
- 資料與安全影響：無（純文件與靜態原型）。
- 未驗證：T-117 的實際鍵盤走查與螢幕閱讀器要等票 005 有可執行前端；畫布的主題切換互動只在本機靜態渲染驗證。
