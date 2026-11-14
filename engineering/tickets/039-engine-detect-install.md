# 039 — 管理員可以看到 Docker、Podman、K3s 各自狀態並在頁面一鍵安裝 Docker

**Epic:** 容器引擎（`epic:containers`）  
**User Story:** 管理員可以管理容器引擎  
**Milestone:** M2 容器、Compose 與 GitHub  
**Blocked by:** 019 管理員可以用一次性註冊碼加入節點，並看到離線、過期、維護與撤銷狀態, 016 管理員可以預覽並批准高風險變更計畫，內容改變時原批准失效, 012 維運者可以執行需要高權限的固定動作，而 Agent 主程序維持低權限  
**規格章節:** 09｜節點加入、自動偵測與多節點、39｜跨功能流程：從按鈕到真正完成  
**Issue:** [#39](https://github.com/HazelnutParadise/hazelminx/issues/39)  

## What it delivers

這張票是索引，自己不交付內容，三張子票都在 `done/` 時一起關閉。拆分原因：偵測（執行器、Agent、Core、API）、以 root 安裝軟體（change plan、工作、執行器新動作與帳本）、畫面分屬不同元件與審查重點，一個 PR 做不完也驗不完。

| 項目 | 子票 |
|---|---|
| 引擎偵測：程式、版本、單元、外掛、每個端點一個環境（擁有者、rootful／rootless、以端點回答判斷種類）、狀態分開、`/management/v1` API（NOD-003、NOD-004、ENV-001、ENV-002 後端，T-051、T-149、T-150、T-156） | 039.1 |
| 安裝 Docker 的預檢、變更計畫、批准與工作，執行器安裝動作與帳本；安裝後驗證；停止、啟動、重新安裝、移除；K3s containerd 不移除、失敗不動防火牆與其他引擎（INS-008、ENV-006，T-152） | 039.2 |
| 容器頁的引擎與環境狀態、未安裝畫面與安裝流程（畫板 Containers、ContainersEmpty） | 039.3 |
