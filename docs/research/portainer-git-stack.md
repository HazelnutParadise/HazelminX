> 來源：portainer/portainer（develop，commit 2d1ea91，2026-11-06）Git Stack 部署流程唯讀分析，2026-11-07，由派工 agent（opencode / big-pickle）產出，主 agent 核對過授權與 libstack 位置。注意：此版本已把 GitCredential 重構為 Source；報告是線索。

# Portainer CE Git Stack 部署調查

以下依原始碼靜態閱讀整理，未實際啟動 Portainer 或執行部署。

## Q1：Git 部署流程與認證方式

- 建立入口是 `POST /stacks/create/{type}/{method}`，Compose 走 `standalone/repository`，最終呼叫 `createComposeStackFromGitRepository`（`api/http/handler/stacks/handler.go:66`、`api/http/handler/stacks/stack_create.go:78-89`、`api/http/handler/stacks/create_compose_stack.go:265-333`）。
- Payload 欄位包含 `Name`、`SourceID`、`RepositoryURL`、`RepositoryReferenceName`、`RepositoryAuthentication`、`RepositoryUsername`、`RepositoryPassword`、`ComposeFile`、`AdditionalFiles`、`AutoUpdate`、`Env`、`TLSSkipVerify`（`api/http/handler/stacks/create_compose_stack.go:183-211`）。其中 URL、帳密、`TLSSkipVerify` 已標記 Deprecated，改用 `SourceID`。
- 建置器是 `GitMethodStackBuilder.prepare`，會解析 Source 或 inline 認證、設定 `ConfigFilePath`、下載 repo、儲存 commit hash，最後建立 Workflow（`api/stacks/stackbuilders/stack_git_builder.go:27-190`）。
- Clone 流程是 `DownloadGitRepository` → `GitService.CloneRepository` → go-git `CloneContext`，使用 `Depth: 1`、`NoTags`、`InsecureSkipTLS`、HTTP Basic Auth（`api/stacks/stackutils/gitops.go:20`、`api/git/service.go:80-119`）。
- 帳號密碼處理在 `GetBasicAuth`：沒有密碼就不帶認證；有密碼但帳號為空時，帳號自動變成 `"token"`（`api/git/service.go:354-366`）。這是 PAT 常見用法，但並非所有 Git 服務都接受。
- 沒有找到 SSH key、SSH agent 或 OAuth 認證實作；`ssh` transport 有註冊，但沒有對應的 key auth。`file` protocol 被停用，Git transport 外層有 SSRF 防護（`api/git/protocols.go:12-19`）。
- Azure DevOps 走不同路徑：不是 go-git clone，而是下載 zip 後解壓，且只支援 Basic Auth（`api/git/azure.go:66-140`）。
- Clone 會拒絕 symlink，clone 後預設刪除 `.git`（`api/git/git.go:25-76`）。
- 手動重新部署 `PUT /stacks/{id}/git/redeploy` 會先備份再 clone 最新版，不比較 hash；是否重新拉 image 由 `RepullImageAndRedeploy` 控制（`api/http/handler/stacks/stack_update_git_redeploy.go:180-233`）。

## Q2：Stack 檔案與 Additional Files

- Stack 檔案實際存在檔案系統，不在資料庫。Compose stack 的專案目錄是 `<dataStore>/compose/<stackID>`（`api/filesystem/filesystem.go:151-153`、`:229-247`）。
- `Stack.EntryPoint` 是主檔名，`Stack.AdditionalFiles` 只是檔名清單，不是檔案內容（`api/portainer.go:1283-1313`）。
- 部署時 `GetStackFilePaths` 會回傳「EntryPoint + AdditionalFiles」的絕對路徑，順序固定，主檔在前（`api/stacks/stackutils/util.go:21-31`）。
- 這些路徑直接傳給 compose 函式庫載入，沒有 tar/zip 打包，也沒有另外複製到暫存目錄（`api/exec/compose_stack.go:50-66`）。
- Git 建立 stack 時，整個 repo 被 clone 到專案目錄，因此 additional files 必須存在於 repo 中；後端只記錄檔名。
- 檔案上傳建立 Compose stack 的 payload 只有 `Name`、`StackFileContent`、`Env`，沒有 `AdditionalFiles`（`api/http/handler/stacks/create_compose_stack.go:335-339`）。多檔 Compose 目前主要走 Git 路徑。
- K8s Git stack 也讀 `EntryPoint + AdditionalFiles`，但會先加上 app labels、寫到暫存目錄，再交給 Kubernetes deployer（`api/stacks/deployments/deployment_kubernetes_config.go:31-80`）。

