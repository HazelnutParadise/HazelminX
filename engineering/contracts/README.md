# Core↔Agent 通道契約

Agent 主動連到 Core 的 Agent 入口（預設 `:9801`，路徑 `/agent/v1/connect`），在 TLS 上開 WebSocket，每個文字訊框一則 JSON 訊息。Rust 型別在 `agent/crates/protocol`，Go 型別在 `core/internal/protocol`；兩邊都用本目錄 `fixtures/` 的黃金樣本做契約測試，任何一邊改了形狀，兩邊的測試會一起失敗。

## 身分

- Core 安裝時產生自己的 CA（Ed25519），CA 私鑰與 Core 身分私鑰存在機密服務（`internal/secrets`），不落明文檔。
- Core 身分憑證：CA 簽發，EKU `serverAuth`，DNS SAN `core.hazelminx.internal`。Agent 驗 Core 時只看憑證鏈與這個名稱，不看 URL 的 IP 或主機名，所以 Core 換 IP 不影響身分。
- 節點憑證：CA 簽發，EKU `clientAuth`，CN 是節點指紋（節點 Ed25519 公鑰原始 32 位元組的 SHA-256 小寫 hex）。節點私鑰不離開節點。
- Agent 端檔案：`state_dir/identity/node.key`、`node.crt`、`core-ca.crt`、`privileged-issuer.pub`（後三者由註冊寫入），以及 `state_dir/enrollment.json`（`coreUrl`、`nodeId`、`enrolledAt`；設定檔沒有 `core_url` 時用這裡的網址）。

## 註冊

管理員在 Core 產生一次性註冊碼，在主機上執行 `hazelminx-agent enroll --core-url <Agent 入口網址> --code <註冊碼>`。決策紀錄見 `decisions/0031`。

### 註冊碼

- 形式是 `HZX-` 加四組、每組 4 個字元，例如 `HZX-7Q4M-2KD9-PX81-C3VB`。字元表是 Crockford base32：`0123456789ABCDEFGHJKMNPQRSTVWXYZ`。
- 正規化：去掉 `-` 與空白，轉大寫，`O` 換成 `0`，`I`、`L` 換成 `1`；結果必須是 `HZX` 加 16 個字元表內的字元。
- 第一組（例子裡的 `7Q4M`）是查詢 ID `codeId`，取 CA 時以未驗證的 TLS 送出，視為公開。後三組 12 個字元是秘密（60 位元）。
- 金鑰 `key` 是 `SHA-256("hazelminx-enroll-v1\n" + 正規化後的碼)` 的 32 位元組。Core 只存這個值，不存碼。
- 碼只能用一次，有效期預設 10 分鐘，可以設 1 到 60 分鐘。碼建立時可以指定群組。

### 步驟

1. **取得 Core CA**：`GET /agent/v1/enroll/discovery?codeId=7Q4M`。這一步 Agent 還沒有 CA，`pinned` 模式下不驗 TLS 伺服端憑證；`proxy` 模式照常驗代理的憑證鏈。回應 `200 {"caCertificate": "<PEM>", "mac": "<base64>"}`。`mac` 是 `HMAC-SHA256(key, "hazelminx-enroll-discovery-v1\n" + caCertificate 的位元組)`，標準 base64。Agent 驗不過就停止，不送出註冊碼。查不到可用的碼時回 `404`，錯誤碼 `ENROLLMENT_CODE_INVALID`，不說明原因，而且和送錯碼一樣計入下方的次數限制。
2. **送出註冊碼**：`POST /agent/v1/enroll`。`pinned` 模式下以上一步的 CA 驗 TLS，名稱是 `core.hazelminx.internal`。請求內容：

   | 欄位 | 說明 |
   |---|---|
   | `code` | 註冊碼，可以帶 `-` |
   | `nodePublicKey` | 節點 Ed25519 公鑰原始 32 位元組，base64url 不補 `=` |
   | `hostname`、`machineId`、`os`、`arch`、`agentVersion` | 給管理員核對主機用，各自最多 256 個字元；`machineId` 取 `/etc/machine-id`，讀不到時是空字串 |
   | `group` | 選填。帶了就必須和碼的群組相同 |

   未知欄位、重複的鍵都是錯誤。
