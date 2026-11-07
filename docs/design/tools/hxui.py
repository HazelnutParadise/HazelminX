"""HazelminX 設計原型的共用元件：token CSS、導覽、圖表（純 SVG，CSS hover）。

所有圖表只輸出靜態 SVG 標記，不用 JavaScript；hover 提示用 CSS。
數值全部是示例資料（假設），由 build_screens.py 以固定種子產生。
"""
from __future__ import annotations

import math
from dataclasses import dataclass, field

FONT_LINK = ('<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Geist:wght@400;500;600'
             '&family=Geist+Mono:wght@400;500&family=Noto+Sans+TC:wght@400;500;700&display=swap">')

DARK = dict(bg="#161412", panel="#1D1B18", raised="#262320", line="#35312B", ink="#ECE6DA", muted="#A79E8E",
            faint="#9A917F", hazel="#D9A066", hazelink="#1A1410", blue="#8DB4F2", warn="#F0B35B",
            danger="#F28B82", ok="#9BD3A4", s1="#AF711F", s2="#5682BB", s3="#47AA62", s4="#B55B9A",
            q1="#2A241C", q2="#4A3A22", q3="#6E5228", q4="#9A6F33", q5="#D9A066", scrim="rgba(0,0,0,.45)")
LIGHT = dict(bg="#F6F2EA", panel="#FFFDF8", raised="#EFE9DE", line="#D9D1C2", ink="#1B1814", muted="#5E574C",
             faint="#6B6457", hazel="#8F5B23", hazelink="#FFFFFF", blue="#2E5FA8", warn="#9A5A0B",
             danger="#B4382E", ok="#2F6B3C", s1="#7C4B02", s2="#2E598F", s3="#218A45", s4="#883170",
             q1="#F3E6D3", q2="#E3C9A4", q3="#CFA772", q4="#B38347", q5="#8F5B23", scrim="rgba(0,0,0,.25)")


def _vars(t: dict) -> str:
    return ";".join(f"--{k}:{v}" for k, v in t.items())


