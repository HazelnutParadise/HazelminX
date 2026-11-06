# 049 — 專案管理員可以從 GitHub 以固定 commit 隔離抓取、建置並部署，手動 URL／PAT／SSH 模式照常可用

**Epic:** GitHub 來源（`epic:github`）  
**User Story:** 使用者可以從 GitHub 部署  
**Milestone:** M2 容器、Compose 與 GitHub  
**Blocked by:** [048 使用者可以瀏覽、搜尋、分頁載入可用 Repo，並選分支、commit 與 Compose 檔案](048-repo-picker.md), [044 專案管理員可以建立專案與環境，匯入完整 Compose 並驗證後部署](044-compose-projects.md), [012 維運者可以執行需要高權限的固定動作，而 Agent 主程序維持低權限](012-privileged-executor.md)  
**Status:** blocked  
**規格章節:** 15｜完整 Compose、GitHub 與環境變數、31｜整體架構：每一部分負責什麼  
**Issue:** [#49](https://github.com/HazelnutParadise/hazelminx/issues/49)  

## What it delivers

部署綁定（連線參照、Repo ID、ref、固定 SHA、Compose 清單）、受控抓取服務與不可信建置隔離、submodule／LFS 逐項驗權、憑證不進 URL／日誌／映像、SSH Deploy Key 指紋驗證、GitHub 秘密不當應用變數。

## Acceptance criteria

- [ ] **CMP-002** 管理員可以指定 GitHub repo、分支／tag／commit 與 Compose 路徑。 — 完成條件：每次部署固定 commit SHA，不在建置到一半改讀移動中的分支。
- [ ] **CMP-003** 管理員可以使用 HTTPS 憑證、GitHub App 或 SSH Deploy Key 存取私有 repo。 — 完成條件：採最小權限；驗證伺服器指紋／CA；憑證不進 URL、日誌或建置 context。
- [ ] **CMP-013** 管理員可以管理 submodule 與 Git LFS 專案。 — 完成條件：支援時拉到固定版本並受同樣授權；不支援的組合在預檢明確阻擋，不少拉檔案仍部署。
- [ ] **GIT-009** 使用者可以繼續手動填 Repo URL 並指定個別 PAT 或 SSH 金鑰。 — 完成條件：不必先建立 GitHub App；原有部署不被強制改成帳號瀏覽模式。
- [ ] **GIT-010** 使用者可以為挑選的 Repo 套用平台 `.env` 與客製設定。 — 完成條件：多環境設定分開；GitHub 秘密不當作應用變數；新 commit 不覆寫既有掛載、網路與 Port。
- [ ] **GIT-011** 專案管理員可以讓多個部署綁定共用連線。 — 完成條件：保存連線參照而非複製明文 Token；每個專案的 Repo 與環境權限分別驗證。
- [ ] **GIT-025** 系統可以隔離抓取憑證與不可信建置。 — 完成條件：App 私鑰不出 Core；抓取憑證不進 Git URL、全域設定、映像、日誌或一般建置環境，暫存可追蹤清除。
- [ ] **GIT-027** 管理員可以管理私人 submodule 與 Git LFS 的抓取。 — 完成條件：固定來源與版本，逐項驗權；跨站轉址不傳原憑證，不能存取雲端中繼資料或管理入口。
- [ ] **GIT-038** 使用者可以確認讀取、選擇與部署是同一份來源。 — 完成條件：授權、Repo ID、來源站台、ref、commit 與設定摘要綁定；預覽後權限或來源改變須重新檢查。
- [ ] 惡意 Dockerfile 無法取得 App 私鑰或永久 PAT
- [ ] 讀取、選擇與部署綁定同一份來源，權限或來源改變須重新檢查

## Acceptance tests（第 42 章）

- **T-063** Repo 需要未支援 submodule／LFS。 → 預檢明確拒絕或完整處理，不部署缺檔內容。
- **T-066** Build 過程嘗試讀宿主機秘密。 → 受到隔離與最小授權；建置產物與日誌不含測試秘密。
- **GH-T09** 不設定 GitHub App，以手動 URL、專案 PAT、SSH Deploy Key 各部署。 → 原有模式能獨立使用，SSH 指紋與個別範圍照常驗證。
- **GH-T10** 由 Repo 選擇器部署後修改平台 `.env`、Network、Port 與 Volume，再收到 push。 → 設定不被覆寫，正式／測試資料分開，GitHub 秘密未注入應用。
- **GH-T24** 惡意 Dockerfile 與容器嘗試讀來源憑證、Git remote、環境與暫存。 → 不能取得 App 私鑰／永久 PAT；抓取與建置隔離，秘密無落入映像或日誌。
- **GH-T26** 私人 submodule／LFS 指向另一 Repo，再加入跨站轉址及內網惡意位址。 → 逐項核對來源與憑證，缺權限預檢失敗；不跨站傳 Token 或存取敏感入口。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
