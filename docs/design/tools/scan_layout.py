#!/usr/bin/env python3
"""版面掃描：每個畫板在多種寬度下檢查
- 超出面板／列（spill）
- 文字被裁切（CLIP）
- 桌面寬度（≥1024）單一文字被擠成兩行（WRAP）
- 桌面寬度表格或矩陣需要橫向捲動（HSCROLL）
- 整頁橫向溢出、JS 錯誤

用法：python3 docs/design/tools/scan_layout.py <render-dir>
<render-dir> 是 build_screens.py --render-dir 產出的資料夾。全部通過時結束碼 0。
需要 playwright（pip install playwright && playwright install chromium）。
"""
import asyncio, json, sys
from pathlib import Path
from playwright.async_api import async_playwright

WIDTHS = (360, 768, 1024, 1280, 1440, 1920)
JS = r"""() => { const out=[];
 const sc = el => { for (let p=el.parentElement; p; p=p.parentElement){ const o=getComputedStyle(p).overflowX; if(o==='auto'||o==='scroll') return true; if(p.matches('.panel,.tile,.ncard,.card,.row,.pstage,.mx .c,.ttab,.st,.btn,.chip,.edsb,.modal')) return false;} return false; };
 for (const el of document.querySelectorAll('main *')) { const cs=getComputedStyle(el); if(cs.display==='none'||cs.visibility==='hidden') continue; const r=el.getBoundingClientRect(); if(r.width===0||r.height===0) continue;
   const box = el.parentElement.closest('.panel,.tile,.ncard,.card,.row,.pstage,.mx .c,.ttab,.st,.btn,.chip,.edsb,.modal'); const b=box?box.getBoundingClientRect():null;
   if (b && (r.right>b.right+1 || r.left<b.left-1) && !sc(el)) { const t=(el.innerText||el.textContent||'').trim().slice(0,30); out.push(t||el.tagName); }
   if (el.textContent.trim() && el.scrollWidth>el.clientWidth+2 && (cs.overflow==='hidden'||cs.overflowX==='hidden'||cs.overflowX==='clip') && !el.matches('.panel,.tiles,.hb,main,.ncard') && !sc(el)) out.push('CLIP:'+el.textContent.trim().slice(0,30));
 }
 if (innerWidth>=1024) for (const el of document.querySelectorAll('main .row .st, main .row .mono, main .row .num, main .mx .st')) { if (el.closest('.wr')) continue; const cs=getComputedStyle(el); if(cs.display==='none') continue; const lh=parseFloat(cs.lineHeight)||16; const leaf=[...el.children].every(c=>c.tagName==='I'||c.tagName==='WBR'); if (leaf && el.getBoundingClientRect().height > lh*1.7 && el.textContent.trim().length<40) out.push('WRAP:'+el.textContent.trim().slice(0,30)); }
 if (innerWidth>=1024) for (const t of document.querySelectorAll('main .tw, main .mxw')) if (t.scrollWidth>t.clientWidth+1) out.push('HSCROLL:'+(t.closest('.panel')?.querySelector('h2,h3')?.textContent||'?').slice(0,24)+' +'+(t.scrollWidth-t.clientWidth));
 for (const lg of document.querySelectorAll('main .legend, main .lg')) { const kids=[...lg.children].filter(c=>getComputedStyle(c).display!=='none'); for (let k=1;k<kids.length;k++){ const a=kids[k-1].getBoundingClientRect(), b=kids[k].getBoundingClientRect(); if (Math.abs(a.top-b.top)<4 && b.left-a.right<8) { out.push('LEGEND-GAP:'+kids[k].textContent.trim().slice(0,12)); break; } } }
 return [...new Set(out)].slice(0,12); }"""


async def main(src: Path) -> int:
    names = sorted({p.name.rsplit("-", 1)[0] for p in src.glob("*-dark.html")})
    res = {}
    async with async_playwright() as p:
        b = await p.chromium.launch()
        for name in names:
            for theme in ("dark", "light"):
                for w in WIDTHS:
                    if theme == "light" and w not in (360, 1440):
                        continue
                    pg = await b.new_page(viewport={"width": w, "height": 900})
                    errs = []
                    pg.on("pageerror", lambda e: errs.append(str(e)))
                    await pg.goto(f"file://{src}/{name}-{theme}.html")
                    await pg.wait_for_timeout(80)
                    r = await pg.evaluate(JS)
                    sw = await pg.evaluate("document.documentElement.scrollWidth")
                    if sw > w:
                        r.append(f"PAGE-OVERFLOW +{sw - w}")
                    r += [f"JS-ERROR {e}" for e in errs]
                    if r:
                        res[f"{name}-{theme}@{w}"] = r
                    await pg.close()
        await b.close()
    for k, v in res.items():
        print(k, " | ".join(x.replace("\n", "/") for x in v))
    print(f"{len(names)} boards, {len(res)} with issues")
    return 1 if res else 0


if __name__ == "__main__":
    sys.exit(asyncio.run(main(Path(sys.argv[1]).resolve())))
