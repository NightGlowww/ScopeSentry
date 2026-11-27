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
# skip：需要额外凭据才能跑的检查。跳过不算失败（否则没配 key 的环境每次都是红的）
skip() { echo "   SKIP $1"; }
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
# 只断言缓存头不够：SPA 的 HTML 兜底也会回 no-store，所以必须同时确认状态码是 404
miss=$(curl -sS -m 20 -o /dev/null -D /tmp/cc_404h -w '%{http_code}' "$BASE/assets/gone-contract-check.js")
cc404=$(tr -d '\r' < /tmp/cc_404h | awk -F': ' 'tolower($1)=="cache-control"{print $2}')
if [ "$miss" = "404" ] && case "$cc404" in *no-store*) true ;; *) false ;; esac; then
  ok "asset 缺失 → 404 且 cache-control = $cc404"
else
  bad "asset 缺失 → status=$miss cache-control=$cc404（会被缓存一年）"
fi

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
# 6) 受限 key 的 tools/list 必须被裁剪：agent 不该看到自己调不动的工具。
#    顺带把「scope → 可见工具数」这条映射也验掉（asset:read 应为 22）。
created=$(curl -sS -m 20 -X POST "$BASE/api/mcp/keys" -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $TOKEN" -d '{"name":"contract-toolslist-probe","scope":["asset:read"],"tools":[],"rateLimitPerMin":0}')
probe_key=$(printf '%s' "$created" | python3 -c 'import json,sys;print(json.load(sys.stdin)["data"]["key"])' 2>/dev/null || echo "")
probe_id=$(printf '%s' "$created" | python3 -c 'import json,sys;print(json.load(sys.stdin)["data"]["id"])' 2>/dev/null || echo "")
if [ -n "$probe_key" ]; then
  visible=$(curl -sS -m 30 -X POST "$BASE/mcp" -H 'Content-Type: application/json' \
    -H 'Accept: application/json, text/event-stream' -H "X-API-Key: $probe_key" \
    -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}' \
    | sed -n 's/^data: //p;' | tail -1 \
    | python3 -c 'import json,sys;d=json.load(sys.stdin);print(len((d.get("result") or {}).get("tools") or []))' 2>/dev/null || echo 0)
  # 期望值不再写死数字：从 /api/mcp/tools 的 scope→工具映射里数出来。
  # 写死 22 的那版每加一个 asset:read 工具就要改脚本，很容易忘记（2026-11-28 加 list_port_dict 时踩到）。
  expect_read=$(printf '%s' "$tools" | python3 -c 'import json,sys;d=json.load(sys.stdin)["data"];print(sum(1 for t in d["list"] if t["scope"]=="asset:read"))' 2>/dev/null || echo x)
  if [ "${visible:-0}" = "${expect_read}" ] && [ "${expect_read}" != "x" ]; then
    ok "asset:read key tools/list 可见 ${visible} 个工具（与 scope 表一致）"
  else
    bad "asset:read key tools/list 可见 ${visible:-0} 个（scope 表算出 ${expect_read}）"
  fi
  # 资产总数两处一致：MCP count_assets 与仪表盘 stats（同一事实源被各算一遍，最容易一处悄悄归零）
  mcp_assets=$(curl -sS -m 30 -X POST "$BASE/mcp" -H 'Content-Type: application/json' \
    -H 'Accept: application/json, text/event-stream' -H "X-API-Key: $probe_key" \
    -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"count_assets","arguments":{"asset_type":"asset"}}}' \
    | sed -n 's/^data: //p;' | tail -1 \
    | python3 -c 'import json,sys;d=json.load(sys.stdin);print(json.loads(d["result"]["content"][0]["text"])["total"])' 2>/dev/null || echo x)
  dash_assets=$(curl -sS -m 30 -X POST "$BASE/api/dashboard/stats" -H 'Content-Type: application/json' \
    -H "Authorization: Bearer $TOKEN" -d '{}' \
    | python3 -c 'import json,sys;print((json.load(sys.stdin).get("data") or {}).get("asset"))' 2>/dev/null || echo x)
  if [ "$mcp_assets" = "$dash_assets" ] && [ "$mcp_assets" != "x" ] && [ "$mcp_assets" != "None" ]; then
    ok "资产总数一致：count_assets=$mcp_assets 仪表盘=$dash_assets"
  else
    bad "资产总数不一致：count_assets=$mcp_assets 仪表盘=$dash_assets"
  fi
  curl -sS -m 20 -X DELETE "$BASE/api/mcp/keys/$probe_id" -H "Authorization: Bearer $TOKEN" >/dev/null || true
