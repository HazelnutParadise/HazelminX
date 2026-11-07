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
RING_LEGEND = [("color:var(--hazel)", "正常"), ("color:var(--warn)", "≥ 70%"), ("color:var(--danger)", "≥ 90%"), ("color:var(--ok)", "執行中的容器")]
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
      <div class="ph"><h2 id="h-nodes">艦隊 · 3 節點</h2>{U.legend(RING_LEGEND)}<a class="more" href="NodeDetail.dc.html">表格檢視 →</a></div>
      <div class="fleet">
        {U.node_card("hazel-tw-01", "Ubuntu 24.04 · 控制中心 · Docker · 8 核 · L4 GPU", U.status("ok", "連線中"), 37, (11.2, 31.3), (412, 931), CPU_TW[-12:], "2 秒前", (9, 10))}
        {U.node_card("hazel-jp-02", "Debian 12 · Docker · 4 核 · 資料碟未綁定", U.status("warn", "狀態過期"), 64, (13.9, 15.5), (870, 931), CPU_JP[-12:], "03:41:18", (3, 3), stale=True)}
        {U.node_card("lab-mini", "Debian 13 · Podman rootless · 4 核", U.status("info", "維護中"), 3, (1.1, 7.6), (38, 118), CPU_LAB[-12:], "5 秒前", (0, 2))}
      </div>
      <div class="note" style="padding: 0 14px 10px">* 上次觀測值，節點已過期 · 綠色段＝執行中的容器</div>
    </section>"""

    topo = U.topology([
        ("邊緣", [("cf", "Cloudflare", "155K／日 · 取樣 0.9%", "ok")]),
        ("入口", [("zt", "Zentinel", "tw-01 · WAF 偵測", "ok")]),
        ("站點", [("shop", "shop.example.tw", "憑證 11 天 · 64%", "warn"), ("wiki", "wiki.example.tw", "正常 · 31%", "ok"), ("stg", "staging.shop", "502 · 部署失敗", "danger")]),
        ("服務", [("api", "shop-api", "4 容器 · p95 310", "ok"), ("wapp", "wiki-app", "2 容器 · p95 180", "ok"), ("sapi", "shop-api · stg", "migration 失敗", "danger")]),
        ("節點", [("tw", "hazel-tw-01", "連線中", "ok"), ("jp", "hazel-jp-02", "狀態過期", "warn"), ("lab", "lab-mini", "維護中", "info")]),
    ], [("cf", "zt", 1, ""), ("zt", "shop", .64, ""), ("zt", "wiki", .31, ""), ("zt", "stg", .05, "danger"),
        ("shop", "api", .64, ""), ("wiki", "wapp", .31, ""), ("stg", "sapi", .05, "danger"),
        ("api", "tw", .64, ""), ("wapp", "jp", .31, ""), ("sapi", "lab", .05, "danger")], w=820, box_w=142, box_h=42)
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
      <div class="ph"><h2 id="h-proj">專案 · 3 環境</h2>{U.legend(TL_LEGEND)}<a class="more" href="Projects.dc.html">全部 →</a></div>
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
      <h2 id="h-att" style="font-size: 13px; color: var(--muted); letter-spacing: .04em">需要處理 · 4</h2>
      {card("節點失聯 · 48 秒", "var(--danger)", "hazel-jp-02 · 最後心跳 03:41:18", "受影響：wiki-app、wiki-db、zentinel（3 容器）<br><span class='mono'>heartbeat send failed: dial tcp 203.0.113.22:8443: i/o timeout</span>", "查看節點", "SSH 備援")}
      {card("待批准 · 變更計畫 #418", "var(--hazel)", "shop production · commit 9b3e7f1 · 規則：新增宿主機掛載", "受影響：shop-api、shop-worker · 自動部署已暫停<br><span class='mono'>compose.yaml:41  - /:/host:ro</span>", "檢視差異", "拒絕")}
      {card("憑證到期 · 11 天", "var(--warn)", "shop.example.tw · 續期失敗 ×3", "到期 11 月 18 日 · 目前憑證有效<br><span class='mono'>acme: dns-01 challenge timeout (cloudflare)</span>", "重試續期", "工作紀錄")}
      {card("資料碟 ≥ 90% · 93%", "var(--danger)", "hazel-jp-02 /mnt/data · 剩 61 GiB", "裝置身分：未綁定 · 30 天 +121 GiB · 預估 15 天後滿<br><span class='mono'>/dev/sdb1 870G/931G</span>", "查看儲存", "靜音 24h")}
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
      <div class="name"><a href="Nodes.dc.html" class="note">主機與節點 /</a><h1 class="mono">hazel-tw-01</h1></div>
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
    tabs = ('<div class="panel"><div class="tabs" role="tablist"><button type="button" class="tab on" role="tab" aria-selected="true">監控</button><a class="tab" role="tab" href="System.dc.html">系統</a><a class="tab" role="tab" href="Containers.dc.html">容器 10</a><a class="tab" role="tab" href="Terminal.dc.html">終端機</a><a class="tab" role="tab" href="Logs.dc.html">日誌</a><a class="tab" role="tab" href="Storage.dc.html">儲存</a></div></div>')
    main = (head + tabs + rings + range_row() + f'<div class="cg">{cpu}{mem}{io}{net}{load}{temp}</div><div class="cg">{disks}{hot}</div>'
            + gpu_head + f'<div class="cg">{gpu_util}{gpu_vram}{gpu_power}</div><div class="cg">{gpu_proc}{gpu_auth}</div>')
    return U.shell("nodes", main)


# ---------- 專案與服務 ----------
def projects() -> str:
    head = U.page_head("專案與服務", OBS, '<button type="button" class="btn pri">部署最新 commit</button><button type="button" class="btn">新增專案</button><button type="button" class="btn">從 GitHub 匯入</button>')
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
      <div class="ph"><h2 id="h-stopo">shop · production · 拓撲</h2><span class="sub">來自 compose.yaml：入口 → 服務 → 資料 → 儲存與備份；線寬＝呼叫量</span><a class="more" href="#compose">原始 Compose →</a></div>
      <div style="padding: 10px 14px 6px"><div class="ch-d">{svc_topo}</div><div class="ch-m">{svc_topo_m}</div></div>
    </section>"""
    deploys = f"""
    <section class="panel" aria-labelledby="h-dep">
      <div class="ph"><h2 id="h-dep">部署歷史 · 近 24 次</h2><div class="legend" style="margin-left:0"><span><i class="rect" style="background: var(--ok)"></i>成功</span><span><i class="rect" style="background: var(--danger)"></i>失敗</span><span><i class="rect" style="background: var(--warn)"></i>回滾</span><span><i class="rect" style="background: var(--blue)"></i>進行中</span></div><a class="more" href="WorkCenter.dc.html">工作中心 →</a></div>
      <div style="padding: 12px 14px; display: flex; flex-direction: column; gap: 8px">{U.state_timeline("oooooooofoooooooooooooro" , [f"#{380 + i}" for i in range(24)])}<div class="note" style="display:flex;justify-content:space-between"><span>#380 · 10 月 21 日</span><span>#403 · a3f9c1e · 02:57 今天</span></div></div>
      <div class="row hd dprow"><span>部署</span><span>commit</span><span class="c3">觸發</span><span class="c4">時長</span><span>結果</span></div>
      <div class="row dprow"><span>#403</span><span class="mono">a3f9c1e 修正結帳逾時</span><span class="c3">Webhook</span><span class="mono c4">1 分 48 秒</span><span>{U.status("ok", "成功")} <span class="note">02:57</span></span></div>
      <div class="row dprow"><span>#402</span><span class="mono">88b1d02 升級 redis 7.4</span><span class="c3">手動 · tim</span><span class="mono c4">3 分 12 秒</span><span>{U.status("warn", "回滾")} <button type="button" class="btn sm">回到這版</button></span></div>
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
    main = head + U.scope_bar([("全部節點", "3 環境"), ("hazel-tw-01", "shop production"), ("hazel-jp-02", "wiki"), ("lab-mini", "shop staging")]) + tabs + topo_panel + services + f'<div class="cg">{rq_card}{er_card}</div>{lat_card}' + deploys
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
                f'<span class="mono c5">{net}</span><span class="mono c5" style="color:{"var(--warn)" if restarts else "var(--faint)"}">{restarts}</span><span class="mono c5 note">{ports}</span>{U.actions(["log", "term", "restart", "more"])}</a>')
    table = f"""
    <section class="panel" aria-labelledby="h-ct">
      <div class="ph"><h2 id="h-ct">容器 · 16</h2><span class="sub">顯示 8，依 CPU 排序</span><a class="more" href="#all">全部 →</a></div>
      <div class="scroll"><div style="min-width: 320px">
      <div class="row hd crow"><span>容器</span><span>狀態</span><span class="c3">節點</span><span class="c4">CPU</span><span class="c4">記憶體</span><span class="c5">網路 I/O</span><span class="c5">重啟</span><span class="c5">Port</span><span></span></div>
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
      <div class="ph"><h2 id="h-img">映像 · 23 · tw-01</h2><span class="sub">段條＝使用中／未用／建置快取；拉取、推送、匯入匯出、建置、標籤</span><div class="wrap" style="margin-left:auto"><button type="button" class="btn sm">清理未使用 4.1 GiB</button></div></div>
      <div style="padding:12px 14px;display:flex;flex-direction:column;gap:8px">{U.tall_bar([(9.8, "使用中 16", "var(--q5)"), (4.1, "未使用 7", "var(--q3)"), (1.2, "建置快取", "var(--q2)")], 15.1, unit="G")}
      <div class="scale"><i style="background:var(--q5)"></i>使用中 <i style="background:var(--q3)"></i>未使用（可回收） <i style="background:var(--q2)"></i>建置快取</div></div>
      {U.ranked([("ghcr.io/hazelnut/shop-api:a3f9c1e", 812, "812 MiB · 使用中"), ("ollama/ollama:0.5.7", 2900, "2.9 GiB · 使用中"), ("postgres:16.4", 430, "430 MiB · 使用中"), ("ghcr.io/hazelnut/shop-api:88b1d02", 810, "810 MiB · 未使用 · 可回收"), ("node:22（建置）", 1100, "1.1 GiB · 未使用")], vmax=3000)}
    </section>"""
    netvol = f"""
    <section class="panel" aria-labelledby="h-nv">
      <div class="ph"><h2 id="h-nv">網路、儲存卷、登錄站</h2></div>
      <div style="padding:12px 14px">{U.matrix(["shop_default（bridge）", "wiki_default（bridge）", "hazelminx_internal", "shop-db-data（volume）", "wiki-media（volume）", "ghcr.io（登錄站）", "registry.example.tw"], ["狀態", "關聯", "備註"], [
        [("ok", "4 容器"), ("none", "shop-api、worker、db、redis"), ("none", "172.18.0.0/16 · 不對外")], [("warn", "上次觀測"), ("none", "wiki-app、wiki-db"), ("none", "jp-02")], [("ok", "2 容器"), ("none", "zentinel ↔ agent"), ("none", "僅內部")],
        [("ok", "84 GiB"), ("none", "shop-db"), ("none", "/mnt/data · 綁定 UUID")], [("warn", "190 GiB"), ("none", "wiki-app"), ("none", "jp-02 · 未綁定")], [("ok", "已登入"), ("none", "hazelnut 組織"), ("none", "Token 61 天")], [("ok", "已登入"), ("none", "私有"), ("none", "鏡像 docker.io")],
      ], first_col="minmax(190px,1.4fr)")}</div>
    </section>"""
    compose = f"""
    <section class="panel" aria-labelledby="h-cp">
      <div class="ph"><h2 id="h-cp">Compose 專案 · 3</h2><span class="sub">完整 Compose 檔由專案頁管理；這裡列引擎看到的</span><a class="more" href="Projects.dc.html">專案與服務 →</a></div>
      <div class="row hd srow"><span>專案</span><span>狀態</span><span class="c3">節點 · 服務</span><span class="c4">來源</span><span class="c4">上次部署</span><span class="c5"></span><span></span></div>
      <a class="row srow" href="Projects.dc.html"><span class="name"><span class="n">shop</span><span class="d">production</span></span><span>{U.status("ok", "4/4 執行中")}</span><span class="c3 mono">tw-01 · 4</span><span class="c4 note">GitHub a3f9c1e</span><span class="mono c4">02:57</span><span class="c5"></span><span></span></a>
      <a class="row srow" href="Projects.dc.html"><span class="name"><span class="n">wiki</span><span class="d">production</span></span><span>{U.status("warn", "過期")}</span><span class="c3 mono">jp-02 · 2</span><span class="c4 note">GitHub 7d02b4a</span><span class="mono c4">1 天前</span><span class="c5"></span><span></span></a>
      <a class="row srow" href="Projects.dc.html"><span class="name"><span class="n">shop</span><span class="d">staging</span></span><span>{U.status("dng", "1/4 失敗")}</span><span class="c3 mono">lab-mini · 4</span><span class="c4 note">GitHub c91e0d7</span><span class="mono c4">03:12</span><span class="c5"></span><span></span></a>
    </section>"""
    main = head + U.scope_bar([("全部節點", "16 容器"), ("hazel-tw-01", "Docker"), ("hazel-jp-02", "Docker · 過期"), ("lab-mini", "Podman · K3s")]) + tiles + cmap_panel + filt + table + f'<div class="cg">{heat}{hot}</div>' + f'<div class="cg">{images}{netvol}</div>' + compose
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
             "健康檢查 <span class='mono'>GET /healthz</span>：2／3 通過 · 失敗時回滾到 a3f9c1e",
             '<button type="button" class="btn sm">要求取消（回滾到 a3f9c1e）</button><button type="button" class="btn sm">看日誌</button>', pct=66)}
        {job("備份 wiki-db → S3 tokyo-backups", "hazel-tw-01 · 計畫任務 03:00 · 心跳 1 秒前", U.status("info", "執行中"),
             [("預檢", "done", "空間足夠"), ("dump", "cur", "此步不能取消"), ("傳輸", "todo", "1.8／4.6 GiB 已排程"), ("校驗", "todo", "sha256 + 還原測試")],
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
        return (f'<label class="row rrow" style="cursor:pointer;{"background: var(--raised)" if cur else ""}"><input type="radio" name="repo" aria-label="選擇此 Repo" {"checked" if cur else ""} style="width:18px;height:18px;accent-color: var(--hazel)">'
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
        <div class="field"><label for="path">compose 檔路徑</label><div class="inp"><span class="mono">deploy/compose.yaml</span>{U.status("ok", "已找到")}</div><span class="help">也偵測到 <span class="mono">compose.override.yaml</span>，部署時不套用。</span></div>
        <div class="field"><label>觸發方式</label><div class="wrap"><label class="btn" style="gap:8px"><input type="radio" name="trig" aria-label="觸發方式" checked style="accent-color: var(--hazel)">Webhook（推送即部署）</label><label class="btn" style="gap:8px"><input type="radio" name="trig" aria-label="觸發方式" style="accent-color: var(--hazel)">定期檢查（每 5 分鐘）</label><label class="btn" style="gap:8px"><input type="radio" name="trig" aria-label="觸發方式" style="accent-color: var(--hazel)">只手動</label></div><span class="help">Webhook 會在 GitHub 上建立；沒有公開入口時自動改用定期檢查。</span></div>
        <div class="field"><label for="env">目標環境與節點</label><div class="wrap"><div class="inp" style="width:auto;min-width:160px"><span>production</span></div><div class="inp" style="width:auto;min-width:200px"><span class="mono">hazel-tw-01</span><span class="note" style="margin-left:auto">412 GiB 可用</span></div></div><span class="errt">hazel-jp-02 不可選：狀態過期且資料碟 93%。</span></div>
      </div>
      <div class="ph" style="border-top: 1px solid var(--line); border-bottom: 0"><span class="note">讀取 compose 檔 → 列出服務、Port、Volume → 建立部署工作</span><div class="wrap" style="margin-left:auto"><a class="btn" href="Projects.dc.html">上一步</a><a class="btn pri" href="DeployCheck.dc.html">偵測與設定 →</a></div></div>
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
        <h2 style="font-size:16px">刪除資料碟 <span class="mono">hazel-jp-02 /mnt/data</span>（sda1）？</h2>
        <p style="font-size:13px;color:var(--muted)">會解除掛載並清除 870 GiB 資料，包含 <span class="mono">wiki-db</span> 的資料目錄與 2 個備份暫存。最近的備份 wiki-db 校驗通過但尚未還原測試。</p>
        <div class="field"><label for="dd">輸入磁碟名稱 <span class="mono">sda1</span> 以確認</label><div class="inp err"><input id="dd" aria-label="磁碟名稱" value="sda2" style="background:transparent;border:0;flex:1" aria-invalid="true" aria-describedby="dd-err"></div><span class="errt" id="dd-err">名稱不符：請輸入 sda1。</span></div>
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
    <section class="panel" aria-labelledby="h-sites">
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
                f'<span class="note">{sched} → {dest}</span>{U.calendar(states)}<span class="note">{last}</span>'
                f'<div class="wrap"><button type="button" class="btn sm">立即執行</button><button type="button" class="btn sm">還原…</button><button type="button" class="btn sm">紀錄</button></div></div>')
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
      <div class="ph"><h2 id="h-rt">告警路由</h2><span class="sub">送達＝供應商回覆已接受，不等於有人看到；測試訊息不帶正式機密</span><a class="more" href="#routes">編輯路由 →</a></div>
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
        {card("嚴重 · 節點失聯 · 03:41 起", "var(--danger)", "hazel-jp-02 · 最後心跳 03:41:18 · 外部探測 wiki.example.tw：200", "受影響：wiki-app、wiki-db、zentinel<br><span class='mono'>heartbeat send failed: dial tcp 203.0.113.22:8443: i/o timeout</span>", "查看節點", "確認已處理")}
        {card("警告 · 憑證到期 · 02:58 起", "var(--warn)", "shop.example.tw · 到期 11 月 18 日 · 續期失敗 ×3", "<span class='mono'>acme: dns-01 challenge timeout (cloudflare)</span>", "重試續期", "靜音 24h")}
        {card("警告 · 部署失敗 · 03:12 起", "var(--warn)", "shop staging #71 · lab-mini · 步驟 migrate 結束碼 1", "服務中版本：c91e0d7 之前的 c0ffee1（自動回滾）<br><span class='mono'>migrate: relation 「orders_v2」 already exists (0004_orders.sql:12)</span>", "看工作", "指派給")}
      </div>
    </section>"""
    main = head + U.scope_bar([("全部節點", "3"), ("hazel-tw-01", "Docker"), ("hazel-jp-02", "Docker · 過期"), ("lab-mini", "Podman · K3s")]) + tiles + rules + f'<div class="cg">{heat}{top}</div>' + route_panel + open_
    return U.shell("events", main)



