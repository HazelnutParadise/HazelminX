# hazelminx-core

HazelminX 的控制中心：管理 API、權限、工作協調、設定版本與嵌入式網頁介面。單一 Go 執行檔，資料放在 SQLite（純 Go 驅動，主機不需要 C 編譯器）。

## 建置與測試

```bash
go build ./cmd/hazelminx-core
go test ./...
```

嵌入前端：CI 先建置 `web/dist`，複製到 `core/web/dist`，再以 `-tags embedweb` 建置。沒有這個 tag 時，介面路徑回 `503 UI_NOT_EMBEDDED`，管理 API 照常可用。

## 執行

```bash
./hazelminx-core -listen 127.0.0.1:9800 -data-dir /var/lib/hazelminx
```

| 設定 | 旗標 | 環境變數 | 預設 |
|---|---|---|---|
| 管理 API 監聽位址 | `-listen` | `HAZELMINX_LISTEN_ADDR` | `127.0.0.1:9800` |
| Agent 通道監聽位址（TLS） | `-agent-listen` | `HAZELMINX_AGENT_LISTEN` | `0.0.0.0:9801` |
| 資料目錄 | `-data-dir` | `HAZELMINX_DATA_DIR` | `/var/lib/hazelminx` |
| 日誌格式 | `-log-format` | `HAZELMINX_LOG_FORMAT` | `json` |
| 使用者開啟面板的網址（Passkey 綁這個網域） | `-public-origin` | `HAZELMINX_PUBLIC_ORIGIN` | 空白，依每個請求的網址推算 |

端點：`GET /healthz`（含資料庫健康）、`GET /management/v1`（操作總覽：每個操作都連到 OpenAPI 裡的說明，另列已註冊的工作種類）、`GET /management/v1/openapi.json`（OpenAPI 3.1，由路由註冊表產生）、`GET /management/v1/agent-connections`（已連線的 Agent 與最近被拒絕的連線，含雙方協定範圍）。共用契約見下一節。

## 工作引擎

長操作一律建立工作（`POST /management/v1/jobs`，回 `202` 與工作 ID），工作不隨 HTTP 連線消失。每個工作種類向引擎註冊一個 `jobs.Handler`，自己定義步驟規劃、執行前再檢查、執行、復原、對帳與完成檢查；沒有註冊的種類不能提交，會回 `400 VALIDATION_FAILED`，`fields` 指出 `kind`。Agent 執行的種類目前只有 `node.inspect`（唯讀：問指定節點的 Agent 是誰，完成檢查是回答者的指紋等於工作指定的節點）。

- 狀態依規格 33.1，表格在 `internal/jobs/state.go`；終態不可再改。
- 同一 `requestId` 相同內容回同一工作，不同內容回 `409 REQUEST_ID_CONFLICT`。
- Core 自己的工作種類只有 `list.query`（背景列表查詢，見下一節），Agent 相關的種類隨票 004／007 進來。
- 資源鎖以裝置身分、掛載與正規化路徑比對，父子路徑與同磁碟都算衝突；鎖在 Core 重啟後由未完成工作重建。
- 取消：未開始的立即取消；執行中可取消步驟中斷，不可逆步驟做完才停，狀態 `cancel_requested` 期間步驟帶 `waitReason`。
- 失敗若所有已執行步驟都有復原方法則自動復原為 `rolled_back`；復原失敗為 `needs_attention`；執行端斷線或 Core 中途停止為 `unknown`，由 `Engine.Reconcile` 以執行端紀錄接續，不重跑不可逆步驟。
- 預算：每類並行上限（`jobs.Options.Budgets`）與資料目錄空間保留（`ReserveBytes`）。
- 工作端點掛在 IAM 驗證之後：讀取要全域 `platform:read`，提交與取消要全域 `platform:write`。需要較窄權限的工作種類各自開端點，例如背景列表查詢只要該列表的讀取權限。

## 管理 API 共用契約（票 010）

