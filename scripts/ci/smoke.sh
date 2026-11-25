#!/usr/bin/env bash
# 部署后冒烟检查（在生产机本地执行）。
#
# 为什么不能只看 healthz：healthz 只证明 HTTP 起来了。常见的失败是
#   版本号没打进去（自称 dev）、Mongo 连不上但 200 照返、节点被误删导致任务无处可跑。
# 这些 healthz 全过。
#
# 两个必须注意的点（都是踩出来的）：
#   1. 刚 restart 完服务还没监听，单发一次 curl 会拿到 000 → 必须轮询等它起来；
#   2. 退出码不可靠：sudo -n bash -c "exit 1" 经 ssh 回来仍是 0。
#      结论用最后一行 `SMOKE_RESULT=PASS|FAIL` 传递，调用方 grep 这一行。
#
# 用法：smoke.sh <期望版本>
set -uo pipefail
WANT=${1:-}
FAIL=0

chk() {
  local name="$1" got="$2" want="$3"
  if [ "$got" = "$want" ]; then
    echo "   OK   $name = $got"
  else
    echo "   FAIL $name = $got (期望 $want)"
    FAIL=1
  fi
}

echo "== 冒烟 =="

# 1) HTTP 活着 —— 最多等 120 秒（刚重启时端口还没监听）
code=000
for i in $(seq 1 40); do
  code=$(curl -sS -m 3 -o /dev/null -w "%{http_code}" http://127.0.0.1:8082/healthz 2>/dev/null || echo 000)
  [ "$code" = "200" ] && break
  sleep 3
done
chk healthz "$code" 200

# 2) 版本号真的打进去了（不注入 ldflags 会自称 dev）
ver=$(docker logs --since 10m scope-sentry 2>&1 | grep -a "version:" | tail -1 | sed "s/.*version: *//" | tr -d "\r")
if [ -z "$ver" ]; then echo "   FAIL version = (日志里没有 version 行)"; FAIL=1
elif [ -n "$WANT" ] && [ "$ver" != "$WANT" ]; then echo "   FAIL version = $ver (期望 $WANT)"; FAIL=1
else echo "   OK   version = $ver"; fi

# 3) Mongo 可用（healthz 不查库）
db=$(docker exec scopesentry-mongodb mongosh --quiet --eval 'const a=db.getSiblingDB("admin");a.auth(process.env.MONGO_INITDB_ROOT_USERNAME,process.env.MONGO_INITDB_ROOT_PASSWORD);const s=db.getSiblingDB("ScopeSentry");print(s.asset.estimatedDocumentCount())' 2>/dev/null | tail -1 | tr -d "\r")
if [ -z "$db" ] || ! [ "$db" -gt 0 ] 2>/dev/null; then echo "   FAIL mongo assets = ${db:-空}"; FAIL=1; else echo "   OK   mongo assets = $db"; fi

# 4) 至少有一个节点（节点被清空则所有任务都无法推进）
nodes=$(docker exec scopesentry-redis redis-cli -a "${REDIS_PASS:-ac6b0cf473fb8d23a0e13ff3}" --no-auth-warning --scan --pattern "node:*" 2>/dev/null | grep -vc ":currentTask$")
if [ "${nodes:-0}" -ge 1 ] 2>/dev/null; then echo "   OK   nodes = $nodes"; else echo "   FAIL nodes = ${nodes:-0}"; FAIL=1; fi

if [ "$FAIL" = "0" ]; then
  echo "== 冒烟全过 =="
  echo "SMOKE_RESULT=PASS"
else
  echo "== 冒烟失败 =="
  echo "SMOKE_RESULT=FAIL"
fi
exit $FAIL