3. **等管理員核對**：第一次送出時 Core 把碼標成「已使用」，記下這把公鑰與主機資料，回 `202 {"status": "pending", "enrollmentId": "...", "retryAfterSeconds": 5}`。Agent 用同一個碼、同一把公鑰重送同一個請求來詢問結果。核對的等待期限是 24 小時，從碼被使用的時間起算，不受碼的有效期影響。
4. **核准**：回 `200 {"status": "approved", "enrollmentId", "nodeId", "nodeCertificate", "caCertificate", "privilegedIssuerKey"}`。Agent 確認 `caCertificate` 和第一步拿到的相同、`nodeCertificate` 由這張 CA 簽發而且 CN 是自己的指紋，才寫入檔案。核准之後重送同一個請求會拿到同一張憑證。

回應一律是 JSON，帶 `Content-Length`。被拒絕時是 `{"status": "refused", "code", "message"}`：

| HTTP | code | 意思 |
|---|---|---|
| 400 | `ENROLLMENT_REQUEST_INVALID` | 請求格式不對 |
| 403 | `ENROLLMENT_CODE_INVALID` | 沒有這個碼，或秘密部分不符 |
| 403 | `ENROLLMENT_CODE_EXPIRED` | 碼已過有效期，還沒被使用 |
| 403 | `ENROLLMENT_CODE_USED` | 碼已經被另一把公鑰使用 |
| 403 | `ENROLLMENT_CODE_REVOKED` | 管理員作廢了這個碼 |
| 403 | `ENROLLMENT_GROUP_MISMATCH` | `group` 和碼的群組不同 |
| 403 | `ENROLLMENT_REJECTED` | 管理員拒絕了這台主機 |
| 403 | `ENROLLMENT_EXPIRED` | 24 小時內沒有人核對 |
| 403 | `NODE_KEY_REVOKED` | 這把公鑰屬於已撤銷、已被取代或身分衝突的節點；要用 `--new-identity` 換一把金鑰再註冊 |
| 429 | `TOO_MANY_ATTEMPTS` | 同一來源 10 分鐘內送了 20 次不存在或不對的碼（取 CA 與送出註冊碼合計） |

被拒絕的請求不會改變碼的狀態，也不會延長有效期。

### 同一台主機再註冊

- 公鑰和既有節點相同：核准後沿用該節點，重新簽發憑證，舊憑證立即失效。
- 公鑰和已移除（「不再管理」）的節點相同：核准後恢復該節點。
- 新公鑰，但 `machineId` 和既有節點相同：管理員核准時必須選「取代該節點」或「建立新節點」。取代時節點 ID 不變，舊公鑰從此被拒絕。

## 握手

連線建立後依序交換四則訊息，任何一步失敗就送 `reject` 並關閉。

| 順序 | 方向 | type | 重點欄位 |
|---|---|---|---|
| 1 | Agent→Core | `hello` | `protocol`（支援範圍 min/max）、`agentVersion`、`nodeCertificate`（PEM）、`nonce`、`kex`、`capabilities` |
| 2 | Core→Agent | `welcome` 或 `reject` | `protocolVersion`（協商結果）、`coreCertificate`、`nonce`、`kex`、`sessionId`、`proof`、`heartbeatSeconds`、`capabilities` |
| 3 | Agent→Core | `auth` | `proof` |
| 4 | Core→Agent | `ready` | `sessionId` |

