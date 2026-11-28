#!/usr/bin/env python3
"""MCP 工具参数审计：声明了但 handler 从不读的入参。


为什么需要这个检查：go-sdk 用 handler 的泛型入参结构体自动生成 JSON Schema，
所以「Schema 与 handler 类型不一致」这类漂移在编译期就没了。剩下的风险是另一种：
结构体里声明了字段（甚至标了 required），但 handler 里一次都没读。
调用方按 Schema 传了值，工具照常回 200，只是这个值被静静丢掉——
典型表现是「筛选项不生效」「分页参数不起作用」，而且没有任何报错。

启发式说明（宁可少报，不制造噪音）：
  1. handler 把整个入参对象往下传（出现 `input` 作为实参、`&input`、`req.Input`），
     说明字段在下游被消费，整个工具跳过；
  2. 字段按 Go 名字匹配 `input.Xxx`。允许写在嵌套结构里（`input.Filter.Xxx` 也算）；
  3. 只统计有 jsonschema required 的字段作为「必填未读」，其余记为「可选未读」。
"""
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parents[2]
SRC = ROOT / 'internal/mcp/tools.go'

TOOL_RE = re.compile(r'Name:\s+"([a-z0-9_]+)"[\s\S]{0,600}?\},\s*([a-zA-Z0-9_]+)\)', re.M)
FUNC_RE = re.compile(r'^func\s+([a-zA-Z0-9_]+)\(ctx context\.Context,\s*_?\s*\*mcp\.CallToolRequest,\s*input\s+([A-Za-z0-9_]+)\)', re.M)
STRUCT_RE = re.compile(r'^type\s+([A-Za-z0-9_]+)\s+struct\s*\{', re.M)
FIELD_RE = re.compile(r'^\s*([A-Z][A-Za-z0-9_]*)\s+[^\n]*?`([^`]*)`', re.M)


def load():
    return SRC.read_text(encoding='utf-8')


def tool_handlers(text):
    out = []
    for m in TOOL_RE.finditer(text):
        out.append((m.group(1), m.group(2)))
    seen = set()
    uniq = []
    for name, h in out:
        if name in seen:
            continue
        seen.add(name)
        uniq.append((name, h))
    return uniq


def func_body(text, name):
    m = re.search(r'^func\s+%s\(' % re.escape(name), text, re.M)
    if not m:
        return None
    i = text.find('{', m.end())
    if i < 0:
        return None
    depth = 0
    for j in range(i, len(text)):
        c = text[j]
        if c == '{':
            depth += 1
        elif c == '}':
            depth -= 1
            if depth == 0:
                return text[i:j + 1]
    return None


def struct_fields(text, type_name):
    m = re.search(r'^type\s+%s\s+struct\s*\{' % re.escape(type_name), text, re.M)
    if not m:
        return None
    i = m.end() - 1
    depth = 0
    for j in range(i, len(text)):
        c = text[j]
        if c == '{':
            depth += 1
        elif c == '}':
            depth -= 1
            if depth == 0:
                body = text[i + 1:j]
                fields = []
                for fm in FIELD_RE.finditer(body):
                    go_name, tags = fm.group(1), fm.group(2)
                    jm = re.search(r'json:"([^",]+)', tags)
                    json_name = jm.group(1) if jm else go_name
                    required = 'required' in tags
                    fields.append((go_name, json_name, required))
                return fields
    return None


def write_tool_exercise_report():
    """写工具是否在某个测试或 sweep 脚本里被真正点名。

    只登记 scope 不写用例的写工具，等于「接口存在但没人验过」：
    MCP 的写工具没有 gin 那层 binding 兜底，问题会直接落到数据上。
    """
    scope_src = (ROOT / 'internal/services/mcpguard/scope.go').read_text(encoding='utf-8')
    write_tools = re.findall(r'"([a-z0-9_]+)"\s*:\s*Scope[A-Za-z]*Write', scope_src)
    corpus = []
    for p in list((ROOT / 'internal/mcp').glob('*_test.go')) + list((ROOT / 'internal/api/routes').glob('*_test.go')):
        corpus.append(p.read_text(encoding='utf-8', errors='ignore'))
    sweep = ROOT / 'scripts/ci/mcp_sweep.py'
    if sweep.exists():
        corpus.append(sweep.read_text(encoding='utf-8', errors='ignore'))
    blob = chr(10).join(corpus)
    missing = [n for n in sorted(set(write_tools)) if n not in blob]
    print('写工具 %d 个，未被任何测试/sweep 点名 %d 个' % (len(set(write_tools)), len(missing)))
    for n in missing:
        print('  未点名:', n)
    return missing


def main():
    text = load()
    handlers = {}
    for _, h in tool_handlers(text):
        b = func_body(text, h)
        if b:
            handlers[h] = b
    inputs = {}
    fm = {m.group(1): m.group(2) for m in FUNC_RE.finditer(text)}
    problems = []
    skipped_wholesale = []
    checked = 0
    for tool, handler in tool_handlers(text):
        type_name = fm.get(handler)
        if not type_name:
            continue
        fields = struct_fields(text, type_name)
        body = handlers.get(handler)
        if fields is None or body is None:
            continue
        checked += 1
        if re.search(r'(?<![.A-Za-z])input\b\s*[,)]', body) or '&input' in body or 'req.Input' in body:
            skipped_wholesale.append(tool)
            continue
        unused_required, unused_optional = [], []
        for go_name, json_name, required in fields:
            if re.search(r'input\.%s\b' % re.escape(go_name), body):
                continue
            (unused_required if required else unused_optional).append(json_name)
        if unused_required:
            problems.append((tool, '必填未读', unused_required))
        elif unused_optional:
            problems.append((tool, '可选未读', unused_optional))
    print('工具 %d 个，可静态判定 %d 个（整体下传 %d 个跳过）' % (
        len(tool_handlers(text)), checked, len(skipped_wholesale)))
    fatal = [p for p in problems if p[1] == '必填未读']
    for tool, kind, fields in problems:
        print('  %-9s %-22s %s' % (kind, tool, ', '.join(fields)))
    missing = write_tool_exercise_report()
    if fatal:
        print('MCP_PARAM_AUDIT=FAIL 有必填参数从不被 handler 读取：%d 个工具' % len(fatal))
        return 1
    if missing:
        print('MCP_PARAM_AUDIT=FAIL 写工具没被任何测试/sweep 点名：%d 个' % len(missing))
        return 1
    print('MCP_PARAM_AUDIT=PASS 必填参数都有人读，写工具都有人点名')
    return 0


if __name__ == '__main__':
    sys.exit(main())
