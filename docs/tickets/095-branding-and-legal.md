# 095 — 使用者看到的品牌是 HazelminX，法律與授權資訊仍可讀且來源正確

**Epic:** 基礎建設（`epic:foundation`）  
**User Story:** 使用者可以在一致的介面完成日常管理  
**Milestone:** M0 基礎與設計  
**Blocked by:** [005 使用者可以開啟 React 介面並切換語言與淺深色](005-web-skeleton.md)  
**Status:** ready  
**規格章節:** 44｜遷移、授權與長期維護  
**Issue:** [#94](https://github.com/HazelnutParadise/hazelminx/issues/94)  

## What it delivers

Logo、favicon、登入頁、空畫面、錯誤頁、CLI 與郵件樣板改為 HazelminX；法律資訊頁列出 1Panel、Komodo 等來源與授權；NOTICE 與第三方授權清單由 CI 產生。

## Acceptance criteria

- [ ] **UI-010** 使用者不會被品牌改造導向原廠購買、登入或更新服務。 — 完成條件：Logo、favicon、登入頁、郵件、CLI、空畫面及錯誤頁改為 HazelminX；法律資訊除外。
- [ ] 搜尋不到導向原廠購買、登入或更新服務的入口
- [ ] GPL-3.0 only／or-later 的最終宣告仍標待核定，不自行改寫

## Acceptance tests（第 42 章）

- **T-122** 搜尋使用者可見的上游品牌與購買入口。 → 換成 HazelminX；必要授權告知仍可讀且來源正確。
- **T-123** 執行檔與安裝包來源、依賴及授權稽核。 → 固定版本、來源證據及必要聲明齊全，不混入未授權商業碼。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