else
  bad "无法创建探针 key（检查 SS_TOKEN 权限）"
fi

# 9) 节点数三处必须一致（同一事实源被不同端点各算一遍，最容易一处悄悄归零）
nodes_json=$(curl -sS -m 20 "$BASE/api/node" -H "Authorization: Bearer $TOKEN")
console_json=$(curl -sS -m 20 "$BASE/api/console/info" -H "Authorization: Bearer $TOKEN")
read api_n api_online con_n con_online <<EOF
$(printf '%s' "$nodes_json" | python3 -c 'import json,sys;d=json.load(sys.stdin)["data"];l=d.get("list") if isinstance(d,dict) else d;l=l or [];print(len(l), sum(1 for x in l if str(x.get("state"))=="1"))' 2>/dev/null || echo "x x") $(printf '%s' "$console_json" | python3 -c 'import json,sys;d=json.load(sys.stdin)["data"];print(d.get("nodeCount"), d.get("nodeOnline"))' 2>/dev/null || echo "x x")
EOF
if [ "$api_n" = "$con_n" ] && [ "$api_online" = "$con_online" ] && [ "$api_n" != "x" ]; then
  ok "节点口径一致：/api/node=$api_n（在线 $api_online） console/info=$con_n（在线 $con_online）"
else
  bad "节点口径不一致：/api/node=$api_n/$api_online console/info=$con_n/$con_online"
fi

