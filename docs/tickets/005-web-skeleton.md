# 005 — 使用者可以開啟 React 介面並切換語言與淺深色

**Epic:** 基礎建設（`epic:foundation`）  
**User Story:** 使用者可以在一致的介面完成日常管理  
**Milestone:** M0 基礎與設計  
**Blocked by:** [001 維護者可以在單一 repo 取得規格、架構決策、票務與工作規則](001-repo-scaffold.md), [002 設計師與開發者可以依 DESIGN.md 與互動原型審查主要畫面](002-design-system.md)  
**Status:** blocked  
**規格章節:** 07｜介面與操作體驗  
**Issue:** [#5](https://github.com/HazelnutParadise/hazelminx/issues/5)  

## What it delivers

web/ 使用 React、TypeScript 與 Vite+：路由殼層、設計 token、zh-TW／en 的 i18n、淺深色、API client 產生器、vitest 與 vp check；建置產物可被 Core 嵌入。

## Acceptance criteria

- [ ] **UI-001** 使用者可以切換繁體中文、英文、淺色與深色模式。 — 完成條件：系統選單、錯誤與日期顯示一致；第三方原始訊息保留原文及白話摘要。
- [ ] 第三方原始訊息保留原文並附白話摘要的呈現元件存在
- [ ] 日期以使用者時區顯示，儲存值為 UTC RFC 3339

## Acceptance tests（第 42 章）

- **T-118** 切深淺色、中文英文、錯誤與空資料。 → 文字與狀態完整，不只靠顏色，不顯示假的零值。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
