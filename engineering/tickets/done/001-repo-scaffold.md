# 001 — 維護者可以在單一 repo 取得規格、架構決策、票務與工作規則

**Epic:** 基礎建設（`epic:foundation`）  
**User Story:** 維護者可以接手專案  
**Milestone:** M0 基礎與設計  
**Blocked by:** 無  
**規格章節:** 46｜交給實作 Agent 的判斷邊界  
**Issue:** [#1](https://github.com/HazelnutParadise/hazelminx/issues/1)  

## What it delivers

repo 根目錄有 AGENTS.md、CLAUDE.md、ENG.md、delivery-status.md、docs/spec、docs/tickets、.gitattributes、.editorconfig、.gitignore 與 README；GitHub 有對應的 issue、milestone 與標籤。

## Acceptance criteria

- [ ] 新 agent 只讀 AGENTS.md、ENG.md、delivery-status.md 就能挑下一張票
- [ ] docs/spec 內的規格檔 SHA-256 與交付包 SHA256SUMS.txt 相符

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
