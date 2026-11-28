
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
import json
import pathlib
import re
import sys
from collections import Counter

ROOT = pathlib.Path(__file__).resolve().parents[2]
SNAPSHOT = ROOT / 'docs/audit/routes.snapshot.tsv'
OUT_MD = ROOT / 'docs/audit/REST_CLOSURE.md'
EXCUSES_FILE = ROOT / 'scripts/ci/closure_excuses.json'


def load_excuses():
    """已登记的未覆盖写操作。

    这些端点不是「忘了写测试」，而是有明确原因没写（要外部网络、要插件密钥、
    或者一执行就会动真实数据）。把原因写在文件里，比放一个'预算 10'的数字强：
    数字只会被慢慢调大，一条一条的理由会被读到。
    """
    if not EXCUSES_FILE.exists():
        return {}
    try:
        raw = json.loads(EXCUSES_FILE.read_text(encoding='utf-8'))
    except Exception as exc:
        print('CLOSURE_EXCUSES=FAIL 解析 %s 失败: %s' % (EXCUSES_FILE.name, exc))
        return {}
    return raw.get('excuses', {})
BT = chr(96)

READ_HINTS = ('/list', '/detail', '/data', '/stats', '/statistics', '/info', '/count', '/number',
              '/names', '/body', '/hosts', '/card', '/changelog', '/diff', '/search', '/export',
              '/preview', '/trend', '/logs', '/sysinfo', '/widgets', '/query', '/tree', '/overview',
              '/report', '/online', '/all', '/read', '/download', '/chain', '/domains', '/ports',
              '/config', '/source', '/records', '/fields', '/items', '/values', '/provinces',
              '/structure', '/templates', '/fingers', '/dictionaries', '/lines', '/plugins')
