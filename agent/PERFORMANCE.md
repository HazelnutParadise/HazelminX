# hazelminx-agent 閒置資源量測

規格 38.1 節「基本 Agent」的目標：在 2 核心以上的 Linux、每 10 秒監控一次、不含子程序的條件下，空閒平均 CPU 低於單核心 2%，常駐記憶體低於 200 MiB。

## 結論

| 對象 | 平均 CPU（單核心 %） | 記憶體峰值 | 是否達標 |
|---|---|---|---|
| `hazelminx-agent run`（票 004 骨架） | 低於 0.004（第 1、3、5 輪都是 0 ticks） | 3.09 MiB | 達標 |
| 監控探針，照搬 Komodo 的程序更新 | 2.45、0.20、0.39、0.09（第 1～4 輪） | 4.29 MiB | 第 1 輪超標 |
| 監控探針，只更新程序的 CPU 與記憶體 | 0.20、0.22、0.09（第 2～4 輪） | 4.30 MiB | 量到的三輪達標 |
| `hazelminx-agent run`，監控迴圈每 10 秒一次（票 020.1，`3971508`） | 0.0100、0.0133（第 6、7 輪） | 4.54 MiB | 達標 |

記憶體峰值取每輪 `VmRSS` 取樣最大值與核心統計的 `VmHWM` 兩者中較大的。CPU 的解析度是 1 tick（1/100 秒），300 秒窗口內 0 ticks 表示低於 0.004%。

- 骨架還沒有監控迴圈，所以 `hazelminx-agent` 本身的數字沒有涵蓋「每 10 秒監控一次」這個條件。這個條件要等監控迴圈加入後，由票 020 的驗收重量判定。38 章全部情境的最終驗收在票 099。
- 為了預估監控迴圈的成本，另外量了一個探針（見「監控探針」）。它和 agent 用相同的 tokio 設定，每 10 秒做一次 Komodo Periphery 的系統指標更新。
- 以非 root 帳號執行時，兩種程序更新的成本差不多：第 2 輪 0.1967% 對 0.2000%，第 4 輪兩者都是 0.0864%。只有第 3 輪（918 個程序）照搬版較高（0.39% 對 0.22%）。推測原因是非 root 讀不到其他帳號程序的環境變數、執行檔路徑等欄位，照搬版實際讀到的資料和精簡版接近，沒有用 root 對照量過。
- 照搬版唯一超標的第 1 輪沒有同時量精簡版，所以這組數據不能證明精簡版在相同情況下會達標。
- 建議票 020 每 10 秒只更新程序的 CPU 與記憶體，完整程序欄位改成有人查看時才讀。依據是第 3 輪的差距，加上以 root 執行時照搬版會多讀其他帳號程序的欄位（推測，未量測）。監控迴圈完成後，依「重新量測」量 `hazelminx-agent` 本身。

### 票 020.1：監控迴圈

2026-11-14 依「重新量測」量了兩輪，受測的是分支 `ticket/020-host-overview-monitoring` 的 `3971508` 以 `cargo build --release --locked -p hazelminx-agent` 建的執行檔（4.99 MB）。

| 輪次 | 窗口內 CPU ticks | 平均 CPU（單核心 %） | RSS 平均／峰值 | 可見程序數 |
|---|---|---|---|---|
| 第 6 輪 | 3 | 0.0100 | 2.95／4.54 MiB | 310 |
| 第 7 輪 | 4 | 0.0133 | 3.16／4.39 MiB | 317 |

- 容器改用本機已有的 `hzx-ledger-test:trixie`（以 `buildpack-deps:trixie` 為底，另裝 systemd 與 Rust），一樣 `--cpuset-cpus=0,1`、`--pid=host`、uid 65534，兩輪依序跑、沒有並行。核心 `7.2.12-orbstack`，aarch64，容器內 `nproc=2`。
- 監控迴圈只在有 Core 網址時啟動，所以加了 `--core-url https://127.0.0.1:9`：Agent 照常每 10 秒收集一次，連不上 Core，樣本留在暫存裡並依退避重連。這比連上 Core 時多了重連的成本，少了送出報告的成本。
- `3971508` 之後的提交（`db0db67`、`10367ba`）只改了報告的送出與重送，收集的部分沒有變。
- 這個容器沒有 `/proc/pressure`，GPU 與 SMART 不在這張票，量到的是不含這些來源的成本。


