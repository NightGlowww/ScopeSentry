
#!/usr/bin/env python3
"""MCP 工具闭环矩阵：把「82 个工具到底各自验到什么程度」变成可重复的账本。

为什么需要：tools/list 里有、scope 表里有，都不等于「能用」。真正的闭环至少要回答：
  - 读：用真实 id 调一次是不是 isError=false（读得到数据）
  - 拒绝分支：喂不存在的 id / 非法参数是不是 isError=true（不假成功）
  - 写往返：合法参数下真的改到库、并且能还原（最硬的证据，目前 5 个工具）
剩下的工具不是坏的，而是没有可安全验证的前置条件（比如要真跑一次扫描）。这类必须显式写出来，
否则「82/82 覆盖」会变成一句空话。

用法：
  python3 scripts/ci/mcp_closure.py            # 打印汇总
  python3 scripts/ci/mcp_closure.py --write     # 重新生成 docs/audit/MCP_CLOSURE.md
  python3 scripts/ci/mcp_closure.py --strict    # 有未归类工具时退出码 1（可接 CI）
"""
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parents[2]
TOOLS_GO = ROOT / 'internal/mcp/tools.go'
SCOPE_GO = ROOT / 'internal/services/mcpguard/scope.go'
SWEEP = ROOT / 'scripts/ci/mcp_sweep.py'
OUT_MD = ROOT / 'docs/audit/MCP_CLOSURE.md'

# 写路径做过「真改库 + 复核 + 还原」往返的工具（线上真实对象，2026-11-28）
WRITE_ROUNDTRIP = {
    'assign_tasks_project': '绑定任务到项目 → REST 复核 bindProject → 传空 projectId 解除 → 回到原值',
    'set_scheduled_task_state': '计划任务 enabled False→True→False，cycleType 与其余字段不变',
    'create_scan_policy': '建 ci-rt 临时策略 → REST policy/list 可见 → 删除 → 列表归零',
    'set_port_dict_entry': '新增 ci-rt 条目 → 条数 3→4 且值回读 12345 → 按 id 删除 → 回到 3 条',
    'set_sensitive_rule_state': '规则 state True→False→True，列表回读每一步都对得上',
}

# 需要前置样本或明确不该自动跑的工具：写清原因，别让「未验证」变成静默
NOTES = {
    'create_scan_task': '会真的建任务并触发扫描；sweep 只用非法/缺参验 schema，不制造扫描',
    'pause_task_targets': '需要「运行中且目标尚未全部领取」的任务；线上没有这种样本时只验拒绝分支',
    'resume_task_targets': '与 pause 成对，同一前置条件',
    'apply_scan_policy': '策略试算（不写库、不建任务）；需要至少一个真实策略样本，线上无策略时该分支跳过',
    'create_export_task': '会落文件并占磁盘；成功路径由 verify_contract.sh 的 SS_EXPORT_ROUNDTRIP 单独开关跑',
    'create_icp_task': '会对外部 ICP 数据源发起查询并起后台 goroutine；只验非法入参',
    'export_task_workbook': '会生成 Excel 文件；只验非法入参',
    'delete_nodes': '节点是扫描执行体，误删影响扫面；只验不存在的名字',
}

def read(p):
    return p.read_text(encoding='utf-8')

def parse_tools(src):
    out = {}
    for m in re.finditer(r'Name:\s+"([a-z0-9_]+)"', src):
        out.setdefault(m.group(1), True)
    return out

def parse_scope_values(src):
    """ScopeAssetWrite = "asset:write" → {ScopeAssetWrite: asset:write}"""
    return {m.group(1): m.group(2) for m in re.finditer(r'(Scope\w+)\s*=\s*"([^"]+)"', src)}

def parse_tool_scopes(src):
    return {m.group(1): m.group(2) for m in re.finditer(r'"([a-z0-9_]+)":\s*(Scope\w+),', src)}

