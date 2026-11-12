# 019 — 管理員可以用一次性註冊碼加入節點，並看到離線、過期、維護與撤銷狀態

**Epic:** 節點與 Agent（`epic:nodes`）  
**User Story:** 管理員可以把主機納入管理  
**Milestone:** M1 單機主機管理  
**Blocked by:** 004 管理員可以在主機上以 systemd 執行 hazelminx-agent 並回報身分, 007 Core 與 Agent 可以用版本化、雙向驗證的通道交換能力與工作事件, 009 擁有者可以登入、建立帳號與角色，未授權的 API 呼叫一律被拒絕  
**規格章節:** 09｜節點加入、自動偵測與多節點、32｜節點通訊、信任與高權限操作  
**Issue:** [#19](https://github.com/HazelnutParadise/hazelminx/issues/19)  

## What it delivers

註冊碼（期限、次數、群組）、節點身分與指紋、憑證輪替與撤銷、複製 VM 身分衝突偵測、心跳與過期顯示、維護模式、移除節點的四種語意、重連退避與對帳。

## Acceptance criteria

證據路徑以 repo 根目錄為準。Go 以 `go test -race ./...`（`core/`）執行；Rust 以 `cargo test --workspace`（`agent/`）執行；真 Agent 的端到端測試以 `cargo build -p hazelminx-agent --features test-actions` 建置後，`HAZELMINX_AGENT_BIN=<執行檔> go test ./internal/nodereg ./internal/nodes -run Interop` 執行。2026-11-12，macOS arm64。

- [x] **NOD-001** 管理員可以用短效、單次註冊碼加入節點。 — 完成條件：碼使用後失效；註冊可核對主機身分；重複使用不建立第二個節點。 — 證據：`core/internal/nodereg/enroll_test.go` `TestEnrollmentWaitsForApprovalThenIssuesTheCertificate`（碼預設 10 分鐘；主機送出後停在「等待核對」，管理員看得到主機名稱、machine-id、來源位址與金鑰指紋；同一主機重送拿到同一筆、核准後拿到同一張憑證，始終只有一個節點）；`TestCodesThatCannotBeUsed`（另一把金鑰用同一個碼 → `ENROLLMENT_CODE_USED`）；真 Agent：`core/internal/nodereg/interop_test.go` `TestInteropEnrollConnectRotateRevoke`（`hazelminx-agent enroll` 取 CA、驗 HMAC、等核准、寫入憑證後 `run` 連回；第二台主機用同一個碼被拒）；管理 API：`core/internal/api/nodes_test.go` `TestNodeEnrollmentAndLifecycleOverTheAPI`（碼只在建立時回一次）。
- [x] **NOD-005** 管理員可以命名、標記、分組與搜尋節點。 — 完成條件：節點身分使用持久 ID／金鑰，不以會改變的 IP 或主機名稱作唯一識別。 — 證據：`core/internal/nodereg/nodes_test.go` `TestNamesTagsGroupsAndSearch`（改名、標籤、群組、依名稱／標籤／群組／指紋搜尋，改名不改指紋與 ID）；`TestReEnrollment`（換金鑰後節點 ID 不變）；`PATCH /management/v1/nodes/{id}` 要 `If-Match`（`TestNodeEnrollmentAndLifecycleOverTheAPI`）；只看得到有權限的節點（`TestNodeScopesOverTheAPI`）。
- [x] **NOD-006** 管理員可以撤銷、輪替與重新註冊節點憑證。 — 完成條件：被撤銷節點不能接受新工作；複製磁碟產生的重複身分會被阻止或要求重新註冊。 — 證據：撤銷 `TestRevokeOnlineNode`（步驟被 `NODE_REVOKED` 擋下，沒有送出：`core/internal/nodes/registry_hooks_test.go` `TestRunnerGateRefusesBeforeSending`）；輪替 `TestCertificateRotation`、`TestInteropEnrollConnectRotateRevoke`；重新註冊 `TestReEnrollment`、`TestConflictIsResolvedByReEnrolling`；重複身分 `TestInteropClonedNodeIsAConflict`（複製狀態目錄的第二份同時上線 → 節點變 `identity_conflict`，兩份都斷線並收到 `IDENTITY_CONFLICT`）；被取代的金鑰不能再註冊或連線：`TestApprovingAClaimWhoseKeyWasRetiredMeanwhileIsRefused`。
- [x] **NOD-007** 管理員可以知道節點離線、資料過期與局部故障。 — 完成條件：顯示最後聯絡時間；離線不等於所有應用已停止；不偽造即時數值。 — 證據：`TestConnectionState`（`never_connected`、`online`、超過 30 秒沒訊息 `stale`、斷線後 `offline` 並保留真正的最後聯絡時間）；API 文件寫明 `offline` 只表示通道中斷；節點回應不含任何即時指標，元件清單與憑證輪替錯誤、時鐘誤差各自顯示（`TestComponentsListAgentAndCertificate`、`TestRotationFailureKeepsTheOldCertificate`、`TestClockSkewIsShown`）。各引擎、各服務的局部故障要等 NOD-003（票 039）與服務模型的票。
- [ ] **NOD-008** 管理員可以讓節點進入維護模式。 — 完成條件：阻止新的衝突工作，列出受影響服務；維護結束重新檢查能力與健康。 — 已完成：維護中建立的工作一律被擋（`NODE_IN_MAINTENANCE`，什麼都沒送），之前的工作照跑；開啟時回傳節點上未完成的工作（`TestMaintenance`）。維護結束時切斷會話，Agent 重連並重新宣告能力，時間記為 `maintenance.checkedAt`；接著自動跑 `node.recheck` 工作，核對回應的是這個節點的 Agent，並讀主機的開機時間、負載、記憶體與根目錄檔案系統，結果 `passed`／`failed`／`inconclusive` 與原始數值放在 `maintenance.recheck`，失敗可以用 `POST /management/v1/nodes/{id}/recheck` 重跑（`core/internal/nodereg/recheck_test.go`、`core/internal/nodes/recheck_test.go`、真 Agent `TestInteropRecheckReadsHostBasics`、`agent/crates/agent/src/actions.rs` 的 `host_health_reports_values_or_null_never_made_up`）。**未完成：**「列出受影響服務」。平台還沒有服務的資料模型，目前列的是工作；使用者 2026-11-12 決定等票 044 有服務後再補，交接寫在 044。
- [x] **NOD-009** 管理員可以移除節點而不刪除其應用。 — 完成條件：明確分開「不再管理」「移除 Agent」「移除叢集節點」「刪除工作負載」。 — 證據：`POST /management/v1/nodes/{id}/remove` 的 `mode` 分成 `unmanage`、`remove-agent`、`remove-cluster-node`、`delete-workloads`（`TestRemoveModes`、`TestNodeEnrollmentAndLifecycleOverTheAPI`）。`unmanage` 只撤掉信任、不碰主機與應用；其他三種回 `NOT_AVAILABLE` 並說明由哪張票提供（037、043／087、各應用的刪除），交接寫在票 037。
- [x] **SEC-001** 管理員可以使用一次性碼加入節點。 — 完成條件：過期、已使用、錯群組與撤銷的碼不能重用；重試不產生多個節點。 — 證據：`TestCodesThatCannotBeUsed`（四種各有錯誤碼、被拒不改變碼的狀態、不延長期限、沒有節點）、`TestRejectedHostCannotRetry`、`TestPendingApprovalExpires`、`TestWrongCodesFromOneSourceAreLimited`、`TestDiscoveryOfUnknownCodesCountsAsWrongCodes`；兩邊算出同樣的碼雜湊與 HMAC：`core/internal/protocol/enroll_test.go`、`agent/crates/protocol/tests/enrollment.rs`（同一份 `engineering/contracts/fixtures/enrollment.json`）。
- [x] **SEC-002** 系統可以輪替節點憑證。 — 完成條件：先驗證新憑證可用，再撤回舊憑證；有到期告警，時鐘誤差明確顯示。 — 證據：`TestCertificateRotation`（新舊序號都接受，直到節點用新憑證握手才停用舊的）；真 Agent `TestInteropEnrollConnectRotateRevoke`（Agent 存 `node.crt.next`、重連、握手成功才換成 `node.crt`，舊序號隨後被拒）；剩不到 30 天時 `certificate.state = expiring` 並在連線時自動輪替（`TestRotationOnConnectAndExpiryWarning`）；Agent 心跳的時鐘差顯示在 `connection.clockSkewSeconds`，超過 60 秒有 `clockSkewWarning`（`TestHeartbeatShowsClockSkew`、`TestClockSkewIsShown`）。「告警」目前是狀態欄位，通知要等票 081。
- [x] **SEC-003** 管理員可以撤銷節點。 — 完成條件：撤銷後不能收新命令或讀新秘密；現有連線依政策關閉；記錄傳播延遲。 — 證據：撤銷時立即關閉會話並記錄 `sessionClosedAt`、`agentNotifiedAt`、`propagationSeconds`（`TestRevokeOnlineNode`、`TestInteropEnrollConnectRotateRevoke`）；之後的連線被拒、步驟被擋；握手中途被撤銷的會話也會被關掉（`TestSessionThatStartsAfterRevocationIsClosed`）。Agent 能拿到的秘密目前只有帳本同步的短效授權，只發給在線會話，所以撤銷後拿不到新的。
- [x] **SEC-004** Agent 可以拒絕不屬於自己的操作。 — 完成條件：其他節點、錯誤資源範圍、過期或舊控制世代的命令均被拒絕。 — 證據：別的節點、過期、控制世代倒退的步驟被拒（票 007 交付：`agent/crates/agent/src/steps/tests.rs` 的 `decide_wrong_node_*`、`decide_expired_*`、`decide_*stale*`）。錯誤資源範圍：`step_execute` 帶工作在這個節點保留的資源（`jobs` 能力 v2），每個動作宣告它的參數會碰哪些資源，碰到沒保留的就回 `WRONG_RESOURCE_SCOPE`、不寫紀錄、不執行（`step_outside_its_resources_is_refused_and_not_journaled`、`unclean_or_unnamed_resources_are_refused`、`every_writing_action_declares_what_it_touches`、真 Agent `TestInteropStepOutsideItsResourcesIsRefused`）；Core 只送屬於這個節點的資源，叫不出名字的資源不送出步驟（`TestRunnerSendsTheJobsResourcesOnThisNode`、`core/internal/jobs/wire_test.go`）；兩語言的契約樣本都更新（`fixtures/messages/step_execute.json`、`fixtures/invalid/step_execute_*`）。資源本身該不該由這個工作碰（擁有者），使用者 2026-11-12 確認由票 090 的 ARC-005 處理。
- [x] **SEC-008** 管理員可以在連線中斷後恢復節點通道。 — 完成條件：使用有上限的重試與等待，不造成連線風暴；重連後先對帳工作，不盲目重送。 — 證據：重連等待 0.1 秒到上限之間隨機、上限 2 秒起加倍到 60 秒、需要人處理的拒絕等 5 到 10 分鐘（`connection::tests::backoff_grows_to_cap_with_full_jitter_and_resets_after_stable_session`、`operator_refusals_wait_ten_minutes`）；重連後先對帳（`TestInteropConnectionLossDuringStepReconcilesWithoutRerun` 等，票 007）。本票另外讓 Core 在舊連線沒回應時直接換掉，重連不會被拒或卡住（`TestDuplicateWhoseOldSessionIsDeadReplacesIt`、`TestReplacingASilentSessionLeavesTimeForReady`）。
- [x] 40 秒內顯示狀態過期，不斷言主機已關機 — 證據：超過 30 秒沒收到訊息就是 `stale`，Core 也在 30 秒時關閉沒有心跳的會話，之後是 `offline`（`TestConnectionState`，以假時鐘驗）。狀態值只描述通道，沒有「關機」。
- [x] 被撤銷節點不能接受新工作；離線節點顯示待同步 — 證據：`TestRevokeOnlineNode`、`TestRunnerGateRefusesBeforeSending`；`TestRevokeOfflineNodeIsPendingUntilItCalls`（離線撤銷時 `pendingSync` 有 `revocation`，節點下次連線被拒後清掉，傳播延遲 90 秒）；憑證輪替、維護結束的重新檢查、移除也列在 `pendingSync`。
- [x] （增補 03 51.6）設計時決定並寫進本票：節點頁「元件清單」的內容與資料來源（銜接 NOD-003、NOD-011）。 — 決定寫在 Notes「節點頁元件清單」；目前的 API 回 `components` 的前兩項（`TestComponentsListAgentAndCertificate`）。

## Acceptance tests（第 42 章）

- **T-016** 重用過期與已使用加入碼。 → 不建立節點、不延長有效期。 — 通過：`TestCodesThatCannotBeUsed`、`TestInteropEnrollConnectRotateRevoke`。
- **T-017** 複製含相同 Agent 私鑰的 VM 同時上線。 → 辨識身分衝突，要求重新註冊，不混合資料。 — 通過：`TestInteropClonedNodeIsAConflict`（真 Agent）、`TestDuplicateWhoseOldSessionAnswersIsAConflict`、`TestConflictIsResolvedByReEnrolling`。
- **T-019** 撤銷在線節點後繼續送命令。 → 依撤銷政策切斷或拒絕，顯示完成時間。 — 通過：`TestRevokeOnlineNode`、`TestInteropEnrollConnectRotateRevoke`。
- **T-020** 撤銷離線節點或其使用者權限。 → 顯示待同步範圍；重連後不執行舊命令。 — 節點部分通過：`TestRevokeOfflineNodeIsPendingUntilItCalls`（重連在握手就被拒，什麼都不執行）。「撤銷使用者權限」屬於 IAM，本票沒有驗。
- **T-022** Agent 連線進行中輪替憑證。 → 新身分驗證成功再切換；失敗可保留安全通道。 — 通過：成功路徑 `TestInteropEnrollConnectRotateRevoke`（真 Agent）；失敗路徑 `TestRotationFailureKeepsTheOldCertificate`、`TestRotationIsDeliveredAgainAfterTheAgentFellBack`、`connection::tests::renewed_certificate_survives_two_blips_but_not_a_refusal`、`tls::tests::renewed_certificate_is_tried_then_promoted_or_discarded`。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。

## Notes

### 票 113 交過來的項目

- 節點註冊時把 Core 資料目錄的 `privileged-issuer.pub` 放到主機的 `/etc/hazelminx/privileged/issuer.pub`（與票 013 相同）。
- 用票 007 的 `nodes.PKI.IssueNodeCertificate` 以一次性註冊碼換節點憑證，寫入 Agent 的 `node.crt`／`core-ca.crt`，並以節點紀錄實作 `nodes.Authorizer` 的撤銷。
- 設計審查留下：加入節點的註冊碼對話框與「等待連線」狀態要做出來。

### 節點頁元件清單（增補 03 51.6 的決定）

`GET /management/v1/nodes/{id}` 的 `components` 是節點頁的元件清單。每一項有 `kind`、`name`、`version`、`state`、`source`（資料從哪裡來）、`detail`。項目與來源：

| kind | 內容 | 資料來源 | 由哪張票提供 |
|---|---|---|---|
| `agent` | Agent 版本、協商的協定版本、Agent 支援的協定範圍、宣告的能力；狀態跟著連線狀態 | 最近一次握手（`hello`），存在節點紀錄 | 019，已提供 |
| `node-certificate` | 序號、到期時間、金鑰指紋；狀態 `valid`／`expiring`／`expired` | 節點名冊 | 019，已提供 |
| `privileged-executor` | 高權限執行器版本與是否可用 | Agent 經 socket 問執行器的健康狀態，由 Agent 回報 | 020（主機資訊）或 012 的後續 |
| `zentinel` | Zentinel 版本、執行狀態、設定版本 | Agent 的唯讀偵測 | 055 |
| `management-tool` | 偵測到的其他管理工具（1Panel、Portainer、Cockpit、Webmin…），一個工具一項 | Agent 的唯讀偵測（ROB-012） | 042／107 |

版本與能力的比對（NOD-011）放在 `agent` 這一項：Agent 比 Core 新或舊時，畫面從 `detail.agentProtocol` 與 Core 的範圍判斷。偵測不到的元件不列，不用空值或「未安裝」冒充偵測結果。

### 交付時的結果與留給後續的事

- **NOD-008 未完成的部分**：「列出受影響服務」目前回的是節點上未完成的工作，因為平台還沒有服務的資料模型。使用者 2026-11-12 決定等票 044 有服務後再補（交接寫在 044 的 Notes）；那之前本票留在最上層，不移到 `done/`。主機的長期監控仍是票 020。
- **SEC-004 的範圍**：Agent 只比對名字，不追 symlink；不同種類的資源不互相涵蓋（保留磁碟不等於保留磁碟上的路徑）。之後新增會改主機的動作（票 021 起），要在 `ActionSpec.touches` 宣告它碰的資源，Core 的工作要保留同一種資源。
- 前端（另一張票）要用的 API：`GET/PATCH /management/v1/nodes{/id}`、`POST /management/v1/nodes/{id}/revoke`、`POST .../rotate-certificate`、`PUT .../maintenance`、`POST .../remove`、`POST .../recheck`、`POST/GET /management/v1/node-enrollments`、`GET/DELETE /management/v1/node-enrollments/{id}`、`POST .../approve`（`replaceNodeId`／`asNewNode`）、`POST .../reject`。畫板「等待主機連線」可以輪詢 `GET /management/v1/node-enrollments/{id}`，`state` 從 `waiting` 變 `pending_approval` 時顯示主機資料與 `matchingNodes`。`matchingNodes` 的 `keyRetired` 為 true 時，這把金鑰曾被撤銷、身分衝突或被取代（原因在 `keyRetiredReason`），核准一定會被拒，畫面要提示主機換新金鑰重新註冊。
- 設計缺口：畫板的註冊碼是三組（`HZX-7Q4M-2KD9-PX81`），實際是四組（`HZX-XXXX-XXXX-XXXX-XXXX`，決策 0031）；畫板沒有「同一台機器已有節點，要取代還是新建」的選擇、身分衝突狀態、撤銷待同步、憑證輪替中與失敗的樣子。
- 已知風險（寫在 `engineering/architecture.md` 的前提假設）：經代理時同一來源猜錯碼太多次會擋住其他主機 10 分鐘；經代理時複製偵測可能誤判。