- 協商取雙方範圍交集的最大版本；沒有交集回 `reject`，`code` 為 `PROTOCOL_INCOMPATIBLE`，`supported` 帶 Core 的範圍。
- `nonce` 是 32 位元組亂數，base64url 不補 `=`。`kex` 是每次連線新產生的 X25519 公鑰（32 位元組，base64url 不補 `=`）。`proof` 是 Ed25519 簽章，標準 base64。
- 簽章內容（逐行，結尾有換行）：

  ```text
  hazelminx-session-v1
  {role}
  {agentNonce}
  {coreNonce}
  {agentKex}
  {coreKex}
  {protocolVersion}
  {nodeFingerprint}
  {sessionId}
  {helloDigest}
  {welcomeDigest}
  ```

  `helloDigest` 是 hello 原文（Agent 送出、Core 收到的那段文字，逐位元組）的 SHA-256 小寫 hex。`welcomeDigest` 是 welcome 其餘參數的 SHA-256 小寫 hex，用不依賴 JSON 格式的長度前綴編碼：依序寫入 `lp("hazelminx-welcome-v1")`、`lp(coreVersion)`、`heartbeatSeconds` 的 4 位元組大端序、能力數量的 4 位元組大端序，再對每個能力寫 `lp(name)` 與 `version` 的 4 位元組大端序；`lp(x)` 是 x 的位元組長度（4 位元組大端序）接 x 本身。

  `role` 是 `core` 或 `agent`。Core 以身分私鑰簽 `core` 版本，Agent 以節點私鑰簽 `agent` 版本。雙方 nonce 都是新的，所以舊握手無法重放；角色字串不同，所以一方的簽章不能拿來冒充另一方；兩個 `kex`、整份 hello 與 welcome 的參數都在簽章內，所以中間人換掉任何一方的 `kex`、改支援範圍、能力清單或心跳間隔，簽章就對不上。
- 節點身分以 `nodeCertificate`＋`proof` 證明，不依賴 TLS 用戶端憑證，也不讀任何 HTTP Header。經會終止 TLS 的代理時仍成立；直連時 Agent 另外出示 TLS 用戶端憑證，Core 要求兩者指紋一致。
- Agent 的 `core_transport = "pinned"`（預設）以 `core-ca.crt` 驗 TLS 伺服端；`"proxy"` 以代理的憑證鏈驗 TLS（`proxy_ca_file`，不設則用作業系統的信任清單），Core 身分改由 `coreCertificate`＋`proof` 證明。
- 雙方以自己的 X25519 私鑰和對方的 `kex` 算出共同秘密，再用 HKDF-SHA256（不加鹽）展開成兩把 32 位元組金鑰：`info` 是 `hazelminx-session-v1 core->agent {sessionId}` 的金鑰給 Core 送出的訊息用，`hazelminx-session-v1 agent->core {sessionId}` 的給 Agent 送出的訊息用。代理看得到握手內容，但算不出這兩把金鑰。
- 握手訊息單則上限 64 KiB，hello 要在連線後 3 秒內送到；Core 同時進行中的握手上限 256 條、同一來源（IPv4 位址或 IPv6 /64）32 條，滿了就關掉一條讓新連線進來：先關還沒送 hello 的，最後才關已出示有效憑證、在等 auth 的，同一階段關最舊的；簽章驗過的連線不再佔名額；被拒紀錄只保留 Agent 提供文字的前 256 個字元。
- `reject` 沒有簽章：經會終止 TLS 的代理可以偽造一則 `reject`，讓 Agent 以為被拒而等待重試。代理本來就能直接切斷連線，這只是同一種阻斷，不會讓任何指令被執行。

## 訊框

`ready` 之後，雙方送出的每則訊息都包在一個訊框裡：

```json
{"n": 1, "body": "{\"type\":\"ack\",\"seq\":17}", "mac": "<base64>"}
```

- `n` 是這個方向的訊框序號，從 1 起每則加 1；收方只接受下一個號碼。
- `body` 是訊息本身的 JSON 文字（字串，不是物件），握手後訊息的形狀與規則都適用在它上面。
- `mac` 是 HMAC-SHA256，金鑰是送方方向的那把，內容依序是 `hazelminx-frame-v1\n`、`n` 的 8 位元組大端序、`body` 的 UTF-8 位元組，標準 base64。
- 訊框多或少欄位、重複的鍵、序號不對、MAC 不符、二進位訊框，收方一律直接關閉會話，不看內容、不回 `protocol_error`。這擋住經會終止 TLS 的代理插入、重放或調換訊息。
- 握手後單則訊框上限 1 MiB。Agent 回報的步驟結果超過 256 KiB 時改記為失敗 `RESULT_TOO_LARGE`。
- 訊框只保證完整性與順序，內容沒有另外加密：直連時有 TLS 保護，經會終止 TLS 的代理時代理看得到內容。機密不得以明文放進步驟參數或結果。