### 票 030.1：儲存清單與用量量測

2026-11-14 量了兩輪閒置與兩輪用量量測，受測的是分支 `ticket/030-disk-overview` 的 `f02278e`，以 `cargo build --release --locked -p hazelminx-agent --features test-actions` 建置（5.3 MB）。加 `test-actions` 是為了用 `HAZELMINX_TEST_USAGE_FIRST_SECONDS` 控制第一輪量測的時間，這個功能只多一個測試動作，收集的程式沒有不同。

閒置（量測窗口內不跑用量量測，`HAZELMINX_TEST_USAGE_FIRST_SECONDS=100000`；儲存清單照常每 60 秒讀一次，監控照常每 10 秒一次）：

| 輪次 | 窗口內 CPU ticks | 平均 CPU（單核心 %） | RSS 平均／峰值 | 可見程序數 |
|---|---|---|---|---|
| 第 8 輪 | 6 | 0.0200 | 3.33／4.82 MiB | 311 |
| 第 9 輪 | 4 | 0.0133 | 3.77／4.67 MiB | 311 |

- 做法同 020.1：`hzx-ledger-test:trixie`、`--cpuset-cpus=0,1`、`--pid=host`、uid 65534、`--core-url https://127.0.0.1:9`。兩輪依序跑。窗口結束時 1 分鐘平均負載是 0.21 與 0.00，cpu0、cpu1 的 steal 前後都是 0，兩輪都有效。
- 比 020.1 的 0.0100%、0.0133% 多不到 0.01 個百分點，在每輪 3 到 6 個 tick 的量測誤差內。

用量量測（一輪的成本）：容器的 `/` 是 overlay，不在量測範圍內，所以掛一個 volume 到 `/var/lib/docker`，把映像的 `/usr` 複製進去當作受測目錄（29,101 筆），Agent 以 root 執行，在第一輪前後讀它的 CPU ticks：

| 輪次 | 筆數 | 耗時 | 這段時間的 CPU | 峰值記憶體（VmHWM） |
|---|---|---|---|---|
| 1 | 29,100 | 293 ms | 0.13 秒 | 4.91 MiB |
| 2 | 29,100 | 312 ms | 0.16 秒 | 4.90 MiB |

- 每筆約 5 微秒 CPU；耗時裡約 140 ms 是每 2048 筆停 10 毫秒的節流。
- 推算（未實測）：100 萬筆的主機一輪約 5 秒 CPU，每 6 小時一輪，平均約單核心 0.02%；一輪上限 500 萬筆約 25 秒 CPU。量更大的目錄樹時本機 Docker 磁碟用完，沒有量到。
- 執行緒設成 nice 19 與 idle I/O 類別，只在其他工作讓出時才跑，所以上面的耗時在忙碌的主機上會變長，CPU 總量不變。
- 硬連結記錄最多 100 萬個 inode（每個 16 位元組加上雜湊表開銷，最多約數十 MiB），只有在主機真的有那麼多硬連結檔時才會用到，這次沒有量到。

## 量測方法

兩支腳本都在 [`scripts/`](scripts/)：

