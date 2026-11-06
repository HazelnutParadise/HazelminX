# 004 — 管理員可以在主機上以 systemd 執行 hazelminx-agent 並回報身分

**Epic:** 節點與 Agent（`epic:nodes`）  
**User Story:** 管理員可以把主機納入管理  
**Milestone:** M0 基礎與設計  
**Blocked by:** [001 維護者可以在單一 repo 取得規格、架構決策、票務與工作規則](001-repo-scaffold.md)  
**Status:** ready  
**規格章節:** 31｜整體架構：每一部分負責什麼、32｜節點通訊、信任與高權限操作、38｜效能、資源與可觀測品質要求  
**Issue:** [#4](https://github.com/HazelnutParadise/hazelminx/issues/4)  

## What it delivers

Rust workspace agent/ 含 hazelminx-agent 執行檔：讀設定、產生節點身分金鑰與憑證簽署請求、持久工作紀錄目錄、systemd 單元檔、低權限服務帳號；與 Core 的實際握手在票 007 接上。

## Acceptance criteria

- [ ] 以 Komodo Periphery 選定 commit 為改造起點，列出保留／替換／新增的 crate 與授權
- [ ] 空閒 CPU 低於單核 2%、常駐記憶體低於 200 MiB（38 章目標，記錄量測）

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
