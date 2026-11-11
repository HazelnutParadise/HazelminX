# 規格增補 04｜專案的部署單元、單元之間的連線與 env 檔

- **狀態**：已核定。2026-11-08 使用者決定以下幾件事：
  - 一個專案可以包含多個 Compose 與 Kubernetes 部署。
  - 單元之間的連線參考 Zeabur 的介面。
  - 環境變數分成有名稱的組，組名就是檔名，Compose 的 `env_file` 依名稱讀取。
  - 所有可以填環境變數的地方，都可以上傳任意檔名的 env 檔；上傳時要填組名，預設帶入上傳的檔名。
  - 組名平時也可以改。
  - 可以像 Portainer 一樣指定要用哪幾份 Compose 檔。
  - 每次部署都重新建置，建置完成後才無縫替換，過程中不斷線。
  - 以上新的預設都可以關掉。
- **位置**：本增補是第 52 章，接在增補 03（第 51 章）之後。v1.1 原檔與 `SHA256SUMS.txt` 不改。需求 ID 以 `PRJ-` 開頭，驗收案例從 `T-189` 開始編號。
- **設計原則**：設定怎麼分、怎麼命名、怎麼疊，以 Compose、Dockerfile、Helm、Kubernetes 實際讀取的方式為準（52.5）。平台自己加的規則一律標成「HazelminX 規則」，不說成工具的原生行為（呼應 15.2）。
- **細化的既有需求**：本增補細化下列條文，並沿用它們的隔離規則。
  - 15.2 環境變數的明確規則（repo `.env`、上傳檔、介面值的合併順序、乾淨的執行環境、相容解析器）。
  - 第 18 章不中斷部署：DEP-001～005、DEP-008，以及 18.1 的部署能力標籤。
  - 19.2 Zentinel 連到容器的方式、39.3 容量不足時的處理。
  - 34.1「專案與環境」與「正式環境權限不能被測試環境繼承放大」。
  - CMP-001、CMP-002、CMP-004、CMP-005、CMP-006、CMP-011、CMP-016、CMP-017、CMP-018。
  - ENV-007、GIT-010、KUB-005、KUB-007、KUB-012。
  - 增補 03 的 ROB-002（漂移）與 ROB-006（容器歸屬判定）。
- **不變的部分**：v1.1 第 162 行「不保證任意 Compose 能無損轉成 Kubernetes」仍然成立，平台不做 Compose 轉 Kubernetes。
- **用詞**：
  - 「專案環境」指 production、staging 這類一套設定。
  - 「引擎環境」指增補 02 定義的「主機＋引擎＋擁有者＋連接端點」。
  - 「專案目錄」指 Compose 的專案目錄：第一份 Compose 檔所在的目錄，或使用者另外指定的工作目錄。

## 52.1 專案的結構

一個專案分三層：

| 層 | 是什麼 | 例子 |
|---|---|---|
| 專案 | 一個應用或產品 | shop |
| 專案環境 | 一套獨立的變數、機密、網域、資料與批准規則 | production、staging |
| 部署單元 | 專案環境裡的一個部署，各自有來源與目標 | web、worker、search |

部署單元有兩種：

| 類型 | 來源 | 目標 |
|---|---|---|
| Compose | 符合 CMP-001 的任一種來源：Git repo 加 Compose 檔、上傳的檔案、編輯器或商店。只有 Dockerfile 的 repo 視為只有一個服務的 Compose 單元，平台產生的服務定義存在覆寫層 | 一個引擎環境（某台主機上的 Docker 或 Podman） |
| Kubernetes | Git repo 裡的 Helm chart 路徑或 manifest 目錄、Helm repo 上的 chart，或 OCI registry 上的 chart | 一個叢集加一個 namespace |

規則：

- **一個單元一個目標**：同一個專案環境裡的單元，可以放在不同主機、不同引擎或不同叢集。
- **只有一個單元時**：專案環境頁面不顯示「部署單元」區塊與單元切換，畫面與 v1.1 的單一 Compose 專案相同。
- **Podman**：Compose 單元部署到 Podman 時，平台用 Docker Compose CLI（v2）連 Podman 的 socket，不使用 `podman-compose`。這樣 52.4、52.5 描述的 Compose 行為對兩種引擎都一樣。兩者行為仍有差異的地方，依增補 02 ENV-002 的偵測結果，在預檢中列出。
- **名稱規則**：專案、專案環境、單元、服務的名稱只能用小寫英文字母、數字與 `-`，必須以字母開頭，長度最多 30 字元。這組名稱要同時用在內部位址（DNS）、Compose 專案名稱與 Kubernetes 資源名稱，所以三種都要能接受。
- **Compose 專案名稱**：一律是 `<專案>-<專案環境>-<單元>`，例如 `shop-production-web`。
  - 這個名稱在單元建立時固定，之後改名也不變。
  - 平台執行 Compose 時一律用 `-p` 指定這個名稱。

## 52.2 單元之間的連線

參考 Zeabur 的做法：每個服務都有一個不會變動的內部位址；服務可以把變數設為「公開」，同專案的其他服務用 `${...}` 引用。

### 內部位址

- **每個服務都有一個內部位址**，格式是 `<服務>.<單元>.<專案環境>.<專案>.internal`。
  - Compose 單元裡的每個服務各有一個位址。
  - Kubernetes 單元裡的每個 Service 各有一個位址。
- **入口服務**：每個單元指定一個入口服務，入口服務另外有簡短位址 `<單元>.<專案環境>.<專案>.internal`，例如 `search.production.shop.internal`。只有一個服務對外提供 Port 時，它就是預設的入口服務。
- **改名不變**：位址裡用的是單元、服務建立時的名稱，之後改名也不變。
- **連線區塊**：每個單元的「連線」區塊列出：
  - 各服務的內部位址。
  - 對內 Port，含名稱與號碼。
  - 對外網址（有設定才顯示）。
  - 從其他主機連進來的方式（見下表）。

內部位址怎麼連通，依兩個單元的相對位置決定：

| 兩個單元的位置 | 怎麼連 |
|---|---|
| 同一個 Compose 單元內 | 用 Compose 服務名稱，不經過平台 |
| 同一台主機，不同單元 | 平台在主機上提供內部位址解析，轉到目標服務的 Port |
| 不同主機，且已有私有網路（例如 WireGuard） | 解析到私有網路位址 |
| 不同主機，沒有私有網路 | 使用 v1.1 第 1004 行所說「明確授權的加密轉送」，只開放被引用的 Port，不開放整個網段 |
| Kubernetes 單元 | 內部位址指向對應的 Service。叢集外的單元要連進來時，平台建立轉送，不要求使用者自行改成 NodePort |

