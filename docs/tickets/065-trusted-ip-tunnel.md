# 065 — 管理員可以為每個入口設定可信代理模式，Cloudflare Tunnel 流量取得正確訪客 IP

**Epic:** WAF 與可信 IP（`epic:security`）  
**User Story:** 管理員可以保護網站與 API  
**Milestone:** M3 網站入口與安全防護  
**Blocked by:** [055 管理員可以從介面安裝原生 Zentinel，Core 重啟不影響既有網站，救援入口不依賴它](055-zentinel-install.md), [064 安全管理員可以安裝 WAF，逐站設定模式、規則、例外與失效政策](064-waf.md)  
**Status:** blocked  
**規格章節:** 22｜真實訪客 IP、Cloudflare Tunnel 與代理鏈、39｜跨功能流程：從按鈕到真正完成  
**Issue:** [#64](https://github.com/HazelnutParadise/hazelminx/issues/64)  

## What it delivers

22.1 判斷規則、單一可信訪客身分結果供 WAF／限速／封鎖／稽核／分析共用、判定依據顯示、偽造與歧義處理、Pseudo IPv4、Workers、可信範圍更新、後送 Header 清理、Tunnel 入口隔離。

## Acceptance criteria

- [ ] **IP-001** 管理員可以為每個 listener 設定直接、Cloudflare、Tunnel 或自訂代理模式。 — 完成條件：相同網域的不同入口可以有不同信任規則，不能全域無條件相信 Header。
- [ ] **IP-002** 平台可以產生單一可信訪客身分結果。 — 完成條件：WAF、IP 限速、封鎖、稽核與分析使用同一結果，不各自重新解析。
- [ ] **IP-003** 管理員可以查看來源判定依據。 — 完成條件：顯示 peer IP、client IP、取得方法、可信程度及入口，不暴露機密 Header。
- [ ] **IP-004** 平台可以防止偽造或有歧義的來源資訊。 — 完成條件：多個不同 CF-Connecting-IP、無效 IPv6、超長 XFF 或混合格式都有明確拒絕／降級測試。
- [ ] **IP-005** 平台可以處理 Pseudo IPv4 與原 IPv6。 — 完成條件：不把保留給轉換用途的偽 IPv4 當真實地理來源；保存可驗證的原 IPv6。
- [ ] **IP-006** 平台可以處理 Cloudflare Workers 與其他中間服務。 — 完成條件：無法保證 Header 仍代表原訪客時標示不確定，不以固定 Worker 位址偽造真人分析。[S12](#source-S12)
- [ ] **IP-007** 管理員可以更新可信代理範圍。 — 完成條件：來源與版本可查；更新失敗保留上一份有效清單，不改成信任所有位址。
- [ ] **IP-008** 平台可以清理後送的身分 Header。 — 完成條件：移除外部偽造值後重建可信資訊，避免上游再相信原本不可信 Header。
- [ ] **IP-009** 管理員可以確認 Tunnel 入口不被其他本機帳號或容器冒用。 — 完成條件：透過專用網路、權限或經驗證的本機連接方式隔離；共享 loopback 單獨不足以通過。
- [ ] 真實 Tunnel 測試取得正確 IP；本機其他容器模仿 Header 無法取得可信身分

## Acceptance tests（第 42 章）

- **T-091** 直接連線偽造 `CF-Connecting-IP`。 → 使用直接連線來源，不相信偽造值。
- **T-092** 真實 Cloudflare Tunnel HTTP 請求。 → 正確取得可信訪客 IP；peer 與 client 分開保存。
- **T-093** 本機其他容器模仿 Tunnel Header。 → 無法取得可信 Tunnel 身分。
- **T-094** IPv6、Pseudo IPv4、Workers 與多層代理。 → 依已測規則解析；無法確認的情境標未知，不假造原始 IP。
- **T-095** 同一訪客跨節點發送超過全域限速的請求。 → 全域政策有真正共用計數或明示只支援各節點限制，不能誤稱全域。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
