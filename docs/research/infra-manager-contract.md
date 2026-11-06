> 來源：HazelnutParadise/infra-manager（commit 2ace566）唯讀原始碼契約整理，2026-11-07，由派工 agent（opencode / big-pickle）產出，主 agent 親自比對 middlewares/auth.go、api/routes.go 與 models/models.go。這是必須相容的契約。

# infra-manager 相容性契約

## 0. 交付與驗證

- 改了哪些檔案：無。全程只讀，`git status --porcelain` 無輸出，HEAD 為 `2ace566 fix: prevent stale management page assets`。
- 驗證方式：逐條比對原始碼，路由清單以 `api/routes.go` 為準，中介層規則直接引用 `middlewares/auth.go`。
- 未執行 `go test`、前端測試、服務啟動或 Docker。所有未經執行驗證的推導列在「未驗證項目」。
- 相關 Project Memory：主 agent 表示無，故無舊記憶可比對。

## 1. 全部路由

全域中介層先套用快取控制與 session（`api/routes.go:20-44`）。`/use`、`/api`、`/robots.txt` 不套用管理頁的 `no-store` 規則（`api/routes.go:23-33`）。

### 公開路由

| 方法 | 路徑 | Handler | 需 admin session | 經 token 驗證 | 依據 |
|---|---|---|---|---|---|
| GET/HEAD | `/static/*filepath` | Gin 靜態檔服務，目錄 `./static` | 否 | 否 | `api/routes.go:47` |
| GET/HEAD | `/robots.txt` | `./static/robots.txt` | 否 | 否 | `api/routes.go:49` |
| GET | `/login` | `controllers.ShowLogin` | 否 | 否 | `api/routes.go:55` |
| POST | `/auth/login` | `controllers.Login` | 否 | 否 | `api/routes.go:56` |
| GET | `/logout` | `controllers.Logout` | 否 | 否 | `api/routes.go:57` |
| GET | `/` | 有 `admin_id` 轉 `/dashboard`，否則轉 `/login` | 否 | 否 | `api/routes.go:60-67` |

### 管理介面頁面（`/` group + `middlewares.AdminAuth()`）

| 方法 | 路徑 | Handler | 需 admin session | 經 token 驗證 | 依據 |
|---|---|---|---|---|---|
| GET | `/dashboard` | inline `dashboard.html` | 是 | 否 | `api/routes.go:70-77` |
| GET | `/failures` | inline `failures.html` | 是 | 否 | `api/routes.go:78-80` |
| GET | `/users` | inline `users.html` | 是 | 否 | `api/routes.go:81-85` |
| GET | `/services` | inline `services.html` | 是 | 否 | `api/routes.go:86-90` |
| GET | `/tokens` | inline `tokens.html` | 是 | 否 | `api/routes.go:91-95` |
| GET | `/change-password` | `controllers.ShowChangePasswordPage` | 是 | 否 | `api/routes.go:97` |

### 管理 API（`/admin` group + `middlewares.AdminAuth()`）

以下全部需要 admin session，不經 token 驗證。未登入時 `AdminAuth` 回 302 轉 `/login`，不是 JSON 401（`middlewares/auth.go:26-31`）。

| 方法 | 路徑 | Handler | 依據 |
|---|---|---|---|
| POST | `/admin/change-password` | `controllers.ChangePassword` | `api/routes.go:105` |
| GET | `/admin/users` | `controllers.GetAllUsers` | `api/routes.go:108` |
| GET | `/admin/users/:id` | `controllers.GetUser` | `api/routes.go:109` |
| POST | `/admin/users` | `controllers.CreateUser` | `api/routes.go:110` |
| PUT | `/admin/users/:id` | `controllers.UpdateUser` | `api/routes.go:111` |
| DELETE | `/admin/users/:id` | `controllers.DeleteUser` | `api/routes.go:112` |
| PATCH | `/admin/users/:id/status` | `controllers.ToggleUserStatus` | `api/routes.go:113` |
| GET | `/admin/services` | `controllers.GetAllServices` | `api/routes.go:116` |
| GET | `/admin/services/:id` | `controllers.GetService` | `api/routes.go:117` |
| POST | `/admin/services` | `controllers.CreateService` | `api/routes.go:118` |
| PUT | `/admin/services/:id` | `controllers.UpdateService` | `api/routes.go:119` |
| PUT | `/admin/services/:id/details` | `controllers.UpdateServiceDetails` | `api/routes.go:120` |
| DELETE | `/admin/services/:id` | `controllers.DeleteService` | `api/routes.go:121` |
| PATCH | `/admin/services/:id/status` | `controllers.ToggleServiceStatus` | `api/routes.go:122` |
| GET | `/admin/tokens` | `controllers.GetAllTokens` | `api/routes.go:125` |
| GET | `/admin/tokens/:id` | `controllers.GetToken` | `api/routes.go:126` |
| GET | `/admin/user-tokens/:user_id` | `controllers.GetUserTokens` | `api/routes.go:127` |
| GET | `/admin/service-tokens/:service_id` | `controllers.GetServiceTokens` | `api/routes.go:128` |
| POST | `/admin/tokens` | `controllers.CreateToken` | `api/routes.go:129` |
| PUT | `/admin/tokens/:id` | `controllers.UpdateToken` | `api/routes.go:130` |
| PUT | `/admin/tokens/:id/details` | `controllers.UpdateTokenDetails` | `api/routes.go:131` |
| DELETE | `/admin/tokens/:id` | `controllers.DeleteToken` | `api/routes.go:132` |
| PATCH | `/admin/tokens/:id/status` | `controllers.ToggleTokenStatus` | `api/routes.go:133` |
| GET | `/admin/stats/failures` | `controllers.GetRequestFailures` | `api/routes.go:138` |
| GET | `/admin/stats/recent` | `controllers.GetRecentStats` | `api/routes.go:141` |
| GET | `/admin/stats/dashboard-recent` | `controllers.GetDashboardRecentStats` | `api/routes.go:142` |
| GET | `/admin/stats/dashboard-responses` | `controllers.GetDashboardResponseChart` | `api/routes.go:143` |
| GET | `/admin/stats/dashboard-services` | `controllers.GetDashboardServiceUsage` | `api/routes.go:144` |
| GET | `/admin/stats/dashboard-service-details` | `controllers.GetDashboardServiceDetail` | `api/routes.go:145` |
| GET | `/admin/stats/dashboard-snapshot` | `controllers.GetDashboardSnapshotComparison` | `api/routes.go:146` |
| GET | `/admin/stats/services` | `controllers.GetServicesUsageStats` | `api/routes.go:147` |
| GET | `/admin/stats/services/:service_id/time` | `controllers.GetServiceTimeStats` | `api/routes.go:150` |
| GET | `/admin/stats/users/services` | `controllers.GetUserServiceStats` | `api/routes.go:153` |
| GET | `/admin/stats/users/tokens` | `controllers.GetUserTokenStats` | `api/routes.go:154` |
| GET | `/admin/stats/users/:user_id/services/time` | `controllers.GetUserServiceTimeStats` | `api/routes.go:157` |
| GET | `/admin/stats/users/:user_id/tokens/time` | `controllers.GetUserTokenTimeStats` | `api/routes.go:160` |
| GET | `/admin/stats/tokens/:token_id/time` | `controllers.GetTokenTimeStats` | `api/routes.go:163` |

### 代理路由

| 方法 | 路徑 | 中介層鏈與 Handler | 需 admin session | 經 token 驗證 | 依據 |
|---|---|---|---|---|---|
| 全部方法 | `/use/*path` | `NoIndex` → `FailureRecorder` → `TokenAuth` → `Logger` → `services.ProxyRequest` | 否 | 是 | `api/routes.go:169-170` |
| 全部方法 | `/api/*path` | 同上，舊客戶端相容別名 | 否 | 是 | `api/routes.go:173-174` |

`Any` 代表 Gin 註冊該路徑的所有 HTTP 方法。`/api/*` 與 `/use/*` 的 handler 完全相同（`api/routes.go:170`、`api/routes.go:174`）。

## 2. Token 驗證中介層的精確規則

