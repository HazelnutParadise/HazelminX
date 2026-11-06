> 來源：1Panel v2.4.1（commit f9f9169）唯讀原始碼盤點，2026-11-07，由派工 agent（opencode / big-pickle）產出，主 agent 核對過 Q1、Q4、Q9 與 Q11 的關鍵結論。報告是線索，實作前以固定 commit 重新核對。

# 1Panel OSS v2.4.1 後端架構盤點

## Q1. 整體架構與 core/agent 通訊方式

- 系統拆成兩個 Go 程式：`core`（面板 API／認證／設定／panel 自身備份）與 `agent`（宿主機操作／網站／容器／資料庫／AI）。
- `core` 以 Unix domain socket 反向代理到 `agent`：`core/init/proxy/proxy.go:16` 定義 `SockPath = "/etc/1panel/agent.sock"`，`Init()` 用 `net.Dialer.DialContext(ctx, "unix", SockPath)`（`proxy.go:22-27`），再包成 `httputil.ReverseProxy`（`proxy.go:36-39`）。
- 代理失敗回 502：`proxy.go:60` 寫入 `Bad Gateway: ...`。
- 轉發判斷在 `core/middleware/helper.go:9-17` `ShouldProxyToAgent()`：`/api/v2` 開頭且不是 `/api/v2/core`（`/api/v2/core/xpack` 例外）就轉給 agent；`/1panel/swagger` 與非 `/api/v2` 留在 core。
- 目標節點由 query `operateNode` 或 header `CurrentNode` 決定（`core/init/router/proxy.go`）。
- `agent` 在 master 模式自己監聽同一個 socket：`agent/server/server.go:37-39`，目錄權限 `0700`、socket 權限 `0600`（`server.go:45-56`、`140-153`）。
- 多節點時 `agent/middleware/certificate.go` 會驗證 `Proxy-Id` header 與 `/etc/1panel/.nodeProxyID` 內容；`Connection: Upgrade`（WebSocket）直接放行。
- 重點：**OSS 版 core 會把幾乎整個 `/api/v2` 都代理給 agent**。新平台若只把 agent 換成 Rust，必須完整重刻這 691 條 agent 路由，不能只做「宿主機操作」那幾條。

## Q2. agent 的 HTTP 路由與端點

- 掛載點：`agent/init/router/router.go:20-26`，前綴 `/api/v2`，依序掛 `Certificate()`（非 master）與 `OperationResolveMeta()`，最後加 `GET /api/v2/health/check`。
- 路由介面：`agent/router/ro_router.go` 只有 `CommonRouter` 介面；實際群組在 `agent/router/common.go` 註冊 24 個 Router。
- core 端：`core/init/router/router.go` 掛 `/api/v2/core`，群組在 `core/router/common.go`，共 8 個 Router。
- 端點數量（以 `grep '\.GET\|\.POST'` 計）：agent 約 691 條、core 約 72 條。

| 進程 | 群組前綴 | 主要檔案 |
|---|---|---|
| core | `auth`, `backups`, `commands`, `groups`, `logs`, `script`, `settings`, `hosts/diagnostics` | `core/router/ro_*.go` |
| agent | `dashboard`, `hosts`, `hosts/diagnostics`, `containers`, `logs`, `files`, `toolbox`, `cronjobs`, `backups`, `settings`, `apps`, `websites`, `websites/dns`, `websites/acme`, `websites/ssl`, `websites/templates`, `websites/ca`, `databases`, `openresty`, `runtimes`, `process`, `ai`, `groups`, `alert`, `/share` | `agent/router/ro_*.go` |

- 端點密度最高：`ro_ai.go` 106、`ro_host.go` 78、`ro_container.go` 73、`ro_website.go` 68、`ro_database.go` 63、`ro_file.go` 60。
- core 專屬端點：登入／MFA／passkey／API key（`ro_base.go:18-46`）、設定與 SSL 重載（`ro_setting.go:17-52`）、指令庫（`ro_script_library.go:17-22`）、panel 備份（`ro_backup.go:17-21`）、日誌（`ro_log.go:18-20`）、診斷（`ro_runtime_diagnostics.go:17-19`）。
- agent 專屬端點：應用商店（`ro_app.go`）、網站與 SSL（`ro_website*.go`）、容器（`ro_container.go`）、主機（`ro_host.go`）、資料庫（`ro_database.go`）、AI（`ro_ai.go`）。

