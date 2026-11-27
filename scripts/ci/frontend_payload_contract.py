#!/usr/bin/env python3
"""静态检查：后端回 data:null 的接口，前端不许读 res.data.<字段>。

背景（2026-11-28）：修改密码成功后前端写的是 ElMessage.success(res.data.message)，
而后端 /api/user/change_password 回的是 data: null —— 读 message 直接抛 TypeError，
用户看到的是「改成功了但一句提示都没有」。这类错不报编译错、不报接口错，
只在用户点到那条路径时表现为「没反应」。

判定链：
  1. 扫 Go handler，找出**函数体内**（不跨 func 边界）确实调用 response.Success(c, nil, ...) 的函数；
  2. 用路由表把「group 前缀 + 子 Path」拼成完整 URL，映射到这些函数；
  3. 扫前端 api/*.ts，把 export const fn = ... url: '/api/...' 映射成 fn → url（完整 URL 等值匹配，
     不做短后缀匹配 —— 子 Path 形如 "status"/"run" 会让任何以它结尾的 URL 误命中）；
  4. 扫前端调用点：= await fn(...) 之后 5 行内出现 <var>.data.<field> 就算违规（注释行跳过）。

用法：python3 scripts/ci/frontend_payload_contract.py
退出码：0 干净 / 1 有违规 / 2 找不到源码
"""
import pathlib
import re
import sys

BT = chr(96)
API_PREFIX = '/api'
GO_HANDLERS = 'internal/api/handlers'
GO_ROUTES = 'internal/api/routes'
FE_API = 'frontend/src/api'
FE_SRC = 'frontend/src'

NIL_SUCCESS = re.compile(r'response\.Success\(\s*c\s*,\s*nil\s*,')
FUNC_START = re.compile(r'^func (\w+)\(')
GROUP_HEAD = re.compile(r'Path:\s*"([^"]*)"\s*,\s*\n\s*Routes:\s*\[\]models\.Route\{')
ROUTE_ENTRY = re.compile(r'Method:\s*"(\w+)"\s*,\s*Path:\s*"([^"]*)"\s*,\s*Handler:\s*([\w.]+)')
FN_URL = re.compile(r"export const (\w+)[^=]*=[\s\S]{0,600}?url:\s*[" + BT + r"'\"]([^" + BT + r"'\"]+)")


def nil_payload_handlers():
    """{(handler 目录, 函数名)} —— 只按函数名会撞车（backup.Status 与 plugin.Status 同名）。"""
    out = set()
    for p in sorted(pathlib.Path(GO_HANDLERS).rglob('*.go')):
        if p.name.endswith('_test.go'):
            continue
        pkgdir = p.parent.name
        current = None
        for line in p.read_text(encoding='utf-8', errors='replace').splitlines():
            m = FUNC_START.match(line)
            if m:
                current = m.group(1)
                continue
            if current and NIL_SUCCESS.search(line):
                out.add((pkgdir, current))
    return out


IMPORT_LINE = re.compile(r'^\s*(\w+)?\s*"([^"]*handlers/[^"]+)"', re.M)


def full_routes(handlers):
    """完整 URL → (目录, 函数)；只保留 data:null 的 handler。"""
    urls = {}
    for p in sorted(pathlib.Path(GO_ROUTES).rglob('*.go')):
        if p.name.endswith('_test.go'):
            continue
        txt = p.read_text(encoding='utf-8', errors='replace')
        alias = {}
        for m in IMPORT_LINE.finditer(txt):
            name, path = m.group(1), m.group(2)
            d = path.rstrip('/').split('/')[-1]
            alias[name or d] = d
        heads = [(m.start(), m.group(1)) for m in GROUP_HEAD.finditer(txt)]
        for idx, (pos, gpath) in enumerate(heads):
            end = heads[idx + 1][0] if idx + 1 < len(heads) else len(txt)
            for m in ROUTE_ENTRY.finditer(txt[pos:end]):
                child, ref = m.group(2), m.group(3)
                pkg, fn = ref.split('.') if '.' in ref else ('', ref)
                key = (alias.get(pkg, pkg), fn)
                if key in handlers:
                    urls[API_PREFIX + gpath + child] = key
    return urls


def frontend_fns():
    out = {}
    for p in sorted(pathlib.Path(FE_API).rglob('*.ts')):
        txt = p.read_text(encoding='utf-8', errors='replace')
        for m in FN_URL.finditer(txt):
            out[m.group(1)] = m.group(2)
    return out


def strip_comment(line):
    s = line.strip()
    if s.startswith('//') or s.startswith('*') or s.startswith('/*'):
        return ''
    idx = line.find('//')
    return line[:idx] if idx >= 0 else line


def main():
    if not pathlib.Path(GO_HANDLERS).is_dir() or not pathlib.Path(FE_API).is_dir():
        print('请在仓库根目录运行（找不到 %s 或 %s）' % (GO_HANDLERS, FE_API))
        return 2

    nilfns = nil_payload_handlers()
    routes = full_routes(nilfns)
    fns = {name: url for name, url in frontend_fns().items() if url in routes}

    flags = []
    for p in sorted(pathlib.Path(FE_SRC).rglob('*')):
        if p.suffix not in ('.vue', '.ts'):
            continue
        rel = str(p)
        if 'locales' in rel or '/api/' in rel:
            continue
        raw = p.read_text(encoding='utf-8', errors='replace').splitlines()
        lines = [strip_comment(x) for x in raw]
        for i, line in enumerate(lines):
            for fn in fns:
                m = re.search(r'(?:const\s+(\w+)\s*=\s*)?await\s+' + re.escape(fn) + r'\s*\(', line)
                if not m:
                    continue
                var = m.group(1) or 'res'
                window = '\n'.join(lines[i:i + 5])
                if re.search(re.escape(var) + r'\.data\??\.\w+', window):
                    flags.append((m.group(0).strip()[:16], rel, i + 1, var, fn, routes[fns[fn]]))

    print('data:null 接口 %d 个 → 前端对应 api 函数 %d 个' % (len(routes), len(fns)))
    for _, rel, ln, var, fn, key in flags:
        print('  %s:%d  读 %s.data.* 但 %s（%s/%s）回 data:null' % (rel, ln, var, fn, key[0], key[1]))
    print()
    if flags:
        print('PAYLOAD_CONTRACT=FAIL (%d)' % len(flags))
        return 1
    print('PAYLOAD_CONTRACT=PASS（没有前端读 data:null 接口的字段）')
    return 0


if __name__ == '__main__':
    sys.exit(main())
