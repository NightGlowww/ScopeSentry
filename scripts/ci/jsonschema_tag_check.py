import pathlib, re, sys
ROOT = pathlib.Path(__file__).resolve().parents[2]
src = (ROOT / 'internal/mcp/tools.go').read_text(encoding='utf-8')
# 只挑 jsonschema:"..." 里的内容；等号紧跟在中文/字母串后面、且后面还有非空白内容，就是可疑的默认值写法
tags = re.findall(r'jsonschema:\"([^\"]*)\"', src)
suspicious = []
for t in tags:
    for m in re.finditer(r'([\w\u4e00-\u9fa5]{1,20})=([^\s，。；、）)]+)', t):
        suspicious.append((m.group(0), t[:40]))
if suspicious:
    print('JSONSCHEMA_TAG_WARN=%d' % len(suspicious))
    for frag, ctx in suspicious[:10]:
        print('  %-24s ... %s' % (frag, ctx))
else:
    print('JSONSCHEMA_TAG_WARN=0')