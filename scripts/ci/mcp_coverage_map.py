#!/usr/bin/env python3
"""MCP 覆盖面对照：REST 路由 ↔ MCP 工具，按 URL 模块给出「谁能做、谁只能在 UI 做」。

用法: python3 scripts/ci/mcp_coverage_map.py [--md]
路由前缀按 RouteGroup 嵌套还原（外层 /api/<mod>，内层子组依次拼接），不猜语义；
工具归模块只按名字关键词，归不上的显式列在末尾，避免把「没覆盖」说成「已覆盖」。
"""
import os
import re
import sys
from collections import defaultdict

ROOT = os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', '..')

TOOL_HINTS = {
    'asset': ['asset', 'subdomain', 'url', 'rootdomain', 'crawler', 'dirscan', 'ipasset', 'stat_finger', 'fingerprint'],
    'task': ['task', 'template'],
    'poc': ['poc'],
    'project': ['project'],
    'dictionary': ['dictionary', 'port_dict'],
    'node': ['node'],
    'plugin': ['plugin'],
    'export': ['export'],
    'icp': ['icp'],
    'github': ['github'],
    'sensitive': ['sensitive_rule'],
    'policy': ['scan_polic'],
    'assetgroup': ['asset_group', 'asset_scope'],
    'dashboard': ['dashboard', 'profile', 'console'],
    'audit': ['audit'],
    'system': ['system_info'],
    'nuclei': ['nuclei'],
    'wih': ['wih'],
    'intel': ['intel'],
}


def rest_routes():
    out = []
    base = os.path.join(ROOT, 'internal', 'api', 'routes')
    for dp, dn, fn in os.walk(base):
        for f in sorted(fn):
            if not f.endswith('.go') or f.endswith('_test.go'):
                continue
            p = os.path.join(dp, f)
            groups = []  # (line_no, path)
            routes = []  # (line_no, method, path)
            for i, line in enumerate(open(p, encoding='utf-8', errors='ignore')):
                mm = re.search(r'\{Method:\s*"(\w+)",\s*Path:\s*"([^"]*)"', line)
                if mm:
                    routes.append((i, mm.group(1), mm.group(2)))
                    continue
                gm = re.search(r'Path:\s*"(/[^"]*)"', line)
                if gm:
                    groups.append((i, gm.group(1)))
            for ln, method, path in routes:
                before = [g for g in groups if g[0] < ln]
                base_group = ''
                for idx in range(len(before) - 1, -1, -1):
                    if before[idx][1].startswith('/api'):
                        base_group = before[idx][1]
                        inner = [g[1] for g in before[idx + 1:]]
                        break
                else:
                    inner = [g[1] for g in before]
                full = base_group + ''.join(inner) + path
                out.append((method, re.sub(r'/+', '/', full), os.path.basename(p)))
    return out


def mcp_tools():
    p = os.path.join(ROOT, 'internal', 'mcp', 'tools.go')
    text = open(p, encoding='utf-8', errors='ignore').read()
    seen, out = set(), []
    for n in re.findall(r'Name:\s*"([a-z0-9_]+)"', text):
        if n not in seen:
            seen.add(n)
            out.append(n)
    return out


def module_of(path):
    parts = [x for x in path.split('/') if x]
    if parts and parts[0] == 'api' and len(parts) > 1:
        return parts[1]
    return parts[0] if parts else '?'


def main():
    routes = rest_routes()
    tools = mcp_tools()
    by_mod = defaultdict(list)
    for m, path, f in routes:
        by_mod[module_of(path)].append((m, path))
    print('REST 端点 %d（%d 个模块），MCP 工具 %d' % (len(routes), len(by_mod), len(tools)))
    print()
    gaps = []
    for mod in sorted(by_mod):
        hints = TOOL_HINTS.get(mod, [mod])
        mine = sorted({t for t in tools if any(h in t for h in hints)})
        flag = '' if mine else '   ← 无 MCP 覆盖'
        if not mine:
            gaps.append((mod, len(by_mod[mod])))
        print('%-16s REST %2d  MCP %2d  %s%s' % (mod, len(by_mod[mod]), len(mine), ','.join(mine[:6]), flag))
    print()
    print('无 MCP 覆盖的模块：' + (', '.join('%s(%d)' % g for g in gaps) if gaps else '无'))
    if '--md' in sys.argv:
        print()
        print('| 模块 | REST 端点数 | MCP 工具数 |')
        print('| --- | --- | --- |')
        for mod in sorted(by_mod):
            hints = TOOL_HINTS.get(mod, [mod])
            mine = {t for t in tools if any(h in t for h in hints)}
            print('| %s | %d | %d |' % (mod, len(by_mod[mod]), len(mine)))


if __name__ == '__main__':
    main()