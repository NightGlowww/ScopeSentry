#!/usr/bin/env python3
"""MCP 工具矩阵回归：把「每个工具到底能不能用、对不存在的 id 是否假成功」固化成可重复脚本。

用法：
  SS_BASE=http://host:8082 SS_MCP_KEY=ssk_xxx python3 scripts/ci/mcp_sweep.py

约定：
- 读类工具用真实 id（先从 list_* 里取），断言 isError=false；
- 写类工具只喂不存在的 id / 非法参数，断言 isError=true（这是防止「假成功」的核心）；
- 需要真实写库的工具（create_*）只用缺参数触发 schema 校验，不制造数据；
- 任何一项不符合预期 → 退出码 1。
"""
import json, os, subprocess, sys, time

BASE = os.environ.get('SS_BASE', 'http://127.0.0.1:8082').rstrip('/')
KEY = os.environ.get('SS_MCP_KEY', '').strip()
NOPE = '000000000000000000000000'

if not KEY:
    print('SS_MCP_KEY 未设置（需要一个全 scope 的 MCP key）')
    sys.exit(2)

_rid = [0]

def _once(name, args):
    _rid[0] += 1
    payload = json.dumps({'jsonrpc': '2.0', 'id': _rid[0], 'method': 'tools/call',
                          'params': {'name': name, 'arguments': args}})
    p = subprocess.run(['curl', '-sS', '-m', '90', '-X', 'POST', BASE + '/mcp',
                        '-H', 'Content-Type: application/json',
                        '-H', 'Accept: application/json, text/event-stream',
                        '-H', 'X-API-Key: ' + KEY, '--data-binary', payload],
                       capture_output=True, text=True)
    raw = p.stdout
    line = ''
    for ln in raw.splitlines():
        if ln.startswith('data: '):
            line = ln[6:]
    if not line:
        line = raw.strip()
    try:
        d = json.loads(line)
    except Exception:
        return {'kind': 'BADJSON', 'text': raw[:200], 'raw_len': len(raw)}
    if d.get('error'):
        return {'kind': 'RPCERR', 'text': json.dumps(d['error'])[:220]}
    res = d.get('result') or {}
    txt = ''.join(c.get('text', '') for c in (res.get('content') or []) if c.get('type') == 'text')
    return {'kind': 'ERR' if res.get('isError') else 'OK', 'text': txt}


def rpc(name, args):
    # 空响应/无法解析时重试：本地代理偶发把连接掐断（服务端日志里没有对应记录），
    # 直接把一次瞬时故障判成「工具坏了」会误报。
    last = None
    for attempt in range(3):
        r = _once(name, args)
        if r['kind'] != 'BADJSON':
            return r
        last = r
        time.sleep(1.2 * (attempt + 1))
    return last
    if d.get('error'):
        # JSON-RPC 层错误（schema 校验失败等）
        return {'kind': 'RPCERR', 'text': json.dumps(d['error'])[:220]}
    res = d.get('result') or {}
    txt = ''.join(c.get('text', '') for c in (res.get('content') or []) if c.get('type') == 'text')
    return {'kind': 'ERR' if res.get('isError') else 'OK', 'text': txt}

def jcall(name, args):
    r = rpc(name, args)
    if r['kind'] != 'OK':
        return None
    try:
        return json.loads(r['text'])
    except Exception:
        return None

def probe_ids():
    ids = {}
    d = jcall('list_tasks', {'pageIndex': 1, 'pageSize': 1}) or {}
    ids['task'] = ((d.get('list') or [{}])[0] or {}).get('id')
    d = jcall('list_pocs', {'pageIndex': 1, 'pageSize': 1}) or {}
    ids['poc'] = ((d.get('list') or [{}])[0] or {}).get('id')
    d = jcall('list_scan_templates', {'pageIndex': 1, 'pageSize': 1}) or {}
    ids['template'] = ((d.get('list') or [{}])[0] or {}).get('id')
    d = jcall('list_dictionaries', {'kind': 'dir', 'pageIndex': 1, 'pageSize': 1}) or {}
    ids['dict'] = ((d.get('list') or [{}])[0] or {}).get('name')
    d = jcall('list_nodes', {}) or {}
    ids['node'] = ((d.get('list') or [{}])[0] or {}).get('name')
    d = jcall('list_projects_data', {'pageIndex': 1, 'pageSize': 1}) or {}
    proj = None
    res = d.get('result') if isinstance(d, dict) else None
    if isinstance(res, dict):
        for _, rows in res.items():
            if rows:
                proj = rows[0].get('id')
                break
    ids['project'] = proj
    ids['asset'] = os.environ.get('SS_ASSET_ID', '')
    return ids

def rows_ok(expect_json=True):
    return True


