# hazelminx-web

HazelminX 的網頁介面：React 19、TypeScript、Vite+（`vp`），元件庫是 Cloudflare Kumo（`@cloudflare/kumo`），主題照 `DESIGN.md`。建置結果由 Core 嵌入提供，主機上不需要 Node.js。

## 開發

需要 Node.js 24。套件管理由 Vite+ 處理（`package.json` 的 `devEngines` 指定 npm 版本，`vp` 會自動取得），所以一律用 `vp` 或 `vpr`，不要直接用系統的 `npm`。

```bash
vp install            # 安裝套件
vp dev                # 開發伺服器 http://localhost:5173，/management 轉給 HAZELMINX_CORE_URL（預設 http://127.0.0.1:9800）
vp check              # 格式、lint、型別
vp test               # 單元測試（vitest + Testing Library）
vpr e2e               # Playwright：外殼流程與版面／無障礙掃描
vp build              # 產出 dist/
```

第一次跑 Playwright 前要下載瀏覽器：`vp exec playwright install chromium`。

## 嵌入 Core

`scripts/embed-web.sh`（repo 根目錄）會建置 `web/dist`、複製到 `core/web/dist`，再以 `-tags embedweb` 建置 Core。Core 提供方式：

- `/assets/*` 是帶雜湊的檔名，回 `Cache-Control: public, max-age=31536000, immutable`；找不到時回 404，不回 `index.html`。
- 其他靜態檔（`boot.js`、`favicon.svg`）回 `no-cache`。
- 其他路徑一律回 `index.html`（`no-store`），交給前端路由；`/management/`、`/integrations/` 不在此列。

掃描已嵌入的建置：啟動 Core 後 `HAZELMINX_E2E_URL=http://127.0.0.1:9800 vpr e2e`（開發用的元件展示頁不在正式建置裡，會自動略過）。

## 目錄

| 路徑                             | 內容                                                                                                                                                               |
| -------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `src/app/navigation.ts`          | 所有頁面的路徑、圖示、側欄分組（唯一來源）                                                                                                                         |
| `src/app/routes.tsx`             | 路由；各頁目前是空殼，由各自的票填入                                                                                                                               |
| `src/app/shellStatus.ts`         | 外殼顯示的狀態（觀測時間、告警數、工作數、沒有回應的主機、使用者）；沒有值就不顯示，不補 0                                                                         |
| `src/shell/`                     | 頂列、側欄、手機 chip 導覽、頁首、空頁、404、載入失敗頁、首次載入骨架（畫板 `ShellStates`）                                                                        |
| `src/components/`                | 全系統共用元件：儲存列 `SaveBar`、主機狀態列 `HostBar`、元件停止橫條 `ComponentDownBanner`、原文＋白話摘要 `RawMessage`／`ApiErrorMessage`、狀態 pill `StatusPill` |
| `src/styles/theme-hazelminx.css` | 用 DESIGN.md 色票覆蓋 Kumo 語意 token（對映表在 `design/component-mapping.md`）                                                                               |
| `src/i18n/`                      | i18next；`locales/zh-TW.json` 是預設與鍵的基準                                                                                                                     |
| `src/lib/time.ts`                | RFC 3339 解析、UTC 序列化、依使用者時區顯示                                                                                                                        |
| `src/api/`                       | `client.ts`（Cookie 工作階段＋CSRF、錯誤轉 `ApiError`）、`typed.ts`（openapi-fetch 型別化 client）                                                                 |
| `tools/openapi/`                 | API 型別產生器（獨立小套件，見下）                                                                                                                                 |
| `public/boot.js`                 | 首次繪製前套用已存的深淺色與語言，避免閃爍                                                                                                                         |
| `e2e/`                           | Playwright 測試                                                                                                                                                    |

## 深淺色與語言

- `<html data-theme="hazelminx" data-mode="dark|light">`。Kumo 依 `data-mode` 設 `color-scheme`，主題 token 用 `light-dark()` 取值。預設深色。
- 語言預設台灣繁體中文（`zh-TW`，`<html lang="zh-Hant-TW">`）。偏好存在 `localStorage` 的 `hx.mode`、`hx.lang`、`hx.sidebar`。
- 新增語言：在 `src/i18n/locales/` 加一個和 `zh-TW.json` 同鍵的檔案，並在 `src/i18n/index.ts` 的 `LANGUAGES` 加一筆；`locales.test.ts` 會檢查鍵與插值變數是否一致。英文等有複數變化的語言用 i18next 的 `_one`／`_other` 尾碼。
- 時間一律以 UTC RFC 3339 傳遞與儲存，畫面用 `formatDateTime()` 依使用者時區顯示。

## API 型別產生器

Core 會在 `/management/v1/openapi.json` 發布 OpenAPI 文件（票 010）。產生 `src/api/schema.gen.ts`：

```bash
vpr api:gen                                   # 從執行中的 Core（預設 http://127.0.0.1:9800）
HAZELMINX_OPENAPI=path/to/openapi.json vpr api:gen
node tools/openapi/gen.mjs <檔案或網址> --check  # CI 用：產物過期時結束碼 1
```

`openapi-typescript` 需要 TypeScript 5 的編譯器 API，TypeScript 7 沒有，所以產生器放在 `tools/openapi/` 自己的 `package.json` 與鎖檔裡。使用方式：

```ts
import type { paths } from "./schema.gen";
import { createManagementClient } from "./typed";
const api = createManagementClient<paths>();
const { data, error } = await api.GET("/management/v1/auth/session");
```

## 側欄圖示對照（Phosphor Icons）

圖示來自 `@phosphor-icons/react`（Kumo 的同一套），一律 16px、`regular` 粗細、`aria-hidden`，文字標籤才是可及名稱。

| 頁面                           | 路徑                | Phosphor 圖示    |
| ------------------------------ | ------------------- | ---------------- |
| 總覽                           | `/`                 | `SquaresFour`    |
| 工作中心                       | `/jobs`             | `ListChecks`     |
| 事件與告警                     | `/events`           | `Bell`           |
| 專案與服務                     | `/projects`         | `Stack`          |
| 應用商店                       | `/app-store`        | `Storefront`     |
| 網站與入口                     | `/websites`         | `Globe`          |
| 資料庫                         | `/databases`        | `Database`       |
| AI                             | `/ai`               | `Brain`          |
| API 代理                       | `/api-proxy`        | `PlugsConnected` |
| 主機與節點                     | `/nodes`            | `HardDrives`     |
| 容器                           | `/containers`       | `Cube`           |
| Kubernetes                     | `/kubernetes`       | `SteeringWheel`  |
| 儲存與備份                     | `/storage`          | `Archive`        |
| 計畫任務                       | `/cron`             | `Clock`          |
| 安全防護                       | `/security`         | `ShieldCheck`    |
| 流量分析                       | `/analytics`        | `ChartLine`      |
| 日誌                           | `/logs`             | `Scroll`         |
| 身分與存取                     | `/iam`              | `UsersThree`     |
| 平台設定                       | `/settings`         | `GearSix`        |
| 終端機（頂列；手機在帳號選單） | `/terminal`         | `TerminalWindow` |
| 帳號安全（帳號選單）           | `/account/security` | `LockKey`        |
| 收合側欄                       | —                   | `SidebarSimple`  |

頂列其他圖示：搜尋 `MagnifyingGlass`、需要處理 `Bell`、切換深淺色 `Sun`／`Moon`、帳號 `UserCircle`、未登入時的語言 `Globe`。進行中的工作只在側欄「工作中心」計數，頂列不重複放。帳號選單另有登出 `SignOut`。
