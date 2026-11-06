# 012 — 維運者可以執行需要高權限的固定動作，而 Agent 主程序維持低權限

**Epic:** 節點與 Agent（`epic:nodes`）  
**User Story:** 管理員可以把主機納入管理  
**Milestone:** M0 基礎與設計  
**Blocked by:** [004 管理員可以在主機上以 systemd 執行 hazelminx-agent 並回報身分](004-agent-skeleton.md)  
**Status:** blocked  
**規格章節:** 32｜節點通訊、信任與高權限操作  
**Issue:** [#12](https://github.com/HazelnutParadise/hazelminx/issues/12)  

## What it delivers

hazelminx-privileged：以本機 socket 接受固定動作與結構化參數（不組 shell 字串）、呼叫者身分驗證、動作白名單、稽核；終端機與腳本列為高風險權限。

## Acceptance criteria

- [ ] **SEC-005** 管理員可以限制終端機與腳本權限。 — 完成條件：只有容器唯讀權限的人不能經終端機取得宿主機 root。
- [ ] **SEC-006** 系統可以安全處理命令參數。 — 完成條件：路徑、使用者名稱與環境變數含空白、引號、換行及 shell 符號時不變成額外命令。
- [ ] **SEC-009** 系統可以限制管理通道的負載。 — 完成條件：大型下載、檔案傳輸、終端機輸出不阻塞授權撤銷與健康回報。
- [ ] **SEC-010** 管理員可以查看安全事件但不看到不該看的秘密。 — 完成條件：授權失敗、憑證異常、參數拒絕有紀錄；不寫完整私鑰或登入碼。
- [ ] 檔名含空白、引號、換行與 shell 符號時不會變成額外命令
- [ ] 大型下載與終端機輸出不阻塞授權撤銷與健康回報

## Acceptance tests（第 42 章）

- **T-039** 檔名包含空白、引號、換行及 shell 符號。 → 正確處理原檔名，沒有執行額外命令。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
