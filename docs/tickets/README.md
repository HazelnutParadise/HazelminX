# 票務索引

每張票是一件使用者可以做的事，切過它需要的所有層。票檔是驗收條件的唯一來源；GitHub issue 與票檔同步，PR 以票號命名分支 `ticket/<id>-<slug>`。

狀態：`ready` 所有阻擋票已完成，可立即領取；`blocked` 仍有阻擋票；`in_progress` 已有分支與草稿 PR；`done` 已合併並驗收。

## M0 基礎與設計

| 票 | 使用者可以做什麼 | Epic | Blocked by | Issue | 狀態 |
|---|---|---|---|---|---|
| [001](001-repo-scaffold.md) | 維護者可以在單一 repo 取得規格、架構決策、票務與工作規則 | 基礎建設 | — | [#1](https://github.com/HazelnutParadise/hazelminx/issues/1) | done |
| [002](002-design-system.md) | 設計師與開發者可以依 DESIGN.md 與互動原型審查主要畫面 | 設計系統 | — | [#2](https://github.com/HazelnutParadise/hazelminx/issues/2) | in_progress |
| [003](003-core-skeleton.md) | 管理員可以啟動 hazelminx-core 並通過健康檢查 | 基礎建設 | 001 | [#3](https://github.com/HazelnutParadise/hazelminx/issues/3) | done |
| [004](004-agent-skeleton.md) | 管理員可以在主機上以 systemd 執行 hazelminx-agent 並回報身分 | 節點與 Agent | 001 | [#4](https://github.com/HazelnutParadise/hazelminx/issues/4) | done |
| [005](005-web-skeleton.md) | 使用者可以開啟 React 介面並切換語言與淺深色 | 基礎建設 | 001, 002 | [#5](https://github.com/HazelnutParadise/hazelminx/issues/5) | done |
| [006](006-ci-pipeline.md) | 維護者可以在 CI 建置、測試並產生各架構成品 | 發布與供應鏈 | 003, 004, 005 | [#6](https://github.com/HazelnutParadise/hazelminx/issues/6) | in_progress |
| [007](007-core-agent-protocol.md) | Core 與 Agent 可以用版本化、雙向驗證的通道交換能力與工作事件 | 節點與 Agent | 003, 004 | [#7](https://github.com/HazelnutParadise/hazelminx/issues/7) | done |
| [008](008-job-engine.md) | 使用者可以提交工作後關閉頁面，之後仍能查到進度、證據與結果 | 基礎建設 | 003 | [#8](https://github.com/HazelnutParadise/hazelminx/issues/8) | done |
| [009](009-iam-foundation.md) | 擁有者可以登入、建立帳號與角色，未授權的 API 呼叫一律被拒絕 | 身分與存取 | 003 | [#9](https://github.com/HazelnutParadise/hazelminx/issues/9) | done |
| [010](010-management-api-contract.md) | 整合者可以用 /management/v1 的分頁、版本條件與工作回應操作資源 | API、CLI 與整合 | 003, 008 | [#10](https://github.com/HazelnutParadise/hazelminx/issues/10) | done |
| [011](011-secrets-and-master-key.md) | 管理員可以安全保存機密，且主金鑰可輪替並驗證舊備份仍可解密 | 基礎建設 | 003 | [#11](https://github.com/HazelnutParadise/hazelminx/issues/11) | done |
| [012](012-privileged-executor.md) | 維運者可以執行需要高權限的固定動作，而 Agent 主程序維持低權限 | 節點與 Agent | 004 | [#12](https://github.com/HazelnutParadise/hazelminx/issues/12) | done |
| [113](113-host-change-ledger.md) | 平台對主機的每個變更都寫進變更帳本，並在安裝時記錄安裝前快照 | 主機 | 012 | [#120](https://github.com/HazelnutParadise/hazelminx/issues/120) | done |
| [092](092-baseline-inventory.md) | 驗收者可以查每項 1Panel v2.4.1 OSS 功能在 HazelminX 的對應位置與狀態 | 品質與驗收 | 001 | [#91](https://github.com/HazelnutParadise/hazelminx/issues/91) | in_progress |
| [095](095-branding-and-legal.md) | 使用者看到的品牌是 HazelminX，法律與授權資訊仍可讀且來源正確 | 基礎建設 | 005 | [#94](https://github.com/HazelnutParadise/hazelminx/issues/94) | ready |

## M1 單機主機管理

| 票 | 使用者可以做什麼 | Epic | Blocked by | Issue | 狀態 |
|---|---|---|---|---|---|
| [013](013-one-line-install.md) | 管理員可以一行安裝單機模式、重開機後登入，且安裝不需要任何編譯器 | 安裝、更新與卸載 | 003, 004, 009, 012 | [#13](https://github.com/HazelnutParadise/hazelminx/issues/13) | ready |
| [014](014-mfa-and-sessions.md) | 使用者可以設定 TOTP、Passkey、復原碼並檢視與撤銷自己的工作階段 | 身分與存取 | 009 | [#14](https://github.com/HazelnutParadise/hazelminx/issues/14) | done |
| [015](015-users-roles-tokens.md) | 擁有者可以管理使用者、角色、資源範圍與有期限的 API Token | 身分與存取 | 009, 010 | [#15](https://github.com/HazelnutParadise/hazelminx/issues/15) | ready |
| [016](016-change-plans.md) | 管理員可以預覽並批准高風險變更計畫，內容改變時原批准失效 | 身分與存取 | 008, 010 | [#16](https://github.com/HazelnutParadise/hazelminx/issues/16) | ready |
| [017](017-ui-shell.md) | 使用者可以在主導覽、全域搜尋與工作中心之間完成日常操作 | 基礎建設 | 005, 008, 010 | [#17](https://github.com/HazelnutParadise/hazelminx/issues/17) | ready |
| [018](018-platform-settings.md) | 管理員可以設定面板名稱、時區、入口、通知、更新通道與遙測 | 基礎建設 | 017 | [#18](https://github.com/HazelnutParadise/hazelminx/issues/18) | blocked |
| [019](019-node-enrollment.md) | 管理員可以用一次性註冊碼加入節點，並看到離線、過期、維護與撤銷狀態 | 節點與 Agent | 004, 007, 009 | [#19](https://github.com/HazelnutParadise/hazelminx/issues/19) | blocked |
| [020](020-host-overview-monitoring.md) | 維運者可以查看主機資訊、資源指標與歷史趨勢 | 主機管理 | 019, 017 | [#20](https://github.com/HazelnutParadise/hazelminx/issues/20) | blocked |
| [021](021-packages-and-updates.md) | 維運者可以搜尋、安裝、更新、移除套件並套用安全更新 | 主機管理 | 019, 012 | [#21](https://github.com/HazelnutParadise/hazelminx/issues/21) | blocked |
| [022](022-systemd-services.md) | 維運者可以管理 systemd 服務、計時器與其設定檔 | 主機管理 | 012 | [#22](https://github.com/HazelnutParadise/hazelminx/issues/22) | ready |
| [023](023-processes-users-sudo.md) | 維運者可以查看程序並管理 Linux 使用者、群組與 sudo | 主機管理 | 012 | [#23](https://github.com/HazelnutParadise/hazelminx/issues/23) | ready |
| [024](024-network-and-firewall.md) | 維運者可以管理網路、DNS、主機名稱、時間服務與防火牆 | 主機管理 | 012, 008 | [#24](https://github.com/HazelnutParadise/hazelminx/issues/24) | ready |
| [025](025-reboot-shutdown.md) | 維運者可以重新開機或關機，並看到受影響的工作與服務 | 主機管理 | 008, 012 | [#25](https://github.com/HazelnutParadise/hazelminx/issues/25) | ready |
| [026](026-ssh-keys-and-sshd.md) | 維運者可以管理 SSH 公鑰、sshd 設定與主機指紋 | SSH 與終端機 | 012 | [#26](https://github.com/HazelnutParadise/hazelminx/issues/26) | ready |
| [027](027-web-terminal.md) | 使用者可以在網頁使用授權終端機，且稽核政策可設定 | SSH 與終端機 | 019, 009, 012 | [#27](https://github.com/HazelnutParadise/hazelminx/issues/27) | blocked |
| [028](028-file-manager.md) | 使用者可以瀏覽、整理、授權與追蹤宿主機檔案 | 檔案管理 | 012, 017 | [#28](https://github.com/HazelnutParadise/hazelminx/issues/28) | blocked |
| [029](029-file-transfer-edit.md) | 使用者可以上傳、下載、續傳大檔，線上編輯、壓縮與安全預覽 | 檔案管理 | 028, 010 | [#29](https://github.com/HazelnutParadise/hazelminx/issues/29) | blocked |
| [030](030-disk-overview-alerts.md) | 維運者可以查看磁碟、分割區、檔案系統、健康與容量告警 | 磁碟與儲存 | 019, 020 | [#30](https://github.com/HazelnutParadise/hazelminx/issues/30) | blocked |
| [031](031-disk-operations.md) | 維運者可以分割、格式化、掛載磁碟並管理 LVM、RAID、LUKS 與網路儲存 | 磁碟與儲存 | 012, 016, 030 | [#31](https://github.com/HazelnutParadise/hazelminx/issues/31) | blocked |
| [032](032-storage-bindings.md) | 管理員可以設定資料位置並以裝置身分綁定，掛錯磁碟時服務不啟動 | 磁碟與儲存 | 031, 008 | [#32](https://github.com/HazelnutParadise/hazelminx/issues/32) | blocked |
| [033](033-move-app-data.md) | 管理員可以把應用資料安全搬到另一顆磁碟 | 磁碟與儲存 | 032, 016 | [#33](https://github.com/HazelnutParadise/hazelminx/issues/33) | blocked |
| [034](034-baseline-host-tools.md) | 維運者可以管理 FTP／SFTP、Fail2ban、防毒等基線工具 | 主機管理 | 021, 022 | [#34](https://github.com/HazelnutParadise/hazelminx/issues/34) | blocked |
| [035](035-logs-retention-diagnostics.md) | 管理員可以設定日誌與統計容量、保存政策並產生遮罩診斷包 | 監控與告警 | 003, 017 | [#35](https://github.com/HazelnutParadise/hazelminx/issues/35) | blocked |
| [036](036-platform-update.md) | 管理員可以更新 HazelminX，失敗時回到相容舊版本或進入明確復原 | 安裝、更新與卸載 | 013, 008, 007 | [#36](https://github.com/HazelnutParadise/hazelminx/issues/36) | blocked |
| [037](037-offline-install-uninstall.md) | 管理員可以用離線包安裝，並解除安裝平台而保留應用與移交維護 | 安裝、更新與卸載 | 013 | [#37](https://github.com/HazelnutParadise/hazelminx/issues/37) | blocked |
| [038](038-recovery-mode.md) | 擁有者可以在資料庫損壞或鎖死時用離線救援方式恢復管理 | 安裝、更新與卸載 | 013 | [#38](https://github.com/HazelnutParadise/hazelminx/issues/38) | blocked |
| [107](107-resource-state-drift-impact.md) | 維運者可以看到每個資源由誰管理、與平台紀錄差在哪，並在寫入前看到影響範圍 | 基礎建設 | 007, 008, 010 | [#114](https://github.com/HazelnutParadise/hazelminx/issues/114) | blocked |

規格增補 03（`docs/spec/addendum-03-robustness.md`，異常情境的整體處理，ROB-001～015、T-157～188、T-231～233 與 T-242～243），2026-11-07 使用者核定，ROB-013～015 於 2026-11-08 加入。ROB-001／002／003／006／012 由票 107 擁有，ROB-004／005／007／008／013 由 017、ROB-009 由 020、ROB-010／014 由 024、ROB-011 由 081、ROB-015 由 035 擁有；010、027、028、040 另有驗證條目。

## M2 容器、Compose 與 GitHub

| 票 | 使用者可以做什麼 | Epic | Blocked by | Issue | 狀態 |
|---|---|---|---|---|---|
| [039](039-engine-detect-install.md) | 管理員可以看到 Docker、Podman、K3s 各自狀態並在頁面一鍵安裝 Docker | 容器引擎 | 019, 016, 012 | [#39](https://github.com/HazelnutParadise/hazelminx/issues/39) | blocked |
| [040](040-container-management.md) | 容器管理員可以建立、檢視、操作容器並查看日誌、終端機與暴露範圍 | 容器引擎 | 039, 027 | [#40](https://github.com/HazelnutParadise/hazelminx/issues/40) | blocked |
| [041](041-images-registries-networks-volumes.md) | 管理員可以管理映像、登錄站、容器網路與 Volume 並採用安全更新 | 容器引擎 | 039 | [#41](https://github.com/HazelnutParadise/hazelminx/issues/41) | blocked |
| [042](042-container-takeover-engine-settings.md) | 管理員可以匯入既有容器選擇接管，並管理引擎設定、儲存位置與 GPU 裝置 | 容器引擎 | 040, 032 | [#42](https://github.com/HazelnutParadise/hazelminx/issues/42) | blocked |
| [043](043-swarm.md) | 管理員可以管理 Swarm 節點、服務、Stack、Secret 與更新策略 | 容器引擎 | 041 | [#43](https://github.com/HazelnutParadise/hazelminx/issues/43) | blocked |
| [044](044-compose-projects.md) | 專案管理員可以建立專案與環境，匯入完整 Compose 並驗證後部署 | Compose 部署 | 039, 008, 016 | [#44](https://github.com/HazelnutParadise/hazelminx/issues/44) | blocked |
| [045](045-env-layers.md) | 管理員可以分層管理 .env、介面變數與覆寫層，並預覽每個服務實際收到的值 | Compose 部署 | 044, 011 | [#45](https://github.com/HazelnutParadise/hazelminx/issues/45) | blocked |
| [046](046-deploy-revisions.md) | 管理員可以查看部署版本、三方比較並回到相容舊版 | Compose 部署 | 045 | [#46](https://github.com/HazelnutParadise/hazelminx/issues/46) | blocked |
| [047](047-github-connections.md) | 使用者可以用 GitHub 網頁授權或 PAT 建立可重用連線，並保存多帳號多授權 | GitHub 來源 | 009, 011, 010 | [#47](https://github.com/HazelnutParadise/hazelminx/issues/47) | ready |
| [048](048-repo-picker.md) | 使用者可以瀏覽、搜尋、分頁載入可用 Repo，並選分支、commit 與 Compose 檔案 | GitHub 來源 | 047, 017 | [#48](https://github.com/HazelnutParadise/hazelminx/issues/48) | blocked |
| [049](049-git-deploy.md) | 專案管理員可以從 GitHub 以固定 commit 隔離抓取、建置並部署，手動 URL／PAT／SSH 模式照常可用 | GitHub 來源 | 048, 044, 012 | [#49](https://github.com/HazelnutParadise/hazelminx/issues/49) | blocked |
| [050](050-auto-deploy-webhooks.md) | 管理員可以設定 Webhook 或定期檢查觸發部署，並決定多環境與連續 commit 政策 | GitHub 來源 | 049 | [#50](https://github.com/HazelnutParadise/hazelminx/issues/50) | blocked |
| [051](051-connection-sharing-rotation.md) | 管理員可以分開授權連線的查看、瀏覽、使用與管理，並安全輪替、撤銷與斷開 | GitHub 來源 | 049, 016 | [#51](https://github.com/HazelnutParadise/hazelminx/issues/51) | blocked |
| [108](108-project-units-connections.md) | 專案管理員可以在一個專案環境放多個 Compose 與 Kubernetes 部署單元，單元之間用公開變數與內部位址連線 | Compose 部署 | 044, 049, 086, 088 | [#115](https://github.com/HazelnutParadise/hazelminx/issues/115) | blocked |
| [109](109-env-groups-and-files.md) | 使用者可以把環境變數分成以檔名命名的組，在每個填變數的地方上傳任意檔名的 env 檔，並看到每個服務實際收到的值 | Compose 部署 | 045, 017 | [#116](https://github.com/HazelnutParadise/hazelminx/issues/116) | blocked |
| [110](110-compose-semantics-zero-downtime.md) | 部署照 Compose 與 Helm 的實際讀法處理多份 compose 檔、profile、建置變數、設定檔與 values，並且每次建置、建置完才無縫替換 | Compose 部署 | 044, 046, 108, 109 | [#117](https://github.com/HazelnutParadise/hazelminx/issues/117) | blocked |
| [052](052-appstore-install.md) | 管理員可以同步官方商店並安裝整套應用，選擇節點、網路、Port 與資料位置 | 應用商店 | 044, 032 | [#52](https://github.com/HazelnutParadise/hazelminx/issues/52) | blocked |
| [053](053-appstore-update-uninstall.md) | 管理員可以編輯、更新與卸載商店應用，自訂設定不被新版覆寫 | 應用商店 | 052, 046 | [#53](https://github.com/HazelnutParadise/hazelminx/issues/53) | blocked |
| [054](054-appstore-sources.md) | 管理員可以新增受信任的第三方商店來源與本機範本 | 應用商店 | 052 | [#54](https://github.com/HazelnutParadise/hazelminx/issues/54) | blocked |
| [112](112-app-template-network-mounts.md) | 從 1Panel 商店安裝時轉換範本（網路、Port、資料路徑、資料庫連線），應用的網路與資料位置安裝時能設、之後也能隨時改 | 應用商店 | 052, 053, 109 | [#119](https://github.com/HazelnutParadise/hazelminx/issues/119) | blocked |
| [114](114-compose-deploy-compat.md) | 部署前讀懂既有 compose 的寫法（外部網路、容器名稱、主機路徑、主機建置、寫死的機密、對外 Port、GPU、浮動標籤），不用改 repo 就能安全部署 | Compose 部署 | 044, 049, 107, 109 | [#130](https://github.com/HazelnutParadise/hazelminx/issues/130) | blocked |

## M3 網站入口與安全防護

| 票 | 使用者可以做什麼 | Epic | Blocked by | Issue | 狀態 |
|---|---|---|---|---|---|
| [055](055-zentinel-install.md) | 管理員可以從介面安裝原生 Zentinel，Core 重啟不影響既有網站，救援入口不依賴它 | 網站入口 | 021, 012, 016 | [#55](https://github.com/HazelnutParadise/hazelminx/issues/55) | blocked |
| [056](056-websites-and-routing.md) | 使用者可以建立靜態與反向代理網站、綁定多網域並穩定路由到容器服務 | 網站入口 | 055, 044 | [#56](https://github.com/HazelnutParadise/hazelminx/issues/56) | blocked |
| [057](057-certificates-acme.md) | 管理員可以申請、匯入、續期憑證並管理 ACME 帳號與 DNS 供應商 | 網站入口 | 056 | [#57](https://github.com/HazelnutParadise/hazelminx/issues/57) | blocked |
| [058](058-proxy-features.md) | 使用者可以設定重導、重寫、標頭、快取、壓縮、上傳限制、多後端與串流代理 | 網站入口 | 056 | [#58](https://github.com/HazelnutParadise/hazelminx/issues/58) | blocked |
| [059](059-language-runtimes.md) | 使用者可以建立 PHP、Java、Node.js、Go、Python、.NET 執行環境網站 | 網站入口 | 056, 044 | [#59](https://github.com/HazelnutParadise/hazelminx/issues/59) | blocked |
| [060](060-tcp-udp-proxy.md) | 使用者可以設定 TCP／UDP 代理與連接埠轉送 | 網站入口 | 055 | [#60](https://github.com/HazelnutParadise/hazelminx/issues/60) | blocked |
| [062](062-token-api-proxy.md) | 管理員可以發布 infra-manager 相容的 Token API 代理，既有客戶端不需修改 | API 代理 | 055, 015 | [#61](https://github.com/HazelnutParadise/hazelminx/issues/61) | blocked |
| [063](063-infra-manager-import.md) | 管理員可以匯入既有 infra-manager 的服務、Token 與使用者 | API 代理 | 062 | [#62](https://github.com/HazelnutParadise/hazelminx/issues/62) | blocked |
| [064](064-waf.md) | 安全管理員可以安裝 WAF，逐站設定模式、規則、例外與失效政策 | WAF 與可信 IP | 055 | [#63](https://github.com/HazelnutParadise/hazelminx/issues/63) | blocked |
| [065](065-trusted-ip-tunnel.md) | 管理員可以為每個入口設定可信代理模式，Cloudflare Tunnel 流量取得正確訪客 IP | WAF 與可信 IP | 055, 064 | [#64](https://github.com/HazelnutParadise/hazelminx/issues/64) | blocked |
| [066](066-traffic-analytics.md) | 使用者可以依時間、節點、網域與入口查看流量、來源、路徑、裝置與 API 用量 | 流量分析 | 065, 035 | [#65](https://github.com/HazelnutParadise/hazelminx/issues/65) | blocked |
| [067](067-cloudflare-analytics.md) | 管理員可以連接 Cloudflare Analytics 並分開看邊緣與來源站 | 流量分析 | 066 | [#66](https://github.com/HazelnutParadise/hazelminx/issues/66) | blocked |
| [068](068-openresty-migration.md) | 管理員可以把 OpenResty 網站遷移到 Zentinel，無法轉換的項目先列出 | 遷移與接管 | 056, 059, 060 | [#67](https://github.com/HazelnutParadise/hazelminx/issues/67) | blocked |

## M4 資料庫、AI、備份與監控

| 票 | 使用者可以做什麼 | Epic | Blocked by | Issue | 狀態 |
|---|---|---|---|---|---|
| [069](069-database-services.md) | 管理員可以安裝或接管資料庫服務，管理帳號、連線、健康與對外開放 | 資料庫 | 052, 040 | [#68](https://github.com/HazelnutParadise/hazelminx/issues/68) | blocked |
| [070](070-sql-data-editor.md) | 使用者可以瀏覽與編輯 PostgreSQL 與 MySQL 的結構與資料並執行 SQL | 資料庫 | 069 | [#69](https://github.com/HazelnutParadise/hazelminx/issues/69) | blocked |
| [071](071-redis-manager.md) | 使用者可以管理 Redis 鍵、TTL、ACL、持久化與拓樸 | 資料庫 | 069 | [#70](https://github.com/HazelnutParadise/hazelminx/issues/70) | blocked |
| [072](072-mongodb-manager.md) | 使用者可以管理 MongoDB 實例、集合、文件與索引 | 資料庫 | 069 | [#71](https://github.com/HazelnutParadise/hazelminx/issues/71) | blocked |
| [073](073-database-upgrade-backup.md) | 管理員可以執行資料庫備份、還原與大版本升級 | 資料庫 | 070, 016 | [#72](https://github.com/HazelnutParadise/hazelminx/issues/72) | blocked |
| [074](074-ollama-models.md) | 管理員可以安裝或接管 Ollama 並管理模型的下載、刪除、複製與配額 | AI 與模型 | 052 | [#73](https://github.com/HazelnutParadise/hazelminx/issues/73) | blocked |
| [075](075-gpu-chat-inference-api.md) | 管理員可以選 GPU、在網頁測試模型，並透過 Token 代理發布推論 API | AI 與模型 | 074, 062 | [#74](https://github.com/HazelnutParadise/hazelminx/issues/74) | blocked |
| [076](076-baseline-ai-mcp.md) | 管理員可以保留基線 AI／MCP／Agent 能力，MCP 預設唯讀且危險操作需批准 | AI 與模型 | 074, 015 | [#75](https://github.com/HazelnutParadise/hazelminx/issues/75) | blocked |
| [111](111-model-hub-hf-runtime.md) | 管理員可以建立多個模型庫、在面板搜尋並下載 Hugging Face 模型，再用內建的 vLLM 或 llama.cpp 部署推論服務 | AI 與模型 | 074, 075 | [#118](https://github.com/HazelnutParadise/hazelminx/issues/118) | blocked |
| [115](115-object-storage.md) | 管理員可以在指定主機與磁碟建立物件儲存（預設 RustFS），管理 bucket 與限定範圍的金鑰，給應用用、當備份目的地，並接管或遷移既有 MinIO | 資料庫 | 039, 040, 077, 109 | [#131](https://github.com/HazelnutParadise/hazelminx/issues/131) | blocked |
| [077](077-backup-accounts.md) | 管理員可以建立本機、S3、SFTP、WebDAV、雲端硬碟與 NAS 備份目的地並測試讀寫 | 備份與還原 | 011, 017 | [#76](https://github.com/HazelnutParadise/hazelminx/issues/76) | blocked |
| [078](078-backup-jobs.md) | 使用者可以備份應用、資料庫、目錄與平台設定，含一致性、多目的地、保留與加密 | 備份與還原 | 077, 069, 046 | [#77](https://github.com/HazelnutParadise/hazelminx/issues/77) | blocked |
| [079](079-restore.md) | 管理員可以隔離演練還原、預覽覆寫並還原到不同節點與磁碟 | 備份與還原 | 078 | [#78](https://github.com/HazelnutParadise/hazelminx/issues/78) | blocked |
| [080](080-cron-jobs.md) | 管理員可以建立計畫任務，設定重疊、錯過、重試、依賴、通知與腳本庫 | 計畫任務 | 008, 017 | [#79](https://github.com/HazelnutParadise/hazelminx/issues/79) | blocked |
| [081](081-alerts-notifications.md) | 管理員可以設定告警、通知管道與靜音，並從告警跳到相關資源 | 監控與告警 | 020, 018 | [#80](https://github.com/HazelnutParadise/hazelminx/issues/80) | blocked |
| [082](082-self-monitoring-metrics.md) | 管理員可以監控 HazelminX 自己並輸出標準監控指標 | 監控與告警 | 020 | [#81](https://github.com/HazelnutParadise/hazelminx/issues/81) | blocked |
| [083](083-integrations-export-cli.md) | 管理員可以匯出匯入設定、收發整合 Webhook，並用 CLI 查看版本、診斷與升級 | API、CLI 與整合 | 010, 016 | [#82](https://github.com/HazelnutParadise/hazelminx/issues/82) | blocked |

## M5 多節點、叢集、遷移與發布

| 票 | 使用者可以做什麼 | Epic | Blocked by | Issue | 狀態 |
|---|---|---|---|---|---|
| [084](084-multi-node-views.md) | 管理員可以在單一畫面查看與批次操作跨節點資源 | 節點與 Agent | 019, 017 | [#83](https://github.com/HazelnutParadise/hazelminx/issues/83) | blocked |
| [085](085-cross-node-copy.md) | 使用者可以跨節點複製資料並確認 UID／GID 映射 | 檔案管理 | 028, 084 | [#84](https://github.com/HazelnutParadise/hazelminx/issues/84) | blocked |
| [086](086-kubernetes-connect.md) | 管理員可以連接既有 Kubernetes／K3s 並管理工作負載、Service、設定與診斷 | Kubernetes 與叢集 | 019 | [#85](https://github.com/HazelnutParadise/hazelminx/issues/85) | blocked |
| [087](087-k3s-cluster-lifecycle.md) | 管理員可以建立 K3s 叢集、加入／移除／升級節點並建立高可用 | Kubernetes 與叢集 | 086, 055, 016 | [#86](https://github.com/HazelnutParadise/hazelminx/issues/86) | blocked |
| [088](088-kubernetes-helm-storage-yaml.md) | 使用者可以安裝 Helm Chart、管理儲存、匯入 YAML、備份 datastore 並安排維護 | Kubernetes 與叢集 | 086 | [#87](https://github.com/HazelnutParadise/hazelminx/issues/87) | blocked |
| [089](089-zero-downtime-deploy.md) | 管理員可以在符合條件時不中斷部署，不符合時看到具體原因與回復選項 | 不中斷部署 | 046, 056, 008 | [#88](https://github.com/HazelnutParadise/hazelminx/issues/88) | blocked |
| [090](090-control-generation.md) | 系統可以用控制世代與資源擁有者阻止舊控制中心或第二個控制器寫入 | 節點與 Agent | 008, 019 | [#89](https://github.com/HazelnutParadise/hazelminx/issues/89) | blocked |
| [091](091-migrate-from-1panel.md) | 管理員可以從 1Panel 乾跑匯入並分批接管，原服務未批准前不停止 | 遷移與接管 | 053, 056, 069, 078 | [#90](https://github.com/HazelnutParadise/hazelminx/issues/90) | blocked |
| [093](093-windows-agent.md) | 管理員可以把 Windows Server 加為受管節點（條件式範圍，待核定） | Windows 節點 | 019, 012 | [#92](https://github.com/HazelnutParadise/hazelminx/issues/92) | blocked |
| [094](094-disaster-recovery.md) | 管理員可以在控制中心完全損毀後用救援工具與復原金鑰在乾淨主機還原 | 備份與還原 | 078, 013 | [#93](https://github.com/HazelnutParadise/hazelminx/issues/93) | blocked |
| [096](096-mobile-keyboard-a11y.md) | 使用者可以在 360px 手機與只用鍵盤的情況完成日常操作 | 品質與驗收 | 017 | [#95](https://github.com/HazelnutParadise/hazelminx/issues/95) | blocked |
| [097](097-release-supply-chain.md) | 維護者可以發布附簽署清單、SBOM、授權與支援矩陣的正式 Release | 發布與供應鏈 | 006, 036 | [#96](https://github.com/HazelnutParadise/hazelminx/issues/96) | blocked |
| [098](098-release-gates-traceability.md) | 驗收者可以依 G-01 至 G-14 門檻與需求→測試→證據追溯表判定可否發布 | 品質與驗收 | 092, 097 | [#97](https://github.com/HazelnutParadise/hazelminx/issues/97) | blocked |
| [099](099-performance-budget.md) | 維護者可以依 38 章情境量測效能並附測試條件 | 品質與驗收 | 084, 066 | [#98](https://github.com/HazelnutParadise/hazelminx/issues/98) | blocked |

## M6 宿主機服務模組（Webmin 等價）

規格增補 01（`docs/spec/addendum-01-host-service-modules.md`），2026-11-07 使用者核定納入。

規格增補 02（`docs/spec/addendum-02-multi-environment.md`，同一台主機上的多個引擎環境，ENV-001～009、T-149～156），2026-11-07 使用者核定，驗收條件併入票 039、040、042、086。

| 票 | 使用者可以做什麼 | Epic | Blocked by | Issue | 狀態 |
|---|---|---|---|---|---|
| [100](100-host-service-module-framework.md) | 維運者可以在通用框架裡看模組狀態、結構化或原始編輯設定、套用前驗證並回滾 | 宿主機服務模組 | 022, 012, 029 | [#107](https://github.com/HazelnutParadise/hazelminx/issues/107) | blocked |
| [101](101-dns-dhcp-servers.md) | 維運者可以管理 BIND 權威 DNS 與 DHCP（ISC／Kea）的 zone、紀錄、子網、租約 | 宿主機服務模組 | 100 | [#108](https://github.com/HazelnutParadise/hazelminx/issues/108) | blocked |
| [102](102-mail-servers.md) | 維運者可以管理 Postfix／Dovecot 的網域、信箱、別名、TLS 與佇列 | 宿主機服務模組 | 100, 011 | [#109](https://github.com/HazelnutParadise/hazelminx/issues/109) | blocked |
| [103](103-file-sharing-servers.md) | 維運者可以管理 Samba 分享與 NFS exports 並看目前連線 | 宿主機服務模組 | 100, 023 | [#110](https://github.com/HazelnutParadise/hazelminx/issues/110) | blocked |
| [104](104-native-web-servers.md) | 維運者可以管理宿主機原生 Apache／Nginx 的虛擬主機與模組，並與 Zentinel 分工 | 宿主機服務模組 | 100, 060 | [#111](https://github.com/HazelnutParadise/hazelminx/issues/111) | blocked |
| [105](105-logrotate-and-quotas.md) | 維運者可以管理 logrotate 與磁碟配額 | 宿主機服務模組 | 100, 030 | [#112](https://github.com/HazelnutParadise/hazelminx/issues/112) | blocked |
| [106](106-pam-mime-printers.md) | 維運者可以管理 PAM、MIME 對映與 CUPS 印表機 | 宿主機服務模組 | 100, 009 | [#113](https://github.com/HazelnutParadise/hazelminx/issues/113) | blocked |

規格增補 04（`docs/spec/addendum-04-project-units.md`，專案的部署單元、單元之間的連線與 env 檔，PRJ-001～015、T-189～220），2026-11-08 使用者核定。由票 108、109、110 擁有。

規格增補 05（`docs/spec/addendum-05-model-hub.md`，內建 Hugging Face 模型 runtime 與模型庫，HFM-001～008、T-221～230），2026-11-08 使用者核定。由票 111 擁有。

規格增補 06（`docs/spec/addendum-06-app-network-mounts.md`，1Panel 商店範本的轉換，以及應用的網路與資料掛載，APX-001～008、T-234～241），2026-11-08 使用者核定（含 54.1 網路模型）。由票 112 擁有。

規格增補 07（`docs/spec/addendum-07-clean-uninstall.md`，主機變更帳本與完全移除，UNX-001～007、T-244～251），2026-11-08 使用者核定。UNX-001／002 由票 113 擁有，UNX-003～007 由 037 擁有。

規格增補 08（`docs/spec/addendum-08-git-deploy-objstore.md`，既有 Compose 專案的部署相容、分支觸發與物件儲存，GDP-001～014、OBJ-001～008、T-252～267），2026-11-08 使用者核定。GDP-001～010 由票 114 擁有，GDP-011～014 由 050 擁有，OBJ-001～008 由 115 擁有。

規格增補 09（`docs/spec/addendum-09-fail2ban.md`，Fail2ban 管理，F2B-001～008、T-268～275），2026-11-08 使用者核定。由票 034 擁有。

## 覆蓋檢查

- 規格需求 449 條＋增補 01 的 14 條＋增補 02 的 9 條＋增補 03 的 15 條＋增補 04 的 15 條＋增補 05 的 8 條＋增補 06 的 8 條＋增補 07 的 7 條＋增補 08 的 22 條＋增補 09 的 8 條，已指派擁有票 555 條，未指派 0 條。
- 驗收案例 186 個＋增補 01 的 9 個＋增補 02 的 8 個＋增補 03 的 37 個＋增補 04 的 40 個＋增補 05 的 10 個＋增補 06 的 8 個＋增補 07 的 8 個＋增補 08 的 16 個＋增補 09 的 8 個，已連到票 330 個。
- 本檔由 `docs/tickets/traceability.json` 同步產生；修改票的需求範圍時同時更新兩者。