### 2.1 Bearer 只在 `/use/` 解析

```go
path := c.Param("path")
path = strings.TrimPrefix(path, "/")

parts := strings.SplitN(path, "/", 3)
serviceName := parts[0]
bearer := ""
if strings.HasPrefix(c.FullPath(), "/use/") {
    header := strings.Fields(c.GetHeader("Authorization"))
    if len(header) == 2 && strings.EqualFold(header[0], "Bearer") {
        bearer = header[1]
    }
}
```
依據：`middlewares/auth.go:78-90`。

- `Authorization` 用 `strings.Fields` 切分，必須剛好兩個欄位，第一個不分大小寫等於 `Bearer`。
- `/api/*` 的 `c.FullPath()` 是 `/api/*path`，不符合前綴 `/use/`，所以 **`/api/` 不解析 Bearer**。測試也證明 `/api` 帶錯誤 Bearer 仍可用 URL token 通過（`middlewares/auth_test.go:108-116`）。

### 2.2 路徑不足的判定

```go
if len(parts) < 2 && bearer == "" {
    markFailure(c, "authentication", "invalid_path")
    c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "無效的API路徑"})
    return
}
```
依據：`middlewares/auth.go:91-95`。

- `parts` 由 `SplitN(path, "/", 3)` 產生。`/use/svc` 會得到 `["svc"]`，沒有 Bearer 時回 400。
- 有 Bearer 時會跳過這個檢查。
- 依程式推導，`/use/svc/` 會得到 `["svc", ""]`，不會回 400，而是進入空 token 查詢，最後回 403 `無效的Token`。此邊界未實測。

### 2.3 URL token 的辨識條件與優先順序

```go
tokenValue := bearer
targetEndpoint := ""
if len(parts) > 1 {
    urlToken := bearer == ""
    forceBearer := bearer != "" && strings.EqualFold(strings.TrimSpace(c.GetHeader("X-Infra-Token-Source")), "bearer")
    if !urlToken && !forceBearer {
        // Generated tokens are 32 lowercase hex characters. Existing custom
        // tokens (including revoked ones) also retain URL precedence.
        _, hexErr := hex.DecodeString(parts[1])
        urlToken = len(parts[1]) == 32 && hexErr == nil && parts[1] == strings.ToLower(parts[1])
        if !urlToken && parts[1] != "" {
            var existing models.Token
            err := db.DB.Session(&gorm.Session{Logger: logger.Discard}).Unscoped().Where("token_value = ?", parts[1]).First(&existing).Error
            if err == nil {
                urlToken = true
            } else if !errors.Is(err, gorm.ErrRecordNotFound) {
                markFailure(c, "local", "database_error")
                c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "無法查詢Token"})
                return
            }
        }
    }
    if urlToken {
        tokenValue = parts[1]
        if len(parts) > 2 {
            targetEndpoint = parts[2]
        }
    } else {
        targetEndpoint = strings.TrimPrefix(path, serviceName+"/")
    }
}
```
依據：`middlewares/auth.go:110-140`。

辨識條件整理：

1. `bearer == ""` 時，只要路徑有第二段，第二段一律視為 URL token。
2. `bearer != ""` 且 `X-Infra-Token-Source` 去空白後不分大小寫等於 `bearer` 時，強制使用 Bearer。
3. `bearer != ""` 且沒有強制 Bearer 時，第二段符合下列任一條件就視為 URL token，且 **URL token 優先，不因無效、停用、過期而改用 Bearer**：
   - 長度 32、可 hex 解碼、且等於自身小寫（產生的 token 格式，`controllers/token_controller.go:17-23`）。
   - 資料庫 `Unscoped()` 曾存在同 `token_value` 的 token。查詢不分服務、包含軟刪除與停用 token。
4. 上述資料庫查詢若發生非 `ErrRecordNotFound` 的錯誤，回 500 `無法查詢Token`。
5. 若判定不是 URL token，才使用 Bearer，`targetEndpoint` 是去掉服務名稱後的完整剩餘路徑。

`X-Infra-Token-Source: bearer` 只有在 Bearer 存在時才有作用。Bearer 為空時 `forceBearer` 必為 false。

### 2.4 服務查詢

```go
var service models.Service
if err := db.DB.Where("name = ? AND is_active = ?", serviceName, true).First(&service).Error; err != nil {
    if errors.Is(err, gorm.ErrRecordNotFound) {
        markFailure(c, "authentication", "service_unavailable")
    } else {
        markFailure(c, "local", "database_error")
    }
    c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "找不到服務"})
    return
}
```
依據：`middlewares/auth.go:97-107`。

- 只接受 `name` 完全相同且 `is_active = true` 的服務。
- 服務不存在、服務停用、資料庫查詢錯誤都回 404 `找不到服務`。資料庫錯誤另外記為 `local/database_error`。

### 2.5 Token 查詢、到期與使用者狀態

```go
if err := db.DB.Session(&gorm.Session{Logger: logger.Discard}).Where("token_value = ? AND service_id = ? AND is_active = ?", tokenValue, service.ID, true).First(&token).Error; err != nil {
    if errors.Is(err, gorm.ErrRecordNotFound) {
        markFailure(c, "authentication", "invalid_token")
    } else {
        markFailure(c, "local", "database_error")
    }
    c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "無效的Token"})
    return
}

farFuture := time.Now().AddDate(900, 0, 0)
if token.ExpiresAt.Before(time.Now()) && token.ExpiresAt.Before(farFuture) {
    markFailure(c, "authentication", "expired_token")
    c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Token已過期"})
    return
}
```
依據：`middlewares/auth.go:142-160`。

- Token 查詢條件是 `token_value`、`service_id`、`is_active = true` 三者同時成立。
- `disabled` 不在此查詢條件內。只要 `is_active` 仍為 true，`disabled = true` 的 token 理論上仍可通過這一關。實務上刪除服務會同時把 `is_active` 設為 false（`controllers/service_controller.go:116-122`）。
- 到期判定：`ExpiresAt` 早於現在，且早於「現在 + 900 年」才算過期。超過 900 年的到期日視為永久有效。永久 token 建立時設為現在 + 1000 年（`controllers/token_controller.go:164-167`）。

```go
var user models.User
if err := db.DB.Where("id = ? AND is_active = ?", token.UserID, true).First(&user).Error; err != nil {
    if errors.Is(err, gorm.ErrRecordNotFound) {
        markFailure(c, "authentication", "user_unavailable")
    } else {
        markFailure(c, "local", "database_error")
    }
    c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "用戶已被停權"})
    return
}
```
依據：`middlewares/auth.go:162-172`。使用者不存在、停用、查詢錯誤都回 403 `用戶已被停權`。

### 2.6 失敗回應總表

| 情況 | HTTP | Body | 失敗 stage/reason | 依據 |
|---|---|---|---|---|
| 路徑不足且無 Bearer | 400 | `{"error":"無效的API路徑"}` | `authentication/invalid_path` | `auth.go:91-94` |
| 服務不存在或停用 | 404 | `{"error":"找不到服務"}` | `authentication/service_unavailable` | `auth.go:99-106` |
| 服務查詢資料庫錯誤 | 404 | `{"error":"找不到服務"}` | `local/database_error` | `auth.go:102-105` |
| URL token 存在性查詢資料庫錯誤 | 500 | `{"error":"無法查詢Token"}` | `local/database_error` | `auth.go:125-128` |
| Token 無效、停用、服務不符 | 403 | `{"error":"無效的Token"}` | `authentication/invalid_token` | `auth.go:144-151` |
| Token 查詢資料庫錯誤 | 403 | `{"error":"無效的Token"}` | `local/database_error` | `auth.go:147-150` |
| Token 過期 | 403 | `{"error":"Token已過期"}` | `authentication/expired_token` | `auth.go:154-159` |
| 使用者不存在或停用 | 403 | `{"error":"用戶已被停權"}` | `authentication/user_unavailable` | `auth.go:164-171` |
| 使用者查詢資料庫錯誤 | 403 | `{"error":"用戶已被停權"}` | `local/database_error` | `auth.go:167-170` |

