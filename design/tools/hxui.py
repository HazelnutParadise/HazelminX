"""HazelminX 設計原型的共用元件：token CSS、導覽、圖表（純 SVG，CSS hover）。

所有圖表只輸出靜態 SVG 標記，不用 JavaScript；hover 提示用 CSS。
數值全部是示例資料（假設），由 build_screens.py 以固定種子產生。
"""
from __future__ import annotations

import re
import math
from dataclasses import dataclass, field

FONT_LINK = ('<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Geist:wght@400;500;600'
             '&family=Geist+Mono:wght@400;500&family=Noto+Sans+TC:wght@400;500;700&display=swap">')

DARK = dict(bg="#161412", panel="#1D1B18", raised="#262320", line="#35312B", ink="#ECE6DA", muted="#A79E8E",
            faint="#9A917F", hazel="#D9A066", hazelink="#1A1410", use="#A39B8E", blue="#8DB4F2", warn="#F0B35B",
            danger="#F28B82", ok="#9BD3A4", s1="#AF711F", s2="#5682BB", s3="#47AA62", s4="#B55B9A",
            q1="#2A241C", q2="#4A3A22", q3="#6E5228", q4="#9A6F33", q5="#D9A066", scrim="rgba(0,0,0,.45)", hairline="#2B2825")
LIGHT = dict(bg="#F6F2EA", panel="#FFFDF8", raised="#EFE9DE", line="#D9D1C2", ink="#1B1814", muted="#5E574C",
             faint="#6B6457", hazel="#8F5B23", hazelink="#FFFFFF", use="#857C6F", blue="#2E5FA8", warn="#9A5A0B",
             danger="#B4382E", ok="#2F6B3C", s1="#7C4B02", s2="#2E598F", s3="#218A45", s4="#883170",
             q1="#F3E6D3", q2="#E3C9A4", q3="#CFA772", q4="#B38347", q5="#8F5B23", scrim="rgba(0,0,0,.25)", hairline="#ECE6DA")


def _vars(t: dict) -> str:
    return ";".join(f"--{k}:{v}" for k, v in t.items())


