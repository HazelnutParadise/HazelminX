# 參考專案研究

這裡放參考專案的原始碼研究報告。報告由派工 agent 唯讀閱讀原始碼產出，主 agent 核對過關鍵結論；它們是線索，實作前以固定 commit 重新核對。

| 報告 | 來源與版本 | 用途 |
|---|---|---|
| `1panel-backend.md` | 1Panel v2.4.1 `core/`、`agent/` | 功能基線、可重用的 Go 業務邏輯、宿主機操作替換面 |
| `1panel-frontend.md` | 1Panel v2.4.1 `frontend/` | 路由與選單盤點、每頁子功能、商業版受控項目 |
| `komodo-periphery.md` | Komodo main（2026-09） `bin/periphery` | Rust Agent 改造起點：模組、通訊、授權 |
| `portainer-git-stack.md` | Portainer develop（2026-11） `api/` | Git Stack 部署、Source／Artifact、自動更新 |
| `infra-manager-contract.md` | infra-manager commit 2ace566 | 必須相容的 Token API 代理契約 |
| `zentinel.md` | Zentinel 0.6.54 | 網站入口的設定、重載、Agent、TLS、可信 IP、限制 |
| `beszel.md` | Beszel 2026-11 | 監控指標、GPU 採集、告警 |
