#!/usr/bin/env bash
# 主机恢复后的一键收尾：自检 → 部署 → 扫描容器限载生效 → 复检。
# 用法: scripts/recover.sh [version]
#   省略 version 时用 git 最新 tag（v 前缀会自动去掉）
set -uo pipefail
HOST="${PROD_HOST:-prod-tx-sh-app-02}"
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." >/dev/null 2>&1 && pwd -P)"

VERSION="${1:-}"
if [[ -z "$VERSION" ]]; then
  VERSION=$(git -C "$ROOT" describe --tags --abbrev=0 2>/dev/null | sed 's/^v//')
fi
if [[ -z "$VERSION" ]]; then
  echo "✗ 无法确定版本号，请显式传参：scripts/recover.sh 2.0.130" >&2
  exit 2
fi
echo "== 恢复目标版本: $VERSION   主机: $HOST"

echo "== [1/4] 恢复前自检"
if ! bash "$ROOT/scripts/healthcheck.sh" "$HOST"; then
  echo "✗ 自检失败：主机/容器/应用仍未就绪，先别部署" >&2
  exit 1
fi

echo "== [2/4] 部署 v$VERSION"
if ! bash "$ROOT/scripts/release.sh" "$VERSION"; then
  echo "✗ 部署失败，见上方 release 输出" >&2
  exit 1
fi

echo "== [3/4] 扫描容器限载生效（重建 scopesentry-scan）"
ssh -o BatchMode=yes "$HOST" "cd /opt/scopesentry/ScopeSentry && docker compose -f single-host-deployment.yml -f docker-compose.fork.yml up -d --no-deps scopesentry-scan && docker inspect -f '{{.Name}} cpus={{.HostConfig.NanoCpus}} mem={{.HostConfig.Memory}}' scopesentry-scan"

echo "== [4/4] 部署后复检"
bash "$ROOT/scripts/healthcheck.sh" "$HOST"
rc=$?
if [[ $rc -eq 0 ]]; then
  echo "✅ 恢复完成：v$VERSION 已上线，主机资源正常"
else
  echo "⚠️ 部署完成但复检未全绿，请查看上面输出"
fi
exit $rc