連不連得到，由平台定期從引用端所在的主機實際連線探測，結果顯示在連線區塊與拓撲圖上。

### 公開變數與引用

- **公開與引用**：單元可以把自己的任一變數設為「公開」。同一個專案環境的其他單元，在平台的變數值裡用 `${單元.變數}` 引用；要引用非入口服務的特殊變數時，寫成 `${單元.服務.變數}`。
  - 例子：`SEARCH_URL=http://${search.HZX_HOST}:${search.HZX_PORT_HTTP}`。
  - Zeabur 直接寫 `${變數}`。這裡加上單元名稱，是為了避免不同單元有同名變數時互相蓋掉。
- **只用在平台的變數值裡**：這種寫法只能出現在平台的變數值中。Compose 的變數名稱不能含 `.`，所以寫進 Compose 檔會被當成錯誤。平台在寫出 env 檔之前就把引用解析完，Compose 讀到的已經是最終值。
- **特殊變數**：平台提供的特殊變數一律以 `HZX_` 開頭，使用者不能建立 `HZX_` 開頭的變數。
  - `HZX_HOST`：服務的內部位址。
  - `HZX_PORT_<名稱>`：對內 Port。Port 名稱轉成大寫，`-` 轉成 `_`。
  - `HZX_URL`：對外網址。
  - `HZX_PROJECT`、`HZX_PROJECT_ENV`：專案名稱與專案環境名稱。
  - `HZX_GIT_SHA`：只在建置階段提供。
- **不帶單元前綴的 `${X}`**：只在同一個單元內解析。順序依 52.4 的四層，找不到時才查特殊變數。
- **預檢會擋下的情況**：下列情況在部署預檢時擋下，不會用空字串代替。
  - 引用不到。
  - 引用到未公開的變數。
  - 變數之間互相引用形成循環，例如 A 的 X 引用 B 的 Y，B 的 Y 又引用 A 的 X。
- **單元互相引用是允許的**：兩個單元互相用對方的 `HZX_HOST`、`HZX_PORT_*` 或 `HZX_URL` 很常見。這三種值在部署前就已確定，引用它們不會要求被引用的單元先部署。
- **機密**：
  - 機密變數可以公開給同一個專案環境的其他單元。引用端只看得到「機密 · 來自 search」，看不到值。
  - 組合出來的值只要含有機密，例如 `postgres://u:${db.PASS}@...`，整個值就視為機密，遮罩並記入稽核。
- **隔離**：不同專案環境之間不能互相引用，不同專案之間也不行。這沿用 34.1「正式環境權限不能被測試環境繼承放大」。
- **被引用的值變了**：被引用的公開變數改變後，引用它的單元標「引用的值已變更，需要重新部署」，平台不自動重新部署。
- **回滾**：回滾時使用該版本當時解析出的值。
- **改名後的引用**：單元或服務改名後，舊名稱保留為引用別名，原本的 `${舊名.X}` 照常可以解析。畫面同時提供「把引用改成新名稱」。

## 52.3 部署與回滾

- **部署全部**：專案環境有「部署全部」。
  - 一個單元引用另一個單元的非特殊變數時，被引用的單元先部署。
  - 沒有這種依賴關係的單元同時部署。
  - 使用者可以手動調整順序。
- **結果與失敗**：
  - 每個單元各自回報結果。
  - 一個單元失敗時，預設停止還沒開始、而且直接或間接依賴它的單元。不依賴它的單元照常部署。
  - 也可以改成「任一單元失敗就停止全部還沒開始的單元」。
- **版本**：專案環境的版本，等於各單元版本的組合。單元版本的內容依 CMP-011，包含 commit、Compose 檔或 chart、覆寫層、變數版本與映像 digest。
- **回滾**：
  - 可以整個專案環境回到某個版本，也可以只回滾一個單元。
  - 回滾前檢查引用是否相容。例如回滾 search 後，web 引用的某個公開變數在舊版不存在，就先列出來。
- **Webhook**：一個 repo 的推送，只觸發來源是這個 repo 的單元。

## 52.4 變數組與 env 檔

### 變數組

環境變數不是一張平表，而是有名稱的「變數組」。一組就是一個 env 檔，組名就是檔名，例如 `.env`、`app.env`、`config/web.env`。

| 組 | 用途 | 依據 |
|---|---|---|
| `.env` | 預設只用來替換 Compose 檔裡的 `${VAR}`，不會自動進入容器。Compose 檔明確寫 `env_file: .env` 時，也會照常進入那個服務的容器 | Compose 讀取專案目錄 `.env` 的方式；CMP-005 |
| 其他組 | 被 `env_file:` 引用時，內容進入該服務的容器 | Compose 的 `env_file` |
| 標成「建置時」的組 | 建置時提供。一般變數以 build args 提供，機密以 `build.secrets` 提供 | Dockerfile 的 `ARG`；CMP-017 規定機密不能放在一般 build args |

### 四層

變數組放在哪一層，決定誰看得到。誰真的收到，仍由 Compose 的 `env_file:` 與 `environment:` 決定。

| 層 | 放什麼 | 怎麼進到容器 |
|---|---|---|
| 專案（所有專案環境共用） | 各環境都一樣、而且不是機密的值，例如 `APP_NAME`。這一層不能放機密 | 與同名的下層組合併，經 `env_file` 進入 |
| 專案環境 | 這個專案環境的單元共用的組，例如資料庫連線 | 經 `env_file` 進入 |
| 部署單元 | 單元自己的組，以及它的替換用組（`.env`） | 經 `env_file` 進入；替換用組只做替換 |
| 服務 | 只給某一個服務、又不想改 Git 的值 | 平台寫進覆寫層，放在該服務的 `environment:`（CMP-006） |

- **專案層的權限**：修改專案層的組，必須同時擁有所有專案環境的修改權限。否則只有 staging 權限的人，改專案層就會影響到 production。
- **專案層不能放機密**：
  - 名稱分段後（以 `_` 切開，不分大小寫）含 `PASSWORD`、`SECRET`、`TOKEN`、`KEY`、`PRIVATE` 的變數，預設標成機密。例如 `API_KEY` 符合，`MONKEY` 不符合。
  - 專案層拒收標成機密的變數。使用者若取消機密標記，要再確認一次，並記入稽核。