## Q3. 認證與授權

- Session 認證：`core/middleware/session.go` `SessionAuth()`，讀 `API_AUTH`、`LOCAL_REQUEST`、匿名白名單、`psession`，並用 `SessionTimeout` 設定刷新。
- API Key：`core/app/auth/api_auth.go`，支援 MD5 與 HMAC-SHA256、時間戳驗證、IP 白名單、trusted proxy 正規化（`GenerateMD5`、`GenerateHMACSHA256`、`IsValid1PanelTokenWithVersion`）。
- API Key 管理：`core/app/auth/api_key.go`、`api_key_panel.go`、`api_key_legacy_lock.go`，含舊 key 遷移與鎖定（`core/utils/apikey_migration/owner.go`、`apikey_audit/audit.go`）。
- MFA：`core/utils/mfa/mfa.go`，TOTP（依賴 `github.com/xlzd/gotp`）。
- Passkey：`core/utils/passkey/*`，WebAuthn（`github.com/go-webauthn/webauthn`）。
- 其他認證能力：core `go.mod` 含 OIDC（`coreos/go-oidc`）、SAML（`crewjam/saml`）、LDAP（`go-ldap/ldap`）。
- 入口防護：`core/init/router/router.go` 掛 `WhiteAllow`、`BindDomain`、`CSRFTokenGuard`、`PasswordExpired`、`GlobalLoading`，並用 `security.CheckSecurity` 控制安全入口。
- 授權擴充點：`core/utils/xpack/providers/auth.go` 的 `AuthProvider` 介面（Login、MFALogin、GenerateApiKey、Passkey*、CoreAPIAuthMiddleware、CoreRBACMiddlewares 等）；OSS 實作在 `core/utils/xpack/helper/auth_helper.go`。

## Q4. 資料庫與 migration

- ORM：GORM v1.31.2；SQLite driver 是 `github.com/glebarez/sqlite`（純 Go）。
- 連線：`core/utils/common/sqlite.go` `LoadDBConnByPath` / `GetDBWithPath`，目錄 `0700`、DB 檔 `0600`。
- core DB：`core/init/db/db.go`，`core.db`、`task.db`、`agent.db`、`alert.db`，路徑 `<installDir>/1panel/db/`。
- agent DB：`agent/init/db/db.go`，`agent.db`、`task.db`、`monitor.db`、`gpu_monitor.db`、`vllm_monitor.db`、`alert.db`，可選讀 `core.db`。
- Migration：`github.com/go-gormigrate/gormigrate/v2`。core 入口 `core/init/migration/migrate.go`，agent 入口 `agent/init/migration/migrate.go`（`InitAgentDB`／`InitTaskDB`／`InitAlertDB`）。
- Migration 檔：`core/init/migration/migrations/{api_key,init,vm}.go` 與 `helper/{alert,menu}.go`；agent 為 `migrations/{agents_native,firewall_whitelist,init}.go`。
- Model：core 12 檔（含 `Setting`、`APIKey`、`Task`、`BackupAccount`、`WebsiteSSL`、`WebsiteCA`）；agent 46 檔（含 `Website`、`AppInstall`、`Cronjob`、`Database*`、`WebsiteSSL` 等）。

## Q5. 應用商店安裝

