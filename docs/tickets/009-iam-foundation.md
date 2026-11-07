# 009 — 擁有者可以登入、建立帳號與角色，未授權的 API 呼叫一律被拒絕

**Epic:** 身分與存取（`epic:iam`）  
**User Story:** 擁有者可以控制誰能做什麼  
**Milestone:** M0 基礎與設計  
**Blocked by:** [003 管理員可以啟動 hazelminx-core 並通過健康檢查](003-core-skeleton.md)  
**Status:** in_progress  
**規格章節:** 05｜誰能用、誰能做什麼、36｜管理 API 與事件契約  
**Issue:** [#9](https://github.com/HazelnutParadise/hazelminx/issues/9)  

## What it delivers

登入工作階段（HttpOnly Cookie、CSRF 防護）、5.1 的角色預設、資源範圍、權限預設拒絕的中介層、稽核紀錄（操作者、前後摘要、工作編號、遮罩）、登入嘗試限制。

## Acceptance criteria

- [x] **IAM-001** 擁有者可以建立帳號、角色與資源範圍，權限預設拒絕。 — 完成條件：對未授權節點手動呼叫 API 也遭拒絕，不只隱藏按鈕。 — 證據：`core/internal/iam/authz.go` 的 `Authorize` 無任何指派即拒絕（`TestDefaultDeny`、`TestScopeIsolation`）；`POST /users`、`POST /users/{id}/roles` 由唯讀帳號直接呼叫回 `403` 且資料未建立（`TestT013ReadOnlyAccountCannotChange`）。
- [x] **IAM-006** 稽核者可以查誰在何時改了什麼。 — 完成條件：同一操作能連回變更前後摘要、工作編號、節點與結果，機密保持遮罩。 — 證據：`audit_events` 有 actor、before/after、jobId、nodeId、result、requestId、sourceIp（`0002_iam.sql`）；`user.create` 事件 `after.password` 為 `***`（`TestUsersRolesAndAudit`、`TestAuditLedgerLinksChangeAndMasksSecrets`）；其他套件以 `Service.Record` 寫入工作事件。
- [x] **IAM-012** 系統可以限制登入與復原碼嘗試。 — 完成條件：不洩漏帳號存在與否；按帳號與可信來源限制，不能讓攻擊者簡單永久鎖死所有管理員。 — 證據：`core/internal/iam/limiter.go`；帳號冷卻上限 15 分鐘、可信來源不受帳號冷卻影響、來源噴灑另行封鎖（`TestAccountCoolDownIsBoundedAndNotPermanent`、`TestTrustedSourceBypassesAccountCoolDown`、`TestLoginThrottleBoundedAndTrustedSource`）。復原碼（票 014）共用 `Service.Limiter()`。
- [x] 專案 A 使用者猜專案 B 的資源 ID 時，查詢、下載、搜尋與事件串流都不洩漏 — 證據：`RequireScoped` 對「存在但不可見」與「不存在」回同一份 `404` 內容；稽核搜尋以範圍過濾回空頁（`TestT014GuessedResourceIDsDoNotLeak`）。事件串流端點本身屬票 008／010，這裡驗證的是它們將套用的同一中介層。
- [x] 不洩漏帳號存在與否；限制不能讓攻擊者永久鎖死所有管理員 — 證據：未知帳號與密碼錯誤回同一份 `401` 內容且做同樣的雜湊運算（`TestLoginErrorsDoNotRevealAccounts`、`TestLoginDoesNotRevealAccounts`）；未知帳號套同一節流（`TestLoginThrottleBoundedAndTrustedSource`）。

## Acceptance tests（第 42 章）

- **T-013** 唯讀帳號直接呼叫變更 API。 → 後端拒絕，不只隱藏 UI 按鈕。
- **T-014** 專案 A 使用者猜專案 B 的資源 ID。 → 查詢、下載、搜尋、事件串流都不洩漏內容。

## Delivery record

- **需求 ID**：IAM-001、IAM-006、IAM-012；T-013、T-014。
- **變更**：新增 `core/internal/iam`（帳號、角色與範圍、argon2id 密碼、工作階段、預設拒絕授權、稽核、登入節流）、`core/internal/api/iam_routes.go`（工作階段與 CSRF 中介層、`RequireGlobal`／`RequireScoped`、setup／auth／users／roles／audit-events 端點）、migration `0002_iam.sql`、`main.go` 接線與首次設定碼輸出。
- **影響範圍**：`/management/v1` 下除 `setup` 與 `auth/login` 外全部需要工作階段；後續票透過 `Server.Management()` 註冊路由即繼承。`api.New` 簽名不變。
- **測試環境**：macOS arm64、Go 1.26.5、純 Go SQLite；`go vet ./... && go test ./...` 全綠；另以 `hazelminx-core -listen 127.0.0.1:9873` 實際跑過 setup → login → CSRF 拒絕 → 建帳號 → 稽核查詢。
- **正常案例**：首次設定碼建立擁有者；登入設 HttpOnly Cookie；擁有者建帳號、指派專案 A 的 `project_admin`；專案 A 使用者操作 A 成功；稽核者查到建帳號事件含前後摘要與請求 ID。
- **失敗案例**：錯誤設定碼 `403`、第二次 setup `409`；匿名 `401`；缺 CSRF 標頭或跨站 Origin `403`；唯讀帳號寫入 `403`；猜 B 的 ID `404`（與不存在相同）；帳號節流 `429` 附 `Retry-After`，15 分鐘後解除；可信來源不受影響；來源噴灑 20 次後封鎖。
- **資料與安全影響**：新增 `users`、`role_assignments`、`sessions`、`audit_events` 四張表；密碼只存 argon2id、工作階段只存 SHA-256；稽核寫入前遮罩機密鍵；設定碼只印 stdout 不進日誌。
- **實際證據**：`core/internal/iam/*_test.go`、`core/internal/api/iam_routes_test.go`、`core/cmd/hazelminx-core/main_test.go`；PR 描述附手動 curl 結果。
- **未驗證部分**：真實瀏覽器端 Cookie 行為（前端票 005 之後以 Playwright 驗）；經 Zentinel 轉發時的來源 IP（`TrustedProxies` 預設空，票 065 接線）；多 Core 並行不在範圍（35.1）。
- **不在本票**：TOTP／Passkey／復原碼（票 014）；API Token 只留 `TokenAuthenticator` 介面（票 015）；停權同時撤銷工作階段與 Token（票 015）。
