# Architecture: HazelminX

這份檔案描述系統現在怎麼建：架構、元件邊界、測試接縫、資料遷移順序與前提假設。每個難以回頭的選擇為什麼這樣做，寫在 `engineering/decisions/`。每張票自己的驗收條件在 `engineering/tickets/`，不在這裡重複。規格原文在 `engineering/specs/`；這裡只寫「我們怎麼做」，不改寫「要做成什麼」。

## Architecture

```text
瀏覽器（React 靜態檔，由 Core 嵌入提供）
   │ HTTPS  /management/v1/*  （Cookie session + CSRF；長操作回 202 + jobId）
   │ WebSocket /management/v1/events  （事件序號接續）
   ▼
hazelminx-core（Go，systemd；單一執行檔，嵌入 web/dist）
   ├─ api/        路由、驗權中介層、OpenAPI 產生
   ├─ iam/        使用者、角色、資源範圍、工作階段、API Token、稽核
   ├─ jobs/       工作引擎：狀態機、去重、資源鎖、事件序號、控制世代
   ├─ plans/      變更計畫（change-plans）與批准
   ├─ nodes/      Node PKI、心跳、能力、Agent 通道（mTLS WebSocket 伺服端）、步驟執行器
   ├─ nodereg/    節點名冊：註冊碼與核准、節點 ID／名稱／標籤／群組、憑證輪替、撤銷、維護、移除、連線狀態
   ├─ domain/     專案、服務、設定版本、部署來源、儲存綁定、網站、備份、排程…
   │              （從 1Panel v2.4.1 core/agent 移植的業務規則落在這裡）
   ├─ secrets/    機密參照、主金鑰、輪替
   ├─ store/      SQLite（modernc，純 Go，WAL）＋ migration
   └─ obs/        結構化日誌、分段流量日誌、metrics、診斷包
          ▲
          │ Agent 主動建立 TLS 長連線（mTLS）；訊息有版本；心跳 10 s
          ▼
hazelminx-agent（Rust，systemd，低權限使用者；以 Komodo Periphery 選定 commit 改造）
   ├─ connection/ 主動連 Core、重連退避、對帳
   ├─ jobs/       持久工作紀錄（本機 sqlite／append-only 檔）、步驟證據
   ├─ probes/     OS、引擎、磁碟、網路、GPU 偵測（唯讀）
   ├─ stats/      sysinfo 指標、GPU adapter
   ├─ docker/     bollard：容器、映像、網路、Volume、Compose（呼叫 docker compose CLI）
   ├─ files/      目錄控制代碼為基準的檔案操作、分段傳輸
   ├─ terminal/   portable-pty 串流
   └─ privileged-client/  經本機 Unix socket 呼叫高權限執行器
          │ SOCK_PEERCRED 驗證呼叫者；固定動作 + 結構化參數
          ▼
hazelminx-privileged（Rust，root；只實作白名單動作：套件、systemd、掛載、磁碟、防火牆、sshd、使用者、Zentinel 設定套用）

hazelminx（Rust CLI）：install / update / doctor / rescue / uninstall；可在 Core 或 Agent 壞掉時執行

網站訪客 ─► Zentinel（原生 systemd；KDL 設定由 Core 產生、Agent 經 privileged 套用、SIGHUP 重載）
                ├─ filters: agent（WAF agent、Token API 政策快照）、rate-limit、compress
                ├─ ACME（內建 HTTP-01 / DNS-01）、SNI、mTLS
                └─ upstream：受控本機回送 Port 或已驗證本機網路；叢集走 Service/Gateway
Token API 代理（infra-manager 相容）：Core 內的 proxy 模組掛在獨立 listener，不經 /management
Kubernetes／K3s：Core 以 kubeconfig 經授權 API 管理，不碰 Pod 底層容器
```

信任邊界：瀏覽器→Core（不可信輸入）；Core→Agent（雙向憑證，節點身分獨立於 IP）；Agent→privileged（本機 socket、呼叫者身分）；Zentinel→上游（本機回送或已驗證網路）；Webhook 入口與一般管理 API 分開 listener 與驗證。

## Repository layout

