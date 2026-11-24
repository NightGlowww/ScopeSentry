#!/usr/bin/env python3
"""模板精准度静态检查：找出会导致大面积误报的 matcher 写法。

检查项：
  R1 字面量被写成字符类：regex 只有 (?i)[word] 这种全字母方括号，等于"页面含任一字母即命中"
  R2 只有状态码 matcher：没有任何内容判据（word/regex/binary/dsl），任何 200/403 都算命中
  R3 过短 word：单个常见词（如 "admin"、"login"）作唯一内容判据
"""
import json,re,sys,pathlib
import yaml

ONLY_LETTER_CLASS = re.compile(r"^(?:\(\?[a-z]+\))?\[([A-Za-z]{3,})\]$")
GENERIC_WORDS = {"admin","login","root","index","test","error","server","true","false","ok","yes"}

def matchers_of(doc):
    out=[]
    for req in doc.get("http") or []:
        out.extend(req.get("matchers") or [])
        for part in req.get("matchers-condition") and [req] or []:
            pass
    return out

def main(path):
    rows=json.load(open(path))
    stats={"parsed":0,"R1":[],"R2":[],"R3":[]}
    for r in rows:
        try:
            doc=yaml.safe_load(r["content"])
        except Exception:
            continue
        if not isinstance(doc,dict): continue
        stats["parsed"]+=1
        ms=matchers_of(doc)
        for m in ms:
            if not isinstance(m,dict): continue
            for pat in (m.get("regex") or []):
                s=str(pat).strip()
                mm=ONLY_LETTER_CLASS.match(s)
                if mm and len(mm.group(1))>=3 and mm.group(1).islower() is not None:
                    # 全小写字母且长度>=3 的方括号组，几乎一定是字面量被当成字符类
                    stats["R1"].append((r["name"],r["id"],s))
        kinds={m.get("type") for m in ms if isinstance(m,dict)}
        if ms and kinds and kinds <= {"status"}:
            stats["R2"].append((r["name"],r["id"]))
        content=[m for m in ms if isinstance(m,dict) and m.get("type") in ("word","regex")]
        if ms and content and all(str(w).strip().strip("\"'").lower() in GENERIC_WORDS for m in content for w in (m.get("words") or m.get("regex") or [])):
            stats["R3"].append((r["name"],r["id"]))
    print(json.dumps({"parsed":stats["parsed"],"R1":len(stats["R1"]),"R2":len(stats["R2"]),"R3":len(stats["R3"])},ensure_ascii=False))
    for k in ("R1","R2","R3"):
        for name,i,extra in [(n,i,(e if k=="R1" else None)) for (n,i,*rest) in stats[k] for e in [rest[0] if rest else None]]:
            print(k+"\t"+i+"\t"+name+"\t"+(extra or ""))

if __name__=="__main__":
    main(sys.argv[1])
