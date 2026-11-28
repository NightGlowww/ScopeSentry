#!/usr/bin/env bash
# 前端类型检查门禁：`pnpm ts:check`（vue-tsc）必须 0 错。
#
# 为什么要单独一条：`build:pro` 走 vite，vite 只转译不做类型检查，
# 所以类型错误在打包产物里完全看不出来（历史上攒到 143 条）。
# 2026-11-28 归零之后，这里就是硬门禁：
#   TYPECHECK=PASS  → 可以合
#   TYPECHECK=FAIL  → 输出前 40 行错误，直接看
#
# 用法：bash scripts/ci/typecheck.sh
set -uo pipefail
cd "$(dirname "$0")/../.." || exit 2
if [ ! -d frontend/node_modules ]; then
  echo "TYPECHECK=SKIP（frontend/node_modules 不存在，先 pnpm install）"
  exit 0
fi
out=$(cd frontend && pnpm -s ts:check 2>&1)
if printf '%s' "$out" | grep -q 'error TS'; then
  printf '%s\n' "$out" | grep 'error TS' | head -40
  echo "TYPECHECK=FAIL（$(printf '%s' "$out" | grep -c 'error TS') 条）"
  exit 1
fi
echo "TYPECHECK=PASS（0 条）"