BASE_CSS = f"""
body{{margin:0}}
.hb{{{_vars(DARK)};font-family:"Geist","Noto Sans TC","PingFang TC","Microsoft JhengHei",sans-serif;font-synthesis:none;line-break:strict;color:var(--ink);background:var(--bg);font-size:14px;line-height:1.5;color-scheme:dark}}
.hb.theme-light{{{_vars(LIGHT)};color-scheme:light}}
.hb *{{box-sizing:border-box}}
.hb .mono{{font-family:"Geist Mono",ui-monospace,Menlo,monospace;font-variant-numeric:tabular-nums}}
.hb a{{color:var(--ink);text-decoration:none}}.hb a:hover{{color:var(--hazel)}}
.hb button,.hb input,.hb select{{font:inherit;color:inherit}}
.hb button{{cursor:pointer}}
.hb :is(a,button,input,select,[tabindex]):focus-visible{{outline:2px solid var(--hazel);outline-offset:2px}}
.hb h1{{margin:0;font-size:20px;font-weight:600;line-height:1.3}}
.hb h2{{margin:0;font-size:14px;font-weight:600;line-height:1.4}}
.hb p{{margin:0}}
.hb .btn{{display:inline-flex;align-items:center;justify-content:center;gap:6px;min-height:36px;padding:0 12px;border-radius:6px;border:1px solid var(--line);background:transparent;color:var(--ink);font-size:13px;white-space:nowrap}}
.hb .btn.pri{{background:var(--hazel);border-color:var(--hazel);color:var(--hazelink);font-weight:600}}
.hb .btn.dng{{background:var(--danger);border-color:var(--danger);color:var(--hazelink);font-weight:600}}
.hb .btn.sm{{min-height:32px;padding:0 10px;font-size:12px;color:var(--muted)}}
.hb .btn:active{{transform:translateY(1px)}}
.hb .panel{{background:var(--panel);border:1px solid var(--line);border-radius:8px;overflow:hidden}}
.hb .ph{{display:flex;align-items:center;gap:10px;padding:10px 14px;border-bottom:1px solid var(--line);flex-wrap:wrap}}
.hb .ph .sub{{font-size:12px;color:var(--muted)}}
.hb .ph .more{{margin-left:auto;font-size:13px;color:var(--hazel)}}
.hb .card{{background:var(--raised);border:1px solid var(--line);border-radius:8px;padding:12px;display:flex;flex-direction:column;gap:6px}}
.hb .row{{display:grid;gap:12px;align-items:center;min-height:44px;padding:0 14px;border-bottom:1px solid var(--line)}}
.hb .row:hover{{background:var(--raised)}}
.hb .row:last-child{{border-bottom:0}}
.hb .hd{{font-size:11px;color:var(--faint);letter-spacing:.06em;text-transform:uppercase;min-height:34px;font-weight:500}}
.hb .hd:hover{{background:transparent}}
.hb .name{{display:flex;flex-direction:column;gap:2px;min-width:0}}
.hb .name .n{{font-family:"Geist Mono",ui-monospace,monospace;font-weight:500;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}}
.hb .name .d{{font-size:12px;color:var(--faint);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}}
.hb .st{{display:inline-flex;align-items:center;gap:6px;white-space:nowrap}}
.hb .st i{{display:inline-block;width:8px;height:8px;flex:none}}
.hb .st.ok{{color:var(--ok)}}.hb .st.ok i{{border-radius:50%;background:var(--ok)}}
.hb .st.warn{{color:var(--warn)}}.hb .st.warn i{{border-radius:50%;border:2px solid var(--warn)}}
.hb .st.dng{{color:var(--danger)}}.hb .st.dng i{{background:var(--danger)}}
.hb .st.info{{color:var(--blue)}}.hb .st.info i{{background:var(--blue)}}
.hb .st.off{{color:var(--muted)}}.hb .st.off i{{border-radius:50%;border:2px solid var(--muted);border-style:dashed}}
.hb .nv{{display:flex;align-items:center;gap:10px;min-height:40px;padding:0 10px;border-radius:6px;font-size:14px;color:var(--ink)}}
.hb .nv:hover{{background:var(--raised);color:var(--ink)}}
.hb .nv.cur{{background:var(--raised);color:var(--hazel);font-weight:600}}
.hb .nv .cnt{{margin-left:auto;font-family:"Geist Mono",ui-monospace,monospace;font-size:12px}}
.hb .kbd{{font-family:"Geist Mono",ui-monospace,monospace;font-size:11px;color:var(--faint);border:1px solid var(--line);border-radius:4px;padding:1px 5px;margin-left:auto}}
.hb .navgrp{{font-size:11px;color:var(--faint);letter-spacing:.06em;padding:6px 10px 0}}
.hb .navcol{{display:flex;flex-direction:column;gap:2px}}
.hb .tiles{{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(170px,100%),1fr));gap:10px}}
.hb .tile{{display:flex;flex-direction:column;gap:4px;padding:12px 14px;background:var(--panel);border:1px solid var(--line);border-radius:8px;min-width:0}}
.hb .tile .l{{font-size:12px;color:var(--muted)}}
.hb .tile .v{{font-size:28px;font-weight:600;line-height:1.1;display:flex;align-items:baseline;gap:6px;flex-wrap:wrap}}
.hb .tile .v small{{font-size:12px;font-weight:400;color:var(--muted)}}
.hb .tile .dl{{font-size:12px;color:var(--muted);display:flex;align-items:center;gap:8px;flex-wrap:wrap}}
.hb .up{{color:var(--ok)}}.hb .dn{{color:var(--danger)}}
.hb .meter{{display:flex;align-items:center;gap:8px;min-width:0}}
.hb .meter .tr{{flex:1;height:6px;border-radius:4px;background:var(--q2);overflow:hidden;min-width:40px}}
.hb .meter .fl{{height:100%;border-radius:4px;background:var(--hazel)}}
.hb .meter .fl.warn{{background:var(--warn)}}.hb .meter .fl.dng{{background:var(--danger)}}
.hb .meter .num{{font-family:"Geist Mono",ui-monospace,monospace;font-variant-numeric:tabular-nums;font-size:12px;white-space:nowrap}}
.hb .cg{{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(440px,100%),1fr));gap:12px}}
.hb .chart{{background:var(--panel);border:1px solid var(--line);border-radius:8px;overflow:hidden;min-width:0}}
.hb .chart .ph{{padding:8px 12px}}
.hb .chart .unit{{font-size:12px;color:var(--faint)}}
.hb .chart .legend{{display:flex;gap:12px;flex-wrap:wrap;font-size:12px;color:var(--muted);margin-left:auto}}
.hb .legend i{{display:inline-block;width:12px;height:2px;vertical-align:middle;margin-right:5px;border-radius:1px}}
.hb .legend i.rect{{height:8px;border-radius:2px}}
.hb .chart .body{{padding:6px 10px 8px}}
.hb .foot{{display:flex;gap:10px;flex-wrap:wrap;padding:6px 12px 8px;font-size:12px;color:var(--faint);border-top:1px solid var(--line)}}
.hb .chart .foot a{{color:var(--hazel)}}
.hb svg.ch{{display:block;width:100%;height:auto;font-family:"Geist Mono",ui-monospace,monospace;font-variant-numeric:tabular-nums}}
.hb svg.ch text{{font-size:11px;fill:var(--faint)}}
.hb .grid{{stroke:var(--line);stroke-width:1}}
.hb .hit .xh,.hb .hit .tip{{opacity:0;transition:opacity .08s}}
.hb .hit:hover .xh,.hb .hit:hover .tip,.hb .hit:focus-within .xh,.hb .hit:focus-within .tip{{opacity:1}}
.hb .tip rect{{fill:var(--raised);stroke:var(--line)}}
.hb .tip text{{fill:var(--ink)}}
.hb .tip text.k{{fill:var(--muted)}}
.hb .ch-m{{display:none}}
.hb .ranks{{display:flex;flex-direction:column;gap:6px;padding:10px 14px}}
.hb .rank{{display:grid;grid-template-columns:minmax(0,1.2fr) minmax(0,2fr) auto;gap:10px;align-items:center;font-size:13px;min-height:28px}}
.hb .rank .bar{{height:8px;border-radius:4px;background:var(--q2);overflow:hidden}}
.hb .rank .bar i{{display:block;height:100%;border-radius:4px;background:var(--s1)}}
.hb .timeline{{display:flex;gap:2px;height:10px}}
.hb .timeline i{{flex:1;border-radius:2px;background:var(--ok)}}
.hb .timeline i.f{{background:var(--danger)}}.hb .timeline i.r{{background:var(--warn)}}.hb .timeline i.p{{background:var(--blue)}}.hb .timeline i.e{{background:var(--q2)}}
.hb .seg{{display:flex;gap:2px;height:8px;border-radius:4px;overflow:hidden}}
.hb .seg i{{display:block;height:100%}}
.hb .steps{{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:4px}}
.hb .step{{display:flex;flex-direction:column;gap:4px;font-size:12px;color:var(--muted)}}
.hb .step b{{display:block;height:6px;border-radius:3px;background:var(--q2)}}
.hb .step.done b{{background:var(--ok)}}.hb .step.cur b{{background:var(--blue)}}.hb .step.wait b{{background:var(--warn)}}.hb .step.fail b{{background:var(--danger)}}
.hb .step.cur{{color:var(--ink)}}
.hb .heat{{display:grid;gap:2px}}
.hb .heat i{{display:block;aspect-ratio:1.6;border-radius:2px;min-height:14px}}
.hb .heat .lab{{font-size:12px;color:var(--muted);display:block;line-height:22px;font-family:"Geist Mono",ui-monospace,monospace;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}}
.hb .range{{display:flex;gap:4px;flex-wrap:wrap}}
.hb .range .btn{{min-height:32px;padding:0 10px;font-size:12px;color:var(--muted)}}
.hb .range .btn.on{{background:var(--raised);color:var(--hazel);border-color:var(--line);font-weight:600}}
.hb .field{{display:flex;flex-direction:column;gap:6px}}
.hb .field label{{font-size:12px;color:var(--muted)}}
.hb .inp{{display:flex;align-items:center;gap:8px;min-height:40px;padding:0 10px;background:var(--raised);border:1px solid var(--line);border-radius:6px;color:var(--ink);width:100%}}
.hb .inp.err{{border-color:var(--danger)}}
.hb .help{{font-size:12px;color:var(--faint)}}
.hb .errt{{font-size:12px;color:var(--danger)}}
.hb .skel{{background:var(--raised);border-radius:4px;position:relative;overflow:hidden}}
.hb .skel::after{{content:"";position:absolute;inset:0;transform:translateX(-100%);background:linear-gradient(90deg,transparent,rgba(255,255,255,.06),transparent)}}
@media (prefers-reduced-motion:no-preference){{.hb .skel::after{{animation:hx-sh 1.4s infinite}}}}
@keyframes hx-sh{{to{{transform:translateX(100%)}}}}
.hb .dim{{opacity:.45}}
.hb .banner{{display:flex;gap:10px;align-items:center;flex-wrap:wrap;padding:10px 14px;border:1px solid var(--line);border-radius:8px;background:var(--raised);font-size:13px}}
.hb .tabs{{display:flex;gap:2px;border-bottom:1px solid var(--line);padding:0 14px}}
.hb .tab{{min-height:40px;display:inline-flex;align-items:center;padding:0 10px;border-bottom:2px solid transparent;color:var(--muted);font-size:13px;background:transparent;border-top:0;border-left:0;border-right:0}}
.hb .tab.on{{color:var(--hazel);border-bottom-color:var(--hazel);font-weight:600}}
.hb .diff{{overflow-x:auto;font-family:"Geist Mono",ui-monospace,monospace;font-size:12px;line-height:1.6;display:flex;flex-direction:column}}
.hb .diff div{{display:grid;grid-template-columns:36px 36px 1fr;gap:0;padding:0 14px;white-space:pre}}
.hb .diff .ln{{color:var(--faint);text-align:right;padding-right:10px}}
.hb .diff .add{{background:color-mix(in oklab,var(--ok) 14%,transparent)}}
.hb .diff .del{{background:color-mix(in oklab,var(--danger) 14%,transparent)}}
.hb .diff .hot{{outline:1px solid var(--danger);outline-offset:-1px}}
.hb .check{{display:grid;grid-template-columns:20px minmax(0,1fr) auto;gap:10px;align-items:start;padding:10px 14px;border-bottom:1px solid var(--line);font-size:13px}}
.hb .check:last-child{{border-bottom:0}}
.hb .check .mk{{width:16px;height:16px;border-radius:50%;margin-top:2px}}
.hb .check .mk.ok{{background:var(--ok)}}.hb .check .mk.warn{{border:3px solid var(--warn)}}.hb .check .mk.dng{{background:var(--danger);border-radius:2px}}
.hb .check .ev{{font-size:12px;color:var(--muted);font-family:"Geist Mono",ui-monospace,monospace}}
.hb .modal{{background:var(--raised);border:1px solid var(--line);border-radius:8px;padding:16px;display:flex;flex-direction:column;gap:10px;max-width:520px}}
.hb .scrim{{background:var(--scrim);padding:24px;display:flex;justify-content:center}}
.hb .list{{display:flex;flex-direction:column;gap:8px}}
.hb .job{{display:flex;flex-direction:column;gap:6px;padding:10px 12px;border:1px solid var(--line);border-radius:8px}}
.hb .job .t{{display:flex;justify-content:space-between;gap:8px}}
.hb .job .s{{font-size:12px;color:var(--muted)}}
.hb .prog{{height:4px;background:var(--q2);border-radius:2px;overflow:hidden}}
.hb .prog i{{display:block;height:100%;background:var(--blue)}}
.hb .prog.pulse i{{width:30%;background:repeating-linear-gradient(90deg,var(--blue) 0 8px,transparent 8px 12px)}}
.hb .note{{font-size:12px;color:var(--faint)}}
.hb .wrap{{display:flex;flex-wrap:wrap;gap:8px;align-items:center}}
.hb .scroll{{overflow-x:auto}}
.hb .aside{{width:340px;border-left:1px solid var(--line);background:var(--panel);display:flex;flex-direction:column;gap:18px;padding:18px 16px 24px}}
.hb .main{{flex:999 1 560px;min-width:0;display:flex;flex-direction:column;gap:22px;padding:18px 24px 40px}}
.hb .side{{width:232px;border-right:1px solid var(--line);background:var(--panel);display:flex;flex-direction:column;gap:14px;padding:14px 10px 20px}}
.hb .nrow{{grid-template-columns:minmax(0,1.5fr) minmax(0,.9fr) minmax(0,.9fr) minmax(0,1fr) minmax(0,1fr) 110px minmax(0,.8fr)}}
.hb .prow{{grid-template-columns:minmax(0,1.2fr) minmax(0,1.5fr) minmax(0,.5fr) minmax(0,.6fr) minmax(0,1fr) minmax(0,1.1fr)}}
.hb .srow{{grid-template-columns:minmax(0,1.5fr) minmax(0,.9fr) minmax(0,.9fr) minmax(0,.9fr) minmax(0,.9fr) 110px minmax(0,.7fr)}}
.hb .crow{{grid-template-columns:minmax(0,1.5fr) minmax(0,1fr) minmax(0,.5fr) minmax(0,.9fr) minmax(0,.9fr) minmax(0,1fr) minmax(0,.4fr) minmax(0,.7fr)}}
.hb .drow{{grid-template-columns:minmax(0,.8fr) minmax(0,1.6fr) minmax(0,.6fr) minmax(0,1.4fr) minmax(0,.8fr)}}
.hb .gprow{{grid-template-columns:minmax(0,1.4fr) minmax(0,.9fr) minmax(0,.5fr) minmax(0,1.2fr)}}
.hb .garow{{grid-template-columns:minmax(0,.8fr) minmax(0,1.5fr) minmax(0,1.3fr) minmax(0,1.4fr)}}
.hb .dprow{{grid-template-columns:minmax(0,.5fr) minmax(0,1.6fr) minmax(0,.7fr) minmax(0,.7fr) minmax(0,1.4fr)}}
.hb .hrow{{grid-template-columns:minmax(0,1.6fr) minmax(0,.8fr) minmax(0,.7fr) minmax(0,1.4fr)}}
.hb .rrow{{grid-template-columns:24px minmax(0,1.5fr) minmax(0,1.3fr) minmax(0,.5fr) minmax(0,.6fr)}}
@media (max-width:1100px){{.hb .aside{{width:100%;border-left:0;border-top:1px solid var(--line)}}}}
@media (max-width:767px){{
 .hb .side{{width:100%;border-right:0;border-bottom:1px solid var(--line);padding-bottom:10px}}
 .hb .navcol{{flex-direction:row;flex-wrap:wrap}}.hb .side .nv{{flex:none;white-space:nowrap}}.hb .side .navcol:not(:first-of-type){{flex-wrap:nowrap;overflow-x:auto;padding-bottom:2px}}.hb .navgrp{{display:none}}.hb .side .nv{{min-height:44px}}
 .hb .main{{padding:14px 16px 32px;gap:16px}}.hb .aside{{padding:14px 16px 24px}}
 .hb .c3,.hb .c4,.hb .c5,.hb .kbd{{display:none}}
 .hb .ch-d{{display:none}}.hb .ch-m{{display:block}}
 .hb .heat i{{min-height:18px}}
 .hb .steps{{grid-template-columns:repeat(2,minmax(0,1fr))}}
 .hb .btn{{min-height:44px}}.hb .btn.sm{{min-height:40px}}.hb .range .btn{{min-height:40px}}
 .hb .nrow{{grid-template-columns:minmax(0,1.5fr) minmax(0,1fr) minmax(0,.9fr)}}
 .hb .prow{{grid-template-columns:minmax(0,1fr) minmax(0,1.3fr)}}
 .hb .srow{{grid-template-columns:minmax(0,1.4fr) minmax(0,1fr) minmax(0,.8fr)}}
 .hb .crow{{grid-template-columns:minmax(0,1.4fr) minmax(0,1fr)}}
 .hb .drow{{grid-template-columns:minmax(0,.9fr) minmax(0,1.2fr) minmax(0,1.3fr)}}
 .hb .gprow{{grid-template-columns:minmax(0,1.2fr) minmax(0,.8fr) minmax(0,1fr)}}
 .hb .garow{{grid-template-columns:minmax(0,.8fr) minmax(0,1.4fr) minmax(0,1.2fr)}}
 .hb .dprow{{grid-template-columns:minmax(0,.5fr) minmax(0,1.4fr) minmax(0,1.3fr)}}
 .hb .hrow{{grid-template-columns:minmax(0,1.4fr) minmax(0,.7fr) minmax(0,1.2fr)}}
 .hb .rrow{{grid-template-columns:24px minmax(0,1.6fr) minmax(0,.8fr)}}
 .hb .tile .v{{font-size:24px}}
}}
"""

