> 來源：zentinelproxy/zentinel（workspace 0.6.54）唯讀原始碼調查，2026-11-07，由派工 agent（opencode / big-pickle）產出，主 agent 核對過設定範例、deploy/ 與授權。報告是線索，實作前以固定版本重新核對。

# Zentinel 原始碼調查報告

以下結論全部來自唯讀閱讀原始碼，沒有啟動服務、沒有跑測試、沒有修改任何檔案。

## 1. Crate 結構

- workspace 成員：`crates/proxy`、`crates/agent-protocol`、`crates/config`、`crates/common`、`crates/gateway`、`crates/stack`、`crates/wasm-runtime`、`agents/echo`、`agents/data-masking`（`Cargo.toml:1-19`）。
- `zentinel-proxy` 是主執行檔，binary 名稱 `zentinel`，lib 名稱 `zentinel_proxy`（`crates/proxy/Cargo.toml:13-16`）。
- `zentinel-config` 只依賴 `zentinel-common`；`zentinel-agent-protocol` 也只依賴 `zentinel-common`（各 crate 的 `Cargo.toml`）。
- 輔助 crate 各有明確定位：
  - `crates/gateway`：Kubernetes Gateway API controller，把 GatewayClass/Gateway/HTTPRoute/ReferenceGrant 轉成 Zentinel 設定（`crates/gateway/src/lib.rs:1-33`）。
  - `crates/stack`：all-in-one 啟動器，管理 proxy 與 agent 子程序（`crates/stack/src/main.rs:1-6`）。
  - `crates/wasm-runtime`：以 Wasmtime 執行 in-process WASM agent（`crates/wasm-runtime/src/lib.rs:1-25`）。
  - `crates/config-inspect`：靜態拓樸分析與 Mermaid 輸出（`crates/config-inspect/src/lib.rs:1-35`）。
  - `crates/sim`、`crates/playground-wasm`：瀏覽器用設定模擬器與 WASM 綁定（`crates/sim/src/lib.rs:1-30`、`crates/playground-wasm/src/lib.rs:1-20`）。
- workspace 版本 `0.6.54`、edition 2021、`rust-version = "1.99.0"`（`Cargo.toml:21-29`）。

## 2. 設定系統（KDL）

- 設定格式是 KDL，入口 `Config::from_file`、`Config::from_kdl`、`Config::validate`（`crates/config/src/lib.rs:346`、`683`、`773`）。
- KDL 解析器分成 server、routes、upstreams、filters、namespace 等子模組（`crates/config/src/kdl/mod.rs:1-20`）。
- 目錄式設定由 `MultiFileLoader` 與 `ConfigDirectory` 處理，支援遞迴讀取、`include`、environment overrides（`crates/config/src/multi_file/mod.rs:1-40`）。
- namespace 會在載入後 flatten 成 qualified ID，供 runtime O(1) 查找（`crates/config/src/flatten.rs:1-40`）。
- **不支援環境變數代換**：值含 `${...}` 會被明確拒絕，常數 `NO_ENV_SUBSTITUTION` 在 `crates/config/src/validation.rs:64-65`；listener、metrics address、upstream target 都有檢查（`crates/config/src/lib.rs:842-933`）。
- 內建預設設定 `DEFAULT_CONFIG_KDL` 提供 8080 主服務與 9090 admin/health listener（`crates/config/src/defaults.rs:16-45`）。
- 主要設定區塊：`system`、`listeners`、`routes`、`upstreams`、`filters`、`agents`、`waf`、`namespaces`、`limits`、`observability`、`rate-limits`、`cache`（`crates/proxy/src/reload/coverage.rs:60-150`）。
- WAF 設定目前是 hard error：`waf.mode` 設成 `detection` 或 `prevention` 會拒絕啟動，只能 `off`（`crates/config/src/validation.rs:1233-1244`）。

## 3. 熱重載與 admin API