```text
core/      Go module github.com/HazelnutParadise/hazelminx/core
agent/     Cargo workspace：crates/agent, crates/privileged, crates/cli, crates/protocol, crates/platform-{linux,windows}
web/       React + TypeScript + Vite+（vp）；dist 由 CI 建置後嵌入 core
docs/      spec/（規格）、tickets/、research/、baseline/、design/（設計過程檔）
scripts/   CI 與發布腳本（簽署清單、依賴檢查）
.github/   workflows
```

單一 repo、單一 Release 版本號。Core 與前端配對發布；Agent 以協定版本範圍相容。

## Key technology decisions

| 決策 | 選擇 | 理由與來源 |
|---|---|---|
| Go HTTP 框架 | Gin | 1Panel 與 infra-manager 都用 Gin，移植成本最低；路由定義可產生 OpenAPI。 |
| 平台資料庫 | SQLite（`modernc.org/sqlite`，純 Go）、WAL、`PRAGMA foreign_keys=ON` | PKG-001 要求乾淨主機無 C 編譯器；1Panel 已使用純 Go 的 glebarez/sqlite。多 Core 不共用同一檔案（35.1）。 |
| Migration | 版本表 + 有序 SQL 檔 + Go 程式化步驟；只允許向上；啟動時比對 `schemaVersion` 拒絕舊執行檔開新資料 | DAT-006、REL-006。 |
| Core↔Agent 協定 | Agent 主動建立 WebSocket over TLS 1.3（Core 的 Agent 入口，預設 `:9801`），JSON 訊息；握手四則（hello／welcome／auth／ready）交換 `protocolVersion` 範圍、`capabilities` 與 X25519 公鑰，雙方各以 Ed25519 私鑰簽雙方 nonce、雙方 X25519 公鑰、協定版本、節點指紋、session ID、hello 原文雜湊與 welcome 參數雜湊；之後每則訊息包成帶方向序號與 HMAC-SHA256（金鑰由 X25519 共同秘密經 HKDF 導出）的訊框；Core 有自己的 CA（私鑰在機密服務），節點憑證 EKU 只有 clientAuth、Core 憑證固定名稱 `core.hazelminx.internal`；直連時另要求 TLS 用戶端憑證與握手身分一致，經會終止 TLS 的代理時只靠握手簽章，任何 HTTP Header 都不當身分；握手訊息寬鬆解碼、握手後嚴格解碼；持久事件帶每節點單調序號並由 Core 累計確認；請求帶 `requestId`、`controlGeneration`、`expiresAt`。契約在 `engineering/contracts/README.md` | NOD-002、NOD-011、SEC-011、ENG-010、SEC-004。Komodo Periphery 已有 Agent 主動連 Core 的 WebSocket 客戶端（`connection/client.rs`），但身分驗證是 `mogh_pki` 的 Noise 握手而非 mTLS，Go 端沒有現成相容實作；因此 `connection/` 改為 mTLS 加握手簽章，保留其單一 WebSocket 多工請求的設計。docker／stats／terminal 模組保留；`lib/command` 以 shlex 拆參數後走 `Command::args`，可作低權限命令層，但高權限動作仍走 privileged。 |
| GPU 指標 | NVIDIA 以 purego 動態載入 NVML（免 CGO／免綁定），AMD 與 Intel 讀 sysfs（`/sys/class/drm`、hwmon），CLI（nvidia-smi、rocm-smi、intel_gpu_top）只作退路；Rust 端對應用 `libloading` + sysfs | MON-002；Beszel 研究報告第 4 節。缺值明示，不假造跨廠相同欄位。 |
| 協定單一來源 | `agent/crates/protocol`（Rust serde 型別）+ `engineering/contracts/fixtures/*.json` 黃金樣本；Go 端 `core/internal/protocol` 以相同樣本做契約測試 | 兩種語言各自編譯，樣本保證相容。 |
| 高權限隔離 | `hazelminx-privileged` 獨立行程（root），Unix socket，`SO_PEERCRED` 驗證呼叫者 uid；每個動作是具名 enum + 結構化參數，執行時以 `Command::args` 傳遞，不經 shell；每個請求附 Core 簽發的 Ed25519 授權憑證（權限名稱清單、有效期上限 24 小時、可依 id 撤銷），執行器以 `issuer.pub` 驗簽並逐動作比對權限；`health`／`revoke` 不排動作名額，輸出走有界通道 | 32.2、SEC-005、SEC-006、SEC-009。Komodo `lib/command` 以 shell 字串執行，不直接沿用。 |
| 主機變更帳本 | 執行器在每個會改主機的動作前後寫帳本：動作在 `Action::ledger_decl` 宣告會改的對象（只讀動作宣告只讀），執行器先讀出變更前狀態、同步寫入意圖行，才執行，之後讀變更後狀態寫結果行。沒有宣告、帳本不可用、讀不到變更前狀態都拒絕並記安全事件。帳本在 `/var/lib/hazelminx/ledger`（root 0700，上層不得可被他人寫入），每行帶前一行的 SHA-256 形成雜湊鏈，檔案類對象另存原內容備份。Core 的副本經 Core↔Agent 通道同步：Core 為每一頁簽一張只有 `host.ledger.read` 的授權憑證（簽發金鑰在機密服務），送 `ledger_pull`，Agent 轉交執行器的 `ledger_export`，Core 驗證雜湊鏈後存進只能新增的 `host_ledger_lines`。安裝器（013）用同一個 `Recorder` 記錄安裝步驟，安裝前先跑 `snapshot capture` | 增補 07 第 55.1 節、UNX-001、UNX-002、T-250。擁有權依「變更前是否存在」與「變更前後狀態是否不同」判定，之後只升不降。原始行原樣傳給 Core，Go 端以 `json.RawMessage` 驗雜湊，不必重現 Rust 的序列化。 |
| 主機資訊與指標 | Agent 的監控執行緒每 10 秒讀 `/proc`、`/sys` 與 `statvfs` 一次（網路掛載的 `statvfs` 另開執行緒，2 秒沒回就標 `unresponsive`，卡住的掛載點不再重查），每 60 秒重讀主機資訊；樣本放進最多 360 筆的記憶體暫存，滿了丟最舊的並計數。Core 宣告 `host.metrics` v1 時，會話送 `host_facts`（有變才送）與 `metrics_report`，Core 存好才回 `metrics_ack`，Agent 收到才刪。Core 以節點 ID 保存（`host_facts`、`host_metric_latest`、`host_metric_points` 原始點 48 小時、`host_metric_rollups` 5 分鐘彙總 90 天），每小時清一次過期資料 | 票 020.1、SYS-001、MON-001、MON-005、ROB-009、規格 38.1 與 38.2。讀不到的值是 `null`，不產生歷史點；歷史以 `gapBefore` 標出缺口，不補零。網路介面只有實體網卡有歷史，每類（核心、磁碟、介面、檔案系統、感測器）最多 64 個有歷史，最新樣本保留全部。 |
| 工作引擎 | Core 持久化工作與步驟；Agent 在每個步驟開始前與結束後先寫入本機 append-only 紀錄（`journal.jsonl`，fsync）再回報，已確認且已結束的工作會壓縮進封存檔，步驟只留結果（決策 0032）；重連先對帳（Core 以 `step_reconcile` 查 Agent 紀錄）再決定續跑，已完成步驟不重跑，中斷的步驟只有標記可重試時才再跑；資源鎖以「裝置身分 + 掛載 + 正規化路徑」為鍵 | 33 章。 |
| 前端 | React 19、TypeScript、Vite+（`vp`）；路由 react-router；資料 TanStack Query；表單 react-hook-form + zod；UI 以 Cloudflare Kumo（`@cloudflare/kumo`，MIT，Base UI 原語＋Tailwind v4）為元件底層，主題以 DESIGN.md token 覆蓋 Kumo 語意 token，對映表在 `design/component-mapping.md`；i18n i18next（zh-TW、en）；終端機 xterm.js；編輯器 CodeMirror 6；圖表 Kumo Chart（ECharts 6），配色與規則依 DESIGN.md「資料視覺化」 | 7.2、UI-005；不複製上游品牌素材。 |
| Zentinel 整合 | 固定版本；Core 產生完整解析後的 KDL（不含 `${VAR}`）→ Agent 經 privileged 寫入候選檔 → `zentinel --validate`（以選定版本能力為準）→ 原子換檔 → SIGHUP → 實際請求驗證 | 研究報告：無 HTTP reload API、無環境變數代換、admin listener 預設 `0.0.0.0:9090` 且無驗證（必須改綁本機並以防火牆限制）。 |
| 可信訪客 IP | Zentinel 只處理 `X-Forwarded-For` 與 per-listener `trusted-proxies`；`CF-Connecting-IP`／Tunnel 判定由平台維護的 Zentinel agent filter 實作，輸出單一 `X-HazelminX-Client-IP` 供 WAF、限速、分析共用 | IP-002、研究報告第 6 節。 |
| WAF | 透過 Zentinel agent filter 接 WAF agent（候選：`zentinel-agent-modsec`、ZentinelSec）；核心 `waf.mode` 目前是 hard error，不使用 | 21.1、研究報告第 2 節。候選比較與測試是票 064 的驗收項。 |
| TCP／UDP 代理 | Zentinel 無 L4 代理；以獨立受驗證元件提供（候選另評估），不拿 HTTP 代理冒充 | WEB-013、研究報告第 8 節。 |
| Token API 代理 | 以 infra-manager 的 Go 邏輯為基礎移植到 Core 的 `proxy/` 模組，掛獨立 listener；入口政策快照同步到 Zentinel agent 供離線 5 分鐘放行 | 20 章、`docs/research/infra-manager-contract.md`。 |
| Compose 執行 | Agent 呼叫固定版本的 `docker compose` CLI（Compose v2），以 `--env-file` 傳乾淨環境；Core 以 `compose-go/v2` 解析與合併多檔以產生預覽與差異，不自行實作替換語法 | 15.2、CMP-008。1Panel 走 CLI；Portainer 的 libstack 在行程內使用 `docker/compose/v2` Go 函式庫。CLI 讓 Rust Agent 不必綁 Go 函式庫，且行為與使用者手動操作一致；若需要進度事件再評估 in-process。 |
| GitHub | GitHub App（manifest 自架）為網頁授權預設，PAT 與 Deploy Key 同等；受控抓取服務在 Core 側（或指定節點的隔離工作）取得固定 commit 後交付內容；連線（Source）與部署綁定分離，多專案以連線 ID 參照 | 15.6、31.6。Portainer 的 Source／Artifact 分離與以 Source 為單位的排程、`singleflight` 去重是可借鏡的結構；其 CE 預設明文存憑證與「ForcePullImage 設定無效」是要避開的坑。 |
| 時間與容量 | UTC RFC 3339；位元組；排程保存 IANA 時區名稱 | 34.3。 |
| 容器網路模型 | 每個部署單元或應用有自己的網路（Compose 預設網路）；平台管理的共用服務（資料庫、Ollama、推論 runtime）各自一個 `hzx-*` 網路，宣告依賴才接上；不使用全部互通的共用網路，1Panel 範本的 `1panel-network` 在覆寫層改掉，只有相容模式才加入；Zentinel 是原生 systemd，經受控本機回送 Port 連上游；跨主機與跨單元走增補 04 的內部位址 | 增補 06 第 54.1 節（2026-11-08 使用者核定）、第 40 章「不能只信任整段 Docker 網路」。 |
| 主機變更帳本 | 高權限執行器在每個動作前後寫帳本（對象、變更前後狀態、擁有權、工作 ID），主機本機與 Core 各一份；安裝時記錄安裝前快照；完全移除依帳本反向處理並比對快照 | 增補 07（2026-11-08 使用者要求「刪乾淨跟沒裝過一樣」）、UN-004。 |
| 第二驗證 | TOTP 自寫（RFC 6238，HMAC-SHA1、6 位、30 秒、容許前後一步，最後接受的時間步存資料庫防重放），種子放機密庫；Passkey 用 `go-webauthn/webauthn` v0.18.2（BSD-3-Clause），憑證記錄建立時的 `rp_id`；復原碼 10 組 `XXXX-XXXX`，以存在機密庫的金鑰做 HMAC-SHA256 後保存，用掉以條件式 UPDATE 標記；擁有者、平台管理員、主機維運者沒有第二驗證時只能走綁定路由；新增或移除憑證要 10 分鐘內再次驗證 | IAM-002、IAM-011、IAM-012。go-webauthn 是 Go 生態完整實作 WebAuthn 伺服器流程的函式庫（前身 duo-labs/webauthn），2026-09 仍有發版，涵蓋 L3 與 attestation 格式。平台資料庫只有一條連線，交易開著時不能呼叫機密庫，所以驗證在交易外做、交易內只做條件式更新。 |

