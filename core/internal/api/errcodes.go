package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
)

// ErrorCode is one row of the stable error code table (spec 36.2, addendum
// 03 51.1). A code always answers with the same HTTP status; programs branch
// on Code, people read Message.
type ErrorCode struct {
	Code   string `json:"code"`
	Status int    `json:"status"`
	// Retryable is the default for the code; a single error may still say
	// otherwise when it knows better (INTERNAL is retryable, a lost race is
	// not).
	Retryable bool `json:"retryable"`
	// Meaning is the plain-language description published in OpenAPI.
	Meaning string `json:"meaning"`
}

// The codes. Every error the management API sends must use one of these;
// TestErrorCodesComeFromTheTable enforces it.
const (
	CodeUnauthenticated    = "UNAUTHENTICATED"
	CodeInvalidCredentials = "INVALID_CREDENTIALS"
	CodeCSRFMismatch       = "CSRF_TOKEN_MISMATCH"
	CodeOriginMismatch     = "ORIGIN_MISMATCH"
	CodeNotFound           = "NOT_FOUND"
	CodeForbidden          = "FORBIDDEN"
	CodeValidationFailed   = "VALIDATION_FAILED"
	CodeVersionRequired    = "VERSION_REQUIRED"
	CodeQueryLimitExceeded = "QUERY_LIMIT_EXCEEDED"
	CodeDryRunUnsupported  = "DRY_RUN_UNSUPPORTED"
	CodePayloadTooLarge    = "PAYLOAD_TOO_LARGE"
	CodePlatformDegraded   = "PLATFORM_DEGRADED"
	CodeScopeDegraded      = "SCOPE_DEGRADED"
	CodeControllerConflict = "CONTROLLER_CONFLICT"
	CodeResourceReadOnly   = "RESOURCE_READ_ONLY"
	CodeConfirmRequired    = "CONFIRMATION_REQUIRED"
	CodeVersionMismatch    = "VERSION_MISMATCH"
	CodeResourceReferenced = "RESOURCE_REFERENCED"
	CodePrecheckFailed     = "PRECHECK_FAILED"
	CodeAlreadyExists      = "ALREADY_EXISTS"
	CodeRequestIDConflict  = "REQUEST_ID_CONFLICT"
	CodeJobNotCancellable  = "JOB_NOT_CANCELLABLE"
	CodeSetupDone          = "SETUP_DONE"
	CodeSetupCodeInvalid   = "SETUP_CODE_INVALID"
	CodeConfirmExpired     = "CONFIRMATION_EXPIRED"
	CodeConfirmNewConn     = "CONFIRMATION_NEW_CONNECTION_REQUIRED"
	CodeListQueryNotReady  = "LIST_QUERY_NOT_READY"
	CodeListQueryExpired   = "LIST_QUERY_EXPIRED"
	CodeListQueryLimit     = "LIST_QUERY_LIMIT_REACHED"
	CodeTooManyAttempts    = "TOO_MANY_ATTEMPTS"
	// Change plans (ticket 016.1).
	CodeChangePlanChanged  = "CHANGE_PLAN_CHANGED"
	CodeChangePlanExpired  = "CHANGE_PLAN_EXPIRED"
	CodeChangePlanState    = "CHANGE_PLAN_STATE"
	CodeChangePlanRequired = "CHANGE_PLAN_REQUIRED"
	// Second factors and sessions (ticket 014).
	CodeLoginChallengeInvalid = "LOGIN_CHALLENGE_INVALID"
	CodeSecondFactorInvalid   = "SECOND_FACTOR_INVALID"
	CodeEnrollmentRequired    = "SECOND_FACTOR_ENROLLMENT_REQUIRED"
	CodeReauthRequired        = "REAUTHENTICATION_REQUIRED"
	CodeLastSecondFactor      = "LAST_SECOND_FACTOR"
	CodeNotAvailable          = "NOT_AVAILABLE"
	CodeInternal              = "INTERNAL"
	CodeUINotEmbedded         = "UI_NOT_EMBEDDED"
	CodeUIIndexMissing        = "UI_INDEX_MISSING"
)

