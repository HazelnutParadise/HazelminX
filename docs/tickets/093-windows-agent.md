# 093 — 管理員可以把 Windows Server 加為受管節點（條件式範圍，待核定）

**Epic:** Windows 節點（`epic:windows`）  
**User Story:** 管理員可以管理 Windows 主機  
**Milestone:** M5 多節點、叢集、遷移與發布  
**Blocked by:** [019 管理員可以用一次性註冊碼加入節點，並看到離線、過期、維護與撤銷狀態](019-node-enrollment.md), [012 維運者可以執行需要高權限的固定動作，而 Agent 主程序維持低權限](012-privileged-executor.md)  
**Status:** blocked  
**規格章節:** 08｜安裝、更新、開機自啟與解除安裝、39｜跨功能流程：從按鈕到真正完成  
**Issue:** [#92](https://github.com/HazelnutParadise/hazelminx/issues/92)  

> 第 08.7、08.9 章為建議範圍；未經範圍核定與實測前不得顯示正式支援。

## What it delivers

原生 Rust Agent 與 Windows Service、能力矩陣回報、服務／事件／ACL／更新／終端／磁碟原生適配、錯誤 OS 部署阻擋、安全更新與卸載、政策遵守、VSS 一致性證據。

## Acceptance criteria

- [ ] **WIN-001** 管理員可以把 Windows Server 加為受管節點。 — 完成條件：不需 WSL、Docker、Node.js 或編譯器，Agent 可原生安裝、註冊、認證與回報。
- [ ] **WIN-002** Windows Agent 可以開機自啟。 — 完成條件：Server Core 與 Desktop Experience 在無使用者登入時仍可連回 Core。
- [ ] **WIN-003** 管理員可以使用 Windows 原生主機功能。 — 完成條件：服務、事件、檔案 ACL、更新、終端與磁碟按矩陣測試，不套用 Linux 命令。
- [ ] **WIN-004** 管理員可以看見功能不支援的真正原因。 — 完成條件：OS 限制、未實作、缺元件、無權限與離線分開顯示。
- [ ] **WIN-005** 系統可以阻擋錯誤作業系統的部署。 — 完成條件：Windows 原生節點部署 Linux 專用範本時在變更前被阻止；可另選 Linux 節點。
- [ ] **WIN-006** 管理員可以安全更新及卸載 Windows Agent。 — 完成條件：處理使用中的執行檔、服務權限、重開機要求、節點撤銷與結果回報。
- [ ] **WIN-007** 管理員可以遵守現有 Windows 管理政策。 — 完成條件：不繞過網域／WSUS／安全政策，不強迫安裝 WinGet 或 Chocolatey。
- [ ] **WIN-008** 管理員可以知道備份的一致性程度。 — 完成條件：VSS／應用備份成功與否有證據；無法保證一致時不能標成完整可還原備份。
- [ ] UI 分開顯示「OS 不支援」「平台未實作」「缺元件」三種情況

## Acceptance tests（第 42 章）

- **DEP-T13** 在 Windows Server Core 無人登入時重新開機。 → 原生 Agent 自啟、連回與回報正常，不依賴桌面工作階段。
- **DEP-T14** Windows 有網域更新政策且沒有 WinGet。 → 仍可安裝 Agent，尊重政策；不私自安裝套件管理器或切換更新來源。
- **DEP-T15** 在 Windows 原生節點部署 Linux 商店範本或要求 K3s。 → 明確拒絕不支援組合，未修改主機；說明可選 Linux 節點。
- **DEP-T16** Windows 更新時執行檔被使用或需重開機。 → 透過安全的版本切換／核准重開機流程處理，不標示假成功。
- **DEP-T17** Windows VSS writer 失敗。 → 備份失敗或以明確降低保證狀態呈現，不宣稱應用一致。
- **DEP-T18** Core 與新舊 Linux／Windows Agent 同時連線。 → 正確交換能力與協定版本；不相容變更被阻擋，安全讀取／更新路徑可見。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
