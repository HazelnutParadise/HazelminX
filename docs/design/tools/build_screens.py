"""產出 HazelminX 方向 B 的主要畫面原型。

用法：python3 docs/design/tools/build_screens.py [--render-dir DIR]
輸出：docs/design/screens/*.dc.html（Claude Design 畫布格式）
     DIR/<name>-{light,dark}.html（本機瀏覽器可直接開）
"""
from __future__ import annotations

import argparse
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
        U.stat_tile("節點", "2", "/3 連線中", "jp-02 狀態過期", False, spark=[3] * 20 + [2, 2, 2, 2], note=""),
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
      <div class="ph"><h2 id="h-nodes">艦隊 · 3 節點</h2><span class="sub">環＝目前值，門檻 70／90（磁碟 80／90）；離線不等於應用已停止</span><a class="more" href="NodeDetail.dc.html">表格檢視 →</a></div>
      <div class="fleet">
        {U.node_card("hazel-tw-01", "Ubuntu 24.04 · 控制中心 · Docker · 8 核 · L4 GPU", U.status("ok", "連線中"), 37, (11.2, 31.3), (412, 931), CPU_TW[-12:], "2 秒前", (9, 10))}
        {U.node_card("hazel-jp-02", "Debian 12 · Docker · 4 核 · 資料碟未綁定", U.status("warn", "狀態過期"), 64, (13.9, 15.5), (870, 931), CPU_JP[-12:], "03:41:18", (3, 3), stale=True)}
        {U.node_card("lab-mini", "Debian 13 · Podman rootless · 4 核", U.status("info", "維護中"), 3, (1.1, 7.6), (38, 118), CPU_LAB[-12:], "5 秒前", (0, 2))}
      </div>
      <div class="note" style="padding: 0 14px 10px">* 上次觀測值，節點已過期 · 綠色段＝執行中的容器</div>
    </section>"""

    topo = U.topology([
        ("邊緣", [("cf", "Cloudflare", "92K／日 · 取樣 0.9%", "ok")]),
        ("入口", [("zt", "Zentinel", "tw-01 · WAF 偵測", "ok")]),
        ("站點", [("shop", "shop.example.tw", "憑證 11 天 · 64%", "warn"), ("wiki", "wiki.example.tw", "正常 · 31%", "ok"), ("stg", "staging.shop", "502 · 部署失敗", "danger")]),
        ("服務", [("api", "shop-api", "4 容器 · p95 310", "ok"), ("wapp", "wiki-app", "2 容器 · p95 180", "ok"), ("sapi", "shop-api · stg", "migration 失敗", "danger")]),
        ("節點", [("tw", "hazel-tw-01", "連線中", "ok"), ("jp", "hazel-jp-02", "狀態過期", "warn"), ("lab", "lab-mini", "維護中", "info")]),
    ], [("cf", "zt", 1, ""), ("zt", "shop", .64, ""), ("zt", "wiki", .31, ""), ("zt", "stg", .05, "danger"),
        ("shop", "api", .64, ""), ("wiki", "wapp", .31, ""), ("stg", "sapi", .05, "danger"),
        ("api", "tw", .64, ""), ("wapp", "jp", .31, ""), ("sapi", "lab", .05, "danger")], w=820, box_w=142, box_h=42)
    topo_m = U.flowlist([("Cloudflare", "Zentinel", "92K/日"), ("Zentinel", "shop.example.tw", "64%"), ("Zentinel", "wiki.example.tw", "31%"), ("Zentinel", "staging.shop", "失敗"),
                         ("shop.example.tw", "shop-api @ tw-01", "4 容器"), ("wiki.example.tw", "wiki-app @ jp-02", "2 容器"), ("staging.shop", "shop-api @ lab-mini", "失敗")])
    topology_panel = f"""
    <section id="topo" class="panel" aria-labelledby="h-topo">
      <div class="ph"><h2 id="h-topo">流量怎麼走</h2><span class="sub">線寬＝今日請求佔比；形狀＝狀態（●正常 ○過期／待處理 ■失敗／維護）</span><a class="more" href="Websites.dc.html">網站與入口 →</a></div>
      <div style="padding: 10px 14px 6px"><div class="ch-d">{topo}</div><div class="ch-m">{topo_m}</div></div>
    </section>"""

    hostmap = U.hostmap([
        ("hazel-tw-01", [("shop-api", 21, "ok"), ("shop-worker", 9, "ok"), ("shop-db", 3, "ok"), ("shop-redis", 1, "bad"), ("wiki-media", 6, "ok"), ("ollama", 44, "ok"), ("zentinel", 2, "ok"), ("hazelminx-agent", 0, "ok"), ("backup-runner", 12, "ok"), ("mail-relay", 1, "ok")]),
        ("hazel-jp-02", [("wiki-app", 30, "stale"), ("wiki-db", 12, "stale"), ("zentinel", 4, "stale")]),
        ("lab-mini", [("shop-api", 0, "off"), ("shop-db", 0, "off")]),
    ])
    hostmap_panel = f"""
    <section id="hostmap" class="panel" aria-labelledby="h-hm">
      <div class="ph"><h2 id="h-hm">容器地圖 · 15</h2><span class="sub">一格一容器，顏色＝CPU；異常與過期用形狀標</span><a class="more" href="Containers.dc.html">容器 →</a></div>
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
          {U.steps([("已產生", "done", "03:00"), ("已傳輸", "done", "S3 tokyo"), ("校驗通過", "done", "sha256"), ("還原測試", "todo", "尚未")])}
        </div>
      </div>
    </section>"""

    def pcard(name, env, st, commit, when, node, domain, timeline, note=""):
        return (f'<a class="ncard" href="Projects.dc.html"><div class="nh"><span class="n">{name}</span><span class="note">{env}</span>{st}</div>'
                f'<div class="ft"><span class="mono">{commit}</span><span>{when}</span><span style="margin-left:auto" class="mono">{node}</span></div>'
                f'{U.state_timeline(timeline)}<div class="ft"><span class="note">{domain}</span><span style="margin-left:auto" class="note">{note}</span></div></a>')
    projects = f"""
    <section id="projects" class="panel" aria-labelledby="h-proj">
      <div class="ph"><h2 id="h-proj">專案 · 3 環境</h2><span class="sub">橫條＝近 24 次部署結果</span><a class="more" href="Projects.dc.html">全部 →</a></div>
      <div class="fleet">
        {pcard("shop", "production", U.status("ok", "成功"), "a3f9c1e", "02:57", "tw-01", "shop.example.tw", "oooooooofoooooooooooooro", "自動部署暫停：#418 待批准")}
        {pcard("wiki", "production", U.status("ok", "成功"), "7d02b4a", "1 天前", "jp-02", "wiki.example.tw", "oooooooooooooooooooooooo")}
        {pcard("shop", "staging", U.status("dng", "失敗"), "c91e0d7", "03:12", "lab-mini", "staging.shop.example.tw", "oooofooooooooofoooooffff", "migration 不相容")}
      </div>
    </section>"""

    main = (U.page_head("總覽", OBS, '<button type="button" class="btn pri">新增節點</button><button type="button" class="btn">部署專案</button>',
                        " · 自動更新每 10 秒") + tiles + fleet_cards + topology_panel + charts + hostmap_panel + projects + storage)
    aside = attention_aside() + jobs_aside()
    return U.shell("overview", main, aside)


def attention_aside() -> str:
    def card(tag, color, title, evidence, pri, sec):
        return (f'<article class="card"><div style="font-size: 12px; color: {color}; font-weight: 600">{tag}</div>'
                f'<p style="font-size: 14px; font-weight: 500; line-height: 1.45">{title}</p>'
                f'<p class="note" style="color: var(--muted)">{evidence}</p>'
                f'<div class="wrap"><button type="button" class="btn pri sm" style="color: var(--hazelink)">{pri}</button><button type="button" class="btn sm">{sec}</button></div></article>')
    return f"""
    <section id="attention" aria-labelledby="h-att" style="display: flex; flex-direction: column; gap: 10px">
      <h2 id="h-att" style="font-size: 13px; color: var(--muted); letter-spacing: .04em">需要處理 · 3</h2>
      {card("待批准 · 變更計畫 #418", "var(--hazel)", "新 commit 新增 root 掛載，shop production 的自動部署已暫停", "證據 compose.yaml:41 <span class='mono'>/:/host:ro</span>。舊版 a3f9c1e 繼續服務。", "檢視差異", "拒絕")}
      {card("憑證 11 天後到期", "var(--warn)", "shop.example.tw 續期失敗：DNS 驗證逾時", "目前憑證仍有效。下一步：檢查 DNS 供應商 Token 或改用 HTTP 驗證。", "重試續期", "工作紀錄")}
      {card("磁碟 93% 已用", "var(--danger)", "hazel-jp-02 資料碟剩 61 GiB，且未綁定裝置身分", "節點已過期，最後回報 03:41:18。下一步：清理或搬移資料，綁定 UUID。", "查看儲存", "靜音 24h")}
    </section>"""


def jobs_aside() -> str:
    return f"""
    <section id="jobs" aria-labelledby="h-jobs" style="display: flex; flex-direction: column; gap: 10px">
      <h2 id="h-jobs" style="font-size: 13px; color: var(--muted); letter-spacing: .04em">進行中的工作 · 3 <a href="WorkCenter.dc.html" style="color: var(--hazel); font-weight: 400; margin-left: 6px">工作中心 →</a></h2>
      <ol class="list" style="list-style: none; margin: 0; padding: 0">
        <li class="job"><div class="t"><span style="font-weight: 500">部署 shop-api → production</span>{U.status("info", "驗證中")}</div><div class="s">tw-01 · 健康檢查 2／3 · 心跳 2 秒前</div><div class="prog" aria-hidden="true"><i style="width: 66%"></i></div><button type="button" class="btn sm" style="align-self: flex-start">要求取消</button></li>
        <li class="job"><div class="t"><span style="font-weight: 500">備份 wiki-db → S3</span>{U.status("info", "執行中")}</div><div class="s">tw-01 · 1.8／4.6 GiB · 此步不能取消（dump 中）</div><div class="prog" aria-hidden="true"><i style="width: 39%"></i></div></li>
        <li class="job"><div class="t"><span style="font-weight: 500">更新套件 openssl</span>{U.status("warn", "狀態不明")}</div><div class="s">jp-02 · 最後心跳 03:41:18 · 重連後對帳，不自動重跑</div></li>
      </ol>
    </section>"""


def range_row(on="24 小時") -> str:
    items = ["即時（5 分）", "1 小時", "12 小時", "24 小時", "7 天", "30 天"]
    btns = "".join(f'<button type="button" class="btn{" on" if i == on else ""}" aria-pressed="{"true" if i == on else "false"}">{i}</button>' for i in items)
    return f'<div class="wrap"><div class="range" role="group" aria-label="時間範圍">{btns}</div><span class="note" style="margin-left:auto">保留 30 天 · 每 10 秒取樣，24 小時以上顯示每小時平均與最高</span></div>'


# ---------- 節點詳情 ----------
def node_detail() -> str:
    head = f"""
    <div style="display: flex; flex-wrap: wrap; align-items: center; gap: 14px">
      <div class="name"><a href="Overview.dc.html" class="note">主機與節點 /</a><h1 class="mono">hazel-tw-01</h1></div>
      {U.status("ok", "連線中")}<span class="note">最後聯絡 2 秒前</span>
      <div class="wrap" style="margin-left: auto"><button type="button" class="btn">終端機</button><button type="button" class="btn">進入維護模式</button><button type="button" class="btn">重新啟動 agent</button><button type="button" class="btn pri">部署到此節點</button></div>
    </div>
    <div class="panel"><div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(min(150px, 100%), 1fr)); gap: 10px 16px; padding: 12px 14px; font-size: 13px">
      {kv("作業系統", "Ubuntu 24.04.2 LTS")}{kv("核心", "6.8.0-45-generic")}{kv("CPU", "8 核 · AMD EPYC 7B13")}{kv("記憶體", "31.3 GiB")}{kv("執行階段", "Docker 27.3.1")}{kv("Agent", "hazelminx-agent 0.1.0 · mTLS")}{kv("角色", "控制中心 · 資料節點")}{kv("GPU", "NVIDIA L4 · 24 GiB · 驅動 550.90 · NVML")}
    </div></div>"""
    rings = f"""
    <div class="panel"><div style="display: flex; flex-wrap: wrap; gap: 18px 28px; padding: 12px 14px; align-items: flex-end">
      {U.ring(37, "37%", "CPU · 8 核", size=72)}{U.ring(11.2 / 31.3 * 100, "11G", "記憶體 /31G", size=72)}{U.ring(44, "412G", "/mnt/data /931G", size=72, warn=80, danger=90)}{U.ring(31, "29G", "/ /94G", size=72, warn=80, danger=90)}{U.ring(44, "44%", "GPU 核心", size=72)}{U.ring(14.2 / 24 * 100, "14G", "顯示記憶體 /24G", size=72)}{U.ring(51 / 90 * 100, "51°", "GPU 溫度 /83°", size=72, warn=83 / 90 * 100, danger=95)}
      <div style="display:flex;flex-direction:column;gap:4px;margin-left:auto;min-width:160px"><span class="note">負載 1／5／15 分</span><span class="mono" style="font-size:20px;font-weight:600">2.1 · 2.4 · 2.0</span><span class="note">8 核，超過 8 代表排隊</span></div>
      <div style="display:flex;flex-direction:column;gap:4px;min-width:160px"><span class="note">容器</span>{U.segmented([(9, "var(--ok)"), (1, "var(--danger)")])}<span class="note">9 執行中 · 1 異常（shop-redis 重啟中）</span></div>
    </div></div>"""
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
      <div class="row hd drow"><span>掛載點</span><span>裝置</span><span class="c3">檔案系統</span><span>使用</span><span class="c4">裝置身分</span></div>
      <div class="row drow"><span class="mono">/</span><span class="mono">nvme0n1p2</span><span class="c3">ext4</span>{U.meter(31, "29 / 94 GiB", warn=80, danger=90)}<span class="c4">{U.status("ok", "已綁定")}</span></div>
      <div class="row drow"><span class="mono">/mnt/data</span><span class="mono">sda1 → /dev/disk/by-uuid/5a7c…e2</span><span class="c3">xfs</span>{U.meter(44, "412 / 931 GiB", warn=80, danger=90)}<span class="c4">{U.status("ok", "已綁定")}</span></div>
      <div class="row drow"><span class="mono">/boot/efi</span><span class="mono">nvme0n1p1</span><span class="c3">vfat</span>{U.meter(12, "61 / 511 MiB", warn=80, danger=90)}<span class="c4"><span class="note">不適用</span></span></div>
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
      <div class="ph"><h2 id="h-gpu">GPU · NVIDIA L4</h2><span class="sub">24 GiB · 驅動 550.90 · 透過 NVML 每 10 秒取樣</span><span class="note" style="margin-left:auto">欄位依驅動能力顯示，缺值寫「不提供」，不跨廠牌假造</span></div>
      <div class="tiles" style="padding: 12px 14px">
        {U.stat_tile("使用率", "44", "%", "1 分鐘前 91%", None, spark=GPU_UTIL)}
        {U.stat_tile("顯示記憶體", "14.2", "/ 24 GiB", "ollama 佔 13.1 GiB", None, spark=GPU_VRAM)}
        {U.stat_tile("溫度", "51", "°C", "門檻 83°C", None, spark=GPU_TEMP)}
        {U.stat_tile("功耗", "38", "/ 72 W", "", None, spark=GPU_POWER)}
      </div>
    </section>"""
    gpu_proc = f"""
    <section class="panel" aria-labelledby="h-gproc">
      <div class="ph"><h2 id="h-gproc">GPU 程序 · 現在</h2><span class="sub">NVML 程序清單，對回容器名稱</span></div>
      <div class="row hd gprow"><span>程序</span><span>容器</span><span class="c3">類型</span><span>顯示記憶體</span></div>
      <div class="row gprow"><span class="mono">ollama runner · pid 41822</span><span class="mono">ollama</span><span class="c3">運算</span><span>{U.meter(13.1 / 24 * 100, "13.1 GiB", aria="ollama 顯示記憶體")}</span></div>
      <div class="row gprow"><span class="mono">ffmpeg · pid 52107</span><span class="mono">wiki-media</span><span class="c3">解碼</span><span>{U.meter(0.9 / 24 * 100, "0.9 GiB", aria="wiki-media 顯示記憶體")}</span></div>
      <div class="row gprow"><span class="mono">Xorg · pid 1180</span><span class="note">主機程序，非容器</span><span class="c3">繪圖</span><span>{U.meter(0.2 / 24 * 100, "0.2 GiB", aria="Xorg 顯示記憶體")}</span></div>
      <div class="note" style="padding: 8px 14px 10px">每個程序的核心使用率：此驅動不提供，只有整卡數字 · AMD／Intel 卡沒有程序清單時，這個區塊會寫「驅動不提供」而不是空白</div>
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
    main = (head + rings + range_row() + f'<div class="cg">{cpu}{mem}{io}{net}{load}{temp}</div><div class="cg">{disks}{hot}</div>'
            + gpu_head + f'<div class="cg">{gpu_util}{gpu_vram}{gpu_power}</div><div class="cg">{gpu_proc}{gpu_auth}</div>')
    return U.shell("nodes", main)


# ---------- 專案與服務 ----------
def projects() -> str:
    head = U.page_head("專案與服務", OBS, '<button type="button" class="btn pri">新增專案</button><button type="button" class="btn">從 GitHub 匯入</button>')
    tabs = ('<div class="panel"><div class="tabs" role="tablist"><button type="button" class="tab on" role="tab" aria-selected="true">shop · production</button>'
            '<button type="button" class="tab" role="tab" aria-selected="false">shop · staging</button><button type="button" class="tab" role="tab" aria-selected="false">wiki · production</button></div>'
            f'<div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(min(170px, 100%), 1fr)); gap: 10px 16px; padding: 12px 14px; font-size: 13px">'
            f'{kv("來源", "github.com/hazelnut/shop · main")}{kv("最近部署", "a3f9c1e · 02:57 · 成功 · Webhook")}{kv("節點", "hazel-tw-01")}{kv("入口", "shop.example.tw · 憑證 11 天後到期")}{kv("自動部署", "已暫停：變更計畫 #418 待批准")}</div></div>')
    svc_topo = U.topology([
        ("入口", [("dom", "shop.example.tw", "Zentinel · 憑證 11 天", "warn")]),
        ("服務", [("api", "shop-api", "8080 · 2 副本", "ok"), ("wrk", "shop-worker", "無對外 Port", "ok")]),
        ("資料", [("db", "shop-db", "postgres 16.4 · 1.29G", "ok"), ("rd", "shop-redis", "重啟中 · 第 1 次", "info")]),
        ("儲存", [("vol", "shop-db-data", "/mnt/data · 84G", "ok"), ("bk", "每日備份 → S3", "03:00 · 成功", "ok")]),
    ], [("dom", "api", .9, ""), ("api", "db", .6, ""), ("api", "rd", .4, ""), ("wrk", "db", .3, ""), ("wrk", "rd", .3, ""), ("db", "vol", .6, ""), ("vol", "bk", .3, "dim")], w=1140, box_w=170)
    svc_topo_m = U.flowlist([("shop.example.tw", "shop-api", "Zentinel"), ("shop-api", "shop-db", "postgres"), ("shop-api", "shop-redis", "重啟中"), ("shop-worker", "shop-db", ""), ("shop-db", "shop-db-data", "84G"), ("shop-db-data", "每日備份 → S3", "03:00")])
    topo_panel = f"""
    <section class="panel" aria-labelledby="h-stopo">
      <div class="ph"><h2 id="h-stopo">shop · production 的結構</h2><span class="sub">來自 compose.yaml：入口 → 服務 → 資料 → 儲存與備份；線寬＝呼叫量</span><a class="more" href="#compose">原始 Compose →</a></div>
      <div style="padding: 10px 14px 6px"><div class="ch-d">{svc_topo}</div><div class="ch-m">{svc_topo_m}</div></div>
    </section>"""
    deploys = f"""
    <section class="panel" aria-labelledby="h-dep">
      <div class="ph"><h2 id="h-dep">部署歷史 · 近 24 次</h2><div class="legend" style="margin-left:0"><span><i class="rect" style="background: var(--ok)"></i>成功</span><span><i class="rect" style="background: var(--danger)"></i>失敗</span><span><i class="rect" style="background: var(--warn)"></i>回滾</span><span><i class="rect" style="background: var(--blue)"></i>進行中</span></div><a class="more" href="WorkCenter.dc.html">工作中心 →</a></div>
      <div style="padding: 12px 14px; display: flex; flex-direction: column; gap: 8px">{U.state_timeline("oooooooofoooooooooooooro" , [f"#{380 + i}" for i in range(24)])}<div class="note" style="display:flex;justify-content:space-between"><span>#380 · 10 月 21 日</span><span>#403 · a3f9c1e · 02:57 今天</span></div></div>
      <div class="row hd dprow"><span>部署</span><span>commit</span><span class="c3">觸發</span><span class="c4">時長</span><span>結果</span></div>
      <div class="row dprow"><span>#403</span><span class="mono">a3f9c1e 修正結帳逾時</span><span class="c3">Webhook</span><span class="mono c4">1 分 48 秒</span><span>{U.status("ok", "成功")} <span class="note">02:57</span></span></div>
      <div class="row dprow"><span>#402</span><span class="mono">88b1d02 升級 redis 7.4</span><span class="c3">手動 · tim</span><span class="mono c4">3 分 12 秒</span><span>{U.status("warn", "回滾")} <span class="note">昨天 21:10 · 健康檢查失敗</span></span></div>
      <div class="row dprow"><span>#401</span><span class="mono">4e7a9c0 新增 webhook 重試</span><span class="c3">Webhook</span><span class="mono c4">1 分 51 秒</span><span>{U.status("ok", "成功")} <span class="note">昨天 18:02</span></span></div>
    </section>"""

    def svc(name, image, node, port, cpu, mem, rps, err, spark, st):
        return (f'<a class="row srow" href="Containers.dc.html"><span class="name"><span class="n">{name}</span><span class="d">{image}</span></span><span>{st}</span>'
                f'<span class="mono c3">{node}<br><span class="note">{port}</span></span><span class="c4">{U.meter(cpu, f"{cpu:.0f}%")}</span><span class="c4">{U.meter(mem[0] / mem[1] * 100, f"{mem[0]}/{mem[1]}M")}</span>'
                f'<span class="c5">{U.sparkline(spark)}</span><span class="mono">{rps}<br><span class="note" style="color:{"var(--danger)" if err >= 1 else "var(--faint)"}">錯誤 {err:.2f}%</span></span></a>')
    services = f"""
    <section class="panel" aria-labelledby="h-svc">
      <div class="ph"><h2 id="h-svc">服務 · 4</h2><span class="sub">來自 compose.yaml，顯示實際節點與 Port，不只名稱</span></div>
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
    main = head + tabs + topo_panel + services + f'<div class="cg">{rq_card}{er_card}</div>{lat_card}' + deploys
    return U.shell("projects", main)