每個 `/management/v1` 端點都經 `Server.handle` 註冊一筆 `Operation`（`internal/api/operations.go`），總覽、OpenAPI 與實際路由都由同一份資料產生，`TestRoutesMatchOperations` 檢查三者一致。新資源端點照下列方式套用，不另寫一套：

- **錯誤**：代碼表在 `internal/api/errcodes.go`，每個代碼固定一個 HTTP 狀態、預設可否重試與白話說明。回應一律是同一個錯誤物件：`code`、`message`、`traceId`、`retryable`，視情況加 `requestId`、`jobId`、`resourceId`、`fields`（同步驗證沒過的欄位）、`details`（代碼專屬資料，例如 `currentVersion`、`mode`）、`evidence`、`suggestedActions`、`alsoBlockedBy`。用 `respond(c, newError(code, msg))` 回，不要自己寫狀態碼。
- **同時被多個理由擋下**：依 `blockingOrder` 回最前面的一個，其餘列在 `alsoBlockedBy`。順序是：未登入 → CSRF／來源 → 不支援的 dryRun → 不存在或看不到 → 無權 → 輸入錯誤 → 內容太大 → 缺版本條件 → requestId 重送判定 → 平台降級 → 節點或引擎降級 → 控制者衝突 → 資源唯讀 → 等待確認 → 版本不符 → 被引用 → 預檢沒過。前五項不透露資源是否存在。資源狀態擋下時，版本對了也不能寫，所以排在版本之前。
- **寫入閘門**：`ResourceState`（控制者、接管狀態、控制者衝突、所在節點或引擎的降級、待確認變更）交給 `WriteBlockers`，回 `RESOURCE_READ_ONLY`、`CONTROLLER_CONFLICT`、`SCOPE_DEGRADED`、`CONFIRMATION_REQUIRED`。增補 03「控制者與能不能寫入」表只寫在這個函式裡，畫面的停用按鈕也要問同一個函式。`Server.writeBlockers` 另外加上平台降級（`SetPlatformState`，偵測由票 017 接上）與 ROB-010 待確認變更。
- **列表**：回 `{items, page}`。`page` 有 `limit`（每頁筆數）、`queryCap`（查詢上限）、`timeoutSeconds`（逾時）、`nextCursor`、`total`（逾時時為 `null`，即總數未知）、`capped`（超過查詢上限，只能看前 `queryCap` 筆）、`timedOut`、`sort`，以及 `capped` 或 `timedOut` 時才有的 `backgroundQuery`。預設每頁 50、最多 200、查詢上限 10,000、逾時 15 秒。游標記住最後一筆的排序值，翻頁期間前面插進新資料也不會重複或漏筆。游標有伺服器簽章，改過的游標回 400。Core 重啟後舊游標失效，要從第一頁重新開始。查詢參數只接受 `limit`、`cursor`、`sort` 與該列表宣告的篩選，其他參數一律 400。宣告方式是一個 `ListSpec` 加一個 `ListSource`。SQL 的排序與「在這一筆之後」條件由 `internal/paging` 產生。
- **背景列表查詢**：把 `backgroundQuery` 加上 `requestId` 送到 `POST /management/v1/list-queries`，回 202 與工作。權限與該列表的讀取權限相同，執行時會重新確認提出者還有沒有權限。結果最多存 100,000 筆，放在 `list_query_items`，不放進工作紀錄。7 天後由每小時一次的清理刪除，之後讀取回 `410 LIST_QUERY_EXPIRED`。只有提出者看得到，而且提出者的可見範圍要和執行時相同，權限縮小或擴大後舊結果一律回 `404`。每個帳號最多同時保存 10 筆，超過回 `409 LIST_QUERY_LIMIT_REACHED`。
- **版本條件**：可修改的資源帶 `version`，GET 回 `ETag: "v<version>"`。寫入要帶 `If-Match: "v<version>"`。沒帶或帶 `*` 回 `428 VERSION_REQUIRED`，版本過期回 `412 VERSION_MISMATCH`（`details.currentVersion`）。成功的寫入回新的 `ETag`。目前示範在 `PATCH /management/v1/users/{id}`。角色指派與撤銷也會讓使用者版本加一，這兩個操作的 `If-Match` 可帶可不帶。
- **requestId**：工作的 requester 一律是登入的帳號。別的帳號用過的 requestId 回 `409 REQUEST_ID_CONFLICT`，不會把那個工作交出去。
- **長操作**：用 `acceptJob` 回 `202`、`Location` 與 `{jobId, job, links: {self, events, cancel}, deduplicated}`。
- **dryRun**：會改變受管資源的端點在 `Operation` 標 `DryRun: true`，收到 `?dryRun=true` 時回 `200 DryRunResult`（`ok`、`blockers`、`impact.direct`、`impact.indirect`、`preview`），不建立、不保留任何東西。權限照真實寫入檢查。沒標的寫入端點收到 `dryRun` 一律回 `400 DRY_RUN_UNSUPPORTED`，不會忽略參數照做。OpenAPI 以 `x-hazelminx-dry-run` 標記這些端點，作為 ROB-003 的核對清單。目前 `POST /management/v1/jobs` 支援，預覽會寫出是否視為重送、會排在哪些工作後面、空間夠不夠。
- **確認端點**：`GET`／`POST /management/v1/confirmations/{id}`（ROB-010）。只有套用變更的同一個帳號看得到、能確認（工作階段或 API Token 都可以），其他人一律 `404`。確認要從變更套用之後才建立的連線送出（`api.ConnContext` 記下每條連線的建立時間）。變更有指定新入口時，Host 或本機位址也要相符，否則回 `409 CONFIRMATION_NEW_CONNECTION_REQUIRED`。超過時間回 `409 CONFIRMATION_EXPIRED`。指定的本機位址是 `0.0.0.0` 這類萬用位址時只比對 port。只有平台資料庫唯讀時，確認會回 `503`，其他降級不擋確認。同一資源有待確認的變更時，其他寫入回 `409 CONFIRMATION_REQUIRED`。倒數、自動回復與重啟續算由票 024 接上。Core 前面若有反向代理，Core 看不到瀏覽器那段連線，這個判定就不成立。目前管理入口由瀏覽器直連 Core。
- **非 UTF-8 名稱**：主機來的名稱用 `RawName`：`text` 一律是有效 UTF-8（無效位元組換成 U+FFFD），原始內容不是有效 UTF-8 時另附 `bytes`（標準 base64）。兩個顯示相同的名稱以 `bytes` 區分。改名、下載等操作把整個 `RawName` 送回來，有 `bytes` 就以它為準。

