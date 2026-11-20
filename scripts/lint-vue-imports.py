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



# ---- Vue API 导入检查 ----
# 起因：ProgressInfo.vue 用了 computed() 但没 import，setup 直接抛错，整个弹窗白屏，
# 而构建、lint、发版全部通过。原有检查只覆盖 Element Plus 组件。
VUE_APIS = ["computed", "watch", "watchEffect", "nextTick", "onMounted", "onUnmounted",
            "onBeforeMount", "onBeforeUnmount", "provide", "inject", "defineAsyncComponent"]
VUE_IMPORT_RE = re.compile(r"^import\s*\{([^}]*)\}\s*from\s*'vue'", re.M)
SCRIPT_RE = re.compile(r"<script[^>]*>(.*?)</script>", re.S)


def vue_api_problems(src: str):
    m = SCRIPT_RE.search(src)
    if not m:
        return []
    script = m.group(1)
    imported = set()
    for im in VUE_IMPORT_RE.finditer(script):
        for part in im.group(1).split(","):
            name = part.strip().split(" as ")[-1].strip()
            if name:
                imported.add(name)
    missing = []
    for api in VUE_APIS:
        if re.search(r"\b" + api + r"\s*\(", script) and api not in imported:
            missing.append(api)
    return missing

def main() -> int:
    warn_only = "--warn" in sys.argv
    problems = []
    for root in SCAN_DIRS:
        if not root.exists():
            continue
        for path in root.rglob("*.vue"):
            src = path.read_text(encoding="utf-8", errors="ignore")
            used = set(USED_RE.findall(src))
            missing = sorted(used - imported_names(src) - IGNORE) if used else []
            api_missing = vue_api_problems(src)
            if missing or api_missing:
                problems.append((str(path.relative_to(ROOT)), missing, api_missing))

    if not problems:
        print("✓ 前端导入检查通过（Element Plus 组件 + Vue API 均已 import）")
        return 0

    print("✗ 以下文件存在未 import 的用法（会导致渲染失败或白屏）：")
    for path, missing, api_missing in problems:
        bits = []
        if missing:
            bits.append("组件: " + ", ".join(missing))
        if api_missing:
            bits.append("Vue API: " + ", ".join(api_missing))
        print(f"  {path}: {'; '.join(bits)}")
    print("\n修复：组件加进 element-plus 的 import；Vue API 加进 vue 的 import")
    return 0 if warn_only else 1

if __name__ == "__main__":
    sys.exit(main())