- **沒有找到 HTTP reload API**。重載來源是 SIGHUP、檔案變更監控、Gateway API controller；`ReloadTrigger::Manual` 只出現在內部與測試（`crates/proxy/src/reload/mod.rs:62-73`）。
- signal 行為：SIGHUP reload、SIGUSR1 reopen logs、SIGTERM/SIGINT graceful shutdown、SIGUSR2 忽略（`crates/proxy/src/main.rs:1456-1529`）。
- `ConfigManager` 用 `Arc<ArcSwap<Config>>` 原子切換，保留 `previous_config` 供 rollback，並以 `reload_mutex` 序列化重載（`crates/proxy/src/reload/mod.rs:180-200`、`424-437`、`616`、`782`）。
- 重載涵蓋範圍：
  - 會套用：routes、upstreams、filters、agents、waf、namespaces、limits、rate-limits（`crates/proxy/src/reload/coverage.rs:88-145`）。
  - 需重啟：system、listeners、observability、cache（`crates/proxy/src/reload/coverage.rs:80-150`）。
  - 變更需重啟的區塊時會明確警告，不會靜默忽略（`crates/proxy/src/reload/coverage.rs:155-170`）。
- SIGHUP 除了 reload 設定，也會獨立重載 TLS 憑證；單一 listener 失敗會保留舊憑證（`crates/proxy/src/main.rs:1754-1771`）。
- admin 端點不是獨立 API server，而是 builtin route：
  - `status`、`health`、`metrics`、`not-found`、`config`、`upstreams`、`cache-purge`、`cache-stats`（`crates/config/src/routes.rs:168-185`、`crates/config/src/kdl/routes.rs:439-497`）。
  - 預設路徑含 `/admin/config`、`/admin/upstreams`、`/admin/cache/stats`、`/admin/cache/purge`（`crates/config/src/defaults.rs:69-118`）。
- `/admin/config` 會 dump 執行中設定，但 TLS 欄位只顯示 `tls_enabled`，不輸出私鑰（`crates/proxy/src/builtin_handlers.rs:349-429`）。
- 風險：預設 embedded 設定的 admin listener 是 `0.0.0.0:9090`，且 builtin handler 本身沒有認證機制（`crates/config/src/defaults.rs:36-43`、`crates/proxy/src/builtin_handlers.rs:351`）。正式環境應限制綁定位址或加上前置認證。

## 4. Agent 機制

- 設定層支援三種 transport：Unix socket、gRPC over TCP、HTTP REST（`crates/config/src/agents.rs:285-300`；KDL 解析在 `crates/config/src/kdl/mod.rs:507-657`）。
- Agent protocol v2 的 runtime transport 是 gRPC 與 binary UDS（`crates/agent-protocol/src/v2/pool.rs:259-265`）。
- Reverse connection 在 protocol crate 有完整實作（`crates/agent-protocol/src/v2/reverse.rs:1-130`），但**沒有找到 proxy 端接線或設定入口**；目前設定只暴露 uds/grpc/http。
- 協定 proto 定義 `AgentServiceV2`，包含 `ProcessStream`、`ControlStream`、`ProcessEvent`（`crates/agent-protocol/proto/agent_v2.proto:521-524`）。
- 事件種類：request headers/body、response headers/body、log、WebSocket frame、guardrail（`crates/config/src/agents.rs:323-336`）。
- `AgentManager` 為每個 agent 建獨立 semaphore，避免 noisy neighbor；另有 per-agent circuit breaker（`crates/proxy/src/agents/manager.rs:74-85`、`281-330`）。
- 失敗模式預設 `Closed`，agent 掛掉時預設阻擋流量（`crates/config/src/routes.rs:463-471`）。
- body 傳送模式：`Buffer`（預設）、`Stream`、`Hybrid`、`InspectFirst`（`crates/config/src/agents.rs:18-64`）。
- 範例 agent：`agents/echo`、`agents/data-masking`、`agents/wasm-allowlist`。

## 5. TLS

