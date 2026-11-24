#!/usr/bin/env bash
# 东京本机构建扫描端：git pull → go build → docker build → compose 切换。
# 上海上行只有 30-100 KB/s，传不了 164 MB 二进制；东京到 GitHub 有 4 MB/s。
set -euo pipefail

REPO="${REPO:-https://github.com/NightGlowww/ScopeSentry-Scan.git}"
BRANCH="${BRANCH:-main}"
SRC=/opt/scopesentry/src/ScopeSentry-Scan
BUILD=/opt/scopesentry/scan/build
STAMP=$(date +%Y%m%d-%H%M)

echo "== [1/5] 同步源码 =="
if [ -d "$SRC/.git" ]; then
  git -C "$SRC" fetch --depth 1 origin "$BRANCH"
  git -C "$SRC" reset --hard "origin/$BRANCH"
else
  mkdir -p "$(dirname "$SRC")"
  git clone --depth 1 -b "$BRANCH" "$REPO" "$SRC"
fi
COMMIT=$(git -C "$SRC" rev-parse --short HEAD)
echo "   commit=$COMMIT"

echo "== [2/5] 构建 linux/amd64 二进制 =="
mkdir -p /opt/scopesentry/gomod /opt/scopesentry/gocache
docker run --rm -v "$SRC":/app -w /app -v /opt/scopesentry/gomod:/go/pkg/mod -v /opt/scopesentry/gocache:/root/.cache/go-build -e GOPROXY=https://goproxy.cn,direct -e GOOS=linux -e GOARCH=amd64 -e CGO_ENABLED=0 golang:1.24 go build -trimpath -ldflags "-s -w" -o /app/ScopeSentry-Scan ./cmd/ScopeSentry/main.go
SHA=$(sha256sum "$SRC/ScopeSentry-Scan" | cut -d" " -f1)
echo "   sha=${SHA:0:16} size=$(stat -c %s "$SRC/ScopeSentry-Scan")"

echo "== [3/5] 组装构建上下文 =="
mkdir -p "$BUILD"
cp "$SRC/ScopeSentry-Scan" "$BUILD/ScopeSentry-Scan"
chmod 0755 "$BUILD/ScopeSentry-Scan"
cp /opt/scopesentry/scan/state/healthcheck.py "$BUILD/healthcheck.py"

echo "== [4/5] 构建镜像 =="
TAG="scopesentry-scan-jp:git-${COMMIT}-${STAMP}"
docker build --network none -t "$TAG" "$BUILD" >/dev/null
echo "   image=$TAG"

echo "== [5/5] 切换并重启 =="
cd /opt/scopesentry/scan
cp compose.yaml "compose.yaml.bak-${STAMP}"
sed -i "s|image: scopesentry-scan-jp:.*|image: ${TAG}|" compose.yaml
docker compose up -d scan
echo "   已切换到 $TAG"