### 2.7 Authorization 是否轉送上游

會轉送。`ProxyRequest` 複製所有請求標頭，只略過 `Host`：

```go
for key, values := range c.Request.Header {
    if key != "Host" {
        for _, value := range values {
            proxyReq.Header.Add(key, value)
        }
    }
}
```
依據：`services/proxy.go:68-76`。

因此 `Authorization`、`X-Infra-Token-Source`、`Cookie` 等都會原樣送到上游。程式沒有另外加上 `X-Forwarded-For`、`X-Forwarded-Proto` 或 `X-Real-IP`。

## 3. 反向代理行為

### 3.1 請求轉送

- HTTP 方法沿用原始請求：`http.NewRequest(c.Request.Method, targetURL.String(), nil)`（`services/proxy.go:55`）。
- Body 直接接上原始 `c.Request.Body`，並複製 `Content-Length`，不先讀進記憶體（`services/proxy.go:62-66`）。
- Query string 原樣複製：`targetURL.RawQuery = c.Request.URL.RawQuery`（`services/proxy.go:52`）。
- 目標路徑以 `path.Join(targetURL.Path, targetEndpointStr)` 重組（`services/proxy.go:49`）。`path.Join` 會清理重複斜線與 `.`、`..`。
- `targetEndpoint` 的來源：
  - URL token：第三段之後的完整剩餘路徑，例如 `/use/svc/<token>/items/42` → `items/42`（`auth.go:132-136`）。
  - Bearer：去掉 `/use/<service>/` 後的完整剩餘路徑，例如 `/use/svc/items/42` → `items/42`（`auth.go:137-139`）。
- URL token 沒有目標路徑時，`targetEndpoint` 是空字串。
- BaseURL 的 scheme、host、userinfo、原有 path 都保留，只把 path 與 targetEndpoint 串接。

### 3.2 標頭

- 請求標頭：全部複製，除了 `Host`（`services/proxy.go:68-76`）。`Host` 由 `http.Client` 依目標 URL 設定。
- 回應標頭：全部複製（`services/proxy.go:97-118`）。
- `Location` 不改寫（`services/proxy.go:98-104`）。
- `Set-Cookie` 會用 regex `(?i)Domain=[^;]+;?\s?` 移除 `Domain` 屬性（`services/proxy.go:106-112`）。
- 代理回應額外設定 `Cache-Control: no-store, no-cache, must-revalidate, max-age=0`、`Pragma: no-cache`、`Expires: 0`、`X-Robots-Tag: noindex, nofollow`（`services/proxy.go:120-125`）。
- `NoIndex` 中介層也會先設定 `X-Robots-Tag`（`middlewares/noindex.go:6-10`）。

### 3.3 逾時、串流與用戶端中斷

- **沒有應用層逾時**。`client := &http.Client{}` 的 `Timeout` 是零值，代表不限制整體請求時間（`services/proxy.go:79`）。實際只有 Go `http.DefaultTransport` 的連線與 TLS handshake 預設逾時。
- **沒有用請求 context**。使用 `http.NewRequest` 而非 `http.NewRequestWithContext`（`services/proxy.go:55`），所以用戶端斷線不會取消上游請求。此為依程式推導，未實測。
- **預設跟隨重新導向**。`http.Client` 未設定 `CheckRedirect`，會依 Go 預設最多跟隨 10 次。上游的 3xx 可能在代理端就被跟完，客戶端看到的是最後一個回應。此為依 Go 標準函式庫行為推導。
- **沒有 cookie jar**。上游 `Set-Cookie` 不會在代理端保存，也不會自動帶入後續請求。
- 串流判定（`services/proxy.go:168-190`）：
  - `Transfer-Encoding` 含 `chunked`
  - `Content-Type` 開頭為 `text/event-stream` 或 `multipart/x-mixed-replace`
  - `ContentLength == -1`
- 串流模式：先 `c.Status(proxyResp.StatusCode)`，非 HEAD 才用 `io.Copy` 逐塊寫回（`services/proxy.go:128-138`）。程式沒有明確呼叫 `Flush`，SSE 是否即時送出取決於 net/http 與 Gin 的 writer，未實測。
- 非串流模式：`io.ReadAll` 完整讀入後，設定 `Content-Length` 為實際長度、移除 `Transfer-Encoding`、寫回 body（`services/proxy.go:141-160`）。HEAD 不寫 body。
- 依程式推導，HEAD 回應若走非串流分支，`Content-Length` 會被覆寫成 `0`，因為讀到的 body 長度是 0。未實測。

### 3.4 上游錯誤與失敗分類

| 情況 | 回應 | 失敗 stage/reason | 依據 |
|---|---|---|---|
| BaseURL 解析失敗 | 500 `{"error":"服務URL配置錯誤"}` | `local/invalid_service_url` | `proxy.go:41-45` |
| 建立代理請求失敗 | 500 `{"error":"無法創建代理請求"}` | `local/request_creation_failed` | `proxy.go:55-59` |
| 連線、DNS、TLS、逾時失敗 | 502 `{"error":"代理請求失敗","details":"<err>"}` | `upstream_connection/<classify>` | `proxy.go:80-84` |
| 上游回應 4xx/5xx | 原狀態碼與原 body 轉回 | `upstream_response/http_error`，另記上游狀態碼 | `proxy.go:87-90` |
| 非串流讀取上游 body 失敗 | 500 `{"error":"讀取代理響應失敗"}` | `upstream_response/response_read_failed` | `proxy.go:142-147` |
| 串流傳輸失敗 | 不保證改狀態碼，只記錄 | `transfer/response_transfer_failed`，另記上游狀態碼 | `proxy.go:132-136` |
| 寫回客戶端失敗 | 不保證改狀態碼，只記錄 | `transfer/client_write_failed`，另記上游狀態碼 | `proxy.go:154-159` |

`classifyConnectionError` 的分類（`services/proxy.go:192-213`）：

| 判斷 | reason |
|---|---|
| `net.Error.Timeout()` | `timeout` |
| `*net.DNSError` | `dns_failed` |
| `syscall.ECONNREFUSED` | `connection_refused` |
| x509 憑證或 TLS record 錯誤 | `tls_failed` |
| 其他 | `connection_failed` |

`details` 會帶出 `err.Error()`（`proxy.go:83`），可能包含目標 URL 或內部位址。這是既有行為。

## 4. 資料模型

所有 model 定義在 `models/models.go` 與 `models/dashboard_snapshot.go`。

### 4.1 User

| 欄位 | 型別 | 標籤與說明 | 依據 |
|---|---|---|---|
| `gorm.Model` | 嵌入 | 提供 `CreatedAt`、`UpdatedAt`、`DeletedAt`（軟刪除，`DeletedAt` 有 index） | `models.go:11` |
| `ID` | `uint` | `primaryKey` | `models.go:12` |
| `Username` | `string` | `unique;not null` | `models.go:13` |
| `IsActive` | `bool` | `default:true`，停權欄位 | `models.go:14` |
| `Tokens` | `[]Token` | `foreignKey:UserID` | `models.go:15` |
| `AccessLogs` | `[]AccessLog` | `foreignKey:UserID` | `models.go:16` |

### 4.2 Service

| 欄位 | 型別 | 標籤與說明 | 依據 |
|---|---|---|---|
| `gorm.Model` | 嵌入 | `CreatedAt`、`UpdatedAt`、`DeletedAt` | `models.go:21` |
| `ID` | `uint` | `primaryKey` | `models.go:22` |
| `Name` | `string` | `unique;not null` | `models.go:23` |
| `Description` | `string` | 無額外標籤 | `models.go:24` |
| `Type` | `string` | `not null;default:''`，最多 64 字（controller 驗證） | `models.go:25`、`controllers/service_controller.go:50-53` |
| `BaseURL` | `string` | `not null` | `models.go:26` |
| `IsActive` | `bool` | `default:true`，停權欄位 | `models.go:27` |
| `Tokens` | `[]Token` | `foreignKey:ServiceID` | `models.go:28` |
| `AccessLogs` | `[]AccessLog` | `foreignKey:ServiceID` | `models.go:29` |

