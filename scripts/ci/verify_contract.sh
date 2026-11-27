#!/usr/bin/env bash
# 契约核对：把「这一轮轮手工验过的不变量」固化成可重复脚本。
#
# 为什么需要：分页上限、404 语义、静态缓存头、空集返回 [] 这些点，
# 每次改动都可能悄悄回归，而冒烟脚本只查 healthz/版本/Mongo/节点。
#
# 用法：SS_BASE=http://host:8082 SS_TOKEN=<jwt> bash scripts/ci/verify_contract.sh
set -uo pipefail
BASE=${SS_BASE:-http://127.0.0.1:8082}
TOKEN=${SS_TOKEN:-}
FAIL=0
NOPE=000000000000000000000000

ok()   { echo "   OK   $1"; }
bad()  { echo "   FAIL $1"; FAIL=1; }
need() { local name="$1" got="$2"; if [ -n "$got" ]; then ok "$name = $got"; else bad "$name 为空"; fi }

postj() { # path body -> body
  curl -sS -m 30 -X POST "$BASE$1" -H 'Content-Type: application/json' \
    -H "Authorization: Bearer $TOKEN" -d "$2"
}

echo "== 契约核对 ($BASE) =="

# 1) 静态资源缓存头：存在=immutable，缺失=no-store
entry=$(curl -sS -m 20 "$BASE/" | grep -oE 'index-[A-Za-z0-9_-]+\.js' | head -1)
if [ -n "$entry" ]; then
  cc=$(curl -sSI -m 20 "$BASE/assets/$entry" | tr -d '\r' | awk -F': ' 'tolower($1)=="cache-control"{print $2}')
  case "$cc" in *immutable*) ok "asset 200 cache-control = $cc" ;; *) bad "asset 200 cache-control = ${cc:-空}" ;; esac
else
  bad "找不到入口 chunk"
fi
cc404=$(curl -sSI -m 20 "$BASE/assets/gone-contract-check.js" | tr -d '\r' | awk -F': ' 'tolower($1)=="cache-control"{print $2}')
case "$cc404" in *no-store*) ok "asset 404 cache-control = $cc404" ;; *) bad "asset 404 cache-control = ${cc404:-空}（会被缓存一年）" ;; esac

# 2) 删除类接口对不存在的 id 必须是 404（不是 200 成功）
code=$(curl -sS -m 20 -o /dev/null -w '%{http_code}' -X POST "$BASE/api/task/delete" \
  -H 'Content-Type: application/json' -H "Authorization: Bearer $TOKEN" -d "{\"ids\":[\"$NOPE\"]}")
[ "$code" = "404" ] && ok "task/delete 不存在 → 404" || bad "task/delete 不存在 → $code（应为 404）"
code=$(curl -sS -m 20 -o /dev/null -w '%{http_code}' -X POST "$BASE/api/node/restart" \
  -H 'Content-Type: application/json' -H "Authorization: Bearer $TOKEN" -d '{"name":"__contract_nope__"}')
[ "$code" = "404" ] && ok "node/restart 不存在 → 404" || bad "node/restart 不存在 → $code（应为 404）"

# 3) 分页硬上限：pageSize=100000 必须被收敛到 5000 以内并带回执头
hdr=$(curl -sS -m 60 -D /tmp/cc_h -o /tmp/cc_b -X POST "$BASE/api/assets/asset" \
  -H 'Content-Type: application/json' -H "Authorization: Bearer $TOKEN" -d '{"pageIndex":1,"pageSize":100000}')
rows=$(python3 -c 'import json;print(len((json.load(open("/tmp/cc_b")).get("data") or {}).get("list") or []))' 2>/dev/null || echo 0)
clamp=$(tr -d '\r' < /tmp/cc_h | awk -F': ' 'tolower($1)=="x-pagesize-clamped"{print $2}')
if [ "${rows:-0}" -le 5000 ] 2>/dev/null && [ "${rows:-0}" -gt 0 ]; then ok "pageSize=100000 收敛后行数 = $rows（回执 $clamp）"; else bad "pageSize 收敛异常：rows=${rows:-0} clamp=${clamp:-无}"; fi

# 4) MCP 工具↔scope 对照表可用且覆盖全部工具
tools=$(curl -sS -m 30 "$BASE/api/mcp/tools" -H "Authorization: Bearer $TOKEN")
total=$(printf '%s' "$tools" | python3 -c 'import json,sys;d=json.load(sys.stdin)["data"];print(d["total"])' 2>/dev/null || echo 0)
unscoped=$(printf '%s' "$tools" | python3 -c 'import json,sys;d=json.load(sys.stdin)["data"];print(sum(1 for t in d["list"] if not t["scope"]))' 2>/dev/null || echo 1)
if [ "${total:-0}" -ge 60 ] 2>/dev/null && [ "${unscoped:-1}" = "0" ]; then ok "MCP 工具 $total 个，全部登记了 scope"; else bad "MCP 工具表异常：total=${total:-0} unscoped=${unscoped:-?}"; fi

# 5) 清空后的列表接口必须回 [] 而不是 null
shape=$(curl -sS -m 20 "$BASE/api/export/records" -H "Authorization: Bearer $TOKEN" | python3 -c 'import json,sys;d=json.load(sys.stdin)["data"];print(type(d["list"]).__name__)' 2>/dev/null || echo err)
[ "$shape" = "list" ] && ok "export/records list 类型 = list" || bad "export/records list 类型 = ${shape:-err}"

if [ "$FAIL" = "0" ]; then echo "== 契约全过 =="; echo "CONTRACT_RESULT=PASS"; else echo "== 契约失败 =="; echo "CONTRACT_RESULT=FAIL"; fi
exit $FAIL