## Q3：平台環境變數是否注入容器

- `Stack.Env` 會存進資料庫，格式是 `[]portainer.Pair`（`api/portainer.go:1288-1289`）。
- Compose stack 部署時，`createEnvFile` 會把 `Stack.Env` 寫成專案目錄下的 `stack.env`，並把 repo 內的 `.env`（位於 `EntryPoint` 同層）內容先複製進來（`api/exec/compose_stack.go:160-183`）。
- `stack.env` 以 `EnvFilePath` 傳給 compose 函式庫，主要用於 Compose 檔案的變數替換，不會自動變成容器內的環境變數（`api/exec/compose_stack.go:57-65`、`pkg/libstack/compose/composeplugin.go:297-323`）。
- 容器要拿到變數，Compose 檔仍需使用 `environment` 或 `env_file`。這是常見誤解點。
- 若 `Stack.Env` 為空，後端不建立 `stack.env`；此時 compose 的 `WithDotEnv` 仍會讀取工作目錄下的 `.env`（`pkg/libstack/compose/composeplugin.go:323`）。
- Swarm stack 則把 `Stack.Env` 組成 `NAME=VALUE` 清單，傳給 Swarm 的 compose loader 做變數替換，也不是直接注入 service（`api/exec/swarm_stack.go:58-72`、`pkg/libstack/swarm/swarm.go:639-644`）。
- React UI 的「Load variables from .env file」只是前端讀檔、解析後填入變數清單，檔案不會上傳到後端；限制 1MB（`app/react/components/form-components/EnvironmentVariablesFieldset/SimpleMode.tsx:70-115`、`utils.ts:9-18`）。
- 沒有找到專屬的 `.env` 上傳 API。

## Q4：Auto Update 的實作

- 輪詢已改成以 `Source` 為單位。`SourceScheduler` 為每個 Source 建排程，讀 `Source.Interval`，不是讀 `Stack.AutoUpdate.Interval`（`api/gitops/scheduling/scheduler.go:105-150`）。
- `AutoUpdateSettings.Interval` 已標記 Deprecated，只用於資料庫相容；API 回應會從 Source 回填（`api/portainer.go:53-66`、`api/http/handler/stacks/response.go:89-102`）。
- 排程觸發後呼叫 `RedeployWhenChanged`；`StackStatusInactive` 會直接跳過，`WorkflowID == 0` 也直接跳過（`api/stacks/deployments/deploy.go:39-79`）。
- 變更判定是比較遠端最新 commit hash 與 `gitConfig.ConfigHash`，相同就不重新部署（`api/git/update/update.go:32-56`）。
- `AutoUpdate.ForceUpdate` 為 true 時會忽略變更、無條件重部署（`api/stacks/deployments/deploy.go:191-196`）。
- Compose 重部署固定帶 `prune=true`、`forcePullImage=true`、`forceRecreate=false`；Swarm 固定帶 `prune=true`、`pullImage=true`（`api/stacks/deployments/deploy.go:236-251`）。
- `AutoUpdateSettings.ForcePullImage` 欄位在後端沒有被讀取；實際行為是自動更新一律強制拉 image。這是設定與行為不一致的地方。
- 輪詢有 `singleflight` 去重，排程本身也有 per-source 的 atomic running 防重入（`api/stacks/deployments/deploy.go:57-60`、`api/gitops/scheduling/scheduler.go:140-148`）。
- Webhook `POST /stacks/webhooks/{webhookID}` 是公開端點，只用 `StackStatusDeploying` 回 409 擋重複，沒有走 `singleflight`；之後以 goroutine 非同步重部署（`api/http/handler/stacks/webhook_invoke.go:24-56`、`api/stacks/deployments/deploy.go:66-80`）。
- 建立 stack 時仍會驗證 `AutoUpdate.Interval`，但程式碼中沒有看到把它寫入 `Source.Interval` 的路徑；Source interval 是由 GitOps Sources 的 create/update 設定（`api/git/update/validate.go:10-34`、`api/http/handler/gitops/sources/create_git.go:135`、`update_git.go:175-176`）。舊 API 只帶 `AutoUpdate.Interval` 時是否真的會輪詢，待實測。

## Q5：Git 憑證模型、加密與共用

