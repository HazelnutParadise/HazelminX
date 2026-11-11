# 119 — 使用者可以在畫面上登入、完成首次設定、綁定第二驗證並管理自己的帳號安全

**Epic:** 身分與存取（`epic:iam`）
**User Story:** 使用者不用呼叫 API 就能登入與保護自己的帳號
**Milestone:** M1 單機主機管理
**Blocked by:** 005 使用者可以開啟 React 介面並切換語言與淺深色, 009 擁有者可以登入、建立帳號與角色，未授權的 API 呼叫一律被拒絕, 014 使用者可以設定 TOTP、Passkey、復原碼並檢視與撤銷自己的工作階段
**規格章節:** 無（來自 2026-11-11 之前記在 AGENTS.md 的後續事項）
**Issue:** [#137](https://github.com/HazelnutParadise/hazelminx/issues/137)

## What it delivers

登入、首次設定、第二驗證綁定與帳號安全的畫面。票 009、014 只交 API，票 005 只做了外殼：`/account/security` 目前是空殼頁，帳號選單的「登出」會回到 `/`，但沒有登入頁可以接。設計稿已有 Login、Setup、AccountSecurity、MfaDialogs 畫板。外殼顯示的目前使用者與未登入狀態（`web/src/app/shellStatus.ts`）也由這張票接上資料來源。

## Acceptance criteria

- [ ] 未登入時開啟任何頁面會到登入頁，登入成功後回到原本要去的頁面；登出後回到登入頁。
- [ ] 第一次啟動時走首次設定流程建立擁有者帳號。
- [ ] 登入第二步、綁定 TOTP 與 Passkey、復原碼、強制綁定、重新驗證的畫面依 MfaDialogs 畫板實作，錯誤狀態齊全。
- [ ] `/account/security` 顯示第二驗證、復原碼、登入中的裝置與閒置設定，可以撤銷其他裝置。
- [ ] 外殼的帳號選單顯示目前使用者，未登入時顯示未登入頂列。
- [ ] 用真實瀏覽器與實體驗證器（含同步型 Passkey）測過註冊、第二步登入與再次驗證。票 014 的 Passkey 只用軟體驗證器（`core/internal/iam/softauth_test.go`）測過。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
