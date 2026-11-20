#!/usr/bin/env bash
# 端到端冒烟入口：把 scripts/remote/e2e-smoke.py 送到生产机执行。
# 覆盖链路：建任务 → 轮询完成（校验进度不回退）→ 结果 → 处置 → 统计 → 导出 → 校验行数。
# 用法:
#   scripts/e2e-smoke.sh              # 跑一次
#   SMOKE=1 scripts/release.sh 2.0.x  # 发版后自动跑（约 5 分钟）
set -euo pipefail
ROOT="$(cd -- "$(dirname -- "$0")/.." >/dev/null 2>&1 && pwd -P)"
PROD_HOST="${PROD_HOST:-prod-tx-sh-app-01}"
scp -q -o BatchMode=yes "$ROOT/scripts/remote/e2e-smoke.py" "$PROD_HOST:/opt/scopesentry/e2e-smoke.py"
ssh -o BatchMode=yes "$PROD_HOST" "python3 -u /opt/scopesentry/e2e-smoke.py"