# ---------- 主機與節點（列表） ----------
def nodes() -> str:
    head = U.page_head("主機與節點", OBS, '<button type="button" class="btn pri">新增節點</button><button type="button" class="btn">終端機</button><button type="button" class="btn">批次維護</button>')
    tiles = '<div class="tiles">' + "".join([
        U.stat_tile("節點", "3", "", "2 連線中 · 1 維護中 · jp-02 狀態過期", None),
        U.stat_tile("引擎", "Docker 2 · Podman 1 · K3s 1", "", "agent 0.1.0 × 3 · 相容", None),
        U.stat_tile("節點憑證", "1", "/3 需在 30 天內輪替", "tw-01 剩 12 天", False),
        U.stat_tile("待套用安全更新", "7", "套件", "jp-02 5 · lab-mini 2 · 維護時段 03:00", None),
    ]) + "</div>"
    reg = ('<a class="ncard" href="#add" style="justify-content:center;align-items:center;text-align:center;border-style:dashed;min-height:230px"><span style="font-size:28px;color:var(--hazel)">+</span><span style="font-weight:500">加入節點</span>'
           '<span class="note">產生 10 分鐘內有效的單次註冊碼；Agent 主動連回 Core，不用開入站 Port</span></a>')
    fleet = f"""
    <section class="panel" aria-labelledby="h-fl">
      <div class="ph"><h2 id="h-fl">艦隊</h2>{U.legend(RING_LEGEND)}<div class="wrap" style="margin-left:auto"><span class="chip on">全部 3</span><span class="chip">production 2</span><span class="chip">lab 1</span><span class="chip">tw 1</span><span class="chip">jp 1</span></div></div>
      <div class="fleet">
        {U.node_card("hazel-tw-01", "Ubuntu 24.04 · 控制中心 · Docker 27.3 · 8 核 · L4 GPU", U.status("ok", "連線中"), 37, (11.2, 31.3), (412, 931), CPU_TW[-12:], "2 秒前", (9, 10))}
        {U.node_card("hazel-jp-02", "Debian 12 · Docker 27.1 · 4 核 · 資料碟未綁定", U.status("warn", "狀態過期"), 64, (13.9, 15.5), (870, 931), CPU_JP[-12:], "03:41:18", (3, 3), stale=True)}
        {U.node_card("lab-mini", "Debian 13 · Podman 5.2 rootless · K3s 1.31 · 4 核", U.status("info", "維護中"), 3, (1.1, 7.6), (38, 118), CPU_LAB[-12:], "5 秒前", (0, 2))}
        {reg}
      </div>
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
      <div class="ph"><h2 id="h-eng">引擎與元件</h2><span class="sub">版本、可管理與否；不同但相容的 Agent 可以協作</span></div>
      <div style="padding:12px 14px">{U.matrix(["hazel-tw-01", "hazel-jp-02", "lab-mini"], ["Agent", "Docker", "Podman", "K3s", "Zentinel", "節點憑證"], [
        [("ok", "0.1.0"), ("ok", "27.3.1"), ("none", ""), ("none", ""), ("ok", "0.6.54"), ("warn", "12 天")],
        [("warn", "0.1.0 過期"), ("ok", "27.1.0"), ("none", ""), ("none", ""), ("warn", "0.6.50 可更新"), ("ok", "71 天")],
        [("ok", "0.1.0"), ("none", ""), ("ok", "5.2 rootless"), ("ok", "1.31.2"), ("none", ""), ("ok", "88 天")],
      ])}</div>
      <div class="note" style="padding:0 14px 10px">rootless Podman 依 Linux 使用者分環境：lab-mini 有 <span class="mono">deploy</span>、<span class="mono">tim</span> 兩個</div>
    </section>"""
    tools = f"""
    <section class="panel" aria-labelledby="h-tools">
      <div class="ph"><h2 id="h-tools">主機工具 · hazel-tw-01</h2><span class="sub">每張卡＝現況＋一個動作；改動走工作中心</span><div class="range" style="margin-left:auto"><button type="button" class="btn on" aria-pressed="true">tw-01</button><button type="button" class="btn" aria-pressed="false">jp-02</button><button type="button" class="btn" aria-pressed="false">lab-mini</button></div></div>
      <div class="fleet" style="grid-template-columns:repeat(auto-fill,minmax(min(200px,100%),1fr))">
        <a class="ncard" href="System.dc.html"><span class="note">時區</span><span>Asia/Taipei · NTP 同步 · 偏差 +3 ms</span></a><a class="ncard" href="System.dc.html"><span class="note">主機名稱</span><span>hazel-tw-01 · /etc/hosts 4 條</span></a><a class="ncard" href="System.dc.html"><span class="note">DNS</span><span>1.1.1.1、8.8.8.8 · 回應 11 ms</span></a>
        <a class="ncard" href="System.dc.html"><span class="note">Swap</span>{U.meter(12, "0.5 / 4 GiB")}<span class="note">swappiness 10</span></a>
        <a class="ncard" href="System.dc.html"><span class="note">可清理空間</span>{U.tall_bar([(4.1, "未用映像", "var(--q5)"), (1.8, "系統日誌", "var(--q4)"), (0.9, "套件快取", "var(--q3)")], 8, unit="G")}<span class="note">共 6.8 GiB · 上次清理 10 月 30 日</span></a>
        <a class="ncard" href="System.dc.html"><span class="note">Supervisor</span><span>{U.status("ok", "3 / 3 執行中")}</span><span class="note">shop-cron、mail-relay、backup-runner</span></a>
        <a class="ncard" href="System.dc.html"><span class="note">Fail2ban</span><span>{U.status("ok", "啟用 · 封鎖 14 IP")}</span>{U.sparkline([2, 1, 3, 0, 1, 4, 2, 1, 0, 2, 3, 1], w=100)}<span class="note">sshd、zentinel 兩個 jail</span></a>
        <a class="ncard" href="System.dc.html"><span class="note">ClamAV</span><span>{U.status("ok", "病毒碼 3 小時前")}</span><span class="note">每週日 05:00 掃 /mnt/data · 上次 0 感染</span></a>
        <a class="ncard" href="System.dc.html"><span class="note">FTP／SFTP</span><span>{U.status("info", "SFTP only · 2 帳號")}</span><span class="note">FTP 明文已停用</span></a>
        <a class="ncard" href="System.dc.html"><span class="note">root 密碼</span><span>{U.status("warn", "281 天未換")}</span><span class="note">密碼登入：允許</span></a>
        <a class="ncard" href="System.dc.html"><span class="note">套件來源</span><span>{U.status("ok", "簽章有效 · 3 來源")}</span><span class="note">無待套用更新</span></a>
        <a class="ncard" href="System.dc.html"><span class="note">開機 / 關機</span><span>{U.status("ok", "運行 41 天")}</span><span class="note">受影響容器：10</span></a>
      </div>
    </section>"""
    ports = f"""
    <section class="panel" aria-labelledby="h-fw">
      <div class="ph"><h2 id="h-fw">防火牆與對外 Port</h2><span class="sub">主機規則與容器對外暴露放在同一張圖；Docker 繞過防火牆的 Port 會標出來</span><a class="more" href="Security.dc.html">安全防護 →</a></div>
      <div style="padding:12px 14px">{U.matrix(["hazel-tw-01", "hazel-jp-02", "lab-mini"], ["22", "80", "443", "8443", "8080", "5432", "6379", "11434", "轉發"], [
        [("ok", "sshd"), ("ok", "zentinel"), ("ok", "zentinel"), ("ok", "core · 白名單"), ("warn", "shop-api 直通"), ("none", "僅內部"), ("none", "僅內部"), ("warn", "ollama 直通"), ("ok", "2 條")],
        [("ok", "sshd"), ("ok", "zentinel"), ("ok", "zentinel"), ("none", ""), ("none", ""), ("dng", "wiki-db 對外!"), ("none", ""), ("none", ""), ("none", "0")],
        [("ok", "sshd 2222"), ("none", ""), ("none", ""), ("none", ""), ("none", ""), ("none", ""), ("none", ""), ("none", ""), ("none", "0")],
      ], first_col="minmax(100px,1fr)")}</div>
      <div class="note" style="padding:0 14px 10px">■ 紅 = 容器用 <span class="mono">-p</span> 直接對外且沒有規則擋；Docker 的 iptables 鏈會繞過 ufw，所以這裡看的是實際可達</div>
    </section>"""
    procs = f"""
    <section class="panel" aria-labelledby="h-proc">
      <div class="ph"><h2 id="h-proc">程序 · hazel-tw-01 · 現在</h2><div class="range" role="group" style="margin-left:auto"><button type="button" class="btn on" aria-pressed="true">CPU</button><button type="button" class="btn" aria-pressed="false">記憶體</button><button type="button" class="btn" aria-pressed="false">I/O</button><button type="button" class="btn" aria-pressed="false">連線</button></div></div>
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
    main = head + tiles + fleet + f'<div class="cg">{cross}{engines}</div>' + tools + ports + f'<div class="cg">{procs}{ssh}</div>'
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
        ("Namespace", [("ns1", "wiki", "4 工作負載", "ok"), ("ns2", "monitoring", "3 工作負載", "ok"), ("ns3", "shop-stg", "1 CrashLoop", "danger")]),
        ("節點", [("c1", "lab-mini（控制＋工作）", "4 核 · 8 GiB · 23 Pod", "warn"), ("w1", "lab-pi-01（工作）", "4 核 · 4 GiB · 0 Pod · NotReady", "danger")]),
    ], [("zt", "tr", .9, ""), ("tr", "ns1", .7, ""), ("tr", "ns2", .2, ""), ("tr", "ns3", .1, "danger"), ("ns1", "c1", .7, ""), ("ns2", "c1", .2, ""), ("ns3", "c1", .1, "danger"), ("ns1", "w1", .05, "dim")], w=1140, box_w=200)
    topo_m = U.flowlist([("Zentinel", "Traefik", "80/443"), ("Traefik", "wiki", "4 工作負載"), ("Traefik", "shop-stg", "CrashLoop"), ("wiki", "lab-mini", "23 Pod"), ("lab-pi-01", "NotReady", "0 Pod")])
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
    <section class="panel" aria-labelledby="h-wl">
      <div class="ph"><h2 id="h-wl">工作負載 · 9</h2><div class="wrap" style="margin-left:auto"><span class="chip on">全部</span><span class="chip">Deployment 5</span><span class="chip">StatefulSet 2</span><span class="chip">DaemonSet 1</span><span class="chip">CronJob 1</span></div></div>
      <div class="row hd srow acts-end"><span>名稱</span><span>狀態</span><span class="c3">就緒副本</span><span class="c4">診斷</span><span></span></div>
      {wl("wiki", "Deployment", "wiki-app", 2, 2, U.status("ok", "就緒"), "ghcr.io/hazelnut/wiki:7d02b4a", "—")}
      {wl("wiki", "StatefulSet", "wiki-db", 1, 1, U.status("ok", "就緒"), "postgres:16.4", "PVC local-path 42G")}
      {wl("shop-stg", "Deployment", "shop-api", 0, 1, U.status("dng", "CrashLoopBackOff"), "ghcr.io/hazelnut/shop-api:c91e0d7", "探針失敗：/healthz 500 · migration 不相容")}
      {wl("monitoring", "DaemonSet", "node-exporter", 1, 2, U.status("warn", "部分"), "prom/node-exporter:1.8", "lab-pi-01 NotReady")}
      {wl("wiki", "CronJob", "wiki-backup", 1, 1, U.status("ok", "上次 03:00 成功"), "hazelminx/backup-runner", "下次 11/08 03:00")}
      <div class="note" style="padding:8px 14px;border-top:1px solid var(--line)">診斷欄把排程、映像拉取、探針、掛載、DNS 的失敗原因寫成白話，原始事件可展開</div>
    </section>"""
    storage = f"""
    <section class="panel" aria-labelledby="h-kpv">
      <div class="ph"><h2 id="h-kpv">儲存類別與 PVC</h2></div>
      <div style="padding:12px 14px;display:flex;flex-direction:column;gap:10px">
        <div><span class="note">local-path（lab-mini /mnt/data）· 118 GiB</span>{U.tall_bar([(42, "wiki-db", "var(--q5)"), (8, "wiki-media", "var(--q4)"), (3, "prometheus", "var(--q3)")], 118)}</div>
        <div><span class="note">nfs（hazel-tw-01 /mnt/data/nfs）· 200 GiB</span>{U.tall_bar([(19, "wiki-backup", "var(--q5)")], 200)}</div>
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
    main = head + U.scope_bar([("lab-k3s", "K3s · lab-mini ＋ lab-pi-01"), ("＋ 連接既有叢集", ""), ("＋ 用 K3s 建叢集", "")], label="叢集") + tiles + topo_panel + f'<div class="cg">{reserve}{storage}</div>' + workloads + f'<div class="cg">{helm}{maint}</div>'
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
        {app("Pg", "PostgreSQL", "16.4", "tw-01 · shop-db · 僅內部", U.status("ok", "執行中"), 31, "1.29 / 4 GiB", "被 shop、wiki 重用")}
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
        {catalog("Nc", "Nextcloud", "儲存", ["Docker", "Podman"], ["amd64", "arm64"], "31.0.2 · AGPL · 漏洞 0 已知", "需要 PostgreSQL／Redis，可重用現有")}
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
      <div style="padding:12px 14px">{U.pipeline([("選節點", "done", "tw-01", "Docker · amd64 OK"), ("相依", "done", "重用 shop-db", "PostgreSQL 16 · 新 DB"), ("Port", "done", "8081 空", "不與 shop-api 衝突"), ("資料位置", "cur", "/mnt/data/apps", "剩 519 GiB"), ("入口", "todo", "cloud.example.tw", "憑證自動"), ("安裝", "todo", "—", "可看腳本")])}</div>
    </section>"""
    main = head + U.scope_bar([("全部節點", "7 應用"), ("hazel-tw-01", "Docker"), ("hazel-jp-02", "Docker · 過期"), ("lab-mini", "Podman · K3s")]) + tabs + installed + upg + allapps + f'<div class="cg">{sources}{preflight}</div>'
    return U.shell("store", main)