var errorTable = []ErrorCode{
	{CodeUnauthenticated, http.StatusUnauthorized, false, "沒有登入，或登入已失效。"},
	{CodeInvalidCredentials, http.StatusUnauthorized, false, "帳號或密碼不正確。"},
	{CodeCSRFMismatch, http.StatusForbidden, false, "以瀏覽器登入狀態送出的變更，缺少或帶錯 X-CSRF-Token。"},
	{CodeOriginMismatch, http.StatusForbidden, false, "要求來自其他網站，已拒絕。"},
	{CodeNotFound, http.StatusNotFound, false, "資源不存在，或這個帳號無權得知它是否存在。"},
	{CodeForbidden, http.StatusForbidden, false, "這個帳號看得到資源，但沒有執行這個操作的權限。"},
	{CodeValidationFailed, http.StatusBadRequest, false, "輸入不正確；fields 寫出是哪些欄位。"},
	{CodeVersionRequired, http.StatusPreconditionRequired, false, "這個寫入要帶 If-Match 版本條件：先 GET 取得 ETag，再以 If-Match 送回；不接受 *。"},
	{CodeQueryLimitExceeded, http.StatusBadRequest, false, "已到這個列表的查詢上限，不能再往後翻頁；可以改成背景工作。"},
	{CodeDryRunUnsupported, http.StatusBadRequest, false, "這個端點不提供 dryRun 影響預覽；沒有執行任何變更。"},
	{CodePayloadTooLarge, http.StatusRequestEntityTooLarge, false, "要求內容超過這個端點的大小上限；大量資料請用分段上傳。"},
	{CodePlatformDegraded, http.StatusServiceUnavailable, true, "平台目前降級，這個操作暫時不能執行；details.mode 寫出是哪一種降級。"},
	{CodeScopeDegraded, http.StatusConflict, true, "目標節點或引擎環境降級，這個操作暫時不能執行；details.mode 寫出是哪一種。"},
	{CodeControllerConflict, http.StatusConflict, false, "資源偵測到兩個控制者，管理員選定之前不能寫入。"},
	{CodeResourceReadOnly, http.StatusConflict, false, "依資源的控制者與接管狀態，平台目前不能寫入它。"},
	{CodeConfirmRequired, http.StatusConflict, true, "同一資源有一個變更還在等確認，確認或回復之前不能送出其他變更。"},
	{CodeVersionMismatch, http.StatusPreconditionFailed, false, "資源在你讀取後已被修改；details.currentVersion 是目前版本，重新讀取再決定。"},
	{CodeResourceReferenced, http.StatusConflict, false, "項目被這次刪除範圍外的項目使用，不能刪除；details.referencedBy 列出使用者。"},
	{CodePrecheckFailed, http.StatusConflict, false, "預檢發現目前環境不允許這個操作；details.checks 寫出沒通過的檢查。同步寫入時是 HTTP 409；工作的預檢沒過時是工作的 errorCode。"},
	{CodeAlreadyExists, http.StatusConflict, false, "已經有同樣的項目，例如名稱已被使用或角色已指派。"},
	{CodeRequestIDConflict, http.StatusConflict, false, "同一個 requestId 已用於內容不同的要求；換一個新的 requestId。"},
	{CodeJobNotCancellable, http.StatusConflict, false, "工作目前的狀態不能取消。"},
	{CodeSetupDone, http.StatusConflict, false, "首次設定已經完成。"},
	{CodeSetupCodeInvalid, http.StatusForbidden, false, "設定碼不正確或已過期。"},
	{CodeConfirmExpired, http.StatusConflict, false, "確認時間已過，變更會或已經自動回復。"},
	{CodeConfirmNewConn, http.StatusConflict, false, "確認必須從新的連線（必要時經新的入口）送出，才能證明管理路徑仍可用。"},
	{CodeListQueryNotReady, http.StatusConflict, true, "背景查詢還沒成功完成；依 jobId 追蹤工作。"},
	{CodeListQueryExpired, http.StatusGone, false, "背景查詢的結果已過 7 天保存期限並已刪除；請重新提交查詢。"},
	{CodeListQueryLimit, http.StatusConflict, false, "這個帳號保存的背景查詢已達上限；結果在提交 7 天後刪除。"},
	{CodeChangePlanChanged, http.StatusConflict, false, "計畫的內容或目標版本和你審查的不同；原批准不能用。details.targets 寫出哪個目標從哪一版變成哪一版，details.planned 與 details.current 是審查時與現在的差異；請重新產生計畫。"},
	{CodeChangePlanExpired, http.StatusConflict, false, "計畫已過有效期限，不能再批准或執行；請重新產生計畫。"},
	{CodeChangePlanState, http.StatusConflict, false, "計畫目前的狀態不允許這個操作，例如已拒絕、已撤回、已失效、已執行或還沒批准。"},
	{CodeChangePlanRequired, http.StatusConflict, false, "這類工作要先產生變更計畫並經批准，不能直接建立；請改用 POST /management/v1/change-plans。"},
	{CodeTooManyAttempts, http.StatusTooManyRequests, true, "嘗試太多次，請稍後再試；retryAfterSeconds 寫出要等多久。"},
	{CodeLoginChallengeInvalid, http.StatusUnauthorized, false, "登入第二步的挑戰已過期、已用過或答錯太多次，請重新輸入密碼。"},
	{CodeSecondFactorInvalid, http.StatusUnauthorized, false, "驗證碼、復原碼或 Passkey 沒有通過。"},
	{CodeEnrollmentRequired, http.StatusForbidden, false, "這個帳號的角色必須先綁定驗證器 App 或 Passkey，綁定前只能使用綁定相關的操作。"},
	{CodeReauthRequired, http.StatusForbidden, false, "新增或移除登入憑證前，要在 10 分鐘內重新驗證密碼與第二驗證（POST /management/v1/auth/reauthenticate）。"},
	{CodeLastSecondFactor, http.StatusConflict, false, "這個帳號至少要保留一個驗證器 App 或 Passkey；請先新增另一個再移除。"},
	{CodeNotAvailable, http.StatusConflict, false, "這個功能在目前的環境不能使用，例如用 IP 開啟面板時不能使用 Passkey；message 寫出原因。"},
	{CodeInternal, http.StatusInternalServerError, true, "伺服器內部錯誤；traceId 可用來查日誌。"},
	{CodeUINotEmbedded, http.StatusServiceUnavailable, false, "這個版本沒有內嵌網頁介面，請使用管理 API。"},
	{CodeUIIndexMissing, http.StatusInternalServerError, false, "內嵌介面缺少 index.html。"},
}

