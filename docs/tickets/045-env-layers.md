# 045 — 管理員可以分層管理 .env、介面變數與覆寫層，並預覽每個服務實際收到的值

**Epic:** Compose 部署（`epic:compose`）  
**User Story:** 專案管理員可以部署完整 Compose  
**Milestone:** M2 容器、Compose 與 GitHub  
**Blocked by:** [044 專案管理員可以建立專案與環境，匯入完整 Compose 並驗證後部署](044-compose-projects.md), [011 管理員可以安全保存機密，且主金鑰可輪替並驗證舊備份仍可解密](011-secrets-and-master-key.md)  
**Status:** blocked  
**規格章節:** 15｜完整 Compose、GitHub 與環境變數  
**Issue:** [#45](https://github.com/HazelnutParadise/hazelminx/issues/45)  

## What it delivers

15.2 的載入順序（repo .env → 上傳檔依序 → 介面值）、Compose 替換變數與服務環境變數分開、來源與覆蓋顯示、乾淨程序環境與明確 env file、相容解析器、兩種遮罩預覽、平台覆寫層（Port／掛載／網路／資源）。

## Acceptance criteria

- [ ] **CMP-004** 管理員可以上傳多份 `.env` 並指定順序，或在介面設定變數。 — 完成條件：明確顯示每個值來自何處、被誰覆蓋；空字串、未設定與刪除不是同一件事。
- [ ] **CMP-005** 管理員可以分開設定 Compose 替換變數與服務內環境變數。 — 完成條件：上傳 `.env` 不會把所有秘密自動注入所有容器；容器只收到明確指定的值。[S18](#source-S18)
- [ ] **CMP-006** 管理員可以在不改 Git 的情況下修改 Port、掛載、網路與資源限制。 — 完成條件：變更存在平台覆寫層，更新 Git 後仍在；變數可在同 commit 下獨立部署。
- [ ] 不對 .env 執行 source 或 eval
- [ ] 空字串、未設定與刪除不是同一件事

## Acceptance tests（第 42 章）

- **T-053** Repo 沒有 `.env`，由 UI 上傳。 → 不改 Repo，變數替換成功，機密不寫回 Git。
- **T-054** 同名變數來自 Repo、兩個上傳檔及 UI。 → 依公布優先序解析，畫面顯示來源而不洩密。
- **T-055** 變數未被服務 environment／env_file 使用。 → 不自動進入所有容器，避免誤傳機密。
- **T-056** 變數有美元符號、空值、多行或缺少必填值。 → 與引擎規則一致；必要缺值在部署前失敗。
- **T-061** 環境變數改變，但 Git commit 沒變。 → 能產生新設定版本並部署，不被忽略。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
