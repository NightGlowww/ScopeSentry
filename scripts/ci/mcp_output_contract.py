#!/usr/bin/env python3
"""MCP 输出契约审计：把「工具返回了东西」升级成「返回的东西和库里的真相一致」。

为什么单独做一层（docs/audit/MCP_GAPS.md §10 遗留项）：
  mcp_sweep.py 只验「读类工具不报错 / 写类工具对坏参数报错」，
  它拦不住「total 说有 4935 条，但 rows 是空的」或「filter 只影响 total 不影响 rows」
  这类假成功——而那正是自动化最容易被骗的地方。

用法：
  SS_BASE=http://host:8082 SS_MCP_KEY=ssk_xxx python3 scripts/ci/mcp_output_contract.py

检查项（每条都对应一次真实的踩坑）：
  1. 顶层是 JSON 对象，不是纯文本
  2. list_* 的 total 是整数，rows == min(pageSize, total)
  3. filter 必须同时作用于 rows（不只 total）
  4. count_assets 与 list_assets.total 同口径
  5. asset_distribution 的分桶数 / 维度回显 / 截断标记
  6. search_assets 返回的每条都真的含关键词
  7. get_asset_profile 回显 target，计数不小于样本数
  8. get_* 回显请求的 id（不能拿错文档当成功）
  9. list_nodes 的字段契约（含本轮新增的运行态字段）
 10. 分页边界：pageIndex 越界返回空列表而不是报错或复读第一页
"""
import json
import os
import subprocess
import sys

BASE = os.environ.get('SS_BASE', 'http://127.0.0.1:8082').rstrip('/')
KEY = os.environ.get('SS_MCP_KEY', '').strip()
if not KEY:
    print('SS_MCP_KEY 未设置（需要一个全 scope 的 MCP key）')
    sys.exit(2)

_rid = [0]
FAILS = []
CHECKS = [0]


def call(name, args):
    _rid[0] += 1
    payload = json.dumps({'jsonrpc': '2.0', 'id': _rid[0], 'method': 'tools/call',
                          'params': {'name': name, 'arguments': args}})
    raw = ''
    for _ in range(3):
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
        line = line or raw.strip()
        try:
            d = json.loads(line)
        except Exception:
            continue
        res = d.get('result') or {}
        txt = ''.join(c.get('text', '') for c in (res.get('content') or []) if c.get('type') == 'text')
        if res.get('isError'):
            return None, txt[:200]
        try:
            return json.loads(txt), None
        except Exception:
            return None, 'NONJSON: ' + txt[:160]
    return None, 'BADJSON: ' + raw[:160]


def ok(cond, what, detail=''):
    CHECKS[0] += 1
    if cond:
        print('  ok   ' + what)
        return True
    msg = what + (('  → ' + detail) if detail else '')
    print('  FAIL ' + msg)
    FAILS.append(msg)
    return False


