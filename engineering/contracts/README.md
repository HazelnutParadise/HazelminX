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
| 403 | `NODE_KEY_REVOKED` | 這把公鑰屬於已撤銷、已被取代或身分衝突的節點（節點之後被移除也一樣）；要用 `--new-identity` 換一把金鑰再註冊 |
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
| `step_execute` | Core→Agent | 執行一個工作步驟：`requestId`、`jobId`、`step`、`stepName`、`action`、`actionVersion`、`params`（物件）、`targetNode`、`resources`（見下方「資源範圍」）、`controlGeneration`、`expiresAt`、`retryable`、`irreversible` |
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
| `host_facts` | Agent→Core | 主機資訊：`collectedAt`、`facts`（見下方「主機資訊與指標」） |
| `metrics_report` | Agent→Core | Core 還沒確認的樣本，由舊到新：`samples`、`droppedSamples` |
| `metrics_ack` | Core→Agent | `seq` 以前的樣本都已保存 |
| `host_storage` | Agent→Core | 儲存清單：`collectedAt`、`storage`（見下方「儲存清單與用量」） |
| `storage_usage` | Agent→Core | 最近一輪用量量測：`usage` |
| `engines_probe` | Core→Agent | 要主機上的容器引擎：`requestId`、`grant`（帶 `host.engines.read`），見下方「容器引擎」 |
| `engines_report` | Agent→Core | 執行器的引擎報告原樣轉送：`requestId`、`collectedAt`、`report` |
| `engines_unavailable` | Agent→Core | 拿不到報告：`requestId`、`code`（`EXECUTOR_UNREACHABLE`、`EXECUTOR_REFUSED`、`INSPECT_FAILED`）、`message` |

### 能力

`capabilities` 是 `{name, version}` 清單。協定 v1 的能力：

| name | version | 說明 |
|---|---|---|
| `jobs` | 2 | `step_execute`（v2 起帶 `resources`）、`step_reconcile`、`event`、`ack` |
| `agent.info` | 1 | 唯讀動作：回報 Agent 版本、節點指紋、作業系統、架構、主機名 |
| `host.health` | 1 | 唯讀動作：`collectedAt`、`uptimeSeconds`、`load`（`one`、`five`、`fifteen`）、`memory`（`totalBytes`、`availableBytes`）、`rootFilesystem`（`totalBytes`、`freeBytes`）；讀不到的值是 `null`，不補值。Core 在節點離開維護後的 `node.recheck` 工作用它 |
| `host.ledger` | 1 | `ledger_pull`、`ledger_page`、`ledger_unavailable` |
| `node.certificate` | 1 | `certificate_renew`、`certificate_installed`、`certificate_refused` |
| `host.metrics` | 1 | `host_facts`、`metrics_report`、`metrics_ack`。雙方都宣告時 Agent 才送；Core 沒宣告時 Agent 不送 |
| `host.storage` | 1 | `host_storage`、`storage_usage`。雙方都宣告時 Agent 才送；Core 沒宣告時 Agent 不送 |
| `host.engines` | 1 | `engines_probe`、`engines_report`、`engines_unavailable`。Agent 宣告時 Core 才送 `engines_probe` |

Core 派送前先比對能力；Agent 沒宣告或版本較低的動作，Core 不送出，直接讓步驟失敗為 `CAPABILITY_UNSUPPORTED`。Agent 收到未宣告的動作、多餘參數欄位、別的節點、已過期、控制世代倒退或碰到工作沒保留的資源的步驟，一律回 `step_rejected`，不執行。

### `step_rejected` 代碼

| code | 意思 |
|---|---|
| `UNSUPPORTED_ACTION` | 動作或動作版本不在 Agent 能力內 |
| `INVALID_PARAMS` | 參數不是物件、缺欄位或有未知欄位 |
| `WRONG_NODE` | `targetNode` 不是這個節點的指紋 |
| `EXPIRED` | 已超過 `expiresAt` |
| `STALE_CONTROL_GENERATION` | `controlGeneration` 小於 Agent 已見過的最大值 |
| `OUTCOME_UNKNOWN` | 紀錄顯示這個步驟開始過但沒有結束（例如 Agent 中途重開機），要先對帳；請求標記可重試時才會再跑。步驟已完成、但紀錄已壓縮掉只剩結果時也回這個碼，Core 對帳後會看到「已完成」 |
| `JOURNAL_UNAVAILABLE` | Agent 寫不了步驟紀錄（例如磁碟滿），所以什麼都沒做；可以稍後重試 |
| `WRONG_RESOURCE_SCOPE` | 動作會碰到 `resources` 沒列的資源，或 `resources` 裡有沒名字、不乾淨的項目 |

