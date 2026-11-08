# Delivery Status

## Current Phase
M0 基礎與設計：Core 與 Agent 後端骨架（票 003、004、008、009、011、012）、CI（票 006，前端 job 待補）、管理 API 共用契約（票 010）、Core↔Agent 通道（票 007）、第二驗證（票 014）、主機變更帳本（票 113）、設計系統與畫板（票 002，PR #102）、前端骨架（票 005，PR #126）都已合併進 `main`。票 002 仍是 in_progress：實作回報的設計缺口會繼續補畫，之後以新 PR 合併。

## Stage Objective
完成 `DESIGN.md` 與主要畫面原型並通過設計審查，讓票 003、004、005、007 可以在一致的視覺與互動基準上開工；M0 結束時 Core、Agent、Web 骨架可建置並在 CI 通過。

## Active Workstreams
- 設計系統與原型（票 002，PR #102）→ `DESIGN.md`、`docs/design/`（30 個畫板，19 個主導覽項全部有畫面）、`DESIGN-REVIEW.md`（三輪）。
- 1Panel 功能基線盤點（票 092，PR #99）→ `docs/baseline/`。
- CI（票 006，已在 `main`，in_progress）→ `.github/workflows/ci.yml`：Go 與 Rust 在 x86_64、ARM64 原生機器上檢查、測試、建置、冒煙測試並產生依賴報告。剩前端 job 與嵌入前端的 Core 建置，等票 005。
- Core↔Agent 通道（票 007，已在 `main`）→ `docs/contracts/`、`agent/crates/protocol`、Agent 連線與持久步驟紀錄、`core/internal/protocol`、`core/internal/nodes`。SEC-011 的升級路徑移到票 036。票 008 的 ENG-003 Agent 端已由 007 接上。
- 主機變更帳本（票 113，已在 `main`，in_progress）→ `agent/crates/privileged/src/ledger/`：執行器在每個會改主機的動作前後寫帳本，沒寫成就拒絕；有安裝前快照與手動修改偵測。剩下的項目列在 Handoff Notes。
- 前端骨架（票 005，已在 `main`，PR #126 經 #102 合併）→ `web/`：Kumo＋HazelminX 主題、外殼、i18n、深淺色、共用元件（儲存列、主機狀態列、元件停止橫條、原文＋白話摘要）、Playwright 版面與無障礙掃描；Core 嵌入版已實測。CI 還沒有前端 job（票 006）。
- 參考專案研究收尾 → `docs/research/`（1Panel 後端／前端、Komodo Periphery、Portainer Git Stack、Beszel 報告待補）。

## Milestones
| id | target | owner | status | verification_signal |
|---|---|---|---|---|
| M0 | 基礎與設計：設計基準、三個骨架、協定、工作引擎、IAM 基礎、CI | — | in_progress | 票 001–012、092、095 全部 done；CI 綠燈 |
| M1 | 單機主機管理：一行安裝、主機／檔案／磁碟／SSH 管理、更新與卸載 | — | not_started | T-001～T-050 主要案例通過 |
| M2 | 容器、Compose 與 GitHub | — | not_started | T-051～T-066、GH-T01～T34 |
| M3 | 網站入口與安全防護 | — | not_started | T-067、T-078～T-097 |
| M4 | 資料庫、AI、備份與監控 | — | not_started | T-098～T-116 |
| M5 | 多節點、叢集、遷移與發布 | — | not_started | G-01～G-14 門檻 |

## Current Blockers
None。

## Next Verifiable Output
票 019 節點註冊：一次性註冊碼換節點憑證（用票 007 的 `nodes.PKI.IssueNodeCertificate`），寫入 Agent 的 `node.crt`／`core-ca.crt`，並以節點紀錄實作 `nodes.Authorizer` 的撤銷。票 005（PR #126）隨 PR #102（票 002）之後合併；合併後票 006 補前端 job 與嵌入前端的 Core 建置，票 017 在 `web/` 外殼上接工作中心、降級列與搜尋。

## Next Ticket
019 節點註冊（等 007 完成後 ready）、113 的 Core 副本同步（用 007 的通道）。005 前端骨架已完成驗收（PR #126），等 PR #102 合併後再合併。014 已合併；015、016、022、023、024、025、026、047 的前置票都已 done，狀態為 ready；013 的前置票都已 done，狀態為 ready。

