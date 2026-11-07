# Delivery Status

## Current Phase
M0 基礎與設計：規格已整併、票務已切好、GitHub issue 已開；正在進行設計系統與 UI/UX 原型（票 002）。

## Stage Objective
完成 `DESIGN.md` 與主要畫面原型並通過設計審查，讓票 003、004、005、007 可以在一致的視覺與互動基準上開工；M0 結束時 Core、Agent、Web 骨架可建置並在 CI 通過。

## Active Workstreams
- 設計系統與原型（票 002）→ `DESIGN.md`、`docs/design/`（30 個畫板，19 個主導覽項全部有畫面）、`DESIGN-REVIEW.md`（三輪）。
- 後端 M0 票並行開工：008 工作引擎、009 IAM、011 機密與主金鑰（基底 `ticket/003-core-skeleton`）、012 高權限執行器（基底 `ticket/004-agent-skeleton`），各自獨立 session 與分支，PR 堆疊在骨架 PR 上。
- 1Panel 功能基線盤點（票 092）→ `docs/baseline/`。
- Core／Agent 骨架補完（票 003、004，PR #100、#101）與基線對照表填寫（票 092，PR #99）。

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
`DESIGN.md` 與主要畫面原型（總覽、節點、專案／服務、工作中心、GitHub 來源選擇器、部署檢查、錯誤與空狀態、360px 手機版）通過 `DESIGN-REVIEW.md` 審查。

## Next Ticket
002 設計系統與 UI/UX 原型（進行中）。003、004、092 已有骨架草稿 PR，待補完驗收條件；007 等 003 與 004 合併後開工。

## Decision Log
| decision | rationale | timestamp | impacted_ticket_ids |
|---|---|---|---|
| 票以「使用者可以做什麼」切，449 條需求各指派一張擁有票 | 規格第 43 章要求需求→測試→證據可追溯 | 2026-11-06 | 全部 |
| Agent 主動連 Core（mTLS WebSocket），不沿用 Periphery 的 Core 連 Agent 模式 | NOD-002 不要求節點開公網管理 Port | 2026-11-06 | 004、007、019 |
| 高權限執行器獨立行程，只接受具名動作 | 32.2、SEC-006 | 2026-11-06 | 012 |
| 可信 IP 與 WAF 以 Zentinel agent filter 實作；核心 `waf.mode` 不用 | Zentinel 研究：核心 WAF 未接線、只有 X-Forwarded-For 處理 | 2026-11-06 | 064、065 |
| TCP/UDP 代理另選元件 | Zentinel 無 L4 代理 | 2026-11-06 | 060 |
| Windows 節點票標 `scope:pending-approval` | 08.7 為建議範圍 | 2026-11-06 | 093 |

## Source Links
- 架構決策：`ENG.md`
- 規格：`docs/spec/HazelminX_Spec_v1.1_zh-TW.md`
- 票務：`docs/tickets/README.md`、GitHub issues #1–#98（milestone M0–M5）
- 草稿 PR：#99（092 基線盤點）、#100（003 Core 骨架）、#101（004 Agent 骨架）；002 設計於 `ticket/002-design-system`
- 研究：`docs/research/`
- 工作規則：`AGENTS.md`

## Handoff Notes
- 所有產品測試均未執行；規格裡的「必須」都是要求。
- 參考專案原始碼放在本機暫存目錄，不進 repo；研究報告是線索，實作前以固定 commit 重新核對。
- Vite+ 的 `vp` 尚未安裝在開發機；票 005 開工時先裝全域 CLI（`curl -fsSL https://vite.plus | bash`）。
- 在 `/Volumes/SSD` 上 `cargo build` 需設 `CARGO_TARGET_DIR` 到支援檔案鎖的磁碟（project memory 有紀錄）。
