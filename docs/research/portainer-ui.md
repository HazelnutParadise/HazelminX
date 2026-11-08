# Portainer CE 前端研究（工作狀態摘要）

來源：Portainer develop（2026-11）`app/`，派工 agent（opencode / big-pickle 免費模型）唯讀閱讀原始碼，2026-11-07。這份是執行者在逾時前留下的工作狀態摘要，不是完整的八題回報；行號以它自己標示的為準，主 agent 只核對了 Home 環境卡、Docker dashboard 無圖表、容器列表無內嵌圖形三項。實作前以固定 commit 重新核對。

## Objective
- 以唯讀方式分析 Portainer CE 前端 `app/`，回答 8 個指定問題：路由與頁面、Home 環境卡片、Environment dashboard、Containers/Stats、Stacks、Kubernetes、共用元件、三個優點與三個缺點。
- 最終需以繁體中文 Markdown 回報，每個結論附相對路徑與行號或元件名稱，並包含「## 未驗證項目」與「## 本輪經驗」。

## Important Details
- Repo：`/private/tmp/claude-501/-Volumes-SSD-Developer-hazelminx/e0449c93-a575-4a40-9ad7-b72e256f8907/scratchpad/refs/portainer`
- 嚴格限制：不得修改任何檔案；不得呼叫任何其他 agent 或 CLI；不得 commit／stash／reset。
- 技術架構：AngularJS UI-Router 與 React 混合；React 元件常用 `r2a` 包成 Angular component。
- 路由主要註冊於：
  - `app/portainer/__module.js`
  - `app/docker/__module.js`
  - `app/kubernetes/__module.js`
  - `app/docker/react/views/containers.ts`
  - `app/portainer/rbac/index.js`
- 圖表函式庫：`recharts`，共用圖表元件為 `app/react/components/Charts/StatsLineChart.tsx`。
- Home 環境卡片未顯示 Tags，只顯示群組名稱；Tags 在環境詳細頁。
- Shell 注意：zsh 下未加引號的 `--include=*.tsx` 會噴 `zsh:1: no matches found`；`echo ===` 也可能被 zsh 解讀錯誤。應使用 `--include='*.tsx'` 或加引號。

## Work State
### Completed
- 已盤點主要路由：
  - Home：`portainer.home`，`app/portainer/__module.js:272`，元件 `homeView`；React 入口 `app/react/portainer/HomeView/HomeView.tsx`。
  - Environment dashboard：`docker.dashboard`，`app/docker/__module.js:150`，元件 `dockerDashboardView`；React `app/react/docker/DashboardView/DashboardView.tsx`。
  - Containers：`docker.containers`，`app/docker/react/views/containers.ts:49`；詳情 `:62`；Stats `app/docker/react/views/containers.ts:128`。
  - Images：`app/docker/__module.js:199`，元件 `imagesListView`；React `app/react/docker/images/ListView/ListView.tsx`。
  - Volumes：`app/docker/__module.js:519`，Angular template `app/docker/views/volumes/volumes.html`；React datatable `app/react/docker/volumes/ListView/VolumesDatatable/VolumesDatatable.tsx`，經 `app/docker/react/components/volumes.ts` 包裝。
  - Networks：`app/docker/__module.js:251`，元件 `networksListView`；React `app/react/docker/networks/ListView/NetworksDatatable.tsx`。
  - Stacks：`app/docker/__module.js:406`，template `~@/portainer/views/stacks/stacks.html`；React list `app/react/docker/stacks/ListView/StacksDatatable/StacksDatatable.tsx`。
  - Services：`app/docker/__module.js:356`，Angular template `app/docker/views/services/services.html`；React datatable `app/react/docker/services/ListView/ServicesDatatable/ServicesDatatable.tsx`。
  - Events：`app/docker/__module.js:186`，元件 `eventsListView`；React `app/react/docker/events/ListView.tsx`。
  - Host：`docker.host`，`app/docker/__module.js:163`，元件 `hostView`；Angular `app/docker/views/host/host-view.html` + `app/docker/components/host-overview/host-overview.html`；React `app/react/docker/host/HostDetailsPanel/HostDetailsPanel.tsx`。
  - Nodes：`docker.nodes`，`app/docker/__module.js:288`；node detail `:297` 使用 Angular `nodeDetailsView`，檔案 `app/docker/views/nodes/node-details/node-details-view.js`。
  - Users：`portainer.users`，`app/portainer/__module.js:459`；React `app/react/portainer/users/ListView/ListView.tsx`。
  - RBAC Roles：`portainer.roles`，`app/portainer/rbac/index.js:20`，`url: '/roles'`，Admin-only；Angular template `app/portainer/rbac/views/roles/roles.html`；React datatable `app/react/portainer/users/RolesView/RbacRolesDatatable.tsx`。
  - Settings：`portainer.settings`，`app/portainer/__module.js:403`；React `app/react/portainer/settings/SettingsView/SettingsView.tsx`。
  - Kubernetes：`app/kubernetes/__module.js`，`kubernetes` `:56`、applications `:260`、stacks `:345`、configurations `:367`、cluster `:449`；dashboard React `app/react/kubernetes/dashboard/DashboardView.tsx`。