WRITE_HINTS = ('/add', '/edit', '/update', '/save', '/set', '/create', '/delete', '/import',
               '/restart', '/stop', '/resume', '/pause', '/bind', '/clear', '/reorder', '/apply',
               '/run', '/trigger', '/upload', '/sync', '/assign', '/dissolve', '/transfer', '/state',
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


def composed_hit(path, lits):
    """测试里用字符串拼接的路径（"/api/mcp/keys/" + id + "/status"）也算引用。

    字面量扫描会在拼接处截断，截断结果正好是路由在第一个参数之前的前缀。
    为了不把覆盖数吹大，只认一种形态：截断处后面紧接着就是参数段
    （'/api/mcp/keys' → '/api/mcp/keys/:id/status' 算；
     '/api/task' → '/api/task/delete' 不算，因为后面第一段不是参数）。
    """
    for lit in lits:
        if len(lit) <= 10 or not path.startswith(lit + '/'):
            continue
        rest = path[len(lit) + 1:].split('/')
        if rest and rest[0].startswith(':'):
            return True
    return False

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

def ui_referenced():
    """前端真的会调的接口。

    单独统计的原因：这个脚本原来只数「测试/契约脚本点名」，读起来像覆盖率，
    实际只反映验证面。资产组 edit/reorder 就是这样——被误判成「没人用」，
    其实 UI 早就在调（frontend/src/api/assetGroup/index.ts）。两个维度分开看，
    才知道一条路由到底缺什么：缺测试、缺 UI，还是两边都缺（多半是死路由）。
    """
    lits = set()
    pat = re.compile(r'(/api/[A-Za-z0-9_./-]+)')
    for ext in ('ts', 'tsx', 'vue', 'js'):
        for f in (ROOT / 'frontend/src').rglob('*.' + ext):
            try:
                text = f.read_text(encoding='utf-8', errors='replace')
            except Exception:
                continue
            for m in pat.finditer(text):
                lits.add(m.group(1).rstrip('/'))
    return lits


def main():
    rows = []
    lits = referenced()
    ui_lits = ui_referenced()
    for method, path in load_routes():
        rx = to_regex(path)
        hit = path in lits or any(rx.match(l) for l in lits) or composed_hit(path, lits)
        # 前端常用模板串拼路径（/api/mcp/keys/${id}/status），字面量扫描会在 $ 处截断；
        # 截断后正好是路由前缀，所以前缀命中也算引用。第一版只做精确匹配，把这条误判成死路由。
        ui = (path in ui_lits or any(rx.match(l) for l in ui_lits)
              or any(path.startswith(l + '/') for l in ui_lits if len(l) > 10))
        rows.append((method, path, classify(path), hit, ui))

    total = len(rows)
    cov = sum(1 for r in rows if r[3])
    covered_ui = sum(1 for r in rows if r[4])
    neither = [r for r in rows if not r[3] and not r[4] and r[2] == 'write']
    by_kind = Counter(); cov_kind = Counter()
    for m, p, k, h, ui in rows:
        by_kind[k] += 1
        if h:
            cov_kind[k] += 1

    print('路由 %d（快照 %s）' % (total, SNAPSHOT.name))
    print('被点名 %d（%.0f%%）' % (cov, cov * 100.0 / total))
    for k in ('read', 'write', 'unknown'):
        if by_kind[k]:
            print('  %-7s %3d / %3d' % (k, cov_kind[k], by_kind[k]))

    print('UI 引用 %d（%.0f%%）：测试与 UI 都没碰的写操作 %d 条' % (
        covered_ui, covered_ui * 100.0 / total, len(neither)))

    mod_total = Counter(); mod_cov = Counter()
    for m, p, k, h, ui in rows:
        seg = p.split('/')
        mod = seg[2] if len(seg) > 2 else 'root'
        mod_total[mod] += 1
        if h:
            mod_cov[mod] += 1

    unref_write = sorted((m, p) for m, p, k, h, ui in rows if k == 'write' and not h)
    dead_write = sorted((m, p) for m, p, k, h, ui in rows if k == 'write' and not h and not ui)
    excuses = load_excuses()
    excused = [(m, p) for m, p in unref_write if '%s %s' % (m, p) in excuses]
    unexcused = [(m, p) for m, p in unref_write if '%s %s' % (m, p) not in excuses]
    print('写操作没被点名 %d 条（已登记例外 %d 条，待补 %d 条）' % (
        len(unref_write), len(excused), len(unexcused)))
    for m, p in unexcused[:15]:
        print('  %-6s %s' % (m, p))

    # --budget N：未覆盖写操作条数不得超过 N。
    # 这是防回归用的：接口改动能随手加一条没人验的写路由，
    # 而「未覆盖 189 条」这种数字没人会盯着看。超了就 fail，逼着补用例或写进白名单。
    budget = None
    if '--budget' in sys.argv:
        idx = sys.argv.index('--budget')
        if idx + 1 < len(sys.argv):
            budget = int(sys.argv[idx + 1])
    if budget is not None:
        if len(unexcused) > budget:
            print('CLOSURE_BUDGET=FAIL 待补写操作 %d 条 > 预算 %d：%s' % (
                len(unexcused), budget,
                ', '.join('%s %s' % (m, p) for m, p in unexcused[:5])))
            print('  补用例，或在 scripts/ci/closure_excuses.json 里写明为什么没法补')
            return 1
        print('CLOSURE_BUDGET=PASS 待补写操作 %d 条 ≤ 预算 %d（另有 %d 条已登记例外）' % (
            len(unexcused), budget, len(excused)))

    if '--write' in sys.argv:
        lines = ['# REST 接口引用索引', '',
                 '> 由 scripts/ci/rest_closure.py --write 生成，勿手改。',
                 '> 路由快照来自 SS_ROUTE_DUMP=1 跑 TestSetupRouterInventory（需要真实 Mongo/Redis）。', '',
                 '**这是索引，不是覆盖率裁决。** 两处天然低估：路径可能是拼出来的（脚本按字面量 + 前缀匹配）；',
                 '这个项目 POST 大量用于查列表，光看 method 分不出读写，所以读写分类是名字启发式。', '',
                 '两个维度分开看：**测试/契约点名**反映验证面（谁验过这条接口），**UI 引用**反映产品面',
                 '（界面会不会真的调到）。只看前者会把「UI 在用但没人写测试」误读成「没人用」。', '',
                 '| 维度 | 数量 |', '| --- | --- |',
                 '| 路由总数 | %d |' % total,
                 '| 被测试/契约脚本点名 | %d（%.0f%%） |' % (cov, cov * 100.0 / total),
                 '| 读操作 点名/总数 | %d / %d |' % (cov_kind['read'], by_kind['read']),
                 '| 写操作 点名/总数 | %d / %d |' % (cov_kind['write'], by_kind['write']),
                 '| 无法判读 点名/总数 | %d / %d |' % (cov_kind['unknown'], by_kind['unknown']),
                 '| UI 引用 | %d（%.0f%%） |' % (covered_ui, covered_ui * 100.0 / total),
                 '| 写操作：测试与 UI 都没碰 | %d |' % len(dead_write),
                 '', '## 按模块（未点名 / 总数）', '', '| 模块 | 未点名 | 总数 |', '| --- | --- |']
        for mod in sorted(mod_total, key=lambda x: mod_total[x] - mod_cov[x], reverse=True):
            lines.append('| %s | %d | %d |' % (mod, mod_total[mod] - mod_cov[mod], mod_total[mod]))
        lines += ['', '## 写操作里完全没被点名的（优先补测试）', '', '| 方法 | 路径 |', '| --- | --- |']
        for m, p in unexcused:
            lines.append('| %s | %s |' % (m, BT + p + BT))
        if not unexcused:
            lines.append('| - | 全部已点名，或已登记例外 |')
        lines += ['', '## 已登记的例外（为什么还没点名）', '',
                  '来源 `scripts/ci/closure_excuses.json`。登记不等于免检：条件变了（有了本地桩、有了测试密钥）就该补用例。',
                  '', '| 方法 | 路径 | 原因 |', '| --- | --- | --- |']
        for m, p in excused:
            lines.append('| %s | %s | %s |' % (m, BT + p + BT, excuses['%s %s' % (m, p)]))
        if not excused:
            lines.append('| - | 无 | - |')
        lines += ['', '## 写操作里测试与 UI 都没碰的（死路由候选）', '',
                  '这些要么删掉，要么补上真正的调用方；留着只会让「接口覆盖」看起来比实际好。', '',
                  '| 方法 | 路径 |', '| --- | --- |']
        for m, p in dead_write:
            lines.append('| %s | %s |' % (m, BT + p + BT))
        OUT_MD.write_text(chr(10).join(lines) + chr(10), encoding='utf-8')
        print('written %s' % OUT_MD.relative_to(ROOT))

if __name__ == '__main__':
    sys.exit(main() or 0)