- [`measure-idle.sh`](scripts/measure-idle.sh) `<pid> 300 10`：讀 `/proc/<pid>/stat` 的 `utime + stime`，取 300 秒窗口前後的差，除以實際經過秒數，100% 是一整個核心。只算該程序（含它的執行緒），不含子程序。每 10 秒讀一次 `VmRSS`，結束時讀 `VmHWM`，峰值取兩者較大的。同時印出核心版本、`nproc` 與可見程序數。受測程序在窗口中結束時回傳非 0。
- [`measure-in-container.sh`](scripts/measure-in-container.sh)：在容器裡先起 300 個閒置的 `sleep`，以 uid 65534 啟動受測程序，等 30 秒後跑 `measure-idle.sh`。窗口前後各記一次 `/proc/loadavg`，以及容器可用核心在 `/proc/stat` 的 CPU 列（第 9 欄是 steal）。最後印出受測程序的日誌。可用環境變數 `MEASURE_WINDOW`、`MEASURE_SETTLE`、`MEASURE_SLEEPS` 改窗口、等待秒數與 `sleep` 數量。CPU 列依 cgroup v2 的 `cpuset.cpus.effective` 取容器可用核心，讀不到時記彙總的 `cpu` 列。

環境：

| 項目 | 值 |
|---|---|
| 受測版本 | 第 1～5 輪的 `hazelminx-agent` 都是 `main` 的 `de30d42` 建置的同一個執行檔 |
| 建置 | `cargo build --release --locked -p hazelminx-agent`，Rust 1.97.0，`release` profile（`lto = "thin"`、`codegen-units = 1`、`strip = true`），執行檔 1.9 MB |
| 主機 | macOS 上的 OrbStack Linux VM，核心 `7.2.12-orbstack`，aarch64，8 個 vCPU、8 GiB |
| 容器 | `buildpack-deps:trixie`（`sha256:4c9dc8ac4ff58f6fe23d2b007fe3bb8e25716f947adae6fb312d6cfbaac2b3c3`），`--cpuset-cpus=0,1`（容器內 `nproc=2`），`--pid=host`。同一輪的容器都綁在 cpu0 與 cpu1 |
| 帳號 | uid／gid 65534（nobody），代替 systemd 單元裡的 `hazelminx-agent` 帳號，兩者都不是 root |
| 程序數 | `--pid=host` 下看到的是 OrbStack Docker 執行環境的程序，平常約 11～17 個。每個容器另起 300 個 `sleep`，同時跑的容器會互相看到對方的 `sleep`，所以可見程序數隨並行容器數變動（311～918） |

`hazelminx-agent` 執行時沒有設定檔，也沒有 `core_url`：

```bash
hazelminx-agent --config /tmp/hx/absent.toml --state-dir /tmp/hx/state --log-format json run
```

### 監控探針

探針不屬於 Agent，只用來預估成本。它模仿 Komodo Periphery `v2.3.3` 的 `bin/periphery/src/stats/mod.rs`，但只做系統指標的部分：

- 每 10 秒（規格的監控頻率，上游預設是 5 秒）呼叫 `refresh_cpu_all`、`refresh_memory`、`Disks::refresh`、`Networks::refresh` 與程序更新（`sysinfo` 0.39.6）。
- 環境變數 `PROBE_MODE` 不是 `lean` 時，用上游的 `ProcessRefreshKind::everything().without_tasks()`（照搬）。是 `lean` 時用 `ProcessRefreshKind::nothing().with_cpu().with_memory()`（精簡）。
- 沒有包含上游的 Docker 狀態輪詢，也沒有包含 `stats/mem.rs` 的 `/proc/meminfo` 與 ZFS ARC 解析。
- 啟動時用 `System::new_all()` 讀一次全部欄位，所以精簡版的記憶體峰值包含這次讀取，不能拿來比較兩種更新方式的記憶體用量。

`Cargo.toml`：

```toml
[package]
name = "stats-probe"
version = "0.0.0"
edition = "2024"
publish = false

[workspace]

[dependencies]
sysinfo = "=0.39.6"
tokio = { version = "=1.54.1", features = ["rt-multi-thread", "macros", "time", "signal"] }

[profile.release]
lto = "thin"
codegen-units = 1
strip = true
```

`src/main.rs`：