- **同名的組怎麼合併（HazelminX 規則）**：同一個組名出現在多層時，逐一變數合併，順序是專案 → 專案環境 → 部署單元，後面的優先。合併結果才寫成 Compose 讀取的那一個檔案。
- **同名時誰優先**：
  - Compose 本身的規則依 Docker Docs「Environment variables precedence」：服務覆寫寫進的 `environment:` 與 Compose 檔自己寫的 `environment:` 優先於 `env_file:`，`env_file:` 又優先於映像的 `ENV`。
  - 覆寫層排在 Compose 檔清單的最後，所以服務覆寫的值會蓋過 Compose 檔自己寫的 `environment:`。
  - 一個服務的 `env_file:` 列了多個組時，由上往下讀，同名時後面的組優先。
- **與 repo 裡的同名檔合併（沿用 15.2）**：repo 在同一個路徑已經有檔案時，逐一變數合併。repo 的檔案是基礎，再依序套用上傳的檔案與介面上的值。畫面標出每個值來自 repo、上傳檔或介面。

### 組名與路徑

- **路徑基準**：組名就是相對於專案目錄的路徑。`env_file: app.env` 讀名為 `app.env` 的組，`env_file: ./config/web.env` 讀名為 `config/web.env` 的組。合併多份 Compose 檔時，所有檔案裡的相對路徑都以專案目錄為基準，這與 Compose 相同。
  - 例子：主檔是 `deploy/prod.yml` 時，專案目錄是 `deploy/`。這時 `.env` 組對應的是 `deploy/.env`。
- **特殊路徑**：
  - 路徑含 `..` 時，正規化後仍要在 repo 範圍內，組名用正規化後的路徑。
  - 絕對路徑不對應任何組。預檢會提示「這個檔案由主機提供，平台不管理」。
- **寫到主機上的位置**：部署時，平台把合併後的組寫進平台管理的專案工作目錄，放在 Compose 預期的相對路徑，權限 0600。
  - 這些檔案會一直保留，讓 `compose ps`、`compose down` 等指令照常運作。
  - 平台每次觀測都比對檔案內容。有人在主機上直接改了這個檔案，就標為漂移（ROB-002）。
- **引用了不存在的檔案**：
  - 平台的組與 repo 裡都沒有這個檔案時，預檢擋下，並提供「建立這一組」與「上傳 env 檔」。
  - 只有 repo 有時，就用 repo 的檔案，「實際收到的值」標出來源是 repo。
  - Compose 的 `env_file` 長語法寫了 `required: false` 時，缺檔只提示，不擋下，與 Compose 相同。
- **沒被引用的組**：標「沒被引用」，不會自動刪除。
- **Kubernetes 單元**：
  - **名稱轉換**：變數組轉成 ConfigMap，機密轉成 Secret。名稱要符合 Kubernetes 的 DNS 子網域規則，所以轉成小寫、`/` 與 `.` 換成 `-`，並去掉開頭的符號。例如 `app.env` 轉成 `app-env`，`config/web.env` 轉成 `config-web-env`。畫面顯示轉換後的名稱。
  - **優先順序**：依 Kubernetes 的規則，`env` 優先於 `envFrom`；多個 `envFrom` 有相同變數時，排在後面的優先。平台依專案 → 專案環境 → 部署單元的順序排進 `envFrom`，服務層的值放在 `env`。
  - **掛進工作負載**：平台用 Helm 的 post-renderer，替使用者選的工作負載加上 `envFrom`。這些欄位的擁有者是平台（KUB-012）。
  - **更新觸發**：變數組內容改變時，平台在 Pod 範本加上內容摘要的註記，觸發滾動更新。`envFrom` 只在容器啟動時讀取，不加註記的話，改了也不會生效。
  - **替換用組**：Kubernetes 單元沒有 Compose 的替換步驟，所以不使用替換用組（`.env`）。

### 上傳與命名

- **在哪些地方可以用**：
  - 專案層、專案環境、部署單元、服務覆寫。
  - 匯入流程。
  - 建立容器。
  - 計畫任務的環境變數。
  - 應用安裝的進階參數。
- **放進來的方式**：直接上傳檔案、拖放，或貼上文字。也可以切換成「原始文字」，一次編輯整組。
- **組名**：上傳時要填組名，預設帶入上傳的檔名，例如上傳 `prod.env` 預設叫 `prod.env`。檔名與副檔名不限，`.env.production`、`settings.txt` 都可以。
- **預設放在哪一層**：在單元裡上傳的檔案，預設建在該單元。兩個單元都寫 `env_file: app.env` 很常見，但內容不一定相同，放在共用層容易互相覆蓋。要共用時，可以把組「移到專案環境」。
- **同名的組已經存在**：只比對同一層。同層已有同名的組時，讓使用者選「合併進現有的組」或「改用別的名稱」。
- **改名**：組名平時也可以改。改名前列出所有引用這個組的地方：
  - 引用在平台的覆寫層：一起更新。
  - 引用在 Git 的 Compose 檔：平台改不到，所以舊名稱保留為別名，並標「Git 仍使用舊名稱」，不會讓下一次部署因為找不到檔案而失敗。
  - 別名期間：新的組不能使用這個名稱。部署時平台檢查 Compose 檔，已經沒有任何引用時，別名自動移除。
- **多個替換用組**：一個 Compose 單元可以把多個組標成「替換用」並排好順序。平台用多個 `--env-file` 依序傳給 Compose，後面的優先。這對應 CMP-004「多份 `.env` 指定順序」。

### 解析與寫出

- **相容目標**：解析器以 Compose 使用的 compose-go dotenv 格式為相容目標（呼應 15.2「相容解析器」）。
- **語法**：
  - 支援 `KEY=VALUE`、`export KEY=VALUE`、單雙引號、`#` 註解、跳脫字元，以及雙引號內跨行的值。
  - 編碼是 UTF-8，含或不含 BOM 都可以；換行可以是 LF 或 CRLF。
- **三種狀態分開**：
  - `KEY=`：值是空字串。
  - 組裡沒有這一行：未設定。
  - 「刪除」：從組裡移除這一行。
- **無效的行**：逐行列出行號與原因，其餘有效的行照常匯入。
- **重複的名稱**：以後出現的為準，並標出來。
- **行號**：畫面上的「第幾行」，一律指平台寫出的那個組檔案中的行號。
- **寫出時的跳脫**：
  - 平台寫出 env 檔時，值一律加單引號，裡面的單引號跳脫。Compose 不會展開單引號值裡的 `${}`，所以值會照原樣進入容器。
  - 服務覆寫寫進覆寫層 YAML 的 `environment:` 時，`$` 寫成 `$$`。
  - 以上兩點以實測為準，寫進驗收案例 T-215。

### 套用前預覽

- **變更分類**：分成新增、變更、相同三類。組裡現有、但檔案裡沒有的變數，預設保留；要逐一勾選才會刪除。
- **機密**：依「專案層不能放機密」那一點的規則判定機密，使用者可以改；值一律遮罩。

