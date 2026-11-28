#!/usr/bin/env python3
"""界面 ↔ 接口闭环：找出「后端有、前端从没调用过」的写路由。

为什么要有这个检查：按钮点了没反应好发现（那是死按钮），反过来的「接口在那儿但界面
没有入口」几乎不会有人报 —— 结果是功能半成品：周期建错了改不了、结果总览调不到。
2026-11-29 第 106 轮就是这么捞出 GitHub 周期任务编辑与结果总览的。

判定：把 routes.snapshot.tsv 里的写路由（非 GET）与 frontend/src 里的 "/api/..." 字面量
（含模板串与 + 拼接前缀）做归一化比对；:param 与 ${...} 都归一成 *。
未命中的必须是「有意为之」，登记在 ui_api_excuses.json 里并写清原因，否则退出码 1。

  python3 scripts/ci/ui_api_closure.py           # 检查（verify_all 用）
  python3 scripts/ci/ui_api_closure.py --write   # 重新生成 docs/audit/UI_API_CLOSURE.md
"""
import json
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parents[2]
SNAPSHOT = ROOT / "docs/audit/routes.snapshot.tsv"
FRONTEND = ROOT / "frontend/src"
EXCUSES = ROOT / "scripts/ci/ui_api_excuses.json"
OUT_MD = ROOT / "docs/audit/UI_API_CLOSURE.md"


def norm(path: str) -> str:
    p = re.sub(r"\$\{[^}]*\}", "*", path)
    p = re.sub(r":[A-Za-z_]+", "*", p)
    p = p.split("?")[0]
    return p.rstrip("/")


def write_routes():
    out = []
    for line in SNAPSHOT.read_text(encoding="utf-8").splitlines():
        if not line.strip():
            continue
        method, path = line.split("\t")
        # 只查写路由：GET 读路由没人调通常只是「页面还没做」，写路由没人调是真断链
        if method.strip() != "GET":
            out.append(path.strip())
    return out


def frontend_paths():
    """收集前端出现过的接口路径。

    三种写法都要认，否则会大量误报：
      1) 字面量           '/api/task/'
      2) 模板串           `/api/task/${id}/stop`
      3) 加号拼接         '/api/github/threat/' + kind + '_config'
    第 3 种只能取到前缀，比对时按前缀命中处理（前缀越短误报越少、漏报越多，取路径前两段即止）。
    """
    exact, prefixes = set(), set()
    for f in list(FRONTEND.rglob("*.ts")) + list(FRONTEND.rglob("*.tsx")) + list(FRONTEND.rglob("*.vue")):
        text = f.read_text(encoding="utf-8", errors="ignore")
        for m in re.finditer(r"[\'\"`](/api/[^\'\"`\s]*)", text):
            exact.add(norm(m.group(1)))
        for m in re.finditer(r"[\'\"`](/api/[^\'\"`\s]*/)[\'\"`]\s*\+", text):
            prefixes.add(norm(m.group(1)))
    return exact, prefixes


def referenced(route: str, exact: set, prefixes: set) -> bool:
    rn = norm(route)
    if rn in exact:
        return True
    for e in exact:
        if e.endswith("/*") and rn.startswith(e[:-1]):
            return True
        if e and rn.startswith(e + "/"):
            return True
    for p in prefixes:
        if rn.startswith(p):
            return True
    return False


def load_excuses():
    if not EXCUSES.exists():
        return {}
    data = json.loads(EXCUSES.read_text(encoding="utf-8"))
    return data.get("routes", {})


def main():
    routes = write_routes()
    exact, prefixes = frontend_paths()
    excuses = load_excuses()
    missing = [r for r in routes if not referenced(r, exact, prefixes)]
    unexcused = [r for r in missing if r not in excuses]
    stale = [r for r in excuses if r not in missing]

    print("写路由 %d" % len(routes))
    print("  前端字面量路径 %d（另 %d 个拼接前缀）" % (len(exact), len(prefixes)))
    print("  无前端入口 %d（其中已登记 %d）" % (len(missing), len(missing) - len(unexcused)))
    for r in unexcused:
        print("    ! %s" % r)
    for r in stale:
        print("    ~ 登记已过期（前端已接或有变动）: %s" % r)

    if "--write" in sys.argv:
        lines = ["# 界面 ↔ 接口闭环", "",
                 "> 生成：`python3 scripts/ci/ui_api_closure.py --write`。写路由 = routes.snapshot.tsv 中非 GET 的项。",
                 "> 未接前端的必须登记在 `scripts/ci/ui_api_excuses.json` 并写清原因，否则门禁失败。", "",
                 "| 指标 | 数量 |", "| --- | --- |",
                 "| 写路由 | %d |" % len(routes),
                 "| 前端有入口 | %d |" % (len(routes) - len(missing)),
                 "| 无入口·已登记 | %d |" % (len(missing) - len(unexcused)),
                 "| 无入口·未登记 | %d |" % len(unexcused), ""]
        if unexcused:
            lines += ["## 未登记（需要处理）", ""]
            lines += ["- `%s`" % r for r in sorted(unexcused)]
            lines.append("")
        lines += ["## 已登记的无入口写路由", "", "| 路由 | 原因 |", "| --- | --- |"]
        for r in sorted(missing):
            if r in excuses:
                lines.append("| `%s` | %s |" % (r, excuses[r]))
        lines.append("")
        OUT_MD.write_text("\n".join(lines), encoding="utf-8")
        print("written %s" % OUT_MD.relative_to(ROOT))

    if unexcused:
        print("UI_API_CLOSURE=FAIL")
        return 1
    print("UI_API_CLOSURE=PASS")
    return 0


if __name__ == "__main__":
    sys.exit(main())
