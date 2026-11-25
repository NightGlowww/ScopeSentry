#!/usr/bin/env bash
# 部署后冒烟检查（在生产机本地执行）。
#
# 为什么不能只看 healthz：healthz 只证明 HTTP 起来了。
# 见过的失败包括——版本号没打进去（自称 dev）、Mongo 连不上但 200 照返、
# 节点被误删导致任务无处可跑。这些 healthz 全过。
#
# 注意：退出码不可靠。实测 sudo -n bash -c "exit 1" 经 ssh 回来仍是 0，
# 所以结论用最后一行 `SMOKE_RESULT=PASS|FAIL` 传递，调用方 grep 这一行。
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
code=$(curl -sS -m 5 -o /dev/null -w '%{http_code}' http://127.0.0.1:8082/healthz || echo 000)
chk healthz "$code" 200

ver=$(docker logs --since 5m scope-sentry 2>&1 | grep -a 'version:' | tail -1 | sed 's/.*version: *//' | tr -d '\r')
if [ -z "$ver" ]; then echo "   FAIL version = (日志里没有 version 行)"; FAIL=1
elif [ -n "$WANT" ] && [ "$ver" != "$WANT" ]; then echo "   FAIL version = $ver (期望 $WANT)"; FAIL=1
else echo "   OK   version = $ver"; fi

db=$(docker exec scopesentry-mongodb mongosh --quiet --eval 'const a=db.getSiblingDB("admin");a.auth(process.env.MONGO_INITDB_ROOT_USERNAME,process.env.MONGO_INITDB_ROOT_PASSWORD);const s=db.getSiblingDB("ScopeSentry");print(s.asset.estimatedDocumentCount())' 2>/dev/null | tail -1 | tr -d '\r')
if [ -z "$db" ] || ! [ "$db" -gt 0 ] 2>/dev/null; then echo "   FAIL mongo assets = ${db:-空}"; FAIL=1; else echo "   OK   mongo assets = $db"; fi

nodes=$(docker exec scopesentry-redis redis-cli -a "${REDIS_PASS:-ac6b0cf473fb8d23a0e13ff3}" --no-auth-warning --scan --pattern 'node:*' 2>/dev/null | grep -vc ':currentTask$')
if [ "${nodes:-0}" -ge 1 ] 2>/dev/null; then echo "   OK   nodes = $nodes"; else echo "   FAIL nodes = ${nodes:-0}"; FAIL=1; fi

if [ "$FAIL" = "0" ]; then
  echo "== 冒烟全过 =="
  echo "SMOKE_RESULT=PASS"
else
  echo "== 冒烟失败 =="
  echo "SMOKE_RESULT=FAIL"
fi
exit $FAIL
