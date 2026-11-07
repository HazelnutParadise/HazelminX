"""產出 HazelminX 方向 B 的主要畫面原型。

用法：python3 docs/design/tools/build_screens.py [--render-dir DIR]
輸出：docs/design/screens/*.dc.html（Claude Design 畫布格式）
     DIR/<name>-{light,dark}.html（本機瀏覽器可直接開）
"""
from __future__ import annotations

import argparse
import re
import json
import math
import os
import random
import sys

sys.path.insert(0, os.path.dirname(__file__))
import hxui as U  # noqa: E402

R = random.Random(7)
HOURS = [f"{(4 + i) % 24:02d}:00" for i in range(24)]  # 04:00 昨天 → 03:00 今天
OBS = "03:42:06"


def walk(n, base, amp, lo=0, hi=100, spikes=None):
    v, out = base, []
    for i in range(n):
        v = max(lo, min(hi, v + R.uniform(-amp, amp)))
        out.append(v)
    for i, s in (spikes or {}).items():
        out[i] = s
    return out


TOPO_LEGEND = [("ok", "正常"), ("warn", "過期／待處理"), ("dng", "失敗／維護"), ("line:2", "少"), ("line:6", "多")]
RING_LEGEND = [("color:var(--use)", "正常"), ("color:var(--warn)", "≥ 70%"), ("color:var(--danger)", "≥ 90%"), ("color:var(--ok)", "執行中的容器")]
TL_LEGEND = [("color:var(--ok)", "成功"), ("color:var(--danger)", "失敗"), ("color:var(--warn)", "回滾／靜音"), ("color:var(--blue)", "進行中"), ("color:var(--q2)", "無資料")]

# ---------- 示例資料（假設） ----------
CPU_TW = walk(24, 36, 6, 18, 60, {22: 71, 23: 37})
CPU_JP = walk(24, 62, 5, 45, 80)
CPU_LAB = walk(24, 3, 1.5, 1, 9)
REQ_EDGE = [int(x) for x in walk(24, 7600, 1800, 2200, 12500, {22: 11890})]
REQ_ORIGIN = [int(e * R.uniform(0.52, 0.66)) for e in REQ_EDGE]
MEM_USED = walk(24, 9.4, .5, 7, 13)
MEM_CACHE = walk(24, 6.1, .6, 4, 9)
MEM_FREE = [max(0.5, 31.3 - u - c) for u, c in zip(MEM_USED, MEM_CACHE)]
IO_R = walk(24, 18, 9, 2, 60, {22: 142})
IO_W = walk(24, 41, 14, 8, 90, {22: 118})
NET_IN = walk(24, 3.2, 1.1, .4, 9)
NET_OUT = walk(24, 11.8, 3.5, 2, 24, {22: 23.1})
LOAD1 = walk(24, 2.1, .8, .3, 6, {22: 5.9})
LOAD5 = [sum(LOAD1[max(0, i - 2):i + 1]) / len(LOAD1[max(0, i - 2):i + 1]) for i in range(24)]
LOAD15 = [sum(LOAD1[max(0, i - 5):i + 1]) / len(LOAD1[max(0, i - 5):i + 1]) for i in range(24)]
TEMP = walk(24, 48, 2.5, 40, 70, {22: 63})
GPU_UTIL = walk(24, 58, 14, 4, 97, {22: 91, 23: 44})          # NVIDIA L4 · ollama 推論
GPU_VRAM = walk(24, 13.6, 1.1, 9, 21.5)                       # GiB，上限 24
GPU_POWER = [12 + u * 0.58 for u in GPU_UTIL]                 # W，上限 72
GPU_TEMP = [38 + u * 0.3 for u in GPU_UTIL]                   # °C
CONTAINERS = ["shop-api", "shop-worker", "shop-db", "shop-redis", "wiki-app", "wiki-db", "zentinel", "hazelminx-agent"]
HEAT = [walk(12, b, a, 0, 100) for b, a in [(28, 12), (14, 8), (9, 4), (3, 2), (12, 6), (6, 3), (11, 5), (2, 1)]]
HEAT[0][10] = 88


def kv(k, v, cls=""):
    return f'<span class="{cls}" style="display:flex;flex-direction:column;gap:2px"><span class="note">{k}</span><span>{v}</span></span>'


# ---------- 總覽 ----------
def overview() -> str:
    tiles = '<div class="tiles">' + "".join([
        U.stat_tile("主機", "2", "/3 連線中", "jp-02 沒有回應", False, spark=[3] * 20 + [2, 2, 2, 2], note=""),
        U.stat_tile("服務", "10", "2 專案", "1 部署失敗（staging）", False, spark=[9, 9, 9, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10]),
        U.stat_tile("今日請求（Zentinel 來源站）", U.fmt(sum(REQ_ORIGIN)), "", "較昨日 +8.4%", True, spark=REQ_ORIGIN, note="邊緣完整度 99.1%"),
        U.stat_tile("進行中的工作", "3", "", "1 狀態不明", False, spark=[1, 1, 2, 2, 1, 1, 1, 2, 3, 2, 2, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 3, 3, 3]),
    ]) + "</div>"

    fleet = U.ts_card("叢集 CPU · 24 小時", [
        U.Series("hazel-tw-01", CPU_TW, "var(--s1)", kind="line"),
        U.Series("hazel-jp-02", CPU_JP, "var(--s2)", stale_from=23),
        U.Series("lab-mini", CPU_LAB, "var(--s3)"),
    ], HOURS, unit="%", vmax=100, markers=[(22, "a3f9c1e 部署")],
        foot='100% = 該節點所有核心 · jp-02 的 <span class="mono">*</span> 為上次觀測（03:41:18） · <a href="#table">表格檢視</a>')
    traffic = U.ts_card("今日請求 · 每小時", [
        U.Series("Cloudflare 邊緣", REQ_EDGE, "var(--s1)"),
        U.Series("Zentinel 來源站", REQ_ORIGIN, "var(--s2)"),
    ], HOURS, unit="", bar=True, fmt_val=lambda v: f"{v / 1000:.1f}K" if v >= 1000 else f"{v:.0f}",
        markers=[(22, "a3f9c1e 部署")],
        foot='邊緣 = Cloudflare 收到的請求（含快取直接回應與邊緣阻擋） · 來源站 = 真正抵達 Zentinel 的請求 · 兩者是同一批流量的兩個觀測點，不相加 · 邊緣資料完整度 99.1%（0.9% 取樣） · 本機 WAF 只偵測：今日 1,204 次命中未阻擋 · <a href="#analytics">流量分析</a>')
    charts = f'<div class="cg">{fleet}{traffic}</div>'

    fleet_cards = f"""
    <section id="nodes" class="panel" aria-labelledby="h-nodes">
      <div class="ph"><h2 id="h-nodes">主機 · 3 台</h2>{U.legend(RING_LEGEND)}<a class="more" href="Nodes.dc.html">全部主機 →</a></div>
      <div class="fleet">
        {U.node_card("hazel-tw-01", "Ubuntu 24.04 · 控制中心 · Docker · 8 核 · L4 GPU", U.status("ok", "連線中"), 37, (11.2, 31.3), (412, 931), CPU_TW[-12:], "2 秒前", (9, 10))}
        {U.node_card("hazel-jp-02", "Debian 12 · Docker · 4 核 · 資料碟未綁定", U.status("dng", "沒有回應"), 64, (13.9, 15.5), (870, 931), CPU_JP[-12:], "03:41:18", (3, 3), stale=True)}
        {U.node_card("lab-mini", "Debian 13 · Podman rootless · 4 核", U.status("info", "維護中"), 3, (1.1, 7.6), (38, 118), CPU_LAB[-12:], "5 秒前", (0, 2))}
      </div>
      <div class="note" style="padding: 0 14px 10px">* 上次觀測值，節點已過期 · 綠色段＝執行中的容器</div>
    </section>"""

    topo = U.topology([
        ("邊緣", [("cf", "Cloudflare", "155K／日 · 取樣 0.9%", "ok")]),
        ("入口", [("zt", "Zentinel @ tw-01", "WAF 偵測", "ok"), ("zt2", "Zentinel @ jp-02", "狀態過期", "warn")]),
        ("站點", [("shop", "shop.example.tw", "憑證 11 天 · 64%", "warn"), ("wiki", "wiki.example.tw", "正常 · 31%", "ok"), ("stg", "staging.shop", "502 · 部署失敗", "danger")]),
    ], [("cf", "zt", .7, ""), ("cf", "zt2", .3, ""), ("zt", "shop", .64, ""), ("zt2", "wiki", .31, ""), ("zt", "stg", .05, "danger")])
    topo_m = U.flowlist([("Cloudflare", "Zentinel", "155K → 92K/日"), ("Zentinel", "shop.example.tw", "64%"), ("Zentinel", "wiki.example.tw", "31%"), ("Zentinel", "staging.shop", "失敗"),
                         ("shop.example.tw", "shop-api @ tw-01", "4 容器"), ("wiki.example.tw", "wiki-app @ jp-02", "2 容器"), ("staging.shop", "shop-api @ lab-mini", "失敗")])
    topology_panel = f"""
    <section id="topo" class="panel" aria-labelledby="h-topo">
      <div class="ph"><h2 id="h-topo">流量拓撲</h2>{U.legend(TOPO_LEGEND)}<a class="more" href="Websites.dc.html">網站與入口 →</a></div>
      <div style="padding: 10px 14px 6px"><div class="ch-d">{topo}</div><div class="ch-m">{topo_m}</div></div>
    </section>"""

    hostmap = U.hostmap([
        ("hazel-tw-01", [("shop-api", 21, "ok"), ("shop-worker", 9, "ok"), ("shop-db", 3, "ok"), ("shop-redis", 1, "bad"), ("wiki-media", 6, "ok"), ("ollama", 44, "ok"), ("zentinel", 2, "ok"), ("hazelminx-agent", 0, "ok"), ("backup-runner", 12, "ok"), ("mail-relay", 1, "ok")]),
        ("hazel-jp-02", [("wiki-app", 30, "stale"), ("wiki-db", 12, "stale"), ("zentinel", 4, "stale")]),
        ("lab-mini", [("shop-api", 0, "off"), ("shop-db", 0, "off"), ("shop-redis", 0, "off")]),
    ])
    hostmap_panel = f"""
    <section id="hostmap" class="panel" aria-labelledby="h-hm">
      <div class="ph"><h2 id="h-hm">容器地圖 · 16</h2><span class="sub">一格一容器，顏色＝CPU；異常與過期用形狀標</span><a class="more" href="Containers.dc.html">容器 →</a></div>
      {hostmap}
    </section>"""

    storage = f"""
    <section id="storage" class="panel" aria-labelledby="h-sto">
      <div class="ph"><h2 id="h-sto">儲存與備份</h2><span class="sub">段條＝資料碟怎麼被用掉；日曆＝每日備份結果</span><a class="more" href="Storage.dc.html">儲存與備份 →</a></div>
      <div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(min(300px, 100%), 1fr)); gap: 1px; background: var(--line)">
        <div style="display: flex; flex-direction: column; gap: 10px; padding: 12px 14px; background: var(--panel)">
          <div style="display:flex;justify-content:space-between;gap:8px;flex-wrap:wrap"><span class="mono" style="font-weight: 500">hazel-tw-01 /mnt/data</span>{U.status("ok", "已綁定 UUID")}</div>
          {U.tall_bar([(290, "容器資料", "var(--q5)"), (84, "資料庫", "var(--q4)"), (38, "備份暫存", "var(--q3)")], 931)}
          <span class="note">412 / 931 GiB · 30 天 +38 GiB · 預估 410 天後滿</span>
          <div style="display:flex;justify-content:space-between;gap:8px;flex-wrap:wrap"><span class="mono" style="font-weight: 500">hazel-jp-02 /mnt/data</span>{U.status("dng", "未綁定 · 掛錯磁碟不會被擋")}</div>
          {U.tall_bar([(610, "容器資料", "var(--q5)"), (190, "媒體", "var(--q4)"), (70, "備份暫存", "var(--q3)")], 931)}
          <span class="note" style="color: var(--danger)">870 / 931 GiB · 30 天 +121 GiB · 預估 15 天後滿</span>
          <div class="scale"><i style="background:var(--q5)"></i>容器資料 <i style="background:var(--q4)"></i>資料庫／媒體 <i style="background:var(--q3)"></i>備份暫存 <i style="background:var(--q1)"></i>可用</div>
        </div>
        <div style="display: flex; flex-direction: column; gap: 10px; padding: 12px 14px; background: var(--panel)">
          <div style="display:flex;justify-content:space-between;gap:8px;flex-wrap:wrap"><span style="font-weight: 500">每日備份 · wiki-db → S3 tokyo</span><span class="note">近 98 天</span></div>
          {U.calendar("o" * 40 + "f" + "o" * 20 + "r" + "o" * 33 + "e" * 2 + "n" * 2, "8 月 1 日", "今天")}
          <div class="scale"><i style="background:var(--ok)"></i>成功 <i style="background:var(--danger)"></i>失敗（10 月 11 日：S3 逾時） <i style="background:var(--warn)"></i>部分 <i style="background:var(--q1)"></i>未排</div>
          <span class="note">今天的驗證：</span>
          {U.steps([("已產生", "done", "03:00"), ("已傳輸", "done", "S3 tokyo"), ("校驗通過", "done", "sha256"), ("還原測試", "done", "9 天前通過")])}
        </div>
      </div>
    </section>"""

    def pcard(name, env, st, commit, when, node, domain, timeline, note=""):
        return (f'<a class="ncard" href="ProjectDetail.dc.html"><div class="nh"><span class="n">{name}</span><span class="note">{env}</span>{st}</div>'
                f'<div class="ft"><span class="mono">{commit}</span><span>{when}</span><span style="margin-left:auto" class="mono">{node}</span></div>'
                f'{U.state_timeline(timeline)}<div class="ft"><span class="note">{domain}</span><span style="margin-left:auto" class="note">{note}</span></div></a>')
    projects = f"""
    <section id="projects" class="panel" aria-labelledby="h-proj">
      <div class="ph"><h2 id="h-proj">專案 · 3 環境</h2>{U.legend(TL_LEGEND)}<a class="more" href="Projects.dc.html">全部 →</a></div>
      <div class="fleet">
        {pcard("shop", "production", U.status("ok", "成功"), "a3f9c1e", "02:57", "tw-01", "shop.example.tw", "oooooooofoooooooooooooro", "自動部署暫停：#418 待批准")}
        {pcard("wiki", "production", U.status("ok", "成功"), "7d02b4a", "1 天前", "jp-02", "wiki.example.tw", "oooooooooooooooooooooooo")}
        {pcard("shop", "staging", U.status("dng", "失敗"), "c91e0d7", "03:12", "lab-mini", "staging.shop.example.tw", "oooofooooooooofoooooffff", "migration 不相容")}
      </div>
    </section>"""

    main = (U.page_head("總覽", OBS, '<a class="btn pri" href="AddNode.dc.html">新增主機</a><a class="btn" href="GitHubSource.dc.html">部署專案</a>',
                        " · 自動更新每 10 秒") + tiles + fleet_cards + topology_panel + charts + hostmap_panel + projects + storage)
    aside = attention_aside() + jobs_aside()
    return U.shell("overview", main, aside)


def attention_aside() -> str:
    def card(tag, color, title, evidence, pri, sec):
        return ((f'<article class="card"><div style="font-size: 12px; color: {color}; font-weight: 600">{tag}</div>'
                f'<p style="font-size: 14px; font-weight: 500; line-height: 1.45">{title}</p>'
                f'<p class="note" style="color: var(--muted)">{evidence}</p>')
                + (f'<div class="wrap"><a class="btn pri sm" style="color: var(--hazelink)" href="{pri[1]}">{pri[0]}</a>' if isinstance(pri, tuple) else f'<div class="wrap"><button type="button" class="btn pri sm" style="color: var(--hazelink)">{pri}</button>')
                + f'<button type="button" class="btn sm">{sec}</button></div></article>')
    return f"""
    <section id="attention" aria-labelledby="h-att" style="display: flex; flex-direction: column; gap: 10px">
      <h2 id="h-att" style="font-size: 13px; color: var(--muted); letter-spacing: .04em">需要處理 · 5</h2>
      {card("主機沒有回應 · 48 秒", "var(--danger)", "hazel-jp-02 · 網站 wiki.example.tw 仍可連", "失聯前 3 分鐘：tim 在這台套用 openssl 更新（會重啟 sshd）<br>受影響：wiki-app、wiki-db、zentinel", ("處理", "NodeOffline.dc.html"), "靜音 1 小時")}
      {card("待批准 · 變更計畫 #418", "var(--hazel)", "shop production · commit 9b3e7f1 · 規則：新增宿主機掛載", "受影響：shop-api、shop-worker · 自動部署已暫停<br><span class='mono'>compose.yaml:41  - /:/host:ro</span>", ("檢視差異", "DeployCheck.dc.html"), "拒絕")}
      {card("憑證到期 · 11 天", "var(--warn)", "shop.example.tw · 續期失敗 ×3", "到期 11 月 18 日 · 目前憑證有效<br><span class='mono'>acme: dns-01 challenge timeout (cloudflare)</span>", "重試續期", "工作紀錄")}
      {card("對外開著 · 2 個 Port", "var(--danger)", "wiki-db 5432、ollama 11434 從外部連得到", "jp-02 · tw-01 · 沒有防火牆擋<br><span class='mono'>0.0.0.0:5432 → wiki-db</span>", ("處理", "Security.dc.html#exposure"), "靜音 24h")}
      {card("資料碟 ≥ 90% · 93%", "var(--danger)", "hazel-jp-02 /mnt/data · 剩 61 GiB", "裝置身分：未綁定 · 30 天 +121 GiB · 預估 15 天後滿<br><span class='mono'>/dev/sdb1 870G/931G</span>", ("查看儲存", "Storage.dc.html"), "靜音 24h")}
    </section>"""


def jobs_aside() -> str:
    return f"""
    <section id="jobs" aria-labelledby="h-jobs" style="display: flex; flex-direction: column; gap: 10px">
      <h2 id="h-jobs" style="font-size: 13px; color: var(--muted); letter-spacing: .04em">進行中的工作 · 3 <a href="WorkCenter.dc.html" style="color: var(--hazel); font-weight: 400; margin-left: 6px">工作中心 →</a></h2>
      <ol class="list" style="list-style: none; margin: 0; padding: 0">
        <li class="job"><div class="t"><span style="font-weight: 500">部署 shop-api → production</span>{U.status("info", "驗證中")}</div><div class="s">tw-01 · 健康檢查 2／3 · 心跳 2 秒前</div><div class="prog" aria-hidden="true"><i style="width: 66%"></i></div><button type="button" class="btn sm" style="align-self: flex-start">要求取消</button></li>
        <li class="job"><div class="t"><span style="font-weight: 500">備份 shop-db → S3</span>{U.status("info", "執行中")}</div><div class="s">tw-01 · 0.5／1.3 GiB · 此步不能取消（dump 中）</div><div class="prog" aria-hidden="true"><i style="width: 39%"></i></div></li>
        <li class="job"><div class="t"><span style="font-weight: 500">更新套件 openssl</span>{U.status("warn", "狀態不明")}</div><div class="s">jp-02 · 最後心跳 03:41:18 · 重連後對帳，不自動重跑</div></li>
      </ol>
    </section>"""


def range_row(on="24 小時") -> str:
    items = ["即時（5 分）", "1 小時", "12 小時", "24 小時", "7 天", "30 天"]
    btns = "".join(f'<button type="button" class="btn{" on" if i == on else ""}" aria-pressed="{"true" if i == on else "false"}">{i}</button>' for i in items)
    return f'<div class="wrap"><div class="range" role="group" aria-label="時間範圍">{btns}</div><span class="note" style="margin-left:auto">保留 30 天 · 每 10 秒取樣，24 小時以上顯示每小時平均與最高</span></div>'


# ---------- 節點詳情 ----------
def node_tabs(active: str, containers: int = 10) -> str:
    """節點工作區的分頁（學 Cockpit 的單機分頁）：單一主機才有意義的東西都在這裡，側欄不另放。"""
    items = [("概況", "NodeDetail.dc.html"), ("系統", "System.dc.html"), (f"容器 {containers}", "Containers.dc.html"), ("檔案", "Files.dc.html"), ("日誌", "NodeLogs.dc.html"), ("終端機", "NodeTerminal.dc.html")]
    return ('<div class="panel"><div class="tabs" role="tablist">' + "".join(
        f'<a class="tab{" on" if l.split(" ")[0] == active else ""}" role="tab" aria-selected="{"true" if l.split(" ")[0] == active else "false"}" href="{h}">{l}</a>' for l, h in items) + "</div></div>")


def node_head(name: str, st: str, note: str, acts: str) -> str:
    """節點工作區頁首：麵包屑回到主機，名稱、狀態、主要動作。"""
    return f"""
    <div style="display: flex; flex-wrap: wrap; align-items: center; gap: 14px">
      <div class="name">{U.crumbs([("主機", "Nodes.dc.html")])}<h1 class="mono">{name}</h1></div>
      {st}<span class="note">{note}</span>
      <div class="wrap" style="margin-left: auto">{acts}</div>
    </div>"""


def nodes_tabs(active: str) -> str:
    """主機的頁面分頁：機群（每台一列）與服務模組（模組 × 節點）。"""
    items = [("主機 · 3", "Nodes.dc.html"), ("服務模組 · 7", "HostServices.dc.html")]
    return ('<div class="panel"><div class="tabs" role="tablist">' + "".join(
        f'<a class="tab{" on" if l.startswith(active) else ""}" role="tab" aria-selected="{"true" if l.startswith(active) else "false"}" href="{h}">{l}</a>' for l, h in items) + "</div></div>")


def node_detail() -> str:
    head = f"""
    <div style="display: flex; flex-wrap: wrap; align-items: center; gap: 14px">
      <div class="name">{U.crumbs([("主機", "Nodes.dc.html")])}<h1 class="mono">hazel-tw-01</h1></div>
      {U.status("ok", "連線中")}<span class="note">最後聯絡 2 秒前</span>
      <div class="wrap" style="margin-left: auto"><button type="button" class="btn">終端機</button><button type="button" class="btn">進入維護模式</button><button type="button" class="btn">重新啟動 agent</button><button type="button" class="btn pri">部署到此節點</button></div>
    </div>
    <div class="panel"><div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(min(150px, 100%), 1fr)); gap: 10px 16px; padding: 12px 14px; font-size: 13px">
      {kv("作業系統", "Ubuntu 24.04.2 LTS")}{kv("核心", "6.8.0-45-generic")}{kv("CPU", "8 核 · AMD EPYC 7B13")}{kv("記憶體", "31.3 GiB")}{kv("執行階段", "Docker 27.3.1")}{kv("Agent", "hazelminx-agent 0.1.0 · mTLS")}{kv("角色", "控制中心 · 資料節點")}{kv("GPU", "NVIDIA L4 · 24 GiB · 驅動 550.90 · NVML")}
    </div></div>"""
    hw = (f'<section class="panel" aria-labelledby="h-hw"><div class="ph"><h2 id="h-hw">硬體</h2><span class="note">03:42:06</span></div><div class="hwg">'
          + U.hw_group("CPU", [("8 核", 37, "37%", "")], "EPYC 7B13 · 3.5 GHz · 負載 2.1")
          + U.hw_group("記憶體", [("RAM", 11.2 / 31.3 * 100, "11G", "/31G"), ("Swap", 6, "0.2G", "/4G")])
          + U.hw_group("磁碟", [("/mnt/data", 44, "412G", "/931G"), ("/", 31, "29G", "/94G")], "inode 3% · 1%")
          + U.hw_group("GPU 0 · L4", [("核心", 44, "44%", ""), ("顯示記憶體", 14.2 / 24 * 100, "14G", "/24G")], "P0")
          + U.hw_group("網卡", [("eth0", 31, "312 Mb/s", ""), ("eth1", 8, "84 Mb/s", ""), ("docker0", 12, "118 Mb/s", ""), ("wg0", 2, "21 Mb/s", ""), ("ens5f1", 0, "0 Mb/s", "")], "5 個 · 1 Gb/s 為滿格")
          + U.temps([("CPU 封裝", "hwmon", 62, 95), ("CPU 最熱核心 · 5", "hwmon", 68, 95), ("主機板 PCH", "hwmon", 48, 90), ("DIMM A1", "hwmon", 39, 85), ("DIMM B1", "hwmon", 40, 85),
                     ("NVMe nvme0", "smartctl", 41, 85), ("SATA sda", "smartctl", 34, 60), ("GPU 核心", "NVML", 51, 83), ("GPU 熱點", "NVML", 58, 95), ("網卡 eth1 (mlx5)", "hwmon", 55, 105), ("機殼 acpitz", "hwmon", 36, 98)])
          + '</div></section>')
    cpu = U.ts_card("CPU 使用率", [U.Series("平均", CPU_TW, "var(--s1)", kind="area"), U.Series("最高（單核）", [min(100, v * 1.9 + 8) for v in CPU_TW], "var(--s2)")],
                    HOURS, unit="%", vmax=100, markers=[(22, "a3f9c1e 部署")], foot="100% = 8 核全忙 · 部署標記來自工作中心")
    mem = U.ts_card("記憶體", [U.Series("已用", MEM_USED, "var(--s1)"), U.Series("快取", MEM_CACHE, "var(--s2)"), U.Series("可用", MEM_FREE, "var(--s3)")],
                    HOURS, unit=" GiB", stacked=True, vmax=31.3, fmt_val=lambda v: f"{v:.0f}G", foot="堆疊：三段相加 = 31.3 GiB · 快取可回收，不算壓力")
    io = U.ts_card("磁碟 I/O · /mnt/data", [U.Series("讀取", IO_R, "var(--s1)"), U.Series("寫入", IO_W, "var(--s2)")], HOURS, unit=" MB/s",
                   fmt_val=lambda v: f"{v:.0f}", foot="單位 MB/s · 02:00 的尖峰是 wiki-db 備份 dump")
    net = U.ts_card("網路", [U.Series("進站", NET_IN, "var(--s1)"), U.Series("出站", NET_OUT, "var(--s2)")], HOURS, unit=" MB/s",
                    fmt_val=lambda v: f"{v:.1f}", foot="單位 MB/s · 不含容器間流量")
    load = U.ts_card("負載平均", [U.Series("1 分鐘", LOAD1, "var(--s1)"), U.Series("5 分鐘", LOAD5, "var(--s2)"), U.Series("15 分鐘", LOAD15, "var(--s3)")],
                     HOURS, unit="", vmax=8, fmt_val=lambda v: f"{v:.1f}", foot="8 核 · 超過 8 代表有程序在排隊")
    temp = U.ts_card("溫度", [U.Series("CPU 封裝", TEMP, "var(--s1)"), U.Series("GPU", GPU_TEMP, "var(--s2)")], HOURS, unit="°C", vmax=90,
                     fmt_val=lambda v: f"{v:.0f}°", foot="門檻：CPU 85°C、GPU 83°C 告警 · 來源 hwmon k10temp、NVML")
    disks = f"""
    <section class="panel" aria-labelledby="h-disk">
      <div class="ph"><h2 id="h-disk">磁碟使用</h2><span class="sub">路徑保存原值與解析結果</span></div>
      <div class="row hd drow"><span>掛載點</span><span>裝置</span><span class="c5">檔案系統</span><span>使用</span><span class="c4">裝置身分</span></div>
      <div class="row drow"><span class="mono">/</span><span class="mono">nvme0n1p2</span><span class="c5">ext4</span>{U.meter(31, "29 / 94 GiB", warn=80, danger=90)}<span class="c4">{U.status("ok", "已綁定")}</span></div>
      <div class="row drow"><span class="mono">/mnt/data</span><span class="mono">sdb1 → /dev/disk/by-uuid/5a7c…e2</span><span class="c5">xfs</span>{U.meter(44, "412 / 931 GiB", warn=80, danger=90)}<span class="c4">{U.status("ok", "已綁定")}</span></div>
      <div class="row drow"><span class="mono">/boot/efi</span><span class="mono">nvme0n1p1</span><span class="c5">vfat</span>{U.meter(12, "61 / 511 MiB", warn=80, danger=90)}<span class="c4"><span class="note">不適用</span></span></div>
    </section>"""
    hot = f"""
    <section class="panel" aria-labelledby="h-hot">
      <div class="ph"><h2 id="h-hot">最熱容器 · 現在</h2><div class="range" role="group" aria-label="排序依據" style="margin-left:auto"><button type="button" class="btn on" aria-pressed="true">CPU</button><button type="button" class="btn" aria-pressed="false">記憶體</button><button type="button" class="btn" aria-pressed="false">網路</button></div></div>
      {U.ranked([("shop-api", 21.4, "21.4%"), ("shop-worker", 8.9, "8.9%"), ("wiki-app", 3.1, "3.1%"), ("shop-db", 2.6, "2.6%"), ("zentinel", 1.8, "1.8%"), ("hazelminx-agent", 0.4, "0.4%")], vmax=25)}
      <div class="note" style="padding: 0 14px 10px">只列前 6 個，佔節點 CPU 的 38.2% · <a href="Containers.dc.html" style="color: var(--hazel)">全部容器 →</a></div>
    </section>"""
    gpu_util = U.ts_card("GPU 使用率", [U.Series("核心", GPU_UTIL, "var(--s1)", kind="area")], HOURS, unit="%", vmax=100, markers=[(22, "a3f9c1e 部署")],
                         foot="100% = 核心全忙 · 02:00 的高峰是 ollama 批次嵌入")
    gpu_vram = U.ts_card("顯示記憶體", [U.Series("已用", GPU_VRAM, "var(--s1)", kind="area")], HOURS, unit=" GiB", vmax=24,
                         fmt_val=lambda v: f"{v:.0f}G", foot="上限 24 GiB · 模型載入後常駐，不隨使用率下降")
    gpu_power = U.ts_card("功耗", [U.Series("GPU", GPU_POWER, "var(--s1)")], HOURS, unit=" W", vmax=72,
                          fmt_val=lambda v: f"{v:.0f}", foot="上限 72 W 由驅動回報 · 不含主機其他元件")
    gpu_head = f"""
    <section class="panel" aria-labelledby="h-gpu">
      <div class="ph"><h2 id="h-gpu">GPU · NVIDIA L4</h2><span class="sub">24 GiB · 驅動 550.90 · 透過 NVML 每 10 秒取樣</span></div>
      <div class="tiles" style="padding: 12px 14px">
        {U.stat_tile("使用率", "44", "%", "1 分鐘前 91%", None, spark=GPU_UTIL)}
        {U.stat_tile("顯示記憶體", "14.2", "/ 24 GiB", "ollama 佔 13.1 GiB", None, spark=GPU_VRAM)}
        {U.stat_tile("溫度", "51", "°C", "門檻 83°C", None, spark=GPU_TEMP)}
        {U.stat_tile("功耗", "38", "/ 72 W", "上限 72 W", None, spark=GPU_POWER)}
        {U.stat_tile("效能狀態", "P0", "", "最高效能 · 核心 2040 MHz", None)}
      </div>
    </section>"""
    def dl(rows):
        return '<dl class="dl">' + "".join(f"<dt>{k}</dt><dd>{v}</dd>" for k, v in rows) + "</dl>"
    gpu_dev = f"""
    <section class="panel" aria-labelledby="h-gdev">
      <div class="ph"><h2 id="h-gdev">裝置詳細 · GPU 0</h2><span class="note mono">00000000:01:00.0</span><div class="wrap" style="margin-left:auto"><a class="btn sm" href="#gh">歷史</a></div></div>
      <div class="gdev">
        <div><h3>識別</h3>{dl([("型號", "NVIDIA L4"), ("架構", "Ada Lovelace"), ("UUID", '<span class="mono">GPU-3f9a…c21e</span>'), ("驅動", '<span class="mono">550.90.07</span>'), ("CUDA", '<span class="mono">12.4</span>')])}</div>
        <div><h3>狀態</h3>{dl([("效能狀態", U.status("ok", "P0 · 最高效能")), ("持久模式", "開"), ("顯示輸出", "否"), ("運算模式", "Default"), ("MIG", "不支援")])}</div>
        <div><h3>時脈</h3>{dl([("核心", '<span class="mono">2040 / 2040 MHz</span>'), ("顯示記憶體", '<span class="mono">6251 MHz</span>'), ("媒體", '<span class="mono">1650 MHz</span>'), ("降頻原因", U.status("ok", "無"))])}</div>
        <div><h3>PCIe</h3>{dl([("世代", '<span class="mono">Gen 4 / 最高 4</span>'), ("寬度", '<span class="mono">x16 / 最高 x16</span>')])}</div>
        <div><h3>功耗</h3>{dl([("目前", '<span class="mono">38 W</span>'), ("上限", '<span class="mono">72 W</span>'), ("預設上限", '<span class="mono">72 W</span>'), ("最大上限", '<span class="mono">72 W</span>')])}</div>
        <div><h3>溫度與散熱</h3>{dl([("核心", '<span class="mono">51 °C</span>'), ("熱點", '<span class="mono">58 °C</span>'), ("顯示記憶體", '<span class="mono">不提供</span>'), ("風扇", '<span class="mono">不提供（被動散熱）</span>')])}</div>
        <div><h3>顯示記憶體</h3>{dl([("已用", '<span class="mono">14.2 GiB</span>'), ("可用", '<span class="mono">9.3 GiB</span>'), ("保留", '<span class="mono">0.5 GiB</span>'), ("頻寬使用率", '<span class="mono">28%</span>')])}</div>
        <div><h3>引擎使用率</h3>{dl([("編碼", '<span class="mono">0%</span>'), ("解碼", '<span class="mono">6%</span>'), ("JPEG", '<span class="mono">0%</span>'), ("OFA", '<span class="mono">0%</span>')])}</div>
        <div><h3>ECC</h3>{dl([("模式", "開"), ("待生效", "否"), ("可修正錯誤", '<span class="mono">0 · 累計 2</span>'), ("不可修正錯誤", '<span class="mono">0 · 累計 0</span>')])}</div>
      </div>
    </section>"""
    def gp(pid, proc, ctr, kind, vram, sm):
        c = f'<a class="mono" href="Containers.dc.html#c-{ctr}">{ctr}</a>' if ctr else '<span class="note">主機</span>'
        return (f'<div class="row gprow"><span class="mono">{pid}</span><span class="name"><span class="n mono">{proc}</span></span><span>{c}</span><span class="c3">{kind}</span>'
                f'<span>{U.meter(vram / 24 * 100, f"{vram} GiB", aria=f"{proc} 顯示記憶體")}</span><span>{U.meter(sm, f"{sm}%", aria=f"{proc} 核心使用率")}</span></div>')
    gpu_proc = f"""
    <section class="panel" aria-labelledby="h-gproc" data-sort="4:desc">
      <div class="ph"><h2 id="h-gproc">正在使用 GPU · 3 個程序</h2><span class="note">03:42:06</span></div>
      <div class="row hd gprow"><span>PID</span><span>程序</span><span>容器</span><span class="c3">類型</span><span>顯示記憶體</span><span>核心</span></div>
      {gp("41822", "ollama runner", "ollama", "運算", 13.1, 41)}
      {gp("52107", "ffmpeg", "wiki-media", "運算", 0.9, 3)}
      {gp("1180", "Xorg", None, "繪圖", 0.2, 0)}
    </section>"""
    gpu_auth = f"""
    <section class="panel" aria-labelledby="h-gauth">
      <div class="ph"><h2 id="h-gauth">容器 GPU 授權</h2><span class="sub">看得到 GPU 不等於容器能用：主機驅動、容器工具與裝置權限分開檢查</span></div>
      <div class="row hd garow"><span>容器</span><span>授權方式</span><span class="c3">容器工具</span><span>實際可用</span></div>
      <div class="row garow"><span class="mono">ollama</span><span class="mono">device: nvidia.com/gpu=all</span><span class="c3 mono">nvidia-container-toolkit 1.16</span><span>{U.status("ok", "已驗證 · 推論用 GPU")}</span></div>
      <div class="row garow"><span class="mono">wiki-media</span><span class="mono">device: nvidia.com/gpu=0 · 僅 video</span><span class="c3 mono">nvidia-container-toolkit 1.16</span><span>{U.status("ok", "已驗證")}</span></div>
      <div class="row garow"><span class="mono">shop-worker</span><span class="note">未授權</span><span class="c3 note">—</span><span>{U.status("info", "無 GPU 存取")} <span class="note">compose 沒宣告，不會自動給</span></span></div>
      <div class="note" style="padding: 8px 14px 10px">不以 <span class="mono">privileged</span> 當通用解法 · 驅動升級會影響整台主機，從 <a href="WorkCenter.dc.html" style="color: var(--hazel)">工作中心</a> 走批准流程</div>
    </section>"""
    tabs = node_tabs("概況")
    main = (head + tabs + hw + range_row() + f'<div class="cg">{cpu}{mem}{io}{net}{load}{temp}</div><div class="cg">{disks}{hot}</div>'
            + nodes(part="procs") + gpu_head + gpu_dev + gpu_proc + f'<div class="cg">{gpu_util}{gpu_vram}{gpu_power}</div>' + gpu_auth)
    return U.shell("nodes", main)


# ---------- 專案與服務 ----------
def project_list() -> str:
    """專案清單：一列一個環境，同專案的環境排在一起。專案一多（學 Railway／Coolify 的 project → environment），先挑再進去。"""
    head = U.page_head("專案與服務", OBS, '<a class="btn pri" href="GitHubSource.dc.html">新增專案</a>')
    tiles = '<div class="tiles">' + "".join([
        U.stat_tile("專案", "2", "· 3 環境", "正式 2 · 測試 1", None),
        U.stat_tile("今日部署", "7", "", "成功 6 · 失敗 1", None, spark=[0, 1, 0, 2, 1, 0, 0, 1, 0, 1, 0, 1]),
        U.stat_tile("等待批准", "1", "", "shop production · #418", False),
        U.stat_tile("失敗中的環境", "1", "", "shop staging · migration", False),
    ]) + "</div>"
    more = f'<button type="button" class="ib" aria-label="更多動作">{U.icon("more")}</button>'
    def env(proj, env_, tier, st, commit, when, node, domain, tl, acts):
        return (f'<a class="row" href="ProjectDetail.dc.html"><span class="name"><span class="n">{proj}</span><span class="d">{env_}</span></span><span>{tier}</span><span>{st}</span>'
                f'<span class="mono">{commit} <span class="note">{when}</span></span><span class="mono c3">{node}</span><span class="c6">{domain}</span><span class="c5">{U.state_timeline(tl)}</span><span class="wrap" style="justify-content:flex-end;flex-wrap:nowrap">{acts}</span></a>')
    dep = '<button type="button" class="btn sm">部署</button>'
    table = f"""
    <section class="panel" aria-labelledby="h-plist" data-sort="0:asc">
      <div class="ph"><h2 id="h-plist">環境 · 3</h2><div class="wrap" style="margin-left:auto"><label class="inp" style="min-width:220px"><span class="note">搜尋</span><input placeholder="專案、網域、commit" style="background:transparent;border:0;flex:1;min-width:0"></label><span class="chip on">全部 3</span><span class="chip">正式 2</span><span class="chip">測試 1</span><span class="chip">有問題 2</span></div></div>
      <div class="row hd"><span>專案／環境</span><span>等級</span><span>狀態</span><span>最近部署</span><span class="c3">節點</span><span class="c6">入口</span><span class="c5">近 24 次部署</span><span></span></div>
      {env("shop", "production", U.tier("正式"), U.status("warn", "待批准 #418"), "a3f9c1e", "02:57", "tw-01", "shop.example.tw", "oooooooofoooooooooooooro", '<button type="button" class="btn sm">檢視 #418</button>' + more)}
      {env("shop", "staging", U.tier("測試"), U.status("dng", "部署失敗"), "c91e0d7", "03:12", "lab-mini", "staging.shop.example.tw", "oooofooooooooofoooooffff", '<button type="button" class="btn sm">看日誌</button>' + more)}
      {env("wiki", "production", U.tier("正式"), U.status("warn", "節點過期"), "7d02b4a", "1 天前", "jp-02", "wiki.example.tw", "oooooooooooooooooooooooo", dep + more)}
    </section>"""
    return U.shell("projects", head + tiles + table)


def projects() -> str:
    """專案詳細：麵包屑回專案清單，環境用分頁切換（同專案內），不再和其他專案混在同一排分頁。"""
    head = f"""
    <div style="display: flex; flex-wrap: wrap; align-items: center; gap: 14px">
      <div class="name">{U.crumbs([("專案與服務", "Projects.dc.html")])}<h1>shop</h1></div>
      <span class="note mono">github.com/hazelnut/shop</span>
      <div class="wrap" style="margin-left: auto"><button type="button" class="btn pri">部署最新 commit</button><a class="btn" href="ProjectEnv.dc.html">環境變數</a><button type="button" class="btn">設定</button></div>
    </div>"""
    tabs = ('<div class="panel"><div class="tabs" role="tablist"><button type="button" class="tab on" role="tab" aria-selected="true">production</button>'
            '<button type="button" class="tab" role="tab" aria-selected="false">staging</button><button type="button" class="tab" aria-label="新增環境">＋ 環境</button></div>'
            f'<div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(min(170px, 100%), 1fr)); gap: 10px 16px; padding: 12px 14px; font-size: 13px">'
            f'{kv("來源", "github.com/hazelnut/shop · main")}{kv("最近部署", "a3f9c1e · 02:57 · 成功 · Webhook")}{kv("部署單元", "3 個 · 2 台主機 · 1 個叢集")}{kv("web 單元", "hazel-tw-01")}{kv("入口", "shop.example.tw · 憑證 11 天後到期")}{kv("自動部署", "已暫停：變更計畫 #418 待批准")}</div>' + U.res_strip("HazelminX", "正式", "03:42:06", drift="shop-redis 映像 redis:7.2 → redis:7.4（主機上手動改）") + '</div>')
    svc_topo = U.topology([
        ("入口", [("dom", "shop.example.tw", "Zentinel · 憑證 11 天", "warn")]),
        ("服務", [("api", "shop-api", "8080 · 2 副本", "ok"), ("wrk", "shop-worker", "無對外 Port", "ok")]),
        ("資料", [("db", "shop-db", "postgres 16.4 · 1.29G", "ok"), ("rd", "shop-redis", "重啟中 · 第 1 次", "info")]),
        ("儲存", [("vol", "shop-db-data", "/mnt/data · 84G", "ok"), ("bk", "每日備份 → S3", "03:00 · 成功", "ok")]),
    ], [("dom", "api", .9, ""), ("api", "db", .6, ""), ("api", "rd", .4, ""), ("wrk", "db", .3, ""), ("wrk", "rd", .3, ""), ("db", "vol", .6, ""), ("vol", "bk", .3, "dim")])
    svc_topo_m = U.flowlist([("shop.example.tw", "shop-api", "Zentinel"), ("shop-api", "shop-db", "postgres"), ("shop-api", "shop-redis", "重啟中"), ("shop-worker", "shop-db", ""), ("shop-db", "shop-db-data", "84G"), ("shop-db-data", "每日備份 → S3", "03:00")])
    topo_panel = f"""
    <section class="panel" aria-labelledby="h-stopo">
      <div class="ph"><h2 id="h-stopo">web 單元 · 服務拓撲</h2><span class="sub">來自 compose.yaml：入口 → 服務 → 資料 → 儲存與備份；線寬＝呼叫量</span><a class="more" href="ComposeEditor.dc.html">編輯 compose.yaml →</a></div>
      <div style="padding: 10px 14px 6px"><div class="ch-d">{svc_topo}</div><div class="ch-m">{svc_topo_m}</div></div>
    </section>"""
    deploys = f"""
    <section class="panel" aria-labelledby="h-dep">
      <div class="ph"><h2 id="h-dep">部署歷史 · 近 24 次</h2><div class="legend" style="margin-left:0"><span><i class="rect" style="background: var(--ok)"></i>成功</span><span><i class="rect" style="background: var(--danger)"></i>失敗</span><span><i class="rect" style="background: var(--warn)"></i>回滾</span><span><i class="rect" style="background: var(--blue)"></i>進行中</span></div><a class="more" href="WorkCenter.dc.html">工作中心 →</a></div>
      <div style="padding: 12px 14px; display: flex; flex-direction: column; gap: 8px">{U.state_timeline("oooooooofoooooooooooooro" , [f"#{380 + i}" for i in range(24)])}<div class="note" style="display:flex;justify-content:space-between"><span>#380 · 10 月 21 日</span><span>#403 · a3f9c1e · 02:57 今天</span></div></div>
      <div class="row hd dprow"><span>部署</span><span>commit</span><span class="c3">觸發</span><span class="c4">時長</span><span>結果</span></div>
      <div class="row dprow"><span>#403</span><span class="mono">a3f9c1e 修正結帳逾時</span><span class="c3">Webhook</span><span class="mono c4">1 分 48 秒</span><span>{U.status("ok", "成功")} <span class="note">02:57</span></span></div>
      <div class="row dprow"><span>#402</span><span class="mono">88b1d02 升級 redis 7.4</span><span class="c3">手動 · tim</span><span class="mono c4">3 分 12 秒</span><span>{U.status("warn", "回滾")} <a class="btn sm" href="DeployCheck.dc.html">與目前比較</a></span></div>
      <div class="row dprow"><span>#401</span><span class="mono">4e7a9c0 新增 webhook 重試</span><span class="c3">Webhook</span><span class="mono c4">1 分 51 秒</span><span>{U.status("ok", "成功")} <span class="note">昨天 18:02</span></span></div>
    </section>"""

    def svc(name, image, node, port, cpu, mem, rps, err, spark, st):
        return (f'<a class="row srow" href="Containers.dc.html"><span class="name"><span class="n">{name}</span><span class="d">{image}</span></span><span>{st}</span>'
                f'<span class="mono c3">{node}<br><span class="note">{port}</span></span><span class="c4">{U.meter(cpu, f"{cpu:.0f}%")}</span><span class="c4">{U.meter(mem[0] / mem[1] * 100, f"{mem[0]}/{mem[1]}M")}</span>'
                f'<span class="c5">{U.sparkline(spark)}</span><span class="mono">{rps}<br><span class="note" style="color:{"var(--danger)" if err >= 1 else "var(--faint)"}">錯誤 {err:.2f}%</span></span></a>')
    services = f"""
    <section class="panel" aria-labelledby="h-svc">
      <div class="ph"><h2 id="h-svc">web 單元 · 服務 · 4</h2><span class="sub">來自 compose.yaml，顯示實際節點與 Port，不只名稱</span></div>
      <div class="row hd srow"><span>服務</span><span>狀態</span><span class="c3">節點 · Port</span><span class="c4">CPU</span><span class="c4">記憶體</span><span class="c5">請求 1 小時</span><span>請求／秒</span></div>
      {svc("shop-api", "ghcr.io/hazelnut/shop-api:a3f9c1e", "tw-01", "8080 → 內部", 21, (812, 2048), "38.2", 0.14, walk(12, 38, 6, 20, 60), U.status("ok", "健康 2／2"))}
      {svc("shop-worker", "ghcr.io/hazelnut/shop-worker:a3f9c1e", "tw-01", "無對外 Port", 9, (410, 1024), "—", 0.0, walk(12, 5, 2, 1, 10), U.status("ok", "健康"))}
      {svc("shop-db", "postgres:16.4", "tw-01", "5432 → 僅內部", 3, (1290, 4096), "—", 0.0, walk(12, 3, 1, 1, 6), U.status("ok", "健康"))}
      {svc("shop-redis", "redis:7.2", "tw-01", "6379 → 僅內部", 1, (62, 256), "—", 0.0, walk(12, 1, .5, 0, 3), U.status("info", "重啟中 · 第 1 次"))}
    </section>"""
    rq = [U.Series("請求／秒", walk(24, 36, 7, 15, 60, {22: 58}), "var(--s1)", kind="area")]
    er = [U.Series("錯誤率", [round(x, 2) for x in walk(24, .2, .12, 0, 1.2, {22: 1.9, 23: .3})], "var(--danger)", kind="line")]
    lat = [U.Series("p50", walk(24, 84, 12, 40, 160), "var(--s1)"), U.Series("p95", walk(24, 310, 40, 180, 600, {22: 940}), "var(--s2)")]
    rq_card = U.ts_card("shop-api · 請求／秒", rq, HOURS, unit="", fmt_val=lambda v: f"{v:.0f}", markers=[(22, "#403")], foot="來源：Zentinel 存取日誌 · 完整度 99.1%")
    er_card = U.ts_card("shop-api · 錯誤率（5xx）", er, HOURS, unit="%", vmax=2.5, fmt_val=lambda v: f"{v:.1f}%", markers=[(22, "#403")], foot="門檻 1% 告警 · 02:57 部署期間 1.9%，持續 4 分鐘")
    lat_card = U.ts_card("shop-api · 延遲", lat, HOURS, unit=" ms", wide=True, fmt_val=lambda v: f"{v:.0f}", foot="單位 ms · p50 與 p95 同一軸；p99 在表格檢視")
    main = head + tabs + units_panel() + connection_panel() + topo_panel + services + f'<div class="cg">{rq_card}{er_card}</div>{lat_card}' + deploys
    return U.shell("projects", main)


# ---------- 容器 ----------
def containers() -> str:
    head = U.page_head("容器", OBS, '<a class="btn pri" href="ContainerCreate.dc.html">建立容器</a><button type="button" class="btn">拉取映像</button><button type="button" class="btn">清理未使用</button>')
    tiles = '<div class="tiles">' + "".join([
        U.stat_tile("執行中", "14", "/16", "1 重啟中、1 已停止", None),
        U.stat_tile("映像", "23", "", "7 個未使用 · 4.1 GiB 可回收", None),
        U.stat_tile("容器 CPU 合計", "41", "% / 8 核", "tw-01", None, spark=[sum(h[i] for h in HEAT) / 8 for i in range(12)]),
        U.stat_tile("容器記憶體合計", "9.2", "GiB / 31.3", "", None, spark=walk(12, 9, .4, 8, 10)),
    ]) + "</div>"
    filt = ('<div class="wrap">'
            '<label class="inp" style="width: auto; min-width: 200px"><span class="note">狀態</span><select style="background:transparent;border:0;flex:1"><option>全部</option><option>執行中</option></select></label>'
            '<label class="inp" style="flex: 1; min-width: 200px"><span class="note">搜尋</span><input placeholder="名稱、映像、標籤" style="background:transparent;border:0;flex:1;min-width:0"></label></div>')

    def crow(name, img, node, st, cpu, mem, memt, net, restarts, ports, gpu=None, drift=None):
        dr = U.attr("diff", f"與平台紀錄不同：{drift}", "", "warn") if drift else ""
        return (f'<a class="row crow" href="ContainerDetail.dc.html"><span class="name"><span class="n">{name} {dr}</span><span class="d">{img}</span></span><span>{st}</span>'
                f'<span class="c3">{U.attr("nodes", f"主機 systemd · hazel-{node}", node) if img.startswith("原生") else U.attr("docker", f"Docker（root）· hazel-{node}", node)}</span><span class="c4">{U.meter(cpu, f"{cpu:.1f}%")}</span><span class="c4">{U.meter(mem / memt * 100, f"{mem / 1024:.1f} / {memt / 1024:.1f}G")}</span>'
                + (f'<span class="c5">{U.meter(gpu[0], f"{gpu[0]}% · {gpu[1]}G", aria=f"GPU 使用率 {gpu[0]}%，顯示記憶體 {gpu[1]} GiB")}</span>' if gpu else '<span class="c5 note">—</span>')
                + f'<span class="mono c6">{net}</span><span class="mono c5 note">{ports}</span>{U.actions(["log", "term", "restart", "more"])}</a>')
    table = f"""
    <section class="panel" aria-labelledby="h-ct" data-sort="3:desc" data-batch data-pages="1–9 / 17">
      <div class="ph"><h2 id="h-ct">容器 · 16</h2>{U.attr("k8s", "另有 1 個節點容器由 kind 叢集 dev 管理，不計入", "+1")}<span class="sub">依 CPU 排序</span></div>
      <div class="scroll"><div style="min-width: 320px">
      <div class="row hd crow"><span>容器</span><span>狀態</span><span class="c3">環境</span><span class="c4">CPU</span><span class="c4">記憶體</span><span class="c5">GPU</span><span class="c6">網路 I/O（MB/s）</span><span class="c5">Port</span><span></span></div>
      {crow("ollama", "ollama/ollama:0.5.7", "tw-01", U.status("ok", "執行中 · 3 天"), 44.0, 2890, 8192, "0.2 / 0.4", 0, "11434 對外", gpu=(41, 13.1))}
      {crow("shop-api", "ghcr.io/hazelnut/shop-api:a3f9c1e", "tw-01", U.status("ok", "執行中 · 46 分"), 21.4, 812, 2048, "3.1 / 11.8", 0, "8080")}
      {crow("shop-worker", "ghcr.io/hazelnut/shop-worker:a3f9c1e", "tw-01", U.status("ok", "執行中 · 46 分"), 8.9, 410, 1024, "0.4 / 0.2", 0, "—")}
      {crow("wiki-app", "ghcr.io/hazelnut/wiki:7d02b4a", "jp-02", U.status("warn", "過期 · 03:41"), 3.1, 356, 1024, "0.6 / 1.9*", 0, "3000")}
      {crow("shop-db", "postgres:16.4", "tw-01", U.status("ok", "執行中 · 9 天"), 2.6, 1290, 4096, "0.3 / 0.5", 0, "5432 內部")}
      {crow("zentinel", "原生 systemd（非容器）", "tw-01", U.status("ok", "執行中 · 12 天"), 1.8, 94, 512, "11.9 / 12.1", 0, "80, 443")}
      {crow("wiki-db", "postgres:16.4", "jp-02", U.status("warn", "過期 · 03:41"), 0.9, 540, 2048, "0.1 / 0.1*", 0, "5432 內部")}
      {crow("shop-redis", "redis:7.4", "tw-01", U.status("info", "重啟中 · 第 1 次"), 0.8, 62, 256, "—", 1, "6379 內部", drift="映像 redis:7.2 → 7.4（主機上手動改）")}
      <div class="row crow"><span class="name"><span class="n">dev-control-plane</span><span class="d">kindest/node:v1.31.0</span></span><span>{U.status("info", "由 dev 叢集管理")}</span><span class="c3">{U.attr("docker", "Docker（root）· hazel-tw-01", "tw-01")}</span><span class="c4">{U.meter(3.2, "3.2%")}</span><span class="c4">{U.meter(18, "0.7 / 4.0G")}</span><span class="c5 note">—</span><span class="mono c6">0.1 / 0.1</span><span class="mono c5 note">41235 → 6443</span><span class="wrap" style="justify-content:flex-end;flex-wrap:nowrap">{U.attr("lock", "由 kind 叢集 dev 管理，寫入動作停用")}<a class="ib" href="Kubernetes.dc.html" aria-label="到 Kubernetes 頁操作" data-tip="到 Kubernetes 頁操作">{U.icon("k8s")}</a></span></div>
      </div></div>
      <div class="note" style="padding: 8px 14px; border-top: 1px solid var(--line)">* 上次觀測 · 原生 systemd 服務列在同一張表，讓「什麼在哪台、用哪個 Port」一眼看完</div>
    </section>"""
    hours12 = [f"{(16 + i) % 24:02d}" for i in range(12)]
    heat = f"""
    <section class="panel" aria-labelledby="h-heat">
      <div class="ph"><h2 id="h-heat">容器 × 小時 CPU · 近 12 小時</h2><span class="sub">單色深淺：掃出哪個容器、哪個時段熱</span><div class="legend"><span><i class="rect" style="background: var(--q1)"></i>0</span><span><i class="rect" style="background: var(--q3)"></i>50%</span><span><i class="rect" style="background: var(--q5)"></i>100%</span></div></div>
      <div style="padding: 12px 14px">{U.heatgrid(CONTAINERS, hours12, HEAT)}</div>
      <div class="foot"><span>格 = 該小時平均，100% = 一核全忙</span><span>shop-api 02 時 88% · 部署 #403</span><a href="#table">表格檢視</a></div>
    </section>"""
    hot = f"""
    <section class="panel" aria-labelledby="h-hot2">
      <div class="ph"><h2 id="h-hot2">最吃記憶體</h2><span class="sub">全部節點</span></div>
      {U.ranked([("shop-db", 1290, "1.29 GiB"), ("shop-api", 812, "812 MiB"), ("wiki-db", 540, "540 MiB*"), ("shop-worker", 410, "410 MiB"), ("wiki-app", 356, "356 MiB*"), ("zentinel", 94, "94 MiB")], vmax=1400)}
      <div class="note" style="padding: 0 14px 10px">* 上次觀測（jp-02 過期）</div>
    </section>"""
    cmap = U.hostmap([
        ("hazel-tw-01", [("shop-api", 21, "ok"), ("shop-worker", 9, "ok"), ("shop-db", 3, "ok"), ("shop-redis", 1, "bad"), ("wiki-media", 6, "ok"), ("ollama", 44, "ok"), ("zentinel", 2, "ok"), ("hazelminx-agent", 0, "ok"), ("backup-runner", 12, "ok"), ("mail-relay", 1, "ok")]),
        ("hazel-jp-02", [("wiki-app", 30, "stale"), ("wiki-db", 12, "stale"), ("zentinel", 4, "stale")]),
        ("lab-mini", [("shop-api", 0, "off"), ("shop-db", 0, "off"), ("shop-redis", 0, "off")]),
    ])
    cmap_panel = f"""
    <section class="panel" aria-labelledby="h-cmap">
      <div class="ph"><h2 id="h-cmap">容器地圖 · 16</h2><span class="sub">一格一容器，依節點分組；顏色＝CPU，形狀＝異常／過期／已停止。點格子開詳情</span><a class="more" href="#table">表格 →</a></div>
      {cmap}
    </section>"""
    images = f"""
    <section class="panel" aria-labelledby="h-img">
      <div class="ph"><h2 id="h-img">映像 · 23 · tw-01</h2><span class="sub">段條＝使用中／未用／建置快取；拉取、推送、匯入匯出、建置、標籤</span><div class="wrap" style="margin-left:auto"><button type="button" class="btn sm">清理未使用 4.1 GiB</button><a class="more" href="ContainerResources.dc.html">管理 →</a></div></div>
      <div style="padding:12px 14px;display:flex;flex-direction:column;gap:8px">{U.tall_bar([(9.8, "使用中 16", "var(--q5)"), (4.1, "未使用 7", "var(--q3)"), (1.2, "建置快取", "var(--q2)")], 15.1, unit="G")}
      <div class="scale"><i style="background:var(--q5)"></i>使用中 <i style="background:var(--q3)"></i>未使用（可回收） <i style="background:var(--q2)"></i>建置快取</div></div>
      {U.ranked([("ghcr.io/hazelnut/shop-api:a3f9c1e", 812, "812 MiB · 使用中"), ("ollama/ollama:0.5.7", 2900, "2.9 GiB · 使用中"), ("postgres:16.4", 430, "430 MiB · 使用中"), ("ghcr.io/hazelnut/shop-api:88b1d02", 810, "810 MiB · 未使用 · 可回收"), ("node:22（建置）", 1100, "1.1 GiB · 未使用")], vmax=3000)}
    </section>"""
    netvol = f"""
    <section class="panel" aria-labelledby="h-nv">
      <div class="ph"><h2 id="h-nv">網路、儲存卷、登錄站</h2><a class="more" href="ContainerResources.dc.html">管理 →</a></div>
      <div style="padding:12px 14px">{U.matrix(["shop_default（bridge）", "wiki_default（bridge）", "hzx-ollama（bridge）", "shop-db-data（volume）", "wiki-media（volume）", "ghcr.io（登錄站）", "registry.example.tw"], ["狀態", "關聯", "備註"], [
        [("ok", "4 容器"), ("none", "shop-api、worker、db、redis"), ("none", "172.18.0.0/16 · 不對外")], [("warn", "上次觀測"), ("none", "wiki-app、wiki-db"), ("none", "jp-02")], [("ok", "2 容器"), ("none", "ollama、nextcloud"), ("none", "平台元件網路")],
        [("ok", "84 GiB"), ("none", "shop-db"), ("none", "/mnt/data · 綁定 UUID")], [("warn", "190 GiB"), ("none", "wiki-app"), ("none", "jp-02 · 未綁定")], [("ok", "已登入"), ("none", "hazelnut 組織"), ("none", "Token 61 天")], [("ok", "已登入"), ("none", "私有"), ("none", "鏡像 docker.io")],
      ], first_col="minmax(190px,1.4fr)")}</div>
    </section>"""
    compose = f"""
    <section class="panel" aria-labelledby="h-cp">
      <div class="ph"><h2 id="h-cp">Compose 專案 · 3</h2><span class="sub">完整 Compose 檔由專案頁管理；這裡列引擎看到的</span><a class="more" href="Projects.dc.html">專案與服務 →</a></div>
      <div class="row hd srow"><span>專案</span><span>狀態</span><span class="c3">節點 · 服務</span><span class="c4">來源</span><span class="c4">上次部署</span><span class="c5"></span><span></span></div>
      <a class="row srow" href="ProjectDetail.dc.html"><span class="name"><span class="n">shop</span><span class="d">production</span></span><span>{U.status("ok", "4/4 執行中")}</span><span class="c3 mono">tw-01 · 4</span><span class="c4 note">GitHub a3f9c1e</span><span class="mono c4">02:57</span><span class="c5"></span><span></span></a>
      <a class="row srow" href="ProjectDetail.dc.html"><span class="name"><span class="n">wiki</span><span class="d">production</span></span><span>{U.status("warn", "過期")}</span><span class="c3 mono">jp-02 · 2</span><span class="c4 note">GitHub 7d02b4a</span><span class="mono c4">1 天前</span><span class="c5"></span><span></span></a>
      <a class="row srow" href="ProjectDetail.dc.html"><span class="name"><span class="n">shop</span><span class="d">staging</span></span><span>{U.status("dng", "1/4 失敗")}</span><span class="c3 mono">lab-mini · 4</span><span class="c4 note">GitHub c91e0d7</span><span class="mono c4">03:12</span><span class="c5"></span><span></span></a>
    </section>"""
    main = head + U.scope_bar([("全部環境", "16 容器"), ("tw-01 · Docker", "root"), ("jp-02 · Docker", "過期"), ("lab-mini · Podman", "deploy"), ("lab-mini · Podman", "tim")], label="環境") + tiles + cmap_panel + filt + table + f'<div class="cg">{heat}{hot}</div>' + f'<div class="cg">{images}{netvol}</div>' + compose
    return U.shell("docker", main)



def container_detail() -> str:
    """容器詳細：清單 → 單一容器。設定由專案管理時，改動導回專案；這頁負責看與日常操作（日誌、終端機、重啟）。"""
    head = f"""
    <div style="display: flex; flex-wrap: wrap; align-items: center; gap: 14px">
      <div class="name">{U.crumbs([("容器", "Containers.dc.html")])}<h1 class="mono">shop-api-1</h1></div>
      {U.status("ok", "執行中 · 46 分")}{U.attr("docker", "Docker（root）· hazel-tw-01", "tw-01")}<a class="note" href="ProjectDetail.dc.html" style="color:var(--hazel)">專案 shop · production</a>
      <div class="wrap" style="margin-left: auto"><button type="button" class="btn">重啟這個副本</button><button type="button" class="btn">滾動重啟 2 個副本</button><button type="button" class="btn">停止</button><button type="button" class="ib" aria-label="更多動作">{U.icon("more")}</button></div>
    </div>"""
    tabs = ('<div class="panel"><div class="tabs" role="tablist">' + "".join(
        f'<a class="tab{" on" if i == 0 else ""}" role="tab" aria-selected="{"true" if i == 0 else "false"}" href="#{h}">{t}</a>'
        for i, (t, h) in enumerate([("概況", "ov"), ("日誌", "log"), ("終端機", "term"), ("檔案", "fs"), ("原始設定", "inspect")])) + "</div>"
            + U.res_strip("HazelminX（專案 shop）", "正式", "03:42:06") + "</div>")
    info = f"""
    <section class="panel" aria-labelledby="h-cinfo">
      <div class="ph"><h2 id="h-cinfo">設定</h2><a class="more" href="ProjectDetail.dc.html">在專案裡修改 →</a></div>
      <div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(min(200px, 100%), 1fr)); gap: 12px 16px; padding: 12px 14px; font-size: 13px">
        {kv("映像", '<span class="mono">ghcr.io/hazelnut/shop-api:a3f9c1e</span>')}{kv("Digest", '<span class="mono">sha256:4be1…c09d</span>')}{kv("建立", "今天 02:57 · 部署 #403")}
        {kv("重啟政策", "unless-stopped · 已重啟 0 次")}{kv("健康檢查", U.status("ok", "健康 · GET /healthz 每 10 秒"))}{kv("執行身分", '<span class="mono">node（uid 1000）</span>')}
        {kv("指令", '<span class="mono">node dist/server.js</span>')}{kv("資源上限", "CPU 2 核 · 記憶體 2 GiB")}{kv("副本", "2 個 · shop-api-1、shop-api-2")}
      </div>
    </section>"""
    cpu = U.ts_card("CPU", [U.Series("shop-api-1", walk(24, 21, 6, 6, 60, {22: 58}), "var(--s1)", kind="area")], HOURS, unit="%", vmax=100, markers=[(22, "#403")], foot="100% = 一核全忙 · 上限 2 核")
    mem = U.ts_card("記憶體", [U.Series("使用", walk(24, 790, 40, 650, 900), "var(--s2)", kind="area")], HOURS, unit="M", vmax=2048, fmt_val=lambda v: f"{v:.0f}M", foot="上限 2048 MiB")
    def prow(cells):
        return '<div class="row">' + "".join(f"<span>{c}</span>" for c in cells) + "</div>"
    ports = f"""
    <section class="panel" aria-labelledby="h-cport">
      <div class="ph"><h2 id="h-cport">Port 與網路</h2></div>
      <div class="row hd"><span>容器 Port</span><span>對應</span><span>網路</span><span>對外</span></div>
      {prow(['<span class="mono">8080/tcp</span>', '<span class="mono">127.0.0.1:18080</span>', '<span class="mono">shop_default · 172.20.0.4</span>', U.status("ok", "經 Zentinel · shop.example.tw")])}
      {prow(['<span class="mono">9464/tcp</span>', '<span class="note">未發布</span>', '<span class="mono">shop_default</span>', U.status("info", "僅內部 · 指標")])}
    </section>"""
    mounts = f"""
    <section class="panel" aria-labelledby="h-cmnt">
      <div class="ph"><h2 id="h-cmnt">掛載</h2></div>
      <div class="row hd"><span>容器路徑</span><span>來源</span><span>模式</span><span></span></div>
      {prow(['<span class="mono">/app/uploads</span>', '<span class="mono">/mnt/data/apps/shop/uploads</span>', "rw", '<a class="btn sm" href="Files.dc.html">開啟資料夾</a>'])}
      {prow(['<span class="mono">/app/config</span>', '<span class="mono">shop_config（儲存卷）</span>', "ro", '<a class="btn sm" href="Files.dc.html">開啟資料夾</a>'])}
    </section>"""
    envs = f"""
    <section class="panel" aria-labelledby="h-cenv">
      <div class="ph"><h2 id="h-cenv">環境變數 · 6</h2><a class="more" href="ProjectDetail.dc.html">在專案裡修改 →</a></div>
      <div class="row hd"><span>名稱</span><span>值</span><span>來源</span></div>
      {prow(['<span class="mono">NODE_ENV</span>', '<span class="mono">production</span>', "compose.yaml"])}
      {prow(['<span class="mono">PORT</span>', '<span class="mono">8080</span>', "compose.yaml"])}
      {prow(['<span class="mono">DATABASE_URL</span>', '<span class="mono" style="overflow-wrap:anywhere;white-space:normal">postgres://shop@shop-db:5432/shop</span>', ".env"])}
      {prow(['<span class="mono">DB_PASSWORD</span>', U.attr("lock", "存在機密庫，容器啟動時注入", "機密 shop/db-password"), "機密庫"])}
      {prow(['<span class="mono">REDIS_URL</span>', '<span class="mono">redis://shop-redis:6379</span>', ".env"])}
      {prow(['<span class="mono">STRIPE_KEY</span>', U.attr("lock", "存在機密庫，容器啟動時注入", "機密 shop/stripe"), "機密庫"])}
    </section>"""
    logs = f"""
    <section class="panel" aria-labelledby="h-clog">
      <div class="ph"><h2 id="h-clog">最近日誌</h2><div class="wrap" style="margin-left:auto"><span class="chip on">全部</span><span class="chip">stderr 2</span><a class="btn sm" href="NodeLogs.dc.html">完整日誌</a></div></div>
      <div style="padding:12px 14px"><div class="term">03:41:58 <span style="color:var(--ok)">INFO</span>  GET /api/cart 200 42ms
03:41:59 <span style="color:var(--ok)">INFO</span>  POST /api/checkout 201 318ms
03:42:01 <span style="color:var(--warn)">WARN</span>  redis reconnecting (attempt 1)
03:42:02 <span style="color:var(--ok)">INFO</span>  redis connected
03:42:05 <span style="color:var(--ok)">INFO</span>  GET /healthz 200 2ms</div></div>
    </section>"""
    main = head + tabs + info + f'<div class="cg">{cpu}{mem}</div>' + f'<div class="cg">{ports}{mounts}</div>' + f'<div class="cg">{envs}{logs}</div>'
    return U.shell("docker", main)



# ---------- 登入 ----------
def login() -> str:
    """登入：帳號密碼 → 第二步驗證（Passkey 優先、TOTP、復原碼）。錯誤訊息不透露帳號是否存在（IAM-012）；
    忘記密碼沒有網頁重設入口，只說明在主機上離線救援（IAM-008）；外部登入是選配，本機帳號永遠保留（IAM-010）。"""
    def field(label, ph, typ="text", val=""):
        return (f'<label style="display:flex;flex-direction:column;gap:6px;font-size:13px"><span>{label}</span>'
                f'<span class="inp"><input type="{typ}" placeholder="{ph}" value="{val}" style="background:transparent;border:0;flex:1;min-width:0"></span></label>')
    card = "background:var(--panel);border:1px solid var(--line);border-radius:12px;padding:28px;display:flex;flex-direction:column;gap:16px;width:100%;max-width:380px"
    brand = '<div style="display:flex;align-items:center;gap:10px;font-weight:600;font-size:16px"><span class="logo" aria-hidden="true"></span>HazelminX</div>'
    step1 = f"""
      <section style="{card}" aria-labelledby="h-login">
        {brand}
        <h1 id="h-login" style="font-size:20px">登入管理介面</h1>
        <span class="note mono">panel.example.tw</span>
        {field("帳號", "使用者名稱", val="tim")}
        {field("密碼", "密碼", typ="password", val="••••••••••")}
        <button type="button" class="btn pri" style="min-height:40px">繼續</button>
        <div style="display:flex;align-items:center;gap:10px" class="note"><span style="flex:1;border-top:1px solid var(--line)"></span>或<span style="flex:1;border-top:1px solid var(--line)"></span></div>
        <button type="button" class="btn" style="min-height:40px">以公司帳號登入（OIDC）</button>
        <details><summary class="note" style="cursor:pointer">無法登入？</summary>
          <ul class="keep" style="margin:8px 0 0;padding-left:18px;display:flex;flex-direction:column;gap:4px;font-size:13px"><li>忘記二次驗證：第二步選「使用復原碼」</li><li>忘記密碼：請管理員重設，或在主機上執行 <span class="mono">sudo hazelminx recover-login</span></li></ul></details>
      </section>"""
    step2 = f"""
      <section style="{card}" aria-labelledby="h-2fa">
        {brand}
        <h1 id="h-2fa" style="font-size:20px">驗證身分</h1>
        <span class="note">tim · 第二步</span>
        <button type="button" class="btn pri" style="min-height:40px">{U.icon("lock", 14)}使用 Passkey</button>
        <div style="display:flex;align-items:center;gap:10px" class="note"><span style="flex:1;border-top:1px solid var(--line)"></span>或輸入驗證器上的 6 位數<span style="flex:1;border-top:1px solid var(--line)"></span></div>
        <div style="display:grid;grid-template-columns:repeat(6,1fr);gap:8px" role="group" aria-label="6 位數驗證碼">{"".join(f'<input class="inp mono" inputmode="numeric" maxlength="1" aria-label="第 {i + 1} 位" value="{d}" style="text-align:center;font-size:18px;min-height:46px;padding:0">' for i, d in enumerate("4829  "))}</div>
        <div style="display:flex;justify-content:space-between;gap:10px;flex-wrap:wrap"><button type="button" class="btn">使用復原碼</button><button type="button" class="btn">回上一步</button></div>
      </section>"""
    body = (f'<div style="flex:1;display:flex;flex-wrap:wrap;gap:24px;align-items:flex-start;justify-content:center;padding:56px 16px">{step1}{step2}</div>')
    return f'<div class="hb theme-{{{{theme}}}}" style="min-height:100vh;display:flex;flex-direction:column"><main class="main" style="padding:0">{body}</main></div>'


# ---------- 工作中心 ----------
def work_center() -> str:
    head = U.page_head("工作中心", OBS, '<button type="button" class="btn">只看我的</button><button type="button" class="btn">歷史紀錄</button>')
    tiles = '<div class="tiles">' + "".join([
        U.stat_tile("執行中", "3", "", "1 狀態不明", False),
        U.stat_tile("等待批准", "1", "", "變更計畫 #418 · 等了 44 分", None),
        U.stat_tile("24 小時完成", "27", "", "成功 25", True, spark=[1, 0, 2, 1, 1, 3, 0, 1, 2, 1, 1, 0, 1, 2, 1, 0, 1, 3, 2, 1, 0, 1, 2, 1]),
        U.stat_tile("24 小時失敗", "2", "", "staging 部署 ×2", False),
    ]) + "</div>"

    def job(title, node, st, step_items, extra, actions, pulse=False, pct=None):
        prog = ""
        if pct is not None:
            prog = f'<div class="prog" aria-hidden="true"><i style="width: {pct}%"></i></div>'
        elif pulse:
            prog = '<div class="prog pulse" aria-hidden="true"><i></i></div>'
        viz = U.pipeline(step_items) if step_items and len(step_items[0]) == 4 else U.steps(step_items)
        return (f'<article class="job" style="gap: 8px"><div class="t"><span style="font-weight: 500">{title}</span>{st}</div>'
                f'<div class="s">{node}</div>{viz}{prog}<div class="s">{extra}</div><div class="wrap">{actions}</div></article>')
    running = f"""
    <section class="panel" aria-labelledby="h-run">
      <div class="ph"><h2 id="h-run">進行中 · 3</h2><span class="sub">長操作都是工作：關掉瀏覽器也繼續；狀態不明的工作在重連後對帳</span></div>
      <div class="list" style="padding: 12px 14px">
        {job("部署 shop-api → production（#403 之後的 #404 熱修）", "hazel-tw-01 · 由 Webhook 觸發 · 開始 03:40:12 · 心跳 2 秒前", U.status("info", "驗證中"),
             [("拉取 commit", "done", "12 秒", "7c21e0b"), ("預檢", "done", "18 秒", "磁碟、Port、憑證"), ("批准", "done", "自動", "無高風險變更"), ("建置", "done", "1 分 02 秒", "ghcr.io 推送完成"), ("滾動替換", "done", "40 秒", "舊版仍在服務"), ("驗證", "cur", "進行中", "健康檢查 2／3")],
             "健康檢查 <span class='mono'>GET /healthz</span>：2／3 通過 · 失敗時回滾到 a3f9c1e",
             '<button type="button" class="btn sm">要求取消（回滾到 a3f9c1e）</button><button type="button" class="btn sm">看日誌</button>', pct=66)}
        {job("備份 shop-db → S3 tokyo-backups", "hazel-tw-01 · 計畫任務 03:00 · 心跳 1 秒前", U.status("info", "執行中"),
             [("預檢", "done", "空間足夠"), ("dump", "cur", "此步不能取消"), ("傳輸", "todo", "0.5／1.3 GiB 已排程"), ("校驗", "todo", "sha256 + 還原測試")],
             "<span class='mono'>38 MB/s</span> · 剩 1 分 14 秒 · 不可中斷步驟：pg_dump",
             '<button type="button" class="btn sm" disabled aria-disabled="true" style="opacity:.6">此步不能取消</button><button type="button" class="btn sm">看日誌</button>', pct=39)}
        {job("更新套件 openssl（安全性）", "hazel-jp-02 · 由 tim 手動觸發 03:38:50 · 最後心跳 03:41:18", U.status("warn", "狀態不明"),
             [("預檢", "done", "7 秒"), ("批准", "done", "tim"), ("執行", "wait", "apt 進行到一半"), ("驗證", "todo", "")],
             "最後心跳 03:41:18 · 重連後對帳 apt 狀態（政策：不自動重跑）",
             '<button type="button" class="btn sm">查看節點</button><button type="button" class="btn sm">標記為需人工處理</button>', pulse=True)}
      </div>
    </section>"""
    waiting = f"""
    <section class="panel" aria-labelledby="h-wait">
      <div class="ph"><h2 id="h-wait">等待批准 · 1</h2></div>
      <div class="list" style="padding: 12px 14px">
        {job("變更計畫 #418 · shop production · commit 9b3e7f1", "hazel-tw-01 · 由 Webhook 偵測 02:58:30 · 自動部署已暫停", U.status("warn", "等待批准"),
             [("預檢", "done", "發現 1 個高風險變更"), ("批准", "wait", "需要擁有者"), ("執行", "todo", ""), ("驗證", "todo", "")],
             "規則：新增宿主機掛載 · <span class='mono'>compose.yaml:41  - /:/host:ro</span> · 受影響：shop-api、shop-worker · 策略：滾動",
             '<a class="btn pri sm" href="DeployCheck.dc.html" style="color: var(--hazelink)">檢視差異與批准</a><button type="button" class="btn sm">拒絕</button>')}
      </div>
    </section>"""
    rate = U.ts_card("備份傳輸速率 · shop-db", [U.Series("MB/s", walk(24, 36, 6, 0, 60, {0: 0, 1: 12}), "var(--hazel)", kind="area")],
                     [f"{i * 5:02d}s" for i in range(24)], unit=" MB/s", vmax=60, fmt_val=lambda v: f"{v:.0f}", foot="近 2 分鐘，每 5 秒一點 · 總量已知（1.3 GiB）才顯示百分比")
    hist = f"""
    <section class="panel" aria-labelledby="h-hist">
      <div class="ph"><h2 id="h-hist">近 24 小時 · 27 件</h2><a class="more" href="BatchResult.dc.html">批次 #540 部分完成 →</a><div class="legend" style="margin-left:0"><span><i class="rect" style="background: var(--ok)"></i>成功</span><span><i class="rect" style="background: var(--danger)"></i>失敗</span><span><i class="rect" style="background: var(--warn)"></i>回滾</span></div><a class="more" href="#history">全部 →</a></div>
      <div style="padding: 12px 14px; display:flex;flex-direction:column;gap:8px">{U.state_timeline("ooooooooooooofoooooorooooof")}<div class="note" style="display:flex;justify-content:space-between"><span>昨天 03:42</span><span>現在</span></div></div>
      <div class="row hd hrow"><span>工作</span><span>目標</span><span class="c3">時長</span><span>結果</span></div>
      <div class="row hrow"><span>部署 shop → staging #71</span><span class="mono">lab-mini</span><span class="mono c3">2 分 40 秒</span><span>{U.status("dng", "失敗")} <span class="note">migration 不相容</span></span></div>
      <div class="row hrow"><span>部署 shop → production #403</span><span class="mono">tw-01</span><span class="mono c3">1 分 48 秒</span><span>{U.status("ok", "成功")} <span class="note">02:57</span></span></div>
      <div class="row hrow"><span>憑證續期 shop.example.tw</span><span class="mono">zentinel</span><span class="mono c3">31 秒</span><span>{U.status("dng", "失敗")} <span class="note">DNS 驗證逾時</span></span></div>
      <div class="row hrow"><span>部署 shop → production #402</span><span class="mono">tw-01</span><span class="mono c3">3 分 12 秒</span><span>{U.status("warn", "回滾")} <span class="note">健康檢查失敗</span></span></div>
    </section>"""
    main = head + tiles + running + waiting + f'<div class="cg">{rate}{hist}</div>'
    return U.shell("jobs", main)


# ---------- GitHub 來源選擇器 ----------
def github_source() -> str:
    head = U.page_head("從 GitHub 匯入專案", OBS, '<a class="btn" href="Projects.dc.html">取消</a>')
    stepper = U.steps([("連接 GitHub", "done", "GitHub App · hazelnut 組織"), ("選擇倉庫與分支", "cur", ""), ("偵測與設定", "todo", "compose、環境變數"), ("部署檢查", "todo", "預檢後才建立")])

    def repo(name, desc, branch, updated, attrs, cur=False, compose=True):
        det = U.attr("check", "找到 deploy/compose.yaml", "compose", "ok") if compose else U.attr("x", "沒有偵測到 compose 檔", "compose", "dng")
        return (f'<div class="row rrow{" sel" if cur else ""}"><span><input type="radio" name="repo" aria-label="選擇 {name}" {"checked" if cur else ""} style="width:18px;height:18px;accent-color: var(--hazel)"></span>'
                f'<span class="name"><span class="n" data-tip="{desc}">{name}</span></span><span class="ats">{attrs}</span><span>{det}</span>'
                f'<span class="c4">{U.attr("branch", "預設分支", branch)}</span><span class="mono note">{updated}</span></div>')
    left = f"""
    <section class="panel" aria-labelledby="h-repo">
      <div class="ph"><h2 id="h-repo">倉庫 · hazelnut（12）</h2><span class="sub">只列 GitHub App 被授權的倉庫</span><a class="more" href="https://github.com/apps/hazelminx">管理授權 ↗</a></div>
      <div style="padding: 10px 14px"><label class="inp"><span class="note">搜尋</span><input value="shop" style="background:transparent;border:0;flex:1;min-width:0" aria-label="搜尋倉庫"></label></div>
      <div class="row hd rrow"><span></span><span>倉庫</span><span>屬性</span><span>偵測</span><span class="c4">分支</span><span>更新</span></div>
      {repo("hazelnut/report", "報表服務", "main", "12 分鐘前", U.attr("lock", "私有倉庫") + U.attr("box", "Dockerfile · 1 個服務", "1"), cur=True)}
      {repo("hazelnut/shop", "電商主站 · 已用於專案 shop", "main", "2 小時前", U.attr("lock", "私有倉庫") + U.attr("box", "4 個 compose 服務", "4"))}
      {repo("hazelnut/shop-mobile-api", "行動 API", "main", "3 天前", U.attr("lock", "私有倉庫") + U.attr("box", "2 個 compose 服務", "2"))}
      {repo("hazelnut/shop-legacy", "舊版 PHP", "master", "1 年前", U.attr("lock", "私有倉庫") + U.attr("archive", "已封存，唯讀", "", "warn"), compose=False)}
      
    </section>"""
    right = f"""
    <section class="panel" aria-labelledby="h-br">
      <div class="ph"><h2 id="h-br">hazelnut/report</h2><span class="sub">分支、路徑與觸發</span></div>
      <div style="padding: 12px 14px; display: flex; flex-direction: column; gap: 14px">
        <div class="field"><label for="br">分支或標籤</label><div class="inp"><span class="mono">main</span><span class="note" style="margin-left:auto">最新 4d81a2c · 12 分鐘前</span></div><span class="help">之後每次推送到這個分支都會建立變更計畫；高風險變更先暫停等批准。</span></div>
        <div class="field"><label for="path">建置方式</label><div class="inp"><span>Dockerfile（倉庫根目錄）</span>{U.status("ok", "已找到")}</div><span class="help">沒有 compose 檔，當成單一服務部署</span></div></div>
        <div class="field"><label>觸發方式</label><div class="wrap"><label class="btn" style="gap:8px"><input type="radio" name="trig" aria-label="觸發方式" checked style="accent-color: var(--hazel)">Webhook（推送即部署）</label><label class="btn" style="gap:8px"><input type="radio" name="trig" aria-label="觸發方式" style="accent-color: var(--hazel)">定期檢查（每 5 分鐘）</label><label class="btn" style="gap:8px"><input type="radio" name="trig" aria-label="觸發方式" style="accent-color: var(--hazel)">只手動</label></div><span class="help">Webhook 會在 GitHub 上建立；沒有公開入口時自動改用定期檢查。</span></div>
        <div class="field"><label for="env">目標環境與節點</label><div class="wrap"><div class="inp" style="width:auto;min-width:160px"><span>production</span></div><div class="wrap" role="radiogroup" aria-label="目標節點"><button type="button" class="btn on" role="radio" aria-checked="true"><span class="mono">hazel-tw-01</span>{U.attr("storage", "/mnt/data 可用 412 GiB", "412G")}</button><button type="button" class="btn" role="radio" aria-checked="false" aria-disabled="true" style="opacity:.55"><span class="mono">hazel-jp-02</span>{U.attr("attention", "不可選：狀態過期、資料碟 93%", "", "warn")}</button></div></div></div>
      </div>
      <div class="ph" style="border-top: 1px solid var(--line); border-bottom: 0"><div class="wrap" style="margin-left:auto"><a class="btn" href="Projects.dc.html">上一步</a><a class="btn pri" href="ImportConfigure.dc.html">偵測與設定 →</a></div></div>
    </section>"""
    main = head + f'<div class="panel" style="padding: 12px 14px">{stepper}</div><div class="cg">{left}{right}</div>'
    return U.shell("projects", main)


# ---------- 部署檢查 ----------
def deploy_check(confirm: bool = False) -> str:
    head = f"""
    <div style="display: flex; flex-wrap: wrap; align-items: center; gap: 14px">
      <div class="name">{U.crumbs([("工作中心", "WorkCenter.dc.html"), ("等待批准", "WorkCenter.dc.html#approve")])}<h1>變更計畫 #418 · shop production</h1></div>
      {U.status("warn", "等待批准 · 44 分")}
      <div class="wrap" style="margin-left: auto"><button type="button" class="btn">拒絕</button><button type="button" class="btn pri">批准並部署…</button></div>
    </div>"""
    summary = f"""
    <div class="panel"><div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(min(190px, 100%), 1fr)); gap: 10px 16px; padding: 12px 14px; font-size: 13px">
      {kv("變更", "a3f9c1e → 9b3e7f1 · 3 個 commit · 由 ava@hazelnut 推送")}{kv("目標", "hazel-tw-01 · production")}{kv("受影響服務", "shop-api、shop-worker（重建）· shop-db、shop-redis 不動")}{kv("預估中斷", "0 秒（滾動更新）· 失敗自動回滾到 a3f9c1e")}{kv("資料影響", "無 migration · 磁碟區不變")}
    </div></div>"""
    checks = f"""
    <section class="panel" aria-labelledby="h-chk">
      <div class="ph"><h2 id="h-chk">預檢 · 1 高風險、1 警告、6 通過</h2><span class="sub">完成於 02:58:41 · 18 秒</span></div>
      <div class="check"><span class="mk dng" aria-hidden="true"></span><span><strong>高風險：新增 root 檔案系統掛載</strong><br><span class="ev">compose.yaml:41 · shop-worker · volumes: /:/host:ro</span><br><span class="note">影響：容器可讀整台主機的檔案，含其他專案的資料與機密參照。即使唯讀也超出 shop-worker 原本的範圍。</span></span><span class="note">需要擁有者批准</span></div>
      <div class="check"><span class="mk warn" aria-hidden="true"></span><span><strong>警告：映像標籤由 commit 短碼改成 latest</strong><br><span class="ev">compose.yaml:12 · shop-api · image: …/shop-api:latest</span><br><span class="note">規則：映像標籤應為固定 commit 短碼 · 影響：回滾目標不可確定</span></span><span class="note">可批准</span></div>
      <div class="check"><span class="mk ok" aria-hidden="true"></span><span>映像可拉取（ghcr.io 認證有效）<br><span class="ev">shop-api:9b3e7f1 · 212 MB · shop-worker:9b3e7f1 · 198 MB</span></span><span class="note">通過</span></div>
      <div class="check"><span class="mk ok" aria-hidden="true"></span><span>Port 無衝突<br><span class="ev">8080 → 僅內部，由 Zentinel 轉發 443</span></span><span class="note">通過</span></div>
      <div class="check"><span class="mk ok" aria-hidden="true"></span><span>必要環境變數齊全<br><span class="ev">DATABASE_URL、REDIS_URL、STRIPE_KEY（機密參照 sec_01h…）</span></span><span class="note">通過</span></div>
      <div class="check"><span class="mk ok" aria-hidden="true"></span><span>磁碟空間足夠<br><span class="ev">hazel-tw-01 /mnt/data 剩 519 GiB，需要 0.5 GiB</span></span><span class="note">通過</span></div>
      <div class="check"><span class="mk ok" aria-hidden="true"></span><span>健康檢查已定義<br><span class="ev">shop-api GET /healthz · 10 秒 ×3</span></span><span class="note">通過</span></div>
      <div class="check"><span class="mk ok" aria-hidden="true"></span><span>無資料庫 migration<br><span class="ev">未偵測到 migrations/ 變更</span></span><span class="note">通過</span></div>
    </section>"""
    diff = f"""
    <section class="panel" aria-labelledby="h-diff">
      <div class="ph"><h2 id="h-diff">compose.yaml 差異</h2><span class="sub">a3f9c1e → 9b3e7f1 · +4 −1</span><a class="more" href="https://github.com/hazelnut/shop/compare/a3f9c1e...9b3e7f1">GitHub 上檢視 ↗</a></div>
      <div class="diff" style="padding: 8px 0">
        <div><span class="ln">10</span><span class="ln">10</span><span>  shop-api:</span></div>
        <div class="del"><span class="ln">11</span><span class="ln"></span><span>-   image: ghcr.io/hazelnut/shop-api:a3f9c1e</span></div>
        <div class="add"><span class="ln"></span><span class="ln">11</span><span>+   image: ghcr.io/hazelnut/shop-api:latest</span></div>
        <div><span class="ln">12</span><span class="ln">12</span><span>    healthcheck:</span></div>
        <div><span class="ln">…</span><span class="ln">…</span><span></span></div>
        <div><span class="ln">38</span><span class="ln">38</span><span>  shop-worker:</span></div>
        <div><span class="ln">39</span><span class="ln">39</span><span>    image: ghcr.io/hazelnut/shop-worker:9b3e7f1</span></div>
        <div class="add"><span class="ln"></span><span class="ln">40</span><span>+   volumes:</span></div>
        <div class="add hot"><span class="ln"></span><span class="ln">41</span><span>+     - /:/host:ro</span></div>
        <div class="add"><span class="ln"></span><span class="ln">42</span><span>+     - shop-tmp:/tmp</span></div>
      </div>
    </section>"""
    modal = ""
    if confirm:
        modal = f"""
    <div class="scrim" role="dialog" aria-modal="true" aria-labelledby="h-cf">
      <div class="modal">
        <h2 id="h-cf" style="font-size: 16px">批准並部署到 <span class="mono">hazel-tw-01</span> · production？</h2>
        <p style="font-size: 13px; color: var(--muted)">會重建 <span class="mono">shop-api</span>、<span class="mono">shop-worker</span>，滾動更新、預估中斷 0 秒。失敗時自動回滾到 <span class="mono">a3f9c1e</span>。</p>
        <div class="card" style="gap: 4px"><span style="font-size: 12px; color: var(--danger); font-weight: 600">你正在批准 1 個高風險變更</span><span style="font-size: 13px">shop-worker 將可唯讀整台 hazel-tw-01 的檔案系統（<span class="mono">/:/host:ro</span>）。</span></div>
        <div class="field"><label for="cf">輸入環境名稱 <span class="mono">production</span> 以確認</label><div class="inp"><input id="cf" style="background:transparent;border:0;flex:1" placeholder="" aria-describedby="cf-help"></div><span class="help" id="cf-help">會記錄批准者 tim 與時間，並通知 ava@hazelnut。</span></div>
        <div class="wrap" style="justify-content: flex-end"><button type="button" class="btn">取消</button><button type="button" class="btn dng" disabled aria-disabled="true" style="opacity:.6">批准並部署</button></div>
      </div>
    </div>"""
    swap = f"""
    <section class="panel" aria-labelledby="h-swap">
      <div class="ph"><h2 id="h-swap">替換方式</h2><span class="sub">先建置；建置完才動舊容器</span></div>
      <div class="row hd"><span>服務</span><span>部署時</span><span class="c3">原因</span></div>
      <div class="row"><span class="mono">shop-api ×2</span><span>{U.status("ok", "無縫替換")}</span><span class="c3 note">新舊並行 → 健康檢查 → Zentinel 切換 → 排空 30 秒</span></div>
      <div class="row"><span class="mono">shop-worker</span><span>{U.status("ok", "無縫替換")}</span><span class="c3 note">新舊同時消費佇列 · 可接受</span></div>
      <div class="row"><span class="mono">shop-db、shop-redis</span><span>{U.status("info", "不重建")}</span><span class="c3 note">這次沒有變更</span></div>
    </section>"""
    main = head + summary + checks + swap + diff + modal
    return U.shell("jobs", main)


# ---------- 錯誤與空狀態 ----------
def states() -> str:
    head = U.page_head("狀態樣式：載入、空、錯誤、部分結果、離線", OBS, "")
    loading = f"""
    <section class="chart" aria-label="載入中範例"><div class="ph"><h2>載入中</h2><span class="sub">骨架沿用最終版面的形狀，不用轉圈</span></div>
      <div class="body" style="display:flex;flex-direction:column;gap:10px;padding:12px">
        <div style="display:flex;gap:8px"><span class="skel" style="width:120px;height:14px"></span><span class="skel" style="width:60px;height:14px;margin-left:auto"></span></div>
        <div style="display:grid;grid-template-columns:40px 1fr;gap:8px;height:120px"><div style="display:flex;flex-direction:column;justify-content:space-between"><span class="skel" style="width:28px;height:10px"></span><span class="skel" style="width:28px;height:10px"></span><span class="skel" style="width:28px;height:10px"></span></div><div class="skel" style="height:100%"></div></div>
        <div class="row" style="padding:0;min-height:36px;border:0"><span class="skel" style="width:40%;height:12px"></span><span class="skel" style="width:20%;height:12px"></span></div>
      </div></section>"""
    empty = f"""
    <section class="chart" aria-label="空資料範例"><div class="ph"><h2>還沒有節點</h2></div>
      <div class="body" style="display:flex;flex-direction:column;gap:10px;padding:20px 14px;align-items:flex-start">
        <p style="font-size:14px">HazelminX 目前只管理這台控制中心本身。加入第一台節點後，這裡會顯示它的狀態、資源與工作。</p>
        <p class="note">節點需要能主動連到控制中心的 8443 Port；不需要開放任何入站 Port。</p>
        <div class="wrap"><button type="button" class="btn pri">新增主機</button><a class="btn" href="#docs">看安裝步驟</a></div>
      </div></section>"""
    error = f"""
    <section class="chart" aria-label="錯誤範例"><div class="ph"><h2>錯誤</h2></div>
      <div class="body" style="padding:12px 14px"><article class="card" style="border-color: var(--danger)">
        <div style="font-size:12px;color:var(--danger);font-weight:600">部署失敗 · shop staging #71</div>
        <p style="font-size:14px;font-weight:500">步驟 migrate 結束碼 1 · lab-mini · 03:12:40</p>
        <p class="note" style="color:var(--muted)">服務中版本：c0ffee1（自動回滾）· 影響範圍：staging.shop.example.tw</p>
        <p class="note"><span class="mono">migrate: relation "orders_v2" already exists (0004_orders.sql:12)</span></p>
        <div class="wrap"><button type="button" class="btn pri sm" style="color:var(--hazelink)">看完整日誌</button><button type="button" class="btn sm">用 c0ffee1 重跑</button><button type="button" class="btn sm">複製診斷包</button></div>
      </article></div></section>"""
    partial = f"""
    <section class="chart" aria-label="部分結果範例"><div class="ph"><h2>部分結果</h2><span class="sub">能拿到的先顯示，缺的說清楚</span></div>
      <div class="body" style="padding:0">
        <div class="banner" style="border-radius:0;border-left:0;border-right:0;border-top:0">{U.status("warn", "1／3 節點過期")}<span>hazel-jp-02 最後回報 03:41:18，以下數字不含它的最新值，標 <span class="mono">*</span> 者為上次觀測。</span><button type="button" class="btn sm" style="margin-left:auto">重新整理</button></div>
        <div class="tiles" style="padding:12px 14px">{U.stat_tile("叢集 CPU", "32", "%*", "2 節點即時 + 1 上次觀測", None)}{U.stat_tile("資料完整度", "92.3", "%", "流量分析取樣 7.7%", False)}</div>
      </div></section>"""
    offline = f"""
    <section class="chart" aria-label="離線範例"><div class="ph"><h2>與控制中心失去連線</h2><span class="sub">保留上一次畫面並降低不透明度，不閃骨架</span></div>
      <div class="body" style="padding:0">
        <div class="banner" style="border-radius:0;border-left:0;border-right:0;border-top:0">{U.status("off", "離線 · 12 秒")}<span>最後更新 03:41:54 · 工作在 Core 上繼續執行</span><button type="button" class="btn sm" style="margin-left:auto">重試連線</button></div>
        <div class="dim" style="padding:12px 14px" aria-hidden="true">{U.timeseries([U.Series("CPU", CPU_TW[-12:], "var(--hazel)", kind="area")], HOURS[-12:], unit="%", vmax=100, w=440, h=120)}</div>
      </div></section>"""
    nodata = f"""
    <section class="chart" aria-label="圖表無資料範例"><div class="ph"><h2>溫度 · CPU 封裝</h2><span class="unit">°C</span></div>
      <div class="body" style="display:flex;flex-direction:column;align-items:center;justify-content:center;gap:6px;min-height:150px;padding:12px">
        <p style="font-size:14px;font-weight:500">這台節點沒有回報溫度</p><p class="note" style="text-align:center">lab-mini 的 hwmon 沒有可讀的感測器。這不是錯誤，圖表保留位置讓版面一致。</p>
      </div></section>"""
    danger = f"""
    <section class="chart" aria-label="危險操作確認範例"><div class="ph"><h2>危險操作確認</h2><span class="sub">寫出實際名稱，不只「確定嗎」</span></div>
      <div class="body" style="padding:12px 14px"><div class="modal" style="max-width:none">
        <h2 style="font-size:16px">刪除資料碟 <span class="mono">hazel-jp-02 /mnt/data</span>（sdb1）？</h2>
        <p style="font-size:13px;color:var(--muted)">會解除掛載並清除 870 GiB 資料，包含 <span class="mono">wiki-db</span> 的資料目錄與 2 個備份暫存。最近的備份 wiki-db 校驗通過但尚未還原測試。</p>
        <div class="field"><label for="dd">輸入磁碟名稱 <span class="mono">sdb1</span> 以確認</label><div class="inp err"><input id="dd" aria-label="磁碟名稱" value="sda2" style="background:transparent;border:0;flex:1" aria-invalid="true" aria-describedby="dd-err"></div><span class="errt" id="dd-err">名稱不符：請輸入 sdb1。</span></div>
        <div class="wrap" style="justify-content:flex-end"><button type="button" class="btn">取消</button><button type="button" class="btn dng" disabled aria-disabled="true" style="opacity:.6">刪除資料碟</button></div>
      </div></div></section>"""
    unsaved = f"""
    <section class="chart" aria-label="關閉未儲存檔案範例"><div class="ph"><h2>關閉未儲存的檔案</h2></div>
      <div class="body" style="padding:12px 14px"><div class="modal" role="alertdialog" aria-labelledby="h-us" style="max-width:none">
        <h2 id="h-us" style="font-size:16px">儲存 <span class="mono">.env</span> 的變更？</h2>
        <p class="mono" style="font-size:13px;color:var(--muted)">hazel-tw-01:/mnt/data/apps/wiki/.env · 已修改 2 行 · 812 B → 824 B</p>
        <div class="wrap" style="justify-content:flex-end"><button type="button" class="btn">取消</button><button type="button" class="btn">不儲存</button><button type="button" class="btn pri">儲存並關閉</button></div>
      </div></div></section>"""
    main = head + f'<div class="cg">{loading}{empty}{error}{partial}{offline}{nodata}</div><div class="cg">{danger}{unsaved}</div>'
    return U.shell("overview", main)



# ---------- 網站與入口 ----------
def websites() -> str:
    head = U.page_head("網站與入口", OBS, '<a class="btn pri" href="SiteCreate.dc.html">新增站點</a><button type="button" class="btn">申請憑證</button><button type="button" class="btn">Zentinel 設定</button>')
    tiles = '<div class="tiles">' + "".join([
        U.stat_tile("站點", "3", "", "1 憑證即將到期、1 上游失敗", False),
        U.stat_tile("今日請求（來源站）", U.fmt(sum(REQ_ORIGIN)), "", "較昨日 +8.4%", True, spark=REQ_ORIGIN),
        U.stat_tile("WAF 命中（只偵測）", "1,204", "", "未阻擋任何請求", None, spark=walk(24, 50, 20, 5, 120, {22: 118})),
        U.stat_tile("可信 IP 判定", "98.7", "% 來自 Tunnel", "1.1% 直接連線 · 0.2% 不可信 Header", None),
    ]) + "</div>"
    topo = U.topology([
        ("邊緣", [("cf", "Cloudflare", "Tunnel · 3 網域", "ok")]),
        ("入口", [("zt1", "Zentinel @ tw-01", "80／443 · WAF 偵測", "ok"), ("zt2", "Zentinel @ jp-02", "80／443 · 狀態過期", "warn")]),
        ("站點", [("shop", "shop.example.tw", "憑證 11 天 · 64%", "warn"), ("wiki", "wiki.example.tw", "憑證 71 天 · 31%", "ok"), ("stg", "staging.shop", "502 · 5%", "danger")]),
        ("上游", [("api", "shop-api:8080", "2 副本 · p95 310", "ok"), ("wapp", "wiki-app:3000", "1 副本 · p95 180", "ok"), ("sapi", "shop-api:8080 · stg", "無健康副本", "danger")]),
    ], [("cf", "zt1", .7, ""), ("cf", "zt2", .3, ""), ("zt1", "shop", .64, ""), ("zt2", "wiki", .31, ""), ("zt1", "stg", .05, "danger"),
        ("shop", "api", .64, ""), ("wiki", "wapp", .31, ""), ("stg", "sapi", .05, "danger")])
    topo_m = U.flowlist([("Cloudflare", "Zentinel @ tw-01", "70%"), ("Cloudflare", "Zentinel @ jp-02", "30%"), ("Zentinel @ tw-01", "shop.example.tw", "64%"), ("Zentinel @ jp-02", "wiki.example.tw", "31%"), ("Zentinel @ tw-01", "staging.shop", "502"), ("shop.example.tw", "shop-api:8080", "2 副本"), ("staging.shop", "shop-api:8080", "無健康副本")])
    topo_panel = f"""
    <section class="panel" aria-labelledby="h-wt">
      <div class="ph"><h2 id="h-wt">入口拓撲</h2>{U.legend(TOPO_LEGEND)}</div>
      <div style="padding: 10px 14px 6px"><div class="ch-d">{topo}</div><div class="ch-m">{topo_m}</div></div>
    </section>"""

    def cert(domain, days, issuer, auto):
        pct = min(100, days / 90 * 100)
        col = "var(--danger)" if days < 14 else ("var(--warn)" if days < 30 else "var(--ok)")
        shape = "■" if days < 14 else ("○" if days < 30 else "●")
        return (f'<div class="rank" style="grid-template-columns:minmax(0,1.1fr) minmax(0,1.2fr) auto"><span class="mono" style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap">{domain}</span>'
                f'<span class="bar" aria-hidden="true" style="position:relative"><i style="width:{pct:.0f}%;background:{col}"></i><b style="position:absolute;left:{14/90*100:.1f}%;top:-3px;width:1px;height:14px;background:var(--faint)"></b></span>'
                f'<span class="mono" style="font-size:12px;color:{col}">{shape} {days} 天 <span class="note">· {issuer} · {auto}</span></span></div>')
    certs = f"""
    <section class="panel" aria-labelledby="h-cert">
      <div class="ph"><h2 id="h-cert">憑證到期</h2><span class="sub">橫條＝剩餘天數（滿格 90 天）；細線＝14 天自動續期門檻</span><a class="more" href="#certs">全部憑證 →</a></div>
      <div class="ranks">
        {cert("shop.example.tw", 11, "Let's Encrypt", "續期失敗：DNS 驗證逾時")}
        {cert("wiki.example.tw", 71, "Let's Encrypt", "自動續期")}
        {cert("staging.shop.example.tw", 44, "Let's Encrypt", "自動續期")}
        {cert("panel.example.tw（管理入口）", 83, "自簽 · 內部 CA", "手動")}
      </div>
      <div class="note" style="padding: 0 14px 10px">續期失敗不等於憑證失效：shop.example.tw 目前仍有效，11 天內要處理 DNS Token</div>
    </section>"""
    waf = U.ts_card("WAF 命中 · 每小時", [U.Series("偵測命中", [int(x) for x in walk(24, 50, 20, 5, 120, {22: 118})], "var(--s1)")], HOURS, unit="",
                    bar=True, fmt_val=lambda v: f"{v:.0f}", markers=[(22, "掃描高峰")], foot="模式：只偵測（不阻擋）· 切成阻擋前先看誤判率 · 規則來源 OWASP CRS 4.x")
    rules = f"""
    <section class="panel" aria-labelledby="h-rules">
      <div class="ph"><h2 id="h-rules">命中最多的規則 · 今日</h2><span class="sub">依命中次數；右邊是會被阻擋的比例（估）</span></div>
      {U.ranked([("掃描器 UA（sqlmap、nikto）", 612, "612 · 100%"), ("路徑穿越 ../", 231, "231 · 100%"), ("SQLi 關鍵字", 188, "188 · 94%"), ("XSS 標籤", 97, "97 · 88%"), ("PHP 探針 /wp-login.php", 76, "76 · 100%")], vmax=650)}
      <div class="note" style="padding: 0 14px 10px">誤判樣本：SQLi 規則命中 11 次來自 wiki 搜尋「select」· 建議先排除該路徑再切阻擋</div>
    </section>"""
    ipsrc = f"""
    <section class="panel" aria-labelledby="h-ip">
      <div class="ph"><h2 id="h-ip">訪客 IP 來源 · 今日</h2><span class="sub">只有可信入口的 Header 會被採用；偽造 Header 的直接連線不能指定別人的 IP</span></div>
      <div style="padding: 12px 14px; display:flex; flex-direction:column; gap:8px">
        {U.tall_bar([(98.7, "Cloudflare Tunnel（CF-Connecting-IP）90,807", "var(--q5)"), (1.1, "直接連線（來源 IP）1,012", "var(--q3)"), (0.2, "不可信 Header（已忽略）182", "var(--danger)")], 100, unit="%")}
        <div class="scale"><i style="background:var(--q5)"></i>Tunnel 98.7% <i style="background:var(--q3)"></i>直接連線 1.1% <i style="background:var(--danger)"></i>不可信 Header 0.2%（限速與封鎖用真實來源 IP）</div>
      </div>
    </section>"""

    def site(domain, st, node, upstream, certd, wafm, spark, note=""):
        return (f'<a class="row srow" href="#site"><span class="name"><span class="n">{domain}</span><span class="d">{note}</span></span><span>{st}</span>'
                f'<span class="mono c3">{node}<br><span class="note">{upstream}</span></span><span class="c4 mono">{certd}</span><span class="c4">{wafm}</span><span class="c5">{U.sparkline(spark)}</span><span class="mono">{U.fmt(sum(spark) * 60)}</span></a>')
    table = f"""
    <section class="panel" aria-labelledby="h-sites" data-sort="0:asc">
      <div class="ph"><h2 id="h-sites">站點 · 3</h2><span class="sub">同一個站點在容器頁、備份頁是同一份資料</span></div>
      <div class="row hd srow"><span>網域</span><span>狀態</span><span class="c3">入口 · 上游</span><span class="c4">憑證</span><span class="c4">WAF</span><span class="c5">請求 1 小時</span><span>今日</span></div>
      {site("shop.example.tw", U.status("warn", "憑證 11 天"), "tw-01", "shop-api:8080", "11 天", "偵測", walk(12, 980, 120, 600, 1300), "production · HTTP/2 · HSTS")}
      {site("wiki.example.tw", U.status("ok", "正常"), "jp-02", "wiki-app:3000", "71 天", "偵測", walk(12, 470, 60, 300, 650), "production · 狀態過期的節點")}
      {site("staging.shop.example.tw", U.status("dng", "502 上游"), "tw-01", "shop-api:8080（staging）", "44 天", "偵測", walk(12, 80, 30, 10, 140), "staging · 部署失敗後無健康副本")}
    </section>"""
    runtimes = f"""
    <section class="panel" aria-labelledby="h-rt">
      <div class="ph"><h2 id="h-rt">運作環境與範本</h2><span class="sub">PHP／Node.js／Java／Go／Python／.NET 環境是容器，網站範本可匯出</span></div>
      <div class="fleet" style="grid-template-columns:repeat(auto-fill,minmax(min(220px,100%),1fr))">
        <div class="ncard"><span class="note">PHP 8.3 · tw-01</span><span>{U.status("ok", "執行中 · 2 站點")}</span>{U.meter(31, "記憶體 128/512M")}<span class="note">擴充 12 · Supervisor 1 · 相依檢驗通過</span></div>
        <div class="ncard"><span class="note">Node.js 22 · tw-01</span><span>{U.status("ok", "執行中 · 1 站點")}</span>{U.meter(40, "記憶體 410/1024M")}<span class="note">pnpm · 模組 184</span></div>
        <div class="ncard"><span class="note">Python 3.12 · lab-mini</span><span>{U.status("info", "維護中")}</span>{U.meter(5, "記憶體 20/512M")}<span class="note">venv · 0 站點</span></div>
        <div class="ncard"><span class="note">網站範本</span><span>3 個：靜態站、反向代理、PHP+MySQL</span><span class="note">可匯出成本機範本</span></div>
      </div>
    </section>"""
    siteconf = f"""
    <section class="panel" aria-labelledby="h-sc">
      <div class="ph"><h2 id="h-sc">shop.example.tw · 設定</h2><span class="sub">圖形化設定與原始設定檔可切換；不認識的欄位不會被刪</span><div class="wrap" style="margin-left:auto"><span class="chip on">圖形化</span><span class="chip">原始設定</span><span class="chip">日誌</span><span class="chip">原始碼</span></div></div>
      <div style="padding:12px 14px">{U.matrix(["網域", "反向代理 / 負載", "HTTPS / HSTS", "真實 IP", "防盜鏈 / 流量限制", "密碼保護 / CORS", "重寫 / 重導", "Stream（L4）"], ["狀態", "目前值"], [
        [("ok", "2"), ("none", "shop.example.tw、www · 驗證通過")], [("ok", "2 上游"), ("none", "shop-api:8080 ×2 · 健康檢查 /healthz")], [("warn", "11 天"), ("none", "Let's Encrypt · HSTS 6 個月 · TLS 1.2+")],
        [("ok", "Tunnel"), ("none", "只信 Cloudflare · CF-Connecting-IP")], [("ok", "啟用"), ("none", "靜態檔防盜鏈 · 120 請求/分/IP")], [("none", "未啟用"), ("none", "CORS 允許 app.example.tw")],
        [("ok", "3 條"), ("none", "/old → /new 301 · 偽靜態")], [("none", "不支援"), ("none", "Zentinel 無 L4；另選元件")],
      ], first_col="minmax(150px,1.2fr)")}</div>
    </section>"""
    main = head + U.scope_bar([("全部入口", "3 站點"), ("Zentinel @ hazel-tw-01", "2 站點"), ("Zentinel @ hazel-jp-02", "1 站點 · 過期")], label="入口") + tiles + topo_panel + f'<div class="cg">{certs}{waf}</div>' + f'<div class="cg">{rules}{ipsrc}</div>' + table + f'<div class="cg">{siteconf}{runtimes}</div>'
    return U.shell("web", main)


# ---------- 儲存與備份 ----------
def storage() -> str:
    head = U.page_head("儲存與備份", OBS, '<a class="btn pri" href="BackupCreate.dc.html">新增備份任務</a><button type="button" class="btn">綁定裝置身分</button><button type="button" class="btn">清理可回收</button>')
    tiles = '<div class="tiles">' + "".join([
        U.stat_tile("資料碟合計", "1.32", "/ 1.98 TiB", "jp-02 15 天後滿", False, spark=walk(12, 1200, 20, 1100, 1330)),
        U.stat_tile("未綁定裝置身分", "1", "磁碟", "hazel-jp-02 /mnt/data", False),
        U.stat_tile("近 7 天備份", "20", "/21 成功", "1 失敗：S3 逾時", None, spark=[3, 3, 3, 2, 3, 3, 3]),
        U.stat_tile("還原測試", "9", "天前", "wiki-db · 通過", None),
    ]) + "</div>"

    def disk(node, mount, dev, st, parts, total, growth, note, warn=False):
        col = "var(--danger)" if warn else "var(--muted)"
        return (f'<div style="display:flex;flex-direction:column;gap:8px;padding:12px 14px;background:var(--panel)">'
                f'<div style="display:flex;justify-content:space-between;gap:8px;flex-wrap:wrap"><span class="mono" style="font-weight:500">{node} {mount}</span>{st}</div>'
                f'<span class="note mono">{dev}</span>{U.tall_bar(parts, total)}'
                f'<div style="display:flex;align-items:center;gap:10px;flex-wrap:wrap"><span class="note" style="color:{col}">{note}</span><span style="margin-left:auto" class="note">30 天</span>{U.sparkline(growth, w=80)}</div></div>')
    disks = f"""
    <section class="panel" aria-labelledby="h-disks">
      <div class="ph"><h2 id="h-disks">資料碟</h2><span class="sub">段條＝誰吃掉了空間；未綁定裝置身分的磁碟，重開機掛錯時不會被擋下</span><div class="scale" style="margin-left:auto"><i style="background:var(--q5)"></i>容器資料 <i style="background:var(--q4)"></i>資料庫／媒體 <i style="background:var(--q3)"></i>備份暫存 <i style="background:var(--q2)"></i>可回收 <i style="background:var(--q1)"></i>可用</div></div>
      <div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(min(300px,100%),1fr));gap:1px;background:var(--line)">
        {disk("hazel-tw-01", "/mnt/data", "sdb1 → by-uuid/5a7c…e2 · xfs", U.status("ok", "已綁定 UUID"), [(290, "容器資料", "var(--q5)"), (84, "資料庫", "var(--q4)"), (38, "備份暫存", "var(--q3)"), (12, "可回收映像", "var(--q2)")], 931, walk(12, 390, 8, 370, 412), "412 / 931 GiB · +38 GiB · 預估 410 天後滿")}
        {disk("hazel-jp-02", "/mnt/data", "sdb1（未綁定）· ext4", U.status("dng", "未綁定 · 掛錯磁碟不會被擋"), [(610, "容器資料", "var(--q5)"), (190, "媒體", "var(--q4)"), (70, "備份暫存", "var(--q3)"), (31, "可回收映像", "var(--q2)")], 931, walk(12, 760, 15, 740, 870), "870 / 931 GiB · +121 GiB · 預估 15 天後滿", warn=True)}
        {disk("lab-mini", "/mnt/data", "nvme0n1p3 → by-uuid/91b0…77 · ext4", U.status("ok", "已綁定 UUID"), [(28, "容器資料", "var(--q5)"), (6, "資料庫", "var(--q4)"), (4, "可回收映像", "var(--q2)")], 118, walk(12, 36, 2, 30, 38), "38 / 118 GiB · +2 GiB")}
      </div>
    </section>"""
    vols = f"""
    <section class="panel" aria-labelledby="h-vols">
      <div class="ph"><h2 id="h-vols">最大的儲存卷</h2><span class="sub">全部節點 · 前 8</span><a class="more" href="#vols">全部 →</a></div>
      {U.ranked([("wiki-media（jp-02）", 190, "190 GiB"), ("shop-db-data（tw-01）", 84, "84 GiB"), ("ollama-models（tw-01）", 61, "61 GiB"), ("wiki-db-data（jp-02）", 42, "42 GiB"), ("backup-staging（tw-01）", 38, "38 GiB"), ("shop-uploads（tw-01）", 27, "27 GiB"), ("registry-cache（tw-01）", 19, "19 GiB"), ("shop-redis-data（tw-01）", 1, "0.8 GiB")], vmax=200)}
    </section>"""
    io = U.ts_card("磁碟 I/O · 全部資料碟", [U.Series("讀取", IO_R, "var(--s1)"), U.Series("寫入", IO_W, "var(--s2)")], HOURS, unit=" MB/s", fmt_val=lambda v: f"{v:.0f}",
                   markers=[(22, "wiki-db dump")], foot="02:00 的尖峰是每日備份 dump；想看單一磁碟到節點詳情")

    def bjob(name, sched, dest, states, last, st):
        return (f'<div style="display:flex;flex-direction:column;gap:8px;padding:12px 14px;background:var(--panel)">'
                f'<div style="display:flex;justify-content:space-between;gap:8px;flex-wrap:wrap"><span style="font-weight:500">{name}</span>{st}</div>'
                f'<span class="note">{sched} → {dest}</span>{U.calendar(states)}<span class="note">{last}</span>'
                f'<div class="wrap"><button type="button" class="btn sm">立即執行</button><button type="button" class="btn sm">測試還原（到暫存庫）</button><button type="button" class="btn sm">還原到…</button><button type="button" class="btn sm">紀錄</button></div></div>')
    backups = f"""
    <section class="panel" aria-labelledby="h-bk">
      <div class="ph"><h2 id="h-bk">備份任務 · 近 14 週</h2><div class="scale" style="margin-left:auto"><i style="background:var(--ok)"></i>成功 <i style="background:var(--danger)"></i>失敗 <i style="background:var(--warn)"></i>部分 <i style="background:var(--q1)"></i>未排</div></div>
      <div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(min(300px,100%),1fr));gap:1px;background:var(--line)">
        {bjob("wiki-db 每日", "每天 03:00", "S3 tokyo-backups", "o" * 40 + "f" + "o" * 20 + "r" + "o" * 33 + "e" * 2 + "n" * 2, "今天 03:00 · 4.6 GiB · 已校驗 · 還原測試 9 天前通過", U.status("ok", "正常"))}
        {bjob("shop-db 每日", "每天 03:30", "S3 tokyo-backups", "o" * 61 + "f" + "o" * 33 + "e" * 2 + "n" * 2, "今天 03:30 · 1.3 GiB · 已校驗 · 還原測試從未執行", U.status("warn", "未做還原測試"))}
        {bjob("平台快照 每週", "每週日 04:00", "本機 /mnt/backup + S3", "e" * 6 + ("o" + "e" * 6) * 13 + "n" * 2, "10 月 26 日 · 212 MiB · 含設定、機密包、資料庫", U.status("ok", "正常"))}
      </div>
    </section>"""
    verify = f"""
    <section class="panel" aria-labelledby="h-ver">
      <div class="ph"><h2 id="h-ver">備份進度 · wiki-db · 今天</h2><span class="sub">每一步都有證據；沒做還原測試不算「可還原」</span></div>
      <div style="padding: 12px 14px">{U.pipeline([("dump", "done", "1 分 48 秒", "pg_dump · 4.6 GiB"), ("傳輸", "done", "2 分 02 秒", "38 MB/s → S3"), ("校驗", "done", "12 秒", "sha256 相符"), ("保留", "done", "即時", "30 日 · 刪 10/08"), ("還原測試", "todo", "排程 11/10", "lab-mini 沙盒")])}</div>
    </section>"""
    dests = f"""
    <section class="panel" aria-labelledby="h-dst">
      <div class="ph"><h2 id="h-dst">備份目的地</h2></div>
      <div style="padding: 12px 14px; display:flex; flex-direction:column; gap:12px">
        <div><div style="display:flex;justify-content:space-between"><span class="mono">S3 tokyo-backups</span><span class="note">配額 1,000 GiB</span></div>{U.meter(41, "412 / 1,000 GiB", warn=80, danger=90)}<span class="note">保留 30 日 · 上次連線測試 03:31 成功</span></div>
        <div><div style="display:flex;justify-content:space-between"><span class="mono">本機 /mnt/backup（tw-01）</span><span class="note">500 GiB</span></div>{U.meter(16, "81 / 500 GiB", warn=80, danger=90)}<span class="note">只放平台快照 · 同一台機器，不算異地</span></div>
      </div>
    </section>"""
    main = head + U.scope_bar([("全部節點", "3"), ("hazel-tw-01", "Docker"), ("hazel-jp-02", "Docker · 過期"), ("lab-mini", "Podman · K3s")]) + tiles + disks + f'<div class="cg">{vols}{io}</div>' + backups + f'<div class="cg">{verify}{dests}</div>'
    return U.shell("storage", main)


# ---------- 事件與告警 ----------
def events() -> str:
    head = U.page_head("事件與告警", OBS, '<button type="button" class="btn pri">新增告警規則</button><button type="button" class="btn">通知路由</button><button type="button" class="btn">靜音中 · 1</button>')
    tiles = '<div class="tiles">' + "".join([
        U.stat_tile("開啟中的告警", "3", "", "1 嚴重、2 警告", False),
        U.stat_tile("24 小時事件", "142", "", "較昨日 +31", None, spark=[int(x) for x in walk(24, 6, 3, 0, 14, {22: 13})]),
        U.stat_tile("通知送達", "47", "/48", "1 次 Telegram 逾時後重送成功", None),
        U.stat_tile("靜音中", "1", "", "jp-02 磁碟 · 剩 22 小時", None),
    ]) + "</div>"

    def rule(name, states, st, note):
        return (f'<div class="row" style="grid-template-columns:minmax(0,1.3fr) minmax(0,2.2fr) minmax(0,1fr);gap:12px;align-items:center">'
                f'<span class="name"><span class="n" style="font-family:inherit">{name}</span><span class="d">{note}</span></span><span>{U.state_timeline(states)}</span><span>{st}</span></div>')
    rules = f"""
    <section class="panel" aria-labelledby="h-rl">
      <div class="ph"><h2 id="h-rl">告警規則 · 近 24 小時</h2>{U.legend(TL_LEGEND)}</div>
      <div class="row hd" style="grid-template-columns:minmax(0,1.3fr) minmax(0,2.2fr) minmax(0,1fr)"><span>規則</span><span>24 小時</span><span>現在</span></div>
      {rule("主機沒有回應 > 40 秒", "o" * 23 + "f", U.status("dng", "觸發 · jp-02"), "任何節點 · 嚴重")}
      {rule("資料碟 > 90%", "o" * 18 + "f" * 3 + "r" * 3, U.status("warn", "靜音 22 小時"), "任何節點 · 嚴重")}
      {rule("憑證 < 14 天", "o" * 20 + "f" * 4, U.status("warn", "觸發 · shop.example.tw"), "任何站點 · 警告")}
      {rule("部署失敗", "o" * 14 + "f" + "o" * 8 + "f", U.status("warn", "觸發 · shop staging"), "任何專案 · 警告")}
      {rule("5xx 錯誤率 > 1%（5 分）", "o" * 22 + "f" + "o", U.status("ok", "已恢復 03:01"), "shop-api · 警告")}
      {rule("GPU 溫度 > 83°C", "o" * 24, U.status("ok", "正常"), "tw-01 · 警告")}
      {rule("備份失敗", "e" * 3 + "o" * 21, U.status("ok", "正常"), "任何備份任務 · 嚴重")}
    </section>"""
    srcs = ["hazel-tw-01", "hazel-jp-02", "lab-mini", "zentinel", "shop-api", "wiki-app", "備份", "平台"]
    hours12 = [f"{(16 + i) % 24:02d}" for i in range(12)]
    vals = [[int(x) for x in walk(12, b, a, 0, 20, sp)] for b, a, sp in [(2, 1.5, None), (3, 2, {10: 14, 11: 12}), (1, 1, None), (2, 1, {10: 9}), (2, 1.5, {10: 11}), (1, .8, None), (0, .5, {11: 3}), (1, .5, None)]]
    heat = f"""
    <section class="panel" aria-labelledby="h-eh">
      <div class="ph"><h2 id="h-eh">事件熱格 · 來源 × 小時</h2><span class="sub">一眼掃出事件從哪裡來、什麼時候集中</span><div class="legend"><span><i class="rect" style="background: var(--q1)"></i>0</span><span><i class="rect" style="background: var(--q3)"></i>10</span><span><i class="rect" style="background: var(--q5)"></i>20+</span></div></div>
      <div style="padding: 12px 14px">{U.heatgrid(srcs, hours12, vals, vmax=20)}</div>
      <div class="foot"><span>02 時的集中：jp-02 沒有回應的連鎖（主機、zentinel、wiki-app 同時出事件）</span><a href="#table">表格檢視</a></div>
    </section>"""
    top = f"""
    <section class="panel" aria-labelledby="h-top">
      <div class="ph"><h2 id="h-top">事件最多的來源 · 24 小時</h2></div>
      {U.ranked([("hazel-jp-02", 41, "41"), ("shop-api（staging）", 27, "27"), ("zentinel @ tw-01", 22, "22"), ("hazel-tw-01", 18, "18"), ("shop-api", 15, "15"), ("wiki-app", 9, "9"), ("備份", 6, "6"), ("平台", 4, "4")], vmax=45)}
    </section>"""
    route = U.topology([
        ("告警", [("a1", "主機沒有回應", "嚴重", "danger"), ("a2", "資料碟 > 90%", "嚴重 · 靜音中", "warn"), ("a3", "憑證 < 14 天", "警告", "warn")]),
        ("路由", [("r1", "嚴重 → 立刻", "不分時段", "ok"), ("r2", "警告 → 工作時段", "09–22 · 其餘彙整", "ok")]),
        ("管道", [("c1", "Telegram · 維運群", "47/48 送達", "ok"), ("c2", "Email · tim", "備援", "ok"), ("c3", "Webhook · 值班系統", "未設定", "dim")]),
    ], [("a1", "r1", .9, ""), ("a2", "r1", .4, "dim"), ("a3", "r2", .5, ""), ("r1", "c1", .9, ""), ("r1", "c2", .3, ""), ("r2", "c1", .5, ""), ("r2", "c3", .1, "dim")])
    route_m = U.flowlist([("主機沒有回應", "嚴重 → 立刻", "Telegram、Email"), ("資料碟 > 90%", "嚴重 → 立刻", "靜音中"), ("憑證 < 14 天", "警告 → 工作時段", "Telegram")])
    route_panel = f"""
    <section class="panel" aria-labelledby="h-rt">
      <div class="ph"><h2 id="h-rt">告警路由</h2><span class="sub">送達＝供應商回覆已接受，不等於有人看到；測試訊息不帶正式機密</span><a class="more" href="#routes">編輯路由 →</a></div>
      <div style="padding: 10px 14px 6px"><div class="ch-d">{route}</div><div class="ch-m">{route_m}</div></div>
    </section>"""
    def card(tag, color, title, evidence, pri, sec):
        return ((f'<article class="card"><div style="font-size: 12px; color: {color}; font-weight: 600">{tag}</div>'
                f'<p style="font-size: 14px; font-weight: 500; line-height: 1.45">{title}</p><p class="note" style="color: var(--muted)">{evidence}</p>')
                + (f'<div class="wrap"><a class="btn pri sm" style="color: var(--hazelink)" href="{pri[1]}">{pri[0]}</a>' if isinstance(pri, tuple) else f'<div class="wrap"><button type="button" class="btn pri sm" style="color: var(--hazelink)">{pri}</button>')
                + f'<button type="button" class="btn sm">{sec}</button></div></article>')
    open_ = f"""
    <section class="panel" aria-labelledby="h-open">
      <div class="ph"><h2 id="h-open">開啟中 · 3</h2><span class="sub">發生什麼、影響什麼、證據、下一步</span></div>
      <div class="fleet">
        {card("嚴重 · 主機沒有回應 · 03:41 起", "var(--danger)", "hazel-jp-02 · 最後心跳 03:41:18 · 外部探測 wiki.example.tw：200", "受影響：wiki-app、wiki-db、zentinel<br><span class='mono'>heartbeat send failed: dial tcp 203.0.113.22:8443: i/o timeout</span>", ("處理", "NodeOffline.dc.html"), "確認已處理")}
        {card("警告 · 憑證到期 · 02:58 起", "var(--warn)", "shop.example.tw · 到期 11 月 18 日 · 續期失敗 ×3", "<span class='mono'>acme: dns-01 challenge timeout (cloudflare)</span>", "重試續期", "靜音 24h")}
        {card("警告 · 部署失敗 · 03:12 起", "var(--warn)", "shop staging #71 · lab-mini · 步驟 migrate 結束碼 1", "服務中版本：c91e0d7 之前的 c0ffee1（自動回滾）<br><span class='mono'>migrate: relation 「orders_v2」 already exists (0004_orders.sql:12)</span>", ("看工作", "WorkCenter.dc.html"), "指派給")}
      </div>
    </section>"""
    main = head + U.scope_bar([("全部節點", "3"), ("hazel-tw-01", "Docker"), ("hazel-jp-02", "Docker · 過期"), ("lab-mini", "Podman · K3s")]) + tiles + rules + f'<div class="cg">{heat}{top}</div>' + route_panel + open_
    return U.shell("events", main)



# ---------- 主機（列表） ----------
def nodes(part: str | None = None) -> str:
    head = U.page_head("主機", OBS, '<a class="btn pri" href="AddNode.dc.html">新增主機</a>')
    tiles = '<div class="tiles">' + "".join([
        U.stat_tile("主機", "3", "", "1 連線中 · 1 沒有回應 · 1 維護中", None),
        U.stat_tile("引擎環境", "6", "", "Docker 2 · Podman 2 · K3s 1 · kind 1", None),
        U.stat_tile("節點憑證", "1", "/3 需在 30 天內輪替", "tw-01 剩 12 天", False),
        U.stat_tile("待套用安全更新", "7", "套件", "<a href=\"Updates.dc.html\">jp-02 5 · lab-mini 2 → 查看</a>", None),
    ]) + "</div>"
    def nrow(name, os_, st, cpu, mem, disk, spark, ctr, last, stale=False, href="NodeDetail.dc.html"):
        acts = "".join(f'<button type="button" class="ib" aria-label="{t}" title="{t}">{U.icon(k)}</button>' for k, t in (("files", "檔案"), ("term", "終端機"), ("more", "更多")))
        return (f'<a class="row" href="{href}"><span class="name"><span class="n mono">{name}</span><span class="d">{os_}</span></span><span>{st}</span>'
                f'<span>{U.meter(cpu, f"{cpu:.0f}%{chr(42) if stale else chr(32)}".strip())}</span><span>{U.meter(mem[0] / mem[1] * 100, f"{mem[0]:.0f}/{mem[1]:.0f}G")}</span>'
                f'<span class="c3">{U.meter(disk[0] / disk[1] * 100, f"{disk[0]}/{disk[1]}G")}</span><span class="c5">{U.sparkline(spark, stale=stale)}</span>'
                f'<span class="mono c4">{ctr}</span><span class="mono c6">{last}</span><span class="acts">{acts}</span></a>')
    fleet = f"""
    <section class="panel" aria-labelledby="h-fl" data-sort="1:desc" data-batch>
      <div class="ph"><h2 id="h-fl">主機 · 3</h2><div class="wrap" style="margin-left:auto"><label class="inp" style="min-width:200px"><span class="note">搜尋</span><input placeholder="名稱、標籤、IP" style="background:transparent;border:0;flex:1;min-width:0"></label><span class="chip on">全部 3</span><span class="chip">production 2</span><span class="chip">lab 1</span><span class="chip">有問題 1</span><span class="range" role="group" aria-label="檢視方式"><button type="button" class="btn" aria-pressed="false">卡片</button><button type="button" class="btn on" aria-pressed="true">表格</button></span></div></div>
      <div class="row hd"><span>節點</span><span>狀態</span><span>CPU</span><span>記憶體</span><span class="c3">資料碟</span><span class="c5">CPU 1 小時</span><span class="c4">容器</span><span class="c6">最後聯絡</span><span></span></div>
      {nrow("hazel-jp-02", "Debian 12 · Docker 27.1 · 4 核", U.status("dng", "沒有回應 · 48 秒"), 64, (13.9, 15.5), (870, 931), CPU_JP[-12:], "3/3", "03:41:18", stale=True, href="NodeOffline.dc.html")}
      {nrow("hazel-tw-01", "Ubuntu 24.04 · 控制中心 · 8 核 · L4", U.status("ok", "連線中"), 37, (11.2, 31.3), (412, 931), CPU_TW[-12:], "9/10", "2 秒前")}
      {nrow("lab-mini", "Debian 13 · Podman 5.2 · K3s · 4 核", U.status("info", "維護中"), 3, (1.1, 7.6), (38, 118), CPU_LAB[-12:], "0/2", "5 秒前")}
    </section>"""
    cross = f"""
    <section class="panel" aria-labelledby="h-cross">
      <div class="ph"><h2 id="h-cross">跨節點資源</h2><span class="sub">同一條尺，看誰還有空間</span></div>
      <div style="padding:12px 14px;display:flex;flex-direction:column;gap:12px">
        <div><span class="note">CPU（核）</span>{U.tall_bar([(3.0, "tw-01 已用", "var(--q5)"), (2.6, "jp-02 已用*", "var(--q4)"), (0.1, "lab-mini 已用", "var(--q3)")], 16, unit=" 核")}</div>
        <div><span class="note">記憶體（GiB）</span>{U.tall_bar([(11.2, "tw-01", "var(--q5)"), (13.9, "jp-02*", "var(--q4)"), (1.1, "lab-mini", "var(--q3)")], 54.4, unit="G")}</div>
        <div><span class="note">資料碟（GiB）</span>{U.tall_bar([(412, "tw-01", "var(--q5)"), (870, "jp-02*", "var(--q4)"), (38, "lab-mini", "var(--q3)")], 1980, unit="G")}</div>
        <div class="scale"><i style="background:var(--q5)"></i>tw-01 <i style="background:var(--q4)"></i>jp-02（* 上次觀測） <i style="background:var(--q3)"></i>lab-mini <i style="background:var(--q1)"></i>可用</div>
      </div>
    </section>"""
    engines = f"""
    <section class="panel" aria-labelledby="h-eng">
      <div class="ph"><h2 id="h-eng">元件</h2></div>
      <div style="padding:12px 14px">{U.matrix(["hazel-tw-01", "hazel-jp-02", "lab-mini"], ["Agent", "Zentinel", "節點憑證", "其他管理工具"], [
        [("ok", "0.1.0"), ("ok", "0.6.54"), ("warn", "12 天"), ("none", "")],
        [("warn", "0.1.0 過期"), ("warn", "0.6.50 可更新"), ("ok", "71 天"), ("warn", "1Panel 2.4.1 · 3 容器")],
        [("ok", "0.1.0"), ("none", ""), ("ok", "88 天"), ("none", "")],
      ])}</div>
    </section>"""
    def env(kind, node, owner, endpoint, ver, st, load, acts):
        return (f'<div class="row envrow"><span class="name"><span class="n">{kind}</span><span class="d mono">{node}</span></span><span>{owner}</span>'
                f'<span class="mono c4 wr">{endpoint}</span><span class="mono c3">{ver}</span><span>{st}</span><span class="c5">{load}</span><span>{acts}</span></div>')
    more = f'<button type="button" class="ib" aria-label="更多動作">{U.icon("more")}</button>'
    envs = f"""
    <section class="panel" aria-labelledby="h-env" data-sort="0:asc">
      <div class="ph"><h2 id="h-env">引擎環境 · 6</h2><div class="wrap" style="margin-left:auto"><button type="button" class="btn sm">重新偵測</button><a class="btn sm" href="ContainersEmpty.dc.html">新增環境</a></div></div>
      <div class="row hd envrow"><span>環境</span><span>擁有者</span><span class="c4">連接端點</span><span class="c3">版本</span><span>狀態</span><span class="c5">工作量</span><span></span></div>
      {env("Docker", "hazel-tw-01", U.attr("user", "rootful", "root"), "/var/<wbr>run/<wbr>docker.sock", "27.3.1", U.status("ok", "執行中"), U.attr("box", "10 個容器", "10") + U.attr("k8s", "1 個由 dev 叢集管理", "1"), more)}
      {env("kind 叢集 dev", "hazel-tw-01 · 在 Docker（root）裡", U.attr("user", "rootful", "root"), "https://127.0.0.1:41235", "v1.31.0", U.status("ok", "執行中"), U.attr("nodes", "1 個節點容器", "1"), more)}
      {env("Docker", "hazel-jp-02", U.attr("user", "rootful", "root"), "/var/<wbr>run/<wbr>docker.sock", "27.1.0", U.status("warn", "狀態過期"), U.attr("box", "3 個容器", "3"), more)}
      {env("Podman", "lab-mini", U.attr("user", "rootless · uid 1001", "deploy"), "/run/<wbr>user/<wbr>1001/<wbr>podman/<wbr>podman.sock", "5.2.3", U.status("warn", "未驗證版本"), U.attr("box", "3 個容器", "3"), more)}
      {env("Podman", "lab-mini", U.attr("user", "rootless · uid 1000", "tim"), "/run/<wbr>user/<wbr>1000/<wbr>podman/<wbr>podman.sock", "5.2.3", U.status("info", "socket 未啟用"), U.attr("box", "0 個容器", "0"), more)}
      {env("K3s 叢集 lab-k3s", "lab-mini · server", U.attr("user", "rootful", "root"), "https://10.8.0.5:6443", "v1.31.2+k3s1", U.status("ok", "執行中"), U.attr("nodes", "2 個節點", "2"), more)}
    </section>"""
    ports = f"""
    <section class="panel" aria-labelledby="h-fw">
      <div class="ph"><h2 id="h-fw">Port × 節點</h2><span class="sub">主機規則與容器對外暴露放在同一張圖；Docker 繞過防火牆的 Port 會標出來</span></div>
      <div style="padding:12px 14px">{U.matrix(['22', '80', '443', '8443', '18080', '5432', '6379', '11434', '轉發規則'], ["hazel-tw-01", "hazel-jp-02", "lab-mini"], [[('ok', 'sshd'), ('ok', 'sshd'), ('ok', 'sshd 2222')], [('ok', 'zentinel'), ('ok', 'zentinel'), ('none', '')], [('ok', 'zentinel'), ('ok', 'zentinel'), ('none', '')], [('ok', 'core · 白名單'), ('none', ''), ('none', '')], [('none', '僅本機（18080）'), ('none', ''), ('none', '')], [('none', '僅內部'), ('dng', 'wiki-db 對外!'), ('none', '')], [('none', '僅內部'), ('none', ''), ('none', '')], [('warn', 'ollama 直通'), ('none', ''), ('none', '')], [('ok', '2 條'), ('none', '0'), ('none', '0')]], first_col="minmax(80px,.6fr)")}</div>
      <div class="note" style="padding:0 14px 10px">■ 紅 = 容器用 <span class="mono">-p</span> 直接對外且沒有規則擋；Docker 的 iptables 鏈會繞過 ufw，所以這裡看的是實際可達</div>
    </section>"""
    def pub(port, ctr, node, bind, ext, rule, acts):
        return (f'<div class="row pubrow"><span class="name"><span class="n mono">{port}</span><span class="d">{ctr} · {node}</span></span>'
                f'<span class="mono c5">{bind}</span><span>{ext}</span><span class="c3">{rule}</span><span class="wrap" style="justify-content:flex-end;flex-wrap:nowrap">{acts}</span></div>')
    def b(t, pri=False):
        return f'<button type="button" class="btn sm {"c4 pri" if pri else "c5"}">{t}</button>'
    more = f'<button type="button" class="ib" aria-label="更多動作">{U.icon("more")}</button>'
    published = f"""
    <section class="panel" aria-labelledby="h-pub" data-batch>
      <div class="ph"><h2 id="h-pub">容器發布的 Port · 6</h2><div class="wrap" style="margin-left:auto">{U.status("dng", "外部可連 2")}<button type="button" class="btn sm">重測外部可達</button><button type="button" class="btn sm">封鎖選取的外部連線</button></div></div>
      <div class="row hd pubrow"><span>主機 Port</span><span class="c5">綁定</span><span>外部實測</span><span class="c3">攔截規則</span><span></span></div>
      {pub("5432/tcp", "wiki-db", "jp-02", "0.0.0.0", U.status("dng", "可連 · 03:40"), U.status("warn", "jp-02 沒有回應 · 恢復後套用"), '<a class="btn sm c4 pri" href="FirewallRule.dc.html">排入：擋掉外部連線</a>' + b("只允許指定來源") + more)}
      {pub("11434/tcp", "ollama", "tw-01", "0.0.0.0", U.status("dng", "可連 · 03:40"), '<span class="note">—</span>', b("擋掉外部連線", True) + b("只允許指定來源") + b("只讓本機連") + more)}
      {pub("18080/tcp", "shop-api", "tw-01", "127.0.0.1", U.status("ok", "僅本機 · 經 Zentinel"), '<span class="note">不需要</span>', more)}
      {pub("3000/tcp", "wiki-app", "jp-02", "0.0.0.0", U.status("ok", "擋住 · 03:40"), '<span class="mono">DOCKER-USER</span><span class="note"> · 只允許 127.0.0.1</span>', b("編輯規則") + b("解除") + more)}
      {pub("9000/tcp", "minio", "lab-mini · Podman deploy", "0.0.0.0", U.status("ok", "擋住 · 03:39"), '<span class="mono">ufw</span><span class="note"> · 只允許 10.8.0.0/24</span>', b("編輯規則") + b("解除") + more)}
      {pub("6379/tcp", "shop-redis", "tw-01", "127.0.0.1", U.status("ok", "僅本機"), '<span class="note">不需要</span>', more)}
    </section>"""
    procs = f"""
    <section class="panel" aria-labelledby="h-proc">
      <div class="ph"><h2 id="h-proc">程序 · 現在</h2><div class="range" role="group" style="margin-left:auto"><button type="button" class="btn on" aria-pressed="true">CPU</button><button type="button" class="btn" aria-pressed="false">記憶體</button><button type="button" class="btn" aria-pressed="false">I/O</button><button type="button" class="btn" aria-pressed="false">連線</button></div></div>
      {U.ranked([("ollama runner（容器 ollama）", 44, "44%"), ("postgres: shop（容器 shop-db）", 12, "12%"), ("node shop-api（容器 shop-api ×2）", 11, "11%"), ("zentinel（主機）", 2, "2%"), ("hazelminx-agent（主機）", 0.4, "0.4%"), ("systemd-journald", 0.3, "0.3%")], vmax=50)}
      <div class="note" style="padding:0 14px 10px">程序對回容器名稱；殭屍程序與高 I/O 等待另列診斷 · 連線分頁列出監聽與已建立連線</div>
    </section>"""
    ssh = f"""
    <section class="panel" aria-labelledby="h-ssh">
      <div class="ph"><h2 id="h-ssh">SSH 與金鑰</h2><span class="sub">主機指紋、sshd 設定與授權金鑰一眼看完</span></div>
      <div style="padding:12px 14px">{U.matrix(["hazel-tw-01", "hazel-jp-02", "lab-mini"], ["Port", "root 登入", "密碼登入", "授權金鑰", "指紋", "登入中"], [
        [("ok", "22"), ("ok", "禁止"), ("warn", "允許"), ("ok", "3 把"), ("ok", "SHA256:9f3a…"), ("info", "tim × 1")],
        [("ok", "22"), ("ok", "禁止"), ("ok", "禁止"), ("ok", "2 把"), ("warn", "上次改變 10/29"), ("none", "")],
        [("ok", "2222"), ("ok", "僅金鑰"), ("ok", "禁止"), ("ok", "4 把"), ("ok", "SHA256:71c0…"), ("info", "deploy × 2")],
      ])}</div>
      <div class="note" style="padding:0 14px 10px">jp-02 的主機指紋在 10 月 29 日改變：若不是你重裝，先別信任 · 平台 Agent 不靠 sshd，sshd 壞掉仍可從這裡修</div>
    </section>"""
    if part:
        return {"ports": ports, "published": published, "procs": procs, "ssh": ssh}[part]
    main = head + nodes_tabs("主機") + tiles + fleet + envs + f'<div class="cg">{cross}{engines}</div>'
    return U.shell("nodes", main)


# ---------- Kubernetes ----------
def kubernetes() -> str:
    head = U.page_head("Kubernetes", OBS, '<button type="button" class="btn pri">部署工作負載</button><button type="button" class="btn">安裝 Helm Chart</button><button type="button" class="btn">YAML 匯入</button><button type="button" class="btn">加入節點</button>')
    tiles = '<div class="tiles">' + "".join([
        U.stat_tile("叢集", "lab-k3s", "", "K3s 1.31.2 · 1 控制 · 1 工作 · 單控制節點（非 HA）", None),
        U.stat_tile("工作負載", "9", "", "8 就緒 · 1 CrashLoop", False),
        U.stat_tile("Pod", "23", "/24 就緒", "1 Pending：節點資源不足", False, spark=[22, 22, 23, 23, 24, 24, 24, 23, 23, 23, 23, 23]),
        U.stat_tile("PVC", "6", "", "已綁定 6 · local-path 5 · nfs 1", None),
    ]) + "</div>"
    topo = U.topology([
        ("入口", [("zt", "Zentinel @ hazel-tw-01", "80／443 → Traefik", "ok"), ("tr", "Traefik（K3s 內建）", "Ingress · 2 規則", "ok")]),
        ("Namespace", [("ns1", "lab", "4 工作負載", "ok"), ("ns2", "monitoring", "3 工作負載", "ok"), ("ns3", "shop-stg", "1 CrashLoop", "danger")]),
        ("節點", [("c1", "lab-mini（控制＋工作）", "4 核 · 8 GiB · 23 Pod", "warn"), ("w1", "lab-pi-01（工作）", "4 核 · 4 GiB · 0 Pod · NotReady", "danger")]),
    ], [("zt", "tr", .9, ""), ("tr", "ns1", .7, ""), ("tr", "ns2", .2, ""), ("tr", "ns3", .1, "danger"), ("ns1", "c1", .7, ""), ("ns2", "c1", .2, ""), ("ns3", "c1", .1, "danger"), ("ns1", "w1", .05, "dim")])
    topo_m = U.flowlist([("Zentinel", "Traefik", "80/443"), ("Traefik", "lab", "4 工作負載"), ("Traefik", "shop-stg", "CrashLoop"), ("lab", "lab-mini", "23 Pod"), ("lab-pi-01", "NotReady", "0 Pod")])
    topo_panel = f"""
    <section class="panel" aria-labelledby="h-kt">
      <div class="ph"><h2 id="h-kt">叢集拓撲</h2><span class="sub">Zentinel 負責對外 TLS 與 WAF，Traefik 負責叢集內路由：責任寫死，不重疊</span></div>
      <div style="padding:10px 14px 6px"><div class="ch-d">{topo}</div><div class="ch-m">{topo_m}</div></div>
    </section>"""
    reserve = f"""
    <section class="panel" aria-labelledby="h-rsv">
      <div class="ph"><h2 id="h-rsv">節點資源保留</h2><span class="sub">requests vs 可配置；Pending 的原因就在這</span></div>
      <div style="padding:12px 14px;display:flex;flex-direction:column;gap:12px">
        <div><div style="display:flex;justify-content:space-between"><span class="mono">lab-mini</span><span class="note">4 核 · 7.6 GiB 可配置</span></div>
          <div style="display:flex;gap:24px;flex-wrap:wrap;margin-top:6px">{U.ring(88, "3.5", "CPU requests /4", size=64)}{U.ring(79, "6.0G", "記憶體 requests /7.6", size=64)}{U.ring(96, "23", "Pod /24 上限", size=64, warn=85, danger=95)}
          <div style="display:flex;flex-direction:column;gap:4px;justify-content:center"><span class="note">實際使用</span>{U.meter(41, "CPU 41%")}{U.meter(62, "記憶體 62%")}</div></div></div>
        <div><div style="display:flex;justify-content:space-between"><span class="mono">lab-pi-01</span>{U.status("dng", "NotReady 18 分")}</div><span class="note">最後心跳 03:24 · 標籤 arch=arm64 · 污點 node.kubernetes.io/unreachable</span></div>
      </div>
    </section>"""
    def wl(ns, kind, name, ready, total, st, img, note):
        return (f'<a class="row srow acts-end" href="#wl"><span class="name"><span class="n">{name}</span><span class="d">{ns} · {kind} · {img}</span></span><span>{st}</span>'
                f'<span class="c3">{U.meter(ready / total * 100 if total else 0, f"{ready}/{total}", warn=101, danger=101)}</span><span class="c4 note">{note}</span>{U.actions(["log", "term", "restart", "more"])}</a>')
    workloads = f"""
    <section class="panel" aria-labelledby="h-wl" data-sort="0:asc" data-batch>
      <div class="ph"><h2 id="h-wl">工作負載 · 9</h2><div class="wrap" style="margin-left:auto"><span class="chip on">全部</span><span class="chip">Deployment 5</span><span class="chip">StatefulSet 2</span><span class="chip">DaemonSet 1</span><span class="chip">CronJob 1</span></div></div>
      <div class="row hd srow acts-end"><span>名稱</span><span>狀態</span><span class="c3">就緒副本</span><span class="c4">診斷</span><span></span></div>
      {wl("lab", "Deployment", "lab-api", 2, 2, U.status("ok", "就緒"), "ghcr.io/hazelnut/lab:7d02b4a", "—")}
      {wl("lab", "StatefulSet", "lab-db", 1, 1, U.status("ok", "就緒"), "postgres:16.4", "PVC local-path 42G")}
      {wl("shop-stg", "Deployment", "shop-api", 0, 1, U.status("dng", "CrashLoopBackOff"), "ghcr.io/hazelnut/shop-api:c91e0d7", "探針失敗：/healthz 500 · migration 不相容")}
      {wl("monitoring", "DaemonSet", "node-exporter", 1, 2, U.status("warn", "部分"), "prom/node-exporter:1.8", "lab-pi-01 NotReady")}
      {wl("lab", "CronJob", "lab-backup", 1, 1, U.status("ok", "上次 03:00 成功"), "hazelminx/backup-runner", "下次 11/08 03:00")}
      <div class="note" style="padding:8px 14px;border-top:1px solid var(--line)">診斷欄把排程、映像拉取、探針、掛載、DNS 的失敗原因寫成白話，原始事件可展開</div>
    </section>"""
    storage = f"""
    <section class="panel" aria-labelledby="h-kpv">
      <div class="ph"><h2 id="h-kpv">儲存類別與 PVC</h2></div>
      <div style="padding:12px 14px;display:flex;flex-direction:column;gap:10px">
        <div><span class="note">local-path（lab-mini /mnt/data）· 118 GiB</span>{U.tall_bar([(42, "lab-db", "var(--q5)"), (8, "lab-media", "var(--q4)"), (3, "prometheus", "var(--q3)")], 118)}</div>
        <div><span class="note">nfs（hazel-tw-01 /mnt/data/nfs）· 200 GiB</span>{U.tall_bar([(19, "lab-backup", "var(--q5)")], 200)}</div>
        <div class="note">RWO 5 · RWX 1 · 快照：local-path 不支援，nfs 由節點端快照代替</div>
      </div>
    </section>"""
    helm = f"""
    <section class="panel" aria-labelledby="h-helm">
      <div class="ph"><h2 id="h-helm">Helm Release · 3</h2><a class="more" href="#helm">全部 →</a></div>
      <div class="row hd srow"><span>Release</span><span>狀態</span><span class="c3">Chart</span><span class="c4">版本</span><span class="c4"></span><span class="c5"></span><span></span></div>
      <a class="row srow" href="#h1"><span class="name"><span class="n">kube-prometheus</span><span class="d">monitoring</span></span><span>{U.status("ok", "deployed")}</span><span class="mono c3">prometheus-community</span><span class="mono c4">65.1 → 66.2 可更新</span><span class="c4"></span><span class="c5"></span><span></span></a>
      <a class="row srow" href="#h2"><span class="name"><span class="n">cert-manager</span><span class="d">cert-manager</span></span><span>{U.status("ok", "deployed")}</span><span class="mono c3">jetstack</span><span class="mono c4">1.16.1</span><span class="c4"></span><span class="c5"></span><span></span></a>
      <a class="row srow" href="#h3"><span class="name"><span class="n">traefik</span><span class="d">kube-system（K3s 內建）</span></span><span>{U.status("info", "K3s 管理")}</span><span class="mono c3">—</span><span class="mono c4">27.0.2</span><span class="c4"></span><span class="c5"></span><span></span></a>
    </section>"""
    maint = f"""
    <section class="panel" aria-labelledby="h-km">
      <div class="ph"><h2 id="h-km">維護：升級 K3s 1.31.2 → 1.31.4</h2><span class="sub">單控制節點會中斷控制平面；工作負載照常跑</span></div>
      <div style="padding:12px 14px">{U.pipeline([("備份 datastore", "done", "48 秒", "sqlite → S3"), ("cordon + drain", "todo", "預估 2 分", "PDB 允許"), ("升級 agent", "todo", "預估 3 分", "lab-pi-01 先"), ("升級 server", "todo", "預估 4 分", "控制平面中斷"), ("驗證", "todo", "—", "探針＋Ingress")])}</div>
    </section>"""
    main = head + U.scope_bar([("lab-k3s", "K3s · lab-mini ＋ lab-pi-01"), ("dev", "kind · tw-01 Docker 裡"), ("＋ 連接既有叢集", ""), ("＋ 用 K3s 建叢集", "")], label="叢集") + tiles + topo_panel + f'<div class="cg">{reserve}{storage}</div>' + workloads + f'<div class="cg">{helm}{maint}</div>'
    return U.shell("k8s", main)


# ---------- 應用商店 ----------
def appstore() -> str:
    head = U.page_head("應用商店", OBS, '<button type="button" class="btn pri">同步商店</button><button type="button" class="btn">新增來源</button><button type="button" class="btn">本機範本</button>')
    tabs = ('<div class="panel"><div class="tabs" role="tablist"><button type="button" class="tab on" role="tab" aria-selected="true">已安裝 · 7</button><button type="button" class="tab" role="tab" aria-selected="false">可升級 · 3</button>'
            '<button type="button" class="tab" role="tab" aria-selected="false">全部應用 · 212</button><button type="button" class="tab" role="tab" aria-selected="false">來源與設定</button></div></div>')
    def app(ini, name, ver, node, st, meter_pct, meter_txt, note, upg=""):
        return (f'<a class="appcard" href="#app"><span class="ic" aria-hidden="true">{ini}</span><span class="bd"><span class="n">{name} <span class="note">{ver}</span></span><span class="d">{node}</span>{st}{U.meter(meter_pct, meter_txt)}<span class="d">{note}</span>{upg}</span></a>')
    installed = f"""
    <section class="panel" aria-labelledby="h-inst">
      <div class="ph"><h2 id="h-inst">已安裝 · 7</h2><span class="sub">量表＝該應用的記憶體；黃色＝有新版本</span><div class="wrap" style="margin-left:auto"><span class="chip on">全部節點</span><span class="chip">tw-01 5</span><span class="chip">jp-02 2</span></div></div>
      <div class="fleet">
        {app("Wk", "Wiki.js", "2.5.303", "jp-02 · 2 容器 · 3000 → wiki.example.tw", U.status("warn", "過期 · 上次觀測"), 35, "356 / 1024 MiB", "資料 /mnt/data/apps/wiki · 備份每日")}
        {app("Ol", "Ollama", "0.5.7", "tw-01 · 1 容器 · GPU L4 · 11434", U.status("ok", "執行中"), 54, "13.1 GiB VRAM", "模型 3 個 · 61 GiB", '<span class="chip on">0.6.1 可更新 · 看變更</span>')}
        {app("Pg", "PostgreSQL", "16.4", "tw-01 · shop-db · 僅內部", U.status("ok", "執行中"), 31, "1.29 / 4 GiB", "被 shop 使用")}
        {app("Rd", "Redis", "7.2", "tw-01 · shop-redis", U.status("info", "重啟中 · 第 1 次"), 24, "62 / 256 MiB", "被 shop 使用", '<span class="chip on">7.4 可更新</span>')}
        {app("Zt", "Zentinel", "0.6.54", "tw-01 · 原生 systemd", U.status("ok", "執行中"), 18, "94 MiB", "入口元件，更新走維護流程")}
        {app("Mr", "mail-relay", "1.2", "tw-01 · 1 容器 · 僅內部", U.status("ok", "執行中"), 8, "20 MiB", "通知用 SMTP 轉送", '<span class="chip on">1.3 可更新</span>')}
        {app("Up", "Uptime 探測", "2.0", "jp-02 · 1 容器", U.status("warn", "過期 · 上次觀測"), 12, "48 MiB", "對外探測 3 個站點")}
      </div>
    </section>"""
    upg = f"""
    <section class="panel" aria-labelledby="h-upg">
      <div class="ph"><h2 id="h-upg">可升級 · 3</h2><span class="sub">先看範本變更與映像差異，再決定；資料不動</span></div>
      <div class="row hd srow"><span>應用</span><span>版本</span><span class="c3">範本變更</span><span class="c4">映像</span><span class="c4">風險</span><span class="c5"></span><span></span></div>
      <a class="row srow" href="#u1"><span class="name"><span class="n">Ollama</span><span class="d">tw-01</span></span><span class="mono">0.5.7 → 0.6.1</span><span class="c3 note">+2 行：GPU device reservation 寫法改變</span><span class="c4 mono">ollama/ollama:0.6.1</span><span class="c4">{U.status("warn", "需重建容器")}</span><span class="c5"></span><span><button type="button" class="btn sm">看差異</button></span></a>
      <a class="row srow" href="#u2"><span class="name"><span class="n">Redis</span><span class="d">tw-01</span></span><span class="mono">7.2 → 7.4</span><span class="c3 note">無範本變更</span><span class="c4 mono">redis:7.4.1</span><span class="c4">{U.status("ok", "滾動")}</span><span class="c5"></span><span><button type="button" class="btn sm">看差異</button></span></a>
      <a class="row srow" href="#u3"><span class="name"><span class="n">mail-relay</span><span class="d">tw-01</span></span><span class="mono">1.2 → 1.3</span><span class="c3 note">+1 環境變數（預設值）</span><span class="c4 mono">hazelnut/mail-relay:1.3</span><span class="c4">{U.status("ok", "滾動")}</span><span class="c5"></span><span><button type="button" class="btn sm">看差異</button></span></a>
      <div class="note" style="padding:8px 14px;border-top:1px solid var(--line)">「忽略這個版本」會記住到下一版；Port 衝突在安裝前檢查，不在啟動時才失敗</div>
    </section>"""
    def catalog(ini, name, cat, engines, arch, dl, note):
        return (f'<a class="appcard" href="#c"><span class="ic" aria-hidden="true">{ini}</span><span class="bd"><span class="n">{name}</span><span class="d">{cat} · {note}</span><span class="wrap">{"".join(f"<span class=chip>{e}</span>" for e in engines)}{"".join(f"<span class=chip>{a}</span>" for a in arch)}</span><span class="d">{dl}</span></span></a>')
    allapps = f"""
    <section class="panel" aria-labelledby="h-all">
      <div class="ph"><h2 id="h-all">全部應用 · 212</h2><span class="sub">1Panel 官方商店 · 同步 03:30 · 完整</span><div class="wrap" style="margin-left:auto"><span class="chip on">全部</span><span class="chip">網站 48</span><span class="chip">資料庫 21</span><span class="chip">AI 17</span><span class="chip">工具 63</span><span class="chip">儲存 19</span><span class="chip">其他 44</span></div></div>
      <div class="wrap" style="padding:10px 14px 0"><label class="inp" style="flex:1;min-width:220px"><span class="note">搜尋</span><input placeholder="名稱、標籤、說明" style="background:transparent;border:0;flex:1;min-width:0"></label><span class="chip">只看支援 arm64</span><span class="chip">只看 Podman 可用</span><span class="chip">無已知漏洞</span></div>
      <div class="fleet">
        {catalog("Nc", "Nextcloud", "儲存", ["Docker", "Podman"], ["amd64", "arm64"], "31.0.2 · AGPL · 漏洞 0 已知", "需要 PostgreSQL／Redis，可重用現有").replace('href="#c"', 'href="AppInstall.dc.html"', 1)}
        {catalog("Gt", "Gitea", "開發", ["Docker", "Podman", "K3s"], ["amd64", "arm64"], "1.23.1 · MIT · 漏洞 0 已知", "")}
        {catalog("Im", "Immich", "媒體", ["Docker"], ["amd64"], "1.123 · AGPL · 1 中風險（CVE-2026-…）", "需要 GPU 才能硬體轉碼")}
        {catalog("Mn", "Minio", "儲存", ["Docker", "Podman"], ["amd64", "arm64"], "2026-10 · AGPL", "授權變更提醒：請讀來源說明")}
        {catalog("Uk", "Uptime Kuma", "監控", ["Docker", "Podman"], ["amd64", "arm64"], "2.0.0 · MIT", "")}
        {catalog("Vw", "Vaultwarden", "安全", ["Docker"], ["amd64", "arm64"], "1.33 · AGPL", "")}
      </div>
    </section>"""
    sources = f"""
    <section class="panel" aria-labelledby="h-src">
      <div class="ph"><h2 id="h-src">來源</h2><span class="sub">來源暫時失效時，已安裝的應用照常管理</span></div>
      <div class="row hd srow"><span>來源</span><span>狀態</span><span class="c3">應用數</span><span class="c4">上次同步</span><span class="c4">簽章</span><span class="c5"></span><span></span></div>
      <div class="row srow"><span class="name"><span class="n">1Panel 官方商店</span><span class="d">apps.fit2cloud.com</span></span><span>{U.status("ok", "正常")}</span><span class="mono c3">198</span><span class="mono c4">03:30</span><span class="c4">{U.status("ok", "有效")}</span><span class="c5"></span><span></span></div>
      <div class="row srow"><span class="name"><span class="n">hazelnut 私有來源</span><span class="d">git.example.tw/apps</span></span><span>{U.status("warn", "同步失敗 2 次")}</span><span class="mono c3">11</span><span class="mono c4">昨天 21:00</span><span class="c4">{U.status("ok", "有效")}</span><span class="c5"></span><span></span></div>
      <div class="row srow"><span class="name"><span class="n">本機範本</span><span class="d">/mnt/data/hazelminx/templates</span></span><span>{U.status("ok", "正常")}</span><span class="mono c3">3</span><span class="mono c4">—</span><span class="c4 note">不適用</span><span class="c5"></span><span></span></div>
    </section>"""
    preflight = f"""
    <section class="panel" aria-labelledby="h-pf">
      <div class="ph"><h2 id="h-pf">安裝 Nextcloud 的預檢</h2><span class="sub">安裝 = 一個工作：選節點、網路、Port、資料位置，相依服務可重用</span></div>
      <div style="padding:12px 14px">{U.pipeline([("選節點", "done", "tw-01", "Docker · amd64 OK"), ("相依", "done", "新建 PostgreSQL", "專用"), ("Port", "done", "8081 空", "不與 shop-api 衝突"), ("資料位置", "cur", "/mnt/data/apps", "剩 519 GiB"), ("入口", "todo", "cloud.example.tw", "憑證自動"), ("安裝", "todo", "—", "可看腳本")])}</div>
    </section>"""
    main = head + U.scope_bar([("全部節點", "7 應用"), ("hazel-tw-01", "Docker"), ("hazel-jp-02", "Docker · 過期"), ("lab-mini", "Podman · K3s")]) + tabs + installed + upg + allapps + f'<div class="cg">{sources}{preflight}</div>'
    return U.shell("store", main)


# ---------- API 代理 ----------
def _api_tabs(on):
    items = [("總覽", "APIProxy.dc.html"), ("服務 · 4", "APIProxyService.dc.html"), ("使用者與 Token", "APIProxyTokens.dc.html"), ("失敗紀錄", "APIProxyFailures.dc.html")]
    t = "".join(f'<a class="tab{" on" if i == on else ""}" role="tab" aria-selected="{"true" if i == on else "false"}" href="{h}">{n}</a>' for i, (n, h) in enumerate(items))
    return f'<div class="panel"><div class="tabs" role="tablist">{t}</div></div>'


def apiproxy() -> str:
    head = U.page_head("API 代理", OBS, '<button type="button" class="btn pri">新增服務</button><button type="button" class="btn">發 Token</button><button type="button" class="btn">匯入 infra-manager 資料</button>')
    tiles = '<div class="tiles">' + "".join([
        U.stat_tile("啟用服務", "3", "", "共 4 · 1 停用 · 較 30 天前 +1", None),
        U.stat_tile("啟用使用者 · Token", "5 · 7", "", "即將到期 2（7 天內 0）", False),
        U.stat_tile("今日請求", "18,420", "", "被拒 212（1.2%）", None, spark=[int(x) for x in walk(24, 760, 200, 200, 1300)]),
        U.stat_tile("p95 延遲", "184", "ms", "上游 162 · 代理 22", None, spark=walk(12, 180, 20, 140, 240)),
    ]) + "</div>"
    topo = U.topology([
        ("使用者 / Token", [("u1", "tim · 3 Token", "擁有者", "ok"), ("u2", "lab-bot · 2 Token", "到期 9 天", "warn"), ("u3", "partner-a · 1 Token", "限速 60/分", "ok")]),
        ("服務", [("s1", "/ollama", "推論 API · 限速", "ok"), ("s2", "/shop-admin", "需 Header 轉換", "ok"), ("s3", "/legacy-report", "已停用", "dim")]),
        ("上游", [("t1", "ollama:11434 @ tw-01", "健康", "ok"), ("t2", "shop-api:8080 @ tw-01", "健康", "ok"), ("t3", "report:9000 @ jp-02", "狀態過期", "warn")]),
    ], [("u1", "s1", .5, ""), ("u1", "s2", .3, ""), ("u2", "s1", .4, ""), ("u3", "s1", .2, ""), ("u3", "s3", .05, "dim"), ("s1", "t1", .8, ""), ("s2", "t2", .3, ""), ("s3", "t3", .05, "dim")])
    topo_m = U.flowlist([("tim", "/ollama", "50%"), ("lab-bot", "/ollama", "到期 9 天"), ("partner-a", "/ollama", "限速"), ("/ollama", "ollama:11434", "健康"), ("/shop-admin", "shop-api:8080", "健康"), ("/legacy-report", "report:9000", "停用")])
    topo_panel = f"""
    <section class="panel" aria-labelledby="h-at">
      <div class="ph"><h2 id="h-at">API 流向</h2><span class="sub">沿用 infra-manager 的路徑、驗證方式與回應碼；線寬＝今日請求佔比</span></div>
      <div style="padding:10px 14px 6px"><div class="ch-d">{topo}</div><div class="ch-m">{topo_m}</div></div>
    </section>"""
    def tok(name, owner, scope, days, used, limit):
        pct = min(100, days / 90 * 100); col = "var(--danger)" if days < 14 else ("var(--warn)" if days < 30 else "var(--ok)")
        return (f'<div class="rank" style="grid-template-columns:minmax(0,1.3fr) minmax(0,1fr) minmax(0,1fr) auto"><span class="mono" style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap">{name} <span class="note">· {owner} · {scope}</span></span>'
                f'<span class="bar" aria-hidden="true"><i style="width:{pct:.0f}%;background:{col}"></i></span>{U.meter(used / limit * 100, f"{used}/{limit} 次/分")}<span class="mono" style="font-size:12px;color:{col}">{days} 天</span></div>')
    tokens = f"""
    <section class="panel" aria-labelledby="h-tok">
      <div class="ph"><h2 id="h-tok">Token · 11</h2><span class="sub">左條＝剩餘效期（滿格 90 天）；右量表＝目前分鐘用量 / 上限</span><a class="more" href="#tokens">全部 →</a></div>
      <div class="ranks">
        {tok("hx_live_7f3a…", "lab-bot", "/ollama", 9, 58, 60)}
        {tok("hx_live_c21e…", "partner-a", "/ollama", 41, 12, 60)}
        {tok("hx_live_90bd…", "tim", "/ollama", 77, 3, 600)}
        {tok("hx_ci_118e…", "ci-runner", "/shop-admin", 12, 0, 120)}
      </div>
      <div class="note" style="padding:0 14px 10px">撤銷 Token 時可選「既有長連線立即斷」或「跑完這一次」；到期前 14 天通知擁有者</div>
    </section>"""
    usage = U.ts_card("今日請求 · 每小時", [U.Series("允許", [int(x) for x in walk(24, 700, 200, 200, 1300)], "var(--s1)"), U.Series("被拒", [int(x) for x in walk(24, 9, 6, 0, 40, {14: 61})], "var(--danger)")], HOURS, unit="",
                      bar=True, fmt_val=lambda v: f"{v:.0f}", markers=[(14, "lab-bot 超限")], foot="被拒原因在右表 · 上游 5xx 不算被拒，算失敗")
    rejected = f"""
    <section class="panel" aria-labelledby="h-rej">
      <div class="ph"><h2 id="h-rej">被拒 · 今日 212</h2><span class="sub">每一筆都能查到規則與 Token</span></div>
      {U.ranked([("超過限速（lab-bot /ollama）", 131, "131"), ("Token 已過期", 38, "38"), ("方法不允許（DELETE /shop-admin）", 22, "22"), ("路徑不在白名單", 14, "14"), ("上游拒絕（401 從 report:9000）", 7, "7")], vmax=140)}
    </section>"""
    def svc(path, st, upstream, auth, rate, users, lat):
        return (f'<a class="row srow" href="#svc"><span class="name"><span class="n">{path}</span><span class="d">{upstream}</span></span><span>{st}</span><span class="c3 note">{auth}</span><span class="c4 mono">{rate}</span><span class="c4 mono">{users}</span><span class="c5">{U.sparkline(lat)}</span><span class="mono">{lat[-1]:.0f} ms</span></a>')
    services = f"""
    <section class="panel" aria-labelledby="h-svcs" data-sort="0:asc">
      <div class="ph"><h2 id="h-svcs">服務 · 4</h2></div>
      <div class="row hd srow"><span>路徑</span><span>狀態</span><span class="c3">上游驗證 · Header</span><span class="c4">限速</span><span class="c4">使用者</span><span class="c5">p95 1 小時</span><span>現在</span></div>
      {svc("/ollama", U.status("ok", "啟用"), "ollama:11434 @ tw-01 · 串流 OK", "無 · 加 X-Forwarded-User", "60/分 · 10k/日", "3", walk(12, 180, 25, 140, 260))}
      {svc("/shop-admin", U.status("ok", "啟用"), "shop-api:8080 @ tw-01", "上游 Bearer（機密庫）· 去 Cookie", "600/分", "2", walk(12, 90, 12, 60, 130))}
      {svc("/files", U.status("ok", "啟用"), "hazelminx core · 上傳 ≤ 2 GiB", "平台 Token", "120/分", "1", walk(12, 40, 8, 20, 70))}
      {svc("/legacy-report", U.status("info", "停用"), "report:9000 @ jp-02", "Basic", "—", "0", [0] * 12)}
    </section>"""
    rng = '<div class="wrap"><span class="note">期間</span>' + _seg(["今天", "7 天", "30 天", "90 天", "自訂"], 0, "期間") + '<span class="note">使用量最多保留 90 天</span></div>'
    main = head + _api_tabs(0) + f'<div class="panel" style="padding:10px 14px">{rng}</div>' + tiles + topo_panel + f'<div class="cg">{tokens}{rejected}</div>' + usage + services
    return U.shell("api", main)


# ---------- API 代理 › 服務設定 ----------
def apiproxy_service() -> str:
    head = U.page_head("API 代理", OBS, '<button type="button" class="btn pri">新增服務</button><button type="button" class="btn">匯入 infra-manager 資料</button>')
    target = _fp("服務 /ollama · 目標", _grid(
        _fld("名稱（路徑第一段）", _in("ollama", mono=True), "客戶端呼叫 /use/ollama/…；舊客戶端的 /api/ollama/… 照樣可用"),
        _fld("類型", _in("LLM 推論"), "自由填寫，最多 64 字；總覽依類型分組"), _fld("描述", _in("實驗室共用的本地模型")),
        _fld("目標類型", _seg(["容器", "主機服務", "叢集服務", "外部網址"], 0, "目標類型")),
        _fld("目標", _dd("ollama · hazel-tw-01 · Port 11434/tcp（唯一的 HTTP Port）")),
        _fld("狀態", U.status("ok", "啟用 · 健康檢查 /api/tags 200")),
        _fld("外部網址允許清單", _in("選「外部網址」時才需要", ph=True), "擋雲端 metadata、內網與管理 API")))
    upstream = _fp("上游驗證與 Header", f"""
      <div class="row hd"><span>規則</span><span>Header</span><span>值</span><span></span></div>
      <div class="row"><span>{U.status("info", "加上")}</span><span class="mono">Authorization</span><span class="mono">Bearer ••••••（機密庫 ollama/upstream-key）</span><span>{U.actions(["more"])}</span></div>
      <div class="row"><span>{U.status("info", "加上")}</span><span class="mono">X-Forwarded-User</span><span class="mono">呼叫者的使用者名稱</span><span>{U.actions(["more"])}</span></div>
      <div class="row"><span>{U.status("none", "移除")}</span><span class="mono">Cookie</span><span class="mono">—</span><span>{U.actions(["more"])}</span></div>
      <div class="wrap" style="padding:10px 14px 12px"><button type="button" class="btn sm">{U.icon("plus", 14)}新增規則</button>{U.status("ok", "客戶端的代理 Token 不會傳到上游")}</div>""")
    limits = _fp("允許與限制", _grid(
        _fld("允許的方法", '<div class="wrap"><span class="chip on">GET</span><span class="chip on">POST</span><span class="chip">PUT</span><span class="chip">DELETE</span></div>'),
        _fld("允許的路徑", _in("/api/chat、/api/generate、/api/tags", mono=True), "其他路徑回 403，記在失敗紀錄"),
        _fld("限速", _in("60 次 / 分 · 每個 Token", mono=True), "多台主機時是全域計算"),
        _fld("用量上限", _in("10,000 次 / 日 · 每個使用者", mono=True)),
        _fld("上傳上限", _in("50 MiB", mono=True)), _fld("逾時", _in("連線 5 秒 · 回應 300 秒", mono=True), "串流回應不受回應逾時限制"),
        _fld("撤銷後的既有連線", _seg(["立即中斷", "這次跑完"], 1, "撤銷後"), "新請求一律立即拒絕")))
    def tr(code, cond, resp):
        return f'<div class="row"><span class="mono">{code}</span><span>{cond}</span><span class="note">{resp}</span></div>'
    compat = _fp("相容 infra-manager", f"""
      <div class="row hd"><span>回應碼</span><span>情況</span><span>回應</span></div>
      {tr("400", "路徑少於兩段且沒有 Bearer", "無效的API路徑")}
      {tr("404", "服務不存在或已停用", "找不到服務")}
      {tr("403", "Token 無效、過期，或使用者停權", "無效的Token、Token已過期、用戶已被停權")}
      {tr("502", "上游連不到", "記為上游連線失敗")}
      <div class="wrap" style="padding:10px 14px 12px">{U.status("ok", "/use/ 讀 Bearer；/api/ 只讀網址裡的 Token，與舊版相同")}</div>""")
    def sv(name, typ, tgt, st, req, ok, users, toks, last, on=False):
        return (f'<a class="row srow{" sel" if on else ""}" href="#svc"><span class="name"><span class="n mono">/{name}</span><span class="d">{typ} · {tgt}</span></span><span>{st}</span>'
                f'<span class="c3 mono">{req}</span><span class="c3 mono">{ok}</span><span class="c4 mono">{users} · {toks}</span><span class="c4">{last}</span><span></span></a>')
    lst = f"""
    <section class="panel" aria-labelledby="h-svl" data-sort="2:desc">
      <div class="ph"><h2 id="h-svl">服務 · 4</h2><label class="inp" style="margin-left:auto;min-width:min(220px,100%)">{U.icon("search", 14)}<input placeholder="搜尋服務" style="background:transparent;border:0;flex:1;min-width:0"></label></div>
      <div class="row hd srow"><span>服務</span><span>狀態</span><span class="c3">今日請求</span><span class="c3">正常回應率</span><span class="c4">使用者 · Token</span><span class="c4">最後使用</span><span></span></div>
      {sv("ollama", "LLM 推論", "容器 ollama @ tw-01", U.status("ok", "啟用"), "14,880", "99.2%", 3, 4, "剛剛", True)}
      {sv("shop-admin", "管理 API", "容器 shop-api @ tw-01", U.status("ok", "啟用"), "3,410", "98.1%", 2, 3, "3 小時前")}
      {sv("files", "檔案", "主機服務 hazelminx core", U.status("ok", "啟用"), "130", "100%", 1, 1, "昨天")}
      {sv("legacy-report", "報表", "容器 report @ jp-02", U.status("none", "停用"), "0", "—", 0, 1, "41 天前")}
    </section>"""
    def pp(port, src, probe, st, on=False, dis=""):
        radio = (f'<span tabindex="0" data-tip="{dis}"><input type="radio" name="pp" disabled aria-label="{port}"></span>' if dis
                 else f'<input type="radio" name="pp"{" checked" if on else ""} aria-label="{port}">')
        return (f'<div class="row"><span class="wrap" style="flex-wrap:nowrap">{radio}<span class="mono">{port}</span></span>'
                f'<span class="note">{src}</span><span class="mono">{probe}</span><span>{st}</span></div>')
    picker = _fp("新增服務 · 選擇目標", f"""
      <div style="padding:12px 16px 4px">{_grid(_fld("目標類型", _seg(["容器", "主機服務", "叢集服務", "外部網址"], 0, "目標類型")), _fld("容器或部署單元的服務", _dd("minio · Podman（deploy）@ lab-mini")), minw=280)}</div>
      <div class="row hd"><span>Port</span><span>從哪裡知道</span><span>探測</span><span>可以代理</span></div>
      {pp("9000/tcp", "映像 EXPOSE · 有監聽", "HTTP 403 /", U.status("ok", "HTTP · S3 API"), True)}
      {pp("9001/tcp", "compose ports · 有監聽", "HTTP 200 /", U.status("ok", "HTTP · 管理介面"))}
      {pp("9090/tcp", "有監聽 · 沒宣告", "HTTP 200 /metrics", U.status("info", "HTTP · 只在容器內"))}
      {pp("7946/udp", "映像 EXPOSE", "—", U.status("none", "UDP · 不能代理"), dis="API 代理只轉 HTTP；UDP 不能選")}
      <div style="padding:10px 16px 12px;display:flex;flex-direction:column;gap:8px">
        <div class="wrap">{U.status("info", "一個服務對一個 Port")}<span tabindex="0" data-tip="要同時開放 S3 API 和管理介面，就建兩個服務，例如 /minio 與 /minio-console">{U.icon("info", 14)}</span></div>
        <div class="wrap"><span class="note">上游路徑前綴</span><span style="flex:0 1 220px">{_in("/", mono=True)}</span><span class="note">呼叫 /use/minio/bucket → 轉到 9000/bucket</span></div>
      </div>""")
    def dom(host, maps, cert, st):
        return f'<div class="row"><span class="mono">{host}</span><span class="mono">{maps}</span><span>{cert}</span><span>{st}</span><span>{U.actions(["more"])}</span></div>'
    domains = _fp("專屬網址", f"""
      <div class="row hd"><span>網址</span><span>對應</span><span>憑證</span><span>DNS</span><span></span></div>
      {dom("api.example.tw", "整個 API 代理 · /use/&lt;服務&gt;/…", U.status("ok", "自動 · 剩 82 天"), U.status("ok", "已指向入口"))}
      {dom("ollama.api.example.tw", "只到 /ollama · 不用寫 /use/ollama", U.status("ok", "自動 · 剩 82 天"), U.status("ok", "已指向入口"))}
      {dom("llm.lab.example.tw", "只到 /ollama", U.status("warn", "等 DNS"), U.status("warn", "還沒指向 · 新增 A 紀錄 203.0.113.22"))}
      <div style="padding:10px 16px 12px;display:flex;flex-direction:column;gap:8px">
        <div class="term">curl https://ollama.api.example.tw/api/tags -H "Authorization: Bearer hx_live_…"</div>
        <div class="wrap"><button type="button" class="btn sm">{U.icon("plus", 14)}新增網址</button>{U.status("info", "服務專屬網址只收 Bearer")}<span tabindex="0" data-tip="網址裡沒有 /use/服務 這一段，所以不支援把 Token 放在路徑；/use/ 與 /api/ 的舊路徑照常可用">{U.icon("info", 14)}</span></div>
      </div>""")
    main = head + _api_tabs(1) + lst + domains + picker + f'<div class="cg">{target}{upstream}</div>' + f'<div class="cg">{limits}{compat}</div>' + U.savebar("/ollama：2 處未儲存", "儲存後新請求立即套用", "儲存")
    return U.shell("api", main)


# ---------- API 代理 › 使用者與 Token ----------
def apiproxy_tokens() -> str:
    head = U.page_head("API 代理", OBS, '<button type="button" class="btn pri">發 Token</button><button type="button" class="btn">新增使用者</button>')
    def user(name, st, toks, svcs, last):
        return (f'<div class="row srow"><span class="name"><span class="n">{name}</span><span class="d">{svcs}</span></span><span>{st}</span>'
                f'<span class="c3 mono">{toks}</span><span class="c4">{last}</span><span class="c4"></span><span class="c5"></span><span>{U.actions(["more"])}</span></div>')
    users = f"""
    <section class="panel" aria-labelledby="h-au">
      <div class="ph"><h2 id="h-au">API 使用者 · 6</h2><span class="sub">只能呼叫 API，不能登入面板</span></div>
      <div class="row hd srow"><span>使用者</span><span>狀態</span><span class="c3">Token</span><span class="c4">最後使用</span><span class="c4"></span><span class="c5"></span><span></span></div>
      {user("tim", U.status("ok", "啟用"), "2", "/ollama、/shop-admin", "2 分鐘前")}
      {user("lab-bot", U.status("ok", "啟用"), "2", "/ollama", "剛剛")}
      {user("partner-a", U.status("ok", "啟用"), "1", "/ollama", "1 小時前")}
      {user("ci-runner", U.status("ok", "啟用"), "1", "/shop-admin", "3 小時前")}
      {user("partner-b", U.status("ok", "啟用"), "1", "/ollama", "6 天前")}
      {user("old-intern", U.status("none", "停權"), "1", "/legacy-report", "41 天前")}
    </section>"""
    def tok(pre, who, svc, note, exp, used, st):
        return (f'<div class="row srow"><span class="name"><span class="n mono">{pre}</span><span class="d">{note}</span></span><span>{st}</span>'
                f'<span class="c3">{who}</span><span class="c3 mono">{svc}</span><span class="c4">{exp}</span><span class="c4">{used}</span>'
                f'<span>{U.actions(["stop", "more"])}</span></div>')
    toks = f"""
    <section class="panel" aria-labelledby="h-tk" data-batch>
      <div class="ph"><h2 id="h-tk">Token · 11</h2><span class="sub">一個 Token 只對一個服務；完整值只在發出時顯示一次</span></div>
      <div class="wrap" style="padding:10px 14px;border-bottom:1px solid var(--line)"><label class="inp" style="flex:1;min-width:min(220px,100%)">{U.icon("search", 14)}<input placeholder="搜尋 Token、使用者或備註" style="background:transparent;border:0;flex:1;min-width:0"></label><span style="flex:0 1 170px;min-width:140px">{_dd("使用者：全部")}</span><span style="flex:0 1 170px;min-width:140px">{_dd("服務：全部")}</span><span class="chip on">全部</span><span class="chip">啟用 7</span><span class="chip">停用 1</span><span class="chip">已過期 2</span><span class="chip">失效 1</span><span class="chip">14 天內到期 2</span></div>
      <div class="row hd srow"><span>Token</span><span>狀態</span><span class="c3">使用者</span><span class="c3">服務</span><span class="c4">到期</span><span class="c4">今日用量</span><span></span></div>
      {tok("hx_live_7f3a…", "lab-bot", "/ollama", "實驗室排程", U.status("warn", "9 天後"), U.meter(58 / 60 * 100, "58/60 次/分"), U.status("ok", "啟用"))}
      {tok("hx_live_c21e…", "partner-a", "/ollama", "合作夥伴試用", "41 天後", U.meter(20, "12/60 次/分"), U.status("ok", "啟用"))}
      {tok("hx_live_90bd…", "tim", "/ollama", "筆電", "77 天後", U.meter(1, "3/600 次/分"), U.status("ok", "啟用"))}
      {tok("hx_live_4e77…", "tim", "/shop-admin", "筆電", "77 天後", U.meter(0, "0/600 次/分"), U.status("none", "停用"))}
      {tok("hx_live_b902…", "partner-b", "/ollama", "外洩後作廢", "—", "—", U.status("dng", "失效"))}
      {tok("hx_ci_118e…", "ci-runner", "/shop-admin", "GitHub Actions", "12 天後", U.meter(0, "0/120 次/分"), U.status("ok", "啟用"))}
      {tok("hx_live_0a1c…", "old-intern", "/legacy-report", "—", "已過期", "—", U.status("none", "已過期"))}
    </section>"""
    issue = _fp("發 Token", _grid(
        _fld("使用者", _dd("lab-bot")), _fld("服務", _dd("/ollama"), "一個 Token 對一個服務，與 infra-manager 相同"),
        _fld("到期", _seg(["30 天", "90 天", "1 年", "自訂"], 1, "到期")), _fld("備註", _in("實驗室排程 v2")),
        _fld("限速覆寫", _in("沿用服務設定", ph=True)), _fld("來源 IP", _in("10.8.0.0/24", mono=True), "留空＝不限")) +
        '<div class="wrap" style="padding:0 16px 14px;justify-content:flex-end"><button type="button" class="btn pri">發出</button></div>')
    shown = _fp("Token 已發出 · 只顯示這一次", f"""
      <div style="padding:12px 16px;display:flex;flex-direction:column;gap:10px">
        <div class="term">hx_live_5d2e9c0f6a7b41d8b3e2c9a0f1e6d7c4</div>
        <div class="wrap"><button type="button" class="btn">{U.icon("copy", 14)}複製</button>{U.status("warn", "關掉後無法再看到完整值")}</div>
      </div>""")
    main = head + _api_tabs(2) + users + toks + f'<div class="cg">{issue}{shown}</div>'
    return U.shell("api", main)


# ---------- API 代理 › 失敗紀錄 ----------
def apiproxy_failures() -> str:
    head = U.page_head("API 代理", OBS, '<button type="button" class="btn">匯出 CSV</button>')
    kinds = [("驗證失敗", 188, "var(--warn)"), ("上游回應失敗", 11, "var(--danger)"), ("上游連線失敗", 9, "var(--danger)"), ("傳輸中斷", 4, "var(--s2)"), ("本系統錯誤", 0, "var(--s3)")]
    seg = U.ranked([(f"{n}", v, str(v)) for n, v, _ in kinds])
    summary = f"""
    <section class="panel" aria-labelledby="h-fs">
      <div class="ph"><h2 id="h-fs">今日失敗 · 212</h2><span class="sub">最多保留 90 天或 100,000 筆，滿了刪最舊的</span></div>
      {seg}
    </section>"""
    def fr(t, kind, reason, svc, who, code, up):
        return (f'<div class="row srow"><span class="name"><span class="n mono">{t}</span><span class="d">{who}</span></span><span>{kind}</span>'
                f'<span class="c3">{reason}</span><span class="c3 mono">{svc}</span><span class="c4 mono">{code}</span><span class="c4 mono">{up}</span><span></span></div>')
    rows = f"""
    <section class="panel" aria-labelledby="h-fl" data-pages="1–6 / 212">
      <div class="ph"><h2 id="h-fl">失敗紀錄</h2></div>
      <div class="wrap" style="padding:10px 14px;border-bottom:1px solid var(--line)"><span style="flex:0 1 180px;min-width:150px">{_dd("類型：全部")}</span><span style="flex:0 1 180px;min-width:150px">{_dd("服務：全部")}</span><span style="flex:0 1 150px;min-width:130px">{_dd("回應碼：全部")}</span><span style="flex:0 1 220px;min-width:180px">{_dd("11/08 00:00 – 現在")}</span><button type="button" class="btn">查詢</button></div>
      <div class="row hd srow"><span>時間 · 呼叫者</span><span>類型</span><span class="c3">原因</span><span class="c3">服務</span><span class="c4">回應碼</span><span class="c4">上游回應碼</span><span></span></div>
      {fr("03:41:12", U.status("warn", "驗證失敗"), "超過限速（60 次/分）", "/ollama", "lab-bot · Token #12", "429", "—")}
      {fr("03:40:55", U.status("warn", "驗證失敗"), "Token 已過期", "/legacy-report", "old-intern · Token #3", "403", "—")}
      {fr("03:39:31", U.status("warn", "驗證失敗"), "Token 無效或已停用", "/ollama", "—", "403", "—")}
      {fr("03:38:20", U.status("warn", "驗證失敗"), "方法不允許（DELETE）", "/shop-admin", "ci-runner · Token #9", "403", "—")}
      {fr("03:21:07", U.status("dng", "上游連線失敗"), "連線逾時", "/legacy-report", "tim · Token #5", "502", "—")}
      {fr("03:05:44", U.status("dng", "上游連線失敗"), "上游 TLS 驗證失敗", "/shop-admin", "ci-runner · Token #9", "502", "—")}
      {fr("02:58:41", U.status("dng", "上游回應失敗"), "上游回傳錯誤", "/shop-admin", "tim · Token #6", "500", "500")}
      {fr("02:12:03", U.status("info", "傳輸中斷"), "傳輸中斷，來源未確認", "/ollama", "partner-a · Token #11", "200", "200")}
      <div class="note" style="padding:8px 14px 12px">不記錄 Token 值與請求路徑 · 傳輸中斷可能發生在回應送出之後，所以回應碼可能是 200</div>
    </section>"""
    main = head + _api_tabs(3) + summary + rows
    return U.shell("api", main)


# ---------- 容器 › 映像、儲存卷、網路與快取 ----------
def _locked_acts(keys, why):
    """列尾動作：刪除停用並說明原因（ROB-003）。"""
    return (U.actions(keys).replace("</span>", "", 1).rstrip()
            + f'<span tabindex="0" data-tip="{why}"><button type="button" class="ib" disabled aria-label="刪除（{why}）">{U.icon("trash")}</button></span>'
            + U.actions(["more"]).replace('<span class="acts">', "", 1))


def container_resources() -> str:
    head = U.page_head("容器資源", OBS, '<button type="button" class="btn pri">拉取映像</button><button type="button" class="btn">建置映像</button><button type="button" class="btn">清理…</button>')
    tabs = ('<div class="panel"><div class="tabs" role="tablist"><a class="tab" role="tab" aria-selected="false" href="Containers.dc.html">容器 · 16</a>'
            '<button type="button" class="tab on" role="tab" aria-selected="true">映像 · 23</button><button type="button" class="tab" role="tab" aria-selected="false">儲存卷 · 9</button>'
            '<button type="button" class="tab" role="tab" aria-selected="false">網路 · 6</button><button type="button" class="tab" role="tab" aria-selected="false">建置快取</button>'
            '<button type="button" class="tab" role="tab" aria-selected="false">登錄站 · 2</button></div></div>')
    def du(kind, total, used, recl, pct):
        return (f'<div class="row"><span>{kind}</span><span class="mono">{total}</span><span>{U.meter(pct, used)}</span><span class="mono">{recl}</span></div>')
    space = _fp("空間 · Docker（root）@ tw-01", f"""
      <div class="row hd"><span>類型</span><span>總共</span><span>使用中</span><span>可回收</span></div>
      {du("映像", "15.1 GiB", "9.8 GiB · 16 個", "4.1 GiB · 7 個", 65)}
      {du("容器可寫層", "1.3 GiB", "1.2 GiB · 14 個", "0.1 GiB · 2 個已停止", 92)}
      {du("儲存卷", "318 GiB", "316 GiB · 7 個", "2.0 GiB · 2 個沒被使用", 99)}
      {du("建置快取", "6.4 GiB", "—", "6.4 GiB", 0)}
      {du("容器日誌", "3.1 GiB", "3.1 GiB", "依日誌保留設定自動清", 100)}""")
    prune = _fp("清理預覽", f"""
      <div style="padding:12px 16px;display:flex;flex-direction:column;gap:10px">
        <div class="wrap">{_cb("未使用的映像（7 · 4.1 GiB）", True)}{_cb("建置快取（6.4 GiB）", True)}{_cb("已停止的容器（2）", False)}{_cb("沒被使用的儲存卷（2 · 2.0 GiB）", False, "資料會消失")}</div>
        <div class="wrap">{_cb("保留回滾要用的映像", True, "shop-api:88b1d02 仍是上一個可回滾版本")}{_cb("只清超過 7 天的", True)}</div>
        <div class="wrap" style="justify-content:space-between">{U.status("ok", "預計回收 9.7 GiB")}<button type="button" class="btn pri">清理</button></div>
      </div>""")
    def img(repo, tag, digest, size, used_by, created, st):
        return (f'<div class="row srow"><span class="name"><span class="n mono">{repo}:{tag}</span><span class="d mono">{digest}</span></span><span>{st}</span>'
                f'<span class="c3">{used_by}</span><span class="c4 mono">{size}</span><span class="c4">{created}</span><span class="c5"></span>'
                f'<span>{U.actions(["copy", "download", "trash", "more"]) if used_by == "—" else _locked_acts(["copy", "download"], "有容器在用，不能刪除")}</span></div>')
    images = f"""
    <section class="panel" aria-labelledby="h-imgs" data-sort="3:desc" data-batch data-pages="1–6 / 23">
      <div class="ph"><h2 id="h-imgs">映像 · 23</h2><div class="wrap" style="margin-left:auto"><span class="chip on">全部</span><span class="chip">使用中 16</span><span class="chip">未使用 7</span><span class="chip">懸空 3</span><span class="chip">有更新 2</span></div></div>
      <div class="row hd srow"><span>映像</span><span>狀態</span><span class="c3">誰在用</span><span class="c4">大小</span><span class="c4">建立</span><span class="c5"></span><span></span></div>
      {img("ollama/ollama", "0.5.7", "sha256:91c2…", "2.9 GiB", "ollama", "41 天前", U.status("info", "有更新 0.6.1"))}
      {img("ghcr.io/hazelnut/shop-api", "a3f9c1e", "sha256:4be1…", "812 MiB", "shop-api ×2", "今天", U.status("ok", "使用中"))}
      {img("ghcr.io/hazelnut/shop-api", "88b1d02", "sha256:7a03…", "810 MiB", "—", "3 天前", U.status("none", "回滾用 · 保留"))}
      {img("postgres", "16.4", "sha256:c1e8…", "430 MiB", "shop-db、nextcloud-db", "2 個月前", U.status("ok", "使用中"))}
      {img("node", "22", "sha256:0d42…", "1.1 GiB", "—", "12 天前", U.status("none", "未使用"))}
      {img("&lt;none&gt;", "&lt;none&gt;", "sha256:e5b0…", "640 MiB", "—", "20 天前", U.status("none", "懸空"))}
    </section>"""
    def vol(name, mp, size, used_by, drv, st):
        return (f'<div class="row srow"><span class="name"><span class="n mono">{name}</span><span class="d mono">{mp}</span></span><span>{st}</span>'
                f'<span class="c3">{used_by}</span><span class="c4 mono">{size}</span><span class="c4">{drv}</span><span class="c5"></span>'
                f'<span>{U.actions(["download", "trash", "more"]) if used_by == "—" else _locked_acts(["download"], "有容器在用，不能刪除")}</span></div>')
    vols = f"""
    <section class="panel" aria-labelledby="h-vols" data-batch>
      <div class="ph"><h2 id="h-vols">儲存卷 · 9</h2><div class="wrap" style="margin-left:auto"><button type="button" class="btn sm">{U.icon("plus", 14)}建立儲存卷</button></div></div>
      <div class="row hd srow"><span>儲存卷</span><span>狀態</span><span class="c3">誰在用</span><span class="c4">大小</span><span class="c4">驅動</span><span class="c5"></span><span></span></div>
      {vol("shop-production-web_uploads", "/var/lib/docker/volumes/…/_data", "84 GiB", "shop-api ×2", "local", U.status("ok", "使用中"))}
      {vol("nextcloud-db_data", "/var/lib/docker/volumes/…/_data", "1.2 GiB", "nextcloud-db", "local", U.status("ok", "使用中"))}
      {vol("old-report_cache", "/var/lib/docker/volumes/…/_data", "1.8 GiB", "—", "local", U.status("none", "沒被使用"))}
      {vol("nfs-media", "nas.lan:/export/media", "—", "wiki-app", "local · nfs", U.status("ok", "使用中"))}
    </section>"""
    def net(name, drv, subnet, members, st, sys_=False):
        acts = f'<span tabindex="0" data-tip="引擎內建網路，不能刪除">{U.actions(["more"])}</span>' if sys_ else U.actions(["trash", "more"])
        return (f'<div class="row srow"><span class="name"><span class="n mono">{name}</span><span class="d">{drv}</span></span><span>{st}</span>'
                f'<span class="c3">{members}</span><span class="c4 mono">{subnet}</span><span class="c4"></span><span class="c5"></span><span>{acts}</span></div>')
    nets = f"""
    <section class="panel" aria-labelledby="h-nets">
      <div class="ph"><h2 id="h-nets">網路 · 6</h2><div class="wrap" style="margin-left:auto"><button type="button" class="btn sm">{U.icon("plus", 14)}建立網路</button></div></div>
      <div class="row hd srow"><span>網路</span><span>狀態</span><span class="c3">裡面的容器</span><span class="c4">子網</span><span class="c4"></span><span class="c5"></span><span></span></div>
      {net("shop-production-web_default", "bridge · 部署單元 web", "172.18.0.0/16", "shop-api ×2、shop-db、shop-redis", U.status("ok", "使用中"))}
      {net("nextcloud_default", "bridge · 應用 nextcloud", "172.21.0.0/16", "nextcloud、nextcloud-db、nextcloud-redis", U.status("ok", "使用中"))}
      {net("hzx-ollama", "bridge · 平台元件", "172.24.0.0/16", "ollama、nextcloud", U.status("ok", "使用中"))}
      {net("old_default", "bridge", "172.30.0.0/16", "—", U.status("none", "沒有容器"))}
      {net("bridge", "bridge · 引擎內建", "172.17.0.0/16", "—", U.status("none", "內建"), True)}
      {net("host", "host · 引擎內建", "—", "—", U.status("none", "內建"), True)}
    </section>"""
    regs = _fp("登錄站 · 2", f"""
      <div class="row hd"><span>登錄站</span><span>帳號</span><span>狀態</span><span></span></div>
      <div class="row"><span class="mono">ghcr.io</span><span>hazelnut 組織</span><span>{U.status("ok", "已登入 · Token 61 天到期")}</span><span>{U.actions(["more"])}</span></div>
      <div class="row"><span class="mono">registry.example.tw</span><span>deploy</span><span>{U.status("ok", "已登入 · 鏡像 docker.io")}</span><span>{U.actions(["more"])}</span></div>""")
    main = (head + U.scope_bar([("Docker（root）@ tw-01", "15.1 GiB 映像"), ("Docker（root）@ jp-02", "過期"), ("Podman（deploy）@ lab-mini", "")], 0, "引擎環境")
            + tabs + f'<div class="cg">{space}{prune}</div>' + images + vols + nets + regs)
    return U.shell("docker", main)


# ---------- 資料庫 ----------
def database() -> str:
    head = U.page_head("資料庫", OBS, '<button type="button" class="btn pri">安裝資料庫</button><button type="button" class="btn">連接既有</button><button type="button" class="btn">SQL 主控台</button>')
    tabs = ('<div class="panel"><div class="tabs" role="tablist"><button type="button" class="tab on" role="tab" aria-selected="true">全部 · 5</button><button type="button" class="tab" role="tab" aria-selected="false">PostgreSQL 2</button>'
            '<button type="button" class="tab" role="tab" aria-selected="false">MySQL 1</button><button type="button" class="tab" role="tab" aria-selected="false">Redis 1</button><button type="button" class="tab" role="tab" aria-selected="false">MongoDB 1</button></div></div>')
    def inst(ini, name, ver, node, st, conn, connmax, size, sizemax, expose, bk, note):
        return (f'<a class="ncard" href="#db"><div class="nh"><span class="n">{name}</span>{st}<span class="d">{ver} · {node}</span></div>'
                f'<div class="rings">{U.ring(conn / connmax * 100, str(conn), f"連線 /{connmax}")}{U.ring(size / sizemax * 100, f"{size}G", f"容量 /{sizemax}G", warn=80, danger=90)}{U.ring(bk, "今天" if bk == 100 else "—", "備份", warn=101, danger=101)}</div>'
                f'<div class="ft">{expose}<span style="margin-left:auto" class="note">{note}</span></div></a>')
    instances = f"""
    <section class="panel" aria-labelledby="h-dbi">
      <div class="ph"><h2 id="h-dbi">實例 · 5</h2><span class="sub">服務生命週期與資料內容權限分開管；對外開放一律標出來</span></div>
      <div class="fleet">
        {inst("Pg", "shop-db", "PostgreSQL 16.4", "tw-01 · 容器", U.status("ok", "健康"), 23, 100, 84, 200, U.status("ok", "僅內部"), 100, "被 shop 使用")}
        {inst("Pg", "wiki-db", "PostgreSQL 16.4", "jp-02 · 容器", U.status("warn", "過期 · 上次觀測"), 8, 100, 42, 100, U.status("dng", "5432 對外"), 100, "")}
        {inst("My", "legacy-mysql", "MySQL 8.0.39", "jp-02 · 遠端（非容器）", U.status("ok", "健康"), 4, 150, 12, 50, U.status("info", "遠端"), 0, "無備份任務")}
        {inst("Rd", "shop-redis", "Redis 7.2", "tw-01 · 容器", U.status("info", "重啟中 · 第 1 次"), 11, 10000, 0.06, 0.25, U.status("ok", "僅內部"), 100, "AOF 每秒")}
        {inst("Mg", "wiki-mongo", "MongoDB 7.0", "lab-mini · 容器", U.status("info", "維護中"), 0, 100, 3, 20, U.status("ok", "僅內部"), 100, "實例不支援交易")}
      </div>
    </section>"""
    conn = U.ts_card("shop-db · 連線與查詢", [U.Series("連線", [int(x) for x in walk(24, 20, 5, 5, 60, {22: 48})], "var(--s1)"), U.Series("查詢／秒 ÷10", [x / 10 for x in walk(24, 180, 40, 60, 400, {22: 390})], "var(--s2)")],
                     HOURS, unit="", vmax=60, fmt_val=lambda v: f"{v:.0f}", markers=[(22, "a3f9c1e 部署")], foot="兩個序列共用同一軸（查詢／秒已除以 10）· 上限 100 連線")
    slow = f"""
    <section class="panel" aria-labelledby="h-slow">
      <div class="ph"><h2 id="h-slow">shop-db · 慢查詢 · 24 小時</h2><span class="sub">依總耗時</span></div>
      {U.ranked([("SELECT … FROM orders WHERE status = $1 ORDER BY…", 48, "48 秒 · 312 次"), ("UPDATE inventory SET qty = … WHERE sku IN (…)", 21, "21 秒 · 88 次"), ("SELECT count(*) FROM events", 12, "12 秒 · 1,400 次"), ("VACUUM ANALYZE orders", 9, "9 秒 · 1 次")], vmax=50)}
      <div class="note" style="padding:0 14px 10px">第一條缺 <span class="mono">orders(status, created_at)</span> 索引 · 建索引走變更計畫</div>
    </section>"""
    dbs = f"""
    <section class="panel" aria-labelledby="h-dbs" data-sort="0:asc">
      <div class="ph"><h2 id="h-dbs">shop-db · 資料庫與帳號</h2><a class="more" href="#browse">瀏覽資料 →</a></div>
      <div style="padding:12px 14px;display:flex;flex-direction:column;gap:8px">
        <span class="note">容量 84 GiB</span>{U.tall_bar([(61, "shop", "var(--q5)"), (14, "nextcloud", "var(--q4)"), (6, "shop_analytics", "var(--q3)"), (3, "postgres / 系統", "var(--q2)")], 84)}
      </div>
      <div class="row hd srow"><span>帳號</span><span>權限</span><span class="c3">資料庫</span><span class="c4">來源限制</span><span class="c5">最後登入</span><span class="c5"></span><span></span></div>
      <div class="row srow"><span class="name"><span class="n">shop_app</span><span class="d">應用帳號</span></span><span class="note">讀寫 shop</span><span class="mono c3">shop</span><span class="mono c4">容器網路</span><span class="mono c4">2 秒前</span><span class="c5"></span><span></span></div>
      <div class="row srow"><span class="name"><span class="n">nextcloud</span><span class="d">應用帳號</span></span><span class="note">讀寫 nextcloud</span><span class="mono c3">nextcloud</span><span class="mono c4">容器網路</span><span class="mono c4">1 分前</span><span class="c5"></span><span></span></div>
      <div class="row srow"><span class="name"><span class="n">analyst</span><span class="d">人員 · tim 建立</span></span><span class="note">唯讀 shop_analytics</span><span class="mono c3">shop_analytics</span><span class="mono c4">僅 Tunnel</span><span class="mono c4">3 天前</span><span class="c5"></span><span></span></div>
      <div class="row srow"><span class="name"><span class="n">postgres</span><span class="d">超級使用者</span></span><span class="note">全部</span><span class="mono c3">*</span><span class="mono c4">僅本機</span><span class="mono c4">備份 03:00</span><span class="c5"></span><span></span></div>
    </section>"""
    schema = f"""
    <section class="panel" aria-labelledby="h-sch">
      <div class="ph"><h2 id="h-sch">shop · 結構</h2><span class="sub">點資料表進資料列檢視（篩選、排序、分頁、安全編輯）</span></div>
      <div style="padding:8px 14px">{U.tree([(0, "public", "18 表", False), (1, "orders", "2.1M 列 · 31G", True), (1, "order_items", "7.8M 列 · 19G", False), (1, "inventory", "41k 列 · 120M", False), (1, "events", "11M 列 · 9G", False), (0, "views", "4", False), (1, "v_daily_sales", "", False), (0, "索引", "41", False)])}</div>
    </section>"""
    redis = f"""
    <section class="panel" aria-labelledby="h-rd">
      <div class="ph"><h2 id="h-rd">shop-redis · 現在</h2><span class="sub">單機 · AOF 每秒 · ACL 3 使用者 · FLUSHALL 已禁用</span></div>
      <div style="padding:12px 14px;display:flex;gap:20px;flex-wrap:wrap">{U.ring(24, "62M", "記憶體 /256M")}{U.ring(94, "94%", "命中率", warn=101, danger=101)}{U.ring(0, "0", "淘汰／分", warn=1, danger=50)}{U.ring(11, "11", "客戶端 /10k", warn=101, danger=101)}
        <div style="display:flex;flex-direction:column;gap:4px;min-width:200px"><span class="note">慢查詢 24 小時</span>{U.sparkline(walk(24, 2, 1.5, 0, 8), w=180)}<span class="note">最慢 KEYS * 1.2 秒（已列入限制指令）</span></div></div>
    </section>"""
    audit = f"""
    <section class="panel" aria-labelledby="h-dba">
      <div class="ph"><h2 id="h-dba">資料變更紀錄 · 24 小時</h2><span class="sub">誰在 HazelminX 裡改了哪一列</span></div>
      <div style="padding:12px 14px;display:flex;flex-direction:column;gap:8px">{U.state_timeline("o" * 24)}
        <span class="note">7 筆：tim 改 inventory 3 列（03:10）、analyst 匯出 shop_analytics 1 次（昨天 18:02）、系統備份 2 次、結構變更 0</span></div>
    </section>"""
    main = head + U.scope_bar([("全部節點", "5 實例"), ("hazel-tw-01", "Docker"), ("hazel-jp-02", "Docker · 過期"), ("lab-mini", "Podman · K3s")]) + tabs + instances + f'<div class="cg">{conn}{slow}</div>' + f'<div class="cg">{dbs}{schema}</div>' + f'<div class="cg">{redis}{audit}</div>'
    return U.shell("db", main)



# ---------- AI ----------
def ai() -> str:
    head = U.page_head("AI", OBS, '<button type="button" class="btn">安裝 Ollama</button><button type="button" class="btn">新增 MCP 服務</button><button type="button" class="btn">新增 Agent</button>')
    tabs = ('<div class="panel"><div class="tabs" role="tablist"><button type="button" class="tab on" role="tab" aria-selected="true">模型與推論</button><a class="tab" role="tab" aria-selected="false" href="ModelHub.dc.html">模型庫 · Hugging Face</a><button type="button" class="tab" role="tab" aria-selected="false">Agent · 2</button>'
            '<button type="button" class="tab" role="tab" aria-selected="false">MCP 服務 · 3</button><button type="button" class="tab" role="tab" aria-selected="false">GPU</button><button type="button" class="tab" role="tab" aria-selected="false">推論 API</button></div></div>')
    inst = f"""
    <section class="panel" aria-labelledby="h-ol">
      <div class="ph"><h2 id="h-ol">推論服務 · 2</h2><span class="sub">看見 GPU 不等於容器能用：驅動、容器工具、裝置權限、實際推論分開檢查</span></div>
      <div class="fleet">
        <a class="ncard" href="#ol1"><div class="nh"><span class="n">ollama @ hazel-tw-01</span>{U.status("ok", "執行中")}<span class="d">0.5.7 · 容器 · GPU L4 · 11434 對外可連 → /ollama 代理</span></div>
          <div class="rings">{U.ring(44, "44%", "GPU 核心")}{U.ring(14.2 / 24 * 100, "14G", "顯示記憶體 /24G")}{U.ring(61 / 120 * 100, "61G", "模型 /120G 配額", warn=80, danger=90)}</div>
          <div class="ft">{U.steps([("驅動", "done", "550.90"), ("容器工具", "done", "1.16"), ("裝置權限", "done", "gpu=all"), ("實際推論", "done", "38 tok/s")])}</div>
          <div class="ft"><span class="note">已載入 qwen2.5:14b · 2 個請求處理中</span><span style="margin-left:auto" class="chip on">0.6.1 可更新</span></div></a>
        <a class="ncard" href="#ol2"><div class="nh"><span class="n">ollama @ lab-mini</span>{U.status("info", "CPU 模式")}<span class="d">0.5.7 · 既有容器（接管）· 11434</span></div>
          <div class="rings">{U.ring(3, "3%", "CPU")}{U.ring(2.1 / 7.6 * 100, "2.1G", "記憶體 /7.6G")}{U.ring(9 / 40 * 100, "9G", "模型 /40G", warn=80, danger=90)}</div>
          <div class="ft">{U.steps([("驅動", "fail", "無 GPU"), ("容器工具", "todo", "不適用"), ("裝置權限", "todo", "不適用"), ("實際推論", "done", "4 tok/s")])}</div>
          <div class="ft"><span class="note">GPU：未偵測到 · 模式：CPU</span></div></a>
      </div>
    </section>"""
    def model(name, size, quota_pct, node, st, digest, src, note, loaded=None):
        load = "" if loaded is None else f'<button type="button" class="btn sm">{"卸載" if loaded else "載入"}</button>'
        acts = U.actions(["stop"]) if loaded is None else U.actions(["term", "trash", "more"])
        return (f'<div class="row srow"><span class="name"><a class="n" href="#m">{name}</a><span class="d">{src} · {digest}</span></span><span>{st}</span><span class="c3 mono">{node}</span><span class="c4">{U.meter(quota_pct, size, warn=101, danger=101)}</span><span class="c4 note">{note}</span><span class="c5">{load}</span><span>{acts}</span></div>')
    models = f"""
    <section class="panel" aria-labelledby="h-md" data-sort="0:asc" data-batch>
      <div class="ph"><h2 id="h-md">模型 · 5</h2><span class="sub">量表＝佔該節點模型配額的比例；digest 用來確認下載完整</span><div class="wrap" style="margin-left:auto"><button type="button" class="btn sm">同步清單</button></div></div>
      <div class="wrap" style="padding:10px 14px;border-bottom:1px solid var(--line)"><label class="inp" style="flex:1;min-width:min(240px,100%)"><span class="note">模型</span><input value="deepseek-r1:32b" aria-label="要下載的模型名稱" style="background:transparent;border:0;flex:1;min-width:0"></label><span style="flex:0 1 220px;min-width:160px">{_dd("ollama @ tw-01")}</span><button type="button" class="btn pri">{U.icon("download", 14)}下載模型</button></div>
      <div class="row hd srow"><span>模型</span><span>狀態</span><span class="c3">節點</span><span class="c4">大小 / 配額</span><span class="c4">備註</span><span class="c5"></span><span></span></div>
      {model("qwen2.5:14b", "9.0 GiB", 7.5, "tw-01", U.status("ok", "已載入 · GPU"), "sha256:7c1a…", "ollama.com", "Apache-2.0 · 2 請求中", True)}
      {model("llama3.1:8b", "4.9 GiB", 4, "tw-01", U.status("info", "未載入"), "sha256:42e0…", "ollama.com", "Agent wiki-qa 使用中", False)}
      {model("nomic-embed-text", "274 MiB", 0.2, "tw-01", U.status("ok", "已載入 · 嵌入"), "sha256:0a90…", "ollama.com", "02:00 批次嵌入用", True)}
      {model("deepseek-r1:32b", "19.9 GiB", 16.6, "tw-01", U.status("warn", "下載中 71%"), "—", "ollama.com", "預估 6 分 · 速率 48 MB/s")}
      {model("qwen2.5:7b", "4.7 GiB", 11.7, "lab-mini", U.status("ok", "已載入 · CPU"), "sha256:b11c…", "ollama.com", "4 tok/s", True)}
    </section>"""
    delm = f"""
    <section class="panel" aria-labelledby="h-delm" role="dialog" aria-modal="false">
      <div class="ph"><h2 id="h-delm">刪除模型 llama3.1:8b（tw-01）？</h2></div>
      <div style="padding:12px 14px;display:flex;flex-direction:column;gap:10px">
        <div class="kv"><span class="k">釋放空間</span><span class="v mono">4.9 GiB</span></div>
        <div class="kv"><span class="k">使用中</span><span class="v">{U.status("warn", "Agent wiki-qa")}</span></div>
        <div class="kv"><span class="k">其他節點</span><span class="v">lab-mini 沒有這個模型</span></div>
        <div class="wrap" style="justify-content:flex-end"><button type="button" class="btn">取消</button><button type="button" class="btn">先停用 wiki-qa</button><button type="button" class="btn dng">刪除模型</button></div>
      </div>
    </section>"""
    pull = f"""
    <section class="panel" aria-labelledby="h-pull">
      <div class="ph"><h2 id="h-pull">下載 · deepseek-r1:32b → tw-01</h2><span class="sub">是一個工作：關頁照跑；配額不夠在開始前就擋</span></div>
      <div style="padding:12px 14px">{U.pipeline([("配額檢查", "done", "即時", "61+20 < 120 GiB"), ("下載", "cur", "71% · 14.1/19.9G", "48 MB/s"), ("校驗 digest", "todo", "—", "sha256"), ("載入測試", "todo", "—", "GPU 記憶體 20G 夠")])}</div>
    </section>"""
    chat = f"""
    <section class="panel" aria-labelledby="h-chat">
      <div class="ph"><h2 id="h-chat">測試對話 · qwen2.5:14b @ tw-01</h2><span class="sub">串流；每次回覆顯示 token 數、速率與首字延遲</span></div>
      <div style="padding:12px 14px;display:flex;flex-direction:column;gap:10px">
        <div class="card-raised" style="background:var(--raised);border:1px solid var(--line);border-radius:8px;padding:10px 12px"><span class="note">你</span><p>用一句話說明 Cloudflare 邊緣與來源站請求數為什麼不能相加。</p></div>
        <div style="padding:10px 12px"><span class="note">qwen2.5:14b · 212 tokens · 38 tok/s · 首字 0.4 秒</span><p>因為它們是同一批流量在兩個不同觀測點的計數：邊緣包含被擋下與快取直接回應的請求，來源站只有真正抵達的請求，相加會把同一次訪問算兩遍。</p></div>
        <div style="display:flex;align-items:center;gap:10px"><span class="note">tok/s 近 10 次</span>{U.sparkline([36, 38, 41, 37, 39, 38, 40, 38, 37, 38], w=140)}<label class="inp" style="flex:1"><input placeholder="輸入訊息" style="background:transparent;border:0;flex:1;min-width:0"></label><button type="button" class="btn pri">送出</button></div>
      </div>
    </section>"""
    mcp = f"""
    <section class="panel" aria-labelledby="h-mcp">
      <div class="ph"><h2 id="h-mcp">MCP 服務 · 3 · Agent · 2</h2><span class="sub">AI 可以提出變更計畫，但不會自動執行；每個服務有自己的授權範圍</span></div>
      <div class="row hd srow"><span>名稱</span><span>狀態</span><span class="c3">類型 · 綁定</span><span class="c4">授權範圍</span><span class="c4">今日呼叫</span><span class="c5"></span><span></span></div>
      <div class="row srow"><span class="name"><span class="n">hazelminx-mcp</span><span class="d">平台內建 · 管理 API</span></span><span>{U.status("ok", "執行中")}</span><span class="c3 note">MCP · 綁 Token hx_mcp_…</span><span class="c4 note">讀取全部 · 寫入只能「提出計畫」</span><span class="mono c4">41</span><span class="c5"></span><span></span></div>
      <div class="row srow"><span class="name"><span class="n">filesystem-ro</span><span class="d">容器 · 掛載 /mnt/data/docs 唯讀</span></span><span>{U.status("ok", "執行中")}</span><span class="c3 note">MCP · 掛 wiki 文件</span><span class="c4 note">唯讀</span><span class="mono c4">12</span><span class="c5"></span><span></span></div>
      <div class="row srow"><span class="name"><span class="n">github-mcp</span><span class="d">容器 · 連 hazelnut 組織</span></span><span>{U.status("warn", "Token 9 天到期")}</span><span class="c3 note">MCP · 綁 GitHub 連線</span><span class="c4 note">repo 讀取</span><span class="mono c4">3</span><span class="c5"></span><span></span></div>
      <div class="row srow"><span class="name"><span class="n">ops-assistant</span><span class="d">Agent · qwen2.5:14b · 綁 hazelminx-mcp、filesystem-ro</span></span><span>{U.status("ok", "執行中")}</span><span class="c3 note">對外 assistant.example.tw</span><span class="c4 note">提出計畫（需批准）</span><span class="mono c4">27 對話</span><span class="c5"></span><span></span></div>
      <div class="row srow"><span class="name"><span class="n">wiki-qa</span><span class="d">Agent · llama3.1:8b · 綁 filesystem-ro</span></span><span>{U.status("info", "未載入模型")}</span><span class="c3 note">內部</span><span class="c4 note">唯讀</span><span class="mono c4">0</span><span class="c5"></span><span></span></div>
    </section>"""
    api = f"""
    <section class="panel" aria-labelledby="h-iapi">
      <div class="ph"><h2 id="h-iapi">推論 API · /ollama</h2><span class="sub">透過 API 代理發布：Token、限速、用量都在那裡管</span><a class="more" href="APIProxy.dc.html">API 代理 →</a></div>
      <div style="padding:12px 14px;display:flex;gap:20px;flex-wrap:wrap">{U.ring(58 / 60 * 100, "58", "lab-bot /60 分", warn=85, danger=95)}{U.ring(20, "12", "partner-a /60 分", warn=85, danger=95)}{U.ring(0.5, "3", "tim /600 分", warn=85, danger=95)}
        <div style="display:flex;flex-direction:column;gap:4px;min-width:220px"><span class="note">今日 tokens 產出</span><span class="mono" style="font-size:20px;font-weight:600">1.9M</span>{U.sparkline([int(x) for x in walk(24, 80, 30, 10, 180)], w=200)}</div></div>
    </section>"""
    main = head + U.scope_bar([("全部節點", "2 推論服務"), ("hazel-tw-01", "GPU L4"), ("lab-mini", "CPU")]) + tabs + inst + models + f'<div class="cg">{pull}{delm}</div>' + chat + mcp + api
    return U.shell("ai", main)


# ---------- AI › 模型庫（Hugging Face） ----------
def model_hub() -> str:
    head = U.page_head("AI", OBS, '<button type="button" class="btn">{}新增模型庫</button><button type="button" class="btn pri">部署推論服務</button>'.format(U.icon("plus", 14)))
    tabs = ('<div class="panel"><div class="tabs" role="tablist"><a class="tab" role="tab" aria-selected="false" href="AI.dc.html">模型與推論</a>'
            '<button type="button" class="tab on" role="tab" aria-selected="true">模型庫 · Hugging Face</button><button type="button" class="tab" role="tab" aria-selected="false">Agent · 2</button>'
            '<button type="button" class="tab" role="tab" aria-selected="false">MCP 服務 · 3</button><button type="button" class="tab" role="tab" aria-selected="false">GPU</button></div></div>')
    def lib(name, host, disk, path, pct, used, n, st):
        return (f'<div class="row srow"><span class="name"><span class="n">{name}</span><span class="d mono">{path}</span></span><span>{st}</span>'
                f'<span class="c3 mono">{host}</span><span class="c3">{disk}</span><span class="c4">{U.meter(pct, used)}</span><span class="c4 mono">{n}</span>'
                f'<span>{U.actions(["move", "more"])}</span></div>')
    libs = f"""
    <section class="panel" aria-labelledby="h-lib">
      <div class="ph"><h2 id="h-lib">模型庫 · 4</h2><span class="sub">每顆資料磁碟有預設位置，第一次用才建立；綁磁碟 UUID</span></div>
      <div class="row hd srow"><span>模型庫</span><span>狀態</span><span class="c3">主機</span><span class="c3">磁碟</span><span class="c4">使用 / 配額</span><span class="c4">模型</span><span></span></div>
      {lib("fast", "tw-01", "nvme1n1 · /mnt/fast", "/mnt/fast/hazelminx/models", 31, "61 / 200 GiB", "4", U.status("ok", "可用"))}
      {lib("data", "tw-01", "sdb1 · /mnt/data", "/mnt/data/hazelminx/models", 9, "18 / 200 GiB", "2", U.status("ok", "可用"))}
      {lib("mini", "lab-mini", "sda1 · /", "/var/lib/hazelminx/models", 23, "9 / 40 GiB", "1", U.status("ok", "可用"))}
      {lib("usb", "lab-mini", "sdc1 · 未掛載", "/mnt/usb/hazelminx/models", 0, "— / 500 GiB", "3", U.status("dng", "磁碟不在"))}
    </section>"""
    def res(name, org, lic, upd, params, dl, fmt, fit, gated=False):
        lock = f' {U.status("warn", "需要同意條款")}' if gated else ""
        return (f'<div class="row srow"><span class="name"><a class="n" href="#hf">{org}/{name}</a><span class="d">{lic} · 更新 {upd}</span></span><span>{fit}{lock}</span>'
                f'<span class="c3 mono">{params}</span><span class="c3">{fmt}</span><span class="c4 mono">{dl}</span><span class="c4"></span>'
                f'<span><button type="button" class="btn sm">選擇檔案</button></span></div>')
    search = f"""
    <section class="panel" aria-labelledby="h-hfs">
      <div class="ph"><h2 id="h-hfs">搜尋 Hugging Face</h2><span class="sub">放不放得下是依目標主機的記憶體與模型庫空間估算</span></div>
      <div style="padding:12px 14px;display:flex;flex-direction:column;gap:10px">
        <div class="wrap"><label class="inp" style="flex:1;min-width:min(240px,100%)">{U.icon("search", 14)}<input value="qwen2.5 7b" aria-label="搜尋模型" style="background:transparent;border:0;flex:1;min-width:0"></label><span style="flex:0 1 200px;min-width:160px">{_dd("目標：lab-mini · mini")}</span><span style="flex:0 1 160px;min-width:140px">{_dd("排序：下載數")}</span></div>
        <div class="wrap"><span class="note">格式</span><span class="chip on">GGUF</span><span class="chip">safetensors</span><span class="note" style="margin-left:8px">任務</span><span class="chip on">文字生成</span><span class="chip">嵌入</span><span class="note" style="margin-left:8px">參數</span><span class="chip on">≤ 14B</span><span class="note" style="margin-left:8px">授權</span><span class="chip">可商用</span></div>
      </div>
      <div class="row hd srow"><span>模型</span><span>放不放得下</span><span class="c3">參數</span><span class="c3">格式</span><span class="c4">下載數</span><span class="c4"></span><span></span></div>
      {res("Qwen2.5-7B-Instruct-GGUF", "Qwen", "Apache-2.0", "3 週前", "7.6B", "1.2M", "GGUF", U.status("ok", "放得下 · CPU"))}
      {res("Meta-Llama-3.1-8B-Instruct-GGUF", "bartowski", "Llama 3.1", "2 個月前", "8.0B", "890K", "GGUF", U.status("ok", "放得下 · CPU"), True)}
      {res("Qwen2.5-14B-Instruct-GGUF", "Qwen", "Apache-2.0", "3 週前", "14.8B", "640K", "GGUF", U.status("warn", "記憶體不夠 · 只有 Q2"))}
      {res("Qwen2.5-7B-Instruct", "Qwen", "Apache-2.0", "3 週前", "7.6B", "3.4M", "safetensors", U.status("none", "需要 NVIDIA GPU · 換主機"))}
    </section>"""
    def qf(file, size, fit, on):
        return (f'<div class="row"><span class="wrap" style="flex-wrap:nowrap"><input type="radio" name="q"{" checked" if on else ""} aria-label="{file}"><span class="mono">{file}</span></span>'
                f'<span class="mono">{size}</span><span>{fit}</span></div>')
    pick = f"""
    <section class="panel" aria-labelledby="h-pick">
      <div class="ph"><h2 id="h-pick">Qwen/Qwen2.5-7B-Instruct-GGUF</h2><span class="sub">只下載選的那個檔，不整個 repo 一起拉</span></div>
      <div class="row hd"><span>檔案</span><span>大小</span><span>lab-mini（16 GiB 記憶體）</span></div>
      {qf("qwen2.5-7b-instruct-q4_k_m.gguf", "4.7 GiB", U.status("ok", "放得下 · 約 9 tok/s"), True)}
      {qf("qwen2.5-7b-instruct-q8_0.gguf", "8.1 GiB", U.status("ok", "放得下 · 較慢"), False)}
      {qf("qwen2.5-7b-instruct-fp16.gguf", "15.2 GiB", U.status("dng", "記憶體不夠"), False)}
      <div style="padding:12px 14px;display:flex;flex-direction:column;gap:10px">
        <div class="wrap"><span class="note">可用 runtime</span>{U.status("ok", "llama.cpp")}<span tabindex="0" data-tip="lab-mini 沒有 NVIDIA GPU">{U.status("none", "vLLM 不適用")}</span></div>
        <div class="wrap"><span style="flex:1;min-width:min(220px,100%)">{_dd("下載到：mini（lab-mini · 剩 31 GiB）")}</span><button type="button" class="btn pri">{U.icon("download", 14)}下載 4.7 GiB</button></div>
      </div>
    </section>"""
    dl = f"""
    <section class="panel" aria-labelledby="h-hfd">
      <div class="ph"><h2 id="h-hfd">下載 · bartowski/Meta-Llama-3.1-8B-Instruct-GGUF → fast</h2><span class="sub">關頁照跑；中斷會續傳</span></div>
      <div style="padding:12px 14px">{U.pipeline([("空間檢查", "done", "即時", "61+4.9 < 200 GiB"), ("Hugging Face Token", "done", "機密庫", "已同意條款"), ("下載", "cur", "62% · 3.0/4.9G", "41 MB/s"), ("核對 sha256", "todo", "—", "每個檔案")])}</div>
    </section>"""
    def svc(name, rt, model, host, gpu, tps, st):
        return (f'<div class="row srow"><span class="name"><span class="n">{name}</span><span class="d mono">{model}</span></span><span>{st}</span>'
                f'<span class="c3">{rt}</span><span class="c3 mono">{host}</span><span class="c6">{gpu}</span><span class="c4 mono">{tps}</span>'
                f'<span>{U.actions(["log", "stop", "more"])}</span></div>')
    svcs = f"""
    <section class="panel" aria-labelledby="h-rt">
      <div class="ph"><h2 id="h-rt">推論服務 · 3</h2><span class="sub">只綁本機；對外走 API 代理</span><a class="more" href="APIProxy.dc.html">API 代理 →</a></div>
      <div class="row hd srow"><span>服務</span><span>狀態</span><span class="c3">runtime</span><span class="c3">主機</span><span class="c6">GPU 記憶體</span><span class="c4">tok/s</span><span></span></div>
      {svc("qwen-7b-vllm", "vLLM 0.6", "Qwen/Qwen2.5-7B-Instruct", "tw-01", U.meter(78, "18.7 / 24 GiB"), "61", U.status("ok", "執行中"))}
      {svc("qwen-7b-cpu", "llama.cpp", "qwen2.5-7b-instruct-q4_k_m.gguf", "lab-mini", '<span class="note">CPU · 8 執行緒</span>', "9", U.status("ok", "執行中"))}
      {svc("embed-bge", "llama.cpp", "bge-m3-q8_0.gguf", "lab-mini", '<span class="note">CPU</span>', "—", U.status("dng", "沒啟動 · 模型庫 usb 磁碟不在"))}
    </section>"""
    main = head + U.scope_bar([("全部主機", "4 模型庫"), ("hazel-tw-01", "GPU L4"), ("lab-mini", "CPU")]) + tabs + libs + search + f'<div class="cg">{pick}{dl}</div>' + svcs
    return U.shell("ai", main)


# ---------- 系統元件停止（ROB-013） ----------
def component_down_page() -> str:
    head = U.page_head("網站與入口", OBS, '<span tabindex="0" data-tip="Zentinel 停止中，新增的站點不會生效"><button type="button" class="btn pri" disabled>新增站點</button></span><button type="button" class="btn">申請憑證</button><button type="button" class="btn">Zentinel 設定</button>')
    bar = U.component_down("Zentinel", "hazel-tw-01", "4 分鐘前", "退出碼 1 · 設定檢查失敗", "shop.example.tw、staging.shop 目前無法連線", kind="systemd 服務 · zentinel.service")
    log = f"""
    <section class="panel" aria-labelledby="h-zl">
      <div class="ph"><h2 id="h-zl">停止前最後的日誌 · journalctl -u zentinel</h2><a class="more" href="Logs.dc.html">完整日誌 →</a></div>
      <div style="padding:12px 14px"><div class="term" style="white-space:pre-wrap;overflow-wrap:anywhere">03:38:02 INFO  reload requested (SIGHUP)
03:38:02 ERROR route shop.example.tw: upstream "shop-api:8080" not resolvable
03:38:02 ERROR config check failed, refusing to start
03:38:02 exit status 1</div></div>
    </section>"""
    def site(dom, st, req, up):
        return (f'<div class="row srow stale"><span class="name"><span class="n mono">{dom}</span><span class="d">上次觀測 03:38</span></span><span>{st}</span>'
                f'<span class="c3 mono">{req}</span><span class="c4">{up}</span><span class="c4"></span><span class="c5"></span>'
                f'<span><span tabindex="0" data-tip="Zentinel 停止中，套用不會生效">{U.actions(["more"])}</span></span></div>')
    sites = f"""
    <section class="panel" aria-labelledby="h-zs">
      <div class="ph"><h2 id="h-zs">站點 · 3</h2></div>
      <div class="row hd srow"><span>站點</span><span>狀態</span><span class="c3">今日請求</span><span class="c4">上游</span><span class="c4"></span><span class="c5"></span><span></span></div>
      {site("shop.example.tw", U.status("dng", "無法連線"), "41,220 *", "shop-api:8080")}
      {site("staging.shop", U.status("dng", "無法連線"), "802 *", "shop-api:8080 · stg")}
      {site("wiki.example.tw", U.status("ok", "正常 · 由 jp-02 提供"), "18,904", "wiki-app:3000")}
    </section>"""
    def cert(dom, days, st):
        return f'<div class="row"><span class="mono">{dom}</span><span class="mono">{days}</span><span>{st}</span></div>'
    certs = f"""
    <section class="panel" aria-labelledby="h-zc">
      <div class="ph"><h2 id="h-zc">憑證</h2></div>
      <div class="row hd"><span>網域</span><span>剩餘</span><span>續期</span></div>
      {cert("shop.example.tw", "11 天", U.status("warn", "Zentinel 停止中 · 恢復後續期"))}
      {cert("wiki.example.tw", "71 天", U.status("ok", "自動"))}
    </section>"""
    main = head + bar + log + sites + certs
    return U.shell("web", main)


# ---------- 檔案 ----------
def files() -> str:
    head = node_head("hazel-tw-01", U.status("ok", "連線中"), "最後聯絡 2 秒前", '<a class="btn" href="NodeTerminal.dc.html">在此開終端機</a>') + node_tabs("檔案")
    scope = U.scope_bar([("主機檔案系統", ""), ("wiki-api", "容器內"), ("shop-api", "容器內")], label="位置")
    def tb(k, label, dis=False):
        return f'<button type="button" class="ib"{" aria-disabled=true" if dis else ""}>{U.icon(k)}<span>{label}</span></button>'
    toolbar = ('<div class="toolbar">' + tb("newfolder", "新資料夾") + tb("upload", "上傳") + tb("download", "下載") + '<span class="sep"></span>'
               + tb("rename", "重新命名") + tb("move", "搬移") + tb("copy", "複製") + tb("zip", "壓縮") + tb("share", "分享") + tb("trash", "刪除")
               + '<span style="margin-left:auto" class="note">已選 1 項 · 7.2 GiB</span><span class="sep"></span>'
               + '<button type="button" class="ib" aria-label="清單檢視" aria-pressed="true" style="color:var(--hazel)">' + U.icon("list") + '</button><button type="button" class="ib" aria-label="格狀檢視">' + U.icon("grid") + '</button>'
               + '<button type="button" class="ib" aria-pressed="false">' + U.icon("eye") + '<span>隱藏檔</span></button></div>')
    crumbs = ('<div class="panel" style="display:flex;align-items:center;gap:10px;padding:6px 12px;flex-wrap:wrap">'
              f'<div class="crumbs"><a href="#r" aria-label="根目錄">{U.icon("storage", 14)}</a><span class="sep">/</span><a href="#1">mnt</a><span class="sep">/</span><a href="#2">data</a><span class="sep">/</span><a href="#3">apps</a><span class="sep">/</span><a href="#4">shop</a></div>'
              '<span class="note">tim · 可寫 · 5 項 · 48 GiB</span>'
              '<label class="inp" style="margin-left:auto;width:auto;flex:1 1 200px;max-width:360px;min-height:34px"><span class="note">搜尋</span><input placeholder="名稱、內容" aria-label="搜尋檔案" style="background:transparent;border:0;flex:1;min-width:0"></label></div>')
    tree_html = U.tree([(0, "/mnt/data", "931G", False), (1, "apps", "402G", False), (2, "shop", "48G", True), (3, "data", "41G", False), (3, "config", "2M", False), (3, "uploads", "7G", False), (2, "wiki", "290G", False), (3, "uploads", "27G", False), (3, "db-data", "84G", False), (2, "ollama", "61G", False), (3, "models", "61G", False), (1, "backups", "38G", False), (2, "2026-11", "9G", False), (2, "2026-10", "29G", False), (1, "hazelminx", "1.2G", False), (1, "nfs", "19G", False)], open_depth=1)
    def frow(name, kind, size, owner, perm, mtime, sel=False, folder=False, up=False):
        ic = U.icon("up" if up else ("folder" if folder else "file"))
        return (f'<a class="row frow{" sel" if sel else ""}" href="#f"><span class="ck"><input type="checkbox" aria-label="選取 {name}"{" checked" if sel else ""}{" disabled" if up else ""}></span>'
                f'<span class="name" style="flex-direction:row;align-items:center;gap:8px">{ic}<span><span class="n">{name}</span><span class="d">{kind}</span></span></span>'
                f'<span class="mono">{size}</span><span class="mono">{mtime}</span>{U.actions(["download", "rename", "more"]) if not up else "<span></span>"}</a>')
    listing = f"""
    <div class="pane">
      <div class="row hd frow"><span class="ck"><input type="checkbox" aria-label="全選"></span><span class="sortable on">名稱 {U.icon("up", 12)}</span><span class="sortable">大小 {U.icon("sort", 12)}</span><span class="sortable">修改 {U.icon("sort", 12)}</span><span></span></div>
      {frow("..", "上一層 apps", "", "", "", "", up=True, folder=True)}
      {frow("data", "shop-db 掛載 /var/lib/postgresql", "41 GiB", "999:999", "drwx------", "2 分前", folder=True)}
      {frow("uploads", "shop-api 掛載 /app/uploads · 分享 1", "7.2 GiB", "1000:1000", "drwxr-x---", "昨天 21:14", sel=True, folder=True)}
      {frow("config", "", "2.1 MiB", "root:root", "drwxr-x---", "10/30 09:12", folder=True)}
      {frow("compose.yaml", "GitHub a3f9c1e · 歷史 9 版", "3.4 KiB", "root:root", "-rw-r-----", "10/30 09:12")}
      {frow(".env", "shop-api 環境變數 · 編輯中", "812 B", "root:root", "-rw-------", "10/30 09:12")}
      <div class="pager"><span>5 項 · 已選 1</span><span class="pg"><button type="button" class="ib" aria-label="上一頁" aria-disabled="true">{U.icon("chev")}</button><span>1 / 1</span><button type="button" class="ib" aria-label="下一頁" aria-disabled="true">{U.icon("chev")}</button></span></div>
    </div>"""
    details = f"""
    <div class="pane detp">
      <div class="pt">{U.icon("folder")}<span class="mono" style="color:var(--ink)">uploads/</span><span style="margin-left:auto">{U.status("ok", "掛載中")}</span></div>
      <dl class="dl"><dt>路徑</dt><dd class="mono">/mnt/<wbr>data/<wbr>apps/<wbr>shop/<wbr>uploads</dd><dt>大小</dt><dd>7.2 GiB · 1,284 檔案 · 36 資料夾</dd><dt>擁有者</dt><dd class="mono">1000:1000（www-data）</dd><dt>權限</dt><dd class="mono">drwxr-x--- (750) · ACL：deploy r-x</dd><dt>修改</dt><dd>昨天 21:14</dd><dt>掛載</dt><dd>容器 shop-api → /app/uploads（rw）</dd><dt>分享</dt><dd>1 條 · 7 天 · 剩 38 次下載</dd><dt>備份</dt><dd>shop-db 每日 03:00（含此目錄）</dd><dt>回收筒</dt><dd>30 天</dd></dl>
      <div class="wrap" style="padding:0 12px 12px"><button type="button" class="btn sm">權限與 ACL</button><button type="button" class="btn sm">分享連結</button><button type="button" class="btn sm">跨節點複製</button><a class="btn sm" href="Containers.dc.html">shop-api</a></div>
    </div>"""
    fm = f'<div class="fm"><div class="pane treep"><div class="pt">{U.icon("nodes")}hazel-tw-01<span style="margin-left:auto" class="mono">931G</span></div><div style="padding:6px 8px;overflow:auto">{tree_html}</div></div>{listing}{details}</div>'
    def kv(k, v, cur=False):
        return ("▸" if cur else "") + f'<span class="sx-k">{k}</span><span class="sx-o">=</span><span class="sx-v">{v}</span>'
    env_lines = [
        '<span class="sx-c"># shop-api · 由 compose.yaml 的 env_file 載入</span>',
        '<span class="sx-c"># DB_PASSWORD、STRIPE_KEY 由機密庫注入，不寫在這個檔案</span>',
        kv("NODE_ENV", "production"),
        kv("PORT", "8080"),
        kv("DATABASE_URL", "postgres://shop@shop-db:5432/shop"),
        kv("REDIS_URL", "redis://shop-redis:6379", cur=True),
        kv("MAIL_FROM", "shop@example.tw"),
        kv("CHECKOUT_TIMEOUT_MS", "8000"),
        "",
        '<span class="sx-c"># 10/30 tim：關掉新會員註冊活動</span>',
        kv("FEATURE_SIGNUP_PROMO", "false"),
    ]
    editor = f"""
    <section class="panel" aria-labelledby="h-ed">
      <div class="ph"><h2 id="h-ed" class="mono">/mnt/data/apps/shop/.env</h2><span class="note">hazel-tw-01 · root:root · -rw-------</span><div class="wrap" style="margin-left:auto"></div></div>
      <div style="padding:12px 14px">{U.code_editor([("compose.yaml", False, False), (".env", True, True)], env_lines,
        ["第 6 行，第 30 欄", "dotenv", "UTF-8", "LF", "812 B → 824 B", "未儲存", "外部修改：ops-ann 03:40:12"],
        '<label class="inp" style="min-height:28px;width:auto"><span class="note">語言</span><select aria-label="語言" style="background:transparent;border:0"><option>dotenv</option></select></label>',
        banner=('<div style="display:flex;flex-direction:column;gap:8px;width:100%">'
                '<div class="wrap">' + U.status("warn", "這個檔案在你編輯時被改過") + '<span>ops-ann 在 03:40:12 存了新版本 · 他改第 4 行 · 你改第 6 行 · 沒有重疊</span></div>'
                '<div class="mini-diff"><span class="note">第 4 行 · ops-ann</span><span class="mono"><span style="color:var(--danger)">− REDIS_URL=redis://shop-redis:6379</span><br><span style="color:var(--ok)">+ REDIS_URL=redis://shop-redis:6379/1</span></span>'
                '<span class="note">第 6 行 · 你</span><span class="mono"><span style="color:var(--danger)">− CHECKOUT_TIMEOUT_MS=5000</span><br><span style="color:var(--ok)">+ CHECKOUT_TIMEOUT_MS=8000</span></span></div>'
                '<div class="wrap"><button type="button" class="btn sm pri">合併兩邊的修改</button><button type="button" class="btn sm">只留我的 · ops-ann 第 4 行的修改會不見</button><button type="button" class="btn sm">改用 ops-ann 的版本 · 我的修改會不見</button></div></div>'))}</div>
    </section>"""
    transfers = f"""
    <section class="panel" aria-labelledby="h-tr">
      <div class="ph"><h2 id="h-tr">傳輸 · 2</h2></div>
      <div style="padding:12px 14px;display:flex;flex-direction:column;gap:12px">
        <div><div style="display:flex;justify-content:space-between;gap:8px;flex-wrap:wrap"><span>上傳 media-2026-11.tar（4.2 GiB）→ uploads/</span>{U.status("info", "傳輸中")}</div>{U.meter(63, "2.6 / 4.2 GiB · 31 MB/s · 剩 52 秒")}</div>
        <div><div style="display:flex;justify-content:space-between;gap:8px;flex-wrap:wrap"><span>跨節點複製 uploads/ → hazel-jp-02</span>{U.status("warn", "等待 jp-02")}</div>{U.pipeline([("校驗來源", "done", "7.2G", "sha256 清單"), ("傳輸", "wait", "暫停", "jp-02 狀態過期"), ("校驗目的", "todo", "—", ""), ("完成", "todo", "—", "")])}</div>
      </div>
    </section>"""
    main = head + scope + toolbar + crumbs + fm + editor + transfers + U.savebar(".env：1 處未儲存", "先處理 ops-ann 的修改才能儲存 · 存檔後要重建 shop-api 才會生效", "合併後儲存並重建 shop-api", "合併後只儲存")
    return U.shell("files", main)


# ---------- 計畫任務 ----------
def cron() -> str:
    head = U.page_head("計畫任務", OBS, '<a class="btn pri" href="CronCreate.dc.html">新增任務</a><button type="button" class="btn">腳本庫</button><button type="button" class="btn">匯入</button>')
    tiles = '<div class="tiles">' + "".join([
        U.stat_tile("任務", "9", "", "8 啟用 · 1 暫停", None),
        U.stat_tile("今天執行", "27", "", "成功 25 · 失敗 1 · 略過 1", None, spark=[1, 0, 2, 1, 1, 3, 0, 1, 2, 1, 1, 0, 1, 2, 1, 0, 1, 3, 2, 1, 0, 1, 2, 1]),
        U.stat_tile("下一個", "03:45", "", "清理日誌 @ tw-01（3 分後）", None),
        U.stat_tile("時鐘偏差", "+3", "ms", "三個節點都在 NTP 同步；jp-02 上次觀測", None),
    ]) + "</div>"
    hours24 = [f"{(4 + i) % 24:02d}" for i in range(24)]
    sched = f"""
    <section class="panel" aria-labelledby="h-sch">
      <div class="ph"><h2 id="h-sch">24 小時排程 · 任務 × 小時</h2><span class="sub">深格＝有執行；重疊政策：同任務上一輪沒結束就「略過並記錄」</span></div>
      <div style="padding:12px 14px">{U.heatgrid(["wiki-db 備份", "shop-db 備份", "平台快照", "清理日誌", "憑證續期檢查", "套件安全更新", "ClamAV 掃描", "shop 報表", "健康探測"], hours24, [
        [100 if h == 23 else 0 for h in range(24)], [100 if h == 23 else 0 for h in range(24)], [0] * 24, [100 if h % 6 == 3 else 0 for h in range(24)],
        [100 if h % 12 == 0 else 0 for h in range(24)], [100 if h == 23 else 0 for h in range(24)], [0] * 24, [100 if h == 20 else 0 for h in range(24)], [60] * 24], vmax=100)}</div>
      <div class="foot"><span>平台快照與 ClamAV 是每週；健康探測每 5 分鐘</span><span>夏令時間與時鐘偏差：以 UTC 排程、使用者時區顯示</span></div>
    </section>"""
    def task(name, kind, node, sched_txt, st, cal, last, policy):
        return (f'<a class="row srow" href="#t"><span class="name"><span class="n">{name}</span><span class="d">{kind} · {node} · {sched_txt}</span></span><span>{st}</span><span class="c3">{U.state_timeline(cal)}</span><span class="c4 note">{last}</span><span class="c4 note">{policy}</span><span class="c5"></span><span></span></a>')
    tasks = f"""
    <section class="panel" aria-labelledby="h-tk">
      <div class="ph"><h2 id="h-tk">任務 · 9</h2>{U.legend(TL_LEGEND)}</div>
      <div class="row hd srow"><span>任務</span><span>狀態</span><span class="c3">近 24 次</span><span class="c4">上次</span><span class="c4">政策</span><span class="c5"></span><span></span></div>
      {task("wiki-db 備份", "備份", "tw-01", "每天 03:00", U.status("ok", "啟用"), "o" * 24, "今天 03:00 · 2 分 50 秒", "重疊跳過 · 錯過補跑 · 失敗重試 2 次")}
      {task("shop-db 備份", "備份", "tw-01", "每天 03:30", U.status("ok", "啟用"), "o" * 20 + "f" + "o" * 3, "今天 03:30 · 1 分 10 秒", "重疊跳過 · 錯過補跑 · 失敗重試 2 次")}
      {task("清理日誌", "清理", "全部節點", "每 6 小時", U.status("ok", "啟用"), "o" * 24, "03:00 · 釋出 1.8 GiB", "排隊 · 不補跑 · 不重試")}
      {task("憑證續期檢查", "維護", "zentinel", "每 12 小時", U.status("warn", "上次失敗"), "o" * 22 + "f" * 2, "02:58 · DNS 驗證逾時", "重疊跳過 · 錯過補跑 · 失敗重試 3 次")}
      {task("套件安全更新", "維護", "jp-02、lab-mini", "每天 03:00（維護時段）", U.status("warn", "狀態不明"), "o" * 23 + "p", "03:38 · apt 進行到一半", "排隊 · 不補跑 · 需批准")}
      {task("shop 報表", "腳本 · 腳本庫 v3", "tw-01", "每天 20:00", U.status("ok", "啟用"), "o" * 24, "昨天 20:00 · 41 秒", "重疊跳過 · 錯過補跑 · 失敗重試 1 次")}
      {task("健康探測", "URL", "jp-02", "每 5 分鐘", U.status("warn", "過期"), "o" * 20 + "e" * 4, "03:41 上次觀測", "節點離線時本地續跑")}
      {task("ClamAV 掃描", "維護", "tw-01", "每週日 05:00", U.status("ok", "啟用"), "o" * 24, "11/02 · 0 感染", "重疊跳過 · 錯過補跑")}
      {task("舊報表歸檔", "腳本", "tw-01", "每月 1 日", U.status("info", "暫停"), "o" * 24, "10/01", "—")}
    </section>"""
    deps = U.topology([
        ("先", [("a", "wiki-db 備份", "03:00", "ok"), ("b", "shop-db 備份", "03:30", "ok")]),
        ("再", [("c", "校驗與保留", "成功才跑", "ok"), ("d", "還原測試", "每週 · lab-mini", "dim")]),
        ("最後", [("e", "通知 Telegram", "失敗立刻 · 成功彙整", "ok")]),
    ], [("a", "c", .6, ""), ("b", "c", .6, ""), ("c", "d", .3, "dim"), ("c", "e", .6, ""), ("d", "e", .2, "dim")])
    deps_m = U.flowlist([("wiki-db 備份", "校驗與保留", "成功才跑"), ("shop-db 備份", "校驗與保留", "成功才跑"), ("校驗與保留", "還原測試", "每週"), ("校驗與保留", "通知", "失敗立刻")])
    deps_panel = f"""
    <section class="panel" aria-labelledby="h-dep">
      <div class="ph"><h2 id="h-dep">任務依賴 · 備份鏈</h2><span class="sub">上游失敗時下游略過並記錄原因，不靜默</span></div>
      <div style="padding:10px 14px 6px"><div class="ch-d">{deps}</div><div class="ch-m">{deps_m}</div></div>
    </section>"""
    run = f"""
    <section class="panel" aria-labelledby="h-run">
      <div class="ph"><h2 id="h-run">執行紀錄 · shop-db 備份 · 昨天 03:30</h2><span class="sub">每次執行都有進度、輸出與結果；產出的備份檔可直接還原</span></div>
      <div style="padding:12px 14px">{U.pipeline([("預檢", "done", "2 秒", "空間足夠"), ("dump", "done", "48 秒", "1.3 GiB"), ("傳輸", "fail", "逾時 60 秒", "S3 tokyo 無回應"), ("重試 1/2", "done", "51 秒", "成功"), ("校驗", "done", "6 秒", "sha256 相符")])}
      <div class="term" style="margin-top:10px;min-height:90px">03:30:02 preflight ok (free 519G)
03:30:50 pg_dump done 1.3G
03:31:50 upload timeout after 60s (S3 tokyo)
03:31:51 retry 1/2
03:32:42 upload ok · sha256 verified</div></div>
    </section>"""
    lib = f"""
    <section class="panel" aria-labelledby="h-lib">
      <div class="ph"><h2 id="h-lib">腳本庫 · 4</h2><span class="sub">有版本；任務綁定特定版本，不會被改壞</span></div>
      <div class="row hd srow"><span>腳本</span><span>版本</span><span class="c3">被誰用</span><span class="c4">最後修改</span><span class="c4"></span><span class="c5"></span><span></span></div>
      <div class="row srow"><span class="name"><span class="n">shop-report.sh</span><span class="d">產生每日報表並上傳</span></span><span class="mono">v3</span><span class="c3 note">shop 報表</span><span class="mono c4">10/20 · tim</span><span class="c4"><button type="button" class="btn sm">立即執行</button></span><span class="c5"></span><span></span></div>
      <div class="row srow"><span class="name"><span class="n">archive-old.sh</span><span class="d">把 90 天前的報表壓縮歸檔</span></span><span class="mono">v1</span><span class="c3 note">舊報表歸檔</span><span class="mono c4">9/01 · tim</span><span class="c4"><button type="button" class="btn sm">立即執行</button></span><span class="c5"></span><span></span></div>
      <div class="row srow"><span class="name"><span class="n">clean-docker.sh</span><span class="d">清理無標籤映像</span></span><span class="mono">v2</span><span class="c3 note">未綁定</span><span class="mono c4">10/30 · tim</span><span class="c4"><button type="button" class="btn sm">立即執行</button></span><span class="c5"></span><span></span></div>
    </section>"""
    main = head + U.scope_bar([("全部節點", "9 任務"), ("hazel-tw-01", "Docker"), ("hazel-jp-02", "Docker · 過期"), ("lab-mini", "Podman · K3s")]) + tiles + sched + tasks + f'<div class="cg">{deps_panel}{run}</div>' + lib
    return U.shell("cron", main)


# ---------- 安全防護 ----------
def security() -> str:
    head = U.page_head("安全防護", OBS, '<a class="btn pri" href="FirewallRule.dc.html">新增防火牆規則</a><button type="button" class="btn">新增 WAF 政策</button><button type="button" class="btn">更新 WAF 規則</button><button type="button" class="btn">匯出安全事件</button>')
    tiles = '<div class="tiles">' + "".join([
        U.stat_tile("外部可連的容器 Port", "2", "", "5432 wiki-db · 11434 ollama", False),
        U.stat_tile("今日封鎖", "14", "IP", "Fail2ban 11 · 手動 3", None, spark=[2, 1, 3, 0, 1, 4, 2, 1, 0, 2, 3, 1]),
        U.stat_tile("WAF", "偵測", "3 站點", "0 阻擋 · p95 1.8 ms", None),
        U.stat_tile("規則版本", "CRS 4.8", "", "9 天前更新 · 下一版可用", None),
    ]) + "</div>"
    modes = f"""
    <section class="panel" aria-labelledby="h-wm">
      <div class="ph"><h2 id="h-wm">每個站點的防護</h2><span class="sub">關閉／只偵測／阻擋逐站設定；WAF 故障時的政策也逐站</span></div>
      <div style="padding:12px 14px">{U.matrix(["shop.example.tw", "wiki.example.tw", "staging.shop", "/ollama（API）"], ["WAF 模式", "故障時", "限速", "地區／IP 政策", "body 上限", "健康"], [
        [("warn", "只偵測"), ("ok", "拒絕"), ("ok", "120/分/IP"), ("ok", "2 條"), ("ok", "10 MiB"), ("ok", "1.8 ms")],
        [("warn", "只偵測"), ("ok", "拒絕"), ("ok", "60/分/IP"), ("ok", "1 條"), ("ok", "50 MiB 上傳"), ("warn", "jp-02 過期")],
        [("none", "關閉"), ("none", ""), ("ok", "30/分/IP"), ("ok", "僅 Tunnel"), ("ok", "10 MiB"), ("dng", "上游 502")],
        [("ok", "阻擋"), ("warn", "放行"), ("ok", "Token 限速"), ("ok", "僅 Tunnel"), ("ok", "2 MiB"), ("ok", "2.1 ms")],
      ], first_col="minmax(130px,1.2fr)")}</div>
      <div class="note" style="padding:0 14px 10px">切成「阻擋」前先用右下的「檢查這個請求」驗證誤判；政策逐步發布到多節點（先 tw-01 再 jp-02）</div>
    </section>"""
    rollout = f"""
    <section class="panel" aria-labelledby="h-ro">
      <div class="ph"><h2 id="h-ro">發布 shop.example.tw 改成阻擋</h2><span class="sub">變更計畫 #421 · 逐節點、可回退</span></div>
      <div style="padding:12px 14px">{U.pipeline([("誤判檢查", "done", "11 條例外", "wiki 搜尋路徑排除"), ("批准", "done", "tim", "影響 1 站點"), ("tw-01", "cur", "觀察 10 分", "阻擋 0 · 偵測 3"), ("jp-02", "todo", "等 tw-01 通過", "節點過期"), ("完成", "todo", "—", "")])}</div>
    </section>"""
    hits = U.ts_card("WAF 命中 · 每小時 · 全部站點", [U.Series("偵測", [int(x) for x in walk(24, 50, 20, 5, 120, {22: 118})], "var(--s1)"), U.Series("阻擋", [int(x) for x in walk(24, 2, 2, 0, 6)], "var(--danger)")], HOURS, unit="",
                     bar=True, fmt_val=lambda v: f"{v:.0f}", markers=[(22, "掃描高峰")], foot="阻擋只來自 /ollama · 有歧義的 HTTP 請求（重複 Content-Length 等）直接拒絕，不算 WAF 命中")
    policies = f"""
    <section class="panel" aria-labelledby="h-pol">
      <div class="ph"><h2 id="h-pol">封鎖與例外 · 到期</h2><span class="sub">橫條＝剩餘時間；永久的寫「永久」</span></div>
      <div class="ranks">
        <div class="rank"><span class="mono">185.220.101.0/24 · 封鎖</span><span class="bar" aria-hidden="true"><i style="width:80%;background:var(--danger)"></i></span><span class="mono" style="font-size:12px">■ 19 小時 · Fail2ban sshd</span></div>
        <div class="rank"><span class="mono">45.33.12.9 · 封鎖</span><span class="bar" aria-hidden="true"><i style="width:35%;background:var(--danger)"></i></span><span class="mono" style="font-size:12px">■ 8 小時 · 手動 · tim</span></div>
        <div class="rank"><span class="mono">RU、KP 地區 · 拒絕</span><span class="bar" aria-hidden="true"><i style="width:100%;background:var(--q4)"></i></span><span class="mono" style="font-size:12px">永久 · shop、wiki</span></div>
        <div class="rank"><span class="mono">/wiki/search SQLi 規則 · 例外</span><span class="bar" aria-hidden="true"><i style="width:60%;background:var(--warn)"></i></span><span class="mono" style="font-size:12px">○ 18 天 · 審核後續</span></div>
        <div class="rank"><span class="mono">203.0.113.0/24 · 允許（辦公室）</span><span class="bar" aria-hidden="true"><i style="width:100%;background:var(--ok)"></i></span><span class="mono" style="font-size:12px">永久 · 管理入口</span></div>
      </div>
    </section>"""
    check = f"""
    <section class="panel" aria-labelledby="h-chk">
      <div class="ph"><h2 id="h-chk">規則檢查</h2><span class="sub">貼上 curl 或選一筆日誌；結果列出命中的規則與原因</span></div>
      <div style="padding:12px 14px;display:flex;flex-direction:column;gap:10px">
        <div class="term" style="min-height:70px">GET /wiki/search?q=select+pages+from+2024 HTTP/1.1
Host: wiki.example.tw
CF-Connecting-IP: 203.0.113.7</div>
        <div style="display:flex;gap:10px;flex-wrap:wrap;align-items:center"><button type="button" class="btn pri">檢查</button>{U.status("warn", "會命中 942100 SQLi（分數 5 / 門檻 5）")}<span class="note">已有例外 /wiki/search → 實際放行</span></div>
      </div>
    </section>"""
    fw = f"""
    <section class="panel" aria-labelledby="h-fw2">
      <div class="ph"><h2 id="h-fw2">主機防護</h2></div>
      <div style="padding:12px 14px">{U.matrix(["hazel-tw-01", "hazel-jp-02", "lab-mini"], ["防火牆", "Fail2ban", "ClamAV", "安全更新"], [
        [("ok", "ufw 啟用"), ("ok", "11 封鎖"), ("ok", "0 感染"), ("ok", "無待套用")],
        [("ok", "nftables"), ("warn", "上次觀測"), ("none", "未安裝"), ("warn", "5 待套用")],
        [("ok", "ufw 啟用"), ("ok", "0 封鎖"), ("none", "未安裝"), ("warn", "2 待套用")],
      ])}</div>
    </section>"""
    exposure = ('<h2 class="grp" id="exposure">對外曝露<span class="note">主機與容器 · 3 節點</span></h2>'
                + nodes(part="published") + f'<div class="cg">{nodes(part="ports")}{fw}</div>' + nodes(part="ssh"))
    web = ('<h2 class="grp" id="waf">網站防護<span class="note">Zentinel WAF · 3 站點</span></h2>'
           + U.scope_bar([("全部入口", "2 Zentinel"), ("Zentinel @ hazel-tw-01", ""), ("Zentinel @ hazel-jp-02", "過期")], label="入口")
           + modes + f'<div class="cg">{rollout}{hits}</div>' + f'<div class="cg">{policies}{check}</div>')
    main = head + tiles + exposure + web
    return U.shell("security", main)


# ---------- 流量分析 ----------
def analytics() -> str:
    head = U.page_head("流量分析", OBS, '<button type="button" class="btn">匯出</button><button type="button" class="btn">保留與遮罩</button><button type="button" class="btn pri">連接 Cloudflare Analytics</button>')
    filt = ('<div class="panel"><div style="display:flex;gap:10px;flex-wrap:wrap;align-items:center;padding:10px 14px">'
            + "".join(f'<label class="inp" style="width:auto;min-width:150px"><span class="note">{k}</span><select style="background:transparent;border:0;flex:1"><option>{v}</option></select></label>' for k, v in [("時間", "今天"), ("節點", "全部"), ("專案", "全部"), ("網域", "shop.example.tw"), ("服務", "全部"), ("入口", "全部")])
            + '<span class="chip on">排除機器人</span><span class="chip">只看可信 IP</span></div></div>')
    views = f"""
    <section class="panel" aria-labelledby="h-3v">
      <div class="ph"><h2 id="h-3v">三個觀測點 · shop.example.tw · 今天</h2><span class="sub">同一批流量，三個地方數；不相加</span></div>
      <div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(min(280px,100%),1fr));gap:1px;background:var(--line)">
        <div style="padding:12px 14px;background:var(--panel);display:flex;flex-direction:column;gap:6px"><span class="note">Cloudflare 邊緣</span><span class="mono" style="font-size:24px;font-weight:600">{U.fmt(sum(REQ_EDGE))}</span>{U.sparkline(REQ_EDGE, w=200)}<span class="note">GraphQL Analytics · httpRequestsAdaptiveGroups · 免費方案 · 取樣 0.9% · 保留 7 天 · 上次同步 03:40</span>{U.tall_bar([(41, "快取直接回應", "var(--q4)"), (3, "邊緣阻擋", "var(--danger)"), (56, "回來源站", "var(--q5)")], 100, unit="%")}</div>
        <div style="padding:12px 14px;background:var(--panel);display:flex;flex-direction:column;gap:6px"><span class="note">Zentinel 來源站</span><span class="mono" style="font-size:24px;font-weight:600">{U.fmt(sum(REQ_ORIGIN))}</span>{U.sparkline(REQ_ORIGIN, w=200)}<span class="note">存取日誌 · 完整 100% · 保留 30 天 · 即時</span>{U.tall_bar([(92, "2xx", "var(--ok)"), (5, "3xx", "var(--q3)"), (2, "4xx", "var(--warn)"), (1, "5xx", "var(--danger)")], 100, unit="%")}</div>
        <div style="padding:12px 14px;background:var(--panel);display:flex;flex-direction:column;gap:6px"><span class="note">本機 WAF</span><span class="mono" style="font-size:24px;font-weight:600">1,204</span>{U.sparkline([int(x) for x in walk(24, 50, 20, 5, 120, {22: 118})], w=200)}<span class="note">命中（只偵測）· 阻擋 0 · 完整 100%</span>{U.tall_bar([(51, "掃描器", "var(--q5)"), (19, "路徑穿越", "var(--q4)"), (16, "SQLi", "var(--q3)"), (14, "其他", "var(--q2)")], 100, unit="%")}</div>
      </div>
      <div class="note" style="padding:8px 14px;border-top:1px solid var(--line)">沒接 Cloudflare Token 時，第一欄顯示「未連接」而不是 0 · CF-Ray 只做關聯線索，不假設一一對應</div>
    </section>"""
    lat = U.ts_card("延遲 · 來源站處理 vs 上游 vs 完整傳輸", [U.Series("p50 完整", walk(24, 120, 15, 60, 200), "var(--s1)"), U.Series("p95 完整", walk(24, 380, 50, 200, 700, {22: 940}), "var(--s2)"), U.Series("p95 上游", walk(24, 310, 40, 180, 600, {22: 900}), "var(--s3)")], HOURS, unit=" ms",
                    fmt_val=lambda v: f"{v:.0f}", markers=[(22, "a3f9c1e 部署")], foot="三條線分開：處理、上游回應、完整傳輸；不拿平均冒充 p95 · 部署標記來自工作中心")
    paths = f"""
    <section class="panel" aria-labelledby="h-paths">
      <div class="ph"><h2 id="h-paths">熱門路徑 · 來源站</h2><span class="sub">右邊：方法 · 5xx 比例</span></div>
      {U.ranked([("/api/checkout", 18400, "18.4K · POST · 5xx 0.3%"), ("/", 14200, "14.2K · GET"), ("/api/products", 12900, "12.9K · GET"), ("/static/*", 9800, "9.8K · GET · 快取 98%"), ("/api/cart", 7100, "7.1K · GET/POST"), ("/healthz", 4300, "4.3K · 探測")], vmax=20000)}
    </section>"""
    geo = f"""
    <section class="panel" aria-labelledby="h-geo">
      <div class="ph"><h2 id="h-geo">來源 · 可信 IP 判定後</h2></div>
      <div style="padding:12px 14px;display:flex;flex-direction:column;gap:10px">
        <div><span class="note">國家／地區</span>{U.tall_bar([(71, "TW", "var(--q5)"), (12, "JP", "var(--q4)"), (8, "US", "var(--q3)"), (4, "SG", "var(--q2)")], 100, unit="%")}</div>
        <div><span class="note">ASN</span>{U.tall_bar([(38, "HiNet 3462", "var(--q5)"), (21, "Taiwan Mobile", "var(--q4)"), (14, "FarEasTone", "var(--q3)"), (9, "Google Cloud", "var(--q2)")], 100, unit="%")}</div>
        <div><span class="note">來源類型</span>{U.tall_bar([(52, "直接／未知", "var(--q5)"), (29, "搜尋", "var(--q4)"), (14, "推薦網站", "var(--q3)"), (5, "機器人推估", "var(--q2)")], 100, unit="%")}</div>
        <div><span class="note">裝置</span>{U.tall_bar([(63, "手機", "var(--q5)"), (33, "桌機", "var(--q4)"), (4, "其他", "var(--q3)")], 100, unit="%")}</div>
      </div>
    </section>"""
    api = f"""
    <section class="panel" aria-labelledby="h-apiu">
      <div class="ph"><h2 id="h-apiu">API 用量 · 使用者 × 服務</h2><a class="more" href="APIProxy.dc.html">API 代理 →</a></div>
      <div style="padding:12px 14px">{U.heatgrid(["tim", "lab-bot", "partner-a", "ci-runner"], ["/ollama", "/shop-admin", "/files", "/legacy"], [[1200, 800, 120, 0], [9900, 0, 0, 0], [3100, 0, 0, 0], [0, 2600, 0, 0]], vmax=10000)}</div>
      <div class="foot"><span>格＝今日請求數</span><span>lab-bot 今日 131 次超限</span></div>
    </section>"""
    anomaly = f"""
    <section class="panel" aria-labelledby="h-an">
      <div class="ph"><h2 id="h-an">異常流量</h2><span class="sub">從這裡直接建政策；事件、WAF 命中與部署版本都連得起來</span></div>
      <div style="padding:12px 14px;display:flex;flex-direction:column;gap:10px">
        <article class="card" style="background:var(--raised);border:1px solid var(--line);border-radius:8px;padding:12px"><div style="font-size:12px;color:var(--warn);font-weight:600">02:00–02:20 · 請求數 6.2× 基線</div><p style="font-weight:500">185.220.101.0/24 → shop、wiki · 1,880 請求 · WAF 規則 913100（掃描器 UA）命中 98%</p><p class="note">邊緣阻擋 3% · 來源站 4xx 11%</p><div class="wrap"><button type="button" class="btn pri sm" style="color:var(--hazelink)">封鎖此網段 24 小時</button><button type="button" class="btn sm">看事件</button></div></article>
      </div>
    </section>"""
    main = head + filt + views + lat + f'<div class="cg">{paths}{geo}</div>' + f'<div class="cg">{api}{anomaly}</div>'
    return U.shell("analytics", main)



# ---------- 宿主機系統管理（單一節點） ----------
def system() -> str:
    head = f"""
    <div style="display: flex; flex-wrap: wrap; align-items: center; gap: 14px">
      <div class="name">{U.crumbs([("主機", "Nodes.dc.html")])}<h1 class="mono">hazel-tw-01</h1></div>
      {U.status("ok", "連線中")}<span class="note">最後聯絡 2 秒前</span>
      <div class="wrap" style="margin-left: auto"><button type="button" class="btn">終端機</button><button type="button" class="btn">重新開機</button><button type="button" class="btn pri">套用 5 個安全更新</button></div>
    </div>"""
    tabs = ('<nav class="subtabs" aria-label="跳到">' + "".join(f'<a class="st2{" on" if i == 0 else ""}" href="#{h}"{" aria-current=true" if i == 0 else ""}>{t}</a>' for i, (t, h) in enumerate([("套件與更新", "h-pk"), ("服務與計時器", "h-sv"), ("使用者與 sudo", "h-us"), ("網路", "h-net"), ("掛載", "h-mnt"), ("時間與開關機", "h-time"), ("主機工具", "h-tools")])) + '</nav>')
    tools = f"""
    <section class="panel" aria-labelledby="h-tools">
      <div class="ph"><h2 id="h-tools">主機工具</h2></div>
      <div class="fleet" style="grid-template-columns:repeat(auto-fill,minmax(min(200px,100%),1fr))">
        <div class="ncard"><span class="note">Swap</span>{U.meter(12, "0.5 / 4 GiB")}<span class="note">swappiness 10</span><button type="button" class="btn sm" style="align-self:flex-start">調整</button></div>
        <div class="ncard"><span class="note">可清理空間</span>{U.tall_bar([(4.1, "未用映像", "var(--q5)"), (1.8, "系統日誌", "var(--q4)"), (0.9, "套件快取", "var(--q3)")], 8, unit="G")}<span class="note">共 6.8 GiB · 上次清理 10 月 30 日</span><button type="button" class="btn sm" style="align-self:flex-start">預覽清理</button></div>
        <div class="ncard"><span class="note">Supervisor</span><span>{U.status("ok", "3 / 3 執行中")}</span><span class="note">shop-cron、mail-relay、backup-runner</span><button type="button" class="btn sm" style="align-self:flex-start">管理程序</button></div>
        <div class="ncard"><span class="note">FTP／SFTP</span><span>{U.status("info", "SFTP only · 2 帳號")}</span><span class="note">FTP 明文已停用</span><button type="button" class="btn sm" style="align-self:flex-start">帳號</button></div>
      </div>
    </section>"""
    pk_tiles = '<div class="tiles">' + "".join([
        U.stat_tile("已安裝套件", "1,284", "", "apt · Debian 12 · 來源簽章有效", None),
        U.stat_tile("可更新", "23", "", "安全性 5 · 一般 18", False),
        U.stat_tile("維護時段", "03:00–04:00", "", "安全更新自動套用 · 需重開機的除外", None),
        U.stat_tile("上次更新", "10 月 29 日", "", "14 套件 · 成功 · 無需重開", None),
    ]) + "</div>"
    def pkg(name, cur, new, kind, restart, note):
        return (f'<a class="row srow" href="#p"><span class="name"><span class="n">{name}</span><span class="d">{note}</span></span><span>{kind}</span><span class="mono c3">{cur}</span><span class="mono c4">→ {new}</span><span>{restart}</span><span class="c5"></span><span></span></a>')
    pkgs = f"""
    <section class="panel" aria-labelledby="h-pk" data-sort="1:desc" data-batch data-pages="1–6 / 23">
      <div class="ph"><h2 id="h-pk">可更新 · 23</h2><span class="sub">安全性優先；會重啟服務或需要重開機的標出來，套用走工作中心</span><div class="wrap" style="margin-left:auto"><label class="inp" style="min-width:220px"><span class="note">搜尋</span><input placeholder="套件名稱、描述（含未安裝）" style="background:transparent;border:0;flex:1;min-width:0"></label><span class="chip on">安全性 5</span><span class="chip">一般 18</span><span class="chip">已保留 1</span>{U.impact_legend(["svc", "ctr", "reboot", "none"])}</div></div>
      <div class="row hd srow"><span>套件</span><span>類型</span><span class="c3">目前</span><span class="c4">新版</span><span>影響</span><span class="c5"></span><span></span></div>
      {pkg("openssl", "3.0.15-1", "3.0.15-1+deb12u2", U.status("dng", "安全性"), U.impacts([("svc", "zentinel、sshd")]), "CVE-2026-31337 · 高")}
      {pkg("libssl3", "3.0.15-1", "3.0.15-1+deb12u2", U.status("dng", "安全性"), U.impacts([("svc", "zentinel、sshd")]), "來源套件 openssl")}
      {pkg("linux-image-amd64", "6.1.112-1", "6.1.115-1", U.status("dng", "安全性"), U.impacts([("reboot", "核心 6.1.112 → 6.1.115")]), "核心 · 排進維護時段")}
      {pkg("curl", "7.88.1-10", "7.88.1-10+deb12u8", U.status("dng", "安全性"), U.impacts([("none", "")]), "")}
      {pkg("docker-ce", "27.1.0", "27.3.1", U.status("info", "一般"), U.impacts([("ctr", "這台 14 個容器"), ("svc", "docker、containerd")]), "需批准 · 已保留（hold）")}
      {pkg("postgresql-client-16", "16.4", "16.6", U.status("info", "一般"), U.impacts([("none", "")]), "")}
      <div class="note" style="padding:8px 14px;border-top:1px solid var(--line)">來源：deb.debian.org（簽章有效）、download.docker.com（簽章有效）· 移除套件時列出相依與受影響服務</div>
    </section>"""
    upd = f"""
    <section class="panel" aria-labelledby="h-upd">
      <div class="ph"><h2 id="h-upd">安全更新 · 預覽 5 套件</h2><span class="sub">這是一個工作；中途節點斷線不會重跑一半的 apt</span></div>
      <div style="padding:12px 14px">{U.pipeline([("預檢", "done", "即時", "空間、鎖、來源簽章"), ("下載", "todo", "預估 20 秒", "41 MiB"), ("安裝", "todo", "預估 1 分", "dpkg 不可中斷"), ("重啟服務", "todo", "zentinel、sshd", "各約 1 秒"), ("驗證", "todo", "—", "版本＋服務健康")])}
      <div class="note" style="margin-top:8px">核心更新另排 03:00 維護時段，重開機前列出受影響的 3 個容器</div></div>
    </section>"""
    def svc(name, st, kind, enabled, mem, cpu, note):
        return (f'<a class="row srow" href="#s"><span class="name"><span class="n">{name}</span><span class="d">{kind} · {note}</span></span><span>{st}</span><span class="c3">{enabled}</span><span class="c4">{U.meter(mem[0] / mem[1] * 100, f"{mem[0]}M")}</span><span class="c4 mono">{cpu}</span><span class="c5"></span><span><button type="button" class="btn sm">重啟</button></span></a>')
    services = f"""
    <section class="panel" aria-labelledby="h-sv" data-sort="0:asc" data-pages="1–7 / 141">
      <div class="ph"><h2 id="h-sv">systemd 服務與計時器 · 顯示 7 / 141</h2><span class="sub">啟停、開機啟用、設定檔編輯（有歷史）、日誌</span><div class="wrap" style="margin-left:auto"><span class="chip on">執行中 112</span><span class="chip">失敗 1</span><span class="chip">計時器 9</span><span class="chip">平台相關</span></div></div>
      <div class="row hd srow"><span>單元</span><span>狀態</span><span class="c3">開機</span><span class="c4">記憶體</span><span class="c4">CPU</span><span class="c5"></span><span></span></div>
      {svc("hazelminx-agent.service", U.status("warn", "執行中 · 48 秒沒心跳"), "服務", "啟用", (38, 128), "0.4%", "平台 · 勿停")}
      {svc("docker.service", U.status("ok", "執行中 · 12 天"), "服務", "啟用", (180, 1024), "1.2%", "3 容器")}
      {svc("zentinel.service", U.status("ok", "執行中 · 12 天"), "服務", "啟用", (94, 512), "1.8%", "入口")}
      {svc("sshd.service", U.status("ok", "執行中"), "服務", "啟用", (6, 64), "0%", "Port 22")}
      {svc("fail2ban.service", U.status("dng", "失敗 · 03:12"), "服務", "啟用", (0, 64), "—", "exit 1：jail 設定語法錯誤（第 41 行）")}
      {svc("apt-daily-upgrade.timer", U.status("ok", "下次 03:00"), "計時器", "啟用", (0, 1), "—", "安全更新")}
      {svc("hazelminx-backup.timer", U.status("ok", "下次 03:00"), "計時器", "啟用", (0, 1), "—", "shop-db")}
    </section>"""
    def urow(name, login, admin, keys, last):
        return (f'<div class="row"><span class="wrap" style="flex-wrap:nowrap">{U.icon("user", 14)}<span class="mono">{name}</span></span>'
                f'<span>{login}</span><span>{admin}</span><span class="c3">{keys}</span><span class="c4">{last}</span>'
                f'<span>{U.actions(["rename", "more"])}</span></div>')
    def tipst(kind, text, tip):
        return f'<span tabindex="0" data-tip="{tip}">{U.status(kind, text)}</span>'
    users = f"""
    <section class="panel" aria-labelledby="h-us" data-pages="1–4 / 4">
      <div class="ph"><h2 id="h-us">Linux 使用者</h2><span class="sub">這台主機上的 Linux 帳號，跟平台的登入帳號分開</span><div class="wrap" style="margin-left:auto"><span class="chip">顯示系統帳號（28）</span><button type="button" class="btn sm">{U.icon("plus", 14)}新增使用者</button></div></div>
      <div class="row hd"><span>帳號</span><span>登入方式</span><span>管理權限</span><span class="c3">SSH 金鑰</span><span class="c4">最後登入</span><span></span></div>
      {urow("root", tipst("warn", "密碼 · 281 天沒改", "root 可以用密碼登入，而且密碼 281 天沒改"), U.status("none", "root 本身"), "1 把", "03:38 · 本機")}
      {urow("tim", U.status("ok", "SSH 金鑰"), U.status("ok", "sudo · 不用密碼"), "2 把", "03:38 · 203.0.113.7")}
      {urow("deploy", U.status("ok", "SSH 金鑰"), tipst("warn", "docker 群組 · 等同 root", "docker 群組的成員可以把整個主機磁碟掛進容器，權限等同 root"), "1 把", "昨天 · CI")}
      {urow("www-data", U.status("none", "不能登入"), U.status("none", "無"), "—", "—")}
    </section>"""
    net = f"""
    <section class="panel" aria-labelledby="h-net">
      <div class="ph"><h2 id="h-net">網路</h2><span class="sub">改 IP、DNS、路由走變更計畫；錯了會把自己鎖在外面的改動，先給復原方法</span></div>
      <div style="padding:12px 14px;display:flex;flex-direction:column;gap:10px">
        {U.matrix(["eth0", "docker0", "cloudflared（tunnel）", "wg0"], ["狀態", "IPv4", "IPv6", "閘道", "流量 24 小時"], [
          [("ok", "UP · 1 Gb"), ("none", "203.0.113.22/24"), ("none", "2001:db8::22/64"), ("none", "203.0.113.1"), ("ok", "進 71G · 出 284G")],
          [("ok", "UP"), ("none", "172.17.0.1/16"), ("none", "—"), ("none", "—"), ("ok", "容器間")],
          [("ok", "UP"), ("none", "—"), ("none", "—"), ("none", "—"), ("ok", "4 連線")],
          [("none", "DOWN"), ("none", "10.8.0.2/24"), ("none", "—"), ("none", "—"), ("none", "0")],
        ], first_col="minmax(140px,1.2fr)")}
        <div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(min(220px,100%),1fr));gap:10px">{kv("DNS", "1.1.1.1、8.8.8.8 · 搜尋網域 example.tw", "ncard")}{kv("預設路由", "203.0.113.1 via eth0 · metric 100", "ncard")}{kv("/etc/hosts", "6 條 · 含 wiki.internal", "ncard")}{kv("主機名稱", "hazel-tw-01 · FQDN hazel-tw-01.example.tw", "ncard")}</div>
      </div>
    </section>"""
    mounts = f"""
    <section class="panel" aria-labelledby="h-mnt">
      <div class="ph"><h2 id="h-mnt">掛載與檔案系統</h2><span class="sub">fstab、網路儲存、裝置身分；未綁定的資料碟標紅</span><a class="more" href="Storage.dc.html">儲存與備份 →</a></div>
      <div class="row hd drow"><span>掛載點</span><span>裝置</span><span class="c5">檔案系統</span><span>使用</span><span class="c4">開機掛載 · 身分</span></div>
      <div class="row drow"><span class="mono">/</span><span class="mono">nvme0n1p2</span><span class="c5">ext4</span>{U.meter(38, "36 / 94 GiB", warn=80, danger=90)}<span class="c4">{U.status("ok", "fstab UUID")}</span></div>
      <div class="row drow"><span class="mono">/mnt/data</span><span class="mono">sdb1</span><span class="c5">ext4</span>{U.meter(44, "412 / 931 GiB", warn=80, danger=90)}<span class="c4">{U.status("ok", "fstab UUID")}</span></div>
      <div class="row drow"><span class="mono">/mnt/nfs/backup</span><span class="mono">hazel-tw-01:/mnt/data/nfs</span><span class="c5">nfs4</span>{U.meter(10, "19 / 200 GiB", warn=80, danger=90)}<span class="c4">{U.status("ok", "systemd automount")}</span></div>
      <div class="row drow"><span class="mono">swap</span><span class="mono">/swapfile</span><span class="c5">swap</span>{U.meter(12, "0.5 / 4 GiB", warn=80, danger=90)}<span class="c4">{U.status("ok", "fstab")}</span></div>
    </section>"""
    time_ = f"""
    <section class="panel" aria-labelledby="h-time">
      <div class="ph"><h2 id="h-time">時間、主機名稱、開關機</h2></div>
      <div class="fleet" style="grid-template-columns:repeat(auto-fill,minmax(min(220px,100%),1fr))">
        {kv("時區", "Asia/Taipei（節點）· 使用者顯示 Asia/Taipei", "ncard")}{kv("NTP", "chrony · pool.ntp.org · 偏差 +3 ms · 同步", "ncard")}{kv("主機名稱", "hazel-tw-01 · FQDN hazel-tw-01.example.tw", "ncard")}
        <div class="ncard"><span class="note">運行時間</span><span>41 天 · 上次重開 9 月 27 日（核心更新）</span><span class="note">待重開機：核心 6.1.115</span></div>
        <div class="ncard"><span class="note">重新開機會影響</span><span>{U.segmented([(3, "var(--danger)"), (0, "var(--q2)")])}</span><span class="note">shop-api ×2、shop-db、shop-redis、zentinel · 03:00 備份延後</span></div>
        <div class="ncard"><span class="note">root 密碼</span><span>{U.status("warn", "281 天未換")}</span><span class="note">上次變更 2 月 1 日</span></div>
      </div>
    </section>"""
    main = head + node_tabs("系統", 2) + tabs + pk_tiles + pkgs + upd + services + f'<div class="cg">{users}{net}</div>' + mounts + time_ + tools
    return U.shell("nodes", main)


# ---------- 身分與存取 ----------
def iam() -> str:
    head = U.page_head("身分與存取", OBS, '<a class="btn pri" href="UserInvite.dc.html">邀請同事</a><button type="button" class="btn">新增角色</button><button type="button" class="btn">發 API Token</button><button type="button" class="btn">外部登入</button>')
    tiles = '<div class="tiles">' + "".join([
        U.stat_tile("使用者", "5", "", "4 啟用 · 1 停權", None),
        U.stat_tile("二次驗證", "3", "/4 已啟用", "analyst 未設 · 高權限帳號必須", False),
        U.stat_tile("工作階段", "7", "", "tim 3 · deploy-bot 2 · 其他 2", None),
        U.stat_tile("待批准", "1", "", "變更計畫 #418 · 等了 44 分", None),
    ]) + "</div>"
    def user(name, role, mfa, sess, last, st):
        return (f'<a class="row srow" href="#u"><span class="name"><span class="n">{name}</span><span class="d">{role}</span></span><span>{st}</span><span class="c3">{mfa}</span><span class="mono c4">{sess}</span><span class="mono c5">{last}</span><span class="c5"></span><span></span></a>')
    users = f"""
    <section class="panel" aria-labelledby="h-usr" data-sort="0:asc" data-batch>
      <div class="ph"><h2 id="h-usr">使用者 · 5</h2><span class="sub">停權同時撤銷工作階段與平台 Token；本機救援帳號不受外部登入影響</span></div>
      <div class="row hd srow"><span>使用者</span><span>狀態</span><span class="c3">二次驗證</span><span class="c4">工作階段</span><span class="c4">最後登入</span><span class="c5"></span><span></span></div>
      {user("tim", "擁有者 · 本機救援帳號", U.status("ok", "TOTP ＋ Passkey ＋ 復原碼"), "3", "03:38 · 203.0.113.7", U.status("ok", "啟用"))}
      {user("deploy-bot", "部署者 · shop、wiki", U.status("info", "服務帳號 · Token"), "2", "02:57 · Webhook", U.status("ok", "啟用"))}
      {user("ops-ann", "維運者 · 全部節點", U.status("ok", "TOTP"), "1", "昨天 22:10 · OIDC", U.status("ok", "啟用"))}
      {user("analyst", "唯讀 · shop_analytics", U.status("warn", "未設定 · 7 天內必須"), "1", "3 天前", U.status("ok", "啟用"))}
      {user("old-contractor", "—", U.status("info", "—"), "0", "9 月 2 日", U.status("dng", "停權 · Token 已撤"))}
    </section>"""
    roles = f"""
    <section class="panel" aria-labelledby="h-roles">
      <div class="ph"><h2 id="h-roles">角色 × 資源範圍</h2><span class="sub">預設拒絕；格子＝這個角色對這類資源能做到哪；API 也照這張表擋</span></div>
      <div style="padding:12px 14px">{U.matrix(["擁有者", "維運者", "部署者", "唯讀", "稽核者"], ["節點", "容器", "專案部署", "檔案", "機密", "終端機", "使用者", "批准高風險"], [
        [("ok", "全部"), ("ok", "全部"), ("ok", "全部"), ("ok", "全部"), ("ok", "全部"), ("ok", "root"), ("ok", "全部"), ("ok", "是")],
        [("ok", "全部"), ("ok", "全部"), ("ok", "全部"), ("ok", "全部"), ("warn", "讀"), ("ok", "root"), ("none", "否"), ("warn", "需二人")],
        [("warn", "讀"), ("ok", "指定專案"), ("ok", "指定專案"), ("warn", "專案目錄"), ("warn", "指定專案"), ("warn", "容器內"), ("none", "否"), ("none", "否")],
        [("warn", "讀"), ("warn", "讀"), ("warn", "讀"), ("none", "否"), ("none", "否"), ("none", "否"), ("none", "否"), ("none", "否")],
        [("warn", "讀"), ("warn", "讀"), ("warn", "讀"), ("none", "否"), ("none", "否（遮罩）"), ("none", "否"), ("warn", "讀"), ("none", "否")],
      ], first_col="minmax(90px,.8fr)")}</div>
      <div class="note" style="padding:0 14px 10px">專案 A 的使用者猜專案 B 的資源 ID：查詢、下載、搜尋、事件串流一律 404，不洩漏存在</div>
    </section>"""
    def tok(name, owner, scope, days, used):
        pct = min(100, days / 90 * 100); col = "var(--danger)" if days < 14 else ("var(--warn)" if days < 30 else "var(--ok)")
        return (f'<div class="rank"><span class="mono" style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap">{name} <span class="note">· {owner} · {scope}</span></span><span class="bar" aria-hidden="true"><i style="width:{pct:.0f}%;background:{col}"></i></span><span class="mono" style="font-size:12px;color:{col}">{days} 天 <span class="note">· {used}</span></span></div>')
    tokens = f"""
    <section class="panel" aria-labelledby="h-tk">
      <div class="ph"><h2 id="h-tk">平台 API Token · 6</h2><span class="sub">有期限、範圍與用途；橫條＝剩餘效期（滿格 90 天）</span></div>
      <div class="ranks">{tok("hx_pat_3c1f…", "deploy-bot", "部署 shop、wiki", 61, "今天 02:57")}{tok("hx_pat_90bd…", "tim", "全部（CLI）", 77, "03:30")}{tok("hx_mcp_a7e2…", "ops-assistant", "讀取 · 提出計畫", 29, "03:12")}{tok("hx_pat_118e…", "ci-runner", "部署 shop staging", 12, "03:12")}{tok("hx_pat_55aa…", "old-contractor", "已撤銷", 0, "9/02")}</div>
    </section>"""
    sessions = f"""
    <section class="panel" aria-labelledby="h-ss">
      <div class="ph"><h2 id="h-ss">目前工作階段 · 7</h2><span class="sub">自己的可撤銷；管理員可撤銷任何人的</span></div>
      <div class="row hd srow"><span>使用者</span><span>來源</span><span class="c3">裝置</span><span class="c4">登入</span><span class="c4">到期</span><span class="c5"></span><span></span></div>
      <div class="row srow"><span class="name"><span class="n">tim</span><span class="d">這個瀏覽器</span></span><span class="mono">203.0.113.7 · 辦公室</span><span class="c3 note">macOS · Safari</span><span class="mono c4">03:38</span><span class="mono c4">8 小時</span><span class="c5"></span><span><button type="button" class="btn sm" disabled aria-disabled="true" style="opacity:.6">目前</button></span></div>
      <div class="row srow"><span class="name"><span class="n">tim</span><span class="d">CLI</span></span><span class="mono">203.0.113.7</span><span class="c3 note">hazelminx-cli 0.1</span><span class="mono c4">03:30</span><span class="mono c4">Token</span><span class="c5"></span><span><button type="button" class="btn sm">撤銷</button></span></div>
      <div class="row srow"><span class="name"><span class="n">tim</span><span class="d">手機</span></span><span class="mono">198.51.100.4 · 4G</span><span class="c3 note">iOS · Safari</span><span class="mono c4">昨天 23:10</span><span class="mono c4">5 小時</span><span class="c5"></span><span><button type="button" class="btn sm">撤銷</button></span></div>
      <div class="row srow"><span class="name"><span class="n">ops-ann</span><span class="d">OIDC · company.okta</span></span><span class="mono">198.51.100.20</span><span class="c3 note">Windows · Edge</span><span class="mono c4">昨天 22:10</span><span class="mono c4">2 小時</span><span class="c5"></span><span><button type="button" class="btn sm">撤銷</button></span></div>
    </section>"""
    audit = f"""
    <section class="panel" aria-labelledby="h-aud">
      <div class="ph"><h2 id="h-aud">稽核 · 24 小時</h2><span class="sub">誰在何時改了什麼，連回變更前後摘要、工作編號、節點與結果；機密遮罩</span><a class="more" href="Logs.dc.html">全部日誌 →</a></div>
      <div style="padding:12px 14px;display:flex;flex-direction:column;gap:8px">{U.state_timeline("o" * 24)}
      <div class="note" style="display:flex;justify-content:space-between"><span>昨天 03:42</span><span>現在 · 41 筆</span></div></div>
      <div class="row hd srow"><span>時間</span><span>誰</span><span class="c3">做了什麼</span><span class="c4">對象</span><span class="c4">結果</span><span class="c5"></span><span></span></div>
      <div class="row srow"><span class="mono">03:38:50</span><span>tim</span><span class="c3 note">觸發套件更新 openssl</span><span class="mono c4">hazel-jp-02 · 工作 #512</span><span class="c4">{U.status("warn", "狀態不明")}</span><span class="c5"></span><span></span></div>
      <div class="row srow"><span class="mono">03:10:12</span><span>tim</span><span class="c3 note">修改 inventory 3 列（前後摘要可看）</span><span class="mono c4">shop-db</span><span class="c4">{U.status("ok", "成功")}</span><span class="c5"></span><span></span></div>
      <div class="row srow"><span class="mono">02:58:30</span><span>系統</span><span class="c3 note">暫停自動部署，建立變更計畫 #418</span><span class="mono c4">shop production</span><span class="c4">{U.status("warn", "待批准")}</span><span class="c5"></span><span></span></div>
      <div class="row srow"><span class="mono">01:12:04</span><span>未知來源 185.220.101.9</span><span class="c3 note">登入失敗 ×5（不洩漏帳號）</span><span class="mono c4">管理入口</span><span class="c4">{U.status("dng", "來源限制 15 分")}</span><span class="c5"></span><span></span></div>
    </section>"""
    main = head + tiles + users + roles + f'<div class="cg">{tokens}{sessions}</div>' + audit
    return U.shell("iam", main)


# ---------- 平台設定 ----------
def settings() -> str:
    head = U.page_head("平台設定", OBS, f'<button type="button" class="btn">匯出設定</button><button type="button" class="btn">診斷包</button><button type="button" class="btn" aria-disabled="true" data-tip="需要第二位擁有者批准；送出後建立變更計畫" style="opacity:.6">{U.icon("lock", 14)}輪替主金鑰</button><button type="button" class="btn pri">檢查更新</button>')
    tabs = ('<div class="panel"><div class="tabs" role="tablist">' + "".join(f'<button type="button" class="tab{" on" if i == 0 else ""}" role="tab" aria-selected="{"true" if i == 0 else "false"}">{t}</button>' for i, t in enumerate(["基本", "管理入口與安全", "通知", "更新", "快照與匯入匯出", "整合", "遙測", "關於"])) + '</div></div>')
    def card(title, rows, note=""):
        body = "".join(f'<div style="display:flex;justify-content:space-between;gap:10px;min-height:32px;align-items:center;border-top:1px solid var(--line);padding:4px 0"><span class="note">{k}</span><span style="text-align:right">{v}</span></div>' for k, v in rows)
        n = f'<span class="note" style="margin-top:6px">{note}</span>' if note else ""
        return f'<div class="ncard"><span style="font-weight:500">{title}</span>{body}{n}</div>'
    basic = f"""
    <section class="panel" aria-labelledby="h-bs">
      <div class="ph"><h2 id="h-bs">基本</h2><span class="sub">每個設定有作用範圍：平台／節點／使用者</span></div>
      <div class="fleet">
        {card("面板", [("名稱", "HazelminX（榛果）"), ("語言", "繁體中文 · 英文可切"), ("外部位址", "https://panel.example.tw"), ("預設節點", "hazel-tw-01")], "品牌、favicon、登入頁、郵件、CLI、空畫面都是 HazelminX")}
        {card("時區", [("使用者顯示", "Asia/Taipei（tim）"), ("節點", "tw-01 Taipei · jp-02 Taipei · lab Taipei"), ("儲存", "UTC")], "節點時區與顯示時區分開，不混用")}
        {card("登入與工作階段", [("逾時", "8 小時 · 手機 5 小時"), ("長工作", "不因登入過期取消"), ("目前工作階段", "7 · 可撤銷")])}
      </div>
    </section>"""
    entrance = f"""
    <section class="panel" aria-labelledby="h-en">
      <div class="ph"><h2 id="h-en">管理入口與 HTTPS</h2><span class="sub">改入口前先確認可達，新入口可用前保留舊的；不會把自己鎖在外面</span></div>
      <div class="fleet">
        {card("監聽", [("位址", "0.0.0.0:8443 · tw-01"), ("網域", "panel.example.tw"), ("憑證", "自簽 · 內部 CA · 83 天"), ("衝突檢查", U.status("ok", "無"))])}
        {card("允許來源", [("CIDR", "203.0.113.0/24、Tunnel"), ("入口路徑", "/hx-a7f2（選用）"), ("不是唯一驗證", U.status("ok", "仍需登入＋二次驗證"))], "特殊路徑只是多一層，忘了路徑可用救援方式")}
        {card("變更復原", [("上次變更", "10 月 12 日 · Port 8443"), ("復原方法", "hazelminx-cli entrance reset"), ("救援帳號", "tim（本機）")])}
        {card("出站代理與 CA", [("代理", "無"), ("信任 CA", "內部 CA · 1 張"), ("跳過 TLS 驗證", U.status("ok", "禁止"))])}
      </div>
    </section>"""
    notify = f"""
    <section class="panel" aria-labelledby="h-nt">
      <div class="ph"><h2 id="h-nt">通知帳號與測試</h2><span class="sub">「送達」＝供應商接受，不等於有人看到；測試訊息不帶正式機密</span><a class="more" href="Events.dc.html">告警路由 →</a></div>
      <div class="row hd srow"><span>管道</span><span>狀態</span><span class="c3">用途</span><span class="c4">24 小時送達</span><span class="c4">上次測試</span><span class="c5"></span><span></span></div>
      <div class="row srow"><span class="name"><span class="n">Telegram · 維運群</span><span class="d">Bot hx-ops</span></span><span>{U.status("ok", "正常")}</span><span class="c3 note">嚴重 ＋ 警告</span><span class="c4">{U.meter(98, "47/48")}</span><span class="mono c4">03:31 成功</span><span class="c5"></span><span><button type="button" class="btn sm">送測試</button></span></div>
      <div class="row srow"><span class="name"><span class="n">Email · tim</span><span class="d">SMTP mail-relay</span></span><span>{U.status("ok", "正常")}</span><span class="c3 note">備援 ＋ 每日摘要</span><span class="c4">{U.meter(100, "3/3")}</span><span class="mono c4">昨天</span><span class="c5"></span><span><button type="button" class="btn sm">送測試</button></span></div>
      <div class="row srow"><span class="name"><span class="n">Webhook · 值班系統</span><span class="d">未設定</span></span><span>{U.status("info", "未設定")}</span><span class="c3 note">—</span><span class="c4 note">—</span><span class="mono c4">—</span><span class="c5"></span><span><button type="button" class="btn sm">設定</button></span></div>
    </section>"""
    update = f"""
    <section class="panel" aria-labelledby="h-up">
      <div class="ph"><h2 id="h-up">更新</h2><span class="sub">預設穩定版；不用上游 1Panel 更新器；每次更新有來源與相容資訊，失敗回到相容舊版</span></div>
      <div style="padding:12px 14px;display:flex;flex-direction:column;gap:12px">
        <div style="display:flex;gap:20px;flex-wrap:wrap">{kv("通道", "穩定版 · 每天檢查 04:00", "")}{kv("目前", "0.1.0（2026-11-06）· 來源 github.com/HazelnutParadise 簽章有效", "")}{kv("可用", "無新版", "")}</div>
        {U.pipeline([("檢查", "done", "04:00", "清單簽章有效"), ("下載", "todo", "—", "各架構成品"), ("安全點", "todo", "—", "無執行中的工作"), ("套用", "todo", "—", "Core → Agent"), ("驗證", "todo", "—", "失敗回 0.1.0")])}
        <div class="row hd srow"><span>元件</span><span>版本</span><span class="c3">相容</span><span class="c4">來源</span><span class="c4"></span><span class="c5"></span><span></span></div>
        <div class="row srow"><span class="mono">hazelminx-core</span><span class="mono">0.1.0</span><span class="c3">{U.status("ok", "基準")}</span><span class="c4 note">GitHub Release</span><span class="c4"></span><span class="c5"></span><span></span></div>
        <div class="row srow"><span class="mono">hazelminx-agent × 3</span><span class="mono">0.1.0</span><span class="c3">{U.status("ok", "相容")}</span><span class="c4 note">GitHub Release</span><span class="c4"></span><span class="c5"></span><span></span></div>
        <div class="row srow"><span class="mono">Zentinel × 2</span><span class="mono">0.6.54 / 0.6.50</span><span class="c3">{U.status("warn", "jp-02 可更新")}</span><span class="c4 note">官方發布</span><span class="c4"></span><span class="c5"></span><span></span></div>
        <div class="row srow"><span class="mono">Docker × 2 · Podman · K3s</span><span class="mono">27.3 / 27.1 · 5.2 · 1.31</span><span class="c3">{U.status("ok", "已驗證矩陣內")}</span><span class="c4 note">發行版來源</span><span class="c4"></span><span class="c5"></span><span></span></div>
      </div>
    </section>"""
    snap = f"""
    <section class="panel" aria-labelledby="h-sn">
      <div class="ph"><h2 id="h-sn">平台快照、匯入匯出、診斷包</h2><span class="sub">同一套備份與機密政策；匯出前可預覽內容，不自動送外部</span></div>
      <div class="fleet">
        {card("平台快照", [("排程", "每週日 04:00"), ("上次", "10 月 26 日 · 212 MiB"), ("內容", "設定、機密包（加密）、資料庫"), ("目的地", "本機 ＋ S3")])}
        {card("匯出設定", [("格式", "YAML · 可預覽"), ("機密", "不含（另走機密包）"), ("上次", "從未")])}
        {card("診斷包", [("內容", "版本、日誌（遮罩）、系統狀態"), ("遮罩", "IP、Token、密碼"), ("送出", "手動下載，不自動上傳")])}
      </div>
    </section>"""
    integ = f"""
    <section class="panel" aria-labelledby="h-int">
      <div class="ph"><h2 id="h-int">整合與遙測</h2><span class="sub">不需要的整合可以關；非必要遙測預設關閉</span></div>
      <div style="padding:12px 14px">{U.matrix(["管理 API /management/v1", "CLI", "MCP 服務", "Webhook 入站", "Webhook 出站", "GitHub 連線", "Cloudflare Analytics", "使用資訊回報"], ["狀態", "範圍", "最後使用"], [
        [("ok", "啟用"), ("none", "Token 範圍"), ("ok", "03:40")], [("ok", "啟用"), ("none", "tim · 擁有者"), ("ok", "03:30")], [("ok", "啟用"), ("none", "讀取 · 提出計畫"), ("ok", "03:12")],
        [("ok", "啟用"), ("none", "GitHub · 簽章驗證"), ("ok", "02:58")], [("none", "停用"), ("none", "—"), ("none", "—")], [("ok", "2 組"), ("none", "hazelnut 組織"), ("ok", "02:57")],
        [("warn", "未連接"), ("none", "需 Analytics 讀取 Token"), ("none", "—")], [("none", "關閉（預設）"), ("none", "—"), ("none", "—")],
      ], first_col="minmax(180px,1.4fr)")}</div>
    </section>"""
    main = head + tabs + basic + entrance + notify + update + f'<div class="cg">{snap}{integ}</div>' + U.savebar("通知設定：2 處未儲存", "按儲存才會生效", "儲存")
    return U.shell("settings", main)


# ---------- 日誌 ----------
def logs() -> str:
    head = U.page_head("日誌", OBS, '<button type="button" class="btn">保留政策</button><button type="button" class="btn">匯出</button><button type="button" class="btn pri">診斷包</button>')
    tabs = ('<div class="panel"><div class="tabs" role="tablist">' + "".join(f'<button type="button" class="tab{" on" if i == 0 else ""}" role="tab" aria-selected="{"true" if i == 0 else "false"}">{t}</button>' for i, t in enumerate(["操作稽核", "登入", "工作", "主機系統（journald）", "SSH 登入", "網站存取／錯誤", "平台後端"])) + '</div></div>')
    vol = U.ts_card("日誌量 · 每小時 · 全部來源", [U.Series("網站存取", [int(x) for x in walk(24, 3800, 900, 1000, 6000)], "var(--s1)"), U.Series("主機系統", [int(x) for x in walk(24, 400, 120, 100, 900, {22: 1800})], "var(--s2)"), U.Series("平台", [int(x) for x in walk(24, 120, 40, 20, 300)], "var(--s3)"), U.Series("稽核", [int(x) for x in walk(24, 6, 3, 0, 20)], "var(--s4)")],
                    HOURS, unit="", fmt_val=lambda v: f"{v / 1000:.1f}K" if v >= 1000 else f"{v:.0f}", markers=[(22, "jp-02 沒有回應")], foot="7 天、500 萬筆的查詢 p95 < 3 秒；太大的查詢先回「要不要改成工作查詢」· 保留：稽核 1 年、存取 30 天、系統 14 天")
    viewer = f"""
    <section class="panel" aria-labelledby="h-lv">
      <div class="ph"><h2 id="h-lv">主機系統 · hazel-jp-02 · journald</h2><span class="sub">虛擬捲動、可取消、保留篩選；有資料量與截斷提示</span><div class="wrap" style="margin-left:auto"><span class="chip on">tail 500</span><span class="chip">時間戳</span><span class="chip">反向</span><span class="chip">只看 err+</span><span class="chip">即時 5 秒</span></div></div>
      <div style="padding:10px 14px"><label class="inp" style="margin-bottom:8px"><span class="note">搜尋</span><input value="hazelminx-agent" aria-label="搜尋日誌" style="background:transparent;border:0;flex:1;min-width:0"></label>
      <div class="term">03:41:18 jp-02 hazelminx-agent[1180]: <span style="color:var(--ok)">INFO</span>  heartbeat ok seq=48812 rtt=41ms
03:41:28 jp-02 hazelminx-agent[1180]: <span style="color:var(--warn)">WARN</span>  heartbeat send failed: dial tcp 203.0.113.22:8443: i/o timeout
03:41:38 jp-02 hazelminx-agent[1180]: <span style="color:var(--warn)">WARN</span>  reconnect attempt 1/∞ backoff=2s
03:41:40 jp-02 kernel: <span style="color:var(--danger)">ERR</span>   eth0: NETDEV WATCHDOG: transmit queue 0 timed out
03:41:41 jp-02 systemd[1]: fail2ban.service: Main process exited, code=exited, status=1/FAILURE
03:41:41 jp-02 fail2ban-server[2210]: ERROR  Failed during configuration: /etc/fail2ban/jail.local:41 Bad value
… 顯示 500 / 18,204 筆（已截斷，縮小時間範圍或改成工作查詢）</div></div>
    </section>"""
    audit = f"""
    <section class="panel" aria-labelledby="h-la" data-sort="0:desc" data-pages="1–4 / 41">
      <div class="ph"><h2 id="h-la">操作稽核 · 今天 41 筆</h2><div class="wrap" style="margin-left:auto"><span class="chip on">全部</span><span class="chip">失敗 2</span><span class="chip">tim</span><span class="chip">系統</span><span class="chip">節點 jp-02</span></div></div>
      <div class="row hd srow"><span>時間</span><span>誰</span><span class="c3">動作</span><span class="c4">對象</span><span class="c4">結果</span><span class="c5"></span><span></span></div>
      <div class="row srow"><span class="mono">03:38:50</span><span>tim · 203.0.113.7</span><span class="c3 note">套件更新 openssl（工作 #512）</span><span class="mono c4">hazel-jp-02</span><span class="c4">{U.status("warn", "狀態不明")}</span><span class="c5"></span><span></span></div>
      <div class="row srow"><span class="mono">03:30:07</span><span>tim · CLI</span><span class="c3 note">hx jobs list</span><span class="mono c4">管理 API</span><span class="c4">{U.status("ok", "200")}</span><span class="c5"></span><span></span></div>
      <div class="row srow"><span class="mono">03:12:40</span><span>ci-runner · Token</span><span class="c3 note">部署 shop staging #71</span><span class="mono c4">lab-mini</span><span class="c4">{U.status("dng", "失敗")}</span><span class="c5"></span><span></span></div>
      <div class="row srow"><span class="mono">02:58:30</span><span>系統</span><span class="c3 note">自動部署暫停 → 變更計畫 #418</span><span class="mono c4">shop production</span><span class="c4">{U.status("warn", "待批准")}</span><span class="c5"></span><span></span></div>
    </section>"""
    ssh = f"""
    <section class="panel" aria-labelledby="h-ls">
      <div class="ph"><h2 id="h-ls">SSH 登入 · 24 小時 · 全部節點</h2><span class="sub">失敗來源會餵給 Fail2ban</span></div>
      {U.ranked([("185.220.101.9 · 失敗 ×214 · root", 214, "214 · 已封鎖"), ("45.33.12.9 · 失敗 ×88 · admin", 88, "88 · 已封鎖"), ("203.0.113.7 · 成功 ×6 · tim", 6, "6 · 辦公室"), ("198.51.100.20 · 成功 ×2 · deploy", 2, "2 · CI")], vmax=220)}
    </section>"""
    web = f"""
    <section class="panel" aria-labelledby="h-lw">
      <div class="ph"><h2 id="h-lw">網站存取 · shop.example.tw · 1 小時</h2><a class="more" href="Analytics.dc.html">流量分析 →</a></div>
      <div style="padding:12px 14px;display:flex;flex-direction:column;gap:8px">{U.tall_bar([(92, "2xx", "var(--ok)"), (5, "3xx", "var(--q3)"), (2, "4xx", "var(--warn)"), (1, "5xx", "var(--danger)")], 100, unit="%")}
      <div class="term" style="min-height:100px">03:42:01 203.0.113.7 GET /api/checkout 200 184ms cf-ray=8e1a… up=shop-api:8080
03:42:01 198.51.100.4 GET /static/app.js 304 2ms cache=HIT
03:42:02 185.220.101.9 GET /wp-login.php 404 1ms waf=detect:scanner-ua
03:42:03 203.0.113.7 POST /api/cart 502 60001ms up=shop-api:8080 err=upstream timeout</div></div>
    </section>"""
    def ret(kind, where, used, limit, keep, last, st):
        return (f'<div class="row srow"><span class="name"><span class="n">{kind}</span><span class="d mono">{where}</span></span><span>{st}</span>'
                f'<span class="c3">{U.meter(used, limit)}</span><span class="c4">{keep}</span><span class="c4 mono">{last}</span><span class="c5"></span><span>{U.actions(["more"])}</span></div>')
    retention = f"""
    <section class="panel" aria-labelledby="h-ret">
      <div class="ph"><h2 id="h-ret">保留與自動清理 · hazel-tw-01</h2><div class="wrap" style="margin-left:auto"><button type="button" class="btn sm">預覽並立即清理</button></div></div>
      <div class="row hd srow"><span>日誌</span><span>自動清理</span><span class="c3">用量 / 上限</span><span class="c4">保留</span><span class="c4">上次清理</span><span class="c5"></span><span></span></div>
      {ret("系統日誌（journald）", "SystemMaxUse · MaxRetentionSec", 61, "610 MiB / 1 GiB", "30 天", "03:00", U.status("ok", "開啟 · 預設"))}
      {ret("平台建立的容器", "每個容器 5 個檔 × 10 MiB", 34, "最多 50 MiB / 容器", "依大小輪替", "持續", U.status("ok", "開啟 · 預設"))}
      {ret("既有容器 · 3 個", "wiki-app、old-report、ollama", 0, "2.8 GiB · 沒有上限", "—", "—", U.status("warn", "沒有日誌輪替"))}
      {ret("Zentinel 存取日誌", "/var/log/zentinel", 40, "1.2 GiB / 3 GiB", "7 天", "03:00", U.status("ok", "開啟 · 預設"))}
      {ret("/var/log（logrotate）", "主機原本的設定", 22, "880 MiB", "依各組設定", "06:25", U.status("info", "沿用主機設定"))}
      {ret("稽核紀錄", "平台資料庫", 9, "180 MiB", "90 天 · 不自動清", "—", U.status("none", "不在自動清理"))}
      <div class="wrap" style="padding:10px 14px 12px"><button type="button" class="btn sm">下次部署時替既有容器加上輪替</button></div>
    </section>"""
    main = head + U.scope_bar([("全部節點", "3"), ("hazel-tw-01", "Docker"), ("hazel-jp-02", "Docker · 過期"), ("lab-mini", "Podman · K3s")]) + tabs + vol + retention + viewer + audit + f'<div class="cg">{ssh}{web}</div>'
    return U.shell("logs", main)


# ---------- 終端機 ----------
def terminal() -> str:
    head = U.page_head("終端機", OBS, '<button type="button" class="btn pri">新連線</button><button type="button" class="btn">快捷指令</button><button type="button" class="btn">設定</button>')
    def ttab(on, dot, ic, name, sub, label):
        return (f'<div class="ttab{" on" if on else ""}" role="tab" aria-selected="{"true" if on else "false"}" tabindex="{0 if on else -1}">'
                f'<span class="dot {dot}" aria-hidden="true"></span>{U.icon(ic, 14)}<span class="tn">{name}</span><span class="tu">{sub}</span>'
                f'<button type="button" class="ib" aria-label="關閉 {label}">{U.icon("x", 14)}</button></div>')
    tabs = ('<div class="panel"><div class="ttabs"><div role="tablist" aria-label="終端機分頁" style="display:flex">'
            + ttab(True, "ok", "nodes", "hazel-tw-01", "root " + U.tier("正式"), "hazel-tw-01 root")
            + ttab(False, "ok", "docker", "shop-api", "容器 · tw-01", "shop-api 容器")
            + ttab(False, "info", "nodes", "lab-mini", "deploy", "lab-mini deploy")
            + f'</div><span class="tadd"><button type="button" class="ib" aria-label="新分頁">{U.icon("plus")}</button></span>'
            + f'<span class="tend"><button type="button" class="ib" aria-label="縮到底部 Dock">{U.icon("dock")}</button><button type="button" class="ib" aria-label="全螢幕">{U.icon("max")}</button></span></div></div>')
    term = f"""
    <section class="panel" aria-labelledby="h-tm">
      <div class="ph"><h2 id="h-tm" class="mono">root@hazel-tw-01</h2><span class="note">會話 #3 · 03:36 開始 · 經 Agent</span><div class="wrap" style="margin-left:auto">{U.status("info", "錄製中")}{U.status("ok", "心跳 3 ms")}</div></div>
      <div style="padding:10px 14px"><div class="term" style="min-height:300px">root@hazel-tw-01:~# docker ps --format 'table {{{{.Names}}}}\t{{{{.Status}}}}'
NAMES             STATUS
shop-api-1        Up 46 minutes (healthy)
shop-api-2        Up 46 minutes (healthy)
shop-worker       Up 46 minutes
shop-db           Up 9 days (healthy)
shop-redis        Restarting (1) 12 seconds ago
ollama            Up 3 days
root@hazel-tw-01:~# journalctl -u hazelminx-agent -n 2 --no-pager
Nov 07 03:42:06 hazel-tw-01 hazelminx-agent[912]: INFO heartbeat ok seq=51204 rtt=3ms
Nov 07 03:42:16 hazel-tw-01 hazelminx-agent[912]: INFO heartbeat ok seq=51205 rtt=3ms
root@hazel-tw-01:~# <span style="background:var(--ink);color:var(--panel)"> </span></div></div>
    </section>"""
    hosts = f"""
    <section class="panel" aria-labelledby="h-hs">
      <div class="ph"><h2 id="h-hs">連線目標</h2><span class="sub">節點經 Agent；外部主機用 SSH 資產（金鑰在機密庫）</span></div>
      <div class="row hd srow"><span>目標</span><span>狀態</span><span class="c3">方式</span><span class="c4">身分</span><span class="c4">稽核</span><span class="c5"></span><span></span></div>
      <a class="row srow" href="#h1"><span class="name"><span class="n">hazel-tw-01</span><span class="d">節點</span></span><span>{U.status("ok", "連線中")}</span><span class="c3 note">Agent</span><span class="mono c4">root</span><span class="c4 note">完整錄製</span><span class="c5"></span><span><button type="button" class="btn sm">開啟</button></span></a>
      <a class="row srow" href="#h2"><span class="name"><span class="n">hazel-jp-02</span><span class="d">節點</span></span><span>{U.status("warn", "狀態過期")}</span><span class="c3 note">Agent</span><span class="mono c4">root</span><span class="c4 note">完整錄製</span><span class="c5"></span><span><button type="button" class="btn sm">開啟</button></span></a>
      <a class="row srow" href="#h3"><span class="name"><span class="n">lab-mini</span><span class="d">節點 · rootless</span></span><span>{U.status("info", "維護中")}</span><span class="c3 note">Agent</span><span class="mono c4">deploy（非 root）</span><span class="c4 note">指令紀錄</span><span class="c5"></span><span><button type="button" class="btn sm">開啟</button></span></a>
      <a class="row srow" href="#h4"><span class="name"><span class="n">nas.example.tw</span><span class="d">外部 SSH 資產</span></span><span>{U.status("ok", "可達")}</span><span class="c3 note">SSH · 金鑰 id_nas</span><span class="mono c4">backup</span><span class="c4 note">指令紀錄</span><span class="c5"></span><span><button type="button" class="btn sm">開啟</button></span></a>
      <a class="row srow" href="#h5"><span class="name"><span class="n">shop-api（容器）</span><span class="d">tw-01 · docker exec</span></span><span>{U.status("ok", "執行中")}</span><span class="c3 note">Agent · exec</span><span class="mono c4">node（容器內）</span><span class="c4 note">指令紀錄</span><span class="c5"></span><span><button type="button" class="btn sm">開啟</button></span></a>
    </section>"""
    cmds = f"""
    <section class="panel" aria-labelledby="h-qc">
      <div class="ph"><h2 id="h-qc">快捷指令 · 6</h2><span class="sub">可匯入；高風險指令要二次確認</span></div>
      {U.ranked([("docker ps（健康欄）", 41, "41 次"), ("journalctl -u hazelminx-agent -f", 18, "18 次"), ("df -h /mnt/data", 12, "12 次"), ("pg_isready -h shop-db", 7, "7 次"), ("systemctl restart zentinel ⚠", 3, "3 次"), ("apt list --upgradable", 2, "2 次")], vmax=45)}
    </section>"""
    policy = f"""
    <section class="panel" aria-labelledby="h-tp">
      <div class="ph"><h2 id="h-tp">稽核政策與設定</h2></div>
      <div class="fleet" style="grid-template-columns:repeat(auto-fill,minmax(min(220px,100%),1fr))">
        {kv("root 終端機", "完整錄製（輸入＋輸出）· 保留 90 天", "ncard")}{kv("一般帳號", "指令紀錄 · 保留 30 天", "ncard")}{kv("容器終端機", "指令紀錄 · 只有容器唯讀權限的人不能開宿主機 root", "ncard")}
        {kv("外觀", "Geist Mono 13px · 行高 1.4 · 深色", "ncard")}{kv("預設連線", "hazel-tw-01 · root", "ncard")}{kv("AI 輔助", "關閉（可選 ops-assistant 解釋輸出）", "ncard")}
      </div>
    </section>"""
    main = head + tabs + term + f'<div class="cg">{hosts}{cmds}</div>' + policy
    return U.shell("terminal", main)



# ---------- 安裝應用（1Panel 市集格式） ----------
def app_install() -> str:
    head = f"""
    <div class="phead"><div class="name">{U.crumbs([("應用商店", "AppStore.dc.html")])}<h1>安裝 Nextcloud</h1></div>
      <div class="wrap" style="margin-left:auto"><a class="btn" href="AppStore.dc.html">取消</a><button type="button" class="btn pri">安裝</button></div></div>"""
    def field(label, control, help_=""):
        return f'<div class="field"><label>{label}</label>{control}{f"<span class=help>{help_}</span>" if help_ else ""}</div>'
    def inp(val, ro=False, mono=True):
        return f'<div class="inp"><span class="{"mono" if mono else ""}" style="flex:1">{val}</span>{"" if not ro else U.icon("eye")}</div>'
    def sel(val):
        return f'<div class="inp"><span style="flex:1;min-width:0;overflow-wrap:anywhere">{val}</span>{U.icon("down")}</div>'
    meta = f"""
    <section class="panel">
      <div style="display:flex;gap:14px;padding:14px 16px;align-items:flex-start;flex-wrap:wrap">
        <span class="ic" aria-hidden="true" style="width:56px;height:56px;border-radius:12px;background:var(--q2);display:flex;align-items:center;justify-content:center;font-weight:700;font-size:18px">Nc</span>
        <div style="flex:1;min-width:220px;display:flex;flex-direction:column;gap:4px"><span style="font-weight:600;font-size:15px">Nextcloud <span class="note">· 儲存 · AGPL-3.0</span></span>
          <span class="note">來源 1Panel 官方商店 · key nextcloud · 版本 31.0.2 / 30.0.8 / 29.0.14 · 架構 amd64、arm64 · 需要記憶體 2 GiB · GPU 不需要</span>
          <div class="wrap"><a class="chip" href="#web">網站</a><a class="chip" href="#gh">GitHub</a><a class="chip" href="#doc">文件</a><a class="chip" href="#readme">README</a><a class="chip" href="#compose">docker-compose.yml</a><a class="chip" href="#script">安裝腳本</a></div></div>
        <div style="display:flex;flex-direction:column;gap:6px;min-width:200px">{U.status("ok", "漏洞：0 已知（31.0.2）")}{U.status("ok", "簽章：有效")}{U.status("info", "已安裝 0 · 上限 1")}</div>
      </div>
    </section>"""
    basic = f"""
    <section class="panel"><div class="ph"><h2>基本</h2></div>
      <div style="padding:14px 16px;display:grid;grid-template-columns:repeat(auto-fit,minmax(min(260px,100%),1fr));gap:14px">
        {field("名稱", inp("nextcloud", mono=True), "容器與資料目錄用這個名字")}{field("版本", sel("31.0.2（最新）"))}{field("節點", sel("hazel-tw-01 · Docker · amd64"))}{field("資料目錄", inp("/mnt/data/apps/nextcloud"), "剩 519 GiB · 綁定 UUID")}
      </div></section>"""
    params = f"""
    <section class="panel"><div class="ph"><h2>參數</h2><span class="sub">來自 data.yml</span></div>
      <div style="padding:14px 16px;display:grid;grid-template-columns:repeat(auto-fit,minmax(min(260px,100%),1fr));gap:14px">
        {field("HTTP Port（PANEL_APP_PORT_HTTP）", f'<div class="inp"><span class="mono" style="flex:1">8081</span>{U.status("ok", "未被占用")}</div>', "1024–65535 · 已檢查 tw-01 監聽與容器 -p")}
        {field("資料庫（PANEL_DB_HOST）", sel("新建 PostgreSQL 16（專用）"), "也可以重用既有資料庫；正式資料庫共用會互相影響")}
        {field("資料庫名稱（PANEL_DB_NAME）", inp("nextcloud"))}{field("資料庫帳號（PANEL_DB_USER）", inp("nextcloud"))}
        {field("資料庫密碼（PANEL_DB_USER_PASSWORD）", inp("•••••••••••••••• 隨機產生", ro=True), "存入機密庫，不進變更紀錄")}
        {field("快取（PANEL_REDIS_HOST）", sel("新建 Redis 7.4"))}
        {field("管理員帳號（NEXTCLOUD_ADMIN_USER）", inp("admin"))}{field("管理員密碼（NEXTCLOUD_ADMIN_PASSWORD）", inp("•••••••••••••••• 隨機產生", ro=True))}
        {field("信任網域（NEXTCLOUD_TRUSTED_DOMAINS）", inp("cloud.example.tw"))}
      </div></section>"""
    resources = f"""
    <section class="panel"><div class="ph"><h2>資源與網路</h2></div>
      <div style="padding:14px 16px;display:grid;grid-template-columns:repeat(auto-fit,minmax(min(260px,100%),1fr));gap:14px">
        {field("CPU 上限", inp("2 核"))}{field("記憶體上限", inp("4 GiB"), "最低 2 GiB")}{field("GPU", sel("不使用"))}
        {field("加入網路", '<div class="wrap"><span class="chip on">nextcloud_default（專用，自動建立）</span><span class="chip">hzx-ollama · 連 Ollama</span><span tabindex="0" data-tip="範本寫的共用網路；加入後會跟所有 1Panel 應用互通"><span class="chip">1panel-network</span></span><span class="chip">＋ 其他網路</span></div>', "安裝後也可以隨時加入或移出")}
        {field("資料位置 · /var/www/html", sel("/mnt/data/apps/nextcloud/html（預設）"), "每顆資料磁碟有預設位置；也可以選資料夾")}
        {field("資料位置 · /var/www/html/data", sel("/mnt/fast/hazelminx/apps/nextcloud/data"), "使用者檔案；放在比較快的磁碟")}
        {field("對外", sel("只透過 Zentinel（不開主機 Port）"), "選「開放主機 Port」會寫進防火牆矩陣")}{field("網域", inp("cloud.example.tw"), "Zentinel 站點 · 自動申請憑證")}{field("映像拉取", sel("安裝時拉取 · 登錄站 docker.io"))}
      </div></section>"""
    def cv(item, orig, ours):
        return f'<div class="row"><span>{item}</span><span class="mono">{orig}</span><span class="mono">{ours}</span></div>'
    advanced = f"""
    <section class="panel"><div class="ph"><h2>範本轉換</h2><span class="sub">只寫在平台的覆寫層，商店原始範本不改</span><div class="wrap" style="margin-left:auto"><span class="chip on">對照</span><span class="chip">編輯 compose 原始檔</span></div></div>
      <div class="row hd"><span>項目</span><span>範本原本</span><span>平台調整</span></div>
      {cv("網路", "1panel-network（外部）", "nextcloud_default")}
      {cv("容器名稱", "${CONTAINER_NAME}", "nextcloud")}
      {cv("Port", "8081:80（所有位址）", "127.0.0.1:8081:80 · Zentinel 對外")}
      {cv("資料", "./data", "依上面兩個資料位置")}
      {cv("時區", "Asia/Shanghai", "Asia/Taipei（主機設定）")}
      {cv("標籤", "createdBy: Apps", "保留 ＋ 平台服務 ID")}
    </section>"""
    preflight = f"""
    <section class="panel"><div class="ph"><h2>安裝前檢查</h2></div>
      <div style="padding:12px 16px">{U.pipeline([("節點", "done", "tw-01", "amd64 · Docker 27.3"), ("相依", "done", "PostgreSQL 16", "新建 · 專用"), ("Port", "done", "8081 空", ""), ("資料目錄", "done", "519 GiB", "綁定 UUID"), ("記憶體", "done", "20 GiB 可用", "需 2 GiB"), ("入口", "todo", "cloud.example.tw", "憑證申請")])}</div>
      <div style="padding:0 16px 12px;display:flex;flex-direction:column;gap:6px"><span class="note">安裝＝一個工作：拉取映像 → 建立網路與資料目錄 → 啟動 → 健康檢查 → 綁定入口。關掉頁面照跑。</span>
      <div class="wrap"><button type="button" class="btn pri">安裝</button><button type="button" class="btn">只建立不啟動</button><button type="button" class="btn">加入批次安裝</button></div></div>
    </section>"""
    main = head + meta + basic + params + f'<div class="cg">{resources}{advanced}</div>' + preflight
    return U.shell("store", main)


# ---------- 已安裝應用 › 網路與掛載 ----------
def app_settings() -> str:
    head = _form_head([("應用商店", "AppStore.dc.html"), ("已安裝", "AppStore.dc.html")], "nextcloud · 網路與掛載", '<a class="btn" href="AppStore.dc.html">返回</a>')
    def net(name, kind, sub, who, why, removable=True):
        act = U.actions(["more"]) if removable else f'<span tabindex="0" data-tip="應用自己的網路，移出後服務之間會連不到">{U.actions(["more"])}</span>'
        rm = '<button type="button" class="btn sm">移出</button>' if removable else ""
        return (f'<div class="row"><span class="name"><span class="n mono">{name}</span><span class="d">{kind} · {sub}</span></span>'
                f'<span>{who}</span><span class="note">{why}</span><span>{rm}</span><span>{act}</span></div>')
    nets = _fp("網路 · 2", f"""
      <div class="row hd"><span>網路</span><span>也在裡面的</span><span>為什麼加入</span><span></span><span></span></div>
      {net("nextcloud_default", "bridge", "172.21.0.0/16", "nextcloud-db、nextcloud-redis（專用）", "應用自己的網路", False)}
      {net("hzx-ollama", "bridge", "172.24.0.0/16", "ollama", "Nextcloud Assistant 連 Ollama")}
      <div class="wrap" style="padding:10px 14px 12px"><span style="flex:1;min-width:min(220px,100%)">{_dd("加入網路：選擇或新建")}</span><button type="button" class="btn">加入</button></div>
      <div class="wrap" style="padding:0 14px 12px">{U.status("info", "加入與移出不必重建容器")}<span tabindex="0" data-tip="平台用 docker network connect 立即生效，並寫進覆寫層，下次部署或更新也保留">{U.icon("info", 14)}</span></div>""")
    def mnt(inside, where, disk, size, kind):
        return (f'<div class="row"><span class="mono">{inside}</span><span class="name"><span class="n mono">{where}</span><span class="d">{kind}</span></span>'
                f'<span>{disk}</span><span class="mono">{size}</span><span><button type="button" class="btn sm">改位置</button></span></div>')
    mounts = _fp("資料掛載 · 3", f"""
      <div class="row hd"><span>容器內</span><span>主機上</span><span>磁碟</span><span>大小</span><span></span></div>
      {mnt("/var/www/html", "/mnt/data/apps/nextcloud/html", "sdb1 · /mnt/data", "1.4 GiB", "資料夾")}
      {mnt("/var/www/html/data", "/mnt/data/apps/nextcloud/data", "sdb1 · /mnt/data", "212 GiB", "資料夾 · 使用者檔案")}
      {mnt("/var/lib/postgresql/data", "nextcloud-db_data", "sda1 · /", "1.2 GiB", "Docker 儲存卷")}""")
    move = _fp("改位置 · /var/www/html/data", f"""
      <div style="padding:12px 16px;display:flex;flex-direction:column;gap:12px">
        {_grid(_fld("新位置", _dd("/mnt/fast（預設位置）"), "→ /mnt/fast/hazelminx/apps/nextcloud/data；也可以選資料夾"),
               _fld("方式", _seg(["搬移資料", "只改掛載點"], 0, "方式"), "新位置已經有資料時才用「只改掛載點」"),
               _fld("空間", U.status("ok", "需要 212 GiB · /mnt/fast 剩 641 GiB")),
               _fld("停機", U.status("warn", "約 2 分鐘"), "大部分在服務照跑時先複製，只有最後同步時停止"))}
        {U.pipeline([("空間檢查", "done", "即時", "212 / 641 GiB"), ("先複製", "todo", "服務照跑", "約 38 分"), ("停止 nextcloud", "todo", "", "約 2 分"), ("最後同步與核對", "todo", "", "檔案數與大小"), ("改掛載並啟動", "todo", "", "健康檢查"), ("保留舊資料", "todo", "", "7 天後再刪")])}
        <div class="wrap" style="justify-content:flex-end"><button type="button" class="btn">排進維護時段</button><button type="button" class="btn pri">開始搬移</button></div>
      </div>""")
    main = head + nets + mounts + move
    return U.shell("store", main)


# ---------- 邊界情境 ----------
def edge_cases() -> str:
    head = U.page_head("邊界情境", OBS, "")
    def card(title, body):
        return f'<section class="panel"><div class="ph"><h2>{title}</h2></div><div style="padding:12px 16px;display:flex;flex-direction:column;gap:8px">{body}</div></section>'
    big = card("大量資料：1,284 個容器", f"""{U.scope_bar([("全部節點", "1,284 容器"), ("tw-01", "612"), ("jp-02", "410"), ("lab-mini", "262")])}
      <div class="row hd crow"><span>容器</span><span>狀態</span><span class="c3">節點</span><span class="c4">CPU</span><span class="c4">記憶體</span><span class="c5">網路</span><span class="c5">重啟</span><span class="c5">Port</span><span></span></div>
      <div class="row crow"><span class="name"><span class="n">shop-api-00412</span><span class="d">ghcr.io/hazelnut/shop-api:a3f9c1e</span></span><span>{U.status("ok", "執行中")}</span><span class="mono c3">tw-01</span><span class="c4">{U.meter(21, "21%")}</span><span class="c4">{U.meter(40, "812M")}</span><span class="c5 mono">3.1 / 11.8</span><span class="c5 mono">0</span><span class="c5 mono">—</span>{U.actions(["log", "term", "restart", "more"])}</div>
      <div class="pager"><span>顯示 1–50 / 1,284 · 虛擬捲動 · 篩選保留在網址</span><span class="pg"><span>每頁 50</span></span></div>
      <span class="note">容器地圖超過 300 格時改成「節點 × 狀態」摘要格，點節點再展開</span>""")
    longname = card("超長名稱與路徑", f"""<div class="row cols-3"><span class="name"><span class="n">registry.internal.example.tw:5000/platform-team/backend-services/order-processing-worker-with-very-long-name:2026.11.07-rc.3-amd64</span><span class="d">/mnt/data/apps/order-processing-worker-with-very-long-name/data/uploads/2026/11/07</span></span><span>{U.status("ok", "執行中")}</span><span class="mono">tw-01</span></div>
      <span class="note">名稱可斷行最多 2 行，完整值在 title 與詳情；表格不用省略號吃掉尾端（tag 與副檔名在尾端）</span>""")
    conflict = card("並行編輯衝突", f"""<div class="banner" style="border-color:var(--warn)">{U.status("warn", "版本衝突")}<span>compose.yaml 在你編輯期間被 ops-ann 於 03:40:12 改過（v10）。你的版本基於 v9。</span></div>
      <div class="wrap"><button type="button" class="btn pri sm" style="color:var(--hazelink)">看差異（v9 → v10）</button><button type="button" class="btn sm">以我的版本覆蓋</button><button type="button" class="btn sm">放棄我的修改</button></div>
      <span class="note">所有寫入帶版本條件（規格 34 章）；衝突不自動合併</span>""")
    denied = card("權限不足", f"""<div class="banner" style="border-color:var(--danger)">{U.status("dng", "403")}<span>你的角色「部署者 · shop」不能重啟 hazel-tw-01 的 systemd 服務。</span></div>
      <div class="wrap"><button type="button" class="btn sm">要求擁有者批准（建立變更計畫）</button><button type="button" class="btn sm">查看我的權限</button></div>
      <span class="note">按鈕不藏起來，點了告訴你為什麼不行；API 同樣拒絕</span>""")
    half = card("半完成的操作", f"""{U.pipeline([("拉取", "done", "12 秒", ""), ("建立網路", "done", "1 秒", ""), ("啟動", "fail", "exit 125", "port 8081 已被占用"), ("健康檢查", "todo", "—", ""), ("入口", "todo", "—", "")])}
      <span class="note">已建立：網路 nextcloud_default、目錄 /mnt/data/apps/nextcloud（空）。未建立：容器、入口。</span>
      <div class="wrap"><button type="button" class="btn pri sm" style="color:var(--hazelink)">改 Port 後重試</button><button type="button" class="btn sm">清掉已建立的部分</button><button type="button" class="btn sm">保留現狀</button></div>""")
    clock = card("時鐘偏差與時區", f"""<div class="banner" style="border-color:var(--warn)">{U.status("warn", "偏差 +4 分 12 秒")}<span>hazel-jp-02 的時鐘比 Core 快 252 秒；憑證、Token 與排程以 Core 時間為準。</span></div>
      <span class="note">顯示一律帶時區（03:42 UTC+8）；節點時區 Asia/Tokyo 與使用者顯示時區不同時，排程頁兩個都列</span>""")
    upload = card("上傳中斷", f"""{U.meter(63, "2.6 / 4.2 GiB · 已中斷 03:41:30")}
      <div class="wrap"><button type="button" class="btn pri sm" style="color:var(--hazelink)">續傳（分片 312/512）</button><button type="button" class="btn sm">放棄並刪除暫存</button></div>
      <span class="note">分片存 Agent 本機暫存 24 小時；重新整理或換裝置都可續</span>""")
    full = card("磁碟滿", f"""<div class="banner" style="border-color:var(--danger)">{U.status("dng", "/mnt/data 100%")}<span>hazel-jp-02 寫入失敗：No space left on device。平台資料庫在 / 不受影響。</span></div>
      <div class="wrap"><button type="button" class="btn pri sm" style="color:var(--hazelink)">清理可回收 31 GiB</button><button type="button" class="btn sm">停止寫入最多的容器 wiki-media</button><button type="button" class="btn sm">搬移資料</button></div>""")
    timeout = card("API 逾時與部分失敗", f"""<div class="banner">{U.status("warn", "2／3 節點回應")}<span>lab-mini 在 8 秒內未回應；以下是其餘節點的結果。</span></div>
      <div class="wrap"><button type="button" class="btn sm">重試 lab-mini</button><button type="button" class="btn sm">改成背景工作</button></div>
      <span class="note">批次操作逐一列結果：成功 2、失敗 0、未回應 1；不把未回應算成失敗</span>""")
    unknown = card("不認識的引擎版本／欄位", f"""<div class="banner">{U.status("info", "Docker 29.0.0 未在驗證矩陣")}<span>已驗證 27.x–28.x。只開放讀取與升級路徑；建立／刪除先拒絕。</span></div>
      <span class="note">compose 裡不認識的合法欄位（x-hazel-*、新 schema）原樣保留，不刪不改</span>""")
    deleting = card("刪除仍被引用的東西", f"""<div class="banner" style="border-color:var(--danger)">{U.status("dng", "無法刪除 shop-db")}<span>仍被 shop 應用引用；備份任務 shop-db 每日 也指向它。</span></div>
      <div class="wrap"><button type="button" class="btn sm">看引用</button><button type="button" class="btn sm" disabled aria-disabled="true" style="opacity:.6">刪除</button></div>""")
    tabs = card("同一帳號多個分頁", f"""<div class="banner">{U.status("info", "另一個分頁正在編輯")}<span>這個 compose 已在你的另一個分頁（03:39 開啟）打開；兩邊都能存，後存的會遇到版本衝突。</span></div>""")
    main = head + big + f'<div class="cg">{longname}{conflict}</div><div class="cg">{denied}{tabs}</div><div class="cg">{half}{clock}</div><div class="cg">{upload}{full}</div><div class="cg">{timeout}{unknown}</div>{deleting}'
    return U.shell("docker", main)



# ---------- 宿主機服務模組（Webmin 等價） ----------
def host_services() -> str:
    head = U.page_head("主機", OBS, '<button type="button" class="btn pri">安裝模組</button><button type="button" class="btn">新增 adapter</button>') + nodes_tabs("服務模組")
    scope = U.scope_bar([("全部節點", "3"), ("hazel-tw-01", "Docker"), ("hazel-jp-02", "過期"), ("lab-mini", "Podman")])
    tiles = '<div class="tiles">' + "".join([
        U.stat_tile("已安裝模組", "7", "/12", "3 節點合計", None),
        U.stat_tile("設定驗證失敗", "1", "", "BIND @ jp-02 · named-checkconf", False),
        U.stat_tile("待套用變更", "2", "", "Samba 分享、logrotate", None),
        U.stat_tile("最近回滾", "1", "", "Nginx vhost · 10 月 30 日", None),
    ]) + "</div>"
    mx = U.matrix(["BIND DNS", "DHCP（Kea）", "Postfix／Dovecot", "Samba", "NFS exports", "Apache（原生）", "Nginx（原生）", "logrotate", "磁碟配額", "PAM", "MIME", "CUPS"], ["hazel-tw-01", "hazel-jp-02", "lab-mini"], [
        [("ok", "9.18 · 3 zone"), ("dng", "驗證失敗"), ("none", "未安裝")],
        [("none", "未安裝"), ("ok", "2.4 · 41 租約"), ("none", "未安裝")],
        [("ok", "3.8 · 佇列 0"), ("none", "未安裝"), ("none", "未安裝")],
        [("ok", "4.19 · 3 分享"), ("warn", "待套用"), ("none", "未安裝")],
        [("ok", "2 exports"), ("none", "未安裝"), ("none", "未安裝")],
        [("none", "未安裝"), ("none", "未安裝"), ("none", "未安裝")],
        [("ok", "1.26 · 2 vhost"), ("none", "未安裝"), ("info", "已安裝未啟用")],
        [("ok", "14 組"), ("warn", "待套用"), ("ok", "6 組")],
        [("ok", "/mnt/data 啟用"), ("none", "未啟用"), ("none", "未啟用")],
        [("ok", "12 服務"), ("ok", "12 服務"), ("ok", "11 服務")],
        [("ok", "1,842 型別"), ("ok", "1,842 型別"), ("ok", "1,842 型別")],
        [("none", "未安裝"), ("none", "未安裝"), ("ok", "1 印表機")],
    ], first_col="minmax(150px,1.2fr)")
    modules = f"""
    <section class="panel" aria-labelledby="h-mods">
      <div class="ph"><h2 id="h-mods">模組 × 節點</h2>{U.legend([("ok", "執行中 · 驗證通過"), ("warn", "待套用"), ("dng", "驗證失敗"), ("info", "已安裝未啟用")])}</div>
      <div style="padding:12px 16px">{mx}</div>
    </section>"""
    zones = f"""
    <section class="panel" aria-labelledby="h-z" data-sort="0:asc">
      <div class="ph"><h2 id="h-z">BIND DNS @ hazel-tw-01 · zone · 3</h2><div class="wrap" style="margin-left:auto"><span class="chip on">結構化</span><span class="chip">named.conf 原始檔</span><span class="chip">版本 7</span><button type="button" class="btn sm pri" style="color:var(--hazelink)">新增 zone</button></div></div>
      <div class="row hd srow"><span>zone</span><span>驗證</span><span class="c3">類型</span><span class="c4">序號</span><span class="c4">紀錄</span><span class="c5">轉送</span><span></span></div>
      <a class="row srow" href="#z1"><span class="name"><span class="n">example.tw</span><span class="d">/etc/bind/zones/db.example.tw</span></span><span>{U.status("ok", "checkzone 通過")}</span><span class="c3">主要</span><span class="mono c4">2026110702</span><span class="mono c4">18</span><span class="mono c5">→ 203.0.113.53</span>{U.actions(["log", "restart", "more"])}</a>
      <a class="row srow" href="#z2"><span class="name"><span class="n">internal.example.tw</span><span class="d">/etc/bind/zones/db.internal</span></span><span>{U.status("ok", "checkzone 通過")}</span><span class="c3">主要</span><span class="mono c4">2026110401</span><span class="mono c4">41</span><span class="mono c5">—</span>{U.actions(["log", "restart", "more"])}</a>
      <a class="row srow" href="#z3"><span class="name"><span class="n">113.0.203.in-addr.arpa</span><span class="d">反解</span></span><span>{U.status("ok", "checkzone 通過")}</span><span class="c3">主要</span><span class="mono c4">2026103001</span><span class="mono c4">9</span><span class="mono c5">—</span>{U.actions(["log", "restart", "more"])}</a>
    </section>"""
    def rec(name, typ, val, ttl, note=""):
        return f'<div class="row srow"><span class="name"><span class="n">{name}</span><span class="d">{note}</span></span><span class="mono">{typ}</span><span class="mono c3 wr">{val}</span><span class="mono c5">{ttl}</span><span class="c4"></span><span class="c5"></span>{U.actions(["rename", "trash", "more"])}</div>'
    records = f"""
    <section class="panel" aria-labelledby="h-r" data-sort="0:asc" data-batch data-pages="1–6 / 18">
      <div class="ph"><h2 id="h-r">example.tw · 紀錄 · 18</h2><div class="wrap" style="margin-left:auto"><label class="inp" style="min-width:200px;min-height:32px"><span class="note">搜尋</span><input placeholder="名稱、值" aria-label="搜尋紀錄" style="background:transparent;border:0;flex:1;min-width:0"></label><button type="button" class="btn sm pri" style="color:var(--hazelink)">新增紀錄</button></div></div>
      <div class="row hd srow"><span>名稱</span><span>類型</span><span class="c3">值</span><span class="c5">TTL</span><span class="c4"></span><span class="c5"></span><span></span></div>
      {rec("@", "SOA", "ns1.example.tw. hostmaster.example.tw. 2026110702", "3600")}
      {rec("@", "NS", "ns1.example.tw.", "3600")}
      {rec("@", "A", "203.0.113.22", "300", "hazel-tw-01")}
      {rec("shop", "A", "203.0.113.22", "300", "Zentinel 站點 shop.example.tw")}
      {rec("wiki", "CNAME", "hazel-jp-02.example.tw.", "300")}
      {rec("@", "MX", "10 mail.example.tw.", "3600", "Postfix @ tw-01")}
      <div class="pager" style="border-top:0"><span>待套用：新增 <span class="mono">cloud A 203.0.113.22</span>（未驗證）</span><span class="pg"><button type="button" class="btn sm">驗證（checkzone）</button><button type="button" class="btn sm pri" style="color:var(--hazelink)">套用並重載</button></span></div>
    </section>"""
    apply = f"""
    <section class="panel" aria-labelledby="h-ap">
      <div class="ph"><h2 id="h-ap">套用流程</h2></div>
      <div style="padding:12px 16px">{U.pipeline([("寫入暫存", "done", "即時", "db.example.tw.new"), ("named-checkzone", "todo", "—", "序號 → 2026110703"), ("named-checkconf", "todo", "—", ""), ("存版本 8", "todo", "—", "可回滾到 7"), ("rndc reload", "todo", "—", "example.tw"), ("驗證解析", "todo", "—", "dig @127.0.0.1")])}</div>
    </section>"""
    history = f"""
    <section class="panel" aria-labelledby="h-h">
      <div class="ph"><h2 id="h-h">設定版本 · example.tw</h2></div>
      <div class="row hd srow"><span>版本</span><span>結果</span><span class="c3">誰</span><span class="c4">時間</span><span class="c4"></span><span class="c5"></span><span></span></div>
      <div class="row srow"><span class="name"><span class="n">v7</span><span class="d">新增 wiki CNAME</span></span><span>{U.status("ok", "目前")}</span><span class="c3">tim</span><span class="mono c4">11/07 02:10</span><span class="c4"></span><span class="c5"></span><span><button type="button" class="btn sm">與上一版比較</button></span></div>
      <div class="row srow sel"><span class="name"><span class="n">v6</span><span class="d">改 shop A</span></span><span>{U.status("ok", "套用成功")}</span><span class="c3">tim</span><span class="mono c4">11/04 21:30</span><span class="c4"></span><span class="c5"></span><span><button type="button" class="btn sm on" aria-pressed="true">與目前比較</button></span></div>
      <div class="row srow"><span class="name"><span class="n">v5</span><span class="d">MX 改 mail</span></span><span>{U.status("dng", "checkzone 失敗 · 未套用")}</span><span class="c3">ops-ann</span><span class="mono c4">10/30 11:02</span><span class="c4"></span><span class="c5"></span><span><button type="button" class="btn sm">看輸出</button></span></div>
    </section>"""
    rdiff = f"""
    <section class="panel" aria-labelledby="h-rd">
      <div class="ph"><h2 id="h-rd">回滾到 v6 會改變的內容 · db.example.tw</h2><span class="note">v7（目前）→ v8（內容同 v6）</span><div class="wrap" style="margin-left:auto"><button type="button" class="btn sm">關閉</button><button type="button" class="btn sm pri">回滾到 v6</button></div></div>
      <div class="diff" style="padding:8px 0">
        <div><span class="ln">1</span><span class="ln">1</span><span>  $TTL 3600</span></div>
        <div class="del"><span class="ln">2</span><span class="ln"></span><span>- @  IN SOA ns1.example.tw. hostmaster.example.tw. 2026110702 3600 900 1209600 300</span></div>
        <div class="add"><span class="ln"></span><span class="ln">2</span><span>+ @  IN SOA ns1.example.tw. hostmaster.example.tw. 2026110703 3600 900 1209600 300</span></div>
        <div><span class="ln">14</span><span class="ln">14</span><span>  shop   IN A      203.0.113.10</span></div>
        <div class="del"><span class="ln">15</span><span class="ln"></span><span>- wiki   IN CNAME  hazel-jp-02.example.tw.</span></div>
        <div><span class="ln">16</span><span class="ln">15</span><span>  @      IN MX 10  mail.example.tw.</span></div>
      </div>
      <div class="wrap" style="padding:10px 14px;border-top:1px solid var(--line)"><span class="note">影響</span>{U.status("dng", "wiki.example.tw 停止解析")}{U.status("info", "序號 2026110702 → 2026110703")}{U.status("info", "通知次要 DNS ns2.example.tw")}</div>
    </section>"""
    def mrow(mod, node, ver, svc, boot, acts):
        return (f'<div class="row mrow"><span class="name"><span class="n">{mod}</span><span class="d mono">{node}</span></span><span class="mono c3">{ver}</span>'
                f'<span>{svc}</span><span class="c4">{boot}</span><span class="wrap" style="justify-content:flex-end;flex-wrap:nowrap">{acts}</span></div>')
    def mb(t, pri=False):
        return f'<button type="button" class="btn sm {"c4 pri" if pri else "c5"}">{t}</button>'
    mm = f'<button type="button" class="ib" aria-label="更多動作">{U.icon("more")}</button>'
    installed = f"""
    <section class="panel" aria-labelledby="h-inst" data-sort="0:asc" data-batch>
      <div class="ph"><h2 id="h-inst">已安裝模組 · 7</h2><div class="wrap" style="margin-left:auto"><button type="button" class="btn sm">停止選取</button><button type="button" class="btn sm">移除選取</button></div></div>
      <div class="row hd mrow"><span>模組</span><span class="c3">版本</span><span>服務</span><span class="c4">開機啟動</span><span></span></div>
      {mrow("BIND DNS", "hazel-tw-01 · named", "9.18.28", U.status("ok", "執行中"), "開", mb("停止") + mb("重新啟動") + mb("重新安裝") + mm)}
      {mrow("BIND DNS", "hazel-jp-02 · named", "9.18.28", U.status("dng", "設定驗證失敗"), "開", mb("看輸出", True) + mb("重新安裝") + mm)}
      {mrow("Postfix／Dovecot", "hazel-tw-01 · postfix、dovecot", "3.7.11 · 2.3.19", U.status("ok", "執行中"), "開", mb("停止") + mb("重新啟動") + mb("重新安裝") + mm)}
      {mrow("Samba", "hazel-tw-01 · smbd、nmbd", "4.19.5", U.status("ok", "執行中"), "開", mb("停止") + mb("重新啟動") + mb("重新安裝") + mm)}
      {mrow("Nginx（原生）", "hazel-tw-01 · nginx", "1.26.2", U.status("ok", "執行中"), "開", mb("停止") + mb("重新啟動") + mb("重新安裝") + mm)}
      {mrow("Nginx（原生）", "lab-mini · nginx", "1.26.2", U.status("info", "已停止"), "關", mb("啟動", True) + mb("重新安裝") + mb("移除") + mm)}
      {mrow("DHCP（Kea）", "hazel-jp-02 · kea-dhcp4", "2.4.1", U.status("ok", "執行中"), "開", mb("停止") + mb("重新啟動") + mb("重新安裝") + mm)}
    </section>"""
    remove = f"""
    <section class="chart" aria-label="移除模組確認範例"><div class="ph"><h2>移除模組</h2></div>
      <div class="body" style="padding:12px 14px"><div class="modal" role="alertdialog" aria-labelledby="h-rm" style="max-width:none">
        <h2 id="h-rm" style="font-size:16px">移除 BIND DNS @ <span class="mono">hazel-tw-01</span>？</h2>
        <div class="field"><span class="note">移除到哪裡</span>
          <label class="opt"><input type="radio" name="rmk"> 只停用：停止 named、關閉開機啟動，套件與設定保留</label>
          <label class="opt"><input type="radio" name="rmk" checked> 移除套件，保留 /etc/bind 與 zone 資料</label>
          <label class="opt"><input type="radio" name="rmk"> 移除套件與設定（先備份到 backups/2026-11）</label></div>
        <div class="card" style="gap:4px"><span style="font-size:12px;font-weight:600">會停止解析的 zone · 3</span><span class="mono" style="font-size:13px">example.tw、shop.example.tw、10.8.0.in-addr.arpa</span><span class="note">對外 53/udp、53/tcp 規則一併移除 · 次要 DNS ns2.example.tw 失去來源</span></div>
        <div class="field"><label for="rmc">輸入 <span class="mono">bind@hazel-tw-01</span> 以確認</label><div class="inp"><input id="rmc" style="background:transparent;border:0;flex:1"></div></div>
        <div class="wrap" style="justify-content:flex-end"><button type="button" class="btn">取消</button><button type="button" class="btn dng" disabled aria-disabled="true" style="opacity:.6">移除</button></div>
      </div></div></section>"""
    main = head + scope + tiles + modules + installed + zones + records + f'<div class="cg">{apply}{history}</div>' + U.savebar("example.tw 紀錄：1 筆新增、1 筆修改", "按套用才會寫進 BIND，並先跑 named-checkzone", "套用到 BIND") + rdiff + remove
    return U.shell("svcmod", main)


# ---------- 容器引擎未安裝 ----------
def _chk(kind, title, ev, res):
    return (f'<div class="check"><span class="mk {kind}" aria-hidden="true"></span><span><strong>{title}</strong><br><span class="ev">{ev}</span></span>'
            f'<span class="note">{res}</span></div>')


def _choice(name, ver, lines, btn, on=False, href=None):
    tag = "a" if href else "div"
    h = f' href="{href}"' if href else ""
    return (f'<{tag} class="ncard{" on" if on else ""}"{h}><div class="nh"><span class="n">{name}</span><span class="d">{ver}</span></div>'
            + "".join(f'<span class="note">{l}</span>' for l in lines) + f'<div class="wrap">{btn}</div></{tag}>')


def containers_empty() -> str:
    head = U.page_head("容器", OBS, '<button type="button" class="btn">重新偵測</button>')
    scope = U.scope_bar([("hazel-tw-01", "Docker"), ("hazel-jp-02", "Docker"), ("lab-mini", "Podman"), ("lab-pi-02", "未安裝")], current=3, label="節點")
    choose = f"""
    <section class="panel" aria-labelledby="h-eng">
      <div class="ph"><h2 id="h-eng">lab-pi-02 沒有容器引擎</h2><span class="note mono">Debian 12 · amd64 · 4 核 · 8 GiB · 偵測 03:41:50</span></div>
      <div class="fleet choices">
        {_choice("Docker Engine", "27.3.1 · download.docker.com", ["含 Compose v2.29、Buildx 0.17", "服務 docker、containerd", "會在 iptables 新增 DOCKER 鏈"], '<button type="button" class="btn sm pri" aria-pressed="true">已選擇</button>', on=True)}
        {_choice("Podman", "4.3.1 · Debian 官方套件", ["rootful 或 rootless（指定使用者）", "不常駐背景服務；socket 依需要啟用", "相容 Compose 檔（podman compose）"], '<button type="button" class="btn sm">選擇</button>')}
        {_choice("K3s", "Kubernetes 發行版", ["叢集層級的安裝在 Kubernetes 頁", "會內建 containerd，與 Docker 分開管理"], '<span class="btn sm">到 Kubernetes 頁</span>', href="KubernetesEmpty.dc.html")}
      </div>
    </section>"""
    checks = f"""
    <section class="panel" aria-labelledby="h-epre">
      <div class="ph"><h2 id="h-epre">預檢 · Docker Engine 27.3.1 @ lab-pi-02</h2><span class="note">1 警告 · 6 通過 · 03:42:02</span></div>
      {_chk("ok", "作業系統與架構支援", "Debian 12 (bookworm) · amd64", "通過")}
      {_chk("ok", "套件管理鎖可取得", "/var/lib/dpkg/lock-frontend 未被占用", "通過")}
      {_chk("ok", "磁碟空間", "/var/lib/docker 所在 / 剩 38 GiB，需要 0.6 GiB", "通過")}
      {_chk("ok", "沒有衝突套件", "docker.io、podman-docker、containerd（非 K3s）皆未安裝", "通過")}
      {_chk("ok", "套件來源簽章", "download.docker.com · 金鑰 9DC8 5822 9FC7 DD38 854A E2D8 8D81 803C 0EBF CD88", "通過")}
      {_chk("ok", "核心模組", "overlay、br_netfilter 可載入", "通過")}
      {_chk("warn", "ufw 啟用中", "Docker 發布的 Port 會繞過 ufw；平台會在 DOCKER-USER 預設擋外部連線", "警告")}
    </section>"""
    plan = f"""
    <section class="panel" aria-labelledby="h-eplan">
      <div class="ph"><h2 id="h-eplan">變更計畫</h2><div class="wrap" style="margin-left:auto"><button type="button" class="btn sm">下載離線包</button><button type="button" class="btn sm pri">批准並安裝</button></div></div>
      <div class="gdev">
        <div><h3>新增套件 · 5</h3><dl class="dl"><dt>docker-ce</dt><dd class="mono">27.3.1</dd><dt>docker-ce-cli</dt><dd class="mono">27.3.1</dd><dt>containerd.io</dt><dd class="mono">1.7.22</dd><dt>buildx</dt><dd class="mono">0.17.1</dd><dt>compose</dt><dd class="mono">2.29.7</dd></dl></div>
        <div><h3>新增服務</h3><dl class="dl"><dt>docker.service</dt><dd>開機啟動</dd><dt>containerd.service</dt><dd>開機啟動</dd><dt>docker.socket</dt><dd class="mono">0660 root:docker</dd></dl></div>
        <div><h3>網路與防火牆</h3><dl class="dl"><dt>網橋</dt><dd class="mono">docker0 172.17.0.0/16</dd><dt>iptables</dt><dd class="mono">DOCKER、DOCKER-FORWARD 鏈</dd><dt>平台規則</dt><dd class="mono">DOCKER-USER：擋外部到發布 Port</dd></dl></div>
        <div><h3>設定 /etc/docker/daemon.json</h3><dl class="dl"><dt>日誌</dt><dd class="mono">local · 每容器 100 MiB</dd><dt>預設綁定</dt><dd class="mono">127.0.0.1</dd><dt>資料目錄</dt><dd class="mono">/var/lib/docker</dd></dl></div>
        <div><h3>使用者與群組</h3><dl class="dl"><dt>docker 群組</dt><dd>建立，不加入任何使用者</dd></dl></div>
        <div><h3>預估</h3><dl class="dl"><dt>下載</dt><dd class="mono">96 MiB</dd><dt>時間</dt><dd class="mono">約 2 分</dd><dt>需要重開機</dt><dd>否</dd></dl></div>
      </div>
    </section>"""
    run = f"""
    <section class="panel" aria-labelledby="h-erun">
      <div class="ph"><h2 id="h-erun">安裝 · 工作 #531</h2>{U.status("info", "執行中")}<a class="more" href="WorkCenter.dc.html">工作中心 →</a></div>
      <div style="padding:12px 14px">{U.pipeline([("預檢", "done", "4 秒", "7 項"), ("下載與驗證", "done", "21 秒", "96 MiB · 簽章有效"), ("安裝套件", "cur", "進行中", "dpkg 3 / 5"), ("啟動服務", "todo", "—", "docker、containerd"), ("驗證", "todo", "—", "Engine、Compose、Buildx、Socket"), ("測試容器", "todo", "—", "hello-world 後清除")])}</div>
    </section>"""
    main = head + scope + choose + checks + plan + run
    return U.shell("docker", main)


def kubernetes_empty() -> str:
    head = U.page_head("Kubernetes", OBS, "")
    choose = f"""
    <section class="panel" aria-labelledby="h-k0">
      <div class="ph"><h2 id="h-k0">還沒有叢集</h2></div>
      <div class="fleet choices">
        {_choice("連接既有叢集", "kubeconfig 或服務帳號 Token", ["驗證 CA、叢集身分與實際權限", "預設唯讀，之後再提升", "kubeconfig 存進機密庫，不給瀏覽器"], '<button type="button" class="btn sm">上傳 kubeconfig</button>')}
        {_choice("建立 K3s 叢集", "K3s v1.31.2+k3s1", ["選 1 或 3 台控制節點、任意工作節點", "預檢 OS、時間、主機名、Port", "內建 Traefik／ServiceLB 預設停用"], '<button type="button" class="btn sm pri" aria-pressed="true">已選擇</button>', on=True)}
      </div>
    </section>"""
    nodes = f"""
    <section class="panel" aria-labelledby="h-k1" data-batch>
      <div class="ph"><h2 id="h-k1">選擇節點 · 2 / 4</h2></div>
      <div class="row hd krow"><span>節點</span><span>角色</span><span class="c3">現有引擎</span><span>預檢</span></div>
      <div class="row krow sel"><span class="name"><span class="n mono">lab-pi-02</span><span class="d">Debian 12 · 4 核 · 8 GiB</span></span><span>{U.status("info", "控制節點")}</span><span class="c3 note">無</span><span>{U.status("ok", "通過 8 / 8")}</span></div>
      <div class="row krow sel"><span class="name"><span class="n mono">lab-mini</span><span class="d">Debian 13 · 4 核 · 8 GiB</span></span><span>{U.status("info", "工作節點")}</span><span class="c3 mono">Podman rootless</span><span>{U.status("warn", "通過 7 / 8")}</span></div>
      <div class="row krow"><span class="name"><span class="n mono">hazel-tw-01</span><span class="d">Ubuntu 24.04 · 8 核 · 31 GiB</span></span><span class="note">—</span><span class="c3 mono">Docker 27.3.1</span><span>{U.status("ok", "可加入")}</span></div>
      <div class="row krow"><span class="name"><span class="n mono">hazel-jp-02</span><span class="d">Debian 12 · 4 核 · 16 GiB</span></span><span class="note">—</span><span class="c3 mono">Docker 27.1.0</span><span>{U.status("warn", "狀態過期")}</span></div>
    </section>"""
    checks = f"""
    <section class="panel" aria-labelledby="h-k2">
      <div class="ph"><h2 id="h-k2">預檢 · K3s v1.31.2 · 2 個節點</h2><span class="note">1 警告 · 7 通過</span></div>
      {_chk("ok", "作業系統、架構、核心", "lab-pi-02 Debian 12 amd64 6.1 · lab-mini Debian 13 amd64 6.12", "通過")}
      {_chk("ok", "時間同步", "兩台 NTP 同步 · 偏差 +3 ms", "通過")}
      {_chk("ok", "主機名稱唯一", "lab-pi-02、lab-mini", "通過")}
      {_chk("ok", "Port 可用", "6443、10250、8472/udp、2379–2380（lab-pi-02）", "通過")}
      {_chk("ok", "Pod 與 Service 網段不衝突", "10.42.0.0/16、10.43.0.0/16 · 與 docker0、內網 10.8.0.0/24 不重疊", "通過")}
      {_chk("ok", "80／443 不被搶", "Traefik、ServiceLB 停用；入口由 Zentinel 轉送", "通過")}
      {_chk("ok", "防火牆", "ufw 規則新增 6443、10250、8472/udp 僅限節點間；不關閉防火牆", "通過")}
      {_chk("warn", "lab-mini 已有 Podman rootless", "K3s containerd 與 Podman 分開；同名映像不共用", "警告")}
      <div class="wrap" style="justify-content:flex-end;padding:10px 14px"><button type="button" class="btn">上一步</button><button type="button" class="btn pri">批准並建立叢集</button></div>
    </section>"""
    main = head + choose + nodes + checks
    return U.shell("k8s", main)



# ---------- 表單共用 ----------
def _fld(label, control, help_=""):
    h = f'<span class="help">{help_}</span>' if help_ else ""
    return f'<div class="field"><label>{label}</label>{control}{h}</div>'


def _in(val, mono=False, ph=False, tail=""):
    cls = "mono" if mono else ""
    style = "flex:1;min-width:0;overflow-wrap:anywhere" + (";color:var(--faint)" if ph else "")
    return f'<div class="inp"><span class="{cls}" style="{style}">{val}</span>{tail}</div>'


def _dd(val):
    return f'<div class="inp"><span style="flex:1;min-width:0;overflow-wrap:anywhere">{val}</span>{U.icon("down")}</div>'


def _seg(opts, on=0, label="選項"):
    btns = "".join(f'<button type="button" class="btn{" on" if i == on else ""}" aria-pressed="{"true" if i == on else "false"}">{o}</button>' for i, o in enumerate(opts))
    return f'<div class="range" role="group" aria-label="{label}" style="flex-wrap:wrap">{btns}</div>'


def _cb(label, on=True, note=""):
    n = f' <span class="note">{note}</span>' if note else ""
    return f'<label style="display:flex;gap:8px;align-items:center;min-height:32px"><input type="checkbox"{" checked" if on else ""}>{label}{n}</label>'


def _grid(*fields, minw=240):
    return f'<div style="padding:14px 16px;display:grid;grid-template-columns:repeat(auto-fit,minmax(min({minw}px,100%),1fr));gap:14px">{"".join(fields)}</div>'


def _fp(title, body, sub="", hid="", extra=""):
    s = f'<span class="sub">{sub}</span>' if sub else ""
    i = f' id="{hid}" aria-labelledby="h-{hid}"' if hid else ""
    h = f' id="h-{hid}"' if hid else ""
    return f'<section class="panel"{i}><div class="ph"><h2{h}>{title}</h2>{s}{extra}</div>{body}</section>'


def _erows(cols, rows, add=""):
    """小型可編輯清單：每列尾端有移除鈕，底部一顆新增。"""
    rm = f'<button type="button" class="ib" aria-label="移除這列">{U.icon("trash")}</button>'
    hd = '<div class="row hd">' + "".join(f"<span>{c}</span>" for c in cols) + "<span></span></div>"
    body = "".join('<div class="row">' + "".join(f"<span>{c}</span>" for c in r) + f"<span>{rm}</span></div>" for r in rows)
    foot = f'<div style="padding:8px 14px 12px"><button type="button" class="btn sm">＋ {add}</button></div>' if add else ""
    return hd + body + foot


def _form_head(crumbs, title, actions):
    return f"""
    <div class="phead"><div class="name">{U.crumbs(crumbs)}<h1>{title}</h1></div>
      <div class="wrap" style="margin-left:auto">{actions}</div></div>"""


def _submit(primary, secondary=""):
    sec = f'<button type="button" class="btn">{secondary}</button>' if secondary else ""
    return f'<div class="wrap" style="padding:0 16px 14px"><button type="button" class="btn pri">{primary}</button>{sec}<button type="button" class="btn">取消</button></div>'


# ---------- 建立容器 ----------
def container_create() -> str:
    head = _form_head([("容器", "Containers.dc.html")], "建立容器", '<a class="btn" href="Containers.dc.html">取消</a><button type="button" class="btn pri">建立並啟動</button>')
    kind = f'<div class="panel" style="padding:10px 14px">{_seg(["單一容器", "Compose 專案（到專案與服務）"], 0, "建立方式")}</div>'
    image = _fp("映像與位置", _grid(
        _fld("映像", _in("ghcr.io/<wbr>hazelnut/<wbr>report-worker:1.4.0", mono=True, tail=U.status("ok", "tw-01 已有")), "支援 tag 與 digest"),
        _fld("容器名稱", _in("report-worker", mono=True, tail=U.status("ok", "未重複"))),
        _fld("環境", _dd("Docker（root）@ hazel-tw-01"), "引擎與節點一起選，不自動猜"),
        _fld("拉取政策", _seg(["缺少時拉取", "永遠拉取", "不拉取"], 0, "拉取政策"))), hid="img")
    ports = _fp("Port", _erows(["容器 Port", "主機綁定", "對外"], [
        ['<span class="mono">8080/tcp</span>', _in("127.0.0.1:18090", mono=True, tail=U.status("ok", "未占用")), _seg(["經 Zentinel", "只本機", "對外開放"], 0, "對外方式")],
        ['<span class="mono">9464/tcp</span>', _in("不發布", ph=True), _seg(["經 Zentinel", "只本機", "對外開放"], 1, "對外方式")]], add="新增 Port"), sub="選「對外開放」會同時建立防火牆規則")
    vols = _fp("儲存", _erows(["容器路徑", "來源", "模式"], [
        ['<span class="mono">/data</span>', _dd("儲存卷 report_data（新建）"), _seg(["rw", "ro"], 0, "讀寫")],
        ['<span class="mono">/config</span>', _in("/mnt/data/apps/report/config", mono=True, tail=U.status("ok", "存在")), _seg(["rw", "ro"], 1, "讀寫")]], add="新增掛載"),
        sub="刪除容器時預設保留這些資料")
    envs = _fp("環境變數", _erows(["名稱", "值", "來源"], [
        ['<span class="mono">REPORT_TZ</span>', _in("Asia/Taipei", mono=True), _dd("直接填")],
        ['<span class="mono">DB_URL</span>', _in("postgres://report@shop-db:5432/report", mono=True), _dd("直接填")],
        ['<span class="mono">SMTP_PASSWORD</span>', U.attr("lock", "存在機密庫，容器啟動時注入", "機密 report/smtp"), _dd("機密庫")]], add="新增變數"),
        extra=f'<a class="btn sm" style="margin-left:auto" href="EnvUpload.dc.html">{U.icon("upload", 14)}上傳 env 檔</a>')
    run = _fp("資源與執行", _grid(
        _fld("CPU 上限", _in("1 核")), _fld("記憶體上限", _in("1 GiB"), "tw-01 可用 20 GiB"),
        _fld("重啟政策", _dd("unless-stopped")), _fld("日誌", _dd("輪替 · 5 個檔 × 10 MiB（預設）"), "可以改上限或關掉"), _fld("執行身分", _in("1000:1000", mono=True), "映像預設 root，這裡改成一般使用者"),
        _fld("健康檢查", _in("curl -f http://localhost:8080/healthz", mono=True), "每 10 秒 · 逾時 3 秒 · 失敗 3 次算不健康"),
        _fld("網路", _dd("shop_default（與 shop-db 同網路）")), _fld("GPU", _dd("不使用")), _fld("標籤", _in("team=report", mono=True))))
    preview = _fp("建立前檢查", f"""
      <div style="padding:12px 16px;display:flex;flex-direction:column;gap:10px">
        {U.pipeline([("映像", "done", "本機已有", "簽章有效"), ("名稱", "done", "未重複", ""), ("Port", "done", "18090 空", "只綁本機"), ("掛載", "done", "路徑存在", "/config 唯讀"), ("資源", "done", "1 核 · 1 GiB", "可用 20 GiB")])}
        <div class="term">docker run -d --name report-worker --restart unless-stopped \\
  -p 127.0.0.1:18090:8080 -v report_data:/data -v /mnt/data/apps/report/config:/config:ro \\
  --cpus 1 --memory 1g --user 1000:1000 --network shop_default \\
  ghcr.io/hazelnut/report-worker:1.4.0</div>
      </div>""" + _submit("建立並啟動", "只建立不啟動"), sub="對應的指令只供核對，平台用引擎 API 建立")
    main = head + kind + image + f'<div class="cg">{ports}{vols}</div>' + f'<div class="cg">{envs}{run}</div>' + preview
    return U.shell("docker", main)


# ---------- 新增站點 ----------
def site_create() -> str:
    head = _form_head([("網站與入口", "Websites.dc.html")], "新增站點", '<a class="btn" href="Websites.dc.html">取消</a><button type="button" class="btn pri">建立站點</button>')
    kinds = ('<section class="panel"><div class="ph"><h2>類型</h2></div><div class="fleet choices">'
             + _choice("反向代理", "最常用", ["轉到專案服務、容器或主機 Port"], U.status("ok", "已選"), on=True)
             + _choice("靜態網站", "", ["由 Zentinel 直接提供檔案"], '<a class="btn sm" href="SiteStatic.dc.html">選擇</a>')
             + _choice("執行環境", "PHP · Node", ["用平台管理的執行環境容器"], '<button type="button" class="btn sm">選擇</button>')
             + _choice("重新導向", "", ["整個網域轉到另一個網址"], '<button type="button" class="btn sm">選擇</button>') + "</div></section>")
    domains = _fp("網域", _erows(["網域", "DNS", "衝突"], [
        ['<span class="mono">report.example.tw</span>', U.status("ok", "指向 203.0.113.22"), U.status("ok", "無")],
        ['<span class="mono">www.report.example.tw</span>', U.status("warn", "未指向此入口"), U.status("ok", "無")]], add="新增網域"), sub="DNS 檢查從外部解析")
    target = _fp("轉到哪裡", _grid(
        _fld("目標類型", _seg(["專案服務", "容器", "主機 Port", "外部 URL"], 1, "目標類型")),
        _fld("容器與 Port", _dd("report-worker · 8080（tw-01）"), "用容器名稱與服務識別，容器重建後仍找得到"),
        _fld("目標健康", _in("GET / → 200 · 41 ms", mono=True, tail=U.status("ok", "可連"))),
        _fld("入口", _dd("Zentinel @ hazel-tw-01"), "這個入口在 tw-01，目標也在 tw-01")))
    https = _fp("HTTPS", _grid(
        _fld("憑證", _seg(["自動申請", "匯入", "使用既有", "不啟用"], 0, "憑證")),
        _fld("驗證方式", _seg(["HTTP-01", "DNS-01（Cloudflare）"], 0, "驗證方式"), "80 Port 從外部實測可連 · Tunnel 或萬用網域要用 DNS-01"),
        _fld("ACME 帳號", _dd("tim@example.tw · Let's Encrypt")), _fld("強制 HTTPS", _seg(["開", "關"], 0, "強制 HTTPS"))))
    guard = _fp("防護與限制", _grid(
        _fld("WAF", _seg(["關閉", "只偵測", "阻擋"], 1, "WAF")), _fld("限速", _in("60 / 分 / IP")),
        _fld("存取", _dd("所有人")), _fld("上傳上限", _in("10 MiB")),
        _fld("其他", '<div class="wrap">' + _cb("壓縮") + _cb("快取靜態檔") + _cb("重新導向規則", False) + _cb("自訂標頭", False) + "</div>")))
    preview = _fp("變更預覽", f"""
      <div style="padding:12px 16px;display:flex;flex-direction:column;gap:10px">
        {U.pipeline([("網域", "done", "無衝突", ""), ("DNS", "wait", "1 個未指向", "www 先不簽"), ("目標", "done", "200", "41 ms"), ("設定驗證", "done", "zentinel check", "通過"), ("憑證", "todo", "建立後申請", "HTTP-01")])}
        <div class="term"><span style="color:var(--ok)">+ route "report.example.tw" {{</span>
<span style="color:var(--ok)">+   upstream  report-worker:8080   # 容器 report-worker</span>
<span style="color:var(--ok)">+   waf       detect</span>
<span style="color:var(--ok)">+   ratelimit 60/m per client_ip</span>
<span style="color:var(--ok)">+ }}</span></div>
      </div>""" + _submit("建立站點", "建立但先不啟用"), sub="Zentinel 設定差異；套用是一個工作，可回復")
    main = head + kinds + f'<div class="cg">{domains}{target}</div>' + f'<div class="cg">{https}{guard}</div>' + preview
    return U.shell("web", main)


# ---------- 防火牆規則 ----------
def firewall_rule() -> str:
    head = _form_head([("安全防護", "Security.dc.html"), ("對外曝露", "Security.dc.html#exposure")], "新增防火牆規則", '<a class="btn" href="Security.dc.html">取消</a><button type="button" class="btn pri">排入</button>')
    where = _fp("套用到", f"""
      <div class="row hd"><span>節點</span><span>防火牆</span><span>目前規則</span></div>
      <label class="row"><span class="wrap"><input type="checkbox"><span class="mono">hazel-tw-01</span></span><span>ufw</span><span class="mono">14 條</span></label>
      <label class="row"><span class="wrap"><input type="checkbox" checked><span class="mono">hazel-jp-02</span></span><span>nftables</span><span class="mono">9 條</span></label>
      <div class="row"><span class="wrap"><span tabindex="0" data-tip="先選定 lab-mini 由哪個防火牆寫入"><input type="checkbox" disabled aria-label="lab-mini"></span><span class="mono">lab-mini</span></span><span>{U.status("warn", "ufw ＋ firewalld")}</span><span class="mono">6 ＋ 11 條</span></div>""")
    def layer(name, backend, n, role):
        return f'<div class="row"><span class="mono">{name}</span><span class="mono">{backend}</span><span class="mono">{n}</span><span>{role}</span></div>'
    multi = _fp("lab-mini · 2 個防火牆同時啟用", f"""
      <div class="row hd"><span>誰在管</span><span>寫在哪</span><span>規則</span><span>平台</span></div>
      {layer("ufw", "iptables-nft · filter", "6", U.status("none", "未選定"))}
      {layer("firewalld", "nftables · inet firewalld", "11", U.status("none", "未選定"))}
      {layer("Podman（netavark）", "nftables · netavark", "引擎自己的", U.status("none", "不動"))}
      <div style="padding:12px 16px;display:flex;flex-direction:column;gap:10px">
        <div class="wrap">{U.status("warn", "封包要每一層都放行才進得來")}<span tabindex="0" data-tip="ufw 開了 5432、firewalld 沒開，外部還是連不到；任何一層擋掉就擋掉">{U.icon("info", 14)}</span></div>
        <div class="wrap"><span class="note">平台寫入（還沒選）</span>{_seg(["ufw", "firewalld"], -1, "主要防火牆")}<button type="button" class="btn sm">停用 ufw（變更計畫）</button></div>
      </div>""")
    rule = _fp("規則", _grid(
        _fld("動作", _seg(["只允許這些來源，其他擋掉", "擋掉這些來源", "限速"], 0, "動作")), _fld("方向", _seg(["進站", "出站"], 0, "方向")),
        _fld("協定", _seg(["TCP", "UDP", "兩者"], 0, "協定")), _fld("Port", _in("5432", mono=True)),
        _fld("來源", _in("10.8.0.0/24", mono=True), "只允許 WireGuard 內網"), _fld("備註", _in("wiki-db 只給內網")),
        _fld("保護對象", _seg(["主機上的服務", "容器發布的 Port"], 1, "保護對象"), "容器 Port 寫進 DOCKER-USER 或 Podman 的網路；不動引擎自己的鏈")))
    impact = _fp("套用前檢查", f"""
      <div style="padding:12px 16px;display:flex;flex-direction:column;gap:8px">
        {_chk("warn", "hazel-jp-02 沒有回應", "規則先排入，主機恢復後才寫入；寫入後一樣要確認", "排入")}
        {_chk("ok", "管理路徑", "SSH 22、管理入口 8443、Agent 通道都不在這條規則裡", "不受影響")}
        {_chk("warn", "會中斷的連線", "jp-02 上有 2 條來自 198.51.100.7 的 5432 連線", "套用後中斷")}
        {_chk("ok", "規則衝突", "與現有 9 條沒有重疊或互相抵銷", "無")}
        <div class="term">iptables -I DOCKER-USER -p tcp -m conntrack --ctorigdstport 5432 ! -s 10.8.0.0/24 -j DROP   # hazel-jp-02</div>
      </div>""")
    confirm = _fp("套用後", f"""
      <div style="padding:12px 16px;display:flex;flex-direction:column;gap:10px">
        {U.pipeline([("排入", "done", "03:44", "等 jp-02"), ("寫入規則", "wait", "主機恢復後", "DOCKER-USER"), ("確認", "todo", "60 秒內", "從新連線確認"), ("沒確認就回復", "todo", "", "回到套用前")])}
      </div>""")
    main = head + f'<div class="cg">{where}{rule}</div>' + multi + impact + confirm
    return U.shell("security", main)


# ---------- 新增計畫任務 ----------
def cron_create() -> str:
    head = _form_head([("計畫任務", "Cron.dc.html")], "新增計畫任務", '<a class="btn" href="Cron.dc.html">取消</a><button type="button" class="btn pri">建立</button>')
    kinds = ('<section class="panel"><div class="ph"><h2>類型</h2></div><div class="fleet choices">'
             + _choice("腳本", "shell", ["在節點上以指定身分執行"], U.status("ok", "已選"), on=True)
             + _choice("備份", "", ["改到儲存與備份建立"], '<a class="btn sm" href="BackupCreate.dc.html">前往</a>')
             + _choice("呼叫 URL", "HTTP", ["GET / POST，記錄回應碼"], '<button type="button" class="btn sm">選擇</button>')
             + _choice("容器內指令", "exec", ["在指定容器裡執行"], '<button type="button" class="btn sm">選擇</button>')
             + _choice("清理", "", ["日誌、映像、暫存檔"], '<button type="button" class="btn sm">選擇</button>')
             + _choice("升級套件", "apt · dnf", ["只套安全更新或全部", "在維護時段跑；要重開機時另外排", "主機已開自動安全更新時會提醒重複"], '<button type="button" class="btn sm">選擇</button>') + "</div></section>")
    run = _fp("執行", _grid(
        _fld("名稱", _in("report-nightly")), _fld("節點", _dd("hazel-tw-01")),
        _fld("執行身分", _dd("deploy（非 root）")), _fld("工作目錄", _in("/mnt/data/apps/report", mono=True)),
        _fld("機密", _dd("report/smtp → $SMTP_PASSWORD"), "以環境變數注入，不寫進腳本")) +
        '<div style="padding:0 16px 14px"><div class="term" style="min-height:96px">#!/usr/bin/env bash\nset -euo pipefail\n./bin/report --since yesterday --mail ops@example.tw</div></div>')
    nxt = "".join(f'<li class="mono">{t}</li>' for t in ["11/08（六）03:30", "11/09（日）03:30", "11/10（一）03:30", "11/11（二）03:30", "11/12（三）03:30"])
    sched = _fp("排程", _grid(
        _fld("頻率", _seg(["每天", "每週", "每月", "自訂 Cron"], 0, "頻率")), _fld("時間", _in("03:30", mono=True)),
        _fld("等同 Cron", '<span class="mono">30 3 * * *</span>', "選「自訂 Cron」才需要自己寫"), _fld("時區", _dd("Asia/Taipei（UTC+8）"), "這個時區沒有夏令時間"),
        _fld("同時段", U.status("warn", "03:30 已有 shop-db 備份；03:00–04:00 是維護時段")),
        _fld("接下來 5 次", f'<ol style="margin:0;padding-left:18px;display:flex;flex-direction:column;gap:2px">{nxt}</ol>')))
    behave = _fp("行為", _grid(
        _fld("上一次還沒跑完時", _seg(["跳過這次", "排隊", "允許同時跑"], 0, "重疊政策"), "預設不同時跑"),
        _fld("錯過排程時（例如關機）", _seg(["跳過", "開機後補跑一次"], 1, "錯過排程")),
        _fld("逾時", _in("30 分")), _fld("失敗重試", _seg(["不重試", "重試 2 次"], 0, "失敗重試"), "腳本沒標成可重試時不自動重試"),
        _fld("資源上限", _in("CPU 1 核 · 記憶體 512 MiB")), _fld("通知", _dd("失敗時 → Telegram ops"))))
    main = head + kinds + run + f'<div class="cg">{sched}{behave}</div>' + _fp("建立", '<div style="height:4px"></div>' + _submit("建立", "建立並立即試跑一次"))
    return U.shell("cron", main)


# ---------- 新增備份任務 ----------
def backup_create() -> str:
    head = _form_head([("儲存與備份", "Storage.dc.html")], "新增備份任務", '<a class="btn" href="Storage.dc.html">取消</a><button type="button" class="btn pri">建立</button>')
    src = ('<section class="panel"><div class="ph"><h2>備份什麼</h2></div><div class="fleet choices">'
           + _choice("應用", "", ["應用檔案＋它的資料庫"], '<button type="button" class="btn sm">選擇</button>')
           + _choice("資料庫", "shop-db", ["PostgreSQL 16.4 · 1.29 GiB"], U.status("ok", "已選"), on=True)
           + _choice("目錄", "", ["主機上的路徑，可設排除"], '<button type="button" class="btn sm">選擇</button>')
           + _choice("平台設定", "", ["HazelminX 自己的設定與金鑰包"], '<button type="button" class="btn sm">選擇</button>') + "</div></section>")
    dup = f'<div class="panel" style="padding:10px 14px">{U.status("warn", "shop-db 已經有每日備份（03:30 → S3 tokyo-backups）")} <a class="btn sm" href="Storage.dc.html">改用那個任務</a></div>'
    what = _fp("包含與排除", _erows(["項目", "大小", "狀態"], [
        ['<span class="mono">shop（資料庫）</span>', '<span class="mono">1.21 GiB</span>', U.status("ok", "包含")],
        ['<span class="mono">shop_audit（資料庫）</span>', '<span class="mono">84 MiB</span>', U.status("ok", "包含")],
        ['<span class="mono">pg_stat_statements（擴充資料）</span>', '<span class="mono">—</span>', U.status("info", "排除")]], add="新增排除規則"),
        sub="預覽：共 1.29 GiB")
    consist = _fp("一致性", _grid(
        _fld("方法", _seg(["pg_dump（一致）", "停寫後複製", "檔案快照"], 0, "一致性方法"), "執行中的資料庫不用直接複製儲存卷"),
        _fld("結果標示", U.status("ok", "整套一致"))))
    dest = _fp("目的地", _erows(["目的地", "必要性", "連線"], [
        ['<span class="mono">S3 tokyo-backups · shop-db/</span>', _seg(["必需", "選配"], 0, "必要性"), U.status("ok", "測試通過 · 182 ms")],
        ['<span class="mono">NFS /mnt/nfs/backup</span>', _seg(["必需", "選配"], 1, "必要性"), U.status("ok", "可寫 · 剩 181 GiB")]], add="新增目的地"),
        sub="每個目的地各自回報結果")
    account = (_grid(
        _fld("名稱", _in("R2 osaka")), _fld("Endpoint", _in("https://&lt;account&gt;.r2.cloudflarestorage.com", mono=True)),
        _fld("Bucket", _in("hazel-backup", mono=True)), _fld("區域", _in("auto", mono=True)),
        _fld("Access Key", _in("存入機密庫後只顯示末 4 碼", ph=True)), _fld("Secret Key", _in("••••••••••••", tail=U.icon("eye"))),
        _fld("連線測試", '<div class="wrap"><button type="button" class="btn sm">測試寫入與刪除</button>' + U.status("warn", "尚未測試") + "</div>")))
    sched = _fp("排程與保留", _grid(
        _fld("頻率", _seg(["每天", "每週", "自訂"], 0, "頻率")), _fld("時間", _in("03:00 Asia/Taipei", mono=True)),
        _fld("保留份數", _in("14")), _fld("保留天數", _in("30 天")), _fld("長期保留", _in("每月 1 份，保留 12 個月")),
        _fld("最後一份保護", U.status("ok", "新備份失敗時不刪最後一份可還原副本"))))
    secure = _fp("加密與驗證", _grid(
        _fld("加密", _seg(["AES-256（金鑰在機密庫）", "不加密"], 0, "加密")), _fld("壓縮", _dd("zstd 等級 3")),
        _fld("增量", _seg(["關", "開"], 0, "增量")),
        _fld("驗證", '<div style="display:flex;flex-direction:column">' + _cb("傳輸後 sha256 校驗") + _cb("每週還原測試到暫存資料庫") + "</div>")))
    account = f'<details class="panel"><summary class="ph" style="cursor:pointer"><h2>新增目的地 · S3 相容儲存</h2></summary>{account}</details>'
    main = head + src + dup + f'<div class="cg">{what}{consist}</div>' + dest + account + f'<div class="cg">{sched}{secure}</div>' + _fp("建立", '<div style="height:4px"></div>' + _submit("建立", "建立並立即備份一次"))
    return U.shell("storage", main)


# ---------- 新增主機 ----------
def add_node() -> str:
    head = _form_head([("主機", "Nodes.dc.html")], "新增主機", '<a class="btn" href="Nodes.dc.html">取消</a>')
    steps = f'<div class="panel" style="padding:12px 14px">{U.steps([("設定", "done", "lab-pi-02"), ("在主機上執行", "done", "註冊碼已產生"), ("核對主機", "cur", "等你確認"), ("完成", "todo", "")])}</div>'
    setup = _fp("1 · 設定", _grid(
        _fld("節點名稱", _in("lab-pi-02", mono=True)), _fld("標籤", _in("lab, arm64", mono=True)),
        _fld("角色", _seg(["一般", "資料主機", "只觀測"], 0, "角色"), "一般：可以部署；資料主機：放資料庫與備份；只觀測：只收集狀態，不接受任何變更"), _fld("維護時段", _in("03:00–04:00 Asia/Taipei"))))
    code = _fp("2 · 在主機上執行", f"""
      <div style="padding:12px 16px;display:flex;flex-direction:column;gap:10px">
        <div class="wrap">{U.status("info", "註冊碼 9 分 41 秒後失效 · 只能用一次")}<button type="button" class="btn sm">重新產生</button></div>
        <div class="term">curl -fsSL https://panel.example.tw/install-agent.sh | sudo sh -s -- \\
  --core https://panel.example.tw --code HZX-7Q4M-2KD9-PX81</div>
        <div class="wrap"><button type="button" class="btn sm">{U.icon("copy", 14)}複製指令</button><button type="button" class="btn sm">看安裝腳本內容</button><button type="button" class="btn sm">下載離線安裝包</button></div>
        <span class="note">用 SSH 登入那台主機後貼上執行。Agent 會主動連回控制中心，不用開任何入站 Port；沒有 Docker 也可以，之後在容器頁安裝。</span>
      </div>""")
    verify = _fp("3 · 核對主機", f"""
      <div style="padding:12px 16px;display:flex;flex-direction:column;gap:10px">
        <div class="wrap">{U.status("ok", "已連線 · 03:44:12 · 來源 203.0.113.61")}</div>
        <div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(min(180px,100%),1fr));gap:10px 16px;font-size:13px">
          {kv("主機名稱", '<span class="mono">lab-pi-02</span>')}{kv("作業系統", "Debian 13 · arm64")}{kv("CPU／記憶體", "4 核 · 8 GiB")}
          {kv("machine-id", '<span class="mono">5c1e…a09f</span>')}{kv("Agent 憑證指紋", '<span class="mono">SHA256:3f0b…91de</span>')}{kv("偵測到的引擎", "Podman 5.2（rootless · pi）")}
        </div>
        <div class="wrap"><button type="button" class="btn pri">是這台，加入</button><button type="button" class="btn">不是，拒絕並作廢註冊碼</button></div>
      </div>""", sub="同一台重新註冊不會變成第二個節點")
    main = head + steps + f'<div class="cg">{setup}{code}</div>' + verify
    return U.shell("nodes", main)


# ---------- ⌘K ----------
def command_palette() -> str:
    def item(ic, title, sub, kbd="", on=False):
        k = f'<span class="kbd">{kbd}</span>' if kbd else ""
        return (f'<a class="cmdi{" on" if on else ""}" href="#"><span class="ic" aria-hidden="true">{U.icon(ic)}</span>'
                f'<span style="display:flex;flex-direction:column;min-width:0"><span>{title}</span><span class="note">{sub}</span></span>{k}</a>')
    def grp(name, items):
        return f'<div class="cmdg" role="group" aria-label="{name}"><div class="note" style="padding:6px 12px">{name}</div>{"".join(items)}</div>'
    pal = f"""
    <div class="cmdp" role="dialog" aria-label="搜尋或下指令">
      <div class="cmdq">{U.icon("search")}<input value="shop" aria-label="搜尋" style="flex:1;background:transparent;border:0;font-size:15px;min-width:0"><span class="kbd">esc</span></div>
      <div class="wrap" style="padding:8px 12px;border-bottom:1px solid var(--line)"><span class="chip on">全部</span><span class="chip">動作</span><span class="chip">專案</span><span class="chip">容器</span><span class="chip">網站</span><span class="chip">節點</span><span class="chip">檔案</span></div>
      <div class="cmdl">
        {grp("動作", [item("deploy", "部署 shop · production", "最新 commit 9b3e7f1 · 需要批准", "↵", on=True), item("restart", "重啟 shop-api（2 個副本）", "tw-01 · Docker"), item("log", "看 shop-api 的日誌", "tw-01")])}
        {grp("專案與服務", [item("projects", "shop · production", "tw-01 · 待批准 #418"), item("projects", "shop · staging", "lab-mini · 部署失敗")])}
        {grp("容器", [item("docker", "shop-api-1", "執行中 · tw-01"), item("docker", "shop-db", "postgres 16.4 · tw-01"), item("docker", "shop-redis", "重啟中 · tw-01")])}
        {grp("網站", [item("web", "shop.example.tw", "憑證 11 天後到期"), item("web", "staging.shop.example.tw", "上游 502")])}
        {grp("檔案", [item("files", "/mnt/data/apps/shop", "tw-01 · 290 GiB")])}
      </div>
      <div class="note" style="display:flex;gap:14px;padding:8px 12px;border-top:1px solid var(--line);flex-wrap:wrap"><span><span class="kbd">↑</span><span class="kbd">↓</span> 移動</span><span><span class="kbd">↵</span> 執行</span><span><span class="kbd">⌘</span><span class="kbd">↵</span> 新分頁開啟</span><span>結果只列你有權限的資源</span></div>
    </div>"""
    # 蓋在總覽頁上：全畫面遮罩，含頂列與側欄
    page = overview()
    i = page.index('<main class="main">') + len('<main class="main">')
    return page[:i] + f'<div class="scrim">{pal}</div>' + page[i:]


# ---------- 批次操作結果 ----------
def batch_result() -> str:
    head = f"""
    <div class="phead"><div class="name">{U.crumbs([("工作中心", "WorkCenter.dc.html")])}<h1>批次重啟 5 個容器 · 工作 #540</h1></div>
      <div class="wrap" style="margin-left:auto">{U.status("warn", "部分完成")}<button type="button" class="btn pri">重試失敗的 1 項</button></div></div>"""
    summary = _fp("結果", f"""
      <div style="padding:12px 16px;display:flex;flex-direction:column;gap:8px">
        {U.tall_bar([(3, "成功 3", "var(--ok)"), (1, "失敗 1", "var(--danger)"), (1, "略過 1", "var(--q3)")], 5, unit="")}
        <div class="wrap note"><span>開始 03:40:02 · tim</span><span>總時長 1 分 12 秒</span><span>一次最多 2 個同時進行</span></div>
      </div>""")
    def r(name, node, st, dur, why, acts):
        return (f'<div class="row"><span class="name"><span class="n mono">{name}</span><span class="d">{node}</span></span><span>{st}</span>'
                f'<span class="mono">{dur}</span><span class="c3">{why}</span><span class="wrap" style="justify-content:flex-end;flex-wrap:nowrap">{acts}</span></div>')
    log = '<button type="button" class="btn sm">日誌</button>'
    table = f"""
    <section class="panel" aria-labelledby="h-br">
      <div class="ph"><h2 id="h-br">每一項</h2><div class="wrap" style="margin-left:auto"><span class="chip on">全部 5</span><span class="chip">失敗 1</span><span class="chip">略過 1</span></div></div>
      <div class="row hd"><span>容器</span><span>結果</span><span>時長</span><span class="c3">原因</span><span></span></div>
      {r("shop-redis", "tw-01 · Docker", U.status("dng", "失敗"), "41 秒", '<span class="mono">health check timeout after 30s</span>', '<button type="button" class="btn sm">重試這項</button>' + log)}
      {r("wiki-app", "jp-02 · Docker", U.status("info", "略過"), "—", "節點狀態過期，不送出寫入", '<button type="button" class="btn sm">節點恢復後重試</button>')}
      {r("shop-api-1", "tw-01 · Docker", U.status("ok", "成功"), "8 秒", "健康 2／2", log)}
      {r("shop-api-2", "tw-01 · Docker", U.status("ok", "成功"), "9 秒", "健康 2／2", log)}
      {r("shop-worker", "tw-01 · Docker", U.status("ok", "成功"), "6 秒", "健康", log)}
    </section>"""
    main = head + summary + table
    return U.shell("jobs", main)


# ---------- Compose 編輯器 ----------
def compose_editor() -> str:
    head = f"""
    <div class="phead"><div class="name">{U.crumbs([("專案與服務", "Projects.dc.html"), ("shop", "ProjectDetail.dc.html"), ("production", "ProjectDetail.dc.html")])}<h1 class="mono">compose.yaml</h1></div>
      <span class="note">草稿 · 未儲存 3 處 · 線上版本 a3f9c1e</span>
      <div class="wrap" style="margin-left:auto">{_seg(["編輯", "與線上比較"], 0, "檢視")}<button type="button" class="btn">儲存草稿</button><a class="btn pri" href="DeployCheck.dc.html">預覽變更計畫</a></div></div>"""
    lines = ["services:", "  shop-api:", "    image: ghcr.io/hazelnut/shop-api:${TAG}", "    deploy:", "      replicas: 2", "    ports:", '      - "127.0.0.1:18080:8080"',
             "    env_file: .env", "    depends_on: [shop-db, shop-redis]", "  shop-worker:", "    image: ghcr.io/hazelnut/shop-worker:${TAG}", "    volumes:",
             "      - /:/host:ro", "  shop-redis:", "    image: redis:latest", "  shop-db:", "    image: postgres:16.4", "    volumes:", "      - shop-db-data:/var/lib/postgresql/data"]
    marks = {12: "dng", 14: "warn"}
    ed = "".join(f'<div class="ln{" " + marks[i] if i in marks else ""}"><span class="no">{i + 1}</span><span class="code">{l}</span></div>' for i, l in enumerate(lines))
    editor = f'<section class="panel" aria-label="編輯器"><div class="editor mono">{ed}</div></section>'
    issues = _fp("檢查 · 1 高風險、1 警告", f"""
      <div style="padding:12px 14px;display:flex;flex-direction:column;gap:8px">
        {_chk("dng", "第 13 行 · 掛載宿主機根目錄", "/:/host:ro 讓容器讀到整台主機的檔案", "需要批准")}
        {_chk("warn", "第 15 行 · 映像用 latest", "latest 會跟著上游改變，部署紀錄記不到固定版本", "固定版本")}
        {_chk("ok", "語法與相依", "4 個服務、1 個儲存卷，depends_on 都存在", "通過")}
      </div>""")
    parsed = _fp("解析結果", f"""
      <div class="row hd"><span>服務</span><span>映像</span><span>副本</span></div>
      <div class="row"><span class="mono">shop-api</span><span class="mono">shop-api:${{TAG}}</span><span class="mono">2</span></div>
      <div class="row"><span class="mono">shop-worker</span><span class="mono">shop-worker:${{TAG}}</span><span class="mono">1</span></div>
      <div class="row"><span class="mono">shop-redis</span><span class="mono">redis:latest</span><span class="mono">1</span></div>
      <div class="row"><span class="mono">shop-db</span><span class="mono">postgres:16.4</span><span class="mono">1</span></div>""")
    main = head + f'<div class="edwrap">{editor}<div style="display:flex;flex-direction:column;gap:16px;min-width:0">{issues}{parsed}</div></div>' + U.savebar("compose.yaml：3 處未儲存", "儲存成草稿後，要建立變更計畫才會部署", "儲存草稿", "儲存並預覽變更計畫")
    return U.shell("projects", main, collapsed=True)


# ---------- 節點工作區：日誌、終端機 ----------
def node_logs() -> str:
    head = node_head("hazel-tw-01", U.status("ok", "連線中"), "最後聯絡 2 秒前", '<button type="button" class="btn">匯出</button>') + node_tabs("日誌")
    filt = ('<div class="panel" style="padding:10px 14px;display:flex;flex-wrap:wrap;gap:10px;align-items:center">'
            + _seg(["全部", "systemd", "容器", "Agent", "Zentinel", "核心"], 0, "來源")
            + f'<div class="inp" style="min-width:200px;flex:1"><span class="note">搜尋</span><span style="flex:1"></span></div>{_dd("等級：警告以上")}{_dd("最近 1 小時")}'
            + '<button type="button" class="btn on" aria-pressed="true">跟隨最新</button></div>')
    lines = [("03:41:58", "INFO", "ok", "shop-api-1", "GET /api/cart 200 42ms"), ("03:42:01", "WARN", "warn", "shop-api-1", "redis reconnecting (attempt 1)"),
             ("03:42:01", "ERR", "dng", "shop-redis", "Can't open the append-only file: Permission denied"), ("03:42:02", "INFO", "ok", "systemd", "Started docker-shop-redis.scope."),
             ("03:42:03", "WARN", "warn", "kernel", "TCP: request_sock_TCP: Possible SYN flooding on port 443"), ("03:42:05", "INFO", "ok", "zentinel", "reload ok (14 routes)"),
             ("03:42:06", "INFO", "ok", "hazelminx-agent", "heartbeat ok seq=48812 rtt=12ms")]
    body = "\n".join(f'{t} <span style="color:var(--{c if c != "dng" else "danger"})">{lv:<4}</span> <span style="color:var(--muted)">{src:<16}</span> {msg}' for t, lv, c, src, msg in lines)
    viewer = _fp("hazel-tw-01 · 最近 1 小時 · 1,284 行", f'<div style="padding:12px 14px"><div class="term" style="min-height:260px">{body}</div></div>',
                 extra='<a class="more" href="Logs.dc.html">全部節點的日誌 →</a>')
    main = head + filt + viewer
    return U.shell("nodes", main)


def node_terminal() -> str:
    head = node_head("hazel-tw-01", U.status("ok", "連線中"), "最後聯絡 2 秒前", '<a class="btn" href="Terminal.dc.html">在多分頁終端機開啟</a>') + node_tabs("終端機")
    who = ('<div class="panel" style="padding:10px 14px;display:flex;flex-wrap:wrap;gap:10px;align-items:center">'
           + U.status("dng", "身分 root@hazel-tw-01") + U.tier("正式") + _dd("切換身分：deploy") + '<span class="note">這個工作階段會錄下輸入與輸出 · 閒置 15 分自動結束</span></div>')
    term = ('<section class="panel" aria-label="終端機"><div style="padding:12px 14px"><div class="term" style="min-height:320px">'
            'root@hazel-tw-01:~# docker ps --format "{{.Names}}\\t{{.Status}}" | head -4\n'
            'shop-api-1\tUp 46 minutes (healthy)\nshop-api-2\tUp 46 minutes (healthy)\nshop-redis\tRestarting (1) 4 seconds ago\nshop-db\tUp 9 days\n'
            'root@hazel-tw-01:~# journalctl -u docker --since "10 min ago" | tail -1\n'
            'Nov 07 03:42:01 hazel-tw-01 dockerd[912]: container shop-redis exited (1)\nroot@hazel-tw-01:~# <span class="cur" aria-hidden="true">▋</span></div></div></section>')
    cmds = _fp("快捷指令", '<div class="wrap" style="padding:12px 14px">' + "".join(f'<button type="button" class="btn sm mono">{c}</button>' for c in ["docker ps", "df -h", "journalctl -xe", "systemctl --failed", "ss -tulpn", "htop"]) + "</div>")
    main = head + who + term + cmds
    return U.shell("nodes", main)



# ---------- 主機沒有回應（事故頁） ----------
def node_offline() -> str:
    head = node_head("hazel-jp-02", U.status("dng", "沒有回應 · 48 秒"), "最後心跳 03:41:18 · 以下是上次觀測", '<a class="btn" href="Terminal.dc.html">從控制中心 SSH 連線</a><button type="button" class="btn">標記為維護中</button>') + node_tabs("概況", 3)
    impact = _fp("影響", f"""
      <div class="row hd"><span>對象</span><span>現在</span><span>依據</span></div>
      <div class="row"><span class="name"><span class="n">wiki.example.tw</span><span class="d">網站</span></span><span>{U.status("ok", "仍可連")}</span><span class="note">外部探測 03:42:01 · 200 · 212 ms</span></div>
      <div class="row"><span class="name"><span class="n">wiki-app、wiki-db</span><span class="d">容器 · Docker</span></span><span>{U.status("warn", "不明")}</span><span class="note">上次觀測 03:41:18 執行中</span></div>
      <div class="row"><span class="name"><span class="n">zentinel</span><span class="d">主機服務</span></span><span>{U.status("warn", "不明")}</span><span class="note">網站仍可連</span></div>
      <div class="row"><span class="name"><span class="n">寫入動作</span><span class="d">更新、防火牆、部署</span></span><span>{U.status("info", "暫停送出")}</span><span class="note">排隊 1 件 · 恢復後送出</span></div>""")
    timeline = _fp("發生經過", f"""
      <div style="padding:12px 16px;display:flex;flex-direction:column;gap:8px">
        {_chk("warn", "03:38:50 · tim 在這台套用 openssl 更新", "這個更新會重啟 sshd、zentinel · 工作狀態不明（apt 進行到一半）", '<a href="WorkCenter.dc.html">工作</a>')}
        {_chk("dng", "03:41:12 · 核心訊息 eth0: NETDEV WATCHDOG: transmit queue 0 timed out", "網卡傳送卡住，之後 Agent 心跳送不出去", '<a href="NodeLogs.dc.html">日誌</a>')}
        {_chk("dng", "03:41:18 · 最後一次心跳", "之後連到控制中心 203.0.113.22:8443 逾時", "")}
        {_chk("ok", "03:42:01 · 外部探測 wiki.example.tw", "從外部連得到，回應 200", "")}
      </div>""")
    probes = _fp("從控制中心檢查", f"""
      <div class="row hd"><span>檢查</span><span>結果</span><span></span></div>
      <div class="row"><span>ping 198.51.100.35</span><span>{U.status("dng", "無回應 · 3 次")}</span><span><button type="button" class="btn sm">再測一次</button></span></div>
      <div class="row"><span>TCP 22（SSH）</span><span>{U.status("dng", "逾時")}</span><span><button type="button" class="btn sm">再測一次</button></span></div>
      <div class="row"><span>TCP 443（Zentinel）</span><span>{U.status("ok", "可連 · 從外部")}</span><span></span></div>""")
    blocked = _fp("恢復前不能做的事", f"""
      <div style="padding:12px 16px;display:flex;flex-direction:column;gap:8px">
        {_chk("warn", "套用安全更新", "openssl 的更新狀態不明，恢復後先對帳 apt，再決定要不要重跑", "已停用")}
        {_chk("warn", "擋掉 5432 對外連線", "已排入，恢復後套用，套用後一樣要確認", '<a href="FirewallRule.dc.html">看排入的規則</a>')}
        {_chk("warn", "重新開機、部署", "主機沒有回應時不送出", "已停用")}
      </div>""")
    actions = _fp("可以做的事", '<div class="wrap" style="padding:12px 16px"><a class="btn pri" href="Terminal.dc.html">從控制中心 SSH 連線</a><button type="button" class="btn">開主機商主控台</button><button type="button" class="btn">標記為維護中（暫停告警）</button><button type="button" class="btn">通知 ops-ann</button></div>')
    main = head + f'<div class="cg">{impact}{timeline}</div>' + f'<div class="cg">{probes}{blocked}</div>' + actions
    return U.shell("nodes", main)


# ---------- GitHub 匯入：偵測與設定 ----------
def import_configure() -> str:
    head = _form_head([("專案與服務", "Projects.dc.html"), ("從 GitHub 匯入", "GitHubSource.dc.html")], "設定 hazelnut/report", '<a class="btn" href="GitHubSource.dc.html">上一步</a><a class="btn pri" href="WorkCenter.dc.html">部署</a>')
    stepper = f'<div class="panel" style="padding:12px 14px">{U.steps([("連接 GitHub", "done", "hazelnut"), ("選擇倉庫", "done", "hazelnut/report · main"), ("偵測與設定", "cur", "建置、變數、網址"), ("部署", "todo", "")])}</div>'
    detect = _fp("偵測結果", _grid(
        _fld("建置方式", U.status("ok", "Dockerfile（沒有 compose，當成單一服務）")),
        _fld("專案名稱", _in("report")), _fld("環境", _dd("production")),
        _fld("主機", _dd("hazel-tw-01 · Docker（root）")),
        _fld("服務 Port", _in("8080", mono=True, tail=U.status("ok", "Dockerfile EXPOSE 8080"))),
        _fld("健康檢查", _in("GET /healthz", mono=True))) + '<div style="padding:0 16px 14px" class="note">沒有 compose 也沒有 Dockerfile 的倉庫不能直接部署；要先在倉庫加入其中一個。</div>')
    envs = _fp("環境變數", _erows(["名稱", "值", "類型"], [
        ['<span class="mono">DATABASE_URL</span>', _in("postgres://report@shop-db:5432/report", mono=True), _dd("一般")],
        ['<span class="mono">SMTP_PASSWORD</span>', _in("••••••••••", tail=U.icon("eye")), _dd("機密（存機密庫）")]], add="新增變數"),
        extra=f'<a class="btn sm" style="margin-left:auto" href="EnvUpload.dc.html">{U.icon("upload", 14)}上傳 env 檔</a>')
    domain = _fp("網址與 HTTPS", _grid(
        _fld("網址", _in("report.example.tw", mono=True)), _fld("HTTPS", _seg(["自動申請（推薦）", "先不要"], 0, "HTTPS")),
        _fld("DNS 設定", f'<div class="banner" style="display:flex;flex-direction:column;gap:6px;align-items:flex-start">{U.status("warn", "還沒指向這台主機")}<span>到你的 DNS 服務新增一筆紀錄：</span><span class="mono">A　report　203.0.113.22</span><button type="button" class="btn sm">{U.icon("copy", 14)}複製</button></div>', "設定好後這裡會自動變綠，再申請憑證")))
    summary = _fp("部署會做的事", f'<div style="padding:12px 16px">{U.pipeline([("拉取", "todo", "main · 4d81a2c", ""), ("建置", "todo", "Dockerfile", "隔離建置"), ("啟動", "todo", "tw-01", "健康檢查"), ("網址", "todo", "report.example.tw", "Zentinel"), ("憑證", "todo", "DNS 指向後", "自動")])}</div>' + _submit("部署", "只建立，先不部署"))
    main = head + stepper + f'<div class="cg">{detect}{envs}</div>' + domain + summary
    return U.shell("projects", main)


# ---------- 專案環境變數 ----------
def project_env() -> str:
    head = f"""
    <div style="display: flex; flex-wrap: wrap; align-items: center; gap: 14px">
      <div class="name">{U.crumbs([("專案與服務", "Projects.dc.html"), ("shop", "ProjectDetail.dc.html")])}<h1>環境變數</h1></div>
      <div class="wrap" style="margin-left: auto">{_seg(["production", "staging"], 0, "環境")}</div>
    </div>"""
    eye = f'<button type="button" class="ib" aria-label="顯示值">{U.icon("eye")}</button>'
    def r(k, v, src, svc, secret=False):
        val = f'<span class="wrap" style="flex-wrap:nowrap"><span class="mono">••••••••••</span>{eye}</span>' if secret else f'<span class="wr"><span class="mono" style="overflow-wrap:anywhere;white-space:normal">{v}</span></span>'
        return f'<div class="row"><span class="mono">{k}</span><span>{val}</span><span>{src}</span><span class="c3">{svc}</span><span>{U.actions(["rename", "trash"])}</span></div>'
    table = f"""
    <section class="panel" aria-labelledby="h-pv">
      <div class="ph"><h2 id="h-pv">production · 8 個</h2><div class="wrap" style="margin-left:auto"><button type="button" class="btn sm">貼上 .env</button><button type="button" class="btn sm pri">＋ 新增</button></div></div>
      <div class="row hd"><span>名稱</span><span>值</span><span>種類</span><span class="c3">用在</span><span></span></div>
      {r("NODE_ENV", "production", "一般", "全部服務")}
      {r("DATABASE_URL", "postgres://shop@shop-db:5432/shop", "一般", "shop-api、shop-worker")}
      {r("REDIS_URL", "redis://shop-redis:6379", "一般", "shop-api、shop-worker")}
      {r("CHECKOUT_TIMEOUT_MS", "8000", "一般 · 未儲存", "shop-api")}
      {r("DB_PASSWORD", "", "機密", "shop-api、shop-worker、shop-db", secret=True)}
      {r("STRIPE_KEY", "", "機密", "shop-api", secret=True)}
      {r("MAIL_FROM", "shop@example.tw", "一般", "shop-worker")}
      {r("FEATURE_SIGNUP_PROMO", "false", "一般", "shop-api")}
    </section>"""
    apply = _fp("套用", f"""
      <div style="padding:12px 16px;display:flex;flex-direction:column;gap:10px">
        <div class="wrap">{U.status("warn", "1 個變更還沒生效")}<span class="note">CHECKOUT_TIMEOUT_MS 5000 → 8000</span></div>
        <div class="wrap"><span class="note">生效要重建：</span><span class="mono">shop-api ×2</span><span class="note">滾動重建，一次一個，不中斷服務</span></div>
        <div class="wrap"><button type="button" class="btn pri">儲存並重新部署</button><button type="button" class="btn">只儲存</button><button type="button" class="btn">捨棄</button></div>
      </div>""")
    main = head + table + apply
    return U.shell("projects", main)


# ---------- 邀請同事 ----------
def user_invite() -> str:
    head = _form_head([("身分與存取", "IAM.dc.html")], "邀請同事", '<a class="btn" href="IAM.dc.html">取消</a><button type="button" class="btn pri">送出邀請</button>')
    who = _fp("誰", _grid(_fld("Email", _in("mei@example.tw")), _fld("顯示名稱", _in("Mei")), _fld("登入方式", _seg(["密碼＋二次驗證", "公司帳號（OIDC）"], 0, "登入方式"))))
    roles = ('<section class="panel"><div class="ph"><h2>能做什麼</h2></div><div class="fleet choices">'
             + _choice("只能看", "唯讀", ["看得到畫面與日誌", "不能改任何東西、看不到機密值"], U.status("ok", "已選"), on=True)
             + _choice("可以部署", "部署者", ["部署與重啟指定專案", "不能改主機與帳號"], '<button type="button" class="btn sm">選擇</button>')
             + _choice("管理主機", "維運者", ["主機、容器、備份、防火牆", "危險操作要批准"], '<button type="button" class="btn sm">選擇</button>')
             + _choice("查帳", "稽核者", ["只看稽核紀錄與設定", "看不到應用的日誌內容"], '<button type="button" class="btn sm">選擇</button>') + "</div></section>")
    scope = _fp("範圍", _grid(_fld("可以看的專案", '<div class="wrap">' + _cb("shop") + _cb("wiki", False) + "</div>"),
                             _fld("可以看的主機", '<div class="wrap">' + _cb("hazel-tw-01") + _cb("hazel-jp-02", False) + _cb("lab-mini", False) + "</div>"),
                             _fld("期限", _dd("不設期限")), _fld("二次驗證", U.status("ok", "第一次登入時強制設定"))))
    main = head + who + roles + scope + _fp("送出", '<div style="height:4px"></div>' + _submit("送出邀請", "產生邀請連結"))
    return U.shell("iam", main)


# ---------- 第一次啟動 ----------
def setup() -> str:
    def field(label, val, typ="text"):
        return f'<label style="display:flex;flex-direction:column;gap:6px;font-size:13px"><span>{label}</span><span class="inp"><input type="{typ}" value="{val}" style="background:transparent;border:0;flex:1;min-width:0"></span></label>'
    card = "background:var(--panel);border:1px solid var(--line);border-radius:12px;padding:28px;display:flex;flex-direction:column;gap:14px;width:100%;max-width:380px"
    brand = '<div style="display:flex;align-items:center;gap:10px;font-weight:600;font-size:16px"><span class="logo" aria-hidden="true"></span>HazelminX</div>'
    s1 = f"""<section style="{card}" aria-labelledby="h-s1">{brand}<h1 id="h-s1" style="font-size:20px">建立擁有者帳號</h1>{U.steps([("帳號", "cur", ""), ("二次驗證", "todo", ""), ("復原碼", "todo", "")])}
      {field("帳號", "tim")}{field("密碼", "••••••••••••", "password")}<span class="note">安裝時印出的一次性設定碼已驗證</span><button type="button" class="btn pri" style="min-height:40px">繼續</button></section>"""
    qr = '<div aria-label="QR code" style="width:148px;height:148px;align-self:center;background:repeating-conic-gradient(var(--ink) 0 25%,var(--panel) 0 50%) 0 0/18px 18px;border:8px solid #fff;border-radius:6px"></div>'
    s2 = f"""<section style="{card}" aria-labelledby="h-s2">{brand}<h1 id="h-s2" style="font-size:20px">綁定二次驗證</h1>{U.steps([("帳號", "done", ""), ("二次驗證", "cur", ""), ("復原碼", "todo", "")])}
      <button type="button" class="btn pri" style="min-height:40px">{U.icon("lock", 14)}用這台裝置建立 Passkey</button><span class="note" style="text-align:center">或用驗證器 App 掃描</span>{qr}{field("輸入 App 上的 6 位數", "")}</section>"""
    codes = "".join(f'<span class="mono">{c}</span>' for c in ["7KQ2-9MXD", "P4WN-3RCH", "B8TL-6YFE", "Q2ZV-5HJS", "M9DC-1XKP", "R6FA-8NLU"])
    s3 = f"""<section style="{card}" aria-labelledby="h-s3">{brand}<h1 id="h-s3" style="font-size:20px">存好復原碼</h1>{U.steps([("帳號", "done", ""), ("二次驗證", "done", ""), ("復原碼", "cur", "")])}
      <span>手機不見時，用其中一組登入。每組只能用一次。</span><div style="display:grid;grid-template-columns:1fr 1fr;gap:6px 14px;padding:12px;border:1px solid var(--line);border-radius:8px">{codes}</div>
      <div class="wrap"><button type="button" class="btn sm">{U.icon("download", 14)}下載</button><button type="button" class="btn sm">{U.icon("copy", 14)}複製</button></div>{_cb("我已經存在安全的地方", False)}<a class="btn pri" href="Overview.dc.html" style="min-height:40px">進入總覽</a></section>"""
    body = f'<div style="flex:1;display:flex;flex-wrap:wrap;gap:24px;align-items:flex-start;justify-content:center;padding:56px 16px">{s1}{s2}{s3}</div>'
    return f'<div class="hb theme-{{{{theme}}}}" style="min-height:100vh;display:flex;flex-direction:column"><main class="main" style="padding:0">{body}</main></div>'


# ---------- 跨主機安全更新 ----------
def updates() -> str:
    head = _form_head([("主機", "Nodes.dc.html")], "安全更新 · 7 個套件", '<button type="button" class="btn pri">排進今晚維護時段（03:00）</button><button type="button" class="btn">現在套用</button>')
    def r(node, st, pk, impact, when, acts):
        return f'<div class="row"><span class="name"><span class="n mono">{node}</span><span class="d">{pk}</span></span><span>{st}</span><span>{impact}</span><span class="c3">{when}</span><span class="wrap" style="justify-content:flex-end;flex-wrap:nowrap">{acts}</span></div>'
    table = f"""
    <section class="panel" aria-labelledby="h-up" data-batch>
      <div class="ph"><h2 id="h-up">依主機</h2></div>
      <div class="row hd"><span>主機與套件</span><span>可以套用嗎</span><span>影響</span><span class="c3">維護時段</span><span></span></div>
      {r("hazel-jp-02", U.status("dng", "不行 · 主機沒有回應"), "openssl、libssl3、curl、linux-image、docker-ce（5）", U.impacts([("svc", "sshd、zentinel"), ("reboot", "核心 6.1.115")]), "03:00–04:00", '<a class="btn sm" href="NodeOffline.dc.html">看主機</a>')}
      {r("lab-mini", U.status("ok", "可以"), "openssl、libssl3（2）", U.impacts([("svc", "sshd")]), "03:00–04:00", '<a class="btn sm" href="System.dc.html">看清單</a>')}
      {r("hazel-tw-01", U.status("ok", "沒有待套用"), "—", "—", "03:00–04:00", "")}
    </section>"""
    note = _fp("規則", '<div style="padding:12px 16px;display:flex;flex-direction:column;gap:8px">'
               + _chk("ok", "自動套用", "不需要重開機的安全更新，每天在維護時段自動套用", "開")
               + _chk("info", "需要重開機的", "核心等需要重開機的更新，等你按下排程或現在套用", "手動")
               + _chk("warn", "正在進行或狀態不明的更新", "jp-02 的 openssl 更新還沒對帳，這台先不排", "暫停") + "</div>")
    main = head + table + note
    return U.shell("nodes", main)



# ---------- 專案：部署單元 ----------
def units_panel() -> str:
    topo = U.topology([
        ("對外", [("dom", "shop.example.tw", "Zentinel · HTTPS", "ok")]),
        ("部署單元", [("web", "web", "Compose · tw-01 Docker", "ok"), ("wrk", "worker", "Compose · jp-02 Docker", "warn")]),
        ("被引用", [("srch", "search", "Helm · lab-k3s / shop", "ok")]),
    ], [("dom", "web", .8, ""), ("web", "srch", .5, ""), ("wrk", "srch", .3, ""), ("wrk", "web", .2, "dim")])
    topo_m = U.flowlist([("shop.example.tw", "web", "對外"), ("web", "search", "SEARCH_URL"), ("worker", "search", "SEARCH_URL"), ("worker", "web", "API_URL")])
    def u(name, kind, target, st, ver, swap, acts):
        return (f'<div class="row"><span class="name"><span class="n mono">{name}</span><span class="d">{kind}</span></span><span>{st}</span><span class="mono c3">{target}</span>'
                f'<span class="mono c4">{ver}</span><span>{swap}</span><span class="wrap" style="justify-content:flex-end;flex-wrap:nowrap">{acts}</span></div>')
    cfg = '<a class="btn sm" href="UnitConfig.dc.html">設定</a>'
    return f"""
    <section class="panel" aria-labelledby="h-units">
      <div class="ph"><h2 id="h-units">部署單元 · 3</h2><div class="wrap" style="margin-left:auto"><button type="button" class="btn sm">＋ 單元</button><button type="button" class="btn sm pri">部署全部</button></div></div>
      <div style="padding:10px 14px 6px"><div class="ch-d">{topo}</div><div class="ch-m">{topo_m}</div></div>
      <div class="row hd"><span>單元</span><span>狀態</span><span class="c3">部署到</span><span class="c4">版本</span><span>替換方式</span><span></span></div>
      {u("web", "Compose · deploy/compose.yaml", "tw-01 · Docker（root）", U.status("ok", "執行中"), "a3f9c1e", U.status("ok", "無縫替換"), cfg)}
      {u("worker", "Compose · worker/compose.yaml", "jp-02 · Docker（root）", U.status("warn", "主機沒有回應"), "a3f9c1e", U.status("ok", "無縫替換"), cfg)}
      {u("search", "Helm · charts/search", "lab-k3s · namespace shop", U.status("ok", "3／3 就緒"), "chart 1.4.2", U.status("ok", "滾動更新"), cfg)}
    </section>"""


def connection_panel() -> str:
    eye = f'<button type="button" class="ib" aria-label="複製">{U.icon("copy")}</button>'
    def pub(k, v, on, refs):
        tog = f'<label class="wrap" style="flex-wrap:nowrap"><input type="checkbox"{" checked" if on else ""} aria-label="公開 {k}">公開</label>'
        return f'<div class="row"><span class="mono">{k}</span><span class="wr"><span class="mono" style="overflow-wrap:anywhere;white-space:normal">{v}</span></span><span>{tog}</span><span class="c3">{refs}</span></div>'
    def ref(k, expr, val, st):
        return f'<div class="row"><span class="mono">{k}</span><span class="wr"><span class="mono" style="overflow-wrap:anywhere;white-space:normal">{expr}</span></span><span class="wr"><span class="mono" style="overflow-wrap:anywhere;white-space:normal">{val}</span></span><span>{st}</span></div>'
    return f"""
    <section class="panel" aria-labelledby="h-conn">
      <div class="ph"><h2 id="h-conn">search · 連線</h2>{_seg(["web", "worker", "search"], 2, "單元")}</div>
      <div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(min(220px,100%),1fr));gap:10px 16px;padding:12px 14px;font-size:13px">
        {kv("內部位址（入口服務 search）", '<span class="wrap" style="flex-wrap:nowrap"><span class="mono">search.production.shop.internal</span>' + eye + "</span>")}
        {kv("對內 Port", '<span class="mono">http 9200 · transport 9300</span>')}
        {kv("對外網址", '<span class="note">未設定</span>')}
        {kv("從其他主機連入", "tw-01：私有網路 · jp-02：加密轉送，只開 9200")}
      </div>
      <div class="row hd"><span>公開的變數</span><span>值</span><span>公開</span><span class="c3">被誰引用</span></div>
      {pub("SEARCH_URL", "http://${search.HZX_HOST}:${search.HZX_PORT_HTTP}", True, "web、worker")}
      {pub("SEARCH_ADMIN_TOKEN", "••••••••（機密）", False, "—")}
      <div class="ph" style="border-top:1px solid var(--line)"><h2>web 引用了誰</h2></div>
      <div class="row hd"><span>變數</span><span>寫法</span><span>解析結果</span><span>檢查</span></div>
      {ref("SEARCH_URL", "${search.SEARCH_URL}", "http://search.production.shop.internal:9200", U.status("ok", "連得到"))}
      {ref("ADMIN_TOKEN", "${search.SEARCH_ADMIN_TOKEN}", "—", U.status("dng", "search 沒有公開這個變數"))}
    </section>"""


# ---------- 單元設定 ----------
def unit_config() -> str:
    head = _form_head([("專案與服務", "Projects.dc.html"), ("shop", "ProjectDetail.dc.html"), ("production", "ProjectDetail.dc.html")], "單元 web · 設定", '<a class="btn" href="ProjectDetail.dc.html">取消</a><button type="button" class="btn pri">儲存</button>')
    def f(path, on, order, note):
        return (f'<div class="row"><span class="wrap" style="flex-wrap:nowrap"><input type="checkbox"{" checked" if on else ""} aria-label="使用 {path}"><span class="mono">{path}</span></span>'
                f'<span class="mono">{order}</span><span class="c3">{note}</span><span>{U.actions(["more"])}</span></div>')
    files = _fp("Compose 檔", f"""
      <div style="padding:12px 14px;display:flex;flex-direction:column;gap:10px">
        <div class="wrap">{_seg(["自動（同 compose 預設）", "指定檔案（像 Portainer）"], 1, "模式")}<span class="note">指定檔案時不會自動合併 compose.override.yaml，要用就勾進清單</span></div>
      </div>
      <div class="row hd"><span>repo 裡找到的 compose 檔</span><span>順序</span><span class="c3">說明</span><span></span></div>
      {f("deploy/compose.yaml", True, "1 主檔", "4 個服務")}
      {f("deploy/monitoring.yml", True, "2", "加上 node-exporter")}
      {f("compose.override.yaml", False, "—", U.status("warn", "把 ./src 掛進 shop-api · 疑似開發用"))}
      {f("compose.yaml", False, "—", "根目錄 · 舊版")}
      <div style="padding:10px 14px 12px"><button type="button" class="btn sm">預覽合併結果</button> <span class="note">平台的覆寫層永遠排在最後</span></div>""")
    prof = _fp("Profile", f"""
      <div style="padding:12px 14px;display:flex;flex-direction:column;gap:8px">
        <div class="wrap">{_cb("預設（沒有 profile 的服務）", True)}{_cb("debug", False)}{_cb("metrics", True)}</div>
        <div class="wrap">{U.status("info", "shop-debug 不會啟動 · profile debug 未啟用")}</div>
      </div>""")
    swap = _fp("替換方式", f"""
      <div class="row hd"><span>服務</span><span>部署時</span><span class="c3">原因</span><span></span></div>
      <div class="row"><span class="mono">shop-api</span><span>{U.status("ok", "無縫替換")}</span><span class="c3 note">經 Zentinel · 健康檢查 /healthz</span><span>{_seg(["自動", "先停再起"], 0, "shop-api")}</span></div>
      <div class="row"><span class="mono">shop-worker</span><span>{U.status("ok", "無縫替換")}</span><span class="c3 note">佇列消費者 · 新舊同時消費可接受</span><span>{_seg(["自動", "先停再起"], 0, "shop-worker")}</span></div>
      <div class="row"><span class="mono">shop-db</span><span>{U.status("warn", "先停再起 · 約 6 秒 · 需要批准")}</span><span class="c3"><span class="mono">compose.yaml:23</span> container_name · 單寫入資料</span><span></span></div>
      <div class="row"><span class="mono">shop-redis</span><span>{U.status("warn", "先停再起 · 約 2 秒 · 需要批准")}</span><span class="c3"><span class="mono">compose.yaml:31</span> ports 6379:6379 · 單寫入資料</span><span></span></div>""")
    build = _fp("建置", _grid(
        _fld("每次部署", _seg(["重新建置（推薦）", "沒變時沿用上次的映像"], 0, "建置")),
        _fld("快取", _cb("這次不用快取", False)),
        _fld("建置時變數", _dd("build.env（3 個）"), "以 build args 提供，不會進到執行中的容器"),
        _fld("Compose 專案名稱", '<span class="mono">shop-production-web</span>', "改單元名稱也不會變；儲存卷是 shop-production-web_uploads")))
    cfiles = _fp("設定檔", f"""
      <div class="row hd"><span>名稱（路徑）</span><span>掛到</span><span class="c3">版本</span><span></span></div>
      <div class="row"><span class="mono">deploy/nginx.conf</span><span class="mono">shop-api:/etc/nginx/nginx.conf</span><span class="c3">v4 · 10/30</span><span>{U.actions(["rename", "more"])}</span></div>
      <div class="row"><span class="mono">secrets/stripe.pem</span><span class="mono">secrets: stripe_key</span><span class="c3">機密庫</span><span>{U.actions(["rename", "more"])}</span></div>
      <div style="padding:8px 14px 12px"><button type="button" class="btn sm">{U.icon("upload", 14)}上傳設定檔</button></div>""")
    main = head + files + f'<div class="cg">{prof}{build}</div>' + swap + cfiles + U.savebar("web 單元設定：3 處未儲存", "下次部署才會套用", "儲存", "儲存並部署")
    return U.shell("projects", main)


# ---------- 環境變數（四層變數組） ----------
def env_groups_page(upload: bool = False) -> str:
    head = f"""
    <div style="display: flex; flex-wrap: wrap; align-items: center; gap: 14px">
      <div class="name">{U.crumbs([("專案與服務", "Projects.dc.html"), ("shop", "ProjectDetail.dc.html")])}<h1>環境變數</h1></div>
      <div class="wrap" style="margin-left: auto">{_seg(["production", "staging"], 0, "專案環境")}<a class="btn pri" href="EnvUpload.dc.html">{U.icon("upload", 14)}上傳 env 檔</a></div>
    </div>"""
    def g(name, n, refs, on=False, note=""):
        return (f'<a class="row{" sel" if on else ""}" href="#g"><span class="name"><span class="n mono">{name}</span><span class="d">{n} 個 · {refs}</span></span><span>{note}</span></a>')
    groups = f"""
    <section class="panel" aria-labelledby="h-gl">
      <div class="ph"><h2 id="h-gl">變數組</h2><button type="button" class="btn sm" style="margin-left:auto">＋ 新增組</button></div>
      <div class="ph"><span class="note">專案 · 所有環境共用 · 不放機密</span></div>
      {g("shared.env", 2, "web、worker 引用")}
      <div class="ph"><span class="note">專案環境 · production</span></div>
      {g("app.env", 6, "web、worker 引用", on=True)}
      <div class="ph"><span class="note">單元 · web</span></div>
      {g(".env", 2, "Compose 替換用", note=U.status("info", "替換"))}
      {g("app.env", 2, "蓋過專案環境的同名組")}
      {g("build.env", 3, "建置時", note=U.status("info", "建置時"))}
      <div class="ph"><span class="note">服務覆寫 · shop-api</span></div>
      {g("（覆寫層 environment）", 1, "只給 shop-api")}
    </section>"""
    eye = f'<button type="button" class="ib" aria-label="顯示值">{U.icon("eye")}</button>'
    def v(k, val, secret=False):
        vv = f'<span class="wrap" style="flex-wrap:nowrap"><span class="mono">••••••••</span>{eye}</span>' if secret else f'<span class="wr"><span class="mono" style="overflow-wrap:anywhere;white-space:normal">{val}</span></span>'
        return f'<div class="row"><span class="mono">{k}</span><span>{vv}</span><span>{"機密" if secret else "一般"}</span><span>{U.actions(["rename", "trash"])}</span></div>'
    detail = f"""
    <section class="panel" aria-labelledby="h-gd">
      <div class="ph"><h2 id="h-gd" class="mono">app.env</h2><span class="note">專案環境 production · web、worker 用 env_file 引用</span>
        <div class="wrap" style="margin-left:auto"><button type="button" class="btn sm">改名</button><button type="button" class="btn sm">原始文字</button><button type="button" class="btn sm">{U.icon("download", 14)}匯出</button></div></div>
      <div class="row hd"><span>名稱</span><span>值</span><span>種類</span><span></span></div>
      {v("DATABASE_URL", "postgres://shop@shop-db:5432/shop")}
      {v("REDIS_URL", "redis://shop-redis:6379")}
      {v("LOG_LEVEL", "info")}
      {v("MAIL_FROM", "shop@example.tw")}
      {v("DB_PASSWORD", "", True)}
      {v("STRIPE_KEY", "", True)}
    </section>"""
    rename = _fp("改名 app.env → web.env 會影響", f"""
      <div style="padding:12px 16px;display:flex;flex-direction:column;gap:8px">
        {_chk("ok", "覆寫層 · worker 的 env_file", "一起改成 web.env", "自動更新")}
        {_chk("warn", "Git · deploy/compose.yaml:12  env_file: app.env", "平台改不到 Git；app.env 保留為別名，標「Git 仍使用舊名稱」", "保留別名")}
        <div class="wrap"><button type="button" class="btn pri sm">改名</button><button type="button" class="btn sm">取消</button></div>
      </div>""")
    def e(k, val, src, over):
        return f'<div class="row"><span class="mono">{k}</span><span class="mono">{val}</span><span class="note">{src}</span><span class="c3 note">{over}</span></div>'
    eff = f"""
    <section class="panel" aria-labelledby="h-eff">
      <div class="ph"><h2 id="h-eff">shop-api 實際收到的值</h2>{_seg(["shop-api", "shop-worker", "shop-db"], 0, "服務")}</div>
      <div class="row hd"><span>名稱</span><span>最終值</span><span>來源</span><span class="c3">被蓋掉</span></div>
      {e("LOG_LEVEL", "warn", "服務覆寫", "compose environment: error · web/app.env: debug · production/app.env: info")}
      {e("APP_NAME", "Shop", "專案 shared.env:1", "映像 ENV: app")}
      {e("DATABASE_URL", "postgres://shop@shop-db…", "production/app.env:1", "—")}
      {e("CHECKOUT_TIMEOUT_MS", "8000", "web/app.env:2", "—")}
      {e("DB_PASSWORD", "••••••••", "production/app.env:5 · 機密", "—")}
      {e("NODE_ENV", "production", "compose environment（compose.yaml:14）", "映像 ENV: development")}
    </section>"""
    main = head + f'<div class="cg">{groups}{detail}</div>' + f'<div class="cg">{eff}{rename}</div>' + U.savebar("app.env：1 處未儲存（LOG_LEVEL info → warn）", "web、worker 要重新部署才會收到", "儲存並重新部署", "只儲存")
    return U.shell("projects", main)


def env_upload() -> str:
    page = env_groups_page()
    def p(k, kind, old, new, line):
        return f'<div class="row"><span class="mono">{k}</span><span>{kind}</span><span class="mono">{old}</span><span class="mono">{new}</span><span class="note">{line}</span></div>'
    dlg = f"""
    <div class="cmdp" role="dialog" aria-labelledby="h-up" style="max-width:720px">
      <div class="ph"><h2 id="h-up">上傳 env 檔</h2></div>
      <div style="padding:12px 16px;display:flex;flex-direction:column;gap:12px">
        <div class="wrap">{U.icon("files")}<span class="mono">prod.env</span><span class="note">2.1 KB · 24 行 · UTF-8 · LF</span></div>
        {_grid(_fld("組名（就是 env_file 的檔名）", _in("prod.env", mono=True), "預設是上傳的檔名，可以改"), _fld("放在", _seg(["單元 web", "專案環境 production", "專案"], 0, "放在")), minw=220)}
        <section class="panel" aria-label="預覽"><div class="row hd"><span>名稱</span><span>變更</span><span>目前</span><span>上傳後</span><span>行</span></div>
        {p("API_BASE", U.status("ok", "新增"), "—", "https://api.example.tw", "3")}
        {p("LOG_LEVEL", U.status("warn", "變更"), "info", "warn", "7 · 19 重複 · 以 19 為準")}
        {p("DB_PASSWORD", U.status("info", "新增 · 判定為機密"), "—", "••••••••", "11")}
        {p("MULTILINE_CERT", U.status("ok", "新增"), "—", "-----BEGIN…（3 行）", "14–16")}</section>
        <div class="wrap">{U.status("dng", "第 21 行無效：=oops（缺名稱）")}<span class="note">其餘 22 行照常匯入 · 現有但檔案裡沒有的 4 個變數保留</span></div>
        <div class="wrap"><button type="button" class="btn pri">匯入成 prod.env</button><button type="button" class="btn">取消</button></div>
      </div>
    </div>"""
    i = page.index('<main class="main">') + len('<main class="main">')
    return page[:i] + f'<div class="scrim">{dlg}</div>' + page[i:]



# ---------- 新增站點：靜態網站 ----------
def site_static() -> str:
    head = _form_head([("網站與入口", "Websites.dc.html"), ("新增站點", "SiteCreate.dc.html")], "靜態網站", '<a class="btn" href="Websites.dc.html">取消</a><button type="button" class="btn pri">建立站點</button>')
    kinds = ('<section class="panel"><div class="ph"><h2>類型</h2></div><div class="fleet choices">'
             + _choice("反向代理", "", ["轉到專案服務、容器或主機 Port"], '<a class="btn sm" href="SiteCreate.dc.html">選擇</a>')
             + _choice("靜態網站", "", ["由 Zentinel 直接提供檔案"], U.status("ok", "已選"), on=True)
             + _choice("執行環境", "PHP · Node", ["用平台管理的執行環境容器"], '<button type="button" class="btn sm">選擇</button>')
             + _choice("重新導向", "", ["整個網域轉到另一個網址"], '<button type="button" class="btn sm">選擇</button>') + "</div></section>")
    def disk(name, mnt, free, default, created, on=False):
        st = U.status("ok", "已建立") if created else U.status("info", "第一次用時建立")
        return (f'<div class="ncard{" on" if on else ""}"><div class="nh"><span class="n mono">{mnt}</span><span class="d">{name}</span></div>'
                f'{U.meter(free[0] / free[1] * 100, f"{free[0]}/{free[1]} GiB")}<span class="note">預設位置</span><span class="mono">{default}</span>'
                f'<div class="wrap">{st}{U.status("ok", "已選") if on else chr(32)}</div></div>')
    where = f"""
    <section class="panel" aria-labelledby="h-loc">
      <div class="ph"><h2 id="h-loc">檔案放在哪裡</h2><span class="note">Zentinel @ hazel-tw-01 直接讀取，所以要在 tw-01 上</span></div>
      <div style="padding:12px 14px 4px">{_seg(["用預設位置", "選擇資料夾…", "上傳檔案", "從 Git 部署"], 0, "來源")}</div>
      <div class="fleet choices">
        {disk("資料碟 · sdb1", "/mnt/data", (412, 931), "/mnt/data/sites/report.example.tw", True, on=True)}
        {disk("系統碟 · nvme0n1p2", "/", (36, 94), "/srv/sites/report.example.tw", False)}
        {disk("NFS · tw-01:/mnt/data/nfs", "/mnt/nfs/backup", (19, 200), "/mnt/nfs/backup/sites/report.example.tw", False)}
      </div>
      <div style="padding:0 14px 14px;display:flex;flex-direction:column;gap:8px">
        {_fld("實際路徑", _in("/mnt/data/sites/report.example.tw", mono=True, tail='<a class="btn sm" href="FolderPicker.dc.html">選擇資料夾…</a>'))}
        <div class="wrap">{U.status("ok", "Zentinel（zentinel 使用者）可以讀取")}{U.status("info", "資料夾還不存在 · 建立站點時建立，擁有者 deploy:zentinel 750")}</div>
      </div>
    </section>"""
    serve = _fp("怎麼提供", _grid(
        _fld("首頁", _in("index.html", mono=True)), _fld("找不到頁面時", _seg(["404 頁", "回到 index.html（單頁應用）"], 1, "找不到頁面")),
        _fld("目錄列表", _seg(["關", "開"], 0, "目錄列表")), _fld("快取", _dd("HTML 不快取 · 其他 7 天"))))
    domains = _fp("網域與 HTTPS", _grid(_fld("網域", _in("report.example.tw", mono=True)), _fld("HTTPS", _seg(["自動申請（推薦）", "先不要"], 0, "HTTPS"))))
    main = head + kinds + where + f'<div class="cg">{serve}{domains}</div>' + _fp("建立", '<div style="height:4px"></div>' + _submit("建立站點"))
    return U.shell("web", main)


def folder_picker() -> str:
    page = site_static()
    tree = U.tree([(0, "/mnt/data", "931G", False), (1, "apps", "402G", False), (2, "shop", "290G", False), (2, "wiki", "48G", False), (1, "sites", "1.4G", False), (2, "docs.example.tw", "220M", False, "站點 docs.example.tw 正在用"), (2, "report.example.tw", "—", True), (1, "backups", "38G", False), (0, "/srv", "12G", False), (1, "sites", "—", False), (0, "/mnt/nfs/backup", "19G", False),
                   (0, "/etc", "24M", False, "系統目錄不能選"), (0, "/boot", "312M", False, "系統目錄不能選"), (0, "/proc", "—", False, "系統目錄不能選"), (0, "/sys", "—", False, "系統目錄不能選")], open_depth=1)
    def r(name, kind, size, note="", dis=""):
        cls = ' dis" aria-disabled="true" data-tip="' + dis if dis else ""
        lock = U.icon("lock", 14) + " " if dis else ""
        return f'<div class="row{cls}"><span class="name"><span class="n mono">{lock}{name}</span><span class="d">{kind}</span></span><span class="mono">{size}</span><span class="note">{dis or note}</span></div>'
    dlg = f"""
    <div class="cmdp" role="dialog" aria-labelledby="h-fp" style="max-width:860px">
      <div class="ph"><h2 id="h-fp">選擇資料夾 · hazel-tw-01</h2></div>
      <div style="padding:10px 14px;display:flex;gap:8px;flex-wrap:wrap;align-items:center;border-bottom:1px solid var(--line)">
        <div class="crumbs"><a href="#r" aria-label="根目錄">{U.icon("storage", 14)}</a><span class="sep">/</span><a href="#1">mnt</a><span class="sep">/</span><a href="#2">data</a><span class="sep">/</span><a href="#3">sites</a></div>
        <button type="button" class="btn sm" style="margin-left:auto">{U.icon("newfolder", 14)}新資料夾</button>
      </div>
      <div class="fm" style="grid-template-columns:minmax(200px,260px) 1fr">
        <div class="pane treep" style="padding:6px 8px;overflow:auto">{tree}</div>
        <section class="panel" style="border:0;border-radius:0" aria-label="sites 的內容">
          <div class="row hd"><span>名稱</span><span>大小</span><span>說明</span></div>
          {r("docs.example.tw", "資料夾", "220 MiB", dis="站點 docs.example.tw 正在用 · 不能選")}
          {r("report.example.tw", "資料夾 · 還不存在", "—", "建立站點時建立")}
          {r("README.txt", "檔案", "2 KiB", dis="這裡要選資料夾 · 檔案不能選")}
        </section>
      </div>
      <div style="padding:10px 14px;display:flex;flex-direction:column;gap:8px;border-top:1px solid var(--line)">
        <div class="wrap"><span class="note">選取</span><span class="mono">/mnt/data/sites/report.example.tw</span>{U.status("ok", "Zentinel 可以讀取")}</div>
        <div class="wrap note">只列 tw-01 上的資料夾 · 不能選的項目照樣列出，滑過看原因</div>
        <div class="wrap"><button type="button" class="btn pri">選這個資料夾</button><button type="button" class="btn">取消</button></div>
      </div>
    </div>"""
    i = page.index('<main class="main">') + len('<main class="main">')
    return page[:i] + f'<div class="scrim">{dlg}</div>' + page[i:]


SCREENS = {
    # name: (title, builder, w, h_guess, mobile)
    "Overview": ("總覽", overview, 1440, 1700),
    "NodeDetail": ("節點 hazel-tw-01", node_detail, 1440, 1700),
    "Projects": ("專案與服務", project_list, 1440, 900),
    "ProjectDetail": ("專案 shop", projects, 1440, 1700),
    "ContainerResources": ("容器 · 映像、儲存卷、網路與快取", container_resources, 1440, 2200),
    "Containers": ("容器", containers, 1440, 1500),
    "ContainerDetail": ("容器 shop-api-1", container_detail, 1440, 1500),
    "WorkCenter": ("工作中心", work_center, 1440, 1700),
    "NodeOffline": ("主機 hazel-jp-02 沒有回應", node_offline, 1440, 1100),
    "ImportConfigure": ("GitHub 匯入 · 偵測與設定", import_configure, 1440, 1300),
    "ProjectEnv": ("專案 shop › 環境變數（變數組）", env_groups_page, 1440, 1100),
    "EnvUpload": ("上傳 env 檔", env_upload, 1440, 1100),
    "UnitConfig": ("單元 web › 設定", unit_config, 1440, 1400),
    "UserInvite": ("邀請同事", user_invite, 1440, 1100),
    "Setup": ("第一次啟動", setup, 1440, 900),
    "Updates": ("跨主機安全更新", updates, 1440, 800),
    "ContainerCreate": ("建立容器", container_create, 1440, 1800),
    "SiteCreate": ("新增站點", site_create, 1440, 1700),
    "SiteStatic": ("新增站點 · 靜態網站", site_static, 1440, 1300),
    "FolderPicker": ("選擇資料夾", folder_picker, 1440, 1300),
    "FirewallRule": ("新增防火牆規則", firewall_rule, 1440, 1300),
    "CronCreate": ("新增計畫任務", cron_create, 1440, 1500),
    "BackupCreate": ("新增備份任務", backup_create, 1440, 1900),
    "AddNode": ("新增主機", add_node, 1440, 1200),
    "CommandPalette": ("⌘K 搜尋", command_palette, 1440, 900),
    "BatchResult": ("批次操作結果", batch_result, 1440, 900),
    "ComposeEditor": ("Compose 編輯器（側欄收合）", compose_editor, 1440, 1100),
    "NodeLogs": ("節點 hazel-tw-01 › 日誌", node_logs, 1440, 1000),
    "NodeTerminal": ("節點 hazel-tw-01 › 終端機", node_terminal, 1440, 1000),
    "GitHubSource": ("從 GitHub 匯入", github_source, 1440, 1100),
    "DeployCheck": ("部署檢查 · 變更計畫 #418", lambda: deploy_check(False), 1440, 1500),
    "DeployConfirm": ("批准確認", lambda: deploy_check(True), 1440, 1900),
    "States": ("錯誤與空狀態", states, 1440, 1300),
    "Websites": ("網站與入口", websites, 1440, 2200),
    "ComponentDown": ("網站與入口 · Zentinel 已停止", component_down_page, 1440, 1100),
    "Storage": ("儲存與備份", storage, 1440, 2300),
    "Events": ("事件與告警", events, 1440, 2200),
    "Nodes": ("主機", nodes, 1440, 2600),
    "System": ("宿主機系統 hazel-jp-02", system, 1440, 2600),
    "Terminal": ("終端機", terminal, 1440, 1800),
    "Kubernetes": ("Kubernetes", kubernetes, 1440, 2200),
    "AppStore": ("應用商店", appstore, 1440, 2400),
    "APIProxyService": ("API 代理 · 服務設定", apiproxy_service, 1440, 1300),
    "APIProxyTokens": ("API 代理 · 使用者與 Token", apiproxy_tokens, 1440, 1500),
    "APIProxyFailures": ("API 代理 · 失敗紀錄", apiproxy_failures, 1440, 1100),
    "APIProxy": ("API 代理", apiproxy, 1440, 2200),
    "Database": ("資料庫", database, 1440, 2600),
    "AI": ("AI", ai, 1440, 2600),
    "ModelHub": ("AI · 模型庫", model_hub, 1440, 2200),
    "Files": ("檔案", files, 1440, 2200),
    "Cron": ("計畫任務", cron, 1440, 2600),
    "Security": ("安全防護", security, 1440, 2400),
    "Analytics": ("流量分析", analytics, 1440, 2400),
    "IAM": ("身分與存取", iam, 1440, 2400),
    "Settings": ("平台設定", settings, 1440, 2600),
    "Logs": ("日誌", logs, 1440, 2200),
    "AppSettings": ("nextcloud · 網路與掛載", app_settings, 1440, 1300),
    "AppInstall": ("安裝應用（1Panel 市集）", app_install, 1440, 1800),
    "HostServices": ("服務模組（Webmin 等價）", host_services, 1440, 1900),
    "ContainersEmpty": ("容器引擎未安裝", containers_empty, 1440, 1900),
    "KubernetesEmpty": ("Kubernetes 未連接", kubernetes_empty, 1440, 1500),
    "Login": ("登入", login, 1440, 900),
    "M-Overview": ("總覽 · 360", overview, 360, 3600),
    "M-WorkCenter": ("工作中心 · 360", work_center, 360, 3200),
    "M-DeployConfirm": ("批准確認 · 360", lambda: deploy_check(True), 360, 3400),
}


def _top_spans(inner: str):
    """把一列的頂層 <span>…</span> 切出來（處理巢狀）。回傳 list[str]。"""
    out, depth, start, i = [], 0, None, 0
    while True:
        m = re.search(r"<(/?)span\b", inner[i:])
        if not m:
            break
        j = i + m.start()
        if m.group(1) == "":
            if depth == 0:
                start = j
            depth += 1
        else:
            depth -= 1
            if depth == 0:
                end = inner.index(">", j) + 1
                out.append(inner[start:end]); start = None
        i = j + 1
    return out


def trim_columns(html: str) -> str:
    """每張表：在所有列都是空的欄位（含表頭無字）拿掉，列加 cols-N。"""
    def empty(sp):
        return (re.sub(r"<[^>]+>", "", sp).strip() == "" and "<svg" not in sp and "<button" not in sp and "<i " not in sp
                and "class=\"meter" not in sp and "class=\"sp" not in sp and "class=\"st" not in sp and "class=\"timeline" not in sp and "class=\"seg" not in sp and "<a " not in sp)
    def fix_section(sec: str) -> str:
        rows = list(re.finditer(r'<(a|div) class="row ([^"]*)"([^>]*)>(.*?)</\1>', sec, flags=re.S))
        if len(rows) < 2:
            return sec
        spans = [_top_spans(r.group(4)) for r in rows]
        if len({len(x) for x in spans}) != 1 or len(spans[0]) < 4:
            return sec
        n = len(spans[0])
        is_hd = ["hd" in r.group(2).split() for r in rows]
        drop = [j for j in range(n) if all(empty(sp[j]) for sp, hd in zip(spans, is_hd) if not hd) and all(re.sub(r"<[^>]+>", "", sp[j]).strip() == "" for sp, hd in zip(spans, is_hd) if hd)]
        if not drop or n - len(drop) < 3:
            return sec
        keep = n - len(drop)
        out = sec
        for r, sp in reversed(list(zip(rows, spans))):
            inner = "".join(x for j, x in enumerate(sp) if j not in drop)
            cls = " ".join(c for c in r.group(2).split() if c not in ("srow", "nrow", "crow") and not c.startswith("cols-"))
            tail = "acts-end" if "acts-end" in cls else f"cols-{keep}"
            cls = cls.replace("acts-end", "").strip()
            new = f'<{r.group(1)} class="row {cls} {tail}"{r.group(3)}>{inner}</{r.group(1)}>'
            out = out[:r.start()] + new + out[r.end():]
        return out
    return re.sub(r'<section class="panel".*?</section>', lambda m: fix_section(m.group(0)), html, flags=re.S)


def table_affordances(html: str) -> str:
    """section 帶 data-sort="N:asc|desc"、data-batch、data-pages="1–8 / 16" 時補排序指示、勾選框、分頁列。"""
    def fix(m):
        sec = m.group(0)
        attrs = m.group(1)
        sort = re.search(r'data-sort="(\d+):(asc|desc)"', attrs)
        batch = "data-batch" in attrs
        pages = re.search(r'data-pages="([^"]+)"', attrs)
        hd = re.search(r'<div class="row hd ([^"]*)"([^>]*)>(.*?)</div>', sec, flags=re.S)
        if hd:
            spans = _top_spans(hd.group(3))
            si, sd = (int(sort.group(1)), sort.group(2)) if sort else (0, "asc")
            new = []
            for i, sp in enumerate(spans):
                inner = re.sub(r"^<span[^>]*>|</span>$", "", sp)
                cls = re.search(r'class="([^"]*)"', sp)
                cls = cls.group(1) if cls else ""
                if inner.strip() and i < 5 and "ck" not in cls:
                    arrow = U.icon("up" if sd == "asc" else "down", 12) if i == si else U.icon("sort", 12)
                    inner = f'<span class="sortable{" on" if i == si else ""}">{inner} {arrow}</span>'
                new.append(f'<span class="{cls}">{inner}</span>')
            sec = sec[:hd.start(3)] + "".join(new) + sec[hd.end(3):]
        if batch:
            # 獨立勾選欄：表頭與每列都在第一欄插入 .ck，列加 batch 類別
            sec = re.sub(r'<(a|div) class="row ([^"]*)"([^>]*)>', lambda r: f'<{r.group(1)} class="row {r.group(2)} batch"{r.group(3)}><span class="ck"><input type="checkbox" aria-label="{"全選" if "hd" in r.group(2).split() else "選取"}"{" checked" if "sel" in r.group(2).split() else ""}></span>', sec)
        if pages:
            footer = (f'<div class="pager"><span>顯示 {pages.group(1)}</span><span class="pg"><button type="button" class="ib" aria-label="上一頁" aria-disabled="true">{U.icon("chev")}</button>'
                      f'<span>每頁 25</span><button type="button" class="ib" aria-label="下一頁">{U.icon("chev")}</button></span></div>')
            sec = sec[: sec.rfind("</section>")] + footer + "</section>"
        return sec
    return re.sub(r'<section class="panel"([^>]*)>.*?</section>', fix, html, flags=re.S)


class _Row:
    def __init__(self, start, end, tag, cls, attrs, inner):
        self.s, self.e, self.tag, self.cls, self.attrs, self.inner = start, end, tag, cls, attrs, inner
    def start(self): return self.s
    def end(self): return self.e
    def group(self, i):
        return {1: self.tag, 2: self.cls, 3: self.attrs, 4: self.inner}[i]


def _rows(sec: str):
    """找出區塊裡的 .row（考慮同名標籤嵌套，例如列裡的 div 時間軸）。"""
    out = []
    for m in re.finditer(r'<(a|div|label) class="row ([^"]*)"([^>]*)>', sec):
        tag = m.group(1); depth = 1; i = m.end()
        pat = re.compile(rf"<(/?){tag}\b")
        while depth:
            n = pat.search(sec, i)
            if not n:
                break
            depth += 1 if n.group(1) == "" else -1
            i = n.end()
        close = sec.rfind(f"</{tag}", 0, i)
        end = sec.index(">", close) + 1
        out.append(_Row(m.start(), end, tag, m.group(2), m.group(3), sec[m.end():close]))
    # 只留最外層
    return [r for r in out if not any(o.s < r.s and r.e <= o.e for o in out if o is not r)]


def auto_tables(html: str) -> str:
    """表格欄寬依內容自動算（對齊真正的 table／Kumo Table）：面板變成 grid，列用 subgrid 共用欄。
    名稱欄吃剩餘空間，其他欄依內容；.c3/.c4/.c5 欄的軌道在隱藏時一起拿掉（--k3/--k4/--k5 設為空）。"""
    def text(sp):
        return re.sub(r"<[^>]+>", "", sp).strip()
    def fix(m):
        sec = m.group(0)
        rows = _rows(sec)
        if len(rows) < 2 or any("frow" in r.group(2).split() for r in rows):
            return sec
        spans = [_top_spans(r.group(4)) for r in rows]
        n = len(spans[0])
        if n < 2 or any(len(x) != n for x in spans):
            return sec
        body = [sp for sp, r in zip(spans, rows) if "hd" not in r.group(2).split()] or spans
        def cls_of(j):
            c = re.search(r'class="([^"]*)"', spans[0][j][:spans[0][j].index(">")])
            return c.group(1).split() if c else []
        cols = list(range(n))
        name_col = next((j for j in cols if any('class="name"' in sp[j][:40] for sp in body)), None)
        if name_col is None:
            cand = [j for j in cols if "ck" not in cls_of(j)]
            name_col = max(cand, key=lambda j: sum(len(text(sp[j])) for sp in body))
        tracks = []
        for j in cols:
            c = cls_of(j)
            if "ck" in c:
                t = "44px" if j == 0 else "28px"
            elif j == name_col:
                t = "minmax(min(160px,34%),1fr)"
            elif any(('class="timeline' in sp[j]) or ('class="seg' in sp[j]) for sp in body):
                t = "minmax(160px,2fr)"
            else:
                t = "auto"
            for k in ("c3", "c4", "c5", "c6"):
                if k in c:
                    t = f"var(--k{k[1]},{t})"
            tracks.append(t)
        a, b = rows[0].start(), rows[-1].end()
        mid = re.sub(r'(class="row [^"]*"[^>]*style="[^"]*?)grid-template-columns:[^;"]*;?', r"\1", sec[a:b])
        return sec[:a] + f'<div class="tw" style="--tc:{" ".join(tracks)}">' + mid + "</div>" + sec[b:]
    return re.sub(r'<section class="panel"[^>]*>.*?</section>', fix, html, flags=re.S)


def iconize(html: str) -> str:
    """UI 以圖示為主：說明句（help、句子型 note、名稱下的句子）改成 ⓘ 提示，不直接佔版面。"""
    SENT = re.compile(r"[，；。]")
    def plain(x):
        return re.sub(r"<[^>]+>", "", x).strip()
    def span_end(h, start):
        depth, i = 0, start
        while True:
            m = re.search(r"<(/?)span\b", h[i:])
            j = i + m.start()
            depth += 1 if m.group(1) == "" else -1
            i = j + 1
            if depth == 0:
                return h.index(">", j) + 1
    while True:
        k = html.find('<span class="help"')
        if k == -1:
            break
        e = span_end(html, k)
        inner = re.sub(r'^<span class="help"[^>]*>|</span>$', "", html[k:e])
        html = html[:k] + U.hint(plain(inner)) + html[e:]
    # 欄位的 ⓘ 移到欄位標題後面
    out, pos = [], 0
    for m in re.finditer(r'<span class="hint".*?</span>', html, flags=re.S):
        start = m.start()
        f = html.rfind('<div class="field"', 0, start)
        lab = html.find("</label>", f) if f != -1 else -1
        if f != -1 and f < lab < start and lab >= pos:
            out.append(html[pos:lab]); out.append(" " + m.group(0)); out.append(html[lab:start]); pos = m.end()
    out.append(html[pos:]); html = "".join(out)
    def note(m):
        t = plain(m.group(2))
        return U.hint(t) if SENT.search(t) else m.group(0)
    html = re.sub(r'<span class="(note|d)"[^>]*>((?:(?!</?span).)*)</span>', note, html, flags=re.S)
    def divnote(m):
        t = plain(m.group(1))
        return f'<div class="foot">{U.hint(t)}</div>' if SENT.search(t) else m.group(0)
    html = re.sub(r'<div class="note"[^>]*>(.*?)</div>', divnote, html, flags=re.S)
    # 只剩一個 ⓘ 的區塊註腳：移到該面板標題旁
    def sec(m):
        body = m.group(0)
        hints = re.findall(r'<div class="foot">(<span class="hint".*?</span>)</div>', body, flags=re.S)
        if not hints or "</h2>" not in body:
            return body
        body = re.sub(r'<div class="foot"><span class="hint".*?</span></div>', "", body, flags=re.S)
        return body.replace("</h2>", "</h2>" + "".join(hints), 1)
    html = re.sub(r'<section\b.*?</section>', sec, html, flags=re.S)
    return html


def declutter(html: str) -> str:
    """UI 不寫文章：拿掉說明句、保留短標籤、連結與圖例。"""
    def sub_ok(m):
        t = re.sub(r"<[^>]+>", "", m.group(1))
        return m.group(0) if (len(t) <= 14 and "；" not in t and "＝" not in t and "=" not in t and "，" not in t) else ""
    html = re.sub(r'<span class="sub">(.*?)</span>', sub_ok, html)
    html = re.sub(r'<div class="note" style="padding:\s*(0 14px 10px|8px 14px 10px|8px 14px;[^"]*)">.*?</div>', "", html, flags=re.S)
    def foot_fix(m):
        parts = [x.strip() for x in m.group(1).split(" · ")]
        keep = [x for x in parts if "<a " in x or (len(re.sub(r"<[^>]+>", "", x)) <= 14 and "=" not in x and "＝" not in x and "，" not in x and "：" not in x)]
        return f'<div class="foot">{" · ".join(keep)}</div>' if keep else ""
    html = re.sub(r'<div class="foot">(.*?)</div>', foot_fix, html, flags=re.S)
    return html


BANNED = ["同上", "同左", "如上", "見上", "同前", "上述", "下一步", "建議", "可能", "若不是", "不假裝", "無關", "暖機", "別手動", "先別", "才算", "不能取消的原因", "不等於", "就在這", "一眼"]


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", default=os.path.join(os.path.dirname(__file__), "..", "screens"))
    ap.add_argument("--render-dir", default=None)
    args = ap.parse_args()
    os.makedirs(args.out, exist_ok=True)
    if args.render_dir:
        os.makedirs(args.render_dir, exist_ok=True)
    for name, (title, fn, w, h) in SCREENS.items():
        raw = fn().replace('class="row"', 'class="row r0"').replace('class="row hd"', 'class="row hd r0"')
        body = auto_tables(table_affordances(trim_columns(iconize(declutter(raw)))))
        for bad in BANNED:
            for m in re.finditer(bad, re.sub(r"<[^>]+>", " ", body)):
                print(f"  ! {name}: 「{bad}」 … {re.sub(r'<[^>]+>', ' ', body)[max(0, m.start() - 20):m.end() + 20].strip()}")
        with open(os.path.join(args.out, f"{name}.dc.html"), "w", encoding="utf-8") as f:
            f.write(U.dc_wrap(f"HazelminX {title}", body, w, h))
        if args.render_dir:
            for theme in ("dark", "light"):
                with open(os.path.join(args.render_dir, f"{name}-{theme}.html"), "w", encoding="utf-8") as f:
                    f.write(U.plain_html(f"HazelminX {title}", body, theme))
    print("built", len(SCREENS), "screens →", os.path.abspath(args.out))


if __name__ == "__main__":
    main()