- 使用 rustls 與 rustls-pemfile 載入憑證與私鑰（`crates/proxy/src/tls.rs:1095-1300`）。
- SNI 由 `SniResolver` 處理，支援預設憑證、`sni { ... }` 額外憑證、`sni-certs` 憑證資料夾（`crates/proxy/src/tls.rs:127`、`281-329`、`874`）。
- `allow-sni-overlaps` 預設 false，重疊會是設定錯誤，避免憑證選擇依目錄順序（`crates/config/src/server.rs:290-300`、`crates/config/src/kdl/server.rs:399`）。
- 熱重載由 `HotReloadableSniResolver` 與 `CertificateReloader` 實作（`crates/proxy/src/tls.rs:647`、`989`）。
- OCSP stapling 支援背景刷新（`crates/proxy/src/tls.rs:83-84`、`806-832`）。
- mTLS：`ca_file`、`client_auth`；TLS 版本、cipher suites、session resumption 都可設定（`crates/config/src/server.rs:262-340`、`crates/proxy/src/tls.rs:1495-1589`）。
- ACME 內建：
  - HTTP-01 challenge 由 `ChallengeManager` 提供 `/.well-known/acme-challenge/<token>`（`crates/proxy/src/acme/challenge.rs:1-83`）。
  - DNS-01 支援 wildcard，provider 含 Hetzner 與 webhook（`crates/proxy/src/acme/mod.rs:1-21`、`crates/proxy/src/acme/dns/`）。
  - 憑證持久化、背景續約、ARI 都在 `crates/proxy/src/acme/{storage,scheduler,ari}.rs`。

## 6. 真實訪客 IP

- 核心在 `crates/proxy/src/proxy/forwarded.rs`，由 `apply_forwarded_headers` 執行（`crates/proxy/src/proxy/forwarded.rs:116-155`）。
- 預設不信任任何來源：
  - `X-Forwarded-For` 以 TCP peer 位址取代。
  - `X-Forwarded-Proto` 以 listener 實際觀測值取代。
  - `X-Forwarded-Host` 以 request 自身 host 取代。
  - `Forwarded` 與 `X-Real-IP` 直接移除（`crates/proxy/src/proxy/forwarded.rs:13-55`、`125-160`）。
- 只有在 peer 位於 listener 的 `trusted-proxies` 時，才會 append `X-Forwarded-For`、保留送入的 proto/host，並從右往左找第一個非 trusted hop 當 client IP（`crates/proxy/src/proxy/forwarded.rs:80-100`、`155`）。
- `trusted-proxies` 是 per-listener 設定（`crates/config/src/server.rs:134-153`、KDL 解析 `crates/config/src/kdl/server.rs:112-140`）。
- `apply_forwarded` 在 request filter 之前就設定 `ctx.client_ip`，確保 rate limit、geo filter、agent 讀到正確 IP（`crates/proxy/src/proxy/http_trait.rs:230-263`）。

## 7. 上游管理

- `UpstreamConfig` 包含 targets、load balancing、sticky session、health check、circuit breaker、connection pool、timeouts、upstream TLS、HTTP version、discovery（`crates/config/src/upstreams.rs:88-140`）。
- 設定層 discovery 支援 static、DNS、DNS-SRV、Consul、Kubernetes、File（`crates/config/src/upstreams.rs:157-230`）。
- runtime discovery 實作在 `crates/proxy/src/discovery.rs`；Kubernetes 需 feature（`crates/proxy/src/lib.rs:64-67`）。
- 負載平衡演算法：RoundRobin、LeastConnections、Random、IpHash、Weighted、ConsistentHash、PowerOfTwoChoices、Adaptive、LeastTokensQueued、Maglev、LocalityAware、PeakEWMA、DeterministicSubset、WeightedLeastConnections、Sticky（`crates/common/src/types.rs:137-220`）。
- 主動健康檢查 `ActiveHealthChecker`、被動健康檢查 `PassiveHealthChecker`、runner `HealthCheckRunner`（`crates/proxy/src/health.rs:24`、`394`、`1177`）。
- 健康檢查型別：HTTP、TCP、gRPC、Inference、MCP（`crates/common/src/types.rs:194-230`）。
- gRPC 健康檢查使用標準 `grpc.health.v1.Health/Check`（`crates/proxy/src/grpc_health.rs:1-60`）。
- 被動健康檢查用 ring buffer 計算失敗率，超過 threshold 標記 unhealthy（`crates/proxy/src/health.rs:1177-1265`）。
- 其他上游能力：連線池、timeouts、upstream TLS、HTTP/1.1 或 HTTP/2、drain、sticky session、deterministic subset、inference/MCP 健康檢查（`crates/config/src/upstreams.rs:287`、`337`、`421`、`456`；`crates/proxy/src/upstream/`）。
- WebSocket：route 的 `websocket` 與 `websocket-inspection` 欄位（`crates/config/src/routes.rs:89-100`），實作在 `crates/proxy/src/websocket/`，可做 frame-level agent inspection。
- SSE：inference route 會初始化 `StreamingTokenCounter`，解析 SSE chunk 計 token（`crates/proxy/src/proxy/http_trait.rs:2617-2644`、`crates/proxy/src/inference/streaming.rs:1-160`）。
- gRPC：沒有專用 `service-type "grpc"`，該值會被視為無效（`crates/config/src/kdl/routes.rs:3330-3370`）；gRPC 走 HTTP/2 透傳，健康檢查另有 gRPC 實作。
- HTTP/3 未實作：`protocol "h3"` 在驗證階段就被拒絕（`crates/config/src/validation.rs:724-730`）。

