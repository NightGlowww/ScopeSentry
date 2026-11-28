#!/usr/bin/env python3
"""写接口幽灵 id 体检：按 id 改/删的接口，给一个不存在的 id 必须报 4xx。

起因（2026-11-29）：scheduled/update 收到不存在的 id 回 **500「操作失败」**，
把「你给的 id 不存在」说成「服务器挂了」；模板更新、通知配置更糟，直接回 200 假装成功。
共同点还是那个：UpdateOne 匹配不到文档返回 nil。

做法：对每条按 id 改/删的接口发一个幽灵 ObjectID，断言不是 2xx（配置类 upsert 单独放行）。
用法：SS_BASE=http://host:8082 SS_TOKEN=<JWT> python3 scripts/ci/write_ghost_probe.py
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

GHOST = '6b0b0f2723c73ac5ad1c3999'

# (名字, 方法, 路径, body)  —— 全部按 id 改/删实体
PROBES = [
    ('asset_group/edit', 'POST', '/api/assets/asset_group/edit', {'id': GHOST, 'name': 'ghost'}),
    ('asset_scope/update', 'POST', '/api/assets/asset_scope/update', {'id': GHOST, 'name': 'ghost'}),
    ('asset_scope/bind_enterprise', 'POST', '/api/assets/asset_scope/bind_enterprise', {'id': GHOST, 'enterprise': 'x'}),
    ('asset_scope/delete', 'POST', '/api/assets/asset_scope/delete', {'ids': [GHOST]}),
    ('task/stop', 'POST', '/api/task/stop', {'ids': [GHOST]}),
    ('task/delete', 'POST', '/api/task/delete', {'ids': [GHOST]}),
    ('scheduled/update', 'POST', '/api/task/scheduled/update',
     {'id': GHOST, 'name': 'ghost', 'node': ['n'], 'cycleType': 'daily', 'hour': 1, 'minute': 0}),
    ('scheduled/delete', 'POST', '/api/task/scheduled/delete', {'ids': [GHOST]}),
    ('poc/update', 'POST', '/api/poc/update',
     {'id': GHOST, 'content': 'id: x\ninfo:\n  name: x\n  author: y\n  severity: info\n'}),
    ('poc/delete', 'POST', '/api/poc/delete', {'ids': [GHOST]}),
    ('sensitive/update', 'POST', '/api/sensitive/update',
     {'id': GHOST, 'name': 'ghost', 'regular': 'x', 'color': 'red', 'state': True}),
    ('notification/update', 'POST', '/api/configuration/notification/update',
     {'id': GHOST, 'name': 'ghost', 'method': 'POST', 'url': 'http://127.0.0.1:9/x',
      'contentType': 'application/json', 'data': '{}', 'state': False}),
    ('template/save', 'POST', '/api/task/template/save',
     {'id': GHOST, 'result': {'TaskName': 'ghost', 'TargetHandler': ['x'], 'SubdomainScan': ['1'],
                              'SubdomainSecurity': ['1'], 'PortScanPreparation': ['1'], 'PortScan': ['1'],
                              'PortFingerprint': ['1'], 'AssetMapping': ['1'], 'AssetHandle': ['1'],
                              'URLScan': ['1'], 'WebCrawler': ['1'], 'URLSecurity': ['1'],
                              'DirScan': ['920546788addc6d29ea63e4a314a1b85']}}),
    ('dictionary/port/delete', 'POST', '/api/dictionary/port/delete', {'ids': [GHOST]}),
]

FAILS = []


def probe(name, method, path, body):
    cmd = ['curl', '-sS', '-m', '60', '-X', method, BASE + path,
           '-H', 'Content-Type: application/json',
           '-H', 'Authorization: Bearer ' + TOKEN,
           '--data-binary', json.dumps(body),
           '-o', '/dev/null', '-w', '%{http_code}']
    p = subprocess.run(cmd, capture_output=True, text=True)
    code = p.stdout.strip()
    ok = code[:1] in ('4',)
    print('  %s %-32s %s' % ('ok  ' if ok else 'FAIL', name, code))
    if not ok:
        FAILS.append('%s=%s' % (name, code))


def main():
    print('== 写接口幽灵 id 体检 ==')
    for name, method, path, body in PROBES:
        probe(name, method, path, body)
    print()
    if FAILS:
        print('WRITE_GHOST=FAIL %d/%d：%s' % (len(FAILS), len(PROBES), ', '.join(FAILS)))
        return 1
    print('WRITE_GHOST=PASS (%d probes)' % len(PROBES))
    return 0


if __name__ == '__main__':
    sys.exit(main())
