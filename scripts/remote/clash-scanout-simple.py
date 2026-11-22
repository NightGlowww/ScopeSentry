#!/usr/bin/env python3
import re, pathlib, yaml
p = pathlib.Path("/root/clashctl/resources/mixin.yaml")
s = p.read_text(encoding="utf-8")

s = re.sub(r"^proxy-groups:\n(?:[ \t].*\n|[ \t]*\n)*", "", s, flags=re.M)
s = re.sub(r"^listeners:\n(?:[ \t].*\n|[ \t]*\n)*", "", s, flags=re.M)
s = re.sub(r"^proxy-providers:\n(?:[ \t].*\n|[ \t]*\n)*", "", s, flags=re.M)

BLOCK = """listeners:
  - name: scan-out
    type: redir
    port: 7893
    listen: 127.0.0.1
    proxy: SCAN-OUT

proxy-providers:
  ytoo:
    type: file
    path: ./profiles/1.yaml
    health-check:
      enable: true
      url: http://www.gstatic.com/generate_204
      interval: 300

proxy-groups:
  prefix:
    - name: SCAN-OUT
      type: url-test
      url: http://www.gstatic.com/generate_204
      interval: 300
      tolerance: 50
      proxies:
        - MESL
      use:
        - ytoo
"""
s = s.rstrip() + "\n\n" + BLOCK
d = yaml.safe_load(s)
assert "proxy-groups" in d and "listeners" in d and "proxy-providers" in d
p.write_text(s, encoding="utf-8")
print("rewritten; prefix groups:", len(d["proxy-groups"]["prefix"]))
