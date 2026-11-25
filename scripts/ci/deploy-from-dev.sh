#!/usr/bin/env bash
# 在开发机（dev-tx-sh-app-01）构建并部署 ScopeSentry 服务端。
#
# 为什么不用生产机自己从 GitHub 拉：codeload 40MB 要 20 分钟（实测 39 KB/s）。
# 开发机到 GitHub 4.9 MB/s、原生 Go 可用、到生产 1.4 MB/s：
#   拉源码 32s（首次）/ 秒级（增量） + 构建 47s 冷 / 5s 热 + 传输 ~73s + 重启 6s ≈ 1.5–2 分钟。
#
# 用法：~/ss-build/deploy.sh [branch]
set -uo pipefail
REPO=https://github.com/NightGlowww/ScopeSentry.git
SRC=$HOME/ss-build/ScopeSentry
BRANCH=${1:-main}
PROD=ubuntu@1.15.46.212
T0=$(date +%s)

echo "== [1/5] 更新源码 =="
if [ ! -d "$SRC/.git" ]; then
  git clone --depth 1 -b "$BRANCH" "$REPO" "$SRC" || exit 1
else
  git -C "$SRC" fetch --depth 1 origin "$BRANCH" -q && git -C "$SRC" reset --hard "origin/$BRANCH" -q || exit 1
fi
COMMIT=$(git -C "$SRC" rev-parse --short HEAD)
echo "   commit=$COMMIT"

echo "== [2/5] 版本 =="
VERSION=$(python3 -c "import json;print(json.load(open('$SRC/version.json'))['server'])" 2>/dev/null || echo "")
VERSION=${VERSION#v}
if [ -z "$VERSION" ]; then VERSION="0.0.0-$COMMIT"; fi
echo "   version=$VERSION"

echo "== [3/5] 构建 =="
cd "$SRC" || exit 1
rm -f /tmp/ScopeSentry.new
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 GOPROXY=https://goproxy.cn,direct \
  go build -trimpath \
  -ldflags "-s -w -X github.com/Autumn-27/ScopeSentry/internal/constants.Version=$VERSION" \
  -o /tmp/ScopeSentry.new ./cmd/main/main.go || { echo "   build failed"; exit 1; }
if [ ! -s /tmp/ScopeSentry.new ]; then echo "   build produced no binary"; exit 1; fi
echo "   size=$(stat -c %s /tmp/ScopeSentry.new)"

echo "== [4/5] 传输 =="
scp -q -o BatchMode=yes -o StrictHostKeyChecking=no /tmp/ScopeSentry.new "$PROD:/tmp/ScopeSentry.new.tmp" || { echo "   scp failed"; exit 1; }
echo "   scp done at $(( $(date +%s) - T0 ))s"

echo "== [5/5] 替换并重启 =="
# 注意：heredoc 必须加引号，否则 $c / $(seq) 会在开发机这一侧被展开（踩过一次）
ssh -o BatchMode=yes -o StrictHostKeyChecking=no "$PROD" 'sudo -n bash -s' <<'REMOTE'
set -e
docker cp /tmp/ScopeSentry.new.tmp scope-sentry:/opt/ScopeSentry/ScopeSentry.next
docker exec scope-sentry sh -c "mv /opt/ScopeSentry/ScopeSentry.next /opt/ScopeSentry/ScopeSentry"
cp /tmp/ScopeSentry.new.tmp /home/ubuntu/ScopeSentry.prev 2>/dev/null || true
docker restart scope-sentry >/dev/null
c=000
for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20 21 22 23 24 25 26 27 28 29 30 31 32 33 34 35 36 37 38 39 40; do
  sleep 3
  c=$(curl -sS -m 3 -o /dev/null -w "%{http_code}" http://127.0.0.1:8082/healthz || true)
  [ "$c" = "200" ] && break
done
echo "   healthz=$c"
docker logs --since 60s scope-sentry 2>&1 | grep -a "version:" | tail -1 || true
REMOTE
RC=$?
echo "TOTAL=$(( $(date +%s) - T0 ))s commit=$COMMIT version=$VERSION remote_rc=$RC"
exit $RC