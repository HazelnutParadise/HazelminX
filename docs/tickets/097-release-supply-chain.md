# 097 — 維護者可以發布附簽署清單、SBOM、授權與支援矩陣的正式 Release

**Epic:** 發布與供應鏈（`epic:release`）  
**User Story:** 維護者可以發布可安裝的版本  
**Milestone:** M5 多節點、叢集、遷移與發布  
**Blocked by:** [006 維護者可以在 CI 建置、測試並產生各架構成品](006-ci-pipeline.md), [036 管理員可以更新 HazelminX，失敗時回到相容舊版本或進入明確復原](036-platform-update.md)  
**Status:** blocked  
**規格章節:** 37｜安裝位置、版本更新與平台自我修復  
**Issue:** [#96](https://github.com/HazelnutParadise/hazelminx/issues/96)  

## What it delivers

37.1 建置矩陣與實機測試、Release 清單內容、簽章與來源證明、前後端配對、依賴報告、相容清單、離線包。

## Acceptance criteria

- [ ] **REL-001** 維護者可以由 CI 發布各架構成品。 — 完成條件：所有宣稱支援的組合都有實際安裝、啟動、更新與卸載測試。
- [ ] **PKG-004** 維護者可以檢查成品依賴。 — 完成條件：每個發布目標有原生執行與函式庫檢查報告，不以交叉編譯成功代替。
- [ ] **PKG-005** 維護者可以發布前後端配對版本。 — 完成條件：服務提供的 UI 與 Core API 相容；舊瀏覽器分頁遇到不相容時明確要求重新載入。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