NAV = [
    ("總覽", "overview", "Overview.dc.html"),
    ("主機與節點", "nodes", "NodeDetail.dc.html"),
    ("專案與服務", "projects", "Projects.dc.html"),
    ("Docker／Podman", "docker", "Containers.dc.html"),
    ("Kubernetes", "k8s", None),
    ("應用商店", "store", None),
    ("網站與入口", "web", None),
    ("API 代理", "api", None),
    ("資料庫", "db", None),
    ("AI", "ai", None),
    ("檔案", "files", None),
    ("儲存", "storage", None),
    ("備份與還原", "backup", None),
    ("計畫任務", "cron", None),
    ("安全防護", "security", None),
    ("流量分析", "analytics", None),
    ("事件與告警", "events", None),
    ("身分與存取", "iam", None),
    ("平台設定", "settings", None),
]
GROUPS = [("基礎設施", ["nodes", "docker", "k8s", "storage", "files"]),
          ("應用", ["projects", "store", "db", "ai", "backup", "cron"]),
          ("入口與安全", ["web", "api", "security", "analytics", "events", "iam", "settings"])]


def nav(current: str, jobs: int = 3, attention: int = 3, mobile_links: bool = True) -> str:
    def link(key: str) -> str:
        label, k, file = next(n for n in NAV if n[1] == key)
        href = file if file else f"#{k}"
        cur = ' cur" aria-current="page' if k == current else ""
        return f'<a class="nv{cur}" href="{href}">{label}</a>'
    ov_cls = " cur" if current == "overview" else ""
    ov_cur = ' aria-current="page"' if current == "overview" else ""
    top = (f'<a class="nv{ov_cls}" href="Overview.dc.html"{ov_cur}>總覽<span class="kbd">G O</span></a>'
           f'<a class="nv{" cur" if current == "jobs" else ""}" href="WorkCenter.dc.html">工作中心'
           f'<span class="cnt" style="color: var(--hazel)">{jobs}</span></a>'
           f'<a class="nv" href="Overview.dc.html#attention">需要處理<span class="cnt" style="color: var(--warn)">{attention}</span></a>')
    groups = "".join(
        f'<div class="navgrp">{g}</div><div class="navcol">{"".join(link(k) for k in keys)}</div>'
        for g, keys in GROUPS)
    return f"""
  <nav class="side" aria-label="主導覽">
    <div style="display: flex; align-items: center; gap: 10px; padding: 4px 10px 10px; border-bottom: 1px solid var(--line)">
      <span aria-hidden="true" style="width: 22px; height: 22px; border-radius: 50%; background: var(--hazel); display: inline-block; box-shadow: inset 0 0 0 5px var(--panel), inset 0 0 0 7px var(--hazel)"></span>
      <span style="font-weight: 600; font-size: 15px">HazelminX</span>
      <span class="mono" style="font-size: 11px; color: var(--faint); margin-left: auto">tw-01</span>
    </div>
    <button type="button" class="inp" style="color: var(--muted); text-align: left"><span>搜尋或下指令</span><span class="kbd">⌘K</span></button>
    <div class="navcol">{top}</div>
    {groups}
    <div style="margin-top: auto; display: flex; align-items: center; gap: 8px; padding: 8px 10px 0; border-top: 1px solid var(--line); font-size: 13px; color: var(--muted)">
      <span>tim · 擁有者</span>
      <button type="button" class="btn sm" aria-label="切換淺色或深色" style="margin-left: auto; min-width: 44px">{{{{themeLabel}}}}</button>
    </div>
  </nav>"""


