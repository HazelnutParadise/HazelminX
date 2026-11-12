# 013 — 管理員可以一行安裝單機模式、重開機後登入，且安裝不需要任何編譯器

**Epic:** 安裝、更新與卸載（`epic:install`）  
**User Story:** 管理員可以安裝與啟動平台  
**Milestone:** M1 單機主機管理  
**Blocked by:** 003 管理員可以啟動 hazelminx-core 並通過健康檢查, 004 管理員可以在主機上以 systemd 執行 hazelminx-agent 並回報身分, 009 擁有者可以登入、建立帳號與角色，未授權的 API 呼叫一律被拒絕, 012 維運者可以執行需要高權限的固定動作，而 Agent 主程序維持低權限  
**規格章節:** 08｜安裝、更新、開機自啟與解除安裝、37｜安裝位置、版本更新與平台自我修復、39｜跨功能流程：從按鈕到真正完成  
**Issue:** [#13](https://github.com/HazelnutParadise/hazelminx/issues/13)  

## What it delivers

hazelminx CLI（Rust）的 install：預檢、簽署清單選版本、先下載驗證再變更、建立服務帳號／目錄／systemd、首次登入一次性啟用碼、安裝帳冊、安裝工作可在 UI 接續查看。

## Acceptance criteria

- [ ] **INS-001** 管理員可以一行安裝並選擇模式、資料目錄與管理入口。 — 完成條件：全新支援環境可完成；重跑不重設密碼、不覆寫既有資料、不重複建立服務。
- [ ] **INS-002** 管理員可以先看安裝預檢。 — 完成條件：檢查 OS、架構、libc、systemd、權限、磁碟空間與 inode、時間、網路、相依套件、占用連接埠及現有管理工具。
- [ ] **INS-003** 安裝程式可以先準備全部必要檔案，再變更系統。 — 完成條件：下載中斷、簽章錯誤或架構不符時，原服務保持不變；不先刪除舊執行檔。
- [ ] **INS-004** 管理員可以明確選擇使用官方套件來源或離線包。 — 完成條件：不擅自替換套件來源、關閉 TLS 驗證或執行未知鏡像的腳本。
- [ ] **INS-005** 管理員可以在安裝後安全完成首次登入。 — 完成條件：沒有固定預設密碼；一次性啟用資料有期限；完整密碼不進共享日誌。
- [ ] **INS-006** 管理員可以讓 Core、Agent、Zentinel 預設開機自啟。 — 完成條件：重新開機後服務有效啟動，介面能登入且功能健康檢查通過。
- [ ] **INS-013** 管理員可以檢視安裝的每一步與診斷。 — 完成條件：瀏覽器斷線後仍能找回同一工作；能區分失敗、已復原、部分復原與需人工處理。
- [ ] **PKG-001** 管理員可以只安裝發布成品啟動平台。 — 完成條件：乾淨支援 OS 沒有 Go、Rust、Node.js、npm、Vite+、C／C++ 編譯器仍可登入並使用基本管理。
- [ ] **PKG-002** 管理員可以選 Core、Agent 或完整模式。 — 完成條件：未選的服務與工具不被安裝；各模式產物與相依清單可核對。
- [ ] **REL-002** 安裝器可以依簽署清單選版本。 — 完成條件：選錯 CPU、作業系統、libc 或相容協定時在變更前拒絕。
- [ ] 重跑安裝不重設密碼、不覆寫既有資料、不重複建立服務
- [ ] 三種模式（完整、控制中心、只有 Agent）各有產物與相依清單
- [ ] （增補 03 機制四、51.6）安裝時預先配置固定大小的稽核附加檔（預設 64 MiB），並建議放在與平台資料庫不同的檔案系統。

## Acceptance tests（第 42 章）

- **T-001** 乾淨受支援主機安裝單機模式，再重開機。 → Core、Agent 正常，能登入；不需手動 enable。
- **T-003** 在未支援架構執行安裝器。 → 預檢明確拒絕，沒有安裝到一半。
- **T-004** 下載到一半切斷網路。 → 舊版本仍可用；重試驗證下載，不執行半份檔案。
- **T-005** 提供校驗或簽章不符的套件。 → 拒絕安裝，顯示來源驗證失敗。
- **T-006** 安裝前讓資料磁碟空間不足。 → 在變更服務前停止，列出需要與可用空間。
- **DEP-T01** 在沒有語言工具鏈的乾淨 Linux 安裝完整平台。 → 不下載編譯器；UI、Agent 與資料儲存正常；重新開機仍可登入。
- **DEP-T02** 只安裝 Agent。 → 不安裝 Core、前端服務、Docker、Zentinel 或資料庫服務。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。

## Notes

### 票 113 交過來的項目

- 安裝器第一步執行 `hazelminx-privileged snapshot capture`，安裝步驟用 `ledger::recorder::Recorder` 記錄，並讓 `/var/lib/hazelminx` 維持 root 擁有、群組與其他人不可寫。
- 把 Core 資料目錄的 `privileged-issuer.pub` 放到主機的 `/etc/hazelminx/privileged/issuer.pub`。沒有這個檔，執行器不會啟動，Core 也拿不到帳本。
- dnf／rpm 主機的帳本只用模擬輸出測過。x86_64 與 ARM64 的 Ubuntu 24.04 已由 CI 以 root 在 systemd 下實測（PR #127）。

### 票 019 交過來的項目

- 遠端主機加入：安裝腳本以 Agent 的執行帳號跑 `hazelminx-agent enroll --core-url <Agent 入口網址> --code <註冊碼>`（帳號錯了，`identity/` 的擁有者會不對）。註冊成功後，再以 root 把 `state_dir/identity/privileged-issuer.pub` 複製到 `/etc/hazelminx/privileged/issuer.pub`，然後啟動 Agent 服務。Agent 會用 `state_dir/enrollment.json` 記下的網址連回，`agent.toml` 不必寫 `core_url`。
- `enroll` 會等管理員核准（預設 30 分鐘）。腳本中斷後用同一個碼重跑會接著等，不會變成第二台主機。
- 本機第一個節點（Core 與 Agent 在同一台）目前也要走同樣的註冊碼流程。若要省掉核准步驟，需要另外決定，不在票 019 的範圍。
- （PR #143 審查 2026-11-12）高權限執行器信任的簽發公鑰，是由低權限的 Agent 帳號先寫到 `state_dir/identity/privileged-issuer.pub`，再由安裝腳本以 root 複製到 `/etc/hazelminx/privileged/issuer.pub`。Agent 帳號被攻陷時，可以在複製前換掉這個檔，讓執行器信任攻擊者的簽發金鑰。動工時要決定安裝腳本怎麼確認這個檔沒有被換過，例如：
  - 以 root 直接向 Core 取得公鑰，用註冊時取得的 Core CA 驗證 Core，不經過 Agent 帳號寫的檔。
  - 或者核對 Core 管理介面顯示的公鑰指紋，與管理員在安裝時輸入的指紋一致。