- 這個版本沒有獨立的 `GitCredential` 資料模型或 dataservice。憑證存在 `Source` 的 `Git` 欄位中，bucket 名稱是 `sources`（`api/dataservices/source/source.go:8-34`、`api/git/types/types.go:40-44`）。
- `GitAuthentication` 欄位是 `Username`、`Password`，另有 legacy 的 `Provider`、`AuthorizationType`、`GitCredentialID`（`api/git/types/types.go:130-136`）。後兩者目前只作為舊資料相容與 clone cache key，不用於實際認證。
- API 回傳前會用 `SanitizeGitSource` 清掉 URL 內嵌帳密與密碼（`api/git/types/types.go:57-74`）。
- 沒有逐筆憑證加密；只有整個 BoltDB 的加密。CE 預設沒有 encryption key，`loadEncryptionSecretKey` 找不到 secret 就回傳 nil，資料庫以明文 `portainer.db` 儲存（`api/cmd/portainer/main.go:353-370`、`:393`）。
- 有 secret key 時，整個資料庫以 AES-GCM 加密成 `portainer.edb`（`api/database/boltdb/db.go:23-30`、`:87-107`、`api/database/boltdb/json.go:21-64`）。
- 多 stack 共用靠 `SourceID`。建立 stack 時可以引用既有 Source；沒有 SourceID 時 `FindOrCreateGitSource` 會用正規化 URL + 認證去尋找可重用的 Source，找不到才建立（`api/dataservices/source/tx.go:172-232`、`api/stacks/stackbuilders/stack_git_builder.go:130-170`）。
- Stack 本身不再直接持有有效 Git 設定；`Stack.GitConfig` 已標記 Deprecated，實際透過 `WorkflowID` 關聯到 Source 與 Artifact（`api/portainer.go:1308-1318`）。
- 舊資料由 migration 轉換，例如 `migrate_2_43_0.go` 把舊 Git credential 欄位搬進 `GitAuthentication`（`api/datastore/migrator/migrate_2_43_0.go:27-48`）。

## Q6：Compose 執行方式

- Portainer 使用 in-process 的 Docker Compose v2 Go 函式庫，不是呼叫 `docker compose` CLI 子程序（`pkg/libstack/compose/composeplugin.go:43-110`）。
- Docker API client 由 `libstack.WithCli` 建立，compose service 直接對 Docker daemon 操作（`pkg/libstack/dockercli.go:32`）。
- `Deploy` 會載入 project、加 labels、必要時 `Build`，再呼叫 compose service 的 `Up`（`pkg/libstack/compose/composeplugin.go:74-110`）。
- `Pull` 直接呼叫 compose service 的 `Pull`，自動更新時會先 Pull 再 Up（`pkg/libstack/compose/composeplugin.go:174-176`、`api/stacks/deployments/deployer.go:60-79`）。
- 多個檔案會全部傳入 compose project options，沒有合併成單一檔案（`pkg/libstack/compose/composeplugin.go:281-326`）。
- 沒有找到 `--no-deps` 或等價的 `NoDeps` 開關；依 compose 預設行為處理依賴（全 repo grep 無結果）。
- `prune` 對應 `RemoveOrphans`；compose 檔的 `COMPOSE_REMOVE_ORPHANS`、`COMPOSE_IGNORE_ORPHANS` 可覆寫（`pkg/libstack/compose/composeplugin.go:85-97`）。
- `forceRecreate` 對應 `api.RecreateForce`；自動更新不使用強制重建，只強制拉 image（`pkg/libstack/compose/composeplugin.go:80-86`）。

## Q7：K8s 從 Git 部署

- 入口是 `POST /stacks/create/kubernetes/repository`，payload 是 `kubernetesGitDeploymentPayload`（`api/http/handler/stacks/create_kubernetes_stack.go:44-66`、`:217`）。
- 欄位包含 `StackName`、`Namespace`、`SourceID`、`RepositoryURL` 系列、`ManifestFile`、`AdditionalFiles`、`AutoUpdate`、`TLSSkipVerify`。
- 建置器是 `KubernetesStackGitBuilder`，設定 `Type=KubernetesStack`、`EntryPoint=ManifestFile`，再走共用的 Git 下載流程（`api/stacks/stackbuilders/k8s_git_builder.go:41-60`）。
- 部署時讀取 `EntryPoint + AdditionalFiles`，逐檔加上 app labels，寫到暫存目錄，再交給 Kubernetes deployer 套用（`api/stacks/deployments/deployment_kubernetes_config.go:31-80`）。
- 沒有 compose-to-Kubernetes 轉換流程；檔案一律視為 Kubernetes manifest 套用。
- Payload 有 `ComposeFormat` 欄位，但轉成 `StackPayload` 時沒有帶入，K8s Git builder 也沒有使用（`api/http/handler/stacks/create_kubernetes_stack.go:68-85`、`api/stacks/stackbuilders/stack_payload.go:9-34`）。因此這個欄位在此版本不影響 Git 部署。
- K8s 也支援 `POST /stacks/create/kubernetes/url`，但不在 Git 路徑（`api/http/handler/stacks/create_kubernetes_stack.go:298`）。