### 4.3 Token

| 欄位 | 型別 | 標籤與說明 | 依據 |
|---|---|---|---|
| `gorm.Model` | 嵌入 | `CreatedAt`、`UpdatedAt`、`DeletedAt` | `models.go:34` |
| `ID` | `uint` | `primaryKey` | `models.go:35` |
| `TokenValue` | `string` | `unique;not null`，**明文儲存** | `models.go:36` |
| `UserID` | `uint` | `not null` | `models.go:37` |
| `User` | `User` | 關聯 | `models.go:38` |
| `ServiceID` | `uint` | `not null` | `models.go:39` |
| `Service` | `Service` | 關聯 | `models.go:40` |
| `Description` | `string` | 備註 | `models.go:41` |
| `ExpiresAt` | `time.Time` | 到期時間，未設 not null | `models.go:42` |
| `IsActive` | `bool` | `default:true`，停權欄位 | `models.go:43` |
| `Disabled` | `bool` | `default:false`，失效紀錄欄位 | `models.go:44` |
| `AccessLogs` | `[]AccessLog` | `foreignKey:TokenID` | `models.go:45` |

Token 產生方式：16 bytes 隨機值轉 32 字小寫 hex（`controllers/token_controller.go:17-23`）。未使用雜湊。API 回應會直接包含 `token_value`。

### 4.4 AccessLog

| 欄位 | 型別 | 說明 | 依據 |
|---|---|---|---|
| `gorm.Model` | 嵌入 | `CreatedAt`、`UpdatedAt`、`DeletedAt` | `models.go:50` |
| `UserID` | `uint` | 使用者 ID | `models.go:51` |
| `TokenID` | `uint` | Token ID | `models.go:52` |
| `ServiceID` | `uint` | 服務 ID | `models.go:53` |
| `Endpoint` | `string` | 請求的 `URL.Path`，含網址 token 時會保存 token | `models.go:54`、`middlewares/auth.go:210` |
| `Method` | `string` | HTTP 方法 | `models.go:55` |
| `StatusCode` | `int` | 回應狀態碼 | `models.go:56` |
| `RequestSize` | `int64` | `ContentLength` | `models.go:57` |
| `ResponseSize` | `int64` | `c.Writer.Size()` | `models.go:58` |
| `Duration` | `int64` | 毫秒 | `models.go:59` |

### 4.5 RequestFailure

| 欄位 | 型別 | 說明 | 依據 |
|---|---|---|---|
| `ID` | `uint` | `primaryKey` | `models.go:64` |
| `CreatedAt` | `time.Time` | `index` | `models.go:65` |
| `Stage` | `string` | `authentication`、`local`、`upstream_connection`、`upstream_response`、`transfer` | `models.go:66` |
| `Reason` | `string` | 具體原因 | `models.go:67` |
| `StatusCode` | `int` | 本系統回應碼 | `models.go:68` |
| `UpstreamStatusCode` | `*int` | 上游回應碼，可為 null | `models.go:69` |
| `ServiceID` | `*uint` | 服務 ID，可為 null | `models.go:70` |

沒有 `gorm.Model`，沒有軟刪除，沒有 `UpdatedAt`。

### 4.6 Admin

| 欄位 | 型別 | 說明 | 依據 |
|---|---|---|---|
| `gorm.Model` | 嵌入 | `CreatedAt`、`UpdatedAt`、`DeletedAt` | `models.go:75` |
| `Username` | `string` | `unique;not null` | `models.go:76` |
| `Password` | `string` | `not null`，JSON 不輸出，bcrypt 雜湊 | `models.go:77`、`db/db.go:78` |

### 4.7 DashboardSnapshot

| 欄位 | 型別 | 說明 | 依據 |
|---|---|---|---|
| `Date` | `string` | `primaryKey`，台北日期 `YYYY-MM-DD` | `models/dashboard_snapshot.go:7` |
| `ObservedAt` | `time.Time` | `not null`，UTC 觀測時間 | `models/dashboard_snapshot.go:8` |
| `ActiveUsers` | `int64` | 啟用使用者數 | `models/dashboard_snapshot.go:9` |
| `ActiveServices` | `int64` | 啟用服務數 | `models/dashboard_snapshot.go:10` |
| `ActiveTokens` | `int64` | 有效 Token 數 | `models/dashboard_snapshot.go:11` |
| `ExpiringTokens` | `int64` | 7 天內到期 Token 數 | `models/dashboard_snapshot.go:12` |

### 4.8 索引

- 唯一索引：`users.username`、`services.name`、`tokens.token_value`（`models.go:13`、`models.go:23`、`models.go:36`）。
- `gorm.Model` 的 `DeletedAt` 有 GORM 預設 index。
- `RequestFailure.CreatedAt` 有 `index`（`models.go:65`）。
- `db.go` 另外建立：
  - `idx_access_logs_created_at ON access_logs(created_at)`（`db/db.go:42`）
  - `idx_access_logs_instant ON access_logs(julianday(created_at))`（`db/db.go:46`）
  - `idx_request_failures_instant ON request_failures(julianday(created_at))`（`db/db.go:46`）
- 沒有針對 `user_id`、`service_id`、`token_id` 的外鍵欄位索引標籤。實際 FK constraint 是否由 GORM AutoMigrate 建立，未在檔案中找到 DDL，標為不確定。

## 5. migrations 目錄

套用順序由 `db.ApplyDashboardMigration` 決定，整個流程包在單一 transaction（`db/dashboard_migration.go:15-23`）。

1. `db/migrations/20261028_service_type.sql`
   - 內容：`ALTER TABLE services ADD COLUMN type TEXT NOT NULL DEFAULT '';`（`20261028_service_type.sql:1`）
   - 只有當 `services` 表存在且沒有 `type` 欄位時才執行（`dashboard_migration.go:17-21`）。
   - 用途：讓舊資料庫補上服務類型欄位，既有資料的 `type` 為空字串。

2. `db/migrations/20261028_dashboard_snapshots.sql`
   - 內容：`CREATE TABLE IF NOT EXISTS dashboard_snapshots (...)`，欄位同 `DashboardSnapshot`（`20261028_dashboard_snapshots.sql:1-8`）。
   - 每次都執行，靠 `IF NOT EXISTS` 保持可重複（`dashboard_migration.go:22`）。

migration 之後：

3. `db.InitDB` 執行 `AutoMigrate(&User{}, &Service{}, &Token{}, &AccessLog{}, &Admin{}, &RequestFailure{})`（`db/db.go:38`）。
4. 建立 access log 與 julianday 表達式索引（`db/db.go:42-50`）。
5. `main.go` 又執行一次 `AutoMigrate(&User{}, &Service{}, &Token{}, &AccessLog{}, &Admin{})`（`main.go:20`）。第二次沒有 `RequestFailure`，因為 `InitDB` 已處理。

## 6. 使用紀錄與失敗紀錄

### 6.1 使用紀錄 AccessLog

寫入時機：`Logger` 在 `c.Next()` 之後，只有 context 有 `token` 才寫入（`middlewares/auth.go:185-221`）。

寫入欄位（`middlewares/auth.go:206-216`）：

| 欄位 | 來源 |
|---|---|
| `UserID` | `token.UserID` |
| `TokenID` | `token.ID` |
| `ServiceID` | `service.ID` |
| `Endpoint` | `c.Request.URL.Path` |
| `Method` | `c.Request.Method` |
| `StatusCode` | `c.Writer.Status()` |
| `RequestSize` | `c.Request.ContentLength` |
| `ResponseSize` | `int64(c.Writer.Size())` |
| `Duration` | `time.Since(startTime).Milliseconds()` |

保留政策：

- 最多 90 天：`RemoveExpiredAccessLogs` 以 `julianday(created_at) < julianday(now-90d)` 刪除（`middlewares/failure.go:76-78`）。
- 最多 1,000,000 筆：每次寫入後，若新 ID 超過上限，刪除 `id <= 新ID - 1000000`（`middlewares/failure.go:65-73`、`middlewares/auth.go:218`）。
- 啟動時立即清理一次，之後每 24 小時清理（`middlewares/failure.go:95-118`、`main.go:23`）。
- 清理用每批 1000 筆、批次間 sleep 1ms（`middlewares/failure.go:81-93`）。
- 啟動時的筆數清理用 `max(id) - 上限` 的 rolling window（`middlewares/failure.go:98-100`）。

