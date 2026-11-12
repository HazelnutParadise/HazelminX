# 117 — 同時送出大量密碼猜測時，同一帳號的登入嘗試仍然受冷卻次數限制

**Epic:** 身分與存取（`epic:iam`）
**User Story:** 攻擊者不能靠同時送出請求繞過帳號冷卻
**Milestone:** M1 單機主機管理
**Blocked by:** —
**規格章節:** 無（來自 2026-11-11 之前記在 AGENTS.md 的後續事項）
**Issue:** [#135](https://github.com/HazelnutParadise/hazelminx/issues/135)

## What it delivers

密碼登入（`iam.Service.Login`）目前先 `limiter.Check`、再雜湊密碼、最後才記失敗，同時送出的大量猜測可以超過帳號冷卻的次數。比照票 014 第二步的做法，同一帳號的嘗試一次處理一個。

## Acceptance criteria

- [x] 對同一帳號同時送出超過冷卻上限的錯誤密碼，實際被驗證的次數不超過上限，其餘回冷卻中。
  證據：`TestConcurrentLoginGuessesRespectAccountCooldown`（`core/internal/iam/login_race_test.go`）同時送 40 次錯誤密碼，每次來自不同位址，只有帳號冷卻能擋。存在的帳號 `owner` 和不存在的帳號 `ghost` 都只驗證 5 次，其餘 35 次回冷卻中，兩者結果一樣，不會透露帳號是否存在。`TestConcurrentLoginBurstsAcrossAccountsLeaveNoLocks` 讓 4 個帳號同時各被猜 15 次，每個帳號都剛好驗證 5 次，結束後鎖表是空的。
- [x] 不同帳號的登入互不等待。
  證據：`TestLoginAttemptsOnDifferentAccountsDoNotWait` 先佔住 `owner` 的鎖，這時 `ghost`、`someone-else` 的登入照常回帳密錯誤，而 `OWNER`（大小寫不同）的登入要等鎖放開後才完成。
- [x] 有一條在修正前會失敗的並行測試。
  證據：修正前跑 `go test -race -count=3 -run TestConcurrentLoginGuesses ./internal/iam/`，40 次裡被驗證 17 到 23 次（應為 5 次），三輪都失敗。獨立審查另用 `go test -overlay` 拿掉加鎖的兩行重跑，三條新測試都失敗。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。

## Notes

- 做法：`Login` 在檢查冷卻前，先拿這個帳號名稱的鎖（`keyedLocks`，`core/internal/iam/keylock.go`），記完成功或失敗才放開。鎖的名稱和冷卻計數一樣先去掉前後空白、轉小寫。第二步登入的 `lockUser` 也改用同一個鎖表，行為不變。鎖只在有人持有或等待時存在，所以鎖表的項目數不會超過正在處理的登入數，亂打的帳號名稱不會留下東西。
- 未修，另需處理：同一來源位址的失敗上限（`SourceMaxFailures`，預設 20 次）有同樣的先檢查、後記錄問題。2026-11-12 用暫時測試從同一位址同時對 60 個不同帳號名稱各猜一次，被驗證了 41 到 43 次。這張票只要求每個帳號受限，而且不同帳號要互不等待，所以沒有把同一來源的嘗試排成一列。修法要讓限制器在檢查時先預留名額，會同時影響 setup 與第二步驗證的計數。
- 等鎖時不會因為使用者斷線而提早放棄，這和修正前一樣。冷卻期間排隊的請求進鎖後只做一次記憶體內檢查就離開，不雜湊密碼。