def main():
    ids = probe_ids()
    print('ids:', json.dumps({k: v for k, v in ids.items() if v}, ensure_ascii=False))

    # (工具名, 参数, 期望)  期望: ok / err / rpc_err
    READ = [
        ('count_assets', {'asset_type': 'asset'}),
        ('get_console_info', {}),
        ('get_dashboard_stats', {}),
        ('get_export_fields', {'index': 'asset'}),
        ('get_system_info', {}),
        ('intel_for_assets', {'limit': 1}),
        ('list_asset_groups', {}),
        ('list_asset_scopes', {'pageIndex': 1, 'pageSize': 2}),
        ('list_assets', {'asset_type': 'asset', 'pageIndex': 1, 'pageSize': 2}),
        ('list_audit_logs', {'pageIndex': 1, 'pageSize': 2}),
        ('list_dictionaries', {'kind': 'dir', 'pageIndex': 1, 'pageSize': 2}),
        ('list_export_records', {}),
        ('list_fingerprints', {'pageIndex': 1, 'pageSize': 2}),
        ('list_github_results', {'pageIndex': 1, 'pageSize': 2}),
        ('list_icp_tasks', {'pageIndex': 1, 'pageSize': 2}),
        ('list_nuclei_results', {'pageIndex': 1, 'pageSize': 2}),
        ('list_plugin_modules', {}),
        ('list_plugins', {}),
        ('list_pocs', {'pageIndex': 1, 'pageSize': 2}),
        ('list_port_dict', {'pageIndex': 1, 'pageSize': 2}),
        ('list_projects', {}),
        ('list_projects_data', {'pageIndex': 1, 'pageSize': 2}),
        ('list_scan_policies', {'pageIndex': 1, 'pageSize': 2}),
        ('list_scan_templates', {'pageIndex': 1, 'pageSize': 2}),
        ('list_sensitive_rules', {'pageIndex': 1, 'pageSize': 2}),
        ('list_stat_finger', {'pageIndex': 1, 'pageSize': 2}),
        ('list_tasks', {'pageIndex': 1, 'pageSize': 2}),
        ('list_wih_results', {'pageIndex': 1, 'pageSize': 2}),
        ('preview_dictionary', {'kind': 'dir', 'name': ids.get('dict') or 'x', 'limit': 1}),
        ('search_assets', {'query': 'a', 'limit': 1}),
    ]
    if ids.get('task'):
        READ += [('get_task', {'id': ids['task']}), ('get_task_progress', {'id': ids['task'], 'pageIndex': 1, 'pageSize': 2})]
    if ids.get('poc'):
        READ += [('get_poc', {'id': ids['poc']})]
    if ids.get('template'):
        READ += [('get_scan_template', {'id': ids['template']})]
    if ids.get('node'):
        READ += [('get_node_logs', {'node': ids['node'], 'tailKB': 1}), ('list_node_plugins', {'node': ids['node']})]
    if ids.get('project'):
        READ += [('get_project', {'id': ids['project']})]
    if ids.get('asset'):
        READ += [('get_asset_detail', {'asset_type': 'asset', 'id': ids['asset']})]

    GUARD = [
        ('add_asset_tag', {'asset_type': 'asset', 'id': NOPE, 'tag': 'x'}),
        ('append_dictionary_lines', {'kind': 'dir', 'name': 'no-such-dict-sweep', 'lines': ['x']}),
        ('delete_asset_groups', {'ids': [NOPE]}),
        ('delete_dictionary', {'kind': 'dir', 'name': 'no-such-dict-sweep'}),
        ('delete_dictionary_entries', {'kind': 'dir', 'name': 'no-such-dict-sweep', 'entries': ['x']}),
        ('delete_export_files', {'fileNames': ['no-such-file-sweep.csv']}),
        ('delete_nodes', {'names': ['no-such-node-sweep']}),
        ('delete_port_dict_entries', {'ids': [NOPE], 'confirm': True}),
        ('delete_scan_policies', {'ids': [NOPE]}),
        ('delete_task', {'ids': [NOPE]}),
        ('export_task_workbook', {'taskId': NOPE, 'format': 'xlsx'}),
        ('recheck_plugin', {'node': 'no-such-node-sweep', 'hash': 'abc', 'module': 'x'}),
        ('restart_node', {'node': 'no-such-node-sweep'}),
        ('set_port_dict_entry', {'name': '', 'value': '80'}),
        ('set_sensitive_rule_state', {'ids': [NOPE], 'state': True}),
        ('start_task', {'id': NOPE}),
        ('stop_icp_tasks', {'ids': [NOPE]}),
        ('stop_task', {'id': NOPE}),
        ('uninstall_plugin', {'node': 'no-such-node-sweep', 'hash': 'abc', 'module': 'x'}),
        ('get_poc', {'id': NOPE}),
        ('get_project', {'id': NOPE}),
        ('get_task', {'id': NOPE}),
        ('get_scan_template', {'id': NOPE}),
        ('get_asset_detail', {'asset_type': 'asset', 'id': NOPE}),
        ('count_assets', {'asset_type': 'nope'}),
        ('list_assets', {'asset_type': 'nope'}),
    ]

    fails = 0
    for name, args in READ:
        r = rpc(name, args)
        if r['kind'] != 'OK':
            fails += 1
            print('FAIL %-28s 期望 ok，实际 %s: %s' % (name, r['kind'], r['text'][:120].replace('\n', ' ')))
        else:
            print('ok   %-28s %d bytes' % (name, len(r['text'])))
    for name, args in GUARD:
        r = rpc(name, args)
        if r['kind'] == 'OK':
            fails += 1
            print('FAIL %-28s 期望报错，实际假成功: %s' % (name, r['text'][:120].replace('\n', ' ')))
        elif r['kind'] == 'BADJSON':
            fails += 1
            print('FAIL %-28s 响应无法解析（可能是进程崩溃）' % name)
        else:
            print('ok   %-28s %s' % (name, r['kind']))

    # 分页夹紧：一次大 pageSize 不能把库拖出来
    big = rpc('list_assets', {'asset_type': 'asset', 'pageIndex': 1, 'pageSize': 100000})
    rows = 0
    if big['kind'] == 'OK':
        try:
            rows = len((json.loads(big['text']) or {}).get('list') or [])
        except Exception:
            rows = -1
    if rows < 0 or rows > 500:
        fails += 1
        print('FAIL pageSize 未收敛：rows=%s' % rows)
    else:
        print('ok   pageSize=100000 -> rows=%d' % rows)

    print()
    if fails:
        print('MCP_SWEEP=FAIL (%d)' % fails)
        sys.exit(1)
    print('MCP_SWEEP=PASS (%d read + %d guard)' % (len(READ), len(GUARD)))


if __name__ == '__main__':
    main()