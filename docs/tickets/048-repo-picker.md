# 048 — 使用者可以瀏覽、搜尋、分頁載入可用 Repo，並選分支、commit 與 Compose 檔案

**Epic:** GitHub 來源（`epic:github`）  
**User Story:** 使用者可以從 GitHub 部署  
**Milestone:** M2 容器、Compose 與 GitHub  
**Blocked by:** [047 使用者可以用 GitHub 網頁授權或 PAT 建立可重用連線，並保存多帳號多授權](047-github-connections.md), [017 使用者可以在主導覽、全域搜尋與工作中心之間完成日常操作](017-ui-shell.md)  
**Status:** blocked  
**規格章節:** 07｜介面與操作體驗、15｜完整 Compose、GitHub 與環境變數  
**Issue:** [#48](https://github.com/HazelnutParadise/hazelminx/issues/48)  

## What it delivers

共用來源選擇器元件（7.4）：每份授權與資源擁有者各自的同步游標、完整／部分狀態、限流退避、ETag、合併顯示但綁定單一連線、分支／標籤／commit／檔案瀏覽、空 Repo／缺 Compose／多 Compose 處理。

## Acceptance criteria

- [ ] **GIT-005** 使用者可以瀏覽自己獲授權使用的 Repo。 — 完成條件：列出授權允許的個人、協作與組織 Repo；公開／私人與封存狀態可辨，無權範圍不洩漏。
- [ ] **GIT-006** 使用者可以搜尋與分頁載入完整可用清單。 — 完成條件：超過一頁仍能找到後續 Repo；部分失敗、限流、同步中與完整狀態分開顯示。
- [ ] **GIT-007** 使用者可以在重複 Repo 項目中選擇使用哪份授權。 — 完成條件：清單合併不合併權限；部署記錄一份明確連線；失敗不偷偷更換身分。
- [ ] **GIT-008** 使用者可以瀏覽 Repo 的分支、標籤、commit 與 Compose 檔案。 — 完成條件：空 Repo、缺檔案、多 Compose、分支移動及預設分支變更均有明確處理；部署固定 SHA。
- [ ] **GIT-034** 系統可以遵守 GitHub API 的版本與限流規則。 — 完成條件：版本由相容層管理；有效分頁、ETag 與重試依據可檢查；不靠切帳號或高頻重試逃避限流。
- [ ] 超過兩頁的 Repo 仍能搜尋到
- [ ] 某組織失敗不掩蓋其他結果也不標完整

## Acceptance tests（第 42 章）

- **GH-T05** 準備超過兩頁的 Repo，其中含協作、組織、公開與私人項目。 → 後續頁可搜尋找到；只出現授權及平台範圍共同允許的項目。
- **GH-T06** 同步第二頁時限流或某組織失敗。 → 顯示部分結果與待續，不顯示已完整；依等待資訊重試，不換帳號。
- **GH-T07** 同 Repo 被 App 與 PAT 同時涵蓋，指定 PAT 後使其失效。 → Repo 可合併顯示但綁定仍明確；不自動改用 App。
- **GH-T08** 選 Repo 後移動分支，另測空 Repo、缺 Compose 與多 Compose。 → 既有計畫固定 SHA；缺項預檢阻擋，多檔順序與工作目錄正確。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
