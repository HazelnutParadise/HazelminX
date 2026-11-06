#!/usr/bin/env python3
"""從 1Panel 原始碼抽出 Go 路由與前端路由，產生功能基線盤點的機器可讀清單。

用法：
    python3 scripts/baseline/extract_1panel_routes.py <1panel-checkout> <out-dir>

輸出：
    <out-dir>/1panel-v2.4.1-api-routes.json      core/ 與 agent/ 的 HTTP 端點
    <out-dir>/1panel-v2.4.1-frontend-routes.json frontend/src/routers 的路由
    <out-dir>/1panel-v2.4.1-baseline.csv         合併後的基線對照表（含空白的 HazelminX 對應欄）

只讀取原始碼；不執行 1Panel。
"""
import csv
import json
import os
import re
import subprocess
import sys

GO_ROUTE = re.compile(r'(\w+)\.(GET|POST|PUT|DELETE|PATCH|Any)\(\s*"([^"]*)"\s*,\s*([^)]*)\)')
GO_GROUP = re.compile(r'(\w+)\s*:?=\s*\w+\.Group\(\s*"([^"]*)"\s*\)')
TS_ROUTE_PATH = re.compile(r'path:\s*[\'"]([^\'"]*)[\'"]')
TS_ROUTE_NAME = re.compile(r'name:\s*[\'"]([^\'"]*)[\'"]')
TS_ROUTE_COMP = re.compile(r'component:\s*\(\)\s*=>\s*import\([\'"]([^\'"]*)[\'"]\)')
TS_HIDDEN = re.compile(r'hidden:\s*true')


def git_head(root):
    try:
        return subprocess.check_output(["git", "-C", root, "rev-parse", "HEAD"], text=True).strip()
    except Exception:
        return "unknown"


def scan_go(root, subdir):
    routes = []
    router_dir = os.path.join(root, subdir, "router")
    for fn in sorted(os.listdir(router_dir)):
        if not fn.endswith(".go") or fn.endswith("_test.go"):
            continue
        path = os.path.join(router_dir, fn)
        text = open(path, encoding="utf-8", errors="replace").read()
        groups = {m.group(1): m.group(2) for m in GO_GROUP.finditer(text)}
        for m in GO_ROUTE.finditer(text):
            var, method, p, handler = m.groups()
            prefix = groups.get(var, "")
            handler = handler.strip().split(",")[-1].strip()
            routes.append({
                "process": subdir,
                "file": f"{subdir}/router/{fn}",
                "group": prefix,
                "method": method,
                "path": "/" + "/".join(x.strip("/") for x in (prefix, p) if x.strip("/")),
                "handler": handler,
            })
    return routes


def scan_frontend(root):
    routes = []
    mod_dir = os.path.join(root, "frontend", "src", "routers", "modules")
    files = [os.path.join(root, "frontend", "src", "routers", "router.ts")]
    files += [os.path.join(mod_dir, f) for f in sorted(os.listdir(mod_dir)) if f.endswith(".ts")]
    for path in files:
        text = open(path, encoding="utf-8", errors="replace").read()
        rel = os.path.relpath(path, root)
        # 以物件區塊粗切：每個 "path:" 當一筆，往後找 name/component/hidden 直到下一個 path:
        positions = [m.start() for m in TS_ROUTE_PATH.finditer(text)]
        for i, pos in enumerate(positions):
            end = positions[i + 1] if i + 1 < len(positions) else len(text)
            block = text[pos:end]
            p = TS_ROUTE_PATH.search(block).group(1)
            name = TS_ROUTE_NAME.search(block)
            comp = TS_ROUTE_COMP.search(block)
            routes.append({
                "file": rel,
                "path": p,
                "name": name.group(1) if name else "",
                "component": comp.group(1) if comp else "",
                "hidden": bool(TS_HIDDEN.search(block)),
            })
    return routes


def main():
    if len(sys.argv) != 3:
        print(__doc__)
        sys.exit(2)
    root, out = sys.argv[1], sys.argv[2]
    os.makedirs(out, exist_ok=True)
    head = git_head(root)
    api = scan_go(root, "core") + scan_go(root, "agent")
    fe = scan_frontend(root)
    meta = {"source": "1Panel-dev/1Panel", "tag": "v2.4.1", "commit": head, "generator": "scripts/baseline/extract_1panel_routes.py"}
    json.dump({"meta": meta, "routes": api}, open(os.path.join(out, "1panel-v2.4.1-api-routes.json"), "w"), ensure_ascii=False, indent=1)
    json.dump({"meta": meta, "routes": fe}, open(os.path.join(out, "1panel-v2.4.1-frontend-routes.json"), "w"), ensure_ascii=False, indent=1)
    with open(os.path.join(out, "1panel-v2.4.1-baseline.csv"), "w", newline="") as f:  # noqa: E501
        w = csv.writer(f, lineterminator="\n")
        w.writerow(["kind", "process_or_file", "method", "path", "handler_or_component", "hidden",
                    "hazelminx_ticket", "hazelminx_status", "evidence"])
        for r in api:
            w.writerow(["api", r["process"], r["method"], r["path"], r["handler"], "", "", "", ""])
        for r in fe:
            w.writerow(["frontend", r["file"], "", r["path"], r["component"], "yes" if r["hidden"] else "", "", "", ""])
    print(f"api routes: {len(api)} (core {sum(1 for r in api if r['process']=='core')}, agent {sum(1 for r in api if r['process']=='agent')}); frontend routes: {len(fe)}; commit {head}")


if __name__ == "__main__":
    main()
