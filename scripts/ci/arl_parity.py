#!/usr/bin/env python3
"""ARL-Next 路由族覆盖对照（生成 docs/audit/ARL_NEXT_COVERAGE.md）。

上游 58 个 route 文件、302 个端点，我们这个仓库 314 条路由。这张表回答一个具体问题：
「上游每个功能族，在我们这边有没有对应的接口」。

判读口径：
  ✅ 覆盖     同一族里我们有点名的接口，且数量不少于上游族端点数的 60%
  🟡 部分     有接口，但明显少于上游（缺的是具体端点，表里列出来）
  ❌ 缺失     一条都没有
  ❓ 未登记   MAP 里没这一族（不静默通过：需要人工判断是「不需要」还是「漏了」）

上游族名 → 我们路径关键词的映射是**手写**的：自动语义映射在「site 和 root_domain 都叫域名」
这种地方一定出错，错一次就会把缺失记成覆盖。所以改这张表要人看一眼。

用法：
  python3 scripts/ci/arl_parity.py            # 打印汇总
  python3 scripts/ci/arl_parity.py --write    # 重写 docs/audit/ARL_NEXT_COVERAGE.md
"""
import json
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parents[2]
UPSTREAM = ROOT / 'docs/audit/upstream_routes.json'
SNAPSHOT = ROOT / 'docs/audit/routes.snapshot.tsv'
OUT = ROOT / 'docs/audit/ARL_NEXT_COVERAGE.md'

# 上游文件 → 我们的路径关键词（任一命中即算这一族有接口）
MAP = {
    'assetCert.py': ['/assets/cert'],
    'assetCip.py': ['/assets/cip'],
    'assetDomain.py': ['/assets/root_domain'],
    'assetFileleak.py': ['/assets/fileleak'],
    'assetGroup.py': ['/assets/asset_group'],
    'assetIP.py': ['/assets/ip'],
    'assetNpocService.py': ['/assets/npoc_service'],
    'assetNucleiResult.py': ['/assets/nuclei_result'],
    'assetOverview.py': ['/assets/asset_overview'],
    'assetScope.py': ['/assets/asset_scope'],
    'assetService.py': ['/assets/service'],
    'assetSite.py': ['/assets/site'],
    'assetStatFinger.py': ['/assets/stat_finger'],
    'assetUrl.py': ['/assets/url'],
    'assetVuln.py': ['/assets/vulnerability'],
    'assetWih.py': ['/assets/wih'],
    'backup.py': ['/backup'],
    'batchExport.py': ['/export/batch', '/export'],
    # 通用字典接口是 kind 参数化的（subdomain/dir/brute/cdn/other），brute/cdn 复用同一套
    'brute_dict.py': ['/dict_brute', '/dictionary/'],
    'cdn_dict.py': ['/dict_cdn', '/dictionary/'],
    'cert.py': ['/assets/cert'],
    'cip.py': ['/assets/cip'],
    'console.py': ['/console/info'],
    'dashboard.py': ['/dashboard'],
    'dictionary.py': ['/dictionary/'],
    'domain.py': ['/assets/root_domain'],
    'export.py': ['/export'],
    'fileleak.py': ['/assets/fileleak'],
    'fingerprint.py': ['/fingerprint'],
    'github_monitor_result.py': ['/github/monitor_result'],
    'github_result.py': ['/github/result'],
    'github_scheduler.py': ['/github/scheduler'],
    'github_task.py': ['/github/task'],
    'github_threat.py': ['/github/threat'],
    'http_status.py': ['/assets/http_status'],
    'icp.py': ['/icp/'],
    'image.py': ['/assets/asset/screenshot'],
    'ip.py': ['/assets/ip'],
    # 上游是 REST 面，我们的 MCP 走 JSON-RPC（/api/mcp/* 只有治理面）
    'mcp.py': ['/mcp/'],
    'npoc_service.py': ['/assets/npoc_service'],
    'nuclei_result.py': ['/assets/nuclei_result'],
    'url.py': ['/assets/url'],
    'poc.py': ['/poc'],
    'policy.py': ['/policy'],
    'scheduler.py': ['/task/scheduled'],
    'service.py': ['/assets/service'],
    'site.py': ['/assets/site'],
    'stat_finger.py': ['/assets/stat_finger'],
    # 上游单独一个 syslog 路由；我们复用 /api/dashboard/logs（前端 Syslog.vue 就调它）
    'syslog.py': ['/dashboard/logs'],
    'system_config.py': ['/configuration/', '/system/'],
    'task.py': ['/task/'],
    'taskFofa.py': ['/fofa'],
    'task_schedule.py': ['/task/scheduled'],
    'user.py': ['/user'],
    'vuln.py': ['/assets/vulnerability'],
    'wih.py': ['/assets/wih'],
    # 上游 nuclei.py 是「模板库管理 + 社区同步 + 黑名单」，我们只有结果读模型：
    # 明确不映射，让它落到 ❓ 里被看见，而不是拿 nuclei_result 冒充覆盖。
    'nuclei.py': [],
}

SKIP = {'__init__.py'}