## Agent 通道（票 007）

Agent 主動連到 `-agent-listen` 的 `/agent/v1/connect`（TLS 1.3 上的 WebSocket），節點不需要開任何入站連接埠。訊息格式、握手與版本規則在 `docs/contracts/README.md`，Go 型別在 `internal/protocol`。

- 第一次啟動時在機密服務建立節點 CA 與 Core 身分（`node-pki/ca`、`node-pki/core`，私鑰加密存放）。節點憑證由 `nodes.PKI.IssueNodeCertificate` 簽發，註冊流程（票 019）會用它；Agent 要釘選的 CA 是 `PKI.CACertificatePEM()`。
- 每次連線雙方都要證明身分：Core 驗節點憑證鏈、用途、指紋，以及節點私鑰對握手內容的簽章；直連時 TLS 用戶端憑證必須是同一節點。Agent 驗 Core 憑證鏈、`core.hazelminx.internal` 名稱與 Core 的簽章，不看 IP 或主機名。HTTP Header 一律不當身分，所以經會終止 TLS 的代理時仍有獨立的節點身分證明。握手同時以 X25519 交換金鑰，之後每則訊息帶序號與 HMAC，代理插入、重放或調換訊息都會讓會話斷線。
- 握手訊息上限 64 KiB、hello 要在 3 秒內送到、同時握手上限 256 條且同一來源 32 條（滿了先關還沒送 hello 的、最後才關已出示有效憑證的），被拒紀錄只留 Agent 提供文字的前 256 字元。
- 協定版本沒有交集時拒絕連線並記錄雙方範圍；同一節點已有連線時拒絕第二條（複製的虛擬機）；`Authorizer` 回 `ErrNodeRevoked` 時拒絕（撤銷清單在票 019）。
- 工作步驟經 `nodes.StepRunner` 送到節點：Agent 沒宣告的能力不送；節點不在線是已知失敗（沒送出任何東西）；送出後斷線是 `unknown`。Agent 重連後，`nodes.Reconciler` 在會話存活期間每 15 秒掃一次該節點「狀態不明」的工作，先向 Agent 查持久紀錄再交給引擎對帳，已完成的步驟不重跑。

