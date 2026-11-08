# Komodo Periphery 改造起點

HazelminX 的 Rust Agent 以 Komodo Periphery 為改造起點。本文件固定起點版本，並列出每個 crate 與模組的處理方式、負責的票與授權，供移植程式、檢查授權與更新上游時對照。

## 起點版本

| 項目 | 值 |
|---|---|
| 上游 | [moghtech/komodo](https://github.com/moghtech/komodo) |
| 標籤 | `v2.3.3` |
| commit | `780ac68b992094a9fccd5fffb760e0c84fd3c3d1`（2026-09-01，`2.3.3 (#1605)`） |
| 範圍 | `bin/periphery` 與它相依的 9 個 workspace crate（見「Komodo workspace crate」表） |
| 上游授權 | `GPL-3.0-or-later`（根目錄 `LICENSE` 為 GPL v3 全文，`Cargo.toml` 的 `workspace.package.license`） |

原始碼研究見 [`docs/research/komodo-periphery.md`](../docs/research/komodo-periphery.md)。研究報告寫「main，commit 780ac68」，與上表是同一個 commit。

## 處理方式

| 處理 | 意思 | 授權義務 |
|---|---|---|
| 保留 | 把上游原始碼複製進 HazelminX 的 crate，再改寫型別與介面 | 檔頭加移植告知（見「移植規則」） |
| 替換 | HazelminX 自己寫，或改用其他套件。可以參考上游的設計，不複製程式碼 | 無 |
| 捨棄 | Agent 不沿用。若仍需要對應功能，由其他元件負責（見去向欄） | 無 |
| 新增 | 上游沒有，HazelminX 新寫 | 無 |

`komodo_periphery` 本身標「依模組」：它的各模組分別歸入上面四種，見「`bin/periphery` 模組」。

## 為什麼不直接相依 Komodo crate

`bin/periphery` 只有執行檔 target（`[[bin]]`，沒有 `[lib]`），它的模組本來就不能被別的 crate 相依。能相依的 `lib/command` 又綁著 `komodo_client` 的型別，直接相依會把整套 Komodo 資料模型帶進來。要保留的模組綁定情況：

| 模組 | 綁定的 Komodo 型別或套件 |
|---|---|
| `lib/command` | `komodo_client::entities::{komodo_timestamp, update::Log}`、`komodo_client::parsers::parse_multiline_command`、`svi` |
| `bin/periphery/src/stats` | `komodo_client::entities::stats`、`async_timing_util`、Periphery 全域設定與狀態 |
| `bin/periphery/src/terminal.rs` | `komodo_client::entities`、`encoding`、`periphery_client::transport::EncodedTerminalMessage` |
| `bin/periphery/src/docker` | `komodo_client::entities::docker::*`、`periphery_client::api::swarm::*`、`shell_escape`、`helpers.rs` 的格式化函式 |

## 移植規則

- 上游檔案沒有逐檔版權檔頭。移植來的檔案在檔頭加上這段告知，日期用 UTC：

  ```rust
  // Derived from Komodo (https://github.com/moghtech/komodo),
  // <上游路徑> at commit <上游 commit>.
  // Copyright (C) Komodo contributors. Licensed under GPL-3.0-or-later.
  // Modified by HazelminX contributors, <YYYY-MM-DD>.
  ```

  日期寫最後一次修改的日期。一個檔案合併多個上游檔案時，每個來源各寫一行 `Derived from`。

- 檔頭的 commit 是該檔案的實際來源。上游更新時，只更新重新比對過的檔案的檔頭。「起點版本」表的 commit 只代表新移植檔案的預設起點。
- 移植後的模組改用 HazelminX 自己的型別（`crates/protocol`），不保留 `komodo_client` 或 `periphery_client` 的名稱。
- 命令一律以程式名稱加參數陣列執行（`Command::args`），不接受命令字串，也不經 `sh -c`。網頁終端機的互動 shell 也是以程式名稱加參數啟動，但它會執行使用者輸入的命令，所以需要 `host.shell` 權限（權限名稱暫定，出處是 `agent/README.md` 的高權限執行器動作說明，見下方 `terminal.rs` 列）。容器 exec／attach 與 Compose run 會帶使用者輸入的命令，它們的參數形式與權限由票 027、040、044 決定。
- 需要 root 的動作走 `hazelminx-privileged`。

## Komodo workspace crate

| crate | 路徑 | 授權 | 處理 | 去向 |
|---|---|---|---|---|
| `komodo_periphery` | `bin/periphery` | GPL-3.0-or-later | 依模組 | 見「`bin/periphery` 模組」 |
| `command` | `lib/command` | GPL-3.0-or-later | 保留 | `crates/agent` 的命令層，只給低權限的 Agent 用，`hazelminx-privileged` 有自己的執行器。沒有專屬的票：票 020（GPU 的命令列退路）、040、044（`docker compose` CLI）中最先需要的那張負責移植。保留程序群組隔離、逾時、取消與整組結束。移除接受字串的 API（`run_standard_command`、`run_shell_command`、多行命令），改成參數陣列。`Log` 換成 HazelminX 的步驟紀錄 |
| `komodo_client` | `client/core/rs` | GPL-3.0-or-later | 替換 | `crates/protocol`（票 007） |
| `periphery_client` | `client/periphery/rs` | GPL-3.0-or-later | 替換 | `crates/protocol`（票 007） |
| `transport` | `lib/transport` | GPL-3.0-or-later | 替換 | 票 007 的 WebSocket over mTLS 連線。參考上游在單一連線上多工傳送請求與終端機串流的設計（ENG.md「Core↔Agent 協定」列），不用 Noise 握手 |
| `encoding` | `lib/encoding` | GPL-3.0-or-later | 替換 | 票 007 的 JSON 訊息格式與黃金樣本（`docs/contracts/fixtures/`） |
| `environment` | `lib/environment` | GPL-3.0-or-later | 替換 | Compose 的 `--env-file`（ENG.md「Compose 執行」）。環境檔內容與機密解析由票 045 與票 011 決定，Agent 只把收到的內容寫入檔案 |
| `interpolate` | `lib/interpolate` | GPL-3.0-or-later | 捨棄 | Compose 的變數替換交給 `docker compose` CLI 依 `--env-file` 處理。Core 以 `compose-go/v2` 產生預覽與差異（ENG.md「Compose 執行」） |
| `formatting` | `lib/formatting` | GPL-3.0-or-later | 替換 | 錯誤格式化改用 `anyhow` 與結構化日誌 |
| `git` | `lib/git` | GPL-3.0-or-later | 捨棄 | 不沿用 Periphery 的 Git 實作。Git 取得在受控抓取服務（ENG.md「GitHub」），節點端的隔離抓取由票 049 決定 |

## `bin/periphery` 模組

| 模組 | 處理 | 去向 |
|---|---|---|
| `stats/`（含 `mem.rs` 的 `/proc/meminfo` 與 ZFS ARC 解析） | 保留 | `crates/agent` 的 `stats/`（票 020）。輸出改成 HazelminX 指標型別。上游預設每 5 秒（`client/core/rs/src/entities/config/periphery.rs` 的 `default_stats_polling_rate`）以 `ProcessRefreshKind::everything().without_tasks()` 更新全部程序。[`PERFORMANCE.md`](PERFORMANCE.md) 建議 HazelminX 每 10 秒只更新程序的 CPU 與記憶體，完整程序欄位改成有人查看時才讀（依據與限制見該文件），由票 020 量測後確認 |
| `terminal.rs`（`portable-pty`、輸出歷史緩衝） | 保留 | 票 027。保留 PTY 分配、輸入輸出轉發與歷史緩衝。串流改走票 007 的通道。啟動命令改成程式名稱加參數陣列（上游以 `shlex::split` 拆字串）。主機 shell 需要 `host.shell` 權限。以哪個 Linux 帳號執行、權限在 Agent 還是 `hazelminx-privileged` 檢查，由票 027 決定 |
| `docker/`（容器、映像、網路、Volume、Compose） | 保留 | 票 040、041、042、044。以 `bollard` 為主。`docker compose` CLI 改用參數陣列呼叫，移除 `shell_escape` 字串拼接。低權限的 Agent 怎麼取得 Docker socket 存取權由票 040 決定 |
| `docker/` 與 `api/swarm/` 的 Swarm 部分 | 保留 | 票 043（CTR-013 等）。研究報告建議捨棄，但 Swarm 在 HazelminX 範圍內 |
| `api/` 的請求分派（`api/mod.rs`、`mogh_resolver`） | 替換 | 票 007 的 `crates/protocol` 請求列舉與分派 |
| `api/` 的處理器（`compose.rs`、`container/`、`docker.rs`、`terminal.rs`、`poll.rs`、`swarm/`，以及 `api/mod.rs` 裡的 `GetSystemProcesses`、`PruneSystem`） | 保留 | 跟著上面對應的 `docker/`、`terminal.rs`、`stats/` 列處理。`compose.rs` 裡讀寫主機 Compose 檔與 commit 的部分（`GetComposeContentsOnHost`、`WriteComposeContentsToHost`、`WriteCommitComposeContents`）跟著 `stack/` 與 `lib/git` 捨棄 |
| `api/mod.rs` 的 `GetHealth`、`GetVersion` | 替換 | 票 007 的握手（交換協定與能力版本） |
| `api/mod.rs` 的 `ListGitProviders`、`ListImageRegistries`、`ListSecrets` | 捨棄 | 列出 Periphery 設定檔裡的帳號與機密名稱。HazelminX 的連線與機密在 Core（票 011、047） |
| `api/git.rs` | 捨棄 | 同 `lib/git` |
| `api/build/` | 捨棄 | 不沿用 Periphery 的建置 API。隔離建置由票 049 決定（ENG.md「GitHub 部署」） |
| `api/keys.rs`、Onboarding 與 Passkey | 替換 | 節點身分在 `crates/agent/src/identity.rs`（Ed25519）。加入流程是票 019 |
| `connection/`（主動外連、重連、入站伺服器） | 替換 | 票 007。參考主動外連與重連退避的設計。入站伺服器（`server.rs`）不要，節點不開管理連接埠 |
| `main.rs`（啟動、背景採樣、SIGTERM 時關閉終端機） | 替換 | `crates/agent/src/main.rs`。收到 SIGTERM 時結束終端機的行為屬於票 027 的範圍，但票 027 的驗收還沒有列這一條 |
| `config.rs` | 替換 | `crates/agent/src/config.rs`（TOML、旗標、環境變數） |
| `state.rs` | 替換 | 各模組自己的狀態，不用全域 `OnceLock` 集合 |
| `helpers.rs` 的 Docker 格式化（`format_labels`、`push_environment`、`format_log_grep` 等） | 保留 | 跟著 `docker/` 列改成產生參數陣列 |
| `helpers.rs` 的其他部分（`registry_token`、`git_token`、OpenDNS 查外網 IP、以 OpenSSL 指令產生自簽憑證） | 捨棄 | 登錄站與 Git 憑證以機密參照交付（票 011）。外網 IP 不由 Agent 對外查詢。憑證由票 007 的 mTLS 與票 019 的註冊流程處理 |
| `stack/` | 捨棄 | Komodo 自己的 Stack 目錄規則（預設 `${root_directory}/stacks`，`root_directory` 預設 `/etc/komodo`），HazelminX 的部署設定由 Core 產生 |
| GPU 指標 | 新增 | 票 020（MON-002）。上游沒有 GPU 採集 |

上游沒有、要新寫的其他模組（`jobs/`、`probes/`、`files/`、Windows 實作）見 ENG.md 的元件圖與「Hidden assumptions」。呼叫高權限執行器的客戶端已由票 012 建在 `crates/privileged` 的 `client` 模組。

## 外部相依

以下是 `komodo_periphery` 的 37 個直接外部相依（「重新核對」的 `jq` 指令輸出，已排除 9 個 workspace crate），另外加上 `lib/command` 才有的 `libc`。這一節的「保留」是繼續相依這個 crate，「替換」是改用右欄的套件或寫法，「捨棄」是不再相依，都不涉及複製原始碼。版本取自起點 commit 提交的 `Cargo.lock`，只是上游紀錄。HazelminX 實際使用的版本以 `agent/Cargo.lock` 為準。

### 保留

| crate | 上游版本 | 授權 | 用途 |
|---|---|---|---|
| `tokio` | 1.53.1 | MIT | 非同步執行環境 |
| `tokio-util` | 0.7.19 | MIT | `CancellationToken`（命令與終端機取消） |
| `futures-util` | 0.3.34 | MIT OR Apache-2.0 | `bollard` 串流處理 |
| `tokio-stream` | 0.1.19 | MIT | 終端機輸出串流 |
| `bytes` | 1.12.1 | MIT | 終端機輸入輸出緩衝 |
| `libc` | 0.2.189 | MIT OR Apache-2.0 | 命令層結束整個程序群組（`lib/command` 的相依） |
| `sysinfo` | 0.39.6 | MIT | 主機指標 |
| `portable-pty` | 0.9.0 | MIT | 終端機 |
| `bollard` | 0.21.1 | Apache-2.0 | Docker Engine API |
| `serde` | 1.0.229 | MIT OR Apache-2.0 | 序列化 |
| `serde_json` | 1.0.151 | MIT OR Apache-2.0 | 協定訊息 |
| `anyhow` | 1.0.104 | MIT OR Apache-2.0 | 錯誤處理 |
| `tracing` | 0.1.44 | MIT | 結構化日誌 |
| `clap` | 4.6.6 | MIT OR Apache-2.0 | 命令列 |
| `uuid` | 1.25.0 | Apache-2.0 OR MIT | 終端機通道 ID |
| `rustls` | 0.23.43 | Apache-2.0 OR ISC OR MIT | mTLS（票 007） |

`rustls` 需要選一個加密後端，由票 007 決定。上游用 `aws-lc-rs` 1.16.3（`ISC AND (Apache-2.0 OR ISC)`），它的 `aws-lc-sys` 0.40.0 宣告 `ISC AND (Apache-2.0 OR ISC) AND Apache-2.0 AND MIT AND BSD-3-Clause AND (Apache-2.0 OR ISC OR MIT) AND (Apache-2.0 OR ISC OR MIT-0)`。另一個選項 `ring` 0.17.14 宣告 `Apache-2.0 AND ISC`。這只是 Cargo 的授權欄位，兩者都沒有列 OpenSSL 授權。研究報告寫 `aws-lc` 含 OpenSSL 授權。票 007 選定後端時要讀該 crate 實際的 LICENSE 檔，確認能與 GPL-3.0 一起散布。

`lib/command` 的其他相依：`komodo_client` 見「Komodo workspace crate」。`svi`、`shlex`、`tokio`、`tokio-util` 也是 `komodo_periphery` 的直接相依，已算在 37 個裡。

### 替換

| crate | 上游版本 | 授權 | 換成 |
|---|---|---|---|
| `mogh_pki` | 1.1.3 | MPL-2.0 | `rustls` mTLS 與 `ed25519-dalek` 節點身分 |
| `mogh_resolver` | 1.0.0 | MPL-2.0 | `crates/protocol` 的請求列舉與 `match` 分派 |
| `mogh_config` | 1.4.2 | MPL-2.0 | `toml` 加 `clap`（`crates/agent/src/config.rs`） |
| `mogh_logger` | 1.3.4 | MPL-2.0 | `tracing-subscriber` |
| `mogh_error` | 1.0.4 | MPL-2.0 | `anyhow` |
| `mogh_cache` | 1.1.2 | MPL-2.0 | 各模組自己的快取（`tokio::sync`） |
| `mogh_secret_file` | 1.0.1 | MPL-2.0 | 上游用它讀私鑰與各種機密檔。節點身分金鑰由 `crates/agent/src/identity.rs` 管理。部署用的機密由 Core 以參照交付（票 011） |
| `async_timing_util` | 1.1.0 | MIT | `tokio::time::interval` |
| `svi` | 1.2.0 | MIT | HazelminX 自己的日誌遮罩（只存參照，不寫明文機密） |
| `envy` | 0.4.2 | MIT | `clap` 的 `env` 屬性 |
| `dotenvy` | 0.15.7 | MIT | `clap` 的 `env` 屬性，不讀 `.env` |
| `shell-escape` | 0.1.5 | MIT/Apache-2.0 | 參數陣列 |
| `arc-swap` | 1.9.2 | MIT OR Apache-2.0 | `tokio::sync` 鎖 |
| `strum` | 0.28.0 | MIT | `serde` 列舉標籤 |

替換 `mogh_*` 是技術上的原因：它們是 Komodo 自己的握手、設定、分派、日誌、錯誤、快取與機密檔讀取元件，介面跟著 Komodo 的資料模型走，HazelminX 已有或會用別的做法。

### 捨棄

| crate | 上游版本 | 授權 | 原因 |
|---|---|---|---|
| `mogh_server` | 1.5.0 | MPL-2.0 | Agent 不開入站管理服務 |
| `axum` | 0.8.9 | MIT | 同上 |
| `urlencoding` | 2.1.3 | MIT | 只用在被替換的 `connection/` |
| `shlex` | 2.0.1 | MIT OR Apache-2.0 | 不接受命令字串，不需要拆字串 |
| `hickory-resolver` | 0.26.1 | MIT OR Apache-2.0 | 只用來向 OpenDNS 查外網 IP |
| `serde_yaml_ng` | 0.10.0 | MIT | Compose 解析在 Core（`compose-go/v2`） |
| `colored` | 3.1.1 | MPL-2.0 | 終端顏色。日誌由 `tracing-subscriber` 輸出 |
| `data-encoding` | 2.11.1 | MIT | 只用在 Swarm config 的 base64。移植時改用 workspace 已有的 `base64`（目前由 `hazelminx-privileged` 使用） |

## HazelminX 新增的 crate

### workspace crate

| package | 路徑 | 授權 | 狀態 | 職責 |
|---|---|---|---|---|
| `hazelminx-agent` | `crates/agent` | GPL-3.0-or-later | 已建立（票 004） | 低權限常駐服務、節點身分 |
| `hazelminx-privileged` | `crates/privileged` | GPL-3.0-or-later | 已建立（票 012） | 固定動作的高權限執行器 |
| `hazelminx-protocol` | `crates/protocol` | GPL-3.0-or-later | 已建立（票 007） | Core 與 Agent 的訊息型別、握手簽章內容、訊框 MAC |
| 未命名（執行檔 `hazelminx`） | `crates/cli` | GPL-3.0-or-later | 未建立（票 013） | 安裝、更新、診斷、救援、卸載 |
| 未命名 | `crates/platform-linux`、`crates/platform-windows` | GPL-3.0-or-later | 未建立，未排票 | 平台相依實作。Windows 範圍未核定，只保留介面 |

授權欄位沿用 Komodo 的 `GPL-3.0-or-later`。`GPL-3.0-only` 或 `or-later` 的最終宣告待核定，核定前不改（AGENTS.md）。

### 直接外部相依（已進入 `agent/Cargo.lock`）

| crate | 版本 | 授權 | 使用者 |
|---|---|---|---|
| `ed25519-dalek` | 2.2.0 | BSD-3-Clause | agent（節點身分、握手簽章）、privileged（授權憑證驗簽）、protocol（測試） |
| `x25519-dalek` | 2.0.1 | BSD-3-Clause | protocol（握手金鑰交換） |
| `hkdf` | 0.12.4 | MIT OR Apache-2.0 | protocol（導出訊框金鑰） |
| `hmac` | 0.12.1 | MIT OR Apache-2.0 | protocol（訊框 MAC） |
| `sha2` | 0.10.9 | MIT OR Apache-2.0 | agent、privileged、protocol |
| `rustls` | 0.23.46 | Apache-2.0 OR ISC OR MIT | agent（TLS 1.3 用戶端） |
| `tokio-rustls` | 0.26.6 | MIT OR Apache-2.0 | agent |
| `rustls-pki-types` | 1.15.1 | MIT OR Apache-2.0 | agent |
| `rustls-webpki` | 0.103.16 | ISC | agent（憑證鏈驗證） |
| `rustls-native-certs` | 0.8.5 | Apache-2.0 OR ISC OR MIT | agent（proxy 模式未設 `proxy_ca_file` 時讀作業系統的信任清單） |
| `tokio-tungstenite` | 0.26.2 | MIT | agent（WebSocket） |
| `futures-util` | 0.3.34 | MIT OR Apache-2.0 | agent |
| `url` | 2.5.8 | MIT OR Apache-2.0 | agent（Core 位址） |
| `rand` | 0.9.5 | MIT OR Apache-2.0 | agent（產生金鑰）、protocol、privileged（測試） |
| `hex` | 0.4.3 | MIT OR Apache-2.0 | agent、privileged、protocol |
| `base64` | 0.22.1 | MIT OR Apache-2.0 | agent、privileged、protocol |
| `time` | 0.3.55 | MIT OR Apache-2.0 | agent、privileged、protocol（RFC 3339 時間） |
| `toml` | 0.9.12 | MIT OR Apache-2.0 | agent、privileged（設定檔） |
| `serde_json` | 1.0.154 | MIT OR Apache-2.0 | agent、privileged、protocol |
| `tracing-subscriber` | 0.3.23 | MIT | agent、privileged |
| `tokio` | 1.54.1 | MIT | agent、privileged |
| `clap` | 4.6.7 | MIT OR Apache-2.0 | agent、privileged |
| `anyhow` | 1.0.104 | MIT OR Apache-2.0 | agent、privileged |
| `serde` | 1.0.229 | MIT OR Apache-2.0 | agent、privileged、protocol |
| `tracing` | 0.1.44 | MIT | agent、privileged |
| `tempfile` | 3.27.0 | MIT OR Apache-2.0 | 只用於測試 |

### TLS 加密後端

`rustls` 以 `ring` 0.17.14 為加密後端（`rustls`、`tokio-rustls`、`rustls-webpki` 都關掉預設 feature 改開 `ring`），不用 `aws-lc-rs`；`cargo tree -i aws-lc-rs` 與 `cargo tree -i openssl-sys` 都找不到。`tls12` feature 有編進去，但用戶端設定只允許 TLS 1.3。`ring` 的 `Cargo.toml` 宣告 `Apache-2.0 AND ISC`，實際 LICENSE 檔（2026-11-08 讀 crates.io 下載的 0.17.14 原始碼）：

- `LICENSE`：新寫的程式碼用 ISC 授權，來自 BoringSSL 的程式碼依各檔檔頭，分別是 Apache-2.0 或 ISC；`src/polyfill/once_cell` 來自 once_cell，MIT 或 Apache-2.0。
- `LICENSE-BoringSSL`：Apache License 2.0 全文。
- `LICENSE-other-bits`：ISC 授權全文（Copyright 2015-2025 Brian Smith）。
- 三個檔案都沒有舊版 `ring` 帶的 OpenSSL／SSLeay 授權（有廣告條款，與 GPL 不相容）。

ISC 與 Apache-2.0 都能與 GPL-3.0 一起散布，散布執行檔時附上這兩份授權文字。`aws-lc-rs` 會帶進 C 建置工具鏈與 OpenSSL 授權的程式碼，所以沒有選。

proxy 模式原本考慮用 `webpki-roots` 內建的公開根憑證清單，它的授權是 CDLA-Permissive-2.0（資料授權，FSF 沒有評估過與 GPL 的相容性）。改用 `rustls-native-certs` 讀作業系統的信任清單，`webpki-roots` 不在相依樹裡（`cargo tree -i webpki-roots` 找不到）。這樣也跟主機上其他 TLS 用戶端信任同一組根憑證，管理員自己加的企業 CA 也有效。

### 授權總覽

目前 `agent/Cargo.lock` 解析出 197 個套件，包含間接相依、只用於測試的相依，以及只在其他平台使用的相依（例如 `r-efi`），比 Linux 執行檔實際連結的套件多。授權分布如下。`Apache-2.0 OR MIT` 與 `MIT/Apache-2.0` 和 `MIT OR Apache-2.0` 意思相同，合併計算：

| 授權 | 套件數 |
|---|---|
| MIT OR Apache-2.0（含 `Apache-2.0 OR MIT`、`MIT/Apache-2.0` 寫法） | 130 |
| MIT | 24 |
| Unicode-3.0（`url` 經 `idna` 帶進的 `icu_*`、`zerovec` 等） | 18 |
| Apache-2.0 WITH LLVM-exception OR Apache-2.0 OR MIT | 5 |
| BSD-3-Clause | 4 |
| GPL-3.0-or-later（本專案的三個 crate） | 3 |
| Unlicense OR MIT | 2 |
| MIT OR Apache-2.0 OR LGPL-2.1-or-later（`r-efi` 兩個版本，選 MIT） | 2 |
| Apache-2.0 OR ISC OR MIT | 2 |
| ISC（`rustls-webpki`、`untrusted`） | 2 |
| BSD-2-Clause OR Apache-2.0 OR MIT | 2 |
| Apache-2.0 AND ISC（`ring`） | 1 |
| MIT OR Apache-2.0 OR BSD-1-Clause | 1 |
| (MIT OR Apache-2.0) AND Unicode-3.0（`unicode-ident`） | 1 |

除了本專案自己的三個 crate，沒有只能以 copyleft 授權使用的套件（`r-efi` 可選 MIT），也沒有 MPL-2.0。Unicode-3.0、ISC、Apache-2.0 都能與 GPL-3.0 一起散布，所以全部相依都能與 GPL-3.0 一起散布。

目前的 Agent 程式還沒有從 Komodo 移植任何檔案，所以還沒有需要加告知的檔案。預計最先移植的是票 020 的 `stats/` 或命令層，移植時依「移植規則」加檔頭。

## 重新核對

起點版本與上游授權：

```bash
git clone --filter=blob:none https://github.com/moghtech/komodo.git
cd komodo
git rev-parse 'v2.3.3^{commit}'   # 應為 780ac68b992094a9fccd5fffb760e0c84fd3c3d1
git checkout 780ac68b992094a9fccd5fffb760e0c84fd3c3d1
grep -n license Cargo.toml
# 上游提交的 Cargo.lock 落後 Cargo.toml，加 --locked 會失敗。不加會重新解析並改寫 Cargo.lock，查完要還原。
# 授權取自重新解析的版本。同名多版本的 crate 只會印最後一個版本的授權。
cargo metadata --format-version 1 > /tmp/komodo-meta.json
git checkout Cargo.lock
jq -r '(.packages | map({(.name): .}) | add) as $p
  | .packages[] | select(.name=="komodo_periphery") | .dependencies[]
  | select(.kind==null and .source!=null) | "\(.name) \(.req) \($p[.name].license)"' /tmp/komodo-meta.json
# 上游版本以提交的 Cargo.lock 為準，要查的 crate 名稱放進迴圈
for c in sysinfo bollard portable-pty tokio; do grep -A1 "^name = \"$c\"\$" Cargo.lock; done
```

HazelminX 目前的相依授權：

```bash
cd agent
cargo fetch --locked
cargo metadata --format-version 1 --locked --offline \
  | jq -r '.resolve.nodes[].id as $id | .packages[] | select(.id==$id) | "\(.name) \(.version) \(.license)"' | sort -u
# 授權分布
cargo metadata --format-version 1 --locked --offline \
  | jq -r '.resolve.nodes[].id as $id | .packages[] | select(.id==$id) | .license' \
  | sed -e 's#MIT/Apache-2.0#MIT OR Apache-2.0#' -e 's#^Apache-2.0 OR MIT$#MIT OR Apache-2.0#' | sort | uniq -c | sort -rn
```