def main():
    print('== MCP 输出契约 ==')

    # 2/3/4: 资产列表 / 计数 / 过滤
    total_all = None
    d, err = call('list_assets', {'asset_type': 'asset', 'pageIndex': 1, 'pageSize': 5})
    if ok(d is not None and isinstance(d, dict), 'list_assets 返回 JSON 对象', err or ''):
        rows = d.get('list') or []
        total_all = d.get('total')
        ok(isinstance(total_all, int), 'list_assets.total 是整数', repr(total_all))
        ok(len(rows) == min(5, total_all or 0), 'list_assets rows 与 total 自洽',
           'rows=%d total=%s' % (len(rows), total_all))
        ok(all('domain' in r and 'port' in r for r in rows), 'list_assets 每行有 domain/port',
           json.dumps(rows[0])[:120] if rows else 'no rows')
        # 别名：filter 键叫 statuscode、行字段叫 status；search_assets 叫 host、list_assets 叫 domain。
        # 不给别名，自动化就没法「按过滤键验证行」或跨工具合并结果（2026-11-29 修）。
        ok(all('host' in r for r in rows), 'list_assets 行带 host 别名（与 search_assets 一致）',
           json.dumps(rows[0])[:120] if rows else 'no rows')

    d2, err = call('count_assets', {'asset_type': 'asset'})
    if ok(d2 is not None, 'count_assets 返回 JSON', err or ''):
        ok(d2.get('total') == total_all, 'count_assets 与 list_assets.total 同口径',
           'count=%s list=%s' % (d2.get('total'), total_all))

    # filter 必须同时作用于 rows：历史上出现过 total 变了、rows 没变
    d3, err = call('list_assets', {'asset_type': 'asset', 'pageIndex': 1, 'pageSize': 20,
                                   'filter': {'port': ['443']}})
    if ok(d3 is not None, 'list_assets(filter.port) 返回 JSON', err or ''):
        rows = d3.get('list') or []
        bad = [r for r in rows if str(r.get('port')) != '443']
        ok(not bad, 'filter.port=443 作用于 rows',
           'first bad: ' + json.dumps(bad[0])[:120] if bad else '')
        ok((d3.get('total') or 0) <= (total_all or 0), 'filter 后 total 不大于全量',
           'filtered=%s all=%s' % (d3.get('total'), total_all))

    # statuscode 是 filter 键，行里叫 status：两者必须说的是同一件事
    d4, err = call('list_assets', {'asset_type': 'asset', 'pageIndex': 1, 'pageSize': 20,
                                   'filter': {'statuscode': ['200']}})
    if ok(d4 is not None, 'list_assets(filter.statuscode) 返回 JSON', err or ''):
        rows = d4.get('list') or []
        codes = set(str(r.get('status')) for r in rows)
        ok(codes <= {'200'}, 'filter.statuscode=200 的行 status 都是 200',
           'codes=' + ','.join(sorted(codes))[:80])
        ok(all('statuscode' in r for r in rows), '行带 statuscode 别名（与 filter 键一致）',
           json.dumps(rows[0])[:120] if rows else 'no rows')

    # 5: 分布
    d5, err = call('asset_distribution', {'dimension': 'port', 'limit': 5})
    if ok(d5 is not None, 'asset_distribution 返回 JSON', err or ''):
        buckets = d5.get('list') or []
        ok(d5.get('dimension') in (None, 'port'), 'dimension 回显正确', repr(d5.get('dimension')))
        ok(0 < len(buckets) <= 5, '返回条数不超过 limit', 'len=%d' % len(buckets))
        ok(all('value' in b and 'count' in b for b in buckets), '每个分桶有 value/count',
           json.dumps(buckets[0])[:120] if buckets else 'no buckets')

    # 6: 搜索不能把不相关的塞进来
    d6, err = call('search_assets', {'query': 'example', 'limit': 10})
    if ok(d6 is not None, 'search_assets 返回 JSON', err or ''):
        hits = d6.get('assets') or []
        bad = [x for x in hits if 'example' not in json.dumps(x).lower()]
        ok(not bad, 'search_assets 每条都含关键词',
           'first bad: ' + json.dumps(bad[0])[:120] if bad else '')
        ok(d6.get('assetsCount') is None or isinstance(d6.get('assetsCount'), int),
           'assetsCount 是整数或省略', repr(d6.get('assetsCount')))

    # 7: 画像
    d7, err = call('get_asset_profile', {'target': 'example.com', 'limit': 3})
    if ok(d7 is not None, 'get_asset_profile 返回 JSON', err or ''):
        ok(d7.get('target') == 'example.com', 'target 原样回显', repr(d7.get('target')))
        for key, cnt in (('assets', 'assetCount'), ('subdomains', 'subdomainCount')):
            sample = d7.get(key) or []
            if sample:
                ok((d7.get(cnt) or 0) >= len(sample), '%s 不小于样本数' % cnt,
                   '%s=%s sample=%d' % (cnt, d7.get(cnt), len(sample)))

    # 8: get_* 回显 id
    d8, err = call('list_pocs', {'pageIndex': 1, 'pageSize': 1})
    if d8 and (d8.get('list') or []):
        poc_id = d8['list'][0].get('id')
        d9, err9 = call('get_poc', {'id': poc_id})
        if ok(d9 is not None, 'get_poc 返回 JSON', err9 or ''):
            ok(d9.get('id') == poc_id, 'get_poc 回显请求的 id',
               'want=%s got=%s' % (poc_id, d9.get('id')))

    # 9: 节点字段契约（本轮新增运行态字段，缺了等于界面白改）
    d10, err = call('list_nodes', {})
    if ok(d10 is not None, 'list_nodes 返回 JSON', err or ''):
        nodes = d10.get('list') or []
        ok(all('name' in n and 'state' in n for n in nodes), '每个节点有 name/state')
        if nodes:
            n0 = nodes[0]
            ok('queueLen' in n0, '节点带 queueLen', json.dumps(n0)[:120])
            ok('idleMinutes' in n0 or not n0.get('currentTaskId'),
               '有在跑任务时必须给 idleMinutes', json.dumps(n0)[:160])

    # 10: 分页边界
    d11, err = call('list_assets', {'asset_type': 'asset', 'pageIndex': 99999, 'pageSize': 5})
    if ok(d11 is not None, '越界 pageIndex 不报错', err or ''):
        ok((d11.get('list') or []) == [], '越界返回空列表',
           'rows=%d' % len(d11.get('list') or []))

    print()
    if FAILS:
        print('MCP_OUTPUT=FAIL (%d/%d)' % (len(FAILS), CHECKS[0]))
        return 1
    print('MCP_OUTPUT=PASS (%d checks)' % CHECKS[0])
    return 0


if __name__ == '__main__':
    sys.exit(main())