### 保存、稽核與匯出

- **保存**：平台只保存解析後的變數與來源檔名，不保存上傳的原檔。機密存入機密庫。
- **稽核**：只記錄變數名稱與來源，不記錄值。
- **匯出**：每一組都可以匯出成同名的 env 檔。機密預設不含值，勾選後才含值，並記入稽核。
- **上限**：單一檔案 1 MiB，一組最多 2,000 個變數。超過時回報錯誤，不會只匯入一部分。

## 52.5 跟 Compose、Helm 的實際讀法對齊

| 實際行為 | 畫面與預檢要做到 |
|---|---|
| 沒指定 `-f` 時，Compose 在專案目錄依序找 `compose.yaml`、`compose.yml`、`docker-compose.yml`、`docker-compose.yaml`，只用找到的第一個，同時有多個時只發警告；這一層找不到就往上層找。找到主檔後，在同一層再依序找 `compose.override.yml`、`compose.override.yaml`、`docker-compose.override.yml`、`docker-compose.override.yaml`，只合併第一個。一旦用 `-f` 指定檔案，就只用指定的檔案依序合併，不再自動加 override | 平台一律用 `-f` 執行，沒有自動選檔的模式：單元一定要指定主檔，像 Portainer 的 Compose 路徑加其他路徑。主檔可以是 repo 裡任一份 compose，再勾選要合併的其他檔並排序。override 檔不會自動合併，要用就勾進清單。<br>畫面依 52.5.1 列出 repo 裡找到的所有 compose 檔供勾選，並預覽合併後的結果。override 常放開發用設定，預檢會列出 override 改了哪些設定，並提示疑似開發用的項目：把 repo 目錄掛進容器、在主機上發布 Port、把 `command` 改成 `npm run dev` 這類開發指令。只提示，不擋下 |
| `.env` 裡的 `COMPOSE_PROJECT_NAME`、`COMPOSE_FILE`、`COMPOSE_PROFILES`，與 Compose 檔頂層的 `name:`，都會改變專案名稱、要讀的檔案與啟用的 profile | 平台一律用 `-p`、`-f`、`--profile` 明確指定，以平台的設定為準。<br>這幾個值出現在 `.env` 或 `name:` 時，預檢提示「平台設定優先，以下值不採用」並列出。不會靜默套用，也不會靜默忽略 |
| 有 `profiles:` 的服務，沒啟用該 profile 時不會啟動 | 單元可以選要啟用的 profile。不會啟動的服務標「profile 未啟用」，不算失敗 |
| `${VAR:?訊息}` 缺值時，Compose 直接失敗；`${VAR:-預設}` 會用預設值 | 預檢列出所有替換變數，分成缺值、用預設值、已提供三種。缺必填值時擋下 |
| `${VAR}` 替換時，主機 shell 的環境變數優先於 `.env` | 平台執行 Compose 時使用乾淨的環境（15.2），替換值只來自替換用組，主機上碰巧有的同名變數不會滲進來 |
| `env_file:` 列多個檔案時由上往下讀，同名時後面的優先。多份 Compose 檔合併時，`environment:` 依變數名稱合併，後面的檔案優先 | 服務引用多個組時，畫面上的組順序就是 `env_file` 的順序。平台的覆寫層永遠排在 Compose 檔清單的最後 |
| `build.args` 與 Dockerfile 的 `ARG` 只在建置時存在 | 標成「建置時」的組，只提供給使用者勾選的服務（預設是所有有 `build:` 的服務）。Compose 檔的 `build.args` 有寫值時，以 Compose 檔為準；只寫名稱沒寫值時，從「建置時」組取值。「建置時」組裡的機密改走 `build.secrets`。`HZX_GIT_SHA` 只在 Dockerfile 宣告了同名 `ARG` 時才提供 |
| `configs:`、`secrets:` 與掛進容器的單一檔案（例如 `./nginx.conf`）是檔案，不是變數 | 單元有「設定檔」區。每個檔案有名稱（就是相對於專案目錄的路徑）、內容與版本；與變數組一樣可以上傳、改名、比較版本。`secrets:` 的內容存在機密庫 |
| Compose 的專案名稱決定網路與儲存卷的名稱（`<專案名稱>_<卷>`） | 一律用 52.1 的固定專案名稱，畫面顯示實際的專案名稱與儲存卷名稱 |
| Helm 以 chart 內附的 `values.yaml` 為最底層，再依序疊加 `-f` 的 values 檔，最後套用 `--set`。map 深度合併，list 整個取代，值設成 `null` 會刪掉那個 key | 平台不把 chart 內附的 `values.yaml` 列成組。Kubernetes 單元的 values 是有名稱、有順序的檔案，例如 `values-production.yaml`，存在平台並有版本。合併預覽要標出 list 被整個取代、key 被 `null` 刪除的地方。values 裡標成機密的 key 存在機密庫，以另一份只在部署時產生的 values 檔提供。平台不使用 `--set` |

### 52.5.1 掃描 repo 裡的 Compose 檔

平台不替使用者決定用哪份 Compose 檔，只負責把候選檔全部找出來。檔名與順序依據 compose-go `cli/options.go` 的 `DefaultFileNames`、`DefaultOverrideFileNames`（2026-11-08 讀取，commit `6ac9323e`）。Docker 官方文件以 `compose.yaml` 為首選，舊的 `docker-compose.*` 為了相容仍然支援，所以新舊檔名都要掃到。

- **掃描的檔名**：主檔是 `compose.yaml`、`compose.yml`、`docker-compose.yml`、`docker-compose.yaml`；override 是 `compose.override.yml`、`compose.override.yaml`、`docker-compose.override.yml`、`docker-compose.override.yaml`。範圍是整個 repo 的每一層目錄，不只根目錄。檔名不在這八個裡的 YAML，例如 `deploy/prod.yml`，可以手動加進清單。
- **排列**：依目錄分組，根目錄在最前面。同一個目錄裡照上面的順序排，舊檔名是 `.yml` 排在 `.yaml` 前面，跟新檔名相反。
- **一定要選**：不預先選任何檔案，只有一份候選檔時也一樣。沒指定主檔時不能儲存單元，也不能部署。同一個目錄裡，Compose 不加 `-f` 時會用的那份標「Compose 預設會用」，只是提示。
- **子目錄有多份**：可以選一份，或一次建立成多個單元，每個單元各自指定。
- **大小寫**：檔名要完全一致。`Compose.yaml`、`docker-compose.YML` 在 Linux 上不會被 Compose 讀到，平台列出這類檔案，標「檔名大小寫不符，Compose 不會讀」，不能選。
- **符號連結**：指向 repo 外面的 Compose 檔列出來但不能選，畫面寫出原因。
- **repo 的 `.env` 設了 `COMPOSE_FILE`**：照上表，平台不採用並在預檢列出。旁邊提供「照 COMPOSE_FILE 帶入」，按下後依序帶入它列的檔案，使用者確認後才儲存。
- **指定的檔案在新 commit 不見了**：例如 `docker-compose.yml` 被改名成 `compose.yaml`，預檢擋下並寫「指定的 docker-compose.yml 在 commit 9b3e7f1 不存在」，推送觸發的部署不執行。畫面列出新 commit 裡的候選檔，讓使用者重新指定。
- **新 commit 多了 Compose 檔**：預檢提示「repo 新增了 compose.yaml，沒有使用」，不擋下，照原本指定的檔案部署。
- **都找不到**：只有 Dockerfile 時照 CMP-008 處理；兩者都沒有時寫出缺什麼，不自動產生 Compose 檔。

