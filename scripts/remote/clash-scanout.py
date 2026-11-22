#!/usr/bin/env python3
import re, pathlib, yaml, sys

p = pathlib.Path("/root/clashctl/resources/mixin.yaml")
s = p.read_text(encoding="utf-8")

GROUPS = """proxy-groups:
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
    - name: JEV
      type: fallback
      url: https://www.gstatic.com/generate_204
      interval: 120
      proxies:
        - 🇯🇵 日本 08 家宽
        - 🇯🇵 日本 09 家宽
        - 🇭🇰 香港 13 家宽
    - name: HUNTDECK-AUTO
      type: url-test
      url: https://www.gstatic.com/generate_204
      interval: 180
      tolerance: 50
      proxies:
        - 🇯🇵 日本 08 家宽
        - 🇯🇵 日本 09 家宽
        - 🇯🇵 日本 10 家宽
        - 🇯🇵 日本 11 家宽
        - 🇯🇵 日本 12 家宽
        - 🇭🇰 香港 13 家宽
        - 🇺🇸 美国 10 家宽
    - name: HUNTDECK
      type: select
      proxies:
        - HUNTDECK-AUTO
        - 🇯🇵 日本 08 家宽
        - 🇯🇵 日本 09 家宽
        - 🇯🇵 日本 10 家宽
        - 🇯🇵 日本 11 家宽
        - 🇯🇵 日本 12 家宽
        - 🇭🇰 香港 13 家宽
        - 🇺🇸 美国 10 家宽
        - DIRECT
"""

LISTENERS = """listeners:
  - name: scan-out
    type: redir
    port: 7893
    listen: 127.0.0.1
    proxy: SCAN-OUT
"""

# 1) 删掉所有 proxy-groups 段（含重复的）
s = re.sub(r"^proxy-groups:\n(?:[ \t].*\n|[ \t]*\n)*", "", s, flags=re.M)
# 2) 删掉所有 listeners 段
s = re.sub(r"^listeners:\n(?:[ \t].*\n|[ \t]*\n)*", "", s, flags=re.M)
# 3) 删掉重复的 proxy-providers 段，稍后统一追加
s = re.sub(r"^proxy-providers:\n(?:[ \t].*\n|[ \t]*\n)*", "", s, flags=re.M)

PROVIDERS = """proxy-providers:
  ytoo:
    type: file
    path: ./profiles/1.yaml
    health-check:
      enable: true
      url: http://www.gstatic.com/generate_204
      interval: 300
"""

s = s.rstrip() + "\n\n" + LISTENERS + "\n" + PROVIDERS + "\n" + GROUPS

# 4) 校验：YAML 合法 + 关键键各一份
d = yaml.safe_load(s)
for k in ("proxy-groups", "listeners", "proxy-providers"):
    assert k in d, k
assert len([1 for line in s.splitlines() if line.startswith("proxy-groups:")]) == 1
p.write_text(s, encoding="utf-8")
print("rewritten ok; groups in prefix:", len(d["proxy-groups"]["prefix"]))