### 版本規則

- 握手五種訊息（`hello`、`welcome`、`reject`、`auth`、`ready`）在所有協定版本形狀不變；解碼時忽略未知欄位，讓新舊版本至少能互相說出「不相容」與支援範圍。
- 握手完成後的訊息嚴格解碼：未知欄位、未知 `type`、缺欄位都是錯誤，收方回 `protocol_error`，不執行任何動作。

## 握手後訊息

| type | 方向 | 用途 |
|---|---|---|
| `heartbeat` | 雙向 | 每 `heartbeatSeconds` 一次；超過三個週期沒收到任何訊息就斷線 |
| `step_execute` | Core→Agent | 執行一個工作步驟：`requestId`、`jobId`、`step`、`stepName`、`action`、`actionVersion`、`params`（物件）、`targetNode`、`controlGeneration`、`expiresAt`、`retryable`、`irreversible` |
| `step_rejected` | Agent→Core | 沒有執行就拒絕，也用來回答 Agent 自己的紀錄有問題的情況：`code`、`message`，不支援時附 `supported` 能力清單 |
| `step_reconcile` | Core→Agent | 查詢 Agent 持久紀錄中某步驟的結果，不執行 |
| `reconcile_report` | Agent→Core | `started`、`completed`、`failed`、`evidence`、`error` |
| `event` | Agent→Core | 持久事件：`seq`（每個節點單調遞增，跨重啟不重複）、`kind`（`started`／`completed`／`failed`）、`jobId`、`step`、`requestId`、`at`、`evidence`、`result`、`error` |
| `step_progress` | Agent→Core | 非持久的進度與日誌；通道塞住時丟棄並以 `logsDropped` 計數 |
| `ack` | Core→Agent | 累計確認到 `seq` 為止的事件；Agent 重連後重送未確認事件 |
| `protocol_error` | 雙向 | 收到無法解讀的訊息 |
| `ledger_pull` | Core→Agent | 要主機變更帳本 `afterSeq` 之後的行：`requestId`、`afterSeq`、`limit`、`grant`（給高權限執行器的授權憑證，見下方「主機變更帳本」） |
| `ledger_page` | Agent→Core | 一頁帳本原始行：`requestId`、`ledgerId`、`afterSeq`、`lines`、`head`（`seq`、`hash`）、`more` |
| `ledger_unavailable` | Agent→Core | 拿不到這一頁：`requestId`、`code`、`message` |
| `certificate_renew` | Core→Agent | 換發節點憑證：`requestId`、`certificate`（PEM），見下方「憑證輪替」 |
| `certificate_installed` | Agent→Core | 新憑證已驗證並寫入：`requestId` |
| `certificate_refused` | Agent→Core | 沒有換：`requestId`、`code`（`CERTIFICATE_INVALID`、`WRITE_FAILED`）、`message` |

### 能力

`capabilities` 是 `{name, version}` 清單。協定 v1 的能力：

| name | version | 說明 |
|---|---|---|
| `jobs` | 1 | `step_execute`、`step_reconcile`、`event`、`ack` |
| `agent.info` | 1 | 唯讀動作：回報 Agent 版本、節點指紋、作業系統、架構、主機名 |
| `host.ledger` | 1 | `ledger_pull`、`ledger_page`、`ledger_unavailable` |
| `node.certificate` | 1 | `certificate_renew`、`certificate_installed`、`certificate_refused` |

Core 派送前先比對能力；Agent 沒宣告或版本較低的動作，Core 不送出，直接讓步驟失敗為 `CAPABILITY_UNSUPPORTED`。Agent 收到未宣告的動作、多餘參數欄位、別的節點、已過期或控制世代倒退的步驟，一律回 `step_rejected`，不執行。