# 10) 不存在的 id 不许演成「成功」：ICP / GitHub 的批量操作曾经一律 200 + 计数 0，
#     更糟的是 ICP stop 会给不存在的 id 写一条「收到停止请求」日志（随后 delete 报 deletedLogs=1）。
for pair in "icp/task/stop:{\"ids\":[\"$NOPE\"]}" "icp/task/delete:{\"ids\":[\"$NOPE\"]}" "github/task/delete:{\"ids\":[\"$NOPE\"]}"; do
  path=${pair%%:*}; body=${pair#*:}
  code=$(curl -sS -m 20 -o /dev/null -w '%{http_code}' -X POST "$BASE/api/$path" \
    -H 'Content-Type: application/json' -H "Authorization: Bearer $TOKEN" -d "$body")
  if [ "$code" = "404" ]; then ok "$path 不存在 id → 404"; else bad "$path 不存在 id → $code（应为 404）"; fi
done
# ICP 任务总数在两次删除不存在的 id 之后不许变化（幻影日志也一并被这条兜住）
icp_total=$(curl -sS -m 20 -X POST "$BASE/api/icp/task/overview_count" -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $TOKEN" -d '{}' | python3 -c 'import json,sys;print(json.load(sys.stdin)["data"]["total"])' 2>/dev/null || echo x)
curl -sS -m 20 -o /dev/null -X POST "$BASE/api/icp/task/delete" -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $TOKEN" -d "{\"ids\":[\"$NOPE\"]}" || true
icp_total2=$(curl -sS -m 20 -X POST "$BASE/api/icp/task/overview_count" -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $TOKEN" -d '{}' | python3 -c 'import json,sys;print(json.load(sys.stdin)["data"]["total"])' 2>/dev/null || echo x)
if [ "$icp_total" = "$icp_total2" ]; then ok "ICP 任务总数不受无效删除影响（$icp_total）"; else bad "ICP 任务总数被无效删除改变：$icp_total → $icp_total2"; fi

# 15) 一次 MCP 调用不能打死服务：未知资产 id 曾让 asset.GetAssetByID 解引用 nil，
#     而 MCP 工具跑在 jsonrpc2 自己的 goroutine 里，不在 gin.Recovery 管辖范围 —— 进程直接 SIGSEGV。
#     这里用「调用前后 uptime 不减」来判断进程有没有重启（比看 http 码更直接）。
up_before=$(curl -sS -m 20 "$BASE/api/console/info" -H "Authorization: Bearer $TOKEN" \
  | python3 -c 'import json,sys;print(json.load(sys.stdin)["data"]["uptimeSeconds"])' 2>/dev/null || echo x)
mcp_res=$(curl -sS -m 30 -X POST "$BASE/mcp" -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' -H "X-API-Key: ${MCP_KEY:-}" \
  -d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{\"name\":\"get_asset_detail\",\"arguments\":{\"asset_type\":\"asset\",\"id\":\"$NOPE\"}}}" \
  | sed -n 's/^data: //p' | tail -1)
if [ -n "${MCP_KEY:-}" ]; then
  err_flag=$(printf '%s' "$mcp_res" | python3 -c 'import json,sys;d=json.load(sys.stdin);print("err" if (d.get("result") or {}).get("isError") else "ok")' 2>/dev/null || echo x)
  up_after=$(curl -sS -m 20 "$BASE/api/console/info" -H "Authorization: Bearer $TOKEN" \
    | python3 -c 'import json,sys;print(json.load(sys.stdin)["data"]["uptimeSeconds"])' 2>/dev/null || echo x)
  if [ "$err_flag" = "err" ] && [ "$up_after" != "x" ] && [ "$up_before" != "x" ] && [ "$up_after" -ge "$up_before" ] 2>/dev/null; then
    ok "MCP get_asset_detail 未知 id → 工具级错误，进程未重启（uptime $up_before→$up_after）"
  else
    bad "MCP 未知 id 处理异常：isError=$err_flag uptime $up_before→$up_after（进程可能被杀）"
  fi
else
  skip "MCP 崩溃回归需要 SS_MCP_KEY=<ssk_...>；未提供，本次跳过"
fi

# 16) 策略 / POC 的不存在 id 也要 404（此前 200+deleted:0、500+mongo 文案）
for pair in "policy/delete:{\"ids\":[\"$NOPE\"]}" "poc/detail:{\"id\":\"$NOPE\"}"; do
  path=${pair%%:*}; body=${pair#*:}
  code=$(curl -sS -m 20 -o /dev/null -w '%{http_code}' -X POST "$BASE/api/$path" \
    -H 'Content-Type: application/json' -H "Authorization: Bearer $TOKEN" -d "$body")
  if [ "$code" = "404" ]; then ok "$path 不存在 id → 404"; else bad "$path 不存在 id → $code（应为 404）"; fi
done

# 18) 系统自检三件套：质量报告的结构必须齐全。
#     这些字段缺一个，状态页就少一块，而且不会报错——静默空白最难发现。
q=$(curl -sS -m 60 "$BASE/api/system/quality-report" -H "Authorization: Bearer $TOKEN")
mismatch=$(printf '%s' "$q" | python3 -c '
import json,sys
d=(json.load(sys.stdin).get("data") or {})
need=["alerts","evidence","llm","quarantine","tasks","vulnerabilities"]
miss=[k for k in need if k not in d]
print(",".join(miss))
' 2>/dev/null || echo parse-error)
if [ -z "$mismatch" ]; then ok "system/quality-report 六块结构齐全"; else bad "system/quality-report 缺字段：$mismatch"; fi

# 19) MCP 审计日志：空集也要给数组（同一批接口里 export/records 曾经回 null）
ml=$(curl -sS -m 30 "$BASE/api/mcp/logs?pageIndex=1&pageSize=1" -H "Authorization: Bearer $TOKEN")
mltype=$(printf '%s' "$ml" | python3 -c '
import json,sys
d=json.load(sys.stdin).get("data") or {}
v=d.get("list")
print("null" if v is None else type(v).__name__)
' 2>/dev/null || echo parse-error)
if [ "$mltype" = "list" ]; then ok "mcp/logs list 类型 = list"; else bad "mcp/logs list 类型 = $mltype（表格会崩）"; fi

# 20) 每个资产集合的列表都要自带 total，且必须和总数接口同口径
#     2026-11-28 之前只有部分集合回 total，前端另打一次 /api/assets/common/total，
#     而那次调用只在「搜索/筛选变化」时触发 —— 时间筛选变了 total 停在旧值。
cmp=$(python3 - "$BASE" "$TOKEN" <<'PY'
import json, sys, urllib.request
base, token = sys.argv[1], sys.argv[2]
def post(path, body):
    req = urllib.request.Request(base + path, data=json.dumps(body).encode(),
        headers={'Content-Type': 'application/json', 'Authorization': 'Bearer ' + token})
    return json.loads(urllib.request.urlopen(req, timeout=60).read().decode())
pairs = [('/api/assets/asset', 'asset'), ('/api/assets/crawler', 'crawler'),
         ('/api/assets/url', 'UrlScan'), ('/api/assets/sensitive', 'SensitiveResult'),
         ('/api/assets/dirscan', 'DirScanResult')]
bad = []
for path, index in pairs:
    body = {'pageIndex': 1, 'pageSize': 2, 'timeFrom': '2026-01-01 00:00:00'}
    a = (post(path, body).get('data') or {}).get('total')
    b = (post('/api/assets/common/total', dict(body, index=index)).get('data') or {}).get('total')
    if a is None or a != b:
        bad.append('%s: list=%s common=%s' % (index, a, b))
print('ok %d/%d' % (len(pairs) - len(bad), len(pairs)) if not bad else 'bad ' + '; '.join(bad))
PY
)
case "$cmp" in
  ok*) ok "列表 total 与总数接口一致（${cmp#ok } 个集合）" ;;
  *)   bad "列表 total 与总数接口不一致：${cmp#bad }" ;;
esac
# 21) 备份恢复是最危险的接口：必须三重把门 —— 缺 confirm / confirm 不对 / 归档不存在，
#     一律拒绝，且拒绝之后备份清单不能变。下载 404 也不能把服务器绝对路径吐给客户端。
bn=$(curl -sS -m 30 "$BASE/api/backup/list" -H "Authorization: Bearer $TOKEN" \
  | python3 -c 'import json,sys;d=json.load(sys.stdin).get("data") or [];print(len(d) if isinstance(d,list) else -1)' 2>/dev/null || echo x)
NONEXIST=backup-20200101-000000.tar.gz
r1=$(curl -sS -m 60 -o /dev/null -w '%{http_code}' -X POST "$BASE/api/backup/restore" \
  -H 'Content-Type: application/json' -H "Authorization: Bearer $TOKEN" -d "{\"name\":\"$NONEXIST\"}")
[ "$r1" = "400" ] && ok "backup/restore 缺 confirm → 400" || bad "backup/restore 缺 confirm → $r1（应为 400）"
r2=$(curl -sS -m 60 -o /dev/null -w '%{http_code}' -X POST "$BASE/api/backup/restore" \
  -H 'Content-Type: application/json' -H "Authorization: Bearer $TOKEN" -d "{\"name\":\"$NONEXIST\",\"confirm\":\"restore\"}")
[ "$r2" = "400" ] && ok "backup/restore confirm 大小写不符 → 400" || bad "backup/restore confirm 大小写不符 → $r2（应为 400）"
r3=$(curl -sS -m 60 -o /dev/null -w '%{http_code}' -X POST "$BASE/api/backup/restore" \
  -H 'Content-Type: application/json' -H "Authorization: Bearer $TOKEN" -d "{\"name\":\"$NONEXIST\",\"confirm\":\"RESTORE\"}")
[ "$r3" = "404" ] && ok "backup/restore 归档不存在 → 404（不会空跑一次恢复）" || bad "backup/restore 归档不存在 → $r3（应为 404）"
r4=$(curl -sS -m 60 -o /dev/null -w '%{http_code}' -X POST "$BASE/api/backup/restore" \
  -H 'Content-Type: application/json' -H "Authorization: Bearer $TOKEN" -d '{"name":"../x.tar.gz","confirm":"RESTORE"}')
[ "$r4" = "400" ] && ok "backup/restore 路径穿越名 → 400" || bad "backup/restore 路径穿越名 → $r4（应为 400）"
dl=$(curl -sS -m 30 "$BASE/api/backup/download/$NONEXIST" -H "Authorization: Bearer $TOKEN")
if printf '%s' "$dl" | grep -q '"/data/\|/scopesentry-backups/'; then
  bad "backup/download 404 泄露了服务器绝对路径：$(printf '%s' "$dl" | head -c 80)"
else
  ok "backup/download 404 不回服务器绝对路径"
fi
bn2=$(curl -sS -m 30 "$BASE/api/backup/list" -H "Authorization: Bearer $TOKEN" \
  | python3 -c 'import json,sys;d=json.load(sys.stdin).get("data") or [];print(len(d) if isinstance(d,list) else -1)' 2>/dev/null || echo x)
if [ "$bn" = "$bn2" ]; then ok "无效恢复请求后备份份数不变（$bn）"; else bad "备份份数被改动：$bn → $bn2"; fi
# 22) 导出链路：预览要给出集合/字段/总数；批量导出的入参校验必须在下发任何字节之前
#     （writeDownloadHeaders 一旦跑过，状态码就改不了，只能吐出半截 CSV）。
exp=$(python3 - "$BASE" "$TOKEN" <<'PY'
import json, sys, urllib.request, urllib.error
base, token = sys.argv[1], sys.argv[2]
def post(path, body):
    req = urllib.request.Request(base + path, data=json.dumps(body).encode(),
        headers={'Content-Type': 'application/json', 'Authorization': 'Bearer ' + token})
    try:
        return json.loads(urllib.request.urlopen(req, timeout=90).read().decode())
    except urllib.error.HTTPError as e:
        try:
            return json.loads(e.read().decode() or '{}')
        except Exception:
            return {}
out = []
d = (post('/api/export/preview', {'type': 'domain'}).get('data') or {})
if d.get('collection') == 'subdomain' and d.get('fields') and (d.get('total') or 0) > 0:
    out.append('OK export/preview(domain) 集合=%s 字段=%d 总数=%s' % (d.get('collection'), len(d.get('fields')), d.get('total')))
else:
    out.append('BAD export/preview(domain) 形状异常: %s' % json.dumps(d, ensure_ascii=False)[:120])
b1 = post('/api/export/batch', {'type': 'domain', 'fields': ['domain']})
out.append('OK export/batch 非法字段 → 400 并列出可用字段' if 'is not a field of' in json.dumps(b1, ensure_ascii=False)
           else 'BAD export/batch 非法字段没被拦下: %s' % json.dumps(b1, ensure_ascii=False)[:100])
b2 = post('/api/export/batch', {'type': 'nope', 'fields': ['x']})
out.append('OK export/batch 非法类型 → 400 并列出支持的 type' if 'unknown export type' in json.dumps(b2, ensure_ascii=False)
           else 'BAD export/batch 非法类型没被拦下: %s' % json.dumps(b2, ensure_ascii=False)[:100])
print(chr(10).join(out))
PY
)
while IFS= read -r line; do
  case "$line" in
    OK\ *) ok "${line#OK }" ;;
    BAD\ *) bad "${line#BAD }" ;;
    "") ;;
    *) bad "export 检查输出异常：$line" ;;
  esac
done <<< "$exp"

# 23) 导出记录链路（创建 → 列表 → 下载 → 删除）默认跳过：它会真的落一个文件。
#     这条路以前没人跑过（records 一直是空），而它和流式 batch 是两套代码。
if [ "${SS_EXPORT_ROUNDTRIP:-}" = "1" ]; then
  exprec=$(python3 - "$BASE" "$TOKEN" <<'PY'
import json, sys, io, zipfile, urllib.request, urllib.error
base, token = sys.argv[1], sys.argv[2]
def call(method, path, body=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(base + path, data=data, method=method,
        headers={'Content-Type': 'application/json', 'Authorization': 'Bearer ' + token})
    try:
        r = urllib.request.urlopen(req, timeout=180)
        return r.getcode(), r.read()
    except urllib.error.HTTPError as e:
        return e.code, e.read()
out = []
code, _ = call('POST', '/api/export', {'index': 'vulnerability', 'quantity': 2, 'type': 'search',
                                        'filetype': 'xlsx', 'field': ['url', 'level']})
out.append('OK 导出记录：创建接口 200' if code == 200 else 'BAD 创建导出异常 HTTP %s' % code)
_, raw = call('GET', '/api/export/records')
lst = (json.loads(raw.decode() or '{}').get('data') or {}).get('list') or []
name = None
for x in lst:
    if x.get('file_type') == 'xlsx' and not x.get('file_missing'):
        name = x.get('file_name'); break
out.append('OK 导出记录：列表里能看到刚建的文件' if name else 'BAD 导出记录列表里找不到刚建的文件')
if name:
    c, body = call('GET', '/api/export/download?filename=' + name)
    ok = False
    if c == 200 and body[:2] == b'PK':
        try:
            z = zipfile.ZipFile(io.BytesIO(body))
            ok = any(n.startswith('xl/worksheets/sheet') for n in z.namelist())
        except Exception:
            ok = False
    out.append('OK 导出记录：下载得到可解的 xlsx（%d 字节）' % len(body) if ok else 'BAD 下载的 xlsx 不是有效 zip/缺少工作表')
    c, _ = call('POST', '/api/export/delete', {'ids': [name]})
    out.append('OK 导出记录：删除 200' if c == 200 else 'BAD 删除导出记录 HTTP %s' % c)
    _, raw2 = call('GET', '/api/export/records')
    left = (json.loads(raw2.decode() or '{}').get('data') or {}).get('list') or []
    out.append('OK 导出记录：删除后清单为 0（清理干净）' if not left else 'BAD 删除后仍有 %d 条记录' % len(left))
print(chr(10).join(out))
PY
  )
  while IFS= read -r line; do
    case "$line" in
      OK\ *) ok "${line#OK }" ;;
      BAD\ *) bad "${line#BAD }" ;;
      "") ;;
      *) bad "导出记录检查输出异常：$line" ;;
    esac
  done <<< "$exprec"