## Decision Log
| decision | rationale | timestamp | impacted_ticket_ids |
|---|---|---|---|
| 票以「使用者可以做什麼」切，449 條需求各指派一張擁有票 | 規格第 43 章要求需求→測試→證據可追溯 | 2026-11-06 | 全部 |
| Agent 主動連 Core（mTLS WebSocket），不沿用 Periphery 的 Core 連 Agent 模式 | NOD-002 不要求節點開公網管理 Port | 2026-11-06 | 004、007、019 |
| 高權限執行器獨立行程，只接受具名動作 | 32.2、SEC-006 | 2026-11-06 | 012 |
| 可信 IP 與 WAF 以 Zentinel agent filter 實作；核心 `waf.mode` 不用 | Zentinel 研究：核心 WAF 未接線、只有 X-Forwarded-For 處理 | 2026-11-06 | 064、065 |
| TCP/UDP 代理另選元件 | Zentinel 無 L4 代理 | 2026-11-06 | 060 |
| Windows 節點票標 `scope:pending-approval` | 08.7 為建議範圍 | 2026-11-06 | 093 |
| 設計先出三個方向（風格輪盤、現實標竿、最佳設計師）再選定，不先寫 DESIGN.md | design-studio 的「看得見才能選」原則 | 2026-11-07 | 002、005 |
| 選定方向 B「航站」；圖表加量，依現代控制台做法（表內量表、兩欄時間圖格、部署標記線、狀態時間軸、熱格、最熱排行） | 使用者選擇與要求（2026-11-07） | 2026-11-07 | 002、005、017 |
| 圖表序列色固定四色，深淺兩組通過 dataviz 色弱驗證；小字灰調深到 ≥ 4.5:1 | 可及性硬性限制 | 2026-11-07 | 002、005 |
| 工作失敗時，已執行步驟全部有復原方法才自動復原，否則停在「失敗」並列出殘留；復原失敗為「需要處理」 | 33.1 復原中／已復原／需要處理的語意；不把部分復原偽裝成已復原 | 2026-11-07 | 008 |
| 驗證中的工作不可取消；Core 中途停止視同執行端斷線，工作轉「狀態不明」待對帳 | 步驟已全部完成，結果以驗證為準；重啟不得重設或重跑（ENG-003） | 2026-11-07 | 008 |
| 登入節流用記憶體計數、帳號冷卻上限 15 分鐘、曾成功登入的來源不受帳號冷卻影響 | IAM-012 要求不能永久鎖死管理員；單一 Core 單一程序，重啟只會讓冷卻提早結束 | 2026-11-07 | 009、014 |
| 猜到不可見範圍的資源 ID 回與「不存在」完全相同的 404，可見但無權才回 403 | T-014 要求查詢、下載、搜尋、事件串流都不洩漏存在與否 | 2026-11-07 | 009、008、010 |
| `/management/v1/jobs` 掛在 IAM 驗證之後：讀取要全域 `platform:read`，提交與取消要全域 `platform:write`；各工作種類的權限與範圍由票 010 定 | 合併 008 與 009 時工作 API 原本不需登入；AGENTS.md 要求危險操作在後端驗權 | 2026-11-07 | 008、009、010 |
| 003、004、008 驗收未完成仍先合併進 `main`，狀態維持 in_progress | 使用者決定（2026-11-07）；003 的 CI 條件要等 006，而 006 需要 003 先合併 | 2026-11-07 | 003、004、006、008 |
| Kumo 主要／危險按鈕在 `@layer base` 用 `!important` 改成實心 token＋hazel-ink 字 | Kumo 以 inline style 把 brand 調亮 30% 並鎖白字，深色榛果底對比只有 2.00:1；覆蓋 token 無效 | 2026-11-08 | 005 |
| 對比、觸控目標、可及名稱改用 Playwright 掃描（`web/e2e/scan.ts`），另自算文字對比 | `docs/design/tools/` 只有版面掃描；axe 遇到偽元素與兄弟層的底色只回 incomplete | 2026-11-08 | 005、096 |
| API 型別產生器放在 `web/tools/openapi/` 的獨立小套件 | openapi-typescript 需要 TypeScript 5 的編譯器 API，TypeScript 7 沒有 | 2026-11-08 | 005、010 |
| 「使用者時區」先取瀏覽器時區 | 帳號層級時區設定還沒有擁有票的 API | 2026-11-08 | 005、015、018 |
| 主機變更帳本由高權限執行器寫：每個動作宣告會改的對象，先同步寫意圖行才執行；帳本在 `/var/lib/hazelminx/ledger`（root 0700，上層不得可被他人寫入），每行帶前一行的 SHA-256；Core 拿原始行驗證後保存副本 | 增補 07 第 55.1 節要求沒寫帳本的變更一律拒絕、主機連不上 Core 時仍能只靠本機那份移除 | 2026-11-08 | 113、013、037、007 |
| 113 驗收未完成（缺 Core 的帳本副本）仍先合併進 `main`，狀態維持 in_progress | 使用者決定（2026-11-08）；Core 副本要等 007 的通道，其餘測試都已通過 | 2026-11-08 | 113、007 |
| 有第二驗證的帳號，密碼正確後只拿到一次性挑戰，第二步通過才開工作階段；`setup` 建好擁有者後直接登入 | 使用者核准的介面變更（2026-11-08），對照設計稿 Login／Setup 兩步流程 | 2026-11-08 | 014、005 |
| 票 014 先合併；「知道密碼的人可讓第二步一直冷卻」（與 IAM-012 衝突）之後另開 PR 修 | 使用者決定（2026-11-08）：尚無正式安裝，合併後的風險已比合併前小 | 2026-11-08 | 014、065 |
| 擁有者、平台管理員、主機維運者沒有第二驗證時，工作階段只能走綁定白名單，也不能移除最後一個第二驗證 | IAM-002 高權限帳號的安全預設；主機維運者有終端機與 host:write，等同 root | 2026-11-08 | 014、015、027 |
| 第二步與再次驗證的失敗按帳號計數，不套用「曾登入來源」豁免；同一帳號的第二步一次只處理一個 | 未設可信代理時所有人來源相同；並行猜測會繞過嘗試上限（審查重現 200 次全部被驗證） | 2026-11-08 | 014、065 |
| Passkey 以請求主機名稱為 RP ID，只認完全相同的網域；網域變更前用 `auth/passkeys/domain-impact` 檢查 | WebAuthn RP ID 規則；T-130 要求變更前說明影響 | 2026-11-08 | 014、018 |
| 主金鑰存 `<data-dir>/master.key`（多世代、舊世代永不刪）；SQLite 改 `synchronous=FULL`；`master.key` 存在而 `core.db` 不在視為遺失而非全新安裝 | 35.2 主金鑰不與資料庫同放、輪替後舊備份須可解密；DAT-002 已確認交易不可丟；T-012 不冒用空白庫 | 2026-11-07 | 011、078、013 |
| `/management/v1` 列表統一回 `{items, page}`、以 keyset 游標翻頁；`INVALID_REQUEST`、`JOB_KIND_UNKNOWN` 併入 `VALIDATION_FAILED`，`JOB_NOT_FOUND` 併入 `NOT_FOUND`，`CONFLICT` 改名 `ALREADY_EXISTS` | 使用者決定（2026-11-08）；尚未發版，統一後前端只需處理一套格式 | 2026-11-08 | 010、005、017 |
| 版本過期回 `412 VERSION_MISMATCH`，缺少或 `*` 回 `428 VERSION_REQUIRED`；`409` 只用於資源狀態擋下 | 使用者決定（2026-11-08）；RFC 9110 13.1.1 | 2026-11-08 | 010、015、016 |
| 背景列表查詢結果存獨立表，最多 100,000 筆、保存 7 天 | 使用者決定（2026-11-08）；大結果放在工作紀錄會拖慢工作列表 | 2026-11-08 | 010、017 |
| ROB-010 確認只接受套用者本人、從套用後才建立的連線送出；有反向代理時不成立，前提是管理入口直連 Core | 使用者決定（2026-11-08）；瀏覽器會同時開多條連線，只比連線 ID 不夠 | 2026-11-08 | 010、024、065 |
| Core↔Agent 通道：Agent 撥出到 Core 的 TLS 1.3 WebSocket（預設 `:9801`）；Core 有自己的 Ed25519 CA，私鑰存機密服務；雙方在握手時各自簽雙方 nonce、雙方 X25519 公鑰、協定版本、節點指紋與 session ID；之後每則訊息帶方向序號與 HMAC；直連時另要求 TLS 用戶端憑證與握手身分一致；HTTP Header 一律不當身分 | NOD-002 不開節點入站埠；T-024 經終止 TLS 的代理仍要有獨立節點身分，而且代理不能在握手後插入或改寫訊息（規格 32 章「之後每次收命令也要確認」）；Komodo 的 Noise 握手在 Go 端沒有相容實作 | 2026-11-08 | 007、019 |
| Agent 每個步驟開始前與結束後先寫 append-only `journal.jsonl` 並 fsync 才回報；同一工作步驟有完成或失敗紀錄就重送紀錄不重跑；重開機時中斷的步驟只有 Core 對帳後送來、且標記可重試時才再跑 | ENG-003 不把執行中工作重設成功或重跑；可重試步驟若一律拒絕，對帳後的重跑會永遠卡住 | 2026-11-08 | 007、008 |
| 握手五種訊息寬鬆解碼（忽略未知欄位），握手後訊息嚴格解碼（未知欄位、未知類型都不執行） | NOD-011 不以未知欄位猜測執行；不相容的新舊版本仍要能互相說出支援範圍（T-021） | 2026-11-08 | 007 |
| SEC-011 的升級路徑（遠端更新協定不相容的 Agent）由票 036 交付，票 007 交付交換版本、不相容時拒絕與可讀的拒絕紀錄 | 使用者決定（2026-11-08）；遠端更新屬平台更新流程，需要 036 的版本目錄、安全點與復原 | 2026-11-08 | 007、036 |