# 形状差异：计数少于上游但能力在（删除/导出统一走共享接口），写清楚免得被当成缺口
SHAPE_NOTE = {
    'assetSite.py': '删除/导出走 /api/assets/common/delete 与 /api/export，不再逐族开一套',
    'assetUrl.py': '同上',
    'assetService.py': '同上',
    'assetCip.py': '列表/详情两条已够；导出走通用 /api/export',
    'assetDomain.py': '根域名接口是 /assets/root_domain（单条），删除走 common/delete',
    'domain.py': '同上',
    'site.py': '同上（读模型叫 /assets/site）',
    'service.py': '同上（读模型叫 /assets/service）',
    'mcp.py': ('上游把这些做成 REST（asset_profile/search_assets/create_scan_task…）；'
               '我们做成 MCP 工具（get_asset_profile/search_assets/create_scan_task…），REST 只留治理面'),
    'system_config.py': '代理节点/策略、更新检查这些上游能力我们还没有，见候选缺口',
}


def leaf_gaps(upstream_eps, matched):
    """上游端点里，叶名在我们接口里找不到对应的（启发式，供人复核）。"""
    leaves = set()
    for path in matched:
        for seg in path.split('/'):
            if seg and not seg.startswith(':'):
                leaves.add(seg.lower())
    out = []
    for ep in upstream_eps:
        segs = [s for s in ep.split('/') if s and not s.startswith('<')]
        if not segs:
            continue
        leaf = segs[-1].lower()
        if leaf not in leaves and not any(leaf in l or l in leaf for l in leaves):
            out.append(ep)
    return out


def load_routes():
    out = []
    for line in SNAPSHOT.read_text(encoding='utf-8').splitlines():
        if not line.strip():
            continue
        _, path = line.split('\t')
        out.append(path.strip())
    return out


def verdict(up_count, matched):
    if not matched:
        return '❌ 缺失'
    if up_count == 0:
        return '✅ 覆盖'
    if len(matched) * 5 >= up_count * 3:
        return '✅ 覆盖'
    return '🟡 部分'


def main():
    upstream = json.loads(UPSTREAM.read_text(encoding='utf-8'))
    ours = load_routes()
    rows = []
    unmapped = []
    for fname in sorted(upstream):
        if fname in SKIP:
            continue
        eps = upstream[fname]
        if fname not in MAP:
            unmapped.append((fname, eps))
            continue
        keys = MAP[fname]
        matched = sorted({p for p in ours if any(k in p for k in keys)}) if keys else []
        rows.append((fname, eps, matched, verdict(len(eps), matched)))

    full = sum(1 for r in rows if r[3] == '✅ 覆盖')
    part = sum(1 for r in rows if r[3] == '🟡 部分')
    miss = sum(1 for r in rows if r[3] == '❌ 缺失')
    print('上游族 %d（端点 %d）：✅ %d / 🟡 %d / ❌ %d，未登记 %d' % (
        len(rows), sum(len(r[1]) for r in rows), full, part, miss, len(unmapped)))
    for fname, eps, matched, v in rows:
        if v != '✅ 覆盖':
            print('  %-26s %-8s 上游 %d 端点，我们 %d 条' % (fname, v, len(eps), len(matched)))
    for fname, eps in unmapped:
        print('  %-26s 未登记（上游 %d 端点）：%s' % (fname, len(eps), ', '.join(eps[:4])))
    gaps = 0
    for fname, eps, matched, v in rows:
        gaps += len(leaf_gaps(eps, matched) if matched else eps)
    print('端点级候选缺口（启发式，需人工复核）：%d 条' % gaps)

    if '--write' in sys.argv:
        lines = [
            '# ARL-Next 路由族覆盖对照',
            '',
            '> 由 scripts/ci/arl_parity.py --write 生成，勿手改。',
            '> 上游快照来自 owl234/ARL-Next 仓库 backend/app/routes（%d 个文件、%d 个端点）。' % (
                len(upstream), sum(len(v) for v in upstream.values())),
            '>',
            '> 判读口径：✅ 同族有接口且数量不少于上游 60%；🟡 有接口但明显少；❌ 一条都没有；❓ 未登记（需要人判断）。',
            '> 族名→路径关键词的映射是手写的：自动语义映射会把「都叫域名」的族混在一起，把缺失记成覆盖。',
            '',
            '| 族 | 上游端点 | 我们接口数 | 判定 | 说明 |',
            '| --- | ---: | ---: | --- | --- |',
        ]
        for fname, eps, matched, v in rows:
            lines.append('| %s | %d | %d | %s | %s |' % (fname, len(eps), len(matched), v, SHAPE_NOTE.get(fname, '')))
        lines += ['', '## 未登记的族（上游有、我们没在 MAP 里表态）', '']
        if unmapped:
            lines += ['| 族 | 上游端点 |', '| --- | --- |']
            for fname, eps in unmapped:
                lines.append('| %s | %d |' % (fname, len(eps)))
        else:
            lines.append('无。')
        lines += ['', '## 逐族明细', '']
        for fname, eps, matched, v in rows:
            lines.append('### %s —— %s' % (fname, v))
            lines.append('')
            lines.append('上游：' + ', '.join(eps[:20]) + (' …' if len(eps) > 20 else ''))
            lines.append('')
            if matched:
                lines.append('我们：' + ', '.join(matched[:20]) + (' …' if len(matched) > 20 else ''))
            else:
                lines.append('我们：（无对应接口）')
            lines.append('')
            miss_eps = leaf_gaps(eps, matched) if matched else eps
            if miss_eps:
                lines.append('候选缺口（叶名对不上，需人工判断）：' + ', '.join(miss_eps[:20]) + (' …' if len(miss_eps) > 20 else ''))
                lines.append('')
        OUT.write_text(chr(10).join(lines) + chr(10), encoding='utf-8')
        print('written %s' % OUT.relative_to(ROOT))
    return 0


if __name__ == '__main__':
    sys.exit(main())