截斷：

- `Endpoint` 最多 512 個 rune（`middlewares/failure.go:59-61`）。
- `Method` 最多 32 個 rune（`middlewares/failure.go:62-64`）。
- 只套用在新寫入的 AccessLog。既有資料不會回溯截斷。

已知風險：`Endpoint` 是完整 `URL.Path`，使用網址 token 時會把 token 寫進資料庫（`middlewares/auth.go:210`）。這與 `AGENTS.md` 的第一條 follow-up 一致。

### 6.2 失敗紀錄 RequestFailure

寫入時機：`FailureRecorder` 在 `c.Next()` 之後讀取 context 的 `requestFailure`，補上 `c.Writer.Status()` 後寫入（`middlewares/failure.go:14-30`）。Proxy 也可直接 `c.Set("requestFailure", ...)`（`services/proxy.go:36-39`）。

寫入欄位：

- `Stage`、`Reason`：由 `markFailure` 或 proxy 設定。
- `StatusCode`：`c.Writer.Status()`（`failure.go:25`）。
- `UpstreamStatusCode`：proxy 有上游狀態碼時設定。
- `ServiceID`：服務已找到後才會有值（`auth.go:109`、`failure.go:45-52`）。
- `CreatedAt`：GORM 自動填入。

保留政策：

- 最多 90 天（`middlewares/failure.go:54-56`）。
- 最多 100,000 筆（`middlewares/failure.go:26`、`failure.go:33-43`）。
- 清理時機與批次方式同 AccessLog。
- 失敗紀錄沒有保存路徑、方法或 token，因此沒有截斷問題。

## 7. 儀表板快照與統計的計算規則

### 7.1 每日快照

程式碼：`services/dashboard_snapshot.go`。

- 時區：固定 `Asia/Taipei` UTC+8，`time.FixedZone`（`dashboard_snapshot.go:13`）。
- 先將傳入時間轉 UTC，再換算台北日期作為 `Date`（`dashboard_snapshot.go:17-18`）。
- `ObservedAt` 存 UTC 時間（`dashboard_snapshot.go:17`、`dashboard_snapshot.go:36`）。
- 每個台北日只保留第一次成功觀測。若當天已有資料，不覆寫（`dashboard_snapshot.go:20-40`）。插入使用 `ON CONFLICT DO NOTHING`（`dashboard_snapshot.go:37`）。
- 計算四項數量（`dashboard_snapshot.go:26-32`）：
  - `active_users`：`users.deleted_at IS NULL AND is_active = 1`
  - `active_services`：`services.deleted_at IS NULL AND is_active = 1`
  - `active_tokens`：token、user、service 都未軟刪除，token `is_active = 1`、`COALESCE(disabled,0) = 0`，user 與 service 都 `is_active = 1`，且 `expires_at > now`
  - `expiring_tokens`：符合上述條件，且 `expires_at > now` 且 `expires_at <= now + 7 天`
- 保留：刪除台北日期早於「今天 - 89 天」的資料，並只留最新 90 筆（`dashboard_snapshot.go:41-45`）。兩個條件都套用。
- 啟動時立即觀測一次，之後每小時一次（`dashboard_snapshot.go:70-85`）。停機期間不補資料。
- 上月同日比較（`dashboard_snapshot.go:49-68`）：
  - 取上個月第一天，再加一個月減一天，得到上月最後一天。
  - 若本月日期大於上月最後一天，改用上月最後一天。
  - 找不到資料回 `nil, nil`。
- API `GET /admin/stats/dashboard-snapshot` 回 `{"previous":<snapshot|null>}`（`controllers/dashboard_snapshot.go:12-19`）。

### 7.2 儀表板統計端點

| 端點 | 日期範圍 | 時區與分組 | 成功/錯誤定義 | 依據 |
|---|---|---|---|---|
| `/admin/stats/dashboard-recent` | `days` 預設 30，允許 1–90 | 台北 UTC+8，SQL `DATE(created_at,'+08:00')` | 只計次數與 distinct 數 | `controllers/dashboard_stats.go:12-38` |
| `/admin/stats/dashboard-services` | 最近 30 個台北日 | 台北 UTC+8 取範圍，last_used 輸出 UTC ISO | `errors` 為 `status_code >= 400 AND < 600` | `controllers/dashboard_stats.go:41-66` |
| `/admin/stats/dashboard-responses` | `start`/`end` 或 `days`，最多 90 天 | 台北 UTC+8，SQL `DATE(created_at,'+08:00')` | `success` 為 100–399，`error` 為 `>= 400` | `controllers/dashboard_response_chart.go:17-44`、`controllers/dashboard_date_range.go:11-37` |
| `/admin/stats/dashboard-service-details` | 同 dashboard-responses | 台北 UTC+8，last_used 輸出 UTC ISO | `success` 100–399，`errors` `>= 400`，users/tokens 用 `NULLIF(...,0)` 排除 0 | `controllers/dashboard_service_detail.go:11-42` |

`dashboardDateRange` 規則（`controllers/dashboard_date_range.go:11-37`）：

- 有 `start` 或 `end` 時，兩者都必須提供，且不能再帶 `days`。
- 日期以台北時區解析 `YYYY-MM-DD`。
- `end` 不能早於 `start`，且差距必須小於 90 天。
- 回傳的結束時間是 `end + 1 天`，SQL 用 `>= start AND < end`，所以結束日期包含在內。
- 沒有 `start`/`end` 時，`days` 預設 30，允許 1–90。
- 錯誤回 400，body 是 `{"error":"日期範圍須為 1 到 90 天"}` 或 `{"error":"請提供有效的起訖日期"}`。

### 7.3 舊版統計端點

`GET /admin/stats/recent` 使用伺服器本地時區的 `time.Now()` 與 `Truncate(24*time.Hour)`，SQL 用 `DATE(created_at)`，不是台北時區（`controllers/stats_controller.go:229-311`）。SQLite 的 `DATE()` 預設按 UTC 解讀。這與 dashboard 系列端點不同。

舊版統計查詢也沒有 `deleted_at IS NULL` 條件（`controllers/stats_controller.go:243-258`、`stats_controller.go:37-55` 等），所以軟刪除的 access log 仍可能被計入。Dashboard 系列則有 `deleted_at IS NULL`。

### 7.4 前端詳細圖表的 UTC 用法

管理頁文字明示「以下服務、使用者與 Token 的每日趨勢以 UTC 日期計算」（`templates/dashboard.html:98`）。前端 `main.js` 的日期範圍與過濾使用 UTC，例如 `setUTCDate`、`setUTCHours`（`static/js/main.js:266-271`、`main.js:841-845`）。測試 `tests/chart_refresh_test.js:57-70` 也固定驗證 UTC 日期範圍。

## 8. 管理介面

### 8.1 登入方式

- 頁面：`GET /login`，已登入會轉 `/dashboard`（`controllers/auth_controller.go:31-43`）。
- 登入 API：`POST /auth/login`，JSON body 為 `{"username":"...","password":"..."}`，兩個欄位都 required（`auth_controller.go:17-21`、`auth_controller.go:46-59`）。
  - 格式錯誤：400 `{"error":"請提供有效的用戶名和密碼"}`。
  - 憑證錯誤：401 `{"error":"無效的憑證"}`。
  - 成功：200 `{"message":"登入成功"}`，並設定 session。
- 登出：`GET /logout`，清除 session 後 302 到 `/login`（`auth_controller.go:61-68`）。
- 登入驗證：查 `admins.username`，用 bcrypt 比對 `admins.password`（`middlewares/auth.go:51-59`）。
- Session：
  - Cookie store secret 是硬編碼 `infra_manager_secret`（`api/routes.go:36`）。
  - Session 名稱 `infra_manager_session`（`api/routes.go:44`）。
  - `Path=/`、`MaxAge=86400`、`HttpOnly=true`、`Secure=false`、`SameSite=Lax`（`api/routes.go:37-43`、`middlewares/auth.go:64-70`）。
  - Session 只存 `admin_id`（`middlewares/auth.go:63`）。