- 已確認 Home `EnvironmentCard`：
  - `app/react/portainer/HomeView/EnvironmentList/EnvironmentItem/EnvironmentCard.tsx`
  - imports／render：`EnvironmentIcon` `:65`、`EnvironmentStatusBadge` `:77`、`SnapshotBadge` `:78`、`EdgeIndicator` `:83`、`EngineVersion` `:97`、`EnvironmentURL` `:99`、`EnvironmentGroupName` `:102`、`EnvironmentTypeTag` `:103`、`EnvironmentStats` `:108`、`EditButtons` `:115`。
  - `EnvironmentStatusBadge`：`app/react/components/EnvironmentStatusBadge.tsx`；Edge 顯示 Heartbeat/Down，非 Edge 顯示 Up/Down；綠色／紅色 pill + 2x2 圓點，Heartbeat 有 pulse。
  - Docker stats：`EnvironmentStatsDocker.tsx`，`ContainerStats total/running/stopped`、`NodeStats`、`CPUStats`、`MemoryStats`。
  - K8s stats：`EnvironmentStatsKubernetes.tsx`，GPU 數量 > 0 時顯示 `GpuStats`，另有 Node/CPU/Memory。
  - `StatsItem.tsx`：圖示 + 數字 + 大寫標籤；Node/CPU/Memory/GPU 分別使用 lucide `LaptopMinimal`、`Cpu`、`MemoryStick`、`Gpu`。
  - `EnvironmentHeader.tsx` 使用 `StatusSummaryBar`；`app/react/components/StatusSummaryBar/StatusSummaryBar.tsx` 為彩色狀態分段列。
- 已確認 Docker Environment dashboard：
  - `app/react/docker/DashboardView/DashboardView.tsx`
  - 使用 `DashboardGrid`／`DashboardItem`（`:17-18`）。
  - 統計方塊：Stacks `:54`、Services／Swarm 相關 `:64`、Containers `:73`（內含 `ContainerStatus` `:80`）、Images `:83`（內含 `ImagesTotalSize` `:90`）、Volumes `:93`、Networks `:101`、GPU 條件顯示 `:109`。
  - `ContainerStatus.tsx` 使用 `Power` / `Heart` 圖示，綠／紅文字顯示 running、stopped、healthy、unhealthy。
  - `ImagesTotalSize.tsx` 使用 `PieChart` lucide 圖示 + humanize 大小，不是實際圓餅圖。
  - `EnvironmentInfo.tsx` 使用 `Widget` + `DetailsTable`，含 `GpuInfo`、`SnapshotStats`、`DockerInfo`、`TagsInfo`、`ClusterVisualizerLink`。
  - Docker dashboard 本身沒有圖表；圖表只在容器 Stats view。
