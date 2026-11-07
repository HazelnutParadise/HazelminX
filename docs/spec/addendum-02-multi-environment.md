# 規格增補 02｜同一台主機上的多個引擎環境

- 狀態：已核定（2026-11-07，使用者決定「補上」）。
- 位置：接在增補 01（49 章）之後，章節編號 50。v1.1 原檔與 `SHA256SUMS.txt` 不動；需求 ID 以 `ENV-` 開頭，驗收案例 `T-149` 起。
- 關係：細化 v1.1 第 9 章「一台主機可以同時裝 Docker、Podman 與 K3s，它們是三個不同的管理目標」、NOD-003、NOD-004、T-051，以及第 38 章資料模型的「執行環境」（同節點可有多個互不相同的環境）。

## 50.1 環境是什麼

「環境」是平台管理容器的最小單位：**節點＋引擎種類＋擁有者（root 或某個 Linux 使用者）＋連接端點（socket 或 API 位址）**。版本是環境的屬性，不是身分的一部分；升級不會變成另一個環境。

同一台主機可能同時有：

| 情況 | 會被辨識成 |
|---|---|
| rootful Docker | 1 個 Docker 環境（擁有者 root） |
| 某使用者的 rootless Docker | 另 1 個 Docker 環境（擁有者該使用者，socket 在 `$XDG_RUNTIME_DIR`） |
| 多個 dockerd（不同 `--data-root`、不同 socket） | 各自 1 個 Docker 環境 |
| rootful Podman | 1 個 Podman 環境 |
| 每個使用者的 rootless Podman | 各自 1 個 Podman 環境 |
| K3s、MicroK8s 等裝在主機上的叢集 | 各自 1 個叢集（Kubernetes 頁），主機同時是它的節點 |
| kind、k3d、minikube 等跑在容器裡的叢集 | 各自 1 個叢集；它們的節點容器同時出現在承載它的 Docker／Podman 環境，標為「由叢集管理」 |

## 50.2 要求

| ID | 誰可以做什麼與必要行為 | 可檢驗的完成條件 |
|---|---|---|
| ENV-001 | 管理員可以看到每個節點上的所有環境，每個環境一列。 | 列出引擎、擁有者、連接端點、版本、狀態；同種引擎多個時不合併、不只顯示第一個。 |
| ENV-002 | 系統辨識環境時以連接端點實際回應的引擎為準。 | 呼叫端點的版本 API 判斷是 Docker 還是 Podman（例如 `podman-docker` 讓 `/var/run/docker.sock` 實際連到 Podman）；不只看路徑或行程名稱。 |
| ENV-003 | 容器、映像、網路、儲存卷頁面以環境為範圍。 | 範圍列可選「全部」、單一節點或單一環境；表格有「環境」欄；同名容器在不同環境不混在一起。 |
| ENV-004 | 由叢集管理的容器不能被當成一般容器操作。 | kind、k3d、minikube 的節點容器與 K8s 管理的容器標「由 <叢集> 管理」，停止、刪除、重建等寫入動作停用並導向 Kubernetes 頁；統計不重複計數。 |
| ENV-005 | 管理員可以連接與管理同一台主機上的多個叢集。 | 每個叢集是獨立的管理目標，有自己的 kubeconfig、權限與版本；Kubernetes 頁的範圍列可切換叢集。 |
| ENV-006 | 安裝或建立環境前，預檢會檢查與既有環境的衝突。 | 檢查 Port（6443、10250、80、443、8472/udp 等）、網段、cgroup 驅動、iptables／nftables 後端、containerd 共用；衝突時擋下並列出佔用者。 |
| ENV-007 | 部署時明確指定目標環境。 | Compose 專案、應用安裝、計畫任務綁定「環境」而不只是節點；節點上同種引擎有多個時不自動猜。 |
| ENV-008 | 對外 Port 的攔截依環境選擇正確位置。 | Docker（iptables）用 `DOCKER-USER`；Docker（nftables 後端）與 rootful Podman（netavark）用平台自己的表或鏈，位置在引擎規則之前，不修改引擎自己的鏈；rootless 引擎的 Port 經使用者空間轉發，用主機防火牆的輸入規則。套用後從外部實測。 |
| ENV-009 | 權限可以授權到單一環境。 | 角色範圍可指定「某節點的某環境」；取得某使用者 rootless 環境的權限不等於取得 root 環境（呼應 SEC 與 IAM 範圍）。 |

## 50.3 驗收案例

| ID | 情境 | 預期 |
|---|---|---|
| T-149 | 同一台有 rootful Docker 與 tim 的 rootless Docker。 | 辨識為兩個 Docker 環境，擁有者與 socket 分開；兩邊同名容器不混淆。 |
| T-150 | 裝了 `podman-docker`，`/var/run/docker.sock` 指向 Podman。 | 辨識為 Podman 環境，不顯示成 Docker。 |
| T-151 | 主機上用 kind 建了叢集 `dev`。 | Kubernetes 頁出現叢集 `dev`；Docker 容器表的 kind 節點容器標「由 dev 叢集管理」且不能停止或刪除；容器總數不重複計。 |
| T-152 | 已有 K3s 的主機要再裝 MicroK8s。 | 預檢擋下 10250 與 80／443 衝突，列出佔用者，不自動停用任一方。 |
| T-153 | 部署 Compose 到有 Docker 與 rootful Podman 的節點但沒選環境。 | 要求選擇環境，不自動挑一個。 |
| T-154 | 對 rootful Podman 的發布 Port 套用「封鎖外部」。 | 規則寫在平台自己的表或鏈；netavark 規則未被修改；外部實測連不到。 |
| T-155 | 只被授權 lab-mini 上 tim 的 rootless Podman。 | 看不到也操作不了同節點的 rootful 環境與其他使用者的環境。 |
| T-156 | 一個 Docker 環境的 socket 消失（服務停了）。 | 只有該環境標為無法連線，同節點其他環境照常。 |

## 50.4 不納入的部分

- Docker Desktop（Linux 版）與 Windows 上的 Docker Desktop：官方不支援伺服器情境，偵測到時只顯示、不接管。
- 同一環境內的多個 Docker context 切換：平台以端點為單位，不管理使用者的 CLI context 設定。