合併多份檔案時，另外照下面三條處理。依據是 Docker 文件〈Merge Compose files〉與〈Include〉（2026-11-08 讀取）：

- **跨目錄合併**：Compose 以主檔所在的目錄解析所有合併檔案裡的相對路徑，不是以各檔案自己的位置。勾選的檔案不在主檔的目錄時，預檢逐條列出它裡面的相對路徑和實際會解析成的路徑，例如「compose.override.yaml 第 8 行 ./src → deploy/src」。只提示，不擋下，因為有些 repo 是刻意這樣寫的。
- **`include:`**：主檔或合併的檔案用頂層 `include:` 帶入的檔案，掛在帶入它的那份檔案底下，標「由 include 帶入」，勾選框固定為已勾選、不能取消。被帶入的檔案各自以自己的目錄解析相對路徑；它定義的資源跟其他檔案同名時，Compose 直接報錯，預檢在部署前列出衝突的名稱。被帶入的檔案在新 commit 不見時，跟指定的檔案一樣擋下部署；內容改變時算這個單元的變更，會觸發這個單元的部署。
- **同一份主檔被多個單元使用**：同一個 repo 的同一份主檔已經被其他單元使用時（不分專案環境），預檢列出那些單元。部署到同一個引擎環境時，會衝突的主機 Port 與 `container_name` 擋下並寫出是哪一行；部署到不同引擎環境時只提示。

## 52.6 部署：每次建置，建置完才無縫替換

`docker compose up -d --build` 會先建置，但重建容器時是「先停舊的、再起新的」，中間會斷線。所以平台不直接用這個指令替換符合條件的服務，而是照第 18 章的原則自己做。

以下 1、2 是 HazelminX 規則，與 Compose 的預設不同。Compose 預設的 `pull_policy` 是 `missing`，只有缺映像時才拉取或建置。

1. **每次都建置與拉取**：
   - 有 `build:` 的服務，每次部署都重新建置，使用層快取；可以勾「不用快取」。
   - 只寫 `image:` 的服務，每次部署都重新拉取 tag，並記錄實際的 digest。
   - Compose 檔明確寫了 `pull_policy` 的服務，照 Compose 檔的設定。例如 `never` 就不拉取，`build` 就每次建置。
   - **沒變的服務不替換（DEP-008）**：建置或拉取完，映像 digest 與有效設定都沒變的服務，不重建、不替換。例如 `postgres:16` 的 tag 沒有變動時，資料庫不會被動到。
2. **建置完才動**：建置、拉取、預檢都完成之前，舊容器照常服務，不重建、不停止。任一步失敗，舊版完全不受影響（DEP-002）。
3. **新舊並行**：
   - 新版容器在舊版旁邊啟動，不使用固定的容器名稱，也不在主機上發布固定 Port。Zentinel 連到容器時，可以使用平台配發的受控本機回送 Port（19.2）。
   - 並行期間多出來的容器屬於這次部署，不算副本數不符的漂移（ROB-006）。
   - 主機的 CPU、記憶體或磁碟不夠同時跑兩份時，不會默默改成先停再起。改為拒絕並寫出原因，讓使用者選「先停再起（會中斷約 N 秒）」或排進維護時段（39.3）。
4. **健康了才切換**：
   - 依健康檢查判斷，只看到程序在跑不算健康（DEP-003）。
   - 映像與 Compose 檔都沒有健康檢查時，這個服務不能無縫替換。畫面改標「先停再起」，原因寫「沒有健康檢查」。
   - 健康後，由 Zentinel 把新請求切到新版。
5. **排空與觀察**：
   - 舊版不再接新的外部請求，處理完進行中的請求後，從內部網路名稱移除。長連線依排空政策（DEP-005）。
   - 用內部名稱建立的長連線，超過排空期限後會中斷。
   - 舊版容器保留到觀察期結束（DEP-004，預設 10 分鐘），期間可以一鍵切回，之後才停止並移除。
   - 新版不健康時不切換，舊版繼續服務，這次部署標為失敗。

每個服務在部署前都會標出替換方式（DEP-001）。一個服務可以同時有多個標示，例如「先停再起」加「需要批准」：

| 標示 | 條件 |
|---|---|
| 無縫替換 | 同時符合以下全部條件：<br>透過 Zentinel 或內部名稱接流量。<br>有健康檢查。<br>沒有寫死 `container_name`。<br>沒有在主機上發布固定 Port。<br>沒有用 `network_mode: host`。<br>沒有被標成「單寫入資料」。<br>主機容量足夠同時跑兩份 |
| 先停再起（會中斷約 N 秒） | 不符合上一列任一條件。畫面寫出是哪一條，例如「compose 第 23 行寫死 `container_name: shop-db`」。<br>預估中斷秒數取最近 5 次「停止到健康」的中位數；沒有紀錄時顯示「首次部署，無法預估」 |
| 需要批准 | 被標成「單寫入資料」的服務，或不相容的資料變更（第 18 章表格） |

- **「單寫入資料」怎麼判定**：映像是常見的資料庫（PostgreSQL、MySQL、MariaDB、MongoDB、Redis 等）時，預設標成「單寫入資料」。使用者可以在服務上修改這個標記。無法判定時，偏保守處理。
- **背景工作者**：例如佇列消費者。新舊兩份同時消費可以接受時，標「無縫替換」；不行時，可以設成「先停再起」。
- **都可以關掉**：本節的預設都可以在單元或服務上關掉，關掉的設定記在部署紀錄裡。
  - 每次建置：可以改成「commit 與建置設定都沒變時，沿用上次的映像」。
  - 無縫替換：單一服務可以改成「先停再起」。
- **Kubernetes 單元**：依工作負載自己的更新策略做受控滾動更新，對應 18.1 的「Kubernetes 無狀態服務」。映像拉取依各工作負載的 `imagePullPolicy`。

