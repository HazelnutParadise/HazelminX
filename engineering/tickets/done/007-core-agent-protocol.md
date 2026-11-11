# 007 — Core 與 Agent 可以用版本化、雙向驗證的通道交換能力與工作事件

**Epic:** 節點與 Agent（`epic:nodes`）  
**User Story:** 管理員可以把主機納入管理  
**Milestone:** M0 基礎與設計  
**Blocked by:** 003 管理員可以啟動 hazelminx-core 並通過健康檢查, 004 管理員可以在主機上以 systemd 執行 hazelminx-agent 並回報身分  
**規格章節:** 32｜節點通訊、信任與高權限操作  
**Issue:** [#7](https://github.com/HazelnutParadise/hazelminx/issues/7)  

## What it delivers

agent/crates/protocol 與 core 內對應套件定義：訊息格式與版本、能力宣告、工作指派／進度／證據事件序號、心跳、mTLS 雙向身分、重連對帳；Go 與 Rust 以契約測試互驗。

## Acceptance criteria

證據路徑以 repo 根目錄為準。Go 以 `go test -race ./...`（`core/`）執行；Rust 以 `cargo test --workspace --features hazelminx-agent/test-actions`（`agent/`）執行；互通測試以真的 `hazelminx-agent` 執行檔（`--features test-actions` 建置）連 Go 的 hub，指令見 `agent/README.md`。2026-11-08，macOS arm64（Go 1.26.5、Rust 1.97.0），Rust 另在 Linux 容器（buildpack-deps:trixie，aarch64）跑過一次。

- [x] **NOD-002** Agent 可以主動連回 Core，並在斷線後重連。 — 完成條件：不需在每台節點開公網管理連接埠；重連不重跑已完成工作。 — 證據：Agent 只撥出、不監聽任何連接埠（`agent/crates/agent/src/connection/mod.rs` `dial`）；`core/internal/nodes/interop_test.go` `TestInteropConnectionLossDuringStepReconcilesWithoutRerun`（步驟執行中 Core 端切斷連線 → 工作變 `unknown` → Agent 依退避重連、重送未確認事件 → Core 先對帳、接受完成紀錄、工作成功，步驟只跑過一次）、`TestInteropAgentCrashMidStepIsNotRerunOrMarkedDone`；Core 端以假 Agent 驗同一流程：`core/internal/nodes/runner_test.go` `TestReconnectReconcilesInsteadOfRerunning`；重連等待在 0.1 秒到上限之間隨機（上限 2 秒起加倍到 60 秒）：`connection::tests::backoff_grows_to_cap_with_full_jitter_and_resets_after_stable_session`；重連時步驟還在跑，Core 在會話存活期間持續對帳直到拿到結果：`TestInteropStepStillRunningAtReconnectSettlesInSession`、`TestReconcilerKeepsAskingWhileStepStillRuns`；會話中途變成狀態不明的工作也會被對帳：`TestJobThatTurnsUnknownDuringASessionIsReconciled`；Agent 寫不了紀錄時拒絕或斷線而不是沉默：`TestInteropUnwritableJournalRefusesTheStep`、`TestInteropJournalFailingMidStepEndsSessionForReconcile`。
- [x] **NOD-011** Core 可以與版本不同但相容的 Agent 協作。 — 完成條件：握手交換協定及能力版本；不支援的操作先拒絕，不以未知欄位猜測執行。 — 證據：握手交換 `protocol` 範圍與 `capabilities`，取交集最大版本（`core/internal/nodes/hub_test.go` `TestHandshakeProvesBothSides`、Go `protocol_test.go` 與 Rust `crates/protocol/tests/contract.rs` 的 negotiate 測試）；Agent 沒宣告的能力或版本較低時 Core 不送出（`TestRunnerRefusesUndeclaredCapabilityWithoutSending`）；Agent 收到未宣告的動作、多餘參數欄位一律拒絕不執行（`agent/crates/agent/src/steps/tests.rs` 的 UNSUPPORTED_ACTION／INVALID_PARAMS 測試）；握手後的訊息遇到未知欄位、未知類型、缺欄位都不執行，回 `protocol_error`（兩語言都以 `engineering/contracts/fixtures/invalid/` 驗；`TestUnreadableSessionMessageIsNotActedOn`）；握手訊息忽略未知欄位，讓未來版本仍能說出不相容（`fixtures/handshake-lenient/`）。
- [x] **SEC-011** 維護者可以更新 Core 與 Agent 的不同版本。 — 完成條件：交換協定與能力版本；不相容時拒絕變更，只保留已驗證的讀取與升級路徑。 — 已完成：交換版本與能力（同 NOD-011）；不相容時拒絕連線，不建立會話，任何步驟都送不到該 Agent（`TestIncompatibleProtocolIsRefusedWithSupportedRange`、`TestInteropIncompatibleProtocolIsRefusedAndShown`）；讀取路徑：握手五種訊息形狀凍結且寬鬆解碼，被拒的 Agent 版本與雙方協定範圍可由 `GET /management/v1/agent-connections` 讀到。升級路徑：不相容 Agent 的遠端更新由票 036 交付，見 036 的驗收條件（使用者 2026-11-08 決定）。
- [x] 協定不相容時拒絕操作並顯示支援範圍，不猜測執行未知欄位 — 證據：Core 回 `reject`（`PROTOCOL_INCOMPATIBLE`，`supported` 帶 Core 範圍），並記錄 Agent 版本、雙方範圍與身分是否已證明；`GET /management/v1/agent-connections` 的 `rejections` 回傳這些欄位（需要 `platform:read`）；Agent 日誌寫出雙方範圍並等 10 分鐘再試（`TestInteropIncompatibleProtocolIsRefusedAndShown` 確認 3 秒內沒有重試）。介面上的顯示待 UI 票（見 PR 的設計缺口）。
- [x] 經會終止 TLS 的代理時仍有獨立節點身分證明 — 證據：節點身分以 Core 簽發的節點憑證加上節點私鑰對握手內容（雙方 nonce、協定版本、節點指紋、session ID）的簽章證明，不依賴 TLS 用戶端憑證，也不讀任何 HTTP Header；`TestThroughTLSTerminatingProxyIdentityRestsOnProofNotHeaders`（偽造 Header 加其他 CA 的憑證 → `IDENTITY_INVALID`；有憑證沒私鑰 → `PROOF_INVALID`；真節點經代理 → 連上且標記為 `proxied`）、`TestInteropThroughTLSTerminatingProxy`（真 Agent 以 `core_transport = "proxy"` 經終止 TLS 的代理連上並完成工作）；舊握手不能重放：`TestOldProofCannotBeReplayed`。握手後每則訊息帶方向序號與 HMAC（金鑰由握手的 X25519 交換導出，交換的公鑰在雙方簽章內），代理即使轉送了握手，也無法插入、重放或調換訊息：`TestForgedOrReplayedFrameEndsSession`、`TestSwappedKexBreaksTheProof`；簽章也涵蓋整份 hello 與 welcome 的參數（`TestHelloAndWelcomeDigestsMatchFixture`），代理改不了支援範圍、能力或心跳；匿名連線佔不住握手名額，即使跟真 Agent 來自同一個位址（例如同一個代理）：`TestSilentConnectionsCannotLockAgentsOut`、`TestHandshakeSlotsEvictTheOldestPerSourceAndInTotal`、`TestFloodDoesNotEvictACertifiedHandshake`；兩語言以 `engineering/contracts/fixtures/frames.json` 與 `signature.json` 的金鑰向量互驗。

## Acceptance tests（第 42 章）

- **T-018** Core IP 改變但憑證身分合法。 → 經設定變更後正常重連，不把 IP 當唯一身分。 — 通過：`TestInteropConnectInspectAndAddressChange`（Agent 先以 `localhost` 連上並完成 `node.inspect`，改設定成 `127.0.0.1` 重啟後以同一組信任材料重連）、`TestCoreReachableUnderAnotherAddressKeepsIdentity`。Agent 驗 Core 時固定檢查 `core.hazelminx.internal` 名稱與 Core CA，不看 URL 的主機。
- **T-021** Core 與 Agent 協定不相容。 → 明確顯示不支援，沒有誤解參數執行。 — 通過（API 與日誌層）：`TestIncompatibleProtocolIsRefusedWithSupportedRange`、`TestInteropIncompatibleProtocolIsRefusedAndShown`。UI 顯示待設計。
- **T-024** 經會終止 TLS 的代理連 Agent。 → 不接受偽造節點 Header；仍有獨立身分證明。 — 通過：`TestThroughTLSTerminatingProxyIdentityRestsOnProofNotHeaders`、`TestInteropThroughTLSTerminatingProxy`。

## Delivery record

- 需求 ID：NOD-002、NOD-011、SEC-011 完成；SEC-011 的升級路徑由票 036 交付。另把票 008 ENG-003 的 Agent 端接上（見 008 票檔）。
- 變更：契約與黃金樣本 `engineering/contracts/`；Rust `agent/crates/protocol`；Agent 連線（TLS、握手、重連、會話）`connection/`、持久步驟紀錄 `journal/`、步驟判斷 `steps/`、動作 `actions.rs`；Go `core/internal/protocol`、`core/internal/nodes`（PKI、hub、步驟執行器、重連對帳、`node.inspect` 工作種類）、`GET /management/v1/agent-connections`、Core 新增 `-agent-listen`（預設 `0.0.0.0:9801`）。
- 影響範圍：Core 啟動時在機密服務建立 `node-pki/ca`、`node-pki/core` 兩筆機密；新增一個對外 TLS 監聽埠；沒有資料庫 migration；Agent 設定新增 `core_transport`、`proxy_ca_file`，`core_url` 只接受 `https://`／`wss://`。
- 未驗證：真實 Linux 主機上 systemd 啟動的 Agent 連線（本票在 macOS 與 Linux 容器驗）；長時間運作下 `journal.jsonl` 的大小（目前不壓縮）；`step_progress` 的背壓（目前沒有會輸出日誌的動作）；首次建立 PKI 時兩筆機密之間失敗會留下半套，需要人工清除（見 PR）。
