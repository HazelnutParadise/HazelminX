# 014 — 使用者可以設定 TOTP、Passkey、復原碼並檢視與撤銷自己的工作階段

**Epic:** 身分與存取（`epic:iam`）  
**User Story:** 擁有者可以控制誰能做什麼  
**Milestone:** M1 單機主機管理  
**Blocked by:** 009 擁有者可以登入、建立帳號與角色，未授權的 API 呼叫一律被拒絕  
**規格章節:** 05｜誰能用、誰能做什麼  
**Issue:** [#14](https://github.com/HazelnutParadise/hazelminx/issues/14)  

## What it delivers

第二驗證與單次復原碼、Passkey 註冊與網域變更檢查、工作階段清單與撤銷、登入逾時設定；長工作不因前端登入過期被當成取消。

## Acceptance criteria

- [x] **IAM-002** 使用者可以設定第二種登入驗證與復原碼；高權限帳號啟用時須提供安全預設。 — 完成條件：遺失驗證裝置可用單次復原碼救援；使用過的碼不能再用。
  證據：遺失裝置改用復原碼登入、同一組碼再用被拒：`TestSecondFactorLoginReplayExpiryAndRecoveryCodes`（`core/internal/api/mfa_routes_test.go`）；同一組碼同時送 8 次只成功 1 次：`TestRecoveryCodeConcurrentUseSucceedsOnce`（`core/internal/iam/passkey_test.go`）；重產後舊碼失效：`TestReauthenticationGuardsCredentialChanges`。安全預設：擁有者、平台管理員、主機維運者沒有第二驗證時只能走綁定路由，不能移除最後一個第二驗證：`TestEnrollmentGateHoldsPrivilegedAccounts`；綁定前開的純密碼工作階段在綁定後被登出：`TestFirstFactorEndsPreEnrolmentSessions`。實機：真實執行檔 setup 後寫入回 `403 SECOND_FACTOR_ENROLLMENT_REQUIRED`，綁 TOTP 後才回 `201`。
- [x] **IAM-003** 使用者可以檢視與撤銷自己的登入工作階段。 — 完成條件：撤銷後舊瀏覽器不能繼續送出寫入操作。
  證據：`TestSessionListAndImmediateRevocation`（清單只列自己、標出目前這一個、不含 Token 與 CSRF 值；撤銷後舊瀏覽器的 `POST /users` 與 `GET /auth/session` 都回 `401`；別人的工作階段 ID 回 `404`；「登出其他裝置」保留目前這一個）。實機：瀏覽器 B 執行 `revoke-others` 後，瀏覽器 A 的寫入回 `401`。
- [x] **IAM-011** 使用者可以管理 TOTP、Passkey 與單次復原碼。 — 完成條件：保留基線可用登入方式；新增或移除憑證要再次驗證；網域變更先檢查 Passkey 影響。[S37](#source-S37)
  證據：基線登入方式保留，密碼登入、TOTP（RFC 6238 向量：`TestTOTPHOTPRFC4226Vectors`、`TestTOTPRFC6238SHA1Vectors`）與 Passkey 都可用。時間誤差與防重放：前後各一步可用、兩步外與用過的碼被拒，見 `TestTOTPVerify*`、`TestSecondFactorLoginReplayExpiryAndRecoveryCodes`。Passkey 註冊、第二步登入、他站 origin 與重放被拒：`TestPasskeySecondFactorAndDomainImpact`。再次驗證：超過 10 分鐘要再驗證，只給密碼不夠：`TestReauthenticationGuardsCredentialChanges`；用 Passkey 再次驗證後才能移除：`TestPasskeySecondFactorAndDomainImpact`。網域變更檢查：`GET /management/v1/auth/passkeys/domain-impact` 列出會失效的 Passkey 與失效後沒有第二驗證的帳號，判斷規則和登入一致（只認同一個網域）。同時送大量猜測不會多拿嘗試次數：`TestConcurrentGuessesRespectAttemptLimit`、`TestTwoValidProofsOpenOneSession`、`TestZeroCounterPasskeyAssertionSignsInOnce`。
- [x] **SET-004** 使用者可以設定登入逾時與目前工作階段。 — 完成條件：長工作不因前端登入過期被當成取消，重新登入可查結果。
  證據：`TestLongJobSurvivesSessionEnd`：送出工作後登出，再過 13 小時，工作仍在執行，完成後重新登入可查到 `succeeded`。逾時設定：`TestSessionIdleTimeoutSetting`（15 分鐘到 24 小時，設 30 分鐘後閒置 31 分鐘就失效，設 0 恢復預設 12 小時）；`TestShorterIdleTimeoutCapsOtherSessions`；持續操作時 Cookie 跟著延長：`TestSlidingSessionRenewsCookie`。
- [x] 使用過的復原碼不能再用 — 同 IAM-002 的證據。
- [x] 撤銷後舊瀏覽器不能再送出寫入操作 — 同 IAM-003 的證據。
- [ ] （增補 03 51.6）設計時決定並寫進本票：Core 時間不可信、管理入口網域變更時，TOTP 與 Passkey 的影響與替代登入方式。

## Acceptance tests（第 42 章）

- **T-130** 為管理員設定 Passkey，再修改管理網域。 → 先說明與檢查登入憑證影響，保留可用救援方式。
  本票做完影響檢查：`TestPasskeySecondFactorAndDomainImpact` 驗證只靠 Passkey 的擁有者換網域時回報 `safe=false` 與建議動作，補上 TOTP 後回報 `safe=true`。修改管理網域的操作屬於 SET-003（票 018），該票要在套用前呼叫這個檢查，T-130 的完整流程在那張票驗收。

## 設計缺口

這張票的 API 已經回得出下列狀態，但 `docs/design/` 還沒有對應的畫面，由設計端補畫：

- 綁定 Passkey 的對話框：裝置命名、瀏覽器不支援、使用者取消，以及用 IP 開面板時不能用（`409 NOT_AVAILABLE`）。
- TOTP 綁定：QR 碼和手動輸入金鑰並列、顯示伺服器時間、碼錯或綁定過期的狀態。Setup 畫板目前只有「掃描」與「輸入 6 位數」。
- 登入第二步的錯誤狀態：碼錯（`SECOND_FACTOR_INVALID`）、挑戰過期要重新輸入密碼（`LOGIN_CHALLENGE_INVALID`）、被冷卻時的倒數（`TOO_MANY_ATTEMPTS` 的 `retryAfterSeconds`），以及 `methods` 沒有 `passkey` 時隱藏 Passkey 按鈕。
- 再次驗證的對話框：密碼，加上第二驗證（`REAUTHENTICATION_REQUIRED` 時出現）。
- 帳號安全設定頁：列出已綁的 TOTP 與 Passkey、移除、重新產生復原碼、剩餘復原碼數量。
- 移除憑證的確認，以及「最後一個不能刪」（`LAST_SECOND_FACTOR`）的提示。
- 工作階段清單：裝置、IP、最後活動、目前這一個、撤銷、登出其他裝置。
- 閒置逾時設定（15 分鐘到 24 小時、預設 12 小時）。
- 平台管理員或主機維運者第一次登入就被要求綁定第二驗證的畫面（`SECOND_FACTOR_ENROLLMENT_REQUIRED`），以及 setup 中斷後再登入時的「待綁定」畫面。
- 網域變更的影響檢查結果：會失效的 Passkey、哪些帳號會沒有第二驗證、建議動作。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