# ---------- API 代理 ----------
def apiproxy() -> str:
    head = U.page_head("API 代理", OBS, '<button type="button" class="btn pri">新增服務</button><button type="button" class="btn">發 Token</button><button type="button" class="btn">匯入 infra-manager 資料</button>')
    tiles = '<div class="tiles">' + "".join([
        U.stat_tile("服務", "4", "", "3 啟用 · 1 停用", None),
        U.stat_tile("Token", "11", "", "2 個 14 天內到期", False),
        U.stat_tile("今日請求", "18,420", "", "被拒 212（1.2%）", None, spark=[int(x) for x in walk(24, 760, 200, 200, 1300)]),
        U.stat_tile("p95 延遲", "184", "ms", "上游 162 · 代理 22", None, spark=walk(12, 180, 20, 140, 240)),
    ]) + "</div>"
    topo = U.topology([
        ("使用者 / Token", [("u1", "tim · 3 Token", "擁有者", "ok"), ("u2", "lab-bot · 2 Token", "到期 9 天", "warn"), ("u3", "partner-a · 1 Token", "限速 60/分", "ok")]),
        ("服務", [("s1", "/ollama", "推論 API · 限速", "ok"), ("s2", "/shop-admin", "需 Header 轉換", "ok"), ("s3", "/legacy-report", "已停用", "dim")]),
        ("上游", [("t1", "ollama:11434 @ tw-01", "健康", "ok"), ("t2", "shop-api:8080 @ tw-01", "健康", "ok"), ("t3", "report:9000 @ jp-02", "狀態過期", "warn")]),
    ], [("u1", "s1", .5, ""), ("u1", "s2", .3, ""), ("u2", "s1", .4, ""), ("u3", "s1", .2, ""), ("u3", "s3", .05, "dim"), ("s1", "t1", .8, ""), ("s2", "t2", .3, ""), ("s3", "t3", .05, "dim")], w=1140, box_w=190)
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
        {tok("hx_live_90bd…", "tim", "/ollama、/shop-admin", 77, 3, 600)}
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
    <section class="panel" aria-labelledby="h-svcs">
      <div class="ph"><h2 id="h-svcs">服務 · 4</h2></div>
      <div class="row hd srow"><span>路徑</span><span>狀態</span><span class="c3">上游驗證 · Header</span><span class="c4">限速</span><span class="c4">使用者</span><span class="c5">p95 1 小時</span><span>現在</span></div>
      {svc("/ollama", U.status("ok", "啟用"), "ollama:11434 @ tw-01 · 串流 OK", "無 · 加 X-Forwarded-User", "60/分 · 10k/日", "3", walk(12, 180, 25, 140, 260))}
      {svc("/shop-admin", U.status("ok", "啟用"), "shop-api:8080 @ tw-01", "上游 Bearer（機密庫）· 去 Cookie", "600/分", "2", walk(12, 90, 12, 60, 130))}
      {svc("/files", U.status("ok", "啟用"), "hazelminx core · 上傳 ≤ 2 GiB", "平台 Token", "120/分", "1", walk(12, 40, 8, 20, 70))}
      {svc("/legacy-report", U.status("info", "停用"), "report:9000 @ jp-02", "Basic", "—", "0", [0] * 12)}
    </section>"""
    main = head + tiles + topo_panel + f'<div class="cg">{tokens}{rejected}</div>' + usage + services
    return U.shell("api", main)


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
        {inst("Pg", "shop-db", "PostgreSQL 16.4", "tw-01 · 容器", U.status("ok", "健康"), 23, 100, 84, 200, U.status("ok", "僅內部"), 100, "被 shop、nextcloud 重用")}
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
    <section class="panel" aria-labelledby="h-dbs">
      <div class="ph"><h2 id="h-dbs">shop-db · 資料庫與帳號</h2><a class="more" href="#browse">瀏覽資料 →</a></div>
      <div style="padding:12px 14px;display:flex;flex-direction:column;gap:8px">
        <span class="note">容量 84 GiB</span>{U.tall_bar([(61, "shop", "var(--q5)"), (14, "nextcloud", "var(--q4)"), (6, "shop_analytics", "var(--q3)"), (3, "postgres / 系統", "var(--q2)")], 84)}
      </div>
      <div class="row hd srow"><span>帳號</span><span>權限</span><span class="c3">資料庫</span><span class="c4">來源限制</span><span class="c4">最後登入</span><span class="c5"></span><span></span></div>
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
    head = U.page_head("AI", OBS, '<button type="button" class="btn pri">下載模型</button><button type="button" class="btn">安裝 Ollama</button><button type="button" class="btn">新增 MCP 服務</button><button type="button" class="btn">新增 Agent</button>')
    tabs = ('<div class="panel"><div class="tabs" role="tablist"><button type="button" class="tab on" role="tab" aria-selected="true">模型與推論</button><button type="button" class="tab" role="tab" aria-selected="false">Agent · 2</button>'
            '<button type="button" class="tab" role="tab" aria-selected="false">MCP 服務 · 3</button><button type="button" class="tab" role="tab" aria-selected="false">GPU</button><button type="button" class="tab" role="tab" aria-selected="false">推論 API</button></div></div>')
    inst = f"""
    <section class="panel" aria-labelledby="h-ol">
      <div class="ph"><h2 id="h-ol">推論服務 · 2</h2><span class="sub">看見 GPU 不等於容器能用：驅動、容器工具、裝置權限、實際推論分開檢查</span></div>
      <div class="fleet">
        <a class="ncard" href="#ol1"><div class="nh"><span class="n">ollama @ hazel-tw-01</span>{U.status("ok", "執行中")}<span class="d">0.5.7 · 容器 · GPU L4 · 11434 僅內部 → /ollama 代理</span></div>
          <div class="rings">{U.ring(44, "44%", "GPU 核心")}{U.ring(14.2 / 24 * 100, "14G", "顯示記憶體 /24G")}{U.ring(61 / 120 * 100, "61G", "模型 /120G 配額", warn=80, danger=90)}</div>
          <div class="ft">{U.steps([("驅動", "done", "550.90"), ("容器工具", "done", "1.16"), ("裝置權限", "done", "gpu=all"), ("實際推論", "done", "38 tok/s")])}</div>
          <div class="ft"><span class="note">已載入 qwen2.5:14b · 2 個請求處理中</span><span style="margin-left:auto" class="chip on">0.6.1 可更新</span></div></a>
        <a class="ncard" href="#ol2"><div class="nh"><span class="n">ollama @ lab-mini</span>{U.status("info", "CPU 模式")}<span class="d">0.5.7 · 既有容器（接管）· 11434</span></div>
          <div class="rings">{U.ring(3, "3%", "CPU")}{U.ring(2.1 / 7.6 * 100, "2.1G", "記憶體 /7.6G")}{U.ring(9 / 40 * 100, "9G", "模型 /40G", warn=80, danger=90)}</div>
          <div class="ft">{U.steps([("驅動", "fail", "無 GPU"), ("容器工具", "todo", "不適用"), ("裝置權限", "todo", "不適用"), ("實際推論", "done", "4 tok/s")])}</div>
          <div class="ft"><span class="note">GPU：未偵測到 · 模式：CPU</span></div></a>
      </div>
    </section>"""
    def model(name, size, quota_pct, node, st, digest, src, note):
        return (f'<a class="row srow" href="#m"><span class="name"><span class="n">{name}</span><span class="d">{src} · {digest}</span></span><span>{st}</span><span class="c3 mono">{node}</span><span class="c4">{U.meter(quota_pct, size, warn=101, danger=101)}</span><span class="c4 note">{note}</span><span class="c5"></span><span></span></a>')
    models = f"""
    <section class="panel" aria-labelledby="h-md">
      <div class="ph"><h2 id="h-md">模型 · 5</h2><span class="sub">量表＝佔該節點模型配額的比例；digest 用來確認下載完整</span><div class="wrap" style="margin-left:auto"><label class="inp" style="min-width:220px"><span class="note">搜尋來源</span><input placeholder="ollama.com、Hugging Face、私有" style="background:transparent;border:0;flex:1;min-width:0"></label></div></div>
      <div class="row hd srow"><span>模型</span><span>狀態</span><span class="c3">節點</span><span class="c4">大小 / 配額</span><span class="c4">備註</span><span class="c5"></span><span></span></div>
      {model("qwen2.5:14b", "9.0 GiB", 7.5, "tw-01", U.status("ok", "已載入 · GPU"), "sha256:7c1a…", "ollama.com", "Apache-2.0 · 2 請求中")}
      {model("llama3.1:8b", "4.9 GiB", 4, "tw-01", U.status("info", "未載入"), "sha256:42e0…", "ollama.com", "Llama 授權")}
      {model("nomic-embed-text", "274 MiB", 0.2, "tw-01", U.status("ok", "已載入 · 嵌入"), "sha256:0a90…", "ollama.com", "02:00 批次嵌入用")}
      {model("deepseek-r1:32b", "19.9 GiB", 16.6, "tw-01", U.status("warn", "下載中 71%"), "—", "ollama.com", "預估 6 分 · 速率 48 MB/s")}
      {model("qwen2.5:7b", "4.7 GiB", 11.7, "lab-mini", U.status("ok", "已載入 · CPU"), "sha256:b11c…", "ollama.com", "4 tok/s")}
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
    main = head + U.scope_bar([("全部節點", "2 推論服務"), ("hazel-tw-01", "GPU L4"), ("lab-mini", "CPU")]) + tabs + inst + models + f'<div class="cg">{pull}{chat}</div>' + mcp + api
    return U.shell("ai", main)