## Data flow

**安裝（39.1）**：`hazelminx install` → 預檢報告 → 取得簽署清單並下載驗證 → 建立使用者／目錄／systemd → 啟動 Core → 產生一次性啟用碼（stdout，不入共享日誌）→ 瀏覽器完成擁有者設定 → Agent 以本機通道註冊第一個節點。

**加入節點（NOD-001，決策 0031）**：管理員 `POST /management/v1/node-enrollments` 取得一次性註冊碼（只顯示一次，預設 10 分鐘）→ 主機上執行 `hazelminx-agent enroll --core-url <Agent 入口> --code <碼>` → Agent 先從 Agent 入口取 Core CA，以註冊碼導出的金鑰驗 HMAC，驗過才用這張 CA 驗 TLS 送出註冊碼與節點公鑰 → Core 把碼標成已使用並記下主機名稱、machine-id、來源位址、金鑰指紋，等管理員核對 → 管理員核准（同一把金鑰沿用原節點；新金鑰但 machine-id 相同要選取代或新建）→ Core 簽發節點憑證 → Agent 寫入 `identity/node.crt`、`core-ca.crt`、`privileged-issuer.pub` 與 `enrollment.json`，之後 `run` 依 `enrollment.json` 的網址連回。節點的身分是 `nodes.id`，指紋只是目前用的金鑰；之後的連線都要經 `nodereg.Registry.AuthorizeNode`（狀態、目前或輪替中的憑證序號），步驟都要經 `GateJob`（撤銷、身分衝突、維護）。