else
  skip "导出记录链路需要 SS_EXPORT_ROUNDTRIP=1；默认跳过（会创建并删除一条导出记录）"
fi

# 24) MCP 治理三件套：写错字段名不能静默发全权 key；工具级白名单要真的拦；key 级限速要真的 429。
#     这三条都是「看起来配了、实际没生效」的高危面。脚本自己建 key，finally 里撤销。
gov=$(python3 - "$BASE" "$TOKEN" <<'PY'
import json, sys, urllib.request, urllib.error
base, token = sys.argv[1], sys.argv[2]
def call(method, path, body=None, key=None):
    data = json.dumps(body).encode() if body is not None else None
    h = {'Content-Type': 'application/json', 'Authorization': 'Bearer ' + token}
    if key:
        h = {'Content-Type': 'application/json', 'Accept': 'application/json, text/event-stream', 'X-API-Key': key}
    req = urllib.request.Request(base + path, data=data, method=method, headers=h)
    try:
        r = urllib.request.urlopen(req, timeout=60)
        return r.getcode(), r.read()
    except urllib.error.HTTPError as e:
        return e.code, e.read()
def mkkey(name, **kw):
    body = {'name': name}; body.update(kw)
    c, raw = call('POST', '/api/mcp/keys', body)
    try: d = json.loads(raw)
    except Exception: d = {}
    return c, d
def rpc(key, tool, args):
    body = {'jsonrpc': '2.0', 'id': 1, 'method': 'tools/call', 'params': {'name': tool, 'arguments': args}}
    c, raw = call('POST', '/mcp', body, key=key)
    txt = raw.decode('utf-8', 'replace').strip()
    # 服务端可能回 SSE（event: message / data: {...}），取最后一条 data 行再解析
    if 'data: ' in txt:
        txt = [l[6:] for l in txt.splitlines() if l.startswith('data: ')][-1]
    try: d = json.loads(txt)
    except Exception: d = {}
    return c, d
out = []
created = []
try:
    # 1. 未知字段必须 400（以前会静默发一个 scope=["*"] 的全权 key）
    c, d = mkkey('r24-badfield', allowed_tools=['list_assets'])
    if c == 400 and 'unknown field' in json.dumps(d):
        out.append('OK 建 key 时未知字段 → 400，不再静默发全权 key')
    else:
        out.append('BAD 未知字段没被拒：HTTP %s %s' % (c, json.dumps(d, ensure_ascii=False)[:90]))
        if (d.get('data') or {}).get('id'): created.append(d['data']['id'])
    # 2. 工具白名单：delete_task 必须被拦
    c, d = mkkey('r24-allow', tools=['list_assets'])
    kd = d.get('data') or {}
    if kd.get('id'): created.append(kd['id'])
    key = kd.get('key')
    if key:
        c1, d1 = rpc(key, 'list_assets', {'asset_type': 'asset', 'pageIndex': 1, 'pageSize': 1})
        c2, d2 = rpc(key, 'delete_task', {'ids': ['000000000000000000000000']})
        denied = 'tool not allowed' in json.dumps(d2)
        allowed = 'result' in d1
        out.append('OK 工具白名单：授权工具可用、越权工具被拒（%s）' % 'tool not allowed'
                   if allowed and denied else
                   'BAD 工具白名单没生效：allow=%s deny=%s' % (allowed, denied))
    else:
        out.append('BAD 受限 key 创建失败，无法验证白名单：%s' % json.dumps(d, ensure_ascii=False)[:90])
    # 3. key 级限速：limit=1，第二次必须 429
    c, d = mkkey('r24-rate', tools=['list_nodes'], rateLimitPerMin=1)
    kd = d.get('data') or {}
    if kd.get('id'): created.append(kd['id'])
    rk = kd.get('key')
    if rk:
        rpc(rk, 'list_nodes', {})
        c3, d3 = rpc(rk, 'list_nodes', {})
        body = json.dumps(d3)
        out.append('OK key 级限速：第二次调用 → 429（-32029）' if c3 == 429 and '-32029' in body
                   else 'BAD 限速没生效：HTTP %s %s' % (c3, body[:90]))
    else:
        out.append('BAD 限速 key 创建失败')
finally:
    for kid in created:
        call('DELETE', '/api/mcp/keys/' + kid)
print(chr(10).join(out))
PY
)
while IFS= read -r line; do
  case "$line" in
    OK\ *) ok "${line#OK }" ;;
    BAD\ *) bad "${line#BAD }" ;;
    "") ;;
    *) bad "MCP 治理检查输出异常：$line" ;;
  esac
