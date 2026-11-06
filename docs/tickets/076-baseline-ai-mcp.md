# 076 — 管理員可以保留基線 AI／MCP／Agent 能力，MCP 預設唯讀且危險操作需批准

**Epic:** AI 與模型（`epic:ai`）  
**User Story:** 管理員可以管理模型與推論服務  
**Milestone:** M4 資料庫、AI、備份與監控  
**Blocked by:** [074 管理員可以安裝或接管 Ollama 並管理模型的下載、刪除、複製與配額](074-ollama-models.md), [015 擁有者可以管理使用者、角色、資源範圍與有期限的 API Token](015-users-roles-tokens.md)  
**Status:** blocked  
**規格章節:** 25｜AI 管理、Ollama 與模型、30｜API、CLI、MCP 與外部整合  
**Issue:** [#75](https://github.com/HazelnutParadise/hazelminx/issues/75)  

## What it delivers

基線 OSS AI 功能對照、MCP 伺服器（工具宣告區分讀取與變更）、AI 變更計畫只能提案、資料與指令分離。

## Acceptance criteria

- [ ] **AI-012** 管理員可以保留基線 OSS 的其他 AI／MCP／Agent 管理能力。 — 完成條件：逐項對照實際 OSS 功能；沒有取得的 Pro AI Gateway 不冒充現成來源。
- [ ] **INT-003** 管理員可以使用 MCP 管理授權功能。 — 完成條件：預設唯讀；工具宣告清楚區分讀取與變更；沿用人類操作的安全門檻。[S31](#source-S31)
- [ ] **INT-004** 管理員可以讓 AI 提出變更計畫，但不讓內容自動取得權限。 — 完成條件：網頁、日誌、repo 或模型回覆中的命令只作資料；真正執行需工具授權與必要批准。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