def shell(current: str, main: str, aside: str | None = None, **kw) -> str:
    aside_html = f'<aside class="aside" aria-label="需要處理與進行中的工作">{aside}</aside>' if aside else ""
    return (f'<div class="hb theme-{{{{theme}}}}" style="min-height: 100vh; display: flex; flex-wrap: wrap; align-items: stretch">'
            f'{nav(current, **kw)}<main class="main">{main}</main>{aside_html}</div>')


def page_head(title: str, obs: str = "03:42:06", actions: str = "", extra: str = "") -> str:
    return f"""
    <div style="display: flex; flex-wrap: wrap; align-items: baseline; gap: 14px">
      <h1>{title}</h1>
      <span style="font-size: 13px; color: var(--muted)">觀測時間 <span class="mono">{obs}</span> UTC+8{extra}</span>
      <div class="wrap" style="margin-left: auto">{actions}</div>
    </div>"""


def status(kind: str, text: str) -> str:
    return f'<span class="st {kind}"><i aria-hidden="true"></i>{text}</span>'


def fmt(n: float, d: int = 0) -> str:
    return f"{n:,.{d}f}"


# ---------- 圖表 ----------

def _poly(vals, x0, x1, y0, y1, vmax, vmin=0.0):
    n = len(vals)
    pts = []
    for i, v in enumerate(vals):
        x = x0 + (x1 - x0) * i / (n - 1)
        y = y1 - (y1 - y0) * ((v - vmin) / (vmax - vmin) if vmax > vmin else 0)
        pts.append((x, y))
    return pts