var errorIndex = func() map[string]ErrorCode {
	m := make(map[string]ErrorCode, len(errorTable))
	for _, e := range errorTable {
		if _, dup := m[e.Code]; dup {
			panic("api: duplicate error code " + e.Code)
		}
		m[e.Code] = e
	}
	return m
}()

// lookupCode returns the table row; unknown codes are a programming error
// and come back as INTERNAL so nothing outside the table reaches a client.
func lookupCode(code string) ErrorCode {
	if e, ok := errorIndex[code]; ok {
		return e
	}
	return errorIndex[CodeInternal]
}

// ErrorCodes returns the table sorted by code, for OpenAPI and the
// contract tests.
func ErrorCodes() []ErrorCode {
	out := append([]ErrorCode(nil), errorTable...)
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

// blockingOrder is the precedence when one request meets several refusals
// at once (addendum 03 51.6, decided in ticket 010). Earlier wins. Identity
// and visibility come first so a refusal never reveals more than a 404
// would; resource-state blockers come before the version check because even
// the right version cannot be written; reference and precheck findings
// depend on current content and come last.
var blockingOrder = []string{
	CodeUnauthenticated,
	CodeCSRFMismatch,
	CodeOriginMismatch,
	// A dryRun on a write without a preview is refused from the request
	// alone, before any lookup: it says nothing about the resource and
	// guarantees nothing runs.
	CodeDryRunUnsupported,
	CodeNotFound,
	CodeForbidden,
	CodeValidationFailed,
	CodePayloadTooLarge,
	CodeVersionRequired,
	// A replay of an accepted requestId answers with the original job, and
	// a reused requestId with different content is refused, before any
	// degradation: the job already exists.
	CodeRequestIDConflict,
	CodePlatformDegraded,
	CodeScopeDegraded,
	CodeControllerConflict,
	CodeResourceReadOnly,
	CodeConfirmRequired,
	CodeVersionMismatch,
	CodeResourceReferenced,
	CodePrecheckFailed,
}

// BlockingOrder returns the precedence list (copy).
func BlockingOrder() []string { return append([]string(nil), blockingOrder...) }

// respond sends e with the HTTP status of its code, stamped with the call's
// trace id, and aborts the handler chain.
func respond(c *gin.Context, e apiError) {
	e.TraceID = c.GetString(TraceIDHdr)
	c.AbortWithStatusJSON(lookupCode(e.Code).Status, e)
}

// maxJSONBody bounds request bodies; bulk data goes through uploads (36.2).
const maxJSONBody = 1 << 20

// bindJSON decodes a JSON body strictly: unknown fields are refused rather
// than dropped (spec 34.3), and an oversized body is PAYLOAD_TOO_LARGE. It
// answers the request itself and returns false on failure.
func bindJSON(c *gin.Context, v any, hint string) bool {
	dec := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, maxJSONBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			respond(c, newError(CodePayloadTooLarge, fmt.Sprintf("body is larger than %d bytes", maxJSONBody)))
			return false
		}
		e := newError(CodeValidationFailed, hint+": "+err.Error())
		var syn *json.UnmarshalTypeError
		if errors.As(err, &syn) && syn.Field != "" {
			e.Fields = []FieldError{{Field: syn.Field, Message: "expected " + syn.Type.String()}}
		} else if f, ok := strings.CutPrefix(err.Error(), "json: unknown field "); ok {
			e.Fields = []FieldError{{Field: strings.Trim(f, `"`), Message: "unknown field"}}
		}
		respond(c, e)
		return false
	}
	return true
}