**一般操作（33 章）**：UI 送 `POST /management/v1/change-plans`（或直接 `POST /management/v1/jobs` 對不需計畫的工作種類）→ Core 驗權、由資源票註冊的 `plans.Kind` 算出目標（含版本與實際名稱）、差異、受影響服務與預檢，存成帶 SHA-256 摘要的計畫 → 以審查時的摘要批准（高風險只有擁有者）→ `POST /change-plans/{id}/execute`；批准與執行前都重算計畫，摘要不同就讓計畫失效並回差異（決策 0033）→ 建立 job（帶批准者、計畫 ID 與摘要；計畫種類的工作不能直接 `POST /jobs`） → 取得資源鎖 → 指派給 Agent（含 controlGeneration、expiresAt）→ Agent 持久化步驟、執行、回報證據事件 → Core 驗證完成條件 → 成功／失敗／狀態不明。步驟帶著工作在這個節點保留的資源（`step_execute.resources`），Agent 比對動作宣告會碰的資源，超出就拒絕（SEC-004）；資源該不該給這個工作，由 Core 的資源擁有者決定（票 090）。

**GitHub 部署（39.3）**：來源選擇器讀 `source-connections/{id}/repositories` → 綁定 Repo ID + ref → Core 核對授權與平台權限 → 固定 commit → 合併四份設定（來源、覆寫、變數、固定結果）→ 預檢 → job：受控抓取 → 隔離建置 → compose up（只動受影響服務）→ 健康檢查 → 記錄 digest 與設定版本。