def _path(pts):
    return "M" + " L".join(f"{x:.1f},{y:.1f}" for x, y in pts)


def sparkline(vals, w=96, h=24, accent_last=True, color="var(--faint)", stale=False) -> str:
    vmax = max(vals) or 1
    vmin = min(vals)
    pts = _poly(vals, 1, w - 1, 2, h - 2, vmax, vmin)
    main = _path(pts[:-2]) if accent_last else _path(pts)
    out = [f'<svg class="sp" width="{w}" height="{h}" viewBox="0 0 {w} {h}" aria-hidden="true" style="display:block;max-width:100%">',
           f'<path d="{main}" fill="none" stroke="{color}" stroke-width="2" stroke-linejoin="round" stroke-linecap="round"/>']
    if accent_last:
        out.append(f'<path d="{_path(pts[-3:])}" fill="none" stroke="var(--hazel)" stroke-width="2" stroke-linejoin="round" stroke-linecap="round"/>')
        x, y = pts[-1]
        if stale:
            out.append(f'<circle cx="{x:.1f}" cy="{y:.1f}" r="3" fill="var(--panel)" stroke="var(--warn)" stroke-width="2"/>')
        else:
            out.append(f'<circle cx="{x:.1f}" cy="{y:.1f}" r="3" fill="var(--hazel)"/>')
    out.append("</svg>")
    return "".join(out)


