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

    def node_row(name, desc, st, cpu, mem, memt, disk, diskt, spark, last, lastcls="", stale=False, last_border=False):
        cpu_txt = f"{cpu:.0f}%" + ("*" if stale else "")
        return (f'<a class="row nrow" href="NodeDetail.dc.html"><span class="name"><span class="n">{name}</span><span class="d">{desc}</span></span>'
                f'<span>{st}</span><span class="c3">{U.meter(cpu, cpu_txt, aria=f"{name} CPU")}</span>'
                f'<span class="c4">{U.meter(mem / memt * 100, f"{mem:.1f}/{memt:.0f}G", aria=f"{name} 記憶體")}</span>'
                f'<span class="c5">{U.meter(disk / diskt * 100, f"{disk}/{diskt}G", warn=80, danger=90, aria=f"{name} 資料碟")}</span>'
                f'<span class="c5">{U.sparkline(spark, stale=stale)}</span><span class="mono" style="{lastcls}">{last}</span></a>')

    nodes = f"""
    <section id="nodes" class="panel" aria-labelledby="h-nodes">
      <div class="ph"><h2 id="h-nodes">節點</h2><span class="sub">離線不等於應用已停止；過期資料標示上次觀測</span><a class="more" href="NodeDetail.dc.html">全部 →</a></div>
      <div class="row hd nrow"><span>節點</span><span>狀態</span><span class="c3">CPU</span><span class="c4">記憶體</span><span class="c5">資料碟</span><span class="c5">CPU 1 小時</span><span>最後聯絡</span></div>
      {node_row("hazel-tw-01", "Ubuntu 24.04 · 控制中心 · Docker · 8 核", U.status("ok", "連線中"), 37, 11.2, 31.3, 412, 931, CPU_TW[-12:], "2 秒前")}
      {node_row("hazel-jp-02", "Debian 12 · Docker · 4 核 · 資料碟未綁定", U.status("warn", "狀態過期"), 64, 13.9, 15.5, 870, 931, CPU_JP[-12:], "03:41:18", "color: var(--warn)", stale=True)}
      {node_row("lab-mini", "Debian 13 · Podman rootless（lab） · 4 核", U.status("info", "維護中"), 3, 1.1, 7.6, 38, 118, CPU_LAB[-12:], "5 秒前")}
      <div class="note" style="padding: 8px 14px; border-top: 1px solid var(--line)">* 上次觀測值，節點已過期 · 量表門檻：70% 警告、90% 嚴重（磁碟 80／90）</div>
    </section>"""

    projects = f"""
    <section id="projects" class="panel" aria-labelledby="h-proj">
      <div class="ph"><h2 id="h-proj">專案與服務</h2><a class="more" href="Projects.dc.html">全部 →</a></div>
      <div class="row hd prow"><span>專案 · 環境</span><span>最近部署</span><span class="c3">服務</span><span class="c4">節點</span><span class="c5">入口</span><span class="c5">近 24 次部署</span></div>
      <a class="row prow" href="Projects.dc.html"><span><strong>shop</strong> <span style="color: var(--muted)">· production</span></span><span><span class="mono">a3f9c1e</span> {U.status("ok", "成功")} <span class="note">02:57</span></span><span class="mono c3">4</span><span class="mono c4">tw-01</span><span class="c5" style="color: var(--muted)">shop.example.tw</span><span class="c5">{U.state_timeline("oooooooofoooooooooooooro")}</span></a>
      <a class="row prow" href="Projects.dc.html"><span><strong>wiki</strong> <span style="color: var(--muted)">· production</span></span><span><span class="mono">7d02b4a</span> {U.status("ok", "成功")} <span class="note">1 天前</span></span><span class="mono c3">2</span><span class="mono c4">jp-02</span><span class="c5" style="color: var(--muted)">wiki.example.tw</span><span class="c5">{U.state_timeline("oooooooooooooooooooooooo")}</span></a>
      <a class="row prow" href="Projects.dc.html"><span><strong>shop</strong> <span style="color: var(--muted)">· staging</span></span><span><span class="mono">c91e0d7</span> {U.status("dng", "失敗")} <span class="note">migration 不相容</span></span><span class="mono c3">4</span><span class="mono c4">lab-mini</span><span class="c5" style="color: var(--muted)">staging.shop…</span><span class="c5">{U.state_timeline("oooofooooooooofoooooffff")}</span></a>
    </section>"""

    storage = f"""
    <section id="storage" class="panel" aria-labelledby="h-sto">
      <div class="ph"><h2 id="h-sto">儲存與備份</h2><span class="sub">裝置身分綁定讓重開機掛錯磁碟時拒絕啟動</span></div>
      <div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(min(260px, 100%), 1fr)); gap: 1px; background: var(--line)">
        <div style="display: flex; flex-direction: column; gap: 8px; padding: 12px 14px; background: var(--panel)"><span class="mono" style="font-weight: 500">hazel-tw-01 /mnt/data</span>{U.status("ok", "已綁定裝置身分（UUID）")}{U.meter(44, "412 / 931 GiB", warn=80, danger=90)}<span class="note">30 天增長 +38 GiB · 預估 410 天後滿</span></div>
        <div style="display: flex; flex-direction: column; gap: 8px; padding: 12px 14px; background: var(--panel)"><span class="mono" style="font-weight: 500">hazel-jp-02 /mnt/data</span>{U.status("dng", "未綁定 · 重開機不會阻止掛錯磁碟")}{U.meter(93, "870 / 931 GiB", warn=80, danger=90)}<span class="note" style="color: var(--danger)">30 天增長 +121 GiB · 預估 15 天後滿</span></div>
        <div style="display: flex; flex-direction: column; gap: 8px; padding: 12px 14px; background: var(--panel)"><span style="font-weight: 500">最近備份 wiki-db</span><span class="note">驗證層級</span>{U.steps([("已產生", "done", "03:00"), ("已傳輸", "done", "S3 tokyo"), ("校驗通過", "done", "sha256"), ("還原測試", "todo", "尚未")])}</div>
      </div>
    </section>"""

    main = (U.page_head("總覽", OBS, '<button type="button" class="btn pri">新增節點</button><button type="button" class="btn">部署專案</button>',
                        " · 自動更新每 10 秒") + tiles + charts + nodes + projects + storage)
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
      {kv("作業系統", "Ubuntu 24.04.2 LTS")}{kv("核心", "6.8.0-45-generic")}{kv("CPU", "8 核 · AMD EPYC 7B13")}{kv("記憶體", "31.3 GiB")}{kv("執行階段", "Docker 27.3.1")}{kv("Agent", "hazelminx-agent 0.1.0 · mTLS")}{kv("角色", "控制中心 · 資料節點")}{kv("GPU", "無")}
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
    temp = U.ts_card("溫度 · CPU 封裝", [U.Series("溫度", TEMP, "var(--hazel)", kind="area")], HOURS, unit="°C", vmax=90,
                     fmt_val=lambda v: f"{v:.0f}°", foot="門檻 85°C 告警 · 來源 hwmon k10temp")
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
    main = head + range_row() + f'<div class="cg">{cpu}{mem}{io}{net}{load}{temp}</div><div class="cg">{disks}{hot}</div>'
    return U.shell("nodes", main)


