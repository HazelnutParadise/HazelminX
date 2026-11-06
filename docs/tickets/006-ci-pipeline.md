# 006 — 維護者可以在 CI 建置、測試並產生各架構成品

**Epic:** 發布與供應鏈（`epic:release`）  
**User Story:** 維護者可以發布可安裝的版本  
**Milestone:** M0 基礎與設計  
**Blocked by:** [003 管理員可以啟動 hazelminx-core 並通過健康檢查](003-core-skeleton.md), [004 管理員可以在主機上以 systemd 執行 hazelminx-agent 並回報身分](004-agent-skeleton.md), [005 使用者可以開啟 React 介面並切換語言與淺深色](005-web-skeleton.md)  
**Status:** blocked  
**規格章節:** 37｜安裝位置、版本更新與平台自我修復  
**Issue:** [#6](https://github.com/HazelnutParadise/hazelminx/issues/6)  

## What it delivers

GitHub Actions：Go／Rust／前端的 lint、test、build；Linux x86_64 與 ARM64 成品；前端建置結果嵌入 Core；產物依賴檢查報告（ldd／otool 等效）。

## Acceptance criteria

- [ ] 交叉編譯成功不算驗收；至少一個目標在實際 OS 執行冒煙測試
- [ ] 鎖檔版本固定，CI 不使用浮動工具版本

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
