#!/usr/bin/env python3
"""静态+运行时的交叉检查：表格列的 field 名，必须在列表接口的返回里真的存在。

背景（2026-11-28）：计划任务列表的「状态」列读 row.state，而接口回的是 scheduledTasks
（模型定义 ScheduledState 的 bson 名），于是这一列永远是空白，用户看不出启用状态。
这类错编译不报、接口不报，只有肉眼盯着页面才发现。

做法：
  1. 扫 views 下每个 .vue 里的列定义 field: 'x'；
  2. 取该文件 fetchDataApi 里 await 的那个 xxxApi，按 src/api 的 url 映射成接口地址；
  3. 用真实 JWT 调一次（pageIndex=1,pageSize=1），拿 data.list[0] 的键；
  4. field 不在返回键里、且该列没有 formatter（formatter 自己算值，允许缺字段）→ 报可疑。

用法：SS_BASE=http://host:8082 SS_TOKEN=<JWT> python3 scripts/ci/column_field_check.py
退出码：0 没发现问题 / 1 有可疑列 / 2 缺环境
"""
import json
import os
import pathlib
import re
import sys
import urllib.error
import urllib.request

BT = chr(96)
SKIP_FIELDS = {'selection', 'index', 'action', 'actions', 'operate', 'operation'}
VIEWS = pathlib.Path('frontend/src/views')
APIS = pathlib.Path('frontend/src/api')


def api_urls():
    out = {}
    for p in sorted(APIS.rglob('*.ts')):
        txt = p.read_text(encoding='utf-8', errors='replace')
        for m in re.finditer(r"export const (\w+)[^=]*=[\s\S]{0,600}?url:\s*[" + BT + r"'\"]([^" + BT + r"'\"]+)", txt):
            out[m.group(1)] = m.group(2)
    return out


def tables_of(txt):
    """模板里每个 <Table :columns="X"> 的 X，按出现顺序。"""
    return [m.group(1) for m in re.finditer(r'<Table[\s\S]{0,1200}?:columns="(\w+)"', txt)]


def schema_block(txt, name):
    """取出 const <name> = ... [...] 这一段（到下一个顶层 const 或文件末尾）。"""
    m = re.search(r'const ' + re.escape(name) + r'\s*=\s*[\s\S]{0,80}?\[', txt)
    if not m:
        return ''
    start = m.start()
    nxt = re.search(r'\nconst \w+', txt[m.end():])
    return txt[start:m.end() + (nxt.start() if nxt else len(txt) - m.end())]


def columns(txt, name):
    """[(field, has_formatter)] —— formatter 只看同一小块里的下一个 field 之前。"""
    block = schema_block(txt, name)
    hits = [(m.start(), m.group(1)) for m in re.finditer(r"field:\s*'([A-Za-z0-9_]+)'", block)]
    out = []
    for i, (pos, fld) in enumerate(hits):
        end = hits[i + 1][0] if i + 1 < len(hits) else len(block)
        out.append((fld, 'formatter:' in block[pos:end]))
    return out


def list_api_of(path):
    txt = path.read_text(encoding='utf-8', errors='replace')
    m = re.search(r'fetchDataApi:[\s\S]{0,900}', txt)
    block = m.group(0) if m else ''
    fns = re.findall(r'await\s+(\w+Api)\s*\(', block)
    if not fns:
        fns = re.findall(r'await\s+(\w+Api)\s*\(', txt)
    return fns[0] if fns else None


def fetch_keys(base, token, url):
    # 取 5 行取键的并集：omitempty 的字段只在有值时才出现，只取第 1 行会误判。
    body = json.dumps({'pageIndex': 1, 'pageSize': 5, 'search': ''}).encode()
    req = urllib.request.Request(base + url, data=body, method='POST',
        headers={'Content-Type': 'application/json', 'Authorization': 'Bearer ' + token})
    try:
        d = json.loads(urllib.request.urlopen(req, timeout=60).read().decode() or '{}')
    except Exception:
        return None
    data = d.get('data')
    if not isinstance(data, dict):
        return None
    lst = data.get('list') or data.get('items')
    if not isinstance(lst, list) or not lst:
        return None
    keys = set()
    for row in lst:
        if isinstance(row, dict):
            keys |= set(k.lower() for k in row.keys())
    return keys or None


def main():
    base = os.environ.get('SS_BASE', '').rstrip('/')
    token = os.environ.get('SS_TOKEN', '')
    if not base or not token:
        print('需要 SS_BASE 与 SS_TOKEN')
        return 2

    urls = api_urls()
    suspects = []
    warns = []
    checked = 0
    skipped = 0
    for view in sorted(VIEWS.rglob('*.vue')):
        if 'views/Components' in str(view):
            continue
        txt = view.read_text(encoding='utf-8', errors='replace')
        tbls = tables_of(txt)
        # 一个文件里挂多张表时，列 schema 与接口无法可靠配对（子表多半走详情接口），
        # 硬配会全是假阳性 —— 直接跳过并计数，比报一堆噪声有用。
        if len(tbls) == 1:
            cols = columns(txt, tbls[0])
        elif len(tbls) == 0 and txt.count('<Table') == 1 and 'const crudSchemas' in txt:
            # useCrudSchemas 写法：列定义在 crudSchemas 里，模板用 allSchemas.tableColumns。
            cols = columns(txt, 'crudSchemas')
        else:
            skipped += 1
            continue
        if not cols:
            skipped += 1
            continue
        fn = list_api_of(view)
        url = urls.get(fn or '')
        if not url:
            skipped += 1
            continue
        keys = fetch_keys(base, token, url)
        if keys is None:
            continue
        checked += 1
        for fld, has_fmt in cols:
            if fld in SKIP_FIELDS:
                continue
            if fld.lower() not in keys:
                # 有 formatter 的列自己算值，缺字段多半正常；但也要列出来——
                # 计划任务「状态」列就是「有 formatter + 字段名写错」才一直空白的。
                bucket = warns if has_fmt else suspects
                bucket.append((str(view.relative_to(VIEWS)), fld, url, sorted(keys)[:8]))

    print('检查了 %d 个表格页（列字段 vs 列表接口返回），跳过 %d 个（多表/无接口）' % (checked, skipped))
    for view, fld, url, keys in suspects:
        print('  缺失 %s  列 field=%r 不在 %s 的返回里（返回键示例 %s）' % (view, fld, url, keys))
    for view, fld, url, keys in warns:
        print('  待人工 %s  列 field=%r 有 formatter 但接口没这个字段，确认不是读错名字' % (view, fld))
    print('')
    if suspects:
        print('COLUMN_FIELD=FAIL (%d)' % len(suspects))
        return 1
    print('COLUMN_FIELD=PASS（没有「字段名写错」的列；%d 条待人工确认）' % len(warns))
    return 0


if __name__ == '__main__':
    sys.exit(main())