## 8. TCP/UDP 代理

- **未找到 TCP/UDP 串流代理實作**。
- listener protocol 只有 HTTP 家族：`http`、`https`、`h2`、`h3`、`h2c`（`crates/config/src/server.rs:243-254`）。
- `h3` 解析得到但啟動時拒絕，因為沒有 QUIC（`crates/proxy/src/main.rs:1334-1348`）。
- `h2c` 是 cleartext HTTP/2 prior knowledge，會拒絕非 HTTP/2 preface 的連線（`crates/proxy/src/main.rs:800`、`1094-1153`）。
- L4 相關邏輯只出現在連線限制與 h2c preface 偵測，不是通用 TCP/UDP proxy。

## 9. 可觀測性

- 日誌：
  - access log 格式 `Json` 與 `Combined`（Apache/nginx 風格加 trace_id）（`crates/proxy/src/logging.rs:27-31`）。
  - 可選欄位含 timestamp、trace_id、method、path、query、status、latency、bytes、upstream、connection_reused、rate_limit_hit、geo_country、user_agent、referer、client_ip（`crates/config/src/observability.rs:117-170`）。
  - audit log 事件含 blocked、agent_decision、waf_match、waf_block、rate_limit_exceeded、auth_event、config_change、cert_reload、circuit_breaker_change、cache_purge、admin_action（`crates/proxy/src/logging.rs:301-342`）。
- Metrics：
  - 主要指標 `zentinel_requests_total`、`zentinel_request_duration_seconds`、`zentinel_active_connections`、`zentinel_active_requests`、`zentinel_upstream_requests_total`、`zentinel_agent_requests_total`、`zentinel_circuit_breaker_open`、`zentinel_rate_limited_total`、`zentinel_cache_*`（`crates/proxy/src/metrics.rs:177-363`）。
  - builtin `/metrics` 與獨立 metrics server 共用 `render_prometheus_metrics`，輸出保證一致（`crates/proxy/src/builtin_handlers.rs:237-244`）。
  - 獨立 metrics server 綁在 `observability.metrics.address`，預設 `0.0.0.0:9090`、path `/metrics`（`crates/config/src/observability.rs:340-345`、`crates/proxy/src/metrics_server.rs:1-10`）。
- Tracing：
  - 支援 W3C `traceparent`/`tracestate`（`crates/proxy/src/otel.rs:1-116`）。
  - 設定可寫 `otlp`、`jaeger`、`zipkin`，但 runtime 全部經 OTLP exporter 送往該 endpoint，並非原生 Jaeger/Zipkin 協定（`crates/config/src/observability.rs:323-330`、`crates/proxy/src/otel.rs:146-159`）。
  - Trace ID 預設 TinyFlake，也可選 UUID（`crates/proxy/src/trace_id.rs:90-198`、`crates/common/src/types.rs:100-128`）。

## 10. 部署與發佈

- systemd unit：
  - `ExecReload=/bin/kill -HUP $MAINPID`。
  - `KillMode=mixed`、`KillSignal=SIGTERM`、`TimeoutStopSec=40`。
  - 完整 hardening：`NoNewPrivileges`、`ProtectSystem=strict`、`PrivateTmp`、`SystemCallFilter`、`MemoryMax=2G` 等。
  - `AmbientCapabilities=CAP_NET_BIND_SERVICE` 允許綁 80/443（`deploy/zentinel.service`）。
