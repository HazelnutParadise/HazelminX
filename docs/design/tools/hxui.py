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
.hb .ph{{display:flex;align-items:center;gap:10px;min-height:46px;padding:8px 16px;border-bottom:1px solid var(--line);flex-wrap:wrap}}
.hb .ph .sub{{font-size:12px;color:var(--muted)}}
.hb .ph .more{{margin-left:auto;font-size:13px;color:var(--hazel);min-height:32px;min-width:44px;padding:0 4px;display:inline-flex;align-items:center;justify-content:flex-end}}
.hb .foot a,.hb .note a,.hb a.note{{min-height:28px;display:inline-flex;align-items:center}}
.hb .inp input,.hb .inp select{{min-height:30px}}
.hb .card{{background:var(--raised);border:1px solid var(--line);border-radius:8px;padding:14px;display:flex;flex-direction:column;gap:8px}}
.hb .row{{display:grid;gap:12px;align-items:center;min-height:44px;padding:0 16px;border-bottom:1px solid var(--line)}}
.hb a.row{{cursor:pointer}}
.hb .row:hover{{background:var(--raised)}}
.hb .row:last-child{{border-bottom:0}}
.hb .hd{{font-size:11px;color:var(--faint);letter-spacing:.06em;text-transform:uppercase;min-height:34px;font-weight:500}}
.hb .hd:hover{{background:transparent}}
.hb .name{{display:flex;flex-direction:column;gap:2px;min-width:0}}
.hb .name .n{{font-family:"Geist Mono",ui-monospace,monospace;font-weight:500;overflow-wrap:anywhere;line-height:1.3}}
.hb .name .d{{font-size:12px;color:var(--faint);display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden;line-height:1.35;overflow-wrap:anywhere}}
.hb .st{{display:inline-flex;align-items:center;gap:6px;white-space:nowrap;font-size:12px;font-weight:500;padding:2px 8px 2px 7px;border-radius:999px;background:color-mix(in srgb,currentColor 13%,transparent)}}
.hb .lg .st,.hb .mx .st{{padding:1px 7px 1px 6px}}
.hb.theme-light .st{{background:var(--raised);box-shadow:inset 0 0 0 1px color-mix(in srgb,currentColor 35%,transparent)}}
.hb .st i{{display:inline-block;width:8px;height:8px;flex:none}}
.hb .st.ok{{color:var(--ok)}}.hb .st.ok i{{border-radius:50%;background:var(--ok)}}
.hb .st.warn{{color:var(--warn)}}.hb .st.warn i{{border-radius:50%;border:2px solid var(--warn)}}
.hb .st.dng{{color:var(--danger)}}.hb .st.dng i{{background:var(--danger)}}
.hb .st.info{{color:var(--blue)}}.hb .st.info i{{background:var(--blue)}}
.hb .st.off{{color:var(--muted)}}.hb .st.off i{{border-radius:50%;border:2px solid var(--muted);border-style:dashed}}
.hb .nv{{display:flex;align-items:center;gap:10px;min-height:38px;padding:0 10px;border-radius:6px;font-size:13.5px;color:var(--ink);border:0;background:transparent;width:100%;text-align:left}}
.hb .nv svg{{flex:none;color:var(--muted)}}.hb .nv.cur svg{{color:var(--hazel)}}
.hb .nv:hover{{background:var(--raised);color:var(--ink)}}
.hb .nv.cur{{background:var(--raised);color:var(--hazel);font-weight:600}}
.hb .nv .cnt{{margin-left:auto;font-family:"Geist Mono",ui-monospace,monospace;font-size:12px}}
.hb .kbd{{font-family:"Geist Mono",ui-monospace,monospace;font-size:11px;color:var(--faint);border:1px solid var(--line);border-radius:4px;padding:1px 5px;margin-left:auto}}
.hb .navgrp{{font-size:11px;color:var(--faint);letter-spacing:.06em;padding:6px 10px 0}}
.hb .navcol{{display:flex;flex-direction:column;gap:2px}}
.hb .tiles{{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(170px,100%),1fr));gap:12px}}
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
.hb .cg{{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(440px,100%),1fr));gap:16px}}
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
.hb .fleet{{display:grid;grid-template-columns:repeat(auto-fill,minmax(min(250px,100%),1fr));gap:12px;padding:12px 14px}}
.hb .ncard{{display:flex;flex-direction:column;gap:10px;padding:12px;background:var(--raised);border:1px solid var(--line);border-radius:8px;min-width:0;color:var(--ink)}}
.hb .ncard .nh{{display:flex;align-items:center;gap:8px;flex-wrap:wrap}}
.hb .ncard .nh .n{{font-family:"Geist Mono",ui-monospace,monospace;font-weight:500}}
.hb .ncard .nh .d{{font-size:12px;color:var(--faint);width:100%}}
.hb .rings{{display:flex;gap:10px;justify-content:space-between}}
.hb .ring{{display:flex;flex-direction:column;align-items:center;gap:2px;font-size:11px;color:var(--faint);min-width:0}}
.hb .ring svg{{display:block}}
.hb .ring .v{{font-family:"Geist Mono",ui-monospace,monospace;font-size:12px;fill:var(--ink)}}
.hb .ncard .ft{{display:flex;align-items:center;gap:8px;font-size:12px;color:var(--muted);flex-wrap:wrap}}
.hb .ncard .ft .seg{{flex:1;min-width:60px}}
.hb .hmap{{display:flex;flex-direction:column;gap:10px;padding:12px 14px}}
.hb .hgrp{{display:flex;flex-direction:column;gap:6px}}
.hb .hgrp .lab{{display:flex;gap:8px;align-items:baseline;font-size:12px;color:var(--muted)}}
.hb .hgrp .lab b{{font-family:"Geist Mono",ui-monospace,monospace;font-weight:500;color:var(--ink)}}
.hb .hcells{{display:flex;flex-wrap:wrap;gap:3px}}
.hb .hcell{{position:relative;width:26px;height:26px;border-radius:3px;display:flex;align-items:center;justify-content:center;border:1px solid transparent}}
.hb .hcell.off{{background:transparent;border:1px dashed var(--line)}}
.hb .hcell.bad{{border-color:var(--danger)}}
.hb .hcell.bad::after{{content:"";width:8px;height:8px;background:var(--danger)}}
.hb .hcell.stale::after{{content:"";width:8px;height:8px;border-radius:50%;border:2px solid var(--warn);box-sizing:border-box}}
.hb .scale{{display:flex;align-items:center;gap:4px;font-size:11px;color:var(--faint)}}
.hb .scale i{{display:block;width:14px;height:10px;border-radius:2px}}
.hb .tall{{display:flex;gap:2px;height:28px;border-radius:4px;overflow:hidden}}
.hb .tall i{{display:flex;align-items:center;justify-content:center;height:100%;font-size:11px;font-family:"Geist Mono",ui-monospace,monospace;color:var(--ink);overflow:hidden;white-space:nowrap}}
.hb .cal{{display:grid;grid-auto-flow:column;grid-template-rows:repeat(7,12px);gap:2px}}
.hb .cal i{{display:block;width:12px;height:12px;border-radius:2px;background:var(--q1);position:relative}}
.hb .cal i.o{{background:var(--ok)}}.hb .cal i.f{{background:var(--danger)}}.hb .cal i.r{{background:var(--warn)}}.hb .cal i.n{{background:transparent;border:1px dashed var(--line)}}
.hb .cal i.f::after{{content:"";position:absolute;inset:3px;background:var(--panel)}}
.hb .pipe{{display:flex;align-items:stretch;gap:0;overflow-x:auto}}
.hb .pstage{{display:flex;flex-direction:column;gap:4px;min-width:110px;flex:1;padding:8px 10px;border:1px solid var(--line);border-radius:6px;font-size:12px;color:var(--muted);position:relative;background:var(--panel)}}
.hb .pstage+.pstage{{margin-left:18px}}
.hb .pstage+.pstage::before{{content:"";position:absolute;left:-12px;top:50%;width:6px;height:6px;border-top:2px solid var(--faint);border-right:2px solid var(--faint);transform:translateY(-50%) rotate(45deg)}}
.hb .pstage .t{{color:var(--ink);font-weight:500;display:flex;align-items:center;gap:6px}}
.hb .pstage.done{{border-color:var(--ok)}}.hb .pstage.cur{{border-color:var(--blue);background:var(--raised)}}.hb .pstage.fail{{border-color:var(--danger)}}.hb .pstage.wait{{border-color:var(--warn)}}
.hb svg.topo{{display:block;width:100%;height:auto;font-family:"Geist","Noto Sans TC",sans-serif}}
.hb svg.topo text{{font-size:12px;fill:var(--ink)}}
.hb svg.topo text.sub{{font-size:11px;fill:var(--faint);font-family:"Geist Mono",ui-monospace,monospace}}
.hb svg.topo text.col{{font-size:11px;fill:var(--faint);letter-spacing:.06em;text-transform:uppercase}}
.hb svg.topo .box{{fill:var(--raised);stroke:var(--line)}}
.hb svg.topo .box.danger{{stroke:var(--danger)}}.hb svg.topo .box.warn{{stroke:var(--warn)}}
.hb svg.topo .lnk{{fill:none;stroke:var(--s1);stroke-opacity:.55;stroke-linecap:round}}
.hb svg.topo .lnk.danger{{stroke:var(--danger)}}.hb svg.topo .lnk.dim{{stroke:var(--q3)}}
.hb .flowlist{{display:flex;flex-direction:column;gap:6px;font-size:13px}}
.hb .flowlist .fr{{display:grid;grid-template-columns:minmax(0,1fr) 16px minmax(0,1fr) auto;gap:6px;align-items:center}}
.hb .mx{{display:grid;gap:2px;font-size:12px}}
.hb .mx .c{{min-height:32px;display:flex;align-items:center;justify-content:center;gap:4px;background:var(--raised);border-radius:4px;padding:0 6px;text-align:center}}
.hb .mx .c.empty{{background:transparent;border:1px dashed var(--line);color:var(--faint)}}
.hb .mx .rl{{display:flex;align-items:center;font-family:"Geist Mono",ui-monospace,monospace;color:var(--muted);white-space:nowrap;overflow:hidden;text-overflow:ellipsis;min-height:32px}}
.hb .mx .cl{{display:flex;align-items:flex-end;justify-content:center;font-size:11px;color:var(--faint);padding-bottom:4px;text-align:center}}
.hb .tree{{font-family:"Geist Mono",ui-monospace,monospace;font-size:13px}}
.hb .tree ul{{list-style:none;margin:0;padding-left:16px;border-left:1px solid var(--line)}}
.hb .tree>ul{{padding-left:0;border-left:0}}
.hb .tree li{{display:flex;align-items:center;gap:8px;min-height:30px}}
.hb .tree li .sz{{margin-left:auto;color:var(--faint);font-size:12px}}
.hb .tree li.on{{color:var(--hazel)}}
.hb .appcard{{display:flex;gap:10px;padding:12px;background:var(--raised);border:1px solid var(--line);border-radius:8px;min-width:0;color:var(--ink);align-items:flex-start}}
.hb .appcard .ic{{width:40px;height:40px;border-radius:8px;background:var(--q2);display:flex;align-items:center;justify-content:center;font-family:"Geist Mono",ui-monospace,monospace;font-weight:700;color:var(--ink);flex:none}}
.hb .appcard .bd{{display:flex;flex-direction:column;gap:4px;min-width:0;flex:1}}
.hb .appcard .bd .n{{font-weight:500}}
.hb .appcard .bd .d{{font-size:12px;color:var(--faint)}}
.hb .chip{{display:inline-flex;align-items:center;min-height:22px;padding:0 8px;border-radius:4px;border:1px solid var(--line);font-size:11px;color:var(--muted);background:transparent}}
.hb .chip.on{{color:var(--hazel);border-color:var(--hazel)}}
.hb .term,.hb .diff{{--ink:#ECE6DA;--muted:#A79E8E;--faint:#9A917F;--panel:#14120F;--line:#35312B;--ok:#9BD3A4;--warn:#F0B35B;--danger:#F28B82;--blue:#8DB4F2}}
.hb .term{{background:#14120F;border:1px solid var(--line);border-radius:8px;font-family:"Geist Mono",ui-monospace,monospace;font-size:12px;line-height:1.6;padding:10px 12px;color:var(--ink);white-space:pre;overflow:auto;min-height:220px}}
.hb .lg{{display:flex;gap:12px;flex-wrap:wrap;align-items:center;font-size:12px;color:var(--muted);margin-left:auto}}
.hb .lg span{{display:inline-flex;align-items:center;gap:5px}}
.hb .lg i.sw{{display:inline-block;width:12px;height:8px;border-radius:2px}}
.hb .lg i.ln{{display:inline-block;width:18px;border-radius:2px;background:var(--s1)}}
.hb .lg .st{{gap:5px}}
.hb .scope{{display:flex;align-items:center;gap:8px;flex-wrap:wrap;padding:8px 12px;background:var(--panel);border:1px solid var(--line);border-radius:8px}}
.hb .scope .lbl{{font-size:12px;color:var(--faint);letter-spacing:.06em;text-transform:uppercase}}
.hb .scope .btn{{min-height:32px;padding:0 10px;font-size:12px;color:var(--muted);gap:6px}}
.hb .scope .btn.on{{background:var(--raised);color:var(--ink);border-color:var(--hazel);font-weight:600}}
.hb .scope .btn .mono{{color:var(--faint);font-size:11px}}
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
.hb .diff{{background:#14120F;color:var(--ink);overflow-x:auto;font-family:"Geist Mono",ui-monospace,monospace;font-size:12px;line-height:1.6;display:flex;flex-direction:column}}
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
.hb .main{{flex:999 1 560px;min-width:0;display:flex;flex-direction:column;gap:16px;padding:20px 24px 40px}}
.hb .body{{display:flex;flex-wrap:wrap;align-items:stretch;flex:1}}
.hb .side{{width:220px;border-right:1px solid var(--line);background:var(--panel);display:flex;flex-direction:column;gap:10px;padding:12px 10px 16px}}
.hb .side .coll{{margin-top:auto;color:var(--muted)}}
.hb .top{{display:flex;align-items:center;gap:14px;min-height:56px;padding:0 16px 0 0;border-bottom:1px solid var(--line);background:var(--panel)}}
.hb .brand{{display:flex;align-items:center;gap:10px;width:220px;padding:0 16px;font-weight:600;font-size:15px;border-right:1px solid var(--line);align-self:stretch}}
.hb .logo{{width:22px;height:22px;border-radius:50%;background:var(--hazel);display:inline-block;box-shadow:inset 0 0 0 5px var(--panel),inset 0 0 0 7px var(--hazel);flex:none}}
.hb .crumb{{display:flex;align-items:center;gap:8px;font-size:14px;font-weight:500;white-space:nowrap}}
.hb .crumb .sep{{color:var(--faint)}}
.hb .top .search{{width:auto;flex:1 1 220px;max-width:420px;min-height:36px;color:var(--muted);gap:8px;margin-left:auto}}
.hb .top .search .kbd{{margin-left:auto}}
.hb .tright{{display:flex;align-items:center;gap:4px;margin-left:auto}}
.hb .obs{{font-size:12px;color:var(--muted);margin-right:8px;white-space:nowrap}}
.hb .ib{{position:relative;display:inline-flex;align-items:center;justify-content:center;gap:6px;min-width:36px;min-height:36px;padding:0 8px;border-radius:6px;border:0;background:transparent;color:var(--muted);font-size:13px}}
.hb .ib:hover{{background:var(--raised);color:var(--ink)}}
.hb .ib.user{{padding-right:10px}}
.hb .badge{{position:absolute;top:3px;right:3px;min-width:16px;height:16px;padding:0 4px;border-radius:8px;background:var(--hazel);color:var(--hazelink);font-size:10px;font-weight:600;font-family:"Geist Mono",ui-monospace,monospace;display:flex;align-items:center;justify-content:center}}
.hb .badge.dng{{background:var(--danger)}}
.hb .acts{{display:inline-flex;gap:2px;justify-content:flex-end}}
.hb .acts .ib{{min-width:32px;min-height:32px;padding:0}}
.hb .phead{{display:flex;flex-wrap:wrap;align-items:center;gap:12px;min-height:40px}}
.hb .nrow{{grid-template-columns:minmax(0,1.5fr) minmax(0,.9fr) minmax(0,.9fr) minmax(0,1fr) minmax(0,1fr) 110px minmax(0,.8fr)}}
.hb .prow{{grid-template-columns:minmax(0,1.2fr) minmax(0,1.5fr) minmax(0,.5fr) minmax(0,.6fr) minmax(0,1fr) minmax(0,1.1fr)}}
.hb .srow{{grid-template-columns:minmax(0,1.5fr) minmax(0,.9fr) minmax(0,.9fr) minmax(0,.9fr) minmax(0,.9fr) 110px minmax(0,.7fr)}}
.hb .crow{{grid-template-columns:minmax(0,1.6fr) minmax(0,1fr) minmax(0,.5fr) minmax(0,.9fr) minmax(0,.9fr) minmax(0,1fr) minmax(0,.4fr) minmax(0,.7fr) 130px}}
.hb .row.cols-3{{grid-template-columns:minmax(0,1.6fr) minmax(0,1fr) minmax(0,1fr)}}
.hb .row.cols-4{{grid-template-columns:minmax(0,1.6fr) minmax(0,1fr) minmax(0,1fr) minmax(0,1fr)}}
.hb .row.cols-5{{grid-template-columns:minmax(0,1.6fr) minmax(0,1fr) minmax(0,1fr) minmax(0,1fr) minmax(0,1fr)}}
.hb .row.cols-6{{grid-template-columns:minmax(0,1.6fr) minmax(0,1fr) minmax(0,.9fr) minmax(0,1fr) minmax(0,1fr) minmax(0,1fr)}}
.hb .row.acts-end{{grid-template-columns:minmax(0,1.6fr) minmax(0,1fr) minmax(0,1fr) minmax(0,1.2fr) 130px}}
.hb .rank{{grid-template-columns:minmax(0,1.7fr) minmax(0,2fr) auto}}
.hb .rank .mono,.hb .flowlist .mono{{white-space:normal!important;overflow-wrap:anywhere;line-height:1.3}}
.hb .mxw,.hb .heatw{{overflow-x:auto}}
.hb .row > span:last-child:has(> .btn),.hb .row > .acts{{justify-self:end}}
.hb .drow{{grid-template-columns:minmax(0,.8fr) minmax(0,1.6fr) minmax(0,.6fr) minmax(0,1.4fr) minmax(0,.8fr)}}
.hb .gprow{{grid-template-columns:minmax(0,1.4fr) minmax(0,.9fr) minmax(0,.5fr) minmax(0,1.2fr)}}
.hb .garow{{grid-template-columns:minmax(0,.8fr) minmax(0,1.5fr) minmax(0,1.3fr) minmax(0,1.4fr)}}
.hb .dprow{{grid-template-columns:minmax(0,.5fr) minmax(0,1.6fr) minmax(0,.7fr) minmax(0,.7fr) minmax(0,1.4fr)}}
.hb .hrow{{grid-template-columns:minmax(0,1.6fr) minmax(0,.8fr) minmax(0,.7fr) minmax(0,1.4fr)}}
.hb .rrow{{grid-template-columns:24px minmax(0,1.5fr) minmax(0,1.3fr) minmax(0,.5fr) minmax(0,.6fr)}}
@media (max-width:1100px){{.hb .aside{{width:100%;border-left:0;border-top:1px solid var(--line)}}}}
@media (max-width:767px){{
 .hb .side{{width:100%;border-right:0;border-bottom:1px solid var(--line);flex-direction:row;flex-wrap:nowrap;overflow-x:auto;gap:4px;padding:8px 12px}}
 .hb .navgrp{{display:none}}.hb .navcol{{flex-direction:row;flex:none;gap:4px}}.hb .side .nv{{width:auto;min-width:44px;justify-content:center;min-height:36px;padding:0 10px;white-space:nowrap;font-size:13px}}.hb .side .nv .cnt{{margin-left:4px}}
 .hb .main{{padding:14px 16px 32px;gap:16px}}.hb .aside{{padding:14px 16px 24px}}
 .hb .c3,.hb .c4,.hb .c5,.hb .kbd{{display:none}}
 .hb .top{{flex-wrap:wrap;padding:8px 12px;gap:8px;min-height:0}}.hb .top .search{{min-height:44px}}.hb .brand{{width:auto;border:0;padding:0}}.hb .crumb{{display:none}}.hb .top .search{{order:3;flex:1 1 100%;max-width:none;margin:0}}.hb .obs{{display:none}}.hb .ib.user span{{display:none}}
 .hb .side .coll{{display:none}}.hb .nv svg{{display:none}}
 .hb .row[class*="cols-"],.hb .row.acts-end{{grid-template-columns:minmax(0,1.4fr) minmax(0,1fr) auto}}
 .hb .acts .ib:nth-child(n+3){{display:none}}
 .hb .rank{{grid-template-columns:minmax(0,1fr) auto;row-gap:4px}}.hb .rank .bar{{grid-column:1/-1}}
 .hb .mx{{min-width:620px}}.hb .heat{{min-width:720px}}
 .hb .mx .rl{{white-space:normal}}
 .hb .ch-d{{display:none}}.hb .ch-m{{display:block}}
 .hb .heat i{{min-height:18px}}
 .hb .steps{{grid-template-columns:repeat(2,minmax(0,1fr))}}
 .hb .btn,.hb .btn.sm,.hb .range .btn,.hb .scope .btn,.hb .ib,.hb .acts .ib,.hb .tab,.hb .side .nv,.hb .ph .more,.hb .foot a,.hb a.note,.hb .brand,.hb .inp{{min-height:44px}}.hb .inp input,.hb .inp select{{min-height:40px}}.hb .ib,.hb .acts .ib{{min-width:44px}}
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
 .hb .rings svg{{width:52px;height:52px}}
 .hb .hcell{{width:36px;height:36px}}
 .hb .tabs{{flex-wrap:nowrap;overflow-x:auto}}.hb .tab{{white-space:nowrap;flex:none}}
 .hb .ph .wrap{{width:100%}}
 .hb .pstage{{min-width:96px}}
}}
"""

ICONS = {
 "overview": '<rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/>',
 "jobs": '<path d="M3 12h4l2-6 4 12 2-6h6"/>',
 "attention": '<path d="M12 3 2.5 20h19L12 3z"/><path d="M12 9v5"/><circle cx="12" cy="17" r=".6"/>',
 "terminal": '<path d="m5 7 5 5-5 5"/><path d="M12 19h7"/>',
 "nodes": '<rect x="3" y="4" width="18" height="7" rx="1.5"/><rect x="3" y="13" width="18" height="7" rx="1.5"/><circle cx="7" cy="7.5" r=".7"/><circle cx="7" cy="16.5" r=".7"/>',
 "docker": '<path d="M12 3 3 7.5v9L12 21l9-4.5v-9L12 3z"/><path d="M3 7.5 12 12l9-4.5M12 12v9"/>',
 "k8s": '<path d="M12 2.5 20.5 7v10L12 21.5 3.5 17V7L12 2.5z"/><circle cx="12" cy="12" r="3"/>',
 "storage": '<rect x="3" y="5" width="18" height="14" rx="2"/><path d="M3 13h18"/><circle cx="7" cy="16" r=".7"/>',
 "files": '<path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7z"/>',
 "projects": '<path d="m12 3 9 5-9 5-9-5 9-5z"/><path d="m3 13 9 5 9-5"/><path d="m3 17 9 5 9-5"/>',
 "store": '<path d="M5 8h14l-1 12H6L5 8z"/><path d="M9 8V6a3 3 0 0 1 6 0v2"/>',
 "db": '<ellipse cx="12" cy="6" rx="8" ry="3"/><path d="M4 6v12c0 1.7 3.6 3 8 3s8-1.3 8-3V6"/><path d="M4 12c0 1.7 3.6 3 8 3s8-1.3 8-3"/>',
 "ai": '<rect x="6" y="6" width="12" height="12" rx="2"/><rect x="10" y="10" width="4" height="4"/><path d="M9 2v4M15 2v4M9 18v4M15 18v4M2 9h4M2 15h4M18 9h4M18 15h4"/>',
 "backup": '<path d="M12 4v10"/><path d="m8 10 4 4 4-4"/><path d="M4 16v3a1 1 0 0 0 1 1h14a1 1 0 0 0 1-1v-3"/>',
 "cron": '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
 "web": '<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18"/>',
 "api": '<path d="M7 7h10M7 12h7M7 17h10"/><path d="m16 5 2 2-2 2M8 15l-2 2 2 2"/>',
 "security": '<path d="M12 3 4 6v6c0 5 3.5 8 8 9 4.5-1 8-4 8-9V6l-8-3z"/><path d="m9 12 2 2 4-4"/>',
 "analytics": '<path d="M4 20h16"/><path d="M6 16v-5M11 16V8M16 16v-3M21 16V5"/>',
 "events": '<path d="M6 16V11a6 6 0 0 1 12 0v5l2 2H4l2-2z"/><path d="M10 20a2 2 0 0 0 4 0"/>',
 "logs": '<path d="M6 3h8l4 4v14H6V3z"/><path d="M14 3v4h4M9 12h6M9 16h6"/>',
 "iam": '<circle cx="9" cy="8" r="3.5"/><path d="M3 20a6 6 0 0 1 12 0"/><circle cx="17" cy="9" r="2.5"/><path d="M15 15a4 4 0 0 1 6 5"/>',
 "settings": '<path d="M4 7h10M18 7h2M4 17h4M12 17h8"/><circle cx="16" cy="7" r="2"/><circle cx="10" cy="17" r="2"/>',
 "search": '<circle cx="11" cy="11" r="6"/><path d="m20 20-4-4"/>',
 "sun": '<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M2 12h2M20 12h2M5 5l1.5 1.5M17.5 17.5 19 19M5 19l1.5-1.5M17.5 6.5 19 5"/>',
 "user": '<circle cx="12" cy="8" r="4"/><path d="M4 21a8 8 0 0 1 16 0"/>',
 "chevron": '<path d="m9 6 6 6-6 6"/>',
 "collapse": '<path d="m14 7-5 5 5 5"/><path d="M4 4v16"/>',
 "log": '<path d="M6 3h8l4 4v14H6V3z"/><path d="M9 13h6M9 17h6"/>',
 "term": '<path d="m5 7 5 5-5 5"/><path d="M12 19h7"/>',
 "restart": '<path d="M20 12a8 8 0 1 1-2.3-5.7"/><path d="M20 4v5h-5"/>',
 "more": '<circle cx="6" cy="12" r="1.2"/><circle cx="12" cy="12" r="1.2"/><circle cx="18" cy="12" r="1.2"/>',
 "stop": '<rect x="6" y="6" width="12" height="12" rx="1.5"/>',
}


def icon(name: str, size: int = 16) -> str:
    return (f'<svg viewBox="0 0 24 24" width="{size}" height="{size}" fill="none" stroke="currentColor" stroke-width="1.8" '
            f'stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">{ICONS[name]}</svg>')


def actions(keys: list) -> str:
    """列尾快速動作：icon 按鈕，aria-label 是文字。keys 例 ["log","term","restart","more"]"""
    lab = {"log": "日誌", "term": "終端機", "restart": "重啟", "more": "更多", "stop": "停止"}
    return '<span class="acts">' + "".join(f'<button type="button" class="ib" aria-label="{lab[k]}" title="{lab[k]}">{icon(k)}</button>' for k in keys) + "</span>"


NAV = [
    ("總覽", "overview", "Overview.dc.html"),
    ("主機與節點", "nodes", "Nodes.dc.html"),
    ("專案與服務", "projects", "Projects.dc.html"),
    ("Docker／Podman", "docker", "Containers.dc.html"),
    ("Kubernetes", "k8s", "Kubernetes.dc.html"),
    ("應用商店", "store", "AppStore.dc.html"),
    ("網站與入口", "web", "Websites.dc.html"),
    ("API 代理", "api", "APIProxy.dc.html"),
    ("資料庫", "db", "Database.dc.html"),
    ("AI", "ai", "AI.dc.html"),
    ("檔案", "files", "Files.dc.html"),
    ("儲存", "storage", "Storage.dc.html"),
    ("備份與還原", "backup", "Storage.dc.html"),
    ("計畫任務", "cron", "Cron.dc.html"),
    ("安全防護", "security", "Security.dc.html"),
    ("流量分析", "analytics", "Analytics.dc.html"),
    ("事件與告警", "events", "Events.dc.html"),
    ("日誌", "logs", "Logs.dc.html"),
    ("身分與存取", "iam", "IAM.dc.html"),
    ("平台設定", "settings", "Settings.dc.html"),
]
GROUPS = [("基礎設施", ["nodes", "docker", "k8s", "storage", "files"]),
          ("應用", ["projects", "store", "db", "ai", "backup", "cron"]),
          ("入口與安全", ["web", "api", "security", "analytics", "events", "logs", "iam", "settings"])]


def nav(current: str, jobs: int = 3, attention: int = 4, mobile_links: bool = True) -> str:
    def link(key: str) -> str:
        label, k, file = next(n for n in NAV if n[1] == key)
        href = file if file else f"#{k}"
        cur = ' cur" aria-current="page' if k == current else ""
        return f'<a class="nv{cur}" href="{href}">{icon(k)}<span>{label}</span></a>'
    ov = ' cur" aria-current="page' if current == "overview" else ""
    jb = ' cur" aria-current="page' if current == "jobs" else ""
    top = (f'<a class="nv{ov}" href="Overview.dc.html">{icon("overview")}<span>總覽</span></a>'
           f'<a class="nv{jb}" href="WorkCenter.dc.html">{icon("jobs")}<span>工作中心</span><span class="cnt">{jobs}</span></a>')
    groups = "".join(f'<div class="navgrp">{g}</div><div class="navcol">{"".join(link(k) for k in keys)}</div>' for g, keys in GROUPS)
    return f"""
  <nav class="side" aria-label="主導覽">
    <div class="navcol">{top}</div>
    {groups}
    <button type="button" class="nv coll" aria-label="收合側欄">{icon("collapse")}<span>收合</span></button>
  </nav>"""


def topbar(current: str, jobs: int = 3, attention: int = 4, obs: str = "03:42:06") -> str:
    label = next((n[0] for n in NAV if n[1] == current), {"jobs": "工作中心", "terminal": "終端機", "logs": "日誌"}.get(current, "總覽"))
    return f"""
  <header class="top">
    <a class="brand" href="Overview.dc.html"><span class="logo" aria-hidden="true"></span><span>HazelminX</span></a>
    <div class="crumb"><span class="note">tw-01</span><span class="sep">/</span><span>{label}</span></div>
    <button type="button" class="inp search" aria-label="搜尋或下指令">{icon("search")}<span>搜尋或下指令</span><span class="kbd">⌘K</span></button>
    <div class="tright">
      <span class="obs">觀測 <span class="mono">{obs}</span> · 每 10 秒</span>
      <a class="ib" href="Overview.dc.html#attention" aria-label="需要處理 {attention}">{icon("events")}<b class="badge dng">{attention}</b></a>
      <a class="ib" href="WorkCenter.dc.html" aria-label="進行中的工作 {jobs}">{icon("jobs")}<b class="badge">{jobs}</b></a>
      <a class="ib" href="Terminal.dc.html" aria-label="終端機">{icon("terminal")}</a>
      <button type="button" class="ib" aria-label="切換淺色或深色">{icon("sun")}</button>
      <button type="button" class="ib user" aria-label="tim · 擁有者">{icon("user")}<span>tim</span></button>
    </div>
  </header>"""


def shell(current: str, main: str, aside: str | None = None, **kw) -> str:
    aside_html = f'<aside class="aside" aria-label="需要處理與進行中的工作">{aside}</aside>' if aside else ""
    return (f'<div class="hb theme-{{{{theme}}}}" style="min-height: 100vh; display: flex; flex-direction: column">'
            f'{topbar(current, **kw)}<div class="body">{nav(current, **kw)}<main class="main">{main}</main>{aside_html}</div></div>')


def page_head(title: str, obs: str = "03:42:06", actions: str = "", extra: str = "") -> str:
    return f"""
    <div class="phead">
      <h1>{title}</h1>
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
        f'<div class="rank"><span class="mono">{n}</span>'
        f'<span class="bar" aria-hidden="true"><i style="width: {v / vmax * 100:.0f}%"></i></span><span class="mono" style="font-size:12px;white-space:nowrap!important">{t}</span></div>'
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
    return (f'<div class="heatw"><div class="heat" style="grid-template-columns: minmax(80px, 1.2fr) repeat({ncol}, minmax(0, 1fr))" role="img" '
            f'aria-label="容器 × 小時 CPU 熱格，{len(rows)} 個容器、{ncol} 小時">' + "".join(cells) + "</div></div>")


def steps(items: list[tuple[str, str, str]]) -> str:
    """items: (label, state done|cur|wait|fail|todo, detail)"""
    return '<div class="steps">' + "".join(
        f'<div class="step {s}"><b aria-hidden="true"></b><span>{l}</span><span class="note">{d}</span></div>' for l, s, d in items) + "</div>"



# ---------- 圖形為主的複合元件 ----------
import math as _math


def ring(pct: float, text: str, label: str = "", size: int = 64, warn=70, danger=90, aria: str = "") -> str:
    """單一比例的環形量表：軌道 q2，填充依門檻換色；中央寫數值，形狀＋文字不只靠色。"""
    r = (size - 8) / 2
    c = 2 * _math.pi * r
    p = max(0.0, min(100.0, pct))
    color = "var(--danger)" if p >= danger else ("var(--warn)" if p >= warn else "var(--hazel)")
    dash = f"{c * p / 100:.1f} {c:.1f}"
    svg = (f'<svg width="{size}" height="{size}" viewBox="0 0 {size} {size}" role="meter" aria-valuenow="{p:.0f}" aria-valuemin="0" aria-valuemax="100" aria-label="{aria or label or text}">'
           f'<circle cx="{size/2}" cy="{size/2}" r="{r}" fill="none" stroke="var(--q2)" stroke-width="6"/>'
           f'<circle cx="{size/2}" cy="{size/2}" r="{r}" fill="none" stroke="{color}" stroke-width="6" stroke-linecap="round" stroke-dasharray="{dash}" transform="rotate(-90 {size/2} {size/2})"/>'
           f'<text x="50%" y="50%" dy="0.35em" text-anchor="middle" class="v">{text}</text></svg>')
    return f'<span class="ring">{svg}<span>{label}</span></span>' if label else f'<span class="ring">{svg}</span>'


def node_card(name: str, desc: str, st: str, cpu: float, mem: tuple, disk: tuple, spark: list, last: str,
              containers: tuple = (0, 0), href: str = "NodeDetail.dc.html", stale: bool = False, extra: str = "") -> str:
    """艦隊卡：名稱＋狀態、三個環（CPU／記憶體／資料碟）、CPU 1 小時走勢、容器狀態段條與最後聯絡。"""
    mu, mt = mem; du, dt = disk
    run, total = containers
    star = "*" if stale else ""
    rings_html = (ring(cpu, f"{cpu:.0f}%{star}", "CPU", aria=f"{name} CPU") + ring(mu / mt * 100, f"{mu:.0f}G", f"記憶體 /{mt:.0f}G", aria=f"{name} 記憶體")
                  + ring(du / dt * 100, f"{du:.0f}G", f"資料碟 /{dt:.0f}G", warn=80, danger=90, aria=f"{name} 資料碟"))
    seg = segmented([(run, "var(--ok)"), (total - run, "var(--q2)")]) if total else ""
    ct = f"容器 {run}/{total}" if total else "無容器"
    return (f'<a class="ncard" href="{href}"><div class="nh"><span class="n">{name}</span>{st}<span class="d">{desc}</span></div>'
            f'<div class="rings">{rings_html}</div>'
            f'<div class="ft">{sparkline(spark, w=110, stale=stale)}<span>CPU 1 小時</span><span style="margin-left:auto" class="mono">{last}</span></div>'
            f'<div class="ft">{seg}<span>{ct}</span></div>{extra}</a>')


def hostmap(groups: list, legend: bool = True) -> str:
    """主機地圖：每個節點一組，容器一格，顏色＝CPU 單色序列；失敗格加方塊、過期格加空心圓、停止格虛線。
    groups: [(node, [(container, cpu_pct, state ok|bad|stale|off)])]"""
    q = ["var(--q1)", "var(--q2)", "var(--q3)", "var(--q4)", "var(--q5)"]
    out = []
    for node, cells in groups:
        n_bad = sum(1 for _, _, st in cells if st == "bad")
        cs = "".join(
            f'<span class="hcell {st if st != "ok" else ""}" style="background: {q[min(4, int(v / 100 * 4.999))] if st != "off" else "transparent"}" title="{c} · CPU {v:.0f}%"></span>'
            for c, v, st in cells)
        bad = f' <span style="color:var(--danger)">{n_bad} 異常</span>' if n_bad else ""
        out.append(f'<div class="hgrp"><div class="lab"><b>{node}</b><span>{len(cells)} 個容器</span>{bad}</div><div class="hcells">{cs}</div></div>')
    lg = ""
    if legend:
        lg = ('<div class="scale" style="margin-top:2px">CPU' + "".join(f'<i style="background:{c}"></i>' for c in q) + '高 · <span class="hcell bad" style="width:16px;height:16px"></span>異常 · <span class="hcell stale" style="width:16px;height:16px"></span>過期 · <span class="hcell off" style="width:16px;height:16px"></span>已停止</div>')
    return f'<div class="hmap" role="img" aria-label="主機地圖，{len(groups)} 個節點">' + "".join(out) + lg + "</div>"


def tall_bar(parts: list, total: float, unit: str = "G") -> str:
    """高段條：parts [(value, label, color)]；每段印數值（塞得下才印），餘量算可用。"""
    used = sum(v for v, _, _ in parts)
    segs = []
    for v, lab, col in parts:
        w = v / total * 100
        light_bg = any(k in col for k in ("q4", "q5", "ok", "warn", "danger", "blue"))
        txt = f"{v:.0f}{unit}" if (w >= 9 and light_bg and "q4" not in col) else ""
        fg = "var(--hazelink)" if light_bg else "var(--ink)"
        segs.append(f'<i style="width:{w:.1f}%;background:{col};color:{fg}" title="{lab} {v:.0f}{unit}">{txt}</i>')
    free = max(0.0, total - used)
    fw = free / total * 100
    segs.append(f'<i style="width:{fw:.1f}%;background:var(--q1);color:var(--muted)" title="可用 {free:.0f}{unit}">{f"{free:.0f}{unit}" if fw >= 9 else ""}</i>')
    return '<span class="tall" aria-hidden="true">' + "".join(segs) + "</span>"


def calendar(states: str, start_label: str = "", end_label: str = "") -> str:
    """日曆熱格：每字一天，o 成功 f 失敗 r 部分 e 未排 n 未來；7 列一週，直向填。"""
    cls = {"o": "o", "f": "f", "r": "r", "e": "", "n": "n"}
    cells = "".join(f'<i class="{cls.get(c, "")}"></i>' for c in states)
    lab = f'<div class="note" style="display:flex;justify-content:space-between;margin-top:6px"><span>{start_label}</span><span>{end_label}</span></div>' if start_label else ""
    return (f'<div class="cal" role="img" aria-label="每日結果：{states.count("o")} 成功、{states.count("f")} 失敗、{states.count("r")} 部分">{cells}</div>' + lab)


def pipeline(stages: list) -> str:
    """流程段：stages [(name, state done|cur|wait|fail|todo, duration_text, detail)]；下方再畫一條依時長比例的段條。"""
    icon = {"done": "●", "cur": "■", "wait": "○", "fail": "■", "todo": "·"}
    chips = "".join(
        f'<div class="pstage {st}"><span class="t"><span aria-hidden="true" style="color:{ {"done":"var(--ok)","cur":"var(--blue)","wait":"var(--warn)","fail":"var(--danger)","todo":"var(--faint)"}[st] }">{icon[st]}</span>{n}</span>'
        f'<span class="mono">{dur}</span><span>{det}</span></div>' for n, st, dur, det in stages)
    return f'<div class="pipe">{chips}</div>'


def topology(columns: list, links: list, w: int = 1140, box_w: int = 164, box_h: int = 44, gap: int = 14) -> str:
    """拓撲圖：columns [(title, [(id, label, sub, state ok|warn|danger|dim)])]；links [(src, dst, weight 0–1, state)]。
    欄等距排列，節點垂直置中，連線為三次曲線，粗細依 weight。"""
    ncol = len(columns)
    rows = max(len(c[1]) for c in columns)
    h = 30 + rows * (box_h + gap)
    xs = [20 + i * (w - 40 - box_w) / (ncol - 1) for i in range(ncol)] if ncol > 1 else [20]
    pos = {}
    out = [f'<svg class="topo" viewBox="0 0 {w} {h}" role="img" aria-label="拓撲：{"→".join(c[0] for c in columns)}">']
    for ci, (title, nodes) in enumerate(columns):
        x = xs[ci]
        out.append(f'<text class="col" x="{x:.0f}" y="14">{title}</text>')
        total_h = len(nodes) * box_h + (len(nodes) - 1) * gap
        y0 = 30 + (rows * (box_h + gap) - gap - total_h) / 2
        for ni, (nid, label, sub, st) in enumerate(nodes):
            y = y0 + ni * (box_h + gap)
            pos[nid] = (x, y)
            shape = {"ok": f'<circle cx="{x+12}" cy="{y+box_h/2}" r="4" fill="var(--ok)"/>',
                     "warn": f'<circle cx="{x+12}" cy="{y+box_h/2}" r="4" fill="var(--panel)" stroke="var(--warn)" stroke-width="2"/>',
                     "danger": f'<rect x="{x+8}" y="{y+box_h/2-4}" width="8" height="8" fill="var(--danger)"/>',
                     "info": f'<rect x="{x+8}" y="{y+box_h/2-4}" width="8" height="8" fill="var(--blue)"/>',
                     "dim": f'<circle cx="{x+12}" cy="{y+box_h/2}" r="4" fill="none" stroke="var(--faint)" stroke-width="1.5"/>'}[st]
            out.append(f'<rect class="box {st if st in ("danger","warn") else ""}" x="{x}" y="{y:.1f}" width="{box_w}" height="{box_h}" rx="6"/>{shape}'
                       f'<text x="{x+22}" y="{y+18:.1f}">{label}</text><text class="sub" x="{x+22}" y="{y+33:.1f}">{sub}</text>')
    lk = []
    for src, dst, wgt, st in links:
        (x1, y1), (x2, y2) = pos[src], pos[dst]
        ax, ay = x1 + box_w, y1 + box_h / 2
        bx, by = x2, y2 + box_h / 2
        mx = (ax + bx) / 2
        lk.append(f'<path class="lnk {st}" stroke-width="{1.5 + wgt * 6:.1f}" d="M{ax:.0f},{ay:.1f} C{mx:.0f},{ay:.1f} {mx:.0f},{by:.1f} {bx:.0f},{by:.1f}"/>')
    out.insert(1, "".join(lk))
    out.append("</svg>")
    return "".join(out)


def flowlist(links: list) -> str:
    """拓撲的手機版：每條連線一列。links [(src_label, dst_label, note)]"""
    return '<div class="flowlist">' + "".join(
        f'<div class="fr"><span class="mono" style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap">{a}</span><span class="note">→</span>'
        f'<span class="mono" style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap">{b}</span><span class="note">{n}</span></div>' for a, b, n in links) + "</div>"



def matrix(rows: list, cols: list, cells: list, first_col: str = "minmax(110px,1.2fr)") -> str:
    """狀態矩陣：cells[r][c] 為 (kind ok|warn|dng|info|none, text)；kind none 畫虛線空格。"""
    out = ['<span></span>'] + [f'<span class="cl">{c}</span>' for c in cols]
    for r, row in zip(rows, cells):
        out.append(f'<span class="rl">{r}</span>')
        for kind, txt in row:
            if kind == "none":
                out.append(f'<span class="c empty">{txt or "—"}</span>')
            else:
                out.append(f'<span class="c"><span class="st {kind}"><i aria-hidden="true"></i>{txt}</span></span>')
    return f'<div class="mxw"><div class="mx" style="grid-template-columns:{first_col} repeat({len(cols)},minmax(0,1fr))">' + "".join(out) + "</div></div>"


def tree(items: list) -> str:
    """items: [(depth, name, size_text, on)]"""
    out = ['<div class="tree"><ul>']
    depth = 0
    for d, name, sz, on in items:
        while depth < d:
            out.append("<ul>"); depth += 1
        while depth > d:
            out.append("</ul>"); depth -= 1
        out.append(f'<li class="{"on" if on else ""}"><span>{name}</span><span class="sz">{sz}</span></li>')
    while depth > 0:
        out.append("</ul>"); depth -= 1
    out.append("</ul></div>")
    return "".join(out)


def scope_bar(items: list, current: int = 0, label: str = "範圍", extra: str = "") -> str:
    """範圍列：這一頁在看哪些節點／叢集。items [(label, sub)]；永遠放在頁面標題正下方。"""
    btns = "".join(f'<button type="button" class="btn{" on" if i == current else ""}" aria-pressed="{"true" if i == current else "false"}">{l}{f"<span class=mono>{sub}</span>" if sub else ""}</button>' for i, (l, sub) in enumerate(items))
    return f'<div class="scope" role="group" aria-label="{label}"><span class="lbl">{label}</span>{btns}{extra}</div>'


def legend(items: list) -> str:
    """圖例：items [(kind, label)]；kind 是 ok|warn|dng|info（狀態形狀）、color:<css>（色塊）、line:<px>（線寬）。"""
    out = []
    for kind, label in items:
        if kind in ("ok", "warn", "dng", "info"):
            out.append(f'<span class="st {kind}"><i aria-hidden="true"></i>{label}</span>')
        elif kind.startswith("color:"):
            out.append(f'<span><i class="sw" style="background:{kind[6:]}" aria-hidden="true"></i>{label}</span>')
        elif kind.startswith("line:"):
            out.append(f'<span><i class="ln" style="height:{kind[5:]}px" aria-hidden="true"></i>{label}</span>')
    return '<div class="lg" role="list">' + "".join(out) + "</div>"


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