**網站發布（39.5）**：網站／網域／上游模型 → 產生 KDL 候選 → validate → 原子切換 + SIGHUP → 實際請求驗證 → 記錄路由版本。

**備份（39.7）**：排程觸發 job → 一致性方法（dump／協調快照／停寫）→ 產生 manifest → 多目的地各自上傳與校驗 → 保留政策 → 驗證層級。

## Test seams

| 流程 | 接縫 | 假物 | 為什麼選這層 |
|---|---|---|---|
| 管理 API 與權限 | `POST/GET /management/v1/*`（httptest） | Agent 通道以 in-process fake（實作同一協定，回放黃金樣本） | 存活於內部重構；T-013、T-014 直接在此驗。 |
| 工作引擎 | jobs 套件的 `Engine` 介面 + fake Agent | 注入斷線、磁碟滿、重複請求 | ENG-001～012 的失敗注入必須可程式化。 |
| Core↔Agent 協定 | 黃金 JSON 樣本（Rust serde round-trip 測試 + Go 解碼測試，含固定種子的簽章樣本）；互通測試以真的 `hazelminx-agent`（`--features test-actions`）連 Go hub（`HAZELMINX_AGENT_BIN`） | Core 端單元測試用 Go 寫的假 Agent；會終止 TLS 的代理用 `httputil.ReverseProxy` | 兩語言共用同一組樣本，版本變更時同時失敗；互通測試證明兩個實作真的接得起來（斷線、重開機、代理）。 |
| 節點名冊 | `nodereg.Registry` 的方法（真的 SQLite 與 PKI）；管理 API 用 `httptest` 的 `/management/v1/nodes*`；註冊與輪替的端到端以真的 `hazelminx-agent`（`HAZELMINX_AGENT_BIN`）連真的 hub 與 `AgentHandler` | Agent 通道以 `Channel` 介面的假物（連線清單、斷線、送憑證）；時鐘可注入 | 碼的期限、核對等待、憑證到期都靠時間，假時鐘才能測；真 Agent 測試證明 CA 發現、HMAC、憑證輪替與複製 VM 偵測兩邊真的接得起來。 |
| 高權限執行器 | socket 層：測試客戶端送具名動作 | 動作處理器以 trait 替換（記錄而不執行） | 參數逃逸測試（T-039）在這層。 |
| 宿主機操作 | 真實 Linux VM（CI 的 ubuntu-24.04 / debian-12 runner 或 KVM） | 無；只在標記可丟棄的 VM 執行破壞性測試 | 41.1 要求真實引擎整合。 |
| Compose／Docker | 真實 Docker（CI service）；Podman rootless 另一 job | 無 | T-051、T-052 需原生解析結果比對。 |
| Zentinel | 固定版本二進位 + 真實請求（curl） | 上游以測試容器 | WEB-014 要求實際請求驗證。 |
| Token API 相容 | 從 infra-manager 的 `middlewares/auth_test.go` 等移植為 Core 的契約測試 | 上游 httptest | 20.1 的 403／404／優先順序不得改。 |
| 前端 | vitest + Testing Library；關鍵流程 Playwright（360px 與桌面、鍵盤）對真的 Core：`web/e2e/start-core.sh` 每次建置 `core/` 並以空資料目錄啟動，`first-run` 專案從設定畫面建立擁有者 | 單元測試以替換 `fetch` 的假 Core（`web/src/test/fakeCore.ts`）；Passkey 用 Chromium 虛擬驗證器（CDP `WebAuthn`） | UI-005、T-117、T-118；登入、Cookie、CSRF 與 WebAuthn 只有真的 Core 才驗得到。 |
| GitHub | 模擬 GitHub API（httptest 回放分頁、限流、撤銷）+ 真實 GitHub 測試組織（GH-T34） | 模擬層只覆蓋極端狀態 | 41.3。 |