# ---------- 容器 ----------
def containers() -> str:
    head = U.page_head("Docker／Podman", OBS, '<button type="button" class="btn pri">建立容器</button><button type="button" class="btn">拉取映像</button><button type="button" class="btn">清理未使用</button>')
    tiles = '<div class="tiles">' + "".join([
        U.stat_tile("執行中", "14", "/16", "1 重啟中、1 已停止", None),
        U.stat_tile("映像", "23", "", "7 個未使用 · 4.1 GiB 可回收", None),
        U.stat_tile("容器 CPU 合計", "41", "% / 8 核", "tw-01", None, spark=[sum(h[i] for h in HEAT) / 8 for i in range(12)]),
        U.stat_tile("容器記憶體合計", "9.2", "GiB / 31.3", "", None, spark=walk(12, 9, .4, 8, 10)),
    ]) + "</div>"
    filt = ('<div class="wrap"><label class="inp" style="width: auto; min-width: 220px"><span class="note">節點</span><select style="background:transparent;border:0;flex:1"><option>全部（3）</option><option>hazel-tw-01</option></select></label>'
            '<label class="inp" style="width: auto; min-width: 200px"><span class="note">狀態</span><select style="background:transparent;border:0;flex:1"><option>全部</option><option>執行中</option></select></label>'
            '<label class="inp" style="flex: 1; min-width: 200px"><span class="note">搜尋</span><input placeholder="名稱、映像、標籤" style="background:transparent;border:0;flex:1;min-width:0"></label></div>')

    def crow(name, img, node, st, cpu, mem, memt, net, restarts, ports):
        return (f'<a class="row crow" href="#c-{name}"><span class="name"><span class="n">{name}</span><span class="d">{img}</span></span><span>{st}</span>'
                f'<span class="mono c3">{node}</span><span class="c4">{U.meter(cpu, f"{cpu:.1f}%")}</span><span class="c4">{U.meter(mem / memt * 100, f"{mem}/{memt}M")}</span>'
                f'<span class="mono c5">{net}</span><span class="mono c5" style="color:{"var(--warn)" if restarts else "var(--faint)"}">{restarts}</span><span class="mono c5 note">{ports}</span></a>')
    table = f"""
    <section class="panel" aria-labelledby="h-ct">
      <div class="ph"><h2 id="h-ct">容器 · 16</h2><span class="sub">顯示 8，依 CPU 排序</span><a class="more" href="#all">全部 →</a></div>
      <div class="scroll"><div style="min-width: 320px">
      <div class="row hd crow"><span>容器</span><span>狀態</span><span class="c3">節點</span><span class="c4">CPU</span><span class="c4">記憶體</span><span class="c5">網路 I/O</span><span class="c5">重啟</span><span class="c5">Port</span></div>
      {crow("shop-api", "ghcr.io/hazelnut/shop-api:a3f9c1e", "tw-01", U.status("ok", "執行中 · 46 分"), 21.4, 812, 2048, "3.1 / 11.8 MB/s", 0, "8080")}
      {crow("shop-worker", "ghcr.io/hazelnut/shop-worker:a3f9c1e", "tw-01", U.status("ok", "執行中 · 46 分"), 8.9, 410, 1024, "0.4 / 0.2", 0, "—")}
      {crow("wiki-app", "ghcr.io/hazelnut/wiki:7d02b4a", "jp-02", U.status("warn", "過期 · 03:41"), 3.1, 356, 1024, "0.6 / 1.9*", 0, "3000")}
      {crow("shop-db", "postgres:16.4", "tw-01", U.status("ok", "執行中 · 9 天"), 2.6, 1290, 4096, "0.3 / 0.5", 0, "5432 內部")}
      {crow("zentinel", "原生 systemd（非容器）", "tw-01", U.status("ok", "執行中 · 12 天"), 1.8, 94, 512, "11.9 / 12.1", 0, "80, 443")}
      {crow("wiki-db", "postgres:16.4", "jp-02", U.status("warn", "過期 · 03:41"), 0.9, 540, 2048, "0.1 / 0.1*", 0, "5432 內部")}
      {crow("shop-redis", "redis:7.2", "tw-01", U.status("info", "重啟中 · 第 1 次"), 0.8, 62, 256, "—", 1, "6379 內部")}
      {crow("hazelminx-agent", "原生 systemd（非容器）", "tw-01", U.status("ok", "執行中 · 12 天"), 0.4, 38, 128, "0.1 / 0.1", 0, "→ Core 8443")}
      </div></div>
      <div class="note" style="padding: 8px 14px; border-top: 1px solid var(--line)">* 上次觀測 · 原生 systemd 服務列在同一張表，讓「什麼在哪台、用哪個 Port」一眼看完</div>
    </section>"""
    hours12 = [f"{(16 + i) % 24:02d}" for i in range(12)]
    heat = f"""
    <section class="panel" aria-labelledby="h-heat">
      <div class="ph"><h2 id="h-heat">容器 × 小時 CPU · 近 12 小時</h2><span class="sub">單色深淺：掃出哪個容器、哪個時段熱</span><div class="legend"><span><i class="rect" style="background: var(--q1)"></i>0</span><span><i class="rect" style="background: var(--q3)"></i>50%</span><span><i class="rect" style="background: var(--q5)"></i>100%</span></div></div>
      <div style="padding: 12px 14px">{U.heatgrid(CONTAINERS, hours12, HEAT)}</div>
      <div class="foot"><span>格 = 該小時平均，100% = 一核全忙</span><span>shop-api 02 時 88%：對應部署 #403 的暖機</span><a href="#table">表格檢視</a></div>
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
    main = head + tiles + cmap_panel + filt + table + f'<div class="cg">{heat}{hot}</div>'
    return U.shell("docker", main)


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
             [("拉取 commit", "done", "12 秒", "9b3e7f1"), ("預檢", "done", "18 秒", "磁碟、Port、憑證"), ("批准", "done", "自動", "無高風險變更"), ("建置", "done", "1 分 02 秒", "ghcr.io 推送完成"), ("滾動替換", "done", "40 秒", "舊版仍在服務"), ("驗證", "cur", "進行中", "健康檢查 2／3")],
             "第 3 個健康檢查：<span class='mono'>GET /healthz</span> 連續 3 次 200 才算過，目前 2 次。失敗會自動回滾到 a3f9c1e。",
             '<button type="button" class="btn sm">要求取消（回滾到 a3f9c1e）</button><button type="button" class="btn sm">看日誌</button>', pct=66)}
        {job("備份 wiki-db → S3 tokyo-backups", "hazel-tw-01 · 計畫任務 03:00 · 心跳 1 秒前", U.status("info", "執行中"),
             [("預檢", "done", "空間足夠"), ("dump", "cur", "此步不能取消"), ("傳輸", "todo", "1.8／4.6 GiB 已排程"), ("校驗", "todo", "sha256 + 還原測試")],
             "速率 <span class='mono'>38 MB/s</span>，預估 1 分 14 秒後開始傳輸。不能取消的原因：中斷 dump 會留下不一致的快照。",
             '<button type="button" class="btn sm" disabled aria-disabled="true" style="opacity:.6">此步不能取消</button><button type="button" class="btn sm">看日誌</button>', pct=39)}
        {job("更新套件 openssl（安全性）", "hazel-jp-02 · 由 tim 手動觸發 03:38:50 · 最後心跳 03:41:18", U.status("warn", "狀態不明"),
             [("預檢", "done", "7 秒"), ("批准", "done", "tim"), ("執行", "wait", "apt 進行到一半"), ("驗證", "todo", "")],
             "節點 48 秒沒心跳。不自動重跑：重連後先對帳 apt 的實際狀態，再決定續跑或標記失敗。",
             '<button type="button" class="btn sm">查看節點</button><button type="button" class="btn sm">標記為需人工處理</button>', pulse=True)}
      </div>
    </section>"""
    waiting = f"""
    <section class="panel" aria-labelledby="h-wait">
      <div class="ph"><h2 id="h-wait">等待批准 · 1</h2></div>
      <div class="list" style="padding: 12px 14px">
        {job("變更計畫 #418 · shop production · commit 9b3e7f1", "hazel-tw-01 · 由 Webhook 偵測 02:58:30 · 自動部署已暫停", U.status("warn", "等待批准"),
             [("預檢", "done", "發現 1 個高風險變更"), ("批准", "wait", "需要擁有者"), ("執行", "todo", ""), ("驗證", "todo", "")],
             "高風險：compose.yaml:41 新增 <span class='mono'>/:/host:ro</span>。批准後會重建 shop-api、shop-worker 兩個服務，預估中斷 0 秒（滾動）。",
             '<a class="btn pri sm" href="DeployCheck.dc.html" style="color: var(--hazelink)">檢視差異與批准</a><button type="button" class="btn sm">拒絕</button>')}
      </div>
    </section>"""
    rate = U.ts_card("備份傳輸速率 · wiki-db", [U.Series("MB/s", walk(24, 36, 6, 0, 60, {0: 0, 1: 12}), "var(--hazel)", kind="area")],
                     [f"{i * 5:02d}s" for i in range(24)], unit=" MB/s", vmax=60, fmt_val=lambda v: f"{v:.0f}", foot="近 2 分鐘，每 5 秒一點 · 總量已知（4.6 GiB）才顯示百分比")
    hist = f"""
    <section class="panel" aria-labelledby="h-hist">
      <div class="ph"><h2 id="h-hist">近 24 小時 · 27 件</h2><div class="legend" style="margin-left:0"><span><i class="rect" style="background: var(--ok)"></i>成功</span><span><i class="rect" style="background: var(--danger)"></i>失敗</span><span><i class="rect" style="background: var(--warn)"></i>回滾</span></div><a class="more" href="#history">全部 →</a></div>
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

    def repo(name, desc, branch, updated, cur=False, compose=True):
        mark = U.status("ok", "有 compose.yaml") if compose else U.status("off", "未偵測到 compose")
        return (f'<label class="row rrow" style="cursor:pointer;{"background: var(--raised)" if cur else ""}"><input type="radio" name="repo" {"checked" if cur else ""} style="width:18px;height:18px;accent-color: var(--hazel)">'
                f'<span class="name"><span class="n">{name}</span><span class="d">{desc}</span></span><span class="c3">{mark}</span><span class="mono c4">{branch}</span><span class="mono note">{updated}</span></label>')
    left = f"""
    <section class="panel" aria-labelledby="h-repo">
      <div class="ph"><h2 id="h-repo">倉庫 · hazelnut（12）</h2><span class="sub">只列 GitHub App 被授權的倉庫</span><a class="more" href="https://github.com/apps/hazelminx">管理授權 ↗</a></div>
      <div style="padding: 10px 14px"><label class="inp"><span class="note">搜尋</span><input value="shop" style="background:transparent;border:0;flex:1;min-width:0" aria-label="搜尋倉庫"></label></div>
      <div class="row hd rrow"><span></span><span>倉庫</span><span class="c3">偵測</span><span class="c4">預設分支</span><span>更新</span></div>
      {repo("hazelnut/shop", "電商主站 · 私有 · 4 個 compose 服務", "main", "2 小時前", cur=True)}
      {repo("hazelnut/shop-mobile-api", "行動 API · 私有", "main", "3 天前")}
      {repo("hazelnut/shop-legacy", "舊版 PHP · 封存", "master", "1 年前", compose=False)}
      <div class="note" style="padding: 8px 14px; border-top: 1px solid var(--line)">找不到倉庫？GitHub App 可能沒被授權該倉庫，到「管理授權」加上。</div>
    </section>"""
    right = f"""
    <section class="panel" aria-labelledby="h-br">
      <div class="ph"><h2 id="h-br">hazelnut/shop</h2><span class="sub">分支、路徑與觸發</span></div>
      <div style="padding: 12px 14px; display: flex; flex-direction: column; gap: 14px">
        <div class="field"><label for="br">分支或標籤</label><div class="inp"><span class="mono">main</span><span class="note" style="margin-left:auto">最新 9b3e7f1 · 38 分鐘前</span></div><span class="help">之後每次推送到這個分支都會建立變更計畫；高風險變更先暫停等批准。</span></div>
        <div class="field"><label for="path">compose 檔路徑</label><div class="inp"><span class="mono">deploy/compose.yaml</span>{U.status("ok", "已找到")}</div><span class="help">也偵測到 <span class="mono">compose.override.yaml</span>，部署時不會套用，需要的話在下一步勾選。</span></div>
        <div class="field"><label>觸發方式</label><div class="wrap"><label class="btn" style="gap:8px"><input type="radio" name="trig" checked style="accent-color: var(--hazel)">Webhook（推送即部署）</label><label class="btn" style="gap:8px"><input type="radio" name="trig" style="accent-color: var(--hazel)">定期檢查（每 5 分鐘）</label><label class="btn" style="gap:8px"><input type="radio" name="trig" style="accent-color: var(--hazel)">只手動</label></div><span class="help">Webhook 會在 GitHub 上建立；沒有公開入口時自動改用定期檢查。</span></div>
        <div class="field"><label for="env">目標環境與節點</label><div class="wrap"><div class="inp" style="width:auto;min-width:160px"><span>production</span></div><div class="inp" style="width:auto;min-width:200px"><span class="mono">hazel-tw-01</span><span class="note" style="margin-left:auto">412 GiB 可用</span></div></div><span class="errt">hazel-jp-02 不可選：狀態過期且資料碟 93%。</span></div>
      </div>
      <div class="ph" style="border-top: 1px solid var(--line); border-bottom: 0"><span class="note">下一步會讀取 compose 檔，列出服務、Port、磁碟區與需要的環境變數，不會建立任何東西。</span><div class="wrap" style="margin-left:auto"><a class="btn" href="Projects.dc.html">上一步</a><a class="btn pri" href="DeployCheck.dc.html">偵測與設定 →</a></div></div>
    </section>"""
    main = head + f'<div class="panel" style="padding: 12px 14px">{stepper}</div><div class="cg">{left}{right}</div>'
    return U.shell("projects", main)


# ---------- 部署檢查 ----------
def deploy_check(confirm: bool = False) -> str:
    head = f"""
    <div style="display: flex; flex-wrap: wrap; align-items: center; gap: 14px">
      <div class="name"><a href="WorkCenter.dc.html" class="note">工作中心 / 等待批准 /</a><h1>變更計畫 #418 · shop production</h1></div>
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
      <div class="check"><span class="mk warn" aria-hidden="true"></span><span><strong>警告：映像標籤由 commit 短碼改成 latest</strong><br><span class="ev">compose.yaml:12 · shop-api · image: …/shop-api:latest</span><br><span class="note">影響：回滾時無法確定回到哪個版本。建議保留短碼標籤。</span></span><span class="note">可批准</span></div>
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
    main = head + summary + checks + diff + modal
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
        <div class="wrap"><button type="button" class="btn pri">新增節點</button><a class="btn" href="#docs">看安裝步驟</a></div>
      </div></section>"""
    error = f"""
    <section class="chart" aria-label="錯誤範例"><div class="ph"><h2>錯誤：四段式</h2><span class="sub">發生什麼、影響什麼、證據、下一步</span></div>
      <div class="body" style="padding:12px 14px"><article class="card" style="border-color: var(--danger)">
        <div style="font-size:12px;color:var(--danger);font-weight:600">部署失敗 · shop staging #71</div>
        <p style="font-size:14px;font-weight:500">資料庫 migration 與目前的 schema 不相容，部署已停止，舊版持續服務</p>
        <p class="note" style="color:var(--muted)">影響：staging.shop.example.tw 仍是 c0ffee1 版本；production 不受影響。</p>
        <p class="note">證據：<span class="mono">migrate: relation "orders_v2" already exists（0004_orders.sql:12）</span> · lab-mini · 03:12:40</p>
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
        <div class="banner" style="border-radius:0;border-left:0;border-right:0;border-top:0">{U.status("off", "離線 · 12 秒")}<span>畫面是 03:41:54 的最後狀態。離線不等於服務已停止；工作在控制中心上繼續跑。</span><button type="button" class="btn sm" style="margin-left:auto">重試連線</button></div>
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
        <h2 style="font-size:16px">刪除資料碟 <span class="mono">hazel-jp-02 /mnt/data</span>（sda1）？</h2>
        <p style="font-size:13px;color:var(--muted)">會解除掛載並清除 870 GiB 資料，包含 <span class="mono">wiki-db</span> 的資料目錄與 2 個備份暫存。最近的備份 wiki-db 校驗通過但尚未還原測試。</p>
        <div class="field"><label for="dd">輸入磁碟名稱 <span class="mono">sda1</span> 以確認</label><div class="inp err"><input id="dd" value="sda2" style="background:transparent;border:0;flex:1" aria-invalid="true" aria-describedby="dd-err"></div><span class="errt" id="dd-err">名稱不符：請輸入 sda1。</span></div>
        <div class="wrap" style="justify-content:flex-end"><button type="button" class="btn">取消</button><button type="button" class="btn dng" disabled aria-disabled="true" style="opacity:.6">刪除資料碟</button></div>
      </div></div></section>"""
    main = head + f'<div class="cg">{loading}{empty}{error}{partial}{offline}{nodata}</div>{danger}'
    return U.shell("overview", main)



# ---------- 網站與入口 ----------
def websites() -> str:
    head = U.page_head("網站與入口", OBS, '<button type="button" class="btn pri">新增站點</button><button type="button" class="btn">申請憑證</button><button type="button" class="btn">Zentinel 設定</button>')
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
        ("shop", "api", .64, ""), ("wiki", "wapp", .31, ""), ("stg", "sapi", .05, "danger")], w=1140, box_w=176)
    topo_m = U.flowlist([("Cloudflare", "Zentinel @ tw-01", "70%"), ("Cloudflare", "Zentinel @ jp-02", "30%"), ("Zentinel @ tw-01", "shop.example.tw", "64%"), ("Zentinel @ jp-02", "wiki.example.tw", "31%"), ("Zentinel @ tw-01", "staging.shop", "502"), ("shop.example.tw", "shop-api:8080", "2 副本"), ("staging.shop", "shop-api:8080", "無健康副本")])
    topo_panel = f"""
    <section class="panel" aria-labelledby="h-wt">
      <div class="ph"><h2 id="h-wt">入口怎麼接</h2><span class="sub">線寬＝今日請求佔比；上游用 compose 服務名與 Port，不是容器 ID</span></div>
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
        {cert("hazel.example.tw（管理入口）", 83, "自簽 · 內部 CA", "手動")}
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
      <div class="ph"><h2 id="h-ip">訪客 IP 怎麼判定 · 今日</h2><span class="sub">只有可信入口的 Header 會被採用；偽造 Header 的直接連線不能指定別人的 IP</span></div>
      <div style="padding: 12px 14px; display:flex; flex-direction:column; gap:8px">
        {U.tall_bar([(98.7, "Cloudflare Tunnel（CF-Connecting-IP）90,807", "var(--q5)"), (1.1, "直接連線（來源 IP）1,012", "var(--q3)"), (0.2, "不可信 Header（已忽略）182", "var(--danger)")], 100, unit="%")}
        <div class="scale"><i style="background:var(--q5)"></i>Tunnel 98.7% <i style="background:var(--q3)"></i>直接連線 1.1% <i style="background:var(--danger)"></i>不可信 Header 0.2%（限速與封鎖用真實來源 IP）</div>
      </div>
    </section>"""

    def site(domain, st, node, upstream, certd, wafm, spark, note=""):
        return (f'<a class="row srow" href="#site"><span class="name"><span class="n">{domain}</span><span class="d">{note}</span></span><span>{st}</span>'
                f'<span class="mono c3">{node}<br><span class="note">{upstream}</span></span><span class="c4 mono">{certd}</span><span class="c4">{wafm}</span><span class="c5">{U.sparkline(spark)}</span><span class="mono">{U.fmt(sum(spark) * 60)}</span></a>')
    table = f"""
    <section class="panel" aria-labelledby="h-sites">
      <div class="ph"><h2 id="h-sites">站點 · 3</h2><span class="sub">同一個站點在容器頁、備份頁是同一份資料</span></div>
      <div class="row hd srow"><span>網域</span><span>狀態</span><span class="c3">入口 · 上游</span><span class="c4">憑證</span><span class="c4">WAF</span><span class="c5">請求 1 小時</span><span>今日</span></div>
      {site("shop.example.tw", U.status("warn", "憑證 11 天"), "tw-01", "shop-api:8080", "11 天", "偵測", walk(12, 980, 120, 600, 1300), "production · HTTP/2 · HSTS")}
      {site("wiki.example.tw", U.status("ok", "正常"), "jp-02", "wiki-app:3000", "71 天", "偵測", walk(12, 470, 60, 300, 650), "production · 狀態過期的節點")}
      {site("staging.shop.example.tw", U.status("dng", "502 上游"), "tw-01", "shop-api:8080（staging）", "44 天", "偵測", walk(12, 80, 30, 10, 140), "staging · 部署失敗後無健康副本")}
    </section>"""
    main = head + tiles + topo_panel + f'<div class="cg">{certs}{waf}</div>' + f'<div class="cg">{rules}{ipsrc}</div>' + table
    return U.shell("web", main)


# ---------- 儲存與備份 ----------
def storage() -> str:
    head = U.page_head("儲存與備份", OBS, '<button type="button" class="btn pri">新增備份任務</button><button type="button" class="btn">綁定裝置身分</button><button type="button" class="btn">清理可回收</button>')
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
        {disk("hazel-tw-01", "/mnt/data", "sda1 → by-uuid/5a7c…e2 · xfs", U.status("ok", "已綁定 UUID"), [(290, "容器資料", "var(--q5)"), (84, "資料庫", "var(--q4)"), (38, "備份暫存", "var(--q3)"), (12, "可回收映像", "var(--q2)")], 931, walk(12, 390, 8, 370, 412), "412 / 931 GiB · +38 GiB · 預估 410 天後滿")}
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
                f'<span class="note">{sched} → {dest}</span>{U.calendar(states)}<span class="note">{last}</span></div>')
    backups = f"""
    <section class="panel" aria-labelledby="h-bk">
      <div class="ph"><h2 id="h-bk">備份任務 · 近 14 週</h2><span class="sub">一格一天：●成功 ■失敗 ○部分；灰＝未排程</span><div class="scale" style="margin-left:auto"><i style="background:var(--ok)"></i>成功 <i style="background:var(--danger)"></i>失敗 <i style="background:var(--warn)"></i>部分 <i style="background:var(--q1)"></i>未排</div></div>
      <div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(min(300px,100%),1fr));gap:1px;background:var(--line)">
        {bjob("wiki-db 每日", "每天 03:00", "S3 tokyo-backups", "o" * 40 + "f" + "o" * 20 + "r" + "o" * 33 + "e" * 2 + "n" * 2, "今天 03:00 · 4.6 GiB · 已校驗 · 還原測試 9 天前通過", U.status("ok", "正常"))}
        {bjob("shop-db 每日", "每天 03:30", "S3 tokyo-backups", "o" * 61 + "f" + "o" * 33 + "e" * 2 + "n" * 2, "今天 03:30 · 1.3 GiB · 已校驗 · 還原測試從未執行", U.status("warn", "未做還原測試"))}
        {bjob("平台快照 每週", "每週日 04:00", "本機 /mnt/backup + S3", "e" * 6 + ("o" + "e" * 6) * 13 + "n" * 2, "10 月 26 日 · 212 MiB · 含設定、機密包、資料庫", U.status("ok", "正常"))}
      </div>
    </section>"""
    verify = f"""
    <section class="panel" aria-labelledby="h-ver">
      <div class="ph"><h2 id="h-ver">今天的 wiki-db 備份走到哪</h2><span class="sub">每一步都有證據；沒做還原測試不算「可還原」</span></div>
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
    main = head + tiles + disks + f'<div class="cg">{vols}{io}</div>' + backups + f'<div class="cg">{verify}{dests}</div>'
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
      <div class="ph"><h2 id="h-rl">告警規則 · 近 24 小時</h2><span class="sub">橫條每格一小時：綠＝正常、紅＝觸發、黃＝靜音、藍＝評估中、灰＝沒資料</span></div>
      <div class="row hd" style="grid-template-columns:minmax(0,1.3fr) minmax(0,2.2fr) minmax(0,1fr)"><span>規則</span><span>24 小時</span><span>現在</span></div>
      {rule("節點失聯 > 60 秒", "o" * 23 + "f", U.status("dng", "觸發 · jp-02"), "任何節點 · 嚴重")}
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
      <div class="foot"><span>02 時的集中：jp-02 失聯連鎖（節點、zentinel、wiki-app 同時出事件）</span><a href="#table">表格檢視</a></div>
    </section>"""
    top = f"""
    <section class="panel" aria-labelledby="h-top">
      <div class="ph"><h2 id="h-top">事件最多的來源 · 24 小時</h2></div>
      {U.ranked([("hazel-jp-02", 41, "41"), ("shop-api（staging）", 27, "27"), ("zentinel @ tw-01", 22, "22"), ("hazel-tw-01", 18, "18"), ("shop-api", 15, "15"), ("wiki-app", 9, "9"), ("備份", 6, "6"), ("平台", 4, "4")], vmax=45)}
    </section>"""
    route = U.topology([
        ("告警", [("a1", "節點失聯", "嚴重", "danger"), ("a2", "資料碟 > 90%", "嚴重 · 靜音中", "warn"), ("a3", "憑證 < 14 天", "警告", "warn")]),
        ("路由", [("r1", "嚴重 → 立刻", "不分時段", "ok"), ("r2", "警告 → 工作時段", "09–22 · 其餘彙整", "ok")]),
        ("管道", [("c1", "Telegram · 維運群", "47/48 送達", "ok"), ("c2", "Email · tim", "備援", "ok"), ("c3", "Webhook · 值班系統", "未設定", "dim")]),
    ], [("a1", "r1", .9, ""), ("a2", "r1", .4, "dim"), ("a3", "r2", .5, ""), ("r1", "c1", .9, ""), ("r1", "c2", .3, ""), ("r2", "c1", .5, ""), ("r2", "c3", .1, "dim")], w=1140, box_w=176)
    route_m = U.flowlist([("節點失聯", "嚴重 → 立刻", "Telegram、Email"), ("資料碟 > 90%", "嚴重 → 立刻", "靜音中"), ("憑證 < 14 天", "警告 → 工作時段", "Telegram")])
    route_panel = f"""
    <section class="panel" aria-labelledby="h-rt">
      <div class="ph"><h2 id="h-rt">告警往哪送</h2><span class="sub">送達＝供應商回覆已接受，不等於有人看到；測試訊息不帶正式機密</span><a class="more" href="#routes">編輯路由 →</a></div>
      <div style="padding: 10px 14px 6px"><div class="ch-d">{route}</div><div class="ch-m">{route_m}</div></div>
    </section>"""
    def card(tag, color, title, evidence, pri, sec):
        return (f'<article class="card"><div style="font-size: 12px; color: {color}; font-weight: 600">{tag}</div>'
                f'<p style="font-size: 14px; font-weight: 500; line-height: 1.45">{title}</p><p class="note" style="color: var(--muted)">{evidence}</p>'
                f'<div class="wrap"><button type="button" class="btn pri sm" style="color: var(--hazelink)">{pri}</button><button type="button" class="btn sm">{sec}</button></div></article>')
    open_ = f"""
    <section class="panel" aria-labelledby="h-open">
      <div class="ph"><h2 id="h-open">開啟中 · 3</h2><span class="sub">發生什麼、影響什麼、證據、下一步</span></div>
      <div class="fleet">
        {card("嚴重 · 03:41 起", "var(--danger)", "hazel-jp-02 失聯 48 秒以上，wiki.example.tw 可能仍在服務", "最後心跳 03:41:18；Zentinel @ jp-02 仍回應 HTTP 200（外部探測）。下一步：查節點網路或 agent 日誌。", "查看節點", "確認已處理")}
        {card("警告 · 02:58 起", "var(--warn)", "shop.example.tw 憑證 11 天後到期，自動續期失敗", "DNS 驗證逾時 ×3；目前憑證仍有效。下一步：更新 DNS 供應商 Token 或改 HTTP 驗證。", "重試續期", "靜音 24h")}
        {card("警告 · 03:12 起", "var(--warn)", "shop staging 部署失敗：migration 不相容", "migrate: relation 「orders_v2」 already exists（0004_orders.sql:12）· lab-mini。下一步：修 migration 或回滾資料庫。", "看工作", "指派給")}
      </div>
    </section>"""
    main = head + tiles + rules + f'<div class="cg">{heat}{top}</div>' + route_panel + open_
    return U.shell("events", main)


SCREENS = {
    # name: (title, builder, w, h_guess, mobile)
    "Overview": ("總覽", overview, 1440, 1700),
    "NodeDetail": ("節點 hazel-tw-01", node_detail, 1440, 1700),
    "Projects": ("專案與服務", projects, 1440, 1700),
    "Containers": ("Docker／Podman", containers, 1440, 1500),
    "WorkCenter": ("工作中心", work_center, 1440, 1700),
    "GitHubSource": ("從 GitHub 匯入", github_source, 1440, 1100),
    "DeployCheck": ("部署檢查 · 變更計畫 #418", lambda: deploy_check(False), 1440, 1500),
    "DeployConfirm": ("批准確認", lambda: deploy_check(True), 1440, 1900),
    "States": ("錯誤與空狀態", states, 1440, 1300),
    "Websites": ("網站與入口", websites, 1440, 2200),
    "Storage": ("儲存與備份", storage, 1440, 2300),
    "Events": ("事件與告警", events, 1440, 2200),
    "M-Overview": ("總覽 · 360", overview, 360, 3600),
    "M-WorkCenter": ("工作中心 · 360", work_center, 360, 3200),
    "M-DeployConfirm": ("批准確認 · 360", lambda: deploy_check(True), 360, 3400),
}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", default=os.path.join(os.path.dirname(__file__), "..", "screens"))
    ap.add_argument("--render-dir", default=None)
    args = ap.parse_args()
    os.makedirs(args.out, exist_ok=True)
    if args.render_dir:
        os.makedirs(args.render_dir, exist_ok=True)
    for name, (title, fn, w, h) in SCREENS.items():
        body = fn()
        with open(os.path.join(args.out, f"{name}.dc.html"), "w", encoding="utf-8") as f:
            f.write(U.dc_wrap(f"HazelminX {title}", body, w, h))
        if args.render_dir:
            for theme in ("dark", "light"):
                with open(os.path.join(args.render_dir, f"{name}-{theme}.html"), "w", encoding="utf-8") as f:
                    f.write(U.plain_html(f"HazelminX {title}", body, theme))
    print("built", len(SCREENS), "screens →", os.path.abspath(args.out))


if __name__ == "__main__":
    main()
