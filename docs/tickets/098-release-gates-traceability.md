# 098 — 驗收者可以依 G-01 至 G-14 門檻與需求→測試→證據追溯表判定可否發布

**Epic:** 品質與驗收（`epic:quality`）  
**User Story:** 驗收者可以證明功能沒有遺漏  
**Milestone:** M5 多節點、叢集、遷移與發布  
**Blocked by:** [092 驗收者可以查每項 1Panel v2.4.1 OSS 功能在 HazelminX 的對應位置與狀態](092-baseline-inventory.md), [097 維護者可以發布附簽署清單、SBOM、授權與支援矩陣的正式 Release](097-release-supply-chain.md)  
**Status:** blocked  
**規格章節:** 41｜測試方式與正式發布門檻、42｜具體驗收案例、43｜追蹤表：每個已確認決策都要有去處  
**Issue:** [#97](https://github.com/HazelnutParadise/hazelminx/issues/97)  

## What it delivers

機器可讀的需求→票→測試→證據→版本關係、六層測試環境、每份結果的版本與環境紀錄、未驗證標示。

## Acceptance criteria

- [ ] 沒有證據的需求保持未完成；開發版標示未完成功能

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
