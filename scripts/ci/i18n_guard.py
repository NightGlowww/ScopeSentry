#!/usr/bin/env python3
"""静态检查：界面文案里不许再出现硬编码中文。

背景：多轮英文界面走查都是「渲染看得见的部分」，藏在
aria-label / ElTooltip content / ElMessageBox / 删除确认 里的中文
只有摸到那条路径才会暴露。这个脚本把「界面文案」变成静态可判定的集合。

判据（只覆盖会显示给人看的字符串）：
  1. <template> 块内的裸文本与静态属性值出现 CJK
  2. aria-label / title / placeholder / content 属性值出现 CJK
  3. <script> 里引号内出现 CJK（去掉行尾 // 注释后再判），console.* 除外

不覆盖（有意）：纯注释、console.* 调试输出、CSS、locale 文件、数据比对用的
后端常量（见 ALLOW）。

用法：
  python3 scripts/ci/i18n_guard.py            # 默认扫描 frontend/src
  python3 scripts/ci/i18n_guard.py --demo     # 连 views/Components 一起扫
退出码：0 干净 / 1 有违规 / 2 找不到源码
"""
import argparse
import pathlib
import re
import sys

BT = chr(96)
CJK = re.compile(u'[\u4e00-\u9fa5]')
QUOTE_CHARS = "'" + '"' + BT
# 允许清单：这些中文是「和后端数据比对」或「老数据回落」，不是界面文案。
ALLOW = {
    ("views/Task/Task.vue", u'排队超过'): u'老任务没有 failCode，回落匹配中文前缀',
    ("views/Plugins/plugin.vue", u'内置'): u'后端 tag 值就是「内置」，属于数据不是文案',
}
DEBUG_CALL = re.compile(r'\bconsole\.(log|warn|error|debug|info)\s*\(')
ATTR_CJK = re.compile(r'(aria-label|title|placeholder|content|label)\s*=\s*"([^"]*)"')
QUOTED = re.compile(u'([' + re.escape(QUOTE_CHARS) + u'])((?:[^' + re.escape(QUOTE_CHARS) + u'\\\\]|\\\\.)*?)\\1')
SINGLE_TAG = re.compile(r'<[^>]*>')
MUSTACHE = re.compile(r'\{\{.*?\}\}')
HTML_COMMENT = re.compile(r'<!--.*?-->', re.S)


def strip_line_comment(line):
    """去掉行尾 // 注释：只在引号闭合状态下去切。"""
    out, quote, i = [], None, 0
    while i < len(line):
        ch = line[i]
        if quote:
            if ch == '\\\\':
                out.append(line[i:i + 2])
                i += 2
                continue
            if ch == quote:
                quote = None
        else:
            if ch in QUOTE_CHARS:
                quote = ch
            elif line.startswith('//', i):
                break
        out.append(ch)
        i += 1
    return ''.join(out)


def blank_comments(text):
    """把 <!-- ... --> 整段（可能跨行）替换成空格，保留换行以便行号不变。"""
    def repl(m):
        return re.sub(r'[^\n]', ' ', m.group(0))
    return HTML_COMMENT.sub(repl, text)


def scan_file(path, rel):
    hits = []
    src = path.read_text(encoding='utf-8', errors='replace')

    m = re.search(r'<template>(.*?)</template>', src, re.S)
    if m:
        base = src[:m.start(1)].count('\n')
        block = blank_comments(m.group(1))
        for i, line in enumerate(block.splitlines(), 1):
            stripped = strip_line_comment(line)
            n = []
            for attr in ATTR_CJK.finditer(stripped):
                if CJK.search(attr.group(2)):
                    n.append('attr %s=%s' % (attr.group(1), attr.group(2)[:40]))
            text_only = MUSTACHE.sub(' ', SINGLE_TAG.sub(' ', stripped))
            for run in CJK.findall(text_only):
                n.append('text ' + run[:20])
            if n:
                hits.append((base + i, '; '.join(n), line.strip()[:100]))

    for sm in re.finditer(r'<script[^>]*>(.*?)</script>', src, re.S):
        base = src[:sm.start(1)].count('\n')
        for i, line in enumerate(sm.group(1).splitlines(), 1):
            st = line.strip()
            if st.startswith('//') or st.startswith('*') or st.startswith('/*'):
                continue
            if DEBUG_CALL.search(line):
                continue
            code = strip_line_comment(line)
            if not CJK.search(code):
                continue
            found = [q.group(2) for q in QUOTED.finditer(code) if CJK.search(q.group(2))]
            keep = [s for s in found if (rel, s) not in ALLOW]
            if keep:
                hits.append((base + i, 'string ' + ' | '.join(x[:40] for x in keep), line.strip()[:100]))
    return hits


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--root', default='frontend/src')
    ap.add_argument('--demo', action='store_true', help=u'连 views/Components 一起扫')
    a = ap.parse_args()

    root = pathlib.Path(a.root)
    if not root.is_dir():
        print(u'找不到 %s（请在仓库根目录运行）' % root)
        return 2

    bad = 0
    scanned = 0
    for p in sorted(root.rglob('*.vue')):
        rel = str(p.relative_to(root))
        if not a.demo and rel.startswith('views/Components/'):
            continue
        scanned += 1
        hits = scan_file(p, rel)
        if hits:
            bad += len(hits)
            print('== %s' % rel)
            for no, what, ctx in hits:
                print('   %s:%d  %s' % (rel, no, what))
                print('      ' + ctx)
    print('')
    print(u'扫描 %d 个 .vue，命中 %d 处硬编码中文' % (scanned, bad))
    if bad:
        print('I18N_GUARD=FAIL')
        return 1
    print('I18N_GUARD=PASS')
    return 0


if __name__ == '__main__':
    sys.exit(main())
