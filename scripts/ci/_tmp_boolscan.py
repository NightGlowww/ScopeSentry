import re, pathlib, collections
root = pathlib.Path('internal')
model_files = list((root/'models').rglob('*.go'))
fields = []
for f in model_files:
    txt = f.read_text(encoding='utf-8', errors='ignore')
    for m in re.finditer(r'^\s*(\w+)\s+bool\s+`([^`]*)`', txt, re.M):
        name, tag = m.group(1), m.group(2)
        bson = re.search(r'bson:"([^,"]+)', tag)
        fields.append((f.name, name, bson.group(1) if bson else ''))
srcs = [(p, p.read_text(encoding='utf-8', errors='ignore')) for p in root.rglob('*.go') if 'models' not in p.parts and '_test' not in p.name]
dead = []
for fname, name, bson in fields:
    hits = sum(len(re.findall(r'\.'+name+r'\b', t)) for _, t in srcs)
    if hits == 0:
        dead.append((fname, name, bson))
print('bool fields in models:', len(fields), 'never read outside models:', len(dead))
for d in dead[:40]:
    print('  ', d[0], d[1], '->', d[2])