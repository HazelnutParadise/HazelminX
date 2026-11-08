# Core↔Agent 通道契約

Agent 主動連到 Core 的 Agent 入口（預設 `:9801`，路徑 `/agent/v1/connect`），在 TLS 上開 WebSocket，每個文字訊框一則 JSON 訊息。Rust 型別在 `agent/crates/protocol`，Go 型別在 `core/internal/protocol`；兩邊都用本目錄 `fixtures/` 的黃金樣本做契約測試，任何一邊改了形狀，兩邊的測試會一起失敗。

## 身分

- Core 安裝時產生自己的 CA（Ed25519），CA 私鑰與 Core 身分私鑰存在機密服務（`internal/secrets`），不落明文檔。
- Core 身分憑證：CA 簽發，EKU `serverAuth`，DNS SAN `core.hazelminx.internal`。Agent 驗 Core 時只看憑證鏈與這個名稱，不看 URL 的 IP 或主機名，所以 Core 換 IP 不影響身分。
- 節點憑證：CA 簽發，EKU `clientAuth`，CN 是節點指紋（節點 Ed25519 公鑰原始 32 位元組的 SHA-256 小寫 hex）。節點私鑰不離開節點。
- Agent 端檔案：`state_dir/identity/node.key`、`node.crt`、`core-ca.crt`（後兩者由註冊流程寫入，票 019）。

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
  ```

  `role` 是 `core` 或 `agent`。Core 以身分私鑰簽 `core` 版本，Agent 以節點私鑰簽 `agent` 版本。雙方 nonce 都是新的，所以舊握手無法重放；角色字串不同，所以一方的簽章不能拿來冒充另一方；兩個 `kex` 都在簽章內，所以中間人換掉任何一方的 `kex`，簽章就對不上。
- 節點身分以 `nodeCertificate`＋`proof` 證明，不依賴 TLS 用戶端憑證，也不讀任何 HTTP Header。經會終止 TLS 的代理時仍成立；直連時 Agent 另外出示 TLS 用戶端憑證，Core 要求兩者指紋一致。
- Agent 的 `core_transport = "pinned"`（預設）以 `core-ca.crt` 驗 TLS 伺服端；`"proxy"` 以代理的憑證鏈驗 TLS（`proxy_ca_file` 或內建公開根憑證），Core 身分改由 `coreCertificate`＋`proof` 證明。
- 雙方以自己的 X25519 私鑰和對方的 `kex` 算出共同秘密，再用 HKDF-SHA256（不加鹽）展開成兩把 32 位元組金鑰：`info` 是 `hazelminx-session-v1 core->agent {sessionId}` 的金鑰給 Core 送出的訊息用，`hazelminx-session-v1 agent->core {sessionId}` 的給 Agent 送出的訊息用。代理看得到握手內容，但算不出這兩把金鑰。
- 握手訊息單則上限 64 KiB；Core 同時進行中的握手上限 64 條，超過回 HTTP 503；被拒紀錄只保留 Agent 提供文字的前 256 個字元。

## 訊框

`ready` 之後，雙方送出的每則訊息都包在一個訊框裡：

```json
{"n": 1, "body": "{\"type\":\"ack\",\"seq\":17}", "mac": "<base64>"}
```

- `n` 是這個方向的訊框序號，從 1 起每則加 1；收方只接受下一個號碼。
- `body` 是訊息本身的 JSON 文字（字串，不是物件），握手後訊息的形狀與規則都適用在它上面。
- `mac` 是 HMAC-SHA256，金鑰是送方方向的那把，內容依序是 `hazelminx-frame-v1\n`、`n` 的 8 位元組大端序、`body` 的 UTF-8 位元組，標準 base64。
- 訊框多或少欄位、序號不對、MAC 不符、二進位訊框，收方一律直接關閉會話，不看內容、不回 `protocol_error`。這擋住經會終止 TLS 的代理插入、重放或調換訊息。
- 握手後單則訊框上限 1 MiB。Agent 回報的步驟結果超過 256 KiB 時改記為失敗 `RESULT_TOO_LARGE`。

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

### 能力

`capabilities` 是 `{name, version}` 清單。協定 v1 的能力：

| name | version | 說明 |
|---|---|---|
| `jobs` | 1 | `step_execute`、`step_reconcile`、`event`、`ack` |
| `agent.info` | 1 | 唯讀動作：回報 Agent 版本、節點指紋、作業系統、架構、主機名 |

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

### `reject` 代碼

`PROTOCOL_INCOMPATIBLE`、`HANDSHAKE_MALFORMED`、`IDENTITY_INVALID`（憑證鏈、用途或指紋不符）、`PROOF_INVALID`、`NODE_REVOKED`、`DUPLICATE_SESSION`（同一節點已有連線）。

## 樣本

- `fixtures/messages/*.json`：每種訊息一則合法樣本，兩邊解碼後再編碼要得到同樣的 JSON 值。
- `fixtures/invalid/*.json`：必須被拒絕的訊息，檔名說明原因。
- `fixtures/handshake-lenient/*.json`：帶未知欄位、仍須被握手解碼接受的訊息。
- `fixtures/signature.json`：固定種子的節點、Core 與 X25519 金鑰，簽章內容、預期簽章、共同秘密與兩方向的會話金鑰（Ed25519 是確定性簽章，兩邊必須算出同一個值）。
- `fixtures/frames.json`：用上面的會話金鑰封好的訊框，兩邊封出來的要逐字相同，改任何一處都要被拒絕。
- `fixtures/pki/`：測試用 CA、Core 與節點憑證（僅供測試，私鑰公開，不得用於任何部署）。