## 高權限執行器的授權憑證與主機變更帳本副本（票 012、113）

- 主機上的高權限執行器每個請求都要一張 Core 簽的授權憑證。簽發金鑰（Ed25519）第一次啟動時建在機密服務（`privileged-grants/issuer`），簽發由 `internal/grants` 處理。公鑰每次啟動都寫到資料目錄的 `privileged-issuer.pub`，安裝器與節點註冊（票 013、019）把它放到主機的 `/etc/hazelminx/privileged/issuer.pub`。
- Core 為每台主機保存一份主機變更帳本（規格增補 07 第 55.1 節）。節點連上時馬上同步一次，連線期間每 5 分鐘再同步一次（`internal/hostledger`）。每一頁都簽一張只有 `host.ledger.read`、有效 5 分鐘的憑證，經通道送 `ledger_pull`，收到的行驗證雜湊鏈後才存進 `host_ledger_lines`（migration 0008）。這張表只能新增，資料庫觸發器擋下修改與刪除。
- `host_ledgers` 記錄每台主機副本跟的是哪一本帳本（`ledger_id`）、副本到哪一行、上次成功同步的時間，以及最近一次失敗的原因。失敗代碼：
  - Agent 回的 `EXECUTOR_UNREACHABLE`、`EXECUTOR_REFUSED`、`EXPORT_FAILED`。主機帳本比 Core 副本短時，執行器會回 `EXPORT_FAILED`。
  - `CHAIN_BROKEN`：這一頁接不上副本，或有一行被改過。
  - `LEDGER_REPLACED`：主機的帳本被重建。這時不保存新帳本的任何一行，等人處理。
  - `AGENT_UNSUPPORTED`：Agent 太舊，不支援 `host.ledger`。
  - `NO_ANSWER`：等不到回應。
  - `STORE_FAILED`：寫入資料庫失敗。

  節點離線是正常狀態，不記成失敗。
- 目前還沒有讀取副本的管理 API，也還沒有畫面。完全移除（票 037）用 `hostledger.Store.Lines` 讀副本。

## 登入、第二驗證與權限（票 009、014）

