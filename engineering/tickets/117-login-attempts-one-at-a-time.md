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

- [ ] 對同一帳號同時送出超過冷卻上限的錯誤密碼，實際被驗證的次數不超過上限，其餘回冷卻中。
- [ ] 不同帳號的登入互不等待。
- [ ] 有一條在修正前會失敗的並行測試。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