### 資源範圍

`step_execute` 的 `resources` 是工作在這個節點保留的資源，每一項是 `{"kind", "id"}`，沒有就送空陣列（不能是 `null`）。Core 依工作的資源產生：`path` 用解析後的路徑（沒有時用原路徑），`disk` 用裝置、`mount` 用掛載點，其他種類用 ID；只送屬於這個節點的項目。叫不出名字的資源（例如沒有路徑的 `path`），Core 不送出步驟，直接讓它失敗為 `RESOURCE_UNNAMED`。

Agent 的每個動作宣告它的參數會碰哪些資源。唯讀動作（目前是 `agent.info`、`host.health`）什麼都不碰；其他動作碰到的每一項都必須在 `resources` 裡：同種類、同 ID，`path` 也可以是列出路徑底下的路徑（以 `/` 為界，`/` 本身涵蓋全部）。路徑必須是絕對路徑，不能有 `.`、`..` 或空段，Agent 不替它清理。不同種類之間不互相涵蓋：保留整顆磁碟，不代表可以碰那顆磁碟上的路徑，動作宣告哪一種，工作就要保留哪一種。比對只看名字，不追 symlink，連結的安全由各動作與高權限執行器負責。所以動作參數裡的路徑要用 Core 解析後的路徑（工作資源的 `resolvedPath`），用使用者原本輸入的路徑會在兩者不同時被拒。

這一層保證 Agent 只碰工作保留的資源；資源本身該不該由這個工作碰（擁有者），由 Core 決定（票 090）。

同一個 `jobId`＋`step` 已有完成或失敗紀錄時，Agent 不重跑，直接重送那筆持久事件。步驟跑完卻寫不進紀錄時，Agent 結束會話；重連後 Core 對帳，看到的是「開始過、沒有結束」。

Agent 會壓縮步驟紀錄（決策 0032）：工作的每筆紀錄都被 Core 確認、每個步驟都已結束，而且最後一筆超過 7 天（或比它新的已結束紀錄已超過 1 萬筆），整個工作就移出 `journal.jsonl`。移出後每個步驟仍留下最後一筆紀錄，只是不含 `result`：

- `step_reconcile` 照留下的紀錄回報已完成或已失敗，跟壓縮前一樣。
- 失敗的步驟再收到 `step_execute`，照樣重送那筆失敗事件。
- 完成的步驟再收到 `step_execute`，因為 `result` 已經不在，回 `OUTCOME_UNKNOWN`，讓 Core 先對帳。
- 已結束的步驟不會再開始。

Agent 最多留 5 萬個被壓縮步驟的結果，更舊的會刪掉。Core 若在那之後才對帳那麼舊的步驟，Agent 會回「沒開始過」。

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

### 主機資訊與指標

型別在 `agent/crates/protocol/src/metrics.rs` 與 `core/internal/protocol/metrics.go`，樣本見 `fixtures/messages/host_facts.json`、`metrics_report.json`。