## Q8：Stack 資料模型與部署流程差異

- `Stack` struct 包含 `ID`、`Name`、`Type`、`EndpointID`、`SwarmID`、`EntryPoint`、`Env`、`Status`、`DeploymentStartStatus`、`ProjectPath`、`AdditionalFiles`、`AutoUpdate`、`Option`、`WorkflowID`、`CurrentDeploymentInfo`、`Namespace`、`DeploymentStatus`（`api/portainer.go:1272-1319`）。
- `StackType` 是 `DockerSwarmStack=1`、`DockerComposeStack=2`、`KubernetesStack=3`（`api/portainer.go:2466-2474`）。
- `StackStatus` 是 `Active=1`、`Inactive=2`、`Deploying=3`、`Error=4`；部署前後由 `PrepareStackStatusForDeployment` 與 `UpdateStackStatusFromDeploymentResult` 更新（`api/stacks/stackutils/stack_status.go`）。
- Compose 與 Swarm 共用同一個 Stack struct，差別在 `Stack.Type` 與 deployer 分支。
- Compose 部署走 `ComposeStackManager.Up`，使用 `stack.env`、compose 函式庫、`Prune`、`ForceRecreate`（`api/exec/compose_stack.go:38-66`）。
- Swarm 部署走 `SwarmStackManager.Deploy`，把 `Env` 當成 compose 變數清單，部署後等待 service 狀態，預設有 30 秒早期失敗檢查（`api/exec/swarm_stack.go:35-87`）。
- K8s 部署走 `KubernetesStackDeploymentConfig.Deploy`，加 labels 後套用 manifest（`api/stacks/deployments/deployment_kubernetes_config.go:31-80`）。
- Git 資訊在 Stack 上只剩 deprecated `GitConfig`；新流程以 `WorkflowID` 關聯 Source/Artifact，`CurrentDeploymentInfo` 記錄最後一次部署的 repo 狀態（`api/portainer.go:1308-1318`）。

## Q9：Edge Stack 的差異與部署方式

- Edge stack 是獨立模型 `EdgeStack`，不在一般 Stack bucket；欄位包含 `Status map[EndpointID]EdgeStackStatus`、`EdgeGroups`、`Version`、`NumDeployments`、`ManifestPath`、`DeploymentType`、`UseManifestNamespaces`（`api/portainer.go:334-354`）。
- 一般 stack 綁定 `EndpointID`；edge stack 綁定 `EdgeGroups`，建立時由 `EdgeStackRelatedEndpoints` 算出受影響的環境（`api/internal/edge/edgestacks/service.go:74-122`）。
- 建立 edge stack 時 `Version` 從 1 開始（`api/internal/edge/edgestacks/service.go:34-58`）。
- 更新 edge stack 時 `Version++`，清除 per-endpoint 狀態，重新寫入檔案；這會讓 edge agent 偵測到新版本並重新部署（`api/http/handler/edgestacks/utils_update_stack_version.go:11-17`）。
- Server 本身不直接對 edge 環境執行 Compose 或 kubectl；它只保存 manifest、版本與狀態，實際部署由 edge agent 拉取後執行。
- Edge stack 支援 Compose、Kubernetes manifest，以及 `HelmConfig`（`ChartPath`、`ValuesFiles`、`ChartURL`、`Atomic`、`Timeout`、`Namespace`）（`api/portainer.go:356-386`）。
- 建立入口包含 `/edge_stacks/create/repository`，走 Git 下載與 `PersistEdgeStack`（`api/http/handler/edgestacks/edgestack_create_git.go:115-190`）。

## Q10：授權與重用限制、BE 專屬目錄