- API：`agent/app/api/v2/{app.go,app_install.go,app_ignore_upgrade.go}`；service：`agent/app/service/{app.go,app_install.go,app_upgrade.go,app_utils.go,app_sync_task.go}`。
- 安裝入口 `AppService.Install` 走 `installWithHooks`（`app.go:367/371`）：檢查 Docker network、名稱唯一、vLLM 版本、資料庫 key、`PANEL_DB_HOST`。
- 來源：`agent/global/global.go` `AppRepoURL()`，企業／非 intl 用 `https://apps-assets.fit2cloud.com`，intl 用 `https://apps.1panel.pro`。
- compose 下載：`app.go:451` `%s/%s/1panel/%s/%s/docker-compose.yml`；清單 `1panel.json.zip`、版本 `1panel.json.version.txt`（`app.go:879/951`）。
- 落地目錄：`agent/init/dir/dir.go`，資源 `<base>/1panel/resource/apps`、安裝 `<base>/1panel/apps`，另有 local／remote／custom 資源目錄。
- 安裝流程（`app_utils.go` 約 1000-1040）：複製資源目錄到安裝目錄、`.env` 先讀舊值再 `handleMap` 合併後寫入、compose 寫入 `GetComposePath()`、執行 `scripts/{init,upgrade,uninstall,start,stop,restart}.sh`（timeout 10 分鐘）。
- 升級保留自訂參數：`app_upgrade.go` `renderUpgradeEnv` 合併舊 `.env` 與新 `Env`，特例 `openlist`、`openresty`／`WEBSITE_DIR`；再 `compose.Down`／`Up`。

## Q6. OpenResty／網站／SSL

- Nginx 設定用自家 AST 套件：`agent/utils/nginx/{dumper.go, components/*, parser/*}`，可解析、修改、輸出 `nginx.conf`。
- 站點設定分兩類（`agent/app/service/website_utils.go:1733-1748`）：
  - 一般網站：`<www>/conf.d/<alias>.conf`
  - Stream：`<www>/stream.d/<alias>.conf`
- Stream 預設模板：`nginx_conf.GetWebsiteFile("stream_default.conf")`（`website_utils.go:490`）；Stream 設定寫入 `StreamConf`（`website_utils.go:524/778/1233`）。
- 套用與重載：`agent/app/service/nginx_utils.go:241-264`，先 `docker exec <container> nginx -t`，再 `nginx -s reload`；失敗會把舊內容寫回（rollback）。
- 站點操作統一呼叫 `opNginx`（`website.go:599`、`website_proxy.go:396`、`website_lb.go:214/245`、`website_ssl.go:608/1098`）。
- 反向代理與負載平衡：`website_proxy.go`（`OperateProxy`、`GetProxies`、`UpdateProxyFile`）、`website_lb.go`（`GetLoadBalances`、`CreateLoadBalance`、`UpdateLoadBalance`）。
- SSL：`agent/utils/ssl/{acme.go,client.go,manual_client.go,dns_provider.go}`，用 lego v5（`go.mod` replace 成 `1Panel-dev/lego`）；支援 Let's Encrypt、ZeroSSL、BuyPass、Google、FreeSSL、自訂 CA、DNS-01／HTTP-01、EAB。
- 系統 SSL 重載：`website_ssl.go:352/418` `reloadSystemSSL` → `POST /api/v2/core/settings/ssl/reload`；nginx include `/usr/local/openresty/nginx/conf/ssl/root_ssl.conf`（`nginx.go:425/453`）。
- PHP runtime：`agent/app/service/runtime.go`，PHP 擴充安裝／移除、`php.ini` 讀寫、`php -m` 偵測、FPM 設定。
- 注意：Stream 代理目前靠 `stream.d` + Nginx `stream {}` 區塊，非獨立 TCP proxy 服務。

## Q7. 宿主機操作（exec 入口與規模）

- 統一命令入口：`agent/utils/cmd/cmdx.go`
  - `NewCommandMgr(opts...)`（`:80`）
  - `Run`（`:97`）、`RunWithStdout`（`:107`）、`RunWithOptionalSudo`（`:102`）
  - `RunPipe`／`RunPipeToFile`（`:116/180`）
  - `RunDockerExec`／`RunDockerExecWithStdout`（`:88/92`）
  - `commandStartDiagnostics`、timeout、logger、task、workdir、stdin、env 等 Option。
- Sudo 包裝：`agent/utils/cmd/cmd.go:33-65`（`SudoHandleCmd`、`WrapWithOptionalSudo`、`ExecCommandWithOptionalSudo`、`WriteFileWithOptionalSudo`）。
- 使用規模：40 個 `agent/app/service/*.go` 直接 import `utils/cmd`；service 層約 127 處 `Run*` 呼叫。
- Docker exec 形式呼叫約 54 處（`docker exec`、`RunDockerExec`、`docker compose`）。
- Service 管理：`agent/utils/controller/controller.go`，自動偵測 `systemctl`／`rc-service`／`service`（`:27-33`），提供 `HandleStart/Stop/Restart`、`CheckExist/Active/Enable`、`Reload`、`GetServicePath`。
- 防火牆：`agent/utils/firewall/*`
  - `iptables_helper/*`、`nftables_helper/*`、`forwarding/*`、`filter/*`、`docker_guard/*`
  - 支援 iptables、nftables、ufw、firewalld（grep 出現 21／16／9／2 個檔案）
