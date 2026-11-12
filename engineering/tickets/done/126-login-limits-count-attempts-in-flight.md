# 126 — 同一來源同時猜很多帳號時，被驗證的次數仍然受來源上限限制

**Epic:** 身分與存取（`epic:iam`）
**User Story:** 攻擊者不能靠同時送出請求繞過來源與帳號的嘗試上限
**Milestone:** M1 單機主機管理
**Blocked by:** —
**規格章節:** 無（票 117 審查時發現）
**Issue:** [#149](https://github.com/HazelnutParadise/hazelminx/issues/149)

## What it delivers

登入節流（`iam.Limiter`）先檢查、等雜湊密碼或查資料庫做完才記結果，檢查時看不到還在處理中的嘗試。票 117 已經讓同一帳號的密碼登入一次處理一個，但同一來源位址的失敗上限（預設 20 次）沒有跟著受保護：2026-11-12 從同一位址同時對 60 個不同帳號名稱各猜一次，被驗證了 41 到 43 次。首次設定碼沒有帳號鎖，同時送出的錯誤設定碼也能超過冷卻次數。這張票讓檢查時把處理中的嘗試算進去，不同帳號的登入仍然互不等待。

## Acceptance criteria

- [x] 同一來源同時對超過來源上限個不同帳號送錯誤密碼，被驗證的次數不超過來源上限，其餘回冷卻中。
  證據：`TestConcurrentSprayFromOneSourceRespectsSourceLimit`（`core/internal/iam/limiter_race_test.go`）從同一位址同時對 60 個帳號名稱各猜一次，剛好 20 次被驗證，40 次回冷卻中。
- [x] 同時送出超過冷卻上限的錯誤設定碼，被檢查的次數不超過冷卻上限。
  證據：`TestConcurrentWrongSetupCodesRespectCooldown` 同時送 40 個錯誤設定碼，剛好 5 個被檢查，35 個回冷卻中。
- [x] 一次一個送出時，節流結果和修正前相同（既有的節流測試照常通過）。
  證據：`core/internal/iam/limiter_test.go` 的斷言一行沒改，只把 `l.Check` 換成先 `Begin` 再立刻 `Done` 的 `check`，全部通過。獨立審查另外用 2000 組隨機序列（失敗、成功、時間跳躍、大小寫不同的帳號名稱、可信來源混在一起），逐步比對舊的 `Check` 和新的 `check`，每一步的結果與等待時間都相同。`TestExpiredCoolDownAdmitsOneAttemptAtATime` 確認冷卻到期後一次只放一個，和一個接一個猜的結果一樣。
- [x] 不同帳號的登入仍然互不等待，處理中途出錯的嘗試會把名額還回去。
  證據：票 117 的 `TestLoginAttemptsOnDifferentAccountsDoNotWait` 照常通過，`Begin` 只短暫持有節流器自己的鎖。`TestAttemptsInFlightHoldTheirPlace` 確認沒有結果就結束（`Done`）的嘗試會把名額還回去、不記失敗，重複結束不會多還。`TestConcurrentSprayLeavesNoPendingEntries` 確認一波請求過後，處理中的紀錄是空的。五個呼叫處都在 `Begin` 之後立刻 `defer attempt.Done()`。
- [x] 有一條在修正前會失敗的並行測試。
  證據：修正前跑 `go test -race -count=3`，同一來源的那條被驗證 38 到 43 次（應為 20），設定碼那條 40 次全被檢查（應為 5），每一輪都失敗。獨立審查用 `go test -overlay` 換回修正前的檔案重跑，結果相同。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。

## Notes

- 介面（2026-11-12 使用者核定）：`Limiter.Begin(account, source, trusted)` 取代 `Check`，回傳 `*Attempt`，`nil` 代表拒絕。`Attempt.Failure`、`Success`、`Done` 在節流器的鎖裡記結果並還名額，結束過一次之後再呼叫什麼都不做。`Limiter.Failure`、`Success` 保留給測試用來設定狀態。
- 名額不夠就立刻拒絕，不排隊。這時如果沒有任何封鎖，`retryAfter` 是 0，畫面會顯示 1 秒後再試。封鎖一律先檢查，被封鎖的請求一定拿到真正的冷卻時間（審查發現順序相反時會回 1 秒，已修，見 `TestBlockedAccountReportsRealWaitWhileSourceIsBusy`）。
- 處理中的名額記在另外兩張計數表，數到 0 就刪掉，所以項目數不超過正在處理的請求數，舊的清理邏輯也碰不到它們。
- 沒驗證：沒有經過 HTTP 層做真實壓力測試，也沒看前端遇到 `Retry-After: 1` 時的畫面。
