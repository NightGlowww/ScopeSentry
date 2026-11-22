#!/usr/bin/env bash
# 守护检查：只在规则缺失时才重装（避免每分钟删/加导致计数器归零与瞬时漏包）。
set -euo pipefail
CID=$(docker inspect scopesentry-scan --format '{{.Id}}' 2>/dev/null || true)
[ -z "$CID" ] && exit 0
CG="system.slice/docker-${CID}.scope"
NAT=$(iptables -t nat -S OUTPUT | grep -c -- "cgroup --path \"$CG\".*-j REDIRECT --to-ports 7893" || true)
FLT=$(iptables -S OUTPUT | grep -c -- "cgroup --path \"$CG\".*dports" || true)
if [ "$NAT" -ge 1 ] && [ "$FLT" -ge 1 ]; then
  exit 0
fi
logger -t ss-tproxy-check "egress rules missing (nat=$NAT flt=$FLT); re-applying"
/usr/local/bin/ss-tproxy.sh
