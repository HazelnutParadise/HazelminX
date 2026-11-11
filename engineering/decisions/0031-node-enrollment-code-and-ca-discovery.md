# 0031 — 節點註冊用一次性碼換憑證，Core CA 由註冊碼驗證，管理員核對後才簽發

**Status:** accepted
**Date:** 2026-11-12

## Context
NOD-001、SEC-001 要求用短效、單次的註冊碼加入節點，加入時可以核對主機身分。Agent 第一次連 Core 時還沒有 Core CA，沒辦法驗 Agent 入口的 TLS；如果直接把註冊碼送過一條沒驗證的 TLS，中間人就能拿走註冊碼，用自己的金鑰註冊。設計畫板 `AddNode` 的流程是：產生碼 → 主機執行指令 → 管理員核對主機名稱、machine-id、憑證指紋 → 加入。規格 32.1 要求不要自行拼湊加密協定。

## Decision
- 註冊碼是 `HZX-` 加 16 個 Crockford base32 字元（四組，每組 4 個）：第一組是公開的查詢 ID，16 個字元全部是秘密，共 80 位元。Core 只存 `SHA-256("hazelminx-enroll-v1\n" + 碼)`，不存明文。
- Agent 先以不驗證的 TLS 取得 Core CA（`GET /agent/v1/enroll/discovery`），Core 附上以註冊碼導出金鑰算的 HMAC；Agent 驗過 HMAC 才信任這張 CA，之後用這張 CA 驗 TLS 再送出註冊碼。做法沿用 kubeadm 的 bootstrap token discovery（cluster-info 以 token 簽 JWS）。
- 送出註冊碼後是「等待核對」：管理員在 UI 看到主機名稱、machine-id、作業系統、來源位址與金鑰指紋，按下核准才簽發節點憑證；Agent 以同一個碼加同一把公鑰重送請求來取結果，所以重試不會產生第二筆。
- 同一把金鑰的節點再註冊，核准後沿用原節點；新金鑰但 machine-id 相同時，管理員必須選「取代該節點」或「建立新節點」。
- 憑證輪替經已驗證的通道送新憑證，Core 同時接受新舊兩個序號，Agent 用新憑證完成一次握手後才停止接受舊序號。

## Options rejected
- **把 CA 指紋放進安裝指令（kubeadm 的 `--discovery-token-ca-cert-hash`）**：更直接，但指令更長，畫板的指令只有網址與註冊碼；HMAC 做法讓註冊碼本身就足以驗 CA。之後若要兩者並存，可以在 Agent 加一個選填參數，不改這份契約。
- **註冊碼送出後立即簽發**：少一步，但做不到「核對主機身分後才加入」，拿到碼的人就能加任何主機。
- **把註冊碼本身當 HMAC 的金鑰、在請求裡只送 HMAC 不送碼**：等於自創協定，規格 32.1 不允許。

## Consequences
- 契約在 `engineering/contracts/README.md` 的「註冊」與「憑證輪替」，樣本在 `fixtures/enrollment.json`。
- Agent 在 `proxy` 模式下取 CA 時走代理的憑證鏈；代理本來就看得到通道內容，所以也看得到註冊碼。
- 資料庫存的雜湊值就是 HMAC 用的金鑰。資料庫外洩時，拿到它的人在碼的有效期內可以偽造 CA 發現的回應，所以碼的有效期短：預設 10 分鐘，上限 60 分鐘。
- 影響的票：019、013（安裝腳本呼叫 `hazelminx-agent enroll`）。