- 沒有任何帳號時，啟動會把一次性設定碼印到 **stdout**（不進日誌），一小時內用 `POST /management/v1/setup` 建立擁有者；之後這個端點回 `409 SETUP_DONE`。建立成功後擁有者直接登入，回應與登入成功相同。
- `POST /management/v1/auth/login` 以 argon2id 驗證密碼。帳號沒有第二驗證時，回 `{"step":"signed-in", …}`，設 `hazelminx_session`（HttpOnly、SameSite=Strict）與 `hazelminx_csrf` Cookie，並回 `csrfToken`。帳號已綁 TOTP 或 Passkey 時，回 `{"step":"second-factor","challengeToken":…,"methods":[…]}`，這時還不設 Cookie，要再用 `POST /management/v1/auth/login/second-factor` 送出驗證器 App 的 6 位數、Passkey 斷言或復原碼後才登入（Passkey 先用 `auth/login/passkey-options` 取選項）。挑戰 5 分鐘內有效、只能用一次，答錯 5 次就作廢。
- 寫入類請求必須帶 `X-CSRF-Token`，且 `Origin`／`Referer` 須是本站。
- `/management/v1` 下除了 `setup` 與 `auth/login*`，沒有工作階段一律 `401`。角色表在 `GET /management/v1/roles`；沒有對應授權的呼叫回 `403`，猜到別人範圍的資源 ID 一律回 `404`（與不存在的資源回應完全相同）。
- 登入失敗依帳號與來源分別限制：帳號冷卻最長 15 分鐘，來源 15 分鐘內錯 20 次就封鎖 15 分鐘。曾成功登入該帳號的來源不受帳號冷卻影響，攻擊者因此無法永久鎖死管理員。不存在的帳號也套同一規則，不洩漏帳號存在與否。第二步與再次驗證答錯另外按帳號計數，從曾登入過的來源也一樣算，因為經過反向代理時所有人的來源位址可能相同。
- 擁有者、平台管理員與主機維運者沒有第二驗證時，只能查看自己、綁定 TOTP 或 Passkey、管理自己的工作階段與登出，其他請求（包含讀取）回 `403 SECOND_FACTOR_ENROLLMENT_REQUIRED`。這些角色不能移除最後一個第二驗證（`409 LAST_SECOND_FACTOR`）。
- 綁定第一個 TOTP 或 Passkey 時會發 10 組復原碼，只顯示這一次。資料庫只存以機密庫金鑰計算的 HMAC 值，用過一次就失效。TOTP 種子放在機密庫，接受前後各 30 秒的時間誤差，同一組碼只能用一次。
- 新增或移除 TOTP、Passkey，以及重新產生復原碼，都要在 10 分鐘內登入過或用 `POST /management/v1/auth/reauthenticate` 再驗證一次（密碼，加上已綁的第二驗證），否則回 `403 REAUTHENTICATION_REQUIRED`。新增第一個第二驗證時，同一使用者沒有通過第二驗證的其他工作階段會登出。移除第二驗證時，其他工作階段全部登出。
- Passkey 綁在建立時的網域。用 IP 開面板時不能使用 Passkey。改管理網域前，可用 `GET /management/v1/auth/passkeys/domain-impact?domain=新網域`（需要全域 `platform:write`）查看哪些 Passkey 會失效，以及哪些帳號會因此沒有第二驗證。
- 使用者在 `GET /management/v1/me/sessions` 看自己的工作階段，用 `DELETE /me/sessions/{id}` 或 `POST /me/sessions/revoke-others` 撤銷，下一個請求就會被拒。閒置逾時在 `PUT /me/session-settings` 設定，可設 15 分鐘到 24 小時，絕對上限 7 天。改短時，其他閒置中的工作階段也一起縮短。伺服器每次延長期限都會重送 Cookie。工作不綁工作階段，登入過期或登出後工作照常執行，重新登入可查結果。
- 每次登入、建帳號、指派或撤銷角色、拒絕存取都寫入 `audit_events`（`GET /management/v1/audit-events`，只回有 `audit:read` 的範圍）。寫入前會遮罩 `password`、`token`、`secret`、`key` 這類鍵。
- 每個 `/management/v1` 回應都帶 `X-Trace-ID`（呼叫端有送就沿用，沒送就產生），錯誤內容與稽核事件以 `traceId` 記錄同一個值。`requestId` 只用來指使用者操作的請求識別碼，也就是工作重送去重用的那個。
- `X-Forwarded-For` 預設不信任（`api.IAMOptions.TrustedProxies` 為空），來源 IP 取連線位址；接 Zentinel 後由票 065 設定可信代理。

## 目錄

