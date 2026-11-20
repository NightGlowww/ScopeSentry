#!/usr/bin/env bash
# 清理旧的服务端镜像：保留最近 N 个版本 tag（当前 + 上两个用于回滚）。
# 背景：docker commit 每个版本生成一条 ~10GB 层链，不清理会涨到几十 GB
# （2026-11-20 实测 461 个镜像 / 42GB）。
set -uo pipefail
PREFIX="${1:-autumn27/scopesentry}"
KEEP="${2:-3}"

run() {
  if docker ps >/dev/null 2>&1; then bash -c "$1"; else sg docker -c "$1"; fi
}

ALL=$(run "docker images $PREFIX --format \"{{.Repository}}:{{.Tag}}\"" | grep -E ":v[0-9]+\.[0-9]+\.[0-9]+$" || true)
KEEP_LIST=$(echo "$ALL" | sed "s/.*://" | sort -V | tail -n "$KEEP" | tr "\n" " ")
echo "keep: $KEEP_LIST"

REMOVED=0
for img in $ALL; do
  tag="${img##*:}"
  case " $KEEP_LIST " in
    *" $tag "*) ;;
    *) run "docker rmi $img" >/dev/null 2>&1 && REMOVED=$((REMOVED+1)) ;;
  esac
done

run "docker image prune -f" >/dev/null 2>&1 || true
echo "removed: $REMOVED"
df -h / | tail -1