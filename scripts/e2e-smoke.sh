#!/usr/bin/env bash
# 部署后冒烟：登录 → 仪表盘 → 策略 → 资产集团/地契 → 读模型 → 导出 → ICP provider。
# 用法：BASE_URL=http://127.0.0.1:8082 ADMIN_USER=xxx ADMIN_PASS=yyy ./scripts/e2e-smoke.sh
# 退出码非 0 即失败。
set -euo pipefail

BASE_URL="${BASE_URL:-http://127.0.0.1:8082}"
ADMIN_USER="${ADMIN_USER:?ADMIN_USER required}"
ADMIN_PASS="${ADMIN_PASS:?ADMIN_PASS required}"
PREFIX="e2e-smoke-$(date +%s)"

pass=0; fail=0
ok()   { echo "PASS  $*"; pass=$((pass+1)); }
bad()  { echo "FAIL  $*"; fail=$((fail+1)); }
json() { python3 -c "import sys,json;d=json.load(sys.stdin);print(eval(sys.argv[1],{\"d\":d}))" "$1" 2>/dev/null || true; }
post() { curl -s -m 20 -X POST "$BASE_URL$1" -H "Content-Type: application/json" -H "Authorization: Bearer $TOKEN" -d "${2:-{\}}"; }

LOGIN=$(curl -s -m 20 -X POST "$BASE_URL/api/user/login" -H "Content-Type: application/json" -d "{\"username\":\"$ADMIN_USER\",\"password\":\"$ADMIN_PASS\"}")
TOKEN=$(echo "$LOGIN" | json "d['data']['access_token']")
[ -n "$TOKEN" ] && ok "login" || { bad "login: $LOGIN"; exit 1; }

STATS=$(post /api/dashboard/stats "{}")
ASSET=$(echo "$STATS" | json "d['data']['asset']")
[ -n "$ASSET" ] && ok "dashboard/stats asset=$ASSET" || bad "dashboard/stats: $STATS"

POLICY=$(post /api/policy/create "{\"name\":\"$PREFIX-policy\",\"config\":{\"threadNum\":10,\"rateLimit\":50,\"timeoutSeconds\":60}}")
POLICY_ID=$(echo "$POLICY" | json "d['data']['id']")
[ -n "$POLICY_ID" ] && ok "policy/create" || bad "policy/create: $POLICY"
if [ -n "$POLICY_ID" ]; then
  TEST=$(post /api/policy/test "{\"config\":{\"threadNum\":500}}")
  echo "$TEST" | grep -q "false" && ok "policy/test rejects threadNum=500" || bad "policy/test: $TEST"
  APPLY=$(post /api/policy/apply "{\"policyId\":\"$POLICY_ID\",\"taskTemplate\":{\"threadNum\":5}}")
  echo "$APPLY" | grep -q "thread_num_override" && ok "policy/apply warning" || bad "policy/apply: $APPLY"
  post /api/policy/delete "{\"id\":\"$POLICY_ID\"}" >/dev/null && ok "policy/delete" || bad "policy/delete"
fi

GROUP=$(post /api/assets/asset_group/add "{\"name\":\"$PREFIX-group\"}")
GID=$(echo "$GROUP" | json "d['data']['id']")
[ -n "$GID" ] && ok "asset_group/add" || bad "asset_group/add: $GROUP"
SCOPE=$(post /api/assets/asset_scope/add "{\"name\":\"$PREFIX-scope\",\"scope\":\"a.example.com\\nb.example.com\\n10.0.0.1\",\"groupId\":\"$GID\"}")
SID=$(echo "$SCOPE" | json "d['data']['id']")
[ -n "$SID" ] && ok "asset_scope/add (mixed)" || bad "asset_scope/add: $SCOPE"
LIST=$(post /api/assets/asset_scope/list "{\"groupId\":\"$GID\"}")
echo "$LIST" | grep -q "10.0.0.1" && ok "asset_scope/list ip_array" || bad "asset_scope/list: $LIST"

for ep in site ip cert; do
  R=$(post "/api/assets/$ep/list" "{}")
  [ -n "$R" ] && ok "$ep/list" || bad "$ep/list: $R"
done
OV=$(post /api/assets/asset_overview/count "{}"); [ -n "$OV" ] && ok "asset_overview/count" || bad "asset_overview/count: $OV"
EXP=$(post /api/export/preview "{\"type\":\"asset_site\"}"); [ -n "$EXP" ] && ok "export/preview" || bad "export/preview: $EXP"
TYC=$(post /api/icp/tyc_check "{}")
echo "$TYC" | grep -q "provider" && ok "icp/tyc_check" || bad "icp/tyc_check: $TYC"

if [ -n "$SID" ]; then post /api/assets/asset_scope/delete "{\"ids\":[\"$SID\"]}" >/dev/null || true; fi
if [ -n "$GID" ]; then post /api/assets/asset_group/delete "{\"id\":\"$GID\",\"action\":\"dissolve\"}" >/dev/null || true; fi

echo "----------------------------------------"
echo "PASS=$pass FAIL=$fail"
[ "$fail" -eq 0 ]
