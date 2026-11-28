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


def call_method(method, params=None):
    """非 tools/call 的 JSON-RPC 调用（tools/list 等）。"""
    _rid[0] += 1
    payload = json.dumps({'jsonrpc': '2.0', 'id': _rid[0], 'method': method,
                          'params': params or {}})
    for attempt in range(3):
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
            return json.loads(line)
        except Exception:
            time.sleep(1.2 * (attempt + 1))
    return {}

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
    d = jcall('list_scheduled_tasks', {'pageIndex': 1, 'pageSize': 1}) or {}
    ids['scheduled'] = ((d.get('list') or [{}])[0] or {}).get('id')
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


def coverage(read_list, guard_list):
    """把 tools/list 的工具清单和脚本里的 READ/GUARD 名字做差集。
    新增工具没跟进覆盖时，这里会直接点出来——否则「71 个工具全覆盖」只能靠记忆。"""
    d = call_method('tools/list')
    tools = (d.get('result') or {}).get('tools') or []
    names = sorted(t.get('name') for t in tools if t.get('name'))
    if not names:
        print('tools/list 没返回工具清单，检查 SS_MCP_KEY')
        return 2
    covered = {n for n, _ in read_list} | {n for n, _ in guard_list}
    missing = [n for n in names if n not in covered]
    print('工具总数 %d，已覆盖 %d，未覆盖 %d' % (len(names), len(names) - len(missing), len(missing)))
    for n in missing:
        print('  未覆盖:', n)

    # 注册期静默丢工具：源码里 addTool 了，但 tools/list 里没有。
    # 服务端对单个工具的 schema 非法会 recover 掉只记日志，客户端完全看不出来——
    # 2026-11-28 实测：一个 jsonschema tag 里写了 “空=全部”，list_tasks 整条工具就消失了。
    source_missing = []
    tools_go = os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', '..', 'internal', 'mcp', 'tools.go')
    if os.path.exists(tools_go):
        import re
        src = open(tools_go, encoding='utf-8', errors='ignore').read()
        declared = set(re.findall(r'addTool\(server, allowedTools, &names, &mcp\.Tool\{\s*Name:\s*"([a-z0-9_]+)"', src, re.S))
        served = set(names)
        source_missing = sorted(declared - served)
        print('源码声明 %d，服务端实际 %d' % (len(declared), len(served)))
        for n in source_missing:
            print('  声明了但没注册（schema 非法被跳过？）:', n)

    if missing or source_missing:
        print('MCP_COVERAGE=FAIL')
        return 1
    print('MCP_COVERAGE=PASS')
    return 0


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
        # 备份只读面：只列文件与状态，创建/恢复/删除不给 MCP 入口
        ('list_backups', {}),
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
        # 计划任务（周期扫描）：create_scan_task 能建，之前没有任何工具能看/关
        ('list_scheduled_tasks', {'pageIndex': 1, 'pageSize': 2}),
        ('list_wih_results', {'pageIndex': 1, 'pageSize': 2}),
        ('preview_dictionary', {'kind': 'dir', 'name': ids.get('dict') or 'x', 'limit': 1}),
        ('search_assets', {'query': 'a', 'limit': 1}),
        # 画像/节点：此前没被任何回归覆盖。
        ('list_nodes', {}),
        ('get_asset_profile', {'target': 'example.com'}),
        ('get_enterprise_profile', {'domain': 'example.com'}),
    ]
    if ids.get('task'):
        READ += [('get_task', {'id': ids['task']}), ('get_task_progress', {'id': ids['task'], 'pageIndex': 1, 'pageSize': 2})]
    if ids.get('scheduled'):
        READ += [('get_scheduled_task', {'id': ids['scheduled']})]
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
        # 归属补录：空 ids 与非法 id 必须在入参/服务层被拒（合法但不存在的 id 返回 updated=0 + note，
        # 不算失败——解除归属时「本来就是空」也会是 0，硬判错会变成误报）
        # 不支持的产出维度必须报错：静默退化成「全部」会让用户以为筛到了 nuclei
        ('list_tasks', {'pageIndex': 1, 'pageSize': 1, 'assetYield': 'has_asset', 'assetTypes': ['nuclei']}),
        ('list_tasks', {'pageIndex': 1, 'pageSize': 1, 'assetYield': 'whatever'}),
        ('assign_tasks_project', {'ids': [], 'projectId': ''}),
        ('assign_tasks_project', {'ids': ['zzz'], 'projectId': ''}),
        ('pause_task_targets', {'taskId': NOPE, 'targets': ['x.invalid']}),
        ('resume_task_targets', {'taskId': NOPE, 'targets': ['x.invalid']}),
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
        ('get_scheduled_task', {'id': NOPE}),
        # 开关只改 scheduledTasks，但 id 不存在必须报错（不能假成功返回空任务）
        ('set_scheduled_task_state', {'id': NOPE, 'enabled': False}),
        ('get_scan_template', {'id': NOPE}),
        ('get_asset_detail', {'asset_type': 'asset', 'id': NOPE}),
        ('count_assets', {'asset_type': 'nope'}),
        ('list_assets', {'asset_type': 'nope'}),
        # 字典/插件：字典不存在要报错（不能静默返回空），插件不存在要报错（不能假成功）。
        ('search_dictionary_lines', {'kind': 'port', 'name': '__nope__', 'keyword': '80'}),
        ('reinstall_plugin', {'node': '__nope__', 'hash': '__nope__', 'module': 'AssetMapping'}),
        # 9 个 create_*：只验证「畸形入参在 schema 层就被拒，handler 根本没跑」。
        # 这样既能覆盖写类工具的入参校验，又保证不会真的建出任何东西（2026-11-28）。
        ('create_asset_group', {'__invalid__': True}),
        ('create_asset_scope', {'__invalid__': True}),
        ('create_dictionary', {'__invalid__': True}),
        ('create_export_task', {'__invalid__': True}),
        ('create_icp_task', {'__invalid__': True}),
        ('create_project', {'__invalid__': True}),
        ('create_scan_policy', {'__invalid__': True}),
        ('create_scan_task', {'__invalid__': True}),
        ('create_scan_template', {'__invalid__': True}),
    ]

    if '--coverage' in sys.argv:
        return coverage(READ, GUARD)

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

    # 口径一致：list 里的 total 必须等于 count_assets 的总数。
    # 同一个事实在两处各算一遍，是最容易一处悄悄漂移的地方（REST 侧就丢过 total）。
    for atype in ('asset', 'subdomain'):
        lt = rpc('list_assets', {'asset_type': atype, 'pageIndex': 1, 'pageSize': 2})
        ct = rpc('count_assets', {'asset_type': atype})
        lv = cv = None
        if lt['kind'] == 'OK':
            try:
                lv = (json.loads(lt['text']) or {}).get('total')
            except Exception:
                lv = None
        if ct['kind'] == 'OK':
            try:
                cv = (json.loads(ct['text']) or {}).get('total')
            except Exception:
                cv = None
        if lv is None or cv is None:
            fails += 1
            print('FAIL %-28s 拿不到 total（list=%s count=%s）' % (atype, lv, cv))
        elif lv != cv:
            fails += 1
            print('FAIL %-28s list.total=%s != count_assets=%s' % (atype, lv, cv))
        else:
            print('ok   %-28s total=%s（list 与 count 一致）' % (atype, cv))

    # 过滤真的生效：数值型过滤字段最容易被「类型归一」打成静默 0 条
    # （实测曾有 filter.port:"443" → 0，而库里有 6530 条）。这里只要求「过滤后既不是全量也不是 0」，
    # 不写死具体数字，换库换环境都不会误报。
    base = rpc('count_assets', {'asset_type': 'asset'})
    base_v = None
    if base['kind'] == 'OK':
        try:
            base_v = (json.loads(base['text']) or {}).get('total')
        except Exception:
            base_v = None
    for fkey, fval in (('port', '443'), ('statuscode', '200')):
        fr = rpc('count_assets', {'asset_type': 'asset', 'filter': {fkey: [fval]}})
        fv = None
        if fr['kind'] == 'OK':
            try:
                fv = (json.loads(fr['text']) or {}).get('total')
            except Exception:
                fv = None
        if fv is None or base_v is None:
            fails += 1
            print('FAIL filter.%s 拿不到 total' % fkey)
        elif fv == 0:
            fails += 1
            print('FAIL filter.%s=%s 命中 0 条（过滤被打成静默空结果）' % (fkey, fval))
        elif fv == base_v:
            fails += 1
            print('FAIL filter.%s=%s 等于全量 %s（过滤没生效）' % (fkey, fval, fv))
        else:
            print('ok   filter.%s=%-5s total=%s / 全量 %s' % (fkey, fval, fv, base_v))

    # 产出口径（对齐 ARL-Next 1.5.2）：has_asset 必须少于全量且非 0；
    # 不支持的维度必须报错，不能静默退化成「全部」
    all_tasks = rpc('list_tasks', {'pageIndex': 1, 'pageSize': 1})
    has_tasks = rpc('list_tasks', {'pageIndex': 1, 'pageSize': 1, 'assetYield': 'has_asset'})
    all_v = has_v = None
    if all_tasks['kind'] == 'OK':
        try:
            all_v = (json.loads(all_tasks['text']) or {}).get('total')
        except Exception:
            all_v = None
    if has_tasks['kind'] == 'OK':
        try:
            has_v = (json.loads(has_tasks['text']) or {}).get('total')
        except Exception:
            has_v = None
    if all_v is None or has_v is None:
        fails += 1
        print('FAIL 产出口径 拿不到 total（all=%s has=%s）' % (all_v, has_v))
    elif has_v == 0:
        fails += 1
        print('FAIL assetYield=has_asset 命中 0 条（口径失效）')
    elif has_v >= all_v:
        fails += 1
        print('FAIL assetYield=has_asset=%s 不小于全量 %s（筛选没生效）' % (has_v, all_v))
    else:
        print('ok   assetYield=has_asset total=%s / 全量 %s' % (has_v, all_v))

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
    sys.exit(main())