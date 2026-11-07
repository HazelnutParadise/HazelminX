# 108 — 專案管理員可以在一個專案環境放多個 Compose 與 Kubernetes 部署單元，單元之間用公開變數與內部位址連線

**Epic:** Compose 部署（`epic:compose`）  
**User Story:** 一個應用可以跨主機、跨引擎與叢集部署，仍在同一個專案裡管理  
**Milestone:** M2 容器、Compose 與 GitHub  
**Blocked by:** [044](044-compose-projects.md), [049](049-git-deploy.md), [086](086-kubernetes-connect.md), [088](088-kubernetes-helm-storage-yaml.md)  
**Status:** blocked  
**規格章節:** 增補 04｜52｜專案的部署單元、單元之間的連線與 env 檔  
**Issue:** [#115](https://github.com/HazelnutParadise/hazelminx/issues/115)  

## What it delivers

專案 → 專案環境 → 部署單元的結構。單元是 Compose（部署到某個 Docker／Podman 引擎環境）或 Kubernetes（Helm chart 或 manifest，部署到某個叢集的 namespace）。單元之間參考 Zeabur：每個單元有不會變動的內部位址與「連線」區塊，可以公開變數讓其他單元用 `${單元.變數}` 引用；跨主機時走私有網路，或只開放被引用 Port 的加密轉送。專案環境可以「部署全部」，依引用順序執行，並可整個或單一單元回滾。改單元名稱不改 Compose 專案名稱、儲存卷名稱與內部位址。

## Acceptance criteria

- [ ] **PRJ-001**（增補 04）專案管理員可以在一個專案環境裡加入多個部署單元，類型是 Compose 或 Kubernetes。 — 完成條件：同一個專案環境可以同時有部署到不同主機的 Compose 單元，以及部署到叢集的 Kubernetes 單元。只有一個單元時，不顯示部署單元區塊與單元切換。名稱符合 52.1 的規則；Compose 專案名稱固定為 `<專案>-<專案環境>-<單元>`，並以 `-p` 指定。部署到 Podman 時，用 Docker Compose CLI 連 Podman socket。
- [ ] **PRJ-002**（增補 04）專案管理員可以部署 Kubernetes 單元，來源是 Git、Helm repo 或 OCI。 — 完成條件：來源是 Helm chart 或 manifest 目錄。每次部署固定 commit 或 chart 版本；顯示 values、合併結果、差異與資源；不把 Compose 轉成 Kubernetes。
- [ ] **PRJ-003**（增補 04）每個服務都有不會變動的內部位址，每個單元有入口服務與「連線」區塊。 — 完成條件：改單元或服務名稱後，位址不變；連線區塊列出各服務的內部位址、對內 Port、對外網址與連入方式。
- [ ] **PRJ-004**（增補 04）單元可以公開變數，其他單元在平台變數值裡用 `${單元.變數}` 引用。 — 完成條件：依 52.2 解析，特殊變數以 `HZX_` 開頭。下列情況在預檢擋下：引用不到、引用到未公開的變數、變數循環引用。單元互相引用特殊變數是允許的。不同專案環境之間不能引用。被引用的機密與含機密的組合值，在引用端遮罩。被引用的值變了，引用端標「需要重新部署」。改名後舊名稱保留為引用別名。
- [ ] **PRJ-005**（增補 04）不同位置的單元之間可以照 52.2 的表連線。 — 完成條件：有私有網路時走私有網路；沒有時用只開放被引用 Port 的加密轉送；能連到 Kubernetes 單元的 Service；平台定期實測連通性，拓撲圖畫出引用關係，連不到的引用標紅。
- [ ] **PRJ-006**（增補 04）專案管理員可以部署整個專案環境，也可以回滾整個專案環境或單一單元。 — 完成條件：依 52.3 決定部署順序與失敗政策；每個單元各自回報結果；回滾使用當時解析出的值，並在回滾前列出引用不相容的地方；Webhook 只觸發用到該 repo 的單元。
- [ ] **PRJ-012**（增補 04）單元改名不會改變它的 Compose 專案名稱、儲存卷名稱與內部位址。 — 完成條件：改名後再部署，掛的還是原本的儲存卷；畫面顯示實際的 Compose 專案名稱與儲存卷名稱。

## Acceptance tests

- **T-189** 在共用前提下按「部署全部」。 → 三個單元都顯示在同一個專案環境。search 先部署，web 與 worker 在 search 完成後同時部署。
- **T-190** search 公開 `SEARCH_URL=http://${search.HZX_HOST}:${search.HZX_PORT_HTTP}`，web 設定 `SEARCH_URL=${search.SEARCH_URL}`。 → 預檢把值解析成 `http://search.production.shop.internal:9200`；web 的 shop-api 容器裡實際連得到 search 的 Service。
- **T-191** worker 引用 `${search.ADMIN_TOKEN}`，但 search 沒有把它設為公開。 → 預檢擋下並寫出「search 沒有公開 ADMIN_TOKEN」，不會用空字串部署。
- **T-192** staging 的單元引用 production 的單元。 → 不能引用，畫面上也選不到。
- **T-193** 把 search 單元改名為 search-v2。 → 內部位址不變；web 原本的 `${search.SEARCH_URL}` 照常可以解析；畫面提供「把引用改成新名稱」。
- **T-194** 另一組設定：tw-01 與 jp-02 之間沒有私有網路，worker 引用 web 的 shop-api 的 `HZX_PORT_HTTP`（8080）。 → 平台建立加密轉送；從 jp-02 經由轉送通道只能連到 tw-01 的 8080；轉送端沒有另外開放其他 Port。
- **T-195** 另一組設定：專案環境再加一個不引用 search 的單元 mailer。search 部署失敗。 → web 與 worker 不部署；mailer 照常部署；結果逐一列出每個單元。改成「任一單元失敗就停止全部」後，mailer 也不部署。
- **T-196** 把 search 回滾到一個沒有公開 `SEARCH_URL` 的舊版本，web 仍在引用它。 → 回滾前列出「web 引用的 search.SEARCH_URL 在這個版本不存在」；使用者確認前不回滾。
- **T-205** 把 web 單元改名為 storefront，再部署。 → 掛的仍是原本的 `shop-production-web_uploads` 儲存卷；畫面顯示 Compose 專案名稱仍是 `shop-production-web`。
- **T-218** 推送到 search chart 所在的 repo（web 與 worker 用的是另一個 repo）。 → 只有 search 被觸發部署，web 與 worker 不動。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