- 讀不到的值一律是 `null`，意思是「未提供」，不是 0。
- `host_facts` 的每個欄位是 `{"value", "source"}`，`source` 是讀取的檔案或介面（例如 `/etc/os-release`、`adjtimex`）。連線後送一次，之後只在值改變時再送。
- 樣本每 10 秒一筆（`intervalSeconds`），`seq` 在 Agent 執行期間每筆加 1，重啟後從 1 開始。Core 以節點與 `collectedAt` 判定重複，同一筆重送不會被算兩次。
- 第一筆樣本沒有前一筆可比，CPU 比例與各種速率是 `null`、`cpu.cores` 是空陣列。
- 記憶體的 `usedBytes` 是 `totalBytes - availableBytes`，頁快取與可回收 slab 算可用。沒有 `MemAvailable` 時兩者都是 `null`，不用 `MemFree` 代替。
- `pressure.state`：`ok`、`unsupported`（沒有 `/proc/pressure`）、`disabled`（核心有 PSI 但被關掉，讀取回 EOPNOTSUPP）、`unreadable`。
- 檔案系統 `state`：`ok`、`unresponsive`（`statvfs` 2 秒沒回）、`unreadable`；`inodes`：`ok` 或 `notApplicable`（btrfs、ZFS、bcachefs，或 inode 總數為 0 的檔案系統）。
- 磁碟 `class`：`physical`、`raid`、`mapper`、`virtual`、`loop`、`network`；網路介面 `class`：`physical`、`virtual`、`loopback`。
- Agent 最多暫存 360 筆（Core 連不上時約一小時），滿了丟最舊的，下一則 `metrics_report` 的 `droppedSamples` 帶出丟掉的筆數。一則報告的樣本編碼後不超過 192 KiB，超過就分成多則。
- 每條會話一次只有一則 `metrics_report` 在等確認，收到 `metrics_ack` 才送下一則，所以確認的 `seq`（報告裡最大的值）一定涵蓋它回答的那一則。Core 存好整則才確認，存失敗不回；Agent 30 秒沒收到確認就重送同一批，新連線也從最舊的未確認樣本送起。
- Core 用不了的樣本（`intervalSeconds` 不在 1 到 3600、`collectedAt` 比 Core 的時鐘快 5 分鐘以上或早於 48 小時）不保存，但照樣確認，因為重送也不會變好。比 Core 快 5 分鐘以上的 `host_facts` 也不保存。
- `droppedSamples` 是還沒被確認的丟棄筆數，帶它的那一則被確認才歸零；沒被確認就隨重送再帶一次，所以 Core 可能多算，不會少算。報告裡的樣本全都用不了時，筆數照樣加進節點的統計（節點還沒有任何樣本時除外）。

### 儲存清單與用量

型別在 `agent/crates/protocol/src/storage.rs` 與 `core/internal/protocol/storage.go`，樣本見 `fixtures/messages/host_storage.json`、`storage_usage.json`。容量與 inode 不在這裡，照舊在 `metrics_report` 的 `filesystems`。

