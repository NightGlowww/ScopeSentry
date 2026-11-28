
#!/usr/bin/env python3
"""REST 接口引用索引：314 条路由里，哪些被测试/契约脚本真正点名过。

**这是索引，不是覆盖率裁决。** 两处天然低估：
  1. 路径可能是拼出来的（脚本只做字面量 + 前缀匹配）；
  2. 这个项目 POST 大量用于「查列表」，光看 method 分不出读写，所以读写按名字启发式打标。
用途：改动前先看「这条接口以前有没有人验过」，以及找「写操作里完全没人碰」的高风险面。

用法：
  python3 scripts/ci/rest_closure.py
  python3 scripts/ci/rest_closure.py --write
"""
import pathlib
import re
import sys
from collections import Counter

ROOT = pathlib.Path(__file__).resolve().parents[2]
SNAPSHOT = ROOT / 'docs/audit/routes.snapshot.tsv'
OUT_MD = ROOT / 'docs/audit/REST_CLOSURE.md'
BT = chr(96)

READ_HINTS = ('/list', '/detail', '/data', '/stats', '/statistics', '/info', '/count', '/number',
              '/names', '/body', '/hosts', '/card', '/changelog', '/diff', '/search', '/export',
              '/preview', '/trend', '/logs', '/sysinfo', '/widgets', '/query', '/tree', '/overview',
              '/report', '/online', '/all', '/read', '/download', '/chain', '/domains', '/ports',
              '/config', '/source', '/records', '/fields', '/items', '/values', '/provinces',
              '/structure', '/templates', '/fingers', '/dictionaries', '/lines', '/plugins')
WRITE_HINTS = ('/add', '/edit', '/update', '/save', '/set', '/create', '/delete', '/import',
               '/restart', '/stop', '/resume', '/pause', '/bind', '/clear', '/reorder', '/apply',
               '/run', '/trigger', '/upload', '/sync', '/assign', '/dissolve', '/transfer',
               '/tag', '/toggle', '/status', '/backfill', '/seed', '/slim', '/restore')

def load_routes():
    out = []
    for line in SNAPSHOT.read_text(encoding='utf-8').splitlines():
        if not line.strip():
            continue
        method, path = line.split('\t')
        out.append((method.strip(), path.strip()))
    return out

def classify(path):
    if any(h in path for h in WRITE_HINTS):
        return 'write'
    if any(h in path for h in READ_HINTS):
        return 'read'
    return 'unknown'

def to_regex(path):
    return re.compile('^' + re.sub(r':[A-Za-z_]+', '[^/]+', path) + '$')

def referenced():
    sources = list((ROOT / 'internal/api/routes').glob('*_test.go'))
    sources += [ROOT / 'scripts/ci/verify_contract.sh', ROOT / 'scripts/ci/smoke.sh',
                ROOT / 'scripts/ci/mcp_sweep.py', ROOT / 'scripts/ci/integration.sh']
    lits = set()
    pat = re.compile(r'(/api/[A-Za-z0-9_./-]+)')
    for f in sources:
        if not f.exists():
            continue
        for m in pat.finditer(f.read_text(encoding='utf-8', errors='replace')):
            lits.add(m.group(1).rstrip('/'))
    return lits

def main():
    rows = []
    lits = referenced()
    for method, path in load_routes():
        rx = to_regex(path)
        hit = path in lits or any(rx.match(l) for l in lits)
        rows.append((method, path, classify(path), hit))

    total = len(rows)
    cov = sum(1 for r in rows if r[3])
    by_kind = Counter(); cov_kind = Counter()
    for m, p, k, h in rows:
        by_kind[k] += 1
        if h:
            cov_kind[k] += 1

    print('路由 %d（快照 %s）' % (total, SNAPSHOT.name))
    print('被点名 %d（%.0f%%）' % (cov, cov * 100.0 / total))
    for k in ('read', 'write', 'unknown'):
        if by_kind[k]:
            print('  %-7s %3d / %3d' % (k, cov_kind[k], by_kind[k]))

    mod_total = Counter(); mod_cov = Counter()
    for m, p, k, h in rows:
        seg = p.split('/')
        mod = seg[2] if len(seg) > 2 else 'root'
        mod_total[mod] += 1
        if h:
            mod_cov[mod] += 1

    unref_write = sorted((m, p) for m, p, k, h in rows if k == 'write' and not h)
    print('写操作没被点名 %d 条' % len(unref_write))
    for m, p in unref_write[:15]:
        print('  %-6s %s' % (m, p))

    if '--write' in sys.argv:
        lines = ['# REST 接口引用索引', '',
                 '> 由 scripts/ci/rest_closure.py --write 生成，勿手改。',
                 '> 路由快照来自 SS_ROUTE_DUMP=1 跑 TestSetupRouterInventory（需要真实 Mongo/Redis）。', '',
                 '**这是索引，不是覆盖率裁决。** 两处天然低估：路径可能是拼出来的（脚本按字面量 + 前缀匹配）；',
                 '这个项目 POST 大量用于查列表，光看 method 分不出读写，所以读写分类是名字启发式。', '',
                 '| 维度 | 数量 |', '| --- | --- |',
                 '| 路由总数 | %d |' % total,
                 '| 被测试/契约脚本点名 | %d（%.0f%%） |' % (cov, cov * 100.0 / total),
                 '| 读操作 点名/总数 | %d / %d |' % (cov_kind['read'], by_kind['read']),
                 '| 写操作 点名/总数 | %d / %d |' % (cov_kind['write'], by_kind['write']),
                 '| 无法判读 点名/总数 | %d / %d |' % (cov_kind['unknown'], by_kind['unknown']),
                 '', '## 按模块（未点名 / 总数）', '', '| 模块 | 未点名 | 总数 |', '| --- | --- |']
        for mod in sorted(mod_total, key=lambda x: mod_total[x] - mod_cov[x], reverse=True):
            lines.append('| %s | %d | %d |' % (mod, mod_total[mod] - mod_cov[mod], mod_total[mod]))
        lines += ['', '## 写操作里完全没被点名的（优先补测试）', '', '| 方法 | 路径 |', '| --- | --- |']
        for m, p in unref_write:
            lines.append('| %s | %s |' % (m, BT + p + BT))
        OUT_MD.write_text(chr(10).join(lines) + chr(10), encoding='utf-8')
        print('written %s' % OUT_MD.relative_to(ROOT))

if __name__ == '__main__':
    main()
