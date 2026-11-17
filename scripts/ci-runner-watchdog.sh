#!/usr/bin/env bash
# CI runner watchdog：自建 runner 与 GitHub 的连接会间歇性断流（代理抖动），
# 表现为 runner 显示 offline，或 busy=true 但本机没有 Runner.Worker 进程。
# 只做两件事：服务不 active 就拉起；连接连续不健康就重启。
set -euo pipefail

SERVICE="${RUNNER_SERVICE:-actions.runner.NightGlowww-ScopeSentry.dev-tx-sh-app-01-ss.service}"
STRIKES_FILE="${STRIKES_FILE:-/tmp/ci-runner-watchdog.strikes}"
MAX_STRIKES="${MAX_STRIKES:-3}"

log() { echo "[$(date -Is)] $*"; }

if ! systemctl is-active --quiet "$SERVICE"; then
  log "service inactive -> restart"
  sudo systemctl restart "$SERVICE"
  rm -f "$STRIKES_FILE"
  exit 0
fi

# 正在跑 job 就不动它（restart 会杀掉 job）
if pgrep -f "actions-runner-ss/bin/Runner.Worker" >/dev/null 2>&1; then
  rm -f "$STRIKES_FILE"
  exit 0
fi

# 没有 worker 时检查 listener 是否还与 GitHub 保持着 TCP 连接
CONNS=$(ss -tnp 2>/dev/null | grep -c "actions-runner-ss/bin/Runner.Listener" || true)
if [ "${CONNS:-0}" -gt 0 ]; then
  rm -f "$STRIKES_FILE"
  exit 0
fi

STRIKES=$(( $(cat "$STRIKES_FILE" 2>/dev/null || echo 0) + 1 ))
echo "$STRIKES" > "$STRIKES_FILE"
log "listener has no GitHub connection (strike $STRIKES/$MAX_STRIKES)"
if [ "$STRIKES" -ge "$MAX_STRIKES" ]; then
  log "restarting runner after $STRIKES strikes"
  sudo systemctl restart "$SERVICE"
  rm -f "$STRIKES_FILE"
fi
