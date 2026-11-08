# Delivery Status

## Current Phase
M0 基礎與設計：Core 與 Agent 後端骨架（票 003、004、008、009、011、012）與管理 API 共用契約（票 010）已合併進 `main`；設計系統與 UI/UX 原型（票 002）進行中。

## Stage Objective
完成 `DESIGN.md` 與主要畫面原型並通過設計審查，讓票 003、004、005、007 可以在一致的視覺與互動基準上開工；M0 結束時 Core、Agent、Web 骨架可建置並在 CI 通過。

## Active Workstreams
- 設計系統與原型（票 002，PR #102）→ `DESIGN.md`、`docs/design/`、`DESIGN-REVIEW.md`。
- 1Panel 功能基線盤點（票 092，PR #99）→ `docs/baseline/`。
- 已在 `main` 但驗收未完成：票 003 等 CI（票 006）；票 008 的 ENG-003 Agent 端等票 007。
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
票 007 Core↔Agent 協定：Agent 以 mTLS WebSocket 連上 Core，交換協定版本與能力，Core 與 Agent 用同一組黃金 JSON 樣本通過契約測試。PR #102（票 002）合併後，票 005 依 `DESIGN.md` token 建立 `web/` 骨架。

## Next Ticket
007 Core 與 Agent 協定（ready）。005 前端骨架仍等票 002 完成。另外 014、015、022、023、026、047 的前置票都已 done，狀態為 ready；013、016、024、025 還在等 003 或 008 完成驗收；019 等 007。

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
| `/management/v1` 列表統一回 `{items, page}`、以 keyset 游標翻頁；`INVALID_REQUEST`、`JOB_KIND_UNKNOWN` 併入 `VALIDATION_FAILED`，`JOB_NOT_FOUND` 併入 `NOT_FOUND`，`CONFLICT` 改名 `ALREADY_EXISTS` | 使用者決定（2026-11-08）；尚未發版，統一後前端只需處理一套格式 | 2026-11-08 | 010、005、017 |
| 版本過期回 `412 VERSION_MISMATCH`，缺少或 `*` 回 `428 VERSION_REQUIRED`；`409` 只用於資源狀態擋下 | 使用者決定（2026-11-08）；RFC 9110 13.1.1 | 2026-11-08 | 010、015、016 |
| 背景列表查詢結果存獨立表，最多 100,000 筆、保存 7 天 | 使用者決定（2026-11-08）；大結果放在工作紀錄會拖慢工作列表 | 2026-11-08 | 010、017 |
| ROB-010 確認只接受套用者本人、從套用後才建立的連線送出；有反向代理時不成立，前提是管理入口直連 Core | 使用者決定（2026-11-08）；瀏覽器會同時開多條連線，只比連線 ID 不夠 | 2026-11-08 | 010、024、065 |

## Source Links
- 架構決策：`ENG.md`
- 規格：`docs/spec/HazelminX_Spec_v1.1_zh-TW.md`
- 票務：`docs/tickets/README.md`、GitHub issues #1–#98（milestone M0–M5）
- 草稿 PR：#99（092 基線盤點）、#102（002 設計方向）
- 已合併 PR：#100（003）、#101（004）、#103（011）、#104（009）、#105（008）、#106（012）、#121（010）
- 設計畫布：<https://claude.ai/artifact/NBXgFGYEiZVnKLraoa7Nt8>（第一頁三個方向、第二頁方向 B 定稿 12 畫板，私人）
- 研究：`docs/research/`
- 工作規則：`AGENTS.md`

## Handoff Notes
- 所有產品測試均未執行；規格裡的「必須」都是要求。
- 參考專案原始碼放在本機暫存目錄，不進 repo；研究報告是線索，實作前以固定 commit 重新核對。
- Vite+ 的 `vp` 尚未安裝在開發機；票 005 開工時先裝全域 CLI（`curl -fsSL https://vite.plus | bash`）。
- 在 `/Volumes/SSD` 上 `cargo build` 需設 `CARGO_TARGET_DIR` 到支援檔案鎖的磁碟（project memory 有紀錄）。
- 票 010 新增 migration `0006_management_contract.sql`。其他平行分支若也新增 `0006`，後合併的一方要改號，並同步更新票檔裡的引用。
- `core/internal/api` 的 IAM 測試時鐘原本寫死在 2026-11-07，從 2026-11-08 起整批失敗。票 010（PR #121）已經修正，同時修了 `TestRecoverAfterRestartDoesNotResetOrRerun` 約三成機率失敗的問題。