def parse_sweep(src):
    """只认「元组第二个元素是 dict」的调用，避免把 ('asset_type', 'asset') 之类参数对当工具名。"""
    guard_at = src.find('GUARD = [')
    read_part = src[:guard_at] if guard_at > 0 else src
    guard_part = src[guard_at:] if guard_at > 0 else ''
    pat = re.compile(r"\(\s*'([a-z0-9_]+)'\s*,\s*\{")
    return set(pat.findall(read_part)), set(pat.findall(guard_part))

def main():
    tools = parse_tools(read(TOOLS_GO))
    scope_values = parse_scope_values(read(SCOPE_GO))
    tool_scopes = parse_tool_scopes(read(SCOPE_GO))
    read_names, guard_names = parse_sweep(read(SWEEP))

    rows = []
    for name in sorted(tools):
        marks = []
        if name in read_names:
            marks.append('读')
        if name in guard_names:
            marks.append('拒绝分支')
        if name in WRITE_ROUNDTRIP:
            marks.append('写往返')
        const = tool_scopes.get(name, '')
        rows.append({
            'name': name,
            'scope': scope_values.get(const, const or '缺失'),
            'marks': marks,
            'note': WRITE_ROUNDTRIP.get(name) or NOTES.get(name) or '',
        })

    unaccounted = [r for r in rows if not r['marks'] and r['name'] not in NOTES]
    silent = [r for r in rows if not r['marks'] and r['name'] in NOTES]
    missing_scope = [r for r in rows if r['scope'] == '缺失']

    print('工具总数 %d' % len(rows))
    print('  读路径验证 %d' % sum(1 for r in rows if '读' in r['marks']))
    print('  拒绝分支验证 %d' % sum(1 for r in rows if '拒绝分支' in r['marks']))
    print('  写往返验证 %d' % sum(1 for r in rows if '写往返' in r['marks']))
    print('  仅前置条件/不该自动跑 %d' % len(silent))
    for r in silent:
        print('    - %-24s %s' % (r['name'], r['note'][:60]))
    print('  未归类 %d' % len(unaccounted))
    for r in unaccounted:
        print('    ! %s' % r['name'])
    print('  scope 缺失 %d' % len(missing_scope))

    if '--write' in sys.argv:
        lines = ['# MCP 工具闭环矩阵', '',
                 '> 由 scripts/ci/mcp_closure.py --write 生成，勿手改。',
                 '> 读 = mcp_sweep.py 用真实 id 调通；拒绝分支 = 喂不存在/非法参数必须报错；',
                 '> 写往返 = 合法参数下真的改到库、复核、再还原。', '',
                 '| 工具 | scope | 验证 | 说明 |', '| --- | --- | --- | --- |']
        for r in rows:
            marks = ' / '.join(r['marks']) if r['marks'] else '—'
            lines.append('| %s | %s | %s | %s |' % ('§' + r['name'] + '§', '§' + r['scope'] + '§', marks, r['note']))
        lines += ['', '## 汇总', '', '| 维度 | 数量 |', '| --- | --- |',
                  '| 工具总数 | %d |' % len(rows),
                  '| 读路径验证 | %d |' % sum(1 for r in rows if '读' in r['marks']),
                  '| 拒绝分支验证 | %d |' % sum(1 for r in rows if '拒绝分支' in r['marks']),
                  '| 写往返验证 | %d |' % sum(1 for r in rows if '写往返' in r['marks']),
                  '| 仅前条件说明 | %d |' % len(silent),
                  '| 未归类 | %d |' % len(unaccounted), '']
        OUT_MD.write_text(chr(10).join(lines).replace('§', chr(96)), encoding='utf-8')
        print('written %s' % OUT_MD.relative_to(ROOT))

    ok = not unaccounted and not missing_scope
    print('MCP_CLOSURE=%s' % ('PASS' if ok else 'FAIL'))
    if '--strict' in sys.argv and not ok:
        sys.exit(1)

if __name__ == '__main__':
    main()