### `step_rejected` 代碼

| code | 意思 |
|---|---|
| `UNSUPPORTED_ACTION` | 動作或動作版本不在 Agent 能力內 |
| `INVALID_PARAMS` | 參數不是物件、缺欄位或有未知欄位 |
| `WRONG_NODE` | `targetNode` 不是這個節點的指紋 |
| `EXPIRED` | 已超過 `expiresAt` |
| `STALE_CONTROL_GENERATION` | `controlGeneration` 小於 Agent 已見過的最大值 |
| `OUTCOME_UNKNOWN` | 紀錄顯示這個步驟開始過但沒有結束（例如 Agent 中途重開機），要先對帳；請求標記可重試時才會再跑 |
| `JOURNAL_UNAVAILABLE` | Agent 寫不了步驟紀錄（例如磁碟滿），所以什麼都沒做；可以稍後重試 |

同一個 `jobId`＋`step` 已有完成或失敗紀錄時，Agent 不重跑，直接重送那筆持久事件。步驟跑完卻寫不進紀錄時，Agent 結束會話；重連後 Core 對帳，看到的是「開始過、沒有結束」。

Core 收到 `step_reconcile` 的答案前，Agent 若還在跑那個步驟，會等步驟結束才回答；Core 在會話存活期間定期再問，直到拿到答案。

### 主機變更帳本

主機上的帳本由高權限執行器寫，只有 root 讀得到（規格增補 07 第 55.1 節）。Core 保存一份副本，做法是向 Agent 一頁一頁要：

1. Core 為每一頁簽一張授權憑證：權限只有 `host.ledger.read`，有效 5 分鐘，subject 是 `system:ledger-sync`。
2. Core 送 `ledger_pull`，`afterSeq` 是 Core 副本最後一行的序號，第一次是 0。
3. Agent 把 `grant` 原封不動交給執行器的 `ledger_export` 動作，再把執行器回的原始行放進 `ledger_page`。
4. Core 驗證後才保存。`more` 是 true 時繼續要下一頁。

`grant` 的欄位名稱是執行器自己的格式（`id`、`subject`、`permissions`、`not_before`、`expires_at`、`signature`），和本協定其他欄位的 camelCase 不同。簽章內容是逐行文字：

```text
hazelminx-grant-v1
{id}
{subject}
{not_before}
{expires_at}
{permissions 以逗號連接}
```

時間是 RFC 3339 UTC、只到整秒。`fixtures/grant.json` 是 Rust 端用固定金鑰簽的一張憑證，Go 端要算出同一個簽章。

`lines` 的每一行和主機檔案裡的那一行逐位元組相同：`{"hash":"<hex>","entry":{…}}`。Core 照下面的規則驗證，任何一條不符，整頁都不保存：

- `hash` 是 `entry` 原始位元組的 SHA-256。驗證時直接拿原始位元組算，不重新序列化。
- 每一行只能有 `hash` 和 `entry` 兩個鍵，而且不能重複。
- `entry.seq` 從上一頁的結尾接續，一次加 1。
- `entry.prev_hash` 等於前一行的 `hash`。第一行接 `sha256("hazelminx-ledger-v1\n" + ledgerId)`。
- `more` 是 false 時，最後一行要等於 `head`。

`ledgerId` 和 Core 副本記錄的不同時，代表主機的帳本被重建或換掉了。Core 不保存新帳本的任何一行，並記錄 `LEDGER_REPLACED`，等人決定怎麼處理。

執行器每頁最多回 500 行，Agent 另外要求一頁的行加起來不超過 192 KiB。原因是行裡的引號在訊息裡跳脫一次，在訊框裡再跳脫一次，傳輸時一頁最多會變成四倍大。編碼後的訊息仍超過 480 KiB 時，Agent 用一半的額度重新要一次。

`ledger_unavailable` 代碼：

