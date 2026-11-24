#!/usr/bin/env bash
# 离线预检：发版前把能在本机验证的全跑一遍（不需要生产）。
# 用法: scripts/preflight.sh [--with-frontend]
set -uo pipefail
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." >/dev/null 2>&1 && pwd -P)"
cd "$ROOT"
fail=0
step() { echo "== $1"; }

step "[1/5] go build"
GOPROXY=https://goproxy.cn,direct go build ./... || { echo "   ✗ build failed"; fail=1; }

step "[2/5] go vet"
GOPROXY=https://goproxy.cn,direct go vet ./... 2>&1 | head -10 || true

step "[3/5] 单元测试（无 DB 依赖的包）"
GOPROXY=https://goproxy.cn,direct go test -count=1   ./internal/utils/diskguard/... ./internal/utils/hoststat/...   ./internal/api/handlers/health/... ./internal/services/mcpguard/...   ./internal/services/task/taskpolicy/... ./internal/services/task/taskstate/...   ./internal/services/task/tasksnapshot/... ./internal/services/task/taskmsg/...   ./internal/services/dashboard/...   ./internal/services/vulnverify/... 2>&1 | tail -12 || fail=1

step "[4/5] UI 审计门禁"
python3 scripts/check-ui-audit.py docs/ui-audit-baseline.json || fail=1

step "[5/5] staticcheck（可选）"
if [[ -x "$HOME/go/bin/staticcheck" ]]; then
  "$HOME/go/bin/staticcheck" ./internal/... 2>&1 | grep -E "\(SA[0-9]+\)" | grep -v "internal/symbols/" | head -10 || true
else
  echo "   未安装 staticcheck，跳过（go install honnef.co/go/tools/cmd/staticcheck@latest）"
fi

if [[ "${1:-}" == "--with-frontend" ]]; then
  step "[额外] 前端构建"
  ( cd frontend && pnpm build:pro >/tmp/fe_build.log 2>&1 ) && echo "   ok" || { echo "   ✗ 前端构建失败，见 /tmp/fe_build.log"; fail=1; }
fi

if [[ $fail -ne 0 ]]; then
  echo "✗ 预检未通过"
  exit 1
fi
echo "✅ 预检通过"
