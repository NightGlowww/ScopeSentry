#!/usr/bin/env python3
"""拉取 ARL-Next 上游最新路由快照，并与本地基线做差异对比。

为什么需要它：对齐上游不是一次性的。上游新增一个端点（比如某个族的 /export/），
我们这边没人知道，覆盖表就一直显示「已对齐」。这个脚本把「上游现在有什么」变成
可复现的事实：抓 tree -> 逐个 routes 文件解析装饰器 -> 与 docs/audit/upstream_routes.json 比对。

用法：
  python3 scripts/ci/upstream_sync.py            # 只打印差异
  python3 scripts/ci/upstream_sync.py --write    # 写回基线并记录 ref
"""
import json
import pathlib
import re
import sys
import urllib.request

ROOT = pathlib.Path(__file__).resolve().parents[2]
UPSTREAM = ROOT / "docs/audit/upstream_routes.json"
META = ROOT / "docs/audit/upstream_meta.json"
REPO = "owl234/ARL-Next"
ROUTE_DIR = "backend/app/routes/"

# 同时认 flask 的 @bp.route 与 @bp.get/post 两种写法
ROUTE_RE = re.compile(r"@[\w.]+\.(?:route|get|post|put|delete|patch)\(\s*['\"]([^'\"]+)['\"]", re.M)


def api(url):
    req = urllib.request.Request(url, headers={"User-Agent": "scopesentry-parity"})
    with urllib.request.urlopen(req, timeout=60) as r:
        return r.read()


def head_ref():
    data = json.loads(api("https://api.github.com/repos/%s/commits?per_page=1" % REPO))
    return data[0]["sha"], data[0]["commit"]["committer"]["date"]


def parse_routes(text):
    out = []
    for m in ROUTE_RE.finditer(text):
        p = m.group(1)
        if p not in out:
            out.append(p)
    return out


def main():
    ref, date = head_ref()
    tree = json.loads(api("https://api.github.com/repos/%s/git/trees/%s?recursive=1" % (REPO, ref)))
    files = sorted(t["path"] for t in tree["tree"]
                   if t["type"] == "blob" and t["path"].startswith(ROUTE_DIR) and t["path"].endswith(".py"))
    fresh = {}
    for path in files:
        raw = api("https://raw.githubusercontent.com/%s/%s/%s" % (REPO, ref, path)).decode("utf-8", "ignore")
        fresh[path[len(ROUTE_DIR):]] = parse_routes(raw)

    base = json.loads(UPSTREAM.read_text(encoding="utf-8")) if UPSTREAM.exists() else {}
    added_files = sorted(set(fresh) - set(base))
    gone_files = sorted(set(base) - set(fresh))
    added_eps, gone_eps = [], []
    for f in sorted(set(fresh) & set(base)):
        a = [e for e in fresh[f] if e not in base[f]]
        g = [e for e in base[f] if e not in fresh[f]]
        if a:
            added_eps.append((f, a))
        if g:
            gone_eps.append((f, g))

    print("上游 %s @ %s（%s）" % (REPO, ref[:8], date))
    print("  当前：文件 %d，端点 %d" % (len(fresh), sum(len(v) for v in fresh.values())))
    print("  基线：文件 %d，端点 %d" % (len(base), sum(len(v) for v in base.values())))
    print("")
    print("新增文件 %d：%s" % (len(added_files), ", ".join(added_files) or "-"))
    if added_eps:
        for f, eps in added_eps:
            print("  新增端点 %s: %s" % (f, ", ".join(eps)))
    else:
        print("  新增端点：无")
    print("")
    print("移除文件 %d：%s" % (len(gone_files), ", ".join(gone_files) or "-"))
    for f, eps in gone_eps:
        print("  移除端点 %s: %s" % (f, ", ".join(eps)))

    if "--write" in sys.argv:
        UPSTREAM.write_text(json.dumps(fresh, ensure_ascii=False, indent=1) + "\n", encoding="utf-8")
        META.write_text(json.dumps({"repo": REPO, "ref": ref, "date": date,
                                    "files": len(fresh),
                                    "endpoints": sum(len(v) for v in fresh.values())},
                                   ensure_ascii=False, indent=1) + "\n", encoding="utf-8")
        print("written %s" % UPSTREAM.relative_to(ROOT))
    return 0


if __name__ == "__main__":
    sys.exit(main())
