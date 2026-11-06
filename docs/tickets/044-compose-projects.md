# 044 — 專案管理員可以建立專案與環境，匯入完整 Compose 並驗證後部署

**Epic:** Compose 部署（`epic:compose`）  
**User Story:** 專案管理員可以部署完整 Compose  
**Milestone:** M2 容器、Compose 與 GitHub  
**Blocked by:** [039 管理員可以看到 Docker、Podman、K3s 各自狀態並在頁面一鍵安裝 Docker](039-engine-detect-install.md), [008 使用者可以提交工作後關閉頁面，之後仍能查到進度、證據與結果](008-job-engine.md), [016 管理員可以預覽並批准高風險變更計畫，內容改變時原批准失效](016-change-plans.md)  
**Status:** blocked  
**規格章節:** 15｜完整 Compose、GitHub 與環境變數  
**Issue:** [#44](https://github.com/HazelnutParadise/hazelminx/issues/44)  

## What it delivers

專案／環境模型、上傳與編輯器匯入、多 -f 檔案／profiles／build context／外部資源、預檢（必要變數、映像、相對路徑、磁碟、Port 衝突）、原生 Compose 執行（不經 zbpack）、未知欄位無損保存、相對路徑掛載與持久資料分離。

## Acceptance criteria

- [ ] **CMP-001** 專案管理員可以從 Git、上傳檔案、編輯器與商店匯入 Compose。 — 完成條件：支援指定工作目錄、多個 `-f` 檔案、profiles、build context 與外部資源宣告。
- [ ] **CMP-007** 管理員可以先驗證完整部署設定。 — 完成條件：缺必要變數、無法取得映像、相對路徑錯誤、磁碟缺席、Port 衝突都在修改前顯示。
- [ ] **CMP-008** 管理員可以建置所有需要 build 的服務並部署整個專案。 — 完成條件：不經 zbpack／zbplan；使用受支援的原生 Dockerfile／BuildKit／Compose 能力。
- [ ] **CMP-012** 管理員可以保留相對路徑掛載。 — 完成條件：建置來源與持久資料分開；切換 checkout 不讓運行中服務失去設定檔或資料。
- [ ] **CMP-015** 管理員可以處理不認識的合法 Compose 欄位。 — 完成條件：原文無損保存；執行器不支援時說明，不靜默刪除 security、healthcheck 或 network 設定。
- [ ] **UI-008** 使用者可以切換圖形化設定與原始 Compose／設定編輯。 — 完成條件：不認識的合法欄位不被刪除；無法無損編輯時改以原始編輯器處理並說明。
- [ ] 多 Compose 檔案、profiles、build 與相對路徑的解析結果與原生 Compose 一致

## Acceptance tests（第 42 章）

- **T-052** 部署多 Compose 檔案、profiles、build 與相對路徑。 → 與指定版本原生 Compose 的解析結果一致。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
