# 005 — 使用者可以開啟 React 介面並切換語言與淺深色

**Epic:** 基礎建設（`epic:foundation`）  
**User Story:** 使用者可以在一致的介面完成日常管理  
**Milestone:** M0 基礎與設計  
**Blocked by:** [001 維護者可以在單一 repo 取得規格、架構決策、票務與工作規則](001-repo-scaffold.md), [002 設計師與開發者可以依 DESIGN.md 與互動原型審查主要畫面](002-design-system.md)  
**Status:** in_progress  
**規格章節:** 07｜介面與操作體驗  
**Issue:** [#5](https://github.com/HazelnutParadise/hazelminx/issues/5)  

## What it delivers

web/ 使用 React、TypeScript 與 Vite+：元件庫 Cloudflare Kumo（`@cloudflare/kumo`）＋ `theme-hazelminx.css` 覆蓋語意 token（`docs/design/component-mapping.md`）、路由殼層、設計 token、zh-TW／en 的 i18n、淺深色、API client 產生器、vitest 與 vp check；建置產物可被 Core 嵌入。

## Acceptance criteria

- [x] **UI-001** 使用者可以切換繁體中文、英文、淺色與深色模式。 — 完成條件：系統選單、錯誤與日期顯示一致；第三方原始訊息保留原文及白話摘要。
  - 證據：`web/e2e/shell-flows.spec.ts`「theme toggle switches mode, keeps it across reloads」「language switch translates the shell and persists」（帳號選單切英文後頁首、側欄、文件標題一起換，重新整理後保留）；所有畫面字串走 `web/src/i18n/locales/*.json`，`locales.test.ts` 檢查兩語言鍵與插值變數一致；錯誤用 `ApiErrorMessage` 依錯誤碼翻成白話並保留原文；Kumo 自帶字串經 `KumoLocaleProvider` 翻譯。
- [x] 第三方原始訊息保留原文並附白話摘要的呈現元件存在
  - 證據：`web/src/components/RawMessage.tsx`（`RawMessage`、`ApiErrorMessage`）；`components.test.tsx` 驗證原文逐字保留（含換行、`translate="no"`）、來源與 trace ID、未知錯誤碼退回 HTTP 狀態。
- [x] 日期以使用者時區顯示，儲存值為 UTC RFC 3339
  - 證據：`web/src/lib/time.ts`（`parseRfc3339` 嚴格驗證、`toRfc3339Utc`、`formatDateTime` 依時區與語系、`formatElapsed`）；`time.test.ts` 44 條含跨日（2026-11-08T20:00:00Z 在 Asia/Taipei 顯示 11/09）。目前「使用者時區」取瀏覽器時區；帳號層級的時區設定屬身分與設定的票。
- [x] Kumo 套上 HazelminX 主題後，`docs/design/tools/` 的對比、觸控目標、可及名稱掃描在殼層頁面全部通過
  - 證據：repo 的 `docs/design/tools/` 只有版面掃描（`scan_layout.py`），對比、觸控目標、可及名稱沒有腳本，所以寫成等效的 Playwright 掃描 `web/e2e/scan.ts`：axe-core WCAG 2.1 A/AA、自算文字對比（含漸層與疊層，axe 漏掉的 Kumo 主要按鈕白字 2.00:1 由它抓到並已修正）、觸控目標（手機 ≥ 44、桌面 ≥ 24）、整頁溢出、文字裁切、未翻譯的 i18n 鍵。涵蓋總覽、平台設定、巢狀路徑、404、共用元件展示頁 × 360／768／1024／1280／1440／1920 × 深淺色 × 中英文 × 側欄收合：開發伺服器 85 條、Core 嵌入的正式建置 71 條全部通過（另含錯誤邊界出現即判失敗、主控台錯誤即判失敗）。
- [x] 側欄圖示改用 Phosphor Icons，對照表記在 web/ 的 README
  - 證據：`web/src/app/navigation.ts`；對照表在 `web/README.md`「側欄圖示對照（Phosphor Icons）」。

## Acceptance tests（第 42 章）

- **T-118** 切深淺色、中文英文、錯誤與空資料。 → 文字與狀態完整，不只靠顏色，不顯示假的零值。

## Acceptance tests 結果

- **T-118** 通過：`web/e2e/shell-scan.spec.ts` 在深淺色、中英文、404 與錯誤頁、沒有資料的狀態下全部通過；狀態一律形狀＋文字（●○■），沒有資料時計數不顯示、觀測時間顯示「尚未觀測」，不顯示假的 0（`shell-flows.spec.ts`「unknown counts and observation time are not shown as zero」、`components.test.tsx` NavCount）。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。