# ---------- 檔案 ----------
def files() -> str:
    head = U.page_head("檔案", OBS, '<button type="button" class="btn pri">上傳</button><button type="button" class="btn">新增</button><button type="button" class="btn">遠端下載</button><button type="button" class="btn">在此開終端機</button>')
    bar = (U.scope_bar([("hazel-tw-01", "Docker"), ("hazel-jp-02", "過期"), ("lab-mini", "Podman"), ("shop-api（容器內）", "tw-01")], label="節點") + '<div class="panel"><div style="display:flex;gap:10px;flex-wrap:wrap;align-items:center;padding:10px 14px">'
           '<span class="mono" style="font-size:13px">/mnt/data/apps/wiki/</span><span class="note">tim（擁有者）· 可寫</span>'
           '<label class="inp" style="flex:1;min-width:220px"><span class="note">搜尋</span><input placeholder="名稱、大小、時間或文字內容（授權範圍內）" style="background:transparent;border:0;flex:1;min-width:0"></label>'
           '<span class="chip">顯示隱藏檔</span><span class="chip on">收藏 4</span><span class="chip">回收筒 12</span></div></div>')
    tree_html = U.tree([(0, "/mnt/data", "931G", False), (1, "apps", "402G", False), (2, "wiki", "48G", True), (3, "data", "41G", False), (3, "config", "2M", False), (3, "uploads", "7G", False), (2, "shop", "290G", False), (2, "ollama", "61G", False), (1, "backups", "38G", False), (1, "hazelminx", "1.2G", False)])
    def frow(name, kind, size, owner, perm, mtime, note=""):
        return (f'<a class="row srow" href="#f"><span class="name"><span class="n">{name}</span><span class="d">{kind}{(" · " + note) if note else ""}</span></span><span class="mono">{size}</span><span class="mono c3">{owner}</span><span class="mono c4">{perm}</span><span class="mono c4">{mtime}</span><span class="c5"></span><span></span></a>')
    listing = f"""
    <section class="panel" aria-labelledby="h-fl">
      <div class="ph"><h2 id="h-fl">wiki · 5 項</h2><span class="sub">容器掛載、備份都能直接跳到這裡；分享連結有期限與範圍</span><div class="wrap" style="margin-left:auto"><span class="chip">依名稱</span><span class="chip on">依大小</span><span class="chip">依時間</span></div></div>
      <div class="row hd srow"><span>名稱</span><span>大小</span><span class="c3">擁有者</span><span class="c4">權限</span><span class="c4">修改</span><span class="c5"></span><span></span></div>
      {frow("data/", "目錄 · 容器 wiki-db 掛載 /var/lib/postgresql", "41 GiB", "999:999", "drwx------", "2 分前", "容器掛載中")}
      {frow("uploads/", "目錄 · 容器 wiki-app 掛載 /app/uploads", "7.2 GiB", "1000:1000", "drwxr-x---", "昨天", "分享連結 1 條 · 7 天後到期")}
      {frow("config/", "目錄", "2.1 MiB", "root:root", "drwxr-x---", "10 月 30 日")}
      {frow("compose.yaml", "文字 · 歷史 9 版", "3.4 KiB", "root:root", "-rw-r-----", "10 月 30 日", "來自 GitHub a3f9c1e")}
      {frow(".env", "文字 · 含機密（遮罩）", "812 B", "root:root", "-rw-------", "10 月 30 日", "編輯走機密庫，不直接開")}
    </section>"""
    editor = f"""
    <section class="panel" aria-labelledby="h-ed">
      <div class="ph"><h2 id="h-ed">compose.yaml · 歷史 9 版</h2><span class="sub">存檔前先看差異；可回復任一版</span><div class="wrap" style="margin-left:auto"><span class="chip on">目前</span><span class="chip">v8 · 10/21 tim</span><span class="chip">v7 · 10/12 webhook</span></div></div>
      <div style="padding:12px 14px"><div class="term" style="min-height:160px">  wiki-app:
    image: ghcr.io/hazelnut/wiki:7d02b4a
<span style="color:var(--danger)">-   ports: ["3000:3000"]</span>
<span style="color:var(--ok)">+   expose: ["3000"]</span>
    volumes:
      - ./uploads:/app/uploads
<span style="color:var(--ok)">+     - /:/host:ro   # ⚠ 高風險：掛整個根目錄</span></div>
      <div class="note" style="margin-top:8px">編輯器：語法高亮、多分頁、Ctrl+S；存檔會檢查 compose 合法性並保留不認識的欄位</div></div>
    </section>"""
    transfers = f"""
    <section class="panel" aria-labelledby="h-tr">
      <div class="ph"><h2 id="h-tr">傳輸 · 2</h2><span class="sub">大檔分片、可續傳；跨節點複製走 Agent 對 Agent</span></div>
      <div style="padding:12px 14px;display:flex;flex-direction:column;gap:12px">
        <div><div style="display:flex;justify-content:space-between"><span>上傳 media-2026-11.tar（4.2 GiB）→ tw-01 uploads/</span>{U.status("info", "傳輸中")}</div>{U.meter(63, "2.6 / 4.2 GiB · 31 MB/s · 剩 52 秒")}</div>
        <div><div style="display:flex;justify-content:space-between"><span>跨節點複製 uploads/ tw-01 → jp-02</span>{U.status("warn", "等待 jp-02 恢復")}</div>{U.pipeline([("校驗來源", "done", "7.2G", "sha256 清單"), ("傳輸", "wait", "暫停", "jp-02 狀態過期"), ("校驗目的", "todo", "—", ""), ("完成", "todo", "—", "")])}</div>
      </div>
    </section>"""
    perms = f"""
    <section class="panel" aria-labelledby="h-pm">
      <div class="ph"><h2 id="h-pm">uploads/ 的權限與分享</h2><span class="sub">擁有者、群組、Unix 權限、ACL 一張表；分享連結有期限與下載次數</span></div>
      <div style="padding:12px 14px">{U.matrix(["擁有者 1000", "群組 1000", "其他", "ACL deploy", "分享 7f2a…"], ["讀", "寫", "執行／進入", "到期"], [
        [("ok", "是"), ("ok", "是"), ("ok", "是"), ("none", "")],
        [("ok", "是"), ("none", "否"), ("ok", "是"), ("none", "")],
        [("none", "否"), ("none", "否"), ("none", "否"), ("none", "")],
        [("ok", "是"), ("none", "否"), ("ok", "是"), ("none", "")],
        [("ok", "是"), ("none", "否"), ("none", "否"), ("warn", "7 天 · 剩 38 次")],
      ], first_col="minmax(120px,1fr)")}</div>
      <div class="note" style="padding:0 14px 10px">回收筒保留 30 天；刪除容器掛載中的目錄會先警告「容器仍在寫」</div>
    </section>"""
    main = head + bar + f'<div class="cg" style="grid-template-columns:minmax(0,1fr) minmax(0,2fr)"><section class="panel"><div class="ph"><h2>目錄</h2><span class="sub">數字＝大小</span></div><div style="padding:8px 14px">{tree_html}</div></section>{listing}</div>' + f'<div class="cg">{editor}{transfers}</div>' + perms
    return U.shell("files", main)


