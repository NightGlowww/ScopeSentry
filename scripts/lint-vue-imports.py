#!/usr/bin/env python3
"""检查所有 .vue 文件里用到的 Element Plus 组件是否都已 import。

背景：缺少 import 时 Vue 不会报错，而是把 <ElAlert> 渲染成未解析的 <elalert>
自定义标签——组件样式和交互全部失效，界面上看不出来（2026-11-20 真实踩坑：
任务进度弹窗的工具失败告警因此长期不显示）。

用法：
    python3 scripts/lint-vue-imports.py            # 检查，异常退出码 1
    python3 scripts/lint-vue-imports.py --warn     # 只报告不失败

接进 release.sh 的审计门禁，发布前自动拦截。
"""

import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
SCAN_DIRS = [ROOT / "frontend" / "src" / "views", ROOT / "frontend" / "src" / "components"]

USED_RE = re.compile(r"<(El[A-Z][A-Za-z0-9]+)")
IMPORT_RE = re.compile(r"^import\s*\{([^}]*)\}\s*from\s*'element-plus'", re.M)

# 不是组件的名字（TS 类型等）
IGNORE = {"ElRef"}


def imported_names(src: str) -> set:
    names = set()
    for m in IMPORT_RE.finditer(src):
        for part in m.group(1).split(","):
            name = part.strip().split(" as ")[-1].strip()
            if name:
                names.add(name)
    return names


def main() -> int:
    warn_only = "--warn" in sys.argv
    problems = []
    for root in SCAN_DIRS:
        if not root.exists():
            continue
        for path in root.rglob("*.vue"):
            src = path.read_text(encoding="utf-8", errors="ignore")
            used = set(USED_RE.findall(src))
            if not used:
                continue
            missing = sorted(used - imported_names(src) - IGNORE)
            if missing:
                problems.append((path.relative_to(ROOT), missing))

    if not problems:
        print("✓ vue 组件导入检查通过（无未解析的 Element Plus 组件）")
        return 0

    print("✗ 以下文件使用了未 import 的 Element Plus 组件：")
    for path, missing in problems:
        print(f"  {path}: {', '.join(missing)}")
    print("\n修复：把这些名字加进该文件的 import { ... } from 'element-plus'")
    return 0 if warn_only else 1


if __name__ == "__main__":
    sys.exit(main())
