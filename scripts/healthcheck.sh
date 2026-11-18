#!/usr/bin/env bash
# 主机恢复后的自检脚本：一次跑完「主机活着 / 容器在跑 / 应用健康 / 磁盘水位 / 节点在线」。
# 用法: scripts/healthcheck.sh [prod-tx-sh-app-02]
set -uo pipefail
HOSTS="${1:-prod-tx-sh-app-02}"
fail=0

echo "== 1) SSH 可达"
if ssh -o BatchMode=yes -o ConnectTimeout=8 "$HOSTS" 'echo ok' >/dev/null 2>&1; then
  echo "   ok"
else
  echo "   ✗ SSH 不可达"; exit 1
fi

echo "== 2) 容器状态"
ssh -o BatchMode=yes "$HOSTS" 'docker ps --format "{{.Names}} {{.Status}}" | sort' | sed 's/^/   /'
missing=$(ssh -o BatchMode=yes "$HOSTS" 'for c in scope-sentry scopesentry-mongodb scopesentry-redis scopesentry-scan; do docker inspect -f "{{.State.Running}}" $c 2>/dev/null | grep -q true || echo $c; done')
if [[ -n "$missing" ]]; then echo "   ✗ 未运行: $missing"; fail=1; fi

echo "== 3) 应用健康"
HZ=$(ssh -o BatchMode=yes "$HOSTS" 'curl -fsS -m 8 http://127.0.0.1:8082/healthz' 2>/dev/null)
if [[ -z "$HZ" ]]; then
  echo "   ✗ /healthz 无响应"; fail=1
else
  python3 - "$HZ" <<'PY'
import json,sys
d=json.loads(sys.argv[1])
h=d.get("host") or {}
print("   version:", d.get("version"), "| status:", d.get("status"))
print("   disk free: %.1f%% (%d/%d bytes)" % (h.get("diskFreePercent",-1), h.get("diskFreeBytes",0), h.get("diskTotalBytes",0)))
print("   load1:", h.get("load1"), "| mem avail %:", h.get("memAvailablePercent"), "| cpus:", h.get("cpus"))
w=d.get("warnings") or []
print("   warnings:", w if w else "none")
sys.exit(0 if d.get("status")=="ok" else 2)
PY
  rc=$?
  [[ $rc -ne 0 ]] && { echo "   ✗ 健康状态非 ok"; fail=1; }
fi

echo "== 4) 磁盘水位（>85% 告警）"
ssh -o BatchMode=yes "$HOSTS" 'df -h / | awk "NR==2{print \$5, \$4}"' | sed 's/^/   used /'

echo "== 5) 扫描节点在线"
ssh -o BatchMode=yes "$HOSTS" 'docker exec -i scopesentry-mongodb mongosh -u admin -p a873b84b7830de8b6bc296ee --authenticationDatabase admin ScopeSentry --quiet --eval "print(db.node.countDocuments({}))" 2>/dev/null' | sed 's/^/   nodes recorded: /'

exit $fail