# ---------- 計畫任務 ----------
def cron() -> str:
    head = U.page_head("計畫任務", OBS, '<button type="button" class="btn pri">新增任務</button><button type="button" class="btn">腳本庫</button><button type="button" class="btn">匯入</button>')
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
      {task("wiki-db 備份", "備份", "tw-01", "每天 03:00", U.status("ok", "啟用"), "o" * 24, "今天 03:00 · 2 分 50 秒", "略過 · 補跑 · 重試 2")}
      {task("shop-db 備份", "備份", "tw-01", "每天 03:30", U.status("ok", "啟用"), "o" * 20 + "f" + "o" * 3, "今天 03:30 · 1 分 10 秒", "略過 · 補跑 · 重試 2")}
      {task("清理日誌", "清理", "全部節點", "每 6 小時", U.status("ok", "啟用"), "o" * 24, "03:00 · 釋出 1.8 GiB", "排隊 · 不補跑 · 不重試")}
      {task("憑證續期檢查", "維護", "zentinel", "每 12 小時", U.status("warn", "上次失敗"), "o" * 22 + "f" * 2, "02:58 · DNS 驗證逾時", "略過 · 補跑 · 重試 3")}
      {task("套件安全更新", "維護", "jp-02、lab-mini", "每天 03:00（維護時段）", U.status("warn", "狀態不明"), "o" * 23 + "p", "03:38 · apt 進行到一半", "排隊 · 不補跑 · 需批准")}
      {task("shop 報表", "腳本 · 腳本庫 v3", "tw-01", "每天 20:00", U.status("ok", "啟用"), "o" * 24, "昨天 20:00 · 41 秒", "略過 · 補跑 · 重試 1")}
      {task("健康探測", "URL", "jp-02", "每 5 分鐘", U.status("warn", "過期"), "o" * 20 + "e" * 4, "03:41 上次觀測", "節點離線時本地續跑")}
      {task("ClamAV 掃描", "維護", "tw-01", "每週日 05:00", U.status("ok", "啟用"), "o" * 24, "11/02 · 0 感染", "略過 · 補跑")}
      {task("舊報表歸檔", "腳本", "tw-01", "每月 1 日", U.status("info", "暫停"), "o" * 24, "10/01", "—")}
    </section>"""
    deps = U.topology([
        ("先", [("a", "wiki-db 備份", "03:00", "ok"), ("b", "shop-db 備份", "03:30", "ok")]),
        ("再", [("c", "校驗與保留", "成功才跑", "ok"), ("d", "還原測試", "每週 · lab-mini", "dim")]),
        ("最後", [("e", "通知 Telegram", "失敗立刻 · 成功彙整", "ok")]),
    ], [("a", "c", .6, ""), ("b", "c", .6, ""), ("c", "d", .3, "dim"), ("c", "e", .6, ""), ("d", "e", .2, "dim")], w=560, box_w=150, box_h=42)
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
    head = U.page_head("安全防護", OBS, '<button type="button" class="btn pri">新增政策</button><button type="button" class="btn">更新 WAF 規則</button><button type="button" class="btn">匯出安全事件</button>')
    tiles = '<div class="tiles">' + "".join([
        U.stat_tile("WAF", "偵測", "3 站點", "0 阻擋 · 誤判率估 0.9%", None),
        U.stat_tile("今日封鎖", "14", "IP", "Fail2ban 11 · 手動 3", None, spark=[2, 1, 3, 0, 1, 4, 2, 1, 0, 2, 3, 1]),
        U.stat_tile("防護延遲", "1.8", "ms p95", "WAF 檢查加在每個請求上", True, spark=walk(12, 1.8, .3, 1.2, 2.6)),
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
      <div class="ph"><h2 id="h-fw2">主機層：Fail2ban、防火牆、ClamAV、SSH</h2><span class="sub">與節點頁同一份資料</span><a class="more" href="Nodes.dc.html">節點 →</a></div>
      <div style="padding:12px 14px">{U.matrix(["hazel-tw-01", "hazel-jp-02", "lab-mini"], ["Fail2ban", "防火牆", "Docker 直通 Port", "ClamAV", "SSH 密碼登入", "安全更新"], [
        [("ok", "11 封鎖"), ("ok", "ufw 啟用"), ("warn", "8080、11434"), ("ok", "0 感染"), ("warn", "允許"), ("ok", "無待套用")],
        [("warn", "上次觀測"), ("ok", "nftables"), ("dng", "5432 對外"), ("none", "未安裝"), ("ok", "禁止"), ("warn", "5 待套用")],
        [("ok", "0 封鎖"), ("ok", "ufw 啟用"), ("none", "無"), ("none", "未安裝"), ("ok", "僅金鑰"), ("warn", "2 待套用")],
      ])}</div>
    </section>"""
    main = head + U.scope_bar([("全部入口", "2 Zentinel"), ("Zentinel @ hazel-tw-01", ""), ("Zentinel @ hazel-jp-02", "過期")], label="入口") + tiles + modes + f'<div class="cg">{rollout}{hits}</div>' + f'<div class="cg">{policies}{check}</div>' + fw
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
      <div class="name"><a href="Nodes.dc.html" class="note">主機與節點 /</a><h1 class="mono">hazel-jp-02 · 系統</h1></div>
      {U.status("warn", "狀態過期")}<span class="note">最後聯絡 03:41:18 · 以下是上次觀測</span>
      <div class="wrap" style="margin-left: auto"><button type="button" class="btn">終端機</button><button type="button" class="btn">重新開機</button><button type="button" class="btn pri">套用 5 個安全更新</button></div>
    </div>"""
    tabs = ('<div class="panel"><div class="tabs" role="tablist">' + "".join(f'<button type="button" class="tab{" on" if i == 0 else ""}" role="tab" aria-selected="{"true" if i == 0 else "false"}">{t}</button>' for i, t in enumerate(["套件與更新", "服務與計時器", "使用者與 sudo", "網路", "掛載與檔案系統", "時間與主機名稱", "開關機"])) + '</div></div>')
    pk_tiles = '<div class="tiles">' + "".join([
        U.stat_tile("已安裝套件", "1,284", "", "apt · Debian 12 · 來源簽章有效", None),
        U.stat_tile("可更新", "23", "", "安全性 5 · 一般 18", False),
        U.stat_tile("維護時段", "03:00–04:00", "", "安全更新自動套用 · 需重開機的除外", None),
        U.stat_tile("上次更新", "10 月 29 日", "", "14 套件 · 成功 · 無需重開", None),
    ]) + "</div>"
    def pkg(name, cur, new, kind, restart, note):
        return (f'<a class="row srow" href="#p"><span class="name"><span class="n">{name}</span><span class="d">{note}</span></span><span>{kind}</span><span class="mono c3">{cur}</span><span class="mono c4">→ {new}</span><span class="c4">{restart}</span><span class="c5"></span><span></span></a>')
    pkgs = f"""
    <section class="panel" aria-labelledby="h-pk">
      <div class="ph"><h2 id="h-pk">可更新 · 23</h2><span class="sub">安全性優先；會重啟服務或需要重開機的標出來，套用走工作中心</span><div class="wrap" style="margin-left:auto"><label class="inp" style="min-width:220px"><span class="note">搜尋</span><input placeholder="套件名稱、描述（含未安裝）" style="background:transparent;border:0;flex:1;min-width:0"></label><span class="chip on">安全性 5</span><span class="chip">一般 18</span><span class="chip">已保留 1</span></div></div>
      <div class="row hd srow"><span>套件</span><span>類型</span><span class="c3">目前</span><span class="c4">新版</span><span class="c4">影響</span><span class="c5"></span><span></span></div>
      {pkg("openssl", "3.0.15-1", "3.0.15-1+deb12u2", U.status("dng", "安全性"), U.status("warn", "重啟 zentinel、sshd"), "CVE-2026-… · 高")}
      {pkg("libssl3", "3.0.15-1", "3.0.15-1+deb12u2", U.status("dng", "安全性"), U.status("warn", "同上"), "隨 openssl")}
      {pkg("linux-image-amd64", "6.1.112-1", "6.1.115-1", U.status("dng", "安全性"), U.status("dng", "需要重開機"), "核心 · 排進維護時段")}
      {pkg("curl", "7.88.1-10", "7.88.1-10+deb12u8", U.status("dng", "安全性"), U.status("ok", "無"), "")}
      {pkg("docker-ce", "27.1.0", "27.3.1", U.status("info", "一般"), U.status("dng", "重啟所有容器"), "需批准 · 已保留（hold）")}
      {pkg("postgresql-client-16", "16.4", "16.6", U.status("info", "一般"), U.status("ok", "無"), "")}
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
    <section class="panel" aria-labelledby="h-sv">
      <div class="ph"><h2 id="h-sv">systemd 服務與計時器 · 顯示 7 / 141</h2><span class="sub">啟停、開機啟用、設定檔編輯（有歷史）、日誌</span><div class="wrap" style="margin-left:auto"><span class="chip on">執行中 112</span><span class="chip">失敗 1</span><span class="chip">計時器 9</span><span class="chip">平台相關</span></div></div>
      <div class="row hd srow"><span>單元</span><span>狀態</span><span class="c3">開機</span><span class="c4">記憶體</span><span class="c4">CPU</span><span class="c5"></span><span></span></div>
      {svc("hazelminx-agent.service", U.status("warn", "執行中 · 48 秒沒心跳"), "服務", "啟用", (38, 128), "0.4%", "平台 · 勿停")}
      {svc("docker.service", U.status("ok", "執行中 · 12 天"), "服務", "啟用", (180, 1024), "1.2%", "3 容器")}
      {svc("zentinel.service", U.status("ok", "執行中 · 12 天"), "服務", "啟用", (94, 512), "1.8%", "入口")}
      {svc("sshd.service", U.status("ok", "執行中"), "服務", "啟用", (6, 64), "0%", "Port 22")}
      {svc("fail2ban.service", U.status("dng", "失敗 · 03:12"), "服務", "啟用", (0, 64), "—", "exit 1：jail 設定語法錯誤（第 41 行）")}
      {svc("apt-daily-upgrade.timer", U.status("ok", "下次 03:00"), "計時器", "啟用", (0, 1), "—", "安全更新")}
      {svc("hazelminx-backup.timer", U.status("ok", "下次 03:00"), "計時器", "啟用", (0, 1), "—", "wiki-db")}
    </section>"""
    users = f"""
    <section class="panel" aria-labelledby="h-us">
      <div class="ph"><h2 id="h-us">Linux 使用者與 sudo</h2><span class="sub">平台帳號與 Linux 帳號是兩回事；這裡是宿主機的</span></div>
      <div style="padding:12px 14px">{U.matrix(["root", "tim", "deploy", "www-data", "postgres（容器內）"], ["登入", "sudo", "SSH 金鑰", "群組", "最後登入"], [
        [("warn", "密碼 281 天"), ("ok", "全部"), ("ok", "1 把"), ("none", "root"), ("ok", "03:38 本機")],
        [("ok", "金鑰"), ("ok", "全部 · 免密"), ("ok", "2 把"), ("none", "sudo、docker"), ("ok", "03:38 203.0.113.7")],
        [("ok", "金鑰"), ("warn", "docker 群組＝root 等效"), ("ok", "1 把"), ("none", "docker"), ("ok", "昨天 · CI")],
        [("none", "不可登入"), ("none", "無"), ("none", ""), ("none", "www-data"), ("none", "—")],
        [("none", "不適用"), ("none", "不適用"), ("none", ""), ("none", "容器 UID 999"), ("none", "—")],
      ], first_col="minmax(120px,1fr)")}</div>
      <div class="note" style="padding:0 14px 10px">docker 群組等於 root：deploy 帳號建議改用平台的部署工作，不直接進 docker 群組</div>
    </section>"""
    net = f"""
    <section class="panel" aria-labelledby="h-net">
      <div class="ph"><h2 id="h-net">網路</h2><span class="sub">改 IP、DNS、路由走變更計畫；錯了會把自己鎖在外面的改動，先給復原方法</span></div>
      <div style="padding:12px 14px;display:flex;flex-direction:column;gap:10px">
        {U.matrix(["eth0", "docker0", "cloudflared（tunnel）", "wg0"], ["狀態", "IPv4", "IPv6", "閘道", "流量 24 小時"], [
          [("ok", "UP · 1 Gb"), ("none", "203.0.113.22/24"), ("none", "2001:db8::22/64"), ("none", "203.0.113.1"), ("ok", "進 71G · 出 284G")],
          [("ok", "UP"), ("none", "172.17.0.1/16"), ("none", "—"), ("none", "—"), ("ok", "容器間")],
          [("warn", "上次觀測 UP"), ("none", "—"), ("none", "—"), ("none", "—"), ("ok", "4 連線")],
          [("none", "DOWN"), ("none", "10.8.0.2/24"), ("none", "—"), ("none", "—"), ("none", "0")],
        ], first_col="minmax(140px,1.2fr)")}
        <div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(min(220px,100%),1fr));gap:10px">{kv("DNS", "1.1.1.1、8.8.8.8 · 搜尋網域 example.tw", "ncard")}{kv("預設路由", "203.0.113.1 via eth0 · metric 100", "ncard")}{kv("/etc/hosts", "6 條 · 含 wiki.internal", "ncard")}{kv("主機名稱", "hazel-jp-02 · FQDN hazel-jp-02.example.tw", "ncard")}</div>
      </div>
    </section>"""
    mounts = f"""
    <section class="panel" aria-labelledby="h-mnt">
      <div class="ph"><h2 id="h-mnt">掛載與檔案系統</h2><span class="sub">fstab、網路儲存、裝置身分；未綁定的資料碟標紅</span><a class="more" href="Storage.dc.html">儲存與備份 →</a></div>
      <div class="row hd drow"><span>掛載點</span><span>裝置</span><span class="c3">檔案系統</span><span>使用</span><span class="c4">開機掛載 · 身分</span></div>
      <div class="row drow"><span class="mono">/</span><span class="mono">nvme0n1p2</span><span class="c3">ext4</span>{U.meter(38, "36 / 94 GiB", warn=80, danger=90)}<span class="c4">{U.status("ok", "fstab UUID")}</span></div>
      <div class="row drow"><span class="mono">/mnt/data</span><span class="mono">sdb1（未綁定）</span><span class="c3">ext4</span>{U.meter(93, "870 / 931 GiB", warn=80, danger=90)}<span class="c4">{U.status("dng", "fstab 用 /dev/sdb1 · 掛錯不會擋")}</span></div>
      <div class="row drow"><span class="mono">/mnt/nfs/backup</span><span class="mono">hazel-tw-01:/mnt/data/nfs</span><span class="c3">nfs4</span>{U.meter(10, "19 / 200 GiB", warn=80, danger=90)}<span class="c4">{U.status("ok", "systemd automount")}</span></div>
      <div class="row drow"><span class="mono">swap</span><span class="mono">/swapfile</span><span class="c3">swap</span>{U.meter(12, "0.5 / 4 GiB", warn=80, danger=90)}<span class="c4">{U.status("ok", "fstab")}</span></div>
    </section>"""
    time_ = f"""
    <section class="panel" aria-labelledby="h-time">
      <div class="ph"><h2 id="h-time">時間、主機名稱、開關機</h2></div>
      <div class="fleet" style="grid-template-columns:repeat(auto-fill,minmax(min(220px,100%),1fr))">
        {kv("時區", "Asia/Taipei（節點）· 使用者顯示 Asia/Taipei", "ncard")}{kv("NTP", "chrony · pool.ntp.org · 偏差 +3 ms · 同步", "ncard")}{kv("主機名稱", "hazel-jp-02 · FQDN hazel-jp-02.example.tw", "ncard")}
        <div class="ncard"><span class="note">運行時間</span><span>41 天 · 上次重開 9 月 27 日（核心更新）</span><span class="note">待重開機：核心 6.1.115</span></div>
        <div class="ncard"><span class="note">重新開機會影響</span><span>{U.segmented([(3, "var(--danger)"), (0, "var(--q2)")])}</span><span class="note">wiki-app、wiki-db、zentinel · 排程 03:00 備份將延後</span></div>
        <div class="ncard"><span class="note">root 密碼</span><span>{U.status("warn", "281 天未換")}</span><span class="note">上次變更 2 月 1 日</span></div>
      </div>
    </section>"""
    main = head + U.scope_bar([("hazel-tw-01", "Ubuntu 24.04"), ("hazel-jp-02", "Debian 12 · 過期"), ("lab-mini", "Debian 13")], current=1, label="節點") + tabs + pk_tiles + pkgs + upd + services + f'<div class="cg">{users}{net}</div>' + mounts + time_
    return U.shell("nodes", main)


# ---------- 身分與存取 ----------
def iam() -> str:
    head = U.page_head("身分與存取", OBS, '<button type="button" class="btn pri">新增使用者</button><button type="button" class="btn">新增角色</button><button type="button" class="btn">發 API Token</button><button type="button" class="btn">外部登入</button>')
    tiles = '<div class="tiles">' + "".join([
        U.stat_tile("使用者", "5", "", "4 啟用 · 1 停權", None),
        U.stat_tile("二次驗證", "3", "/4 已啟用", "analyst 未設 · 高權限帳號必須", False),
        U.stat_tile("工作階段", "7", "", "tim 3 · deploy-bot 2 · 其他 2", None),
        U.stat_tile("待批准", "1", "", "變更計畫 #418 · 等了 44 分", None),
    ]) + "</div>"
    def user(name, role, mfa, sess, last, st):
        return (f'<a class="row srow" href="#u"><span class="name"><span class="n">{name}</span><span class="d">{role}</span></span><span>{st}</span><span class="c3">{mfa}</span><span class="mono c4">{sess}</span><span class="mono c4">{last}</span><span class="c5"></span><span></span></a>')
    users = f"""
    <section class="panel" aria-labelledby="h-usr">
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
    head = U.page_head("平台設定", OBS, '<button type="button" class="btn">匯出設定</button><button type="button" class="btn">診斷包</button><button type="button" class="btn pri">檢查更新</button>')
    tabs = ('<div class="panel"><div class="tabs" role="tablist">' + "".join(f'<button type="button" class="tab{" on" if i == 0 else ""}" role="tab" aria-selected="{"true" if i == 0 else "false"}">{t}</button>' for i, t in enumerate(["基本", "管理入口與安全", "通知", "更新", "快照與匯入匯出", "整合", "遙測", "關於"])) + '</div></div>')
    def card(title, rows, note=""):
        body = "".join(f'<div style="display:flex;justify-content:space-between;gap:10px;min-height:32px;align-items:center;border-top:1px solid var(--line);padding:4px 0"><span class="note">{k}</span><span style="text-align:right">{v}</span></div>' for k, v in rows)
        n = f'<span class="note" style="margin-top:6px">{note}</span>' if note else ""
        return f'<div class="ncard"><span style="font-weight:500">{title}</span>{body}{n}</div>'
    basic = f"""
    <section class="panel" aria-labelledby="h-bs">
      <div class="ph"><h2 id="h-bs">基本</h2><span class="sub">每個設定有作用範圍：平台／節點／使用者</span></div>
      <div class="fleet">
        {card("面板", [("名稱", "HazelminX（榛果）"), ("語言", "繁體中文 · 英文可切"), ("外部位址", "https://hazel.example.tw"), ("預設節點", "hazel-tw-01")], "品牌、favicon、登入頁、郵件、CLI、空畫面都是 HazelminX")}
        {card("時區", [("使用者顯示", "Asia/Taipei（tim）"), ("節點", "tw-01 Taipei · jp-02 Taipei · lab Taipei"), ("儲存", "UTC")], "節點時區與顯示時區分開，不混用")}
        {card("登入與工作階段", [("逾時", "8 小時 · 手機 5 小時"), ("長工作", "不因登入過期取消"), ("目前工作階段", "7 · 可撤銷")])}
      </div>
    </section>"""
    entrance = f"""
    <section class="panel" aria-labelledby="h-en">
      <div class="ph"><h2 id="h-en">管理入口與 HTTPS</h2><span class="sub">改入口前先確認可達，新入口可用前保留舊的；不會把自己鎖在外面</span></div>
      <div class="fleet">
        {card("監聽", [("位址", "0.0.0.0:8443 · tw-01"), ("網域", "hazel.example.tw"), ("憑證", "自簽 · 內部 CA · 83 天"), ("衝突檢查", U.status("ok", "無"))])}
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
    main = head + tabs + basic + entrance + notify + update + f'<div class="cg">{snap}{integ}</div>'
    return U.shell("settings", main)


# ---------- 日誌 ----------
def logs() -> str:
    head = U.page_head("日誌", OBS, '<button type="button" class="btn">保留政策</button><button type="button" class="btn">匯出</button><button type="button" class="btn pri">診斷包</button>')
    tabs = ('<div class="panel"><div class="tabs" role="tablist">' + "".join(f'<button type="button" class="tab{" on" if i == 0 else ""}" role="tab" aria-selected="{"true" if i == 0 else "false"}">{t}</button>' for i, t in enumerate(["操作稽核", "登入", "工作", "主機系統（journald）", "SSH 登入", "網站存取／錯誤", "平台後端"])) + '</div></div>')
    vol = U.ts_card("日誌量 · 每小時 · 全部來源", [U.Series("網站存取", [int(x) for x in walk(24, 3800, 900, 1000, 6000)], "var(--s1)"), U.Series("主機系統", [int(x) for x in walk(24, 400, 120, 100, 900, {22: 1800})], "var(--s2)"), U.Series("平台", [int(x) for x in walk(24, 120, 40, 20, 300)], "var(--s3)"), U.Series("稽核", [int(x) for x in walk(24, 6, 3, 0, 20)], "var(--s4)")],
                    HOURS, unit="", fmt_val=lambda v: f"{v / 1000:.1f}K" if v >= 1000 else f"{v:.0f}", markers=[(22, "jp-02 失聯")], foot="7 天、500 萬筆的查詢 p95 < 3 秒；太大的查詢先回「要不要改成工作查詢」· 保留：稽核 1 年、存取 30 天、系統 14 天")
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
    <section class="panel" aria-labelledby="h-la">
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
    main = head + U.scope_bar([("全部節點", "3"), ("hazel-tw-01", "Docker"), ("hazel-jp-02", "Docker · 過期"), ("lab-mini", "Podman · K3s")]) + tabs + vol + viewer + audit + f'<div class="cg">{ssh}{web}</div>'
    return U.shell("logs", main)


# ---------- 終端機 ----------
def terminal() -> str:
    head = U.page_head("終端機", OBS, '<button type="button" class="btn pri">新連線</button><button type="button" class="btn">快捷指令</button><button type="button" class="btn">設定</button>')
    tabs = ('<div class="panel"><div class="tabs" role="tablist"><button type="button" class="tab on" role="tab" aria-selected="true">hazel-tw-01 · root</button><button type="button" class="tab" role="tab" aria-selected="false">shop-api（容器）</button>'
            '<button type="button" class="tab" role="tab" aria-selected="false">lab-mini · deploy</button><button type="button" class="tab" role="tab" aria-selected="false">＋</button><span style="margin-left:auto;display:flex;align-items:center;gap:8px;padding-right:10px">'
            + U.status("dng", "這個終端機代表 root@hazel-tw-01 · 稽核：完整錄製") + '<span class="chip">縮到底部 Dock</span><span class="chip">全螢幕</span></span></div></div>')
    term = f"""
    <section class="panel" aria-labelledby="h-tm">
      <div class="ph"><h2 id="h-tm">hazel-tw-01 · root · 會話 #3 · 開始 03:36 · 心跳正常</h2><span class="sub">經 Agent，不走 sshd；切到別的頁面回來輸出仍完整</span></div>
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
      <a class="row srow" href="#h1"><span class="name"><span class="n">hazel-tw-01</span><span class="d">節點</span></span><span>{U.status("ok", "連線中")}</span><span class="c3 note">Agent</span><span class="mono c4">root · tim 可</span><span class="c4 note">完整錄製</span><span class="c5"></span><span><button type="button" class="btn sm">開啟</button></span></a>
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
    "Nodes": ("主機與節點", nodes, 1440, 2600),
    "System": ("宿主機系統 hazel-jp-02", system, 1440, 2600),
    "Terminal": ("終端機", terminal, 1440, 1800),
    "Kubernetes": ("Kubernetes", kubernetes, 1440, 2200),
    "AppStore": ("應用商店", appstore, 1440, 2400),
    "APIProxy": ("API 代理", apiproxy, 1440, 2200),
    "Database": ("資料庫", database, 1440, 2600),
    "AI": ("AI", ai, 1440, 2600),
    "Files": ("檔案", files, 1440, 2200),
    "Cron": ("計畫任務", cron, 1440, 2600),
    "Security": ("安全防護", security, 1440, 2400),
    "Analytics": ("流量分析", analytics, 1440, 2400),
    "IAM": ("身分與存取", iam, 1440, 2400),
    "Settings": ("平台設定", settings, 1440, 2600),
    "Logs": ("日誌", logs, 1440, 2200),
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


BANNED = ["下一步", "建議", "可能", "若不是", "不假裝", "無關", "暖機", "別手動", "先別", "才算", "不能取消的原因", "不等於", "就在這", "一眼"]


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", default=os.path.join(os.path.dirname(__file__), "..", "screens"))
    ap.add_argument("--render-dir", default=None)
    args = ap.parse_args()
    os.makedirs(args.out, exist_ok=True)
    if args.render_dir:
        os.makedirs(args.render_dir, exist_ok=True)
    for name, (title, fn, w, h) in SCREENS.items():
        body = trim_columns(declutter(fn()))
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