- 已確認 Kubernetes dashboard／cluster：
  - `app/react/kubernetes/dashboard/DashboardView.tsx`
  - `DashboardItem`：Namespace `:55`、Application `:64`、Service `:73`、Ingress `:82`、ConfigMap `:92`、Secret `:102`、Volume `:118`。
  - `app/react/kubernetes/dashboard/EnvironmentInfo.tsx` 使用 plain HTML `<table>` 顯示 Environment/URL/Tags。
  - Cluster `app/react/kubernetes/cluster/ClusterView/ClusterView.tsx`：`PageHeader "Cluster"` + `ClusterResourceReservation` + `NodesDatatable`。
  - `ClusterResourceReservation.tsx` 使用 `ResourceReservation`；`app/react/kubernetes/components/ResourceReservation.tsx` 使用 `ProgressBar`。
  - `ResourceUsageItem.tsx` 為 label + `ProgressBar` + 百分比註記，單色進度條。
- 已確認 Containers list：
  - `app/react/docker/containers/ListView/ListView.tsx`：PageHeader `Container list`；Host 欄位僅 agent + swarm manager。
  - `ContainersDatatable.tsx`：quick actions 包含 logs、inspect、stats、console、attach。
  - Columns `app/react/docker/containers/ListView/ContainersDatatable/columns/index.tsx`：`name`、`state`、`quickActions`、`stack`、`image`、`created`、`ip`、optional `host`、optional `gpus`、`ports`、ownership。
  - `state.tsx`：狀態是文字 label + `label-success/warning/info/danger`；Exited 會顯示 `- code X`；healthy check 有 tooltip。
  - `ContainerQuickActions.tsx`：lucide `FileText` Logs、`Info` Inspect、`BarChart` Stats、`Terminal` Console、`Paperclip` Attach。
  - 列表沒有 inline CPU／記憶體 sparkline 或圖表。
- 已確認 Container Stats：
  - `app/react/docker/containers/StatsView/StatsView.tsx`
  - `StatsLineChart` import `:9`；`CHART_LIMIT = 600` `:23`；預設 refresh 5 秒 `:80`。
  - 圖表區塊：Memory usage `:242`、CPU usage `:271`、Network usage aggregate `:294`、I/O usage aggregate `:313`；實際 `StatsLineChart` `:244/273/297/315`。
  - `StatsLineChart.tsx` 使用 `recharts`：`Area`、`ComposedChart`、`Line`、`ResponsiveContainer`、`XAxis`、`YAxis`、`Tooltip`、`Legend`；`ComposedChart` `:41`、`Area` `:62`、`Line` `:77`。
  - 圖表為 line/area composed chart，保留 600 點 rolling window。
- 已確認 Stacks：
  - List：`app/react/docker/stacks/ListView/StacksDatatable/StacksDatatable.tsx`；columns `columns/index.ts`：name、type（Compose/Swarm）、image-notification、control、created（含 CreatedBy）、updated（含 UpdatedBy）、ownership。
  - Item：`app/react/docker/stacks/ItemView/ItemView.tsx`、`StackDetails.tsx`、`StackInfoTab/StackInfoTab.tsx`。
  - `StackInfoTab.tsx:97-102` 顯示 `GitConfig` 與 `AutoUpdate`，使用 `GitReferenceCard`。
  - `app/react/portainer/gitops/GitReferenceCard.tsx`：Card 呈現 repo URL、reference、config file、commit；validity icon `CheckCircle/GitMerge/XCircle`；webhook 由 `autoUpdate.Webhook` 組 URL；`AutoUpdateIntervalLineItem` `:195/276` 顯示 polling；另有 divergence alert 與 `GitCommitLink`。
  - `TimeWindowDisplay.tsx` 以橘色 `TextTip` 顯示 change window。
  - 未找到部署歷史 timeline／圖形化 history；目前只有 `DeploymentStatusSection` 與 current deployment info。