def stat_tile(label: str, value: str, unit: str = "", delta: str = "", good: bool | None = None,
              spark: list | None = None, note: str = "") -> str:
    cls = "" if good is None else (" up" if good else " dn")
    d = f'<span class="{cls.strip()}">{delta}</span>' if delta else ""
    sp = sparkline(spark, w=120, h=26) if spark else ""
    unit_html = f"<small>{unit}</small>" if unit else ""
    note_html = f'<span class="note">{note}</span>' if note else ""
    return (f'<div class="tile"><span class="l">{label}</span><span class="v">{value}{unit_html}</span>'
            f'<span class="dl">{d}{note_html}</span>{sp}</div>')


def meter(pct: float, text: str, warn=70, danger=90, aria: str = "") -> str:
    cls = " dng" if pct >= danger else (" warn" if pct >= warn else "")
    return (f'<span class="meter" role="meter" aria-valuenow="{pct:.0f}" aria-valuemin="0" aria-valuemax="100" aria-label="{aria or text}">'
            f'<span class="tr" aria-hidden="true"><span class="fl{cls}" style="width: {min(pct, 100):.0f}%"></span></span>'
            f'<span class="num">{text}</span></span>')


@dataclass
class Series:
    name: str
    vals: list
    color: str = "var(--hazel)"
    stale_from: int | None = None  # index after which data is 上次觀測
    kind: str = "line"  # line | area | bar


def _ticks(vmax: float, n: int = 4):
    step = vmax / n
    return [step * i for i in range(n + 1)]