- 檔案操作：`agent/utils/files/file_op.go`，`NewFileOp()` 提供 `GetContent/WriteFile/SaveFile/ChmodR/ChownR/Copy/Mv/Cut/DownloadFileWithProcess/RmRf` 等；另有 zip／tar／7z／rar。
- 磁碟：`agent/app/service/disk.go` 直接用 `lsblk`、`parted`、`partprobe`、`mount`、`umount`；`disk_utils.go` 有 `mkfs`、`/etc/fstab` 讀寫。
- 程序：`agent/app/service/process.go` 用 gopsutil（`process.NewProcess(...).Kill()`、`net.ConnectionsMaxWithContext`）。
- SSH：`agent/utils/ssh/ssh.go`，用 `golang.org/x/crypto/ssh` 建 client。
- 套件管理：**未找到** apt／dnf／yum 直接呼叫；套件安裝主要走 Docker image 或應用商店腳本。
- 這是 Rust Agent 的主要替換面：命令執行、sudo、systemd、防火牆、檔案、磁碟、程序、SSH、Docker CLI。

## Q8. 備份 adapter 與 cron

- 雲端儲存 client：`agent/utils/cloud_storage/client/` 共 12 種：
  - `local`、`s3`、`oss`、`sftp`、`webdav`、`minio`、`cos`、`kodo`、`onedrive`、`up`（UpYun）、`ali`（阿里雲）、`google_drive`
- 工廠：`agent/utils/cloud_storage/cloud_storage_client.go:22` `NewCloudStorageClient`，另有帶 context 版本（`:53`）。
- 備份 service：`agent/app/service/backup.go` 為主，另有 `backup_app.go`、`backup_compose.go`、`backup_container.go`、`backup_mongodb.go`、`backup_mysql.go`、`backup_postgresql.go`、`backup_redis.go`、`backup_runtime.go`、`backup_website.go`、`backup_record.go`、`cronjob_backup.go`。
- 多帳號上傳與重試：`backup.go:471/475` `uploadWithMap`，支援多帳號、下載帳號、retry、失敗清理。
- 加密：`newClient`／`newClientWithContext`（`backup.go:577/581`）可帶 `isEncrypt`。
- Cron（`agent/cron/cron.go`）：
  - 網站每日：`@daily`（`:64`）
  - SSL 每 6 小時：`0 */6 * * *`（`:67`）
  - 應用商店：隨機分鐘／小時，每日一次（`:80`）
  - 備份 token 刷新：`0 3 */31 * *`（`:83`）
  - 描述清理：`@every 240h`（`:86`）
  - 監控：Monitor、GPU Monitor、vLLM Monitor 依 DB 設定啟動
  - 使用者自訂 cronjob：從 `cronjobs` 表載入並 `StartJob`
- 啟動前會做 NTP 對時（`syncBeforeStart`）。

## Q9. Docker 與 compose

- Docker SDK：`github.com/docker/docker v28.5.2`、`github.com/docker/cli v29.9.0`、`github.com/docker/go-connections`。
- Client 建立：`agent/utils/docker/docker.go:28` `NewDockerClient()`，從設定 `DockerSockPath` 讀 socket，預設 `unix:///var/run/docker.sock`。
- 能力：list containers、create/delete network、pull/push/build image、image exists/id、registry auth（`docker.go:71-426`）。
- Compose 執行：**呼叫外部 CLI**，不是 library 執行。
  - `agent/utils/compose/compose.go:21-40` `checkCmd` / `getComposeBaseCmd`
  - `agent/utils/common/common.go:438` `GetDockerComposeCommand()`，優先 `docker compose`，再 `docker-compose`
  - 動作：`Up`、`UpWithoutBuild`、`Down`、`Stop`、`Restart`、`Operate`、`DownAndUp`、`PullComposeImages`