- 已確認共用元件：
  - Datatable：`app/react/components/datatables/Datatable.tsx`，基於 `@tanstack/react-table`；另有 `Table.tsx`、`ExpandableDatatable.tsx`、`TableSettingsMenu`、`useTableState`、多個 `extend-options`。
  - Widget：`app/react/components/Widget/Widget.tsx`、`WidgetTitle.tsx`、`WidgetBody.tsx`、`WidgetTabs.tsx`。
  - Badge：`app/react/components/Badge/Badge.tsx`，`cva` variants：success、warn、danger、info、secondary、muted、accent。
  - StatusBadge：`app/react/components/StatusBadge.tsx`；另有 `EnvironmentStatusBadge.tsx`。
  - DashboardItem：`app/react/components/DashboardItem/DashboardItem.tsx`，icon + 數量 + type label + link，含 loading/refreshing 文字。
  - Chart：僅 `app/react/components/Charts/StatsLineChart.tsx` 為主要共用圖表；`ChartsRecharts.stories.tsx` 為 story。
- 可作為「值得借鏡」：
  1. `EnvironmentCard` 單列式環境卡，整合狀態、群組、型別、版本、URL、統計，資訊層級清楚。
  2. `DashboardItem` / `DashboardGrid`  reusable 統計卡；`StatusSummaryBar` 以彩色分段呈現 Up/Down/Outdated 並可點擊篩選。
  3. `GitReferenceCard` 將 Git source、ref、commit、webhook、polling、divergence 集中在同一卡，對 GitOps 管理很實用。
- 可作為「太文字化／資訊密度低」：
  1. Docker/K8s dashboard 幾乎只有數字卡，沒有趨勢圖、sparkline、歷史；圖表只存在獨立 Stats 頁。
  2. Containers list 狀態只有文字 label + 顏色，沒有 icon/圓點/迷你資源圖；需點進 Stats 才看到圖表。
  3. Stacks detail 與 GitReferenceCard 仍是大量 label:value、TextTip、Alert；沒有 deployment history timeline／視覺化 diff。
  4. K8s `EnvironmentInfo.tsx` 使用 plain HTML table；`ResourceReservation` 只有單色進度條，缺乏 threshold 視覺警示。

### Active
- 正在整理最終繁體中文 Markdown 報告；技术盤點已完成，尚未輸出正式答案。

### Blocked
- 尚未取得所有引用行號的精確值；部分結論以元件名稱與檔案結構為依據，最終回報需避免過度精確。
- 未找到 Docker Swarm nodes 的獨立列表路由；目前只確認 `docker.nodes.node` 詳細頁。
- 未找到 Stacks deployment history 的圖形化 timeline；可能不存在或藏在其他元件，需標「未找到／不確定」。
- Kubernetes dashboard 的 exact route line 未完整確認；只知道 `app/kubernetes/__module.js` 與 React dashboard 檔案。

## Next Move
1. 撰寫最終 Markdown：8 個問題各一標題，逐條列答案並附 `app/...` 路徑與行號／元件名稱。
2. 在文末補「## 未驗證項目」與「## 本輪經驗」；不確定處標「不確定」，查不到標「未找到」，不要臆測。
3. 回報前再次確認未修改任何檔案，且沒有使用其他 agent／CLI。