- `install.sh` 預設只裝 binary 到 `/usr/local/bin`，`--enable-service` 才啟用 systemd，`--skip-service`／`--binary-only` 跳過 service 設定（`install.sh:14-26`、`434-443`）。
- Docker：多階段建置，預設 distroless production image，另有 debug、prebuilt、echo-agent targets（`Dockerfile:1-20`）。
- Bundle：`bundle-versions.lock` 是 agent 版本單一真相來源，binary 編譯時嵌入；CI 用 `scripts/check-bundle-lock.py` 驗證所有 asset URL（`bundle-versions.lock:1-40`）。
- Release：CalVer tag `YY.MM_PATCH` 觸發；先建置所有平台與 Docker，成功後才 bump semver、發佈 crates.io、建立 GitHub release（`.github/workflows/release.yml:1-36`、`193-255`）。

## 11. 授權與 toolchain

- 授權：Apache License 2.0（`LICENSE:1-5`、`Cargo.toml:25`）。
- Rust toolchain：channel `1.99.1`，含 rustfmt、clippy（`rust-toolchain.toml`）。
- workspace 版本 `0.6.54`，`rust-version = "1.99.0"`（`Cargo.toml:21-29`）。
- Pingora 使用 `zentinel-pingora*` fork，版本 0.9.x（`Cargo.toml:32-40`）。

## 12. 測試範圍

- 單元與整合測試：CI 跑 `cargo test --workspace --all-features`（`.github/workflows/tests.yml:79-80`）。
- Docker integration suite：`tests/integration_test.sh`，CI 另有 quick 模式（`.github/workflows/integration.yml:77-81`）。
- 情境測試：`tests/scenarios/` 含 concurrent reload、echo agent、rate limit agent、TLS cert rotation、mTLS、SNI、WAF agent、WAF config block。
- 其他測試腳本：`tests/test_agents.sh`、`tests/test_mega_agent.sh`、`tests/test_waf.sh`、`tests/test_inline_openapi.sh`、`tests/test_first_time_waf.sh`、`tests/test_first_time_lua.sh`。
- 壓測與混沌：`tests/chaos/`、`tests/soak/`，含 docker-compose、分析腳本與設定。
- Kubernetes Gateway API conformance：`conformance/`（Go、kind cluster）與 `sdk-conformance/`（reference agent、runner）。
- bundle 端到端：`tests/bundle_e2e_test.sh`、`tests/bundle_integration_test.sh`。

## 未驗證項目

- 沒有實際啟動 proxy、跑 `cargo test`、跑 integration/chaos/soak，所有行為結論都來自原始碼閱讀。
- Reverse connection 只確認 protocol crate 有實作；proxy 是否透過其他未找到的路徑接線，未驗證。
- WAF agent 的實際偵測與阻擋行為未執行驗證；本報告只確認核心 proxy 的 WAF 引擎尚未接線。
- HTTP/3、QUIC、真實 ACME 對 CA 的簽發流程、Consul/Kubernetes discovery 都沒有實際運行。
- Metrics 完整指標清單未逐項列舉，只列出主要指標。
- 外部文件站 `docs.zentinelproxy.io` 與文件 repo 未在本輪核對。

## 本輪經驗

- `service-type` 不是任意字串：`grpc` 不是合法 service type，gRPC 只走 HTTP/2 透傳；要判斷代理能力不能只看設定欄位存在。
- WAF 的 `mode detection`／`prevention` 目前是啟動 hard error，不是「有設定就有防護」；這類 parsed-but-unwired 的欄位要看 `validate_implementation_status`。
- Zentinel 沒有 HTTP reload API，重載靠 SIGHUP、檔案監控與 Gateway controller；admin 端點是 builtin route，不是獨立 control plane。
- 預設 embedded 設定的 admin listener 綁 `0.0.0.0:9090`，且 builtin handler 無內建認證，部署時要自己收斂。

Skills：無