- Compose 解析：`compose-spec/compose-go/v2`，`agent/utils/docker/compose.go:20` `GetComposeProject()`。
- Podman／Kubernetes：**未支援**。只有 `agent/app/service/dashboard.go:482` 用字串判斷 `podman`／`containerd` 來統計容器，沒有 client 或操作實作。

## Q10. AI／MCP／Ollama／vLLM

- Ollama：`agent/app/service/ai.go` 提供 model search／detail／create／close／recreate／delete／sync、domain bind／get／update。
- vLLM：`agent/app/service/monitor_vllm.go`、`vllm_upgrade.go`；監控資料在 `agent/utils/ai_tools/vllm/*`。
- GPU／加速器：`agent/utils/ai_tools/{gpu,npu,xpu,accelerator}/*`，支援 NVIDIA、AMD、NPU、XPU。
- MCP：`agent/app/service/mcp_server.go`
  - CRUD、啟停、狀態同步、連線測試、domain bind
  - 用 `supergateway` 包 stdio MCP server（`buildSupergatewayCommand`，`:789`）
  - 產生 compose 並透過 Docker 啟動
- AI Agents：`agent/app/service/ai.go` 與 `agents*.go`（accounts、models、skills、channels、pairing、Hermes chat）。
- TensorRT-LLM：`agent/app/service/ai/` 與 `ro_ai.go` 的 `/tensorrt/*`。
- AI Gateway：`ai_gateway_upgrade.go`、`ai_gateway_recover.go`。
- 終端 AI：`agent/utils/terminal/ai/*`，命令生成、風險命令判斷、AI 輸入攔截。

## Q11. xpack 判斷與空實作

- Build tag：`//go:build !xpack && !enterprise`，OSS 版走 community 實作。
- 進入點：
  - `core/utils/xpack/community.go`：`AuthProvider = helper.NewIAuthProvider()`、`MultiNodeProvider = helper.NewIMultiNodeProvider()`
  - `agent/utils/xpack/community.go`：`AlertProvider = helper.NewIAlertProvider()`、`MultiNodeProvider = helper.NewIMultiNodeProvider()`
- Interface：
  - `core/utils/xpack/providers/auth.go`：Login、MFALogin、Logout、MFA、Passkey、API key、RBAC、Password expiration 等
  - `core/utils/xpack/providers/multi_node.go`：Proxy、ProxyDocker、UpdateGroup、LoadNodeInfo、Sync、AutoUpgradeWithMaster 等
  - `agent/utils/xpack/providers/alert.go`：SMS／Webhook／custom webhook alert
- OSS 行為：
  - `agent/utils/xpack/helper/multi_node.go:47` `IsXpack() false`
  - `StartClam` 回 `ErrXpackNotFound`（`:27`）
  - `LoadNodeInfo` 設 `global.IsMaster = true`、scope `master`
  - `GetImagePrefix()` 回空字串、`IsUseCustomApp() false`
  - alert helper 多數回 `ErrXpackNotFound`／`ErrCustomWebhookUnsupported`
- `core/utils/xpack/helper/auth_helper.go` 不是空實作，OSS 本機登入、MFA、API key、`CoreAPIAuthMiddleware` 都在這裡；`CoreRBACMiddlewares()` 回 `nil`。
- 重寫時只要保留這些介面，企業版可替換實作；OSS 版把 multi-node／alert 當 no-op 即可。

## Q12. License 與依賴

- License：GPLv3（根目錄 `LICENSE`）。
- 檔頭：**0 個** Go 檔含 `Copyright` 或 `SPDX-License` 標頭，只有 build tag。
- core 主要依賴：gin、gorm + glebarez/sqlite、go-gormigrate、gopsutil、resty、websocket、webauthn、go-oidc、SAML、LDAP、maroto（PDF）、cobra、viper、swaggo、robfig/cron。
- agent 主要依賴：gin、gorm + glebarez/sqlite、docker SDK、docker/cli、compose-go、lego v5（replace 成 1Panel fork）、gopsutil、resty、websocket、pty、archiver、sftp、minio、aws sdk、aliyun oss、qiniu、tencent cos、upyun、mongo driver、mysql／pgx／redis、gotenv、ini、yaml。
- Go 版本：兩邊都是 `go 1.27.3`。