```rust
use std::hint::black_box;
use sysinfo::{Disks, Networks, ProcessRefreshKind, ProcessesToUpdate, System};

#[tokio::main]
async fn main() {
    let mut system = System::new_all();
    let mut disks = Disks::new_with_refreshed_list();
    let mut networks = Networks::new_with_refreshed_list();
    let mut tick = tokio::time::interval(std::time::Duration::from_secs(10));
    let kind = if std::env::var("PROBE_MODE").as_deref() == Ok("lean") {
        ProcessRefreshKind::nothing().with_cpu().with_memory()
    } else {
        ProcessRefreshKind::everything().without_tasks()
    };
    let mut term =
        tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate()).unwrap();
    loop {
        tokio::select! {
            _ = tick.tick() => {
                system.refresh_cpu_all();
                system.refresh_memory();
                system.refresh_processes_specifics(ProcessesToUpdate::All, true, kind);
                disks.refresh(true);
                networks.refresh(true);
                let rx: u64 = networks.values().map(|n| n.received()).sum();
                black_box((system.global_cpu_usage(), system.used_memory(),
                    System::load_average().one, disks.list().len(), system.processes().len(), rx));
            }
            _ = term.recv() => break,
        }
    }
}
```

在空目錄放好這兩個檔案後執行 `cargo build --release`，執行檔在 `target/release/stats-probe`。間接相依沒有固定版本。把它和兩支腳本放在同一個目錄後量測：

```bash
docker run --rm --pid=host --cpuset-cpus=0,1 -e PROBE_MODE=lean -v "$PWD":/m:ro buildpack-deps:trixie \
  sh /m/measure-in-container.sh /m/stats-probe
```

照搬版拿掉 `-e PROBE_MODE=lean`。

## 重新量測

### 容器（本次的做法）

在 Linux 上建置，執行檔要和容器同一個 CPU 架構，glibc 版本不能比 `buildpack-deps:trixie` 新：

```bash
cd agent
cargo build --release --locked -p hazelminx-agent
mkdir -p /tmp/hx-measure
cp scripts/measure-idle.sh scripts/measure-in-container.sh \
  "${CARGO_TARGET_DIR:-target}/release/hazelminx-agent" /tmp/hx-measure/
cd /tmp/hx-measure
docker run --rm --pid=host --cpuset-cpus=0,1 -v "$PWD":/m:ro buildpack-deps:trixie \
  sh /m/measure-in-container.sh /m/hazelminx-agent \
  --config /tmp/hx/absent.toml --state-dir /tmp/hx/state --log-format json run
```

`--pid=host` 加上 300 個 `sleep` 是為了讓程序數接近一般伺服器。在本來就有幾百個程序的實機上跑容器時，可以拿掉 `--pid=host`，或用 `-e MEASURE_SLEEPS=0` 不起 `sleep`。

### 實機 systemd

執行檔放到 `/usr/local/bin/hazelminx-agent`，`deploy/sysusers.d/hazelminx.conf` 放到 `/etc/sysusers.d/` 並執行 `systemd-sysusers`，`deploy/hazelminx-agent.service` 放到 `/etc/systemd/system/` 並執行 `systemctl daemon-reload`。之後：

```bash
sudo systemctl start hazelminx-agent
sleep 30
cat /proc/loadavg; grep '^cpu ' /proc/stat
sudo agent/scripts/measure-idle.sh "$(systemctl show -p MainPID --value hazelminx-agent)" 300 10
cat /proc/loadavg; grep '^cpu ' /proc/stat
```

### 判定

- 至少跑兩輪，每輪都記錄受測 commit、`nproc`、可見程序數、窗口前後的 `loadavg` 與 CPU 列。
- 任何一輪超過目標就算不達標。只有下列情況才把該輪當成無效並補跑：受測核心（沒綁核心時看彙總的 `cpu` 列）在窗口內的 steal 差值超過該列各欄差值總和的 5%，或 `/proc/loadavg` 所在的 Linux 主機在窗口結束時的 1 分鐘平均負載超過它的核心數。
- 量測期間不要在同一台主機跑編譯或其他重負載。

## 原始數據