## Test matrix

| 類型 | 覆蓋 | 優先 |
|---|---|---|
| 單元 | 權限判定、工作狀態機、資源鎖鍵、變數分層解析、KDL 產生、Token 辨識、路徑正規化 | 高 |
| 契約 | Core↔Agent 樣本、OpenAPI 與伺服器一致、infra-manager 相容 | 高 |
| 整合 | 真實 Docker／Podman／systemd／Zentinel／SQLite 斷電 | 高 |
| 端到端 | 安裝→重開機→登入；GitHub 部署→push→保留設定；備份→異地還原；Core 重啟不影響網站 | 高 |
| 故障注入 | 斷線、磁碟滿、憑證過期、時鐘跳動、舊控制中心 | 中 |
| 效能 | 38 章情境 | 發布前 |

CI 必跑：`go test ./... && go vet`、`cargo test --workspace && cargo clippy -D warnings`、`vp check && vp test`、契約測試、Compose 設定驗證。

## Migration plan

1. 平台資料庫：有序 migration（`core/internal/store/migrations/NNNN_name.sql` 或 `.go`），每個版本記錄 `schemaVersion`；啟動時若資料版本高於執行檔支援版本則拒絕啟動並進入復原狀態（T-009）。
2. 設定與匯出格式：每份匯出帶 `schemaVersion`；未知欄位保留不丟（34.3）。
3. 從 1Panel 匯入（票 091）：唯讀讀取 1Panel 的 SQLite 與檔案，不寫回；對應表 `原 ID → 新 ID → 現場資源` 持久化。
4. 從 infra-manager 匯入（票 063）：Token 明文以雜湊保存查驗值，保留 `token_value` 的辨識規則所需資訊。
5. 大版本升級與資料格式不可逆時，更新前先備份並標示不可降版。