## Q13. 可直接重用 vs 難以搬移

可直接重用（Go Core 幾乎可照搬）：
1. 認證／授權：Session、API key、MFA、Passkey、安全入口、CSRF、IP 白名單（`core/app/auth`、`core/middleware`）。
2. DB 與 migration：GORM + SQLite + gormigrate 模式可直接沿用（`core/init/db`、`agent/init/db`、`*/migration`）。
3. 應用商店業務：版本解析、`.env` 合併、升級保留參數、腳本 hook（`agent/app/service/app*.go`）。
4. 備份抽象：`CloudStorageClient` 介面與 12 個 adapter 可保留（`agent/utils/cloud_storage`）。
5. xpack 介面：Auth／MultiNode／Alert provider 邊界清楚，適合當企業版擴充點。

難以搬移（Rust Agent 要重寫或重新設計）：
1. 命令與 sudo 層：`utils/cmd` 的 timeout、pipe、task log、sudo、process group kill，散落在 40 個 service、127 處呼叫。
2. Docker／compose 生命週期：SDK 與外部 CLI 混用，`docker compose` 偵測、compose-go 解析、image pull 進度、網路與 volume 操作。
3. Nginx 設定 AST：`utils/nginx` 的 parser／components 與 `website*.go` 深度耦合，Stream／SSL／proxy／rewrite 都靠它。
4. 宿主機作業：systemd／rc-service、iptables／nftables／ufw／firewalld、lsblk／parted／mount／fstab、gopsutil process、SSH client。
5. 應用商店安裝腳本：`.sh` hook、Docker exec、`PANEL_DB_HOST`、資料庫初始化，牽涉大量隱性慣例。
6. 多節點與 xpack：憑證、`Proxy-Id`、socket 權限、`operateNode`，企業版行為不在 OSS repo 內。

建議切法：
- Core 保留 Go，繼續擁有 `/api/v2/core`。
- Rust Agent 先實作 `/api/v2` 的 host／file／process／container／website 五組，這五組覆蓋大部分宿主機風險。
- 應用商店與 AI 可先讓 Rust Agent 以「呼叫既有 Docker CLI + 腳本」方式承接，再逐步替換。
- Nginx 設定產生建議獨立成 Rust crate 或保留 Go sidecar，不要一開始就重寫 parser。

## 未驗證項目

- 24 個 agent router 的完整端點清單只抽樣（`ro_ai`、`ro_alert`、`ro_group`、`backup`、core 全部），其餘只取得數量與群組前綴。
- `agent/router/ro_host.go`、`ro_container.go`、`ro_database.go`、`ro_file.go`、`ro_website*.go` 的個別 handler 對應关系未逐一展開。
- 前端 `frontend/` 完全未看。
- WebSocket／terminal 的完整會話流程只確認檔案與部分函式，未追 `terminal/session.go`、`ws_local_session.go` 全文。
- AI Agents／Hermes／skills／channels 只確認路由與 service 檔名，未深入資料模型與執行流程。
- 備份加密演算法、OneDrive／Google Drive OAuth 流程未細讀。
- core／agent migration 的完整 ID 清單未逐條列出（先前輸出被截斷）。
- 企業版 xpack 實作不在 repo 內，無法確認替換行為。
- 防火牆 ufw／firewalld 的實際命令路徑只以 grep 檔案數推論，未逐檔確認呼叫點。

## 本輪經驗

- 大型 router 檔用 `grep -c '\.GET\|\.POST'` 先取數量，再抽樣讀取，比逐檔 `cat` 快很多。
- `grep "func "` 對某些 service 檔會誤判（方法 receiver 寫法不同），改用 `grep "\.GET(\|\.POST("` 與 `grep "^func "` 分開查較穩。
- 先確認 build tag 與 `commonGroups()`，再談端點，可避免把 core 與 agent 的路由混在一起。
- `agent/utils/xpack` 的 community 檔只有介面賦值，真正邏輯在 `helper/*`；只看 `community.go` 會誤判為全空實作。
- 截斷的長輸出要改讀工具快取檔，不要重跑同一個大範圍 grep。

Skills：無