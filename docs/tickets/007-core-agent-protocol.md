# 007 — Core 與 Agent 可以用版本化、雙向驗證的通道交換能力與工作事件

**Epic:** 節點與 Agent（`epic:nodes`）  
**User Story:** 管理員可以把主機納入管理  
**Milestone:** M0 基礎與設計  
**Blocked by:** [003 管理員可以啟動 hazelminx-core 並通過健康檢查](003-core-skeleton.md), [004 管理員可以在主機上以 systemd 執行 hazelminx-agent 並回報身分](004-agent-skeleton.md)  
**Status:** blocked  
**規格章節:** 32｜節點通訊、信任與高權限操作  
**Issue:** [#7](https://github.com/HazelnutParadise/hazelminx/issues/7)  

## What it delivers

agent/crates/protocol 與 core 內對應套件定義：訊息格式與版本、能力宣告、工作指派／進度／證據事件序號、心跳、mTLS 雙向身分、重連對帳；Go 與 Rust 以契約測試互驗。

## Acceptance criteria

- [ ] **NOD-002** Agent 可以主動連回 Core，並在斷線後重連。 — 完成條件：不需在每台節點開公網管理連接埠；重連不重跑已完成工作。
- [ ] **NOD-011** Core 可以與版本不同但相容的 Agent 協作。 — 完成條件：握手交換協定及能力版本；不支援的操作先拒絕，不以未知欄位猜測執行。
- [ ] **SEC-011** 維護者可以更新 Core 與 Agent 的不同版本。 — 完成條件：交換協定與能力版本；不相容時拒絕變更，只保留已驗證的讀取與升級路徑。
- [ ] 協定不相容時拒絕操作並顯示支援範圍，不猜測執行未知欄位
- [ ] 經會終止 TLS 的代理時仍有獨立節點身分證明

## Acceptance tests（第 42 章）

- **T-018** Core IP 改變但憑證身分合法。 → 經設定變更後正常重連，不把 IP 當唯一身分。
- **T-021** Core 與 Agent 協定不相容。 → 明確顯示不支援，沒有誤解參數執行。
- **T-024** 經會終止 TLS 的代理連 Agent。 → 不接受偽造節點 Header；仍有獨立身分證明。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