## Hidden assumptions

- SQLite 單一 Core 可承載 38.1 的擴充控制中心規模 — 風險：壓測未過時需換儲存，但不得引入必須登入平台才能啟動的容器（45.2）。
- Komodo Periphery（`v2.3.3`，commit `780ac68`）的 docker／stats／terminal 模組與 `lib/command` 可在新連線模型中重用 — 以原始碼移植並改寫型別，不相依任何 Komodo 或 `mogh_*` crate，因為這些模組都綁著 `komodo_client` 型別；逐 crate 的保留／替換／捨棄與授權見 `agent/UPSTREAM.md`。風險：移植時要一併改寫型別與介面，成本落在票 007、020、027、040～044；Periphery 無 GPU 採集、無通用檔案 API、無 Windows 條件編譯，這些都要新寫。
- 1Panel OSS 的 core 幾乎把整個 `/api/v2` 代理給 agent（約 691 條路由在 agent、72 條在 core）— 風險：「保留 Go 業務邏輯」的範圍比名稱上的 core 大得多，票 092 的基線盤點必須以 agent 路由為主；1Panel 無 Podman／K8s 支援，這兩塊是純新作。
- Zentinel agent filter 能承載 WAF 與可信 IP 判定且延遲可接受 — 風險：WAF 引擎候選均未經本案測試（45.2）。
- PHP 網站需獨立 HTTP runtime（Zentinel 無 FastCGI）— 風險：相容層是發布門檻（19.1）。
- `docker compose` CLI 在目標 OS 可由套件來源安裝且版本固定 — 風險：發行版套件版本落差，需相容矩陣。
- GitHub App 自架流程在純內網無法回呼 — 以 PAT + 定期檢查為替代（GIT-031）。
- 1Panel 商店格式（compose + data.yml + scripts）穩定 — 風險：商店範本變更需跟隨，票 052 固定快取版本。
- Windows Agent 範圍尚未核定 — 不影響 Linux 承諾；`platform-windows` crate 保留介面但不實作。
- 登入第二步的挑戰與 Passkey 儀式狀態只放記憶體 — Core 重啟後要重新輸入密碼；多 Core 不共用（35.1）。
- 未設定可信代理（票 065）前，經反向代理進來的請求來源 IP 都相同 — 密碼登入的「曾成功登入的來源」豁免會失真；第二驗證與再次驗證因此不套用這個豁免，改以每帳號計數限制。
- Passkey 綁定網域；未設定 `--public-origin` 時以請求的 Host 推導，用 IP 開面板時 Passkey 不可用，只能用 TOTP。
- 註冊端點依來源位址計算錯誤的註冊碼（10 分鐘 20 次）；經會終止 TLS 的代理時所有 Agent 來源相同，一個人猜錯太多次會讓同一代理後面的主機 10 分鐘內都不能註冊。
- 複製 VM 的判斷靠「舊連線還回不回 WebSocket ping」：舊連線 3 秒內沒回應就當成同一台重連而換掉。兩份複本若剛好一份斷線時另一份連上，這一輪看不出衝突，要等兩份同時在線才會被發現。
- 經會終止 TLS 的代理時，Core 看到的「舊連線」是代理到 Core 的那一段。代理若自己回 ping，或 Agent 那一段斷了卻還留著往 Core 的連線，正常重連會被誤判為身分衝突，節點要重新註冊才能用。目前還沒有用實際的代理驗證過。

## Decisions