| 路徑 | 內容 |
|---|---|
| `cmd/hazelminx-core` | 進入點：設定、儲存、HTTP、優雅關閉 |
| `internal/config` | 旗標與環境變數載入、驗證 |
| `internal/store` | SQLite 開啟、WAL、`synchronous=FULL`、開檔前 `quick_check`、只升不降的 migration（`migrations/NNNN_name.sql`） |
| `internal/api` | Gin 路由與管理 API 共用契約（錯誤代碼表、列表、版本條件、dryRun、202 工作回應、OpenAPI）、健康檢查、總覽、工作 API、背景列表查詢、確認端點、介面靜態檔；`iam_routes.go` 是工作階段、CSRF 與授權中介層（`RequireGlobal`、`RequireScoped`）與 IAM 端點，`mfa_routes.go` 是第二驗證、再次驗證、自己的工作階段與強制綁定白名單 |
| `internal/paging` | 列表的 keyset 分頁：把排序與游標換成 SQL 的 ORDER BY 與「在這一筆之後」條件 |
| `internal/confirm` | ROB-010 確認紀錄：建立、查詢、確認（新連線與入口判定）、標記已回復、查同一資源是否有待確認變更 |
| `internal/jobs` | 工作引擎：狀態機、冪等鍵、資源鎖、預算、事件序號、診斷、SQLite 儲存 |
| `internal/protocol` | Core↔Agent 訊息型別、嚴格與寬鬆解碼、版本協商、握手簽章內容；`fixturegen/` 重產 `docs/contracts/fixtures` 的確定性樣本 |
| `internal/nodes` | 節點 PKI、Agent 連線 hub（握手、會話、心跳）、步驟執行器、重連對帳、`node.inspect` 工作種類、向 Agent 要帳本頁（`Hub.PullLedger`） |
| `internal/grants` | 高權限執行器授權憑證的簽發金鑰與簽發（與 Rust 端用同一份樣本對照） |
| `internal/hostledger` | 主機變更帳本的 Core 副本：驗證雜湊鏈、保存、依節點同步 |
| `internal/iam` | 帳號、角色與資源範圍、密碼雜湊、工作階段與閒置逾時、TOTP、Passkey、復原碼、再次驗證、預設拒絕的授權判定、稽核紀錄、登入嘗試限制 |
| `internal/secrets` | 機密參照（加密值、版本、使用範圍、到期、變更紀錄）、主金鑰檔與輪替、Token 雜湊查驗 |
| `web` | 嵌入式前端（`embedweb` build tag） |

## 資料目錄與復原狀態

| 檔案 | 內容 | 權限 |
|---|---|---|
| `core.db`（含 `-wal`、`-shm`） | 平台資料庫 | 0600 |
| `master.key` | 主金鑰檔：各世代金鑰、目前作用中的世代、復原材料已取得的確認。不在資料庫內。 | 0600 |
| `privileged-issuer.pub` | 授權憑證簽發金鑰的公鑰（SPKI PEM），每次啟動重寫。公開資訊，主機的執行器信任它。 | 0644 |

啟動時依序檢查，任一不過就以非零結束並說明原因，不會建立空白資料庫冒充全新安裝（DAT-002、REL-006、T-009、T-012）：

- `master.key` 存在但 `core.db` 不在：資料庫遺失，進復原狀態。
- `core.db` 無法通過 `PRAGMA quick_check`：資料庫損壞，檔案原樣保留。
- schema 版本比執行檔新：拒絕開啟。
- `master.key` 可被擁有者以外讀取、或無法解析：拒絕啟動。
- 資料庫裡有密文引用主金鑰檔沒有的世代：拒絕啟動並列出缺少的世代（舊備份搭配新主金鑰檔時會在這裡被攔下，而不是還原時才發現）。

主金鑰輪替（`secrets.Service.RotateMasterKey`）分三步：新世代先寫進 `master.key` 標為 pending → 一個交易內把所有密文重新包裝並用新舊兩把金鑰驗證 → `master.key` 把新世代設為作用中、舊世代標為 retired。舊世代永遠保留在檔案裡，所以輪替前的備份仍可解密。中途失敗時交易回滾、舊世代維持作用中；中途斷電由下次啟動依資料庫實際引用的世代收尾。