每輪約 300 秒（實際 300～301 秒）、30 個樣本、`CLK_TCK=100`。第 1～3 輪用的是較早版本的 `measure-idle.sh`，它已經印出 `VmHWM`，但判定時沒有用。可見程序數由當時的包裝腳本印出，那時還不記錄 `loadavg` 與 CPU 列。第 4、5 輪用的腳本和目前版本只差在後來加的 `MEASURE_SLEEPS` 等環境變數檢查與 cgroup v1 退路，量測邏輯相同。第 1、3 輪沒有負載與 steal 紀錄，不符合上面的記錄要求。agent 的結論仍然成立，因為 0 ticks 不會因為主機忙碌而被低估，第 5 輪也有完整紀錄。

| 輪次（UTC） | 同時執行 | 可見程序數 | 對象 | CPU ticks | 平均 CPU | RSS 平均／最高 | VmHWM |
|---|---|---|---|---|---|---|---|
| 1（2026-11-07 18:06） | agent、照搬探針 | 613 | agent | 0 | 0.0000% | 2.05／3.09 MiB | 2.91 MiB |
| 1 | | 613 | 照搬探針 | 737 | 2.4485% | 3.00／3.26 MiB | 3.57 MiB |
| 2（18:14） | 兩個探針 | 616 | 照搬探針 | 59 | 0.1967% | 2.92／3.26 MiB | 3.62 MiB |
| 2 | | 616 | 精簡探針 | 60 | 0.2000% | 2.93／3.27 MiB | 3.66 MiB |
| 3（18:26） | agent、兩個探針 | 918 | agent | 0 | 0.0000% | 1.94／3.09 MiB | 2.91 MiB |
| 3 | | 918 | 照搬探針 | 118 | 0.3933% | 3.31／3.64 MiB | 4.21 MiB |
| 3 | | 918 | 精簡探針 | 66 | 0.2200% | 3.35／3.69 MiB | 4.30 MiB |
| 4（2026-11-08 02:35） | 兩個探針 | 617 | 照搬探針 | 26 | 0.0864% | 3.30／3.68 MiB | 4.29 MiB |
| 4 | | 617 | 精簡探針 | 26 | 0.0864% | 3.29／3.67 MiB | 4.29 MiB |
| 5（02:41） | agent | 311 | agent | 0 | 0.0000% | 1.80／3.07 MiB | 2.91 MiB |

第 4、5 輪窗口前後的 1 分鐘 `loadavg` 都在 0.06 以下，cpu0、cpu1 的 steal 欄前後都是 0，macOS 的 1 分鐘平均負載在 2.6～3.6 之間。

agent 的 `VmHWM`（2.91 MiB）比 `VmRSS` 取樣最高值（3.07～3.09 MiB）小，原因沒有確認。推測是核心延後更新 `VmHWM` 的記錄，沒有驗證。

第 1 輪照搬探針的 2.45% 比其他輪高 6～28 倍，原因沒有確認。推測與 Mac 主機負載有關：第 3 輪開始時 macOS 的 15 分鐘平均負載還有 15.55（1 分鐘平均 3.05），第 1 輪在它之前 20 分鐘，負載可能更高。OrbStack VM 在第 4、5 輪回報的 steal 都是 0，所以就算第 1 輪有記錄 steal，也可能無法用它證明 vCPU 被搶走。

## 未驗證

- 沒有在實體機或雲端 VM 上量測，只有 OrbStack VM。也沒有量測 x86_64。
- 沒有以 systemd 單元（`deploy/hazelminx-agent.service`）啟動，單元裡的帳號與限制選項沒有套用。
- 精簡探針沒有在第 1 輪的情況下量過（第 1 輪偏高的原因沒有確認）。
- 探針沒有包含 Docker 狀態輪詢與 `stats/mem.rs` 的解析。
- 探針以非 root 執行，讀不到其他帳號程序的部分欄位。以 root 執行時照搬版會多讀這些欄位，成本可能更高。測試用的 `sleep` 命令列和環境變數都很短，實機上的程序欄位較長，讀取成本可能更高。
- 每輪只有 5 分鐘，沒有觀察記憶體是否會長期增長。
- 骨架沒有連線、監控、工作紀錄，這些功能加入後要重量。