- `host_storage` 每 60 秒讀一次，連線後送一次，之後只在內容改變時再送。兩則訊息都沒有確認：Core 存失敗就等下一次改變或下一條會話，Agent 每條新會話都會重送目前的內容。
- `devices` 是 `/sys/block` 的整顆磁碟與它的分割區。檔案系統類型、UUID、標籤、分割區 UUID 與名稱讀 `/run/udev/data`，沒有時讀 `/dev/disk/by-*` 連結，都沒有就是 `null`。
- `configs` 是 `/etc/fstab` 的每一行與 `/etc/systemd/system/*.mount` 單元（`/run` 下由 fstab 產生的單元不讀）。`reference` 說它怎麼認來源：`uuid`、`label`、`partuuid`、`partlabel`、`id`（換插槽仍認得到），`path`（認插槽），`device`（核心名稱，例如 `/dev/sdb1`，重開機或換插拔後可能指到別顆），`mapper`（device-mapper 或 RAID 名稱），`network`、`virtual`、`other`。`boot` 是 `auto`、`onDemand`（`x-systemd.automount` 或有對應的 `.automount`）、`noauto`。`resolvesTo` 是它現在指到的區塊裝置，找不到時是 `null`。
- `mounts` 是 `/proc/self/mountinfo` 裡非虛擬的掛載，同一個掛載點只留最後一個。`configs` 是 `configs` 陣列的索引，空陣列代表手動掛上、重開機後不會回來；`reference` 有設定時取第一筆設定的，否則看來源本身。
- `problems`：`duplicateMountPoint`（兩筆設定掛同一個點）、`missingDevice`（設定指到這台沒有的裝置）、`notMounted`（開機該掛的現在沒掛）、`deviceName`（本機實體或虛擬磁碟用核心名稱掛載，包含 swap 分割區）。
- `unreadable` 列出讀不到的來源（例如 `/etc/fstab`），讀不到時清單少的那些只是不知道，不是沒有。
- 設定裡的機密不離開主機：`options` 裡 `password`、`pass`、`passwd`、`pw`、`secret`、`token`、`key`、`passphrase` 的值換成 `***`（不分大小寫，`credentials=` 指的是檔案，保留），`spec` 與掛載的 `source` 是帶密碼的網址時（`https://u:密碼@host/`）密碼換成 `***`；`user=`、`username=` 裡 `%` 後面的 CIFS 密碼也換掉，CIFS 密碼裡以 `,,` 寫的逗號連同後面的部分一起遮掉。
- 一則 `host_storage` 編碼後不超過 384 KiB：超過時先丟 loop 裝置與 snap、squashfs 掛載，再丟虛擬與網路掛載，最後才從後面丟其他項目，`unreadable` 加一筆 `truncated: …` 寫出丟了幾筆；用量量測照樣涵蓋被丟掉的本機掛載，但丟掉的掛載在 Core 沒有對應的清單項目。
- `storage_usage` 量測每個本機檔案系統裡一組固定目錄的大小，分類是 `appData`、`database`、`backupStaging`、`containerImages`、`osAndLogs`，沒量到的已用空間是 `other`。每個路徑只算一類：目錄本身是另一類的起點時（`/var/lib/docker/volumes` 在 `/var/lib/docker` 裡），交給那一類。不跨到別的檔案系統，算配置的區塊不算檔案長度，硬連結只算一次。
- 用量 `state`：`complete`（`categories` 加上 `otherBytes` 等於 `usedBytes`）、`partial`（碰到筆數或時間上限、有目錄讀不到，或量到的比已用空間還多，例如 reflink、btrfs 快照共用區塊或量測期間寫入；數字是下限，`otherBytes` 是 `null`）、`unmeasured`（`statvfs` 失敗）。`categories` 只列有位元組的類別。
- 第一輪在 Agent 啟動 5 分鐘後開始，之後每 6 小時一輪；量測執行緒用最低的 CPU 與 I/O 優先權，每 2048 筆停 10 毫秒，一輪最多 500 萬筆、20 分鐘。
- Core 不保存 `collectedAt`（或 `startedAt`）比 Core 的時鐘快 5 分鐘以上的訊息、比已存的舊的重送、索引超出 `configs` 範圍的清單，類別重複或不認得的用量，以及標成 `complete` 卻加不起來的用量。

### 容器引擎

型別在 `agent/crates/protocol/src/engines.rs` 與 `core/internal/protocol/engines.go`，樣本見 `fixtures/messages/engines_report.json`。

- 報告由高權限執行器的唯讀動作 `engine_inspect`（權限 `host.engines.read`）產生，因為 rootful 引擎的 socket 與各使用者的 `/run/user/<uid>` 只有 root 讀得到。Agent 只檢查格式後轉送。
- `engines` 固定列出 `docker`、`podman`、`k3s` 三種，沒裝也列。`state`：`running`、`stopped`、`not_installed`、`incompatible`（版本低於 Docker API 1.41 或 Podman 4.0）、`permission_denied`、`not_responding`、`unknown`（裝了，但讀不到 systemd，不猜是否在跑）。
- `environments` 每個端點一列，同種引擎多個不合併；`id` 是 `env:` 加解析後的端點，端點上換了引擎或停了也不變。`kind` 以端點對 `GET /version` 的回答判斷（`podman-docker` 的 `docker.sock` 是 `podman`），`kindConfirmed` 為 false 表示端點沒回答，`kind` 只是該路徑慣例上的引擎。同一個 socket 經不同路徑（`/var/run` 連到 `/run`）只列一次，`endpoint` 是看的路徑，`endpointResolved` 是解析連結後的路徑。
- `mode` 與 `ownerUid` 取自 socket 檔案的擁有者，`ownerName` 取自 `/etc/passwd`，查不到是 `null`。`/run/user/<uid>/` 底下的路徑連到別處或不屬於該使用者時不連線，列在 `uninspected`。
- 讀不到的值一律是 `null`。看不到的範圍（沒有 systemd、使用者目錄太多、沒開 API socket 的 rootless 引擎）列在 `uninspected`，讀者不能把「沒列出」當成「沒有」。
- 每個 socket 的一次往返最多 5 秒、回答最多 1 MiB，字串最多 256 字元。

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
