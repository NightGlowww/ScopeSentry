#!/usr/bin/env bash
# 集成测试：跑全仓所有带 *_integration_test.go 的包（30+ 个），需要真实 Mongo + Redis。
#
# 为什么是全仓而不是点名几个包：脚本长期只跑 internal/api/routes，其余用例
# （icp、task/common、mcp、assets/* 等）从来没被执行过，红着也没人知道。
# 2026-11-29 首次全量跑，当场抓出两个包在失败。
#
# 没有 Mongo/Redis 的话可以用 VMPal 容器起两个（本地 127.0.0.1）：
#   vmpal_run_container image=mongo:7 name=ss-test-mongo ports=[{hostPort:27017,containerPort:27017}]
#   vmpal_run_container image=redis:7 name=ss-test-redis ports=[{hostPort:6379,containerPort:6379}]
#
# 用法：bash scripts/ci/integration.sh
#       MONGODB_IP=10.0.0.5 MONGODB_PORT=27017 REDIS_IP=10.0.0.6 bash scripts/ci/integration.sh
set -uo pipefail
cd "$(dirname "$0")/../.." || exit 2

export MONGODB_IP=${MONGODB_IP:-127.0.0.1}
export MONGODB_PORT=${MONGODB_PORT:-27017}
export REDIS_IP=${REDIS_IP:-127.0.0.1}
export REDIS_PORT=${REDIS_PORT:-6379}
# 每次跑用独立库，避免上一轮残留数据把用例带偏
export MONGODB_DATABASE=${MONGODB_DATABASE:-ss_it_$(date +%s)}

echo "== 集成测试 (mongo $MONGODB_IP:$MONGODB_PORT/$MONGODB_DATABASE, redis $REDIS_IP:$REDIS_PORT) =="
# -v 是为了数 --- PASS 的个数：不带 -v 只有一行 ok，脚本就没法报「通过几个用例」
#
# -p 1 必须加：不同包默认并行跑各自的测试二进制，多个包同时连同一个库做初始化/建索引
# 会互相踩（实测：并行时 user_username_unique 检查假失败，单独跑就过）。
out=$(go test -tags integration -count=1 -p 1 -timeout 20m -v ./internal/... 2>&1)
rc=$?
pass=$(printf '%s' "$out" | grep -c '^--- PASS' || true)
if [ "$rc" = "0" ]; then
  printf '  通过用例 %s 个\n' "${pass:-?}"
  echo 'INTEGRATION=PASS'
  exit 0
fi
printf '  通过用例 %s 个，下面是被判失败的包：\n' "${pass:-?}"
printf '%s\n' "$out" | grep -E '^FAIL\s' | head -20
printf '%s\n' "$out" | grep -E '^(--- FAIL|    [a-z_0-9]+\.go:[0-9]+:)' | head -20
if printf '%s' "$out" | grep -q 'connection refused'; then
  echo '  提示：连不上 Mongo/Redis。先起两个容器（见本脚本头部注释），或传 MONGODB_IP/REDIS_IP。'
fi
echo 'INTEGRATION=FAIL'
exit 1
