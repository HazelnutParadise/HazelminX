# 125 — 管理員可以在畫面上看主機清單、加入新主機，並改名、維護、輪替憑證、撤銷或不再管理主機

**Epic:** 節點與 Agent（`epic:nodes`）
**User Story:** 管理員不用呼叫 API 就能把主機納入管理並處理它的狀態
**Milestone:** M1 單機主機管理
**Blocked by:** 005 使用者可以開啟 React 介面並切換語言與淺深色, 019 管理員可以用一次性註冊碼加入節點，並看到離線、過期、維護與撤銷狀態
**規格章節:** 09｜節點加入、自動偵測與多節點、07｜介面與操作體驗（需求 ID 由票 019 擁有，本票只做畫面）
**Issue:** [#145](https://github.com/HazelnutParadise/hazelminx/issues/145)

## What it delivers

票 019 交了主機與註冊碼的管理 API（`/management/v1/nodes*`、`/management/v1/node-enrollments*`），但 `web/` 的「主機」頁還是空殼。本票做三件事：主機清單、新增主機的流程、每台主機的操作（改名、標籤、群組、維護、輪替憑證、撤銷、不再管理）。外殼頂列下方的「沒有回應的主機」狀態列也由本票接上資料來源，這一條原本記在票 017。

畫板：`design/screens/Nodes.dc.html`（清單）、`AddNode.dc.html`（新增主機）、`NodeOffline.dc.html` 頁首的狀態寫法。主機詳情頁的監控圖表、系統、檔案、終端機、磁碟等分頁不在本票，等各自的後端票。

## Acceptance criteria

證據路徑以 `web/` 為準。單元測試以 `vp test` 執行，e2e 以 `playwright test` 對這個分支建置出來的 Core 執行（`e2e/start-core.sh`），主機那一端用 Core 的 Agent 入口 `POST /agent/v1/enroll` 模擬（`e2e/core.ts` `enrollAs`）。2026-11-12，macOS arm64。驗收審查另外用真的 Rust Agent（`cargo build -p hazelminx-agent`，執行畫面上複製的指令）跑過連線、斷線、輪替與撤銷。

- [x] 主機清單列出每台主機的名稱、連線狀態（連線中、資料過期、沒有回應、從未連線）、主機狀態（撤銷、身分衝突、維護中）、最後聯絡時間、憑證狀態（有效、即將到期、已過期、輪替中）與還沒送到主機的事項（撤銷、移除、憑證輪替、維護後重新檢查）。沒有回應的主機只寫「通道中斷」與最後聯絡時間，畫面上沒有「關機」「已停止」這類字。 — 證據：`src/nodes/status.test.ts`「host status」（每一種狀態、憑證與待送出的順序）；`src/nodes/pages.test.tsx`「shows each host's status, last contact, certificate and what has not reached it」（另外斷言整頁沒有「關機」「已停止」）；e2e `hosts.spec.ts` 看到從未連線、維護中、輪替中 · 待送出、已撤銷 · 待送達、不再管理。驗收審查用真 Agent 看到連線中、沒有回應 + 通道中斷、輪替中 · 等主機改用新憑證。資料過期、身分衝突、憑證即將到期與已過期只有單元測試：真 Core 要等 30 秒沒有訊息、兩台機器同時上線或憑證接近到期才做得出來。
- [x] 清單可以用文字搜尋（名稱、主機名稱、ID 或指紋開頭），並依標籤、群組、狀態篩選。篩選條件寫進網址，重新整理後保留。 — 證據：e2e「search, status and tag filters live in the URL…」（搜尋與狀態寫進網址、重新整理後保留）；`pages.test.tsx`「filters by status from the URL and by search…」「keeps the search text with its trailing space and writes the URL once」；驗收審查用指紋前 8 碼、ID 開頭、標籤 edge、群組 dc-1 實測。
- [x] 一台主機都沒有時，清單顯示空狀態與「新增主機」按鈕。篩選後沒有結果時，寫出目前的條件與「清除篩選」。 — 證據：`pages.test.tsx`「shows the empty state…」「says which conditions matched nothing and clears them」；e2e「…an empty match says why」；驗收審查在全新 Core 看到空狀態。
- [x] 新增主機：填名稱、標籤、群組與有效時間後產生註冊碼。碼只顯示這一次，畫面有可複製的安裝指令與剩餘有效時間。離開畫面或重新整理後，碼不再出現，只剩它的查詢 ID。 — 證據：e2e「add a host: the code is shown once…」（網址不含碼、重新整理後頁面沒有碼）；`pages.test.tsx`「creates a one-time code…」「never shows the code again…」；驗收審查確認剪貼簿內容與畫面一致、倒數會走。指令是 `hazelminx-agent enroll`（見設計缺口 8、9）。
- [x] 產生碼之後畫面等待主機。主機用了碼，畫面顯示它回報的主機名稱、machine-id、作業系統與架構、Agent 版本、來源位址、金鑰指紋，以及它像哪一台既有主機（同一把金鑰或同一個 machine-id）。 — 證據：`pages.test.tsx`「moves from waiting to the check by itself…」「shows what the host reported…」；e2e 同上一條；驗收審查用真 Agent 與模擬主機都看到自動切換，三種「像哪一台」都看過。
- [x] 管理員可以核准或拒絕。主機和既有主機的 machine-id 相同而金鑰不同時，核准前必須選「取代那台主機」或「當成新主機」。金鑰已經被撤銷、衝突或取代時，畫面說明核准會被拒，要主機換新金鑰重新註冊，核准鈕停用。 — 證據：`pages.test.tsx`「requires choosing to replace…」「sends asNewNode…」「blocks approving a retired key…」「rejects the host after a confirmation」；e2e「a new key on a known machine-id replaces that host…」「…a rejected host stays refused」（之後同一個碼再送回 403 `ENROLLMENT_REJECTED`）。
- [x] 還沒被使用的註冊碼可以收回。碼過期、被收回、被拒絕或核對逾時，畫面寫出是哪一種，並提供重新產生。 — 證據：`pages.test.tsx`「withdraws a code nobody used…」與「says when the enrollment ended as …」（過期、拒絕、核對逾時）；e2e 收回與拒絕。核對逾時要等 24 小時，只有單元測試。
- [x] 主機可以改名、改標籤、改群組。別人同時改過同一台主機時（版本不符），畫面說明並以目前的值重新載入，不覆蓋別人的修改。 — 證據：`pages.test.tsx`「renames and re-tags with the version read…」（If-Match `"v3"` 失敗後以 `"v4"` 重送）「asks before closing a dialog with unsaved input」；e2e 改標籤與群組；驗收審查先用 API 改名再從畫面儲存，畫面顯示版本衝突、載入後伺服器端仍是別人的值。
- [x] 主機可以進入維護與結束維護。進入時列出這台主機上還沒完成的工作，說明它們會繼續跑、新的工作會被擋。結束後顯示重新檢查的結果（等待連線、檢查中、通過、失敗、無法判定），失敗時可以重跑。 — 證據：`pages.test.tsx`「starts maintenance and lists the jobs that keep running」「keeps the maintenance result when the host leaves the filtered list」「ends maintenance」「shows a failed re-check after maintenance and runs it again」；e2e 進入與結束維護（沒有工作時寫「沒有還沒完成的工作」）；驗收審查用真 Agent 看到「等待主機連線」與「無法判定」。有工作的清單與重跑只有單元測試：模擬的主機不會開通道，做不出跑到一半的工作。
- [x] 主機可以輪替憑證。畫面顯示新憑證送達前新舊兩張都有效，主機不在線時顯示「待送出」，輪替失敗時顯示原因。 — 證據：`pages.test.tsx`「rotates a certificate and says an offline host gets it later」「shows a rotation that failed with its reason」；e2e 輪替後「輪替中 · 待送出」；驗收審查用真 Agent 實測在線與離線輪替都換上新序號。輪替失敗只有單元測試。
- [x] 撤銷主機要先確認：確認框寫出主機名稱與後果（連線立刻中斷、之後連不回來、新工作被擋、這把金鑰不能再註冊），並要求輸入主機名稱。主機不在線時，撤銷後顯示「待送達」，送達後顯示傳播延遲。 — 證據：`pages.test.tsx`「revokes only after the host's name is typed…」「keeps a half-typed revocation when the host reconnects…」「shows the revocation still waiting…」「shows the propagation delay…」；e2e 撤銷與「已撤銷 · 待送達」；驗收審查用真 Agent 重連被拒（`NODE_REVOKED`）後畫面顯示傳播延遲 36 秒。
- [x] 「不再管理」與另外三種移除方式（移除 Agent、移除叢集節點、刪除工作負載）分開呈現。「不再管理」寫明不會動主機、Agent 與應用；另外三種停用並寫出原因與由哪張票提供。 — 證據：`pages.test.tsx`「keeps the four removal modes apart…」；e2e 移除後只出現在「不再管理」篩選。
- [x] 外殼的主機狀態列從主機清單取得資料：有主機沒有回應時出現，維護中的不算，點了進到篩選成「沒有回應」的清單。 — 證據：`pages.test.tsx`「feeds the shell's host bar…」「has no host bar when every host answers」；驗收審查用真 Agent 停掉後看到「1 台主機沒有回應」，進入維護後消失，「全部主機」連到 `/nodes?status=unreachable`。
- [x] 沒有 `nodes:write` 的使用者看得到主機，寫入按鈕停用並寫出需要的權限。 — 證據：`pages.test.tsx`「disables writes with the reason…」「lets an account with nodes:write on one host change that host from the list」；e2e「an account without nodes:write…」；驗收審查用 auditor 帳號看過清單、新增頁與主機頁。
- [x] 每條驗收都有單元測試，並有對真的 Core 的 e2e：從產生註冊碼、模擬主機用碼、核准，到改名、維護、輪替、撤銷、不再管理。新增的畫面在 1440 深色、360 深色、1440 淺色都通過 axe、對比與版面掃描。 — 證據：`vp test` 12 個檔、247 個測試通過；`playwright test` 131 個通過，`hosts.spec.ts` 的每個畫面都掃 1440 深色、360 深色、1440 淺色，`shell-scan.spec.ts` 另外把 `/nodes`、`/nodes/new` 掃 360 到 1920 寬、深淺色、中英文。版面掃描現在會先等畫面穩定再量，並檢查對話框有沒有被別的元素蓋住（這條檢查在修正前的外殼 CSS 上會失敗）。
- [x] （審查加入）離開新增頁之後，還在等主機的註冊碼與等著核對的主機仍找得到。 — 證據：`/nodes` 的「註冊中」面板（`src/nodes/Enrollments.tsx`），`pages.test.tsx`「lists codes and hosts still enrolling, even before the first host joined」。e2e 的 `/nodes` 掃描在主機流程留下待核對的註冊時會掃到這個面板，但不保證每次都有。

## Delivery record

依規格第 46.4 節，完整內容在 PR 描述（分支 `ticket/125-host-list-and-add-host-screens`）。

## 設計缺口

實作時發現設計稿缺的畫面、狀態或資料來源，由設計端補畫或決定：

1. **Nodes 清單**：畫板的 CPU、記憶體、資料碟、CPU 1 小時、容器欄需要監控數值，API 沒有（票 020）。實作沒有放這幾欄。
2. **Nodes 清單**：引擎環境表、跨主機資源段條、元件矩陣（Zentinel、其他管理工具）、「服務模組」分頁，以及摘要列的「引擎環境」「待套用安全更新」都沒有資料來源。實作的摘要列改成「主機」「憑證要處理」「有待送出事項」三格。
3. **Nodes 清單**：畫板沒有「憑證」「待送出」兩欄，也沒有撤銷、身分衝突、從未連線、不再管理的狀態樣子。實作加了這兩欄（1024 以下隱藏）與這些狀態。
4. **Nodes 篩選**：畫板把標籤與「有問題」混在同一列 chip，另有卡片／表格切換。實作是一列狀態 chip，加上標籤、群組兩個下拉，沒有卡片檢視。「有問題」要包含哪些狀態沒有定義。
5. **Nodes 列尾動作**：畫板是檔案、終端機、更多三顆，檔案與終端機頁還沒有，實作只放「更多」，選單內容（改名與標籤、進入／結束維護、輪替憑證、撤銷、不再管理）畫板沒畫。
6. **Nodes 空狀態與篩選無結果**：畫板沒畫。實作用共用的空狀態區塊，篩選無結果時寫出目前條件與「清除篩選」。
7. **Nodes 手機版**：表格在 360 寬時名稱欄很窄，長名稱會拆行。要不要改成卡片（票 017 對專案表與使用者表已經這樣決定）待設計決定。
8. **AddNode 指令**：畫板是 `curl … install-agent.sh | sudo sh -s -- --core … --code …`，Core 沒有安裝腳本（票 013），實作顯示 `hazelminx-agent enroll --core-url … --code …`。「看安裝腳本內容」「下載離線安裝包」沒有放。
9. **AddNode Agent 位址**：Core 沒有提供主機連回控制中心的公開位址，實作預填「這個網頁的主機名稱 + 9801」並讓使用者改。需要 Core 在平台設定提供這個位址（建議放票 018）。
10. **AddNode 設定**：畫板的「角色（一般／資料主機／只觀測）」與「維護時段」API 沒有，實作沒放。API 有、畫板沒有的「群組」「註冊碼有效時間」實作加上了。
11. **AddNode 核對主機**：畫板的「CPU／記憶體」「偵測到的引擎」不在主機註冊時回報的資料裡，實作改顯示 Agent 版本與來源位址。
12. **AddNode 狀態**：「取代既有主機／當成新主機」的選擇、金鑰已停用不能核准、收回註冊碼、碼過期／被收回／被拒絕／24 小時沒人核對、重新整理後碼不再顯示、有效時間倒數，畫板都沒有。畫板的「還沒連上？」區塊只有標題，實作沒有放。
13. **AddNode 註冊碼格式**：畫板是三組，實際是四組（`HZX-XXXX-XXXX-XXXX-XXXX`，票 019 已記）。
14. **單台主機頁**：畫板 NodeOffline 是事故頁（影響、發生經過、從控制中心檢查、恢復前不能做的事），API 沒有這些資料。實作的主機頁只放身分、連線、主機憑證、維護與重新檢查、撤銷、元件六個面板，工作區分頁不在本票。
15. **操作對話框**：改名與標籤、進入維護（受影響的工作清單）、結束維護、輪替憑證、撤銷（輸入主機名稱確認）、不再管理（四種方式，三種停用並寫原因）都沒有畫板。
16. **撤銷待送達、輪替中與輪替失敗、身分衝突、重新檢查結果**：畫板沒有（票 019 已記），實作用狀態 pill 與主機頁面板呈現。
17. **註冊中面板**：畫板沒有地方列出還在等主機的註冊碼與等著核對的主機，管理員離開新增頁就找不回來。實作在 `/nodes` 主機清單上方加了「註冊中」面板，位置與樣子待設計確認。
18. **執行者名稱**：撤銷與移除紀錄的執行者，API 只給帳號 ID，畫面寫成「帳號 ID …」。要顯示帳號名稱需要 API 補上。
19. **「資料過期」與「沒有回應」**：需求把兩者分開，DESIGN.md「同一件事全站一個名字」卻寫「沒有回應（不叫狀態過期）」。實作照需求分開顯示，頂端主機狀態列把兩者都算進「沒有回應」。要不要統一由設計決定。
20. **主機沒有回應時的寫入**：DESIGN.md 要求寫入類動作停用或「排入，恢復後套用」。實作讓離線主機仍可輪替與撤銷，確認框寫明下次連線時送出或送達，這是 API 的實際行為。要不要改成停用由設計決定。
21. **側欄名稱**：側欄寫「主機與節點」，畫板與頁面標題是「主機」，DESIGN.md 說「節點」只用在 Kubernetes。要不要把側欄改成「主機」待設計決定，本票沒改。

## Notes

- 待查（審查留下，屬於 Core）：核准時選「取代某台主機」，若那台在核准前幾秒被另一位管理員「不再管理」，Core 的取代分支只用 ID 載入節點、不檢查狀態，會把它改回使用中（`core/internal/nodereg/enroll.go` 取代分支）。要不要擋，需要產品決定，交給票 019 的後續。
- e2e 的模擬主機不會開 Agent 通道，所以只會是「從未連線」。要做出連線中、沒有回應、輪替送達與撤銷傳播延遲，可以在本機建置真的 Agent（`cargo build -p hazelminx-agent`），執行畫面上的註冊指令，`--core-url` 改成 e2e Core 的 Agent 位址，再 `run`。
