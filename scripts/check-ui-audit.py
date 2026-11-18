#!/usr/bin/env python3
"""校验 docs/ui-audit-baseline.json 是否「0 违规」。

用法：
    python3 scripts/check-ui-audit.py docs/ui-audit-baseline.json

退出码 0 表示通过；非 0 时会列出违规路由与字段。
"""
import json
import sys

VIOLATION_KEYS = (
    "notFound",
    "pageOverflow",
    "paginationWithoutRows",
    "destructiveEnabledWithoutSelection",
    "unnamedIconControls",
)


def main() -> int:
    path = sys.argv[1] if len(sys.argv) > 1 else "docs/ui-audit-baseline.json"
    with open(path, encoding="utf-8") as fh:
        data = json.load(fh)

    results = data.get("results") or []
    if not results:
        print(f"FAIL: {path} 里没有任何审计结果")
        return 2

    bad = []
    for row in results:
        hits = {k: row.get(k) for k in VIOLATION_KEYS if row.get(k)}
        if hits:
            bad.append((row.get("route"), hits))

    if bad:
        print(f"FAIL: {len(bad)}/{len(results)} 条路由存在违规")
        for route, hits in bad:
            print(f"  - {route}: {hits}")
        return 1

    print(f"PASS: {len(results)} 条路由，0 违规")
    return 0


if __name__ == "__main__":
    sys.exit(main())
