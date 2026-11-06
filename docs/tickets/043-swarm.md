# 043 — 管理員可以管理 Swarm 節點、服務、Stack、Secret 與更新策略

**Epic:** 容器引擎（`epic:containers`）  
**User Story:** 管理員可以管理容器引擎  
**Milestone:** M2 容器、Compose 與 GitHub  
**Blocked by:** [041 管理員可以管理映像、登錄站、容器網路與 Volume 並採用安全更新](041-images-registries-networks-volumes.md)  
**Status:** blocked  
**規格章節:** 14｜Docker、Podman、映像與登錄站  
**Issue:** [#43](https://github.com/HazelnutParadise/hazelminx/issues/43)  

## What it delivers

與一般 Compose 分開的 Swarm 頁；能力由引擎版本與相容矩陣控制。

## Acceptance criteria

- [ ] **CTR-013** 管理員可以管理 Swarm 的節點、服務、Stack、Secret 與更新策略。 — 完成條件：與一般 Compose 分開；支援能力由引擎版本與相容矩陣控制。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