def timeseries(series: list[Series], x_labels: list[str], unit: str = "%", w: int = 560, h: int = 180,
               vmax: float | None = None, markers: list[tuple[int, str]] | None = None, stacked: bool = False,
               tip_labels: list[str] | None = None, bar: bool = False, fmt_val=None) -> str:
    """折線／面積／長條時間圖。所有序列共用一個 Y 軸。CSS hover 顯示每個 X 的所有序列。"""
    fmt_val = fmt_val or (lambda v: f"{v:.0f}{unit}")
    n = len(series[0].vals)
    padl, padr, padt, padb = 44, 10, 10, 22
    x0, x1, y0, y1 = padl, w - padr, padt, h - padb
    if stacked:
        tops = [sum(s.vals[i] for s in series) for i in range(n)]
        vmax = vmax or max(tops) * 1.1
    else:
        vmax = vmax or max(max(s.vals) for s in series) * 1.15
    vmax = max(vmax, 1e-9)
    out = [f'<svg class="ch" viewBox="0 0 {w} {h}" role="img" aria-label="{"、".join(s.name for s in series)}">']
    for t in _ticks(vmax):
        y = y1 - (y1 - y0) * t / vmax
        out.append(f'<line class="grid" x1="{x0}" y1="{y:.1f}" x2="{x1}" y2="{y:.1f}"/>')
        out.append(f'<text x="{x0 - 6}" y="{y + 4:.1f}" text-anchor="end">{fmt_val(t)}</text>')
    if bar:
        step = (x1 - x0) / n
        xpos = lambda i: x0 + step * (i + 0.5)
    else:
        step = (x1 - x0) / (n - 1) if n > 1 else 0
        xpos = lambda i: x0 + step * i
    lab_every = max(1, round(n / 6))
    label_idx = list(range(0, n, lab_every))
    if n - 1 - label_idx[-1] >= lab_every * 0.6:
        label_idx.append(n - 1)
    for i in label_idx:
        x = xpos(i)
        anchor = "start" if (i == 0 and not bar) else ("end" if (i == n - 1 and not bar) else "middle")
        out.append(f'<text x="{x:.1f}" y="{h - 6}" text-anchor="{anchor}">{x_labels[i]}</text>')
    # marks
    if bar:
        bw = min(24, (step - 2) / len(series))
        for si, s in enumerate(series):
            for i, v in enumerate(s.vals):
                bh = (y1 - y0) * v / vmax
                x = xpos(i) - (bw * len(series)) / 2 + si * bw + 1
                y = y1 - bh
                if bh < 0.5:
                    continue
                r = min(4, bh / 2)
                out.append(f'<path d="M{x:.1f},{y1} V{y + r:.1f} Q{x:.1f},{y:.1f} {x + r:.1f},{y:.1f} H{x + bw - 2 - r:.1f} Q{x + bw - 2:.1f},{y:.1f} {x + bw - 2:.1f},{y + r:.1f} V{y1} Z" fill="{s.color}"/>')
    elif stacked:
        base = [0.0] * n
        for s in series:
            top = [base[i] + s.vals[i] for i in range(n)]
            p_top = _poly(top, x0, x1, y0, y1, vmax)
            p_base = _poly(base, x0, x1, y0, y1, vmax)
            d = _path(p_top) + " L" + " L".join(f"{x:.1f},{y:.1f}" for x, y in reversed(p_base)) + " Z"
            out.append(f'<path d="{d}" fill="{s.color}" fill-opacity=".25" stroke="var(--panel)" stroke-width="2"/>')
            out.append(f'<path d="{_path(p_top)}" fill="none" stroke="{s.color}" stroke-width="2" stroke-linejoin="round"/>')
            base = top
    else:
        for s in series:
            pts = _poly(s.vals, x0, x1, y0, y1, vmax)
            live = pts if s.stale_from is None else pts[: s.stale_from + 1]
            if s.kind == "area":
                d = _path(live) + f" L{live[-1][0]:.1f},{y1} L{live[0][0]:.1f},{y1} Z"
                out.append(f'<path d="{d}" fill="{s.color}" fill-opacity=".10"/>')
            out.append(f'<path d="{_path(live)}" fill="none" stroke="{s.color}" stroke-width="2" stroke-linejoin="round" stroke-linecap="round"/>')
            if s.stale_from is not None:
                x, y = pts[s.stale_from]
                out.append(f'<circle cx="{x:.1f}" cy="{y:.1f}" r="4" fill="var(--panel)" stroke="{s.color}" stroke-width="2"/>')
    for i, label in (markers or []):
        x = xpos(i)
        out.append(f'<line x1="{x:.1f}" y1="{y0}" x2="{x:.1f}" y2="{y1}" stroke="var(--muted)" stroke-width="1" stroke-dasharray="3 3"/>')
        flip = x + 4 + 7 * len(label) > x1
        out.append(f'<text x="{(x - 4) if flip else (x + 4):.1f}" y="{y0 + 9}" text-anchor="{"end" if flip else "start"}" style="fill: var(--muted)">{label}</text>')
    # hover layer
    tw = 150
    th = 16 + 14 * len(series)
    for i in range(n):
        x = xpos(i)
        hx = max(x0, x - step / 2)
        hw = min(x1, x + step / 2) - hx
        tx = x + 8 if x + 8 + tw < w else x - 8 - tw
        rows = "".join(
            f'<text class="k" x="{tx + 8}" y="{th - 6 - 14 * (len(series) - 1 - k)}">{s.name}</text>'
            f'<text x="{tx + tw - 8}" y="{th - 6 - 14 * (len(series) - 1 - k)}" text-anchor="end" style="font-weight:500">'
            f'{fmt_val(s.vals[i])}{"*" if s.stale_from is not None and i > s.stale_from else ""}</text>'
            for k, s in enumerate(series))
        lab = (tip_labels or x_labels)[i]
        out.append(f'<g class="hit" tabindex="0"><rect x="{hx:.1f}" y="{y0}" width="{hw:.1f}" height="{y1 - y0}" fill="transparent"/>'
                   f'<line class="xh" x1="{x:.1f}" y1="{y0}" x2="{x:.1f}" y2="{y1}" stroke="var(--muted)" stroke-width="1"/>'
                   f'<g class="tip"><rect x="{tx}" y="{y0}" width="{tw}" height="{th}" rx="4"/>'
                   f'<text class="k" x="{tx + 8}" y="{y0 + 12}">{lab}</text>{rows}</g></g>')
    out.append("</svg>")
    return "".join(out)