| code | 意思 |
|---|---|
| `EXECUTOR_UNREACHABLE` | Agent 連不上執行器的 socket |
| `EXECUTOR_REFUSED` | 執行器拒絕匯出（憑證、權限或參數不對），`message` 帶執行器的錯誤碼 |
| `EXPORT_FAILED` | 執行器接受了但匯出失敗，例如帳本沒有開啟，或 `afterSeq` 超過主機帳本的最後一行（主機帳本比 Core 副本短） |

### 憑證輪替

1. Core 為同一把節點公鑰簽一張新憑證，記為「待確認」。從這時起 Core 同時接受舊憑證與新憑證。
2. Core 送 `certificate_renew`。Agent 確認新憑證由自己信任的 Core CA 簽發、用途是 `clientAuth`、CN 是自己的指紋，把它寫成 `node.crt.next`，回 `certificate_installed`，然後正常結束會話。
3. Agent 下一次連線用新憑證。握手成功（收到 `ready`）後才把 `node.crt.next` 換成 `node.crt`，舊的留在 `node.crt.prev`。
4. Core 看到這個節點用新憑證完成握手，才停止接受舊憑證。
5. 用新憑證連線被拒或 TLS 失敗時，Agent 刪掉 `node.crt.next`，改用舊憑證重連。舊憑證在 Core 仍然有效，所以通道不會中斷在一個雙方都不認的狀態。

Core 在節點憑證剩不到 30 天時自動輪替，管理員也可以手動要求。節點離線時，輪替要求留到下一次連線。

### `reject` 代碼

`PROTOCOL_INCOMPATIBLE`、`HANDSHAKE_MALFORMED`、`IDENTITY_INVALID`（憑證鏈、用途或指紋不符，或這張憑證已被換掉，或節點沒有註冊）、`PROOF_INVALID`、`NODE_REVOKED`（節點已撤銷、已移除，或這把金鑰已被取代）、`IDENTITY_CONFLICT`（同一把節點金鑰同時出現在兩個仍在運作的連線，例如複製的 VM；兩邊都被拒絕，直到管理員重新註冊）、`DUPLICATE_SESSION`（同一節點已有一條仍有回應的連線）。

同一節點的第二條連線完成簽章驗證時，Core 對舊連線送一次 WebSocket ping。3 秒內沒有回應，Core 直接切斷舊連線（不等關閉交握），讓新連線進來（Agent 重開或網路中斷後重連都是這種情況）。有回應，就表示同一把金鑰有兩份在運作：Core 有節點名冊時關掉兩邊、把節點標成身分衝突並回 `IDENTITY_CONFLICT`，沒有名冊時只拒絕新連線並回 `DUPLICATE_SESSION`。

## 樣本

- `fixtures/messages/*.json`：每種訊息一則合法樣本，兩邊解碼後再編碼要得到同樣的 JSON 值。
- `fixtures/invalid/*.json`：必須被拒絕的訊息，檔名說明原因。
- `fixtures/handshake-lenient/*.json`：帶未知欄位、仍須被握手解碼接受的訊息。
- `fixtures/signature.json`：固定種子的節點、Core 與 X25519 金鑰，hello 原文與兩個雜湊、簽章內容、預期簽章、共同秘密與兩方向的會話金鑰（Ed25519 是確定性簽章，兩邊必須算出同一個值）。
- `fixtures/frames.json`：用上面的會話金鑰封好的訊框。兩邊要算出同一個 `mac`、還原出同一個 `body`；訊框的 JSON 文字本身可能不同（Go 會把 `<`、`&` 這類字元跳脫），所以不比對整段文字。改任何一處都要被拒絕。
- `fixtures/grant.json`：給高權限執行器的授權憑證，用固定種子（公開，只供測試）簽，含簽章內容與公鑰 PEM。
- `fixtures/host-ledger-batch.json`：執行器 `ledger_export` 回的一頁真實帳本行，`messages/ledger_page.json` 用的是同一組行。
- `fixtures/enrollment.json`：固定的註冊碼、正規化結果、`key`、一張 CA 的 PEM 與預期的 `mac`，兩邊要算出同樣的值；另含必須被拒絕的碼。
- `fixtures/pki/`：測試用 CA、Core 與節點憑證（僅供測試，私鑰公開，不得用於任何部署）。