- 每次進入 `AdminAuth` 都會用 `admin_id` 查 `admins` 表。管理員不存在就清 session 並轉 `/login`（`middlewares/auth.go:33-42`）。

### 8.2 預設管理員與環境變數密碼

- 只有在 `admins` 表完全沒有資料時，才建立預設管理員（`db/db.go:57-62`）。
- 帳號讀 `ADMIN_USER`，未設定用 `admin`（`db/db.go:63-70`）。
- 密碼讀 `ADMIN_PASS`，未設定用 `admin123`（`db/db.go:72-75`）。
- 密碼用 bcrypt `DefaultCost` 雜湊後存入（`db/db.go:78-87`）。
- 管理員已存在時，之後再改 `ADMIN_USER` 或 `ADMIN_PASS` 不會更新資料庫密碼。要改密碼需用管理介面的修改密碼功能。

### 8.3 修改密碼

- 頁面：`GET /change-password`，需 admin session（`api/routes.go:97`、`controllers/auth_controller.go:70-75`）。
- API：`POST /admin/change-password`，需 admin session（`api/routes.go:105`）。
- Body：`old_password`、`new_password`、`confirm_password`，皆 required（`auth_controller.go:23-28`）。
- 錯誤：
  - 缺少欄位：400 `{"error":"請提供所有必要的密碼資訊"}`
  - 兩次新密碼不一致：400 `{"error":"兩次輸入的新密碼不一致"}`
  - 未登入：401 `{"error":"請先登入"}`
  - 找不到管理員：500 `{"error":"無法找到管理員資訊"}`
  - 舊密碼錯：401 `{"error":"舊密碼不正確"}`
  - 加密或儲存失敗：500
- 成功：200 `{"message":"密碼已成功更新"}`（`auth_controller.go:78-127`）。

### 8.4 頁面與模板

`r.LoadHTMLGlob("templates/*")`（`api/routes.go:52`）。

| 模板 | 路由 | 用途 |
|---|---|---|
| `templates/login.html` | `/login` | 登入頁 |
| `templates/dashboard.html` | `/dashboard` | 儀表板 |
| `templates/failures.html` | `/failures` | 失敗紀錄 |
| `templates/users.html` | `/users` | 使用者管理 |
| `templates/services.html` | `/services` | 服務管理 |
| `templates/tokens.html` | `/tokens` | Token 管理 |
| `templates/change_password.html` | `/change-password` | 修改管理員密碼 |
| `templates/index.html` | 無路由 | 未使用的舊登入頁，仍有 `main.js` 引用 |

頁面標題由 handler 傳入 `title`，格式多為「頁面名稱 | infra-manager」，服務名稱常數在 `consts/name.go:3`。

### 8.5 static 結構

- `/static` 對應 `./static`（`api/routes.go:47`）。
- CSS：`static/css/admin.css`、`main.css`、`failures.css`。
- JS：`static/js/admin.js`、`main.js`、`overview.js`、`dashboard-date.js`、`dashboard-search.js`、`dashboard-search-target.js`、`failures.js`。
- `static/robots.txt`：`Disallow: /use/`、`Disallow: /api/`、`Allow: /static/`。
- 管理頁與靜態資源會加 `Cache-Control: no-store...`，並移除 `If-Modified-Since`、`If-None-Match`（`api/routes.go:23-33`）。
- `/robots.txt` 不套用上述 no-store（`api/routes.go:25`）。

## 9. 設定與部署

### 9.1 環境變數

| 變數 | 預設值 | 用途 | 依據 |
|---|---|---|---|
| `PORT` | `8080` | Gin 監聽埠 | `main.go:27-30` |
| `ADMIN_USER` | `admin` | 首次建立管理員的帳號 | `db/db.go:63-70` |
| `ADMIN_PASS` | `admin123` | 首次建立管理員的密碼 | `db/db.go:72-75` |
| `GIN_MODE` | 未設定時 Gin 預設 debug | Gin 模式，Compose 設為 `release` | `docker-compose.yml:10` |
| `DATA_DIR` | 無預設，Compose 要求必填 | Docker 主機上的資料目錄，綁定到容器 `/app/data` | `docker-compose.yml:13`、`.env.example:1` |

沒有資料庫路徑環境變數。資料庫固定為工作目錄下的 `data/infra_manager.db`（`db/db.go:21-28`）。`data` 目錄不存在時會自動建立，權限 0755（`db/db.go:22-26`）。

Compose 只把 `GIN_MODE=release` 傳進容器，沒有傳 `ADMIN_USER`、`ADMIN_PASS` 或 `PORT`（`docker-compose.yml:9-10`）。

### 9.2 Dockerfile

- 基底映像：`golang:1.27.3-alpine`（`Dockerfile:1`）。
- 工作目錄 `/app`（`Dockerfile:4`）。
- 複製 `go.mod`、`go.sum` 後 `go mod download`（`Dockerfile:7-10`）。
- 複製全部原始碼（`Dockerfile:13`）。
- `CGO_ENABLED=0 go build -o main .`（`Dockerfile:16`）。
- 啟動 `./main`（`Dockerfile:18`）。
- 映像內含 `static/` 與 `templates/`，因為 `COPY . .`。

### 9.3 Docker Compose

- 服務名稱 `app`，映像 `infra-manager:latest`（`docker-compose.yml:2-3`）。
- 連接埠 `1452:8080`（`docker-compose.yml:8`）。
- 資料 volume：`${DATA_DIR:?請設定 DATA_DIR}` 綁定到 `/app/data`，`create_host_path: true`（`docker-compose.yml:11-16`）。
- `restart: always`（`docker-compose.yml:17`）。
- 外部網路 `infra-net`（`docker-compose.yml:18-23`）。
- `DATA_DIR` 未設定或為空時 Compose 直接報錯，不會啟動（`README.md:33`）。

## 10. 測試涵蓋範圍

### 10.1 Go 測試