## Source Links
- 架構決策：`ENG.md`
- 規格：`docs/spec/HazelminX_Spec_v1.1_zh-TW.md`
- 票務：`docs/tickets/README.md`、GitHub issues #1–#98（milestone M0–M5）
- 草稿 PR：#99（092 基線盤點）
- 已合併 PR：#100（003）、#101（004）、#103（011）、#104（009）、#105（008）、#106（012）、#121（010）、#122（113）、#124（004）、#123（014）、#125（007）、#127（006）、#126（005，併入 #102）、#102（002）
- 設計畫布：<https://claude.ai/artifact/NBXgFGYEiZVnKLraoa7Nt8>（第一頁三個方向、第二頁方向 B 定稿 12 畫板，私人）
- 研究：`docs/research/`
- 工作規則：`AGENTS.md`

## Handoff Notes
- 所有產品測試均未執行；規格裡的「必須」都是要求。
- 參考專案原始碼放在本機暫存目錄，不進 repo；研究報告是線索，實作前以固定 commit 重新核對。
- `web/` 用專案內的 `vite-plus`（`./node_modules/.bin/vp`）；`package.json` 的 `devEngines` 鎖 npm 12.2.2，不要直接跑系統 npm／npx。`node_modules` 放在 SSD 上很慢，開發時可在本機磁碟的 clone 裡跑。
- 在 `/Volumes/SSD` 上 `cargo build` 需設 `CARGO_TARGET_DIR` 到支援檔案鎖的磁碟（project memory 有紀錄）。
- 票 010 新增 migration `0006_management_contract.sql`。其他平行分支若也新增 `0006`，後合併的一方要改號，並同步更新票檔裡的引用。
- `core/internal/api` 的 IAM 測試時鐘原本寫死在 2026-11-07，從 2026-11-08 起整批失敗。票 010（PR #121）已經修正，同時修了 `TestRecoverAfterRestartDoesNotResetOrRerun` 約三成機率失敗的問題。
- 票 113 還沒做完的項目：
  - Core 的帳本副本（UNX-001 最後一項）：007 合併後，把 `agent/crates/privileged/src/ledger/sync.rs` 裡標 `TODO(007)` 的型別搬進 protocol crate，Go 端用 `docs/contracts/fixtures/host-ledger-batch.json` 做契約測試，Core 依節點保存驗證過的行。做完才能把 UNX-001 打勾、113 改成 done。
  - 安裝器（票 013）在第一步執行 `hazelminx-privileged snapshot capture`，安裝步驟用 `ledger::recorder::Recorder` 記錄，並讓 `/var/lib/hazelminx` 維持 root 擁有、群組與其他人不可寫。
  - `docs/tickets/README.md` 與 `traceability.json` 在 `main` 上還沒有 113 那一列（只在票 002 的設計分支）。設計分支合併後，113 要改成 `in_progress`。
  - dnf／rpm 主機只用模擬輸出測過。x86_64 與 ARM64 的 Ubuntu 24.04 已由 CI 以 root 在 systemd 下實測（PR #127）。
  - 主機頁看帳本等六項設計缺口，列在 PR #122 描述的「設計缺口」一節，等設計端補畫。
  - 帳本會改的對象目前只有路徑、systemd 單元、套件、使用者、群組。之後要動防火牆、容器、sysctl、設定鍵的票，要加對應的 `Target` 變體與讀取方式。
- 票 006 還沒做完的項目：
  - 票 005（PR #126）合併後，在 `.github/workflows/ci.yml` 加前端 job：用固定版本的 Vite+ 跑 `vp check`、`vp test`、`vp build`，不能用浮動版本。
  - 同時讓 Core 改用 005 的 `scripts/embed-web.sh` 以 `-tags embedweb` 建置，冒煙測試從「`/` 回 503」改成「`/` 回 `index.html`」。
  - main 分支還沒設定「CI 通過才能合併」的保護規則，要由 repo 管理者在 GitHub 設定。