BASE_CSS = f"""
body{{margin:0}}
@font-face{{font-family:"HZX CJK Punct";src:local("PingFang TC"),local("Noto Sans TC"),local("Noto Sans CJK TC"),local("Microsoft JhengHei");unicode-range:U+3000-303F,U+FF00-FFEF}}.hb{{{_vars(DARK)};font-family:"HZX CJK Punct","Geist","Noto Sans TC","PingFang TC","Microsoft JhengHei",sans-serif;font-synthesis:none;line-break:strict;color:var(--ink);background:var(--bg);font-size:14px;line-height:1.5;color-scheme:dark}}
.hb.theme-light{{{_vars(LIGHT)};color-scheme:light}}
.hb *{{box-sizing:border-box}}
.hb .mono{{font-family:"Geist Mono",ui-monospace,Menlo,monospace;font-variant-numeric:tabular-nums}}
.hb a{{color:var(--ink);text-decoration:none}}.hb a:hover{{color:var(--hazel)}}
.hb button,.hb input,.hb select{{font:inherit;color:inherit}}
.hb button{{cursor:pointer}}
.hb :is(a,button,input,select,[tabindex]):focus-visible{{outline:2px solid var(--hazel);outline-offset:2px}}
.hb h1{{margin:0;font-size:18px;font-weight:600;line-height:1.3}}
.hb h2{{margin:0;font-size:14px;font-weight:600;line-height:1.4}}
.hb p{{margin:0}}
.hb .btn{{display:inline-flex;align-items:center;justify-content:center;gap:6px;min-height:32px;padding:0 12px;border-radius:4px;border:1px solid var(--line);background:var(--panel);color:var(--ink);font-size:13px;font-weight:500;white-space:nowrap}}
.hb .btn.pri{{background:var(--hazel);border-color:var(--hazel);color:var(--hazelink);font-weight:600}}
.hb .btn.dng{{background:var(--danger);border-color:var(--danger);color:var(--hazelink);font-weight:600}}
.hb .btn.sm{{min-height:28px;padding:0 8px;font-size:12px;color:var(--muted)}}
.hb .btn.sm.pri{{color:var(--hazelink)}}
.hb .btn:active{{transform:translateY(1px)}}
.hb .panel{{background:var(--panel);border:1px solid var(--line);border-radius:8px;overflow:hidden}}
.hb .ph{{display:flex;align-items:center;gap:10px;min-height:40px;padding:6px 16px;border-bottom:1px solid var(--hairline);flex-wrap:wrap}}
.hb .ph .sub{{font-size:12px;color:var(--muted)}}
.hb .ph .more{{margin-left:auto;font-size:13px;color:var(--hazel);min-height:32px;min-width:44px;padding:0 4px;display:inline-flex;align-items:center;justify-content:flex-end}}
.hb .foot a,.hb .note a,.hb a.note{{min-height:28px;display:inline-flex;align-items:center}}
.hb .inp input,.hb .inp select{{min-height:30px}}
.hb .card{{background:var(--panel);border:1px solid var(--line);border-radius:8px;padding:14px;display:flex;flex-direction:column;gap:8px}}
.hb .row{{display:grid;grid-template-columns:var(--t);gap:12px;align-items:center;min-height:40px;padding:0 16px;border-bottom:1px solid var(--hairline)}}
.hb .row.batch{{grid-template-columns:28px var(--t)}}
.hb .row .ck{{justify-self:start}}
.hb a.row{{cursor:pointer}}
.hb .row:hover{{background:var(--raised)}}
.hb .row:last-child{{border-bottom:0}}
.hb .hd{{font-size:12px;color:var(--muted);min-height:32px;font-weight:600;background:var(--raised)}}
.hb .hd:hover{{background:transparent}}
.hb .name{{display:flex;flex-direction:column;gap:2px;min-width:0}}
.hb .name .n{{font-family:"Geist Mono",ui-monospace,monospace;font-weight:500;overflow-wrap:anywhere;line-height:1.3}}
.hb .name .d{{font-size:12px;color:var(--faint);display:block;line-height:1.35;word-break:keep-all;overflow-wrap:anywhere}}
.hb .st>i{{flex:none}}
.hb .st{{display:inline-flex;align-items:center;gap:6px;max-width:100%;word-break:keep-all;overflow-wrap:anywhere;font-size:11px;line-height:16px;font-weight:500;padding:1px 8px 1px 7px;border-radius:9px;background:color-mix(in srgb,currentColor 13%,transparent)}}
.hb .lg .st,.hb .mx .st{{padding:1px 7px 1px 6px}}
.hb.theme-light .st{{background:var(--raised);box-shadow:inset 0 0 0 1px color-mix(in srgb,currentColor 35%,transparent)}}
.hb .st i{{display:inline-block;width:8px;height:8px;flex:none}}
.hb .st.ok{{color:var(--ok)}}.hb .st.ok i{{border-radius:50%;background:var(--ok)}}
.hb .st.warn{{color:var(--warn)}}.hb .st.warn i{{border-radius:50%;border:2px solid var(--warn)}}
.hb .st.dng{{color:var(--danger)}}.hb .st.dng i{{background:var(--danger)}}
.hb .st.info{{color:var(--blue)}}.hb .st.info i{{background:var(--blue)}}
.hb .st.off{{color:var(--muted)}}.hb .st.off i{{border-radius:50%;border:2px solid var(--muted);border-style:dashed}}
.hb .nv{{display:flex;align-items:center;gap:10px;min-height:32px;padding:0 12px;border-radius:8px;font-size:13px;color:var(--ink);border:0;background:transparent;width:100%;text-align:left}}
.hb .nv svg{{flex:none;color:var(--muted)}}.hb .nv.cur svg{{color:var(--hazel)}}
.hb .nv:hover{{background:var(--raised);color:var(--ink)}}
.hb .nv.cur{{background:var(--raised);color:var(--hazel);font-weight:600}}
.hb .nv .cnt{{margin-left:auto;display:inline-flex;align-items:center;gap:5px;padding:0 7px 0 6px;border-radius:999px;font-family:"Geist Mono",ui-monospace,monospace;font-size:11px;line-height:18px;color:var(--blue);background:color-mix(in srgb,var(--blue) 15%,transparent)}}
.hb .nv .cnt.dng{{color:var(--danger);background:color-mix(in srgb,var(--danger) 15%,transparent)}}
.hb .scrim{{position:fixed;inset:0;z-index:30;padding:72px 16px;background:color-mix(in srgb,#000 45%,transparent);display:flex;justify-content:center;align-items:flex-start}}.hb .cmdp{{width:100%;max-width:640px;background:var(--raised);border:1px solid var(--line);border-radius:12px;box-shadow:0 18px 48px rgb(0 0 0/.4);overflow:hidden}}.hb .cmdq{{display:flex;align-items:center;gap:10px;padding:12px 14px;border-bottom:1px solid var(--line)}}.hb .cmdl{{max-height:520px;overflow:auto;padding:4px 0}}.hb .cmdi{{display:flex;align-items:center;gap:10px;min-height:44px;padding:6px 12px;margin:0 6px;border-radius:6px}}.hb .cmdi:hover,.hb .cmdi.on{{background:var(--panel)}}.hb .cmdi .ic{{color:var(--muted);display:inline-flex}}.hb .cmdi .kbd{{margin-left:auto}}.hb .edwrap{{display:grid;grid-template-columns:minmax(0,2fr) minmax(300px,1fr);gap:16px;align-items:start}}@media (max-width:1100px){{.hb .edwrap{{grid-template-columns:1fr}}}}.hb .editor{{font-size:12.5px;line-height:1.75;padding:8px 0;overflow-x:auto;background:#14120F;border-radius:8px;color:#EDE6DC}}.hb .editor .ln{{display:flex;padding-right:14px;white-space:pre}}.hb .editor .no{{width:44px;flex:none;text-align:right;padding-right:14px;color:#8A8278;user-select:none}}.hb .editor .ln.dng{{background:color-mix(in srgb,var(--danger) 18%,transparent);box-shadow:inset 3px 0 var(--danger)}}.hb .editor .ln.warn{{background:color-mix(in srgb,var(--warn) 14%,transparent);box-shadow:inset 3px 0 var(--warn)}}.hb .side.mini{{width:56px;padding:12px 8px}}.hb .side.mini .nv span,.hb .side.mini .navgrp{{display:none}}.hb .side.mini .nv{{justify-content:center;padding:0;position:relative}}.hb .side.mini .nv .cnt{{display:inline-flex;position:absolute;top:0;right:-2px;padding:0 4px;font-size:10px;line-height:15px}}.hb .side.mini .nv .cnt .spin{{display:none}}.hb .top .brand.mini{{width:56px;padding:0 17px}}.hb .top .brand.mini span:not(.logo){{display:none}}
.hb .hostbar{{display:flex;flex-wrap:wrap;align-items:center;gap:6px 12px;padding:6px 16px;border-bottom:1px solid var(--line);background:color-mix(in srgb,var(--danger) 10%,var(--panel));font-size:13px}}.hb .hostbar .more{{margin-left:auto}}
.hb .tree .dis,.hb .row.dis{{opacity:.5;cursor:not-allowed}}.hb .row.dis .n,.hb .tree .dis .tn{{text-decoration:none}}
.hb .btn:disabled{{opacity:.45;cursor:not-allowed;filter:saturate(.4)}}.hb .ib:disabled{{opacity:.4;cursor:not-allowed}}.hb .cdown{{display:flex;flex-wrap:wrap;align-items:center;gap:10px 16px;padding:12px 14px;border:1px solid var(--danger);border-left-width:4px;border-radius:8px;background:var(--panel)}}.hb .cdown .t{{display:flex;flex-direction:column;gap:2px;min-width:0;flex:1 1 320px}}.hb .cdown .t strong{{font-size:14px}}.hb .cdown .acts2{{display:flex;flex-wrap:wrap;gap:8px}}.hb .stale{{opacity:.62}}.hb .savebar{{position:sticky;bottom:12px;z-index:12;display:flex;flex-wrap:wrap;align-items:center;gap:8px 12px;padding:10px 14px;border:1px solid var(--hazel);border-radius:8px;background:var(--raised);box-shadow:0 6px 24px rgb(0 0 0/.35)}}.hb .savebar .dot{{width:8px;height:8px;border-radius:50%;background:var(--hazel);flex:none}}.hb .savebar .btn.pri .kbd{{margin-left:6px;opacity:.8}}.hb .savebar .sbtns{{margin-left:auto;flex-wrap:nowrap}}@media (max-width:767px){{.hb .savebar .sbtns{{flex-wrap:wrap;margin-left:0}}.hb .savebar .kbd{{display:none}}}}
.hb .mini-diff{{display:grid;grid-template-columns:auto 1fr;gap:4px 12px;padding:8px 10px;border:1px solid var(--line);border-radius:6px;background:var(--panel);font-size:12.5px}}
.hb .grp{{display:flex;align-items:baseline;gap:10px;margin:10px 0 -4px;font-size:15px;font-weight:600}}.hb .grp .note{{font-weight:400}}
.hb .spin{{width:8px;height:8px;border-radius:50%;border:1.5px solid currentColor;border-top-color:transparent;flex:none}}
@media (prefers-reduced-motion:no-preference){{.hb .spin{{animation:hx-spin 1s linear infinite}}}}
@keyframes hx-spin{{to{{transform:rotate(360deg)}}}}
.hb .kbd{{font-family:"Geist Mono",ui-monospace,monospace;font-size:11px;color:var(--faint);border:1px solid var(--line);border-radius:4px;padding:1px 5px;margin-left:auto}}
.hb .navgrp{{font-size:12px;color:var(--faint);font-weight:500;padding:8px 12px 2px}}
.hb .navcol{{display:flex;flex-direction:column;gap:2px}}
.hb .tiles{{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(190px,100%),1fr));gap:0;background:var(--panel);border:1px solid var(--line);border-radius:8px;overflow:hidden}}
.hb .tile{{display:grid;grid-template-columns:minmax(0,1fr) auto;grid-template-rows:auto auto auto;column-gap:12px;row-gap:2px;align-items:center;padding:12px 16px;background:transparent;border:0;border-right:1px solid var(--hairline);min-width:0}}
.hb .tile .l,.hb .tile .tdl{{grid-column:1/-1}}.hb .tile .v{{grid-column:1;grid-row:2}}
.hb .tile:last-child{{border-right:0}}
.hb .tiles:has(>.tile:nth-child(4):last-child){{grid-template-columns:repeat(2,minmax(0,1fr))}}
.hb .tiles:has(>.tile:nth-child(4):last-child)>.tile:nth-child(2n){{border-right:0}}
.hb .tiles:has(>.tile:nth-child(4):last-child)>.tile:nth-child(-n+2){{border-bottom:1px solid var(--hairline)}}
@media (min-width:1400px){{.hb .tiles:has(>.tile:nth-child(4):last-child){{grid-template-columns:repeat(4,minmax(0,1fr))}}.hb .tiles:has(>.tile:nth-child(4):last-child)>.tile{{border-right:1px solid var(--hairline)}}.hb .tiles:has(>.tile:nth-child(4):last-child)>.tile:last-child{{border-right:0}}.hb .tiles:has(>.tile:nth-child(4):last-child)>.tile:nth-child(-n+2){{border-bottom:0}}}}
@media (min-width:1180px) and (max-width:1399px){{.hb:not(:has(.aside)) .tiles:has(>.tile:nth-child(4):last-child){{grid-template-columns:repeat(4,minmax(0,1fr))}}.hb:not(:has(.aside)) .tiles:has(>.tile:nth-child(4):last-child)>.tile{{border-right:1px solid var(--hairline)}}.hb:not(:has(.aside)) .tiles:has(>.tile:nth-child(4):last-child)>.tile:last-child{{border-right:0}}.hb:not(:has(.aside)) .tiles:has(>.tile:nth-child(4):last-child)>.tile:nth-child(-n+2){{border-bottom:0}}}}
@media (max-width:640px){{.hb .tiles:has(>.tile:nth-child(4):last-child){{grid-template-columns:minmax(0,1fr)}}.hb .tiles:has(>.tile:nth-child(4):last-child)>.tile{{border-right:0;border-bottom:1px solid var(--hairline)}}.hb .tiles:has(>.tile:nth-child(4):last-child)>.tile:last-child{{border-bottom:0}}}}
.hb .tile svg.sp{{width:88px;height:22px;grid-column:2;grid-row:2;align-self:end}}
.hb .tile .l{{font-size:12px;color:var(--muted)}}
.hb .tile .v{{font-size:20px;font-weight:600;line-height:1.2;display:flex;align-items:baseline;gap:6px;flex-wrap:wrap}}
.hb .tile .v small{{font-size:12px;font-weight:400;color:var(--muted)}}
.hb .tile .tdl{{font-size:12px;color:var(--muted);display:flex;align-items:center;gap:8px;flex-wrap:wrap}}
.hb .up{{color:var(--ok)}}.hb .dn{{color:var(--danger)}}
.hb .meter{{display:flex;align-items:center;gap:4px 8px;min-width:0}}
.hb .meter .tr{{flex:1 1 40px;height:6px;border-radius:4px;background:var(--q2);overflow:hidden;min-width:40px}}
.hb .meter .fl{{height:100%;border-radius:4px;background:var(--use)}}
.hb .meter .fl.warn{{background:var(--warn)}}.hb .meter .fl.dng{{background:var(--danger)}}
.hb .meter .num{{font-family:"Geist Mono",ui-monospace,monospace;font-variant-numeric:tabular-nums;font-size:12px;white-space:nowrap}}
.hb .cg{{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(440px,100%),1fr));gap:16px}}
.hb .cg:has(.tw,.mxw){{grid-template-columns:repeat(auto-fit,minmax(min(620px,100%),1fr))}}
.hb .chart{{background:var(--panel);border:1px solid var(--line);border-radius:8px;overflow:hidden;min-width:0}}
.hb .chart .ph{{padding:8px 12px}}
.hb .chart .unit{{font-size:12px;color:var(--faint)}}
.hb .chart .legend{{display:flex;gap:12px;flex-wrap:wrap;font-size:12px;color:var(--muted);margin-left:auto}}
.hb .legend{{display:flex;flex-wrap:wrap;align-items:center;gap:4px 14px;font-size:12px;color:var(--muted)}}
.hb .legend>span{{display:inline-flex;align-items:center;gap:6px;white-space:nowrap}}
.hb .legend i{{display:inline-block;width:12px;height:2px;border-radius:1px;flex:none}}
.hb .legend i.rect{{height:8px;border-radius:2px}}
.hb .chart .body{{padding:6px 10px 8px}}
.hb .foot{{display:flex;gap:10px;flex-wrap:wrap;padding:6px 12px 8px;font-size:12px;color:var(--faint);border-top:1px solid var(--line)}}
.hb .chart .foot a{{color:var(--hazel)}}
.hb svg.ch{{display:block;width:100%;height:auto;font-family:"Geist Mono",ui-monospace,monospace;font-variant-numeric:tabular-nums}}
.hb svg.ch text{{font-size:11px;fill:var(--faint)}}
.hb .grid{{stroke:var(--line);stroke-width:1}}
.hb .ts{{position:relative;font-family:"Geist Mono",ui-monospace,monospace;font-variant-numeric:tabular-nums;font-size:11px;color:var(--faint);padding:8px 8px 20px 46px}}.hb .ts .pa{{position:relative;height:100%}}.hb .ts .plot{{display:block;width:100%;height:100%;overflow:visible}}.hb .ts .yl{{position:absolute;left:0;width:40px;top:8px;bottom:20px}}.hb .ts .yl span{{position:absolute;right:0;transform:translateY(-50%);white-space:nowrap}}.hb .ts .xl{{position:absolute;left:46px;right:8px;bottom:0;height:16px}}.hb .ts .xl span{{position:absolute;transform:translateX(-50%);white-space:nowrap}}.hb .ts .xl .a0{{transform:none}}.hb .ts .xl .a1{{transform:translateX(-100%)}}.hb .ts .mk{{position:absolute;top:0;color:var(--muted);white-space:nowrap;line-height:1}}.hb .ts .sdot{{position:absolute;width:9px;height:9px;border-radius:50%;background:var(--panel);border:2px solid;transform:translate(-50%,-50%)}}.hb .ts .hits{{position:absolute;inset:0}}.hb .ts .hit{{position:absolute;top:0;bottom:0;outline:none}}.hb .ts .xh{{position:absolute;top:0;bottom:0;width:1px;background:var(--muted);opacity:0}}.hb .ts .tip{{position:absolute;top:0;left:calc(100% + 6px);z-index:5;display:none;grid-template-columns:auto auto;gap:3px 14px;min-width:120px;padding:6px 8px;border:1px solid var(--line);border-radius:6px;background:var(--raised);color:var(--ink);white-space:nowrap;opacity:0;pointer-events:none;box-shadow:0 4px 12px rgb(0 0 0/.2)}}.hb .ts .hit.flip .tip{{left:auto;right:calc(100% + 6px)}}.hb .ts .tip .k{{color:var(--muted);display:flex;align-items:center;gap:6px}}.hb .ts .tip .k i{{width:8px;height:8px;border-radius:2px}}.hb .ts .tip b{{font-weight:500;text-align:right}}.hb .ts .hit:hover .xh,.hb .ts .hit:focus-visible .xh{{opacity:1}}.hb .ts .hit:hover .tip,.hb .ts .hit:focus-visible .tip{{display:grid;opacity:1}}
.hb .ch-m{{display:none}}
.hb .ranks{{display:grid;grid-template-columns:fit-content(46%) minmax(72px,1fr) auto;gap:6px 10px;padding:10px 14px}}
.hb .rank{{display:grid;grid-template-columns:subgrid;grid-column:1/-1;align-items:center;font-size:13px;min-height:28px}}
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
.hb .ncard{{display:flex;flex-direction:column;gap:10px;padding:12px;background:var(--panel);border:1px solid var(--line);border-radius:8px;min-width:0;color:var(--ink)}}
.hb .ncard:hover{{border-color:var(--faint)}}
.hb .ncard .nh{{display:flex;align-items:center;gap:8px;flex-wrap:wrap}}
.hb .ncard .nh .n{{font-family:"Geist Mono",ui-monospace,monospace;font-weight:500}}
.hb .ncard .nh .d{{font-size:12px;line-height:18px;color:var(--faint);width:100%;min-height:36px}}
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
.hb .pipe{{display:grid;grid-template-columns:repeat(auto-fit,minmax(max(84px,calc((100% - (var(--n,4) - 1)*18px)/var(--n,4))),1fr));align-items:stretch;gap:10px 18px}}
.hb .pstage{{display:flex;flex-direction:column;gap:4px;min-width:0;padding:8px 10px;border:1px solid var(--line);border-radius:6px;font-size:12px;color:var(--muted);position:relative;background:var(--panel)}}
.hb .pstage+.pstage::before{{content:"";position:absolute;left:-12px;top:50%;width:6px;height:6px;border-top:2px solid var(--faint);border-right:2px solid var(--faint);transform:translateY(-50%) rotate(45deg)}}
.hb .pstage .t{{color:var(--ink);font-weight:500;display:flex;align-items:center;gap:6px}}
.hb .pstage>*{{overflow-wrap:anywhere}}
.hb .pstage.done{{border-color:var(--ok)}}.hb .pstage.cur{{border-color:var(--blue);background:var(--raised)}}.hb .pstage.fail{{border-color:var(--danger)}}.hb .pstage.wait{{border-color:var(--warn)}}
.hb .topow{{overflow:auto;max-width:100%}}
.hb svg.topo{{display:block;font-family:"Geist","Noto Sans TC",sans-serif}}
.hb svg.topo a:hover .box{{stroke:var(--hazel)}}
.hb svg.topo text{{font-size:12px;fill:var(--ink)}}
.hb svg.topo text.sub{{font-size:11px;fill:var(--faint);font-family:"Geist Mono",ui-monospace,monospace}}
.hb svg.topo text.col{{font-size:11px;fill:var(--faint);font-weight:500}}
.hb svg.topo .box{{fill:var(--raised);stroke:var(--line)}}
.hb svg.topo .box.danger{{stroke:var(--danger)}}.hb svg.topo .box.warn{{stroke:var(--warn)}}
.hb svg.topo .lnk{{fill:none;stroke:var(--s1);stroke-opacity:.55;stroke-linecap:round}}
.hb svg.topo .lnk.danger{{stroke:var(--danger)}}.hb svg.topo .lnk.dim{{stroke:var(--q3)}}
.hb .flowlist{{display:flex;flex-direction:column;gap:6px;font-size:13px}}
.hb .flowlist .fr{{display:grid;grid-template-columns:minmax(0,1fr) 16px minmax(0,1fr) auto;gap:6px;align-items:center}}
.hb .mx{{display:grid;gap:2px;font-size:12px}}
.hb .mx .st,.hb .mx .c{{white-space:nowrap}}
.hb .mx .c{{min-height:32px;display:flex;align-items:center;justify-content:center;gap:4px;background:var(--raised);border-radius:4px;padding:0 6px;text-align:center}}
.hb .mx .c.empty{{background:transparent;border:1px dashed var(--line);color:var(--faint)}}.hb .mxlg{{display:flex;align-items:center;gap:6px;margin-top:8px;font-size:12px;color:var(--muted)}}.hb .mxlg .c{{display:inline-block;width:18px;height:12px;border:1px dashed var(--line);border-radius:3px}}
.hb .mx .rl{{display:flex;align-items:center;white-space:normal;overflow-wrap:anywhere;font-family:"Geist Mono",ui-monospace,monospace;color:var(--muted);min-height:32px;line-height:1.3}}
.hb .mx .cl{{display:flex;align-items:flex-end;justify-content:center;font-size:11px;color:var(--faint);padding-bottom:4px;text-align:center}}
.hb .tree{{font-family:"Geist Mono",ui-monospace,monospace;font-size:13px}}
.hb .tree ul{{list-style:none;margin:0;padding-left:16px;border-left:1px solid var(--line)}}
.hb .tree>ul{{padding-left:0;border-left:0}}
.hb .tree li{{display:flex;align-items:center;gap:8px;min-height:30px}}
.hb .tree li .sz{{margin-left:auto;color:var(--faint);font-size:12px}}
.hb .tree li.on,.hb .tree summary.on{{color:var(--hazel)}}
.hb .tree summary{{display:flex;align-items:center;gap:6px;min-height:30px;cursor:pointer;list-style:none;padding:0 6px;border-radius:4px}}
.hb .tree summary::-webkit-details-marker{{display:none}}
.hb .tree summary:hover,.hb .tree li.leaf:hover{{background:var(--raised)}}
.hb .tree summary svg:first-child{{transition:transform .12s;color:var(--faint)}}
.hb .tree details[open]>summary svg:first-child{{transform:rotate(90deg)}}
.hb .tree li.leaf{{padding:0 6px;border-radius:4px;gap:6px}}
.hb .tree .pad{{width:14px;flex:none}}
.hb .tree .tn{{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}}
.hb .tree svg{{flex:none;color:var(--muted)}}
.hb .fm{{display:grid;gap:0;background:var(--panel);border:1px solid var(--line);border-radius:8px;overflow:hidden;min-height:420px}}
.hb .fm>*{{min-width:0}}
.hb .fm .pane{{border-right:1px solid var(--line);display:flex;flex-direction:column}}
.hb .fm .pane:last-child{{border-right:0}}
.hb .fm .pt{{display:flex;align-items:center;gap:8px;min-height:40px;padding:0 12px;border-bottom:1px solid var(--line);font-size:12px;color:var(--muted)}}
.hb .toolbar{{display:flex;align-items:center;gap:4px;flex-wrap:wrap;padding:6px 10px;background:var(--panel);border:1px solid var(--line);border-radius:8px}}
.hb .toolbar .ib{{min-height:34px;gap:6px;padding:0 10px}}
.hb .toolbar .ib[aria-disabled=true]{{opacity:.45}}
.hb .toolbar .sep{{width:1px;height:20px;background:var(--line);margin:0 4px}}
.hb .crumbs{{display:flex;align-items:center;gap:2px;flex-wrap:wrap;font-family:"Geist Mono",ui-monospace,monospace;font-size:13px}}
.hb .crumbs a{{padding:4px 6px;border-radius:4px;color:var(--muted)}}
.hb .crumbs a:hover{{background:var(--raised);color:var(--ink)}}
.hb .crumbs a:last-child{{color:var(--ink);font-weight:500}}
.hb .crumbs .sep{{color:var(--faint)}}
.hb .frow{{--t:28px minmax(0,2fr) 90px 110px 100px}}
.hb .fm{{grid-template-columns:240px minmax(0,1fr) 280px}}
.hb .frow.sel{{background:color-mix(in srgb,var(--hazel) 12%,transparent)}}
.hb .ck{{display:inline-flex;align-items:center;justify-content:center;width:28px;min-height:28px}}
.hb .ck input{{width:16px;height:16px;accent-color:var(--hazel);margin:0}}.hb input.lockck{{accent-color:var(--hazel);pointer-events:none;cursor:not-allowed;opacity:.75}}
.hb .hd .sortable{{display:inline-flex;align-items:center;gap:4px;cursor:pointer}}
.hb .hd .sortable svg{{color:var(--faint);opacity:.5}}
.hb .hd .sortable.on svg{{color:var(--hazel);opacity:1}}
.hb .pager{{display:flex;align-items:center;gap:8px;padding:8px 16px;border-top:1px solid var(--line);font-size:12px;color:var(--muted)}}
.hb .pager .ib{{min-height:32px;min-width:32px}}
.hb .pager .pg{{margin-left:auto;display:flex;align-items:center;gap:4px}}
.hb .dl{{display:grid;grid-template-columns:auto minmax(0,1fr);gap:6px 12px;font-size:13px;padding:10px 12px}}
.hb .dl dt{{color:var(--muted);font-size:12px;white-space:nowrap}}.hb .dl dd{{margin:0;overflow-wrap:anywhere}}
.hb .appcard{{display:flex;gap:10px;padding:12px;background:var(--raised);border:1px solid var(--line);border-radius:8px;min-width:0;color:var(--ink);align-items:flex-start}}
.hb .appcard .ic{{width:40px;height:40px;border-radius:8px;background:var(--q2);display:flex;align-items:center;justify-content:center;font-family:"Geist Mono",ui-monospace,monospace;font-weight:700;color:var(--ink);flex:none}}
.hb .appcard .bd{{display:flex;flex-direction:column;gap:4px;min-width:0;flex:1}}
.hb .appcard .bd .n{{font-weight:500}}
.hb .appcard .bd .d{{font-size:12px;color:var(--faint)}}
.hb .chip{{display:inline-flex;align-items:center;min-height:22px;padding:0 8px;border-radius:999px;border:1px solid var(--line);font-size:11px;font-weight:500;color:var(--muted);background:transparent}}
.hb .chip.on{{color:var(--hazel);border-color:var(--hazel)}}
.hb .term,.hb .diff{{--ink:#ECE6DA;--muted:#A79E8E;--faint:#9A917F;--panel:#14120F;--line:#35312B;--ok:#9BD3A4;--warn:#F0B35B;--danger:#F28B82;--blue:#8DB4F2}}
.hb .term{{background:#14120F;border:1px solid var(--line);border-radius:8px;font-family:"Geist Mono",ui-monospace,monospace;font-size:12px;line-height:1.6;padding:10px 12px;color:var(--ink);white-space:pre;overflow:auto}}
.hb .lg{{display:flex;gap:12px;flex-wrap:wrap;align-items:center;font-size:12px;color:var(--muted);margin-left:auto}}
.hb .lg span{{display:inline-flex;align-items:center;gap:5px}}
.hb .lg i.sw{{display:inline-block;width:12px;height:8px;border-radius:2px}}.hb .tall-lg{{margin:6px 0 0;gap:4px 12px}}.hb .tall-lg i.sw{{margin-right:5px;box-shadow:inset 0 0 0 1px var(--line)}}
.hb .lg i.ln{{display:inline-block;width:18px;border-radius:2px;background:var(--s1)}}
.hb .lg .st{{gap:5px}}
.hb .scope{{display:flex;align-items:center;gap:8px;flex-wrap:wrap;padding:8px 12px;background:var(--panel);border:1px solid var(--line);border-radius:8px}}
.hb .scope .lbl{{font-size:12px;color:var(--faint);font-weight:500}}
.hb .scope .btn{{min-height:28px;padding:0 10px;font-size:12px;color:var(--muted);gap:6px;background:transparent}}
.hb .scope .btn.on{{background:var(--raised);color:var(--ink);border-color:var(--hazel);font-weight:600}}
.hb .scope .btn .mono{{color:var(--faint);font-size:11px}}
.hb .range{{display:flex;gap:4px;flex-wrap:wrap}}
.hb .range .btn{{min-height:28px;padding:0 10px;font-size:12px;color:var(--muted);background:transparent}}
.hb .range .btn.on{{background:var(--raised);color:var(--hazel);border-color:var(--line);font-weight:600}}
.hb .field{{display:flex;flex-direction:column;gap:6px}}.hb .field>.st{{align-self:flex-start}}
.hb .field label{{font-size:12px;color:var(--muted)}}
.hb .inp{{display:flex;align-items:center;gap:8px;min-height:36px;padding:0 10px;background:var(--panel);border:1px solid var(--line);border-radius:6px;color:var(--ink);width:100%}}
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
.hb .job>.t{{display:flex;justify-content:space-between;gap:8px}}.hb .job>.t .st{{flex:none;white-space:nowrap;align-self:flex-start}}
.hb .job .s{{font-size:12px;color:var(--muted)}}
.hb .prog{{height:4px;background:var(--q2);border-radius:2px;overflow:hidden}}
.hb .prog i{{display:block;height:100%;background:var(--blue)}}
.hb .prog.pulse i{{width:30%;background:repeating-linear-gradient(90deg,var(--blue) 0 8px,transparent 8px 12px)}}
.hb .note{{font-size:12px;color:var(--faint)}}
.hb .wrap{{display:flex;flex-wrap:wrap;gap:8px;align-items:center}}
.hb .scroll{{overflow-x:auto}}
.hb .mtitle{{display:none}}.hb .aside{{width:340px;border-left:1px solid var(--line);background:var(--panel);display:flex;flex-direction:column;gap:18px;padding:18px 16px 24px}}
.hb .main{{flex:999 1 560px;min-width:0;display:flex;flex-direction:column;gap:16px;padding:16px 24px 40px}}
.hb .layout{{display:flex;flex-wrap:wrap;align-items:stretch;flex:1}}
.hb .side{{width:220px;border-right:1px solid var(--line);background:var(--panel);display:flex;flex-direction:column;gap:10px;padding:12px 10px 16px}}
.hb .side .coll{{margin-top:auto;color:var(--muted)}}
.hb .top{{display:flex;align-items:center;gap:14px;min-height:48px;padding:0 16px 0 0;border-bottom:1px solid var(--line);background:var(--panel)}}
.hb .brand{{display:flex;align-items:center;gap:10px;width:220px;padding:0 16px;font-weight:600;font-size:14px;border-right:1px solid var(--line);align-self:stretch}}
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
.hb .nrow{{--t:minmax(0,1.5fr) minmax(0,.9fr) minmax(0,.9fr) minmax(0,1fr) minmax(0,1fr) 110px minmax(0,.8fr)}}
.hb .prow{{--t:minmax(0,1.2fr) minmax(0,1.5fr) minmax(0,.5fr) minmax(0,.6fr) minmax(0,1fr) minmax(0,1.1fr)}}
.hb .srow{{--t:minmax(0,1.5fr) minmax(0,.9fr) minmax(0,.9fr) minmax(0,.9fr) minmax(0,.9fr) 110px minmax(0,.7fr)}}
.hb .crow{{--t:minmax(0,1.6fr) minmax(0,1fr) minmax(0,.5fr) minmax(0,.9fr) minmax(0,.9fr) minmax(0,1fr) minmax(0,.4fr) minmax(0,.7fr) 130px}}
.hb .row.cols-3{{--t:minmax(0,1.6fr) minmax(0,1fr) minmax(0,1fr)}}
.hb .row.cols-4{{--t:minmax(0,1.6fr) minmax(0,1fr) minmax(0,1fr) minmax(0,1fr)}}
.hb .row.cols-5{{--t:minmax(0,1.6fr) minmax(0,1fr) minmax(0,1fr) minmax(0,1fr) minmax(0,1fr)}}
.hb .row.cols-6{{--t:minmax(0,1.6fr) minmax(0,1fr) minmax(0,.9fr) minmax(0,1fr) minmax(0,1fr) minmax(0,1fr)}}
.hb .row.acts-end{{--t:minmax(0,1.6fr) minmax(0,1fr) minmax(0,1fr) minmax(0,1.2fr) 130px}}
.hb .rank .mono,.hb .flowlist .mono{{white-space:normal!important;overflow-wrap:anywhere;line-height:1.3}}
.hb .mxw,.hb .heatw{{overflow-x:auto}}
.hb .meter .tr{{min-width:24px}}.hb .meter .num{{flex:none}}
.hb .pipe{{padding-bottom:4px}}
.hb .row > span:last-child:has(> .btn),.hb .row > .acts{{justify-self:end}}
.hb .drow{{--t:minmax(0,.8fr) minmax(0,1.6fr) minmax(0,.6fr) minmax(0,1.4fr) minmax(0,.8fr)}}
.hb .gprow{{--t:minmax(0,1.4fr) minmax(0,.9fr) minmax(0,.5fr) minmax(0,1.2fr)}}
.hb .garow{{--t:minmax(0,.8fr) minmax(0,1.5fr) minmax(0,1.3fr) minmax(0,1.4fr)}}
.hb .dprow{{--t:minmax(0,.5fr) minmax(0,1.6fr) minmax(0,.7fr) minmax(0,.7fr) minmax(0,1.4fr)}}
.hb .hrow{{--t:minmax(0,1.6fr) minmax(0,.8fr) minmax(0,.7fr) minmax(0,1.4fr)}}
.hb .rrow{{--t:24px minmax(0,1.5fr) minmax(0,1.3fr) minmax(0,.5fr) minmax(0,.6fr)}}
@media (max-width:1280px){{.hb .frow{{--t:28px minmax(0,2fr) 90px 100px}}.hb .frow>:nth-child(4){{display:none}}}}
@media (max-width:1024px){{.hb .c5{{display:none}}.hb .crow{{--t:minmax(0,1.6fr) minmax(0,1fr) minmax(0,.5fr) minmax(0,.9fr) minmax(0,.9fr) 100px}}.hb .acts .ib:nth-child(n+3){{display:none}}}}
@media (max-width:900px){{.hb .side{{order:-3}}.hb .mtitle{{display:block;order:-2;padding:14px 16px 0;font-size:20px}}.hb .layout:has(.mtitle) .main h1{{display:none}}.hb .aside{{order:-1;width:100%;border-left:0;border-bottom:1px solid var(--line)}}.hb #attention article:nth-of-type(n+3){{display:none}}.hb #attention .mall{{display:inline-flex}}}}
.hb #attention .mall{{display:none}}
@media (max-width:900px){{.hb .top{{flex-wrap:wrap;padding:8px 12px;gap:8px;min-height:0}}.hb .crumb{{display:none}}.hb .top .search{{order:3;flex:1 1 100%;max-width:none;margin:0}}.hb .obs{{display:none}}.hb .ib.user span{{display:none}}.hb .side{{width:100%;border-right:0;border-bottom:1px solid var(--line);flex-direction:row;flex-wrap:nowrap;overflow-x:auto;gap:4px;padding:8px 12px}}.hb .navgrp{{display:none}}.hb .navcol{{flex-direction:row;flex:none;gap:4px}}.hb .side .nv{{width:auto;min-width:44px;justify-content:center;min-height:36px;padding:0 10px;white-space:nowrap;font-size:13px}}.hb .side .nv .cnt{{margin-left:4px}}.hb .side .coll{{display:none}}.hb .nv svg{{display:none}}.hb .brand{{width:auto;border:0;padding:0 12px}}}}
@media (max-width:1100px){{.hb .aside{{width:100%;border-left:0;border-top:1px solid var(--line)}}.hb .fm{{grid-template-columns:220px minmax(0,1fr)}}.hb .fm .pane.detp{{grid-column:1/-1;border-top:1px solid var(--line)}}}}
@media (max-width:767px){{
 .hb .meter{{flex-wrap:wrap}}
 .hb .side{{width:100%;border-right:0;border-bottom:1px solid var(--line);flex-direction:row;flex-wrap:nowrap;overflow-x:auto;gap:4px;padding:8px 12px}}
 .hb .navgrp{{display:none}}.hb .navcol{{flex-direction:row;flex:none;gap:4px}}.hb .side .nv{{width:auto;min-width:44px;justify-content:center;min-height:36px;padding:0 10px;white-space:nowrap;font-size:13px}}.hb .side .nv .cnt{{margin-left:4px}}
 .hb .main{{padding:14px 16px 32px;gap:16px}}.hb .aside{{padding:14px 16px 24px}}
 .hb .c3,.hb .c4,.hb .c5,.hb .kbd{{display:none}}
 .hb .top{{flex-wrap:wrap;padding:8px 12px;gap:8px;min-height:0}}.hb .top .search{{min-height:44px}}.hb .brand{{width:auto;border:0;padding:0}}.hb .crumb{{display:none}}.hb .top .search{{order:3;flex:1 1 100%;max-width:none;margin:0}}.hb .obs{{display:none}}.hb .ib.user span{{display:none}}
 .hb .side .coll{{display:none}}.hb .nv svg{{display:none}}
 .hb .row[class*="cols-"],.hb .row.acts-end{{--t:minmax(0,1.4fr) minmax(0,1fr) auto}}
 .hb .acts .ib:nth-child(n+3){{display:none}}
 .hb .rank{{grid-template-columns:minmax(0,1fr) auto;row-gap:4px}}.hb .rank .bar{{grid-column:1/-1}}
 .hb .mx{{min-width:620px}}.hb .heat{{min-width:720px}}
 .hb .fm{{grid-template-columns:minmax(0,1fr)}}.hb .fm .pane{{border-right:0;border-bottom:1px solid var(--line)}}.hb .fm .pane.treep{{max-height:260px;overflow:auto}}
 .hb .frow{{--t:28px minmax(0,1fr) 70px 60px}}.hb .frow>:nth-child(n+5){{display:none}}
 .hb .mx .rl{{white-space:normal}}
 .hb .ch-d{{display:none}}.hb .ch-m{{display:block}}
 .hb .heat i{{min-height:18px}}
 .hb .steps{{grid-template-columns:repeat(2,minmax(0,1fr))}}
 .hb .btn,.hb .btn.sm,.hb .range .btn,.hb .scope .btn,.hb .ib,.hb .acts .ib,.hb .tab,.hb .side .nv,.hb .ph .more,.hb .foot a,.hb a.note,.hb .brand,.hb .inp{{min-height:44px}}.hb .inp input,.hb .inp select{{min-height:40px}}.hb .ib,.hb .acts .ib{{min-width:44px}}
 .hb .nrow{{--t:minmax(0,1.5fr) minmax(0,1fr) minmax(0,.9fr)}}
 .hb .prow{{--t:minmax(0,1fr) minmax(0,1.3fr)}}
 .hb .srow{{--t:minmax(0,1.4fr) minmax(0,1fr) minmax(0,.8fr)}}
 .hb .crow{{--t:minmax(0,1.4fr) minmax(0,1fr)}}
 .hb .drow{{--t:minmax(0,.9fr) minmax(0,1.2fr) minmax(0,1.3fr)}}
 .hb .gprow{{--t:minmax(0,1.2fr) minmax(0,.8fr) minmax(0,1fr)}}
 .hb .garow{{--t:minmax(0,.8fr) minmax(0,1.4fr) minmax(0,1.2fr)}}
 .hb .dprow{{--t:minmax(0,.5fr) minmax(0,1.4fr) minmax(0,1.3fr)}}
 .hb .hrow{{--t:minmax(0,1.4fr) minmax(0,.7fr) minmax(0,1.2fr)}}
 .hb .rrow{{--t:24px minmax(0,1.6fr) minmax(0,.8fr)}}
 .hb .tile{{border-right:0;border-bottom:1px solid var(--hairline)}}.hb .tile:last-child{{border-bottom:0}}
 .hb .rings svg{{width:52px;height:52px}}
 .hb .hcell{{width:36px;height:36px}}
 .hb .tabs{{flex-wrap:nowrap;overflow-x:auto}}.hb .tab{{white-space:nowrap;flex:none}}
 .hb .ph .wrap{{width:100%}}
 .hb .pipe{{grid-template-columns:repeat(2,minmax(0,1fr))}}.hb .ts .xo{{display:none}}.hb .diff div{{white-space:pre-wrap;overflow-wrap:anywhere}}
}}
.hb .tw{{display:grid;grid-template-columns:var(--tc);overflow-x:auto;align-content:start}}
.hb .tw>*{{grid-column:1/-1;min-width:0}}
.hb .tw>.row,.hb .tw>.row.batch{{grid-template-columns:subgrid;column-gap:12px}}
.hb .tw>.row:last-child{{border-bottom:0}}
.hb .tw .row .st,.hb .tw .row>.mono,.hb .tw .row>span>.mono,.hb .tw .row .num,.hb .tw .hd>span{{white-space:nowrap}}
.hb .tw .row>.wr{{white-space:normal;overflow-wrap:anywhere}}
.hb .imps{{display:inline-flex;gap:6px;align-items:center}}
.hb .imp{{display:inline-grid;place-items:center;width:24px;height:24px;border-radius:6px;background:color-mix(in srgb,currentColor 13%,transparent);flex:none;cursor:help}}
.hb .lg .imp{{width:20px;height:20px;cursor:default;margin-right:2px}}
.hb .imp.ok{{color:var(--ok)}}.hb .imp.warn{{color:var(--warn)}}.hb .imp.dng{{color:var(--danger)}}
.hb .imp:focus-visible{{outline:2px solid var(--hazel);outline-offset:2px}}
.hb [data-tip]:hover,.hb [data-tip]:focus-visible{{anchor-name:--imp}}
.hb [data-tip]:hover::after,.hb [data-tip]:focus-visible::after{{content:attr(data-tip);position:fixed;position-anchor:--imp;position-area:bottom span-left;position-try-fallbacks:bottom span-right,top span-left;margin:6px 0;z-index:50;white-space:normal;width:max-content;max-width:320px;line-height:1.45;padding:6px 10px;border-radius:6px;font-size:12px;font-weight:500;color:var(--ink);background:var(--raised);border:1px solid var(--line);box-shadow:0 4px 12px rgb(0 0 0 / .25);pointer-events:none}}
.hb .ttabs{{display:flex;align-items:stretch;border-bottom:1px solid var(--line);overflow-x:auto}}
.hb .ttab{{display:flex;align-items:center;gap:8px;height:40px;padding:0 6px 0 12px;border-right:1px solid var(--hairline);white-space:nowrap;color:var(--muted);font-size:13px;flex:none}}
.hb .ttab.on{{color:var(--ink);background:var(--raised);box-shadow:inset 0 -2px 0 var(--hazel)}}
.hb .ttab .dot{{width:8px;height:8px;border-radius:50%;flex:none}}.hb .ttab .dot.ok{{background:var(--ok)}}.hb .ttab .dot.warn{{background:var(--warn)}}.hb .ttab .dot.info{{background:var(--blue)}}
.hb .ttab .tn{{font-family:"Geist Mono",ui-monospace,monospace;font-weight:500}}.hb .ttab .tu{{color:var(--faint);font-size:12px}}
.hb .ttab .ib{{min-width:24px;min-height:24px;width:24px;height:24px}}
.hb .ttabs .tend{{margin-left:auto;display:flex;align-items:center;gap:4px;padding:0 8px;flex:none}}
.hb .ttabs .tadd{{display:flex;align-items:center;padding:0 6px;flex:none}}
.hb .ed{{border:1px solid var(--line);border-radius:8px;overflow:hidden;background:var(--panel);display:flex;flex-direction:column}}
.hb .ed .ttab .mod{{width:8px;height:8px;border-radius:50%;background:var(--hazel);flex:none}}
.hb .edtb{{display:flex;align-items:center;gap:4px;flex-wrap:wrap;padding:6px 8px;border-bottom:1px solid var(--line)}}
.hb .edtb .btn.pri{{gap:6px}}.hb .edtb .kbd{{font-size:11px;opacity:.8}}
.hb .edtb .sep{{width:1px;height:20px;background:var(--line);margin:0 4px}}
.hb .edb{{font-family:"Geist Mono",ui-monospace,monospace;font-size:13px;line-height:20px;padding:8px 0;overflow-x:auto;background:var(--panel);min-height:240px}}
.hb .el{{display:grid;grid-template-columns:48px max-content;min-width:100%}}.hb .el.cur{{background:var(--raised)}}
.hb .el .gn{{text-align:right;padding-right:14px;color:var(--faint);user-select:none}}.hb .el.cur .gn{{color:var(--ink)}}
.hb .el .gc{{white-space:pre;padding-right:16px}}
.hb .sx-c{{color:var(--faint)}}.hb .sx-k{{color:var(--blue)}}.hb .sx-o{{color:var(--faint)}}.hb .sx-v{{color:var(--ink)}}.hb .sx-s{{color:var(--ok)}}
.hb .edsb{{display:flex;flex-wrap:wrap;gap:4px 16px;padding:6px 12px;border-top:1px solid var(--line);background:var(--raised);font-size:12px;color:var(--muted)}}
.hb .edsb span:first-child{{color:var(--ink)}}
.hb *{{scrollbar-width:thin;scrollbar-color:color-mix(in srgb,var(--faint) 45%,transparent) transparent}}
.hb *:hover{{scrollbar-color:var(--faint) transparent}}
.hb .tw .meter{{min-width:max-content;flex-wrap:nowrap}}
.hb .gdev{{display:grid;grid-template-columns:repeat(auto-fill,minmax(min(320px,100%),1fr));gap:0;border-top:0}}
.hb .gdev>div{{border-right:1px solid var(--hairline);border-bottom:1px solid var(--hairline);min-width:0}}
.hb .gdev h3{{font-size:12px;font-weight:600;color:var(--muted);padding:10px 12px 0;margin:0}}
@media (max-width:1280px){{.hb .c6{{display:none}}.hb .tw{{--k6: ;}}}}
.hb .ncard.on{{border-color:var(--hazel);box-shadow:inset 0 0 0 1px var(--hazel)}}
.hb .appcard .bd>.st{{align-self:flex-start}}.hb .ncard>.st{{align-self:flex-start}}.hb .field.wide{{grid-column:span 2}}@media (max-width:767px){{.hb .field.wide{{grid-column:auto}}}}.hb .ncard .pipe{{grid-template-columns:repeat(2,minmax(0,1fr))}}.hb .ncard.pick{{cursor:pointer}}.hb .ncard.pick .nh .d{{min-height:0}}.hb .ncard.pick:hover{{border-color:var(--hazel)}}.hb .ncard .rdo{{flex:none;width:16px;height:16px;border-radius:50%;border:1.5px solid var(--faint);background:var(--panel);box-sizing:border-box}}.hb .ncard.on .rdo{{border:5px solid var(--hazel)}}
.hb .opt{{display:flex;align-items:flex-start;gap:8px;font-size:13px;padding:4px 0}}.hb .opt input{{margin-top:3px}}
.hb .fleet.choices{{grid-template-columns:repeat(auto-fit,minmax(min(250px,100%),1fr))}}
.hb .row.sel{{background:color-mix(in srgb,var(--hazel) 8%,transparent)}}
.hb .hint{{display:inline-grid;place-items:center;width:20px;height:20px;border-radius:50%;color:var(--faint);cursor:help;vertical-align:middle;flex:none}}
.hb .hint:hover,.hb .hint:focus-visible{{color:var(--ink)}}
.hb .at{{display:inline-flex;align-items:center;gap:4px;height:22px;padding:0 6px;border-radius:6px;background:var(--raised);color:var(--muted);font-size:12px;cursor:help;white-space:nowrap;flex:none}}
.hb .at.ok{{color:var(--ok)}}.hb .at.warn{{color:var(--warn)}}.hb .at.dng{{color:var(--danger)}}
.hb .ats{{display:inline-flex;flex-wrap:wrap;gap:4px;align-items:center}}
.hb .edban{{display:flex;flex-wrap:wrap;align-items:center;gap:8px;padding:8px 12px;border-bottom:1px solid var(--line);background:color-mix(in srgb,var(--warn) 10%,var(--panel))}}
.hb .hwg{{display:grid;grid-template-columns:repeat(auto-fill,minmax(min(230px,100%),1fr));gap:12px;padding:12px 14px}}
.hb .hwc{{display:flex;flex-direction:column;gap:8px;padding:12px;border:1px solid var(--line);border-radius:8px;background:var(--panel);min-width:0}}
.hb .hwc.wide{{grid-column:1/-1}}
@media (min-width:1280px){{.hb .hwc.wide{{grid-column:span 3}}}}
.hb .hwt{{display:flex;justify-content:space-between;align-items:baseline;gap:8px;font-weight:600;font-size:13px}}
.hb .hwc .rings{{justify-content:flex-start;gap:14px;flex-wrap:wrap}}
.hb .hwb{{display:grid;grid-template-columns:minmax(0,1fr) minmax(48px,1.2fr) 76px;align-items:center;gap:8px;font-size:12px;min-height:22px}}
.hb .hwb.t{{grid-template-columns:minmax(0,1.4fr) 76px minmax(48px,1fr) 76px}}.hb .hwb>:last-child{{text-align:right;white-space:nowrap}}.hb .hwb.t .at{{justify-content:center}}
.hb .hwb .bar{{height:6px;border-radius:3px;background:var(--q2);overflow:hidden}}.hb .hwb .bar i{{display:block;height:100%;background:var(--hazel)}}
.hb .hwb .bar i.warn{{background:var(--warn)}}.hb .hwb .bar i.dng{{background:var(--danger)}}
.hb .hwb small{{color:var(--faint);font-size:11px}}
.hb details.more>summary{{cursor:pointer;color:var(--hazel);font-size:12px;list-style:none;min-height:24px;display:flex;align-items:center}}.hb details.more>summary::-webkit-details-marker{{display:none}}
.hb details.more[open]>summary{{margin-bottom:8px}}.hb details.more>.hwb+.hwb{{margin-top:8px}}
.hb .pcrumb{{display:flex;flex-wrap:wrap;align-items:center;gap:4px;font-size:12px;color:var(--muted)}}.hb .pcrumb a{{color:var(--muted)}}.hb .pcrumb a:hover{{color:var(--ink)}}.hb .pcrumb .sep{{color:var(--faint)}}
.hb .subtabs{{display:flex;flex-wrap:wrap;gap:4px}}
.hb .st2{{min-height:30px;padding:0 12px;border-radius:999px;border:1px solid transparent;background:transparent;color:var(--muted);font-size:13px;cursor:pointer}}
.hb .st2:hover{{color:var(--ink);background:var(--raised)}}.hb .st2.on{{color:var(--ink);background:var(--raised);border-color:var(--line)}}
@media (max-width:767px){{.hb .top a.ib[href="Terminal.dc.html"]{{display:none}}}}@media (max-width:767px){{.hb .subtabs{{flex-wrap:nowrap;overflow-x:auto}}.hb .st2{{flex:none;min-height:44px}}}}
.hb .rstrip{{display:flex;flex-wrap:wrap;align-items:center;gap:6px;padding:10px 14px;border-top:1px solid var(--hairline)}}
.hb .tier{{display:inline-flex;align-items:center;height:22px;padding:0 8px;border-radius:6px;font-size:12px;font-weight:600;border:1px solid currentColor;cursor:help}}
.hb .tier.dng{{color:var(--danger)}}.hb .tier.info{{color:var(--blue)}}.hb .tier.off{{color:var(--muted)}}
@media (max-width:1024px){{.hb .tw{{--k5: ;}}}}
@media (max-width:767px){{.hb .tw{{--k3: ;--k4: ;--k5: ;--k6: ;}}}}

/* 操作一看就知道能點：按鈕有實底與邊框、圖示按鈕有框、連結有底線、可點的列滑過有箭頭 */
.hb .btn{{background:var(--raised);border-color:color-mix(in srgb,var(--ink) 22%,transparent);box-shadow:0 1px 0 rgb(0 0 0/.25);cursor:pointer}}
.hb .btn:hover{{border-color:var(--hazel);color:var(--ink)}}.hb .btn.pri:hover{{filter:brightness(1.08)}}
.hb .btn.sm{{color:var(--ink)}}.hb .btn.sm.pri{{color:var(--hazelink)}}
.hb .ib{{border:1px solid color-mix(in srgb,var(--ink) 18%,transparent);background:var(--raised);color:var(--ink);cursor:pointer}}.hb .ib:hover{{border-color:var(--hazel);color:var(--hazel)}}
.hb .top .ib{{background:transparent}}
.hb .range{{display:inline-flex;gap:2px;padding:2px;border:1px solid color-mix(in srgb,var(--ink) 18%,transparent);border-radius:6px;background:var(--panel)}}.hb .range .btn{{border-color:transparent;box-shadow:none;background:transparent;color:var(--ink)}}.hb .range .btn.on{{background:var(--raised);border-color:var(--hazel)}}
.hb .ph .more{{border:1px solid color-mix(in srgb,var(--ink) 18%,transparent);border-radius:4px;padding:0 10px;background:var(--raised);cursor:pointer}}.hb .ph .more:hover{{border-color:var(--hazel)}}
.hb .chip{{color:var(--ink);border-color:color-mix(in srgb,var(--ink) 25%,transparent);background:var(--raised);cursor:pointer}}.hb .chip:hover{{border-color:var(--hazel)}}
.hb .note a,.hb .foot a,.hb .check a,.hb p a{{color:var(--hazel);text-decoration:underline;text-underline-offset:2px}}
.hb a.row{{cursor:pointer;position:relative}}.hb a.row:hover{{background:var(--raised);box-shadow:inset 3px 0 var(--hazel)}}
.hb a.ncard{{cursor:pointer}}.hb a.ncard:hover{{border-color:var(--hazel)}}
.hb .hostbar .more{{border:1px solid color-mix(in srgb,var(--ink) 18%,transparent);border-radius:4px;padding:0 10px;min-height:28px;display:inline-flex;align-items:center;background:var(--raised)}}
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
 "objstore": '<path d="M4 7h16l-1.6 12.2a2 2 0 0 1-2 1.8H7.6a2 2 0 0 1-2-1.8L4 7z"/><ellipse cx="12" cy="7" rx="8" ry="2.5"/><path d="M9 12h6"/>',
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
 "svcmod": '<rect x="3" y="4" width="18" height="6" rx="1.5"/><rect x="3" y="14" width="18" height="6" rx="1.5"/><path d="M7 7h.01M7 17h.01M11 7h6M11 17h6"/>',
 "search": '<circle cx="11" cy="11" r="6"/><path d="m20 20-4-4"/>',
 "sun": '<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M2 12h2M20 12h2M5 5l1.5 1.5M17.5 17.5 19 19M5 19l1.5-1.5M17.5 6.5 19 5"/>',
 "user": '<circle cx="12" cy="8" r="4"/><path d="M4 21a8 8 0 0 1 16 0"/>',
 "chevron": '<path d="m9 6 6 6-6 6"/>',
 "collapse": '<path d="m14 7-5 5 5 5"/><path d="M4 4v16"/>',
 "log": '<path d="M6 3h8l4 4v14H6V3z"/><path d="M9 13h6M9 17h6"/>',
 "term": '<path d="m5 7 5 5-5 5"/><path d="M12 19h7"/>',
 "deploy": '<path d="M12 19V5"/><path d="m5 12 7-7 7 7"/><path d="M5 21h14"/>',
 "restart": '<path d="M20 12a8 8 0 1 1-2.3-5.7"/><path d="M20 4v5h-5"/>',
 "power": '<path d="M12 3v8"/><path d="M6.3 6.8a8 8 0 1 0 11.4 0"/>',
 "box": '<path d="M3.5 7.5 12 3l8.5 4.5v9L12 21l-8.5-4.5z"/><path d="M3.5 7.5 12 12l8.5-4.5M12 12v9"/>',
 "check": '<path d="m5 12.5 4.5 4.5L19 7.5"/>',
 "info": '<circle cx="12" cy="12" r="9"/><path d="M12 11v5M12 8h.01"/>',
 "lock": '<rect x="5" y="11" width="14" height="9" rx="2"/><path d="M8 11V8a4 4 0 0 1 8 0v3"/>',
 "globe": '<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18"/>',
 "archive": '<rect x="3" y="4" width="18" height="5" rx="1"/><path d="M5 9v10h14V9M10 13h4"/>',
 "branch": '<circle cx="6" cy="5" r="2"/><circle cx="6" cy="19" r="2"/><circle cx="18" cy="8" r="2"/><path d="M6 7v10M18 10a6 6 0 0 1-6 6H8"/>',
 "x": '<path d="M6 6l12 12M18 6 6 18"/>',
 "save": '<path d="M5 4h11l3 3v13H5z"/><path d="M8 4v5h7V4M8 20v-6h8v6"/>',
 "undo": '<path d="M9 14 4 9l5-5"/><path d="M4 9h10a6 6 0 0 1 0 12h-3"/>',
 "wrapline": '<path d="M4 6h16M4 12h13a3 3 0 0 1 0 6h-4"/><path d="m15 16-2 2 2 2M4 18h5"/>',
 "diff": '<path d="M6 3v12M3 6h6M15 18h6"/><rect x="13" y="3" width="8" height="8" rx="1"/><rect x="3" y="13" width="8" height="8" rx="1"/>',
 "history": '<path d="M3 12a9 9 0 1 0 3-6.7"/><path d="M3 4v5h5M12 7v5l3 2"/>',
 "plus": '<path d="M12 5v14M5 12h14"/>',
 "dock": '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M3 14h18"/>',
 "max": '<path d="M4 9V4h5M20 9V4h-5M4 15v5h5M20 15v5h-5"/>',
 "more": '<circle cx="6" cy="12" r="1.2"/><circle cx="12" cy="12" r="1.2"/><circle cx="18" cy="12" r="1.2"/>',
 "stop": '<rect x="6" y="6" width="12" height="12" rx="1.5"/>',
 "chev": '<path d="m9 6 6 6-6 6"/>',
 "chevl": '<path d="m15 6-6 6 6 6"/>',
 "folder": '<path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7z"/>',
 "file": '<path d="M6 3h8l4 4v14H6V3z"/><path d="M14 3v4h4"/>',
 "sort": '<path d="m8 10 4-4 4 4"/><path d="m8 14 4 4 4-4"/>',
 "up": '<path d="m6 15 6-6 6 6"/>',
 "down": '<path d="m6 9 6 6 6-6"/>',
 "upload": '<path d="M12 16V5"/><path d="m7 10 5-5 5 5"/><path d="M4 19h16"/>',
 "download": '<path d="M12 4v11"/><path d="m7 10 5 5 5-5"/><path d="M4 19h16"/>',
 "newfolder": '<path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7z"/><path d="M12 10v6M9 13h6"/>',
 "rename": '<path d="M4 20h4l10-10-4-4L4 16v4z"/>',
 "move": '<path d="M5 12h14"/><path d="m13 6 6 6-6 6"/>',
 "copy": '<rect x="9" y="9" width="11" height="11" rx="2"/><path d="M5 15V5a2 2 0 0 1 2-2h10"/>',
 "zip": '<path d="M6 3h8l4 4v14H6V3z"/><path d="M10 7h2M10 10h2M10 13h2M10 16h2"/>',
 "trash": '<path d="M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13"/>',
 "share": '<circle cx="18" cy="5" r="2.5"/><circle cx="6" cy="12" r="2.5"/><circle cx="18" cy="19" r="2.5"/><path d="m8 11 8-5M8 13l8 5"/>',
 "grid": '<rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/>',
 "list": '<path d="M8 6h13M8 12h13M8 18h13"/><circle cx="4" cy="6" r="1"/><circle cx="4" cy="12" r="1"/><circle cx="4" cy="18" r="1"/>',
 "eye": '<path d="M2 12s4-7 10-7 10 7 10 7-4 7-10 7S2 12 2 12z"/><circle cx="12" cy="12" r="3"/>',
}


def icon(name: str, size: int = 16) -> str:
    return (f'<svg viewBox="0 0 24 24" width="{size}" height="{size}" fill="none" stroke="currentColor" stroke-width="1.8" '
            f'stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">{ICONS[name]}</svg>')


def actions(keys: list) -> str:
    """列尾快速動作：icon 按鈕，aria-label 是文字。keys 例 ["log","term","restart","more"]"""
    lab = {"log": "日誌", "term": "終端機", "restart": "重啟", "more": "更多", "stop": "停止", "download": "下載", "rename": "重新命名", "share": "分享", "trash": "刪除", "copy": "複製", "move": "搬移"}
    return '<span class="acts">' + "".join(f'<button type="button" class="ib" aria-label="{lab[k]}" title="{lab[k]}">{icon(k)}</button>' for k in keys) + "</span>"


NAV = [
    ("總覽", "overview", "Overview.dc.html"),
    ("主機", "nodes", "Nodes.dc.html"),
    ("專案與服務", "projects", "Projects.dc.html"),
    ("容器", "docker", "Containers.dc.html"),
    ("Kubernetes", "k8s", "Kubernetes.dc.html"),
    ("服務模組", "svcmod", "HostServices.dc.html"),
    ("應用商店", "store", "AppStore.dc.html"),
    ("網站與入口", "web", "Websites.dc.html"),
    ("API 代理", "api", "APIProxy.dc.html"),
    ("資料庫", "db", "Database.dc.html"),
    ("物件儲存", "objstore", "ObjectStorage.dc.html"),
    ("AI", "ai", "AI.dc.html"),
    ("檔案", "files", "Files.dc.html"),
    ("儲存與備份", "storage", "Storage.dc.html"),
    ("計畫任務", "cron", "Cron.dc.html"),
    ("安全防護", "security", "Security.dc.html"),
    ("流量分析", "analytics", "Analytics.dc.html"),
    ("事件與告警", "events", "Events.dc.html"),
    ("日誌", "logs", "Logs.dc.html"),
    ("身分與存取", "iam", "IAM.dc.html"),
    ("平台設定", "settings", "Settings.dc.html"),
]
# 側欄只放「跨節點的資源」。單一主機才有意義的（系統、服務模組、檔案、程序）收進節點頁分頁；
# 全站都會用到的（工作、告警、終端機、搜尋）放頂列與最上方。
GROUPS = [("應用", ["projects", "store", "web", "db", "objstore", "ai", "api"]),
          ("基礎設施", ["nodes", "docker", "k8s", "storage", "cron"]),
          ("安全與紀錄", ["security", "analytics", "logs"]),
          ("管理", ["iam", "settings"])]
NAV_ALIAS = {"svcmod": "nodes", "files": "nodes"}  # 這些頁屬於「主機」底下


def nav(current: str, jobs: int = 3, attention: int = 5, mobile_links: bool = True, collapsed: bool = False) -> str:
    current = NAV_ALIAS.get(current, current)
    def link(key: str) -> str:
        label, k, file = next(n for n in NAV if n[1] == key)
        href = file if file else f"#{k}"
        cur = ' cur" aria-current="page' if k == current else ""
        return f'<a class="nv{cur}" href="{href}" title="{label}">{icon(k)}<span>{label}</span></a>'
    ov = ' cur" aria-current="page' if current == "overview" else ""
    jb = ' cur" aria-current="page' if current == "jobs" else ""
    ev = ' cur" aria-current="page' if current == "events" else ""
    top = (f'<a class="nv{ov}" href="Overview.dc.html">{icon("overview")}<span>總覽</span></a>'
           f'<a class="nv{jb}" href="WorkCenter.dc.html" data-tip="{jobs} 個工作進行中">{icon("jobs")}<span>工作中心</span><span class="cnt" aria-label="{jobs} 個工作進行中"><i class="spin" aria-hidden="true"></i>{jobs}</span></a>'
           f'<a class="nv{ev}" href="Events.dc.html">{icon("events")}<span>事件與告警</span><span class="cnt dng" aria-label="{attention} 件需要處理">{attention}</span></a>')
    groups = "".join(f'<div class="navgrp">{g}</div><div class="navcol">{"".join(link(k) for k in keys)}</div>' for g, keys in GROUPS)
    return f"""
  <nav class="side{" mini" if collapsed else ""}" aria-label="主導覽">
    <div class="navcol">{top}</div>
    {groups}
    <button type="button" class="nv coll" aria-label="{"展開側欄" if collapsed else "收合側欄"}">{icon("collapse")}<span>收合</span></button>
  </nav>"""


def topbar(current: str, jobs: int = 3, attention: int = 5, obs: str = "03:42:06", collapsed: bool = False) -> str:
    label = next((n[0] for n in NAV if n[1] == current), {"jobs": "工作中心", "terminal": "終端機", "logs": "日誌"}.get(current, "總覽"))
    return f"""
  <header class="top">
    <a class="brand{" mini" if collapsed else ""}" href="Overview.dc.html" aria-label="HazelminX 總覽"><span class="logo" aria-hidden="true"></span><span>HazelminX</span></a>
    <a class="inp search" href="CommandPalette.dc.html" aria-label="搜尋或下指令">{icon("search")}<span>搜尋或下指令</span><span class="kbd">⌘K</span></a>
    <div class="tright">
      <span class="obs">資料更新於 <span class="mono">{obs}</span> · 每 10 秒</span>
      <a class="ib" href="Overview.dc.html#attention" aria-label="需要處理 {attention}" data-tip="需要處理 {attention} · 告警 3、待批准 1、對外 Port 1">{icon("events")}<b class="badge dng">{attention}</b></a>
      <a class="ib" href="Terminal.dc.html" aria-label="終端機" data-tip="終端機">{icon("terminal")}</a>
      <button type="button" class="ib" aria-label="切換淺色或深色" data-tip="切換淺色或深色">{icon("sun")}</button>
      <button type="button" class="ib user" aria-label="tim · 擁有者">{icon("user")}<span>tim</span></button>
    </div>
  </header>"""


def host_bar() -> str:
    """主機狀態列：有主機沒有回應時全站常駐一行（不能關閉，恢復後自動消失；維護中的主機不算）。"""
    return (f'<div class="hostbar" role="status"><span class="st dng"><i aria-hidden="true"></i>1 台主機沒有回應</span>'
            f'<span class="mono">hazel-jp-02</span><span class="note">48 秒 · wiki.example.tw 仍可連</span>'
            f'<a class="btn sm" href="NodeOffline.dc.html">處理</a><a class="more" href="Nodes.dc.html">全部主機 →</a></div>')


def savebar(changes: str, effect: str, primary: str, secondary: str = "") -> str:
    """未儲存儲存列：有編輯中的變更時固定在畫面底部（全系統共用）。沒有變更時不出現，編輯器裡的儲存鈕停用。"""
    sec = f'<button type="button" class="btn">{secondary}</button>' if secondary else ""
    return (f'<div class="savebar" role="region" aria-label="未儲存的變更"><span class="dot" aria-hidden="true"></span>'
            f'<span><strong>{changes}</strong><span class="note"> · {effect}</span></span>'
            f'<span class="wrap sbtns"><button type="button" class="btn">捨棄</button>{sec}'
            f'<button type="button" class="btn pri" aria-keyshortcuts="Meta+S">{icon("save", 14)}{primary}<span class="kbd">⌘S</span></button></span></div>')


def component_down(name: str, host: str, since: str, exit_info: str, impact: str, can_restart: bool = True, tries: str = "", kind: str = "", busy: bool = False, fix: str = "") -> str:
    """平台依賴的元件停止時，依賴它的頁面頂端都放這條（ROB-013）。頁面其餘內容照常，以上次觀測值呈現。"""
    btn = (f'<button type="button" class="btn pri" disabled>{icon("restart", 14)}重啟中…</button>' if busy
           else f'<button type="button" class="btn pri">{icon("restart", 14)}重啟 {name}</button>' if can_restart
           else f'<span tabindex="0" data-tip="需要 {name} 的操作權限，請找主機管理員"><button type="button" class="btn pri" disabled>{icon("restart", 14)}重啟 {name}</button></span>')
    if fix and not busy:  # 停止原因是設定錯誤時，直接重啟一定再失敗：主要動作換成修正方法，重啟降為次要
        btn = fix + btn.replace('class="btn pri"', 'class="btn"')
    t = f'<span class="note">{tries}</span>' if tries else ""
    return (f'<div class="cdown" role="alert"><span class="t"><strong>{name} 已停止 · {host} · {since}</strong>' + (f'<span class="note mono">{kind}</span>' if kind else '') + f''
            f'<span>{status("dng", exit_info)} <span class="note">{impact}</span></span>{t}</span>'
            f'<span class="acts2">{btn}<button type="button" class="btn">{icon("log", 14)}看日誌</button></span></div>')


def shell(current: str, main: str, aside: str | None = None, **kw) -> str:
    aside_html = ""
    if aside:
        # 手機寬度時右欄排到內容前面，所以另外放一個只在手機顯示的頁面標題，讓標題永遠在最上面
        t = re.search(r"<h1[^>]*>(.*?)</h1>", main, re.S)
        mt = f'<h1 class="mtitle">{t.group(1)}</h1>' if t else ""
        aside_html = f'{mt}<aside class="aside" aria-label="需要處理與進行中的工作">{aside}</aside>'
    return (f'<div class="hb theme-{{{{theme}}}}" style="min-height: 100vh; display: flex; flex-direction: column">'
            f'{topbar(current, **kw)}{host_bar()}<div class="layout">{nav(current, **kw)}<main class="main">{main}</main>{aside_html}</div></div>')


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
            f'<span class="tdl">{d}{note_html}</span>{sp}</div>')


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
    """折線／面積／長條時間圖，寬度隨容器流動：圖形在 0–1000 × 0–100 的座標裡拉伸、線寬不變（non-scaling-stroke），
    座標軸文字、部署標記、過期點、滑鼠提示都是疊在上面的 HTML，所以任何寬度字都一樣大。w 只保留給舊呼叫，不再影響版面。"""
    fmt_val = fmt_val or (lambda v: f"{v:.0f}{unit}")
    n = len(series[0].vals)
    if stacked:
        vmax = vmax or max(sum(s.vals[i] for s in series) for i in range(n)) * 1.1
    else:
        vmax = vmax or max(max(s.vals) for s in series) * 1.15
    vmax = max(vmax, 1e-9)
    X, Y = 1000.0, 100.0
    if bar:
        step = X / n
        xpos = lambda i: step * (i + 0.5)
    else:
        step = X / (n - 1) if n > 1 else X
        xpos = lambda i: step * i
    yp = lambda v: Y - Y * v / vmax
    pct = lambda x: f"{x / X * 100:.2f}%"
    svg = [f'<svg class="plot" viewBox="0 0 {X:.0f} {Y:.0f}" preserveAspectRatio="none" aria-hidden="true">']
    ticks = _ticks(vmax)
    for t in ticks:
        svg.append(f'<line class="grid" x1="0" y1="{yp(t):.2f}" x2="{X:.0f}" y2="{yp(t):.2f}" vector-effect="non-scaling-stroke"/>')
    over = []
    if bar:
        bw = step * 0.78 / len(series)
        for si, s_ in enumerate(series):
            for i, v in enumerate(s_.vals):
                if v <= 0:
                    continue
                x = xpos(i) - bw * len(series) / 2 + si * bw
                svg.append(f'<rect x="{x:.2f}" y="{yp(v):.2f}" width="{bw * 0.9:.2f}" height="{Y - yp(v):.2f}" fill="{s_.color}"/>')
    elif stacked:
        base = [0.0] * n
        for s_ in series:
            top = [base[i] + s_.vals[i] for i in range(n)]
            up = " L".join(f"{xpos(i):.2f},{yp(top[i]):.2f}" for i in range(n))
            dn = " L".join(f"{xpos(i):.2f},{yp(base[i]):.2f}" for i in reversed(range(n)))
            svg.append(f'<path d="M{up} L{dn} Z" fill="{s_.color}" fill-opacity=".25"/>')
            svg.append(f'<path d="M{up}" fill="none" stroke="{s_.color}" stroke-width="2" stroke-linejoin="round" vector-effect="non-scaling-stroke"/>')
            base = top
    else:
        for s_ in series:
            last = n - 1 if s_.stale_from is None else s_.stale_from
            pts = " L".join(f"{xpos(i):.2f},{yp(s_.vals[i]):.2f}" for i in range(last + 1))
            if s_.kind == "area":
                svg.append(f'<path d="M{pts} L{xpos(last):.2f},{Y:.0f} L0,{Y:.0f} Z" fill="{s_.color}" fill-opacity=".10"/>')
            svg.append(f'<path d="M{pts}" fill="none" stroke="{s_.color}" stroke-width="2" stroke-linejoin="round" stroke-linecap="round" vector-effect="non-scaling-stroke"/>')
            if s_.stale_from is not None:
                over.append(f'<i class="sdot" style="left:{pct(xpos(last))};top:{yp(s_.vals[last]):.2f}%;border-color:{s_.color}" aria-hidden="true"></i>')
    for i, label in (markers or []):
        svg.append(f'<line x1="{xpos(i):.2f}" y1="0" x2="{xpos(i):.2f}" y2="{Y:.0f}" stroke="var(--muted)" stroke-width="1" stroke-dasharray="3 3" vector-effect="non-scaling-stroke"/>')
        side = "right" if xpos(i) > X * 0.6 else "left"
        off = f"right:calc(100% - {pct(xpos(i))} + 4px)" if side == "right" else f"left:calc({pct(xpos(i))} + 4px)"
        over.append(f'<span class="mk" style="{off}">{label}</span>')
    svg.append("</svg>")
    ylab = "".join(f'<span style="top:{yp(t):.2f}%">{fmt_val(t)}</span>' for t in ticks)
    lab_every = max(1, round(n / 6))
    idx = list(range(0, n, lab_every))
    if n - 1 - idx[-1] >= lab_every * 0.6:
        idx.append(n - 1)
    # 窄螢幕只留一半的刻度（最後一個一定留），避免相鄰刻度疊在一起
    last = len(idx) - 1
    thin = {k for k in range(len(idx)) if (last - k) % 2 == 1}
    xlab = "".join(
        f'<span class="{"a0" if (i == 0 and not bar) else ("a1" if (i == n - 1 and not bar) else "")}{" xo" if k in thin else ""}" style="left:{pct(xpos(i))}">{x_labels[i]}</span>' for k, i in enumerate(idx))
    hits = []
    for i in range(n):
        lo, hi = max(0.0, xpos(i) - step / 2), min(X, xpos(i) + step / 2)
        rows = "".join(f'<span class="k"><i style="background:{s_.color}"></i>{s_.name}</span><b>{fmt_val(s_.vals[i])}{"*" if s_.stale_from is not None and i > s_.stale_from else ""}</b>' for s_ in series)
        flip = " flip" if xpos(i) > X * 0.5 else ""
        hits.append(f'<span class="hit{flip}" tabindex="0" style="left:{pct(lo)};width:{pct(hi - lo)}"><i class="xh" style="left:{(xpos(i) - lo) / max(hi - lo, 1e-9) * 100:.1f}%"></i>'
                    f'<span class="tip"><span class="k">{(tip_labels or x_labels)[i]}</span><span></span>{rows}</span></span>')
    return (f'<div class="ts" role="img" aria-label="{"、".join(s_.name for s_ in series)}" style="height:{h}px">'
            f'<div class="yl">{ylab}</div><div class="pa">{"".join(svg)}{"".join(over)}<div class="hits">{"".join(hits)}</div></div><div class="xl">{xlab}</div></div>')


def chart_card(title: str, body_desktop: str, body_mobile: str, series: list[Series] | None = None,
               unit: str = "", foot: str = "", legend_rect: bool = False) -> str:
    legend = ""
    if series and len(series) > 1:
        legend = '<div class="legend">' + "".join(
            f'<span><i class="{"rect" if legend_rect else ""}" style="background: {s.color}"></i>{s.name}</span>' for s in series) + "</div>"
    unit_html = f'<span class="unit">{unit}</span>' if unit else ""
    foot_html = f'<div class="foot">{foot}</div>' if foot else ""
    body = (f'<div class="body"><div class="ch-d">{body_desktop}</div><div class="ch-m">{body_mobile}</div></div>' if body_mobile else f'<div class="body">{body_desktop}</div>')
    return f'<section class="chart"><div class="ph"><h2>{title}</h2>{unit_html}{legend}</div>{body}{foot_html}</section>'


def ts_card(title, series, x_labels, unit="%", foot="", wide=False, **kw) -> str:
    d = timeseries(series, x_labels, unit=unit, h=220 if wide else 190, **kw)
    return chart_card(title, d, "", series, unit=kw.get("unit_label", ""), foot=foot, legend_rect=kw.get("bar", False))


def ranked(items: list[tuple[str, float, str]], vmax: float | None = None) -> str:
    vmax = vmax or max(v for _, v, _ in items) or 1
    rows = "".join(
        f'<div class="rank"><span class="mono">{n}</span>'
        f'<span class="bar" aria-hidden="true"><i style="width: {v / vmax * 100:.0f}%"></i></span><span class="mono" style="font-size:12px;white-space:nowrap!important">{t}</span></div>'
        for n, v, t in items)
    return f'<div class="ranks">{rows}</div>'


TL_KINDS = {
    "deploy": ("部署歷史", {"o": "成功", "f": "失敗", "r": "回滾", "p": "進行中", "e": "無資料"}),
    "alert": ("告警狀態", {"o": "正常", "f": "觸發", "r": "靜音", "p": "評估中", "e": "無資料"}),
    "job": ("執行紀錄", {"o": "成功", "f": "失敗", "r": "略過", "p": "執行中", "e": "沒排程"}),
}


def state_timeline(states: str, labels: list[str] | None = None, kind: str = "deploy") -> str:
    """states: 字串，每字一段：o f r p e，意思依 kind（部署、告警、排程執行）。每格都有提示，不留空白 title。"""
    cls = {"o": "", "f": " f", "r": " r", "p": " p", "e": " e"}
    what, names = TL_KINDS[kind]
    tips = labels or [f"第 {i + 1} 段" for i in range(len(states))]
    segs = "".join(f'<i class="{cls[c].strip()}" title="{tips[i]} · {names[c]}"></i>' for i, c in enumerate(states))
    counts = "、".join(f"{states.count(k)} {names[k]}" for k in "ofr" if states.count(k))
    return f'<div class="timeline" role="img" aria-label="{what}：{counts}">{segs}</div>'


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
        cells.append(f'<span class="lab" title="{r}">{r}</span>' + "".join(
            f'<i style="background: {q[min(4, int(v / vmax * 4.999))]}" title="{r} {cols[j]} {v:.0f}%"></i>' for j, v in enumerate(row)))
    return (f'<div class="heatw"><div class="heat" style="grid-template-columns: minmax(120px, 1.4fr) repeat({ncol}, minmax(0, 1fr))" role="img" '
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
    color = "var(--danger)" if p >= danger else ("var(--warn)" if p >= warn else "var(--use)")
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
    return f'<div class="hmap" role="img" aria-label="主機地圖，{len(groups)} 台主機">' + "".join(out) + lg + "</div>"


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
    lg = "".join(f'<span><i class="sw" style="background:{col}" aria-hidden="true"></i>{lab}{"" if re.search(r"[0-9]", lab) else f" {v:g}{unit}"}</span>' for v, lab, col in parts)
    if fw >= 0.5:
        lg += f'<span><i class="sw" style="background:var(--q1)" aria-hidden="true"></i>{"其他" if unit == "%" else "可用"} {free:g}{unit}</span>'
    return '<span class="tall" aria-hidden="true">' + "".join(segs) + f'</span><span class="lg tall-lg" role="list">{lg}</span>'


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
    return f'<div class="pipe" style="--n:{len(stages)}">{chips}</div>'


def topology(columns: list, links: list, w: int = 0, box_w: int = 176, box_h: int = 44, gap: int = 12, col_gap: int = 88, max_rows: int = 6) -> str:
    """拓撲圖，固定像素尺寸（不隨容器縮放，窄就捲動）。columns [(title, [(id, label, sub, state)])]；links [(src, dst, weight, state)]。
    一欄超過 max_rows 個節點時只畫前 max_rows-1 個，其餘折成「＋N」，連到它們的線改接到折疊框。"""
    cols = []
    redirect = {}
    for title, nodes in columns:
        if len(nodes) > max_rows:
            shown = nodes[: max_rows - 1]
            rest = nodes[max_rows - 1:]
            more_id = f"more-{len(cols)}"
            for nid, *_ in rest:
                redirect[nid] = more_id
            shown = shown + [(more_id, f"＋{len(rest)} 個", "點開看全部", "dim")]
            cols.append((title, shown, len(nodes)))
        else:
            cols.append((title, nodes, len(nodes)))
    ncol = len(cols)
    rows = max(len(c[1]) for c in cols)
    W = 24 * 2 + ncol * box_w + (ncol - 1) * col_gap
    H = 30 + rows * (box_h + gap)
    xs = [24 + i * (box_w + col_gap) for i in range(ncol)]
    pos = {}
    out = [f'<svg class="topo" width="{W}" height="{H}" viewBox="0 0 {W} {H}" role="img" aria-label="拓撲：{"→".join(c[0] for c in cols)}">']
    for ci, (title, nodes, total) in enumerate(cols):
        x = xs[ci]
        out.append(f'<text class="col" x="{x:.0f}" y="14">{title} · {total}</text>')
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
            label = label if len(label) <= 18 else label[:17] + "…"
            sub = sub if len(sub) <= 22 else sub[:21] + "…"
            out.append(f'<a href="#{nid}"><rect class="box {st if st in ("danger","warn") else ""}" x="{x}" y="{y:.1f}" width="{box_w}" height="{box_h}" rx="6"/>{shape}'
                       f'<text x="{x+22}" y="{y+18:.1f}">{label}</text><text class="sub" x="{x+22}" y="{y+33:.1f}">{sub}</text></a>')
    lk = []
    agg = {}
    for src, dst, wgt, st in links:
        s2, d2 = redirect.get(src, src), redirect.get(dst, dst)
        if (s2, d2) in agg:
            agg[(s2, d2)][0] += wgt
        else:
            agg[(s2, d2)] = [wgt, st]
    for (src, dst), (wgt, st) in agg.items():
        (x1, y1), (x2, y2) = pos[src], pos[dst]
        ax, ay = x1 + box_w, y1 + box_h / 2
        bx, by = x2, y2 + box_h / 2
        mx = (ax + bx) / 2
        lk.append(f'<path class="lnk {st}" stroke-width="{1.5 + min(1.0, wgt) * 6:.1f}" d="M{ax:.0f},{ay:.1f} C{mx:.0f},{ay:.1f} {mx:.0f},{by:.1f} {bx:.0f},{by:.1f}"/>')
    out.insert(1, "".join(lk))
    out.append("</svg>")
    return '<div class="topow">' + "".join(out) + "</div>"


def flowlist(links: list) -> str:
    """拓撲的手機版：每條連線一列。links [(src_label, dst_label, note)]"""
    return '<div class="flowlist">' + "".join(
        f'<div class="fr"><span class="mono" style="overflow-wrap:anywhere">{a}</span><span class="note">→</span>'
        f'<span class="mono" style="overflow-wrap:anywhere">{b}</span><span class="note">{n}</span></div>' for a, b, n in links) + "</div>"



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
    lg = '<div class="mxlg"><span class="c empty" aria-hidden="true"></span>虛線格：沒有設定或不適用</div>' if any(k == "none" for row in cells for k, _ in row) else ""
    return f'<div class="mxw"><div class="mx" style="grid-template-columns:{first_col} repeat({len(cols)},minmax(max-content,1fr))">' + "".join(out) + f"</div>{lg}</div>"


def tree(items: list, open_depth: int = 1) -> str:
    """檔案樹：items [(depth, name, size_text, on)]。有子項的資料夾用 <details>，可收合；選中路徑與淺層預設展開。"""
    n = len(items)
    on_idx = next((i for i, it in enumerate(items) if it[3]), -1)
    ancestors = set()
    if on_idx >= 0:
        d = items[on_idx][0]
        for i in range(on_idx - 1, -1, -1):
            if items[i][0] < d:
                ancestors.add(i); d = items[i][0]
    out = ['<div class="tree"><ul>']
    stack = []  # open details/ul pairs
    for i, it in enumerate(items):
        depth, name, sz, on = it[:4]
        dis = it[4] if len(it) > 4 else ""
        has_kids = i + 1 < n and items[i + 1][0] > depth
        while stack and stack[-1] >= depth:
            out.append("</ul></details></li>"); stack.pop()
        if has_kids:
            is_open = depth <= open_depth or on or i in ancestors
            out.append(f'<li><details{" open" if is_open else ""}><summary class="{"on" if on else ""}">{icon("chev", 14)}{icon("folder", 16)}<span class="tn" title="{name}">{name}</span><span class="sz">{sz}</span></summary><ul>')
            stack.append(depth)
        else:
            # 目錄樹只有資料夾；子層還沒讀取的也是可展開的資料夾（展開時才向 Agent 讀取）
            if dis:
                # 不能選的項目照樣列出，呈現不可選的樣式，滑過顯示原因
                out.append(f'<li class="leaf dis" aria-disabled="true" data-tip="{dis}" title="{dis}"><span class="pad"></span>{icon("lock", 14)}{icon("folder", 16)}<span class="tn">{name}</span><span class="sz">{sz}</span></li>')
                continue
            out.append(f'<li><details><summary class="{"on" if on else ""}">{icon("chev", 14)}{icon("folder", 16)}<span class="tn" title="{name}">{name}</span><span class="sz">{sz}</span></summary>'
                       f'<ul><li class="leaf"><span class="pad"></span><span class="note">讀取中…</span></li></ul></details></li>')
    while stack:
        out.append("</ul></details></li>"); stack.pop()
    out.append("</ul></div>")
    return "".join(out)


def scope_bar(items: list, current: int = 0, label: str = "範圍", extra: str = "") -> str:
    """範圍列：這一頁在看哪些節點／叢集。items [(label, sub)]；永遠放在頁面標題正下方。"""
    btns = "".join(f'<button type="button" class="btn{" on" if i == current else ""}" aria-pressed="{"true" if i == current else "false"}">{l}{f"<span class=mono>{sub}</span>" if sub else ""}</button>' for i, (l, sub) in enumerate(items))
    return f'<div class="scope" role="group" aria-label="{label}"><span class="lbl">{label}</span>{btns}{extra}</div>'


IMPACTS = {
    # kind: (icon, 顏色類別, 標籤)
    "svc": ("restart", "warn", "重啟服務"),
    "ctr": ("box", "dng", "重啟容器"),
    "reboot": ("power", "dng", "需要重開機"),
    "none": ("check", "ok", "無影響"),
}


def impacts(items: list) -> str:
    """影響類型圖示：items [(kind, detail)]。每種影響固定一個圖示；滑鼠停留或鍵盤聚焦顯示「標籤：細節」。"""
    out = []
    for kind, detail in items:
        ic, col, label = IMPACTS[kind]
        tip = f"{label}：{detail}" if detail else label
        out.append(f'<span class="imp {col}" tabindex="0" role="img" aria-label="{tip}" data-tip="{tip}">{icon(ic, 16)}</span>')
    return '<span class="imps">' + "".join(out) + "</span>"


def impact_legend(kinds: list) -> str:
    out = []
    for k in kinds:
        ic, col, label = IMPACTS[k]
        out.append(f'<span role="listitem"><span class="imp {col}" aria-hidden="true">{icon(ic, 14)}</span>{label}</span>')
    return '<div class="lg" role="list">' + "".join(out) + "</div>"


def code_editor(tabs: list, lines: list, status: list, toolbar_right: str = "", banner: str = "") -> str:
    """程式碼編輯器（對齊 1Panel 檔案管理的 Monaco 編輯器）。
    tabs: [(name, active, modified)]；lines: 已上色的 HTML 行；status: 狀態列項目。第 cur 行（以 ▸ 開頭標記）為目前行。"""
    t = "".join(
        f'<div class="ttab{" on" if on else ""}" role="tab" aria-selected="{"true" if on else "false"}">{icon("file", 14)}<span class="tn">{n}</span>'
        + (f'<span class="mod" aria-label="未儲存"></span>' if mod else "")
        + f'<button type="button" class="ib" aria-label="關閉 {n}">{icon("x", 14)}</button></div>' for n, on, mod in tabs)
    def tb(ic, label, kbd=""):
        return f'<button type="button" class="ib" aria-label="{label}">{icon(ic)}' + (f'<span class="kbd">{kbd}</span>' if kbd else "") + "</button>"
    rows = []
    for i, ln in enumerate(lines, 1):
        cur = ln.startswith("▸")
        ln = ln[1:] if cur else ln
        rows.append(f'<div class="el{" cur" if cur else ""}"><span class="gn">{i}</span><span class="gc">{ln or " "}</span></div>')
    return ('<div class="ed">'
            f'<div class="ttabs"><div role="tablist" aria-label="開啟的檔案" style="display:flex">{t}</div></div>'
            '<div class="edtb">'
            f'<button type="button" class="btn pri" aria-keyshortcuts="Meta+S">{icon("save", 14)}儲存 · 1 處變更</button>'
            + tb("undo", "復原") + tb("search", "搜尋與取代") + tb("wrapline", "自動換行") + '<span class="sep"></span>'
            + tb("history", "版本歷史") + tb("diff", "與上一版比較")
            + f'<span style="margin-left:auto" class="wrap">{toolbar_right}</span>' + tb("max", "全螢幕")
            + '</div>' + (f'<div class="edban" role="status">{banner}</div>' if banner else "")
            + f'<div class="edb" role="textbox" aria-multiline="true" aria-label="檔案內容">{"".join(rows)}</div>'
            '<div class="edsb">' + "".join(f"<span>{s}</span>" for s in status) + '</div></div>')


def hint(text: str) -> str:
    """說明句不直接顯示：ⓘ 圖示，滑鼠停留或鍵盤聚焦才看得到。"""
    t = text.replace('"', "&quot;")
    return f'<span class="hint" tabindex="0" role="img" aria-label="{t}" data-tip="{t}">{icon("info", 14)}</span>'


def attr(ic: str, tip: str, text: str = "", kind: str = "") -> str:
    """屬性圖示：固定圖示＋（可選）短值，完整意思放在提示。"""
    t = tip.replace('"', "&quot;")
    return f'<span class="at {kind}" tabindex="0" role="img" aria-label="{t}" data-tip="{t}">{icon(ic, 14)}{f"<span>{text}</span>" if text else ""}</span>'


def hw_group(title: str, items: list, unit_note: str = "", max_rings: int = 4) -> str:
    """硬體分組卡：items [(name, pct, value_text, sub)]。4 個以內畫環，超過改精簡橫條（依比例排序，前 4 個＋其餘收合）。"""
    if len(items) <= max_rings:
        body = '<div class="rings">' + "".join(ring(p, v, n + (f" {s}" if s else ""), size=64, warn=80, danger=90) for n, p, v, s in items) + "</div>"
    else:
        srt = sorted(items, key=lambda x: -x[1])
        def bar(n, p, v, s):
            cls = " dng" if p >= 90 else (" warn" if p >= 80 else "")
            return (f'<div class="hwb"><span class="mono">{n}</span><span class="bar" aria-hidden="true"><i class="{cls.strip()}" style="width:{p:.0f}%"></i></span>'
                    f'<span class="mono">{v}</span></div>')
        body = "".join(bar(*it) for it in srt[:4]) + (f'<details class="more"><summary>全部 {len(items)} 個</summary>' + "".join(bar(*it) for it in srt[4:]) + "</details>")
    return f'<div class="hwc"><div class="hwt"><span>{title}</span><span class="note">{unit_note}</span></div>{body}</div>'


def temps(sensors: list, show: int = 6) -> str:
    """溫度：sensors [(name, source, value_c, crit_c)]，依「值／臨界」排序；比例條以各自的臨界溫度為滿格。"""
    srt = sorted(sensors, key=lambda x: -(x[2] / x[3]))
    def row(n, src, v, crit):
        p = v / crit * 100
        cls = "dng" if p >= 90 else ("warn" if p >= 80 else "")
        return (f'<div class="hwb t"><span>{n}</span><span class="at" data-tip="來源 {src}">{src}</span><span class="bar" aria-hidden="true"><i class="{cls}" style="width:{p:.0f}%"></i></span>'
                f'<span class="mono">{v:.0f}°<small> / {crit:.0f}°</small></span></div>')
    head = "".join(row(*s) for s in srt[:show])
    rest = srt[show:]
    more = (f'<details class="more"><summary>全部 {len(sensors)} 個感測器</summary>' + "".join(row(*s) for s in rest) + "</details>") if rest else ""
    return f'<div class="hwc wide"><div class="hwt"><span>溫度</span><span class="note">{len(sensors)} 個感測器</span></div>{head}{more}</div>'


def crumbs(items: list) -> str:
    """頁面麵包屑：items [(label, href)]，最後一層是頁面本身，不寫在這裡（由 h1 呈現）。"""
    return '<nav class="pcrumb" aria-label="位置">' + "".join(f'<a href="{href}">{l}</a><span class="sep" aria-hidden="true">/</span>' for l, href in items) + "</nav>"

def tier(t: str) -> str:
    """用途等級徽章：正式／測試／實驗。"""
    k = {"正式": "dng", "測試": "info", "實驗": "off"}[t]
    return f'<span class="tier {k}" data-tip="用途等級：{t}">{t}</span>'


def res_strip(owner: str, tier_name: str, observed: str, drift: str = "", lock: str = "") -> str:
    """資源狀態條：控制者、用途等級、觀測時間、漂移、鎖，全站同一個元件。"""
    parts = [attr("user", f"控制者：{owner}", owner), tier(tier_name), attr("history", f"觀測時間 {observed}", observed)]
    parts.append(attr("diff", f"與平台紀錄不同：{drift}", "漂移 1", "warn") + '<button type="button" class="btn sm">比較</button>' if drift else attr("check", "與平台紀錄一致", "一致", "ok"))
    parts.append(attr("lock", f"鎖：{lock}", lock, "warn") if lock else attr("lock", "目前沒有工作佔用", "無鎖"))
    return '<div class="rstrip" role="group" aria-label="資源狀態">' + "".join(parts) + "</div>"

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
