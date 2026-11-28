#!/usr/bin/env bash
# 一键本地门禁：不碰线上数据，全部可重复。线上契约用 scripts/ci/verify_contract.sh（需要 SS_BASE/SS_TOKEN）。
#
# 每条门禁的契约只有一条：最后一行必须出现 =PASS。没有就打印输出尾部并以 1 退出，
# 避免「脚本跑失败但汇总看起来是绿的」这种最糟的情况。
#
# 用法：bash scripts/ci/verify_all.sh
set -uo pipefail
cd "$(dirname "$0")/../.." || exit 2
FAIL=0

gate() {
  local name="$1"; shift
  local out
  out=$("$@" 2>&1)
  if printf '%s' "$out" | grep -q '=PASS'; then
    printf '  OK   %-18s %s\n' "$name" "$(printf '%s' "$out" | grep '=PASS' | tail -1)"
  else
    printf '  FAIL %-18s\n' "$name"
    printf '%s\n' "$out" | tail -12 | sed 's/^/        /'
    FAIL=1
  fi
}

echo '== 本地门禁 =='
gate typecheck    bash scripts/ci/typecheck.sh
gate i18n_keys    node scripts/ci/i18n_keys.js
gate i18n_hardcode python3 scripts/ci/i18n_guard.py
gate feedback     python3 scripts/ci/feedback_guard.py
gate payload      python3 scripts/ci/frontend_payload_contract.py
gate scope_drift  python3 scripts/ci/mcp_scope_drift.py
gate mcp_closure  python3 scripts/ci/mcp_closure.py --strict
# jsonschema tag 里的等号会让 MCP 工具静默消失（2026-11-29 实测），先扫出来再说
python3 scripts/ci/jsonschema_tag_check.py | sed 's/^/  /'

# Go 侧：能编译 + vet 干净（不跑需要 Mongo 的集成测试）
GO_OUT=$(go build ./... 2>&1); GO_RC=$?
GO_OUT2=$(go vet ./internal/mcp/... ./internal/services/maintenance/... ./internal/services/task/taskstats/... 2>&1); GO_RC2=$?
if [ "$GO_RC" = "0" ] && [ "$GO_RC2" = "0" ]; then
  printf '  OK   %-18s go build + vet\n' go
else
  printf '  FAIL %-18s go build/vet\n' go
  printf '%s\n%s\n' "$GO_OUT" "$GO_OUT2" | tail -12 | sed 's/^/        /'
  FAIL=1
fi

# 列字段核对需要线上读权限：没给凭据就跳过，不算失败
if [ -n "${SS_TOKEN:-}" ]; then
  gate column_field python3 scripts/ci/column_field_check.py
else
  printf '  SKIP %-18s 需要 SS_BASE + SS_TOKEN\n' column_field
fi

# MCP 输出契约：读类工具「返回的东西和库里一致」——需要真实 key，没有就跳过
if [ -n "${SS_MCP_KEY:-}" ]; then
  gate mcp_output python3 scripts/ci/mcp_output_contract.py
else
  printf '  SKIP %-18s 需要 SS_BASE + SS_MCP_KEY\n' mcp_output
fi

printf '  SKIP %-18s 需要真实 Mongo/Redis：bash scripts/ci/integration.sh\n' integration

if [ "$FAIL" = "0" ]; then echo 'VERIFY_ALL=PASS'; else echo 'VERIFY_ALL=FAIL'; fi
exit $FAIL