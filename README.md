# HazelminX

自己架設的伺服器管理中心。打開一個網站，就能管理多台 Linux 主機、容器、網站、資料庫、AI 模型、檔案、磁碟、備份與安全防護，不必在 1Panel、Portainer、Webmin、資料庫工具與 SSH 之間來回切換。

## 狀態

規劃階段。產品規格 v1.1 已整併（48 章、449 條需求、186 個驗收案例），票務已切好，尚未有可執行的產品。所有「必須」都是要求，不是已完成的能力。

## 這套系統怎麼分工

| 元件 | 語言 | 負責 |
|---|---|---|
| Web UI（`web/`） | React、TypeScript、Vite+ | 讓人看清楚狀態、預覽差異、下決定 |
| Core（`core/`） | Go | 管理 API、權限、工作協調、設定版本、稽核；以 1Panel v2.4.1 OSS 的業務邏輯為基底 |
| Agent（`agent/`） | Rust | 每台主機上的執行者：偵測、監控、檔案、容器、宿主機變更；以 Komodo Periphery 為改造起點 |
| 高權限執行器 | Rust | 只接受固定動作的最小高權限步驟 |
| `hazelminx` CLI | Rust | 安裝、更新、診斷、救援、卸載 |
| Zentinel | 原生 systemd 服務 | 網站與 API 流量入口、TLS、WAF、可信訪客 IP |

一句話：網頁讓人看清楚並下決定，Go 中心安排工作，Rust 主機服務執行操作，Zentinel 處理訪客流量。

## 從哪裡開始讀

1. `engineering/specs/HazelminX_Spec_v1.1_zh-TW.md` — 完整規格；`engineering/specs/HazelminX_Spec_v1.1_zh-TW.html` 是離線閱讀版。
2. `engineering/README.md` — 工程文件的索引。
3. `engineering/architecture.md` — 架構、元件邊界、測試策略與前提假設；每個選擇的原因在 `engineering/decisions/`。
4. `engineering/tickets/` — 全部票務。還沒開始的在最上層，進行中的在 `doing/`，完成的在 `done/`。
5. `docs/research/` — 參考專案的原始碼研究。

## 參考專案與授權

- [1Panel](https://github.com/1Panel-dev/1Panel) v2.4.1 OSS（GPL-3.0）：功能基線與 Go 業務邏輯來源。
- [Komodo](https://github.com/moghtech/komodo) Periphery（GPL-3.0-or-later）：Rust Agent 改造起點。
- [Portainer CE](https://github.com/portainer/portainer)：Git Stack 部署與環境變數的行為參考。
- [infra-manager](https://github.com/HazelnutParadise/infra-manager)：必須相容的 Token API 代理契約。
- [Zentinel](https://github.com/zentinelproxy/zentinel)（Apache-2.0）：網站入口。
- [Beszel](https://github.com/henrygd/beszel)：監控與 GPU 指標收集參考。

HazelminX 的授權方向為 GPL-3.0 相容；`only` 或 `or-later` 的最終宣告待核定。原作者的授權與著作權告知保留，見後續的法律資訊頁。

## 授權

見 `LICENSE`。
