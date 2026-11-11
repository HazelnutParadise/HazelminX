# 元件庫決定：Cloudflare Kumo（2026-11-07）

使用者指示採用 Cloudflare 的 UI 庫。查證結果：

| 項目 | 事實 | 出處 |
|---|---|---|
| 舊的 `cloudflare/cf-ui` | 2019 年起未更新、repo 已封存、README 標示不維護 | GitHub API `archived: true`, `pushed_at 2019-04-14` |
| 現行 `@cloudflare/kumo` | 2.17.0（2026-11-02）、MIT、repo 2026-11-06 仍有提交、4.1k ★ | npm `view`、GitHub API |
| 技術底 | React 18／19、Base UI 無障礙原語、Tailwind v4（或 standalone CSS）、Phosphor Icons、ECharts 6（圖表）、zod 4 | `package.json` peerDependencies |
| 主題 | 語意 token（`--color-kumo-base／elevated／recessed／line／hairline／brand／danger／warning／success／info…`），`light-dark()` 自動深淺色，`data-mode`、`data-theme` 切換；內建 `kumo`、`fedramp` 兩套主題 | `dist/styles/theme-kumo.css` |
| 元件 | 54 個：Table（含 CheckCell／CheckHead、sticky 欄）、Sidebar（icon 收合、可拖寬）、Tabs、Dialog、Toasty、CommandPalette、Breadcrumbs、Pagination、Meter、Badge、Banner、Empty、Loader、SensitiveInput、Code、Collapsible、DateRangePicker、Chart／TimeseriesChart、Combobox、TagInput… | `npx @cloudflare/kumo ls` |
| 沒有的 | 樹狀元件、拓撲圖、熱格、環形量表、日曆熱格、流程段、終端機、編輯器 | 同上 |

結論：採用 Kumo 當元件底層，取代 ENG.md 原本的「Radix Primitives＋自家元件」。DESIGN.md 的 token 對映到 Kumo 的語意 token；我們的圖形元件維持自製（SVG），圖表走 Kumo 的 Chart（ECharts）但配色與規則照 DESIGN.md「資料視覺化」節。

## Token 對映（DESIGN.md → Kumo）

| DESIGN.md | Kumo token |
|---|---|
| neutral（底） | `--color-kumo-canvas` |
| panel | `--color-kumo-base` |
| raised | `--color-kumo-elevated` |
| line | `--color-kumo-line`；1px 內框用 `--color-kumo-hairline` |
| primary／secondary／faint 文字 | `text-kumo-default`／`text-kumo-subtle`／`text-kumo-faint`（以實際 token 名為準） |
| tertiary（hazel） | `--color-kumo-brand`、`--color-kumo-brand-hover` |
| status-ok／warn／danger／info | `--color-kumo-success`／`warning`／`danger`／`info` 與各自 `-tint` |
| 焦點環 | `--color-kumo-focus` |

做法：在票 005 寫一份 `theme-hazelminx.css`，用 `[data-theme="hazelminx"]` 覆蓋上表 token 的 `light-dark()` 值；深淺色用 `data-mode`。字型改 Geist／Geist Mono。不用 Cloudflare 的 Logo 元件與品牌色。

## 畫板元件 → Kumo

| 畫板裡的東西 | 實作 |
|---|---|
| 頂列、側欄（圖示、收合） | `Sidebar`（collapsible="icon"）＋自家頂列（Toolbar、CommandPalette、Badge） |
| 表格（排序、勾選、分頁、列尾動作） | `Table` + `Table.CheckHead/CheckCell` + `Pagination` + `DropdownMenu`；排序用 Head 的按鈕 |
| 狀態 pill | `Badge`（variant 對應 ok／warn／danger／info）＋我們的形狀 |
| 範圍列、分頁列 | `Tabs`（segmented） |
| 卡片、面板 | `Surface`、`LayerCard` |
| 表單（安裝應用、設定） | `Field`、`Input`、`Select`、`Switch`、`SensitiveInput`、`TagInput` |
| 確認框、危險操作 | `Dialog`、`AlertDialog` 原語 |
| 通知、banner | `Toasty`、`Banner` |
| 空狀態、載入 | `Empty`、`Loader` |
| 時間圖、長條 | `TimeseriesChart`／`Chart`（ECharts），配色與規則照 DESIGN.md |
| 環、量表 | `Meter`（線性）；環形量表自製 SVG |
| 拓撲、地圖、熱格、日曆、流程段、樹 | 自製（SVG／CSS），樹用 `Collapsible` 原語組 |
| 終端機、編輯器 | xterm.js、CodeMirror 6（不變） |

## 風險與驗證（票 005 第一週做）

- Tailwind v4 進入建置鏈；不想用就 `styles/standalone`（CSS 較大）。
- 主題覆蓋只能靠覆蓋 CSS 變數，Kumo 沒有消費端的 theme generator；要驗證覆蓋後對比仍符合 DESIGN.md 門檻（跑 `docs/design/tools/` 的掃描）。
- 版本更新快（2026 年內 1.0 → 2.17），鎖定版本、每月升級一次並跑視覺回歸。
- Phosphor Icons 取代畫板裡的自繪線條圖示；側欄圖示對照表在票 005 建立。

## 實作時發現的 Kumo 差異（票 005）

- **Empty 自帶外框**：Kumo 的 `Empty` 會畫自己的外框與內距。放進面板時要關掉它的外框，避免出現框中框；獨立空頁才保留。
- **主要按鈕強制白字**：Kumo 的主要按鈕把文字寫死成白色，在榛果色底上對比只有 2.00。覆蓋主要按鈕的文字色為 `--hazelink`（深色字），淺深兩種主題都要掃描對比。
