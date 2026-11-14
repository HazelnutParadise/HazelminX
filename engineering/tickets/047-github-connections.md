# 047 — 使用者可以用 GitHub 網頁授權或 PAT 建立可重用連線，並保存多帳號多授權

**Epic:** GitHub 來源（`epic:github`）  
**User Story:** 使用者可以從 GitHub 部署  
**Milestone:** M2 容器、Compose 與 GitHub  
**Blocked by:** 009 擁有者可以登入、建立帳號與角色，未授權的 API 呼叫一律被拒絕, 011 管理員可以安全保存機密，且主金鑰可輪替並驗證舊備份仍可解密, 010 整合者可以用 /management/v1 的分頁、版本條件與工作回應操作資源  
**規格章節:** 15｜完整 Compose、GitHub 與環境變數  
**Issue:** [#47](https://github.com/HazelnutParadise/hazelminx/issues/47)  

## What it delivers

這張票是索引，自己不交付內容，兩張子票都在 `done/` 時一起關閉。拆分原因：Core 的 GitHub App、連線、授權回呼與 Token 更新，和前端的連線管理畫面分屬 `core/` 與 `web/`，一個 PR 做不完也驗不完；畫板目前也還沒有連線管理的畫面。

| 項目 | 子票 |
|---|---|
| Core：GitHub App 引導建立（manifest）與手動接入、授權請求（一次性 state、工作階段、回呼白名單、PKCE）、回呼向 GitHub 核對帳號與安裝、PAT 連線驗證、多帳號多授權、短效 Token 更新、回呼與 Webhook 可達性分開檢查，經 `/management/v1/github` 提供（GIT-001～004、GIT-013～016、GIT-031，GH-T01～T04、GH-T12～T15） | 047.1 |
| 畫面：連線清單與切換、新增連線（網頁授權或 PAT）、App 建立精靈、回呼頁與失敗原因、需要重新授權的狀態、可達性檢查結果 | 047.2 |
