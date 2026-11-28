#!/usr/bin/env python3
"""资产过滤器矩阵体检：每个索引 × 每个文档化的 filter 键，真的能筛出东西吗？

起因（2026-11-29）：IP 资产的 filter.port 一直返回 0 —— 文档里 port 是 ports[] 的嵌套字段，
过滤器按顶级字段查，条件永不命中。接口回 200 + 空列表，没有任何地方提示「这个键没生效」。
同类问题还有 POC 搜索只匹配 name、仪表盘传项目名回全 0。共同点：**读路径静默给错答案**。

做法：先不筛拿一批行，从真实行里取一个非空值当过滤条件，再查一次；筛不出来就说明这个键坏了。
注意每个索引有自己的列表端点（asset/subdomain/url/sensitive/dirscan/vulnerability/ip）——
用统一端点去查会得到 0，那是脚本自己的 bug（第一版就踩了）。
用法：SS_BASE=http://host:8082 SS_TOKEN=<JWT> python3 scripts/ci/filter_matrix.py
"""
import json
import os
import subprocess
import sys

BASE = os.environ.get('SS_BASE', 'http://127.0.0.1:8082').rstrip('/')
TOKEN = os.environ.get('SS_TOKEN', '').strip()
if not TOKEN:
    print('SS_TOKEN 未设置')
    sys.exit(2)

PATH = {
    'asset': '/api/assets/asset',
    'subdomain': '/api/assets/subdomain',
    'UrlScan': '/api/assets/url',
    'SensitiveResult': '/api/assets/sensitive',
    'DirScanResult': '/api/assets/dirscan',
    'vulnerability': '/api/assets/vulnerability',
    'IPAsset': '/api/assets/ip',
}

# index -> [(filter 键, 行里对应的字段)]
MATRIX = {
    'asset': [('port', 'port'), ('service', 'service'), ('app', 'products'),
              ('statuscode', 'status'), ('type', 'type')],
    'subdomain': [('type', 'type')],
    'UrlScan': [('statuscode', 'status')],
    'SensitiveResult': [('status', 'status'), ('sname', 'name')],
    'DirScanResult': [('status', 'status')],
    'vulnerability': [('level', 'level'), ('status', 'status')],
    'IPAsset': [('port', 'port'), ('service', 'service'), ('app', 'products')],
}

FAILS = []
CHECKS = [0]


def post(path, body):
    p = subprocess.run(['curl', '-sS', '-m', '90', '-X', 'POST', BASE + path,
                        '-H', 'Content-Type: application/json',
                        '-H', 'Authorization: Bearer ' + TOKEN,
                        '--data-binary', json.dumps(body)], capture_output=True, text=True)
    try:
        return json.loads(p.stdout)
    except Exception:
        return {'code': -1, 'raw': p.stdout[:200]}


def fetch(index, page_size=50):
    d = post(PATH[index], {'pageIndex': 1, 'pageSize': page_size})
    data = (d or {}).get('data') or {}
    return data.get('total'), data.get('list') or []


def pick_value(rows, field):
    for r in rows:
        v = r.get(field)
        if isinstance(v, list):
            v = v[0] if v else ''
        if v is None:
            continue
        s = str(v).strip()
        if s and s not in ('0', 'None'):
            return s
    return ''


def check(index, key, field):
    CHECKS[0] += 1
    total, rows = fetch(index)
    if not rows:
        print('  skip %-16s %-12s 没有数据可取样' % (index, key))
        return
    val = pick_value(rows, field)
    if not val:
        print('  skip %-16s %-12s 取样的行里没有非空的 %s' % (index, key, field))
        return
    d = post(PATH[index], {'pageIndex': 1, 'pageSize': 3, 'filter': {key: [val]}})
    got = ((d or {}).get('data') or {}).get('total')
    ok = isinstance(got, int) and got > 0
    print('  %s %-16s %-12s=%-22s 命中 %s（全量 %s）' % (
        'ok  ' if ok else 'FAIL', index, key, val[:20], got, total))
    if not ok:
        FAILS.append('%s.%s=%s' % (index, key, val))


def main():
    print('== 资产过滤器矩阵 ==')
    for index, keys in MATRIX.items():
        for key, field in keys:
            check(index, key, field)
    print()
    if FAILS:
        print('FILTER_MATRIX=FAIL %d/%d：%s' % (len(FAILS), CHECKS[0], ', '.join(FAILS[:8])))
        return 1
    print('FILTER_MATRIX=PASS (%d checks)' % CHECKS[0])
    return 0


if __name__ == '__main__':
    sys.exit(main())
