# Delivery Status

## Current Phase
M0 基礎與設計：Core 與 Agent 後端骨架（票 003、004、008、009、011、012）已合併進 `main`；Core↔Agent 通道（票 007）驗收完成，在草稿 PR #125 等合併；設計系統與 UI/UX 原型（票 002）進行中。

## Stage Objective
完成 `DESIGN.md` 與主要畫面原型並通過設計審查，讓票 003、004、005、007 可以在一致的視覺與互動基準上開工；M0 結束時 Core、Agent、Web 骨架可建置並在 CI 通過。

## Active Workstreams
- 設計系統與原型（票 002，PR #102）→ `DESIGN.md`、`docs/design/`、`DESIGN-REVIEW.md`。
- 1Panel 功能基線盤點（票 092，PR #99）→ `docs/baseline/`。
- Core↔Agent 通道（票 007，分支 `ticket/007-core-agent-protocol`）→ `docs/contracts/`、`agent/crates/protocol`、Agent 連線與持久步驟紀錄、`core/internal/protocol`、`core/internal/nodes`。驗收全部附證據；SEC-011 的升級路徑移到票 036。
- 已在 `main` 但驗收未完成：票 003 等 CI（票 006）；票 004 缺 Komodo Periphery 改造起點的 crate 與授權清單、閒置 CPU 與記憶體量測。票 008 的 ENG-003 Agent 端已由票 007 接上，008 改為 done。
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
票 019 節點註冊：一次性註冊碼換節點憑證（用票 007 的 `nodes.PKI.IssueNodeCertificate`），寫入 Agent 的 `node.crt`／`core-ca.crt`，並以節點紀錄實作 `nodes.Authorizer` 的撤銷。PR #102（票 002）合併後，票 005 依 `DESIGN.md` token 建立 `web/` 骨架。

## Next Ticket
007 合併後 019 才能開工（還等 007 done）；票 008 done 後 024、025 的前置票都已 done，改為 ready。005 前端骨架仍等票 002 完成。另外 014、022、023、026 的前置票都已 done，狀態改為 ready；010、013、024、025 還在等 003、004 或 008 完成驗收。

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
| 主金鑰存 `<data-dir>/master.key`（多世代、舊世代永不刪）；SQLite 改 `synchronous=FULL`；`master.key` 存在而 `core.db` 不在視為遺失而非全新安裝 | 35.2 主金鑰不與資料庫同放、輪替後舊備份須可解密；DAT-002 已確認交易不可丟；T-012 不冒用空白庫 | 2026-11-07 | 011、078、013 |
| Core↔Agent 通道：Agent 撥出到 Core 的 TLS 1.3 WebSocket（預設 `:9801`）；Core 有自己的 Ed25519 CA，私鑰存機密服務；雙方在握手時各自簽雙方 nonce、雙方 X25519 公鑰、協定版本、節點指紋與 session ID；之後每則訊息帶方向序號與 HMAC；直連時另要求 TLS 用戶端憑證與握手身分一致；HTTP Header 一律不當身分 | NOD-002 不開節點入站埠；T-024 經終止 TLS 的代理仍要有獨立節點身分，而且代理不能在握手後插入或改寫訊息（規格 32 章「之後每次收命令也要確認」）；Komodo 的 Noise 握手在 Go 端沒有相容實作 | 2026-11-08 | 007、019 |
| Agent 每個步驟開始前與結束後先寫 append-only `journal.jsonl` 並 fsync 才回報；同一工作步驟有完成或失敗紀錄就重送紀錄不重跑；重開機時中斷的步驟只有 Core 對帳後送來、且標記可重試時才再跑 | ENG-003 不把執行中工作重設成功或重跑；可重試步驟若一律拒絕，對帳後的重跑會永遠卡住 | 2026-11-08 | 007、008 |
| 握手五種訊息寬鬆解碼（忽略未知欄位），握手後訊息嚴格解碼（未知欄位、未知類型都不執行） | NOD-011 不以未知欄位猜測執行；不相容的新舊版本仍要能互相說出支援範圍（T-021） | 2026-11-08 | 007 |
| SEC-011 的升級路徑（遠端更新協定不相容的 Agent）由票 036 交付，票 007 交付交換版本、不相容時拒絕與可讀的拒絕紀錄 | 使用者決定（2026-11-08）；遠端更新屬平台更新流程，需要 036 的版本目錄、安全點與復原 | 2026-11-08 | 007、036 |

## Source Links
- 架構決策：`ENG.md`
- 規格：`docs/spec/HazelminX_Spec_v1.1_zh-TW.md`
- 票務：`docs/tickets/README.md`、GitHub issues #1–#98（milestone M0–M5）
- 草稿 PR：#99（092 基線盤點）、#102（002 設計方向）、#125（007 Core↔Agent 通道）
- 已合併 PR：#100（003）、#101（004）、#103（011）、#104（009）、#105（008）、#106（012）
- 設計畫布：<https://claude.ai/artifact/NBXgFGYEiZVnKLraoa7Nt8>（第一頁三個方向、第二頁方向 B 定稿 12 畫板，私人）
- 研究：`docs/research/`
- 工作規則：`AGENTS.md`

## Handoff Notes
- 所有產品測試均未執行；規格裡的「必須」都是要求。
- 參考專案原始碼放在本機暫存目錄，不進 repo；研究報告是線索，實作前以固定 commit 重新核對。
- Vite+ 的 `vp` 尚未安裝在開發機；票 005 開工時先裝全域 CLI（`curl -fsSL https://vite.plus | bash`）。
- 在 `/Volumes/SSD` 上 `cargo build` 需設 `CARGO_TARGET_DIR` 到支援檔案鎖的磁碟（project memory 有紀錄）。