## 52.7 要求

| ID | 誰可以做什麼與必要行為 | 可檢驗的完成條件 |
|---|---|---|
| PRJ-001 | 專案管理員可以在一個專案環境裡加入多個部署單元，類型是 Compose 或 Kubernetes。 | 同一個專案環境可以同時有部署到不同主機的 Compose 單元，以及部署到叢集的 Kubernetes 單元。只有一個單元時，不顯示部署單元區塊與單元切換。名稱符合 52.1 的規則；Compose 專案名稱固定為 `<專案>-<專案環境>-<單元>`，並以 `-p` 指定。部署到 Podman 時，用 Docker Compose CLI 連 Podman socket。 |
| PRJ-002 | 專案管理員可以部署 Kubernetes 單元，來源是 Git、Helm repo 或 OCI。 | 來源是 Helm chart 或 manifest 目錄。每次部署固定 commit 或 chart 版本；顯示 values、合併結果、差異與資源；不把 Compose 轉成 Kubernetes。 |
| PRJ-003 | 每個服務都有不會變動的內部位址，每個單元有入口服務與「連線」區塊。 | 改單元或服務名稱後，位址不變；連線區塊列出各服務的內部位址、對內 Port、對外網址與連入方式。 |
| PRJ-004 | 單元可以公開變數，其他單元在平台變數值裡用 `${單元.變數}` 引用。 | 依 52.2 解析，特殊變數以 `HZX_` 開頭。下列情況在預檢擋下：引用不到、引用到未公開的變數、變數循環引用。單元互相引用特殊變數是允許的。不同專案環境之間不能引用。被引用的機密與含機密的組合值，在引用端遮罩。被引用的值變了，引用端標「需要重新部署」。改名後舊名稱保留為引用別名。 |
| PRJ-005 | 不同位置的單元之間可以照 52.2 的表連線。 | 有私有網路時走私有網路；沒有時用只開放被引用 Port 的加密轉送；能連到 Kubernetes 單元的 Service；平台定期實測連通性，拓撲圖畫出引用關係，連不到的引用標紅。 |
| PRJ-006 | 專案管理員可以部署整個專案環境，也可以回滾整個專案環境或單一單元。 | 依 52.3 決定部署順序與失敗政策；每個單元各自回報結果；回滾使用當時解析出的值，並在回滾前列出引用不相容的地方；Webhook 只觸發用到該 repo 的單元。 |
| PRJ-007 | 專案管理員可以把環境變數分成有名稱的組，組名就是相對於專案目錄的路徑，放在專案、專案環境、部署單元或服務四層。 | 四層與合併依 52.4 運作，並標成 HazelminX 規則。專案層：修改要有所有專案環境的權限，拒收機密。`.env` 預設只替換，被 `env_file` 明確引用時也進入容器。與 repo 同名檔逐一變數合併（15.2）。缺檔時，repo 有就用 repo，`required: false` 只提示，都沒有才擋下。Kubernetes 單元依 52.4 的名稱轉換、`envFrom` 順序、post-renderer 與摘要註記運作。 |
| PRJ-008 | 使用者可以在 52.4 列出的每個位置上傳、拖放或貼上 env 檔；上傳時要填組名，預設是上傳的檔名。 | 檔名不限。解析依 52.4，區分空字串、未設定與刪除。預覽分出新增、變更、相同，不自動刪除現有變數。機密依規則判定並遮罩。同一層已有同名的組時，可以選合併或改名。超過 1 MiB 或 2,000 個變數時報錯，不會只匯入一部分。 |
| PRJ-009 | 使用者可以隨時替變數組或設定檔改名。 | 改名前列出所有引用。覆寫層的引用一起更新。Git 裡的引用保留舊名稱當別名並標示，下一次部署不會因找不到檔案而失敗。別名期間，同名不能被新的組使用；Git 已經沒有引用時，別名自動移除。 |
| PRJ-010 | 使用者可以看到每個服務實際收到的環境變數。 | 依 52.4「同名時誰優先」算出最終值。每個值標出來源（哪一層、哪一組的第幾行、repo、上傳檔或介面、Compose 的哪一行、映像），並列出被蓋掉的值；機密遮罩。Kubernetes 單元依 `env` 與 `envFrom` 的順序計算。 |
| PRJ-011 | 部署單元可以依 52.5 設定 Compose 檔、profile、替換變數、建置變數、設定檔與 Helm values。 | 52.5 表中的每一列都有對應的畫面與預檢，預設值與工具的預設行為相同。單元一定要指定 Compose 主檔，候選清單依 52.5.1 掃描新舊檔名；指定的檔案在新 commit 不存在時擋下部署。跨目錄合併、`include:` 與同一份主檔被多個單元使用，依 52.5.1 預檢。`COMPOSE_*` 與 `name:` 不採用時會提示。執行環境是乾淨的。 |
| PRJ-012 | 單元改名不會改變它的 Compose 專案名稱、儲存卷名稱與內部位址。 | 改名後再部署，掛的還是原本的儲存卷；畫面顯示實際的 Compose 專案名稱與儲存卷名稱。 |
| PRJ-013 | 使用者可以把變數組匯出成同名的 env 檔。 | 機密預設不含值，勾選含值會記入稽核。 |
| PRJ-014 | 每次部署都重新建置或重新拉取，建置完成後才替換；沒變的服務不替換。這是 HazelminX 規則。 | 有 `build:` 的服務每次都建置，可以選擇不用快取，也可以關掉，改成「沒變時沿用上次的映像」。`image:` 每次重新拉取並記錄 digest。Compose 檔寫了 `pull_policy` 的服務照它的設定。digest 與有效設定都沒變的服務不替換。建置、拉取或預檢失敗時，舊容器沒有被重建或停止。 |
| PRJ-015 | 符合條件的服務無縫替換；不符合的寫出原因與預估中斷時間。 | 依 52.6 的表標出每個服務的替換方式，可以同時有多個標示；單一服務可以改成先停再起。無縫替換時，部署期間持續送出的外部請求沒有失敗。新版不健康時不切換，舊版繼續服務。舊版保留到觀察期結束，期間可以切回。容量不足時拒絕並讓使用者選擇，不會默默改成先停再起。預估中斷秒數依 52.6 的算法。 |

## 52.8 驗收案例

共用前提：shop production 有三個單元。

- **web**：Compose 單元，部署到 tw-01 的 Docker。服務有 shop-api（入口，Port `http` 8080）、shop-db、shop-redis。儲存卷有 `uploads`。
- **worker**：Compose 單元，部署到 jp-02 的 Docker。
- **search**：Helm 單元，部署到 lab-k3s 叢集的 namespace `shop`。入口 Service 的 Port `http` 是 9200。

