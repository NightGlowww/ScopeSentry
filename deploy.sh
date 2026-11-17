#!/usr/bin/env bash
# ScopeSentry 一键部署脚本（issue #321）
#
# 用法:
#   ./deploy.sh            # 标准部署（Docker Hub 镜像）
#   ./deploy.sh --cn       # 国内镜像覆盖（single-host-deployment.cn.yml）
#   ./deploy.sh --check    # 只检查环境并校验 compose，不启动
#   ./deploy.sh --help     # 查看用法
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT"

USE_CN=0
CHECK_ONLY=0
for arg in "$@"; do
  case "$arg" in
    --cn) USE_CN=1 ;;
    --check) CHECK_ONLY=1 ;;
    -h|--help) sed -n '2,8p' "$0"; exit 0 ;;
    *) echo "未知参数: $arg（--help 查看用法）" >&2; exit 2 ;;
  esac
done

log() { printf '\033[1;34m[deploy]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[warn]\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m[error]\033[0m %s\n' "$*" >&2; exit 1; }

COMPOSE=(docker compose -f single-host-deployment.yml)
if [ "$USE_CN" = 1 ]; then
  COMPOSE+=(-f single-host-deployment.cn.yml)
fi

gen_password() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 16
  else
    head -c 16 /dev/urandom | od -An -tx1 | tr -d ' \n'
  fi
}

require_docker() {
  command -v docker >/dev/null 2>&1 || fail "未找到 docker，请先安装 Docker 20.10+"
  docker info >/dev/null 2>&1 || fail "docker 守护进程不可用（权限不足？试试 sudo 或把用户加入 docker 组）"
}

require_compose() {
  docker compose version >/dev/null 2>&1 || fail "未找到 docker compose v2（v1 不支持 condition: service_healthy）"
}

ensure_env() {
  if [ ! -f .env ]; then
    log "未找到 .env，自动生成一份随机密码配置"
    umask 077
    {
      echo "MONGO_INITDB_ROOT_USERNAME=admin"
      echo "MONGO_INITDB_ROOT_PASSWORD=$(gen_password)"
      echo "REDIS_PASSWORD=$(gen_password)"
    } > .env
    log ".env 已生成（权限 600），请妥善保存"
  else
    log "使用已有 .env"
    grep -q '^MONGO_INITDB_ROOT_PASSWORD=mongodb_password' .env && warn "MONGO_INITDB_ROOT_PASSWORD 仍是默认值，建议修改"
    grep -q '^REDIS_PASSWORD=redis_password' .env && warn "REDIS_PASSWORD 仍是默认值，建议修改"
  fi
}

if [ "$CHECK_ONLY" = 1 ]; then
  require_docker
  require_compose
  log "docker:  $(docker --version)"
  log "compose: $(docker compose version --short 2>/dev/null || docker compose version)"
  "${COMPOSE[@]}" config -q && log "compose 配置校验通过"
  if [ -f .env ]; then log ".env 已存在"; else warn ".env 不存在（正式部署时会自动生成）"; fi
  exit 0
fi

require_docker
require_compose
ensure_env

if [ "$USE_CN" = 1 ]; then
  log "使用国内镜像覆盖：single-host-deployment.cn.yml"
fi

log "启动服务端与默认扫描节点..."
"${COMPOSE[@]}" up -d

log "等待 scope-sentry 通过健康检查（最多 180 秒）..."
i=0
while [ "$i" -lt 60 ]; do
  status=$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' scope-sentry 2>/dev/null || echo missing)
  if [ "$status" = "healthy" ]; then
    log "scope-sentry 已 healthy"
    break
  fi
  i=$((i + 1))
  if [ "$i" -ge 60 ]; then
    warn "等待健康检查超时，最近 50 行日志："
    "${COMPOSE[@]}" logs --tail 50 scope-sentry || true
    fail "服务未在 180 秒内 healthy，请检查 docker compose ps 与上面的日志"
  fi
  sleep 3
done

echo
log "初始账号与密码（从服务端日志提取，若为空请 docker logs scope-sentry 查看）："
docker logs scope-sentry 2>&1 | grep -iE 'password|密码' | head -n 10 || true
echo
log "Web 入口: http://<本机IP>:8082  （健康检查: /healthz）"