- [0001 — 票以「使用者可以做什麼」切，每條需求只有一張擁有票](decisions/0001-slice-tickets-by-user-outcome.md)
- [0002 — Agent 主動連到 Core](decisions/0002-agent-dials-core.md)
- [0003 — 高權限執行器是獨立行程，只接受具名動作](decisions/0003-privileged-executor-separate-process.md)
- [0004 — 可信 IP 與 WAF 用 Zentinel agent filter 實作](decisions/0004-trusted-ip-and-waf-as-zentinel-agent-filter.md)
- [0005 — TCP／UDP 代理另選元件](decisions/0005-separate-component-for-l4-proxy.md)
- [0006 — 設計方向選 B「航站」，圖表加量](decisions/0006-design-direction-b-terminal.md)
- [0007 — 圖表序列色固定四色](decisions/0007-fixed-four-series-colors.md)
- [0008 — 工作失敗時，全部步驟都能復原才自動復原](decisions/0008-job-rollback-only-when-every-step-can-undo.md)
- [0009 — 驗證中的工作不可取消，Core 中途停止視同狀態不明](decisions/0009-verifying-jobs-cannot-be-cancelled.md)
- [0010 — 登入節流用記憶體計數，冷卻上限 15 分鐘](decisions/0010-login-throttling-in-memory.md)
- [0011 — 不可見範圍的資源回 404，可見但無權才回 403](decisions/0011-invisible-resources-return-404.md)
- [0012 — 工作 API 掛在 IAM 驗證之後](decisions/0012-jobs-api-behind-iam.md)
- [0013 — Kumo 主要與危險按鈕用 !important 改成實心 token](decisions/0013-kumo-button-token-override.md)
- [0014 — 對比、觸控目標、可及名稱用 Playwright 掃描](decisions/0014-accessibility-scan-with-playwright.md)
- [0015 — API 型別產生器放在獨立小套件](decisions/0015-openapi-generator-in-separate-package.md)
- [0016 — 使用者時區先取瀏覽器時區](decisions/0016-user-timezone-from-browser.md)
- [0017 — 主機變更帳本由高權限執行器寫](decisions/0017-host-ledger-written-by-privileged-executor.md)
- [0018 — Core 的帳本副本走專用訊息與短效授權憑證](decisions/0018-ledger-copy-over-dedicated-messages.md)
- [0019 — 有第二驗證的帳號分兩步登入](decisions/0019-two-step-login-with-challenge.md)
- [0020 — 高權限角色沒有第二驗證時限制工作階段](decisions/0020-privileged-roles-require-second-factor.md)
- [0021 — 第二步失敗按帳號計數，一次只處理一個](decisions/0021-second-step-failures-counted-per-account.md)
- [0022 — Passkey 以請求主機名稱為 RP ID](decisions/0022-passkey-rp-id-is-request-host.md)
- [0023 — 主金鑰存在資料目錄的 master.key，多世代保留](decisions/0023-master-key-file-beside-data-dir.md)
- [0024 — 管理 API 的列表格式與錯誤碼統一](decisions/0024-management-api-list-and-error-shape.md)
- [0025 — 版本過期回 412，缺少版本回 428](decisions/0025-version-mismatch-412-and-428.md)
- [0026 — 背景列表查詢結果存獨立表](decisions/0026-background-list-query-results-table.md)
- [0027 — ROB-010 確認只接受套用者本人的新連線](decisions/0027-rob-010-confirm-from-new-connection.md)
- [0028 — Core↔Agent 通道的握手與訊息驗證](decisions/0028-core-agent-channel-handshake.md)
- [0029 — Agent 每個步驟前後先寫 journal 才回報](decisions/0029-agent-step-journal.md)
- [0030 — 握手訊息寬鬆解碼，之後嚴格解碼](decisions/0030-lenient-handshake-strict-after.md)
- [0031 — 節點註冊用一次性碼換憑證，Core CA 由註冊碼驗證，管理員核對後才簽發](decisions/0031-node-enrollment-code-and-ca-discovery.md)
- [0032 — Agent 壓縮已結束的步驟紀錄，被壓縮的步驟仍留下結果](decisions/0032-agent-journal-compaction.md)
- [0033 — 變更計畫的批准綁定內容摘要，批准與執行前都重算計畫](decisions/0033-change-plan-approval-binding.md)