| 測試名稱 | 檔案 | 涵蓋行為 |
|---|---|---|
| `TestUseTokenAuthentication` | `middlewares/auth_test.go:22` | URL token、Bearer、大小寫 Bearer、custom URL token 優先、無效/停用/過期 URL token 不回退 Bearer、缺少 token 400、`X-Infra-Token-Source: bearer`、`/api` 不解析 Bearer、代理路徑/query/Authorization 轉送、無效 Bearer 不寫入 DB log |
| `TestClassifyConnectionError` | `services/proxy_test.go:11` | DNS、連線拒絕、逾時、TLS、其他連線錯誤分類 |
| `TestNonStreamingClientWriteFailureIsRecorded` | `services/proxy_write_test.go:22` | 非串流寫回客戶端失敗記為 `transfer/client_write_failed`，保留上游 200 |
| `TestFailureEvents` | `middlewares/failure_test.go:18` | `/use` 與 `/api` 的 403 驗證失敗、上游 503、連線失敗 502 的 stage 與狀態碼 |
| `TestFailureCleanupRemovesOnlyExpired` | `middlewares/failure_test.go:83` | 失敗紀錄 90 天清理邊界 |
| `TestFailureLimitKeepsNewest` | `middlewares/failure_test.go:111` | 失敗紀錄筆數上限保留最新 |
| `TestAccessLogCleanupKeepsNinetyDays` | `middlewares/failure_test.go:137` | 使用紀錄 90 天清理邊界 |
| `TestAccessLogLimitKeepsNewest` | `middlewares/failure_test.go:167` | 使用紀錄筆數上限保留最新 |
| `TestAccessLogLengthIsBounded` | `middlewares/failure_test.go:193` | Endpoint 512 rune、Method 32 rune 截斷 |
| `TestCleanupUsesAbsoluteTimesAcrossOffsets` | `middlewares/failure_test.go:217` | 不同時區字串的絕對時間清理 |
| `TestCleanupAcrossBatches` | `middlewares/failure_test.go:249` | 超過 1000 筆的批次清理 |
| `TestManagementResponsesDoNotUseCachedVersions` | `api/cache_test.go:11` | 管理頁、admin API、static 的 no-store，`robots.txt` 不受影響 |
| `TestDashboardDateRange` | `controllers/dashboard_date_range_test.go:15` | 自訂日期缺漏、反向、超過 90 天、格式錯誤，以及圖表與服務明細的日期過濾 |
| `TestDashboardResponseChart` | `controllers/dashboard_response_chart_test.go:16` | 台北日分組、100–399 成功、400 以上錯誤、`days` 驗證 |
| `TestDashboardServiceUsage` | `controllers/dashboard_service_test.go:15` | 最近 30 天服務使用量、錯誤數、排除 35 天前資料 |
| `TestDashboardServiceDetail` | `controllers/dashboard_service_detail_test.go:16` | 服務明細 count/errors/users/tokens，排除 35 天前資料 |
| `TestDashboardServiceDetailsIncludeType` | `controllers/dashboard_service_type_test.go:14` | 服務明細包含 `type` |
| `TestDashboardStatsTaipeiDaysAndDistinctCounts` | `controllers/dashboard_stats_test.go:16` | 台北日界線、跨時區同一時刻、distinct user 計數、`days` 驗證 |
| `TestFailureQueryFiltersAndPages` | `controllers/failure_controller_test.go:17` | stage、status_code、upstream_status_code、before_id 分頁、until 時間篩選、錯誤參數 |
| `TestServiceTypeCreateEditAndLegacyUpdate` | `controllers/service_type_test.go:16` | 建立 type、舊版更新不覆寫 type、`type:""` 可清除 |
| `TestServiceDetailsPreserveDisabledStatus` | `controllers/service_details_test.go:14` | `/details` 更新不改變停用狀態 |
| `TestEditTokenWithoutStatusPreservesState` | `controllers/token_update_test.go:15` | `/tokens/:id/details` 不改變 `is_active` |
| `TestLegacyTokenUpdateStillHonorsStatus` | `controllers/token_update_test.go:47` | 舊版 `PUT /tokens/:id` 省略 `is_active` 時會設為 false |
| `TestDashboardMigrationPreservesExistingServicesAndIsRepeatable` | `db/dashboard_migration_test.go:9` | service type migration 保留舊資料、可重複執行、建立 snapshot 表 |
| `TestDashboardUpgradeThenAutoMigratePreservesLegacyService` | `db/dashboard_full_upgrade_test.go:10` | 舊 schema 經 migration 與 AutoMigrate 後資料不變 |
| `TestDashboardSnapshotRetentionAndMissingDays` | `services/dashboard_snapshot_test.go:13` | 同日不覆寫、缺上月資料回 nil、最多 90 筆 |
| `TestDashboardSnapshotCountsAndPreviousMonth` | `services/dashboard_snapshot_test.go:60` | 四項數量、disabled null、7 天內到期、月底對應上月最後一天 |
| `TestConcurrentDashboardSnapshotsKeepOneObservation` | `services/dashboard_snapshot_concurrent_test.go:13` | 並行快照只保留一筆 |

`TestUseTokenAuthentication` 的子測試名稱：`legacy`、`bearer`、`bearer root`、`custom url priority`、`url priority`、`inactive url`、`expired url`、`invalid url`、`invalid bearer`、`missing`（`middlewares/auth_test.go:66-81`）。

### 10.2 `tests/` 前端測試

| 測試名稱 | 檔案 | 涵蓋行為 |
|---|---|---|
| `an older token chart response cannot replace a newer one` | `tests/chart_refresh_test.js:7` | 舊 token 圖表回應不覆蓋新回應 |
| `an older service chart response cannot replace a newer one` | `tests/chart_refresh_test.js:7` | 舊 service 圖表回應不覆蓋新回應 |
| `an older daily response cannot destroy the latest chart` | `tests/chart_refresh_test.js:36` | 舊 daily 回應不破壞最新圖表 |
| `an older token response cannot destroy the latest chart` | `tests/chart_refresh_test.js:36` | 舊 token 回應不破壞最新圖表 |
| `an older service response cannot destroy the latest chart` | `tests/chart_refresh_test.js:36` | 舊 service 回應不破壞最新圖表 |
| `UTC date ranges include only the requested calendar days` | `tests/chart_refresh_test.js:57` | UTC 日期範圍與 `filterStatsByDays` |
| `custom overview dates do not invalidate the detailed service chart` | `tests/dashboard_custom_range_compat_test.js:7` | 自訂總覽日期不影響 service 詳細圖表 |
| `custom overview dates do not invalidate the detailed token chart` | `tests/dashboard_custom_range_compat_test.js:7` | 自訂總覽日期不影響 token 詳細圖表 |
| `date ranges are inclusive and limited to 90 calendar days` | `tests/dashboard_date_test.js:6` | 日期查詢字串、反向、超過 90 天、無效日期 |
| `applying dates updates both panels and invalid input leaves the last range intact` | `tests/dashboard_date_test.js:16` | 套用日期更新兩個面板，錯誤輸入保留舊範圍 |
| `daily chart shows HTTP response classes as stacked bars` | `tests/dashboard_response_chart_test.js:6` | 每日回應圖表為堆疊長條，success/error 資料順序 |
| `dashboard search matches names and descriptions without exposing token values` | `tests/dashboard_search_test.js:6` | 搜尋使用者、服務、Token 備註，不搜尋 token 值 |
| `service response rate handles zero and formats real counts` | `tests/dashboard_service_rate_test.js:6` | 正常回應率格式化與零分母 |
| `token action opens only the requested positive token ID on the token page` | `tests/dashboard_token_action_test.js:6` | 只開啟正整數 token ID，並清除 sessionStorage |
| `monthly comparisons handle unavailable and zero baselines honestly` | `tests/dashboard_trend_test.js:6` | 無歷史、零基準、成長率與無法比較 |
| `trend colors distinguish growing expiry risk from other counts` | `tests/dashboard_trend_test.js:16` | 到期風險與一般數量的顏色語氣 |
| `editing a disabled service type retains its disabled status` | `tests/service_type_edit_test.js:6` | 編輯停用服務類型時不送 `is_active`，走 `/details` |

### 10.3 測試未涵蓋的區域

- 沒有 admin session 登入、登出、未登入 302 的自動化測試。
- 沒有完整路由表測試，例如 `/admin/*` 每個端點的方法與狀態碼。
- 沒有 proxy 的串流、SSE、multipart、上游重新導向、`Set-Cookie` Domain 移除、`Location` 保留、HEAD、body 轉送的測試。
- 沒有 900 年永久 token 邊界測試。
- 沒有 token 明文暴露、`Endpoint` 含 token 的測試。
- 沒有 Docker/Compose 啟動測試。

## 11. AGENTS.md 與 DESIGN.md 重點

### AGENTS.md

只有兩條 follow-up，沒有其他開發規範（`AGENTS.md:1-6`）：

1. 檢查並處理既有存取紀錄與伺服器日誌可能保存網址 Token 的問題，包含既有資料與相容性。
   - 對應風險：`Logger` 把完整 `URL.Path` 寫入 `AccessLog.Endpoint`（`middlewares/auth.go:210`）。
2. 檢查服務編輯送出空白描述時無法清除既有描述的問題，處理時保留既有介面相容性。
   - 對應行為：`UpdateService` 使用 `Updates(models.Service{Description: ...})`，GORM 對 struct 的零值欄位不更新，所以空字串無法清除描述（`controllers/service_controller.go:90-97`）。

### DESIGN.md

`DESIGN.md` 是視覺設計規範，不是後端契約（`DESIGN.md:1-53`）：