done <<< "$gov"

# 25) 项目名称必填：空名字不写库（否则列表里出现一张没有标题的卡片）。
#     注意只测「空/空白」——重名用例会真的建出一个项目，不能放进自动化。
pj=$(python3 - "$BASE" "$TOKEN" <<'PY'
import json, sys, urllib.request, urllib.error
base, token = sys.argv[1], sys.argv[2]
def post(path, body):
    req = urllib.request.Request(base + path, data=json.dumps(body).encode(), method='POST',
        headers={'Content-Type': 'application/json', 'Authorization': 'Bearer ' + token})
    try:
        r = urllib.request.urlopen(req, timeout=60)
        return r.getcode(), json.loads(r.read().decode() or '{}')
    except urllib.error.HTTPError as e:
        try:
            return e.code, json.loads(e.read().decode() or '{}')
        except Exception:
            return e.code, {}
out = []
for body in ({}, {'name': ''}, {'name': '   '}):
    c, d = post('/api/project/add', body)
    out.append('OK 项目名称为空被拒（%s）' % json.dumps(body, ensure_ascii=False)
               if c == 400 else 'BAD 空名称没被拒：%s -> %s' % (json.dumps(body, ensure_ascii=False), c))
_, allp = post('/api/project/data', {'pageIndex': 1, 'pageSize': 50})
res = ((allp.get('data') or {}).get('result') or {})
projs = {p.get('id'): p for grp in res.values() for p in grp if p.get('id')}
names = [p.get('name') for p in projs.values()]
bad = [n for n in names if not (n or '').strip()]
out.append('OK 项目列表里没有空标题卡片（去重后 %d 个）' % len(names) if not bad
           else 'BAD 列表里出现空标题项目：%s' % bad)
print(chr(10).join(out))
PY
)
while IFS= read -r line; do
  case "$line" in
    OK\ *) ok "${line#OK }" ;;
    BAD\ *) bad "${line#BAD }" ;;
    "") ;;
    *) bad "项目名校验输出异常：$line" ;;
  esac
done <<< "$pj"
if [ "$FAIL" = "0" ]; then echo "== 契约全过 =="; echo "CONTRACT_RESULT=PASS"; else echo "== 契约失败 =="; echo "CONTRACT_RESULT=FAIL"; fi
exit $FAIL
