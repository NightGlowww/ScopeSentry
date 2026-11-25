#!/usr/bin/env bash
# 在开发机（dev-tx-sh-app-01）构建并部署 ScopeSentry 服务端。
#
# 通道选择依据（实测）：
#   生产→GitHub codeload  39 KB/s（40MB≈20min）   ← 不可用
#   生产→GitHub git clone 300s 超时                ← 不可用
#   开发机→GitHub         4.9 MB/s                 ← 用它
#   开发机→生产 scp       1.4 MB/s                 ← 用它
#
# 压缩档位（zstd，100MB 二进制）：level 9 → 27.5MB/1s 最优；
#   level 19 省下的传输还不够补压缩时间。
#
# 部署后跑冒烟：healthz / 版本 / Mongo / 节点数。任一项不过 → 自动回滚到上一个二进制。
#   注意：结论靠 `SMOKE_RESULT=PASS|FAIL` 传递，因为 sudo 经 ssh 会把退出码吞成 0。
#
# 用法：~/ss-build/deploy.sh [branch]
set -uo pipefail
REPO=https://github.com/NightGlowww/ScopeSentry.git
SRC=$HOME/ss-build/ScopeSentry
BRANCH=${1:-main}
PROD=ubuntu@1.15.46.212
ZLEVEL=9
T0=$(date +%s)

echo "== [1/7] 更新源码 =="
if [ ! -d "$SRC/.git" ]; then
  git clone --depth 1 -b "$BRANCH" "$REPO" "$SRC" || exit 1
else
  git -C "$SRC" fetch --depth 1 origin "$BRANCH" -q && git -C "$SRC" reset --hard "origin/$BRANCH" -q || exit 1
fi
COMMIT=$(git -C "$SRC" rev-parse --short HEAD)
echo "   commit=$COMMIT"

echo "== [2/7] 版本 =="
VERSION=$(python3 -c "import json;print(json.load(open('$SRC/version.json'))['server'])" 2>/dev/null || echo "")
VERSION=${VERSION#v}
if [ -z "$VERSION" ]; then VERSION="0.0.0-$COMMIT"; fi
echo "   version=$VERSION"

echo "== [3/7] 构建 =="
cd "$SRC" || exit 1
rm -f /tmp/ScopeSentry.new
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 GOPROXY=https://goproxy.cn,direct \
  go build -trimpath \
  -ldflags "-s -w -X github.com/Autumn-27/ScopeSentry/internal/constants.Version=$VERSION" \
  -o /tmp/ScopeSentry.new ./cmd/main/main.go || { echo "   build failed"; exit 1; }
if [ ! -s /tmp/ScopeSentry.new ]; then echo "   build produced no binary"; exit 1; fi
echo "   size=$(stat -c %s /tmp/ScopeSentry.new) at $(( $(date +%s) - T0 ))s"

echo "== [4/7] 压缩 =="
rm -f /tmp/ScopeSentry.new.zst
zstd -q -$ZLEVEL -T0 -f -o /tmp/ScopeSentry.new.zst /tmp/ScopeSentry.new || { echo "   zstd failed"; exit 1; }
echo "   $(stat -c %s /tmp/ScopeSentry.new.zst) bytes at $(( $(date +%s) - T0 ))s"

echo "== [5/7] 传输（二进制 + 冒烟脚本）=="
scp -q -o BatchMode=yes -o StrictHostKeyChecking=no /tmp/ScopeSentry.new.zst "$PROD:/tmp/ScopeSentry.new.zst" || { echo "   scp failed"; exit 1; }
scp -q -o BatchMode=yes -o StrictHostKeyChecking=no "$SRC/scripts/ci/smoke.sh" "$PROD:/tmp/smoke.sh" || { echo "   smoke scp failed"; exit 1; }
echo "   scp done at $(( $(date +%s) - T0 ))s"

echo "== [6/7] 解压 + 替换 + 重启 =="
ssh -o BatchMode=yes -o StrictHostKeyChecking=no "$PROD" 'bash -s' <<'REMOTE'
set -uo pipefail
sudo -n mkdir -p /opt/scopesentry
sudo -n mv /tmp/smoke.sh /opt/scopesentry/smoke.sh && sudo -n chmod +x /opt/scopesentry/smoke.sh
zstd -q -d -f -o /tmp/ScopeSentry.new /tmp/ScopeSentry.new.zst || exit 1
test -s /tmp/ScopeSentry.new || exit 1
sudo -n docker cp /tmp/ScopeSentry.new scope-sentry:/opt/ScopeSentry/ScopeSentry.next || exit 1
sudo -n docker exec scope-sentry sh -c "mv /opt/ScopeSentry/ScopeSentry.next /opt/ScopeSentry/ScopeSentry" || exit 1
sudo -n cp /tmp/ScopeSentry.new /home/ubuntu/ScopeSentry.prev 2>/dev/null || true
rm -f /tmp/ScopeSentry.new /tmp/ScopeSentry.new.zst
sudo -n docker restart scope-sentry >/dev/null
REMOTE
if [ $? -ne 0 ]; then echo "   远端替换失败"; exit 1; fi
echo "   replaced at $(( $(date +%s) - T0 ))s"

echo "== [7/7] 冒烟 =="
OUT=$(ssh -o BatchMode=yes -o StrictHostKeyChecking=no "$PROD" "sudo -n bash /opt/scopesentry/smoke.sh $VERSION" 2>&1)
echo "$OUT" | sed "s/^/   /"
if echo "$OUT" | grep -q "SMOKE_RESULT=PASS"; then
  echo "TOTAL=$(( $(date +%s) - T0 ))s commit=$COMMIT version=$VERSION result=PASS"
  exit 0
fi

echo "== 冒烟未通过，回滚到上一个二进制 =="
ssh -o BatchMode=yes -o StrictHostKeyChecking=no "$PROD" 'bash -s' <<'ROLLBACK'
set -uo pipefail
if ! sudo -n test -s /home/ubuntu/ScopeSentry.prev; then echo "   没有可回滚的副本"; exit 1; fi
sudo -n docker cp /home/ubuntu/ScopeSentry.prev scope-sentry:/opt/ScopeSentry/ScopeSentry.next
sudo -n docker exec scope-sentry sh -c "mv /opt/ScopeSentry/ScopeSentry.next /opt/ScopeSentry/ScopeSentry"
sudo -n docker restart scope-sentry >/dev/null
sleep 8
sudo -n bash /opt/scopesentry/smoke.sh 2>&1 | tail -6
ROLLBACK
echo "TOTAL=$(( $(date +%s) - T0 ))s commit=$COMMIT version=$VERSION result=ROLLED_BACK"
exit 1