- 色彩：主色 `#276b51`、次色 `#173c2e`、中性色 `#f4f6f5`。
- 字體：`system-ui`，body 14px、字重 400、行高 1.6。
- 圓角：6px、8px。間距：8px、16px、32px。
- 版面：桌面深綠固定側欄、白色頂部工具列、流動主內容。儀表板先四項摘要，再並排請求趨勢與到期 Token，服務表格在下。窄螢幕導覽移到上方，表格可橫向捲動。
- 視覺：用細邊框分隔，避免無意義陰影與浮動效果。
- 元件：表格、篩選器、表單、狀態提示共用間距與字級。請求圖用綠色與珊瑚色區分 HTTP 回應類別，並標示統計範圍。載入、空資料、錯誤狀態必須明示。
- 規範：使用真實資料與明確時間範圍。不得把服務啟用狀態當健康檢查，不顯示無來源的趨勢。

## 未驗證項目

- 未執行任何 Go 測試、前端 Node 測試、服務啟動或 Docker Compose。所有狀態碼、body、時間行為都以原始碼靜態推導。
- 未檢視實際 SQLite schema，因此 FK constraint 是否存在、索引實際名稱與欄位順序標為不確定。
- HEAD 回應的 `Content-Length` 被覆寫為 0、`http.Client` 自動跟隨重新導向、請求 context 不取消、SSE 是否即時 flush，都是依程式與 Go 標準函式庫推導，未實測。
- 未渲染任何 template，未驗證版面、JavaScript 互動、瀏覽器端日期行為。
- 未驗證 `/use/svc/` 這類尾斜線路徑的實際 Gin 參數值與最終狀態碼。
- 未驗證舊資料庫的完整升級案例，只讀了 migration 測試，未執行。
- 未驗證 Docker 映像建置與 `infra-net` 外部網路。
- 未比對任何舊版 Project Memory，主 agent 表示沒有相關資料。

## 本輪經驗

- 情境：確認 `/api/*` 是否支援 Bearer token。
- 失敗做法：只看 `README.md:12` 會以為 `/use` 與 `/api` 都支援 Bearer，因為兩者都寫在同一段代理路徑說明。
- 已確認原因：`middlewares/auth.go:85` 用 `c.FullPath()` 是否以 `/use/` 開頭來決定要不要解析 `Authorization`，`/api/*path` 永遠不符合。`middlewares/auth_test.go:108-116` 也讓 `/api` 帶錯誤 Bearer 仍以 URL token 回 204。
- 有效做法：把 `api/routes.go` 的路由 group 與 `middlewares/auth.go` 的 `FullPath` 判斷一起讀，再用測試案例反證。
- 證據：`api/routes.go:169-174`、`middlewares/auth.go:85-90`、`middlewares/auth_test.go:108-116`。

---

## Objective
- 以唯讀方式整理 `/private/tmp/claude-501/-Volumes-SSD-Developer-hazelminx/e0449c93-a575-4a40-9ad7-b72e256f8907/scratchpad/refs/infra-manager` 的既有行為，產出可供新平台遵循的完整相容性契約。
- 報告須為繁體中文（台灣用語），涵蓋 11 個指定題目，每條附檔案路徑與行號，最後加「未驗證項目」與「本輪經驗」。

## Important Details
- 嚴格限制：不得修改任何檔案、不得 commit/stash/reset、不得派工給其他 agent 或 CLI、只讀。
- 驗證要求：中介層規則直接引用 `middlewares/auth.go`，路由清單與 `api/routes.go` 一致。
- 回報格式：每個問題一個標題、逐條列、標示不確定處、不得臆測、沒查到寫「未找到」。
- 專案為 Gin + GORM + SQLite，module 名稱 `infra-manager`，Go 1.27.3。
- 關鍵事實：Bearer 只在 `/use/` 解析，`/api/` 只支援 URL token；URL token 一旦被辨識就優先，不會回退 Bearer。
- 關鍵事實：Token 以明文儲存；管理員密碼用 bcrypt；AccessLog 的 `Endpoint` 會保存含網址 token 的完整 path。
- 關鍵事實：代理沒有應用層逾時、沒有請求 context、預設跟隨上游 redirect；`Authorization` 會原樣轉送。
- 關鍵事實：Compose 只傳 `GIN_MODE=release`，未傳 `ADMIN_USER`、`ADMIN_PASS`、`PORT`。
- 專案狀態：`git status --porcelain` 無輸出，HEAD `2ace566 fix: prevent stale management page assets`。

## Work State
### Completed
- 已完成並交付 11 節契約報告，涵蓋路由、Token 中介層、反向代理、資料模型、migrations、紀錄保留、儀表板時區、管理介面、設定部署、測試清單、AGENTS/DESIGN 摘要。
- 已逐條核對 `api/routes.go` 全部路由，包含 `/use/*path`、`/api/*path`、管理頁與 `/admin/*` API。
- 已直接引用 `middlewares/auth.go` 的 URL token 辨識、Bearer 解析、到期判定、失敗狀態碼與 body。
- 已整理 Go 測試 29 個頂層測試名稱與 `tests/*.js` 17 個前端測試名稱。
- 未修改任何檔案。

### Active
- (none)

### Blocked
- 未執行 `go test`、前端測試、服務啟動或 Docker，因任務要求只讀。
- 實際 SQLite schema 與 FK constraint 未檢視，標為不確定。
- HEAD `Content-Length` 被覆寫為 0、Go client 自動跟隨 redirect、SSE 即時 flush、`/use/svc/` 尾斜線行為皆為靜態推導，未實測。

## Next Move
1. (none) — 契約報告已完整交付，等待主 agent 核對。
2. (none)

## Relevant Files
- `api/routes.go`: 全部路由、中介層鏈、session 設定、cache 規則。
- `middlewares/auth.go`: Token 驗證、URL token/Bearer 優先順序、AdminAuth、AccessLog 寫入。
- `middlewares/failure.go`: RequestFailure 寫入、90 天與筆數保留、批次清理、欄位截斷。
- `services/proxy.go`: 反向代理、header/body/query 轉送、串流判定、錯誤分類。
- `models/models.go`: User、Service、Token、AccessLog、RequestFailure、Admin 欄位與關聯。
- `models/dashboard_snapshot.go`: DashboardSnapshot 欄位。
- `db/db.go`: SQLite 路徑 `data/infra_manager.db`、AutoMigrate、索引、預設管理員。
- `db/dashboard_migration.go`: migration 套用順序與條件。
- `db/migrations/20261028_service_type.sql`: `ALTER TABLE services ADD COLUMN type TEXT NOT NULL DEFAULT '';`
- `db/migrations/20261028_dashboard_snapshots.sql`: 建立 `dashboard_snapshots` 表。
- `services/dashboard_snapshot.go`: 台北日快照、UTC `ObservedAt`、90 天保留、上月比較。
- `controllers/dashboard_stats.go`: 台北日統計、最近 30 天服務使用量。
- `controllers/dashboard_date_range.go`: 日期範圍解析與 90 天限制。
- `controllers/dashboard_response_chart.go`: HTTP 100–399 成功、400 以上錯誤。
- `controllers/dashboard_service_detail.go`: 服務明細與 UTC `last_used`。
- `controllers/auth_controller.go`: 登入、登出、修改密碼。
- `controllers/token_controller.go`: Token 產生、預設 30 天、永久 1000 年。
- `controllers/service_details.go`: `/details` 保留停用狀態。
- `controllers/token_details_controller.go`: `/details` 保留 Token 狀態。
- `controllers/failure_controller.go`: 失敗紀錄查詢與分頁。
- `middlewares/auth_test.go`: Token 驗證與代理轉送的關鍵測試。
- `middlewares/failure_test.go`: 保留政策與截斷測試。
- `tests/*.js`: 前端圖表、日期、搜尋、Token 操作測試。
- `README.md`: 代理路徑與 Docker Compose 說明。
- `AGENTS.md`: 兩條 follow-up（日誌含網址 token、空白描述無法清除）。
- `DESIGN.md`: 視覺設計規範。

---

Continue if you have next steps, or stop and ask for clarification if you are unsure how to proceed.

---

沒有下一步，11 節契約報告已交付，工作目錄乾淨、HEAD 仍是 `2ace566`，未改任何檔案。  
剩下未做的是需要寫入或啟服務的實測（HEAD `Content-Length`、redirect、尾斜線），目前唯讀限制下不能跑。  
要我續驗的話，請解除唯讀並指定項目。

Skills：無