引用關係：web 與 worker 都引用 search 公開的 `SEARCH_URL`；worker 不引用 web。

| ID | 情境 | 預期 |
|---|---|---|
| T-189 | 在共用前提下按「部署全部」。 | 三個單元都顯示在同一個專案環境。search 先部署，web 與 worker 在 search 完成後同時部署。 |
| T-190 | search 公開 `SEARCH_URL=http://${search.HZX_HOST}:${search.HZX_PORT_HTTP}`，web 設定 `SEARCH_URL=${search.SEARCH_URL}`。 | 預檢把值解析成 `http://search.production.shop.internal:9200`；web 的 shop-api 容器裡實際連得到 search 的 Service。 |
| T-191 | worker 引用 `${search.ADMIN_TOKEN}`，但 search 沒有把它設為公開。 | 預檢擋下並寫出「search 沒有公開 ADMIN_TOKEN」，不會用空字串部署。 |
| T-192 | staging 的單元引用 production 的單元。 | 不能引用，畫面上也選不到。 |
| T-193 | 把 search 單元改名為 search-v2。 | 內部位址不變；web 原本的 `${search.SEARCH_URL}` 照常可以解析；畫面提供「把引用改成新名稱」。 |
| T-194 | 另一組設定：tw-01 與 jp-02 之間沒有私有網路，worker 引用 web 的 shop-api 的 `HZX_PORT_HTTP`（8080）。 | 平台建立加密轉送；從 jp-02 經由轉送通道只能連到 tw-01 的 8080；轉送端沒有另外開放其他 Port。 |
| T-195 | 另一組設定：專案環境再加一個不引用 search 的單元 mailer。search 部署失敗。 | web 與 worker 不部署；mailer 照常部署；結果逐一列出每個單元。改成「任一單元失敗就停止全部」後，mailer 也不部署。 |
| T-196 | 把 search 回滾到一個沒有公開 `SEARCH_URL` 的舊版本，web 仍在引用它。 | 回滾前列出「web 引用的 search.SEARCH_URL 在這個版本不存在」；使用者確認前不回滾。 |
| T-197 | web 的 compose 寫 `env_file: [app.env, ./config/mail.env]`。平台有 `app.env` 組，沒有 `config/mail.env`，repo 裡也沒有。 | 預檢擋下並寫出缺 `config/mail.env`，旁邊有「建立這一組」與「上傳 env 檔」。建立後再部署，shop-api 收到兩組的變數。 |
| T-198 | 在 web 單元上傳 `prod.env`。檔案裡有 `export DB_PASSWORD="a b"`、一個跨行的雙引號值、一行 `=oops`，並且有重複的 `PORT`。 | 組名預設 `prod.env`，建在 web 單元。前兩個正確解析；`=oops` 列出行號與原因，其餘照常匯入；`PORT` 以後出現的為準並標出；`DB_PASSWORD` 預設標成機密並遮罩。 |
| T-199 | 在 web 單元上傳 `app.env`，而 web 單元已經有 `app.env` 組。 | 讓使用者選合併或改名。選合併時，預覽新增、變更、相同，現有但檔案沒有的變數保留。 |
| T-200 | 把 `app.env` 改名為 `web.env`，web 的 compose 在 Git 裡仍寫 `env_file: app.env`。 | 改名前列出這個引用；改名後 `app.env` 保留為別名並標「Git 仍使用舊名稱」；下一次部署照常成功；別名期間不能新建名為 `app.env` 的組。 |
| T-201 | web 的 compose 有 `environment: LOG_LEVEL=warn`，`app.env` 有 `LOG_LEVEL=debug`，映像的 ENV 是 `LOG_LEVEL=info`。 | 「實際收到的值」顯示 `warn`，來源是 compose 的 `environment`，並列出被蓋過的兩個值。 |
| T-202 | repo 根目錄有 `compose.yaml` 與 `compose.override.yaml`，override 把 `./src` 掛進 shop-api；另有一個服務帶 `profiles: [debug]`。單元指定主檔 `compose.yaml`，再勾選 `compose.override.yaml`。 | 兩個檔案依序合併；預檢提示「override 把 repo 目錄 ./src 掛進 shop-api，疑似開發用設定」，但不擋下；debug 服務標「profile 未啟用」，不啟動也不算失敗；啟用 debug 後才啟動。 |
| T-203 | compose 有 `image: shop:${TAG:?必須指定 TAG}`，替換用組沒有 `TAG`。 | 預檢擋下並顯示「必須指定 TAG」；補上後通過。 |
| T-204 | Next.js 單元的 `NEXT_PUBLIC_API_URL` 放在標「建置時」的組，同一組還有一個機密 `SENTRY_AUTH_TOKEN`。 | `NEXT_PUBLIC_API_URL` 以 build arg 提供，產出的檔案含正確網址；`SENTRY_AUTH_TOKEN` 以 `build.secrets` 提供，`docker history` 看不到它；執行時用的組沒有被當成 build args。 |
| T-205 | 把 web 單元改名為 storefront，再部署。 | 掛的仍是原本的 `shop-production-web_uploads` 儲存卷；畫面顯示 Compose 專案名稱仍是 `shop-production-web`。 |
| T-206 | 匯出 shop production 的 `app.env` 組。 | 匯出的檔名是 `app.env`；機密只有名稱沒有值；勾選「含機密值」後才有值，稽核紀錄有這次匯出。 |
| T-207 | 上傳一份 3 MiB 的 env 檔。 | 回報超過上限，不會只匯入前面一部分。 |
| T-208 | 部署 shop-api（2 個副本，經 Zentinel）時，持續每秒送 50 個外部請求。 | 建置期間舊版照常服務；新版健康後才切換；整段期間沒有失敗的請求；舊版排空後保留到觀察期結束，期間可以一鍵切回。 |
| T-209 | 新版的 shop-api 健康檢查一直失敗。 | 不切換流量，舊版繼續服務；部署標為失敗，寫出健康檢查的回應。 |
| T-210 | shop-db 在 compose 第 23 行寫死 `container_name: shop-db`。 | 部署前同時標「先停再起」與「需要批准」。原因指到第 23 行，並說明 PostgreSQL 被判定為單寫入資料；顯示預估中斷秒數，沒有紀錄時寫「首次部署，無法預估」。 |
| T-211 | 建置時 `npm ci` 失敗。 | 舊容器沒有被重建或停止；部署失敗並附上建置日誌。 |
| T-212 | 單元選「指定檔案」，主檔 `deploy/prod.yml`，再依序加入 `deploy/monitoring.yml`；repo 根目錄另有 `compose.yaml` 與 `compose.override.yaml`。 | 只合併 `deploy/prod.yml` 與 `deploy/monitoring.yml`，順序依設定；根目錄的兩個檔不會被讀；專案目錄是 `deploy/`，`.env` 組對應 `deploy/.env`；部署紀錄記下實際使用的檔案清單。 |
| T-213 | 專案層有 `shared.env` 組，內容 `APP_NAME=Shop`；專案環境有 `app.env` 組，`LOG_LEVEL=info`；web 單元的 `app.env` 組有 `LOG_LEVEL=debug`；服務覆寫替 shop-api 加了 `LOG_LEVEL=warn`。shop-api 的 compose 寫 `env_file: [shared.env, app.env]`，worker 寫 `env_file: [app.env]`。接著試著在專案層加入 `DB_PASSWORD`。 | shop-api 實際收到 `APP_NAME=Shop` 與 `LOG_LEVEL=warn`，畫面列出被蓋掉的 debug 與 info 及各自來源。worker 收到 `LOG_LEVEL=info`。`DB_PASSWORD` 預設標成機密，專案層拒收。 |
| T-214 | search（Helm）單元引用專案環境的 `app.env` 組，選擇掛給 `search` Deployment；之後修改 `app.env`。 | 叢集裡出現 ConfigMap `app-env`；`search` Deployment 的容器有對應的 `envFrom`；修改後 Pod 範本的摘要註記改變並觸發滾動更新，新 Pod 收到新值。 |
| T-215 | 某個值是 `pa$$w'rd`，分別放在 `app.env` 組與服務覆寫，主機上同時有 shell 變數 `LOG_LEVEL=trace`。 | 容器裡兩處的值都是原樣的 `pa$$w'rd`；`LOG_LEVEL` 沒有變成 `trace`。 |
| T-216 | 再次部署 web，`postgres:16.4` 的 digest 與 shop-db 的設定都沒變，只有 shop-api 的程式碼變了。 | shop-db、shop-redis 沒有被重建或替換；只有 shop-api 替換；部署紀錄寫出未替換的原因。 |
| T-217 | tw-01 的可用記憶體不夠同時跑兩份 shop-api。 | 部署前拒絕無縫替換並寫出原因；讓使用者選「先停再起（約 N 秒）」或排進維護時段；不會默默改成先停再起。 |
| T-218 | 推送到 search chart 所在的 repo（web 與 worker 用的是另一個 repo）。 | 只有 search 被觸發部署，web 與 worker 不動。 |
| T-219 | search 的 values 依序是 `values-production.yaml`，chart 內附的 `values.yaml` 有 `tolerations: [a, b]`，`values-production.yaml` 有 `tolerations: [c]` 與 `replicas: null`。 | 合併預覽標出 `tolerations` 被整個取代成 `[c]`、`replicas` 被 `null` 刪除，並顯示產生的 manifest 差異。 |
| T-220 | web 的 `.env` 組有 `COMPOSE_PROJECT_NAME=other`，compose 檔頂層有 `name: legacy`。 | 預檢提示「平台設定優先，以下值不採用」並列出兩者；實際使用的專案名稱仍是 `shop-production-web`。 |
| T-276 | repo 根目錄同時有 `docker-compose.yml` 與 `compose.yaml`，正在建立單元。 | 兩份都列出，都沒有預先選；`compose.yaml` 標「Compose 預設會用」；沒選主檔時「儲存」與「部署」停用。 |
| T-277 | repo 根目錄有 `docker-compose.yaml`、`docker-compose.yml` 與 `compose.override.yml`，`apps/api/` 另有 `compose.yml`。 | 四份都列出：根目錄一組排在前面，主檔依序是 `docker-compose.yml`、`docker-compose.yaml`，`docker-compose.yml` 標「Compose 預設會用」；`compose.override.yml` 列在 override，沒有預先勾選；`apps/api/compose.yml` 另成一組。 |
| T-278 | web 單元指定 `docker-compose.yml`。新 commit 9b3e7f1 把它改名成 `compose.yaml`，推到 production 監聽的 `main`。 | 預檢擋下並寫「指定的 docker-compose.yml 在 commit 9b3e7f1 不存在」，推送觸發的部署不執行；畫面列出新 commit 的候選檔，改指定 `compose.yaml` 後才能部署。 |
| T-279 | repo 根目錄沒有 Compose 檔，`apps/web/compose.yaml` 與 `apps/api/docker-compose.yml` 各有一份。 | 匯入頁兩份都列出，依目錄分組，不預先選；可以選一份，或建立成兩個單元。只剩 `apps/web/compose.yaml` 一份時仍然要使用者選，不自動帶入。 |
| T-280 | repo 的 `.env` 有 `COMPOSE_FILE=compose.yaml:compose.prod.yaml`，單元指定的是 `compose.yaml`。 | 部署只用單元指定的檔案；預檢列出「COMPOSE_FILE 不採用」；按「照 COMPOSE_FILE 帶入」後，`compose.yaml`、`compose.prod.yaml` 依序帶入，確認後才儲存。 |
| T-281 | web 單元的主檔是 `deploy/compose.yaml`，再勾選根目錄的 `compose.override.yaml`；override 第 8 行把 `./src` 掛進 shop-api。 | 預檢提示「compose.override.yaml 第 8 行 ./src → deploy/src」，不擋下；合併預覽顯示的掛載來源是 `deploy/src`。 |
| T-282 | `deploy/compose.yaml` 有 `include: [../db/compose.yaml]`，`db/compose.yaml` 定義 `shop-db`；之後新 commit 把 `db/compose.yaml` 刪掉。 | 候選清單裡 `db/compose.yaml` 掛在 `deploy/compose.yaml` 底下，標「由 include 帶入」，勾選框固定為已勾選、不能取消；刪掉後預檢擋下部署並寫出是哪份 include 不見了。 |
| T-283 | n8n-stack 的 production 單元已經用 `compose.yml` 部署在 tw-01；從同一個 repo 建立 staging 單元，也指定 `compose.yml`、也部署到 tw-01。 | 預檢列出「production 的 n8n 單元也用這份主檔」，`container_name: n8n` 與 Port 5678 標擋下並寫出行號；staging 改部署到 lab-mini 時只剩提示。 |

## 52.9 不納入的部分

- Compose 轉 Kubernetes。
- 單元在多台主機之間自動容錯或負載平衡。v1.1 第 1884 行「不宣稱一般 Compose 自動具備跨節點容錯」仍然成立。
- 跨專案環境、跨專案的變數引用。
- 保存上傳的 env 原檔。
- `podman-compose`。