# ---------- 專案與服務 ----------
def projects() -> str:
    head = U.page_head("專案與服務", OBS, '<button type="button" class="btn pri">新增專案</button><button type="button" class="btn">從 GitHub 匯入</button>')
    tabs = ('<div class="panel"><div class="tabs" role="tablist"><button type="button" class="tab on" role="tab" aria-selected="true">shop · production</button>'
            '<button type="button" class="tab" role="tab" aria-selected="false">shop · staging</button><button type="button" class="tab" role="tab" aria-selected="false">wiki · production</button></div>'
            f'<div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(min(170px, 100%), 1fr)); gap: 10px 16px; padding: 12px 14px; font-size: 13px">'
            f'{kv("來源", "github.com/hazelnut/shop · main")}{kv("最近部署", "a3f9c1e · 02:57 · 成功 · Webhook")}{kv("節點", "hazel-tw-01")}{kv("入口", "shop.example.tw · 憑證 11 天後到期")}{kv("自動部署", "已暫停：變更計畫 #418 待批准")}</div></div>')
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
    main = head + tabs + services + f'<div class="cg">{rq_card}{er_card}</div>{lat_card}' + deploys
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
    main = head + tiles + filt + table + f'<div class="cg">{heat}{hot}</div>'
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
        return (f'<article class="job" style="gap: 8px"><div class="t"><span style="font-weight: 500">{title}</span>{st}</div>'
                f'<div class="s">{node}</div>{U.steps(step_items)}{prog}<div class="s">{extra}</div><div class="wrap">{actions}</div></article>')
    running = f"""
    <section class="panel" aria-labelledby="h-run">
      <div class="ph"><h2 id="h-run">進行中 · 3</h2><span class="sub">長操作都是工作：關掉瀏覽器也繼續；狀態不明的工作在重連後對帳</span></div>
      <div class="list" style="padding: 12px 14px">
        {job("部署 shop-api → production（#403 之後的 #404 熱修）", "hazel-tw-01 · 由 Webhook 觸發 · 開始 03:40:12 · 心跳 2 秒前", U.status("info", "驗證中"),
             [("預檢", "done", "18 秒"), ("批准", "done", "自動（無高風險變更）"), ("執行", "done", "1 分 02 秒"), ("驗證", "cur", "健康檢查 2／3")],
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