def chart_card(title: str, body_desktop: str, body_mobile: str, series: list[Series] | None = None,
               unit: str = "", foot: str = "", legend_rect: bool = False) -> str:
    legend = ""
    if series and len(series) > 1:
        legend = '<div class="legend">' + "".join(
            f'<span><i class="{"rect" if legend_rect else ""}" style="background: {s.color}"></i>{s.name}</span>' for s in series) + "</div>"
    unit_html = f'<span class="unit">{unit}</span>' if unit else ""
    foot_html = f'<div class="foot">{foot}</div>' if foot else ""
    return (f'<section class="chart"><div class="ph"><h2>{title}</h2>{unit_html}{legend}</div>'
            f'<div class="body"><div class="ch-d">{body_desktop}</div><div class="ch-m">{body_mobile}</div></div>{foot_html}</section>')


def ts_card(title, series, x_labels, unit="%", foot="", wide=False, **kw) -> str:
    d = timeseries(series, x_labels, unit=unit, w=1140 if wide else 560, h=200 if wide else 180, **kw)
    m = timeseries(series, x_labels, unit=unit, w=328, h=160, **kw)
    return chart_card(title, d, m, series, unit=kw.get("unit_label", ""), foot=foot, legend_rect=kw.get("bar", False))


def ranked(items: list[tuple[str, float, str]], vmax: float | None = None) -> str:
    vmax = vmax or max(v for _, v, _ in items) or 1
    rows = "".join(
        f'<div class="rank"><span class="mono" style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap">{n}</span>'
        f'<span class="bar" aria-hidden="true"><i style="width: {v / vmax * 100:.0f}%"></i></span><span class="mono" style="font-size:12px">{t}</span></div>'
        for n, v, t in items)
    return f'<div class="ranks">{rows}</div>'


def state_timeline(states: str, labels: list[str] | None = None) -> str:
    """states: 字串，每字一段：o=成功 f=失敗 r=回滾 p=進行中 e=空"""
    cls = {"o": "", "f": " f", "r": " r", "p": " p", "e": " e"}
    segs = "".join(f'<i class="{cls[c].strip()}" title="{(labels or [""] * len(states))[i]}"></i>' for i, c in enumerate(states))
    return f'<div class="timeline" role="img" aria-label="部署歷史：{states.count("o")} 成功、{states.count("f")} 失敗、{states.count("r")} 回滾">{segs}</div>'


def segmented(parts: list[tuple[float, str]]) -> str:
    total = sum(v for v, _ in parts) or 1
    return '<span class="seg" aria-hidden="true">' + "".join(
        f'<i style="width: {v / total * 100:.1f}%; background: {c}"></i>' for v, c in parts if v > 0) + "</span>"


def heatgrid(rows: list[str], cols: list[str], values: list[list[float]], vmax: float = 100) -> str:
    q = ["var(--q1)", "var(--q2)", "var(--q3)", "var(--q4)", "var(--q5)"]
    ncol = len(cols)
    cells = []
    cells.append('<span></span>' + "".join(f'<span class="lab" style="justify-content:center;font-size:11px">{c}</span>' for c in cols))
    for r, row in zip(rows, values):
        cells.append(f'<span class="lab">{r}</span>' + "".join(
            f'<i style="background: {q[min(4, int(v / vmax * 4.999))]}" title="{r} {cols[j]} {v:.0f}%"></i>' for j, v in enumerate(row)))
    return (f'<div class="heat" style="grid-template-columns: minmax(80px, 1.2fr) repeat({ncol}, minmax(0, 1fr))" role="img" '
            f'aria-label="容器 × 小時 CPU 熱格，{len(rows)} 個容器、{ncol} 小時">' + "".join(cells) + "</div>")


def steps(items: list[tuple[str, str, str]]) -> str:
    """items: (label, state done|cur|wait|fail|todo, detail)"""
    return '<div class="steps">' + "".join(
        f'<div class="step {s}"><b aria-hidden="true"></b><span>{l}</span><span class="note">{d}</span></div>' for l, s, d in items) + "</div>"


def dc_wrap(title: str, body: str, width: int, height: int, lang="zh-Hant-TW") -> str:
    return f"""<!doctype html>
<html lang="{lang}">
<head>
<meta charset="utf-8">
<title>{title}</title>
<script src="./support.js"></script>
</head>
<body>
<x-dc>
<helmet>
{FONT_LINK}
<style>{BASE_CSS}</style>
</helmet>
{body}
</x-dc>
<script type="text/x-dc" data-dc-script data-props='{{"light":{{"editor":"boolean","default":false,"section":"主題"}},"$preview":{{"width":{width},"height":{height}}}}}'>
class Component extends DCLogic {{
  renderVals() {{
    const light = this.props.light ?? false;
    return {{ theme: light ? 'light' : 'dark', themeLabel: light ? '深色' : '淺色' }};
  }}
}}
</script>
</body>
</html>
"""


def plain_html(title: str, body: str, theme: str, lang="zh-Hant-TW") -> str:
    body = body.replace("{{theme}}", theme).replace("{{themeLabel}}", "深色" if theme == "light" else "淺色")
    return f"""<!doctype html>
<html lang="{lang}"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>{title}</title>
{FONT_LINK}
<style>{BASE_CSS}</style></head><body>{body}</body></html>"""
