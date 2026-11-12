#!/usr/bin/env python3
"""畫板改過之後，更新設計畫布的版面資料與 repo 的截圖。

用法：
  python3 design/tools/canvas_sync.py layout <render-dir> <目前的 canvas.json> <輸出 canvas.json>
      量每張畫板的實際高度寫回 canvas.json。畫板變高時把下面各列往下推（不往上拉），
      便條跟著它旁邊那一列移動。既有的便條文字不動。
  python3 design/tools/canvas_sync.py shots <render-dir> <design/screenshots> [畫板名稱 ...]
      重拍截圖：深色 1440、360、768 與淺色 1440。不給名稱就全部重拍。

<render-dir> 是 build_screens.py --render-dir 的輸出。需要 playwright（見 scan_layout.py）。
"""
import json
import sys
from pathlib import Path

from playwright.sync_api import sync_playwright

GAP = 120


def layout(render: Path, live: Path, out: Path) -> None:
    d = json.loads(live.read_text(encoding="utf-8"))
    boards, notes = d["boards"], d["notes"]
    fin = {k: v for k, v in boards.items() if v.get("page") == "final"}
    with sync_playwright() as p:
        b = p.chromium.launch()
        for k, v in fin.items():
            pg = b.new_page(viewport={"width": v["w"], "height": 900})
            pg.goto(f"file://{render}/{k[:-8]}-light.html")
            pg.wait_for_timeout(250)
            v["h"] = pg.evaluate("document.documentElement.scrollHeight")
            pg.close()
        b.close()
    rows = sorted({v["y"] for v in fin.values()})
    shift, moves = 0, []
    for i, y in enumerate(rows):
        if shift:
            moves.append((y, shift))
        bottom = max(y + shift + v["h"] for v in fin.values() if v["y"] == y)
        if i + 1 < len(rows) and rows[i + 1] + shift < bottom + GAP:
            shift += bottom + GAP - (rows[i + 1] + shift)
    for k, v in notes.items():
        if v.get("page") != "final" or v["y"] < 0:
            continue
        s = 0
        for y, sh in moves:
            if v["y"] >= y - 60:
                s = sh
        v["y"] += s
    for y, s in moves:
        for v in fin.values():
            if v["y"] == y:
                v["_ny"] = y + s
    for v in fin.values():
        if "_ny" in v:
            v["y"] = v.pop("_ny")
    out.write_text(json.dumps(d, ensure_ascii=False), encoding="utf-8")
    print("rows moved", len(moves), "max shift", max([s for _, s in moves] or [0]), "boards", len(fin), "notes", len(notes))


def shots(render: Path, dest: Path, names: list) -> None:
    names = names or sorted({p.name.rsplit("-", 1)[0] for p in render.glob("*-dark.html")})
    with sync_playwright() as p:
        b = p.chromium.launch()
        for name in names:
            for theme, w in (("dark", 1440), ("dark", 360), ("dark", 768), ("light", 1440)):
                pg = b.new_page(viewport={"width": w, "height": 900})
                pg.goto(f"file://{render}/{name}-{theme}.html")
                pg.wait_for_timeout(500)
                pg.screenshot(path=str(dest / f"{name}-{theme}-{w}.png"), full_page=True)
                pg.close()
        b.close()
    print("shots", len(names) * 4)


if __name__ == "__main__":
    mode, a = sys.argv[1], sys.argv[2:]
    if mode == "layout":
        layout(Path(a[0]).resolve(), Path(a[1]), Path(a[2]))
    elif mode == "shots":
        shots(Path(a[0]).resolve(), Path(a[1]).resolve(), a[2:])
    else:
        sys.exit(__doc__)