## Relevant Files
- `app/portainer/__module.js`：Portainer 主路由、Home、Users、Settings、GitOps。
- `app/docker/__module.js`：Docker 路由、Dashboard、Images、Volumes、Networks、Stacks、Services、Host、Nodes。
- `app/docker/react/views/containers.ts`：Containers 與 Container Stats 路由。
- `app/kubernetes/__module.js`：Kubernetes 路由。
- `app/portainer/rbac/index.js`：RBAC Roles 路由。
- `app/react/portainer/HomeView/HomeView.tsx`：Home 入口。
- `app/react/portainer/HomeView/EnvironmentList/EnvironmentList.tsx`：環境列表。
- `app/react/portainer/HomeView/EnvironmentList/EnvironmentItem/EnvironmentCard.tsx`：環境卡片。
- `app/react/portainer/HomeView/EnvironmentList/EnvironmentItem/EnvironmentStatsDocker.tsx`：Docker 環境統計。
- `app/react/portainer/HomeView/EnvironmentList/EnvironmentItem/EnvironmentStatsKubernetes.tsx`：K8s 環境統計。
- `app/react/portainer/HomeView/EnvironmentHeader/EnvironmentHeader.tsx`：Home 狀態摘要。
- `app/react/components/StatusSummaryBar/StatusSummaryBar.tsx`：彩色狀態分段列。
- `app/react/components/EnvironmentStatusBadge.tsx`：環境狀態 badge。
- `app/react/components/StatsItem.tsx`：統計 icon + 數字元件。
- `app/react/docker/DashboardView/DashboardView.tsx`：Docker dashboard。
- `app/react/docker/DashboardView/ContainerStatus.tsx`：容器狀態統計。
- `app/react/docker/DashboardView/ImagesTotalSize.tsx`：Image 大小顯示。
- `app/react/kubernetes/dashboard/DashboardView.tsx`：K8s dashboard 統計卡。
- `app/react/kubernetes/cluster/ClusterView/ClusterView.tsx`：K8s cluster 頁。
- `app/react/kubernetes/components/ResourceReservation.tsx`：K8s 資源保留視覺。
- `app/react/kubernetes/components/ResourceUsageItem.tsx`：K8s 進度條列。
- `app/react/docker/containers/ListView/ListView.tsx`：Containers 列表頁。
- `app/react/docker/containers/ListView/ContainersDatatable/ContainersDatatable.tsx`：Containers datatable。
- `app/react/docker/containers/ListView/ContainersDatatable/columns/index.tsx`：Containers 欄位。
- `app/react/docker/containers/ListView/ContainersDatatable/columns/state.tsx`：容器狀態呈現。
- `app/react/docker/containers/components/ContainerQuickActions/ContainerQuickActions.tsx`：容器快速操作。
- `app/react/docker/containers/StatsView/StatsView.tsx`：Container Stats 圖表頁。
- `app/react/components/Charts/StatsLineChart.tsx`：recharts 共用折線／面積圖。
- `app/react/docker/stacks/ListView/StacksDatatable/StacksDatatable.tsx`：Stacks 列表。
- `app/react/docker/stacks/ItemView/StackInfoTab/StackInfoTab.tsx`：Stack 詳細資訊。
- `app/react/portainer/gitops/GitReferenceCard.tsx`：Git、webhook、auto-update 呈現。
- `app/react/portainer/gitops/TimeWindowDisplay.tsx`：GitOps change window。
- `app/react/docker/images/ListView/ListView.tsx`：Images 列表。
- `app/react/docker/volumes/ListView/VolumesDatatable/VolumesDatatable.tsx`：Volumes datatable。
- `app/react/docker/networks/ListView/NetworksDatatable.tsx`：Networks datatable。
- `app/react/docker/services/ListView/ServicesDatatable/ServicesDatatable.tsx`：Services datatable。
- `app/react/docker/events/ListView.tsx`：Events 列表。
- `app/react/docker/events/EventsDatatables.tsx`：Events 欄位。
- `app/docker/views/host/host-view.html`：Host 頁 Angular template。
- `app/docker/components/host-overview/host-overview.html`：Host overview panels。
- `app/react/docker/host/HostDetailsPanel/HostDetailsPanel.tsx`：Host details React panel。
- `app/react/portainer/users/ListView/ListView.tsx`：Users 頁。
- `app/react/portainer/users/RolesView/RbacRolesDatatable.tsx`：RBAC Roles datatable。
- `app/react/portainer/settings/SettingsView/SettingsView.tsx`：Settings 頁。
- `app/react/components/datatables/Datatable.tsx`：共用 datatable。
- `app/react/components/Widget/Widget.tsx`：共用 Widget。
- `app/react/components/Badge/Badge.tsx`：共用 Badge。