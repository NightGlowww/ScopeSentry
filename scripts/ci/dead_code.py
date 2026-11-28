#!/usr/bin/env python3
"""死代码体检：包级函数定义了但全仓没人引用。

起因（2026-11-29）：连续挖出三个「写好了没人调」的功能 ——
  - config.Notification：通知配置能配，但从没发过一条消息；
  - ignoredRepos：黑名单能加，采集从不查；
  - 去重的 flag/hour：配置能勾，调度器里没有对应 job。
共同点是「代码在、链路断」，而编译器不会报 —— 这类只能靠扫引用。

规则：统计包级函数名（func X(...)）在全仓（不含 _test）的引用次数；
     0 次 → 要么删、要么登记原因。方法（带接收者）不查：它们常被接口调用，误报太多。

已知误报来源（都写进 ui_api_excuses 风格的白名单）：
  - 由字符串注册/反射调用的（如 scheduler handler 名字）；
  - 仅被 _test 使用的测试辅助函数；
  - 平台入口（main/init）与生成代码。

用法：python3 scripts/ci/dead_code.py [--write]
"""
import json
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parents[2]
EXCUSES = ROOT / "scripts/ci/dead_code_excuses.json"
OUT_MD = ROOT / "docs/audit/DEAD_CODE.md"


# 扫描范围必须覆盖所有 Go 包：只扫 internal/ + cmd/ 会把 plugin-template 里的调用方
# 当成不存在，于是 ConvertFofaToCustomQuery 这种「插件模板在用」的函数被误判成死代码
# （2026-11-29 实测，删掉后 plugin-template 直接编译不过）。
SCAN_DIRS = ("internal", "cmd", "pkg", "plugin-template", "docs")


def sources():
    out = {}
    for base in SCAN_DIRS:
        d = ROOT / base
        if not d.exists():
            continue
        for p in d.rglob("*.go"):
            # symbols/ 是生成代码，只做索引、不产生引用
            if "_test" in p.name or "symbols" in p.parts:
                continue
            out[p] = p.read_text(encoding="utf-8", errors="ignore")
    return out


def main():
    srcs = sources()
    defs = []
    for p, t in srcs.items():
        for m in re.finditer(r"^func ([A-Za-z_]\w*)\(", t, re.M):
            defs.append((p, m.group(1)))

    dead = []
    for p, name in defs:
        if name in ("main", "init"):
            continue
        refs = 0
        for t in srcs.values():
            for m in re.finditer(r"\b" + re.escape(name) + r"\b", t):
                start = t.rfind("\n", 0, m.start()) + 1
                nl = t.find("\n", m.start())
                line = t[start: nl if nl > 0 else len(t)]
                if line.strip().startswith("func " + name + "("):
                    continue
                refs += 1
        if refs == 0:
            dead.append((str(p.relative_to(ROOT)), name))

    excuses = json.loads(EXCUSES.read_text(encoding="utf-8")) if EXCUSES.exists() else {}
    unexcused = [(f, n) for f, n in dead if f + "::" + n not in excuses.get("functions", {})]
    stale = [k for k in excuses.get("functions", {}) if k not in {f + "::" + n for f, n in dead}]

    print("包级函数 %d 个，无人引用 %d 个（其中已登记 %d）" % (len(defs), len(dead), len(dead) - len(unexcused)))
    for f, n in unexcused:
        print("    ! %s :: %s" % (f, n))
    for k in stale:
        print("    ~ 登记已过期（已删除或无引用变化）: %s" % k)

    if "--write" in sys.argv:
        lines = ["# 死代码账本", "",
                 "> 生成：`python3 scripts/ci/dead_code.py --write`。口径：包级函数在全仓（不含 _test）零引用。",
                 "> 未登记的必须删掉或说明为什么留（由字符串/反射调用）。", "",
                 "| 指标 | 数量 |", "| --- | --- |",
                 "| 包级函数 | %d |" % len(defs),
                 "| 无人引用（合计） | %d |" % len(dead),
                 "| 其中已登记 | %d |" % (len(dead) - len(unexcused)),
                 "| 待处理 | %d |" % len(unexcused), ""]
        if unexcused:
            lines += ["## 待处理", ""] + ["- `%s` :: `%s`" % (f, n) for f, n in unexcused] + [""]
        entries = excuses.get("functions", {})
        if entries:
            lines += ["## 已登记（保留原因）", "", "| 函数 | 原因 |", "| --- | --- |"]
            for k, v in sorted(entries.items()):
                lines.append("| `%s` | %s |" % (k, v))
            lines.append("")
        OUT_MD.write_text("\n".join(lines), encoding="utf-8")
        print("written %s" % OUT_MD.relative_to(ROOT))

    if unexcused:
        print("DEAD_CODE=FAIL")
        return 1
    print("DEAD_CODE=PASS")
    return 0


if __name__ == "__main__":
    sys.exit(main())
