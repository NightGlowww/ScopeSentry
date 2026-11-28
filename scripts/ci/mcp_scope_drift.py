#!/usr/bin/env python3
"""MCP 工具 ↔ scope 漂移检查（纯静态，不连库不连网）。

规则：internal/mcp/tools.go 里每个 addTool(...Name: "x") 必须在
internal/services/mcpguard/scope.go 的 ToolScopes 里登记；反向也不许凭空多出条目。
未登记的工具在运行时会被「默认拒绝」，只有持 * 的 key 能调 —— 这类缺口只有真机调一次才会暴露，
所以放在 CI 里静态挡。

用法: python3 scripts/ci/mcp_scope_drift.py
"""
import os
import re
import sys

ROOT = os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', '..')
TOOLS = os.path.join(ROOT, 'internal', 'mcp', 'tools.go')
SCOPE = os.path.join(ROOT, 'internal', 'services', 'mcpguard', 'scope.go')


def registered():
    src = open(TOOLS, encoding='utf-8', errors='ignore').read()
    names = []
    for m in re.finditer(r'addTool\(server, allowedTools, &names, &mcp\.Tool\{\s*Name:\s*"([a-z0-9_]+)"', src, re.S):
        names.append(m.group(1))
    return names


def scoped():
    src = open(SCOPE, encoding='utf-8', errors='ignore').read()
    body = src[src.find('var ToolScopes = map[string]string{'):]
    return set(re.findall(r'"([a-z0-9_]+)":\s*Scope', body))


def main():
    names = registered()
    keys = scoped()
    dup = sorted({n for n in names if names.count(n) > 1})
    missing = [n for n in names if n not in keys]
    extra = sorted(k for k in keys if k not in names)
    print('工具 %d，已登记 scope %d' % (len(names), len(keys)))
    for tag, items in (('重复注册', dup), ('未登记 scope', missing), ('多余 scope 条目', extra)):
        if items:
            print('  %s: %s' % (tag, ', '.join(items)))
    bad = bool(dup or missing or extra)
    print('MCP_SCOPE_DRIFT=%s' % ('FAIL' if bad else 'PASS'))
    return 1 if bad else 0


if __name__ == '__main__':
    sys.exit(main())