#!/usr/bin/env python3
"""Frontend 反馈守卫：拦住「点了没反应」和「失败后按钮转圈不停」两类缺陷。

两类检查：
  STUCK  函数里把 `xxxLoading.value = true` 打开，却只在 try 成功路径上重置(false)，
         一旦接口抛错，按钮永远停在 loading，弹窗变成按不动的死按钮。要求 reset 写进 finally。
  SILENT 变更型接口（delete/update/add/save/toggle/...）调用后既没有 ElMessage 提示，
         也没有刷新列表/关闭弹窗 —— 用户无法判断到底生效没有。作为 WARN 输出，
         由 ALLOW 白名单显式豁免（父组件负责提示的场景）。

用法: python3 scripts/ci/feedback_guard.py [--strict]
"""
import os
import re
import sys

ROOT = os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', '..', 'frontend', 'src')

MUT_API = re.compile(r'await\s+(\w*(?:delete|Delete|update|Update|add|Add|save|Save|toggle|Toggle|set|Set|restore|Restore|run|Run|sync|Sync|create|Create|remove|Remove|upload|Upload|import|Import|apply|Apply|enable|Enable|disable|Disable|reset|Reset|clear|Clear)\w*)Api\(')
FEEDBACK = re.compile(r'ElMessage\s*\.\s*(success|error|warning)')
REFRESH = re.compile(r'(getList\(|fetch\w*\(|load\w*\(|getData\(|reload\w*\(|refresh\w*\()')
CLOSE = re.compile(r'(closeDialog|dialogVisible|visible\.value|emit\()')
LOADING_SET = re.compile(r'(\w*[Ll]oading)\s*\.\s*value\s*=\s*true')

# 父组件负责提示/刷新的组件，或返回值交给调用方处理的工具函数
ALLOW = {
    ('views/Asset/components/AssetAddDialog.vue', 'submitSingle'),
    ('views/Asset/components/AssetAddDialog.vue', 'submitBatch'),
    ('views/AssetGroup/components/GroupDeleteDialog.vue', 'submit'),
    ('views/AssetGroup/components/GroupFormDialog.vue', 'submit'),
    ('views/AssetGroup/components/ScopeFormDialog.vue', 'submit'),
    ('views/Configuration/ApiKey.vue', 'handleToggle'),
    ('views/Configuration/Backup.vue', 'handleDelete'),
    ('views/GitHub/index.vue', 'handleDeleteTask'),
}


def funcs(src):
    for m in re.finditer(r'(?:const|function)\s+(\w+)\s*=?\s*(?:async\s*)?\([^)]*\)\s*(?:=>)?\s*\{', src):
        start = m.end()
        depth = 1
        i = start
        while i < len(src) and depth > 0:
            if src[i] == '{':
                depth += 1
            elif src[i] == '}':
                depth -= 1
            i += 1
        yield m.group(1), src[start:i], src[: m.start()].count(chr(10)) + 1


def main():
    strict = '--strict' in sys.argv
    stuck, silent = [], []
    for dp, dn, fn in os.walk(ROOT):
        if 'node_modules' in dp:
            continue
        for f in sorted(fn):
            if not f.endswith('.vue'):
                continue
            p = os.path.join(dp, f)
            rel = os.path.relpath(p, ROOT)
            src = open(p, encoding='utf-8', errors='ignore').read()
            if '<script' not in src:
                continue
            for name, body, line in funcs(src):
                flags = LOADING_SET.findall(body)
                if flags:
                    catch_idx = body.rfind('catch')
                    finally_idx = body.rfind('finally')
                    for flag in set(flags):
                        reset = re.compile(re.escape(flag) + r'\s*\.\s*value\s*=\s*false')
                        resets = [m.start() for m in reset.finditer(body)]
                        if not resets:
                            # 连一次重置都没有：交给 SILENT/人工，不算卡死
                            continue
                        # 真正的卡死信号：flag 打开后先 await 一个调用，然后在成功路径上重置。
                        # 没有 await 的（纯计时/纯本地）不算——那种是 flag 白设，不是卡死。
                        first_set = re.search(re.escape(flag) + r'\s*\.\s*value\s*=\s*true', body)
                        if not first_set:
                            continue
                        gap = body[first_set.end(): resets[0]]
                        if 'await' not in gap:
                            continue
                        # 正常写法有两种：写进 finally，或成功/失败两个分支各重置一次
                        # （后者至少有一次 reset 出现在 catch 之后）。两种都放过，其余算失败路径卡死。
                        ok = False
                        if finally_idx != -1 and any(r > finally_idx for r in resets):
                            ok = True
                        if catch_idx != -1 and any(r > catch_idx for r in resets):
                            ok = True
                        if not ok:
                            stuck.append((rel, line, name, flag))
                mut_calls = [
                    name
                    for name in MUT_API.findall(body)
                    # 读接口名字里也可能带 set/count 之类：get*/list*/fetch* 一律不算变更
                    if not re.match(r'(get|list|fetch|query|load|search|count|check|preview|export|download|read|view|test|validate)', name)
                ]
                if not mut_calls or len(body) > 2500:
                    continue
                if FEEDBACK.search(body) or REFRESH.search(body) or CLOSE.search(body):
                    continue
                if (rel.replace(os.sep, '/'), name) in ALLOW:
                    continue
                silent.append((rel, line, name))
    print('STUCK  %d' % len(stuck))
    for rel, line, name, flag in stuck:
        print('  %s:%d %s (%s 只在成功路径重置)' % (rel, line, name, flag))
    print('SILENT %d (WARN)' % len(silent))
    for rel, line, name in silent:
        print('  %s:%d %s' % (rel, line, name))
    bad = len(stuck) > 0 or (strict and len(silent) > 0)
    print('FEEDBACK_GUARD=%s' % ('FAIL' if bad else 'PASS'))
    return 1 if bad else 0


if __name__ == '__main__':
    sys.exit(main())