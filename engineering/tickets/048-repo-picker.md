# 048 — 使用者可以瀏覽、搜尋、分頁載入可用 Repo，並選分支、commit 與 Compose 檔案

**Epic:** GitHub 來源（`epic:github`）  
**User Story:** 使用者可以從 GitHub 部署  
**Milestone:** M2 容器、Compose 與 GitHub  
**Blocked by:** 047 使用者可以用 GitHub 網頁授權或 PAT 建立可重用連線，並保存多帳號多授權, 017 使用者可以在主導覽、全域搜尋與工作中心之間完成日常操作  
**規格章節:** 07｜介面與操作體驗、15｜完整 Compose、GitHub 與環境變數  
**Issue:** [#48](https://github.com/HazelnutParadise/hazelminx/issues/48)  

## What it delivers

這張票是索引，自己不交付內容，兩張子票都在 `done/` 時一起關閉。拆分原因和 047 一樣：Core 的 Repo 清單同步、分支與檔案瀏覽，和前端的來源選擇器畫面分屬 `core/` 與 `web/`，一個 PR 做不完也驗不完；畫板也還缺同步狀態、授權選擇與空 Repo 等狀態。

| 項目 | 子票 |
|---|---|
| Core：每份連線、每個資源擁有者各自的同步游標、完整／部分／限流／同步中狀態、限流退避、ETag、合併清單但綁定單一連線、分支／標籤／commit／目錄瀏覽、固定 SHA、Compose 檔案偵測（空 Repo、沒有、一份、多份），經 `/management/v1/github` 提供（GIT-005～008、GIT-034，GH-T05～T08 的 API 部分） | 048.1 |
| 畫面：共用來源選擇器（7.4）：連線與擁有者切換、搜尋與分頁載入、同步狀態與部分結果提醒、重複 Repo 選授權、分支／標籤／commit 選擇、檔案瀏覽、Compose 選擇與白話錯誤說明 | 048.2 |
