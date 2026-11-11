# 006 — 維護者可以在 CI 建置、測試並產生各架構成品

**Epic:** 發布與供應鏈（`epic:release`）  
**User Story:** 維護者可以發布可安裝的版本  
**Milestone:** M0 基礎與設計  
**Blocked by:** 003 管理員可以啟動 hazelminx-core 並通過健康檢查, 004 管理員可以在主機上以 systemd 執行 hazelminx-agent 並回報身分, 005 使用者可以開啟 React 介面並切換語言與淺深色  
**規格章節:** 37｜安裝位置、版本更新與平台自我修復  
**Issue:** [#6](https://github.com/HazelnutParadise/hazelminx/issues/6)  

## What it delivers

GitHub Actions：Go／Rust／前端的 lint、test、build；Linux x86_64 與 ARM64 成品；前端建置結果嵌入 Core；產物依賴檢查報告（ldd／otool 等效）。

## Acceptance criteria

- [x] 交叉編譯成功不算驗收；至少一個目標在實際 OS 執行冒煙測試
  證據：Go 與 Rust 都在原生的 `ubuntu-24.04`（x86_64）與 `ubuntu-24.04-arm`（ARM64）機器上建置，不交叉編譯。建好後在同一台機器上執行：
  - Core 實際啟動，`/healthz` 回 ok，沒嵌入前端時 `/` 回 503。
  - `hazelminx-agent identity` 建立節點身分，身分目錄是 0700。
  - `hazelminx-privileged check-config` 正常結束。
  - 高權限執行器的真實主機測試（`tests/linux_host.rs`）以 root 在 systemd 下執行，兩個測試都通過。

  依賴報告：Core 是靜態連結，Agent 與執行器只連結 `libc.so.6`，沒有 libssl。報告和執行檔都上傳成 artifact。四個 job 都通過（[CI 執行 42443690986](https://github.com/HazelnutParadise/HazelminX/actions/runs/42443690986）。
- [x] 鎖檔版本固定，CI 不使用浮動工具版本
  證據：Go 版本取自 `core/go.mod`（`go 1.26.0`），CI 會比對實際版本，不同就失敗。Rust 取自 `agent/rust-toolchain.toml`（`1.97.0`），同樣比對。Cargo 一律加 `--locked`，Go 先跑 `go mod verify`。四個 actions 都以 commit SHA 固定，旁邊註明版本。

## Delivery record

依規格第 46.4 節，完整內容在 PR #127 描述。

還沒做完，所以維持 `in_progress`：
- 前端 job：等票 005（PR #126）合併後補上，內容是用固定版本的 Vite+ 跑 `vp check`、`vp test`、`vp build`。
- 前端嵌入 Core：等 005 的 `scripts/embed-web.sh` 進 `main` 後，Core 改以 `-tags embedweb` 建置，冒煙測試改成確認 `/` 回 `index.html`。
- 正式發版用的簽署、SBOM 與發布流程屬於票 097，不在這張票。
