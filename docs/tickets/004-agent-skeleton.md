# 004 — 管理員可以在主機上以 systemd 執行 hazelminx-agent 並回報身分

**Epic:** 節點與 Agent（`epic:nodes`）  
**User Story:** 管理員可以把主機納入管理  
**Milestone:** M0 基礎與設計  
**Blocked by:** [001 維護者可以在單一 repo 取得規格、架構決策、票務與工作規則](001-repo-scaffold.md)  
**Status:** done  
**規格章節:** 31｜整體架構：每一部分負責什麼、32｜節點通訊、信任與高權限操作、38｜效能、資源與可觀測品質要求  
**Issue:** [#4](https://github.com/HazelnutParadise/hazelminx/issues/4)  

## What it delivers

Rust workspace agent/ 含 hazelminx-agent 執行檔：讀設定、產生節點身分金鑰與憑證簽署請求、持久工作紀錄目錄、systemd 單元檔、低權限服務帳號；與 Core 的實際握手在票 007 接上。

## Acceptance criteria

- [x] 以 Komodo Periphery 選定 commit 為改造起點，列出保留／替換／新增的 crate 與授權 — 證據：[`agent/UPSTREAM.md`](../../agent/UPSTREAM.md)。起點 `v2.3.3`／`780ac68b992094a9fccd5fffb760e0c84fd3c3d1`。9 個 workspace crate 逐項標處理方式、負責票與授權，`bin/periphery` 各模組逐項標處理方式與負責票，37 個直接外部相依（另列 `lib/command` 的 `libc`）逐項標處理方式與授權。HazelminX 新增 crate 與 `agent/Cargo.lock` 117 個套件的授權分布（沒有只能以 copyleft 授權使用的相依，也沒有 MPL-2.0）
- [x] 空閒 CPU 低於單核 2%、常駐記憶體低於 200 MiB（38 章目標，記錄量測） — 證據：[`agent/PERFORMANCE.md`](../../agent/PERFORMANCE.md)，腳本 `agent/scripts/measure-idle.sh`、`measure-in-container.sh`。macOS 上的 OrbStack Linux VM（aarch64），容器限 2 核心、以 nobody 執行，`hazelminx-agent run`（`de30d42`）三輪各約 300 秒：平均 CPU 都是 0 ticks（低於 0.004%）、記憶體峰值 3.09 MiB。骨架達標。骨架還沒有監控迴圈，規格「每 10 秒監控一次」條件下的判定要等票 020 的監控迴圈。照搬 Komodo 程序更新的 10 秒監控探針四輪中第 1 輪 2.45% 超標，其餘 0.09%～0.39%，只更新 CPU 與記憶體的版本三輪 0.09%～0.22%，但沒有在第 1 輪的情況下量過（第 1 輪偏高的原因沒有確認）

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