- 根目錄 `LICENSE` 是 zlib 類型授權：可商業使用、修改、再散布；限制是不得虛假標示來源、修改版必須明確標示、不得移除授權聲明（`LICENSE:1-22`）。
- `pkg/libstack` 另採 MIT 授權，重用時需保留 copyright 與 permission notice（`pkg/libstack/LICENSE.md:1-20`）。
- 第三方元件授權列在 `ATTRIBUTIONS.md`，包含 CC BY、MIT 等，重用時仍需逐項遵守（`ATTRIBUTIONS.md:1-40`）。
- `README.md` 說 CE 是免費、as-is、不提供正式支援，且不建議用於生產；這是使用定位，不是額外授權限制（`README.md:5-23`）。
- 沒有找到以目錄為單位標示「BE 專屬」的結構；`app/` 下沒有 `ee`、`business` 這類目錄。
- 與 Business Edition 有關的目錄是 `app/react/components/BEFeatureIndicator`、`app/react/sidebar/UpgradeBEBanner`、`app/portainer/components/BEFeatureIndicator`，但這些是升級提示與功能標記，CE 也會執行，不是 BE-only 程式。
- 真正隔離在 feature flags：`app/react/portainer/feature-flags/feature-flags.service.ts:12-46` 把多個功能標為 `Edition.BE`；`withEdition.tsx:3-22` 依 `PORTAINER_EDITION` 決定是否渲染。
- 與本題直接相關的 BE 標記有 `STACK_WEBHOOK`、`STACK_PULL_IMAGE`、`FORCE_REDEPLOYMENT`（`feature-flags.service.ts:34-38`，前端使用於 `WebhookFieldset.tsx:64`、`AutoUpdateSettings.tsx:47`、`ForceDeploymentSwitch.tsx:24`）。
- 這個 repo 只含 CE 原始碼；沒有找到 EE/BE 後端實作目錄。Shared struct 內雖有 `// EE only feature` 註解，但沒有對應 EE 程式（`api/portainer.go:391-392`、`:424-425`）。

## Q11：三個可借鏡設計、三個坑

### 可借鏡設計

- Source 與 Artifact 分離：URL、認證、TLS 放在可共用的 `Source`；ref、path、hash 放在 per-file `ArtifactFile`。多 stack 不會重複存同一組憑證（`api/git/types/types.go:21-44`、`api/gitops/workflows/source_artifact.go:241-285`）。
- `FindOrCreateGitSource` 以正規化 URL + 認證去重，並處理共用權限；適合多個 stack 指向同一 repo 的情境（`api/dataservices/source/tx.go:172-232`）。
- 排程以 Source 為單位，而不是每個 stack 一個 job；同一 Source 的多個 artifact 可共用 `TmpClones`，排程層與部署層都有防重入（`api/gitops/scheduling/scheduler.go:105-150`、`:191-203`）。
- Deployer 抽象加上 in-process compose 函式庫，讓 Compose、Swarm、K8s 共用同一套 stack 狀態與流程，也避免 CLI 子程序的版本與輸出解析問題（`api/stacks/deployments/deployer.go:31-79`、`pkg/libstack/compose/composeplugin.go:43-110`）。

### 三個坑

- 憑證保護不足：CE 預設沒有資料庫加密，Git 密碼會以明文存在 `portainer.db`；只有管理員提供 secret key 才會變成 `portainer.edb`。若照抄這個模型，必須補上自己的加密或外部 secret 管理（`api/cmd/portainer/main.go:353-393`、`api/database/boltdb/db.go:23-30`）。
- Auto Update 設定與實際行為不一致：`ForcePullImage` 後端沒讀，實際一律強制 Pull；`AutoUpdate.Interval` 已廢棄，真正的輪詢間隔在 `Source.Interval`；自動更新又固定 prune。使用者以為關掉的選項其實沒有作用（`api/stacks/deployments/deploy.go:236-251`、`api/portainer.go:53-66`）。
- `Stack.Env` 不是容器環境變數：它只被寫成 `stack.env` 供 Compose 插值；容器要拿到變數仍需 `environment` 或 `env_file`。若把平台變數當成會自動注入，會出現「設定有值但容器內沒有」的現象（`api/exec/compose_stack.go:160-183`、`pkg/libstack/compose/composeplugin.go:304-323`）。

## 未驗證項目

- 未實際啟動 Portainer、建立 stack 或執行 Git clone；以上皆為靜態閱讀。
- 未實測舊 API 只帶 `AutoUpdate.Interval` 時，新建 stack 是否真的不會輪詢。
- 未驗證各 Git 供應商對空白帳號自動轉成 `"token"` 的接受程度。
- 未驗證非 Azure 的 SSH URL 是否能成功 clone；目前只確認程式碼沒有 SSH key 認證。
- 未追蹤 edge agent 端實際拉取與部署 edge stack 的完整程式路徑。
- 未執行前端或後端測試。

## 本輪經驗

- 這個 repo 沒有 `docs/` 目錄，但 `CLAUDE.md` 連到 `docs/guidelines/*`；閱讀前要先確認實際路徑存在。
- zsh 下 `grep --include=*.go` 會因 glob 展開失敗，需加引號。
- Compose 真正執行邏輯在 `pkg/libstack`，不在 `api/exec`；找部署行為要從 `pkg/libstack/compose` 進去。